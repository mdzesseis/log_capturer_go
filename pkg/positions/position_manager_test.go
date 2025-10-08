package positions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionManager_NewPositionManager(t *testing.T) {
	config := Config{
		Enabled:      true,
		FilePath:     "/tmp/positions.json",
		SaveInterval: 30 * time.Second,
		TTL:          24 * time.Hour,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.Equal(t, logger, manager.logger)
	assert.NotNil(t, manager.positions)
}

func TestPositionManager_SetAndGetPosition(t *testing.T) {
	tempDir := os.TempDir()
	positionsFile := filepath.Join(tempDir, "test_positions.json")
	defer os.Remove(positionsFile)

	config := Config{
		Enabled:      true,
		FilePath:     positionsFile,
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Test setting position
	testFile := "/var/log/test.log"
	testPosition := int64(1024)
	testSize := int64(2048)
	testModTime := time.Now()

	manager.SetPosition(testFile, testPosition, testSize, testModTime)

	// Test getting position
	position, size, modTime, exists := manager.GetPosition(testFile)

	assert.True(t, exists)
	assert.Equal(t, testPosition, position)
	assert.Equal(t, testSize, size)
	assert.WithinDuration(t, testModTime, modTime, time.Second)
}

func TestPositionManager_GetPosition_NotExists(t *testing.T) {
	config := Config{
		Enabled:      true,
		FilePath:     "/tmp/test_positions.json",
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Test getting non-existent position
	position, size, modTime, exists := manager.GetPosition("/nonexistent/file.log")

	assert.False(t, exists)
	assert.Equal(t, int64(0), position)
	assert.Equal(t, int64(0), size)
	assert.True(t, modTime.IsZero())
}

func TestPositionManager_DetectTruncation(t *testing.T) {
	tempDir := os.TempDir()
	positionsFile := filepath.Join(tempDir, "test_positions_truncation.json")
	defer os.Remove(positionsFile)

	config := Config{
		Enabled:      true,
		FilePath:     positionsFile,
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Set initial position
	testFile := "/var/log/test.log"
	manager.SetPosition(testFile, 1024, 2048, time.Now())

	// Simulate file truncation (current size < stored position)
	truncated := manager.CheckTruncation(testFile, 512, time.Now())

	assert.True(t, truncated)

	// Verify position was reset
	position, size, _, exists := manager.GetPosition(testFile)
	assert.True(t, exists)
	assert.Equal(t, int64(0), position) // Should be reset to 0
	assert.Equal(t, int64(512), size)   // Should be updated to current size
}

func TestPositionManager_CheckTruncation_NoTruncation(t *testing.T) {
	config := Config{
		Enabled:      true,
		FilePath:     "/tmp/test_positions_no_truncation.json",
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Set initial position
	testFile := "/var/log/test.log"
	initialModTime := time.Now().Add(-1 * time.Hour)
	manager.SetPosition(testFile, 1024, 2048, initialModTime)

	// No truncation (current size >= stored position)
	truncated := manager.CheckTruncation(testFile, 3072, time.Now())

	assert.False(t, truncated)

	// Verify position remains unchanged
	position, size, modTime, exists := manager.GetPosition(testFile)
	assert.True(t, exists)
	assert.Equal(t, int64(1024), position)
	assert.Equal(t, int64(3072), size) // Size should be updated
	assert.True(t, modTime.After(initialModTime)) // ModTime should be updated
}

func TestPositionManager_SaveAndLoadPositions(t *testing.T) {
	tempDir := os.TempDir()
	positionsFile := filepath.Join(tempDir, "test_save_load_positions.json")
	defer os.Remove(positionsFile)

	config := Config{
		Enabled:      true,
		FilePath:     positionsFile,
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour,
		BackupCount:  3,
	}

	logger := logrus.New()
	ctx := context.Background()

	// Create first manager and set positions
	manager1 := NewPositionManager(config, logger, ctx)

	testData := map[string]struct {
		position int64
		size     int64
		modTime  time.Time
	}{
		"/var/log/app.log":    {1024, 2048, time.Now().Add(-1 * time.Hour)},
		"/var/log/error.log":  {512, 1024, time.Now().Add(-30 * time.Minute)},
		"/var/log/access.log": {2048, 4096, time.Now().Add(-15 * time.Minute)},
	}

	for file, data := range testData {
		manager1.SetPosition(file, data.position, data.size, data.modTime)
	}

	// Save positions
	err := manager1.savePositions()
	require.NoError(t, err)

	// Create second manager and load positions
	manager2 := NewPositionManager(config, logger, ctx)
	err = manager2.loadPositions()
	require.NoError(t, err)

	// Verify all positions were loaded correctly
	for file, expectedData := range testData {
		position, size, modTime, exists := manager2.GetPosition(file)
		assert.True(t, exists, "Position for %s should exist", file)
		assert.Equal(t, expectedData.position, position, "Position mismatch for %s", file)
		assert.Equal(t, expectedData.size, size, "Size mismatch for %s", file)
		assert.WithinDuration(t, expectedData.modTime, modTime, time.Second, "ModTime mismatch for %s", file)
	}
}

func TestPositionManager_CleanupExpiredPositions(t *testing.T) {
	config := Config{
		Enabled:      true,
		FilePath:     "/tmp/test_cleanup_positions.json",
		SaveInterval: 1 * time.Second,
		TTL:          1 * time.Hour, // 1 hour TTL
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Add fresh position
	freshFile := "/var/log/fresh.log"
	manager.SetPosition(freshFile, 1024, 2048, time.Now())

	// Add expired position (manually set old last access)
	expiredFile := "/var/log/expired.log"
	manager.SetPosition(expiredFile, 512, 1024, time.Now().Add(-2*time.Hour))

	// Manually set the last access time to be old for the expired file
	manager.mutex.Lock()
	if pos, exists := manager.positions[expiredFile]; exists {
		pos.LastAccess = time.Now().Add(-2 * time.Hour)
	}
	manager.mutex.Unlock()

	// Run cleanup
	cleaned := manager.cleanupExpiredPositions()

	assert.Equal(t, 1, cleaned, "Should clean up 1 expired position")

	// Verify fresh position still exists
	_, _, _, exists := manager.GetPosition(freshFile)
	assert.True(t, exists, "Fresh position should still exist")

	// Verify expired position was removed
	_, _, _, exists = manager.GetPosition(expiredFile)
	assert.False(t, exists, "Expired position should be removed")
}

func TestPositionManager_CreateBackup(t *testing.T) {
	tempDir := os.TempDir()
	positionsFile := filepath.Join(tempDir, "test_backup_positions.json")
	defer os.Remove(positionsFile)

	config := Config{
		Enabled:     true,
		FilePath:    positionsFile,
		BackupCount: 3,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Set a position and save
	manager.SetPosition("/var/log/test.log", 1024, 2048, time.Now())
	err := manager.savePositions()
	require.NoError(t, err)

	// Create backup
	err = manager.createBackup()
	require.NoError(t, err)

	// Verify backup file exists
	backupFile := positionsFile + ".1"
	_, err = os.Stat(backupFile)
	assert.NoError(t, err, "Backup file should exist")
	defer os.Remove(backupFile)

	// Verify backup content matches original
	originalContent, err := os.ReadFile(positionsFile)
	require.NoError(t, err)

	backupContent, err := os.ReadFile(backupFile)
	require.NoError(t, err)

	assert.Equal(t, originalContent, backupContent, "Backup content should match original")
}

func TestPositionManager_AutoSave(t *testing.T) {
	tempDir := os.TempDir()
	positionsFile := filepath.Join(tempDir, "test_autosave_positions.json")
	defer os.Remove(positionsFile)

	config := Config{
		Enabled:      true,
		FilePath:     positionsFile,
		SaveInterval: 100 * time.Millisecond, // Fast interval for testing
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	manager := NewPositionManager(config, logger, ctx)

	// Start auto-save
	go manager.Start()

	// Set a position
	testFile := "/var/log/autosave.log"
	manager.SetPosition(testFile, 1024, 2048, time.Now())

	// Wait for auto-save to occur
	time.Sleep(300 * time.Millisecond)

	// Verify file was created and contains data
	_, err := os.Stat(positionsFile)
	assert.NoError(t, err, "Positions file should be created by auto-save")

	// Create new manager to verify data was saved
	manager2 := NewPositionManager(config, logger, context.Background())
	err = manager2.loadPositions()
	require.NoError(t, err)

	position, size, _, exists := manager2.GetPosition(testFile)
	assert.True(t, exists, "Position should be loaded from saved file")
	assert.Equal(t, int64(1024), position)
	assert.Equal(t, int64(2048), size)
}

func TestPositionManager_ConcurrentAccess(t *testing.T) {
	config := Config{
		Enabled:      true,
		FilePath:     "/tmp/test_concurrent_positions.json",
		SaveInterval: 10 * time.Millisecond,
		TTL:          1 * time.Hour,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	manager := NewPositionManager(config, logger, ctx)

	// Start background saving
	go manager.Start()

	// Simulate concurrent access
	done := make(chan bool, 10)

	// Multiple goroutines setting positions
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				file := fmt.Sprintf("/var/log/concurrent_%d_%d.log", id, j)
				position := int64(id*100 + j)
				size := int64(position * 2)
				manager.SetPosition(file, position, size, time.Now())

				// Verify we can read it back
				readPos, readSize, _, exists := manager.GetPosition(file)
				assert.True(t, exists)
				assert.Equal(t, position, readPos)
				assert.Equal(t, size, readSize)
			}
		}(i)
	}

	// Multiple goroutines reading positions
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				file := fmt.Sprintf("/var/log/concurrent_%d_%d.log", id, j)
				// Just attempt to read (may or may not exist depending on timing)
				manager.GetPosition(file)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

func TestPositionManager_DisabledConfig(t *testing.T) {
	config := Config{
		Enabled: false, // Disabled
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Operations should work but not persist
	manager.SetPosition("/test.log", 1024, 2048, time.Now())

	position, size, _, exists := manager.GetPosition("/test.log")
	assert.True(t, exists)
	assert.Equal(t, int64(1024), position)
	assert.Equal(t, int64(2048), size)

	// Start should not panic when disabled
	go manager.Start()
	time.Sleep(100 * time.Millisecond)

	assert.True(t, true) // Test passes if no panic
}

func TestPositionManager_InvalidFilePath(t *testing.T) {
	config := Config{
		Enabled:  true,
		FilePath: "/invalid/directory/positions.json", // Invalid path
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewPositionManager(config, logger, ctx)

	// Should handle invalid file path gracefully
	err := manager.savePositions()
	assert.Error(t, err, "Should return error for invalid file path")

	err = manager.loadPositions()
	assert.Error(t, err, "Should return error for invalid file path")
}