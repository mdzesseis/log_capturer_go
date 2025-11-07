package positions

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"ssw-logs-capture/internal/metrics"
)

type BufferConfig struct {
	FlushInterval       time.Duration `yaml:"flush_interval" json:"flush_interval"`
	MaxMemoryBuffer     int          `yaml:"max_memory_buffer" json:"max_memory_buffer"`
	MaxMemoryPositions  int          `yaml:"max_memory_positions" json:"max_memory_positions"` // Limite máximo de posições em memória
	ForceFlushOnExit    bool         `yaml:"force_flush_on_exit" json:"force_flush_on_exit"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	MaxPositionAge      time.Duration `yaml:"max_position_age" json:"max_position_age"`
	FlushBatchSize      int          `yaml:"flush_batch_size" json:"flush_batch_size"`      // Number of updates before forcing flush
	AdaptiveFlushEnabled bool         `yaml:"adaptive_flush_enabled" json:"adaptive_flush_enabled"` // Enable adaptive flush
}

type PositionBufferManager struct {
	containerManager *ContainerPositionManager
	fileManager      *FilePositionManager
	config           *BufferConfig
	logger           *logrus.Logger

	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	flushTicker   *time.Ticker
	cleanupTicker *time.Ticker
	tickerMutex   sync.RWMutex // Protege acesso aos tickers

	// Adaptive flush state
	updatesSinceFlush int
	lastFlushTime     time.Time
	flushMutex        sync.Mutex // Protects flush-related fields

	stats struct {
		mu                    sync.RWMutex
		totalFlushes         int64
		totalCleanups        int64
		lastFlushDuration    time.Duration
		lastCleanupDuration  time.Duration
		totalUpdates         int64
		totalErrors          int64
		memoryLimitReached   int64
		positionsDropped     int64
		flushTriggerUpdates  int64 // Flushes triggered by update count
		flushTriggerTimeout  int64 // Flushes triggered by timeout
		flushTriggerShutdown int64 // Flushes triggered by shutdown
	}
}

func NewPositionBufferManager(
	containerManager *ContainerPositionManager,
	fileManager *FilePositionManager,
	config *BufferConfig,
	logger *logrus.Logger,
) *PositionBufferManager {
	ctx, cancel := context.WithCancel(context.Background())

	if config == nil {
		config = &BufferConfig{
			FlushInterval:        5 * time.Second,  // Reduced from 30s to 5s
			MaxMemoryBuffer:      1000,
			MaxMemoryPositions:   5000, // Limite de posições em memória para evitar sobrecarga
			ForceFlushOnExit:     true,
			CleanupInterval:      5 * time.Minute,
			MaxPositionAge:       24 * time.Hour,
			FlushBatchSize:       100,  // Flush after 100 updates
			AdaptiveFlushEnabled: true, // Enable adaptive flush by default
		}
	}

	// Set defaults if not provided
	if config.FlushBatchSize == 0 {
		config.FlushBatchSize = 100
	}

	pbm := &PositionBufferManager{
		containerManager: containerManager,
		fileManager:      fileManager,
		config:           config,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		lastFlushTime:    time.Now(),
	}

	return pbm
}

func (pbm *PositionBufferManager) Start() error {
	pbm.logger.Info("Starting position buffer manager", map[string]interface{}{
		"flush_interval":   pbm.config.FlushInterval.String(),
		"cleanup_interval": pbm.config.CleanupInterval.String(),
		"max_position_age": pbm.config.MaxPositionAge.String(),
	})

	// Load existing positions
	if err := pbm.containerManager.LoadPositions(); err != nil {
		pbm.logger.Error("Failed to load container positions", map[string]interface{}{
			"error": err.Error(),
		})
	}

	if err := pbm.fileManager.LoadPositions(); err != nil {
		pbm.logger.Error("Failed to load file positions", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Start periodic flush - create tickers before starting goroutines
	pbm.tickerMutex.Lock()
	pbm.flushTicker = time.NewTicker(pbm.config.FlushInterval)
	pbm.cleanupTicker = time.NewTicker(pbm.config.CleanupInterval)
	pbm.tickerMutex.Unlock()

	// Wait a moment to ensure tickers are fully initialized
	time.Sleep(1 * time.Millisecond)

	pbm.wg.Add(2)
	go pbm.flushLoop()
	go pbm.cleanupLoop()

	return nil
}

func (pbm *PositionBufferManager) Stop() error {
	pbm.logger.Info("Stopping position buffer manager", nil)

	// Cancel context to stop background goroutines
	pbm.cancel()

	// Stop tickers
	if pbm.flushTicker != nil {
		pbm.flushTicker.Stop()
	}
	if pbm.cleanupTicker != nil {
		pbm.cleanupTicker.Stop()
	}

	// Wait for goroutines to finish
	pbm.wg.Wait()

	// Force final flush if configured
	if pbm.config.ForceFlushOnExit {
		if err := pbm.flushWithTrigger("shutdown"); err != nil {
			pbm.logger.Error("Failed final flush on exit", map[string]interface{}{
				"error": err.Error(),
			})
			return err
		}
	}

	pbm.logger.Info("Position buffer manager stopped", nil)
	return nil
}

func (pbm *PositionBufferManager) Flush() error {
	return pbm.flushWithTrigger("manual")
}

func (pbm *PositionBufferManager) flushWithTrigger(triggerType string) error {
	start := time.Now()

	var errors []error

	// Flush container positions if dirty
	if pbm.containerManager.IsDirty() {
		if err := pbm.containerManager.SavePositions(); err != nil {
			errors = append(errors, err)
			pbm.logger.Error("Failed to flush container positions", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// Flush file positions if dirty
	if pbm.fileManager.IsDirty() {
		if err := pbm.fileManager.SavePositions(); err != nil {
			errors = append(errors, err)
			pbm.logger.Error("Failed to flush file positions", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	duration := time.Since(start)

	// Reset adaptive flush state
	pbm.flushMutex.Lock()
	pbm.updatesSinceFlush = 0
	pbm.lastFlushTime = time.Now()
	pbm.flushMutex.Unlock()

	// Update statistics
	pbm.stats.mu.Lock()
	pbm.stats.totalFlushes++
	pbm.stats.lastFlushDuration = duration
	if len(errors) > 0 {
		pbm.stats.totalErrors++
	}
	// Track flush trigger type
	switch triggerType {
	case "updates":
		pbm.stats.flushTriggerUpdates++
		metrics.RecordPositionFlushTrigger("updates")
	case "timeout":
		pbm.stats.flushTriggerTimeout++
		metrics.RecordPositionFlushTrigger("timeout")
	case "shutdown":
		pbm.stats.flushTriggerShutdown++
		metrics.RecordPositionFlushTrigger("shutdown")
	}
	pbm.stats.mu.Unlock()

	if len(errors) == 0 {
		pbm.logger.Debug("Successfully flushed positions", map[string]interface{}{
			"duration_ms":  duration.Milliseconds(),
			"trigger_type": triggerType,
		})
	}

	if len(errors) > 0 {
		return errors[0] // Return first error
	}

	return nil
}

// maybeFlush checks if adaptive flush should trigger
func (pbm *PositionBufferManager) maybeFlush() {
	if !pbm.config.AdaptiveFlushEnabled {
		return
	}

	pbm.flushMutex.Lock()
	defer pbm.flushMutex.Unlock()

	// Check if we should flush based on update count
	if pbm.updatesSinceFlush >= pbm.config.FlushBatchSize {
		pbm.flushMutex.Unlock() // Release lock before flush
		if err := pbm.flushWithTrigger("updates"); err != nil {
			pbm.logger.Error("Adaptive flush (updates) failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
		pbm.flushMutex.Lock() // Re-acquire for defer
		return
	}

	// Check if we should flush based on time elapsed
	if time.Since(pbm.lastFlushTime) >= pbm.config.FlushInterval {
		pbm.flushMutex.Unlock() // Release lock before flush
		if err := pbm.flushWithTrigger("timeout"); err != nil {
			pbm.logger.Error("Adaptive flush (timeout) failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
		pbm.flushMutex.Lock() // Re-acquire for defer
	}
}

func (pbm *PositionBufferManager) flushLoop() {
	defer pbm.wg.Done()

	for {
		select {
		case <-pbm.ctx.Done():
			return
		case <-func() <-chan time.Time {
			pbm.tickerMutex.RLock()
			defer pbm.tickerMutex.RUnlock()
			if pbm.flushTicker != nil {
				return pbm.flushTicker.C
			}
			return make(chan time.Time) // Never sends if ticker is nil
		}():
			if err := pbm.Flush(); err != nil {
				pbm.logger.Error("Periodic flush failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}
	}
}

func (pbm *PositionBufferManager) cleanupLoop() {
	defer pbm.wg.Done()

	for {
		select {
		case <-pbm.ctx.Done():
			return
		case <-func() <-chan time.Time {
			pbm.tickerMutex.RLock()
			defer pbm.tickerMutex.RUnlock()
			if pbm.cleanupTicker != nil {
				return pbm.cleanupTicker.C
			}
			return make(chan time.Time) // Never sends if ticker is nil
		}():
			pbm.performCleanup()
		}
	}
}

func (pbm *PositionBufferManager) performCleanup() {
	start := time.Now()

	pbm.logger.Debug("Starting position cleanup", map[string]interface{}{
		"max_age": pbm.config.MaxPositionAge.String(),
	})

	// Cleanup old container positions
	pbm.containerManager.CleanupOldPositions(pbm.config.MaxPositionAge)

	// Cleanup old file positions
	pbm.fileManager.CleanupOldPositions(pbm.config.MaxPositionAge)

	duration := time.Since(start)

	pbm.stats.mu.Lock()
	pbm.stats.totalCleanups++
	pbm.stats.lastCleanupDuration = duration
	pbm.stats.mu.Unlock()

	pbm.logger.Debug("Completed position cleanup", map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
	})
}

func (pbm *PositionBufferManager) UpdateContainerPosition(containerID string, since time.Time, logCount int64, bytesRead int64) {
	// Verificar limite de memória antes de atualizar
	if !pbm.checkMemoryLimits() {
		pbm.stats.mu.Lock()
		pbm.stats.memoryLimitReached++
		pbm.stats.mu.Unlock()

		pbm.logger.Warn("Memory limit reached, attempting emergency flush", map[string]interface{}{
			"container_positions": pbm.containerManager.GetStats()["total_positions"],
			"file_positions":      pbm.fileManager.GetStats()["total_positions"],
			"max_positions":       pbm.config.MaxMemoryPositions,
		})

		// Tentar flush de emergência
		if err := pbm.Flush(); err != nil {
			pbm.logger.Error("Emergency flush failed, dropping position update", map[string]interface{}{
				"error":        err.Error(),
				"container_id": containerID,
			})
			pbm.stats.mu.Lock()
			pbm.stats.positionsDropped++
			pbm.stats.mu.Unlock()
			return
		}
	}

	pbm.containerManager.UpdatePosition(containerID, since, logCount, bytesRead)

	pbm.stats.mu.Lock()
	pbm.stats.totalUpdates++
	pbm.stats.mu.Unlock()

	// Increment update counter and check for adaptive flush
	pbm.flushMutex.Lock()
	pbm.updatesSinceFlush++
	pbm.flushMutex.Unlock()

	pbm.maybeFlush()
}

func (pbm *PositionBufferManager) UpdateFilePosition(filePath string, offset int64, size int64, lastModified time.Time, inode uint64, device uint64, bytesRead int64, logCount int64) {
	pbm.fileManager.UpdatePosition(filePath, offset, size, lastModified, inode, device, bytesRead, logCount)

	pbm.stats.mu.Lock()
	pbm.stats.totalUpdates++
	pbm.stats.mu.Unlock()

	// Increment update counter and check for adaptive flush
	pbm.flushMutex.Lock()
	pbm.updatesSinceFlush++
	pbm.flushMutex.Unlock()

	pbm.maybeFlush()
}

func (pbm *PositionBufferManager) GetContainerPosition(containerID string) *ContainerPosition {
	return pbm.containerManager.GetPosition(containerID)
}

func (pbm *PositionBufferManager) GetFilePosition(filePath string) *FilePosition {
	return pbm.fileManager.GetPosition(filePath)
}

func (pbm *PositionBufferManager) GetContainerSince(containerID string) time.Time {
	return pbm.containerManager.GetSinceTime(containerID)
}

func (pbm *PositionBufferManager) GetContainerSinceWithCreated(containerID string, createdTime time.Time) time.Time {
	return pbm.containerManager.GetSinceTimeWithCreated(containerID, createdTime)
}

func (pbm *PositionBufferManager) GetFileOffset(filePath string) int64 {
	return pbm.fileManager.GetOffset(filePath)
}

func (pbm *PositionBufferManager) SetContainerStatus(containerID, status string) {
	pbm.containerManager.SetContainerStatus(containerID, status)
}

func (pbm *PositionBufferManager) SetFileStatus(filePath, status string) {
	pbm.fileManager.SetFileStatus(filePath, status)
}

func (pbm *PositionBufferManager) RemoveContainer(containerID string) {
	pbm.containerManager.RemovePosition(containerID)
}

func (pbm *PositionBufferManager) RemoveFile(filePath string) {
	pbm.fileManager.RemovePosition(filePath)
}

// checkMemoryLimits verifica se os limites de memória foram atingidos
func (pbm *PositionBufferManager) checkMemoryLimits() bool {
	containerStats := pbm.containerManager.GetStats()
	fileStats := pbm.fileManager.GetStats()

	containerPositions, _ := containerStats["total_positions"].(int)
	filePositions, _ := fileStats["total_positions"].(int)
	totalPositions := containerPositions + filePositions

	return totalPositions < pbm.config.MaxMemoryPositions
}

func (pbm *PositionBufferManager) GetStats() map[string]interface{} {
	pbm.stats.mu.RLock()
	defer pbm.stats.mu.RUnlock()

	containerStats := pbm.containerManager.GetStats()
	fileStats := pbm.fileManager.GetStats()

	return map[string]interface{}{
		"buffer_manager": map[string]interface{}{
			"total_flushes":             pbm.stats.totalFlushes,
			"total_cleanups":            pbm.stats.totalCleanups,
			"total_updates":             pbm.stats.totalUpdates,
			"total_errors":              pbm.stats.totalErrors,
			"memory_limit_reached":      pbm.stats.memoryLimitReached,
			"positions_dropped":         pbm.stats.positionsDropped,
			"last_flush_duration_ms":    pbm.stats.lastFlushDuration.Milliseconds(),
			"last_cleanup_duration_ms":  pbm.stats.lastCleanupDuration.Milliseconds(),
			"flush_interval":            pbm.config.FlushInterval.String(),
			"cleanup_interval":          pbm.config.CleanupInterval.String(),
			"max_memory_positions":      pbm.config.MaxMemoryPositions,
		},
		"containers": containerStats,
		"files":      fileStats,
	}
}

func (pbm *PositionBufferManager) GetAllContainerPositions() map[string]*ContainerPosition {
	return pbm.containerManager.GetAllPositions()
}

func (pbm *PositionBufferManager) GetAllFilePositions() map[string]*FilePosition {
	return pbm.fileManager.GetAllPositions()
}