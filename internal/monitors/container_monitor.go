package monitors

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/sirupsen/logrus"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/types"
)

// ===================================================================================
// Solução 2 - Componente 1: O Leitor Consciente de Contexto (readerCtx)
// ===================================================================================

/*
 * O "PORQUÊ": Esta é a peça central que nos permite desbloquear o io.Copy (ou, neste
 * caso, stdcopy.StdCopy) de forma cooperativa. Em vez de uma chamada.Read()
 * que bloqueia indefinidamente, este wrapper primeiro verifica se o contexto
 * foi cancelado. Se foi, ele retorna imediatamente com context.Canceled,
 * fazendo com que o stdcopy.StdCopy pare graciosamente. [1]
 */

// readerCtx é um wrapper io.Reader que respeita um context.Context.
type readerCtx struct {
	ctx context.Context
	r   io.Reader
}

// newContextReader cria um novo readerCtx.
func newContextReader(ctx context.Context, r io.Reader) io.Reader {
	return &readerCtx{ctx: ctx, r: r}
}

// Read implementa a interface io.Reader.
func (r *readerCtx) Read(p []byte) (n int, err error) {
	// 1. Verifica o cancelamento ANTES de cada chamada de Read bloqueante.
	// Se o contexto for cancelado, Read() retorna imediatamente.
	if err := r.ctx.Err(); err != nil {
		return 0, err // Retorna context.Canceled
	}

	// 2. Se não foi cancelado, prossiga com a leitura bloqueante real.
	return r.r.Read(p)
}

// ===================================================================================
// Solução 2 - Componente 2: O Gerenciador de Logs (LogManager)
// ===================================================================================

/*
 * O "PORQUÊ": Em vez de lógica dispersa, centralizamos o estado. Este gerenciador
 * mantém um mapa de todas as goroutines de coleta ativas e é o único
 * responsável por iniciá-las e pará-las. Ele usa um mutex (collectorsMux)
 * para garantir que o mapa seja seguro para concorrência.
 */

// ContainerMonitor gerencia o ciclo de vida de todas as goroutines de coleta de logs.
type ContainerMonitor struct {
	// Docker client
	cli *client.Client

	// Context management
	ctx    context.Context
	cancel context.CancelFunc

	// Collector tracking
	collectors    map[string]context.CancelFunc
	collectorsMux sync.Mutex

	// Configuration
	config          types.DockerConfig
	timestampConfig types.TimestampValidationConfig
	drainDuration   time.Duration

	// Dependencies (integração com sistema)
	dispatcher      types.Dispatcher
	taskManager     types.TaskManager
	positionManager *positions.PositionBufferManager
	logger          *logrus.Logger

	// State
	running    bool
	runningMux sync.RWMutex
	wg         sync.WaitGroup
}

// NewContainerMonitor cria um novo gerenciador de logs.
func NewContainerMonitor(
	config types.DockerConfig,
	timestampConfig types.TimestampValidationConfig,
	dispatcher types.Dispatcher,
	taskManager types.TaskManager,
	positionManager *positions.PositionBufferManager,
	logger *logrus.Logger,
) (*ContainerMonitor, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger é obrigatório")
	}

	if dispatcher == nil {
		return nil, fmt.Errorf("dispatcher é obrigatório")
	}

	// Criar Docker client
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar cliente Docker: %w", err)
	}

	// Testar conexão com Docker daemon
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("falha ao conectar ao Docker daemon: %w", err)
	}

	// Parse drain duration da config (com fallback para 1s)
	drainDuration := 1 * time.Second
	if config.DrainDuration > 0 {
		drainDuration = config.DrainDuration
	}

	return &ContainerMonitor{
		cli:             cli,
		config:          config,
		timestampConfig: timestampConfig,
		dispatcher:      dispatcher,
		taskManager:     taskManager,
		positionManager: positionManager,
		logger:          logger,
		collectors:      make(map[string]context.CancelFunc),
		drainDuration:   drainDuration,
		running:         false,
	}, nil
}

