# SSW Logs Capture - Developer Guide

**Version**: v0.0.2
**Last Updated**: 2025-11-02
**Go Version**: 1.21+
**Target Audience**: Developers contributing to the project

---

## 📋 Table of Contents

- [Introduction](#introduction)
- [Getting Started](#getting-started)
- [Architecture Overview](#architecture-overview)
- [Concurrency Patterns](#concurrency-patterns)
- [Testing Strategy](#testing-strategy)
- [Performance Considerations](#performance-considerations)
- [Security Best Practices](#security-best-practices)
- [Code Organization](#code-organization)
- [Common Patterns & Idioms](#common-patterns--idioms)
- [Troubleshooting & Debugging](#troubleshooting--debugging)
- [Contributing Guidelines](#contributing-guidelines)

---

## 🎯 Introduction

SSW Logs Capture is a high-performance log aggregation system written in Go. This guide provides essential information for developers working on the codebase, including architecture decisions, concurrency patterns, testing strategies, and best practices.

### Key Design Principles

1. **Concurrency-First**: Designed for high-throughput concurrent processing
2. **Reliability**: Graceful degradation, retry mechanisms, and data persistence
3. **Observability**: Comprehensive metrics, tracing, and health checks
4. **Modularity**: Clean separation of concerns with well-defined interfaces
5. **Performance**: Optimized for throughput and low latency
6. **Security**: Defense-in-depth with sanitization, authentication, and TLS

---

## 🚀 Getting Started

### Prerequisites

```bash
# Required
- Go 1.21+
- Docker & Docker Compose
- Make (optional, for build automation)

# Recommended
- golangci-lint (code quality)
- gosec (security scanning)
- govulncheck (vulnerability scanning)
```

### Development Setup

```bash
# Clone repository
git clone https://github.com/your-org/log-capturer.git
cd log-capturer

# Install dependencies
go mod download

# Verify everything works
make test

# Run with race detector (IMPORTANT!)
go test -race ./...

# Build
make build

# Run locally
./bin/ssw-logs-capture --config configs/config.yaml

# Run in Docker
docker-compose up -d
```

### IDE Setup

**VS Code** - Recommended extensions:
```json
{
  "recommendations": [
    "golang.go",
    "ms-vscode.makefile-tools",
    "ms-azuretools.vscode-docker"
  ]
}
```

**GoLand/IntelliJ** - Enable:
- Go modules integration
- Race detector in test configurations
- Code coverage display

---

## 🏗️ Architecture Overview

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                     SSW Logs Capture                         │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐      ┌───────────────┐                   │
│  │  Monitors    │─────▶│  Dispatcher   │                   │
│  │  - Container │      │  - Queue      │                   │
│  │  - File      │      │  - Workers    │                   │
│  └──────────────┘      │  - Batching   │                   │
│                        └───────┬───────┘                   │
│                                │                             │
│                        ┌───────▼───────┐                   │
│                        │  Processing   │                   │
│                        │  - Pipelines  │                   │
│                        │  - Enrichment │                   │
│                        └───────┬───────┘                   │
│                                │                             │
│  ┌──────────────┐      ┌──────▼────────┐                  │
│  │  Sinks       │◀─────│  Sink Manager │                  │
│  │  - Loki      │      │  - Routing    │                  │
│  │  - LocalFile │      │  - DLQ        │                  │
│  └──────────────┘      └───────────────┘                  │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Input Sources** → Monitors detect new log entries
2. **Dispatcher** → Receives entries, queues them, distributes to workers
3. **Processing** → Enriches, validates, and transforms logs
4. **Sink Manager** → Routes to appropriate sinks with retry logic
5. **DLQ** → Captures failed deliveries for later reprocessing

### Key Interfaces

```go
// Monitor interface - all input sources implement this
type Monitor interface {
    Start(ctx context.Context) error
    Stop() error
}

// Sink interface - all output destinations implement this
type Sink interface {
    Send(ctx context.Context, entries []LogEntry) error
    Stop() error
}

// Processor interface - log transformation pipeline
type Processor interface {
    Process(ctx context.Context, entry *LogEntry) error
}
```

---

## 🔐 Concurrency Patterns

**CRITICAL**: This codebase handles high-concurrency workloads. Understanding these patterns is essential for safe code contributions.

### 1. Map Sharing - MUST DeepCopy

**Problem**: Maps are reference types and NOT thread-safe.

```go
// ❌ WRONG - Race condition!
labels := map[string]string{"key": "value"}
entry := types.LogEntry{
    Labels: labels,  // Multiple goroutines may access this!
}

// ✅ CORRECT - Safe copy
labelsCopy := make(map[string]string, len(labels))
for k, v := range labels {
    labelsCopy[k] = v
}
entry := types.LogEntry{
    Labels: labelsCopy,  // Independent copy
}
```

**Locations where this matters**:
- `dispatcher.go:handleLowPriorityEntry()` - Fixed in Phase 2
- `dispatcher.go:Handle()` - Already safe
- Any function passing LogEntry to goroutines

### 2. State Access - MUST Use Mutex

```go
// ❌ WRONG - Data race!
type Worker struct {
    status string  // Multiple goroutines read/write
}

func (w *Worker) SetStatus(s string) {
    w.status = s  // RACE!
}

// ✅ CORRECT - Protected by mutex
type Worker struct {
    mu     sync.RWMutex
    status string
}

func (w *Worker) SetStatus(s string) {
    w.mu.Lock()
    defer w.mu.Unlock()
    w.status = s
}

func (w *Worker) GetStatus() string {
    w.mu.RLock()
    defer w.mu.RUnlock()
    return w.status
}
```

**Best Practices**:
- Use `sync.RWMutex` for read-heavy operations
- Keep critical sections SHORT
- NEVER hold mutex during I/O operations
- Document lock order to avoid deadlocks

### 3. Context Propagation - MUST Respect Context

```go
// ❌ WRONG - Ignores cancellation
func ProcessLogs(logs []LogEntry) error {
    for _, log := range logs {
        process(log)  // Continues even if parent cancelled
    }
    return nil
}

// ✅ CORRECT - Respects context
func ProcessLogs(ctx context.Context, logs []LogEntry) error {
    for _, log := range logs {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if err := process(log); err != nil {
                return err
            }
        }
    }
    return nil
}
```

**Context Guidelines**:
- ALL long-running operations MUST accept `context.Context`
- Check `ctx.Done()` in loops
- Propagate context to called functions
- Use `context.WithTimeout()` for external calls

### 4. Goroutine Lifecycle - MUST Track and Wait

```go
// ❌ WRONG - Goroutine leak!
func (s *Service) Start() {
    go s.worker()  // No way to stop this!
}

// ✅ CORRECT - Tracked and cleanable
type Service struct {
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

func (s *Service) Start() {
    s.ctx, s.cancel = context.WithCancel(context.Background())
    s.wg.Add(1)
    go s.worker()
}

func (s *Service) worker() {
    defer s.wg.Done()
    for {
        select {
        case <-s.ctx.Done():
            return
        case <-time.After(10 * time.Second):
            s.doWork()
        }
    }
}

func (s *Service) Stop() error {
    s.cancel()
    s.wg.Wait()
    return nil
}
```

### 5. Resource Limits - MUST Use Semaphores

```go
// ✅ Limit concurrent operations with semaphore
type RateLimitedProcessor struct {
    sem chan struct{}
}

func NewRateLimitedProcessor(maxConcurrent int) *RateLimitedProcessor {
    return &RateLimitedProcessor{
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (p *RateLimitedProcessor) Process(entry LogEntry) {
    p.sem <- struct{}{}        // Acquire
    defer func() { <-p.sem }() // Release

    // Do work...
}
```

**Example**: Dispatcher retry semaphore limits concurrent retries to prevent resource exhaustion.

### 6. Lock Ordering - MUST Follow Hierarchy

**To avoid deadlocks, always acquire locks in this order**:

1. `Dispatcher.mu`
2. `Sink.mu`
3. `Worker.mu`

```go
// ✅ CORRECT - Consistent order
func (d *Dispatcher) operation() {
    d.mu.Lock()
    defer d.mu.Unlock()

    for _, sink := range d.sinks {
        sink.mu.Lock()
        // work...
        sink.mu.Unlock()
    }
}

// ❌ WRONG - Reverse order causes deadlock!
func (s *Sink) operation(d *Dispatcher) {
    s.mu.Lock()
    defer s.mu.Unlock()

    d.mu.Lock()  // DEADLOCK if Dispatcher also locks in reverse!
    defer d.mu.Unlock()
}
```

### Testing Concurrency

**ALWAYS run tests with race detector**:

```bash
# Unit tests
go test -race ./...

# Specific package
go test -race ./internal/dispatcher

# Stress test
go test -race -count=100 ./...
```

---

## 🧪 Testing Strategy

### Test Coverage Requirements

**Minimum Coverage**: 70% overall
- Critical packages (dispatcher, sinks): ≥ 75%
- Core packages (types, config): ≥ 70%
- Utility packages: ≥ 60%

**Current Status**: 12.5% (as of Phase 9) - See Phase 11 for improvement plan

### Test Types

#### 1. Unit Tests

```go
func TestDispatcherSend(t *testing.T) {
    // Arrange
    d := NewDispatcher(config, logger)
    entry := types.LogEntry{Message: "test"}

    // Act
    err := d.Send(context.Background(), entry)

    // Assert
    require.NoError(t, err)
}
```

**Guidelines**:
- Use `testing.TB` for both tests and benchmarks
- Use `testify/require` for assertions
- Create helper functions for common setup
- Clean up resources in `defer` or `t.Cleanup()`

#### 2. Race Condition Tests

```go
func TestDispatcher_ConcurrentSend(t *testing.T) {
    d := NewDispatcher(config, logger)

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            entry := types.LogEntry{
                Message: fmt.Sprintf("log-%d", id),
            }
            d.Send(context.Background(), entry)
        }(i)
    }
    wg.Wait()
}
```

**Run with**: `go test -race -count=100`

#### 3. Benchmarks

```go
func BenchmarkDispatcher_Send(b *testing.B) {
    d := NewDispatcher(config, logger)
    entry := types.LogEntry{Message: "benchmark"}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        d.Send(context.Background(), entry)
    }
}
```

**Run with**: `go test -bench=. -benchmem`

#### 4. Integration Tests

Located in `tests/integration/`:

```go
func TestEndToEnd_FileToLoki(t *testing.T) {
    // Setup infrastructure
    loki := startMockLoki(t)
    defer loki.Stop()

    // Start log capturer
    app := startApp(t, config)
    defer app.Stop()

    // Write log to monitored file
    writeLog(t, "/tmp/test.log", "test message")

    // Verify log received by Loki
    waitForLog(t, loki, "test message", 5*time.Second)
}
```

#### 5. Mocking

Use interfaces for dependency injection:

```go
// Mock sink for testing
type MockSink struct {
    mock.Mock
}

func (m *MockSink) Send(ctx context.Context, entries []LogEntry) error {
    args := m.Called(ctx, entries)
    return args.Error(0)
}

// Use in tests
func TestWithMockSink(t *testing.T) {
    mockSink := new(MockSink)
    mockSink.On("Send", mock.Anything, mock.Anything).Return(nil)

    // Test code using mockSink
}
```

### Running Tests

```bash
# All tests
go test ./...

# With race detector (REQUIRED before committing!)
go test -race ./...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Specific package
go test -v ./internal/dispatcher

# Benchmarks
go test -bench=. -benchmem ./...

# Integration tests only
go test ./tests/integration/...

# Stress test
go test -race -count=100 -timeout=30m ./...
```

---

## ⚡ Performance Considerations

### 1. Memory Allocations

**Use sync.Pool for frequently allocated objects**:

```go
var logEntryPool = sync.Pool{
    New: func() interface{} {
        return &types.LogEntry{}
    },
}

func acquireLogEntry() *types.LogEntry {
    return logEntryPool.Get().(*types.LogEntry)
}

func releaseLogEntry(e *types.LogEntry) {
    // Clear fields
    e.Message = ""
    e.Labels = nil
    logEntryPool.Put(e)
}
```

**Avoid slice reslicing memory leaks**:

```go
// ❌ WRONG - Keeps entire underlying array in memory
batch = batch[n:]

// ✅ CORRECT - Reallocate when removing items
newBatch := make([]LogEntry, len(batch)-n)
copy(newBatch, batch[n:])
batch = newBatch
```

### 2. Batching

**Adaptive batching** improves throughput:

```go
// Small batches = low latency
// Large batches = high throughput

config.BatchSize = 1000         // Target batch size
config.BatchTimeout = 5 * time.Second  // Max wait time
```

### 3. Worker Pool Sizing

**CPU-bound tasks**: `runtime.NumCPU()`
**I/O-bound tasks**: `2 * runtime.NumCPU()` or higher

```yaml
dispatcher:
  worker_count: 12  # Tune based on workload
```

### 4. Profiling

**CPU Profile**:
```bash
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8080 cpu.prof
```

**Memory Profile**:
```bash
curl http://localhost:6060/debug/pprof/heap > mem.prof
go tool pprof -http=:8080 mem.prof
```

**Goroutine Profile**:
```bash
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof -http=:8080 goroutine.prof
```

### 5. Benchmarking

**Establish baselines** before optimizing:

```bash
# Run benchmarks and save baseline
go test -bench=. -benchmem ./... > baseline.txt

# After optimization, compare
go test -bench=. -benchmem ./... > optimized.txt
benchstat baseline.txt optimized.txt
```

---

## 🔒 Security Best Practices

### 1. Sensitive Data Sanitization

**ALWAYS sanitize logs before writing**:

```go
import "ssw-logs-capture/pkg/security"

// Sanitize log messages
sanitized := security.Sanitize(logMessage)
logger.Info(sanitized)

// Sanitize URLs (removes passwords)
sanitized := security.SanitizeURL("postgres://user:pass@host")
// Output: "postgres://user:****@host"

// Check if contains sensitive data
if security.IsSensitive(message) {
    // Handle specially
}
```

**Sanitized data types**:
- Passwords in URLs
- Bearer tokens
- API keys
- JWT tokens
- Credit card numbers
- Email addresses (optional)
- IP addresses (optional)
- AWS credentials
- SSN/CPF

### 2. Authentication

**API endpoints require authentication** (unless explicitly disabled):

```yaml
security:
  enabled: true
  auth_type: "bearer"  # or "mtls"
  jwt_secret: "${JWT_SECRET}"
```

**Test authenticated endpoints**:
```bash
curl -H "Authorization: Bearer ${TOKEN}" \
  http://localhost:8401/api/endpoint
```

### 3. TLS Configuration

**Production MUST use TLS**:

```yaml
sinks:
  loki:
    url: "https://loki-prod.example.com:3100"
    tls:
      enabled: true
      verify_certificate: true
      ca_file: "/etc/ssl/certs/ca.crt"
      cert_file: "/etc/ssl/certs/client.crt"
      key_file: "/etc/ssl/private/client.key"
```

### 4. Input Validation

**Validate ALL external inputs**:

```go
func ValidateConfig(c *Config) error {
    if c.Dispatcher.QueueSize < 100 || c.Dispatcher.QueueSize > 1000000 {
        return fmt.Errorf("queue_size must be between 100 and 1000000")
    }
    if c.Dispatcher.WorkerCount < 1 || c.Dispatcher.WorkerCount > 128 {
        return fmt.Errorf("worker_count must be between 1 and 128")
    }
    return nil
}
```

### 5. Secrets Management

**NEVER commit secrets**:

```yaml
# ✅ CORRECT - Use environment variables
sinks:
  loki:
    auth:
      token: "${LOKI_TOKEN}"

# ❌ WRONG - Hardcoded secret
sinks:
  loki:
    auth:
      token: "sk_live_abc123"
```

**Check secrets with**:
```bash
# Scan for secrets in commits
git secrets --scan

# Scan codebase
trufflehog filesystem .
```

---

## 📁 Code Organization

### Directory Structure

```
.
├── cmd/                        # Application entry points
│   └── main.go                # Main application
├── internal/                   # Private application code
│   ├── app/                   # Application initialization
│   │   ├── app.go            # Main app struct
│   │   └── handlers.go       # HTTP handlers
│   ├── config/                # Configuration loading
│   ├── dispatcher/            # Log dispatcher
│   │   ├── dispatcher.go     # Main dispatcher logic
│   │   └── dispatcher_test.go
│   ├── monitors/              # Input monitors
│   │   ├── container_monitor.go
│   │   └── file_monitor.go
│   ├── processing/            # Log processors
│   │   └── processor.go
│   ├── sinks/                 # Output sinks
│   │   ├── loki_sink.go
│   │   ├── local_file_sink.go
│   │   └── elasticsearch_sink.go
│   └── metrics/               # Metrics collection
├── pkg/                       # Public packages
│   ├── types/                # Core data structures
│   │   └── types.go
│   ├── circuit/              # Circuit breaker
│   ├── security/             # Security utilities
│   │   ├── auth.go
│   │   └── sanitizer.go
│   ├── anomaly/              # Anomaly detection
│   └── task_manager/         # Task management
├── configs/                   # Configuration files
│   ├── config.yaml
│   └── config.example.yaml
├── docs/                      # Documentation
│   ├── API.md
│   ├── CONFIGURATION.md
│   └── TROUBLESHOOTING.md
├── tests/                     # Integration tests
│   ├── integration/
│   ├── load/
│   └── chaos/
├── provisioning/              # Grafana/Prometheus configs
│   ├── dashboards/
│   └── alerts/
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

### Package Guidelines

**internal/** - Private to this application
- Cannot be imported by external projects
- Implementation details
- Application-specific logic

**pkg/** - Public packages
- Can be imported by others
- Reusable components
- Well-documented APIs

### Naming Conventions

```go
// Types: PascalCase
type LogEntry struct { }

// Interfaces: PascalCase with "er" suffix
type Monitor interface { }
type Processor interface { }

// Functions: camelCase
func processEntry() { }

// Exported functions: PascalCase
func NewDispatcher() { }

// Constants: PascalCase or SCREAMING_SNAKE_CASE
const MaxRetries = 3
const DEFAULT_TIMEOUT = 30 * time.Second

// Private fields: camelCase
type Worker struct {
    status string
}

// Public fields: PascalCase
type Config struct {
    QueueSize int
}
```

---

## 🎨 Common Patterns & Idioms

### 1. Constructor Pattern

```go
func NewComponent(config *Config, logger *Logger, deps ...Dependency) (*Component, error) {
    // Validate inputs
    if config == nil {
        return nil, fmt.Errorf("config is required")
    }

    // Create instance
    c := &Component{
        config: config,
        logger: logger,
        ctx:    context.Background(),
    }

    // Initialize
    if err := c.init(); err != nil {
        return nil, fmt.Errorf("initialization failed: %w", err)
    }

    return c, nil
}
```

### 2. Functional Options Pattern

```go
type Option func(*Component)

func WithTimeout(d time.Duration) Option {
    return func(c *Component) {
        c.timeout = d
    }
}

func NewComponent(opts ...Option) *Component {
    c := &Component{
        timeout: 30 * time.Second, // default
    }
    for _, opt := range opts {
        opt(c)
    }
    return c
}

// Usage
comp := NewComponent(
    WithTimeout(60 * time.Second),
    WithRetries(5),
)
```

### 3. Error Wrapping

```go
// ✅ CORRECT - Preserve error chain
func process() error {
    if err := validate(); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    return nil
}

// Check specific error
if errors.Is(err, ErrNotFound) {
    // Handle not found
}

// Extract error type
var validationErr *ValidationError
if errors.As(err, &validationErr) {
    // Handle validation error
}
```

### 4. Graceful Shutdown

```go
func main() {
    app := NewApp()

    // Start application
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }

    // Wait for interrupt signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    <-sigCh

    // Graceful shutdown with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := app.Stop(ctx); err != nil {
        log.Printf("Shutdown error: %v", err)
    }
}
```

### 5. Logger Pattern

```go
// Use structured logging
logger.Info("processing batch",
    "batch_size", len(batch),
    "sink", sinkName,
    "duration_ms", duration.Milliseconds(),
)

// Use log levels appropriately
logger.Debug("detailed debugging info")    // Development only
logger.Info("normal operation")           // Production events
logger.Warn("degraded performance")       // Potential issues
logger.Error("operation failed",          // Errors
    "error", err,
    "component", "dispatcher",
)
```

---

## 🔧 Troubleshooting & Debugging

### Common Issues

#### 1. Race Conditions

**Symptoms**:
- Intermittent panics
- `fatal error: concurrent map iteration and map write`
- Inconsistent state

**Debug**:
```bash
go test -race ./...
```

**Fix**: See [Concurrency Patterns](#concurrency-patterns)

#### 2. Goroutine Leaks

**Symptoms**:
- Increasing goroutine count over time
- Memory growth
- Slow shutdown

**Debug**:
```bash
# Check goroutine count
curl http://localhost:6060/debug/pprof/goroutine?debug=2

# Profile goroutines
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof goroutine.prof
```

**Fix**: Ensure all goroutines have proper lifecycle management

#### 3. Memory Leaks

**Symptoms**:
- Increasing memory usage
- OOM crashes

**Debug**:
```bash
# Heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8080 heap.prof

# Live heap
go tool pprof http://localhost:6060/debug/pprof/heap
```

**Fix**: Check for slice reslicing leaks, unclosed resources

#### 4. High CPU Usage

**Debug**:
```bash
# CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8080 cpu.prof
```

**Fix**: Optimize hot paths identified in profile

#### 5. Deadlocks

**Symptoms**:
- Application hangs
- No progress

**Debug**:
```bash
# Send SIGQUIT to get goroutine dump
kill -QUIT <pid>

# Or use pprof
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

**Fix**: Review lock ordering, ensure locks are released

### Debugging Tools

#### pprof Endpoints

```bash
# Available at http://localhost:6060/debug/pprof/
- /heap         # Memory allocation
- /goroutine    # Goroutine stack traces
- /profile      # CPU profile
- /block        # Blocking profile
- /mutex        # Mutex contention
- /trace        # Execution tracer
```

#### Metrics

```bash
# Prometheus metrics
curl http://localhost:8001/metrics

# Key metrics:
- log_capturer_goroutines         # Goroutine count
- log_capturer_dispatcher_queue_size
- log_capturer_memory_usage_bytes
- log_capturer_logs_processed_total
```

#### Health Check

```bash
# Detailed health check
curl http://localhost:8401/health | jq '.'

# Check specific components
curl http://localhost:8401/health | jq '.services.dispatcher'
```

#### Logs

```bash
# Enable debug logging
# Edit config.yaml:
app:
  log_level: "debug"

# Reload config
curl -X POST http://localhost:8401/config/reload

# Watch logs
docker-compose logs -f log_capturer
```

### Performance Debugging

```bash
# 1. Get baseline metrics
curl http://localhost:8001/metrics > baseline.txt

# 2. Apply load
./load-test.sh

# 3. Compare metrics
curl http://localhost:8001/metrics > loaded.txt
diff baseline.txt loaded.txt

# 4. Profile during load
curl http://localhost:6060/debug/pprof/profile?seconds=30 > load.prof
go tool pprof -http=:8080 load.prof
```

---

## 🤝 Contributing Guidelines

### Before Submitting

**Checklist**:
- [ ] Run tests: `go test ./...`
- [ ] Run race detector: `go test -race ./...`
- [ ] Run linter: `golangci-lint run`
- [ ] Check coverage: `go test -coverprofile=coverage.out ./...`
- [ ] Update documentation
- [ ] Add tests for new features
- [ ] Follow code style
- [ ] No secrets in commits

### Code Style

**Follow**:
- [Effective Go](https://golang.org/doc/effective_go.html)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

**Key rules**:
```go
// ✅ Good
func ProcessEntry(ctx context.Context, entry *LogEntry) error {
    if entry == nil {
        return ErrNilEntry
    }
    // ... implementation
    return nil
}

// ❌ Bad
func process_entry(e *LogEntry) error {  // Wrong naming
    if e == nil {                       // No context
        return errors.New("nil")        // Not a sentinel error
    }
    return nil
}
```

### Commit Messages

**Format**:
```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style changes (formatting)
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `test`: Adding tests
- `chore`: Maintenance tasks

**Example**:
```
feat(dispatcher): add adaptive batching support

Implement adaptive batching that adjusts batch size based on
queue depth and processing latency. This improves throughput
under high load while maintaining low latency under normal load.

Closes #123
```

### Pull Request Process

1. **Create feature branch**: `git checkout -b feature/my-feature`
2. **Make changes** following code style
3. **Add tests** for new functionality
4. **Run full test suite**: `make test`
5. **Update documentation** if needed
6. **Push to fork**: `git push origin feature/my-feature`
7. **Create PR** with clear description
8. **Address review comments**
9. **Ensure CI passes**
10. **Squash commits** if requested
11. **Wait for approval** from maintainer

### Code Review Checklist

**Reviewer checks**:
- [ ] Code follows style guide
- [ ] Tests are comprehensive
- [ ] Race detector passes
- [ ] Coverage is adequate (≥70%)
- [ ] Documentation is updated
- [ ] No obvious performance issues
- [ ] Security considerations addressed
- [ ] Error handling is appropriate
- [ ] Logging is appropriate
- [ ] No secrets committed

---

## 📚 Additional Resources

### Go Resources

- [Effective Go](https://golang.org/doc/effective_go.html)
- [Go Blog](https://blog.golang.org/)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [Go Memory Model](https://go.dev/ref/mem)

### Project Documentation

- [README.md](README.md) - User guide
- [API.md](docs/API.md) - API reference
- [CONFIGURATION.md](docs/CONFIGURATION.md) - Configuration guide
- [TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) - Troubleshooting guide

### External Documentation

- [Grafana Loki](https://grafana.com/docs/loki/latest/)
- [Prometheus](https://prometheus.io/docs/introduction/overview/)
- [Docker SDK](https://docs.docker.com/engine/api/sdk/)

---

## 📞 Getting Help

**Questions?**
- Check [TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)
- Search [GitHub Issues](https://github.com/your-org/log-capturer/issues)
- Ask in [Discussions](https://github.com/your-org/log-capturer/discussions)

**Found a bug?**
- Check if already reported
- Create detailed bug report with:
  - Go version
  - OS and architecture
  - Steps to reproduce
  - Expected vs actual behavior
  - Relevant logs/metrics

**Want to contribute?**
- Read this guide thoroughly
- Start with "good first issue" label
- Ask questions in the issue before starting

---

**Happy Coding! 🚀**

*This guide is a living document. Please keep it updated as the project evolves.*
