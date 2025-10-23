package workerpool

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Task representa uma tarefa a ser executada
type Task struct {
	ID       string
	Execute  func(ctx context.Context) error
	Priority int
	Created  time.Time
}

// Worker representa um worker do pool
type Worker struct {
	ID       int
	pool     *WorkerPool
	taskChan chan Task
	quit     chan bool
	active   int64
	logger   *logrus.Logger
}

// WorkerPool gerencia um pool de workers reutilizáveis
type WorkerPool struct {
	workers     []*Worker
	taskQueue   chan Task
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	logger      *logrus.Logger
	config      WorkerPoolConfig

	// Métricas
	totalTasks     int64
	activeTasks    int64
	completedTasks int64
	failedTasks    int64

	// Controle
	isRunning bool
	mutex     sync.RWMutex
}

// WorkerPoolConfig configuração do pool de workers
type WorkerPoolConfig struct {
	MaxWorkers      int           `yaml:"max_workers"`
	QueueSize       int           `yaml:"queue_size"`
	WorkerTimeout   time.Duration `yaml:"worker_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	EnableMetrics   bool          `yaml:"enable_metrics"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// NewWorkerPool cria um novo pool de workers
func NewWorkerPool(config WorkerPoolConfig, logger *logrus.Logger) *WorkerPool {
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU()
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.MaxWorkers * 10
	}
	if config.WorkerTimeout == 0 {
		config.WorkerTimeout = 30 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &WorkerPool{
		taskQueue: make(chan Task, config.QueueSize),
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		config:    config,
		workers:   make([]*Worker, 0, config.MaxWorkers),
	}

	// Criar workers
	for i := 0; i < config.MaxWorkers; i++ {
		worker := &Worker{
			ID:       i,
			pool:     pool,
			taskChan: make(chan Task, 1),
			quit:     make(chan bool),
			logger:   logger,
		}
		pool.workers = append(pool.workers, worker)
	}

	return pool
}

// Start inicia o pool de workers
func (wp *WorkerPool) Start() error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	if wp.isRunning {
		return nil
	}

	wp.logger.WithFields(logrus.Fields{
		"max_workers": wp.config.MaxWorkers,
		"queue_size":  wp.config.QueueSize,
	}).Info("Starting worker pool")

	// Iniciar workers
	for _, worker := range wp.workers {
		wp.wg.Add(1)
		go worker.start()
	}

	// Iniciar dispatcher
	wp.wg.Add(1)
	go wp.dispatcher()

	// Iniciar metrics collector se habilitado
	if wp.config.EnableMetrics {
		wp.wg.Add(1)
		go wp.metricsCollector()
	}

	wp.isRunning = true
	return nil
}

// Stop para o pool de workers
func (wp *WorkerPool) Stop() error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	if !wp.isRunning {
		return nil
	}

	wp.logger.Info("Stopping worker pool")

	// Cancelar contexto
	wp.cancel()

	// Parar workers
	for _, worker := range wp.workers {
		close(worker.quit)
	}

	// Aguardar finalização com timeout
	done := make(chan bool)
	go func() {
		wp.wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		wp.logger.Info("Worker pool stopped gracefully")
	case <-time.After(wp.config.ShutdownTimeout):
		wp.logger.Warn("Worker pool shutdown timeout")
	}

	wp.isRunning = false
	return nil
}

// SubmitTask submete uma tarefa para o pool
func (wp *WorkerPool) SubmitTask(task Task) error {
	if !wp.isRunning {
		return ErrPoolNotRunning
	}

	task.Created = time.Now()
	atomic.AddInt64(&wp.totalTasks, 1)

	select {
	case wp.taskQueue <- task:
		return nil
	case <-wp.ctx.Done():
		return wp.ctx.Err()
	default:
		atomic.AddInt64(&wp.failedTasks, 1)
		return ErrQueueFull
	}
}

// SubmitTaskWithTimeout submete uma tarefa com timeout
func (wp *WorkerPool) SubmitTaskWithTimeout(task Task, timeout time.Duration) error {
	if !wp.isRunning {
		return ErrPoolNotRunning
	}

	task.Created = time.Now()
	atomic.AddInt64(&wp.totalTasks, 1)

	select {
	case wp.taskQueue <- task:
		return nil
	case <-time.After(timeout):
		atomic.AddInt64(&wp.failedTasks, 1)
		return ErrTimeout
	case <-wp.ctx.Done():
		return wp.ctx.Err()
	}
}

