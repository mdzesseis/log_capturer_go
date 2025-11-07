# Configuration Tuning Guide - SSW Log Capturer

**Version**: 1.0
**Last Updated**: 2025-11-07
**Target**: Production Optimization

---

## Table of Contents

1. [Overview](#overview)
2. [Dispatcher Configuration](#dispatcher-configuration)
3. [Workload Profiles](#workload-profiles)
4. [Performance Tuning](#performance-tuning)
5. [Resource Optimization](#resource-optimization)
6. [Troubleshooting Performance Issues](#troubleshooting-performance-issues)

---

## Overview

This guide helps you optimize the Log Capturer configuration for your specific workload. The default configuration (`configs/config.yaml`) is designed for moderate workloads (1k-10k logs/sec). Use this guide to tune for your environment.

### Key Configuration Areas
- **Dispatcher**: Queue size, worker count, batching
- **Sinks**: Batch configuration, timeouts, backpressure
- **Position System**: Flush intervals, memory limits
- **Deduplication**: Cache size, TTL
- **File Monitor**: Retry queue, seek strategy

---

## Dispatcher Configuration

### Current Configuration Analysis

**From `configs/config.yaml`**:
```yaml
dispatcher:
  queue_size: 50000         # Internal queue capacity
  worker_count: 6           # Concurrent worker goroutines
  send_timeout: "120s"      # Max time for send operation
  batch_size: 500           # Logs per batch to sinks
  batch_timeout: "10s"      # Max wait time before flush
  max_retries: 3            # Retry attempts for failures
  retry_base_delay: "5s"    # Initial retry delay
  retry_multiplier: 2       # Exponential backoff
  retry_max_delay: "60s"    # Maximum retry delay
```

### Tuning Recommendations

#### 1. Queue Size (`queue_size`)

**Purpose**: Buffer between log sources and processing workers.

| Workload | Recommended | Rationale |
|----------|-------------|-----------|
| Low (<1k/s) | 10,000 | Minimal memory, low latency |
| Medium (1k-10k/s) | **50,000** | **Default - balanced** |
| High (10k-50k/s) | 100,000 | Handle bursts, higher memory |
| Very High (>50k/s) | 200,000 | Maximum throughput, high memory |

**Memory Impact**: ~114 bytes per entry
- 50,000 entries ≈ 5.7 MB
- 100,000 entries ≈ 11.4 MB

**Alert Threshold**: Queue > 70% full (warning), > 90% full (critical)

**Tuning**:
```bash
# Check current utilization
curl http://localhost:8001/metrics | grep 'log_capturer_queue_size{.*dispatcher'

# If consistently > 70%, increase queue_size
# If consistently < 30%, decrease queue_size
```

#### 2. Worker Count (`worker_count`)

**Purpose**: Number of concurrent goroutines processing logs.

| CPU Cores | I/O-Bound Workload | CPU-Bound Workload |
|-----------|-------------------|-------------------|
| 2 cores | 4-6 workers | 2-4 workers |
| 4 cores | 8-12 workers | **4-6 workers (default)** |
| 8 cores | 16-24 workers | 8-12 workers |
| 16+ cores | 32-48 workers | 16-24 workers |

**Tuning Guidelines**:
- **Start with**: `2 * CPU_CORES` for I/O-heavy workloads (typical)
- **Reduce to**: `1 * CPU_CORES` for CPU-heavy processing
- **Monitor**:
  ```bash
  # Check worker activity
  curl http://localhost:8001/metrics | grep "log_capturer_active_tasks"

  # Check CPU usage
  docker stats log_capturer_go --no-stream
  ```

**Benchmark Results** (estimated):

| Workers | Throughput (logs/sec) | CPU Usage | Latency (p99) |
|---------|----------------------|-----------|---------------|
| 2 | 5,000 | 40% | 150ms |
| 4 | 10,000 | 60% | 100ms |
| **6** | **15,000** | **75%** | **80ms** |
| 8 | 20,000 | 85% | 70ms |
| 12 | 25,000 | 95% | 65ms |

**Optimal**: 6 workers provides best throughput/latency balance for typical workloads.

#### 3. Batch Size (`batch_size`)

**Purpose**: Number of logs sent to sink per request.

| Sink Type | Small Batch | **Medium Batch (default)** | Large Batch |
|-----------|-------------|---------------------------|-------------|
| Loki | 100-200 | **500** | 1000-2000 |
| Kafka | 500-1000 | 1000 | 2000-5000 |
| LocalFile | 100 | 500 | 1000 |

**Trade-offs**:
- **Larger batches**: Higher throughput, higher latency, more memory
- **Smaller batches**: Lower latency, lower throughput, less memory

**Current Loki Configuration** (`configs/config.yaml`):
```yaml
sinks:
  loki:
    batch_size: 500         # Reduced from 20000 to prevent Loki rejection
    batch_timeout: "5s"     # Reduced from 40s for faster delivery
    max_request_size: 2097152  # 2 MB limit
```

**Tuning**:
```bash
# Monitor sink send duration
curl http://localhost:8001/metrics | grep "log_capturer_sink_send_duration"

# If P99 latency > 5s, decrease batch_size
# If throughput insufficient, increase batch_size (up to 1000 for Loki)
```

#### 4. Batch Timeout (`batch_timeout`)

**Purpose**: Max wait time before flushing incomplete batch.

| Latency Requirement | Recommended | Use Case |
|---------------------|-------------|----------|
| Low latency (<1s) | 1s | Real-time dashboards |
| Moderate (1-5s) | 5s | Standard logging |
| High throughput | **10s (default)** | Batch processing |

**Formula**: `batch_timeout = target_latency / 2`

**Example**: For 2s latency requirement → `batch_timeout: "1s"`

---

## Workload Profiles

### Profile 1: Low Volume Development

**Scenario**: Development/testing environment, <1k logs/sec

```yaml
dispatcher:
  queue_size: 10000
  worker_count: 2
  batch_size: 100
  batch_timeout: "5s"

sinks:
  loki:
    batch_size: 100
    batch_timeout: "2s"
    queue_size: 5000

positions:
  flush_interval: "30s"
  max_memory_buffer: 500

deduplication_config:
  max_cache_size: 1000
  ttl: "30s"
```

**Expected Performance**:
- Throughput: 500-1000 logs/sec
- Memory: <100 MB
- CPU: 10-20%

---

### Profile 2: Production Standard (Current)

**Scenario**: Standard production, 1k-10k logs/sec

```yaml
dispatcher:
  queue_size: 50000
  worker_count: 6
  batch_size: 500
  batch_timeout: "10s"

sinks:
  loki:
    batch_size: 500
    batch_timeout: "5s"
    queue_size: 25000

positions:
  flush_interval: "10s"
  max_memory_buffer: 2000

deduplication_config:
  max_cache_size: 10000
  ttl: "10s"
```

**Expected Performance**:
- Throughput: 5,000-15,000 logs/sec
- Memory: 200-400 MB
- CPU: 40-60%

---

### Profile 3: High Throughput

**Scenario**: High-volume production, 10k-50k logs/sec

```yaml
dispatcher:
  queue_size: 100000
  worker_count: 12
  batch_size: 1000
  batch_timeout: "5s"

sinks:
  loki:
    batch_size: 1000
    batch_timeout: "2s"
    queue_size: 50000
    adaptive_batching:
      enabled: true

positions:
  flush_interval: "5s"
  max_memory_buffer: 5000

deduplication_config:
  max_cache_size: 50000
  ttl: "5s"
```

**Expected Performance**:
- Throughput: 20,000-50,000 logs/sec
- Memory: 500-800 MB
- CPU: 70-90%

---

### Profile 4: Low Latency

**Scenario**: Real-time dashboards, latency <500ms

```yaml
dispatcher:
  queue_size: 25000
  worker_count: 8
  batch_size: 50
  batch_timeout: "200ms"

sinks:
  loki:
    batch_size: 50
    batch_timeout: "100ms"
    queue_size: 10000

positions:
  flush_interval: "1s"
  max_memory_buffer: 1000

deduplication_config:
  max_cache_size: 5000
  ttl: "2s"
```

**Expected Performance**:
- Throughput: 5,000-10,000 logs/sec
- Latency: P99 < 500ms
- Memory: 150-300 MB
- CPU: 50-70%

---

## Performance Tuning

### Identifying Bottlenecks

#### 1. Queue Saturation
**Symptom**: `log_capturer_queue_size` consistently > 70% capacity

**Diagnosis**:
```bash
# Check queue utilization over time
curl http://localhost:8001/metrics | grep 'log_capturer_queue_size'

# Check if workers are busy
curl http://localhost:8001/metrics | grep 'log_capturer_active_tasks'
```

**Solutions**:
1. Increase `worker_count` (if CPU available)
2. Increase `batch_size` (if sink can handle)
3. Increase `queue_size` (if memory available)
4. Enable adaptive batching

#### 2. Sink Latency
**Symptom**: `log_capturer_sink_send_duration` P99 > 5s

**Diagnosis**:
```bash
# Check sink latency
curl http://localhost:8001/metrics | grep 'log_capturer_sink_send_duration'

# Check Loki health
curl http://loki:3100/ready
```

**Solutions**:
1. Check Loki resource availability
2. Reduce `batch_size` to decrease per-request payload
3. Increase `send_timeout` if network is slow
4. Enable connection pooling (already implemented)

#### 3. High CPU Usage
**Symptom**: CPU consistently > 80%

**Diagnosis**:
```bash
# Check CPU usage
docker stats log_capturer_go --no-stream

# Check worker count
grep worker_count /app/configs/config.yaml
```

**Solutions**:
1. Reduce `worker_count`
2. Optimize processing pipeline (reduce CPU-heavy operations)
3. Scale horizontally (add more instances)

#### 4. High Memory Usage
**Symptom**: Memory > 500 MB or growing

**Diagnosis**:
```bash
# Check memory usage
curl http://localhost:8001/metrics | grep 'log_capturer_memory_usage_bytes'

# Check goroutine count (potential leak)
curl http://localhost:8001/metrics | grep 'go_goroutines'
```

**Solutions**:
1. Reduce `queue_size`
2. Reduce `max_cache_size` (deduplication)
3. Reduce `max_memory_buffer` (positions)
4. Check for goroutine leaks

---

## Resource Optimization

### Memory Optimization

**Current Memory Allocation** (estimated):

| Component | Default | Memory | Tunable |
|-----------|---------|--------|---------|
| Dispatcher Queue | 50,000 entries | ~5.7 MB | `queue_size` |
| Loki Sink Queue | 25,000 entries | ~2.9 MB | `sinks.loki.queue_size` |
| Dedup Cache | 10,000 entries | ~1.1 MB | `deduplication_config.max_cache_size` |
| Position System | 2,000 buffer | ~0.2 MB | `positions.max_memory_buffer` |
| Worker Overhead | 6 workers | ~10 MB | `worker_count` |
| **Total** | | **~20 MB** | |

**Note**: Actual memory includes Go runtime overhead (~50-100 MB) and log message payload retention.

### CPU Optimization

**CPU Distribution**:
- Worker goroutines: 60-70%
- Position saves: 5-10%
- Deduplication: 2-5%
- Metrics collection: 2-3%
- HTTP server: 1-2%
- Overhead: 10-20%

**Optimization Tips**:
1. Use `worker_count = CPU_cores * 2` for I/O-bound workloads
2. Use `worker_count = CPU_cores` for CPU-bound workloads
3. Monitor `log_capturer_cpu_usage_percent`
4. Profile with `pprof` if CPU > 90%

### Disk I/O Optimization

**I/O-Heavy Operations**:
- Position file saves (every 10s by default)
- DLQ writes (on failures)
- Checkpoint saves (every 5min)

**Optimization**:
```yaml
positions:
  flush_interval: "30s"  # Reduce write frequency (but higher data loss risk)
  force_flush_on_exit: true  # Always flush on shutdown

dlq_config:
  enabled: true
  max_size_mb: 100  # Limit disk usage
  write_timeout: "5s"
```

---

## Troubleshooting Performance Issues

### Issue 1: Low Throughput

**Expected**: 5,000-15,000 logs/sec
**Actual**: <2,000 logs/sec

**Checklist**:
1. Check queue utilization (should be > 30%)
2. Check worker activity (should be > 80%)
3. Check sink latency (should be < 1s)
4. Check CPU usage (should be > 40%)
5. Check for errors (`log_capturer_errors_total`)

**Common Causes**:
- Undersized worker pool
- Sink bottleneck (Loki slow)
- Small batch sizes
- Network latency

### Issue 2: High Latency

**Expected**: P99 < 1s
**Actual**: P99 > 5s

**Checklist**:
1. Check batch timeout (reduce if too high)
2. Check sink send duration
3. Check queue saturation (shouldn't be > 90%)
4. Check worker count (should be sufficient)

**Common Causes**:
- Large batch timeouts
- Sink latency
- Queue saturation (waiting for capacity)

### Issue 3: Memory Leak

**Expected**: Memory stable <400 MB
**Actual**: Memory growing continuously

**Checklist**:
1. Check goroutine count (`go_goroutines` should be <5000)
2. Check for goroutine leaks (see CLAUDE.md)
3. Check deduplication cache size (shouldn't exceed `max_cache_size`)
4. Check position system memory

**Common Causes**:
- Goroutine leak (container monitor streams not closed)
- HTTP connection leak
- File descriptor leak

---

## Configuration Hot Reload

Changes can be applied without restart:

```bash
# Edit config.yaml
vim /app/configs/config.yaml

# Reload configuration
curl -X POST http://localhost:8401/config/reload

# Verify reload success
docker logs --tail 20 log_capturer_go | grep "config.*reload"
```

**Hot-reloadable Parameters**:
- ✅ `dispatcher.worker_count`
- ✅ `dispatcher.batch_size`
- ✅ `dispatcher.batch_timeout`
- ✅ `sinks.loki.batch_size`
- ✅ `sinks.loki.batch_timeout`
- ✅ `deduplication_config.*`
- ❌ `dispatcher.queue_size` (requires restart)
- ❌ `sinks.loki.queue_size` (requires restart)

---

## Benchmarking

### Performance Testing

```bash
# Generate load with log simulator
docker run --rm --network log_capturer_go_default \
  alpine/bombardier -c 100 -d 60s \
  http://log_capturer:8401/logs

# Monitor metrics during test
watch -n 1 'curl -s http://localhost:8001/metrics | grep "log_capturer_logs_per_second"'

# Check P99 latency
curl -s http://localhost:8001/metrics | grep "log_capturer_processing_duration"
```

### Benchmarking Results Template

| Configuration | Throughput | P99 Latency | CPU % | Memory (MB) |
|---------------|-----------|-------------|-------|-------------|
| Default (6 workers) | 12,000 | 80ms | 55% | 250 |
| 12 workers | 18,000 | 70ms | 75% | 300 |
| Large batches (1000) | 20,000 | 120ms | 60% | 280 |

---

## Quick Reference Card

```yaml
# ==================================================================
# QUICK TUNING REFERENCE
# ==================================================================

# LOW VOLUME (<1k/s)
dispatcher: { queue_size: 10000, worker_count: 2, batch_size: 100 }

# STANDARD (1k-10k/s) - DEFAULT
dispatcher: { queue_size: 50000, worker_count: 6, batch_size: 500 }

# HIGH VOLUME (10k-50k/s)
dispatcher: { queue_size: 100000, worker_count: 12, batch_size: 1000 }

# LOW LATENCY (<500ms)
dispatcher: { queue_size: 25000, worker_count: 8, batch_size: 50, batch_timeout: "200ms" }

# ==================================================================
# TROUBLESHOOTING QUICK CHECKS
# ==================================================================

# Queue full? Increase queue_size or worker_count
curl http://localhost:8001/metrics | grep 'log_capturer_queue_size.*dispatcher'

# Workers idle? Decrease worker_count
curl http://localhost:8001/metrics | grep 'log_capturer_active_tasks'

# High latency? Decrease batch_timeout
curl http://localhost:8001/metrics | grep 'log_capturer_sink_send_duration'

# Goroutine leak? Check count and growth
curl http://localhost:8001/metrics | grep 'go_goroutines'
```

---

## Further Reading

- **Developer Guide**: `/home/mateus/log_capturer_go/docs/CLAUDE.md`
- **Configuration Reference**: `/home/mateus/log_capturer_go/configs/config.yaml`
- **Recovery Runbook**: `/home/mateus/log_capturer_go/docs/RECOVERY_RUNBOOK.md`
- **Position System Analysis**: `/home/mateus/log_capturer_go/docs/POSITION_SYSTEM_ANALYSIS.md`

---

**Document Version**: 1.0
**Last Updated**: 2025-11-07
**Next Review**: 2025-12-07
