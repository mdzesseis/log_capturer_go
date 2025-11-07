# Diagnostic Tools

This directory contains diagnostic and analysis tools for the log_capturer_go project.

---

## HTTP Transport Diagnostic Tool

### Purpose

Analyzes and verifies HTTP Transport configuration to ensure MaxConnsPerHost is correctly set and preventing goroutine leaks.

### Quick Start

```bash
# Run diagnostic
./run_http_diagnostic.sh

# Or run directly
../bin/http_transport_diagnostic
```

### What It Tests

1. **Loki Sink Configuration** - Verifies MaxConnsPerHost is set in internal/sinks/loki_sink.go
2. **Docker Pool Configuration** - Verifies MaxConnsPerHost is set in pkg/docker/connection_pool.go
3. **Runtime Enforcement** - Tests if MaxConnsPerHost limits are actually enforced
4. **Connection Reuse** - Verifies connection pooling is working
5. **Goroutine Leak Detection** - Monitors goroutine count during load
6. **Performance Benchmark** - Compares different MaxConnsPerHost configurations
7. **Docker Client Verification** - Tests real Docker client (if daemon available)

### Output

The tool generates a JSON report with detailed results:

```json
{
  "timestamp": "2025-11-06T...",
  "go_version": "go1.24.9",
  "overall_status": "PASS",
  "summary": "Diagnostic completed: 5 passed, 0 failed, 1 warnings",
  "results": [...]
}
```

### Files

- `http_transport_diagnostic.go` - Diagnostic tool source code
- `run_http_diagnostic.sh` - Runner script with formatted output
- `../bin/http_transport_diagnostic` - Compiled binary

### Documentation

- **Quick Reference**: `/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_QUICK_REFERENCE.md`
- **Full Analysis**: `/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_ANALYSIS.md`
- **Diagnostic Summary**: `/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_DIAGNOSTIC_SUMMARY.md`

---

## Building Tools

```bash
# Build all tools
make build-tools

# Or build individually
go build -o ../bin/http_transport_diagnostic http_transport_diagnostic.go
```

---

## Adding New Tools

1. Create new Go file in this directory
2. Add build command to Makefile
3. Create corresponding documentation in docs/
4. Update this README

---

## Tool Naming Convention

- Source: `<tool_name>.go`
- Binary: `../bin/<tool_name>`
- Runner: `run_<tool_name>.sh`
- Docs: `../docs/<TOOL_NAME>_*.md`

---

## Future Tools (Planned)

- Memory leak detector
- Configuration validator
- Performance profiler
- Log analyzer
- Metrics exporter

---

## Contributing

When adding new diagnostic tools:

1. Follow Go best practices
2. Include comprehensive tests
3. Document expected output
4. Provide usage examples
5. Add to CI/CD pipeline if applicable
