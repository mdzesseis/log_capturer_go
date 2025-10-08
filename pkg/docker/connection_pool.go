package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// ConnectionPoolConfig configuration for Docker connection pool
type ConnectionPoolConfig struct {
	MaxConnections     int           `yaml:"max_connections"`
	MaxIdleConnections int           `yaml:"max_idle_connections"`
	ConnectionTimeout  time.Duration `yaml:"connection_timeout"`
	IdleTimeout        time.Duration `yaml:"idle_timeout"`
	MaxLifetime        time.Duration `yaml:"max_lifetime"`
	HealthCheckPeriod  time.Duration `yaml:"health_check_period"`
	MinConnections     int           `yaml:"min_connections"`
}

// PooledConnection represents a connection in the pool
type PooledConnection struct {
	Client      *client.Client
	CreatedAt   time.Time
	LastUsed    time.Time
	IsHealthy   bool
	UseCount    int64
	mutex       sync.RWMutex
}

// ConnectionPool manages a pool of Docker client connections
type ConnectionPool struct {
	config       ConnectionPoolConfig
	logger       *logrus.Logger
	connections  []*PooledConnection
	available    chan *PooledConnection
	mutex        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	isRunning    bool

	// Metrics
	totalConnections    prometheus.Gauge
	activeConnections   prometheus.Gauge
	availableConnections prometheus.Gauge
	connectionRequests  prometheus.Counter
	connectionErrors    prometheus.Counter
	connectionLatency   prometheus.Histogram
	connectionLifetime  prometheus.Histogram
}

