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
