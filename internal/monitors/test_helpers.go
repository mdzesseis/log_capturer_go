package monitors

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	pkgTypes "ssw-logs-capture/pkg/types"
)

// ===================================================================================
// Shared Test Helpers for Monitor Tests
// ===================================================================================

// newTestLogger creates a logger for tests with minimal output
func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in test output
	logger.SetOutput(io.Discard)      // Discard logs during tests
	return logger
}

// MockDispatcher implements pkgTypes.Dispatcher for testing
type MockDispatcher struct {
	mu            sync.Mutex
	calls         []DispatchCall
	handleErr     error
	handleDelay   time.Duration
	callCount     int
	addSinkCalls  int
	startCalls    int
	stopCalls     int
	getStatsCalls int
}

// DispatchCall represents a single call to Handle
type DispatchCall struct {
	SourceType string
	SourceID   string
	Message    string
	Labels     map[string]string
	Timestamp  time.Time
}

// NewMockDispatcher creates a new MockDispatcher
func NewMockDispatcher() *MockDispatcher {
	return &MockDispatcher{
		calls: make([]DispatchCall, 0),
	}
}

// Handle implements pkgTypes.Dispatcher
func (m *MockDispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
	// Simulate processing delay if configured
	if m.handleDelay > 0 {
		time.Sleep(m.handleDelay)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep copy labels to avoid race conditions
	labelsCopy := make(map[string]string, len(labels))
	for k, v := range labels {
		labelsCopy[k] = v
	}

	m.calls = append(m.calls, DispatchCall{
		SourceType: sourceType,
		SourceID:   sourceID,
		Message:    message,
		Labels:     labelsCopy,
		Timestamp:  time.Now(),
	})
	m.callCount++

	return m.handleErr
}

// AddSink implements pkgTypes.Dispatcher
func (m *MockDispatcher) AddSink(sink pkgTypes.Sink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addSinkCalls++
}

// Start implements pkgTypes.Dispatcher
func (m *MockDispatcher) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
	return nil
}

// Stop implements pkgTypes.Dispatcher
func (m *MockDispatcher) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls++
	return nil
}

// GetStats implements pkgTypes.Dispatcher
func (m *MockDispatcher) GetStats() pkgTypes.DispatcherStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getStatsCalls++
	return pkgTypes.DispatcherStats{}
}

// GetCallCount returns the number of Handle calls
func (m *MockDispatcher) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// GetCalls returns a copy of all Handle calls
func (m *MockDispatcher) GetCalls() []DispatchCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return a copy to avoid race conditions
	callsCopy := make([]DispatchCall, len(m.calls))
	copy(callsCopy, m.calls)
	return callsCopy
}

// Reset clears all recorded calls
func (m *MockDispatcher) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = make([]DispatchCall, 0)
	m.callCount = 0
}

// SetHandleError configures the mock to return errors
func (m *MockDispatcher) SetHandleError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handleErr = err
}

// SetHandleDelay configures simulated processing delay
func (m *MockDispatcher) SetHandleDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handleDelay = delay
}

// WaitForCalls waits for at least n calls to be made
func (m *MockDispatcher) WaitForCalls(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.GetCallCount() >= n {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// MockTaskManager implements pkgTypes.TaskManager for testing
type MockTaskManager struct {
	mu             sync.Mutex
	startTaskCalls int
	stopTaskCalls  int
	cleanupCalls   int
	shutdownCalls  int
	tasks          map[string]pkgTypes.TaskStatus
}

func NewMockTaskManager() *MockTaskManager {
	return &MockTaskManager{
		tasks: make(map[string]pkgTypes.TaskStatus),
	}
}

func (m *MockTaskManager) StartTask(ctx context.Context, taskID string, fn func(context.Context) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startTaskCalls++
	m.tasks[taskID] = pkgTypes.TaskStatus{
		ID:        taskID,
		State:     "running",
		StartedAt: time.Now(),
	}
	return nil
}

func (m *MockTaskManager) StopTask(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopTaskCalls++
	if status, ok := m.tasks[taskID]; ok {
		status.State = "stopped"
		m.tasks[taskID] = status
	}
	return nil
}

func (m *MockTaskManager) Heartbeat(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.tasks[taskID]; ok {
		status.LastHeartbeat = time.Now()
		m.tasks[taskID] = status
	}
	return nil
}

func (m *MockTaskManager) GetTaskStatus(taskID string) pkgTypes.TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[taskID]
}

func (m *MockTaskManager) GetTaskStatuses() map[string]pkgTypes.TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy
	copy := make(map[string]pkgTypes.TaskStatus, len(m.tasks))
	for k, v := range m.tasks {
		copy[k] = v
	}
	return copy
}

func (m *MockTaskManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupCalls++
	m.tasks = make(map[string]pkgTypes.TaskStatus)
}

func (m *MockTaskManager) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownCalls++
	return nil
}

func (m *MockTaskManager) GetAllTasks() map[string]pkgTypes.TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	tasksCopy := make(map[string]pkgTypes.TaskStatus, len(m.tasks))
	for k, v := range m.tasks {
		tasksCopy[k] = v
	}
	return tasksCopy
}

// waitForCondition waits for a condition to be true with timeout
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// writeToFile appends lines to a file (helper for file monitor tests)
func writeToFile(t *testing.T, path string, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(t, err)
	defer file.Close()

	for _, line := range lines {
		_, err := file.WriteString(line + "\n")
		require.NoError(t, err)
	}

	// Ensure data is written to disk
	err = file.Sync()
	require.NoError(t, err)
}
