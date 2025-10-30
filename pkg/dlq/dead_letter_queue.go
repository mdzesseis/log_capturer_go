package dlq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// ReprocessCallback função callback para reprocessamento
type ReprocessCallback func(entry types.LogEntry, originalSink string) error

// DeadLetterQueue gerencia logs que falharam no processamento
type DeadLetterQueue struct {
	config Config
	logger *logrus.Logger

	queue      chan DLQEntry
	file       *os.File
	mutex      sync.RWMutex
	stats      Stats

	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  bool

	// Monitoramento de alertas
	alertManager *AlertManager

	// Callback para reprocessamento
	reprocessCallback ReprocessCallback
}

// Config configuração da DLQ
type Config struct {
	// Habilitar DLQ
	Enabled bool `yaml:"enabled"`

	// Diretório para armazenar DLQ
	Directory string `yaml:"directory"`

	// Tamanho máximo da fila em memória
	QueueSize int `yaml:"queue_size"`

	// Número máximo de arquivos DLQ
	MaxFiles int `yaml:"max_files"`

	// Tamanho máximo por arquivo (MB)
	MaxFileSize int64 `yaml:"max_file_size_mb"`

	// TTL para arquivos DLQ (dias)
	RetentionDays int `yaml:"retention_days"`

	// Intervalo de flush
	FlushInterval time.Duration `yaml:"flush_interval"`

	// Incluir stack trace de erro
	IncludeStackTrace bool `yaml:"include_stack_trace"`

	// Formatar como JSON
	JSONFormat bool `yaml:"json_format"`

	// Configurações de alerta
	AlertConfig AlertConfig `yaml:"alert_config"`

	// Configurações de reprocessamento
	ReprocessingConfig ReprocessingConfig `yaml:"reprocessing_config"`
}

// ReprocessingConfig configuração de reprocessamento da DLQ
type ReprocessingConfig struct {
	// Habilitar reprocessamento automático
	Enabled bool `yaml:"enabled"`

	// Intervalo entre tentativas de reprocessamento
	Interval time.Duration `yaml:"interval"`

	// Número máximo de tentativas de reprocessamento por entrada
	MaxRetries int `yaml:"max_retries"`

	// Delay inicial entre tentativas
	InitialDelay time.Duration `yaml:"initial_delay"`

	// Multiplicador para delay exponencial
	DelayMultiplier float64 `yaml:"delay_multiplier"`

	// Delay máximo entre tentativas
	MaxDelay time.Duration `yaml:"max_delay"`

	// Número máximo de entradas para processar por batch
	BatchSize int `yaml:"batch_size"`

	// Timeout para cada tentativa de reenvio
	Timeout time.Duration `yaml:"timeout"`

	// Idade mínima das entradas para serem reprocessadas
	MinEntryAge time.Duration `yaml:"min_entry_age"`
}

// AlertConfig configuração de alertas da DLQ
type AlertConfig struct {
	// Habilitar alertas
	Enabled bool `yaml:"enabled"`

	// Threshold de entradas por minuto para disparar alerta
	EntriesPerMinuteThreshold int `yaml:"entries_per_minute_threshold"`

	// Threshold de total de entradas para disparar alerta
	TotalEntriesThreshold int64 `yaml:"total_entries_threshold"`

	// Threshold de tamanho da fila para disparar alerta
	QueueSizeThreshold int `yaml:"queue_size_threshold"`

	// Intervalo de verificação de alertas
	CheckInterval time.Duration `yaml:"check_interval"`

	// Intervalo mínimo entre alertas do mesmo tipo
	CooldownPeriod time.Duration `yaml:"cooldown_period"`

	// Webhook URL para envio de alertas
	WebhookURL string `yaml:"webhook_url"`

	// Email para envio de alertas
	EmailTo string `yaml:"email_to"`

	// Incluir estatísticas no alerta
	IncludeStats bool `yaml:"include_stats"`
}

// DLQEntry entrada na Dead Letter Queue
type DLQEntry struct {
	Timestamp     time.Time         `json:"timestamp"`
	OriginalEntry types.LogEntry    `json:"original_entry"`
	ErrorMessage  string            `json:"error_message"`
	ErrorType     string            `json:"error_type"`
	FailedSink    string            `json:"failed_sink"`
	RetryCount    int               `json:"retry_count"`
	Context       map[string]string `json:"context,omitempty"`
	StackTrace    string            `json:"stack_trace,omitempty"`

	// Campos para reprocessamento
	ReprocessAttempts    int       `json:"reprocess_attempts"`
	LastReprocessAttempt time.Time `json:"last_reprocess_attempt,omitempty"`
	NextReprocessTime    time.Time `json:"next_reprocess_time,omitempty"`
	ReprocessingEnabled  bool      `json:"reprocessing_enabled"`
	EntryID              string    `json:"entry_id"` // ID único para tracking
}

