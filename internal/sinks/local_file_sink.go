package sinks

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// LocalFileSink implementa sink para arquivos locais
type LocalFileSink struct {
	config    types.LocalFileConfig
	logger    *logrus.Logger

	queue     chan types.LogEntry
	files     map[string]*logFile
	filesMutex sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mutex     sync.RWMutex
}

// logFile representa um arquivo de log aberto
type logFile struct {
	path       string
	file       *os.File
	writer     io.Writer
	currentSize int64
	lastWrite   time.Time
	mutex       sync.Mutex
}

// NewLocalFileSink cria um novo sink para arquivos locais
func NewLocalFileSink(config types.LocalFileConfig, logger *logrus.Logger) *LocalFileSink {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão para novos campos
	if config.OutputFormat == "" {
		config.OutputFormat = "json"
	}
	if config.TextFormat.TimestampFormat == "" {
		config.TextFormat.TimestampFormat = "2006-01-02T15:04:05.000Z"
	}

	// Use configured queue size, default to 10000 if not set
	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 10000
	}

	return &LocalFileSink{
		config: config,
		logger: logger,
		queue:  make(chan types.LogEntry, queueSize),
		files:  make(map[string]*logFile),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start inicia o sink
func (lfs *LocalFileSink) Start(ctx context.Context) error {
	if !lfs.config.Enabled {
		lfs.logger.Info("Local file sink disabled")
		return nil
	}

	lfs.mutex.Lock()
	defer lfs.mutex.Unlock()

	if lfs.isRunning {
		return fmt.Errorf("local file sink already running")
	}

	// Criar diretório se não existir
	if err := os.MkdirAll(lfs.config.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	lfs.isRunning = true
	lfs.logger.WithField("directory", lfs.config.Directory).Info("Starting local file sink")

	// Iniciar goroutine de processamento
	go lfs.processLoop()

	// Iniciar goroutine de rotação de arquivos
	go lfs.rotationLoop()

	return nil
}

// Stop para o sink
func (lfs *LocalFileSink) Stop() error {
	lfs.mutex.Lock()
	defer lfs.mutex.Unlock()

	if !lfs.isRunning {
		return nil
	}

	lfs.logger.Info("Stopping local file sink")
	lfs.isRunning = false

	// Cancelar contexto
	lfs.cancel()

	// Fechar arquivos abertos
	lfs.filesMutex.Lock()
	for _, lf := range lfs.files {
		lf.close()
	}
	lfs.files = make(map[string]*logFile)
	lfs.filesMutex.Unlock()

	return nil
}

// Send envia logs para o sink com backpressure - nunca descarta logs
func (lfs *LocalFileSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if !lfs.config.Enabled {
		return nil
	}

	for _, entry := range entries {
		// Implementar backpressure - bloquear até conseguir enviar
		select {
		case lfs.queue <- entry:
			// Enviado para fila com sucesso
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			// Se demorar mais que 5 segundos, log um warning mas continue tentando
			lfs.logger.WithField("queue_utilization", lfs.GetQueueUtilization()).
				Warn("Local file sink queue backpressure - waiting to send log")

			// Tentar novamente sem timeout para garantir que o log seja enviado
			select {
			case lfs.queue <- entry:
				// Enviado com sucesso após espera
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// IsHealthy verifica se o sink está saudável
func (lfs *LocalFileSink) IsHealthy() bool {
	lfs.mutex.RLock()
	defer lfs.mutex.RUnlock()

	// Apenas verificar se está rodando - não marcar como unhealthy por utilização da fila
	// já que implementamos backpressure
	return lfs.isRunning
}

// GetQueueUtilization retorna a utilização da fila
func (lfs *LocalFileSink) GetQueueUtilization() float64 {
	return float64(len(lfs.queue)) / float64(cap(lfs.queue))
}

// processLoop loop principal de processamento
func (lfs *LocalFileSink) processLoop() {
	for {
		select {
		case <-lfs.ctx.Done():
			return
		case entry := <-lfs.queue:
			lfs.writeLogEntry(entry)
		}
	}
}

// rotationLoop loop de rotação de arquivos
func (lfs *LocalFileSink) rotationLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-lfs.ctx.Done():
			return
		case <-ticker.C:
			lfs.rotateFiles()
		}
	}
}

// writeLogEntry escreve uma entrada de log
func (lfs *LocalFileSink) writeLogEntry(entry types.LogEntry) {
	startTime := time.Now()

	// Determinar nome do arquivo baseado nos labels
	filename := lfs.getLogFileName(entry)

	// Obter ou criar arquivo
	lf, err := lfs.getOrCreateLogFile(filename)
	if err != nil {
		lfs.logger.WithError(err).WithField("filename", filename).Error("Failed to get log file")
		metrics.RecordLogSent("local_file", "error")
		metrics.RecordError("local_file_sink", "file_error")
		return
	}

	// Escrever entrada
	if err := lf.writeEntry(entry, lfs.config); err != nil {
		lfs.logger.WithError(err).WithField("filename", filename).Error("Failed to write log entry")
		metrics.RecordLogSent("local_file", "error")
		metrics.RecordError("local_file_sink", "write_error")
		return
	}

	// Métricas
	duration := time.Since(startTime)
	metrics.RecordSinkSendDuration("local_file", duration)
	metrics.RecordLogSent("local_file", "success")
	metrics.SetSinkQueueUtilization("local_file", lfs.GetQueueUtilization())
}

// getLogFileName determina o nome do arquivo de log
func (lfs *LocalFileSink) getLogFileName(entry types.LogEntry) string {
	// Usar combinação de source_type e labels para determinar o arquivo
	var parts []string

	// Adicionar data
	date := entry.Timestamp.Format("2006-01-02")
	parts = append(parts, date)

	// Adicionar source type
	if entry.SourceType != "" {
		parts = append(parts, entry.SourceType)
	}

	// Adicionar container name ou file path
	if containerName, exists := entry.Labels["container_name"]; exists {
		parts = append(parts, sanitizeFilename(containerName))
	} else if filepath, exists := entry.Labels["filepath"]; exists {
		basename := filepath[strings.LastIndex(filepath, "/")+1:]
		parts = append(parts, sanitizeFilename(basename))
	}

	filename := strings.Join(parts, "_") + ".log"
	return filepath.Join(lfs.config.Directory, filename)
}

// getOrCreateLogFile obtém ou cria um arquivo de log
func (lfs *LocalFileSink) getOrCreateLogFile(filename string) (*logFile, error) {
	lfs.filesMutex.RLock()
	lf, exists := lfs.files[filename]
	lfs.filesMutex.RUnlock()

	if exists {
		return lf, nil
	}

	// Criar novo arquivo
	lfs.filesMutex.Lock()
	defer lfs.filesMutex.Unlock()

	// Verificar novamente (double-check locking)
	if lf, exists := lfs.files[filename]; exists {
		return lf, nil
	}

	// Criar arquivo
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Obter tamanho atual
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat log file: %w", err)
	}

	lf = &logFile{
		path:        filename,
		file:        file,
		writer:      file,
		currentSize: info.Size(),
		lastWrite:   time.Now(),
	}

	lfs.files[filename] = lf
	lfs.logger.WithField("filename", filename).Debug("Created new log file")

	return lf, nil
}

// rotateFiles rotaciona arquivos grandes
func (lfs *LocalFileSink) rotateFiles() {
	lfs.filesMutex.Lock()
	defer lfs.filesMutex.Unlock()

	// Coletamos arquivos para rotacionar primeiro para evitar concurrent map iteration/write
	filesToRotate := make([]string, 0)
	for filename, lf := range lfs.files {
		lf.mutex.Lock()

		// Verificar se arquivo precisa ser rotacionado
		maxSizeBytes := int64(lfs.config.Rotation.MaxSizeMB) * 1024 * 1024
		if lf.currentSize > maxSizeBytes {
			filesToRotate = append(filesToRotate, filename)
		}

		lf.mutex.Unlock()
	}

	// Agora rotacionamos os arquivos coletados
	for _, filename := range filesToRotate {
		if lf, exists := lfs.files[filename]; exists {
			lf.mutex.Lock()

			lfs.logger.WithFields(logrus.Fields{
				"filename":     filename,
				"current_size": lf.currentSize,
				"max_size":     int64(lfs.config.Rotation.MaxSizeMB) * 1024 * 1024,
			}).Info("Rotating log file")

			// Fechar arquivo atual
			lf.close()

			// Rotacionar arquivo
			if err := lfs.rotateFile(filename); err != nil {
				lfs.logger.WithError(err).WithField("filename", filename).Error("Failed to rotate log file")
			}

			lf.mutex.Unlock()

			// Remover do mapa (será recriado quando necessário)
			delete(lfs.files, filename)
		}
	}

	// Limpar arquivos antigos
	lfs.cleanupOldFiles()
}

// rotateFile rotaciona um arquivo específico
func (lfs *LocalFileSink) rotateFile(filename string) error {
	// Gerar nome do arquivo rotacionado
	timestamp := time.Now().Format("20060102-150405")
	rotatedName := filename + "." + timestamp

	// Comprimir se habilitado
	if lfs.config.Rotation.Compress {
		rotatedName += ".gz"
		return lfs.compressFile(filename, rotatedName)
	}

	// Renomear arquivo
	return os.Rename(filename, rotatedName)
}

// compressFile comprime um arquivo
func (lfs *LocalFileSink) compressFile(srcFile, dstFile string) error {
	// Abrir arquivo original
	src, err := os.Open(srcFile)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Criar arquivo comprimido
	dst, err := os.Create(dstFile)
	if err != nil {
		return fmt.Errorf("failed to create compressed file: %w", err)
	}
	defer dst.Close()

	// Compressor gzip
	gzWriter := gzip.NewWriter(dst)
	defer gzWriter.Close()

	// Copiar dados
	if _, err := io.Copy(gzWriter, src); err != nil {
		return fmt.Errorf("failed to compress file: %w", err)
	}

	// Remover arquivo original
	if err := os.Remove(srcFile); err != nil {
		lfs.logger.WithError(err).WithField("filename", srcFile).Warn("Failed to remove original file after compression")
	}

	return nil
}

// cleanupOldFiles limpa arquivos antigos
func (lfs *LocalFileSink) cleanupOldFiles() {
	// Listar arquivos no diretório
	files, err := filepath.Glob(filepath.Join(lfs.config.Directory, "*.log*"))
	if err != nil {
		lfs.logger.WithError(err).Error("Failed to list log files for cleanup")
		return
	}

	if len(files) <= lfs.config.Rotation.MaxFiles {
		return
	}

	// Ordenar arquivos por data de modificação
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var fileInfos []fileInfo
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{
			path:    file,
			modTime: info.ModTime(),
		})
	}

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].modTime.Before(fileInfos[j].modTime)
	})

	// Remover arquivos mais antigos
	toRemove := len(fileInfos) - lfs.config.Rotation.MaxFiles
	for i := 0; i < toRemove; i++ {
		file := fileInfos[i].path
		if err := os.Remove(file); err != nil {
			lfs.logger.WithError(err).WithField("filename", file).Error("Failed to remove old log file")
		} else {
			lfs.logger.WithField("filename", file).Info("Removed old log file")
		}
	}
}

