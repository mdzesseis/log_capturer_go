package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// HTTPClientConfig configuration for HTTP client with connection pooling
type HTTPClientConfig struct {
	// Connection pool settings
	MaxIdleConns          int           `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost       int           `yaml:"max_conns_per_host"`
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout"`

	// Timeouts
	DialTimeout           time.Duration `yaml:"dial_timeout"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout"`

	// Keep-Alive settings
	DisableKeepAlives     bool          `yaml:"disable_keep_alives"`
	KeepAlive             time.Duration `yaml:"keep_alive"`

	// Docker socket path
	SocketPath            string        `yaml:"socket_path"`
}

// HTTPDockerClient wraps a Docker client with optimized HTTP transport
type HTTPDockerClient struct {
	client     *client.Client
	httpClient *http.Client
	transport  *http.Transport
	config     HTTPClientConfig
	logger     *logrus.Logger

	// Metrics
	idleConnsGauge    prometheus.Gauge
	activeConnsGauge  prometheus.Gauge
	requestCounter    prometheus.Counter
	errorCounter      prometheus.Counter

	// State management
	mu        sync.RWMutex
	isHealthy bool
	createdAt time.Time
}

// DefaultHTTPClientConfig returns default configuration optimized for Docker SDK
func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		// Connection pool - optimized for high throughput
		MaxIdleConns:          100,  // Total idle connections across all hosts
		MaxIdleConnsPerHost:   10,   // Idle connections per Docker daemon
		MaxConnsPerHost:       50,   // Max concurrent connections per host
		IdleConnTimeout:       90 * time.Second,

		// Timeouts - reasonable defaults
		DialTimeout:           30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		// Keep-Alive - CRITICAL for connection reuse
		DisableKeepAlives:     false, // Must be false to enable pooling
		KeepAlive:             30 * time.Second,

		// Default Docker socket
		SocketPath:            "unix:///var/run/docker.sock",
	}
}

// NewHTTPDockerClient creates a new Docker client with optimized HTTP transport
func NewHTTPDockerClient(config HTTPClientConfig, logger *logrus.Logger) (*HTTPDockerClient, error) {
	// Create custom dialer with keep-alive
	dialer := &net.Dialer{
		Timeout:   config.DialTimeout,
		KeepAlive: config.KeepAlive,
	}

	// Create optimized HTTP transport with connection pooling
	transport := &http.Transport{
		// Connection pool configuration
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,

		// Timeouts
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,

		// Keep-Alive (CRITICAL)
		DisableKeepAlives:     config.DisableKeepAlives,

		// Custom dialer for Unix socket
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Override network and address for Docker Unix socket
			if config.SocketPath != "" {
				return dialer.DialContext(ctx, "unix", "/var/run/docker.sock")
			}
			return dialer.DialContext(ctx, network, addr)
		},

		// Force HTTP/1.1 (Docker API requirement)
		ForceAttemptHTTP2: false,
	}

	// Create HTTP client with custom transport
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   0, // No timeout at HTTP client level (handled by transport)
	}

	// Create Docker client with custom HTTP client
	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(config.SocketPath),
		client.WithHTTPClient(httpClient),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	hdc := &HTTPDockerClient{
		client:     dockerClient,
		httpClient: httpClient,
		transport:  transport,
		config:     config,
		logger:     logger,
		isHealthy:  true,
		createdAt:  time.Now(),
	}

	// Initialize metrics
	hdc.initMetrics()

	logger.WithFields(logrus.Fields{
		"max_idle_conns":          config.MaxIdleConns,
		"max_idle_conns_per_host": config.MaxIdleConnsPerHost,
		"max_conns_per_host":      config.MaxConnsPerHost,
		"keep_alive_enabled":      !config.DisableKeepAlives,
		"idle_conn_timeout":       config.IdleConnTimeout,
	}).Info("HTTP Docker client created with connection pooling")

	return hdc, nil
}

// initMetrics initializes Prometheus metrics for HTTP client monitoring
func (hdc *HTTPDockerClient) initMetrics() {
	// Gauge for idle connections
	hdc.idleConnsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "log_capturer",
		Subsystem: "docker_http",
		Name:      "idle_connections",
		Help:      "Current number of idle HTTP connections to Docker daemon",
	})

	// Gauge for active connections (estimated)
	hdc.activeConnsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "log_capturer",
		Subsystem: "docker_http",
		Name:      "active_connections",
		Help:      "Estimated number of active HTTP connections to Docker daemon",
	})

	// Counter for total requests
	hdc.requestCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "log_capturer",
		Subsystem: "docker_http",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests made to Docker daemon",
	})

	// Counter for errors
	hdc.errorCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "log_capturer",
		Subsystem: "docker_http",
		Name:      "errors_total",
		Help:      "Total number of HTTP request errors to Docker daemon",
	})

	// Register metrics (ignore errors if already registered)
	prometheus.Register(hdc.idleConnsGauge)
	prometheus.Register(hdc.activeConnsGauge)
	prometheus.Register(hdc.requestCounter)
	prometheus.Register(hdc.errorCounter)
}