// Stats estatísticas da DLQ
type Stats struct {
	TotalEntries    int64 `json:"total_entries"`
	EntriesWritten  int64 `json:"entries_written"`
	WriteErrors     int64 `json:"write_errors"`
	CurrentQueueSize int  `json:"current_queue_size"`
	FilesCreated    int64 `json:"files_created"`
	LastFlush       time.Time `json:"last_flush"`

	// Estatísticas de reprocessamento
	ReprocessingAttempts int64     `json:"reprocessing_attempts"`
	ReprocessingSuccesses int64    `json:"reprocessing_successes"`
	ReprocessingFailures  int64    `json:"reprocessing_failures"`
	LastReprocessing      time.Time `json:"last_reprocessing"`
	EntriesReprocessed    int64     `json:"entries_reprocessed"`
}

// NewDeadLetterQueue cria nova DLQ
func NewDeadLetterQueue(config Config, logger *logrus.Logger) *DeadLetterQueue {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.QueueSize == 0 {
		config.QueueSize = 10000
	}
	if config.MaxFiles == 0 {
		config.MaxFiles = 10
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 100 // 100MB
	}
	if config.RetentionDays == 0 {
		config.RetentionDays = 7
	}
	if config.FlushInterval == 0 {
		config.FlushInterval = 30 * time.Second
	}
	if config.Directory == "" {
		config.Directory = "./dlq"
	}

	// Valores padrão para reprocessamento
	if config.ReprocessingConfig.Interval == 0 {
		config.ReprocessingConfig.Interval = 5 * time.Minute
	}
	if config.ReprocessingConfig.MaxRetries == 0 {
		config.ReprocessingConfig.MaxRetries = 3
	}
	if config.ReprocessingConfig.InitialDelay == 0 {
		config.ReprocessingConfig.InitialDelay = 1 * time.Minute
	}
	if config.ReprocessingConfig.DelayMultiplier == 0 {
		config.ReprocessingConfig.DelayMultiplier = 2.0
	}
	if config.ReprocessingConfig.MaxDelay == 0 {
		config.ReprocessingConfig.MaxDelay = 30 * time.Minute
	}
	if config.ReprocessingConfig.BatchSize == 0 {
		config.ReprocessingConfig.BatchSize = 50
	}
	if config.ReprocessingConfig.Timeout == 0 {
		config.ReprocessingConfig.Timeout = 30 * time.Second
	}
	if config.ReprocessingConfig.MinEntryAge == 0 {
		config.ReprocessingConfig.MinEntryAge = 2 * time.Minute
	}

	dlq := &DeadLetterQueue{
		config: config,
		logger: logger,
		queue:  make(chan DLQEntry, config.QueueSize),
		ctx:    ctx,
		cancel: cancel,
	}

	// Inicializar alert manager se configurado
	if config.AlertConfig.Enabled {
		dlq.alertManager = NewAlertManager(config.AlertConfig, dlq, logger)
	}

	return dlq
}

// Start inicia a DLQ
func (dlq *DeadLetterQueue) Start() error {
	if !dlq.config.Enabled {
		dlq.logger.Info("Dead Letter Queue disabled")
		return nil
	}

	dlq.mutex.Lock()
	defer dlq.mutex.Unlock()

	if dlq.isRunning {
		return fmt.Errorf("DLQ already running")
	}

	dlq.logger.WithFields(logrus.Fields{
		"directory":      dlq.config.Directory,
		"queue_size":     dlq.config.QueueSize,
		"max_files":      dlq.config.MaxFiles,
		"max_file_size":  dlq.config.MaxFileSize,
		"retention_days": dlq.config.RetentionDays,
	}).Info("Starting Dead Letter Queue")

	// Criar diretório se não existir
	if err := os.MkdirAll(dlq.config.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create DLQ directory: %w", err)
	}

	// Criar arquivo inicial
	if err := dlq.createNewFile(); err != nil {
		return fmt.Errorf("failed to create initial DLQ file: %w", err)
	}

	dlq.isRunning = true

	// Iniciar worker de processamento
	go dlq.processingLoop()

	// Iniciar limpeza periódica
	go dlq.cleanupLoop()

	// Iniciar reprocessamento se habilitado
	if dlq.config.ReprocessingConfig.Enabled {
		go dlq.reprocessingLoop()
		dlq.logger.WithField("interval", dlq.config.ReprocessingConfig.Interval).
			Info("DLQ reprocessing enabled")
	}

	// Iniciar alert manager se configurado
	if dlq.alertManager != nil {
		if err := dlq.alertManager.Start(); err != nil {
			dlq.logger.WithError(err).Warn("Failed to start DLQ alert manager")
		}
	}

	return nil
}

