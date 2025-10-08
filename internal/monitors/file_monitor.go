package monitors

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/positions"
	"ssw-logs-capture/pkg/types"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)


// FileMonitor monitora arquivos de log
type FileMonitor struct {
	config         types.FileConfig
	dispatcher     types.Dispatcher
	logger         *logrus.Logger
	taskManager    types.TaskManager
	positionManager *positions.PositionBufferManager

	watcher         *fsnotify.Watcher
	files           map[string]*monitoredFile
	specificFiles   map[string]bool // Arquivos específicos do pipeline (precedência)
	mutex           sync.RWMutex

	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool
}

// monitoredFile representa um arquivo sendo monitorado
type monitoredFile struct {
	path        string
	file        *os.File
	reader      *bufio.Reader
	position    int64
	labels      map[string]string
	lastModTime time.Time
	lastRead    time.Time
}

// NewFileMonitor cria um novo monitor de arquivos
func NewFileMonitor(config types.FileConfig, dispatcher types.Dispatcher, taskManager types.TaskManager, positionManager *positions.PositionBufferManager, logger *logrus.Logger) (*FileMonitor, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	fm := &FileMonitor{
		config:          config,
		dispatcher:      dispatcher,
		logger:          logger,
		taskManager:     taskManager,
		positionManager: positionManager,
		watcher:         watcher,
		files:           make(map[string]*monitoredFile),
		specificFiles:   make(map[string]bool),
		ctx:             ctx,
		cancel:          cancel,
	}

	return fm, nil
}

// Start inicia o monitor de arquivos
func (fm *FileMonitor) Start(ctx context.Context) error {
	if !fm.config.Enabled {
		fm.logger.Info("File monitor disabled")
		return nil
	}

	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	if fm.isRunning {
		return fmt.Errorf("file monitor already running")
	}

	fm.isRunning = true
	fm.logger.Info("Starting file monitor")

	// Iniciar position manager
	if err := fm.positionManager.Start(); err != nil {
		return fmt.Errorf("failed to start position manager: %w", err)
	}

	// Iniciar descoberta automática de arquivos em background após 2 segundos
	go func() {
		fm.logger.Info("Starting file discovery goroutine")
		time.Sleep(2 * time.Second)
		fm.logger.Info("Beginning automatic file discovery")
		if err := fm.discoverFiles(); err != nil {
			fm.logger.WithError(err).Warn("Failed to discover files during startup")
		} else {
			fm.logger.Info("Automatic file discovery completed successfully")
		}
	}()

	// Iniciar task de monitoramento principal
	if err := fm.taskManager.StartTask(ctx, "file_monitor", fm.monitorLoop); err != nil {
		return fmt.Errorf("failed to start file monitor task: %w", err)
	}

	return nil
}

// Stop para o monitor de arquivos
func (fm *FileMonitor) Stop() error {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	if !fm.isRunning {
		return nil
	}

	fm.logger.Info("Stopping file monitor")
	fm.isRunning = false

	// Cancelar contexto
	fm.cancel()

	// Parar tasks
	fm.taskManager.StopTask("file_monitor")

	// Parar position manager
	if fm.positionManager != nil {
		fm.positionManager.Stop()
	}

	// Fechar watcher
	if fm.watcher != nil {
		fm.watcher.Close()
	}

	// Fechar arquivos abertos
	for _, file := range fm.files {
		if file.file != nil {
			file.file.Close()
		}
	}

	return nil
}

// IsHealthy verifica se o monitor está saudável
func (fm *FileMonitor) IsHealthy() bool {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	return fm.isRunning
}

// GetStatus retorna o status do monitor
func (fm *FileMonitor) GetStatus() types.MonitorStatus {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	return types.MonitorStatus{
		Name:      "file_monitor",
		IsRunning: fm.isRunning,
		IsHealthy: fm.isRunning,
	}
}

