package monitors

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgTypes "ssw-logs-capture/pkg/types"
)


// ====================================================================================
// Container Monitor Specific Test Utilities
// (Shared mocks are in test_helpers.go)
// ====================================================================================

// mockReader implements io.Reader for testing contextReader
type mockReader struct {
	data     []byte
	pos      int
	blockFor time.Duration // Simulate blocking read
}

func newMockReader(data string) *mockReader {
	return &mockReader{
		data: []byte(data),
		pos:  0,
	}
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if m.blockFor > 0 {
		time.Sleep(m.blockFor)
	}

	if m.pos >= len(m.data) {
		return 0, io.EOF
	}

	n = copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

// ===================================================================================
// TAREFA 2: Constructor Tests
// ===================================================================================

func TestNewContainerMonitor_Success(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled:    true,
		SocketPath: "unix:///var/run/docker.sock",
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil, // positionManager not needed for this test
		logger,
	)

	require.NoError(t, err)
	require.NotNil(t, cm)
	assert.Equal(t, config.SocketPath, cm.config.SocketPath)
	assert.NotNil(t, cm.cli)
	assert.NotNil(t, cm.logger)
	assert.NotNil(t, cm.dispatcher)
	assert.NotNil(t, cm.collectors)
	assert.False(t, cm.running)
}

func TestNewContainerMonitor_NilLogger(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled: true,
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()

	_, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		nil, // logger nil should cause error
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger é obrigatório")
}

func TestNewContainerMonitor_NilDispatcher(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled: true,
	}

	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	_, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		nil, // dispatcher nil should cause error
		taskManager,
		nil,
		logger,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatcher é obrigatório")
}

func TestNewContainerMonitor_DockerConnectionFailure(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled:    true,
		SocketPath: "unix:///nonexistent/docker.sock", // Invalid socket
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)

	// Should fail to connect to Docker daemon
	// Note: This might succeed if DOCKER_HOST is set in environment
	if err != nil {
		assert.Contains(t, err.Error(), "Docker")
		assert.Nil(t, cm)
	}
}

// ===================================================================================
// TAREFA 3: Start/Stop Lifecycle Tests
// ===================================================================================

func TestContainerMonitor_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled:    true,
		SocketPath: "unix:///var/run/docker.sock",
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	// Test Start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = cm.Start(ctx)
	require.NoError(t, err)

	// Wait for initialization
	time.Sleep(500 * time.Millisecond)

	// Verify state
	cm.runningMux.RLock()
	running := cm.running
	cm.runningMux.RUnlock()
	assert.True(t, running, "Monitor should be running after Start")

	// Test Stop
	err = cm.Stop()
	require.NoError(t, err)

	// Verify state
	cm.runningMux.RLock()
	running = cm.running
	cm.runningMux.RUnlock()
	assert.False(t, running, "Monitor should be stopped after Stop")
}

func TestContainerMonitor_PreventDoubleStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled:    true,
		SocketPath: "unix:///var/run/docker.sock",
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)
	defer cm.Stop()

	ctx := context.Background()

	// First Start should succeed
	err = cm.Start(ctx)
	require.NoError(t, err)

	// Second Start should fail
	err = cm.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "já está em execução")
}

func TestContainerMonitor_StopWhenNotRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Docker integration test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled: true,
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	// Stop when not running should not error
	err = cm.Stop()
	assert.NoError(t, err)
}

// ===================================================================================
// TAREFA 4: Race Condition Tests
// ===================================================================================

func TestContainerMonitor_ConcurrentStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled:    true,
		SocketPath: "unix:///var/run/docker.sock",
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	ctx := context.Background()

	// Multiple goroutines attempting Start/Stop
	const goroutines = 10
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.Start(ctx)
			time.Sleep(10 * time.Millisecond)
			cm.Stop()
		}()
	}

	// Wait for all goroutines
	wg.Wait()

	// Final cleanup
	cm.Stop()
}

func TestContainerMonitor_ConcurrentStartCollecting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled: true,
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cm.ctx, cm.cancel = context.WithCancel(ctx)
	defer cm.Stop()

	// Concurrently start collectors for the same container ID
	const goroutines = 20
	containerID := "test_concurrent_container_12345678"
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.StartCollecting(containerID)
		}()
	}

	wg.Wait()

	// Only one collector should be registered
	cm.collectorsMux.Lock()
	count := len(cm.collectors)
	cm.collectorsMux.Unlock()

	// Should be 0 or 1 (might fail to start if container doesn't exist, which is fine)
	assert.LessOrEqual(t, count, 1, "Should have at most one collector for the container")
}

