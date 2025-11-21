package positions

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"ssw-logs-capture/internal/metrics"
)

// CheckpointInfo contains metadata about a checkpoint
type CheckpointInfo struct {
	Filename    string    `json:"filename"`
	Timestamp   time.Time `json:"timestamp"`
	SizeBytes   int64     `json:"size_bytes"`
	EntryCount  int       `json:"entry_count"`
	Compressed  bool      `json:"compressed"`
	MD5Checksum string    `json:"md5_checksum,omitempty"`
}

// CheckpointData represents the full state snapshot
type CheckpointData struct {
	Version          string                        `json:"version"`
	Timestamp        time.Time                     `json:"timestamp"`
	ContainerPositions map[string]*ContainerPosition `json:"container_positions,omitempty"`
	FilePositions    map[string]*FilePosition      `json:"file_positions,omitempty"`
	Metadata         map[string]interface{}        `json:"metadata,omitempty"`
}

// CheckpointManager handles periodic snapshots and restore operations
type CheckpointManager struct {
	mu                 sync.RWMutex
	checkpointDir      string
	checkpointInterval time.Duration
	maxCheckpoints     int
	lastCheckpoint     time.Time
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	logger             *logrus.Logger
	enabled            bool

	// References to position managers
	containerManager *ContainerPositionManager
	fileManager      *FilePositionManager

	stats struct {
		mu                      sync.RWMutex
		totalCheckpoints        int64
		totalRestores           int64
		lastCheckpointDuration  time.Duration
		lastCheckpointSize      int64
		lastCheckpointEntryCount int
		failedCheckpoints       int64
		failedRestores          int64
	}
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(
	checkpointDir string,
	containerManager *ContainerPositionManager,
	fileManager *FilePositionManager,
	logger *logrus.Logger,
) *CheckpointManager {
	if checkpointDir == "" {
		checkpointDir = "/app/data/checkpoints"
	}

	// Ensure checkpoint directory exists
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		logger.Error("Failed to create checkpoint directory", map[string]interface{}{
			"directory": checkpointDir,
			"error":     err.Error(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &CheckpointManager{
		checkpointDir:      checkpointDir,
		checkpointInterval: 5 * time.Minute, // Default: 5 minutes
		maxCheckpoints:     3,                // Keep 3 generations
		ctx:                ctx,
		cancel:             cancel,
		logger:             logger,
		enabled:            true,
		containerManager:   containerManager,
		fileManager:        fileManager,
		lastCheckpoint:     time.Now(),
	}
}

// Start begins periodic checkpoint creation
func (cm *CheckpointManager) Start() error {
	if !cm.enabled {
		cm.logger.Info("Checkpoint manager disabled", nil)
		return nil
	}

	cm.logger.Info("Starting checkpoint manager", map[string]interface{}{
		"interval":         cm.checkpointInterval.String(),
		"max_checkpoints":  cm.maxCheckpoints,
		"checkpoint_dir":   cm.checkpointDir,
	})

	cm.wg.Add(1)
	go cm.checkpointLoop()

	return nil
}

// Stop stops the checkpoint manager
func (cm *CheckpointManager) Stop() error {
	if !cm.enabled {
		return nil
	}

	cm.logger.Info("Stopping checkpoint manager", nil)

	// Cancel context to stop background goroutine
	cm.cancel()

	// Wait for goroutine to finish
	cm.wg.Wait()

	// Create final checkpoint on shutdown
	if err := cm.CreateCheckpoint(); err != nil {
		cm.logger.Error("Failed to create final checkpoint on shutdown", map[string]interface{}{
			"error": err.Error(),
		})
		return err
	}

	cm.logger.Info("Checkpoint manager stopped", nil)
	return nil
}

// checkpointLoop periodically creates checkpoints
func (cm *CheckpointManager) checkpointLoop() {
	defer cm.wg.Done()

	ticker := time.NewTicker(cm.checkpointInterval)
	defer ticker.Stop()

	// Track update rate
	var lastUpdateCount int64
	lastCheck := time.Now()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			// Calculate and update position update rate
			cm.stats.mu.RLock()
			currentCount := cm.stats.totalCheckpoints
			cm.stats.mu.RUnlock()

			elapsed := time.Since(lastCheck).Seconds()
			if elapsed > 0 {
				rate := float64(currentCount-lastUpdateCount) / elapsed
				metrics.UpdatePositionUpdateRate("checkpoint", rate)
			}
			lastUpdateCount = currentCount
			lastCheck = time.Now()

			// Calculate memory usage estimate (rough estimate based on entry count)
			cm.mu.RLock()
			var memoryEstimate int64
			if cm.containerManager != nil {
				positions := cm.containerManager.GetAllPositions()
				memoryEstimate += int64(len(positions) * 256) // ~256 bytes per container position
			}
			if cm.fileManager != nil {
				positions := cm.fileManager.GetAllPositions()
				memoryEstimate += int64(len(positions) * 128) // ~128 bytes per file position
			}
			cm.mu.RUnlock()
			metrics.UpdatePositionMemoryUsage(memoryEstimate)

			// Record lag distribution
			cm.mu.RLock()
			lagSeconds := time.Since(cm.lastCheckpoint).Seconds()
			cm.mu.RUnlock()
			metrics.RecordPositionLagDistribution("checkpoint", lagSeconds)

			if err := cm.CreateCheckpoint(); err != nil {
				cm.logger.Error("Periodic checkpoint failed", map[string]interface{}{
					"error": err.Error(),
				})
				cm.stats.mu.Lock()
				cm.stats.failedCheckpoints++
				cm.stats.mu.Unlock()

				// Update backpressure on failure (indicate system stress)
				metrics.UpdatePositionBackpressure("checkpoint", 1.0)
			} else {
				// Reset backpressure on success
				metrics.UpdatePositionBackpressure("checkpoint", 0.0)
			}
		}
	}
}

// CreateCheckpoint creates a new checkpoint file
func (cm *CheckpointManager) CreateCheckpoint() error {
	if !cm.enabled {
		return nil
	}

	start := time.Now()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Collect current state from both managers
	data := &CheckpointData{
		Version:   "1.0",
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"created_by": "checkpoint_manager",
			"hostname":   getHostname(),
		},
	}

	// Collect container positions
	if cm.containerManager != nil {
		data.ContainerPositions = cm.containerManager.GetAllPositions()
	}

	// Collect file positions
	if cm.fileManager != nil {
		data.FilePositions = cm.fileManager.GetAllPositions()
	}

	// Calculate entry count
	entryCount := len(data.ContainerPositions) + len(data.FilePositions)

	// Generate checkpoint filename with timestamp (include milliseconds for uniqueness)
	timestamp := time.Now().Format("2006-01-02_15-04-05.000000")
	filename := filepath.Join(cm.checkpointDir, fmt.Sprintf("checkpoint_%s.json.gz", timestamp))

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint data: %w", err)
	}

	// Write compressed checkpoint (atomic write: temp + rename)
	tempFile := filename + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp checkpoint file: %w", err)
	}
	defer os.Remove(tempFile) // Clean up on error

	// Compress with gzip
	gzWriter := gzip.NewWriter(file)
	if _, err := gzWriter.Write(jsonData); err != nil {
		file.Close()
		gzWriter.Close()
		return fmt.Errorf("failed to write compressed checkpoint: %w", err)
	}

	if err := gzWriter.Close(); err != nil {
		file.Close()
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close checkpoint file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, filename); err != nil {
		return fmt.Errorf("failed to rename checkpoint file: %w", err)
	}

	// Get file size
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("failed to stat checkpoint file: %w", err)
	}

	duration := time.Since(start)

	// Update statistics
	cm.stats.mu.Lock()
	cm.stats.totalCheckpoints++
	cm.stats.lastCheckpointDuration = duration
	cm.stats.lastCheckpointSize = fileInfo.Size()
	cm.stats.lastCheckpointEntryCount = entryCount
	cm.stats.mu.Unlock()

	// Update last checkpoint time
	cm.lastCheckpoint = time.Now()

	// Record metrics
	metrics.PositionCheckpointCreatedTotal.Inc()
	metrics.PositionSaveSuccess.Inc()
	metrics.PositionCheckpointSizeBytes.Set(float64(fileInfo.Size()))
	metrics.PositionCheckpointAgeSeconds.Set(0) // Just created
	metrics.CheckpointHealth.WithLabelValues("checkpoint_creation").Set(1)

	// Update position file size metric for checkpoint
	metrics.UpdatePositionFileSize("checkpoint", fileInfo.Size())

	// Update position active by status based on entry counts
	readingCount := 0
	idleCount := 0
	for range data.ContainerPositions {
		readingCount++ // Assume all active positions are "reading"
	}
	for range data.FilePositions {
		readingCount++
	}
	metrics.UpdatePositionActiveByStatus("reading", readingCount)
	metrics.UpdatePositionActiveByStatus("idle", idleCount)
	metrics.UpdatePositionActiveByStatus("error", 0)

	cm.logger.Info("Checkpoint created successfully", map[string]interface{}{
		"filename":      filename,
		"size_bytes":    fileInfo.Size(),
		"entry_count":   entryCount,
		"duration_ms":   duration.Milliseconds(),
		"compressed":    true,
	})

	// Cleanup old checkpoints
	if err := cm.CleanupOldCheckpoints(); err != nil {
		cm.logger.Error("Failed to cleanup old checkpoints", map[string]interface{}{
			"error": err.Error(),
		})
	}

	return nil
}

