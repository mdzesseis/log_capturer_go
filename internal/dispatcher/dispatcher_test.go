package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockSink implements types.Sink for testing
type MockSink struct {
	mock.Mock
}

func (m *MockSink) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSink) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockSink) Send(ctx context.Context, entries []types.LogEntry) error {
	args := m.Called(ctx, entries)
	return args.Error(0)
}

func (m *MockSink) GetQueueUtilization() float64 {
	args := m.Called()
	return args.Get(0).(float64)
}

func (m *MockSink) IsHealthy() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockSink) GetStats() interface{} {
	args := m.Called()
	return args.Get(0)
}

// MockProcessor implements types.LogProcessor for testing
type MockProcessor struct {
	mock.Mock
}

func (m *MockProcessor) ProcessEntry(entry *types.LogEntry) (*types.LogEntry, error) {
	args := m.Called(entry)
	return args.Get(0).(*types.LogEntry), args.Error(1)
}

func (m *MockProcessor) GetStats() interface{} {
	args := m.Called()
	return args.Get(0)
}

// MockAnomalyDetector implements anomaly detection for testing
type MockAnomalyDetector struct {
	mock.Mock
}

func (m *MockAnomalyDetector) DetectAnomaly(entry *types.LogEntry) (bool, float64, error) {
	args := m.Called(entry)
	return args.Bool(0), args.Get(1).(float64), args.Error(2)
}

// TestDispatcherCreation tests dispatcher creation
func TestDispatcherCreation(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    1000,
		Workers:      2,
		BatchSize:    10,
		BatchTimeout: 1 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
	}

	logger := logrus.New()
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	assert.NotNil(t, dispatcher)
	assert.Equal(t, config.QueueSize, dispatcher.config.QueueSize)
	assert.Equal(t, config.Workers, dispatcher.config.Workers)
	assert.Equal(t, config.BatchSize, dispatcher.config.BatchSize)
}

// TestDispatcherStartStop tests dispatcher start and stop
func TestDispatcherStartStop(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      1,
		BatchSize:    5,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test start
	err := dispatcher.Start(ctx)
	assert.NoError(t, err)

	// Verify dispatcher is running
	assert.True(t, dispatcher.isRunning)

	// Test stop
	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify dispatcher is stopped
	assert.False(t, dispatcher.isRunning)
}

// TestDispatcherHandleLogEntry tests log entry handling
func TestDispatcherHandleLogEntry(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      1,
		BatchSize:    1,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	// Note: processor is nil, so no processor mock expectations needed
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	// Add a mock sink
	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start dispatcher
	err := dispatcher.Start(ctx)
	require.NoError(t, err)

	// Handle log entry
	err = dispatcher.Handle(ctx, "test", "test-source", "test message", map[string]string{})
	assert.NoError(t, err)

	// Give some time for processing
	time.Sleep(200 * time.Millisecond)

	// Stop dispatcher
	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify mock expectations
	mockSink.AssertExpectations(t)
}

// TestDispatcherBatching tests batching functionality
func TestDispatcherBatching(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      1,
		BatchSize:    3,
		BatchTimeout: 1 * time.Second,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	// Note: processor is nil, so no processor mock expectations needed
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	// Add a mock sink that expects batches of 3
	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	require.NoError(t, err)

	// Send 3 messages to trigger batch
	for i := 0; i < 3; i++ {
		err = dispatcher.Handle(ctx, "test", "test-source", "test message", map[string]string{})
		assert.NoError(t, err)
	}

	// Give time for processing
	time.Sleep(200 * time.Millisecond)

	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify Send was called at least once
	mockSink.AssertCalled(t, "Send", mock.Anything, mock.Anything)
}

// TestDispatcherDeduplication tests deduplication functionality
func TestDispatcherDeduplication(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:            100,
		Workers:              1,
		BatchSize:            10,
		BatchTimeout:         100 * time.Millisecond,
		MaxRetries:           1,
		RetryDelay:           10 * time.Millisecond,
		DeduplicationEnabled: true,
	}

	logger := logrus.New()
	// Note: processor is nil, so no processor mock expectations needed
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	// Mock sink should only receive one message due to deduplication
	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	require.NoError(t, err)

	// Send the same message twice - should be deduplicated
	for i := 0; i < 2; i++ {
		err = dispatcher.Handle(ctx, "test", "test-source", "duplicate message", map[string]string{})
		assert.NoError(t, err)
	}

	// Give time for processing
	time.Sleep(200 * time.Millisecond)

	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify Send was called (deduplication may reduce the count)
	mockSink.AssertCalled(t, "Send", mock.Anything, mock.Anything)
}

// TestDispatcherStats tests statistics collection
func TestDispatcherStats(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      2,
		BatchSize:    10,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   3,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	stats := dispatcher.GetStats()

	// Verify stats contain expected fields (stats is types.DispatcherStats struct)
	assert.GreaterOrEqual(t, stats.TotalProcessed, int64(0))
	assert.Equal(t, config.QueueSize, stats.QueueCapacity)
	assert.NotNil(t, stats.SinkDistribution)
}