// AddFile adiciona um arquivo para monitoramento
func (fm *FileMonitor) AddFile(filePath string, labels map[string]string) error {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	// Verificar se arquivo existe
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory", filePath)
	}

	// Verificar se já está sendo monitorado
	if _, exists := fm.files[filePath]; exists {
		return fmt.Errorf("file %s is already being monitored", filePath)
	}

	// Criar monitoredFile
	mf := &monitoredFile{
		path:        filePath,
		labels:      labels,
		lastModTime: info.ModTime(),
		lastRead:    time.Now(),
	}

	// Carregar posição salva se existir
	if fm.positionManager != nil {
		mf.position = fm.positionManager.GetFileOffset(filePath)
	}

	fm.files[filePath] = mf

	// Adicionar ao watcher
	if err := fm.watcher.Add(filePath); err != nil {
		delete(fm.files, filePath)
		return fmt.Errorf("failed to add file to watcher: %w", err)
	}

	// Atualizar métrica
	sourceID := fm.getSourceID(filePath)
	metrics.SetFileMonitored(filePath, "file", true)

	fm.logger.WithFields(logrus.Fields{
		"path":      filePath,
		"source_id": sourceID,
		"position":  mf.position,
	}).Info("File added to monitoring")

	return nil
}

// RemoveFile remove um arquivo do monitoramento
func (fm *FileMonitor) RemoveFile(filePath string) error {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	mf, exists := fm.files[filePath]
	if !exists {
		return fmt.Errorf("file %s is not being monitored", filePath)
	}

	// Remover do watcher
	fm.watcher.Remove(filePath)

	// Fechar arquivo se estiver aberto
	if mf.file != nil {
		mf.file.Close()
	}

	// Remover do mapa
	delete(fm.files, filePath)

	// Atualizar métrica
	metrics.SetFileMonitored(filePath, "file", false)

	fm.logger.WithField("path", filePath).Info("File removed from monitoring")
	return nil
}

// GetMonitoredFiles retorna lista de arquivos monitorados
func (fm *FileMonitor) GetMonitoredFiles() []map[string]string {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	result := make([]map[string]string, 0, len(fm.files))
	for path := range fm.files {
		result = append(result, map[string]string{
			"task_name": fm.getTaskName(path),
			"filepath":  path,
		})
	}

	return result
}

// monitorLoop loop principal de monitoramento
func (fm *FileMonitor) monitorLoop(ctx context.Context) error {
	ticker := time.NewTicker(fm.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-fm.watcher.Events:
			fm.handleFileEvent(event)
		case err := <-fm.watcher.Errors:
			fm.logger.WithError(err).Error("File watcher error")
			metrics.RecordError("file_monitor", "watcher_error")
		case <-ticker.C:
			fm.pollFiles()
		}

		// Heartbeat
		fm.taskManager.Heartbeat("file_monitor")
	}
}

// handleFileEvent processa eventos do file watcher
func (fm *FileMonitor) handleFileEvent(event fsnotify.Event) {
	if event.Op&fsnotify.Write == fsnotify.Write {
		fm.mutex.RLock()
		mf, exists := fm.files[event.Name]
		fm.mutex.RUnlock()

		if exists {
			fm.readFile(mf)
		}
	}
}

