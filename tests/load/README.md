# Load Testing Guide

This directory contains load tests for validating the log capturer system under various load conditions.

## Test Suites

### 1. Baseline Load Tests (`baseline_test.go`)

Tests system capacity at different load levels to identify the saturation point.

**Tests Available:**
- `TestLoadBaseline_10K` - 10,000 logs/sec for 60 seconds
- `TestLoadBaseline_25K` - 25,000 logs/sec for 60 seconds
- `TestLoadBaseline_50K` - 50,000 logs/sec for 60 seconds
- `TestLoadBaseline_100K` - 100,000 logs/sec for 60 seconds

**Purpose:**
- Identify maximum sustainable throughput
- Measure latency under different loads
- Detect performance degradation points
- Establish production capacity baselines

### 2. Sustained Load Tests (`sustained_test.go`)

Tests system stability over extended periods.

**Tests Available:**
- `TestSustainedLoad_10min` - 10,000 logs/sec for 10 minutes (quick validation)
- `TestSustainedLoad_1h` - 20,000 logs/sec for 1 hour
- `TestSustainedLoad_24h` - 20,000 logs/sec for 24 hours (full validation)

**Purpose:**
- Detect memory leaks over time
- Validate goroutine stability
- Ensure consistent performance
- Production readiness validation

## Prerequisites

### 1. Start the Application

```bash
# From project root
docker-compose up -d

# Or run directly
go run cmd/main.go --config configs/config.yaml
```

### 2. Verify Server is Running

```bash
curl http://localhost:8401/health
```

Should return 200 OK.

### 3. Ensure Sufficient Resources

**Minimum Requirements for Load Testing:**
- CPU: 4+ cores
- RAM: 8+ GB
- Disk: 50+ GB free space
- Network: Stable connection to Loki/sinks

**Recommended for 24h Test:**
- CPU: 8+ cores
- RAM: 16+ GB
- Disk: 100+ GB free space
- Dedicated test environment

## Running Tests

### Quick Validation (10 minutes)

```bash
cd tests/load
go test -v -run TestSustainedLoad_10min
```

### Baseline Capacity Test

```bash
# Test 10K logs/sec
go test -v -run TestLoadBaseline_10K

# Test all baseline levels
go test -v -run TestLoadBaseline
```

### 1-Hour Sustained Test

```bash
go test -v -run TestSustainedLoad_1h -timeout 90m
```

### 24-Hour Sustained Test

```bash
# Run in background with output logging
nohup go test -v -run TestSustainedLoad_24h -timeout 30h > load_test_24h.log 2>&1 &

# Monitor progress
tail -f load_test_24h.log
```

## Interpreting Results

### Baseline Tests

**Success Criteria:**
- ✅ Achieve ≥95% of target throughput
- ✅ Error rate < 1%
- ✅ Average latency < 500ms
- ✅ No crashes or panics

**Example Output:**
```
=== LOAD TEST RESULTS: 10K ===
Duration: 1m0s
Target RPS: 10000 logs/sec

THROUGHPUT:
  Total Sent: 600000
  Total Success: 598500
  Total Errors: 1500
  Actual Throughput: 9975 logs/sec
  Target Achievement: 99.8%

LATENCY:
  Min: 2ms
  Max: 450ms
  Avg: 45ms

ERROR RATE:
  Error Rate: 0.25%

SYSTEM:
  Final Memory: 145.50 MB
  Goroutines: 24
  Num GC: 120

✅ SUCCESS: Achieved 99.8% of target throughput
✅ SUCCESS: Low error rate: 0.25%
✅ SUCCESS: Good average latency: 45ms
```

### Sustained Tests

**Success Criteria:**
- ✅ Maintain ≥90% target throughput for entire duration
- ✅ Error rate < 1%
- ✅ Memory growth < 10 MB/hour
- ✅ Goroutine count stable (±10 from baseline)
- ✅ No performance degradation over time

**Key Metrics to Monitor:**

1. **Memory Growth Rate**
   - Good: < 5 MB/hour
   - Acceptable: 5-10 MB/hour
   - Problem: > 10 MB/hour (likely leak)

2. **Goroutine Stability**
   - Good: ±5 from baseline
   - Acceptable: ±10 from baseline
   - Problem: Continuous growth (leak)

3. **Throughput Consistency**
   - Good: ±5% variation
   - Acceptable: ±10% variation
   - Problem: Continuous degradation

**Example Output:**
```
=== SUSTAINED LOAD TEST RESULTS ===
Planned Duration: 1h0m0s
Actual Duration: 1h0m2s
Target RPS: 20000 logs/sec

THROUGHPUT:
  Total Sent: 72024000
  Total Success: 71950000
  Total Errors: 74000
  Average Throughput: 19986 logs/sec
  Target Achievement: 99.9%

STABILITY:
  Error Rate: 0.1028%
  Memory Baseline: 98.50 MB
  Memory Peak: 152.30 MB
  Memory Growth: 8.50 MB (8.50 MB/hour)
  Goroutines (min/max): 22 / 28
  Total GC Runs: 720

TREND ANALYSIS:
  Throughput Change: -0.5%
  Memory Trend: +2.30 MB
  Goroutine Trend: +2

VALIDATION:
  ✅ Throughput: 99.9% of target
  ✅ Error Rate: 0.1028%
  ✅ Memory Stable: 8.50 MB/hour
  ✅ Goroutines Stable: 22 baseline, 28 peak
  ✅ Latency: 52ms average

✅ ✅ ✅ SUSTAINED LOAD TEST PASSED ✅ ✅ ✅
System is PRODUCTION READY for 20000 logs/sec
```