// Stop para a DLQ
func (dlq *DeadLetterQueue) Stop() error {
	dlq.mutex.Lock()
	defer dlq.mutex.Unlock()

	if !dlq.isRunning {
		return nil
	}

	dlq.logger.Info("Stopping Dead Letter Queue")
	dlq.isRunning = false

	// Parar alert manager se estiver rodando
	if dlq.alertManager != nil {
		if err := dlq.alertManager.Stop(); err != nil {
			dlq.logger.WithError(err).Warn("Failed to stop DLQ alert manager")
		}
	}

	// Cancelar contexto
	dlq.cancel()

	// Processar itens restantes na fila
	dlq.drainQueue()

	// Fechar arquivo
	if dlq.file != nil {
		dlq.file.Close()
		dlq.file = nil
	}

	return nil
}

// AddEntry adiciona entrada à DLQ
func (dlq *DeadLetterQueue) AddEntry(originalEntry types.LogEntry, errorMsg, errorType, failedSink string, retryCount int, context map[string]string) error {
	if !dlq.config.Enabled {
		return nil
	}

	now := time.Now()
	entryID := fmt.Sprintf("%s_%d_%s", failedSink, now.UnixNano(), originalEntry.SourceID)

	entry := DLQEntry{
		Timestamp:     now,
		OriginalEntry: originalEntry,
		ErrorMessage:  errorMsg,
		ErrorType:     errorType,
		FailedSink:    failedSink,
		RetryCount:    retryCount,
		Context:       context,

		// Campos de reprocessamento
		ReprocessAttempts:   0,
		ReprocessingEnabled: dlq.config.ReprocessingConfig.Enabled,
		EntryID:            entryID,
		NextReprocessTime:   now.Add(dlq.config.ReprocessingConfig.MinEntryAge),
	}

	// Tentar adicionar à fila
	select {
	case dlq.queue <- entry:
		dlq.mutex.Lock()
		dlq.stats.TotalEntries++
		dlq.mutex.Unlock()
		return nil
	default:
		// Fila cheia - log warning e descarta
		dlq.logger.Warn("DLQ queue full, dropping entry")
		dlq.mutex.Lock()
		dlq.stats.WriteErrors++
		dlq.mutex.Unlock()
		return fmt.Errorf("DLQ queue is full (capacity: %d), entry dropped", cap(dlq.queue))
	}
}

// processingLoop loop principal de processamento
func (dlq *DeadLetterQueue) processingLoop() {
	flushTicker := time.NewTicker(dlq.config.FlushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-dlq.ctx.Done():
			return

		case entry := <-dlq.queue:
			dlq.writeEntry(entry)

		case <-flushTicker.C:
			dlq.flushFile()
		}
	}
}

// writeEntry escreve entrada no arquivo DLQ
func (dlq *DeadLetterQueue) writeEntry(entry DLQEntry) {
	dlq.mutex.Lock()
	defer dlq.mutex.Unlock()

	if dlq.file == nil {
		dlq.logger.Error("DLQ file not open")
		dlq.stats.WriteErrors++
		return
	}

	// Verificar se precisa rotacionar arquivo
	if dlq.shouldRotateFile() {
		dlq.rotateFile()
	}

	var data []byte
	var err error

	if dlq.config.JSONFormat {
		data, err = json.Marshal(entry)
		if err != nil {
			dlq.logger.WithError(err).Error("Failed to marshal DLQ entry")
			dlq.stats.WriteErrors++
			return
		}
		data = append(data, '\n')
	} else {
		// Formato texto simples
		line := fmt.Sprintf("[%s] SINK:%s ERROR:%s RETRY:%d MSG:%s ORIGINAL:%s\n",
			entry.Timestamp.Format(time.RFC3339),
			entry.FailedSink,
			entry.ErrorType,
			entry.RetryCount,
			entry.ErrorMessage,
			entry.OriginalEntry.Message,
		)
		data = []byte(line)
	}

	if _, err := dlq.file.Write(data); err != nil {
		dlq.logger.WithError(err).Error("Failed to write DLQ entry")
		dlq.stats.WriteErrors++
		return
	}

	dlq.stats.EntriesWritten++
}

