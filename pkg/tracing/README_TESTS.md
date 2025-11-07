# Tracing Package Tests

## Running Tests

Due to Prometheus metrics being registered globally,  tests MUST be run one of these ways:

### Option 1: Run Single Test at a Time (Recommended for Development)
```bash
# Test mode switching
go test -run TestTracingManager_ModeSwitching ./pkg/tracing/

# Test adaptive sampling
go test -run TestTracingManager_AdaptiveSampling ./pkg/tracing/

# Test on-demand
go test -run TestTracingManager_OnDemand ./pkg/tracing/
```

### Option 2: Run All Tests Sequentially
```bash
# This prevents parallel execution and Prometheus registration conflicts
go test -p 1 ./pkg/tracing/
```

### Option 3: Run with Race Detector (for concurrency tests)
```bash
go test -p 1 -race ./pkg/tracing/
```

## Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./pkg/tracing/

# Run specific benchmark
go test -bench=BenchmarkTracingOff -benchmem ./pkg/tracing/

# Save results
go test -bench=. -benchmem ./pkg/tracing/ > tracing_bench_results.txt
```

## Known Limitation

The Prometheus client library (used for metrics) registers metrics globally on first use.
When multiple `EnhancedTracingManager` instances are created in the same test process,
the metrics are registered multiple times, causing a panic.

**Workaround**: Tests create only one manager instance per test function, or run tests sequentially.

**Future Fix**: Refactor metrics to use a custom registry per manager instance instead of the global registry.

## Test Coverage

The test suite covers:

### Unit Tests (`tracing_test.go`)
- ✅ Mode switching (OFF, SYSTEM-ONLY, HYBRID, FULL-E2E)
- ✅ Adaptive sampling activation and probability
- ✅ On-demand tracing enable/disable/expiration
- ✅ Multiple concurrent on-demand rules
- ✅ Thread-safety (concurrent access)
- ✅ Span creation and context propagation
- ✅ Hybrid mode sampling probability (1%, 10%, 50%)
- ✅ Disabled tracing behavior
- ✅ Metrics recording
- ✅ Nil entry handling

### Benchmarks (`tracing_bench_test.go`)
- ✅ Mode overhead comparison (OFF, SYSTEM-ONLY, HYBRID 0/1/10/50/100%, FULL-E2E)
- ✅ Span creation overhead
- ✅ Span creation with attributes overhead
- ✅ On-demand check overhead (single rule and multiple rules)
- ✅ Adaptive sampling check overhead
- ✅ Concurrent tracing decisions
- ✅ Concurrent span creation
- ✅ Memory allocation profiling per mode
- ✅ Log entry size impact
- ✅ End-to-end flow benchmark

## Benchmark Results Interpretation

Expected results (approximate):

| Mode | ns/op | B/op | allocs/op | Description |
|------|-------|------|-----------|-------------|
| OFF | 2-5 ns | 0 | 0 | Minimal overhead |
| SYSTEM-ONLY | 3-6 ns | 0 | 0 | Mode check only |
| HYBRID (0%) | 4-8 ns | 0 | 0 | Mode + rate check |
| HYBRID (1%) | 5-10 ns | 8-16 | 1 | Occasional tracing |
| HYBRID (10%) | 6-12 ns | 16-32 | 1 | More frequent tracing |
| HYBRID (100%) | 10-20 ns | 32-64 | 2 | All logs traced (like FULL-E2E) |
| FULL-E2E | 10-20 ns | 32-64 | 2 | Every log traced |

**CPU Overhead Estimate**:
- OFF: 0%
- SYSTEM-ONLY: <1%
- HYBRID (0%): 1-2%
- HYBRID (10%): 3-4%
- HYBRID (100%): 8-10%
- FULL-E2E: 8-10%

## CI/CD Integration

Add to your CI pipeline:

```yaml
# .github/workflows/test.yml
- name: Run Tracing Tests
  run: go test -p 1 -race -coverprofile=coverage.txt ./pkg/tracing/

- name: Run Tracing Benchmarks
  run: go test -bench=. -benchmem ./pkg/tracing/ > benchmarks.txt
```

## Test Maintenance

When adding new tests:

1. ✅ Create only ONE `EnhancedTracingManager` per test function
2. ✅ Use `newTestConfig()` helper for consistent config
3. ✅ Use `newTestLogger()` helper for silent logging
4. ✅ Clean up resources with `defer tm.Shutdown(context.Background())`
5. ✅ Document the test's purpose in a comment
6. ✅ Use meaningful test names following Go conventions

## Future Improvements

- [ ] Fix Prometheus metrics registration to allow parallel tests
- [ ] Add integration tests with actual OTLP/Jaeger backend
- [ ] Add chaos testing for trace export failures
- [ ] Add performance regression detection
- [ ] Add trace context propagation validation tests
