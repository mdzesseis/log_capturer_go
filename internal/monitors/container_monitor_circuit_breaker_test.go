package monitors

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ssw-logs-capture/pkg/types"
)

// mockDispatcher for testing
type mockDispatcher struct {
	mu      sync.Mutex
	entries []*types.LogEntry
}

func (m *mockDispatcher) AddSink(sink types.Sink) {
	// Not used in circuit breaker tests
}

func (m *mockDispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := &types.LogEntry{
		Message: message,
		Labels:  labels,
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockDispatcher) Start(ctx context.Context) error {
	return nil
}

func (m *mockDispatcher) Stop() error {
	return nil
}

func (m *mockDispatcher) GetStats() types.DispatcherStats {
	return types.DispatcherStats{}
}

func (m *mockDispatcher) GetEntryCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// createTestMonitor creates a minimal ContainerMonitor for testing
func createTestMonitor(t *testing.T) *ContainerMonitor {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce test output noise

	dispatcher := &mockDispatcher{}

	return &ContainerMonitor{
		logger:       logger,
		dispatcher:   dispatcher,
		excludeNames: make([]string, 0),
	}
}

// TestCircuitBreaker_TrackLog tests basic log tracking
func TestCircuitBreaker_TrackLog(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Track logs for a container
	containerID := "test-container-123"
	containerName := "test-container"

	cb.trackLog(containerID, containerName)
	cb.trackLog(containerID, containerName)
	cb.trackLog(containerID, containerName)

	// Verify stats were recorded
	cb.mu.RLock()
	stats, exists := cb.stats[containerID]
	cb.mu.RUnlock()

	require.True(t, exists, "container stats should exist")
	assert.Equal(t, int64(3), stats.count, "should have 3 logs tracked")
	assert.Equal(t, containerID, stats.containerID)
	assert.Equal(t, containerName, stats.containerName)
}

// TestCircuitBreaker_Cleanup tests memory leak prevention
func TestCircuitBreaker_Cleanup(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Override window size for faster testing
	cb.windowSize = 100 * time.Millisecond

	// Track old logs
	cb.trackLog("old-container", "old")
	time.Sleep(50 * time.Millisecond)

	// Track new logs
	cb.trackLog("new-container", "new")

	// Verify both exist initially
	cb.mu.RLock()
	assert.Equal(t, 2, len(cb.stats))
	cb.mu.RUnlock()

	// Wait for cleanup window (2 * windowSize)
	time.Sleep(250 * time.Millisecond)

	// Trigger cleanup manually
	cb.cleanup()

	// Verify old stats were removed
	cb.mu.RLock()
	_, oldExists := cb.stats["old-container"]
	_, newExists := cb.stats["new-container"]
	cb.mu.RUnlock()

	assert.False(t, oldExists, "old container should be cleaned up")
	assert.False(t, newExists, "new container should also be cleaned (no recent activity)")
}

// TestCircuitBreaker_Detection tests self-monitoring detection
func TestCircuitBreaker_Detection(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Override detection interval for faster testing
	cb.windowSize = 1 * time.Second

	// Simulate a container generating 95% of logs
	badContainer := "bad-container-id"
	badContainerName := "log-capturer-self"

	normalContainer := "normal-container-id"
	normalContainerName := "normal-app"

	// Track 95 logs from bad container
	for i := 0; i < 95; i++ {
		cb.trackLog(badContainer, badContainerName)
	}

	// Track 5 logs from normal container
	for i := 0; i < 5; i++ {
		cb.trackLog(normalContainer, normalContainerName)
	}

	// Manually trigger detection
	cb.detectSelfMonitoring()

	// Verify bad container was added to exclusion list
	monitor.mu.RLock()
	excludeNames := monitor.excludeNames
	monitor.mu.RUnlock()

	assert.Contains(t, excludeNames, badContainerName, "bad container name should be excluded")
	assert.Contains(t, excludeNames, badContainer, "bad container ID should be excluded")

	// Verify stats were reset for bad container
	cb.mu.RLock()
	_, exists := cb.stats[badContainer]
	cb.mu.RUnlock()

	assert.False(t, exists, "bad container stats should be deleted after detection")
}

// TestCircuitBreaker_NoDetectionBelowThreshold tests that detection doesn't trigger below 90%
func TestCircuitBreaker_NoDetectionBelowThreshold(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Simulate a container generating 85% of logs (below 90% threshold)
	container1 := "container-1"
	container2 := "container-2"

	// 85 logs from container1
	for i := 0; i < 85; i++ {
		cb.trackLog(container1, "container-1-name")
	}

	// 15 logs from container2
	for i := 0; i < 15; i++ {
		cb.trackLog(container2, "container-2-name")
	}

	// Trigger detection
	cb.detectSelfMonitoring()

	// Verify nothing was excluded
	monitor.mu.RLock()
	excludeCount := len(monitor.excludeNames)
	monitor.mu.RUnlock()

	assert.Equal(t, 0, excludeCount, "no containers should be excluded below threshold")
}

// TestCircuitBreaker_MinimumSampleSize tests that detection requires minimum logs
func TestCircuitBreaker_MinimumSampleSize(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Track only 50 logs (below 100 minimum)
	container := "test-container"
	for i := 0; i < 50; i++ {
		cb.trackLog(container, "test")
	}

	// Trigger detection
	cb.detectSelfMonitoring()

	// Verify nothing was excluded (insufficient sample size)
	monitor.mu.RLock()
	excludeCount := len(monitor.excludeNames)
	monitor.mu.RUnlock()

	assert.Equal(t, 0, excludeCount, "should not trigger with insufficient sample size")
}

// TestCircuitBreaker_ConcurrentTracking tests thread safety
func TestCircuitBreaker_ConcurrentTracking(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Concurrently track logs from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	logsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			containerID := "container-" + string(rune('A'+id))
			for j := 0; j < logsPerGoroutine; j++ {
				cb.trackLog(containerID, containerID+"-name")
			}
		}(i)
	}

	wg.Wait()

	// Verify all logs were tracked
	cb.mu.RLock()
	totalStats := len(cb.stats)
	cb.mu.RUnlock()

	assert.Equal(t, numGoroutines, totalStats, "should have stats for all containers")
}