// writeEntry escreve uma entrada no arquivo
func (lf *logFile) writeEntry(entry types.LogEntry, config types.LocalFileConfig) error {
	lf.mutex.Lock()
	defer lf.mutex.Unlock()

	var line string

	// Formatar entrada baseado no modo de output
	switch strings.ToLower(config.OutputFormat) {
	case "text":
		line = lf.formatTextOutput(entry, config)
	case "json":
		fallthrough
	default:
		line = lf.formatJSONOutput(entry)
	}

	// Escrever
	n, err := lf.writer.Write([]byte(line))
	if err != nil {
		return err
	}

	lf.currentSize += int64(n)
	lf.lastWrite = time.Now()

	// Flush se suportado
	if flusher, ok := lf.writer.(interface{ Flush() error }); ok {
		flusher.Flush()
	}

	return nil
}

// close fecha o arquivo
func (lf *logFile) close() {
	lf.mutex.Lock()
	defer lf.mutex.Unlock()

	if lf.file != nil {
		lf.file.Close()
		lf.file = nil
	}
}

// formatJSONOutput formata entrada como JSON
func (lf *logFile) formatJSONOutput(entry types.LogEntry) string {
	// Criar estrutura para JSON
	output := map[string]interface{}{
		"timestamp":    entry.Timestamp.Format(time.RFC3339Nano),
		"message":      entry.Message,
		"source_type":  entry.SourceType,
		"source_id":    entry.SourceID,
		"processed_at": entry.ProcessedAt.Format(time.RFC3339Nano),
	}

	// Adicionar labels se existirem
	if len(entry.Labels) > 0 {
		output["labels"] = entry.Labels
	}

	// Serializar para JSON
	jsonBytes, err := json.Marshal(output)
	if err != nil {
		// Fallback para formato simples em caso de erro
		return fmt.Sprintf("{\"timestamp\":\"%s\",\"message\":\"%s\",\"error\":\"json_marshal_failed\"}\n",
			entry.Timestamp.Format(time.RFC3339),
			strings.ReplaceAll(entry.Message, "\"", "\\\""))
	}

	return string(jsonBytes) + "\n"
}

