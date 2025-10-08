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
	"ssw-logs-capture/pkg/types"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/sirupsen/logrus"
)

// ContainerMonitor monitora containers Docker
type ContainerMonitor struct {
	config          types.DockerConfig
	dispatcher      types.Dispatcher
	logger          *logrus.Logger
	taskManager     types.TaskManager
	positionManager *positions.PositionBufferManager

	dockerPool    *docker.PoolManager
	containers    map[string]*monitoredContainer
	mutex         sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
}

// monitoredContainer representa um container sendo monitorado
type monitoredContainer struct {
	id         string
	name       string
	image      string
	labels     map[string]string
	since      time.Time
	stream     io.ReadCloser
	lastRead   time.Time
	cancel     context.CancelFunc
}

// NewContainerMonitor cria um novo monitor de containers
func NewContainerMonitor(config types.DockerConfig, dispatcher types.Dispatcher, taskManager types.TaskManager, positionManager *positions.PositionBufferManager, logger *logrus.Logger) (*ContainerMonitor, error) {
	if !config.Enabled {
		return &ContainerMonitor{
			config:          config,
			dispatcher:      dispatcher,
			logger:          logger,
			taskManager:     taskManager,
			positionManager: positionManager,
			isRunning:       false,
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

	return &ContainerMonitor{
		config:          config,
		dispatcher:      dispatcher,
		logger:          logger,
		taskManager:     taskManager,
		positionManager: positionManager,
		dockerPool:      dockerPool,
		containers:      make(map[string]*monitoredContainer),
		ctx:             ctx,
		cancel:          cancel,
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

	// Verificar conectividade com Docker
	if err := cm.checkDockerConnection(); err != nil {
		return fmt.Errorf("docker connection check failed: %w", err)
	}

	// Iniciar task de monitoramento principal
	if err := cm.taskManager.StartTask(ctx, "container_monitor", cm.monitorLoop); err != nil {
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
	// Varredura inicial de containers
	if err := cm.scanContainers(); err != nil {
		cm.logger.WithError(err).Error("Initial container scan failed")
	}

	// Loop de reconexão
	ticker := time.NewTicker(cm.config.ReconnectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Verificar containers periodicamente
			if err := cm.scanContainers(); err != nil {
				cm.logger.WithError(err).Error("Container scan failed")
				metrics.RecordError("container_monitor", "scan_error")
			}
		}

		// Heartbeat
		cm.taskManager.Heartbeat("container_monitor")
	}
}

// eventsLoop monitora eventos do Docker
func (cm *ContainerMonitor) eventsLoop(ctx context.Context) error {
	eventChan, errChan := cm.dockerPool.Events(ctx, dockerTypes.EventsOptions{})

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-eventChan:
			cm.handleDockerEvent(event)
		case err := <-errChan:
			if err != nil {
				cm.logger.WithError(err).Error("Docker events error")
				metrics.RecordError("container_monitor", "events_error")

				// Tentar reconectar após erro
				time.Sleep(5 * time.Second)
				eventChan, errChan = cm.dockerPool.Events(ctx, dockerTypes.EventsOptions{})
			}
		}

		// Heartbeat
		cm.taskManager.Heartbeat("container_events")
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

// handleDockerEvent processa eventos do Docker
func (cm *ContainerMonitor) handleDockerEvent(event events.Message) {
	switch event.Action {
	case "start":
		// Container iniciado - aguardar um pouco e escanear
		// Use o task manager para gerenciar a goroutine
		taskName := "container_scan_" + event.Actor.ID[:12]
		cm.taskManager.StartTask(cm.ctx, taskName, func(ctx context.Context) error {
			// Usar select com context para permitir cancelamento
			select {
			case <-time.After(2 * time.Second):
				return cm.scanContainers()
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	case "die", "stop":
		// Container parado
		containerID := event.Actor.ID[:12]
		cm.mutex.Lock()
		cm.stopContainerMonitoring(containerID)
		cm.mutex.Unlock()
	case "destroy":
		// Container removido - marcar como removed para limpeza futura
		containerID := event.Actor.ID[:12]
		if cm.positionManager != nil {
			cm.positionManager.SetContainerStatus(containerID, "removed")
		}
		cm.logger.WithField("container_id", containerID).Debug("Container marked as removed")
	}
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
		"com.docker.compose.project",
		"com.docker.compose.container-number",
		"version",
		"maintainer",
	}

	for _, essential := range essentialDockerLabels {
		if value, exists := dockerContainer.Labels[essential]; exists {
			// Usar nome simplificado para economizar espaço
			switch essential {
			case "com.docker.compose.service":
				labels["service_name"] = value
			case "com.docker.compose.project":
				labels["project"] = value
			case "com.docker.compose.container-number":
				labels["instance"] = value
			case "version":
				labels["version"] = value
			case "maintainer":
				labels["maintainer"] = value
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
	}).Info("Stopped container monitoring")

	// Cancelar contexto se existir
	if mc.cancel != nil {
		mc.cancel()
	}

	// Fechar stream se existir
	if mc.stream != nil {
		mc.stream.Close()
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

	cm.logger.WithFields(logrus.Fields{
		"container_id":   containerID,
		"container_name": mc.name,
	}).Info("Stopped container monitoring")
}

// monitorContainer monitora logs de um container específico
func (cm *ContainerMonitor) monitorContainer(ctx context.Context, mc *monitoredContainer) error {
	containerCtx, cancel := context.WithCancel(ctx)
	mc.cancel = cancel
	defer cancel()

	// Configurar opções de logs
	logOptions := dockerTypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Since:      mc.since.Format(time.RFC3339),
		Timestamps: true,
	}

	for {
		select {
		case <-containerCtx.Done():
			return nil
		default:
			// Abrir stream de logs
			stream, err := cm.dockerPool.ContainerLogs(containerCtx, mc.id, logOptions)
			if err != nil {
				cm.logger.WithError(err).WithField("container_id", mc.id).Error("Failed to open log stream")
				metrics.RecordError("container_monitor", "stream_open_error")

				// Aguardar antes de tentar novamente
				select {
				case <-containerCtx.Done():
					return nil
				case <-time.After(5 * time.Second):
					continue
				}
			}

			mc.stream = stream

			// Ler logs
			if err := cm.readContainerLogs(containerCtx, mc, stream); err != nil {
				// Não logar como erro se o contexto foi cancelado (comportamento normal)
				if err == context.Canceled || err == context.DeadlineExceeded {
					cm.logger.WithField("container_id", mc.id).Debug("Container log reading stopped due to context cancellation")
					return nil
				}
				cm.logger.WithError(err).WithFields(logrus.Fields{
					"container_id":   mc.id,
					"container_name": mc.name,
					"last_read":      mc.lastRead,
				}).Warn("Container log reading error - will reconnect")
				metrics.RecordError("container_monitor", "read_error")
			}

			stream.Close()

			// Verificar se container ainda existe
			if !cm.containerExists(mc.id) {
				return nil
			}

			// Aguardar antes de reconectar
			select {
			case <-containerCtx.Done():
				return nil
			case <-time.After(2 * time.Second):
				// Atualizar 'since' para evitar logs duplicados usando o último timestamp lido
				if !mc.lastRead.IsZero() {
					logOptions.Since = mc.lastRead.Format(time.RFC3339)
					cm.logger.WithFields(logrus.Fields{
						"container_id":   mc.id,
						"container_name": mc.name,
						"since":          logOptions.Since,
						"last_read":      mc.lastRead,
					}).Debug("Reconnecting to container log stream with lastRead timestamp")
				} else {
					logOptions.Since = time.Now().Format(time.RFC3339)
					cm.logger.WithFields(logrus.Fields{
						"container_id":   mc.id,
						"container_name": mc.name,
						"since":          logOptions.Since,
					}).Debug("Reconnecting to container log stream with current timestamp (no lastRead)")
				}
			}
		}
	}
}

// readContainerLogs lê logs de um stream de container
func (cm *ContainerMonitor) readContainerLogs(ctx context.Context, mc *monitoredContainer, stream io.Reader) error {
	buf := make([]byte, 8192)
	incomplete := ""
	logCount := int64(0)
	bytesRead := int64(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err() // Retorna o erro específico do contexto (context.Canceled)
		default:
			n, err := stream.Read(buf)
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			if n == 0 {
				continue
			}

			bytesRead += int64(n)

			// Processar dados lidos
			data := incomplete + string(buf[:n])
			lines := strings.Split(data, "\n")

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

	// Copiar labels existentes primeiro
	for k, v := range labels {
		result[k] = v
	}

	// Labels padrão obrigatórias (sobrescrevem as existentes)
	result["service"] = "log_capturer"
	result["source"] = "docker"
	result["instance"] = getHostIP()
	result["instance_name"] = getHostname()
	result["test_label"] = "container_test_123"

	return result
}