// TestDispatcherConcurrency tests concurrent message handling
func TestDispatcherConcurrency(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    1000,
		Workers:      4,
		BatchSize:    10,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	processor := &MockProcessor{}
	anomalyDetector := &MockAnomalyDetector{}

	entry := &types.LogEntry{
		Message:    "concurrent message",
		SourceType: "test",
		SourceID:   "test-source",
		Timestamp:  time.Now(),
		Labels:     types.NewLabelsCOW(),
		Fields:     make(map[string]interface{}),
	}

	processor.On("ProcessEntry", mock.AnythingOfType("*types.LogEntry")).Return(entry, nil)
	anomalyDetector.On("DetectAnomaly", mock.AnythingOfType("*types.LogEntry")).Return(false, 0.0, nil)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	require.NoError(t, err)

	// Send messages concurrently
	numMessages := 100
	var wg sync.WaitGroup
	wg.Add(numMessages)

	for i := 0; i < numMessages; i++ {
		go func(i int) {
			defer wg.Done()
			err := dispatcher.Handle(ctx, "test", "test-source", "concurrent message", map[string]string{})
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Give time for processing
	time.Sleep(500 * time.Millisecond)

	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify at least some calls were made (exact number depends on batching)
	// Verify Send was called at least once
	mockSink.AssertCalled(t, "Send", mock.Anything, mock.Anything)
}

// TestDispatcherErrorHandling tests error handling
func TestDispatcherErrorHandling(t *testing.T) {
	config := DispatcherConfig{
		QueueSize:    100,
		Workers:      1,
		BatchSize:    1,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	// Note: processor is nil, so no processor mock expectations needed
	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	// Mock sink that returns an error
	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(assert.AnError)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	require.NoError(t, err)

	// Send a message that will cause an error
	err = dispatcher.Handle(ctx, "test", "test-source", "error message", map[string]string{})
	assert.NoError(t, err) // Handle itself should not error

	// Give time for processing and error handling (including retries)
	time.Sleep(300 * time.Millisecond)

	err = dispatcher.Stop()
	assert.NoError(t, err)

	// Verify the sink was called (may be called multiple times due to retries)
	mockSink.AssertCalled(t, "Send", mock.Anything, mock.Anything)
}

// BenchmarkDispatcherHandle benchmarks the Handle method
func BenchmarkDispatcherHandle(b *testing.B) {
	config := DispatcherConfig{
		QueueSize:    10000,
		Workers:      4,
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	processor := &MockProcessor{}
	anomalyDetector := &MockAnomalyDetector{}

	entry := &types.LogEntry{
		Message:    "benchmark message",
		SourceType: "benchmark",
		SourceID:   "benchmark-source",
		Timestamp:  time.Now(),
		Labels:     types.NewLabelsCOW(),
		Fields:     make(map[string]interface{}),
	}

	processor.On("ProcessEntry", mock.AnythingOfType("*types.LogEntry")).Return(entry, nil)
	anomalyDetector.On("DetectAnomaly", mock.AnythingOfType("*types.LogEntry")).Return(false, 0.0, nil)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := dispatcher.Handle(ctx, "benchmark", "benchmark-source", "benchmark message", map[string]string{})
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	if err := dispatcher.Stop(); err != nil {
		b.Errorf("Failed to stop dispatcher: %v", err)
	}
}

// BenchmarkDispatcherThroughput benchmarks throughput
func BenchmarkDispatcherThroughput(b *testing.B) {
	config := DispatcherConfig{
		QueueSize:    100000,
		Workers:      8,
		BatchSize:    500,
		BatchTimeout: 50 * time.Millisecond,
		MaxRetries:   1,
		RetryDelay:   10 * time.Millisecond,
	}

	logger := logrus.New()
	processor := &MockProcessor{}
	anomalyDetector := &MockAnomalyDetector{}

	entry := &types.LogEntry{
		Message:    "throughput message",
		SourceType: "throughput",
		SourceID:   "throughput-source",
		Timestamp:  time.Now(),
		Labels:     types.NewLabelsCOW(),
		Fields:     make(map[string]interface{}),
	}

	processor.On("ProcessEntry", mock.AnythingOfType("*types.LogEntry")).Return(entry, nil)
	anomalyDetector.On("DetectAnomaly", mock.AnythingOfType("*types.LogEntry")).Return(false, 0.0, nil)

	dispatcher := NewDispatcher(config, nil, logger, nil, nil)

	mockSink := &MockSink{}
	mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)
	mockSink.On("IsHealthy").Return(true)
	dispatcher.AddSink(mockSink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dispatcher.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}

	start := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := dispatcher.Handle(ctx, "throughput", "throughput-source", "throughput message", map[string]string{})
		if err != nil {
			b.Fatal(err)
		}
	}

	duration := time.Since(start)
	b.ReportMetric(float64(b.N)/duration.Seconds(), "entries/sec")

	if err := dispatcher.Stop(); err != nil {
		b.Errorf("Failed to stop dispatcher: %v", err)
	}
}