// pollFiles verifica arquivos periodicamente
func (fm *FileMonitor) pollFiles() {
	fm.mutex.RLock()
	files := make([]*monitoredFile, 0, len(fm.files))
	for _, mf := range fm.files {
		files = append(files, mf)
	}
	fileCount := len(fm.files)
	fm.mutex.RUnlock()

	// Se não há arquivos sendo monitorados, tentar descoberta automática
	if fileCount == 0 && len(fm.config.WatchDirectories) > 0 {
		fm.logger.Info("No files being monitored, triggering automatic file discovery")
		if err := fm.discoverFiles(); err != nil {
			fm.logger.WithError(err).Warn("Failed to discover files during periodic check")
		}
		return
	}

	for _, mf := range files {
		// Verificar se arquivo foi modificado
		info, err := os.Stat(mf.path)
		if err != nil {
			fm.logger.WithError(err).WithField("path", mf.path).Warn("Failed to stat file")
			continue
		}

		if info.ModTime().After(mf.lastModTime) {
			mf.lastModTime = info.ModTime()
			fm.readFile(mf)
		}

		// Health check - verificar se arquivo não foi lido há muito tempo
		timeSinceLastRead := time.Since(mf.lastRead)
		if timeSinceLastRead > 15*time.Minute {
			// Verificar se arquivo tem conteúdo novo ou foi modificado recentemente
			hasRecentChanges := info.ModTime().After(time.Now().Add(-10 * time.Minute))

			logLevel := logrus.DebugLevel
			message := "File has been quiet - normal for low activity files"

			if hasRecentChanges {
				logLevel = logrus.WarnLevel
				message = "File has recent changes but stream is not capturing them - possible file monitor disconnection"

				// Se detectou desconexão de arquivo, forçar reconexão
				if timeSinceLastRead > 20*time.Minute {
					fm.logger.WithFields(logrus.Fields{
						"path": mf.path,
						"minutes_since_read": int(timeSinceLastRead.Minutes()),
					}).Warn("Forcing file monitor reconnection due to prolonged disconnection")

					// Fechar arquivo atual e forçar reabertura
					if mf.file != nil {
						mf.file.Close()
						mf.file = nil
						mf.reader = nil
					}

					// Tentar ler o arquivo novamente
					fm.readFile(mf)

					fm.logger.WithField("path", mf.path).Info("File monitor reconnection completed")
					continue
				}
			}

			fm.logger.WithFields(logrus.Fields{
				"path":               mf.path,
				"minutes_since_read": int(timeSinceLastRead.Minutes()),
				"last_read":          mf.lastRead,
				"last_mod_time":      info.ModTime(),
				"has_recent_changes": hasRecentChanges,
			}).Log(logLevel, message)
		}
	}
}

// readFile lê novas linhas de um arquivo
func (fm *FileMonitor) readFile(mf *monitoredFile) {
	// Abrir arquivo se necessário
	if mf.file == nil {
		file, err := os.Open(mf.path)
		if err != nil {
			fm.logger.WithError(err).WithField("path", mf.path).Error("Failed to open file")
			metrics.RecordError("file_monitor", "file_open_error")
			return
		}

		// Garantir fechamento em caso de erro
		defer func() {
			if mf.file == nil && file != nil {
				file.Close()
			}
		}()

		mf.file = file
		mf.reader = bufio.NewReader(file)

		// Buscar posição salva
		if _, err := file.Seek(mf.position, 0); err != nil {
			fm.logger.WithError(err).WithField("path", mf.path).Warn("Failed to seek to saved position")
			mf.position = 0
			if _, seekErr := file.Seek(0, 0); seekErr != nil {
				fm.logger.WithError(seekErr).WithField("path", mf.path).Error("Failed to seek to beginning")
				// Fechar arquivo em caso de erro fatal
				mf.file.Close()
				mf.file = nil
				mf.reader = nil
				return
			}
		}
	}

	// Ler linhas
	startTime := time.Now()
	linesRead := 0

	for {
		line, err := mf.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Fim do arquivo
				break
			}
			fm.logger.WithError(err).WithField("path", mf.path).Error("Failed to read line")
			metrics.RecordError("file_monitor", "read_error")
			break
		}

		// Remover newline
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}

		// Processar linha com labels padrão
		sourceID := fm.getSourceID(mf.path)
		standardLabels := addStandardLabelsFile(mf.labels)
		if err := fm.dispatcher.Handle(fm.ctx, "file", sourceID, line, standardLabels); err != nil {
			fm.logger.WithError(err).WithField("path", mf.path).Error("Failed to dispatch log line")
			metrics.RecordError("file_monitor", "dispatch_error")
		}

		linesRead++
		mf.position += int64(len(line)) + 1 // +1 para o newline
	}

	mf.lastRead = time.Now()

	// Atualizar posição no position manager
	if fm.positionManager != nil && linesRead > 0 {
		// Get file info for inode and device
		info, err := os.Stat(mf.path)
		if err == nil {
			// Get file size
			fileSize := info.Size()
			lastModTime := info.ModTime()

			// Get inode and device (will be 0 on non-Unix systems, but that's ok)
			var inode, device uint64
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				inode = stat.Ino
				device = stat.Dev
			}

			bytesRead := int64(linesRead * 10) // Rough estimate, could be improved
			fm.positionManager.UpdateFilePosition(
				mf.path,
				mf.position,
				fileSize,
				lastModTime,
				inode,
				device,
				bytesRead,
				int64(linesRead),
			)
		}
	}

	// Métricas
	if linesRead > 0 {
		duration := time.Since(startTime)
		metrics.RecordProcessingDuration("file_monitor", "read_file", duration)
		metrics.RecordLogProcessed("file", fm.getSourceID(mf.path), "file_monitor")
	}
}