// formatTextOutput formata entrada como texto puro
func (lf *logFile) formatTextOutput(entry types.LogEntry, config types.LocalFileConfig) string {
	var parts []string

	// Adicionar timestamp se habilitado
	if config.TextFormat.IncludeTimestamp {
		parts = append(parts, entry.Timestamp.Format(config.TextFormat.TimestampFormat))
	}

	// Adicionar source type e ID
	parts = append(parts, fmt.Sprintf("[%s:%s]",
		strings.ToUpper(entry.SourceType),
		entry.SourceID))

	// Adicionar labels importantes como prefixo se existirem
	// Adicionar labels se habilitado
	if config.TextFormat.IncludeLabels && len(entry.Labels) > 0 {
		var labelPairs []string
		// Fazer cópia do map para evitar concurrent access durante iteração
		labelsCopy := make(map[string]string, len(entry.Labels))
		for k, v := range entry.Labels {
			labelsCopy[k] = v
		}
		for key, value := range labelsCopy {
			// Incluir apenas algumas labels importantes no formato texto
			if key == "level" || key == "service" || key == "container" || key == "container_name" {
				labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", key, value))
			}
		}
		if len(labelPairs) > 0 {
			separator := config.TextFormat.FieldSeparator
			if separator == "" {
				separator = " | "
			}
			parts = append(parts, fmt.Sprintf("{%s}", strings.Join(labelPairs, ",")))
		}
	}

	// Adicionar mensagem
	parts = append(parts, entry.Message)

	separator := config.TextFormat.FieldSeparator
	if separator == "" {
		separator = " | "
	}
	return strings.Join(parts, separator) + "\n"
}

// sanitizeFilename sanitiza nome de arquivo
func sanitizeFilename(name string) string {
	// Substituir caracteres inválidos
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(name)
}