// NewConnectionPool creates a new Docker connection pool
func NewConnectionPool(config ConnectionPoolConfig, logger *logrus.Logger) (*ConnectionPool, error) {
	// Set defaults
	if config.MaxConnections == 0 {
		config.MaxConnections = 20
	}
	if config.MaxIdleConnections == 0 {
		config.MaxIdleConnections = 5
	}
	if config.MinConnections == 0 {
		config.MinConnections = 2
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 30 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 10 * time.Minute
	}
	if config.MaxLifetime == 0 {
		config.MaxLifetime = 1 * time.Hour
	}
	if config.HealthCheckPeriod == 0 {
		config.HealthCheckPeriod = 1 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		config:      config,
		logger:      logger,
		connections: make([]*PooledConnection, 0, config.MaxConnections),
		available:   make(chan *PooledConnection, config.MaxConnections),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize metrics
	pool.initMetrics()

	return pool, nil
}

// Start initializes the connection pool
func (cp *ConnectionPool) Start() error {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	if cp.isRunning {
		return fmt.Errorf("connection pool already running")
	}

	cp.logger.Info("Starting Docker connection pool")

	// Create minimum connections
	for i := 0; i < cp.config.MinConnections; i++ {
		conn, err := cp.createConnection()
		if err != nil {
			cp.logger.WithError(err).Error("Failed to create initial connection")
			continue
		}
		cp.connections = append(cp.connections, conn)
		cp.available <- conn
	}

	cp.isRunning = true

	// Start maintenance goroutines
	go cp.healthCheckLoop()
	go cp.cleanupLoop()

	cp.logger.WithField("initial_connections", len(cp.connections)).Info("Docker connection pool started")
	return nil
}

// Stop closes all connections and stops the pool
func (cp *ConnectionPool) Stop() error {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	if !cp.isRunning {
		return nil
	}

	cp.logger.Info("Stopping Docker connection pool")
	cp.isRunning = false

	// Cancel context to stop background processes
	cp.cancel()

	// Close all connections
	close(cp.available)
	for _, conn := range cp.connections {
		if conn.Client != nil {
			conn.Client.Close()
		}
	}

	cp.connections = nil

	cp.logger.Info("Docker connection pool stopped")
	return nil
}

// GetConnection retrieves a connection from the pool
func (cp *ConnectionPool) GetConnection(ctx context.Context) (*PooledConnection, error) {
	startTime := time.Now()
	defer func() {
		cp.connectionLatency.Observe(time.Since(startTime).Seconds())
	}()

	cp.connectionRequests.Inc()

	// Try to get an available connection
	select {
	case conn := <-cp.available:
		if conn != nil && cp.isConnectionHealthy(conn) {
			conn.mutex.Lock()
			conn.LastUsed = time.Now()
			conn.UseCount++
			conn.mutex.Unlock()

			cp.updateMetrics()
			return conn, nil
		}
		// Connection is unhealthy, try to create a new one
		if conn != nil {
			cp.removeConnection(conn)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// No available connection, try to create a new one
	}

	// Try to create a new connection if we haven't reached the limit
	cp.mutex.Lock()
	if len(cp.connections) < cp.config.MaxConnections {
		conn, err := cp.createConnection()
		if err != nil {
			cp.mutex.Unlock()
			cp.connectionErrors.Inc()
			return nil, fmt.Errorf("failed to create new connection: %w", err)
		}
		cp.connections = append(cp.connections, conn)
		cp.mutex.Unlock()

		conn.mutex.Lock()
		conn.LastUsed = time.Now()
		conn.UseCount++
		conn.mutex.Unlock()

		cp.updateMetrics()
		return conn, nil
	}
	cp.mutex.Unlock()

	// Wait for an available connection with timeout
	timeout := time.NewTimer(cp.config.ConnectionTimeout)
	defer timeout.Stop()

	select {
	case conn := <-cp.available:
		if conn != nil && cp.isConnectionHealthy(conn) {
			conn.mutex.Lock()
			conn.LastUsed = time.Now()
			conn.UseCount++
			conn.mutex.Unlock()

			cp.updateMetrics()
			return conn, nil
		}
		if conn != nil {
			cp.removeConnection(conn)
		}
		cp.connectionErrors.Inc()
		return nil, fmt.Errorf("no healthy connections available")
	case <-timeout.C:
		cp.connectionErrors.Inc()
		return nil, fmt.Errorf("connection timeout after %v", cp.config.ConnectionTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ReturnConnection returns a connection to the pool
func (cp *ConnectionPool) ReturnConnection(conn *PooledConnection) {
	if conn == nil {
		return
	}

	conn.mutex.Lock()
	conn.LastUsed = time.Now()
	conn.mutex.Unlock()

	// Check if connection is still healthy
	if !cp.isConnectionHealthy(conn) {
		cp.removeConnection(conn)
		return
	}

	// Check if pool is still running
	if !cp.isRunning {
		cp.removeConnection(conn)
		return
	}

	// Return to pool
	select {
	case cp.available <- conn:
		// Successfully returned
	default:
		// Pool is full, close the connection
		cp.removeConnection(conn)
	}

	cp.updateMetrics()
}

// createConnection creates a new Docker client connection
func (cp *ConnectionPool) createConnection() (*PooledConnection, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(cp.ctx, 10*time.Second)
	defer cancel()

	_, err = dockerClient.Ping(ctx)
	if err != nil {
		dockerClient.Close()
		return nil, fmt.Errorf("failed to ping docker daemon: %w", err)
	}

	conn := &PooledConnection{
		Client:    dockerClient,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		IsHealthy: true,
		UseCount:  0,
	}

	cp.logger.Debug("Created new Docker connection")
	return conn, nil
}

// isConnectionHealthy checks if a connection is healthy
func (cp *ConnectionPool) isConnectionHealthy(conn *PooledConnection) bool {
	if conn == nil || conn.Client == nil {
		return false
	}

	conn.mutex.RLock()
	defer conn.mutex.RUnlock()

	// Check if connection is too old
	if time.Since(conn.CreatedAt) > cp.config.MaxLifetime {
		return false
	}

	// Check if connection has been idle too long
	if time.Since(conn.LastUsed) > cp.config.IdleTimeout {
		return false
	}

	return conn.IsHealthy
}

// removeConnection removes and closes a connection
func (cp *ConnectionPool) removeConnection(conn *PooledConnection) {
	if conn == nil {
		return
	}

	// Close the client
	if conn.Client != nil {
		conn.Client.Close()
	}

	// Remove from connections slice
	cp.mutex.Lock()
	for i, c := range cp.connections {
		if c == conn {
			cp.connections = append(cp.connections[:i], cp.connections[i+1:]...)
			break
		}
	}
	cp.mutex.Unlock()

	// Record metrics
	conn.mutex.RLock()
	lifetime := time.Since(conn.CreatedAt)
	conn.mutex.RUnlock()
	cp.connectionLifetime.Observe(lifetime.Seconds())

	cp.logger.Debug("Removed Docker connection from pool")
}

// healthCheckLoop periodically checks connection health
func (cp *ConnectionPool) healthCheckLoop() {
	ticker := time.NewTicker(cp.config.HealthCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-ticker.C:
			cp.performHealthCheck()
		}
	}
}

// performHealthCheck checks all connections health
func (cp *ConnectionPool) performHealthCheck() {
	cp.mutex.RLock()
	connections := make([]*PooledConnection, len(cp.connections))
	copy(connections, cp.connections)
	cp.mutex.RUnlock()

	for _, conn := range connections {
		go func(c *PooledConnection) {
			if !cp.pingConnection(c) {
				c.mutex.Lock()
				c.IsHealthy = false
				c.mutex.Unlock()
				cp.logger.Debug("Connection failed health check")
			}
		}(conn)
	}
}

// pingConnection tests if a connection is responsive
func (cp *ConnectionPool) pingConnection(conn *PooledConnection) bool {
	if conn == nil || conn.Client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(cp.ctx, 5*time.Second)
	defer cancel()

	_, err := conn.Client.Ping(ctx)
	return err == nil
}

// cleanupLoop periodically removes old/idle connections
func (cp *ConnectionPool) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-cp.ctx.Done():
			return
		case <-ticker.C:
			cp.performCleanup()
		}
	}
}

// performCleanup removes unhealthy and old connections
func (cp *ConnectionPool) performCleanup() {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	var healthyConnections []*PooledConnection
	removedCount := 0

	for _, conn := range cp.connections {
		if cp.isConnectionHealthy(conn) {
			healthyConnections = append(healthyConnections, conn)
		} else {
			// Close unhealthy connection
			if conn.Client != nil {
				conn.Client.Close()
			}
			removedCount++
		}
	}

	cp.connections = healthyConnections

	if removedCount > 0 {
		cp.logger.WithField("removed_connections", removedCount).Debug("Cleaned up unhealthy connections")
	}

	// Ensure minimum connections
	for len(cp.connections) < cp.config.MinConnections {
		conn, err := cp.createConnection()
		if err != nil {
			cp.logger.WithError(err).Warn("Failed to create connection during cleanup")
			break
		}
		cp.connections = append(cp.connections, conn)
		cp.available <- conn
	}

	cp.updateMetrics()
}

// updateMetrics updates Prometheus metrics
func (cp *ConnectionPool) updateMetrics() {
	cp.mutex.RLock()
	total := len(cp.connections)
	available := len(cp.available)
	cp.mutex.RUnlock()

	active := total - available

	cp.totalConnections.Set(float64(total))
	cp.activeConnections.Set(float64(active))
	cp.availableConnections.Set(float64(available))
}

// initMetrics initializes Prometheus metrics
func (cp *ConnectionPool) initMetrics() {
	cp.totalConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ssw_logs_capture_docker_pool_total_connections",
		Help: "Total number of connections in the Docker pool",
	})

	cp.activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ssw_logs_capture_docker_pool_active_connections",
		Help: "Number of active connections in the Docker pool",
	})

	cp.availableConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ssw_logs_capture_docker_pool_available_connections",
		Help: "Number of available connections in the Docker pool",
	})

	cp.connectionRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ssw_logs_capture_docker_pool_connection_requests_total",
		Help: "Total number of connection requests to the Docker pool",
	})

	cp.connectionErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ssw_logs_capture_docker_pool_connection_errors_total",
		Help: "Total number of connection errors in the Docker pool",
	})

	cp.connectionLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ssw_logs_capture_docker_pool_connection_duration_seconds",
		Help:    "Time taken to acquire a connection from the Docker pool",
		Buckets: prometheus.DefBuckets,
	})

	cp.connectionLifetime = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ssw_logs_capture_docker_pool_connection_lifetime_seconds",
		Help:    "Lifetime of connections in the Docker pool",
		Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 28800},
	})

	// Register metrics
	prometheus.MustRegister(
		cp.totalConnections,
		cp.activeConnections,
		cp.availableConnections,
		cp.connectionRequests,
		cp.connectionErrors,
		cp.connectionLatency,
		cp.connectionLifetime,
	)
}

// GetStats returns pool statistics
func (cp *ConnectionPool) GetStats() map[string]interface{} {
	cp.mutex.RLock()
	defer cp.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_connections":     len(cp.connections),
		"available_connections": len(cp.available),
		"active_connections":    len(cp.connections) - len(cp.available),
		"max_connections":       cp.config.MaxConnections,
		"min_connections":       cp.config.MinConnections,
		"is_running":           cp.isRunning,
	}

	return stats
}