// getSourceID gera um ID único para o arquivo
func (fm *FileMonitor) getSourceID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", hash)[:12]
}

// getTaskName gera nome da task para o arquivo
func (fm *FileMonitor) getTaskName(path string) string {
	return "file_" + fm.getSourceID(path)
}

// discoverFiles descobre arquivos automaticamente baseado em watch_directories
func (fm *FileMonitor) discoverFiles() error {
	// Se tem pipeline configurado, processar ele primeiro
	if fm.config.PipelineConfig != nil {
		fm.logger.Info("Processing file pipeline configuration")

		// Processar arquivos específicos do pipeline
		if err := fm.processSpecificFiles(); err != nil {
			fm.logger.WithError(err).Warn("Failed to process specific files from pipeline")
		}

		// Processar diretórios do pipeline
		if err := fm.processPipelineDirectories(); err != nil {
			fm.logger.WithError(err).Warn("Failed to process directories from pipeline")
		}
	} else {
		// Usar configuração default de files_config
		fm.logger.Info("No pipeline configured, using default directories from files_config")
		for _, directory := range fm.config.WatchDirectories {
			fm.logger.WithField("directory", directory).Info("Scanning directory for log files")

			if err := fm.scanDirectory(directory); err != nil {
				fm.logger.WithError(err).WithField("directory", directory).Error("Failed to scan directory")
				continue
			}
		}
	}

	fm.logger.WithField("monitored_files", len(fm.files)).Info("File discovery completed")
	return nil
}

// processSpecificFiles processa arquivos específicos do pipeline
func (fm *FileMonitor) processSpecificFiles() error {
	if fm.config.PipelineConfig == nil {
		return nil
	}

	for _, fileEntry := range fm.config.PipelineConfig.Files {
		if !fileEntry.Enabled {
			fm.logger.WithField("file", fileEntry.Path).Debug("Skipping disabled file from pipeline")
			continue
		}

		// Verificar se arquivo existe
		if _, err := os.Stat(fileEntry.Path); os.IsNotExist(err) {
			fm.logger.WithField("file", fileEntry.Path).Warn("File configured in pipeline does not exist, skipping")
			continue
		}

		// Marcar como arquivo específico (tem precedência)
		fm.specificFiles[fileEntry.Path] = true

		// Adicionar arquivo para monitoramento
		if err := fm.AddFile(fileEntry.Path, fileEntry.Labels); err != nil {
			fm.logger.WithError(err).WithField("file", fileEntry.Path).Error("Failed to add specific file from pipeline")
		} else {
			fm.logger.WithFields(logrus.Fields{
				"file":   fileEntry.Path,
				"labels": fileEntry.Labels,
			}).Info("Added specific file from pipeline")
		}
	}

	return nil
}

// processPipelineDirectories processa diretórios do pipeline
func (fm *FileMonitor) processPipelineDirectories() error {
	if fm.config.PipelineConfig == nil {
		return nil
	}

	for _, dirEntry := range fm.config.PipelineConfig.Directories {
		if !dirEntry.Enabled {
			fm.logger.WithField("directory", dirEntry.Path).Debug("Skipping disabled directory from pipeline")
			continue
		}

		fm.logger.WithField("directory", dirEntry.Path).Info("Scanning pipeline directory for log files")

		if err := fm.scanPipelineDirectory(dirEntry); err != nil {
			fm.logger.WithError(err).WithField("directory", dirEntry.Path).Error("Failed to scan pipeline directory")
			continue
		}
	}

	return nil
}

