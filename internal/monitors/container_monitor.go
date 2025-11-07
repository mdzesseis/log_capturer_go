package monitors

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/docker"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/selfguard"
	"ssw-logs-capture/pkg/types"
	"ssw-logs-capture/pkg/validation"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// StreamPool manages container log stream lifecycle and enforces limits
type StreamPool struct {
	maxStreams      int
	activeStreams   map[string]*streamInfo
	streamSemaphore chan struct{}
	mu              sync.RWMutex
}

// streamInfo tracks metadata about an active stream
type streamInfo struct {
	containerID   string
	containerName string
	createdAt     time.Time
	lastActive    time.Time
}

// NewStreamPool creates a new stream pool with the specified capacity
func NewStreamPool(maxStreams int) *StreamPool {
	return &StreamPool{
		maxStreams:      maxStreams,
		activeStreams:   make(map[string]*streamInfo),
		streamSemaphore: make(chan struct{}, maxStreams),
	}
}

// AcquireSlot attempts to acquire a slot in the stream pool
func (sp *StreamPool) AcquireSlot(containerID, containerName string) error {
	// Check if already exists (prevent double-acquire)
	sp.mu.Lock()
	if _, exists := sp.activeStreams[containerID]; exists {
		sp.mu.Unlock()
		return fmt.Errorf("container %s already has an active stream slot", containerID)
	}
	sp.mu.Unlock()

	// Try to acquire semaphore slot
	select {
	case sp.streamSemaphore <- struct{}{}:
		// Semaphore acquired, now register in map
		sp.mu.Lock()
		sp.activeStreams[containerID] = &streamInfo{
			containerID:   containerID,
			containerName: containerName,
			createdAt:     time.Now(),
			lastActive:    time.Now(),
		}
		activeCount := len(sp.activeStreams)
		sp.mu.Unlock()

		// Update metrics
		metrics.UpdateActiveStreams(activeCount)
		metrics.UpdateStreamPoolUtilization(activeCount, sp.maxStreams)
		return nil
	default:
		return fmt.Errorf("stream pool at capacity (%d/%d)", sp.maxStreams, sp.maxStreams)
	}
}

// ReleaseSlot releases a slot in the stream pool
func (sp *StreamPool) ReleaseSlot(containerID string) {
	sp.mu.Lock()
	// Check if this container actually has a slot before releasing
	_, exists := sp.activeStreams[containerID]
	if !exists {
		sp.mu.Unlock()
		// Silent return - container was not in pool (already released or never acquired)
		return
	}

	// Remove from active streams
	delete(sp.activeStreams, containerID)
	activeCount := len(sp.activeStreams)
	sp.mu.Unlock()

	// Only release semaphore if we actually had a slot
	<-sp.streamSemaphore

	// Update metrics
	metrics.UpdateActiveStreams(activeCount)
	metrics.UpdateStreamPoolUtilization(activeCount, sp.maxStreams)
}

// UpdateActivity updates the last active time for a stream
func (sp *StreamPool) UpdateActivity(containerID string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if info, exists := sp.activeStreams[containerID]; exists {
		info.lastActive = time.Now()
	}
}

// GetActiveCount returns the current number of active streams
func (sp *StreamPool) GetActiveCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.activeStreams)
}

// ContainerMonitor monitora containers Docker
type ContainerMonitor struct {
	config          types.DockerConfig
	dispatcher         types.Dispatcher
	logger             *logrus.Logger
	taskManager        types.TaskManager
	positionManager    *positions.PositionBufferManager
	timestampValidator *validation.TimestampValidator
	feedbackGuard      *selfguard.FeedbackGuard

	dockerPool    *docker.PoolManager
	containers    map[string]*monitoredContainer
	streamPool    *StreamPool
	mutex         sync.RWMutex

	rotationInterval time.Duration

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
}

// monitoredContainer representa um container sendo monitorado
type monitoredContainer struct {
	id              string
	name            string
	image           string
	labels          map[string]string
	since           time.Time
	stream          io.ReadCloser
	mu              sync.Mutex     // Protects stream access
	lastRead        time.Time
	cancel          context.CancelFunc
	heartbeatWg     sync.WaitGroup // Rastreia goroutine de heartbeat (vida do container)
	readerWg        sync.WaitGroup // Rastreia goroutine de reader (vida de cada stream)
	streamCreatedAt time.Time      // When current stream was created
	rotationCount   int            // Number of rotations performed
}

