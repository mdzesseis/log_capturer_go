package positions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"ssw-logs-capture/internal/metrics"
)

type FilePosition struct {
	FilePath     string    `json:"file_path"`
	Offset       int64     `json:"offset"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	LastRead     time.Time `json:"last_read"`
	Inode        uint64    `json:"inode"`
	Device       uint64    `json:"device"`
	LogCount     int64     `json:"log_count"`
	BytesRead    int64     `json:"bytes_read"`
	Status       string    `json:"status"`
}

type FilePositionManager struct {
	positions    map[string]*FilePosition
	mu           sync.RWMutex
	positionsDir string
	filename     string
	logger       *logrus.Logger
	dirty        bool
	lastFlush    time.Time
}

func NewFilePositionManager(positionsDir string, logger *logrus.Logger) *FilePositionManager {
	if positionsDir == "" {
		positionsDir = "/app/data/positions"
	}

	// Ensure positions directory exists
	if err := os.MkdirAll(positionsDir, 0755); err != nil {
		logger.Error("Failed to create positions directory", map[string]interface{}{
			"directory": positionsDir,
			"error":     err.Error(),
		})
	}

	return &FilePositionManager{
		positions:    make(map[string]*FilePosition),
		positionsDir: positionsDir,
		filename:     filepath.Join(positionsDir, "file_positions.json"),
		logger:       logger,
		lastFlush:    time.Now(),
	}
}

func (fpm *FilePositionManager) LoadPositions() error {
	fpm.mu.Lock()
	defer fpm.mu.Unlock()

	data, err := os.ReadFile(fpm.filename)
	if err != nil {
		if os.IsNotExist(err) {
			fpm.logger.Info("File positions file not found, starting fresh", nil)
			return nil
		}
		return fmt.Errorf("failed to read positions file: %w", err)
	}

	var positions map[string]*FilePosition
	if err := json.Unmarshal(data, &positions); err != nil {
		return fmt.Errorf("failed to unmarshal positions: %w", err)
	}

	fpm.positions = positions
	fpm.logger.Info("Loaded file positions", map[string]interface{}{
		"count": len(positions),
	})

	return nil
}

func (fpm *FilePositionManager) SavePositions() error {
	// Phase 1: Read data under RLock
	fpm.mu.RLock()
	if !fpm.dirty {
		fpm.mu.RUnlock()
		return nil
	}
	positions := fpm.deepCopyPositions()
	positionCount := len(positions)
	fpm.mu.RUnlock()

	// Phase 2: Write to disk (no lock held)
	data, err := json.MarshalIndent(positions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal positions: %w", err)
	}

	tempFile := fpm.filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp positions file: %w", err)
	}

	if err := os.Rename(tempFile, fpm.filename); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename positions file: %w", err)
	}

	// Phase 3: Update state under Lock
	fpm.mu.Lock()
	fpm.dirty = false
	fpm.lastFlush = time.Now()
	fpm.mu.Unlock()

	// Record metrics
	metrics.RecordPositionSaveSuccess()
	metrics.UpdatePositionLag("file", 0)

	fpm.logger.Debug("Saved file positions", map[string]interface{}{
		"count": positionCount,
		"file":  fpm.filename,
	})

	return nil
}

// deepCopyPositions creates a deep copy of positions map
// MUST be called with fpm.mu.RLock() held
func (fpm *FilePositionManager) deepCopyPositions() map[string]*FilePosition {
	positions := make(map[string]*FilePosition, len(fpm.positions))
	for path, pos := range fpm.positions {
		// FilePosition is a struct, so this creates a value copy
		posCopy := &FilePosition{
			FilePath:     pos.FilePath,
			Offset:       pos.Offset,
			Size:         pos.Size,
			LastModified: pos.LastModified,
			LastRead:     pos.LastRead,
			Inode:        pos.Inode,
			Device:       pos.Device,
			LogCount:     pos.LogCount,
			BytesRead:    pos.BytesRead,
			Status:       pos.Status,
		}
		positions[path] = posCopy
	}
	return positions
}

func (fpm *FilePositionManager) GetPosition(filePath string) *FilePosition {
	fpm.mu.RLock()
	defer fpm.mu.RUnlock()

	if pos, exists := fpm.positions[filePath]; exists {
		// Return a copy to avoid concurrent modification
		return &FilePosition{
			FilePath:     pos.FilePath,
			Offset:       pos.Offset,
			Size:         pos.Size,
			LastModified: pos.LastModified,
			LastRead:     pos.LastRead,
			Inode:        pos.Inode,
			Device:       pos.Device,
			LogCount:     pos.LogCount,
			BytesRead:    pos.BytesRead,
			Status:       pos.Status,
		}
	}

	return nil
}

func (fpm *FilePositionManager) UpdatePosition(filePath string, offset int64, size int64, lastModified time.Time, inode uint64, device uint64, bytesRead int64, logCount int64) {
	fpm.mu.Lock()
	defer fpm.mu.Unlock()

	pos, exists := fpm.positions[filePath]
	if !exists {
		pos = &FilePosition{
			FilePath: filePath,
			Status:   "active",
		}
		fpm.positions[filePath] = pos
	}

	// Check if file was rotated (inode or device changed)
	if pos.Inode != 0 && (pos.Inode != inode || pos.Device != device) {
		fpm.logger.Info("File rotation detected", map[string]interface{}{
			"file_path":  filePath,
			"old_inode":  pos.Inode,
			"new_inode":  inode,
			"old_device": pos.Device,
			"new_device": device,
		})
		// Reset offset for rotated file
		pos.Offset = 0
		// Record metric
		metrics.RecordPositionRotation(filePath)
	}

	// Check if file was truncated
	if size < pos.Size {
		fpm.logger.Info("File truncation detected", map[string]interface{}{
			"file_path": filePath,
			"old_size":  pos.Size,
			"new_size":  size,
		})
		// Reset offset for truncated file
		pos.Offset = 0
		// Record metric
		metrics.RecordPositionTruncation(filePath)
	}

	pos.Offset = offset
	pos.Size = size
	pos.LastModified = lastModified
	pos.LastRead = time.Now()
	pos.Inode = inode
	pos.Device = device
	pos.LogCount += logCount
	pos.BytesRead += bytesRead
	fpm.dirty = true

	fpm.logger.Debug("Updated file position", map[string]interface{}{
		"file_path":     filePath,
		"offset":        offset,
		"size":          size,
		"log_count":     pos.LogCount,
		"bytes_read":    pos.BytesRead,
		"last_modified": lastModified.Format(time.RFC3339),
	})
}

func (fpm *FilePositionManager) SetFileStatus(filePath, status string) {
	fpm.mu.Lock()
	defer fpm.mu.Unlock()

	pos, exists := fpm.positions[filePath]
	if !exists {
		pos = &FilePosition{
			FilePath: filePath,
			Status:   status,
		}
		fpm.positions[filePath] = pos
	} else {
		pos.Status = status
	}

	fpm.dirty = true

	fpm.logger.Debug("Set file status", map[string]interface{}{
		"file_path": filePath,
		"status":    status,
	})
}

func (fpm *FilePositionManager) GetOffset(filePath string) int64 {
	pos := fpm.GetPosition(filePath)
	if pos == nil {
		return 0
	}
	return pos.Offset
}

func (fpm *FilePositionManager) RemovePosition(filePath string) {
	fpm.mu.Lock()
	defer fpm.mu.Unlock()

	if _, exists := fpm.positions[filePath]; exists {
		delete(fpm.positions, filePath)
		fpm.dirty = true

		fpm.logger.Debug("Removed file position", map[string]interface{}{
			"file_path": filePath,
		})
	}
}

func (fpm *FilePositionManager) CleanupOldPositions(maxAge time.Duration) {
	fpm.mu.Lock()
	defer fpm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	// Coletamos arquivos para remover primeiro para evitar concurrent map iteration/write
	toDelete := make([]string, 0)
	for filePath, pos := range fpm.positions {
		if pos.LastRead.Before(cutoff) && (pos.Status == "removed" || pos.Status == "deleted") {
			toDelete = append(toDelete, filePath)
		}
	}

	for _, filePath := range toDelete {
		delete(fpm.positions, filePath)
		removed++
	}

	if removed > 0 {
		fpm.dirty = true
		fpm.logger.Info("Cleaned up old file positions", map[string]interface{}{
			"removed": removed,
			"cutoff":  cutoff.Format(time.RFC3339),
		})
	}
}

func (fpm *FilePositionManager) GetAllPositions() map[string]*FilePosition {
	fpm.mu.RLock()
	defer fpm.mu.RUnlock()

	result := make(map[string]*FilePosition, len(fpm.positions))
	for path, pos := range fpm.positions {
		result[path] = &FilePosition{
			FilePath:     pos.FilePath,
			Offset:       pos.Offset,
			Size:         pos.Size,
			LastModified: pos.LastModified,
			LastRead:     pos.LastRead,
			Inode:        pos.Inode,
			Device:       pos.Device,
			LogCount:     pos.LogCount,
			BytesRead:    pos.BytesRead,
			Status:       pos.Status,
		}
	}

	return result
}

func (fpm *FilePositionManager) IsDirty() bool {
	fpm.mu.RLock()
	defer fpm.mu.RUnlock()
	return fpm.dirty
}

func (fpm *FilePositionManager) GetLastFlushTime() time.Time {
	fpm.mu.RLock()
	defer fpm.mu.RUnlock()
	return fpm.lastFlush
}

func (fpm *FilePositionManager) GetStats() map[string]interface{} {
	fpm.mu.RLock()
	defer fpm.mu.RUnlock()

	stats := map[string]interface{}{
		"total_positions": len(fpm.positions),
		"dirty":           fpm.dirty,
		"last_flush":      fpm.lastFlush.Format(time.RFC3339),
		"positions_file":  fpm.filename,
	}

	statusCounts := make(map[string]int)
	totalBytes := int64(0)
	totalLogs := int64(0)

	for _, pos := range fpm.positions {
		statusCounts[pos.Status]++
		totalBytes += pos.BytesRead
		totalLogs += pos.LogCount
	}

	stats["status_counts"] = statusCounts
	stats["total_bytes_read"] = totalBytes
	stats["total_logs_read"] = totalLogs

	return stats
}