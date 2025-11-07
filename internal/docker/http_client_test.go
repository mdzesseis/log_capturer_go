package docker

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultHTTPClientConfig verifies default configuration values
func TestDefaultHTTPClientConfig(t *testing.T) {
	config := DefaultHTTPClientConfig()

	assert.Equal(t, 100, config.MaxIdleConns, "MaxIdleConns should be 100")
	assert.Equal(t, 10, config.MaxIdleConnsPerHost, "MaxIdleConnsPerHost should be 10")
	assert.Equal(t, 50, config.MaxConnsPerHost, "MaxConnsPerHost should be 50")
	assert.Equal(t, 90*time.Second, config.IdleConnTimeout, "IdleConnTimeout should be 90s")
	assert.Equal(t, 30*time.Second, config.DialTimeout, "DialTimeout should be 30s")
	assert.Equal(t, false, config.DisableKeepAlives, "DisableKeepAlives should be false (keep-alive enabled)")
	assert.Equal(t, 30*time.Second, config.KeepAlive, "KeepAlive should be 30s")
	assert.Equal(t, "unix:///var/run/docker.sock", config.SocketPath, "SocketPath should be default Docker socket")
}

// TestNewHTTPDockerClient verifies client creation
func TestNewHTTPDockerClient(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)

	require.NoError(t, err, "NewHTTPDockerClient should not return error")
	require.NotNil(t, client, "Client should not be nil")
	require.NotNil(t, client.Client(), "Docker client should not be nil")
	require.NotNil(t, client.HTTPClient(), "HTTP client should not be nil")
	require.NotNil(t, client.transport, "Transport should not be nil")

	// Verify transport configuration
	assert.Equal(t, config.MaxIdleConns, client.transport.MaxIdleConns, "MaxIdleConns should match config")
	assert.Equal(t, config.MaxIdleConnsPerHost, client.transport.MaxIdleConnsPerHost, "MaxIdleConnsPerHost should match config")
	assert.Equal(t, config.IdleConnTimeout, client.transport.IdleConnTimeout, "IdleConnTimeout should match config")
	assert.Equal(t, config.DisableKeepAlives, client.transport.DisableKeepAlives, "DisableKeepAlives should match config")

	// Verify initial state
	assert.True(t, client.IsHealthy(), "Client should be healthy initially")
	assert.NotZero(t, client.createdAt, "CreatedAt should be set")

	// Cleanup
	client.Close()
}

// TestHTTPDockerClient_IsHealthy verifies health status management
func TestHTTPDockerClient_IsHealthy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// Initial state
	assert.True(t, client.IsHealthy(), "Client should be healthy initially")

	// Set unhealthy
	client.SetHealthy(false)
	assert.False(t, client.IsHealthy(), "Client should be unhealthy after SetHealthy(false)")

	// Set healthy again
	client.SetHealthy(true)
	assert.True(t, client.IsHealthy(), "Client should be healthy after SetHealthy(true)")
}

// TestHTTPDockerClient_IncrementCounters verifies metric counters
func TestHTTPDockerClient_IncrementCounters(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// These methods should not panic
	client.IncrementRequests()
	client.IncrementErrors()

	// We can't easily verify Prometheus counter values in unit tests,
	// but we can ensure the methods don't panic
}

// TestHTTPDockerClient_UpdateConnectionMetrics verifies connection metrics update
func TestHTTPDockerClient_UpdateConnectionMetrics(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// This should not panic
	client.UpdateConnectionMetrics()
}

// TestHTTPDockerClient_CloseIdleConnections verifies idle connection cleanup
func TestHTTPDockerClient_CloseIdleConnections(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// This should not panic
	client.CloseIdleConnections()

	// CloseIdleConnections() closes idle connections internally
	// We can't verify the count, but we can verify no panic
}

