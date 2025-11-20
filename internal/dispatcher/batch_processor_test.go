package dispatcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"ssw-logs-capture/internal/metrics"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestNewBatchProcessor tests constructor validation
func TestNewBatchProcessor(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    100,
		BatchTimeout: 5 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	enhancedMetrics := metrics.NewEnhancedMetrics(logger)

	bp := NewBatchProcessor(config, logger, enhancedMetrics)

	require.NotNil(t, bp)
	assert.Equal(t, config.BatchSize, bp.config.BatchSize)
	assert.Equal(t, config.BatchTimeout, bp.config.BatchTimeout)
	assert.NotNil(t, bp.logger)
	assert.NotNil(t, bp.enhancedMetrics)
}

// TestBatchProcessor_CollectBatch_BatchSize tests batch collection until size limit
func TestBatchProcessor_CollectBatch_BatchSize(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    3,
		BatchTimeout: 10 * time.Second, // High timeout to ensure batch size triggers first
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	queue := make(chan dispatchItem, 10)

	// Add items to queue
	for i := 0; i < 5; i++ {
		entry := &types.LogEntry{
			Message: "test message",
			Labels:  make(map[string]string),
		}
		queue <- dispatchItem{Entry: entry, Retries: 0}
	}

	ctx := context.Background()
	batch, timedOut := bp.CollectBatch(ctx, queue)

	assert.False(t, timedOut, "Should not timeout when batch size is reached")
	assert.Len(t, batch, config.BatchSize, "Batch should contain exactly BatchSize items")
}

// TestBatchProcessor_CollectBatch_Timeout tests batch collection with timeout
func TestBatchProcessor_CollectBatch_Timeout(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    100, // Large batch size
		BatchTimeout: 100 * time.Millisecond, // Short timeout
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	queue := make(chan dispatchItem, 10)

	// Add only 2 items - less than batch size
	for i := 0; i < 2; i++ {
		entry := &types.LogEntry{
			Message: "test message",
			Labels:  make(map[string]string),
		}
		queue <- dispatchItem{Entry: entry, Retries: 0}
	}

	ctx := context.Background()
	batch, timedOut := bp.CollectBatch(ctx, queue)

	assert.True(t, timedOut, "Should timeout when batch size not reached")
	assert.Len(t, batch, 2, "Batch should contain all available items")
}

// TestBatchProcessor_CollectBatch_ContextCancellation tests cancellation handling
func TestBatchProcessor_CollectBatch_ContextCancellation(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    100,
		BatchTimeout: 10 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	queue := make(chan dispatchItem, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	batch, timedOut := bp.CollectBatch(ctx, queue)

	assert.False(t, timedOut)
	assert.Empty(t, batch, "Batch should be empty when context is cancelled")
}

// TestBatchProcessor_CollectBatch_EmptyQueue tests collection from empty queue
func TestBatchProcessor_CollectBatch_EmptyQueue(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 50 * time.Millisecond,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	queue := make(chan dispatchItem, 10) // Empty queue

	ctx := context.Background()
	batch, timedOut := bp.CollectBatch(ctx, queue)

	assert.True(t, timedOut, "Should timeout on empty queue")
	assert.Empty(t, batch, "Batch should be empty")
}

// TestBatchProcessor_ProcessBatch_Success tests successful batch processing
func TestBatchProcessor_ProcessBatch_Success(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	enhancedMetrics := metrics.NewEnhancedMetrics(logger)
	bp := NewBatchProcessor(config, logger, enhancedMetrics)

	// Create test batch
	batch := make([]dispatchItem, 3)
	for i := 0; i < 3; i++ {
		batch[i] = dispatchItem{
			Entry: &types.LogEntry{
				Message:    "test message",
				Labels:     map[string]string{"key": "value"},
				SourceType: "test",
				SourceID:   "test-source",
			},
			Retries: 0,
		}
	}

	// Create mock sink
	mockSink := &MockSink{}
	mockSink.On("IsHealthy").Return(true)
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)

	ctx := context.Background()
	successCount, healthySinks, lastErr := bp.ProcessBatch(ctx, batch, []types.Sink{mockSink}, nil)

	assert.Equal(t, 1, successCount, "Should have 1 successful sink")
	assert.Equal(t, 1, healthySinks, "Should have 1 healthy sink")
	assert.NoError(t, lastErr)
	mockSink.AssertExpectations(t)
}

