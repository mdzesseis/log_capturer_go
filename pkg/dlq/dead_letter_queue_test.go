package dlq

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CRITICAL PATH TESTS (Task 3 - Priority 4)
// =============================================================================

func TestDLQ_AddEntry_Success(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_add_success")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		JSONFormat:    true,
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)
	require.NotNil(t, dlq)

	err = dlq.Start()
	require.NoError(t, err)
	defer dlq.Stop()

	// Create test entry using correct API
	testEntry := &types.LogEntry{
		Message:    "test log entry for DLQ",
		SourceType: "file",
		SourceID:   "test-file-1",
		Timestamp:  time.Now(),
		Labels: types.NewLabelsCOWFromMap(map[string]string{
			"level": "error",
		}),
	}

	contextMap := map[string]string{
		"test_key": "test_value",
		"retry_count": "1",
	}

	// Add entry using correct AddEntry signature (ponteiro)
	err = dlq.AddEntry(testEntry, "test error message", "test_error_type", "local_file", 1, contextMap)
	require.NoError(t, err, "Should successfully add entry to DLQ")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify stats
	stats := dlq.GetStats()
	assert.Equal(t, int64(1), stats.TotalEntries, "Should have 1 total entry")
	assert.Equal(t, int64(1), stats.EntriesWritten, "Should have 1 entry written")

	// Verify file exists
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have created DLQ file")
}

func TestDLQ_AddEntry_Concurrent(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_concurrent_add")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   10240,
		MaxFiles:      5,
		RetentionDays: 7,
		JSONFormat:    true,
		FlushInterval: 50 * time.Millisecond,
		QueueSize:     1000,
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)

	err = dlq.Start()
	require.NoError(t, err)
	defer dlq.Stop()

	// Run with race detector: go test -race
	var wg sync.WaitGroup
	numGoroutines := 100
	entriesPerGoroutine := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				entry := &types.LogEntry{
					Message:    fmt.Sprintf("concurrent entry %d-%d", id, j),
					SourceType: "test",
					SourceID:   fmt.Sprintf("test-%d", id),
					Timestamp:  time.Now(),
					Labels: types.NewLabelsCOWFromMap(map[string]string{
						"goroutine_id": fmt.Sprintf("%d", id),
					}),
				}

				err := dlq.AddEntry(entry, "concurrent test error", "concurrent_test", "test_sink", 0, nil)
				// Note: Some entries might fail if queue is full, which is expected behavior
				if err != nil {
					t.Logf("Entry %d-%d failed to add (queue full): %v", id, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify stats - should have processed many entries
	stats := dlq.GetStats()
	assert.Greater(t, stats.TotalEntries, int64(0), "Should have processed entries")
	t.Logf("Processed %d entries concurrently", stats.TotalEntries)
}

func TestDLQ_FileRotation_Basic(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_file_rotation")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1, // 1MB - small for testing
		MaxFiles:      5,
		RetentionDays: 7,
		JSONFormat:    true,
		FlushInterval: 50 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)

	err = dlq.Start()
	require.NoError(t, err)
	defer dlq.Stop()

	// Add enough entries to trigger file rotation
	for i := 0; i < 50; i++ {
		entry := &types.LogEntry{
			Message:    fmt.Sprintf("Large message to force rotation %d - %s", i, strings.Repeat("x", 1000)),
			SourceType: "test",
			SourceID:   fmt.Sprintf("test-%d", i),
			Timestamp:  time.Now(),
		}

		err := dlq.AddEntry(entry, "rotation test error", "rotation_test", "test_sink", 0, nil)
		require.NoError(t, err)
	}

	// Wait for processing and rotation
	time.Sleep(1 * time.Second)

	// Check that multiple files were created
	files, err := filepath.Glob(filepath.Join(tempDir, "dlq_*.log"))
	require.NoError(t, err)
	assert.Greater(t, len(files), 1, "Should have created multiple files due to rotation")

	stats := dlq.GetStats()
	t.Logf("Created %d files, total entries: %d", stats.FilesCreated, stats.TotalEntries)
}