// Start implementa a interface Monitor.
// Inicia o monitoramento de containers Docker com context-aware architecture.
func (cm *ContainerMonitor) Start(ctx context.Context) error {
	cm.runningMux.Lock()
	if cm.running {
		cm.runningMux.Unlock()
		return fmt.Errorf("container monitor já está em execução")
	}
	cm.running = true
	cm.runningMux.Unlock()

	cm.logger.WithFields(logrus.Fields{
		"component": "container_monitor",
		"approach":  "context-aware-reading",
	}).Info("Iniciando Container Monitor com arquitetura anti-leak")

	// Criar contexto cancelável derivado do contexto da aplicação
	cm.ctx, cm.cancel = context.WithCancel(ctx)

	// Iniciar goroutine principal que executa Run()
	cm.wg.Add(1)
	go func() {
		defer cm.wg.Done()
		cm.Run() // Chama o método Run() existente
	}()

	cm.logger.Info("Container Monitor iniciado com sucesso")
	return nil
}

// Stop implementa a interface Monitor.
// Para graciosamente o monitoramento, aguardando drain period.
func (cm *ContainerMonitor) Stop() error {
	cm.runningMux.Lock()
	if !cm.running {
		cm.runningMux.Unlock()
		cm.logger.Warn("Container Monitor já está parado")
		return nil
	}
	cm.running = false
	cm.runningMux.Unlock()

	cm.logger.WithField("component", "container_monitor").Info("Parando Container Monitor...")

	// Cancelar contexto principal (dispara cancelamento em todos os coletores)
	if cm.cancel != nil {
		cm.cancel()
	}

	// Aguardar goroutine principal terminar (com timeout)
	done := make(chan struct{})
	go func() {
		cm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		cm.logger.Info("Container Monitor parado graciosamente")
	case <-time.After(10 * time.Second):
		cm.logger.Warn("Timeout aguardando Container Monitor parar")
	}

	// Fechar Docker client
	if cm.cli != nil {
		if err := cm.cli.Close(); err != nil {
			cm.logger.WithError(err).Warn("Erro ao fechar Docker client")
		}
	}

	cm.logger.Info("Container Monitor encerrado")
	return nil
}

// Run é o ponto de entrada principal. Ele inicia o monitor de eventos
// e sincroniza o estado inicial com os contêineres em execução.
func (cm *ContainerMonitor) Run() {
	cm.logger.Info("Iniciando event monitor e sincronização inicial")

	// 1. Iniciar o monitor de eventos em uma goroutine.
	// Esta é a nossa *única* fonte de verdade para o ciclo de vida do contêiner.
	go cm.StartEventMonitor()

	// 2. Obter todos os contêineres *atualmente* em execução e iniciar a coleta.
	// Isso lida com qualquer contêiner que já estava rodando antes de iniciarmos.
	containers, err := cm.cli.ContainerList(cm.ctx, dockerTypes.ContainerListOptions{})
	if err != nil {
		cm.logger.WithError(err).Warn("Falha ao listar contêineres iniciais")
		// Em produção, você pode querer tentar novamente.
		return
	}

	cm.logger.WithField("count", len(containers)).Info("Iniciando coleta para contêineres existentes")
	for _, c := range containers {
		cm.logger.WithFields(logrus.Fields{
			"container_id": c.ID[:12],
			"image":        c.Image,
		}).Debug("Iniciando coleta para contêiner existente")
		cm.StartCollecting(c.ID)
	}

	// Mantém o Run() vivo até que o contexto principal seja cancelado.
	<-cm.ctx.Done()
	cm.logger.Info("Container monitor context cancelado, encerrando")
}