// GetStats retorna estatísticas do pool
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	return WorkerPoolStats{
		MaxWorkers:     wp.config.MaxWorkers,
		ActiveWorkers:  wp.getActiveWorkers(),
		QueuedTasks:    len(wp.taskQueue),
		QueueSize:     wp.config.QueueSize,
		TotalTasks:    atomic.LoadInt64(&wp.totalTasks),
		ActiveTasks:   atomic.LoadInt64(&wp.activeTasks),
		CompletedTasks: atomic.LoadInt64(&wp.completedTasks),
		FailedTasks:   atomic.LoadInt64(&wp.failedTasks),
		IsRunning:     wp.isRunning,
	}
}

// dispatcher distribui tarefas para workers disponíveis
func (wp *WorkerPool) dispatcher() {
	defer wp.wg.Done()

	for {
		select {
		case task := <-wp.taskQueue:
			// Encontrar worker disponível
			wp.assignTaskToWorker(task)
		case <-wp.ctx.Done():
			wp.logger.Info("Worker pool dispatcher stopping")
			return
		}
	}
}

// assignTaskToWorker atribui tarefa a um worker disponível
func (wp *WorkerPool) assignTaskToWorker(task Task) {
	// Implementação round-robin simples
	for _, worker := range wp.workers {
		select {
		case worker.taskChan <- task:
			return
		default:
			continue
		}
	}

	// Se todos workers estão ocupados, bloquear até algum ficar disponível
	select {
	case wp.workers[0].taskChan <- task:
		return
	case <-wp.ctx.Done():
		atomic.AddInt64(&wp.failedTasks, 1)
		return
	}
}

// getActiveWorkers retorna número de workers ativos
func (wp *WorkerPool) getActiveWorkers() int {
	active := 0
	for _, worker := range wp.workers {
		if atomic.LoadInt64(&worker.active) > 0 {
			active++
		}
	}
	return active
}

// metricsCollector coleta métricas periodicamente
func (wp *WorkerPool) metricsCollector() {
	defer wp.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := wp.GetStats()
			wp.logger.WithFields(logrus.Fields{
				"active_workers":  stats.ActiveWorkers,
				"queued_tasks":    stats.QueuedTasks,
				"total_tasks":     stats.TotalTasks,
				"completed_tasks": stats.CompletedTasks,
				"failed_tasks":    stats.FailedTasks,
			}).Debug("Worker pool metrics")
		case <-wp.ctx.Done():
			return
		}
	}
}

// start inicia um worker
func (w *Worker) start() {
	defer w.pool.wg.Done()

	w.pool.logger.WithField("worker_id", w.ID).Debug("Worker started")

	for {
		select {
		case task := <-w.taskChan:
			w.executeTask(task)
		case <-w.quit:
			w.pool.logger.WithField("worker_id", w.ID).Debug("Worker stopping")
			return
		case <-w.pool.ctx.Done():
			return
		}
	}
}

// executeTask executa uma tarefa
func (w *Worker) executeTask(task Task) {
	atomic.StoreInt64(&w.active, 1)
	atomic.AddInt64(&w.pool.activeTasks, 1)

	defer func() {
		atomic.StoreInt64(&w.active, 0)
		atomic.AddInt64(&w.pool.activeTasks, -1)
	}()

	startTime := time.Now()

	// Criar contexto com timeout para a tarefa
	taskCtx, cancel := context.WithTimeout(w.pool.ctx, w.pool.config.WorkerTimeout)
	defer cancel()

	// Executar tarefa
	err := task.Execute(taskCtx)
	duration := time.Since(startTime)

	if err != nil {
		atomic.AddInt64(&w.pool.failedTasks, 1)
		w.logger.WithFields(logrus.Fields{
			"worker_id": w.ID,
			"task_id":   task.ID,
			"duration":  duration,
			"error":     err,
		}).Error("Task execution failed")
	} else {
		atomic.AddInt64(&w.pool.completedTasks, 1)
		w.logger.WithFields(logrus.Fields{
			"worker_id": w.ID,
			"task_id":   task.ID,
			"duration":  duration,
		}).Debug("Task completed successfully")
	}
}

// WorkerPoolStats estatísticas do pool
type WorkerPoolStats struct {
	MaxWorkers     int   `json:"max_workers"`
	ActiveWorkers  int   `json:"active_workers"`
	QueuedTasks    int   `json:"queued_tasks"`
	QueueSize     int   `json:"queue_size"`
	TotalTasks    int64 `json:"total_tasks"`
	ActiveTasks   int64 `json:"active_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks   int64 `json:"failed_tasks"`
	IsRunning     bool  `json:"is_running"`
}

// Erros
var (
	ErrPoolNotRunning = fmt.Errorf("worker pool is not running")
	ErrQueueFull     = fmt.Errorf("task queue is full")
	ErrTimeout       = fmt.Errorf("task submission timeout")
)