// TestHTTPDockerClient_Stats verifies stats reporting
func TestHTTPDockerClient_Stats(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	stats := client.Stats()

	// Verify stats structure
	assert.Contains(t, stats, "max_idle_conns", "Stats should contain max_idle_conns")
	assert.Contains(t, stats, "max_idle_per_host", "Stats should contain max_idle_per_host")
	assert.Contains(t, stats, "max_conns_per_host", "Stats should contain max_conns_per_host")
	assert.Contains(t, stats, "keep_alive_enabled", "Stats should contain keep_alive_enabled")
	assert.Contains(t, stats, "idle_conn_timeout", "Stats should contain idle_conn_timeout")
	assert.Contains(t, stats, "is_healthy", "Stats should contain is_healthy")
	assert.Contains(t, stats, "age_seconds", "Stats should contain age_seconds")

	// Verify values
	assert.Equal(t, config.MaxIdleConns, stats["max_idle_conns"], "max_idle_conns should match config")
	assert.Equal(t, config.MaxIdleConnsPerHost, stats["max_idle_per_host"], "max_idle_per_host should match config")
	assert.Equal(t, config.MaxConnsPerHost, stats["max_conns_per_host"], "max_conns_per_host should match config")
	assert.Equal(t, true, stats["keep_alive_enabled"], "keep_alive_enabled should be true")
	assert.Equal(t, true, stats["is_healthy"], "is_healthy should be true")
}

// TestHTTPDockerClient_Close verifies proper cleanup
func TestHTTPDockerClient_Close(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)

	// Close should not error
	err = client.Close()
	assert.NoError(t, err, "Close should not return error")

	// Close() internally closes all idle connections
	// We can't verify the count, but we can verify no error
}

// TestHTTPDockerClient_HealthCheck verifies health check functionality
// Note: This test requires Docker daemon to be running
func TestHTTPDockerClient_HealthCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Health check may fail if Docker daemon is not running
	// We don't fail the test if Docker is unavailable
	err = client.HealthCheck(ctx)
	if err != nil {
		t.Logf("Health check failed (Docker may not be available): %v", err)
		assert.False(t, client.IsHealthy(), "Client should be unhealthy after failed health check")
	} else {
		assert.True(t, client.IsHealthy(), "Client should be healthy after successful health check")
	}
}

// TestGetGlobalHTTPDockerClient verifies singleton pattern
func TestGetGlobalHTTPDockerClient(t *testing.T) {
	// Reset singleton before test
	ResetGlobalHTTPDockerClient()
	defer ResetGlobalHTTPDockerClient()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// First call should create client
	client1, err1 := GetGlobalHTTPDockerClient(logger)
	require.NoError(t, err1, "First GetGlobalHTTPDockerClient should not error")
	require.NotNil(t, client1, "First client should not be nil")

	// Second call should return same client
	client2, err2 := GetGlobalHTTPDockerClient(logger)
	require.NoError(t, err2, "Second GetGlobalHTTPDockerClient should not error")
	require.NotNil(t, client2, "Second client should not be nil")

	// Verify same instance
	assert.Same(t, client1, client2, "Singleton should return same instance")
}

// TestHTTPDockerClient_CustomConfig verifies custom configuration
func TestHTTPDockerClient_CustomConfig(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Custom config with different values
	config := HTTPClientConfig{
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   5,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       60 * time.Second,
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		DisableKeepAlives:     false,
		KeepAlive:             15 * time.Second,
		SocketPath:            "unix:///var/run/docker.sock",
	}

	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// Verify custom config was applied
	assert.Equal(t, config.MaxIdleConns, client.transport.MaxIdleConns)
	assert.Equal(t, config.MaxIdleConnsPerHost, client.transport.MaxIdleConnsPerHost)
	assert.Equal(t, config.IdleConnTimeout, client.transport.IdleConnTimeout)
	assert.Equal(t, config.DisableKeepAlives, client.transport.DisableKeepAlives)
}

// TestHTTPDockerClient_ConcurrentAccess verifies thread-safety
func TestHTTPDockerClient_ConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := DefaultHTTPClientConfig()
	client, err := NewHTTPDockerClient(config, logger)
	require.NoError(t, err)
	defer client.Close()

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			// Concurrent health checks
			_ = client.IsHealthy()
			client.SetHealthy(true)
			client.SetHealthy(false)
			client.SetHealthy(true)

			// Concurrent metric updates
			client.IncrementRequests()
			client.IncrementErrors()
			client.UpdateConnectionMetrics()

			// Concurrent stats access
			_ = client.Stats()
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// BenchmarkHTTPDockerClient_IncrementRequests benchmarks counter increment
func BenchmarkHTTPDockerClient_IncrementRequests(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := DefaultHTTPClientConfig()
	client, _ := NewHTTPDockerClient(config, logger)
	defer client.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.IncrementRequests()
	}
}

// BenchmarkHTTPDockerClient_Stats benchmarks stats retrieval
func BenchmarkHTTPDockerClient_Stats(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := DefaultHTTPClientConfig()
	client, _ := NewHTTPDockerClient(config, logger)
	defer client.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.Stats()
	}
}
