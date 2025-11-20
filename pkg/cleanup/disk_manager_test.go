package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskSpaceManager_NewDiskSpaceManager(t *testing.T) {
	config := Config{
		CheckInterval: 30 * time.Second,
		Directories: []DirectoryConfig{
			{
				Path:          "/tmp/test",
				MaxSizeMB:     100,
				MaxFiles:      10,
				RetentionDays: 7,
				FilePatterns:  []string{"*.log"},
			},
		},
	}

	logger := logrus.New()

	manager := NewDiskSpaceManager(config, logger)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.Equal(t, logger, manager.logger)
}

func TestDiskSpaceManager_CleanupByAge(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "disk_manager_test_age")
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	// Create test files with different ages
	oldFile := filepath.Join(testDir, "old.log")
	newFile := filepath.Join(testDir, "new.log")

	// Create old file (modify its mtime to be old)
	f1, err := os.Create(oldFile)
	require.NoError(t, err)
	f1.Close()

	oldTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago
	err = os.Chtimes(oldFile, oldTime, oldTime)
	require.NoError(t, err)

	// Create new file
	f2, err := os.Create(newFile)
	require.NoError(t, err)
	f2.Close()

	config := Config{
		CheckInterval: 1 * time.Second,
		Directories: []DirectoryConfig{
			{
				Path:         testDir,
				RetentionDays: 7, // 7 days retention
				FilePatterns: []string{"*.log"},
			},
		},
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager := NewDiskSpaceManager(config, logger)

	// Run cleanup
	err = manager.cleanupByAge(config.Directories[0])
	require.NoError(t, err)

	// Verify old file was deleted, new file remains
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "Old file should be deleted")

	_, err = os.Stat(newFile)
	assert.NoError(t, err, "New file should still exist")
}

func TestDiskSpaceManager_CleanupByCount(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "disk_manager_test_count")
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	// Create multiple test files
	files := []string{"file1.log", "file2.log", "file3.log", "file4.log", "file5.log"}
	for i, filename := range files {
		filePath := filepath.Join(testDir, filename)
		f, err := os.Create(filePath)
		require.NoError(t, err)
		f.Close()

		// Set different modification times to ensure predictable cleanup order
		modTime := time.Now().Add(-time.Duration(i) * time.Hour)
		err = os.Chtimes(filePath, modTime, modTime)
		require.NoError(t, err)
	}

	config := Config{
		CheckInterval: 1 * time.Second,
		Directories: []DirectoryConfig{
			{
				Path:         testDir,
				MaxFiles:     3, // Keep only 3 files
				FilePatterns: []string{"*.log"},
			},
		},
	}

	logger := logrus.New()

	manager := NewDiskSpaceManager(config, logger)

	// Run cleanup
	err = manager.cleanupByCount(config.Directories[0])
	require.NoError(t, err)

	// Count remaining files
	entries, err := os.ReadDir(testDir)
	require.NoError(t, err)

	logFiles := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".log" {
			logFiles++
		}
	}

	assert.Equal(t, 3, logFiles, "Should keep exactly 3 files")
}

func TestDiskSpaceManager_CleanupBySize(t *testing.T) {
	// Setup test directory
	testDir := filepath.Join(os.TempDir(), "disk_manager_test_size")
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	// Create test files with known sizes
	files := []string{"file1.log", "file2.log", "file3.log"}
	for i, filename := range files {
		filePath := filepath.Join(testDir, filename)
		f, err := os.Create(filePath)
		require.NoError(t, err)

		// Write some data (each file ~1KB)
		data := make([]byte, 1024)
		for j := range data {
			data[j] = byte('A' + i)
		}
		_, err = f.Write(data)
		require.NoError(t, err)
		f.Close()

		// Set different modification times
		modTime := time.Now().Add(-time.Duration(i) * time.Hour)
		err = os.Chtimes(filePath, modTime, modTime)
		require.NoError(t, err)
	}

	config := Config{
		CheckInterval: 1 * time.Second,
		Directories: []DirectoryConfig{
			{
				Path:         testDir,
				MaxSizeMB:    0.002, // ~2KB limit (should keep 2 files)
				FilePatterns: []string{"*.log"},
			},
		},
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewDiskSpaceManager(config, logger)

	// Run cleanup
	err = manager.cleanupBySize(config.Directories[0])
	require.NoError(t, err)

	// Count remaining files
	entries, err := os.ReadDir(testDir)
	require.NoError(t, err)

	logFiles := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".log" {
			logFiles++
		}
	}

	assert.LessOrEqual(t, logFiles, 2, "Should keep at most 2 files within size limit")
}