// NewContainerMonitor cria um novo monitor de containers
func NewContainerMonitor(config types.DockerConfig, timestampConfig types.TimestampValidationConfig, dispatcher types.Dispatcher, taskManager types.TaskManager, positionManager *positions.PositionBufferManager, logger *logrus.Logger) (*ContainerMonitor, error) {
	// Converter config para o formato do validation package
	validationConfig := validation.Config{
		Enabled:             timestampConfig.Enabled,
		MaxPastAgeSeconds:   timestampConfig.MaxPastAgeSeconds,
		MaxFutureAgeSeconds: timestampConfig.MaxFutureAgeSeconds,
		ClampEnabled:        timestampConfig.ClampEnabled,
		ClampDLQ:            timestampConfig.ClampDLQ,
		InvalidAction:       timestampConfig.InvalidAction,
		DefaultTimezone:     timestampConfig.DefaultTimezone,
		AcceptedFormats:     timestampConfig.AcceptedFormats,
	}
	timestampValidator := validation.NewTimestampValidator(validationConfig, logger, nil)

	// Criar feedback guard com configuração padrão
	feedbackConfig := selfguard.Config{
		Enabled:                  false,
		SelfIDShort:              "log_capturer_go",
		SelfContainerName:        "log_capturer_go",
		SelfNamespace:            "ssw",
		AutoDetectSelf:           true,
		SelfLogAction:            "drop",
		ExcludeContainerPatterns: []string{"log_capturer_go"},
		ExcludeMessagePatterns:   []string{".*ssw-logs-capture.*"},
	}
	feedbackGuard := selfguard.NewFeedbackGuard(feedbackConfig, logger)

	if !config.Enabled {
		return &ContainerMonitor{
			config:             config,
			dispatcher:         dispatcher,
			logger:             logger,
			taskManager:        taskManager,
			positionManager:    positionManager,
			timestampValidator: timestampValidator,
			feedbackGuard:      feedbackGuard,
			isRunning:          false,
		}, nil
	}

	// Criar pool de conexões Docker
	poolConfig := docker.PoolConfig{
		PoolSize:            5, // Default pool size
		SocketPath:          config.SocketPath,
		MaxRetries:          3,
		RetryDelay:          5 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		ConnectionTimeout:   30 * time.Second,
		IdleTimeout:        5 * time.Minute,
	}

	dockerPool, err := docker.NewPoolManager(poolConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker connection pool: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize stream pool with max 50 concurrent streams
	streamPool := NewStreamPool(50)

	// Set rotation interval to 5 minutes
	rotationInterval := 5 * time.Minute

	return &ContainerMonitor{
		config:             config,
		dispatcher:         dispatcher,
		logger:             logger,
		taskManager:        taskManager,
		positionManager:    positionManager,
		timestampValidator: timestampValidator,
		feedbackGuard:      feedbackGuard,
		dockerPool:         dockerPool,
		containers:         make(map[string]*monitoredContainer),
		streamPool:         streamPool,
		rotationInterval:   rotationInterval,
		ctx:                ctx,
		cancel:             cancel,
	}, nil
}

// Start inicia o monitor de containers
func (cm *ContainerMonitor) Start(ctx context.Context) error {
	if !cm.config.Enabled {
		cm.logger.Info("Container monitor disabled")
		return nil
	}

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.isRunning {
		return fmt.Errorf("container monitor already running")
	}

	cm.isRunning = true
	cm.logger.Info("Starting container monitor")

	// Set component health metric
	metrics.SetComponentHealth("monitor", "container_monitor", true)

	// Verificar conectividade com Docker
	if err := cm.checkDockerConnection(); err != nil {
		metrics.SetComponentHealth("monitor", "container_monitor", false)
		return fmt.Errorf("docker connection check failed: %w", err)
	}

	// Iniciar task de monitoramento principal
	if err := cm.taskManager.StartTask(ctx, "container_monitor", cm.monitorLoop); err != nil {
		metrics.SetComponentHealth("monitor", "container_monitor", false)
		return fmt.Errorf("failed to start container monitor task: %w", err)
	}

	// Iniciar task de monitoramento de eventos
	if err := cm.taskManager.StartTask(ctx, "container_events", cm.eventsLoop); err != nil {
		return fmt.Errorf("failed to start container events task: %w", err)
	}

	// Iniciar task de health check
	if err := cm.taskManager.StartTask(ctx, "container_health_check", cm.healthCheckLoop); err != nil {
		return fmt.Errorf("failed to start container health check task: %w", err)
	}

	return nil
}

// Stop para o monitor de containers
func (cm *ContainerMonitor) Stop() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if !cm.isRunning {
		return nil
	}

	cm.logger.Info("Stopping container monitor")
	cm.isRunning = false

	// Update component health
	metrics.SetComponentHealth("monitor", "container_monitor", false)

	// Cancelar contexto
	cm.cancel()

	// Parar tasks
	cm.taskManager.StopTask("container_monitor")
	cm.taskManager.StopTask("container_events")
	cm.taskManager.StopTask("container_health_check")

	// Parar monitoramento de containers - coletamos IDs primeiro para evitar concurrent map iteration/write
	containerIDs := make([]string, 0, len(cm.containers))
	for _, mc := range cm.containers {
		containerIDs = append(containerIDs, mc.id)
	}
	for _, id := range containerIDs {
		cm.stopContainerMonitoring(id)
	}

	// Fechar cliente Docker
	if cm.dockerPool != nil {
		cm.dockerPool.Close()
	}

	return nil
}