// shouldRotateFile verifica se deve rotacionar arquivo
func (dlq *DeadLetterQueue) shouldRotateFile() bool {
	if dlq.file == nil {
		return true
	}

	// Verificar tamanho do arquivo
	info, err := dlq.file.Stat()
	if err != nil {
		return true
	}

	maxSize := dlq.config.MaxFileSize * 1024 * 1024 // Converter MB para bytes
	return info.Size() >= maxSize
}

// rotateFile rotaciona arquivo DLQ
func (dlq *DeadLetterQueue) rotateFile() {
	if dlq.file != nil {
		dlq.file.Close()
	}

	// Criar novo arquivo
	if err := dlq.createNewFile(); err != nil {
		dlq.logger.WithError(err).Error("Failed to create new DLQ file")
	}
}

// createNewFile cria novo arquivo DLQ
func (dlq *DeadLetterQueue) createNewFile() error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("dlq_%s.log", timestamp)
	filepath := filepath.Join(dlq.config.Directory, filename)

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	dlq.file = file
	dlq.stats.FilesCreated++

	dlq.logger.WithField("file", filepath).Debug("Created new DLQ file")
	return nil
}

// flushFile força flush do arquivo
func (dlq *DeadLetterQueue) flushFile() {
	dlq.mutex.Lock()
	defer dlq.mutex.Unlock()

	if dlq.file != nil {
		dlq.file.Sync()
		dlq.stats.LastFlush = time.Now()
	}
}

// drainQueue processa itens restantes na fila
func (dlq *DeadLetterQueue) drainQueue() {
	for {
		select {
		case entry := <-dlq.queue:
			dlq.writeEntry(entry)
		default:
			return
		}
	}
}

// cleanupLoop loop de limpeza de arquivos antigos
func (dlq *DeadLetterQueue) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour) // Cleanup diário
	defer ticker.Stop()

	for {
		select {
		case <-dlq.ctx.Done():
			return
		case <-ticker.C:
			dlq.cleanupOldFiles()
		}
	}
}

// cleanupOldFiles remove arquivos DLQ antigos
func (dlq *DeadLetterQueue) cleanupOldFiles() {
	pattern := filepath.Join(dlq.config.Directory, "dlq_*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		dlq.logger.WithError(err).Error("Failed to list DLQ files for cleanup")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -dlq.config.RetentionDays)
	removedCount := 0

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(file); err != nil {
				dlq.logger.WithError(err).WithField("file", file).Warn("Failed to remove old DLQ file")
			} else {
				removedCount++
				dlq.logger.WithField("file", file).Debug("Removed old DLQ file")
			}
		}
	}

	// Limitar número total de arquivos
	if len(files)-removedCount > dlq.config.MaxFiles {
		// Ordenar por data de modificação e remover os mais antigos
		// (implementação simplificada - pode ser melhorada)
		dlq.logger.WithField("max_files", dlq.config.MaxFiles).
			Debug("DLQ file count exceeds limit, consider manual cleanup")
	}

	if removedCount > 0 {
		dlq.logger.WithField("removed_count", removedCount).Info("DLQ cleanup completed")
	}
}

// GetStats retorna estatísticas da DLQ
func (dlq *DeadLetterQueue) GetStats() Stats {
	dlq.mutex.RLock()
	defer dlq.mutex.RUnlock()

	stats := dlq.stats
	stats.CurrentQueueSize = len(dlq.queue)
	return stats
}

// GetInfo retorna informações detalhadas da DLQ
func (dlq *DeadLetterQueue) GetInfo() map[string]interface{} {
	stats := dlq.GetStats()

	return map[string]interface{}{
		"enabled":             dlq.config.Enabled,
		"directory":           dlq.config.Directory,
		"total_entries":       stats.TotalEntries,
		"entries_written":     stats.EntriesWritten,
		"write_errors":        stats.WriteErrors,
		"current_queue_size":  stats.CurrentQueueSize,
		"max_queue_size":      dlq.config.QueueSize,
		"files_created":       stats.FilesCreated,
		"last_flush":          stats.LastFlush,
		"max_files":           dlq.config.MaxFiles,
		"max_file_size_mb":    dlq.config.MaxFileSize,
		"retention_days":      dlq.config.RetentionDays,
		"json_format":         dlq.config.JSONFormat,
	}
}