func TestDiskSpaceManager_GetDiskSpace(t *testing.T) {
	config := Config{
		CheckInterval: 1 * time.Second,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewDiskSpaceManager(config, logger)

	// Test with a valid path
	freeBytes, totalBytes, err := manager.getDiskSpace("/tmp")
	require.NoError(t, err)
	assert.Greater(t, freeBytes, uint64(0))
	assert.Greater(t, totalBytes, uint64(0))
	assert.LessOrEqual(t, freeBytes, totalBytes)

	// Test with invalid path
	_, _, err = manager.getDiskSpace("/nonexistent/path")
	assert.Error(t, err)
}

func TestDiskSpaceManager_MatchesPattern(t *testing.T) {
	config := Config{}
	logger := logrus.New()
	ctx := context.Background()

	manager := NewDiskSpaceManager(config, logger)

	testCases := []struct {
		filename string
		patterns []string
		expected bool
	}{
		{"test.log", []string{"*.log"}, true},
		{"test.txt", []string{"*.log"}, false},
		{"test.log", []string{"*.log", "*.txt"}, true},
		{"test.txt", []string{"*.log", "*.txt"}, true},
		{"app.log.2023", []string{"*.log*"}, true},
		{"nopattern", []string{}, false},
	}

	for _, tc := range testCases {
		result := manager.matchesPattern(tc.filename, tc.patterns)
		assert.Equal(t, tc.expected, result, "Pattern matching failed for %s with patterns %v", tc.filename, tc.patterns)
	}
}

func TestDiskSpaceManager_Start_Stop(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "disk_manager_test_lifecycle")
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	config := Config{
		CheckInterval: 100 * time.Millisecond, // Fast cleanup for testing
		Directories: []DirectoryConfig{
			{
				Path:         testDir,
				MaxFiles:     5,
				FilePatterns: []string{"*.log"},
			},
		},
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	manager := NewDiskSpaceManager(config, logger)

	// Start manager
	go manager.Start()

	// Give it some time to run
	time.Sleep(300 * time.Millisecond)

	// Create some test files during operation
	for i := 0; i < 3; i++ {
		filePath := filepath.Join(testDir, "test"+string(rune('0'+i))+".log")
		f, err := os.Create(filePath)
		require.NoError(t, err)
		f.Close()
	}

	// Wait a bit more for cleanup to potentially run
	time.Sleep(300 * time.Millisecond)

	// Stop via context cancellation
	cancel()

	// Verify files still exist (should be under limit)
	entries, err := os.ReadDir(testDir)
	require.NoError(t, err)

	logFiles := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".log" {
			logFiles++
		}
	}

	assert.Equal(t, 3, logFiles, "All files should remain (under limit)")
}

func TestDiskSpaceManager_DisabledConfig(t *testing.T) {
	config := Config{
		CheckInterval: 1 * time.Second,
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewDiskSpaceManager(config, logger)

	// Should not panic or error when disabled
	go manager.Start()

	// Give it some time
	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic occurs
	assert.True(t, true)
}

func TestDiskSpaceManager_EmptyDirectories(t *testing.T) {
	config := Config{
		CheckInterval: 1 * time.Second,
		Directories: []DirectoryConfig{}, // Empty directories
	}

	logger := logrus.New()
	ctx := context.Background()

	manager := NewDiskSpaceManager(config, logger)

	// Should not panic with empty directories
	go manager.Start()

	time.Sleep(100 * time.Millisecond)

	// Test passes if no panic occurs
	assert.True(t, true)
}