// IsHealthy verifica se o monitor está saudável
func (cm *ContainerMonitor) IsHealthy() bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.isRunning && cm.dockerPool != nil
}

// GetStatus retorna o status do monitor
func (cm *ContainerMonitor) GetStatus() types.MonitorStatus {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	return types.MonitorStatus{
		Name:      "container_monitor",
		IsRunning: cm.isRunning,
		IsHealthy: cm.isRunning && cm.dockerPool != nil,
	}
}

// checkDockerConnection verifica conectividade com Docker
func (cm *ContainerMonitor) checkDockerConnection() error {
	// O pool manager já gerencia as conexões e health checks
	// Apenas retornar nil aqui pois o pool cuida da conectividade
	return nil
}

// monitorLoop loop principal de monitoramento
func (cm *ContainerMonitor) monitorLoop(ctx context.Context) error {
	// Varredura inicial de containers - única vez
	cm.logger.Info("Performing initial container discovery scan")
	if err := cm.scanContainers(); err != nil {
		cm.logger.WithError(err).Error("Initial container scan failed")
	} else {
		cm.logger.WithField("containers", len(cm.containers)).Info("Initial container discovery completed")
	}

	// Após varredura inicial, apenas aguardar contexto ser cancelado
	// A descoberta agora é totalmente orientada a eventos
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Apenas heartbeat - sem scanning periódico
			cm.taskManager.Heartbeat("container_monitor")
		}
	}
}

// eventsLoop monitora eventos do Docker
func (cm *ContainerMonitor) eventsLoop(ctx context.Context) error {
	cm.logger.Info("Starting Docker events listener for container discovery")

	// Filtrar apenas eventos de containers
	eventFilters := filters.NewArgs()
	eventFilters.Add("type", "container")

	eventChan, errChan := cm.dockerPool.Events(ctx, dockerTypes.EventsOptions{
		Filters: eventFilters,
	})

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			cm.logger.Info("Docker events listener stopped")
			return nil

		case event := <-eventChan:
			cm.handleDockerEvent(event)

		case err := <-errChan:
			if err != nil {
				cm.logger.WithError(err).Error("Docker events stream error - attempting to reconnect")
				metrics.RecordError("container_monitor", "events_error")

				// Aguardar antes de reconectar
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(5 * time.Second):
					// Recriar stream de eventos
					eventChan, errChan = cm.dockerPool.Events(ctx, dockerTypes.EventsOptions{
						Filters: eventFilters,
					})
					cm.logger.Info("Docker events stream reconnected")
				}
			}

		case <-heartbeatTicker.C:
			cm.taskManager.Heartbeat("container_events")
		}
	}
}

// healthCheckLoop verifica saúde dos containers monitorados
func (cm *ContainerMonitor) healthCheckLoop(ctx context.Context) error {
	ticker := time.NewTicker(cm.config.HealthCheckDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cm.healthCheckContainers()
		}

		// Heartbeat
		cm.taskManager.Heartbeat("container_health_check")
	}
}

// scanContainers escaneia containers em execução
func (cm *ContainerMonitor) scanContainers() error {
	ctx, cancel := context.WithTimeout(cm.ctx, 30*time.Second)
	defer cancel()

	containers, err := cm.dockerPool.ContainerList(ctx, dockerTypes.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Containers em execução
	runningContainers := make(map[string]bool)
	for _, dockerContainer := range containers {
		containerID := dockerContainer.ID[:12]
		runningContainers[containerID] = true

		// Verificar se já está sendo monitorado
		if _, exists := cm.containers[containerID]; !exists {
			cm.startContainerMonitoring(dockerContainer)
		}
	}

	// Parar monitoramento de containers que não estão mais em execução
	// Coletamos IDs primeiro para evitar concurrent map iteration/write
	toRemove := make([]string, 0)
	for id := range cm.containers {
		if !runningContainers[id] {
			toRemove = append(toRemove, id)
		}
	}
	for _, id := range toRemove {
		cm.stopContainerMonitoring(id)
	}

	// Atualizar métricas
	metrics.SetActiveTasks("container_monitors", "running", len(cm.containers))

	return nil
}

// handleDockerEvent processa eventos do Docker de forma reativa
func (cm *ContainerMonitor) handleDockerEvent(event events.Message) {
	containerID := event.Actor.ID[:12]
	containerName := event.Actor.Attributes["name"]

	cm.logger.WithFields(logrus.Fields{
		"event":          event.Action,
		"container_id":   containerID,
		"container_name": containerName,
	}).Debug("Received Docker event")

	switch event.Action {
	case "start":
		// Container iniciado - adicionar diretamente
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": containerName,
		}).Info("Container started - adding to monitoring")

		// Aguardar um pouco para garantir que container está pronto
		taskName := "container_add_" + containerID
		cm.taskManager.StartTask(cm.ctx, taskName, func(ctx context.Context) error {
			defer cm.taskManager.StopTask(taskName) // Limpar task após conclusão

			select {
			case <-time.After(1 * time.Second):
				// Buscar informações completas do container
				return cm.addContainerByID(containerID)
			case <-ctx.Done():
				return ctx.Err()
			}
		})

	case "die", "stop":
		// Container parado
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": containerName,
		}).Info("Container stopped - removing from monitoring")

		cm.mutex.Lock()
		cm.stopContainerMonitoring(containerID)
		cm.mutex.Unlock()

		metrics.RecordContainerEvent("stopped", containerID)

	case "destroy":
		// Container removido - limpar posições
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": containerName,
		}).Info("Container destroyed - cleaning up positions")

		if cm.positionManager != nil {
			cm.positionManager.SetContainerStatus(containerID, "removed")
		}

		metrics.RecordContainerEvent("destroyed", containerID)

	case "pause":
		// Container pausado - apenas log
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": containerName,
		}).Debug("Container paused - monitoring continues")

		metrics.RecordContainerEvent("paused", containerID)

	case "unpause":
		// Container despausado - apenas log
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": containerName,
		}).Debug("Container unpaused - monitoring continues")

		metrics.RecordContainerEvent("unpaused", containerID)
	}
}