// StartEventMonitor ouve o stream de eventos do Docker.
// Esta é a abordagem de "baixo impacto na API" que discutimos.
func (cm *ContainerMonitor) StartEventMonitor() {
	cm.logger.Info("Monitor de eventos do Docker iniciado")

	// Filtros para eventos 'die' e 'start' de contêineres. [2]
	filters := filters.NewArgs()
	filters.Add("type", "container")
	filters.Add("event", "start") // Para pegar novos contêineres
	filters.Add("event", "die")   // Para parar coletores [3]

	eventsCh, errCh := cm.cli.Events(cm.ctx, dockerTypes.EventsOptions{Filters: filters})

	for {
		select {
		case event := <-eventsCh:
			switch event.Action {
			case "start":
				// Um novo contêiner iniciou, comece a coletar logs.
				cm.logger.WithField("container_id", event.Actor.ID[:12]).Info("Evento 'start' detectado, iniciando coleta")
				cm.StartCollecting(event.Actor.ID)

			case "die":
				// Um contêiner parou, inicie o processo de desligamento gracioso.
				cm.logger.WithField("container_id", event.Actor.ID[:12]).Info("Evento 'die' detectado, iniciando drenagem")
				cm.StopCollecting(event.Actor.ID)
			}

		case err := <-errCh:
			cm.logger.WithError(err).Warn("Erro no stream de eventos")

			// METRICS: Erro no stream de eventos
			metrics.RecordStreamError("event_stream_error", "event_monitor")

			// Se o contexto principal for cancelado, saia.
			if cm.ctx.Err() != nil {
				cm.logger.Info("Monitor de eventos encerrando devido ao cancelamento do contexto")
				return
			}

			// METRICS: Reconexão do event monitor
			metrics.ErrorsTotal.WithLabelValues("container_monitor", "event_monitor_reconnection").Inc()

			// Tenta se reconectar após um breve período.
			cm.logger.Info("Tentando reconectar o monitor de eventos em 3s")
			time.Sleep(3 * time.Second)
			// Reinicia o monitor (em uma nova goroutine) e termina esta.
			go cm.StartEventMonitor()
			return

		case <-cm.ctx.Done():
			cm.logger.Info("Monitor de eventos encerrando")
			return
		}
	}
}

