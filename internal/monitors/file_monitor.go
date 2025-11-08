package monitors

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nxadm/tail"
	"github.com/sirupsen/logrus"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/types"
)

// ===================================================================================
// Solução 3: Substituição Completa por github.com/nxadm/tail
// ===================================================================================

/*
 * ARQUITETURA: Este monitor implementa a Solução 3 do documento de análise de vazamentos.
 *
 * COMPONENTES:
 *   - FileMonitor: Gerenciador principal que implementa interface Monitor
 *   - WorkerPool: Pool de workers para processamento concorrente de logs
 *   - LogTailer: Produtor que lê linhas via nxadm/tail library
 *
 * ANTI-LEAK MECHANISMS:
 *   - Context-aware reading com nested select
 *   - Explicit cleanup via tailer.Cleanup() e tailer.Stop()
 *   - WaitGroup tracking em todos componentes
 *   - Graceful shutdown sequence: Stop Tailers → Close Channels → Wait Workers
 *
 * PADRÃO: Producer-Consumer com channel buffering
 * THREAD-SAFETY: Todos acessos a maps protegidos por mutex
 */

// ===================================================================================
// CONFIGURAÇÃO PADRÃO (fallbacks se config não especificar)
// ===================================================================================

const (
	// Worker pool defaults
	defaultNumWorkers      = 4
	defaultMaxJobsInQueue  = 1000
	defaultShutdownTimeout = 10 * time.Second
)

// ===================================================================================
// WorkerPool (Consumidor) - Processamento Concorrente
// ===================================================================================

// workerPool gerencia um pool de goroutines para processar linhas de log.
// Esta struct é interna ao FileMonitor e não deve ser exportada.
type workerPool struct {
	// Canal usado para enfileirar trabalho (linhas de log).
	jobsChannel chan *workerJob

	// WaitGroup para rastrear quando todos os workers terminaram.
	wg sync.WaitGroup

	// Dependencies
	dispatcher types.Dispatcher
	logger     *logrus.Logger
}

// workerJob encapsula uma linha de log com metadados para processamento.
type workerJob struct {
	line       string
	sourcePath string
	timestamp  time.Time
}

// newWorkerPool cria e inicia um novo pool de workers.
func newWorkerPool(
	ctx context.Context,
	numWorkers int,
	queueSize int,
	dispatcher types.Dispatcher,
	logger *logrus.Logger,
) *workerPool {
	pool := &workerPool{
		jobsChannel: make(chan *workerJob, queueSize),
		dispatcher:  dispatcher,
		logger:      logger,
	}

	// Adiciona o número de workers ao WaitGroup.
	pool.wg.Add(numWorkers)

	// Inicia os workers.
	for i := 0; i < numWorkers; i++ {
		go pool.worker(ctx, i)
	}

	logger.WithFields(logrus.Fields{
		"component":   "file_monitor",
		"worker_pool": "started",
		"num_workers": numWorkers,
		"queue_size":  queueSize,
	}).Info("Worker pool iniciado")

	return pool
}

// worker é a goroutine de trabalho real.
// Ela consome do jobsChannel até que ele seja fechado.
func (p *workerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	p.logger.WithFields(logrus.Fields{
		"component": "file_monitor",
		"worker_id": id,
	}).Debug("Worker iniciado e aguardando jobs")

	// Consome do canal de jobs.
	// Este loop 'for range' termina automaticamente quando
	// 'p.jobsChannel' é fechado.
	for job := range p.jobsChannel {
		// Verifica se um desligamento foi solicitado *antes* de iniciar
		// um novo trabalho (embora o 'for range' deva parar primeiro).
		select {
		case <-ctx.Done():
			p.logger.WithField("worker_id", id).Debug("Desligamento detectado, saindo")
			return
		default:
			// Continua para processar o job.
		}

		// Processa o trabalho.
		if err := p.processLogLine(ctx, job); err != nil {
			p.logger.WithError(err).WithFields(logrus.Fields{
				"worker_id":   id,
				"source_path": job.sourcePath,
			}).Warn("Erro ao processar linha de log")

			// METRICS: Erro no processamento
			metrics.ErrorsTotal.WithLabelValues("file_monitor", "process_log_line").Inc()
		}
	}

	p.logger.WithField("worker_id", id).Debug("Canal de jobs fechado. Encerrando worker")
}