// TestBatchProcessor_ProcessBatch_EmptyBatch tests empty batch handling
func TestBatchProcessor_ProcessBatch_EmptyBatch(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	batch := make([]dispatchItem, 0)
	mockSink := &MockSink{}

	ctx := context.Background()
	successCount, healthySinks, lastErr := bp.ProcessBatch(ctx, batch, []types.Sink{mockSink}, nil)

	assert.Equal(t, 0, successCount)
	assert.Equal(t, 0, healthySinks)
	assert.NoError(t, lastErr)
	mockSink.AssertNotCalled(t, "Send")
}

// TestBatchProcessor_ProcessBatch_ErrorHandling tests error handling
func TestBatchProcessor_ProcessBatch_ErrorHandling(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	batch := make([]dispatchItem, 1)
	batch[0] = dispatchItem{
		Entry: &types.LogEntry{
			Message: "test message",
			Labels:  make(map[string]string),
		},
		Retries: 0,
	}

	// Create mock sink that returns error
	mockSink := &MockSink{}
	mockSink.On("IsHealthy").Return(true)
	expectedErr := errors.New("sink error")
	mockSink.On("Send", mock.Anything, mock.Anything).Return(expectedErr)

	ctx := context.Background()
	successCount, healthySinks, lastErr := bp.ProcessBatch(ctx, batch, []types.Sink{mockSink}, nil)

	assert.Equal(t, 0, successCount, "Should have 0 successful sinks")
	assert.Equal(t, 1, healthySinks, "Should have 1 healthy sink attempted")
	assert.Error(t, lastErr)
	assert.Equal(t, expectedErr, lastErr)
	mockSink.AssertExpectations(t)
}

// TestBatchProcessor_ProcessBatch_MultiSink tests multiple sinks handling
func TestBatchProcessor_ProcessBatch_MultiSink(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	batch := make([]dispatchItem, 1)
	batch[0] = dispatchItem{
		Entry: &types.LogEntry{
			Message: "test message",
			Labels:  make(map[string]string),
		},
		Retries: 0,
	}

	// Create 3 mock sinks: 2 healthy, 1 unhealthy
	mockSink1 := &MockSink{}
	mockSink1.On("IsHealthy").Return(true)
	mockSink1.On("Send", mock.Anything, mock.Anything).Return(nil)

	mockSink2 := &MockSink{}
	mockSink2.On("IsHealthy").Return(true)
	mockSink2.On("Send", mock.Anything, mock.Anything).Return(nil)

	mockSink3 := &MockSink{}
	mockSink3.On("IsHealthy").Return(false) // Unhealthy sink

	sinks := []types.Sink{mockSink1, mockSink2, mockSink3}

	ctx := context.Background()
	successCount, healthySinks, lastErr := bp.ProcessBatch(ctx, batch, sinks, nil)

	assert.Equal(t, 2, successCount, "Should have 2 successful sinks")
	assert.Equal(t, 2, healthySinks, "Should have 2 healthy sinks")
	assert.NoError(t, lastErr)

	mockSink1.AssertExpectations(t)
	mockSink2.AssertExpectations(t)
	mockSink3.AssertNotCalled(t, "Send") // Unhealthy sink should not be called
}

// TestBatchProcessor_ProcessBatch_PartialSuccess tests partial success scenario
func TestBatchProcessor_ProcessBatch_PartialSuccess(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	batch := make([]dispatchItem, 1)
	batch[0] = dispatchItem{
		Entry: &types.LogEntry{
			Message: "test message",
			Labels:  make(map[string]string),
		},
		Retries: 0,
	}

	// Create 2 sinks: 1 success, 1 failure
	mockSink1 := &MockSink{}
	mockSink1.On("IsHealthy").Return(true)
	mockSink1.On("Send", mock.Anything, mock.Anything).Return(nil)

	mockSink2 := &MockSink{}
	mockSink2.On("IsHealthy").Return(true)
	expectedErr := errors.New("sink2 error")
	mockSink2.On("Send", mock.Anything, mock.Anything).Return(expectedErr)

	sinks := []types.Sink{mockSink1, mockSink2}

	ctx := context.Background()
	successCount, healthySinks, lastErr := bp.ProcessBatch(ctx, batch, sinks, nil)

	assert.Equal(t, 1, successCount, "Should have 1 successful sink")
	assert.Equal(t, 2, healthySinks, "Should have 2 healthy sinks attempted")
	assert.Error(t, lastErr, "Should return error from failed sink")
	assert.Equal(t, expectedErr, lastErr)

	mockSink1.AssertExpectations(t)
	mockSink2.AssertExpectations(t)
}