// TestCircuitBreaker_GracefulShutdown tests proper cleanup on stop
func TestCircuitBreaker_GracefulShutdown(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)

	// Track some logs
	cb.trackLog("test-container", "test")

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		cb.stop()
		close(done)
	}()

	select {
	case <-done:
		// Success - shutdown completed
	case <-time.After(5 * time.Second):
		t.Fatal("circuit breaker shutdown timed out")
	}
}

// TestCircuitBreaker_ContextCancellation tests context-based shutdown
func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)

	// Verify goroutines are running
	cb.trackLog("test", "test")

	// Cancel context
	cb.cancel()

	// Wait for goroutines to exit
	done := make(chan struct{})
	go func() {
		cb.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - goroutines exited
	case <-time.After(3 * time.Second):
		t.Fatal("goroutines did not exit after context cancellation")
	}
}

// TestCircuitBreaker_NoMemoryLeak tests that old data is cleaned up
func TestCircuitBreaker_NoMemoryLeak(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// Override for faster testing
	cb.windowSize = 50 * time.Millisecond

	// Track logs for many containers
	for i := 0; i < 100; i++ {
		containerID := "container-" + string(rune(i))
		cb.trackLog(containerID, containerID+"-name")
	}

	// Verify all tracked
	cb.mu.RLock()
	initialCount := len(cb.stats)
	cb.mu.RUnlock()
	assert.Equal(t, 100, initialCount)

	// Wait for cleanup period (2 * windowSize)
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup
	cb.cleanup()

	// Verify old data was removed
	cb.mu.RLock()
	finalCount := len(cb.stats)
	cb.mu.RUnlock()

	assert.Equal(t, 0, finalCount, "all old stats should be cleaned up")
}

// TestCircuitBreaker_MultipleDetections tests repeated detection cycles
func TestCircuitBreaker_MultipleDetections(t *testing.T) {
	monitor := createTestMonitor(t)
	cb := newCircuitBreaker(monitor)
	defer cb.stop()

	// First detection cycle
	container1 := "bad-container-1"
	for i := 0; i < 100; i++ {
		cb.trackLog(container1, container1+"-name")
	}
	cb.detectSelfMonitoring()

	monitor.mu.RLock()
	firstCount := len(monitor.excludeNames)
	monitor.mu.RUnlock()
	assert.Equal(t, 2, firstCount, "should exclude container name and ID")

	// Second detection cycle with different container
	container2 := "bad-container-2"
	for i := 0; i < 100; i++ {
		cb.trackLog(container2, container2+"-name")
	}
	cb.detectSelfMonitoring()

	monitor.mu.RLock()
	secondCount := len(monitor.excludeNames)
	monitor.mu.RUnlock()
	assert.Equal(t, 4, secondCount, "should have excluded both containers")
}
