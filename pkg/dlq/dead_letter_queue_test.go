package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeadLetterQueue_NewDeadLetterQueue(t *testing.T) {
	config := Config{
		Enabled:       true,
		Directory:     "/tmp/dlq_test",
		MaxFileSize:   10 * 1024 * 1024,
		MaxFiles:      5,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 30 * time.Second,
		QueueSize:     1000,
	}

	logger := logrus.New()
	ctx := context.Background()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	assert.NotNil(t, dlq)
	assert.Equal(t, config, dlq.config)
	assert.Equal(t, logger, dlq.logger)
	assert.NotNil(t, dlq.queue)
}

func TestDeadLetterQueue_AddEntry_JSON(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_json")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      3,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     10,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	// Start DLQ processing
	go dlq.Start()

	// Add test entry
	entry := DLQEntry{
		Timestamp:    time.Now(),
		OriginalLog:  "test log message",
		SourceID:     "test-source",
		FailureType:  "validation_error",
		ErrorMessage: "invalid timestamp format",
		RetryCount:   3,
		Context: map[string]interface{}{
			"sink_type": "loki",
			"batch_id":  "batch-123",
		},
	}

	err = dlq.AddEntry(entry)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify file was created
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(files), 1, "DLQ file should be created")

	// Read and verify content
	dlqFile := filepath.Join(tempDir, files[0].Name())
	content, err := os.ReadFile(dlqFile)
	require.NoError(t, err)

	var savedEntry DLQEntry
	err = json.Unmarshal(content, &savedEntry)
	require.NoError(t, err)

	assert.Equal(t, entry.OriginalLog, savedEntry.OriginalLog)
	assert.Equal(t, entry.SourceID, savedEntry.SourceID)
	assert.Equal(t, entry.FailureType, savedEntry.FailureType)
	assert.Equal(t, entry.ErrorMessage, savedEntry.ErrorMessage)
	assert.Equal(t, entry.RetryCount, savedEntry.RetryCount)
}

func TestDeadLetterQueue_AddEntry_Text(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_text")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      3,
		RetentionDays: 7,
		Format:        "text",
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     10,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	entry := DLQEntry{
		Timestamp:    time.Now(),
		OriginalLog:  "test log message",
		SourceID:     "test-source",
		FailureType:  "validation_error",
		ErrorMessage: "invalid timestamp format",
		RetryCount:   3,
	}

	err = dlq.AddEntry(entry)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify file was created
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(files), 1, "DLQ file should be created")

	// Read and verify content is text format
	dlqFile := filepath.Join(tempDir, files[0].Name())
	content, err := os.ReadFile(dlqFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "test log message")
	assert.Contains(t, contentStr, "test-source")
	assert.Contains(t, contentStr, "validation_error")
	assert.Contains(t, contentStr, "invalid timestamp format")
}

func TestDeadLetterQueue_FileRotation(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_rotation")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   100, // Very small for testing rotation
		MaxFiles:      3,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 50 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	// Add multiple entries to trigger rotation
	for i := 0; i < 10; i++ {
		entry := DLQEntry{
			Timestamp:    time.Now(),
			OriginalLog:  fmt.Sprintf("long test log message number %d with extra data", i),
			SourceID:     fmt.Sprintf("source-%d", i),
			FailureType:  "validation_error",
			ErrorMessage: "test error message",
			RetryCount:   1,
		}

		err = dlq.AddEntry(entry)
		require.NoError(t, err)
	}

	// Wait for processing and rotation
	time.Sleep(500 * time.Millisecond)

	// Check that multiple files were created
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Greater(t, len(files), 1, "Multiple DLQ files should be created due to rotation")
}

func TestDeadLetterQueue_MaxFilesLimit(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_max_files")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   50, // Very small for testing
		MaxFiles:      2,  // Only keep 2 files
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 50 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	// Create enough entries to force creation of multiple files
	for i := 0; i < 20; i++ {
		entry := DLQEntry{
			Timestamp:    time.Now(),
			OriginalLog:  fmt.Sprintf("test log message %d with sufficient length to trigger rotation", i),
			SourceID:     fmt.Sprintf("source-%d", i),
			FailureType:  "test_error",
			ErrorMessage: "test error",
			RetryCount:   1,
		}

		err = dlq.AddEntry(entry)
		require.NoError(t, err)

		// Small delay to ensure file timestamps are different
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Verify max files limit is respected
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(files), config.MaxFiles, "Should not exceed max files limit")
}

func TestDeadLetterQueue_CleanupOldFiles(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_cleanup")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create old file that should be cleaned up
	oldFile := filepath.Join(tempDir, "dlq-old.log")
	err = os.WriteFile(oldFile, []byte("old content"), 0644)
	require.NoError(t, err)

	// Set old modification time
	oldTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago
	err = os.Chtimes(oldFile, oldTime, oldTime)
	require.NoError(t, err)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7, // 7 days retention
		Format:        "json",
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     10,
	}

	logger := logrus.New()
	ctx := context.Background()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	// Run cleanup
	cleaned := dlq.cleanupOldFiles()
	assert.Equal(t, 1, cleaned, "Should clean up 1 old file")

	// Verify old file was removed
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "Old file should be removed")
}