// scanDirectory escaneia um diretório procurando por arquivos que correspondem aos padrões
func (fm *FileMonitor) scanDirectory(directory string) error {
	return filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignorar erros de permissão e continuar
			if os.IsPermission(err) {
				fm.logger.WithField("path", path).Debug("Permission denied, skipping")
				return nil
			}
			return err
		}

		// Pular diretórios
		if info.IsDir() {
			// Verificar se o diretório está na lista de exclusão
			if fm.matchesExcludeDirectories(path) {
				fm.logger.WithField("path", path).Debug("Skipping excluded directory")
				return filepath.SkipDir
			}

			// Se não for recursivo, pular subdiretórios
			if !fm.config.Recursive && path != directory {
				return filepath.SkipDir
			}
			return nil
		}

		// Verificar se é arquivo específico do pipeline (tem precedência)
		if fm.specificFiles[path] {
			fm.logger.WithField("path", path).Debug("Skipping file - already configured as specific file in pipeline")
			return nil
		}

		// Verificar se o arquivo corresponde aos padrões de inclusão
		if fm.matchesIncludePatterns(path) && !fm.matchesExcludePatterns(path) {
			// Verificar se o arquivo já está sendo monitorado
			fm.mutex.RLock()
			_, exists := fm.files[path]
			fm.mutex.RUnlock()

			if !exists {
				labels := fm.generateLabelsForFile(path)
				if err := fm.AddFile(path, labels); err != nil {
					fm.logger.WithError(err).WithField("path", path).Warn("Failed to add discovered file")
				} else {
					fm.logger.WithField("path", path).Info("Auto-discovered and added file for monitoring")
				}
			}
		}

		return nil
	})
}

// scanPipelineDirectory escaneia um diretório do pipeline
func (fm *FileMonitor) scanPipelineDirectory(dirEntry types.FilePipelineDirEntry) error {
	return filepath.Walk(dirEntry.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignorar erros de permissão
			if os.IsPermission(err) {
				fm.logger.WithField("path", path).Debug("Permission denied, skipping")
				return nil
			}
			return err
		}

		// Pular diretórios
		if info.IsDir() {
			// Verificar exclusões específicas do diretório
			if fm.matchesPipelineExcludeDirectories(path, dirEntry.ExcludeDirectories) {
				fm.logger.WithField("path", path).Debug("Skipping excluded directory from pipeline")
				return filepath.SkipDir
			}

			// Se não for recursivo, pular subdiretórios
			if !dirEntry.Recursive && path != dirEntry.Path {
				return filepath.SkipDir
			}
			return nil
		}

		// Verificar se é arquivo específico do pipeline (tem precedência)
		if fm.specificFiles[path] {
			fm.logger.WithField("path", path).Debug("Skipping file - already configured as specific file in pipeline")
			return nil
		}

		// Verificar padrões do diretório
		if fm.matchesPipelinePatterns(path, dirEntry.Patterns) &&
		   !fm.matchesPipelineExcludePatterns(path, dirEntry.ExcludePatterns) {

			// Verificar se já está sendo monitorado
			fm.mutex.RLock()
			_, exists := fm.files[path]
			fm.mutex.RUnlock()

			if !exists {
				// Usar labels default do diretório
				labels := make(map[string]string)
				for k, v := range dirEntry.DefaultLabels {
					labels[k] = v
				}
				// Adicionar informações do arquivo
				labels["file_path"] = path
				labels["file_name"] = filepath.Base(path)

				if err := fm.AddFile(path, labels); err != nil {
					fm.logger.WithError(err).WithField("path", path).Warn("Failed to add file from pipeline directory")
				} else {
					fm.logger.WithFields(logrus.Fields{
						"path":   path,
						"labels": labels,
					}).Info("Added file from pipeline directory")
				}
			}
		}

		return nil
	})
}

// matchesIncludePatterns verifica se o arquivo corresponde aos padrões de inclusão
func (fm *FileMonitor) matchesIncludePatterns(filePath string) bool {
	fileName := filepath.Base(filePath)

	for _, pattern := range fm.config.IncludePatterns {
		matched, err := filepath.Match(pattern, fileName)
		if err == nil && matched {
			return true
		}

		// Para padrões sem wildcards, verificar match exato do nome
		if pattern == fileName {
			return true
		}
	}

	return false
}

