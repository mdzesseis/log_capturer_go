package docker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// PoolManager manages a pool of Docker client connections
type PoolManager struct {
	clients     []*PooledClient
	currentIdx  int
	mutex       sync.RWMutex
	logger      *logrus.Logger
	poolSize    int
	socketPath  string
	maxRetries  int
	retryDelay  time.Duration

	// Health monitoring
	healthCheckInterval time.Duration
	unhealthyClients    map[int]time.Time
	healthMutex        sync.RWMutex

	// C3: Goroutine Leak Fix - Add context and waitgroup for proper shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// PooledClient wraps a Docker client with connection tracking
type PooledClient struct {
	client        *client.Client
	id            int
	inUse         bool
	lastUsed      time.Time
	usageCount    int64
	healthy       bool
	mutex         sync.RWMutex
}

// PoolConfig configuration for Docker connection pool
type PoolConfig struct {
	PoolSize              int           `yaml:"pool_size"`
	SocketPath            string        `yaml:"socket_path"`
	MaxRetries            int           `yaml:"max_retries"`
	RetryDelay            time.Duration `yaml:"retry_delay"`
	HealthCheckInterval   time.Duration `yaml:"health_check_interval"`
	ConnectionTimeout     time.Duration `yaml:"connection_timeout"`
	IdleTimeout          time.Duration `yaml:"idle_timeout"`
}

// NewPoolManager creates a new Docker connection pool manager
func NewPoolManager(config PoolConfig, logger *logrus.Logger) (*PoolManager, error) {
	if config.PoolSize <= 0 {
		config.PoolSize = 5
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 5 * time.Second
	}

	// C3: Goroutine Leak Fix - Create context for coordinated shutdown
	ctx, cancel := context.WithCancel(context.Background())

	pm := &PoolManager{
		clients:             make([]*PooledClient, 0, config.PoolSize),
		logger:              logger,
		poolSize:            config.PoolSize,
		socketPath:          config.SocketPath,
		maxRetries:          config.MaxRetries,
		retryDelay:          config.RetryDelay,
		healthCheckInterval: config.HealthCheckInterval,
		unhealthyClients:    make(map[int]time.Time),
		ctx:                 ctx,
		cancel:              cancel,
	}

	// Initialize connection pool
	if err := pm.initializePool(); err != nil {
		cancel() // Clean up context on error
		return nil, fmt.Errorf("failed to initialize Docker connection pool: %w", err)
	}

	// C3: Start health monitoring with goroutine tracking
	pm.wg.Add(1)
	go pm.healthMonitor()

	return pm, nil
}

// initializePool creates the initial pool of Docker clients
func (pm *PoolManager) initializePool() error {
	for i := 0; i < pm.poolSize; i++ {
		dockerClient, err := pm.createClient()
		if err != nil {
			pm.logger.WithError(err).WithField("client_id", i).Warn("Failed to create Docker client")
			continue
		}

		pooledClient := &PooledClient{
			client:     dockerClient,
			id:         i,
			inUse:      false,
			lastUsed:   time.Now(),
			healthy:    true,
		}

		pm.clients = append(pm.clients, pooledClient)
	}

	if len(pm.clients) == 0 {
		return fmt.Errorf("failed to create any Docker clients")
	}

	pm.logger.WithField("pool_size", len(pm.clients)).Info("Docker connection pool initialized")
	return nil
}

// createClient creates a new Docker client
func (pm *PoolManager) createClient() (*client.Client, error) {
	// Use FromEnv to pick up standard Docker environment variables
	// and then override with custom socket path if provided
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	// Only override host if a custom socket path is provided
	if pm.socketPath != "" && pm.socketPath != "unix:///var/run/docker.sock" {
		opts = append(opts, client.WithHost(pm.socketPath))
	}

	return client.NewClientWithOpts(opts...)
}

// GetClient returns a healthy client from the pool
func (pm *PoolManager) GetClient(ctx context.Context) (*PooledClient, error) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Try to find a healthy, non-busy client
	for attempts := 0; attempts < pm.poolSize*2; attempts++ {
		client := pm.clients[pm.currentIdx]
		pm.currentIdx = (pm.currentIdx + 1) % len(pm.clients)

		client.mutex.Lock()
		if client.healthy && !client.inUse {
			client.inUse = true
			client.lastUsed = time.Now()
			client.usageCount++
			client.mutex.Unlock()
			return client, nil
		}
		client.mutex.Unlock()
	}

	// If no available client, try to create a temporary one
	if len(pm.clients) < pm.poolSize*2 { // Allow some expansion under load
		dockerClient, err := pm.createClient()
		if err == nil {
			tempClient := &PooledClient{
				client:     dockerClient,
				id:         len(pm.clients),
				inUse:      true,
				lastUsed:   time.Now(),
				usageCount: 1,
				healthy:    true,
			}
			return tempClient, nil
		}
	}

	return nil, fmt.Errorf("no healthy Docker clients available in pool")
}