// Client returns the underlying Docker client
func (hdc *HTTPDockerClient) Client() *client.Client {
	return hdc.client
}

// HTTPClient returns the underlying HTTP client
func (hdc *HTTPDockerClient) HTTPClient() *http.Client {
	return hdc.httpClient
}

// IsHealthy returns the health status of the client
func (hdc *HTTPDockerClient) IsHealthy() bool {
	hdc.mu.RLock()
	defer hdc.mu.RUnlock()
	return hdc.isHealthy
}

// SetHealthy updates the health status
func (hdc *HTTPDockerClient) SetHealthy(healthy bool) {
	hdc.mu.Lock()
	defer hdc.mu.Unlock()
	hdc.isHealthy = healthy
}

// IncrementRequests increments the request counter
func (hdc *HTTPDockerClient) IncrementRequests() {
	hdc.requestCounter.Inc()
}

// IncrementErrors increments the error counter
func (hdc *HTTPDockerClient) IncrementErrors() {
	hdc.errorCounter.Inc()
}

// UpdateConnectionMetrics updates connection pool metrics
// Note: http.Transport doesn't expose internal connection counts directly,
// so we track them externally through request/response monitoring
func (hdc *HTTPDockerClient) UpdateConnectionMetrics() {
	// http.Transport doesn't provide direct access to connection pool stats
	// We set gauges to 0 as placeholder - actual tracking happens at request level
	// This is a limitation of the standard library
	hdc.idleConnsGauge.Set(0)
	hdc.activeConnsGauge.Set(0)
}

// CloseIdleConnections closes all idle connections
// Useful for graceful shutdown or forcing connection refresh
func (hdc *HTTPDockerClient) CloseIdleConnections() {
	hdc.logger.Debug("Closing all idle HTTP connections")
	hdc.transport.CloseIdleConnections()
	hdc.UpdateConnectionMetrics()
}

// Close closes the Docker client and all HTTP connections
func (hdc *HTTPDockerClient) Close() error {
	hdc.logger.Info("Closing HTTP Docker client")

	// Close all idle connections
	hdc.CloseIdleConnections()

	// Close Docker client
	if hdc.client != nil {
		if err := hdc.client.Close(); err != nil {
			hdc.logger.WithError(err).Warn("Error closing Docker client")
			return err
		}
	}

	return nil
}

// Stats returns connection pool statistics
func (hdc *HTTPDockerClient) Stats() map[string]interface{} {
	return map[string]interface{}{
		"max_idle_conns":       hdc.config.MaxIdleConns,
		"max_idle_per_host":    hdc.config.MaxIdleConnsPerHost,
		"max_conns_per_host":   hdc.config.MaxConnsPerHost,
		"keep_alive_enabled":   !hdc.config.DisableKeepAlives,
		"idle_conn_timeout":    hdc.config.IdleConnTimeout.String(),
		"is_healthy":           hdc.isHealthy,
		"age_seconds":          time.Since(hdc.createdAt).Seconds(),
	}
}

// HealthCheck performs a health check by pinging Docker daemon
func (hdc *HTTPDockerClient) HealthCheck(ctx context.Context) error {
	_, err := hdc.client.Ping(ctx)
	if err != nil {
		hdc.SetHealthy(false)
		hdc.IncrementErrors()
		return fmt.Errorf("Docker daemon ping failed: %w", err)
	}

	hdc.SetHealthy(true)
	return nil
}

// Singleton pattern for global HTTP Docker client
var (
	globalHTTPClient     *HTTPDockerClient
	globalHTTPClientOnce sync.Once
	globalHTTPClientErr  error
)

// GetGlobalHTTPDockerClient returns the singleton HTTP Docker client
// This ensures all container monitors share the same connection pool
func GetGlobalHTTPDockerClient(logger *logrus.Logger) (*HTTPDockerClient, error) {
	globalHTTPClientOnce.Do(func() {
		config := DefaultHTTPClientConfig()
		globalHTTPClient, globalHTTPClientErr = NewHTTPDockerClient(config, logger)
	})
	return globalHTTPClient, globalHTTPClientErr
}

// ResetGlobalHTTPDockerClient resets the singleton (useful for testing)
func ResetGlobalHTTPDockerClient() {
	if globalHTTPClient != nil {
		globalHTTPClient.Close()
	}
	globalHTTPClient = nil
	globalHTTPClientOnce = sync.Once{}
}