// IsHealthy verifica se a DLQ está saudável
func (dlq *DeadLetterQueue) IsHealthy() bool {
	dlq.mutex.RLock()
	defer dlq.mutex.RUnlock()

	if !dlq.config.Enabled {
		return true // Se desabilitado, considera saudável
	}

	// Verificar se está rodando e arquivo está aberto
	return dlq.isRunning && dlq.file != nil
}

// AlertManager gerencia alertas da DLQ
type AlertManager struct {
	config       AlertConfig
	logger       *logrus.Logger
	dlq          *DeadLetterQueue
	lastAlerts   map[string]time.Time
	mutex        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool
}

// AlertType tipos de alerta
type AlertType string

const (
	AlertHighEntryRate   AlertType = "high_entry_rate"
	AlertHighTotalCount  AlertType = "high_total_count"
	AlertHighQueueSize   AlertType = "high_queue_size"
	AlertWriteErrors     AlertType = "write_errors"
)

// Alert estrutura de alerta
type Alert struct {
	Type        AlertType `json:"type"`
	Severity    string    `json:"severity"`
	Timestamp   time.Time `json:"timestamp"`
	Message     string    `json:"message"`
	CurrentValue interface{} `json:"current_value"`
	Threshold   interface{} `json:"threshold"`
	Stats       *Stats    `json:"stats,omitempty"`
}