// addContainerByID adiciona um container específico para monitoramento por ID
func (cm *ContainerMonitor) addContainerByID(containerID string) error {
	ctx, cancel := context.WithTimeout(cm.ctx, 10*time.Second)
	defer cancel()

	// Buscar informações do container
	containers, err := cm.dockerPool.ContainerList(ctx, dockerTypes.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("id", containerID)),
	})

	if err != nil {
		cm.logger.WithError(err).WithField("container_id", containerID).Error("Failed to fetch container info")
		return fmt.Errorf("failed to list container %s: %w", containerID, err)
	}

	if len(containers) == 0 {
		cm.logger.WithField("container_id", containerID).Warn("Container not found")
		return fmt.Errorf("container %s not found", containerID)
	}

	dockerContainer := containers[0]

	// Verificar se já está sendo monitorado
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if _, exists := cm.containers[containerID]; exists {
		cm.logger.WithField("container_id", containerID).Debug("Container already being monitored")
		return nil
	}

	// Adicionar monitoramento
	cm.startContainerMonitoring(dockerContainer)

	return nil
}

// shouldMonitorContainer verifica se um container deve ser monitorado baseado nos filtros
func (cm *ContainerMonitor) shouldMonitorContainer(dockerContainer dockerTypes.Container) bool {
	name := strings.TrimPrefix(dockerContainer.Names[0], "/")
	labels := dockerContainer.Labels

	// Verificar nomes incluídos
	if len(cm.config.IncludeNames) > 0 {
		found := false
		for _, includeName := range cm.config.IncludeNames {
			if strings.Contains(name, includeName) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Verificar nomes excluídos
	for _, excludeName := range cm.config.ExcludeNames {
		if strings.Contains(name, excludeName) {
			return false
		}
	}

	// Verificar labels incluídas
	if len(cm.config.IncludeLabels) > 0 {
		for key, value := range cm.config.IncludeLabels {
			labelValue, exists := labels[key]
			if !exists {
				return false
			}
			if value != "" && labelValue != value {
				return false
			}
		}
	}

	// Verificar labels excluídas
	for key, value := range cm.config.ExcludeLabels {
		labelValue, exists := labels[key]
		if exists {
			if value == "" || labelValue == value {
				return false
			}
		}
	}

	return true
}

// startContainerMonitoring inicia monitoramento de um container
func (cm *ContainerMonitor) startContainerMonitoring(dockerContainer dockerTypes.Container) {
	containerID := dockerContainer.ID[:12]

	// Verificar se deve monitorar este container
	if !cm.shouldMonitorContainer(dockerContainer) {
		cm.logger.WithFields(logrus.Fields{
			"container_id":   containerID,
			"container_name": strings.TrimPrefix(dockerContainer.Names[0], "/"),
			"image":          dockerContainer.Image,
		}).Debug("Container filtered out, skipping monitoring")
		return
	}

	// Extrair nome e imagem
	name := strings.TrimPrefix(dockerContainer.Names[0], "/")
	image := dockerContainer.Image

	// Criar labels básicos
	labels := map[string]string{
		"container_id":   containerID,
		"container_name": name,
		"image":          image,
	}

	// Filtrar apenas labels essenciais do Docker para evitar excesso (Loki limite: 15)
	essentialDockerLabels := []string{
		"com.docker.compose.service",
		"com.docker.compose.container-number",
	}

	for _, essential := range essentialDockerLabels {
		if value, exists := dockerContainer.Labels[essential]; exists {
			// Usar nome simplificado para economizar espaço
			switch essential {
			case "com.docker.compose.service":
				labels["compose_service"] = value
			case "com.docker.compose.container-number":
				labels["instance"] = value
			}

			// Parar se já temos muitos labels (deixar espaço para labels do pipeline)
			if len(labels) >= 10 {
				break
			}
		}
	}

	// Get position-based since time, using container creation time for new containers
	sinceTime := time.Now()
	if cm.positionManager != nil {
		// Usar data de criação do container para containers novos
		createdTime := time.Unix(dockerContainer.Created, 0)
		sinceTime = cm.positionManager.GetContainerSinceWithCreated(containerID, createdTime)
		cm.positionManager.SetContainerStatus(containerID, "active")
	}

	mc := &monitoredContainer{
		id:       containerID,
		name:     name,
		image:    image,
		labels:   labels,
		since:    sinceTime,
		lastRead: time.Now(),
	}

	cm.containers[containerID] = mc

	// Iniciar task de monitoramento do container
	taskName := "container_" + containerID
	cm.taskManager.StartTask(cm.ctx, taskName, func(ctx context.Context) error {
		return cm.monitorContainer(ctx, mc)
	})

	// Atualizar métricas
	metrics.SetContainerMonitored(containerID, name, image, true)
	metrics.UpdateTotalContainersMonitored(len(cm.containers))

	cm.logger.WithFields(logrus.Fields{
		"container_id":   containerID,
		"container_name": name,
		"image":          image,
	}).Info("Started container monitoring")
}

// stopContainerMonitoring para monitoramento de um container
func (cm *ContainerMonitor) stopContainerMonitoring(containerID string) {
	mc, exists := cm.containers[containerID]
	if !exists {
		return
	}

	cm.logger.WithFields(logrus.Fields{
		"container_id":   containerID,
		"container_name": mc.name,
	}).Info("Stopping container monitoring")

	// CRITICAL: Close stream BEFORE canceling context
	// This interrupts the blocking stream.Read() syscall
	mc.mu.Lock()
	if mc.stream != nil {
		mc.stream.Close()
		mc.stream = nil
	}
	mc.mu.Unlock()

	// Now cancel context to signal goroutines to exit
	if mc.cancel != nil {
		mc.cancel()
	}

	// Parar task
	taskName := "container_" + containerID
	cm.taskManager.StopTask(taskName)

	// Update container status in position manager
	if cm.positionManager != nil {
		cm.positionManager.SetContainerStatus(containerID, "stopped")
	}

	// Remover do mapa
	delete(cm.containers, containerID)

	// Atualizar métricas
	metrics.SetContainerMonitored(containerID, mc.name, mc.image, false)
	metrics.UpdateTotalContainersMonitored(len(cm.containers))

	cm.logger.WithFields(logrus.Fields{
		"container_id":   containerID,
		"container_name": mc.name,
	}).Info("Stopped container monitoring")
}

// monitorContainer monitora logs de um container específico com rotation automática
func (cm *ContainerMonitor) monitorContainer(ctx context.Context, mc *monitoredContainer) error {
	// Acquire slot in stream pool
	if err := cm.streamPool.AcquireSlot(mc.id, mc.name); err != nil {
		cm.logger.WithError(err).WithFields(logrus.Fields{
			"container_id":   mc.id,
			"container_name": mc.name,
		}).Warn("Cannot monitor container - stream pool at capacity")
		metrics.RecordStreamError("pool_full", mc.id)
		return err
	}
	defer cm.streamPool.ReleaseSlot(mc.id)

	containerCtx, cancel := context.WithCancel(ctx)
	mc.cancel = cancel
	defer func() {
		cancel()
		// Aguardar heartbeat goroutine terminar (roda durante toda a vida do container)
		mc.heartbeatWg.Wait()
	}()

	// Enviar heartbeat em goroutine separada com ticker gerenciado internamente
	// Esta goroutine roda durante TODA a vida do container (não é recriada nas rotações)
	taskName := "container_" + mc.id
	mc.heartbeatWg.Add(1)
	go func() {
		defer mc.heartbeatWg.Done()
		// Criar ticker DENTRO da goroutine para garantir limpeza adequada
		heartbeatTicker := time.NewTicker(30 * time.Second)
		defer heartbeatTicker.Stop()

		for {
			select {
			case <-containerCtx.Done():
				return
			case <-heartbeatTicker.C:
				cm.taskManager.Heartbeat(taskName)
			}
		}
	}()

	// Configurar opções de logs iniciais
	logOptions := dockerTypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Since:      mc.since.Format(time.RFC3339),
		Timestamps: true,
	}

	// Loop de rotação de streams
	for {
		select {
		case <-containerCtx.Done():
			return nil
		default:
			// Create context with rotation timeout
			streamCtx, streamCancel := context.WithTimeout(containerCtx, cm.rotationInterval)

			// Record stream creation time
			mc.streamCreatedAt = time.Now()

			// Abrir stream de logs
			stream, err := cm.dockerPool.ContainerLogs(streamCtx, mc.id, logOptions)
			if err != nil {
				streamCancel()
				cm.logger.WithError(err).WithField("container_id", mc.id).Error("Failed to open log stream")
				metrics.RecordStreamError("open_failed", mc.id)

				// Aguardar antes de tentar novamente
				select {
				case <-containerCtx.Done():
					return nil
				case <-time.After(5 * time.Second):
					continue
				}
			}

			// Store stream reference with mutex protection
			mc.mu.Lock()
			mc.stream = stream
			mc.mu.Unlock()

			// Ler logs até o timeout de rotação ou erro
			readErr := cm.readContainerLogs(streamCtx, mc, stream)

			// Calculate stream age
			streamAge := time.Since(mc.streamCreatedAt)

			// CRITICAL: Close stream BEFORE canceling context
			// This interrupts the blocking stream.Read() syscall
			mc.mu.Lock()
			if mc.stream != nil {
				mc.stream.Close()
				mc.stream = nil
			}
			mc.mu.Unlock()

			// Now cancel context
			streamCancel()

			// CRITICAL: Wait for reader goroutine to exit before starting new rotation
			// This ensures the reader goroutine from THIS rotation completes before next rotation starts
			// This prevents reader goroutine accumulation
			mc.readerWg.Wait()

			// Check if this was a planned rotation (timeout) or an error
			if readErr == context.DeadlineExceeded {
				// Planned rotation
				mc.rotationCount++
				metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

				cm.logger.WithFields(logrus.Fields{
					"container_id":     mc.id,
					"container_name":   mc.name,
					"rotation_count":   mc.rotationCount,
					"stream_age_secs":  int(streamAge.Seconds()),
				}).Debug("Stream rotated successfully")
			} else if readErr != nil {
				// Error occurred
				if readErr == context.Canceled {
					// Parent context cancelled, exit gracefully
					cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
					return nil
				}

				// Other error
				cm.logger.WithError(readErr).WithFields(logrus.Fields{
					"container_id":   mc.id,
					"container_name": mc.name,
					"last_read":      mc.lastRead,
					"stream_age":     streamAge,
				}).Warn("Stream read error - will reconnect")
				metrics.RecordStreamError("read_failed", mc.id)
			}

			// Verificar se container ainda existe
			if !cm.containerExists(mc.id) {
				return nil
			}

			// Update log options to continue from last read position
			if !mc.lastRead.IsZero() {
				logOptions.Since = mc.lastRead.Format(time.RFC3339)
			} else {
				logOptions.Since = time.Now().UTC().Format(time.RFC3339)
			}

			// Brief pause before next rotation to prevent tight loops
			select {
			case <-containerCtx.Done():
				return nil
			case <-time.After(100 * time.Millisecond):
				// Continue to next rotation
			}
		}
	}
}

// readContainerLogs lê logs de um stream de container
func (cm *ContainerMonitor) readContainerLogs(ctx context.Context, mc *monitoredContainer, stream io.Reader) error {
	incomplete := ""
	logCount := int64(0)
	bytesRead := int64(0)

	// Canal para receber dados do stream em goroutine separada
	type readResult struct {
		data []byte
		err  error
	}
	readCh := make(chan readResult, 10) // Increased buffer to prevent blocking

	// Context for reader goroutine with explicit cleanup
	// CRITICAL FIX (FASE 6B): Use ctx (which IS streamCtx from caller) as parent
	// instead of global ctx to ensure reader goroutine is cancelled when stream rotates,
	// preventing goroutine leak. Previously this was context.WithCancel(ctx) where ctx
	// was the global app context, causing reader goroutines to never be cancelled on rotation.
	readerCtx, readerCancel := context.WithCancel(ctx)
	defer readerCancel() // Ensure reader goroutine is cancelled when function exits

	// Goroutine para ler do stream - TRACKED WITH READER WAITGROUP
	// Esta goroutine é recriada a cada rotação de stream
	mc.readerWg.Add(1) // Track this goroutine
	go func() {
		defer mc.readerWg.Done() // Always cleanup
		defer close(readCh)      // Close channel when exiting to unblock readers

		for {
			// Check context before blocking read
			select {
			case <-readerCtx.Done():
				return // Exit immediately if context cancelled
			default:
			}

			localBuf := make([]byte, 8192)
			n, err := stream.Read(localBuf)

			// Copiar apenas os bytes lidos
			var data []byte
			if n > 0 {
				data = make([]byte, n)
				copy(data, localBuf[:n])
			}

			select {
			case readCh <- readResult{data: data, err: err}:
				if err != nil {
					return // Sair se houver erro (incluindo EOF)
				}
			case <-readerCtx.Done():
				return // Context cancelado, sair
			}
		}
	}()

	for {
		// Aguardar dados do stream OU cancelamento do context
		var result readResult
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok = <-readCh:
			if !ok {
				// Channel closed, reader goroutine exited
				return nil
			}
			// Dados recebidos, processar abaixo
		}

		data := result.data
		err := result.err

		if len(data) > 0 {
			bytesRead += int64(len(data))

			// Processar dados lidos
			dataStr := incomplete + string(data)
			lines := strings.Split(dataStr, "\n")

			// Última linha pode estar incompleta
			incomplete = lines[len(lines)-1]
			lines = lines[:len(lines)-1]

			// Processar linhas completas
			for _, line := range lines {
				if line == "" {
					continue
				}

				// Remover header do Docker (8 bytes) se presente
				if len(line) > 8 && (line[0] == 1 || line[0] == 2) {
					line = line[8:]
				}

				// Parse timestamp se presente
				if strings.Contains(line, "T") && len(line) > 30 {
					if spaceIdx := strings.Index(line, " "); spaceIdx > 0 && spaceIdx < 35 {
						line = line[spaceIdx+1:]
					}
				}

				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				// Enviar para dispatcher com labels padrão
				sourceID := mc.id
				standardLabels := addStandardLabels(mc.labels)

				// Criar entry para validações
				traceID := uuid.New().String()
				entry := &types.LogEntry{
					TraceID:     traceID,
					Timestamp:   time.Now().UTC(), // Force UTC to prevent Loki "timestamp too new" errors
					Message:     line,
					SourceType:  "docker",
					SourceID:    sourceID,
					Labels:      standardLabels,
					ProcessedAt: time.Now().UTC(),
				}

				// Verificar se é self-log usando feedback guard (temporariamente desabilitado)
				/*
				if cm.feedbackGuard != nil {
					guardResult := cm.feedbackGuard.CheckEntry(entry)
					if guardResult.IsSelfLog && guardResult.Action == "drop" {
						cm.logger.WithFields(logrus.Fields{
							"container_id":   mc.id,
							"container_name": mc.name,
							"reason":         guardResult.Reason,
							"match_pattern":  guardResult.MatchPattern,
						}).Debug("Self-log dropped by feedback guard")
						continue
					}
				}
				*/

				// Validar timestamp se o timestamp validator estiver disponível
				if cm.timestampValidator != nil {
					result := cm.timestampValidator.ValidateTimestamp(entry)
					if !result.Valid && result.Action == "rejected" {
						cm.logger.WithFields(logrus.Fields{
							"container_id":   mc.id,
							"container_name": mc.name,
							"reason":         result.Reason,
							"line":           line,
						}).Warn("Container log line rejected due to invalid timestamp")
						continue
					}
				}

				if err := cm.dispatcher.Handle(ctx, "docker", sourceID, line, standardLabels); err != nil {
					cm.logger.WithError(err).WithField("container_id", mc.id).Error("Failed to dispatch container log")
					metrics.RecordError("container_monitor", "dispatch_error")
				} else {
					logCount++
					// CRÍTICO: Só atualizar lastRead quando efetivamente processamos um log
					mc.lastRead = time.Now()

					// Log periódico para debug (a cada 10 logs para não fazer spam)
					if logCount%10 == 0 {
						cm.logger.WithFields(logrus.Fields{
							"container_id":   mc.id,
							"container_name": mc.name,
							"logs_processed": logCount,
							"last_read":      mc.lastRead,
						}).Debug("Container logs processed")
					}
				}

				// Métricas
				metrics.RecordLogProcessed("docker", sourceID, "container_monitor")
			}

			// Update position if we processed logs successfully
			if logCount > 0 && cm.positionManager != nil {
				cm.positionManager.UpdateContainerPosition(mc.id, mc.lastRead, logCount, bytesRead)
				// Reset counters for next batch
				logCount = 0
				bytesRead = 0
			}
		}

		// Handle read errors
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// containerExists verifica se um container ainda existe
func (cm *ContainerMonitor) containerExists(containerID string) bool {
	ctx, cancel := context.WithTimeout(cm.ctx, 5*time.Second)
	defer cancel()

	_, err := cm.dockerPool.ContainerInspect(ctx, containerID)
	return err == nil
}

// checkContainerRecentLogs verifica se container teve logs recentes via API
func (cm *ContainerMonitor) checkContainerRecentLogs(containerID string) bool {
	ctx, cancel := context.WithTimeout(cm.ctx, 5*time.Second)
	defer cancel()

	// Verificar logs dos últimos 30 segundos
	since := time.Now().Add(-30 * time.Second).Format(time.RFC3339)

	logOptions := dockerTypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      since,
		Tail:       "1", // Apenas verificar se há pelo menos 1 log
	}

	stream, err := cm.dockerPool.ContainerLogs(ctx, containerID, logOptions)
	if err != nil {
		return false
	}
	defer stream.Close()

	// Ler até 1KB para verificar se há conteúdo
	buf := make([]byte, 1024)
	n, _ := stream.Read(buf)
	return n > 0
}

// healthCheckContainers verifica saúde dos containers monitorados
func (cm *ContainerMonitor) healthCheckContainers() {
	cm.mutex.RLock()
	containers := make([]*monitoredContainer, 0, len(cm.containers))
	for _, mc := range cm.containers {
		containers = append(containers, mc)
	}
	cm.mutex.RUnlock()

	for _, mc := range containers {
		// Verificar se o container ainda existe
		if !cm.containerExists(mc.id) {
			cm.mutex.Lock()
			cm.stopContainerMonitoring(mc.id)
			cm.mutex.Unlock()
			continue
		}

		// Verificar se logs foram lidos recentemente (relaxar o tempo para containers com pouca atividade)
		timeSinceLastRead := time.Since(mc.lastRead)
		if timeSinceLastRead > 10*time.Minute {
			// Verificar se container realmente tem logs recentes
			hasRecentLogs := cm.checkContainerRecentLogs(mc.id)

			logLevel := logrus.DebugLevel
			message := "Container has been quiet - normal for low activity containers"

			if hasRecentLogs {
				logLevel = logrus.WarnLevel
				message = "Container has recent logs but our stream is not capturing them - possible stream disconnection"

				// Se detectou desconexão de stream, forçar reconexão
				if timeSinceLastRead > 15*time.Minute {
					cm.logger.WithFields(logrus.Fields{
						"container_id":   mc.id,
						"container_name": mc.name,
						"minutes_since_read": int(timeSinceLastRead.Minutes()),
					}).Warn("Forcing container stream reconnection due to prolonged disconnection")

					// Parar e reiniciar monitoramento do container
					cm.mutex.Lock()
					cm.stopContainerMonitoring(mc.id)

					// Obter informações atualizadas do container
					containers, err := cm.dockerPool.ContainerList(context.Background(), dockerTypes.ContainerListOptions{
						All: true,
						Filters: filters.NewArgs(filters.Arg("id", mc.id)),
					})

					if err != nil || len(containers) == 0 {
						cm.logger.WithError(err).WithField("container_id", mc.id).Error("Failed to get container info for reconnection")
						cm.mutex.Unlock()
						continue
					}

					// Reiniciar monitoramento com informações atualizadas
					cm.startContainerMonitoring(containers[0])
					cm.mutex.Unlock()

					cm.logger.WithFields(logrus.Fields{
						"container_id":   mc.id,
						"container_name": mc.name,
					}).Info("Container stream reconnection completed")
					continue
				}
			}

			cm.logger.WithFields(logrus.Fields{
				"container_id":        mc.id,
				"container_name":      mc.name,
				"minutes_since_read":  int(timeSinceLastRead.Minutes()),
				"last_read":           mc.lastRead,
				"since_time":          mc.since,
				"has_recent_logs":     hasRecentLogs,
			}).Log(logLevel, message)
		}
	}
}

// getHostIP obtém o IP do host
func getHostIP() string {
	// Tentar obter IP através de interface de rede
	interfaces, err := net.Interfaces()
	if err != nil {
		return "unknown"
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue // Interface down ou loopback
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}

	return "unknown"
}

// getHostname obtém o nome do host
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// addStandardLabels adiciona labels padrão para logs de containers
func addStandardLabels(labels map[string]string) map[string]string {
	// Criar um novo mapa copiando as labels existentes
	result := make(map[string]string)

	// Copiar apenas labels permitidas (filtrar labels indesejadas do Docker Compose)
	forbiddenLabels := map[string]bool{
		"test_label":                               true,
		"service_name":                             true,
		"project":                                  true,
		"log_type":                                 true,
		"maintainer":                               true,
		"job":                                      true,
		"environment":                              true,
		"com.docker.compose.project":               true,
		"com.docker.compose.project.config_files": true,
		"com.docker.compose.project.working_dir":   true,
		"com.docker.compose.config-hash":           true,
		"com.docker.compose.version":               true,
		"com.docker.compose.oneoff":                true,
		"com.docker.compose.depends_on":            true,
		"com.docker.compose.image":                 true,
		"org.opencontainers.image.source":          true,
	}

	for k, v := range labels {
		if !forbiddenLabels[k] {
			result[k] = v
		}
	}

	// Labels padrão obrigatórias (sobrescrevem as existentes)
	result["service"] = "ssw-log-capturer"
	result["source"] = "docker"
	result["instance"] = getHostIP()
	result["instance_name"] = getHostname()

	return result
}