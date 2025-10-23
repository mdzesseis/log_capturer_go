package task_manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// Config configuração do task manager
type Config struct {
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	TaskTimeout       time.Duration `yaml:"task_timeout"`
	CleanupInterval   time.Duration `yaml:"cleanup_interval"`
}

// taskManager implementação do gerenciador de tarefas
type taskManager struct {
	config    Config
	tasks     map[string]*task
	mutex     sync.RWMutex
	logger    *logrus.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// task representa uma tarefa em execução
type task struct {
	ID            string
	Fn            func(context.Context) error
	State         string
	StartedAt     time.Time
	LastHeartbeat time.Time
	ErrorCount    int64
	LastError     string
	Context       context.Context
	Cancel        context.CancelFunc
	Done          chan struct{}
}

// New cria uma nova instância do task manager
func New(config Config, logger *logrus.Logger) types.TaskManager {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.TaskTimeout == 0 {
		config.TaskTimeout = 5 * time.Minute
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	tm := &taskManager{
		config: config,
		tasks:  make(map[string]*task),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}

	// Iniciar goroutine de limpeza
	go tm.cleanupLoop()

	return tm
}

// StartTask inicia uma nova tarefa
func (tm *taskManager) StartTask(ctx context.Context, taskID string, fn func(context.Context) error) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Verificar se a tarefa já existe
	if existingTask, exists := tm.tasks[taskID]; exists {
		if existingTask.State == "running" {
			return fmt.Errorf("task %s is already running", taskID)
		}
		// Parar tarefa existente
		existingTask.Cancel()
		<-existingTask.Done
	}

	// Criar contexto da tarefa
	taskCtx, taskCancel := context.WithCancel(ctx)

	// Criar nova tarefa
	newTask := &task{
		ID:            taskID,
		Fn:            fn,
		State:         "running",
		StartedAt:     time.Now(),
		LastHeartbeat: time.Now(),
		Context:       taskCtx,
		Cancel:        taskCancel,
		Done:          make(chan struct{}),
	}

	tm.tasks[taskID] = newTask

	// Iniciar tarefa em goroutine
	go tm.runTask(newTask)

	tm.logger.WithField("task_id", taskID).Info("Task started")
	return nil
}

// runTask executa uma tarefa
func (tm *taskManager) runTask(t *task) {
	defer close(t.Done)

	// Função auxiliar para atualizar estado de forma atômica
	updateTaskState := func(state string, errorCount int64, lastError string) {
		tm.mutex.Lock()
		t.State = state
		t.ErrorCount = errorCount
		t.LastError = lastError
		tm.mutex.Unlock()
	}

	defer func() {
		if r := recover(); r != nil {
			// Incrementar error count de forma thread-safe
			tm.mutex.Lock()
			currentErrorCount := t.ErrorCount + 1
			tm.mutex.Unlock()

			updateTaskState("failed", currentErrorCount, fmt.Sprintf("panic: %v", r))
			tm.logger.WithFields(logrus.Fields{
				"task_id": t.ID,
				"error":   r,
			}).Error("Task panicked")
		}
	}()

	// Executar função da tarefa
	if err := t.Fn(t.Context); err != nil {
		// Incrementar error count de forma thread-safe
		tm.mutex.Lock()
		currentErrorCount := t.ErrorCount + 1
		tm.mutex.Unlock()

		updateTaskState("failed", currentErrorCount, err.Error())
		tm.logger.WithFields(logrus.Fields{
			"task_id": t.ID,
			"error":   err,
		}).Error("Task failed")
		return
	}

	// Obter error count atual para manter consistência
	tm.mutex.Lock()
	currentErrorCount := t.ErrorCount
	tm.mutex.Unlock()

	updateTaskState("completed", currentErrorCount, "")
	tm.logger.WithField("task_id", t.ID).Info("Task completed")
}

// StopTask para uma tarefa
func (tm *taskManager) StopTask(taskID string) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.State != "running" {
		return fmt.Errorf("task %s is not running", taskID)
	}

	// Cancelar contexto da tarefa
	task.Cancel()

	// Aguardar conclusão com timeout
	select {
	case <-task.Done:
		task.State = "stopped"
		tm.logger.WithField("task_id", taskID).Info("Task stopped")
	case <-time.After(10 * time.Second):
		task.State = "failed"
		task.LastError = "stop timeout"
		tm.logger.WithField("task_id", taskID).Warn("Task stop timeout")
	}

	return nil
}

// Heartbeat atualiza o heartbeat de uma tarefa
func (tm *taskManager) Heartbeat(taskID string) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.LastHeartbeat = time.Now()
	return nil
}

// GetTaskStatus retorna o status de uma tarefa
func (tm *taskManager) GetTaskStatus(taskID string) types.TaskStatus {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return types.TaskStatus{
			ID:    taskID,
			State: "not_found",
		}
	}

	return types.TaskStatus{
		ID:            task.ID,
		State:         task.State,
		StartedAt:     task.StartedAt,
		LastHeartbeat: task.LastHeartbeat,
		ErrorCount:    task.ErrorCount,
		LastError:     task.LastError,
	}
}

// GetAllTasks retorna o status de todas as tarefas
func (tm *taskManager) GetAllTasks() map[string]types.TaskStatus {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	result := make(map[string]types.TaskStatus)
	for id, task := range tm.tasks {
		result[id] = types.TaskStatus{
			ID:            task.ID,
			State:         task.State,
			StartedAt:     task.StartedAt,
			LastHeartbeat: task.LastHeartbeat,
			ErrorCount:    task.ErrorCount,
			LastError:     task.LastError,
		}
	}

	return result
}

// cleanupLoop loop de limpeza de tarefas antigas
func (tm *taskManager) cleanupLoop() {
	ticker := time.NewTicker(tm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.cleanupTasks()
		}
	}
}

// cleanupTasks limpa tarefas antigas ou orfãs
func (tm *taskManager) cleanupTasks() {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for id, task := range tm.tasks {
		// Verificar tarefas timeout
		if task.State == "running" && now.Sub(task.LastHeartbeat) > tm.config.TaskTimeout {
			tm.logger.WithField("task_id", id).Warn("Task timeout detected, stopping")
			task.Cancel()
			task.State = "failed"
			task.LastError = "heartbeat timeout"
		}

		// Marcar tarefas concluídas antigas para remoção (mais de 1 hora)
		if task.State != "running" && now.Sub(task.StartedAt) > time.Hour {
			toDelete = append(toDelete, id)
		}
	}

	// Remover tarefas marcadas
	for _, id := range toDelete {
		delete(tm.tasks, id)
		tm.logger.WithField("task_id", id).Debug("Task cleaned up")
	}
}

// Cleanup limpa todos os recursos
func (tm *taskManager) Cleanup() {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// Cancelar contexto principal
	tm.cancel()

	// Parar todas as tarefas em execução
	for id, task := range tm.tasks {
		if task.State == "running" {
			task.Cancel()
			select {
			case <-task.Done:
			case <-time.After(5 * time.Second):
				tm.logger.WithField("task_id", id).Warn("Task cleanup timeout")
			}
		}
	}

	tm.logger.Info("Task manager cleanup completed")
}