## Troubleshooting

### Test Fails to Start

**Error:** "Server not running on localhost:8401"

**Solution:**
```bash
# Check if application is running
curl http://localhost:8401/health

# If not running, start it
docker-compose up -d
# OR
go run cmd/main.go
```

### High Error Rate

**Symptoms:**
- Error rate > 5%
- Many timeout errors
- Connection refused errors

**Possible Causes:**
1. Queue is full (backpressure activated)
2. Sinks are slow or unavailable
3. Insufficient system resources

**Solutions:**
1. Increase queue size in config
2. Add more workers
3. Increase batch size
4. Check sink health
5. Add more system resources

### Memory Growth

**Symptoms:**
- Memory growth > 10 MB/hour
- Continuous increase over time

**Possible Causes:**
1. Memory leak in application
2. Unbounded cache growth
3. Goroutine leak

**Solutions:**
1. Profile with pprof:
   ```bash
   curl http://localhost:8401/debug/pprof/heap > heap.prof
   go tool pprof heap.prof
   ```
2. Check goroutine count:
   ```bash
   curl http://localhost:8401/debug/pprof/goroutine?debug=1
   ```
3. Review deduplication cache settings
4. Check DLQ disk usage

### Performance Degradation

**Symptoms:**
- Throughput decreases over time
- Latency increases over time

**Possible Causes:**
1. Disk space running low
2. GC pressure increasing
3. Network issues
4. Sink performance degrading

**Solutions:**
1. Monitor disk space: `df -h`
2. Check GC stats in test output
3. Monitor network: `netstat -an | grep 8401`
4. Check sink metrics (Loki/Elasticsearch)

## Best Practices

### Before Running Tests

1. **Clean Environment**
   ```bash
   # Clear old data
   docker-compose down -v
   docker-compose up -d

   # Wait for services to be ready
   sleep 30
   ```

2. **Verify Configuration**
   - Check queue size is adequate (≥50000)
   - Verify worker count matches CPU cores
   - Ensure batch size is optimized (100-500)
   - Confirm sinks are healthy

3. **Monitor Resources**
   ```bash
   # In separate terminal
   watch -n 5 'docker stats --no-stream'

   # Or for the Go process
   watch -n 5 'ps aux | grep log-capturer'
   ```

### During Tests

1. **Monitor Progress**
   - Watch test output for warnings
   - Check memory growth rate
   - Monitor error rate
   - Observe throughput trends

2. **System Monitoring**
   ```bash
   # CPU and Memory
   htop

   # Disk I/O
   iotop

   # Network
   iftop
   ```

3. **Application Metrics**
   ```bash
   # Prometheus metrics
   curl http://localhost:8401/metrics

   # Health endpoint
   curl http://localhost:8401/health

   # Stats endpoint
   curl http://localhost:8401/api/v1/stats
   ```

### After Tests

1. **Collect Results**
   - Save test output logs
   - Export Prometheus metrics
   - Take heap/CPU profiles
   - Document observations

2. **Analyze Data**
   - Calculate SLO compliance
   - Identify bottlenecks
   - Document capacity limits
   - Create optimization plan

3. **Clean Up**
   ```bash
   # Stop load test if still running
   pkill -f "go test.*load"

   # Review logs
   docker-compose logs --tail=1000 log-capturer

   # Archive results
   tar -czf load_test_results_$(date +%Y%m%d_%H%M%S).tar.gz load_test_*.log
   ```

## Performance Targets

### Production SLOs

| Metric | Target | Maximum |
|--------|--------|---------|
| **Throughput** | ≥20k logs/sec | - |
| **Latency P50** | <50ms | <100ms |
| **Latency P95** | <200ms | <500ms |
| **Latency P99** | <500ms | <1s |
| **Error Rate** | <0.1% | <1% |
| **Memory Growth** | <5 MB/hour | <10 MB/hour |
| **Uptime** | 99.9% | - |

### Capacity Planning

Based on baseline tests, establish:

1. **Maximum Throughput**: Highest sustained rate with <5% errors
2. **Recommended Load**: 70% of maximum throughput
3. **Alert Threshold**: 85% of maximum throughput
4. **Scale-Up Trigger**: 90% of maximum throughput

**Example:**
- Maximum: 50k logs/sec
- Recommended: 35k logs/sec
- Alert at: 42.5k logs/sec
- Scale at: 45k logs/sec

## Next Steps

After successful load testing:

1. **Document Results** in `PHASE15_LOAD_TESTING_SUMMARY.md`
2. **Update Capacity Plan** with actual numbers
3. **Configure Monitoring** alerts based on findings
4. **Plan Scaling Strategy** for production
5. **Proceed to Phase 16**: Rollback Plan
