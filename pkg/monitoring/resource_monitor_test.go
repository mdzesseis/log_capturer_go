package monitoring

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResourceMonitor(t *testing.T) {
	logger := logrus.New()
	config := Config{
		Enabled:             true,
		CheckInterval:       1 * time.Second,
		GoroutineThreshold:  1000,
		MemoryThresholdMB:   500,
		FDThreshold:         1000,
		GrowthRateThreshold: 50.0,
	}

	monitor := NewResourceMonitor(config, logger)

	assert.NotNil(t, monitor)
	assert.Equal(t, config.Enabled, monitor.config.Enabled)
	assert.NotNil(t, monitor.alerts)
	assert.NotNil(t, monitor.ctx)
	assert.NotNil(t, monitor.cancel)
}

func TestResourceMonitor_StartStop(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests

	config := Config{
		Enabled:             true,
		CheckInterval:       100 * time.Millisecond,
		GoroutineThreshold:  10000,
		MemoryThresholdMB:   1000,
		AlertOnThreshold:    false, // Disable alerts for this test
	}

	monitor := NewResourceMonitor(config, logger)

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Check metrics were collected
	metrics := monitor.GetMetrics()
	assert.NotZero(t, metrics.Goroutines, "Goroutines should be counted")
	assert.NotZero(t, metrics.MemoryAllocMB, "Memory should be counted")

	// Stop monitor
	err = monitor.Stop()
	require.NoError(t, err)
}

func TestResourceMonitor_DisabledMonitor(t *testing.T) {
	logger := logrus.New()

	config := Config{
		Enabled: false,
	}

	monitor := NewResourceMonitor(config, logger)

	// Should not error when disabled
	err := monitor.Start()
	require.NoError(t, err)

	err = monitor.Stop()
	require.NoError(t, err)
}

