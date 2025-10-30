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
	"syscall"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/compression"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// LocalFileSink implementa sink para arquivos locais
type LocalFileSink struct {
	config    types.LocalFileConfig
	logger    *logrus.Logger
	compressor *compression.HTTPCompressor

	queue     chan types.LogEntry
	files     map[string]*logFile
	filesMutex sync.RWMutex

	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mutex     sync.RWMutex

	// Proteções contra disco cheio
	lastDiskCheck time.Time
	diskSpaceMutex sync.RWMutex

	// Gerenciamento de file descriptors (C8: File Descriptor Leak)
	maxOpenFiles int
	openFileCount int
}

// logFile representa um arquivo de log aberto
type logFile struct {
	path         string
	file         *os.File
	writer       io.Writer
	currentSize  int64
	lastWrite    time.Time
	mutex        sync.Mutex
	useCompression bool
	compressor   *compression.HTTPCompressor
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

	// Use configured queue size, default to 3000 if not set (menor para proteger memória)
	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 3000
	}

	// Use configured worker count, default to 3 if not set
	workerCount := config.WorkerCount
	if workerCount <= 0 {
		workerCount = 3
	}

	logger.WithFields(logrus.Fields{
		"queue_size":   queueSize,
		"worker_count": workerCount,
	}).Info("Initializing local file sink")

	// Configurar proteções padrão
	if config.MaxTotalDiskGB <= 0 {
		config.MaxTotalDiskGB = 5.0 // 5GB padrão
	}
	if config.DiskCheckInterval == "" {
		config.DiskCheckInterval = "60s"
	}
	if config.CleanupThresholdPercent <= 0 {
		config.CleanupThresholdPercent = 90.0 // 90% padrão
	}

	// Configurar limite de file descriptors (C8: File Descriptor Leak)
	maxOpenFiles := 100 // Padrão: 100 arquivos abertos simultaneamente
	if config.MaxOpenFiles > 0 {
		maxOpenFiles = config.MaxOpenFiles
	}

	logger.WithFields(logrus.Fields{
		"max_open_files": maxOpenFiles,
	}).Info("File descriptor management configured")

	// Configurar compressor HTTP para local files
	compressionConfig := compression.Config{
		DefaultAlgorithm: compression.AlgorithmGzip,
		AdaptiveEnabled:  false, // Disable adaptive for local files
		MinBytes:         512,   // Compress smaller files for disk space
		Level:            6,
		PoolSize:         5,
		PerSink: map[string]compression.SinkCompressionConfig{
			"local_file": {
				Algorithm: compression.AlgorithmGzip,
				Enabled:   config.Compress, // Use rotation compress setting
				Level:     6,
			},
		},
	}
	compressor := compression.NewHTTPCompressor(compressionConfig, logger)

	return &LocalFileSink{
		config:        config,
		logger:        logger,
		compressor:    compressor,
		queue:         make(chan types.LogEntry, queueSize),
		files:         make(map[string]*logFile),
		ctx:           ctx,
		cancel:        cancel,
		maxOpenFiles:  maxOpenFiles,
		openFileCount: 0,
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

	// Definir como healthy no início
	metrics.SetComponentHealth("sink", "local_file", true)

	// Determine worker count
	workerCount := lfs.config.WorkerCount
	if workerCount <= 0 {
		workerCount = 3 // Default to 3 workers
	}

	// Iniciar goroutines de processamento (multiple workers)
	for i := 0; i < workerCount; i++ {
		workerID := i
		go lfs.processLoop(workerID)
	}

	lfs.logger.WithField("worker_count", workerCount).Info("Started local file sink workers")

	// Iniciar goroutine de monitoramento de disco
	go lfs.diskMonitorLoop()

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

	// Definir como unhealthy ao parar
	metrics.SetComponentHealth("sink", "local_file", false)

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

// closeLeastRecentlyUsed fecha o arquivo menos recentemente usado (LRU) para liberar file descriptors
// Deve ser chamado com filesMutex LOCK já adquirido
func (lfs *LocalFileSink) closeLeastRecentlyUsed() {
	// Encontrar arquivo menos recentemente usado
	var oldestPath string
	var oldestTime time.Time
	firstIteration := true

	for path, lf := range lfs.files {
		lf.mutex.Lock()
		lastWrite := lf.lastWrite
		lf.mutex.Unlock()

		if firstIteration || lastWrite.Before(oldestTime) {
			oldestPath = path
			oldestTime = lastWrite
			firstIteration = false
		}
	}

	// Fechar o arquivo mais antigo
	if oldestPath != "" {
		if lf, exists := lfs.files[oldestPath]; exists {
			lf.close()
			delete(lfs.files, oldestPath)
			lfs.openFileCount--

			lfs.logger.WithFields(logrus.Fields{
				"file":        filepath.Base(oldestPath),
				"last_write":  oldestTime.Format(time.RFC3339),
				"open_files":  lfs.openFileCount,
				"max_files":   lfs.maxOpenFiles,
			}).Debug("Closed LRU file to free file descriptor")
		}
	}
}

// Send envia logs para o sink com backpressure - respeita contexto para evitar deadlock
func (lfs *LocalFileSink) Send(ctx context.Context, entries []types.LogEntry) error {
	if !lfs.config.Enabled {
		return nil
	}

	for _, entry := range entries {
		// SEMPRE respeitar o contexto para evitar deadlock
		select {
		case lfs.queue <- entry:
			// Enviado para fila com sucesso
		case <-ctx.Done():
			// Contexto cancelado/timeout - retornar erro imediatamente
			return fmt.Errorf("failed to send to local file sink queue: %w", ctx.Err())
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
func (lfs *LocalFileSink) processLoop(workerID int) {
	lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker started")

	for {
		select {
		case <-lfs.ctx.Done():
			lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker stopped")
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
	// Verificação robusta de espaço em disco antes de processar
	if !lfs.isDiskSpaceAvailable() {
		lfs.logger.WithFields(logrus.Fields{
			"source_type": entry.SourceType,
			"source_id":   entry.SourceID,
			"reason":      "insufficient_disk_space",
		}).Error("Dropping log entry due to insufficient disk space")
		metrics.RecordError("local_file_sink", "disk_full")

		// Tentar limpeza de emergência imediata
		lfs.performEmergencyCleanup()

		// Verificar novamente após limpeza
		if !lfs.isDiskSpaceAvailable() {
			metrics.RecordError("local_file_sink", "disk_full_after_cleanup")
			return
		}
	}

	// Verificação adicional do tamanho estimado da entrada
	estimatedSize := int64(len(entry.Message) + 200) // ~200 bytes para metadados
	if !lfs.canWriteSize(estimatedSize) {
		lfs.logger.WithFields(logrus.Fields{
			"source_type":     entry.SourceType,
			"source_id":       entry.SourceID,
			"estimated_size":  estimatedSize,
			"reason":          "would_exceed_disk_limit",
		}).Warn("Dropping log entry - would exceed disk limit")
		metrics.RecordError("local_file_sink", "size_limit_exceeded")
		return
	}

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
	// Escolher pattern baseado no source_type
	var pattern string

	if entry.SourceType == "container" && lfs.config.FilenamePatternContainers != "" {
		pattern = lfs.config.FilenamePatternContainers
	} else if entry.SourceType == "file" && lfs.config.FilenamePatternFiles != "" {
		pattern = lfs.config.FilenamePatternFiles
	} else if lfs.config.FilenamePattern != "" {
		pattern = lfs.config.FilenamePattern
	}

	// Se há pattern configurado, usar lógica dinâmica
	if pattern != "" {
		return lfs.buildFilenameFromPattern(entry, pattern)
	}

	// Fallback para lógica legada
	var parts []string

	// Adicionar data
	date := entry.Timestamp.Format("2006-01-02")
	parts = append(parts, date)

	// Adicionar source type
	if entry.SourceType != "" {
		parts = append(parts, entry.SourceType)
	}

	// Adicionar container name ou file path (thread-safe access)
	if containerName, exists := entry.GetLabel("container_name"); exists {
		parts = append(parts, sanitizeFilename(containerName))
	} else if filepath, exists := entry.GetLabel("filepath"); exists {
		basename := filepath[strings.LastIndex(filepath, "/")+1:]
		parts = append(parts, sanitizeFilename(basename))
	}

	filename := strings.Join(parts, "_") + ".log"
	return filepath.Join(lfs.config.Directory, filename)
}

// buildFilenameFromPattern constrói o nome do arquivo substituindo placeholders no pattern
func (lfs *LocalFileSink) buildFilenameFromPattern(entry types.LogEntry, pattern string) string {

	// Substituir {date} - formato: YYYY-MM-DD
	date := entry.Timestamp.Format("2006-01-02")
	pattern = strings.ReplaceAll(pattern, "{date}", date)

	// Substituir {hour} - formato: HH
	hour := entry.Timestamp.Format("15")
	pattern = strings.ReplaceAll(pattern, "{hour}", hour)

	// Determinar source específico baseado no tipo (thread-safe label access)
	if entry.SourceType == "container" {
		// Para containers: {nomedocontainer} e {idcontainer}
		if containerName, exists := entry.GetLabel("container_name"); exists {
			pattern = strings.ReplaceAll(pattern, "{nomedocontainer}", sanitizeFilename(containerName))
		}
		if containerID, exists := entry.GetLabel("container_id"); exists {
			// Usar apenas os primeiros 12 caracteres do ID (como Docker faz)
			shortID := containerID
			if len(shortID) > 12 {
				shortID = shortID[:12]
			}
			pattern = strings.ReplaceAll(pattern, "{idcontainer}", shortID)
		}
	} else if entry.SourceType == "file" {
		// Para arquivos: {nomedoarquivomonitorado}
		if filePath, exists := entry.GetLabel("file_path"); exists {
			basename := filePath[strings.LastIndex(filePath, "/")+1:]
			// Remover extensão do arquivo para o placeholder
			baseName := strings.TrimSuffix(basename, filepath.Ext(basename))
			pattern = strings.ReplaceAll(pattern, "{nomedoarquivomonitorado}", sanitizeFilename(baseName))
		} else if fileName, exists := entry.GetLabel("file_name"); exists {
			baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
			pattern = strings.ReplaceAll(pattern, "{nomedoarquivomonitorado}", sanitizeFilename(baseName))
		}
	}

	// Remover placeholders não substituídos (evitar nomes estranhos)
	pattern = strings.ReplaceAll(pattern, "{nomedoarquivomonitorado}", "unknown-file")
	pattern = strings.ReplaceAll(pattern, "{nomedocontainer}", "unknown-container")
	pattern = strings.ReplaceAll(pattern, "{idcontainer}", "unknown-id")

	return filepath.Join(lfs.config.Directory, pattern)
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

	// C8: Verificar limite de file descriptors ANTES de abrir novo arquivo
	if lfs.openFileCount >= lfs.maxOpenFiles {
		// Fechar arquivo menos recentemente usado para liberar descriptor
		lfs.closeLeastRecentlyUsed()

		lfs.logger.WithFields(logrus.Fields{
			"open_files": lfs.openFileCount,
			"max_files":  lfs.maxOpenFiles,
		}).Debug("Hit max open files limit, closed LRU file")
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
		path:           filename,
		file:           file,
		writer:         file,
		currentSize:    info.Size(),
		lastWrite:      time.Now(),
		useCompression: lfs.config.Compress, // Use rotation compress setting for real-time compression too
		compressor:     lfs.compressor,
	}

	lfs.files[filename] = lf
	lfs.openFileCount++ // C8: Incrementar contador de file descriptors

	lfs.logger.WithFields(logrus.Fields{
		"filename":   filename,
		"open_files": lfs.openFileCount,
		"max_files":  lfs.maxOpenFiles,
	}).Debug("Created new log file")

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
			lfs.openFileCount-- // C8: Decrementar contador de file descriptors
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

	var dataToWrite []byte

	// Aplicar compressão se habilitada
	if lf.useCompression && lf.compressor != nil {
		compressionResult, err := lf.compressor.Compress([]byte(line), compression.AlgorithmAuto, "local_file")
		if err != nil {
			// Fallback para dados não comprimidos em caso de erro
			dataToWrite = []byte(line)
		} else {
			// Usar dados comprimidos
			dataToWrite = compressionResult.Data

			// Compression applied successfully - ratio info available in compressionResult
		}
	} else {
		dataToWrite = []byte(line)
	}

	// Escrever
	n, err := lf.writer.Write(dataToWrite)
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

	// Adicionar labels se existirem (thread-safe copy)
	labelsCopy := entry.CopyLabels()
	if len(labelsCopy) > 0 {
		output["labels"] = labelsCopy
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
	// Adicionar labels se habilitado (thread-safe copy)
	labelsCopy := entry.CopyLabels()
	if config.TextFormat.IncludeLabels && len(labelsCopy) > 0 {
		var labelPairs []string
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

// diskMonitorLoop monitora o espaço em disco e executa limpeza quando necessário
func (lfs *LocalFileSink) diskMonitorLoop() {
	interval, err := time.ParseDuration(lfs.config.DiskCheckInterval)
	if err != nil {
		interval = 60 * time.Second // fallback para 1 minuto
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-lfs.ctx.Done():
			return
		case <-ticker.C:
			lfs.checkDiskSpaceAndCleanup()
		}
	}
}

// checkDiskSpaceAndCleanup verifica espaço em disco e executa limpeza se necessário
func (lfs *LocalFileSink) checkDiskSpaceAndCleanup() {
	lfs.diskSpaceMutex.Lock()
	defer lfs.diskSpaceMutex.Unlock()

	// Verificar espaço disponível no disco
	var stat syscall.Statfs_t
	err := syscall.Statfs(lfs.config.Directory, &stat)
	if err != nil {
		lfs.logger.WithError(err).Error("Failed to check disk space")
		return
	}

	// Calcular uso do disco
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes
	usagePercent := float64(usedBytes) / float64(totalBytes) * 100

	// Calcular tamanho atual do diretório de logs
	dirSizeGB := lfs.getDirSizeGB(lfs.config.Directory)

	lfs.logger.WithFields(logrus.Fields{
		"disk_usage_percent": fmt.Sprintf("%.2f%%", usagePercent),
		"dir_size_gb":        fmt.Sprintf("%.2fGB", dirSizeGB),
		"max_allowed_gb":     lfs.config.MaxTotalDiskGB,
		"cleanup_threshold":  lfs.config.CleanupThresholdPercent,
	}).Debug("Disk space check")

	// Verificar se precisa de limpeza
	needsCleanup := false
	cleanupReason := ""

	if usagePercent >= lfs.config.CleanupThresholdPercent {
		needsCleanup = true
		cleanupReason = fmt.Sprintf("disk usage %.2f%% >= %.2f%%", usagePercent, lfs.config.CleanupThresholdPercent)
	} else if dirSizeGB >= lfs.config.MaxTotalDiskGB {
		needsCleanup = true
		cleanupReason = fmt.Sprintf("directory size %.2fGB >= %.2fGB", dirSizeGB, lfs.config.MaxTotalDiskGB)
	}

	if needsCleanup && lfs.config.EmergencyCleanupEnabled {
		lfs.logger.WithField("reason", cleanupReason).Warn("Emergency cleanup triggered")
		lfs.performEmergencyCleanup()
	}

	lfs.lastDiskCheck = time.Now()
}

// getDirSizeGB calcula o tamanho total do diretório em GB
func (lfs *LocalFileSink) getDirSizeGB(dirPath string) float64 {
	var totalSize int64

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Ignorar erros de arquivos individuais
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		lfs.logger.WithError(err).Error("Failed to calculate directory size")
		return 0
	}

	return float64(totalSize) / (1024 * 1024 * 1024) // Converter para GB
}

// performEmergencyCleanup executa limpeza de emergência
func (lfs *LocalFileSink) performEmergencyCleanup() {
	// Listar todos os arquivos de log
	files, err := filepath.Glob(filepath.Join(lfs.config.Directory, "*.log*"))
	if err != nil {
		lfs.logger.WithError(err).Error("Failed to list log files for cleanup")
		return
	}

	// Ordenar por data de modificação (mais antigos primeiro)
	type fileInfo struct {
		path    string
		modTime time.Time
		size    int64
	}

	var fileInfos []fileInfo
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{
			path:    file,
			modTime: stat.ModTime(),
			size:    stat.Size(),
		})
	}

	// Ordenar por data de modificação
	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].modTime.Before(fileInfos[j].modTime)
	})

	// Remover arquivos mais antigos até liberar espaço suficiente
	var removedFiles int
	var freedBytes int64

	for _, fileInfo := range fileInfos {
		// Não remover arquivos muito recentes (menos de 1 hora)
		if time.Since(fileInfo.modTime) < time.Hour {
			continue
		}

		err := os.Remove(fileInfo.path)
		if err != nil {
			lfs.logger.WithError(err).WithField("file", fileInfo.path).Error("Failed to remove old log file")
			continue
		}

		removedFiles++
		freedBytes += fileInfo.size

		lfs.logger.WithFields(logrus.Fields{
			"file":      filepath.Base(fileInfo.path),
			"size_mb":   float64(fileInfo.size) / (1024 * 1024),
			"mod_time":  fileInfo.modTime.Format(time.RFC3339),
		}).Info("Removed old log file during emergency cleanup")

		// Verificar se já liberamos espaço suficiente
		if removedFiles >= 10 { // Limite de segurança
			break
		}
	}

	lfs.logger.WithFields(logrus.Fields{
		"removed_files": removedFiles,
		"freed_mb":      float64(freedBytes) / (1024 * 1024),
	}).Info("Emergency cleanup completed")
}

// isDiskSpaceAvailable verifica se há espaço suficiente antes de escrever
// Refatorado para evitar deadlock: nunca faz unlock/relock manual dentro de defer
func (lfs *LocalFileSink) isDiskSpaceAvailable() bool {
	// FASE 1: Verificar se precisa atualizar (sem lock para leitura rápida)
	lfs.diskSpaceMutex.RLock()
	lastCheck := lfs.lastDiskCheck
	lfs.diskSpaceMutex.RUnlock()

	// FASE 2: Atualizar se necessário (SEM LOCK - evita deadlock)
	if time.Since(lastCheck) > 5*time.Minute {
		// Chamar sem lock - checkDiskSpaceAndCleanup adquire seu próprio lock
		lfs.checkDiskSpaceAndCleanup()
	}

	// FASE 3: Verificar espaço atual (operação rápida, ok ter lock)
	lfs.diskSpaceMutex.RLock()
	defer lfs.diskSpaceMutex.RUnlock()

	// Verificar espaço no sistema de arquivos (syscall rápido)
	var stat syscall.Statfs_t
	err := syscall.Statfs(lfs.config.Directory, &stat)
	if err != nil {
		return false
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usagePercent := float64(totalBytes-freeBytes) / float64(totalBytes) * 100

	// Bloquear escrita se uso > 95%
	if usagePercent > 95.0 {
		return false
	}

	// NOTA: Removida chamada a getDirSizeGB() aqui para evitar I/O lento com lock
	// A verificação de tamanho do diretório é feita periodicamente por checkDiskSpaceAndCleanup()

	return true
}

// canWriteSize verifica se pode escrever um tamanho específico sem exceder limites
// Refatorado para evitar I/O lento com lock ativo
func (lfs *LocalFileSink) canWriteSize(size int64) bool {
	// FASE 1: Calcular tamanho do diretório SEM LOCK (I/O pode ser lento)
	currentSizeGB := lfs.getDirSizeGB(lfs.config.Directory)

	// FASE 2: Verificações rápidas COM LOCK
	lfs.diskSpaceMutex.RLock()
	maxGB := lfs.config.MaxTotalDiskGB
	lfs.diskSpaceMutex.RUnlock()

	// Converter tamanho para GB
	sizeGB := float64(size) / (1024 * 1024 * 1024)

	// Verificar se escrita excederia o limite configurado
	if (currentSizeGB + sizeGB) >= maxGB {
		return false
	}

	// Verificar espaço livre no sistema de arquivos (syscall rápido)
	var stat syscall.Statfs_t
	err := syscall.Statfs(lfs.config.Directory, &stat)
	if err != nil {
		return false
	}

	freeBytes := stat.Bavail * uint64(stat.Bsize)

	// Garantir que há pelo menos 100MB livres após a escrita
	minFreeBytes := int64(100 * 1024 * 1024) // 100MB
	if int64(freeBytes)-size < minFreeBytes {
		return false
	}

	return true
}

// getQueuePressure retorna a pressão atual da fila (0.0 a 1.0)
func (lfs *LocalFileSink) getQueuePressure() float64 {
	utilization := lfs.GetQueueUtilization()

	// Calcular pressão baseada na utilização da fila e espaço em disco
	diskPressure := lfs.getDiskPressure()

	// Retornar a maior pressão entre fila e disco
	if diskPressure > utilization {
		return diskPressure
	}
	return utilization
}

// getDiskPressure retorna a pressão do disco (0.0 a 1.0)
func (lfs *LocalFileSink) getDiskPressure() float64 {
	currentSizeGB := lfs.getDirSizeGB(lfs.config.Directory)
	return currentSizeGB / lfs.config.MaxTotalDiskGB
}

// shouldDropEntryDueToBackpressure verifica se deve descartar entrada por backpressure
func (lfs *LocalFileSink) shouldDropEntryDueToBackpressure() bool {
	pressure := lfs.getQueuePressure()

	// Se pressão > 95%, começar a descartar entradas não críticas
	if pressure > 0.95 {
		return true
	}

	// Se pressão > 90%, descartar entradas de debug/trace
	if pressure > 0.90 {
		// Implementar lógica baseada em level se necessário
		return false // Por enquanto, não descartar
	}

	return false
}