// NewAlertManager cria novo gerenciador de alertas
func NewAlertManager(config AlertConfig, dlq *DeadLetterQueue, logger *logrus.Logger) *AlertManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão para alertas
	if config.CheckInterval == 0 {
		config.CheckInterval = 1 * time.Minute
	}
	if config.CooldownPeriod == 0 {
		config.CooldownPeriod = 5 * time.Minute
	}
	if config.EntriesPerMinuteThreshold == 0 {
		config.EntriesPerMinuteThreshold = 100
	}
	if config.QueueSizeThreshold == 0 {
		config.QueueSizeThreshold = int(float64(dlq.config.QueueSize) * 0.8) // 80% da capacidade
	}

	return &AlertManager{
		config:     config,
		logger:     logger,
		dlq:        dlq,
		lastAlerts: make(map[string]time.Time),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start inicia o monitoramento de alertas
func (am *AlertManager) Start() error {
	if !am.config.Enabled {
		am.logger.Info("DLQ alerts disabled")
		return nil
	}

	am.mutex.Lock()
	defer am.mutex.Unlock()

	if am.isRunning {
		return fmt.Errorf("alert manager already running")
	}

	am.isRunning = true
	am.logger.Info("Starting DLQ alert manager", map[string]interface{}{
		"check_interval":                am.config.CheckInterval,
		"entries_per_minute_threshold":  am.config.EntriesPerMinuteThreshold,
		"total_entries_threshold":       am.config.TotalEntriesThreshold,
		"queue_size_threshold":          am.config.QueueSizeThreshold,
	})

	// Iniciar loop de monitoramento
	go am.monitoringLoop()

	return nil
}

// Stop para o monitoramento de alertas
func (am *AlertManager) Stop() error {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	if !am.isRunning {
		return nil
	}

	am.logger.Info("Stopping DLQ alert manager")
	am.isRunning = false
	am.cancel()

	return nil
}

// monitoringLoop loop principal de monitoramento
func (am *AlertManager) monitoringLoop() {
	ticker := time.NewTicker(am.config.CheckInterval)
	defer ticker.Stop()

	previousStats := am.dlq.GetStats()

	for {
		select {
		case <-am.ctx.Done():
			return
		case <-ticker.C:
			currentStats := am.dlq.GetStats()
			am.checkAlerts(previousStats, currentStats)
			previousStats = currentStats
		}
	}
}

// checkAlerts verifica se algum threshold foi ultrapassado
func (am *AlertManager) checkAlerts(prevStats, currentStats Stats) {
	// 1. Verificar taxa de entradas por minuto
	duration := time.Since(prevStats.LastFlush)
	if duration > 0 {
		entriesInInterval := currentStats.TotalEntries - prevStats.TotalEntries
		entriesPerMinute := float64(entriesInInterval) / duration.Minutes()

		if entriesPerMinute > float64(am.config.EntriesPerMinuteThreshold) {
			am.triggerAlert(Alert{
				Type:         AlertHighEntryRate,
				Severity:     "warning",
				Timestamp:    time.Now(),
				Message:      fmt.Sprintf("DLQ receiving high volume of entries: %.2f entries/minute", entriesPerMinute),
				CurrentValue: entriesPerMinute,
				Threshold:    am.config.EntriesPerMinuteThreshold,
				Stats:        &currentStats,
			})
		}
	}

	// 2. Verificar total de entradas
	if am.config.TotalEntriesThreshold > 0 && currentStats.TotalEntries > am.config.TotalEntriesThreshold {
		am.triggerAlert(Alert{
			Type:         AlertHighTotalCount,
			Severity:     "critical",
			Timestamp:    time.Now(),
			Message:      fmt.Sprintf("DLQ total entries exceeded threshold: %d", currentStats.TotalEntries),
			CurrentValue: currentStats.TotalEntries,
			Threshold:    am.config.TotalEntriesThreshold,
			Stats:        &currentStats,
		})
	}

	// 3. Verificar tamanho da fila
	if currentStats.CurrentQueueSize > am.config.QueueSizeThreshold {
		am.triggerAlert(Alert{
			Type:         AlertHighQueueSize,
			Severity:     "warning",
			Timestamp:    time.Now(),
			Message:      fmt.Sprintf("DLQ queue size high: %d/%d", currentStats.CurrentQueueSize, am.dlq.config.QueueSize),
			CurrentValue: currentStats.CurrentQueueSize,
			Threshold:    am.config.QueueSizeThreshold,
			Stats:        &currentStats,
		})
	}

	// 4. Verificar erros de escrita
	if currentStats.WriteErrors > prevStats.WriteErrors {
		newErrors := currentStats.WriteErrors - prevStats.WriteErrors
		am.triggerAlert(Alert{
			Type:         AlertWriteErrors,
			Severity:     "error",
			Timestamp:    time.Now(),
			Message:      fmt.Sprintf("DLQ write errors detected: %d new errors", newErrors),
			CurrentValue: newErrors,
			Threshold:    0,
			Stats:        &currentStats,
		})
	}
}

// triggerAlert dispara um alerta se não estiver em cooldown
func (am *AlertManager) triggerAlert(alert Alert) {
	alertKey := string(alert.Type)

	am.mutex.Lock()
	defer am.mutex.Unlock()

	// Verificar cooldown
	if lastAlert, exists := am.lastAlerts[alertKey]; exists {
		if time.Since(lastAlert) < am.config.CooldownPeriod {
			return // Ainda em cooldown
		}
	}

	// Registrar alerta
	am.lastAlerts[alertKey] = alert.Timestamp

	// Log do alerta
	am.logger.WithFields(logrus.Fields{
		"type":          alert.Type,
		"severity":      alert.Severity,
		"current_value": alert.CurrentValue,
		"threshold":     alert.Threshold,
	}).Warn(alert.Message)

	// Enviar alerta
	go am.sendAlert(alert)
}

// sendAlert envia alerta via webhook/email
func (am *AlertManager) sendAlert(alert Alert) {
	// Preparar payload do alerta
	payload := map[string]interface{}{
		"alert":     alert,
		"timestamp": alert.Timestamp.Format(time.RFC3339),
		"service":   "ssw-logs-capture",
		"component": "dead-letter-queue",
	}

	// Se webhook configurado, enviar
	if am.config.WebhookURL != "" {
		if err := am.sendWebhookAlert(payload); err != nil {
			am.logger.WithError(err).Error("Failed to send webhook alert")
		}
	}

	// Se email configurado, enviar
	if am.config.EmailTo != "" {
		if err := am.sendEmailAlert(alert); err != nil {
			am.logger.WithError(err).Error("Failed to send email alert")
		}
	}
}

// sendWebhookAlert envia alerta via webhook
func (am *AlertManager) sendWebhookAlert(payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Implementação simplificada - pode ser melhorada com retry, timeout, etc.
	am.logger.WithFields(logrus.Fields{
		"webhook_url": am.config.WebhookURL,
		"payload":     string(jsonData),
	}).Info("Would send webhook alert (implementation needed)")

	return nil
}

// sendEmailAlert envia alerta via email
func (am *AlertManager) sendEmailAlert(alert Alert) error {
	// Implementação simplificada - pode ser melhorada com SMTP real
	am.logger.WithFields(logrus.Fields{
		"email_to": am.config.EmailTo,
		"subject":  fmt.Sprintf("DLQ Alert: %s", alert.Type),
		"message":  alert.Message,
	}).Info("Would send email alert (implementation needed)")

	return nil
}

// reprocessingLoop loop principal de reprocessamento
func (dlq *DeadLetterQueue) reprocessingLoop() {
	ticker := time.NewTicker(dlq.config.ReprocessingConfig.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-dlq.ctx.Done():
			return
		case <-ticker.C:
			dlq.processReprocessingBatch()
		}
	}
}