func TestDLQ_Cleanup_OldFiles(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_cleanup_old")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create some old files manually
	oldFile1 := filepath.Join(tempDir, "dlq_20200101_120000.log")
	oldFile2 := filepath.Join(tempDir, "dlq_20200102_120000.log")

	err = os.WriteFile(oldFile1, []byte("old content 1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(oldFile2, []byte("old content 2"), 0644)
	require.NoError(t, err)

	// Set old modification times (older than retention)
	oldTime := time.Now().AddDate(0, 0, -10) // 10 days ago
	err = os.Chtimes(oldFile1, oldTime, oldTime)
	require.NoError(t, err)
	err = os.Chtimes(oldFile2, oldTime, oldTime)
	require.NoError(t, err)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   10,
		MaxFiles:      5,
		RetentionDays: 7, // 7 days retention
		JSONFormat:    true,
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     10,
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)

	err = dlq.Start()
	require.NoError(t, err)

	// Wait a bit for cleanup to potentially run
	time.Sleep(200 * time.Millisecond)

	// Manually trigger cleanup
	dlq.cleanupOldFiles()

	err = dlq.Stop()
	require.NoError(t, err)

	// Verify old files were removed
	_, err = os.Stat(oldFile1)
	assert.True(t, os.IsNotExist(err), "Old file 1 should be removed")

	_, err = os.Stat(oldFile2)
	assert.True(t, os.IsNotExist(err), "Old file 2 should be removed")
}

func TestDLQ_Reprocess_Success(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_reprocess")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   10,
		MaxFiles:      5,
		RetentionDays: 7,
		JSONFormat:    true,
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     100,
		ReprocessingConfig: ReprocessingConfig{
			Enabled:      true,
			Interval:     1 * time.Second,
			MaxRetries:   3,
			InitialDelay: 100 * time.Millisecond,
			MinEntryAge:  100 * time.Millisecond,
		},
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)

	// Track reprocessed entries
	reprocessedCount := 0
	var reprocessMutex sync.Mutex

	// Set callback that succeeds
	dlq.SetReprocessCallback(func(entry *types.LogEntry, originalSink string) error {
		reprocessMutex.Lock()
		reprocessedCount++
		reprocessMutex.Unlock()
		return nil // Success
	})

	err = dlq.Start()
	require.NoError(t, err)
	defer dlq.Stop()

	// Add test entries
	for i := 0; i < 5; i++ {
		entry := &types.LogEntry{
			Message:    fmt.Sprintf("reprocess test %d", i),
			SourceType: "test",
			SourceID:   fmt.Sprintf("test-%d", i),
			Timestamp:  time.Now(),
		}

		err := dlq.AddEntry(entry, "reprocess test error", "reprocess_test", "test_sink", 0, nil)
		require.NoError(t, err)
	}

	// Wait for initial write
	time.Sleep(300 * time.Millisecond)

	// Wait for reprocessing to occur
	time.Sleep(2 * time.Second)

	// Check stats
	stats := dlq.GetStats()
	t.Logf("Reprocessing stats: attempts=%d, successes=%d, failures=%d",
		stats.ReprocessingAttempts, stats.ReprocessingSuccesses, stats.ReprocessingFailures)

	// With automatic reprocessing, we should see some activity
	assert.Greater(t, stats.ReprocessingAttempts, int64(0), "Should have reprocessing attempts")
}

// =============================================================================
// ADDITIONAL TESTS
// =============================================================================

func TestDLQ_Disabled(t *testing.T) {
	config := Config{
		Enabled: false, // Disabled
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)
	require.NotNil(t, dlq)

	// Should handle disabled state gracefully
	entry := &types.LogEntry{
		Message:   "test message",
		Timestamp: time.Now(),
	}

	err := dlq.AddEntry(entry, "test error", "test_type", "test_sink", 1, nil)
	assert.NoError(t, err, "Should handle disabled state gracefully")

	// Start should not panic when disabled
	err = dlq.Start()
	assert.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	dlq.Stop()

	stats := dlq.GetStats()
	assert.Equal(t, int64(0), stats.TotalEntries, "No entries when disabled")
}

func TestDLQ_QueueFull(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_queue_full")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		JSONFormat:    true,
		FlushInterval: 5 * time.Second, // Slow flush to test overflow
		QueueSize:     3,                // Very small queue
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dlq := NewDeadLetterQueue(config, logger)
	err = dlq.Start()
	require.NoError(t, err)
	defer dlq.Stop()

	// Try to add more entries than queue capacity
	successCount := 0
	failCount := 0

	for i := 0; i < 10; i++ {
		entry := &types.LogEntry{
			Message:   fmt.Sprintf("queue overflow test %d", i),
			Timestamp: time.Now(),
		}

		err = dlq.AddEntry(entry, "overflow test", "overflow_test", "test_sink", 1, nil)
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}

	t.Logf("Success: %d, Failed: %d", successCount, failCount)

	// Some entries should be accepted, others might fail due to queue overflow
	assert.Greater(t, successCount, 0, "Some entries should be accepted")
}
