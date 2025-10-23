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