# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SSW Logs Capture is a high-performance log aggregation and monitoring system written in Go. It replaces a Python version with significant improvements in performance, memory usage, and stability. The system monitors Docker containers and files, processes logs through configurable pipelines, and sends them to various sinks (Loki, Elasticsearch, Splunk, local files).

## Key Architecture Components

### Core Structure
- **Entry Point**: `cmd/main.go` - Simple main that creates and runs the App
- **Application Core**: `internal/app/app.go` - Main orchestration, HTTP server, graceful shutdown
- **Configuration**: `internal/config/` - YAML-based configuration with environment variable support
- **Task Management**: `pkg/task_manager/` - Goroutine lifecycle management with health checking
- **Dispatcher**: `internal/dispatcher/` - Central log routing with batching and worker pools
- **Monitors**: `internal/monitors/` - FileMonitor (fsnotify) and ContainerMonitor (Docker API)
- **Processing**: `internal/processing/` - Configurable pipeline system (regex, JSON parsing, field manipulation)
- **Sinks**: `internal/sinks/` - Output destinations with circuit breakers and retry logic

### Key Interfaces
- **Monitor**: Start/Stop lifecycle with health checking (`pkg/types/types.go:39`)
- **Sink**: Send logs with batching support (`pkg/types/types.go:47`)
- **Dispatcher**: Central routing for log entries (`internal/dispatcher/`)
- **StepProcessor**: Pipeline processing steps (`internal/processing/`)

### Data Flow
1. Monitors (File/Container) detect log events
2. LogEntry structs created with metadata (trace_id, labels, fields)
3. Dispatcher queues entries and batches them to workers
4. Processing pipeline transforms logs based on YAML config
5. Sinks send processed logs to destinations with circuit breaker protection

## Development Commands

### Local Development
```bash
# Build application
go build -o ssw-logs-capture ./cmd/main.go

# Run locally with default config
go run ./cmd/main.go

# Run with custom config file
go run ./cmd/main.go -config configs/custom.yaml

# Run tests (unit only)
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...

# Format code
go fmt ./...

# Update dependencies
go mod download && go mod tidy
```

### Development Scripts
```bash
# Use development helper script (recommended)
./scripts_go/dev.sh build     # Build application
./scripts_go/dev.sh run       # Run locally
./scripts_go/dev.sh test      # Run tests
./scripts_go/dev.sh fmt       # Format code
./scripts_go/dev.sh lint      # Run golangci-lint
./scripts_go/dev.sh clean     # Clean temporary files
./scripts_go/dev.sh health    # Check running application health
```

### Docker Development
```bash
# Build and run with Docker Compose
docker-compose up --build

# Run specific services
docker-compose up loki grafana prometheus

# View logs
docker-compose logs -f log_capturer_go

# Stop all services
docker-compose down
```

### Testing
```bash
# Comprehensive test runner
./test-scripts/test-runner.sh all          # All tests + coverage
./test-scripts/test-runner.sh unit         # Unit tests only
./test-scripts/test-runner.sh integration  # Integration tests only
./test-scripts/test-runner.sh coverage     # Tests with coverage report
./test-scripts/test-runner.sh performance  # Performance benchmarks
```

## Configuration

Configuration uses YAML files with environment variable overrides:
- **Main config**: `configs/config.yaml` - Application settings, server, monitoring
- **Pipelines**: `configs/pipelines.yaml` - Log processing rules
- **File monitoring**: `configs/file_pipeline.yml` - File-specific monitoring rules

Key environment variables:
- `SSW_CONFIG_FILE` - Override config file path
- `SERVER_HOST`/`SERVER_PORT` - API server settings
- `LOKI_URL` - Loki endpoint
- `LOG_LEVEL` - Logging level (debug, info, warn, error)

## API Endpoints

### Health and Status
- `GET /health` - Basic health check
- `GET /health/detailed` - Detailed component health
- `GET /status` - Dispatcher statistics
- `GET /task/status` - Task manager status
- `GET /metrics` - Prometheus metrics (port 8001)

### File Monitoring Management
- `GET /monitored/files` - List monitored files
- `POST /monitor/file` - Add file to monitoring
- `DELETE /monitor/file/{task_name}` - Remove file monitoring

## Adding New Components

### New Sink Implementation
1. Implement `types.Sink` interface in `internal/sinks/`
2. Add configuration struct to `types.Config.Sinks`
3. Register in `internal/app/app.go` initialization
4. Add environment variable support in config

### New Processing Step
1. Implement `StepProcessor` interface in `internal/processing/`
2. Register in `log_processor.go` step compiler
3. Add configuration schema to `pipelines.yaml`

### New Monitor Type
1. Implement `types.Monitor` interface in `internal/monitors/`
2. Add to App struct and initialization in `internal/app/app.go`
3. Register with task manager for lifecycle management

## Important Packages