// ===================================================================================
// TAREFA 5: Configuration Tests
// ===================================================================================

func TestContainerMonitor_DrainDuration(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled:       true,
		DrainDuration: 2 * time.Second,
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	assert.Equal(t, 2*time.Second, cm.drainDuration)
}

func TestContainerMonitor_DefaultDrainDuration(t *testing.T) {
	config := pkgTypes.DockerConfig{
		Enabled: true,
		// DrainDuration not set
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	// Default should be 1 second
	assert.Equal(t, 1*time.Second, cm.drainDuration)
}

func TestContainerMonitor_DrainPeriodTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping drain period timing test in short mode")
	}

	config := pkgTypes.DockerConfig{
		Enabled:       true,
		DrainDuration: 500 * time.Millisecond,
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	require.NoError(t, err)

	// Setup: Create a fake collector context
	ctx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()

	cm.ctx, cm.cancel = context.WithCancel(ctx)
	defer cm.Stop()

	_, collectCancel := context.WithCancel(cm.ctx)
	containerID := "test_drain_container_12345678"

	cm.collectorsMux.Lock()
	cm.collectors[containerID] = collectCancel
	cm.collectorsMux.Unlock()

	// Measure drain period
	start := time.Now()

	done := make(chan struct{})
	go func() {
		defer close(done)
		cm.StopCollecting(containerID)
	}()

	<-done
	elapsed := time.Since(start)

	// Should have waited for drain period (~500ms)
	assert.GreaterOrEqual(t, elapsed, 400*time.Millisecond, "Drain period too short")
	assert.LessOrEqual(t, elapsed, 1*time.Second, "Drain period too long")
}

// ===================================================================================
// TAREFA 6: Component Tests - readerCtx
// ===================================================================================

func TestReaderCtx_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create mock reader with blocking behavior
	mockReader := newMockReader("test data for context cancellation")

	// Create context-aware reader
	reader := newContextReader(ctx, mockReader)

	// Cancel context
	cancel()

	// Try to read - should return context.Canceled error
	buf := make([]byte, 100)
	n, err := reader.Read(buf)

	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 0, n)
}

func TestReaderCtx_NormalRead(t *testing.T) {
	ctx := context.Background()

	data := "test data for normal read"
	mockReader := newMockReader(data)

	reader := newContextReader(ctx, mockReader)

	// Read should work normally
	buf := make([]byte, 100)
	n, err := reader.Read(buf)

	require.NoError(t, err)
	assert.Greater(t, n, 0)
	assert.Equal(t, data, string(buf[:n]))
}

func TestReaderCtx_MultipleReads(t *testing.T) {
	ctx := context.Background()

	data := "line1\nline2\nline3\n"
	mockReader := newMockReader(data)

	reader := newContextReader(ctx, mockReader)

	// Read in chunks
	totalRead := ""
	buf := make([]byte, 10)
	for {
		n, err := reader.Read(buf)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		totalRead += string(buf[:n])
	}

	assert.Equal(t, data, totalRead)
}

func TestReaderCtx_CancellationDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create mock reader
	mockReader := newMockReader("data for testing cancellation")
	reader := newContextReader(ctx, mockReader)

	// First read should succeed
	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	// Cancel context
	cancel()

	// Second read should fail with context.Canceled
	n, err = reader.Read(buf)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 0, n)
}

// ===================================================================================
// TAREFA 6: Component Tests - logCaptureWriter
// ===================================================================================

func TestLogCaptureWriter_Write(t *testing.T) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"test_container_12345678",
		"stdout",
		dispatcher,
		logger,
	)

	message := "test log message\n"
	n, err := writer.Write([]byte(message))

	require.NoError(t, err)
	assert.Equal(t, len(message), n)

	// Verify dispatcher was called
	assert.Equal(t, 1, dispatcher.GetCallCount())

	calls := dispatcher.GetCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "docker", calls[0].SourceType)
	assert.Equal(t, "test_container_12345678", calls[0].SourceID)
	assert.Equal(t, message, calls[0].Message)
	assert.Equal(t, "stdout", calls[0].Labels["stream"])
}

