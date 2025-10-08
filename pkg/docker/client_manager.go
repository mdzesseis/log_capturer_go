package docker

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// ClientManager manages Docker client operations with connection pooling
type ClientManager struct {
	pool   *ConnectionPool
	logger *logrus.Logger
	mutex  sync.RWMutex
}

// NewClientManager creates a new Docker client manager with connection pooling
func NewClientManager(config ConnectionPoolConfig, logger *logrus.Logger) (*ClientManager, error) {
	pool, err := NewConnectionPool(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	return &ClientManager{
		pool:   pool,
		logger: logger,
	}, nil
}

// Start initializes the client manager
func (cm *ClientManager) Start() error {
	return cm.pool.Start()
}

// Stop shuts down the client manager
func (cm *ClientManager) Stop() error {
	return cm.pool.Stop()
}

// withConnection executes a function with a pooled connection
func (cm *ClientManager) withConnection(ctx context.Context, fn func(*client.Client) error) error {
	conn, err := cm.pool.GetConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection from pool: %w", err)
	}
	defer cm.pool.ReturnConnection(conn)

	return fn(conn.Client)
}

// Ping tests connectivity to Docker daemon
func (cm *ClientManager) Ping(ctx context.Context) error {
	return cm.withConnection(ctx, func(cli *client.Client) error {
		_, err := cli.Ping(ctx)
		return err
	})
}

// ContainerList returns a list of containers
func (cm *ClientManager) ContainerList(ctx context.Context, options dockerTypes.ContainerListOptions) ([]dockerTypes.Container, error) {
	var containers []dockerTypes.Container

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		containers, err = cli.ContainerList(ctx, options)
		return err
	})

	return containers, err
}

// ContainerInspect returns container information
func (cm *ClientManager) ContainerInspect(ctx context.Context, containerID string) (dockerTypes.ContainerJSON, error) {
	var containerJSON dockerTypes.ContainerJSON

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		containerJSON, err = cli.ContainerInspect(ctx, containerID)
		return err
	})

	return containerJSON, err
}

// ContainerLogs returns container logs
func (cm *ClientManager) ContainerLogs(ctx context.Context, containerID string, options dockerTypes.ContainerLogsOptions) (io.ReadCloser, error) {
	var response io.ReadCloser

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		response, err = cli.ContainerLogs(ctx, containerID, options)
		return err
	})

	return response, err
}

// Events returns a channel to receive Docker events
func (cm *ClientManager) Events(ctx context.Context, options dockerTypes.EventsOptions) (<-chan events.Message, <-chan error) {
	eventChan := make(chan events.Message)
	errorChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errorChan)

		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := cm.withConnection(ctx, func(cli *client.Client) error {
					events, errs := cli.Events(ctx, options)

					for {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case event, ok := <-events:
							if !ok {
								return fmt.Errorf("events channel closed")
							}
							select {
							case eventChan <- event:
							case <-ctx.Done():
								return ctx.Err()
							}
						case err, ok := <-errs:
							if !ok {
								return fmt.Errorf("error channel closed")
							}
							if err != nil {
								select {
								case errorChan <- err:
								case <-ctx.Done():
									return ctx.Err()
								}
								return err
							}
						}
					}
				})

				if err != nil {
					if ctx.Err() != nil {
						return
					}
					cm.logger.WithError(err).Error("Error in events stream, retrying")
					select {
					case errorChan <- err:
					case <-ctx.Done():
						return
					}
					// Wait before retrying
					time.Sleep(5 * time.Second)
				}
			}
		}
	}()

	return eventChan, errorChan
}

// ContainerStats returns container stats stream
func (cm *ClientManager) ContainerStats(ctx context.Context, containerID string, stream bool) (dockerTypes.ContainerStats, error) {
	var stats dockerTypes.ContainerStats

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		stats, err = cli.ContainerStats(ctx, containerID, stream)
		return err
	})

	return stats, err
}

// Info returns Docker system information
func (cm *ClientManager) Info(ctx context.Context) (dockerTypes.Info, error) {
	var info dockerTypes.Info

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		info, err = cli.Info(ctx)
		return err
	})

	return info, err
}

// Version returns Docker version information
func (cm *ClientManager) Version(ctx context.Context) (dockerTypes.Version, error) {
	var version dockerTypes.Version

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		version, err = cli.ServerVersion(ctx)
		return err
	})

	return version, err
}

// DiskUsage returns Docker disk usage information
func (cm *ClientManager) DiskUsage(ctx context.Context, options dockerTypes.DiskUsageOptions) (dockerTypes.DiskUsage, error) {
	var diskUsage dockerTypes.DiskUsage

	err := cm.withConnection(ctx, func(cli *client.Client) error {
		var err error
		diskUsage, err = cli.DiskUsage(ctx, options)
		return err
	})

	return diskUsage, err
}

// GetPoolStats returns connection pool statistics
func (cm *ClientManager) GetPoolStats() map[string]interface{} {
	return cm.pool.GetStats()
}

// ExecuteWithTimeout executes a function with a connection and timeout
func (cm *ClientManager) ExecuteWithTimeout(timeout time.Duration, fn func(*client.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return cm.withConnection(ctx, fn)
}

// ExecuteWithRetry executes a function with retry logic
func (cm *ClientManager) ExecuteWithRetry(ctx context.Context, maxRetries int, retryDelay time.Duration, fn func(*client.Client) error) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := cm.withConnection(ctx, fn)
		if err == nil {
			return nil
		}

		lastErr = err
		cm.logger.WithError(err).WithField("attempt", attempt+1).Warn("Operation failed, retrying")

		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay * time.Duration(attempt+1)):
				// Continue to next attempt
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", maxRetries+1, lastErr)
}

// HealthCheck performs a comprehensive health check
func (cm *ClientManager) HealthCheck(ctx context.Context) error {
	// Test basic connectivity
	if err := cm.Ping(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Test container listing
	_, err := cm.ContainerList(ctx, dockerTypes.ContainerListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("container list failed: %w", err)
	}

	// Check pool health
	stats := cm.GetPoolStats()
	if totalConns, ok := stats["total_connections"].(int); ok && totalConns == 0 {
		return fmt.Errorf("no connections available in pool")
	}

	return nil
}

// IsHealthy returns whether the client manager is healthy
func (cm *ClientManager) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return cm.HealthCheck(ctx) == nil
}