### High-Level Packages (`internal/`)
- `app/` - Main application orchestration and HTTP API
- `config/` - Configuration loading and validation
- `dispatcher/` - Central log routing and batching
- `monitors/` - File and container log monitoring
- `processing/` - Log transformation pipeline
- `sinks/` - Output destination implementations
- `metrics/` - Prometheus metrics collection

### Utility Packages (`pkg/`)
- `types/` - Core interfaces and data structures
- `task_manager/` - Goroutine lifecycle management
- `circuit/` - Circuit breaker implementation
- `compression/` - Log compression utilities
- `positions/` - File position tracking for resume capability
- `buffer/` - Disk-based buffering for reliability
- `dlq/` - Dead letter queue for failed log processing
- `leakdetection/` - Resource leak monitoring

## Performance Characteristics

This Go rewrite provides:
- ~60% reduction in memory usage vs Python version
- ~70% faster startup time
- ~3x higher log throughput (10K+ logs/second)
- Native concurrency with goroutines
- Type safety preventing runtime errors
- Better resource management with explicit cleanup

The system is designed for high-throughput log processing with proper backpressure handling, circuit breakers for resilience, and comprehensive monitoring.

## Known Issues and Limitations

**⚠️ IMPORTANT:** See `CODE_REVIEW_REPORT.md` for a comprehensive analysis of 42 identified issues.

### Critical Issues (Production Blockers)
1. **Concurrency Problems**: Race conditions in task manager, local file sink, and map access
2. **Resource Leaks**: Goroutines, file descriptors, and memory leaks in multiple components
3. **Deadlock Potential**: Circuit breaker holds mutex during execution, disk space checks can deadlock
4. **Context Handling**: Anomaly detector and sinks don't properly cancel contexts

### High-Priority Fixes Needed
- Circuit breaker mutex must not be held during function execution
- Task manager needs atomic updates to prevent race conditions
- Local file sink requires file descriptor limits and proper locking
- All concurrent map access must be protected with mutexes or use sync.Map

### Current Production Readiness: 🔴 NOT RECOMMENDED
The system has critical concurrency issues that can cause crashes and data loss. Recommended actions:
1. Fix all critical concurrency issues (C1-C12 in code review)
2. Add comprehensive integration tests
3. Perform load testing with 100k+ logs/second
4. Add chaos engineering tests for failure scenarios

## Code Quality Guidelines

### Concurrency Best Practices

#### ✅ DO
```go
// Use defer for unlocking
func (s *Sink) operation() {
    s.mu.Lock()
    defer s.mu.Unlock()
    // operations
}

// Copy maps before iteration in concurrent contexts
func processLabels(entry types.LogEntry) {
    labelsCopy := make(map[string]string, len(entry.Labels))
    for k, v := range entry.Labels {
        labelsCopy[k] = v
    }
    // work with labelsCopy
}

// Always respect context cancellation
select {
case <-ctx.Done():
    return ctx.Err()
case result := <-ch:
    return result
}
```

#### ❌ DON'T
```go
// Never hold locks during slow operations
func (b *Breaker) Execute(fn func() error) error {
    b.mu.Lock()
    defer b.mu.Unlock()  // BAD: Lock held during fn()
    err := fn()  // This can take seconds!
    return err
}

// Never manually unlock in deferred context
func badPattern() {
    mu.RLock()
    defer mu.RUnlock()

    if condition {
        mu.RUnlock()  // BAD: Manual unlock
        operation()
        mu.RLock()    // BAD: Manual relock
    }
}

// Never ignore context cancellation
for {
    // BAD: No ctx.Done() check
    item := <-ch
    process(item)
}
```

### Resource Management

#### File Handles
```go
// Always limit open file descriptors
const maxOpenFiles = 100

if len(openFiles) >= maxOpenFiles {
    closeLeastRecentlyUsed()
}
```

#### Goroutines
```go
// Always track spawned goroutines
var wg sync.WaitGroup

wg.Add(1)
go func() {
    defer wg.Done()
    worker()
}()

wg.Wait()  // Ensure cleanup
```

#### Memory
```go
// Don't rely on slice reslicing for memory management
// BAD: old array still in memory
buffer = buffer[removeCount:]

// GOOD: reallocate to free memory
newBuffer := make([]T, targetSize)
copy(newBuffer, buffer[removeCount:])
buffer = newBuffer
```

## Common Pitfalls

### 1. Map Concurrent Access
**Problem:** `entry.Labels` accessed by multiple goroutines without protection
```go
// BAD
for k, v := range entry.Labels {  // panic: concurrent map iteration and map write
    process(k, v)
}

// GOOD
labelsCopy := entry.CopyLabels()  // Thread-safe method
for k, v := range labelsCopy {
    process(k, v)
}
```

### 2. Context Without Cancellation
**Problem:** Background contexts that can't be stopped
```go
// BAD
ctx := context.Background()  // No way to cancel

// GOOD
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
```