// RestoreLatestCheckpoint restores from the most recent checkpoint
func (cm *CheckpointManager) RestoreLatestCheckpoint() (*CheckpointData, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	checkpoints, err := cm.ListCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}

	if len(checkpoints) == 0 {
		return nil, fmt.Errorf("no checkpoints available")
	}

	// Sort by timestamp (newest first)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].Timestamp.After(checkpoints[j].Timestamp)
	})

	latestCheckpoint := checkpoints[0]

	cm.logger.Info("Restoring from checkpoint", map[string]interface{}{
		"filename":  latestCheckpoint.Filename,
		"timestamp": latestCheckpoint.Timestamp.Format(time.RFC3339),
	})

	// Read and decompress checkpoint
	data, err := cm.readCheckpoint(latestCheckpoint.Filename)
	if err != nil {
		cm.stats.mu.Lock()
		cm.stats.failedRestores++
		cm.stats.mu.Unlock()

		metrics.PositionCheckpointRestoreAttemptsTotal.WithLabelValues("failure").Inc()
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	// Update statistics
	cm.stats.mu.Lock()
	cm.stats.totalRestores++
	cm.stats.mu.Unlock()

	metrics.PositionCheckpointRestoreAttemptsTotal.WithLabelValues("success").Inc()

	cm.logger.Info("Checkpoint restored successfully", map[string]interface{}{
		"filename":           latestCheckpoint.Filename,
		"container_positions": len(data.ContainerPositions),
		"file_positions":     len(data.FilePositions),
	})

	return data, nil
}