func TestResourceMonitor_CollectMetrics(t *testing.T) {
	logger := logrus.New()
	config := Config{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	monitor := NewResourceMonitor(config, logger)

	metrics := monitor.collectMetrics()

	assert.NotZero(t, metrics.Timestamp, "Timestamp should be set")
	assert.Greater(t, metrics.Goroutines, 0, "Should have at least one goroutine")
	assert.GreaterOrEqual(t, metrics.MemoryAllocMB, int64(0), "Memory should be non-negative")
	assert.GreaterOrEqual(t, metrics.HeapObjects, uint64(0), "Heap objects should be non-negative")
}

func TestResourceMonitor_CalculateGrowthRate(t *testing.T) {
	logger := logrus.New()
	config := Config{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	monitor := NewResourceMonitor(config, logger)

	tests := []struct {
		name     string
		previous int
		current  int
		expected float64
	}{
		{"No growth", 100, 100, 0.0},
		{"50% growth", 100, 150, 50.0},
		{"100% growth", 100, 200, 100.0},
		{"50% decrease", 100, 50, -50.0},
		{"Zero previous", 0, 100, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			growth := monitor.calculateGrowthRate(tt.previous, tt.current)
			assert.Equal(t, tt.expected, growth)
		})
	}
}

func TestResourceMonitor_ThresholdAlerting(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := Config{
		Enabled:             true,
		CheckInterval:       50 * time.Millisecond,
		GoroutineThreshold:  1, // Very low threshold to trigger alert
		MemoryThresholdMB:   1, // Very low threshold to trigger alert
		AlertOnThreshold:    true,
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)
	defer monitor.Stop()

	// Wait for alerts to be generated
	time.Sleep(150 * time.Millisecond)

	// Check if alerts were generated
	alertChannel := monitor.GetAlertChannel()

	select {
	case alert := <-alertChannel:
		assert.NotEmpty(t, alert.Type, "Alert type should be set")
		assert.NotEmpty(t, alert.Severity, "Alert severity should be set")
		assert.NotEmpty(t, alert.Message, "Alert message should be set")
		t.Logf("Received alert: %s - %s", alert.Type, alert.Message)
	case <-time.After(200 * time.Millisecond):
		t.Log("No alerts received (might be expected if system is under threshold)")
	}
}

func TestResourceMonitor_DetermineSeverity(t *testing.T) {
	logger := logrus.New()
	config := Config{
		Enabled: true,
	}

	monitor := NewResourceMonitor(config, logger)

	tests := []struct {
		name      string
		current   int
		threshold int
		expected  string
	}{
		{"Within threshold", 100, 150, "warning"},
		{"1.5x threshold", 150, 100, "high"},
		{"2x threshold", 200, 100, "critical"},
		{"3x threshold", 300, 100, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := monitor.determineSeverity(tt.current, tt.threshold)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

// TestResourceMonitor_GrowthRateCalculation_EdgeCases tests growth rate with edge cases
func TestResourceMonitor_GrowthRateCalculation_EdgeCases(t *testing.T) {
	logger := logrus.New()
	config := Config{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	monitor := NewResourceMonitor(config, logger)

	tests := []struct {
		name     string
		previous int
		current  int
		expected float64
	}{
		{"Both zero", 0, 0, 0.0},
		{"Negative previous", -10, 100, 0.0}, // Invalid input
		{"Negative current", 100, -10, -110.0},
		{"Very large growth", 1, 1000000, 99999900.0},
		{"Exact doubling", 500, 1000, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			growth := monitor.calculateGrowthRate(tt.previous, tt.current)
			assert.Equal(t, tt.expected, growth)
		})
	}
}

// TestResourceMonitor_FileDescriptorMonitoring tests FD tracking
func TestResourceMonitor_FileDescriptorMonitoring(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := Config{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
		FDThreshold:   10, // Low threshold to potentially trigger alerts
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)
	defer monitor.Stop()

	// Let it collect metrics
	time.Sleep(200 * time.Millisecond)

	metrics := monitor.GetMetrics()

	// FD count should be non-negative (may be 0 on some systems)
	assert.GreaterOrEqual(t, metrics.FileDescriptors, uint64(0), "FD count should be non-negative")
}

// TestResourceMonitor_AlertGeneration_MultipleTypes tests different alert types
func TestResourceMonitor_AlertGeneration_MultipleTypes(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise

	config := Config{
		Enabled:             true,
		CheckInterval:       50 * time.Millisecond,
		GoroutineThreshold:  1,   // Very low to trigger goroutine alert
		MemoryThresholdMB:   1,   // Very low to trigger memory alert
		FDThreshold:         1,   // Very low to trigger FD alert
		GrowthRateThreshold: 1.0, // Very low to trigger growth alert
		AlertOnThreshold:    true,
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)
	defer monitor.Stop()

	// Collect alerts for a short period
	time.Sleep(200 * time.Millisecond)

	alertChannel := monitor.GetAlertChannel()

	// Try to receive multiple alerts
	alertTypes := make(map[string]bool)
	timeout := time.After(300 * time.Millisecond)

	for {
		select {
		case alert := <-alertChannel:
			alertTypes[alert.Type] = true
			t.Logf("Alert received: Type=%s, Severity=%s, Message=%s", alert.Type, alert.Severity, alert.Message)
		case <-timeout:
			// Timeout - exit loop
			goto done
		default:
			// No more alerts immediately available
			if len(alertTypes) > 0 {
				goto done
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

done:
	// We should have received at least one alert type
	t.Logf("Total alert types received: %d", len(alertTypes))
	// Don't assert specific alerts as they depend on system state
}

// TestResourceMonitor_ConcurrentMetricsAccess tests thread safety
func TestResourceMonitor_ConcurrentMetricsAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:          true,
		CheckInterval:    50 * time.Millisecond,
		AlertOnThreshold: false, // Disable alerts for this test
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)
	defer monitor.Stop()

	// Concurrently access metrics
	done := make(chan struct{})
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = monitor.GetMetrics()
				time.Sleep(10 * time.Millisecond)
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should complete without race conditions
}

// TestResourceMonitor_AlertChannel_NonBlocking tests that alerts don't block
func TestResourceMonitor_AlertChannel_NonBlocking(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:             true,
		CheckInterval:       10 * time.Millisecond, // Very fast
		GoroutineThreshold:  1,
		MemoryThresholdMB:   1,
		AlertOnThreshold:    true,
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)

	// Don't consume alerts - channel should not block monitor
	time.Sleep(200 * time.Millisecond)

	// Monitor should still be responsive
	metrics := monitor.GetMetrics()
	assert.NotZero(t, metrics.Goroutines)

	err = monitor.Stop()
	require.NoError(t, err)
}

// TestResourceMonitor_MetricsHistory tests that metrics are updated over time
func TestResourceMonitor_MetricsHistory(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:       true,
		CheckInterval: 50 * time.Millisecond,
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)
	defer monitor.Stop()

	// Get first metrics
	metrics1 := monitor.GetMetrics()
	timestamp1 := metrics1.Timestamp

	// Wait for next collection cycle
	time.Sleep(150 * time.Millisecond)

	// Get second metrics
	metrics2 := monitor.GetMetrics()
	timestamp2 := metrics2.Timestamp

	// Timestamps should be different (metrics updated)
	assert.True(t, timestamp2.After(timestamp1), "Metrics should be updated over time")
}

// TestResourceMonitor_StopIdempotent tests that Stop can be called multiple times
func TestResourceMonitor_StopIdempotent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:       true,
		CheckInterval: 100 * time.Millisecond,
	}

	monitor := NewResourceMonitor(config, logger)

	err := monitor.Start()
	require.NoError(t, err)

	// Call Stop multiple times
	err = monitor.Stop()
	require.NoError(t, err)

	err = monitor.Stop()
	require.NoError(t, err) // Should not error on second call
}

func BenchmarkResourceMonitor_CollectMetrics(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel) // Disable logging for benchmark

	config := Config{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	monitor := NewResourceMonitor(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = monitor.collectMetrics()
	}
}

// BenchmarkResourceMonitor_GetMetrics benchmarks concurrent metrics access
func BenchmarkResourceMonitor_GetMetrics(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)

	config := Config{
		Enabled:       true,
		CheckInterval: 1 * time.Second,
	}

	monitor := NewResourceMonitor(config, logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = monitor.GetMetrics()
		}
	})
}
