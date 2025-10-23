package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppCreation tests the creation of a new app instance
func TestAppCreation(t *testing.T) {
	// Create a temporary config file
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"
  log_format: "json"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test_config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Test app creation
	app, err := New(configFile)
	assert.NoError(t, err)
	assert.NotNil(t, app)
	assert.Equal(t, "test-app", app.config.App.Name)
	assert.Equal(t, "v1.0.0", app.config.App.Version)
}

// TestAppCreationWithInvalidConfig tests app creation with invalid config
func TestAppCreationWithInvalidConfig(t *testing.T) {
	invalidConfigFile := "/nonexistent/config.yaml"

	app, err := New(invalidConfigFile)
	assert.Error(t, err)
	assert.Nil(t, app)
}

// TestHealthHandler tests the health check endpoint
func TestHealthHandler(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Create a request to the health endpoint
	req, err := http.NewRequest("GET", "/health", nil)
	require.NoError(t, err)

	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()

	// Call the handler
	app.healthHandler(rr, req)

	// Check the status code
	assert.Equal(t, http.StatusOK, rr.Code)

	// Check the response body
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "status")
	assert.Contains(t, response, "timestamp")
}

// TestStatsHandler tests the stats endpoint
func TestStatsHandler(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	req, err := http.NewRequest("GET", "/stats", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	app.statsHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "application")
	assert.Contains(t, response, "dispatcher")
}

// TestConfigHandler tests the config endpoint
func TestConfigHandler(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	req, err := http.NewRequest("GET", "/config", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	app.configHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "app")
	assert.Contains(t, response, "metrics")
}

// TestAppStartStop tests the app start and stop functionality
func TestAppStartStop(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Test start
	go func() {
		time.Sleep(100 * time.Millisecond)
		app.Stop()
	}()

	err = app.Start()
	assert.NoError(t, err)
}

// TestAppRun tests the complete app run cycle
func TestAppRun(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Create a context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run app in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Run()
	}()

	// Send stop signal after short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		app.cancel()
	}()

	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Test timed out")
	}
}

// TestInitializeComponents tests component initialization
func TestInitializeComponents(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createMinimalConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Components should be initialized during New()
	assert.NotNil(t, app.taskManager)
	assert.NotNil(t, app.dispatcher)
	assert.NotNil(t, app.processor)
}

// TestSecurityIntegration tests security features integration
func TestSecurityIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createSecurityConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Security manager should be initialized
	assert.NotNil(t, app.securityManager)
}

// TestTracingIntegration tests tracing features integration
func TestTracingIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createTracingConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Tracing manager should be initialized
	assert.NotNil(t, app.tracingManager)
}

// TestEnterpriseFeatures tests enterprise features initialization
func TestEnterpriseFeatures(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := createEnterpriseConfig(t, tmpDir)

	app, err := New(configFile)
	require.NoError(t, err)
	require.NotNil(t, app)

	// Enterprise features should be initialized
	assert.NotNil(t, app.securityManager)
	assert.NotNil(t, app.tracingManager)
	assert.NotNil(t, app.sloManager)
	assert.NotNil(t, app.goroutineTracker)
}

// Helper functions

func createMinimalConfig(t *testing.T, tmpDir string) string {
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"
  log_format: "json"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2
  batch_size: 10
  batch_timeout: "1s"
  max_retries: 1
  retry_base_delay: "100ms"

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false

security:
  enabled: false

tracing:
  enabled: false

slo:
  enabled: false

goroutine_tracking:
  enabled: false

resource_monitoring:
  enabled: false

disk_cleanup:
  enabled: false

disk_buffer:
  enabled: false

anomaly_detection:
  enabled: false

hot_reload:
  enabled: false
`

	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)
	return configFile
}

func createSecurityConfig(t *testing.T, tmpDir string) string {
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false

security:
  enabled: true
  authentication:
    enabled: true
    method: "basic"
    users:
      test:
        username: "test"
        password_hash: "test_hash"
        roles: ["admin"]

tracing:
  enabled: false

slo:
  enabled: false

goroutine_tracking:
  enabled: false
`

	configFile := filepath.Join(tmpDir, "config_security.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)
	return configFile
}

func createTracingConfig(t *testing.T, tmpDir string) string {
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false

security:
  enabled: false

tracing:
  enabled: true
  service_name: "test-service"
  service_version: "v1.0.0"
  exporter: "console"

slo:
  enabled: false

goroutine_tracking:
  enabled: false
`

	configFile := filepath.Join(tmpDir, "config_tracing.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)
	return configFile
}

func createEnterpriseConfig(t *testing.T, tmpDir string) string {
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false

security:
  enabled: true
  authentication:
    enabled: true

tracing:
  enabled: true
  service_name: "test-service"

slo:
  enabled: true
  prometheus_url: "http://localhost:9090"

goroutine_tracking:
  enabled: true

resource_monitoring:
  enabled: false

disk_cleanup:
  enabled: false

disk_buffer:
  enabled: false

anomaly_detection:
  enabled: false

hot_reload:
  enabled: false
`

	configFile := filepath.Join(tmpDir, "config_enterprise.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)
	return configFile
}

// BenchmarkAppCreation benchmarks app creation performance
func BenchmarkAppCreation(b *testing.B) {
	tmpDir := b.TempDir()
	configFile := createMinimalConfig(b, tmpDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app, err := New(configFile)
		if err != nil {
			b.Fatal(err)
		}
		if app == nil {
			b.Fatal("app is nil")
		}
	}
}

// BenchmarkHealthHandler benchmarks health endpoint performance
func BenchmarkHealthHandler(b *testing.B) {
	tmpDir := b.TempDir()
	configFile := createMinimalConfig(b, tmpDir)

	app, err := New(configFile)
	if err != nil {
		b.Fatal(err)
	}

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		app.healthHandler(rr, req)
	}
}

// Helper function for benchmarks that need testing.TB interface
func createMinimalConfig(tb testing.TB, tmpDir string) string {
	configContent := `
app:
  name: "test-app"
  version: "v1.0.0"
  log_level: "info"
  log_format: "json"

server:
  enabled: false

metrics:
  enabled: false

dispatcher:
  queue_size: 1000
  worker_count: 2
  batch_size: 10
  batch_timeout: "1s"
  max_retries: 1
  retry_base_delay: "100ms"

file_monitor_service:
  enabled: false

container_monitor:
  enabled: false

sinks:
  loki:
    enabled: false
  local_file:
    enabled: false

positions:
  enabled: false

processing:
  enabled: false

security:
  enabled: false

tracing:
  enabled: false

slo:
  enabled: false

goroutine_tracking:
  enabled: false
`

	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		tb.Fatal(err)
	}
	return configFile
}