// readCheckpoint reads and decompresses a checkpoint file
func (cm *CheckpointManager) readCheckpoint(filename string) (*CheckpointData, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open checkpoint file: %w", err)
	}
	defer file.Close()

	// Decompress with gzip
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Read decompressed data
	decompressedData, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read decompressed data: %w", err)
	}

	// Unmarshal JSON
	var data CheckpointData
	if err := json.Unmarshal(decompressedData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint data: %w", err)
	}

	return &data, nil
}

// ListCheckpoints returns a list of available checkpoints
func (cm *CheckpointManager) ListCheckpoints() ([]CheckpointInfo, error) {
	files, err := os.ReadDir(cm.checkpointDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint directory: %w", err)
	}

	var checkpoints []CheckpointInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Only consider .json.gz files
		if filepath.Ext(file.Name()) != ".gz" {
			continue
		}

		fullPath := filepath.Join(cm.checkpointDir, file.Name())
		info, err := file.Info()
		if err != nil {
			cm.logger.Warn("Failed to get checkpoint file info", map[string]interface{}{
				"filename": file.Name(),
				"error":    err.Error(),
			})
			continue
		}

		checkpoints = append(checkpoints, CheckpointInfo{
			Filename:   fullPath,
			Timestamp:  info.ModTime(),
			SizeBytes:  info.Size(),
			Compressed: true,
		})
	}

	return checkpoints, nil
}