// ReleaseClient returns a client to the pool
func (pm *PoolManager) ReleaseClient(pooledClient *PooledClient) {
	pooledClient.mutex.Lock()
	defer pooledClient.mutex.Unlock()

	pooledClient.inUse = false
	pooledClient.lastUsed = time.Now()
}

// healthMonitor periodically checks the health of clients in the pool
func (pm *PoolManager) healthMonitor() {
	defer pm.wg.Done() // C3: Signal completion when goroutine exits
	defer pm.logger.Debug("Health monitor goroutine terminated")

	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			// C3: Stop health monitoring when context is cancelled
			return
		case <-ticker.C:
			pm.checkClientHealth()
			pm.replaceUnhealthyClients()
		}
	}
}

// checkClientHealth checks the health of all clients
func (pm *PoolManager) checkClientHealth() {
	pm.mutex.RLock()
	clients := make([]*PooledClient, len(pm.clients))
	copy(clients, pm.clients)
	pm.mutex.RUnlock()

	// C3: Goroutine Leak Fix - Track health check goroutines with WaitGroup
	var healthCheckWg sync.WaitGroup
	for _, pooledClient := range clients {
		healthCheckWg.Add(1)
		go func(pc *PooledClient) {
			defer healthCheckWg.Done()
			pm.checkSingleClientHealth(pc)
		}(pooledClient)
	}

	// C3: Wait for all health checks to complete with timeout
	done := make(chan struct{})
	go func() {
		healthCheckWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All health checks completed
	case <-time.After(30 * time.Second):
		pm.logger.Warn("Timeout waiting for health checks to complete")
	case <-pm.ctx.Done():
		// Pool is shutting down
		return
	}
}

// checkSingleClientHealth checks health of a single client
func (pm *PoolManager) checkSingleClientHealth(pooledClient *PooledClient) {
	pooledClient.mutex.RLock()
	if pooledClient.inUse {
		pooledClient.mutex.RUnlock()
		return // Skip busy clients
	}
	client := pooledClient.client
	clientID := pooledClient.id
	pooledClient.mutex.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Simple health check - try to ping Docker daemon
	_, err := client.Ping(ctx)

	pooledClient.mutex.Lock()
	wasHealthy := pooledClient.healthy
	pooledClient.healthy = (err == nil)
	pooledClient.mutex.Unlock()

	if err != nil && wasHealthy {
		pm.logger.WithError(err).WithField("client_id", clientID).Warn("Docker client became unhealthy")
		pm.markClientUnhealthy(clientID)
	} else if err == nil && !wasHealthy {
		pm.logger.WithField("client_id", clientID).Info("Docker client recovered")
		pm.markClientHealthy(clientID)
	}
}

// markClientUnhealthy marks a client as unhealthy
func (pm *PoolManager) markClientUnhealthy(clientID int) {
	pm.healthMutex.Lock()
	defer pm.healthMutex.Unlock()
	pm.unhealthyClients[clientID] = time.Now()
}

// markClientHealthy marks a client as healthy
func (pm *PoolManager) markClientHealthy(clientID int) {
	pm.healthMutex.Lock()
	defer pm.healthMutex.Unlock()
	delete(pm.unhealthyClients, clientID)
}

// replaceUnhealthyClients replaces clients that have been unhealthy for too long
func (pm *PoolManager) replaceUnhealthyClients() {
	pm.healthMutex.RLock()
	unhealthyClients := make(map[int]time.Time)
	for id, timestamp := range pm.unhealthyClients {
		unhealthyClients[id] = timestamp
	}
	pm.healthMutex.RUnlock()

	threshold := time.Now().Add(-5 * time.Minute) // Replace clients unhealthy for 5+ minutes

	for clientID, unhealthyTime := range unhealthyClients {
		if unhealthyTime.Before(threshold) {
			pm.replaceClient(clientID)
		}
	}
}

// replaceClient replaces a specific client in the pool
func (pm *PoolManager) replaceClient(clientID int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if clientID >= len(pm.clients) {
		return
	}

	oldClient := pm.clients[clientID]
	oldClient.mutex.Lock()
	if oldClient.inUse {
		oldClient.mutex.Unlock()
		return // Don't replace busy clients
	}

	// Close old client
	if oldClient.client != nil {
		oldClient.client.Close()
	}
	oldClient.mutex.Unlock()

	// Create new client
	newDockerClient, err := pm.createClient()
	if err != nil {
		pm.logger.WithError(err).WithField("client_id", clientID).Error("Failed to replace unhealthy Docker client")
		return
	}

	newClient := &PooledClient{
		client:     newDockerClient,
		id:         clientID,
		inUse:      false,
		lastUsed:   time.Now(),
		healthy:    true,
	}

	pm.clients[clientID] = newClient
	pm.markClientHealthy(clientID)

	pm.logger.WithField("client_id", clientID).Info("Replaced unhealthy Docker client")
}