// processLogLine processa uma linha de log enviando para o dispatcher.
func (p *workerPool) processLogLine(ctx context.Context, job *workerJob) error {
	// Criar labels para dispatcher
	labels := map[string]string{
		"source":    "file_monitor",
		"file_path": job.sourcePath,
		"job":       "log_capturer",
	}

	// Enviar para dispatcher usando Handle()
	if err := p.dispatcher.Handle(ctx, "file", job.sourcePath, job.line, labels); err != nil {
		return fmt.Errorf("failed to send to dispatcher: %w", err)
	}

	// METRICS: Log processado com sucesso
	metrics.LogsProcessedTotal.WithLabelValues("file", job.sourcePath, "file_monitor").Inc()

	return nil
}

// close fecha o canal de jobs e aguarda todos os workers terminarem.
func (p *workerPool) close() {
	close(p.jobsChannel)
	p.wg.Wait()
}

// ===================================================================================
// LogTailer (Produtor) - Leitura de Arquivo via nxadm/tail
// ===================================================================================

// logTailer encapsula a lógica de "tailing" de um arquivo.
type logTailer struct {
	tailer     *tail.Tail
	pool       *workerPool
	wg         sync.WaitGroup
	sourcePath string
	logger     *logrus.Logger
}

// newLogTailer inicia o tailing em um arquivo de log e o conecta ao pool.
func newLogTailer(
	ctx context.Context,
	path string,
	pool *workerPool,
	config types.FileMonitorServiceConfig,
	logger *logrus.Logger,
) (*logTailer, error) {
	// Configuração do 'tail'.
	// Esta é a configuração chave para robustez contra vazamentos.
	tailConfig := tail.Config{
		// Segue o arquivo (comporta-se como 'tail -f').
		Follow: true,

		// Re-abre o arquivo se for rotacionado (renomeado/movido).
		// Isto resolve o problema de perda de log em rotações.
		ReOpen: true,

		// Determina posição inicial de leitura baseado em config
		Location: determineSeekPosition(config),

		// Usa 'inotify' por padrão (baixa latência).
		// TODO: Adicionar campo UsePolling ao FileMonitorServiceConfig
		Poll: false, // false = usa inotify (padrão)
	}

	t, err := tail.TailFile(path, tailConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to tail file %s: %w", path, err)
	}

	lt := &logTailer{
		tailer:     t,
		pool:       pool,
		sourcePath: path,
		logger:     logger,
	}

	// Adiciona 1 ao WaitGroup para a goroutine 'run'
	lt.wg.Add(1)
	// Inicia a goroutine do produtor
	go lt.run(ctx)

	logger.WithFields(logrus.Fields{
		"component":   "file_monitor",
		"file_path":   path,
		"follow":      tailConfig.Follow,
		"reopen":      tailConfig.ReOpen,
		"use_polling": tailConfig.Poll,
	}).Info("Tailer iniciado para arquivo")

	return lt, nil
}

// determineSeekPosition determina a posição inicial de leitura baseado na configuração.
func determineSeekPosition(config types.FileMonitorServiceConfig) *tail.SeekInfo {
	// Se IgnoreOldTimestamps estiver habilitado, começa do fim
	if config.IgnoreOldTimestamps {
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	}

	// Estratégias de seek
	switch config.SeekStrategy {
	case "end":
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	case "recent":
		// Lê últimos N bytes (configurável)
		offset := int64(config.SeekRecentBytes)
		if offset == 0 {
			offset = 1048576 // 1MB default
		}
		return &tail.SeekInfo{Offset: -offset, Whence: io.SeekEnd}
	case "beginning":
		fallthrough
	default:
		// Lê arquivo completo desde o início
		return &tail.SeekInfo{Offset: 0, Whence: io.SeekStart}
	}
}