// CleanupOldCheckpoints removes checkpoints exceeding the max count
func (cm *CheckpointManager) CleanupOldCheckpoints() error {
	checkpoints, err := cm.ListCheckpoints()
	if err != nil {
		return fmt.Errorf("failed to list checkpoints: %w", err)
	}

	if len(checkpoints) <= cm.maxCheckpoints {
		return nil // No cleanup needed
	}

	// Sort by timestamp (newest first)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].Timestamp.After(checkpoints[j].Timestamp)
	})

	// Remove old checkpoints (keep only maxCheckpoints)
	toDelete := checkpoints[cm.maxCheckpoints:]
	deleted := 0

	for _, checkpoint := range toDelete {
		if err := os.Remove(checkpoint.Filename); err != nil {
			cm.logger.Error("Failed to remove old checkpoint", map[string]interface{}{
				"filename": checkpoint.Filename,
				"error":    err.Error(),
			})
			continue
		}
		deleted++
		cm.logger.Debug("Removed old checkpoint", map[string]interface{}{
			"filename": checkpoint.Filename,
		})
	}

	if deleted > 0 {
		cm.logger.Info("Cleaned up old checkpoints", map[string]interface{}{
			"deleted": deleted,
			"kept":    cm.maxCheckpoints,
		})
	}

	return nil
}

// GetStats returns checkpoint manager statistics
func (cm *CheckpointManager) GetStats() map[string]interface{} {
	cm.stats.mu.RLock()
	defer cm.stats.mu.RUnlock()

	cm.mu.RLock()
	ageSinceLastCheckpoint := time.Since(cm.lastCheckpoint)
	cm.mu.RUnlock()

	return map[string]interface{}{
		"total_checkpoints":          cm.stats.totalCheckpoints,
		"total_restores":             cm.stats.totalRestores,
		"failed_checkpoints":         cm.stats.failedCheckpoints,
		"failed_restores":            cm.stats.failedRestores,
		"last_checkpoint_duration_ms": cm.stats.lastCheckpointDuration.Milliseconds(),
		"last_checkpoint_size_bytes":  cm.stats.lastCheckpointSize,
		"last_checkpoint_entry_count": cm.stats.lastCheckpointEntryCount,
		"age_since_last_checkpoint_seconds": ageSinceLastCheckpoint.Seconds(),
		"checkpoint_interval":        cm.checkpointInterval.String(),
		"max_checkpoints":            cm.maxCheckpoints,
		"enabled":                    cm.enabled,
	}
}

// SetInterval changes the checkpoint interval (for dynamic configuration)
func (cm *CheckpointManager) SetInterval(interval time.Duration) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpointInterval = interval
	cm.logger.Info("Checkpoint interval updated", map[string]interface{}{
		"new_interval": interval.String(),
	})
}

// Enable enables checkpoint creation
func (cm *CheckpointManager) Enable() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.enabled = true
	cm.logger.Info("Checkpoint manager enabled", nil)
	metrics.CheckpointHealth.WithLabelValues("checkpoint_manager").Set(1)
}

// Disable disables checkpoint creation
func (cm *CheckpointManager) Disable() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.enabled = false
	cm.logger.Info("Checkpoint manager disabled", nil)
	metrics.CheckpointHealth.WithLabelValues("checkpoint_manager").Set(0)
}

// getHostname returns the system hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