### 3. Configuration Validation
**Problem:** Invalid configs crash the application
```go
// ALWAYS validate before use
if config.QueueSize < 100 || config.QueueSize > 1000000 {
    return fmt.Errorf("queue_size must be between 100 and 1000000")
}
```

### 4. Error Handling in Defer
**Problem:** Errors in defer are lost
```go
// BAD
defer file.Close()  // Error ignored

// GOOD
defer func() {
    if err := file.Close(); err != nil {
        logger.WithError(err).Error("Failed to close file")
    }
}()
```

## Debugging Tips

### Detecting Race Conditions
```bash
# Run with race detector (slower but catches races)
go test -race ./...
go run -race ./cmd/main.go
```

### Memory Leak Detection
```bash
# Use pprof to detect leaks
go tool pprof http://localhost:8001/debug/pprof/heap

# Compare memory snapshots
curl http://localhost:8001/debug/pprof/heap > heap1.prof
# ... wait and generate load ...
curl http://localhost:8001/debug/pprof/heap > heap2.prof
go tool pprof -base=heap1.prof heap2.prof
```

### Goroutine Leak Detection
```bash
# Check running goroutines
curl http://localhost:8001/debug/pprof/goroutine?debug=1

# Profile goroutines
go tool pprof http://localhost:8001/debug/pprof/goroutine
```

### Deadlock Detection
```bash
# Get full goroutine dump during hang
curl http://localhost:8001/debug/pprof/goroutine?debug=2
```

## Testing Guidelines

### Unit Tests
- Mock external dependencies (Docker, filesystem, HTTP)
- Test error paths, not just happy path
- Use table-driven tests for multiple scenarios
- Aim for 70%+ code coverage

### Integration Tests
```go
func TestFullPipeline(t *testing.T) {
    // Start real components
    app := setupTestApp(t)
    defer app.Cleanup()

    // Send logs through full pipeline
    // Verify output in sinks
}
```

### Load Tests
- Test with 10k-100k logs/second
- Monitor memory usage over time
- Verify backpressure works correctly
- Test recovery from failures

## Performance Optimization

### Hot Paths
1. **Dispatcher.Send**: Most frequent operation
   - Minimize allocations
   - Use object pooling
   - Batch operations

2. **Pipeline Processing**: CPU intensive
   - Cache compiled regexes
   - Reuse buffers
   - Avoid string concatenation

3. **Sink Operations**: I/O bound
   - Use connection pooling
   - Enable compression
   - Batch writes

### Profiling
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof

# Benchmarking
go test -bench=. -benchmem -benchtime=10s
```

## Security Considerations

### Sensitive Data
- Never log credentials, tokens, or API keys
- Sanitize URLs before logging: `sanitizeURL(url)`
- Redact sensitive fields in log messages
- Use secrets manager for credentials

### Input Validation
```go
// Validate all external inputs
func validateConfig(cfg Config) error {
    if cfg.QueueSize < minQueueSize || cfg.QueueSize > maxQueueSize {
        return ErrInvalidQueueSize
    }
    // ... more validations
}
```

### Resource Limits
- Set maximum file sizes
- Limit concurrent connections
- Implement rate limiting
- Add circuit breakers for external services

## Migration from Python Version

### Key Differences
1. **Concurrency Model**: Goroutines vs asyncio/threads
2. **Memory Management**: Manual vs GC (different patterns)
3. **Type Safety**: Static typing catches errors at compile time
4. **Performance**: 3x throughput, 60% less memory

### Migration Checklist
- [ ] Update configuration format to YAML
- [ ] Migrate custom processing logic to Go
- [ ] Test with same log volume as production
- [ ] Verify all pipeline transformations match
- [ ] Update monitoring dashboards
- [ ] Train team on Go-specific debugging

## Contributing

### Before Submitting PR
1. Run `go fmt ./...`
2. Run `go vet ./...`
3. Run `go test -race ./...`
4. Add/update tests for changes
5. Update documentation if needed
6. Check code review report for similar issues

### Code Review Focus
- Thread safety and race conditions
- Resource leaks (goroutines, files, memory)
- Error handling completeness
- Context propagation and cancellation
- Performance implications

## Troubleshooting

### High Memory Usage
1. Check for goroutine leaks: `curl http://localhost:8001/debug/pprof/goroutine?debug=1`
2. Profile heap: `go tool pprof http://localhost:8001/debug/pprof/heap`
3. Review buffer sizes in configuration
4. Check DLQ size

### Slow Log Processing
1. Check queue utilization: `GET /status`
2. Monitor sink latency metrics
3. Verify circuit breakers aren't open
4. Check for CPU saturation (regex processing)

### Logs Not Appearing in Loki
1. Check Loki sink health: `GET /health/detailed`
2. Verify Loki connectivity
3. Check for label cardinality issues
4. Review pipeline processing logs
5. Inspect DLQ for failed entries

### System Crashes
1. Check for race conditions: run with `-race` flag
2. Review panic stack traces in logs
3. Check resource limits (file descriptors, memory)
4. Verify configuration values are valid