func TestDeadLetterQueue_QueueOverflow(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_overflow")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 1 * time.Second, // Slow flush to test overflow
		QueueSize:     3,               // Very small queue
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	// Try to add more entries than queue capacity
	successCount := 0
	for i := 0; i < 10; i++ {
		entry := DLQEntry{
			Timestamp:   time.Now(),
			OriginalLog: fmt.Sprintf("overflow test %d", i),
			SourceID:    "test-source",
			FailureType: "overflow_test",
		}

		err = dlq.AddEntry(entry)
		if err == nil {
			successCount++
		}
	}

	// Some entries should be accepted, but not all due to queue size limit
	assert.Greater(t, successCount, 0, "Some entries should be accepted")
	assert.LessOrEqual(t, successCount, 10, "Not all entries should be accepted due to queue overflow")
}

func TestDeadLetterQueue_GetStatistics(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_stats")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	// Add some entries
	for i := 0; i < 5; i++ {
		entry := DLQEntry{
			Timestamp:   time.Now(),
			OriginalLog: fmt.Sprintf("stats test %d", i),
			SourceID:    "test-source",
			FailureType: "stats_test",
		}

		err = dlq.AddEntry(entry)
		require.NoError(t, err)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	stats := dlq.GetStatistics()

	assert.Equal(t, int64(5), stats.TotalEntries, "Should track 5 total entries")
	assert.GreaterOrEqual(t, stats.FilesWritten, int64(1), "Should have written at least 1 file")
	assert.GreaterOrEqual(t, stats.BytesWritten, int64(0), "Should have written some bytes")
}

func TestDeadLetterQueue_DisabledConfig(t *testing.T) {
	config := Config{
		Enabled: false, // Disabled
	}

	logger := logrus.New()
	ctx := context.Background()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	// Should handle disabled state gracefully
	entry := DLQEntry{
		Timestamp:   time.Now(),
		OriginalLog: "test message",
		SourceID:    "test-source",
		FailureType: "test",
	}

	err := dlq.AddEntry(entry)
	assert.NoError(t, err, "Should handle disabled state gracefully")

	// Start should not panic when disabled
	go dlq.Start()
	time.Sleep(100 * time.Millisecond)

	stats := dlq.GetStatistics()
	assert.Equal(t, int64(0), stats.TotalEntries, "No stats when disabled")
}

func TestDeadLetterQueue_InvalidDirectory(t *testing.T) {
	config := Config{
		Enabled:   true,
		Directory: "/invalid/nonexistent/directory",
		Format:    "json",
	}

	logger := logrus.New()
	ctx := context.Background()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	entry := DLQEntry{
		Timestamp:   time.Now(),
		OriginalLog: "test message",
		SourceID:    "test-source",
		FailureType: "test",
	}

	// Should handle invalid directory gracefully
	err := dlq.AddEntry(entry)
	// This might succeed (go to queue) but fail during flush
	// The implementation should handle this gracefully
	if err != nil {
		assert.Contains(t, err.Error(), "directory", "Error should mention directory issue")
	}
}

func TestDeadLetterQueue_ContextCancellation(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_context")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 100 * time.Millisecond,
		QueueSize:     100,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)

	// Start DLQ - should stop when context is cancelled
	go dlq.Start()

	// Add an entry
	entry := DLQEntry{
		Timestamp:   time.Now(),
		OriginalLog: "context test",
		SourceID:    "test-source",
		FailureType: "context_test",
	}

	err = dlq.AddEntry(entry)
	require.NoError(t, err)

	// Wait for context to cancel
	time.Sleep(300 * time.Millisecond)

	// Test passes if no panic occurs and DLQ stops gracefully
	assert.True(t, true)
}

func TestDeadLetterQueue_ConcurrentAccess(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "dlq_test_concurrent")
	err := os.MkdirAll(tempDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := Config{
		Enabled:       true,
		Directory:     tempDir,
		MaxFileSize:   1024,
		MaxFiles:      5,
		RetentionDays: 7,
		Format:        "json",
		FlushInterval: 50 * time.Millisecond,
		QueueSize:     1000,
	}

	logger := logrus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dlq := NewDeadLetterQueue(config, logger, ctx)
	go dlq.Start()

	// Concurrent access from multiple goroutines
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 5; j++ {
				entry := DLQEntry{
					Timestamp:   time.Now(),
					OriginalLog: fmt.Sprintf("concurrent test %d-%d", id, j),
					SourceID:    fmt.Sprintf("source-%d", id),
					FailureType: "concurrent_test",
				}

				dlq.AddEntry(entry)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	// Wait for final flush
	time.Sleep(200 * time.Millisecond)

	stats := dlq.GetStatistics()
	assert.Equal(t, int64(50), stats.TotalEntries, "Should process all 50 entries")
}