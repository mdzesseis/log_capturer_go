# Prometheus Queries - Container Monitor & Stream Metrics

**Version**: 1.0
**Last Updated**: 2025-11-06
**Related Dashboard**: Container Monitor - Stream Metrics
**Purpose**: Useful Prometheus queries for monitoring, debugging, and alerting

---

## Table of Contents

- [Goroutine Leak Detection](#goroutine-leak-detection)
- [Stream Pool Monitoring](#stream-pool-monitoring)
- [Rotation Metrics](#rotation-metrics)
- [Resource Leak Detection](#resource-leak-detection)
- [Performance Metrics](#performance-metrics)
- [Component Health](#component-health)
- [Advanced Queries](#advanced-queries)

---

## Goroutine Leak Detection

### Current Goroutine Count
```promql
log_capturer_goroutines
```
**Description**: Total number of goroutines currently running.
**Expected**: Should remain stable (400-600 under normal load).
**Action**: Investigate if > 1000 or continuously growing.

### Goroutine Growth Rate (per minute)
```promql
rate(log_capturer_goroutines[5m]) * 60
```
**Description**: Rate of goroutine increase per minute.
**Expected**: < 2 goroutines/min (ideally near 0).
**Action**: Alert if > 2/min, investigate if > 5/min.

### Goroutine Growth Rate (per hour)
```promql
rate(log_capturer_goroutines[1h]) * 3600
```
**Description**: Hourly goroutine growth trend.
**Expected**: < 50 goroutines/hour.
**Action**: Indicates slow leak if consistently positive.

### Goroutine Derivative (30-minute window)
```promql
deriv(log_capturer_goroutines[30m])
```
**Description**: Mathematical derivative showing leak velocity.
**Expected**: Near 0.
**Action**: Alert if > 10/min over 30 minutes.

### Goroutine Percentage Change
```promql
(log_capturer_goroutines - log_capturer_goroutines offset 1h) / log_capturer_goroutines offset 1h * 100
```
**Description**: Percentage change in goroutines vs 1 hour ago.
**Expected**: < 5% change.
**Action**: Investigate if > 20% increase.

---

## Stream Pool Monitoring

### Active Streams Count
```promql
log_capturer_container_streams_active
```
**Description**: Number of currently active container streams.
**Expected**: Matches number of running containers (max 50).
**Action**: Alert if >= 50 (pool capacity reached).

### Stream Pool Utilization (%)
```promql
(log_capturer_container_streams_active / 50) * 100
```
**Description**: Pool capacity utilization percentage.
**Expected**: < 80% (< 40 streams).
**Action**: Consider scaling if consistently > 80%.

### Available Stream Slots
```promql
50 - log_capturer_container_streams_active
```
**Description**: Number of free stream slots.
**Expected**: > 10 slots available.
**Action**: Warning if < 10, critical if < 5.

### Stream Pool History (24h comparison)
```promql
log_capturer_container_streams_active - log_capturer_container_streams_active offset 24h
```
**Description**: Change in active streams vs 24 hours ago.
**Expected**: Depends on workload pattern.
**Action**: Large changes may indicate container churn.

---

## Rotation Metrics

### Total Rotations
```promql
log_capturer_container_stream_rotations_total
```
**Description**: Cumulative count of stream rotations since startup.
**Expected**: Continuously increasing (every 5min per container).
**Action**: Flat line indicates rotation failure.

### Rotation Rate (per minute)
```promql
rate(log_capturer_container_stream_rotations_total[5m]) * 60
```
**Description**: Number of rotations per minute.
**Expected**: ~(active_containers / 5) rotations/min.
**Action**: Alert if 0 for > 10 minutes.

### Expected vs Actual Rotations
```promql
# Expected rotations (containers / 5min interval)
log_capturer_container_streams_active / 5

# Actual rotation rate
rate(log_capturer_container_stream_rotations_total[5m]) * 60
```
**Description**: Compare expected vs actual rotation rates.
**Expected**: Should be approximately equal.
**Action**: Investigate if actual < 50% of expected.

### Rotations Since Last Hour
```promql
increase(log_capturer_container_stream_rotations_total[1h])
```
**Description**: Total rotations in past hour.
**Expected**: ~(active_containers * 12) rotations/hour.
**Action**: Alert if 0 or significantly below expected.

### Average Time Between Rotations
```promql
300 / (rate(log_capturer_container_stream_rotations_total[10m]) * 60 / log_capturer_container_streams_active)
```
**Description**: Average seconds between rotations per stream.
**Expected**: ~300 seconds (5 minutes).
**Action**: Investigate if >> 300s or << 300s.

---

## Resource Leak Detection

### File Descriptors Open
```promql
log_capturer_file_descriptors_open
```
**Description**: Number of open file descriptors.
**Expected**: Stable (< 500 under normal load).
**Action**: Alert if > 1000 or continuously growing.

### File Descriptor Growth Rate (per 10min)
```promql
rate(log_capturer_file_descriptors_open[10m]) * 600
```
**Description**: FD increase per 10 minutes.
**Expected**: < 10 FDs/10min.
**Action**: Alert if > 50/10min, indicates FD leak.

### File Descriptor Leak Velocity (30min derivative)
```promql
deriv(log_capturer_file_descriptors_open[30m])
```
**Description**: Mathematical derivative of FD count.
**Expected**: Near 0.
**Action**: Alert if > 5/min over 30 minutes.

### Memory Usage (MB)
```promql
log_capturer_memory_usage_bytes / 1024 / 1024
```
**Description**: Current memory usage in megabytes.
**Expected**: < 500 MB under normal load.
**Action**: Monitor for continuous growth.

### Memory Growth Rate (per hour)
```promql
rate(log_capturer_memory_usage_bytes[1h]) * 3600 / 1024 / 1024
```
**Description**: Memory increase per hour in MB.
**Expected**: < 50 MB/hour.
**Action**: Alert if > 100 MB/hour, indicates memory leak.

---

## Performance Metrics

### CPU Usage (%)
```promql
log_capturer_cpu_usage_percent
```
**Description**: CPU utilization percentage.
**Expected**: < 60% under normal load.
**Action**: Investigate if > 80% for extended periods.

### Logs Processed (total)
```promql
log_capturer_logs_processed_total
```
**Description**: Cumulative count of logs processed.
**Expected**: Continuously increasing.
**Action**: Flat line indicates processing stopped.

### Log Processing Rate (per second)
```promql
rate(log_capturer_logs_processed_total[5m])
```
**Description**: Logs processed per second.
**Expected**: Depends on workload (10-1000/sec typical).
**Action**: Alert if 0 for > 10 minutes.

### Log Processing Rate (per minute)
```promql
rate(log_capturer_logs_processed_total[5m]) * 60
```
**Description**: Logs processed per minute.
**Expected**: Varies by workload.
**Action**: Monitor for unexpected drops.

### Processing Throughput Trend (1h avg)
```promql
avg_over_time(rate(log_capturer_logs_processed_total[5m])[1h:5m])
```
**Description**: Average processing rate over last hour.
**Expected**: Stable under consistent load.
**Action**: Sharp drops indicate problems.

---

## Component Health

### Container Monitor Health Status
```promql
log_capturer_component_health{component="container_monitor"}
```
**Description**: Health status of Container Monitor component.
**Values**: 1 = Healthy, 0 = Unhealthy.
**Action**: Alert immediately if 0.

### All Components Health
```promql
log_capturer_component_health
```
**Description**: Health status of all components.
**Expected**: All components = 1.
**Action**: Investigate any component with value 0.

### Unhealthy Components Count
```promql
count(log_capturer_component_health == 0)
```
**Description**: Number of unhealthy components.
**Expected**: 0.
**Action**: Critical alert if > 0.

---

## Advanced Queries

### Goroutine Leak Severity Score
```promql
(
  (rate(log_capturer_goroutines[5m]) * 60) * 10 +
  (log_capturer_goroutines > 500) * 20 +
  (log_capturer_goroutines > 1000) * 50
)
```
**Description**: Composite score indicating leak severity.
**Expected**: < 20.
**Action**: Score > 50 = critical, investigate immediately.

### Resource Health Score (0-100)
```promql
100 - (
  ((log_capturer_goroutines / 2000) * 100 * 0.3) +
  ((log_capturer_file_descriptors_open / 2000) * 100 * 0.3) +
  ((log_capturer_memory_usage_bytes / 1073741824) * 100 * 0.2) +
  ((log_capturer_cpu_usage_percent / 100) * 100 * 0.2)
)
```
**Description**: Overall resource health (100 = perfect).
**Expected**: > 70.
**Action**: < 50 indicates resource problems.

### Stream Efficiency Ratio
```promql
rate(log_capturer_logs_processed_total[5m]) / log_capturer_container_streams_active
```
**Description**: Logs processed per active stream per second.
**Expected**: > 1 log/sec per stream.
**Action**: Low values may indicate inefficiency.

### Rotation Reliability (%)
```promql
(
  rate(log_capturer_container_stream_rotations_total[10m]) * 60 /
  (log_capturer_container_streams_active / 5)
) * 100
```
**Description**: Percentage of expected rotations occurring.
**Expected**: > 90%.
**Action**: < 80% indicates rotation problems.

### Multi-Metric Anomaly Detection
```promql
# Detect multiple anomalies simultaneously
(
  (rate(log_capturer_goroutines[5m]) * 60 > 2) * 1 +
  (log_capturer_container_streams_active >= 45) * 1 +
  (rate(log_capturer_file_descriptors_open[10m]) * 600 > 50) * 1 +
  (rate(log_capturer_container_stream_rotations_total[10m]) * 60 == 0) * 1
)
```
**Description**: Count of active anomalies (0-4).
**Expected**: 0.
**Action**: > 1 = warning, > 2 = critical.

### Predictive Goroutine Count (1h ahead)
```promql
predict_linear(log_capturer_goroutines[30m], 3600)
```
**Description**: Predicted goroutine count in 1 hour.
**Expected**: < 1000.
**Action**: Alert if prediction > 1500.

### Stream Pool Saturation ETA
```promql
# Time until pool saturation (seconds)
(50 - log_capturer_container_streams_active) /
(deriv(log_capturer_container_streams_active[30m]) * 60)
```
**Description**: Estimated seconds until pool full.
**Expected**: > 3600 (1 hour).
**Action**: Alert if < 600 seconds (10 minutes).

---

## Query Patterns & Best Practices

### Time Windows
- **Immediate**: `[1m]` - Real-time detection
- **Short-term**: `[5m]` - Standard monitoring
- **Medium-term**: `[30m]` - Trend analysis
- **Long-term**: `[1h]`, `[24h]` - Capacity planning

### Rate Calculations
```promql
# Per second
rate(metric[5m])

# Per minute
rate(metric[5m]) * 60

# Per hour
rate(metric[1h]) * 3600
```

### Derivative vs Rate
- **rate()**: For counters (always increasing)
- **deriv()**: Mathematical derivative, detects acceleration
- **Use rate()** for most metrics
- **Use deriv()** for leak detection

### Comparison Patterns
```promql
# vs 1 hour ago
metric - metric offset 1h

# vs 24 hours ago
metric - metric offset 24h

# Percentage change
(metric - metric offset 1h) / metric offset 1h * 100
```

### Aggregation
```promql
# Sum across all instances
sum(metric)

# Average
avg(metric)

# Max/Min
max(metric)
min(metric)

# Percentiles (for histograms)
histogram_quantile(0.99, rate(metric_bucket[5m]))
```

---

## Alert Thresholds Reference

| Metric | Warning | Critical | Action |
|--------|---------|----------|--------|
| Goroutine Growth | 2/min | 5/min | Investigate leak |
| Active Streams | 40 | 50 | Check capacity |
| FD Growth | 20/10min | 50/10min | Check FD leak |
| Memory Growth | 50 MB/h | 100 MB/h | Check memory leak |
| CPU Usage | 60% | 80% | Optimize or scale |
| Rotation Rate | 0/10min | 0/20min | Check rotation |

---

## Useful Grafana Variables

Add these to dashboard for filtering:

```
# Instance selector
label_values(log_capturer_goroutines, instance)

# Component selector
label_values(log_capturer_component_health, component)

# Time range selector
5m, 15m, 30m, 1h, 6h, 24h
```

---

## Integration with Alerts

All queries in this document can be used in:
- **Prometheus Alert Rules** (`provisioning/alerts/rules.yml`)
- **Grafana Alerts** (dashboard panels)
- **API Queries** (Prometheus HTTP API)
- **Custom Monitoring Tools**

---

## Troubleshooting Common Issues

### Issue: Goroutines increasing but no obvious leak
**Query**:
```promql
# Check if increase matches stream count
log_capturer_goroutines / log_capturer_container_streams_active
```
**Expected**: Constant ratio (e.g., 10-20 goroutines per stream).
**Action**: Growing ratio = leak.

### Issue: No rotations occurring
**Queries**:
```promql
# Check if container monitor is healthy
log_capturer_component_health{component="container_monitor"}

# Check if streams are active
log_capturer_container_streams_active

# Check rotation counter
increase(log_capturer_container_stream_rotations_total[10m])
```
**Action**: Restart container monitor if unhealthy.

### Issue: High FD count
**Queries**:
```promql
# FD per stream
log_capturer_file_descriptors_open / log_capturer_container_streams_active

# FD growth trend
deriv(log_capturer_file_descriptors_open[30m])
```
**Expected**: 2-5 FDs per stream.
**Action**: > 10 FDs/stream indicates leak.

---

## Dashboard Links

- **Main Dashboard**: http://localhost:3000/d/container-monitor-streams
- **Prometheus UI**: http://localhost:9090
- **Prometheus Targets**: http://localhost:9090/targets
- **Prometheus Alerts**: http://localhost:9090/alerts

---

## Related Documentation

- [Container Monitor Architecture](ARCHITECTURE_CONFIGURATION_ANALYSIS.md)
- [Goroutine Leak Fix](FINAL_GOROUTINE_FIX_REPORT.md)
- [Alert Configuration](../provisioning/alerts/rules.yml)
- [Dashboard JSON](../provisioning/dashboards/container-monitor-streams.json)

---

**Last Updated**: 2025-11-06
**Maintainer**: Log Capturer Team
**Version**: 1.0