// matchesExcludePatterns verifica se o arquivo corresponde aos padrões de exclusão
func (fm *FileMonitor) matchesExcludePatterns(filePath string) bool {
	fileName := filepath.Base(filePath)

	for _, pattern := range fm.config.ExcludePatterns {
		matched, err := filepath.Match(pattern, fileName)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// matchesExcludeDirectories verifica se o diretório está na lista de exclusão
func (fm *FileMonitor) matchesExcludeDirectories(dirPath string) bool {
	// Normalizar o caminho removendo trailing slashes
	dirPath = strings.TrimSuffix(dirPath, "/")

	for _, excludeDir := range fm.config.ExcludeDirectories {
		// Normalizar o padrão de exclusão
		excludeDir = strings.TrimSuffix(excludeDir, "/")

		// Remover wildcard no final se houver
		excludePattern := strings.TrimSuffix(excludeDir, "/*")

		// Verificar match exato
		if dirPath == excludePattern {
			return true
		}

		// Verificar se é um subdiretório do padrão excluído
		if strings.HasPrefix(dirPath, excludePattern+"/") {
			return true
		}
	}

	return false
}

// matchesPipelinePatterns verifica se arquivo corresponde aos padrões do pipeline
func (fm *FileMonitor) matchesPipelinePatterns(filePath string, patterns []string) bool {
	fileName := filepath.Base(filePath)

	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, fileName)
		if err == nil && matched {
			return true
		}

		// Match exato para padrões sem wildcards
		if pattern == fileName {
			return true
		}
	}

	return false
}

// matchesPipelineExcludePatterns verifica exclusões do pipeline
func (fm *FileMonitor) matchesPipelineExcludePatterns(filePath string, patterns []string) bool {
	fileName := filepath.Base(filePath)

	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, fileName)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// matchesPipelineExcludeDirectories verifica exclusões de diretórios do pipeline
func (fm *FileMonitor) matchesPipelineExcludeDirectories(dirPath string, excludeDirs []string) bool {
	dirPath = strings.TrimSuffix(dirPath, "/")

	for _, excludeDir := range excludeDirs {
		excludeDir = strings.TrimSuffix(excludeDir, "/")
		excludePattern := strings.TrimSuffix(excludeDir, "/*")

		// Match exato
		if dirPath == excludePattern {
			return true
		}

		// Match de subdiretório
		if strings.HasSuffix(dirPath, "/"+excludePattern) {
			return true
		}

		// Match se contém o padrão
		if strings.Contains(dirPath, "/"+excludePattern+"/") {
			return true
		}
	}

	return false
}

// generateLabelsForFile gera labels automáticos para um arquivo descoberto
func (fm *FileMonitor) generateLabelsForFile(filePath string) map[string]string {
	labels := make(map[string]string)
	fileName := filepath.Base(filePath)

	// Labels padrão
	labels["source"] = "file"
	labels["file_path"] = filePath
	labels["file_name"] = fileName

	// Labels específicos baseado no nome do arquivo
	switch {
	case strings.Contains(fileName, "syslog"):
		labels["log_type"] = "syslog"
		labels["service"] = "system"
	case strings.Contains(fileName, "kern") || strings.Contains(fileName, "dmesg"):
		labels["log_type"] = "kernel"
		labels["service"] = "kernel"
	case strings.Contains(fileName, "auth"):
		labels["log_type"] = "auth"
		labels["service"] = "security"
	case strings.Contains(fileName, "nginx") || strings.Contains(fileName, "apache"):
		labels["log_type"] = "access"
		labels["service"] = "web"
	case strings.Contains(fileName, "mysql") || strings.Contains(fileName, "postgres"):
		labels["log_type"] = "database"
		labels["service"] = "database"
	default:
		labels["log_type"] = "application"
		labels["service"] = "application"
	}

	return labels
}

// getHostIPFile obtém o IP do host (versão para file monitor)
func getHostIPFile() string {
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

// getHostnameFile obtém o nome do host (versão para file monitor)
func getHostnameFile() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// addStandardLabelsFile adiciona labels padrão para logs de arquivos
func addStandardLabelsFile(labels map[string]string) map[string]string {
	// Criar um novo mapa copiando as labels existentes
	result := make(map[string]string)

	// Copiar labels existentes primeiro
	for k, v := range labels {
		result[k] = v
	}

	// Labels padrão obrigatórias (sobrescrevem as existentes)
	result["service"] = "log_capturer"
	result["source"] = "file"
	result["instance"] = getHostIPFile()
	result["instance_name"] = getHostnameFile()

	return result
}