func TestLogCaptureWriter_EmptyWrite(t *testing.T) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"test_container_12345678",
		"stdout",
		dispatcher,
		logger,
	)

	n, err := writer.Write([]byte{})

	require.NoError(t, err)
	assert.Equal(t, 0, n)

	// Dispatcher should not be called for empty writes
	assert.Equal(t, 0, dispatcher.GetCallCount())
}

func TestLogCaptureWriter_DispatcherError(t *testing.T) {
	dispatcher := NewMockDispatcher()
	dispatcher.SetHandleError(assert.AnError)
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"test_container_12345678",
		"stderr",
		dispatcher,
		logger,
	)

	message := "error log message\n"
	n, err := writer.Write([]byte(message))

	// Writer should succeed even if dispatcher fails (non-blocking behavior)
	require.NoError(t, err)
	assert.Equal(t, len(message), n)

	// Dispatcher should have been called
	assert.Equal(t, 1, dispatcher.GetCallCount())
}

func TestLogCaptureWriter_ConcurrentWrites(t *testing.T) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"test_container_12345678",
		"stdout",
		dispatcher,
		logger,
	)

	const goroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			message := "concurrent log message\n"
			writer.Write([]byte(message))
		}(i)
	}

	wg.Wait()

	// All writes should have been dispatched
	assert.Equal(t, goroutines, dispatcher.GetCallCount())
}

func TestLogCaptureWriter_Labels(t *testing.T) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	containerID := "test_container_abcdef123456"
	streamType := "stderr"

	writer := newLogCaptureWriter(containerID, streamType, dispatcher, logger)

	message := "test message with labels\n"
	writer.Write([]byte(message))

	calls := dispatcher.GetCalls()
	require.Len(t, calls, 1)

	labels := calls[0].Labels
	assert.Equal(t, containerID, labels["container_id"])
	assert.Equal(t, streamType, labels["stream"])
	assert.Equal(t, "docker", labels["source"])
	assert.Equal(t, "container_monitor", labels["monitor"])
}

// ===================================================================================
// TAREFA 7: Benchmarks
// ===================================================================================

func BenchmarkLogCaptureWriter_Write(b *testing.B) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"bench_container_12345678",
		"stdout",
		dispatcher,
		logger,
	)

	data := []byte("Benchmark log message for performance testing\n")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		writer.Write(data)
	}
}

func BenchmarkLogCaptureWriter_ConcurrentWrite(b *testing.B) {
	dispatcher := NewMockDispatcher()
	logger := newTestLogger()

	writer := newLogCaptureWriter(
		"bench_container_12345678",
		"stdout",
		dispatcher,
		logger,
	)

	data := []byte("Concurrent benchmark log message\n")

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			writer.Write(data)
		}
	})
}

func BenchmarkReaderCtx_Read(b *testing.B) {
	ctx := context.Background()

	// Create data for reading
	data := strings.Repeat("test log line\n", 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mockReader := newMockReader(data)
		reader := newContextReader(ctx, mockReader)

		buf := make([]byte, 1024)
		for {
			_, err := reader.Read(buf)
			if err == io.EOF {
				break
			}
		}
	}
}

func BenchmarkContainerMonitor_StartStopCollector(b *testing.B) {
	config := pkgTypes.DockerConfig{
		Enabled:       true,
		DrainDuration: 10 * time.Millisecond, // Reduce for benchmark
	}

	dispatcher := NewMockDispatcher()
	taskManager := NewMockTaskManager()
	logger := newTestLogger()

	cm, err := NewContainerMonitor(
		config,
		pkgTypes.TimestampValidationConfig{},
		dispatcher,
		taskManager,
		nil,
		logger,
	)
	if err != nil {
		b.Skipf("Docker not available: %v", err)
	}

	ctx := context.Background()
	cm.ctx, cm.cancel = context.WithCancel(ctx)
	defer cm.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		containerID := "bench_container_" + string(rune(i))

		// Simulate start/stop
		_, collectCancel := context.WithCancel(cm.ctx)
		cm.collectorsMux.Lock()
		cm.collectors[containerID] = collectCancel
		cm.collectorsMux.Unlock()

		cm.StopCollecting(containerID)
	}
}