// StartCollecting inicia uma nova goroutine de coleta para um contêiner.
func (cm *ContainerMonitor) StartCollecting(containerID string) {
	cm.collectorsMux.Lock()
	if _, exists := cm.collectors[containerID]; exists {
		cm.logger.WithField("container_id", containerID[:12]).Debug("Coletor já existe, ignorando")
		cm.collectorsMux.Unlock()
		return
	}

	// Cria um contexto *específico* para esta goroutine de coleta.
	// Isso nos permite cancelar *apenas este* coletor.
	collectCtx, cancel := context.WithCancel(cm.ctx)
	cm.collectors[containerID] = cancel
	activeCount := len(cm.collectors)
	cm.collectorsMux.Unlock()

	// METRICS: Incrementar contador de containers iniciados
	metrics.ErrorsTotal.WithLabelValues("container_monitor", "container_started").Inc()

	// METRICS: Atualizar gauge de coletores ativos
	metrics.UpdateActiveStreams(activeCount)

	// Inicia a goroutine de coleta real.
	go func() {
		cm.logger.WithField("container_id", containerID[:12]).Info("Coletor iniciado")

		// Sempre garanta que o cancelamento seja removido do mapa ao sair.
		defer func() {
			cm.logger.WithField("container_id", containerID[:12]).Info("Coletor encerrando")
			cm.collectorsMux.Lock()
			delete(cm.collectors, containerID)
			finalCount := len(cm.collectors)
			cm.collectorsMux.Unlock()

			// METRICS: Atualizar gauge quando coletor encerra
			metrics.UpdateActiveStreams(finalCount)
		}()

		options := dockerTypes.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
			Timestamps: true, // Útil para depuração
		}

		logStream, err := cm.cli.ContainerLogs(collectCtx, containerID, options)
		if err != nil {
			if collectCtx.Err() != nil {
				return // Contexto cancelado antes de começar, saída limpa.
			}
			cm.logger.WithFields(logrus.Fields{
				"container_id": containerID[:12],
				"error":        err,
			}).Warn("Falha ao obter stream de logs")

			// METRICS: Erro na API do Docker
			metrics.RecordStreamError("docker_api_error", containerID)

			return
		}
		defer logStream.Close()

		// *** A MÁGICA DA SOLUÇÃO 2 ***
		// 1. Envolve o logStream com nosso leitor consciente do contexto.
		wrappedReader := newContextReader(collectCtx, logStream)

		// 2. Usa stdcopy.StdCopy para demultiplexar o stream de log.
		// O "PORQUÊ": O stream de log do Docker (sem TTY) multiplexa stdout
		// e stderr. `io.Copy` imprimiria lixo. `stdcopy.StdCopy`
		// entende esse formato e o divide corretamente.
		//
		// Esta chamada irá bloquear no `wrappedReader.Read()`.
		// Quando `collectCtx` é cancelado, `wrappedReader.Read()` retornará
		// `context.Canceled`, e `stdcopy.StdCopy` irá parar e retornar.
		cm.logger.WithField("container_id", containerID[:12]).Debug("Iniciando cópia de logs")

		// FASE 4: Criar writers que capturam logs e enviam para dispatcher.
		// Substitui prefixedWriter para integrar com o sistema de processamento.
		stdoutWriter := newLogCaptureWriter(containerID, "stdout", cm.dispatcher, cm.logger)
		stderrWriter := newLogCaptureWriter(containerID, "stderr", cm.dispatcher, cm.logger)

		_, err = stdcopy.StdCopy(stdoutWriter, stderrWriter, wrappedReader)

		if err != nil && err != context.Canceled {
			cm.logger.WithFields(logrus.Fields{
				"container_id": containerID[:12],
				"error":        err,
			}).Warn("Erro ao copiar logs")
		} else if err == context.Canceled {
			cm.logger.WithField("container_id", containerID[:12]).Info("Coleta cancelada graciosamente")
		}
	}()
}

// StopCollecting inicia o desligamento gracioso com drenagem.
func (cm *ContainerMonitor) StopCollecting(containerID string) {
	cm.collectorsMux.Lock()
	cancel, exists := cm.collectors[containerID]
	if !exists {
		// Já foi parado ou nunca foi rastreado.
		cm.collectorsMux.Unlock()
		return
	}
	cm.collectorsMux.Unlock() // Desbloqueia o mutex *antes* de operações longas

	// *** A LÓGICA DE DRENAGEM "SEM PERDA DE LOGS" ***
	cm.logger.WithFields(logrus.Fields{
		"container_id":   containerID[:12],
		"drain_duration": cm.drainDuration.Seconds(),
	}).Info("Contêiner morto, aguardando drenagem de logs")

	// 1. Espera pelo período de drenagem.
	// Usamos um timer em vez de time.Sleep para que possamos
	// parar se o contexto principal (cm.ctx) for cancelado.
	drainTimer := time.NewTimer(cm.drainDuration)

	select {
	case <-drainTimer.C:
		// Tempo de drenagem esgotado.
		cm.logger.WithField("container_id", containerID[:12]).Info("Período de drenagem concluído, enviando cancelamento")

	case <-cm.ctx.Done():
		// O programa principal está encerrando, cancela imediatamente.
		cm.logger.WithField("container_id", containerID[:12]).Info("Encerrando drenagem antecipadamente devido ao desligamento")
		drainTimer.Stop()
	}

	// 2. *Agora* chama a função de cancelamento.
	// Isso fará com que o readerCtx.Read() retorne context.Canceled.
	cancel()

	// METRICS: Incrementar contador de containers parados
	metrics.ErrorsTotal.WithLabelValues("container_monitor", "container_stopped").Inc()
}