// run é a goroutine do Produtor.
// Ela lê do 'tail' e envia para o 'WorkerPool'.
func (lt *logTailer) run(ctx context.Context) {
	defer lt.wg.Done()
	// Garante que os recursos do 'tail' (como 'inotify') sejam limpos.
	defer lt.tailer.Cleanup()

	lt.logger.WithFields(logrus.Fields{
		"component": "file_monitor",
		"file_path": lt.tailer.Filename,
	}).Debug("Iniciando tailing de arquivo")

	for {
		select {
		// Caso 1: Desligamento solicitado via contexto.
		case <-ctx.Done():
			lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Sinal de desligamento recebido")

			// Para o tailer. Isso fará com que o canal 'lt.tailer.Lines'
			// seja fechado, o que acionará o 'case' abaixo.
			if err := lt.tailer.Stop(); err != nil {
				lt.logger.WithError(err).Warn("Erro ao parar tailer")
			}

			lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Tailer parado. Encerrando goroutine")
			return

		// Caso 2: Nova linha recebida do arquivo.
		case line, ok := <-lt.tailer.Lines:
			if !ok {
				// O canal foi fechado (provavelmente por lt.tailer.Stop()).
				lt.logger.WithField("file_path", lt.tailer.Filename).Debug("Canal 'Lines' fechado")

				if err := lt.tailer.Err(); err != nil {
					lt.logger.WithError(err).Warn("Erro final do tailer")
					// METRICS: Erro do tailer
					metrics.ErrorsTotal.WithLabelValues("file_monitor", "tailer_error").Inc()
				}
				return
			}

			if line.Err != nil {
				lt.logger.WithError(line.Err).Warn("Erro de linha")
				// METRICS: Erro de linha
				metrics.ErrorsTotal.WithLabelValues("file_monitor", "line_error").Inc()
				continue
			}

			// --- O PONTO DE DESACOPLAMENTO ---
			// Em vez de processar 'line.Text' aqui, enviamos
			// para o pool de workers.

			// Este 'select' aninhado é crucial.
			// Ele tenta enviar para o pool, mas *também*
			// ouve o sinal de desligamento, para que não
			// fique preso aqui indefinidamente se o pool estiver cheio
			// e o aplicativo precisar parar.
			job := &workerJob{
				line:       line.Text,
				sourcePath: lt.sourcePath,
				timestamp:  line.Time,
			}

			select {
			case <-ctx.Done():
				// O desligamento foi solicitado enquanto estávamos
				// esperando para enviar ao pool.
				lt.logger.Debug("Desligamento durante envio ao pool. Descartando última linha")
				return // Sai sem enviar

			case lt.pool.jobsChannel <- job:
				// Linha enviada ao pool com sucesso.
				// METRICS: Linha coletada (será processada por worker)
				// Usamos LogsProcessedTotal com status diferente para distinguir coleta vs processamento
			}
		}
	}
}

// stop para o tailer e aguarda a goroutine run() terminar.
func (lt *logTailer) stop() {
	lt.wg.Wait()
}

// ===================================================================================
// FileMonitor - Gerenciador Principal (implementa interface Monitor)
// ===================================================================================

// FileMonitor gerencia o ciclo de vida do monitoramento de arquivos de log.
// Implementa a interface types.Monitor para integração com o sistema.
type FileMonitor struct {
	// Core components
	workerPool *workerPool
	tailers    map[string]*logTailer // path → tailer
	tailersMux sync.RWMutex          // protege tailers map

	// Context management
	ctxMux sync.RWMutex      // protege ctx e cancel (CRITICAL-001 fix)
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	config types.FileMonitorServiceConfig

	// Dependencies (integração com sistema)
	dispatcher      types.Dispatcher
	taskManager     types.TaskManager
	positionManager *positions.PositionBufferManager
	logger          *logrus.Logger

	// State
	running    bool
	runningMux sync.RWMutex
}

// NewFileMonitor cria um novo monitor de arquivos.
func NewFileMonitor(
	config types.FileMonitorServiceConfig,
	dispatcher types.Dispatcher,
	taskManager types.TaskManager,
	positionManager *positions.PositionBufferManager,
	logger *logrus.Logger,
) (*FileMonitor, error) {
	// Validação de dependências obrigatórias
	if logger == nil {
		return nil, fmt.Errorf("logger é obrigatório")
	}

	if dispatcher == nil {
		return nil, fmt.Errorf("dispatcher é obrigatório")
	}

	// Worker pool config usa defaults (constantes)
	// TODO: Adicionar campos WorkerCount e QueueSize ao FileMonitorServiceConfig

	return &FileMonitor{
		config:          config,
		dispatcher:      dispatcher,
		taskManager:     taskManager,
		positionManager: positionManager,
		logger:          logger,
		tailers:         make(map[string]*logTailer),
		running:         false,
	}, nil
}

// Start implementa a interface Monitor.
// Inicia o monitoramento de arquivos com context-aware architecture.
func (fm *FileMonitor) Start(ctx context.Context) error {
	fm.runningMux.Lock()
	if fm.running {
		fm.runningMux.Unlock()
		return fmt.Errorf("file monitor já está em execução")
	}
	fm.running = true
	fm.runningMux.Unlock()

	fm.logger.WithFields(logrus.Fields{
		"component":    "file_monitor",
		"approach":     "nxadm-tail-producer-consumer",
		"worker_count": defaultNumWorkers,
		"queue_size":   defaultMaxJobsInQueue,
	}).Info("Iniciando File Monitor com arquitetura anti-leak")

	// Criar contexto cancelável derivado do contexto da aplicação
	// CRITICAL-001 FIX: Proteger acesso concorrente a ctx/cancel
	fm.ctxMux.Lock()
	fm.ctx, fm.cancel = context.WithCancel(ctx)
	fm.ctxMux.Unlock()

	// Criar worker pool compartilhado
	fm.workerPool = newWorkerPool(
		fm.ctx,
		defaultNumWorkers,
		defaultMaxJobsInQueue,
		fm.dispatcher,
		fm.logger,
	)

	// Iniciar tailers para todos os arquivos configurados
	if err := fm.startTailers(); err != nil {
		fm.cancel()
		return fmt.Errorf("failed to start tailers: %w", err)
	}

	fm.logger.Info("File Monitor iniciado com sucesso")
	return nil
}

