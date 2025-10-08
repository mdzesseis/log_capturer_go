package positions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type ContainerPosition struct {
	ContainerID   string    `json:"container_id"`
	Since         time.Time `json:"since"`
	LastRead      time.Time `json:"last_read"`
	LastLogTime   time.Time `json:"last_log_time"`
	LogCount      int64     `json:"log_count"`
	BytesRead     int64     `json:"bytes_read"`
	Status        string    `json:"status"`
	RestartCount  int       `json:"restart_count"`
}

type ContainerPositionManager struct {
	positions    map[string]*ContainerPosition
	mu           sync.RWMutex
	positionsDir string
	filename     string
	logger       *logrus.Logger
	dirty        bool
	lastFlush    time.Time
}

func NewContainerPositionManager(positionsDir string, logger *logrus.Logger) *ContainerPositionManager {
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

	return &ContainerPositionManager{
		positions:    make(map[string]*ContainerPosition),
		positionsDir: positionsDir,
		filename:     filepath.Join(positionsDir, "container_positions.json"),
		logger:       logger,
		lastFlush:    time.Now(),
	}
}

func (cpm *ContainerPositionManager) LoadPositions() error {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	data, err := os.ReadFile(cpm.filename)
	if err != nil {
		if os.IsNotExist(err) {
			cpm.logger.Info("Container positions file not found, starting fresh", nil)
			return nil
		}
		return fmt.Errorf("failed to read positions file: %w", err)
	}

	var positions map[string]*ContainerPosition
	if err := json.Unmarshal(data, &positions); err != nil {
		return fmt.Errorf("failed to unmarshal positions: %w", err)
	}

	cpm.positions = positions
	cpm.logger.Info("Loaded container positions", map[string]interface{}{
		"count": len(positions),
	})

	return nil
}

func (cpm *ContainerPositionManager) SavePositions() error {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()

	data, err := json.MarshalIndent(cpm.positions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal positions: %w", err)
	}

	tempFile := cpm.filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp positions file: %w", err)
	}

	if err := os.Rename(tempFile, cpm.filename); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename positions file: %w", err)
	}

	cpm.dirty = false
	cpm.lastFlush = time.Now()

	cpm.logger.Debug("Saved container positions", map[string]interface{}{
		"count": len(cpm.positions),
		"file":  cpm.filename,
	})

	return nil
}

func (cpm *ContainerPositionManager) GetPosition(containerID string) *ContainerPosition {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()

	if pos, exists := cpm.positions[containerID]; exists {
		// Return a copy to avoid concurrent modification
		return &ContainerPosition{
			ContainerID:  pos.ContainerID,
			Since:        pos.Since,
			LastRead:     pos.LastRead,
			LastLogTime:  pos.LastLogTime,
			LogCount:     pos.LogCount,
			BytesRead:    pos.BytesRead,
			Status:       pos.Status,
			RestartCount: pos.RestartCount,
		}
	}

	return nil
}

func (cpm *ContainerPositionManager) UpdatePosition(containerID string, since time.Time, logCount int64, bytesRead int64) {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	pos, exists := cpm.positions[containerID]
	if !exists {
		pos = &ContainerPosition{
			ContainerID: containerID,
			Since:       since,
			Status:      "active",
		}
		cpm.positions[containerID] = pos
	}

	pos.LastRead = time.Now()
	pos.LastLogTime = since
	pos.LogCount += logCount
	pos.BytesRead += bytesRead
	cpm.dirty = true

	cpm.logger.Debug("Updated container position", map[string]interface{}{
		"container_id": containerID,
		"since":        since.Format(time.RFC3339),
		"log_count":    pos.LogCount,
		"bytes_read":   pos.BytesRead,
	})
}

func (cpm *ContainerPositionManager) SetContainerStatus(containerID, status string) {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	pos, exists := cpm.positions[containerID]
	if !exists {
		pos = &ContainerPosition{
			ContainerID: containerID,
			Since:       time.Now(),
			Status:      status,
		}
		cpm.positions[containerID] = pos
	} else {
		pos.Status = status
	}

	if status == "restarted" {
		pos.RestartCount++
	}

	cpm.dirty = true

	cpm.logger.Debug("Set container status", map[string]interface{}{
		"container_id":   containerID,
		"status":         status,
		"restart_count":  pos.RestartCount,
	})
}