// TestBatchProcessor_ValidateBatch tests batch validation
func TestBatchProcessor_ValidateBatch(t *testing.T) {
	config := DispatcherConfig{
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	tests := []struct {
		name        string
		batchSize   int
		expectError bool
	}{
		{"empty batch", 0, true},
		{"valid batch", 5, false},
		{"batch at limit", 10, false},
		{"batch exceeds limit", 11, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := make([]dispatchItem, tt.batchSize)
			for i := 0; i < tt.batchSize; i++ {
				batch[i] = dispatchItem{
					Entry: &types.LogEntry{
						Message: "test",
						Labels:  make(map[string]string),
					},
				}
			}

			err := bp.ValidateBatch(batch)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBatchProcessor_DeepCopyBatch tests deep copy correctness
func TestBatchProcessor_DeepCopyBatch(t *testing.T) {
	original := []dispatchItem{
		{
			Entry: &types.LogEntry{
				Message: "original message",
				Labels:  map[string]string{"key1": "value1"},
			},
			Retries: 1,
		},
	}

	copied := deepCopyBatch(original)

	require.Len(t, copied, 1)
	assert.Equal(t, "original message", copied[0].Message)
	assert.Equal(t, "value1", copied[0].Labels["key1"])

	// Modify original - should not affect copy
	original[0].Entry.Message = "modified"
	original[0].Entry.Labels["key1"] = "modified"

	assert.Equal(t, "original message", copied[0].Message, "Copy should be independent")
	assert.Equal(t, "value1", copied[0].Labels["key1"], "Copy labels should be independent")
}

// TestBatchProcessor_DeepCopyEntries tests deep copy of entries
func TestBatchProcessor_DeepCopyEntries(t *testing.T) {
	original := []types.LogEntry{
		{
			Message: "original",
			Labels:  map[string]string{"key": "value"},
		},
	}

	copied := deepCopyEntries(original)

	require.Len(t, copied, 1)
	assert.Equal(t, "original", copied[0].Message)

	// Modify original
	original[0].Message = "modified"
	original[0].Labels["key"] = "modified"

	assert.Equal(t, "original", copied[0].Message, "Copy should be independent")
	assert.Equal(t, "value", copied[0].Labels["key"], "Copy labels should be independent")
}

// BenchmarkBatchProcessor_ProcessBatch benchmarks batch processing
func BenchmarkBatchProcessor_ProcessBatch(b *testing.B) {
	config := DispatcherConfig{
		BatchSize:    100,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	batch := make([]dispatchItem, 100)
	for i := 0; i < 100; i++ {
		batch[i] = dispatchItem{
			Entry: &types.LogEntry{
				Message: "benchmark message",
				Labels:  map[string]string{"key": "value"},
			},
			Retries: 0,
		}
	}

	mockSink := &MockSink{}
	mockSink.On("IsHealthy").Return(true)
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bp.ProcessBatch(ctx, batch, []types.Sink{mockSink}, nil)
	}
}

// BenchmarkBatchProcessor_CollectBatch benchmarks batch collection
func BenchmarkBatchProcessor_CollectBatch(b *testing.B) {
	config := DispatcherConfig{
		BatchSize:    100,
		BatchTimeout: 1 * time.Second,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	bp := NewBatchProcessor(config, logger, nil)

	queue := make(chan dispatchItem, 1000)
	for i := 0; i < 1000; i++ {
		queue <- dispatchItem{
			Entry: &types.LogEntry{
				Message: "benchmark",
				Labels:  make(map[string]string),
			},
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bp.CollectBatch(ctx, queue)

		// Refill queue if needed
		if len(queue) < 100 {
			for j := 0; j < 100; j++ {
				queue <- dispatchItem{
					Entry: &types.LogEntry{
						Message: "benchmark",
						Labels:  make(map[string]string),
					},
				}
			}
		}
	}
}

// BenchmarkDeepCopyBatch benchmarks deep copy operation
func BenchmarkDeepCopyBatch(b *testing.B) {
	batch := make([]dispatchItem, 100)
	for i := 0; i < 100; i++ {
		batch[i] = dispatchItem{
			Entry: &types.LogEntry{
				Message: "benchmark message",
				Labels:  map[string]string{"key1": "value1", "key2": "value2"},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = deepCopyBatch(batch)
	}
}