// ===================================================================================
// FASE 4: Integração com Dispatcher - logCaptureWriter
// ===================================================================================

// logCaptureWriter captura logs do Docker e envia para o dispatcher.
// Implementa io.Writer para ser compatível com stdcopy.StdCopy.
//
// Este writer substitui prefixedWriter para integrar os logs de containers
// ao sistema de processamento central, permitindo que logs sejam:
// - Roteados para sinks (Loki, Elasticsearch, arquivos locais)
// - Processados por pipelines (enriquecimento, filtragem)
// - Monitorados via métricas e traces
type logCaptureWriter struct {
	containerID string
	streamType  string // "stdout" ou "stderr"
	dispatcher  types.Dispatcher
	logger      *logrus.Logger
}

// newLogCaptureWriter cria um novo writer que captura logs e envia para o dispatcher.
//
// Parâmetros:
//   - containerID: ID completo do container Docker
//   - streamType: "stdout" ou "stderr" para identificar origem
//   - dispatcher: Interface do dispatcher para enviar logs
//   - logger: Logger estruturado para diagnóstico
func newLogCaptureWriter(
	containerID string,
	streamType string,
	dispatcher types.Dispatcher,
	logger *logrus.Logger,
) io.Writer {
	return &logCaptureWriter{
		containerID: containerID,
		streamType:  streamType,
		dispatcher:  dispatcher,
		logger:      logger,
	}
}

// Write implementa io.Writer.
// Cada chamada representa uma linha (ou chunk) de log do container.
//
// IMPORTANTE: Este método é chamado por stdcopy.StdCopy de forma síncrona.
// Para não bloquear a leitura de logs do Docker, SEMPRE retornamos sucesso
// (len(p), nil) mesmo se o dispatcher falhar. Erros são apenas logados.
func (w *logCaptureWriter) Write(p []byte) (n int, err error) {
	// Validação básica
	if len(p) == 0 {
		return 0, nil
	}

	// Converter bytes para string (uma linha de log)
	message := string(p)

	// Criar labels para identificar a origem do log.
	// NOTA: Este map será copiado internamente pelo dispatcher (DeepCopy),
	// então é seguro reutilizar esta estrutura em múltiplas chamadas.
	labels := map[string]string{
		"container_id": w.containerID,
		"stream":       w.streamType,
		"source":       "docker",
		"monitor":      "container_monitor",
	}

	// Usar context.Background() para não bloquear a leitura de logs.
	// O dispatcher tem seu próprio timeout e controle de fluxo.
	ctx := context.Background()

	// Enviar log para o dispatcher.
	// Assinatura: Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error
	if err := w.dispatcher.Handle(ctx, "docker", w.containerID, message, labels); err != nil {
		// Log error mas NÃO falhe a escrita - isso bloquearia stdcopy.StdCopy
		// e interromperia a coleta de logs do container.
		w.logger.WithFields(logrus.Fields{
			"container_id": w.containerID[:12],
			"stream":       w.streamType,
			"error":        err,
		}).Warn("Falha ao enviar log para dispatcher")

		// METRICS: Erro ao enviar para dispatcher
		metrics.RecordStreamError("dispatch_failed", w.containerID)

		// CRÍTICO: Retorna sucesso mesmo com erro para não bloquear leitura.
		// Os logs do dispatcher indicarão o problema (backpressure, sink offline, etc.)
		return len(p), nil
	}

	// METRICS: Log coletado com sucesso
	// Usar a métrica global que rastreia logs por container e stream
	metrics.ErrorsTotal.WithLabelValues("container_monitor", "log_collected_"+w.streamType).Inc()

	// Sucesso - log foi enviado para o dispatcher
	// Debug log removido para evitar overhead - use métricas do dispatcher para monitorar
	return len(p), nil
}
