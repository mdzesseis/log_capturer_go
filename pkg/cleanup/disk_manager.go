package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"ssw-logs-capture/internal/metrics"

	"github.com/sirupsen/logrus"
)

// DiskSpaceManager gerencia espaço em disco e limpeza automática
type DiskSpaceManager struct {
	config Config
	logger *logrus.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração do gerenciador de espaço
type Config struct {
	// Diretórios a serem monitorados
	Directories []DirectoryConfig `yaml:"directories"`

	// Intervalo de verificação
	CheckInterval time.Duration `yaml:"check_interval"`

	// Limite crítico de espaço livre (%)
	CriticalSpaceThreshold float64 `yaml:"critical_space_threshold"`

	// Limite de warning de espaço livre (%)
	WarningSpaceThreshold float64 `yaml:"warning_space_threshold"`
}

// DirectoryConfig configuração de diretório
type DirectoryConfig struct {
	Path                string        `yaml:"path"`
	MaxSizeMB          int64         `yaml:"max_size_mb"`
	RetentionDays      int           `yaml:"retention_days"`
	FilePatterns       []string      `yaml:"file_patterns"`
	MaxFiles           int           `yaml:"max_files"`
	CleanupAgeSeconds  int           `yaml:"cleanup_age_seconds"`
}

// FileInfo informações de arquivo para cleanup
type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// NewDiskSpaceManager cria novo gerenciador
func NewDiskSpaceManager(config Config, logger *logrus.Logger) *DiskSpaceManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &DiskSpaceManager{
		config: config,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start inicia o monitoramento
func (dsm *DiskSpaceManager) Start() error {
	dsm.logger.Info("Starting disk space manager")

	go dsm.monitorLoop()
	return nil
}

// Stop para o monitoramento
func (dsm *DiskSpaceManager) Stop() error {
	dsm.logger.Info("Stopping disk space manager")
	dsm.cancel()
	return nil
}

// monitorLoop loop principal de monitoramento
func (dsm *DiskSpaceManager) monitorLoop() {
	ticker := time.NewTicker(dsm.config.CheckInterval)
	defer ticker.Stop()

	// Execução inicial
	dsm.performCleanup()

	for {
		select {
		case <-dsm.ctx.Done():
			return
		case <-ticker.C:
			dsm.performCleanup()
		}
	}
}

// performCleanup executa limpeza em todos os diretórios
func (dsm *DiskSpaceManager) performCleanup() {
	for _, dirConfig := range dsm.config.Directories {
		if err := dsm.cleanupDirectory(dirConfig); err != nil {
			dsm.logger.WithError(err).WithField("directory", dirConfig.Path).
				Error("Failed to cleanup directory")
		}
	}

	// Verificar espaço em disco geral
	dsm.checkDiskSpace()

	// Atualizar métricas de uso de disco
	dsm.updateDiskMetrics()
}

// updateDiskMetrics atualiza as métricas de uso de disco
func (dsm *DiskSpaceManager) updateDiskMetrics() {
	for _, dirConfig := range dsm.config.Directories {
		usage, err := dsm.getDiskUsage(dirConfig.Path)
		if err != nil {
			dsm.logger.WithError(err).WithField("path", dirConfig.Path).
				Warn("Failed to get disk usage for metrics")
			continue
		}

		// Extract device name from path (simplified - just use path as device identifier)
		device := filepath.Base(dirConfig.Path)

		// Update Prometheus metrics
		metrics.DiskUsageBytes.WithLabelValues(dirConfig.Path, device).Set(float64(usage.Used))
	}
}

// cleanupDirectory limpa um diretório específico
func (dsm *DiskSpaceManager) cleanupDirectory(config DirectoryConfig) error {
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		// Diretório não existe, criar se necessário
		if err := os.MkdirAll(config.Path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", config.Path, err)
		}
		return nil
	}

	// 1. Limpeza por idade
	if err := dsm.cleanupByAge(config); err != nil {
		dsm.logger.WithError(err).Warn("Failed age-based cleanup")
	}

	// 2. Limpeza por tamanho total
	if err := dsm.cleanupBySize(config); err != nil {
		dsm.logger.WithError(err).Warn("Failed size-based cleanup")
	}

	// 3. Limpeza por número de arquivos
	if err := dsm.cleanupByCount(config); err != nil {
		dsm.logger.WithError(err).Warn("Failed count-based cleanup")
	}

	return nil
}

// cleanupByAge remove arquivos antigos
func (dsm *DiskSpaceManager) cleanupByAge(config DirectoryConfig) error {
	if config.RetentionDays <= 0 && config.CleanupAgeSeconds <= 0 {
		return nil // Sem limpeza por idade configurada
	}

	cutoffTime := time.Now()
	if config.RetentionDays > 0 {
		cutoffTime = cutoffTime.AddDate(0, 0, -config.RetentionDays)
	} else if config.CleanupAgeSeconds > 0 {
		cutoffTime = cutoffTime.Add(-time.Duration(config.CleanupAgeSeconds) * time.Second)
	}

	files, err := dsm.findMatchingFiles(config)
	if err != nil {
		return err
	}

	removedCount := 0
	var removedSize int64

	for _, file := range files {
		if file.ModTime.Before(cutoffTime) {
			if err := os.Remove(file.Path); err != nil {
				dsm.logger.WithError(err).WithField("file", file.Path).
					Warn("Failed to remove old file")
				continue
			}
			removedCount++
			removedSize += file.Size
		}
	}

	if removedCount > 0 {
		dsm.logger.WithFields(logrus.Fields{
			"directory":    config.Path,
			"files_removed": removedCount,
			"bytes_freed":  removedSize,
			"cutoff_time":  cutoffTime,
		}).Info("Age-based cleanup completed")
	}

	return nil
}

// cleanupBySize remove arquivos mais antigos até atingir limite de tamanho
func (dsm *DiskSpaceManager) cleanupBySize(config DirectoryConfig) error {
	if config.MaxSizeMB <= 0 {
		return nil // Sem limite de tamanho configurado
	}

	files, err := dsm.findMatchingFiles(config)
	if err != nil {
		return err
	}

	// Calcular tamanho total
	var totalSize int64
	for _, file := range files {
		totalSize += file.Size
	}

	maxBytes := config.MaxSizeMB * 1024 * 1024
	if totalSize <= maxBytes {
		return nil // Dentro do limite
	}

	// Ordenar por data de modificação (mais antigos primeiro)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	removedCount := 0
	var removedSize int64
	currentSize := totalSize

	for _, file := range files {
		if currentSize <= maxBytes {
			break
		}

		if err := os.Remove(file.Path); err != nil {
			dsm.logger.WithError(err).WithField("file", file.Path).
				Warn("Failed to remove file for size cleanup")
			continue
		}

		removedCount++
		removedSize += file.Size
		currentSize -= file.Size
	}

	if removedCount > 0 {
		dsm.logger.WithFields(logrus.Fields{
			"directory":     config.Path,
			"files_removed":  removedCount,
			"bytes_freed":   removedSize,
			"max_size_mb":   config.MaxSizeMB,
			"final_size_mb": currentSize / (1024 * 1024),
		}).Info("Size-based cleanup completed")
	}

	return nil
}

// cleanupByCount remove arquivos mais antigos se exceder número máximo
func (dsm *DiskSpaceManager) cleanupByCount(config DirectoryConfig) error {
	if config.MaxFiles <= 0 {
		return nil // Sem limite de arquivos configurado
	}

	files, err := dsm.findMatchingFiles(config)
	if err != nil {
		return err
	}

	if len(files) <= config.MaxFiles {
		return nil // Dentro do limite
	}

	// Ordenar por data de modificação (mais antigos primeiro)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	// Remover arquivos excedentes
	filesToRemove := len(files) - config.MaxFiles
	removedCount := 0
	var removedSize int64

	for i := 0; i < filesToRemove; i++ {
		file := files[i]
		if err := os.Remove(file.Path); err != nil {
			dsm.logger.WithError(err).WithField("file", file.Path).
				Warn("Failed to remove file for count cleanup")
			continue
		}

		removedCount++
		removedSize += file.Size
	}

	if removedCount > 0 {
		dsm.logger.WithFields(logrus.Fields{
			"directory":     config.Path,
			"files_removed":  removedCount,
			"bytes_freed":   removedSize,
			"max_files":     config.MaxFiles,
			"final_count":   len(files) - removedCount,
		}).Info("Count-based cleanup completed")
	}

	return nil
}

// findMatchingFiles encontra arquivos que correspondem aos padrões
func (dsm *DiskSpaceManager) findMatchingFiles(config DirectoryConfig) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.Walk(config.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continuar mesmo com erros em arquivos específicos
		}

		if info.IsDir() {
			return nil // Pular diretórios
		}

		// Verificar se corresponde aos padrões
		if len(config.FilePatterns) > 0 {
			matched := false
			for _, pattern := range config.FilePatterns {
				if matched, _ := filepath.Match(pattern, info.Name()); matched {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		files = append(files, FileInfo{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})

		return nil
	})

	return files, err
}

// checkDiskSpace verifica espaço livre no disco
func (dsm *DiskSpaceManager) checkDiskSpace() {
	for _, dirConfig := range dsm.config.Directories {
		usage, err := dsm.getDiskUsage(dirConfig.Path)
		if err != nil {
			dsm.logger.WithError(err).WithField("directory", dirConfig.Path).
				Warn("Failed to get disk usage")
			continue
		}

		freePercent := float64(usage.Free) / float64(usage.Total) * 100

		fields := logrus.Fields{
			"directory":     dirConfig.Path,
			"free_percent":  freePercent,
			"free_mb":      usage.Free / (1024 * 1024),
			"total_mb":     usage.Total / (1024 * 1024),
		}

		if freePercent < dsm.config.CriticalSpaceThreshold {
			dsm.logger.WithFields(fields).Error("CRITICAL: Disk space very low")
		} else if freePercent < dsm.config.WarningSpaceThreshold {
			dsm.logger.WithFields(fields).Warn("WARNING: Disk space low")
		}
	}
}

// DiskUsage informações de uso do disco
type DiskUsage struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// getDiskUsage obtém informações de uso do disco
func (dsm *DiskSpaceManager) getDiskUsage(path string) (*DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free

	return &DiskUsage{
		Total: total,
		Free:  free,
		Used:  used,
	}, nil
}

// GetStatus retorna status atual do gerenciador
func (dsm *DiskSpaceManager) GetStatus() map[string]interface{} {
	status := make(map[string]interface{})

	for _, dirConfig := range dsm.config.Directories {
		dirStatus := make(map[string]interface{})

		// Informações de uso do disco
		if usage, err := dsm.getDiskUsage(dirConfig.Path); err == nil {
			dirStatus["disk_usage"] = map[string]interface{}{
				"total_mb":     usage.Total / (1024 * 1024),
				"free_mb":      usage.Free / (1024 * 1024),
				"used_mb":      usage.Used / (1024 * 1024),
				"free_percent": float64(usage.Free) / float64(usage.Total) * 100,
			}
		}

		// Contagem de arquivos
		if files, err := dsm.findMatchingFiles(dirConfig); err == nil {
			var totalSize int64
			for _, file := range files {
				totalSize += file.Size
			}

			dirStatus["files"] = map[string]interface{}{
				"count":    len(files),
				"total_mb": totalSize / (1024 * 1024),
			}
		}

		status[dirConfig.Path] = dirStatus
	}

	return status
}