// processReprocessingBatch processa um batch de entradas para reprocessamento
func (dlq *DeadLetterQueue) processReprocessingBatch() {
	if dlq.reprocessCallback == nil {
		return
	}

	// Ler entradas dos arquivos DLQ
	entries, err := dlq.readEntriesForReprocessing()
	if err != nil {
		dlq.logger.WithError(err).Error("Failed to read DLQ entries for reprocessing")
		return
	}

	if len(entries) == 0 {
		return
	}

	dlq.logger.WithField("entries_count", len(entries)).Debug("Starting reprocessing batch")

	var processedEntries []DLQEntry
	successCount := 0
	failureCount := 0

	for _, entry := range entries {
		// Verificar se é hora de reprocessar esta entrada
		if time.Now().Before(entry.NextReprocessTime) {
			continue
		}

		// Verificar se ainda tem tentativas disponíveis
		if entry.ReprocessAttempts >= dlq.config.ReprocessingConfig.MaxRetries {
			dlq.logger.WithFields(logrus.Fields{
				"entry_id":    entry.EntryID,
				"attempts":    entry.ReprocessAttempts,
				"max_retries": dlq.config.ReprocessingConfig.MaxRetries,
			}).Debug("DLQ entry exceeded max reprocessing attempts")
			continue
		}

		// Tentar reprocessar
		dlq.mutex.Lock()
		dlq.stats.ReprocessingAttempts++
		dlq.mutex.Unlock()

		entry.ReprocessAttempts++
		entry.LastReprocessAttempt = time.Now()

		// Tentar reprocessar (o callback gerencia seu próprio contexto)
		err := dlq.reprocessCallback(entry.OriginalEntry, entry.FailedSink)

		if err != nil {
			// Falha no reprocessamento
			failureCount++

			// Calcular próximo tempo de tentativa (exponential backoff)
			nextDelay := time.Duration(float64(dlq.config.ReprocessingConfig.InitialDelay) *
				math.Pow(dlq.config.ReprocessingConfig.DelayMultiplier, float64(entry.ReprocessAttempts-1)))

			if nextDelay > dlq.config.ReprocessingConfig.MaxDelay {
				nextDelay = dlq.config.ReprocessingConfig.MaxDelay
			}

			entry.NextReprocessTime = time.Now().Add(nextDelay)

			dlq.logger.WithFields(logrus.Fields{
				"entry_id":     entry.EntryID,
				"failed_sink":  entry.FailedSink,
				"attempt":      entry.ReprocessAttempts,
				"next_attempt": entry.NextReprocessTime,
				"error":        err.Error(),
			}).Warn("DLQ reprocessing failed")

			dlq.mutex.Lock()
			dlq.stats.ReprocessingFailures++
			dlq.mutex.Unlock()

			// Atualizar entrada no arquivo
			processedEntries = append(processedEntries, entry)
		} else {
			// Sucesso no reprocessamento - remover da DLQ
			successCount++

			dlq.logger.WithFields(logrus.Fields{
				"entry_id":    entry.EntryID,
				"failed_sink": entry.FailedSink,
				"attempt":     entry.ReprocessAttempts,
			}).Info("DLQ entry reprocessed successfully")

			dlq.mutex.Lock()
			dlq.stats.ReprocessingSuccesses++
			dlq.stats.EntriesReprocessed++
			dlq.mutex.Unlock()

			// Remover entrada da DLQ
			if err := dlq.removeDLQEntry(entry.EntryID); err != nil {
				dlq.logger.WithError(err).WithField("entry_id", entry.EntryID).
					Warn("Failed to remove successfully reprocessed entry from DLQ")
			}
		}
	}

	// Atualizar arquivos DLQ com entradas que falharam
	if len(processedEntries) > 0 {
		if err := dlq.updateDLQFiles(processedEntries); err != nil {
			dlq.logger.WithError(err).Error("Failed to update DLQ files after reprocessing")
		}
	}

	// Atualizar estatísticas
	dlq.mutex.Lock()
	dlq.stats.LastReprocessing = time.Now()
	dlq.mutex.Unlock()

	if successCount > 0 || failureCount > 0 {
		dlq.logger.WithFields(logrus.Fields{
			"successful": successCount,
			"failed":     failureCount,
			"total":      len(entries),
		}).Info("DLQ reprocessing batch completed")
	}
}