func (cpm *ContainerPositionManager) GetSinceTime(containerID string) time.Time {
	pos := cpm.GetPosition(containerID)
	if pos == nil {
		// If no position exists, start from now
		return time.Now()
	}

	// If container was restarted, start from the last log time
	if pos.Status == "restarted" || pos.Status == "stopped" {
		if !pos.LastLogTime.IsZero() {
			return pos.LastLogTime
		}
	}

	// For active containers, continue from last read position
	if !pos.Since.IsZero() {
		return pos.Since
	}

	return time.Now()
}

// GetSinceTimeWithCreated retorna desde quando começar a ler logs, considerando data de criação para containers novos
func (cpm *ContainerPositionManager) GetSinceTimeWithCreated(containerID string, createdTime time.Time) time.Time {
	pos := cpm.GetPosition(containerID)
	if pos == nil {
		// Para containers novos, usar data de criação para capturar todos os logs
		if !createdTime.IsZero() {
			cpm.logger.Debug("New container detected, starting from creation time", map[string]interface{}{
				"container_id": containerID,
				"created_at":   createdTime.Format(time.RFC3339),
			})
			return createdTime
		}
		// Fallback para now se não tiver created time
		return time.Now()
	}

	// If container was restarted, start from the last log time
	if pos.Status == "restarted" || pos.Status == "stopped" {
		if !pos.LastLogTime.IsZero() {
			return pos.LastLogTime
		}
	}

	// For active containers, continue from last read position
	if !pos.Since.IsZero() {
		return pos.Since
	}

	return time.Now()
}

func (cpm *ContainerPositionManager) RemovePosition(containerID string) {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	if _, exists := cpm.positions[containerID]; exists {
		delete(cpm.positions, containerID)
		cpm.dirty = true

		cpm.logger.Debug("Removed container position", map[string]interface{}{
			"container_id": containerID,
		})
	}
}

func (cpm *ContainerPositionManager) CleanupOldPositions(maxAge time.Duration) {
	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	// Coletamos IDs para remover primeiro para evitar concurrent map iteration/write
	toDelete := make([]string, 0)
	for containerID, pos := range cpm.positions {
		// Remover apenas containers com status "removed" que são antigos
		// Containers "stopped" são mantidos para possível retomada
		if pos.LastRead.Before(cutoff) && pos.Status == "removed" {
			toDelete = append(toDelete, containerID)
		}
	}
	for _, containerID := range toDelete {
		delete(cpm.positions, containerID)
		removed++
	}

	if removed > 0 {
		cpm.dirty = true
		cpm.logger.Info("Cleaned up old container positions", map[string]interface{}{
			"removed": removed,
			"cutoff":  cutoff.Format(time.RFC3339),
			"criteria": "status=removed only",
		})
	}
}

func (cpm *ContainerPositionManager) GetAllPositions() map[string]*ContainerPosition {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()

	result := make(map[string]*ContainerPosition, len(cpm.positions))
	for id, pos := range cpm.positions {
		result[id] = &ContainerPosition{
			ContainerID:  pos.ContainerID,
			Since:        pos.Since,
			LastRead:     pos.LastRead,
			LastLogTime:  pos.LastLogTime,
			LogCount:     pos.LogCount,
			BytesRead:    pos.BytesRead,
			Status:       pos.Status,
			RestartCount: pos.RestartCount,
		}
	}

	return result
}

func (cpm *ContainerPositionManager) IsDirty() bool {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()
	return cpm.dirty
}

func (cpm *ContainerPositionManager) GetLastFlushTime() time.Time {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()
	return cpm.lastFlush
}

func (cpm *ContainerPositionManager) GetStats() map[string]interface{} {
	cpm.mu.RLock()
	defer cpm.mu.RUnlock()

	stats := map[string]interface{}{
		"total_positions": len(cpm.positions),
		"dirty":           cpm.dirty,
		"last_flush":      cpm.lastFlush.Format(time.RFC3339),
		"positions_file":  cpm.filename,
	}

	statusCounts := make(map[string]int)
	for _, pos := range cpm.positions {
		statusCounts[pos.Status]++
	}
	stats["status_counts"] = statusCounts

	return stats
}