package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

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
}

// Stats estatísticas da DLQ
type Stats struct {
	TotalEntries    int64 `json:"total_entries"`
	EntriesWritten  int64 `json:"entries_written"`
	WriteErrors     int64 `json:"write_errors"`
	CurrentQueueSize int  `json:"current_queue_size"`
	FilesCreated    int64 `json:"files_created"`
	LastFlush       time.Time `json:"last_flush"`
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
func (dlq *DeadLetterQueue) AddEntry(originalEntry types.LogEntry, errorMsg, errorType, failedSink string, retryCount int, context map[string]string) {
	if !dlq.config.Enabled {
		return
	}

	entry := DLQEntry{
		Timestamp:     time.Now(),
		OriginalEntry: originalEntry,
		ErrorMessage:  errorMsg,
		ErrorType:     errorType,
		FailedSink:    failedSink,
		RetryCount:    retryCount,
		Context:       context,
	}

	// Tentar adicionar à fila
	select {
	case dlq.queue <- entry:
		dlq.mutex.Lock()
		dlq.stats.TotalEntries++
		dlq.mutex.Unlock()
	default:
		// Fila cheia - log warning e descarta
		dlq.logger.Warn("DLQ queue full, dropping entry")
		dlq.mutex.Lock()
		dlq.stats.WriteErrors++
		dlq.mutex.Unlock()
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