// GetPoolStatus returns the current status of the connection pool
func (pm *PoolManager) GetPoolStatus() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var healthyCount, inUseCount, totalUsage int64
	var oldestLastUsed time.Time = time.Now()
	var newestLastUsed time.Time

	for _, client := range pm.clients {
		client.mutex.RLock()
		if client.healthy {
			healthyCount++
		}
		if client.inUse {
			inUseCount++
		}
		totalUsage += client.usageCount

		if client.lastUsed.Before(oldestLastUsed) {
			oldestLastUsed = client.lastUsed
		}
		if client.lastUsed.After(newestLastUsed) {
			newestLastUsed = client.lastUsed
		}
		client.mutex.RUnlock()
	}

	pm.healthMutex.RLock()
	unhealthyCount := len(pm.unhealthyClients)
	pm.healthMutex.RUnlock()

	return map[string]interface{}{
		"pool_size":        len(pm.clients),
		"healthy_clients":  healthyCount,
		"in_use_clients":   inUseCount,
		"unhealthy_clients": unhealthyCount,
		"total_usage":      totalUsage,
		"oldest_last_used": oldestLastUsed.Format(time.RFC3339),
		"newest_last_used": newestLastUsed.Format(time.RFC3339),
	}
}

// Close closes all clients in the pool
func (pm *PoolManager) Close() error {
	// C3: Goroutine Leak Fix - Cancel context to stop health monitor
	pm.cancel()

	// C3: Wait for health monitor goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		pm.logger.Info("Health monitor goroutine stopped cleanly")
	case <-time.After(10 * time.Second):
		pm.logger.Warn("Timeout waiting for health monitor to stop")
	}

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	var lastError error
	for _, pooledClient := range pm.clients {
		pooledClient.mutex.Lock()
		if pooledClient.client != nil {
			if err := pooledClient.client.Close(); err != nil {
				lastError = err
				pm.logger.WithError(err).WithField("client_id", pooledClient.id).Error("Failed to close Docker client")
			}
		}
		pooledClient.mutex.Unlock()
	}

	pm.clients = nil
	return lastError
}

// Wrapper methods to maintain interface compatibility

// ContainerList wraps Docker ContainerList with connection pooling
func (pm *PoolManager) ContainerList(ctx context.Context, options types.ContainerListOptions) ([]types.Container, error) {
	client, err := pm.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	defer pm.ReleaseClient(client)

	return client.client.ContainerList(ctx, options)
}

// ContainerLogs wraps Docker ContainerLogs with connection pooling
func (pm *PoolManager) ContainerLogs(ctx context.Context, containerID string, options types.ContainerLogsOptions) (io.ReadCloser, error) {
	client, err := pm.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	// Note: We don't release the client here because the ReadCloser needs to stay open
	// The caller should call ReleaseClient when done with the stream

	stream, err := client.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		pm.ReleaseClient(client)
		return nil, err
	}

	// Wrap the ReadCloser to release the client when closed
	return &pooledReadCloser{
		ReadCloser: stream,
		client:     client,
		pool:       pm,
	}, nil
}

// ContainerInspect wraps Docker ContainerInspect with connection pooling
func (pm *PoolManager) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	client, err := pm.GetClient(ctx)
	if err != nil {
		return types.ContainerJSON{}, err
	}
	defer pm.ReleaseClient(client)

	return client.client.ContainerInspect(ctx, containerID)
}

// Events wraps Docker Events with connection pooling
func (pm *PoolManager) Events(ctx context.Context, options types.EventsOptions) (<-chan events.Message, <-chan error) {
	client, err := pm.GetClient(ctx)
	if err != nil {
		errChan := make(chan error, 1)
		errChan <- err
		close(errChan)
		return nil, errChan
	}
	// Note: We don't release the client here because the event stream needs to stay open
	// The event monitoring should manage client lifecycle

	eventChan, errChan := client.client.Events(ctx, options)
	return eventChan, errChan
}

// pooledReadCloser wraps a ReadCloser to release the client when closed
type pooledReadCloser struct {
	io.ReadCloser
	client *PooledClient
	pool   *PoolManager
}

func (prc *pooledReadCloser) Close() error {
	err := prc.ReadCloser.Close()
	prc.pool.ReleaseClient(prc.client)
	return err
}