// readEntriesForReprocessing lê entradas dos arquivos DLQ para reprocessamento
func (dlq *DeadLetterQueue) readEntriesForReprocessing() ([]DLQEntry, error) {
	pattern := filepath.Join(dlq.config.Directory, "dlq_*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list DLQ files: %w", err)
	}

	var allEntries []DLQEntry
	processedCount := 0

	for _, filePath := range files {
		entries, err := dlq.readEntriesFromFile(filePath)
		if err != nil {
			dlq.logger.WithError(err).WithField("file", filePath).Warn("Failed to read DLQ file")
			continue
		}

		// Filtrar entradas elegíveis para reprocessamento
		for _, entry := range entries {
			if entry.ReprocessingEnabled &&
				entry.ReprocessAttempts < dlq.config.ReprocessingConfig.MaxRetries &&
				time.Since(entry.Timestamp) >= dlq.config.ReprocessingConfig.MinEntryAge {
				allEntries = append(allEntries, entry)
				processedCount++

				// Limitar batch size
				if processedCount >= dlq.config.ReprocessingConfig.BatchSize {
					return allEntries, nil
				}
			}
		}
	}

	return allEntries, nil
}

// readEntriesFromFile lê entradas de um arquivo DLQ específico
func (dlq *DeadLetterQueue) readEntriesFromFile(filePath string) ([]DLQEntry, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []DLQEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry DLQEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			dlq.logger.WithError(err).WithField("line", line).Warn("Failed to parse DLQ entry")
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return entries, nil
}

// updateDLQFiles atualiza arquivos DLQ com entradas processadas
func (dlq *DeadLetterQueue) updateDLQFiles(updatedEntries []DLQEntry) error {
	// Ler todos os arquivos e reconstruir com atualizações
	pattern := filepath.Join(dlq.config.Directory, "dlq_*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to list DLQ files: %w", err)
	}

	// Criar mapa de entradas atualizadas para busca rápida
	updatedMap := make(map[string]DLQEntry)
	for _, entry := range updatedEntries {
		updatedMap[entry.EntryID] = entry
	}

	for _, filePath := range files {
		originalEntries, err := dlq.readEntriesFromFile(filePath)
		if err != nil {
			dlq.logger.WithError(err).WithField("file", filePath).Warn("Failed to read DLQ file for update")
			continue
		}

		var finalEntries []DLQEntry
		for _, entry := range originalEntries {
			if updated, exists := updatedMap[entry.EntryID]; exists {
				// Usar entrada atualizada
				finalEntries = append(finalEntries, updated)
			} else {
				// Manter entrada original
				finalEntries = append(finalEntries, entry)
			}
		}

		// Reescrever arquivo
		if err := dlq.rewriteDLQFile(filePath, finalEntries); err != nil {
			return fmt.Errorf("failed to rewrite DLQ file %s: %w", filePath, err)
		}
	}

	return nil
}

// rewriteDLQFile reescreve um arquivo DLQ com novas entradas
func (dlq *DeadLetterQueue) rewriteDLQFile(filePath string, entries []DLQEntry) error {
	// Criar arquivo temporário
	tmpFile := filePath + ".tmp"

	file, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			file.Close()
			os.Remove(tmpFile)
			return err
		}

		if _, err := file.Write(append(data, '\n')); err != nil {
			file.Close()
			os.Remove(tmpFile)
			return err
		}
	}

	file.Close()

	// Substituir arquivo original
	return os.Rename(tmpFile, filePath)
}

// SetReprocessCallback define callback para reprocessamento
func (dlq *DeadLetterQueue) SetReprocessCallback(callback ReprocessCallback) {
	dlq.reprocessCallback = callback
}

// removeDLQEntry remove uma entrada específica dos arquivos DLQ
func (dlq *DeadLetterQueue) removeDLQEntry(entryID string) error {
	pattern := filepath.Join(dlq.config.Directory, "dlq_*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to list DLQ files: %w", err)
	}

	for _, filePath := range files {
		entries, err := dlq.readEntriesFromFile(filePath)
		if err != nil {
			continue
		}

		var filteredEntries []DLQEntry
		found := false

		for _, entry := range entries {
			if entry.EntryID != entryID {
				filteredEntries = append(filteredEntries, entry)
			} else {
				found = true
			}
		}

		if found {
			return dlq.rewriteDLQFile(filePath, filteredEntries)
		}
	}

	return fmt.Errorf("DLQ entry %s not found", entryID)
}