// startTailers inicia tailers para todos os arquivos configurados.
func (fm *FileMonitor) startTailers() error {
	// Obter lista de arquivos a monitorar da configuração
	filePaths, err := fm.resolveFilePaths()
	if err != nil {
		return fmt.Errorf("failed to resolve file paths: %w", err)
	}

	if len(filePaths) == 0 {
		fm.logger.Warn("Nenhum arquivo para monitorar. File monitor iniciado mas inativo")
		return nil
	}

	fm.logger.WithField("file_count", len(filePaths)).Info("Iniciando tailers para arquivos")

	// Iniciar tailer para cada arquivo
	fm.tailersMux.Lock()
	defer fm.tailersMux.Unlock()

	for _, path := range filePaths {
		tailer, err := newLogTailer(fm.ctx, path, fm.workerPool, fm.config, fm.logger)
		if err != nil {
			fm.logger.WithError(err).WithField("file_path", path).Warn("Falha ao iniciar tailer")
			// METRICS: Erro ao iniciar tailer
			metrics.ErrorsTotal.WithLabelValues("file_monitor", "start_tailer").Inc()
			continue
		}

		fm.tailers[path] = tailer

		fm.logger.WithField("file_path", path).Debug("Tailer iniciado")
	}

	if len(fm.tailers) == 0 {
		return fmt.Errorf("failed to start any tailer")
	}

	fm.logger.WithField("active_tailers", len(fm.tailers)).Info("Tailers iniciados com sucesso")
	return nil
}

// resolveFilePaths resolve a lista de arquivos a monitorar baseado na configuração.
func (fm *FileMonitor) resolveFilePaths() ([]string, error) {
	// Por enquanto, usamos WatchDirectories da configuração como lista de arquivos diretos
	// TODO: Implementar descoberta de arquivos baseada em patterns (IncludePatterns)

	if len(fm.config.WatchDirectories) > 0 {
		// WatchDirectories será tratado como lista de arquivos diretos por enquanto
		fm.logger.WithField("files", fm.config.WatchDirectories).Debug("Using WatchDirectories as direct file list")
		return fm.config.WatchDirectories, nil
	}

	return nil, fmt.Errorf("no files configured to watch (configure watch_directories)")
}

// Stop implementa a interface Monitor.
// Para graciosamente o monitoramento, aguardando drain period.
func (fm *FileMonitor) Stop() error {
	fm.runningMux.Lock()
	if !fm.running {
		fm.runningMux.Unlock()
		fm.logger.Warn("File Monitor já está parado")
		return nil
	}
	fm.running = false
	fm.runningMux.Unlock()

	fm.logger.WithField("component", "file_monitor").Info("Parando File Monitor...")

	// Cancelar contexto principal (dispara cancelamento em todos os tailers)
	// CRITICAL-001 FIX: Proteger leitura concorrente de cancel
	fm.ctxMux.RLock()
	cancel := fm.cancel
	fm.ctxMux.RUnlock()

	if cancel != nil {
		cancel()
	}

	// --- ORDEM DE DESLIGAMENTO CRÍTICA ---

	// A. Parar todos os Tailers (Produtores) PRIMEIRO.
	fm.logger.Info("Aguardando tailers pararem...")
	fm.stopAllTailers()
	fm.logger.Info("Todos os tailers parados")

	// B. Fechar o Worker Pool SEGUNDO.
	fm.logger.Info("Fechando worker pool...")
	if fm.workerPool != nil {
		fm.workerPool.close()
	}
	fm.logger.Info("Worker pool fechado")

	// C. Aguardar cleanup com timeout
	done := make(chan struct{})
	go func() {
		fm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fm.logger.Info("File Monitor parado graciosamente")
	case <-time.After(defaultShutdownTimeout):
		fm.logger.Warn("Timeout aguardando File Monitor parar")
	}

	fm.logger.Info("File Monitor encerrado")
	return nil
}

// stopAllTailers para todos os tailers ativos.
func (fm *FileMonitor) stopAllTailers() {
	fm.tailersMux.Lock()
	defer fm.tailersMux.Unlock()

	for path, tailer := range fm.tailers {
		fm.logger.WithField("file_path", path).Debug("Parando tailer")
		tailer.stop()
	}

	// Limpar map
	fm.tailers = make(map[string]*logTailer)
}
