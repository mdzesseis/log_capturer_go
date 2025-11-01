# Troubleshooting Guide

**SSW Logs Capture** - Problem Diagnosis and Resolution

---

## Table of Contents

1. [Getting Started](#getting-started)
2. [Diagnostic Tools](#diagnostic-tools)
3. [Common Issues](#common-issues)
   - [Startup Issues](#startup-issues)
   - [Performance Issues](#performance-issues)
   - [Connection Issues](#connection-issues)
   - [Memory & Resource Issues](#memory--resource-issues)
   - [Log Capture Issues](#log-capture-issues)
   - [Sink Delivery Issues](#sink-delivery-issues)
   - [Configuration Issues](#configuration-issues)
4. [Log Analysis](#log-analysis)
5. [Metrics Interpretation](#metrics-interpretation)
6. [Advanced Debugging](#advanced-debugging)
7. [FAQ](#faq)
8. [Getting Help](#getting-help)

---

## Getting Started

### Quick Diagnosis Checklist

Before diving into specific issues, check these basics:

```bash
# 1. Check if the application is running
ps aux | grep ssw-logs-capture

# 2. Check health endpoint
curl http://localhost:8401/health

# 3. View recent logs
docker logs log_capturer_go --tail 50

# 4. Check metrics
curl http://localhost:8001/metrics | grep -E '(error|failed|queue_size)'

# 5. Check disk space
df -h /app

# 6. Check memory usage
free -h

# 7. Check open file descriptors
lsof -p $(pgrep ssw-logs-capture) | wc -l
```

### Health Check Interpretation

```bash
curl http://localhost:8401/health
```

**Healthy Response**:
```json
{
  "status": "healthy",
  "uptime": "2h30m15s",
  "checks": {
    "queue_utilization": {
      "status": "healthy",
      "utilization": "42.50%",
      "size": 21250,
      "capacity": 50000
    },
    "memory": {
      "status": "healthy",
      "alloc_mb": 245,
      "sys_mb": 512,
      "goroutines": 156
    }
  }
}
```

**Unhealthy Indicators**:
- `status: "degraded"` - Component issues detected
- `queue_utilization > 80%` - Queue pressure
- `memory.alloc_mb` growing continuously - Memory leak
- `goroutines` growing continuously - Goroutine leak

---

## Diagnostic Tools

### Built-in Commands

**View statistics**:
```bash
curl http://localhost:8401/stats
```

**View configuration** (sanitized):
```bash
curl http://localhost:8401/config
```

**View file positions**:
```bash
curl http://localhost:8401/positions
```

**View DLQ statistics**:
```bash
curl http://localhost:8401/dlq/stats
```

**Check goroutines** (debug):
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

**Check memory** (debug):
```bash
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8080 heap.prof
```

### Log Analysis Commands

**Find errors in logs**:
```bash
docker logs log_capturer_go 2>&1 | grep -i error | tail -20
```

**Count error types**:
```bash
docker logs log_capturer_go 2>&1 | grep -i error | awk '{print $5}' | sort | uniq -c | sort -rn
```

**Monitor logs in real-time**:
```bash
docker logs -f log_capturer_go
```

**Extract specific time range**:
```bash
docker logs log_capturer_go 2>&1 | grep "2025-11-01 14:"
```

### Metrics Analysis

**Check queue metrics**:
```bash
curl -s http://localhost:8001/metrics | grep dispatcher_queue
```

**Check error rates**:
```bash
curl -s http://localhost:8001/metrics | grep -E '(failed|error)_total'
```

**Check sink latency**:
```bash
curl -s http://localhost:8001/metrics | grep sink_latency
```

**Monitor metrics continuously**:
```bash
watch -n 5 'curl -s http://localhost:8001/metrics | grep -E "(queue_size|error_total)"'
```

---

## Common Issues

### Startup Issues

#### Issue: Application Won't Start

**Symptoms**:
```
container "log_capturer_go" is restarting continuously
```

**Diagnosis**:
```bash
docker logs log_capturer_go --tail 50
```

**Common Causes & Solutions**:

**1. Configuration file not found**:
```
Error: failed to load config: open /app/configs/config.yaml: no such file or directory
```

Solution:
```bash
# Check if config file exists
docker exec log_capturer_go ls -la /app/configs/

# Mount config correctly in docker-compose.yml
volumes:
  - ./configs:/app/configs:ro
```

**2. Invalid YAML syntax**:
```
Error: yaml: line 42: mapping values are not allowed in this context
```

Solution:
```bash
# Validate YAML syntax
yamllint configs/config.yaml

# Common issues:
# - Missing colons after keys
# - Incorrect indentation (use spaces, not tabs)
# - Unquoted special characters
```

**3. Docker socket permission denied**:
```
Error: failed to connect to Docker: permission denied while trying to connect to the Docker daemon socket
```

Solution:
```yaml
# docker-compose.yml - Add user to docker group or use root
user: root  # Or ensure user is in docker group

volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro

# On host, grant permissions
sudo chmod 666 /var/run/docker.sock
# OR add user to docker group
sudo usermod -aG docker $USER
```

**4. Port already in use**:
```
Error: listen tcp :8401: bind: address already in use
```

Solution:
```bash
# Find process using port
sudo lsof -i :8401
sudo netstat -tulpn | grep 8401

# Kill process or change port in config.yaml
server:
  port: 8402  # Use different port
```

---

#### Issue: Loki Connection Failed at Startup

**Symptoms**:
```
Error initializing Loki sink: failed to connect to http://loki:3100
```

**Diagnosis**:
```bash
# Check if Loki is reachable
docker exec log_capturer_go curl -v http://loki:3100/ready

# Check network connectivity
docker exec log_capturer_go ping loki
```

**Solutions**:

**1. Loki not running**:
```bash
docker ps | grep loki
docker-compose up -d loki
```

**2. Wrong URL**:
```yaml
# config.yaml - Check Loki URL
sinks:
  loki:
    url: "http://loki:3100"  # Must match service name in docker-compose
```

**3. Network isolation**:
```yaml
# docker-compose.yml - Ensure same network
services:
  log_capturer:
    networks:
      - monitoring

  loki:
    networks:
      - monitoring

networks:
  monitoring:
    driver: bridge
```

---

### Performance Issues

#### Issue: High CPU Usage

**Symptoms**:
```bash
# CPU usage > 80% continuously
docker stats log_capturer_go
```

**Diagnosis**:
```bash
# Check goroutine count
curl http://localhost:8401/health | jq '.checks.memory.goroutines'

# Check queue utilization
curl http://localhost:8401/stats | jq '.dispatcher.queue_utilization'

# Profile CPU
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8080 cpu.prof
```

**Solutions**:

**1. Too many workers**:
```yaml
# Reduce worker count
dispatcher:
  worker_count: 4  # Reduced from 12
```

**2. Excessive file polling**:
```yaml
# Increase poll intervals
file_monitor_service:
  poll_interval: "60s"  # Increased from 10s
  read_interval: "500ms"  # Increased from 100ms
```

**3. Large batches with slow processing**:
```yaml
# Reduce batch size for faster processing
dispatcher:
  batch_size: 250  # Reduced from 1000
  batch_timeout: "5s"
```

**4. Goroutine leak**:
```bash
# Check for growing goroutines
watch -n 5 'curl -s http://localhost:8401/health | jq ".checks.memory.goroutines"'

# If continuously growing, restart and review logs
docker logs log_capturer_go 2>&1 | grep -i "goroutine leak"
```

---

#### Issue: High Memory Usage

**Symptoms**:
```bash
# Memory usage growing continuously
docker stats log_capturer_go

# Or OOM kills
docker logs log_capturer_go | grep "out of memory"
```

**Diagnosis**:
```bash
# Check memory stats
curl http://localhost:8401/health | jq '.checks.memory'

# Check queue sizes
curl http://localhost:8401/stats | jq '.dispatcher.queue_size'

# Memory profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8080 heap.prof
```

**Solutions**:

**1. Queue too large**:
```yaml
# Reduce queue sizes
dispatcher:
  queue_size: 25000  # Reduced from 100000

sinks:
  loki:
    queue_size: 10000  # Reduced from 50000
```

**2. Large batches in memory**:
```yaml
# Reduce batch size
dispatcher:
  batch_size: 250  # Reduced from 1000

sinks:
  loki:
    batch_size: 250
```

**3. Deduplication cache too large**:
```yaml
# Reduce dedup cache
dispatcher:
  deduplication_config:
    max_cache_size: 50000  # Reduced from 100000
    ttl: "30m"  # Reduced from 1h
```

**4. Enable GC optimization**:
```yaml
resource_monitoring:
  enable_gc_optimization: true
```

---

#### Issue: Slow Log Delivery

**Symptoms**:
- Logs appear in Loki 30+ seconds after generation
- Growing queue size

**Diagnosis**:
```bash
# Check queue utilization
curl http://localhost:8401/stats | jq '.dispatcher.queue_utilization'

# Check sink latency
curl -s http://localhost:8001/metrics | grep sink_latency_seconds

# Check batch settings
curl http://localhost:8401/config | jq '.dispatcher'
```

**Solutions**:

**1. Batch timeout too high**:
```yaml
# Reduce batch timeout for lower latency
dispatcher:
  batch_timeout: "2s"  # Reduced from 10s

sinks:
  loki:
    batch_timeout: "2s"
```

**2. Batch size too small**:
```yaml
# Increase batch size for better throughput
dispatcher:
  batch_size: 1000  # Increased from 100
```

**3. Insufficient workers**:
```yaml
# Increase worker count
dispatcher:
  worker_count: 12  # Increased from 4
```

**4. Enable adaptive batching**:
```yaml
sinks:
  loki:
    adaptive_batching:
      enabled: true
      latency_threshold: "500ms"
      throughput_target: 1000
```

---

### Connection Issues

#### Issue: Loki Connection Timeouts

**Symptoms**:
```
Error sending to Loki: context deadline exceeded (timeout after 120s)
```

**Diagnosis**:
```bash
# Test Loki connectivity
curl -v http://loki:3100/ready

# Check Loki logs
docker logs loki | tail -50

# Test push endpoint
curl -X POST http://loki:3100/loki/api/v1/push \
  -H "Content-Type: application/json" \
  -d '{"streams":[{"stream":{"job":"test"},"values":[["1234567890000000000","test"]]}]}'
```

**Solutions**:

**1. Increase timeout**:
```yaml
sinks:
  loki:
    timeout: "180s"  # Increased from 120s

dispatcher:
  send_timeout: "180s"
```

**2. Reduce batch size** (less data per request):
```yaml
sinks:
  loki:
    batch_size: 250  # Reduced from 1000
    max_request_size: 1048576  # 1MB instead of 2MB
```

**3. Enable backpressure**:
```yaml
sinks:
  loki:
    backpressure_config:
      enabled: true
      timeout_escalation: true  # Dynamic timeout adjustment
```

**4. Check network latency**:
```bash
# Measure latency to Loki
docker exec log_capturer_go time curl -o /dev/null -s http://loki:3100/ready

# If high latency (> 100ms), consider:
# - Moving services closer
# - Using faster network
# - Increasing timeouts
```

---

#### Issue: Docker Socket Connection Issues

**Symptoms**:
```
Error: Cannot connect to the Docker daemon at unix:///var/run/docker.sock
```

**Diagnosis**:
```bash
# Check socket exists
ls -la /var/run/docker.sock

# Check permissions
docker exec log_capturer_go ls -la /var/run/docker.sock

# Test Docker API
docker exec log_capturer_go curl --unix-socket /var/run/docker.sock http://localhost/version
```

**Solutions**:

**1. Socket not mounted**:
```yaml
# docker-compose.yml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

**2. Permission denied**:
```yaml
# Run as root (or add user to docker group)
user: root

# Or on host
sudo chmod 666 /var/run/docker.sock
```

**3. SELinux issues** (CentOS/RHEL):
```bash
# Add SELinux label
docker run -v /var/run/docker.sock:/var/run/docker.sock:z ...

# Or disable SELinux (not recommended for production)
sudo setenforce 0
```

---

### Memory & Resource Issues

#### Issue: Goroutine Leak Detected

**Symptoms**:
```
WARN goroutine leak detected: current=8245, baseline=156, increase=8089
```

**Diagnosis**:
```bash
# Monitor goroutine count
watch -n 5 'curl -s http://localhost:8401/health | jq ".checks.memory.goroutines"'

# Get goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Analyze dump for stuck goroutines
grep -A 5 "goroutine" goroutines.txt | grep -E "(waiting|blocked)" | head -20
```

**Solutions**:

**1. Restart to clear**:
```bash
docker restart log_capturer_go
```

**2. Check for blocking operations**:
```bash
# Look for stuck container monitors
docker logs log_capturer_go 2>&1 | grep "container.*stuck"

# Look for stuck file readers
docker logs log_capturer_go 2>&1 | grep "file.*timeout"
```

**3. Adjust timeouts**:
```yaml
# Ensure operations have timeouts
file_monitor_service:
  read_interval: "500ms"  # Timeout for file reads

container_monitor:
  reconnect_interval: "30s"  # Reconnect timeout

dispatcher:
  send_timeout: "120s"  # Sink delivery timeout
```

**4. Enable goroutine tracking**:
```yaml
goroutine_tracking:
  enabled: true
  check_interval: "60s"
  leak_threshold: 100
  stack_trace_on_leak: true  # Get stack traces
```

---

#### Issue: File Descriptor Leak

**Symptoms**:
```
Error: too many open files
```

**Diagnosis**:
```bash
# Check FD limit
docker exec log_capturer_go ulimit -n

# Check current FD count
docker exec log_capturer_go ls /proc/self/fd | wc -l

# List open files
docker exec log_capturer_go lsof -p 1 | head -50
```

**Solutions**:

**1. Increase FD limit**:
```yaml
# docker-compose.yml
ulimits:
  nofile:
    soft: 65536
    hard: 65536
```

**2. Check for file leak**:
```bash
# Monitor FD count
watch -n 5 'docker exec log_capturer_go ls /proc/self/fd | wc -l'

# If growing, check for unclosed files
docker logs log_capturer_go 2>&1 | grep -i "file descriptor"
```

**3. Reduce file monitoring**:
```yaml
# Reduce max files
file_monitor_service:
  max_files: 500  # Reduced from 1000

# Or reduce directories monitored
files_config:
  watch_directories:
    - "/var/log/apps"  # Only specific directory
```

---

#### Issue: Disk Space Exhausted

**Symptoms**:
```
Error: no space left on device
```

**Diagnosis**:
```bash
# Check disk usage
df -h /app

# Check largest directories
du -h /app | sort -rh | head -10

# Check log file sizes
du -h /app/logs | sort -rh | head -10
```

**Solutions**:

**1. Enable disk cleanup**:
```yaml
cleanup:
  enabled: true
  check_interval: "15m"  # Check more frequently
  critical_space_threshold: 10.0  # Cleanup at 10% free

  directories:
    - path: "/app/logs"
      max_size_mb: 1024  # 1GB max
      retention_days: 3  # Reduced from 7
```

**2. Enable log compression**:
```yaml
sinks:
  local_file:
    rotation:
      compress: true  # Compress rotated logs
```

**3. Reduce buffer sizes**:
```yaml
disk_buffer:
  max_total_size: 1073741824  # 1GB (reduced from 5GB)

dispatcher:
  dlq_config:
    max_size_mb: 50  # Reduced from 100
```

**4. Manual cleanup**:
```bash
# Clean old logs
docker exec log_capturer_go find /app/logs -name "*.log" -mtime +7 -delete

# Clean compressed logs
docker exec log_capturer_go find /app/logs -name "*.gz" -mtime +3 -delete

# Clean DLQ
docker exec log_capturer_go find /app/dlq -name "*.json" -mtime +7 -delete
```

---

### Log Capture Issues

#### Issue: Logs Not Being Captured

**Symptoms**:
- No logs appearing in Loki or local files
- Low log count in metrics

**Diagnosis**:
```bash
# Check if any logs received
curl http://localhost:8401/stats | jq '.logs_received_total'

# Check file monitor status
curl http://localhost:8401/positions

# Check container monitor
docker logs log_capturer_go 2>&1 | grep -i "container"

# Check if sources are enabled
curl http://localhost:8401/config | jq '{file: .file_monitor_service.enabled, container: .container_monitor.enabled}'
```

**Solutions**:

**1. File monitor disabled**:
```yaml
file_monitor_service:
  enabled: true  # Must be true
```

**2. Container monitor disabled**:
```yaml
container_monitor:
  enabled: true  # Must be true
```

**3. No files match patterns**:
```yaml
# Check patterns in file_pipeline.yml
files:
  - path: "/var/log/app.log"  # Exact path
    enabled: true

directories:
  - path: "/var/log"
    patterns:
      - "*.log"  # All .log files
      - "syslog"
    enabled: true
```

**4. Files excluded by filters**:
```yaml
# Check exclude patterns
files_config:
  exclude_patterns:
    - "*.gz"
    - "*.old"
  exclude_directories:
    - "/var/log/monitoring_data_suite"
```

**5. Containers excluded**:
```yaml
container_monitor:
  exclude_names:
    - "log_capturer_go"  # Make sure target containers not in this list
```

---

#### Issue: Duplicate Logs

**Symptoms**:
- Same log appears multiple times in Loki
- Rapidly growing log count

**Diagnosis**:
```bash
# Check deduplication status
curl http://localhost:8401/config | jq '.dispatcher.deduplication_enabled'

# Check for multiple sources monitoring same files
curl http://localhost:8401/positions | jq '.files | keys'

# Check metrics for duplicate pattern
curl -s http://localhost:8001/metrics | grep logs_received_total
```

**Solutions**:

**1. Enable deduplication**:
```yaml
dispatcher:
  deduplication_enabled: true
  deduplication_config:
    max_cache_size: 100000
    ttl: "1h"
    include_source_id: true
```

**2. Multiple sources monitoring same file**:
```yaml
# In file_pipeline.yml, ensure each file monitored once
files:
  - path: "/var/log/app.log"
    enabled: true

# Remove duplicate entries
# - path: "/var/log/app.log"  # REMOVE THIS
```

**3. Container logs and file logs**:
```yaml
# If container writes to file, monitor only one source:

# Option A: Monitor container only
container_monitor:
  enabled: true
file_monitor_service:
  enabled: false

# Option B: Monitor file only, exclude container
container_monitor:
  enabled: true
  exclude_names:
    - "myapp-container"
file_monitor_service:
  enabled: true
```

---

#### Issue: Missing Log Lines

**Symptoms**:
- Some logs appear in source but not in Loki
- Gaps in log sequences

**Diagnosis**:
```bash
# Check for errors
curl http://localhost:8401/stats | jq '{failed: .logs_failed_total, dropped: .logs_dropped_total}'

# Check DLQ
curl http://localhost:8401/dlq/stats

# Check processing errors
docker logs log_capturer_go 2>&1 | grep -i "failed to process"
```

**Solutions**:

**1. Logs sent to DLQ**:
```bash
# Check DLQ
curl http://localhost:8401/dlq/stats

# Reprocess DLQ entries
curl -X POST http://localhost:8401/dlq/reprocess \
  -H "Content-Type: application/json" \
  -d '{"reprocess_all": true}'
```

**2. Queue overflow**:
```yaml
# Increase queue size
dispatcher:
  queue_size: 100000  # Increased from 50000
```

**3. Processing timeout**:
```yaml
# Increase processing timeout
processing:
  processing_timeout: "10s"  # Increased from 5s
  skip_failed_logs: false  # Don't skip failed logs
```

**4. File rotation**:
```yaml
# Configure rotation handling
monitoring:
  rotation_action: "reopen"  # Reopen rotated files
```

---

### Sink Delivery Issues

#### Issue: Logs Not Appearing in Loki

**Symptoms**:
- Stats show logs processed but not in Loki
- No errors in logs

**Diagnosis**:
```bash
# Check sink stats
curl -s http://localhost:8001/metrics | grep 'sink_logs_sent_total{sink="loki"}'

# Check Loki directly
curl "http://loki:3100/loki/api/v1/query?query={service=\"ssw-log-capturer\"}"

# Check Loki sink enabled
curl http://localhost:8401/config | jq '.sinks.loki.enabled'
```

**Solutions**:

**1. Sink disabled**:
```yaml
sinks:
  loki:
    enabled: true  # Must be true
```

**2. Wrong Loki URL**:
```yaml
sinks:
  loki:
    url: "http://loki:3100"  # Check service name
    push_endpoint: "/loki/api/v1/push"  # Correct endpoint
```

**3. Labels missing** (Loki requires labels):
```yaml
sinks:
  loki:
    default_labels:
      service: "ssw-log-capturer"  # At least one label required
      environment: "production"
```

**4. Loki tenant ID mismatch**:
```yaml
sinks:
  loki:
    tenant_id: ""  # Empty for single-tenant Loki
    # OR
    tenant_id: "prod"  # Must match Loki tenant configuration
```

---

#### Issue: DLQ Growing

**Symptoms**:
```
curl http://localhost:8401/dlq/stats
{"total_entries": 15234, "growth_rate": 50.3}
```

**Diagnosis**:
```bash
# Check DLQ entries
curl http://localhost:8401/dlq/stats | jq '.'

# Check failure reasons
docker exec log_capturer_go cat /app/dlq/dlq-*.json | jq '.failure_reason' | sort | uniq -c

# Check sink errors
curl -s http://localhost:8001/metrics | grep 'sink_errors_total'
```

**Solutions**:

**1. Loki connection issues**:
```bash
# Test Loki connectivity
curl -v http://loki:3100/ready

# Fix Loki or adjust timeout
```yaml
sinks:
  loki:
    timeout: "180s"
```

**2. Batch size too large**:
```yaml
sinks:
  loki:
    batch_size: 250  # Reduced from 1000
    max_request_size: 1048576  # 1MB
```

**3. Enable auto-reprocessing**:
```yaml
dispatcher:
  dlq_config:
    reprocessing_config:
      enabled: true
      interval: "5m"
      max_retries: 3
```

**4. Manual reprocessing**:
```bash
# Reprocess all DLQ entries
curl -X POST http://localhost:8401/dlq/reprocess \
  -H "Content-Type: application/json" \
  -d '{"reprocess_all": true}'
```

---

### Configuration Issues

#### Issue: Configuration Not Reloading

**Symptoms**:
```
curl -X POST http://localhost:8401/config/reload
{"error": "hot reload disabled"}
```

**Solution**:
```yaml
hot_reload:
  enabled: true  # Must be enabled
  watch_interval: "5s"
  validate_on_reload: true
```

---

#### Issue: Environment Variables Not Substituted

**Symptoms**:
```yaml
# In config
token: "${LOKI_TOKEN}"

# But literal string used instead of value
```

**Solution**:
```bash
# Ensure environment variable is set
export LOKI_TOKEN=secret_token

# Restart container to pick up new env vars
docker restart log_capturer_go

# Or use docker-compose
services:
  log_capturer:
    environment:
      - LOKI_TOKEN=secret_token
```

---

## Log Analysis

### Understanding Log Levels

**TRACE**: Most verbose, shows every operation
```json
{"level":"trace","msg":"Reading chunk from /var/log/app.log","size":4096}
```

**DEBUG**: Detailed debugging information
```json
{"level":"debug","msg":"Batching 500 logs for Loki"}
```

**INFO**: General informational messages
```json
{"level":"info","msg":"Container started","container":"myapp-prod"}
```

**WARN**: Warning conditions
```json
{"level":"warn","msg":"Queue utilization high","utilization":0.85}
```

**ERROR**: Error conditions
```json
{"level":"error","msg":"Failed to send to Loki","error":"timeout"}
```

**FATAL**: Fatal errors causing shutdown
```json
{"level":"fatal","msg":"Failed to initialize dispatcher","error":"..."}
```

### Common Log Patterns

**Successful startup**:
```
INFO  Starting SSW Logs Capture v0.0.2
INFO  Configuration loaded from /app/configs/config.yaml
INFO  Dispatcher started with 6 workers
INFO  File monitor service started
INFO  Container monitor started
INFO  HTTP server listening on :8401
INFO  Metrics server listening on :8001
```

**Normal operation**:
```
DEBUG Received 500 logs from file source
DEBUG Batching 500 logs for Loki
DEBUG Sent batch to Loki in 45ms
```

**Warning signs**:
```
WARN  Queue utilization: 85% (42500/50000)
WARN  Slow sink detected: loki latency=2.5s
WARN  Goroutine count increased: 8245 (+100 from baseline)
```

**Error patterns**:
```
ERROR Failed to send to Loki: timeout after 120s
ERROR Container connection lost: container_id=abc123
ERROR File read error: /var/log/app.log: permission denied
```

---

## Metrics Interpretation

### Key Metrics

**logs_received_total**:
- Total logs received from all sources
- Should be steadily increasing
- Rate indicates log volume

**logs_processed_total**:
- Successfully processed logs
- Should match `logs_received_total` closely
- Gap indicates processing failures

**dispatcher_queue_size**:
- Current entries in dispatcher queue
- Should stay below 80% of `queue_size`
- Continuously high = processing bottleneck

**dispatcher_queue_utilization**:
- Queue usage percentage (0-1.0)
- < 0.7 = healthy
- 0.7-0.85 = warning
- > 0.85 = critical

**sink_logs_sent_total{sink="loki"}**:
- Total logs delivered to Loki
- Should match `logs_processed_total`
- Difference indicates delivery failures

**sink_errors_total{sink="loki"}**:
- Delivery errors to Loki
- Should be 0 or very low
- Spike indicates Loki issues

**goroutines_count**:
- Current goroutine count
- Baseline varies by configuration
- Continuous growth = leak

**memory_alloc_bytes**:
- Currently allocated memory
- Should stabilize after startup
- Continuous growth = memory leak

### Healthy Metrics Example

```prometheus
# Logs flowing smoothly
logs_received_total 125000
logs_processed_total 125000
logs_failed_total 0

# Queue healthy
dispatcher_queue_size 2500
dispatcher_queue_utilization 0.05  # 5%

# Sinks delivering
sink_logs_sent_total{sink="loki"} 125000
sink_errors_total{sink="loki"} 0
sink_latency_seconds{sink="loki",quantile="0.99"} 0.085  # 85ms

# Resources stable
goroutines_count 156
memory_alloc_bytes 256000000  # ~244MB
file_descriptors_open 45
```

### Unhealthy Metrics Example

```prometheus
# Logs backing up
logs_received_total 150000
logs_processed_total 125000  # 25000 behind!
logs_failed_total 1500

# Queue pressure
dispatcher_queue_size 45000
dispatcher_queue_utilization 0.90  # 90% - CRITICAL!

# Sink issues
sink_logs_sent_total{sink="loki"} 120000
sink_errors_total{sink="loki"} 5000  # Many errors!
sink_latency_seconds{sink="loki",quantile="0.99"} 3.5  # Slow!

# Resource issues
goroutines_count 8500  # Growing!
memory_alloc_bytes 1200000000  # ~1.1GB - High!
file_descriptors_open 950  # Too many!
```

---

## Advanced Debugging

### Memory Profiling

**Capture heap profile**:
```bash
curl http://localhost:6060/debug/pprof/heap > heap.prof

# Analyze with pprof
go tool pprof -http=:8080 heap.prof
```

**Identify memory leaks**:
```bash
# Capture baseline
curl http://localhost:6060/debug/pprof/heap > heap-baseline.prof

# Wait 10 minutes under load
sleep 600

# Capture second profile
curl http://localhost:6060/debug/pprof/heap > heap-after.prof

# Compare
go tool pprof -base heap-baseline.prof heap-after.prof
```

### CPU Profiling

```bash
# Capture 30 seconds of CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof

# Analyze
go tool pprof -http=:8080 cpu.prof
```

### Goroutine Analysis

```bash
# Get goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Find most common goroutine states
grep "goroutine" goroutines.txt | wc -l  # Total count

# Find blocked goroutines
grep -B 3 "chan receive" goroutines.txt | grep "goroutine" | wc -l
```

### Distributed Tracing

If tracing is enabled:

```yaml
tracing:
  enabled: true
  exporter: "jaeger"
  endpoint: "http://jaeger:14268/api/traces"
```

View traces in Jaeger:
```bash
# Open Jaeger UI
open http://localhost:16686

# Search for traces
Service: ssw-logs-capture
Operation: log.process
```

---

## FAQ

### Q: How do I know if logs are being captured?

**A**: Check multiple indicators:
```bash
# 1. Check metrics
curl -s http://localhost:8001/metrics | grep logs_received_total

# 2. Check stats
curl http://localhost:8401/stats | jq '.logs_received_total'

# 3. Check Loki
curl "http://loki:3100/loki/api/v1/query?query={service=\"ssw-log-capturer\"}&limit=10"

# 4. Check local files
ls -lh /app/logs/output/
```

---

### Q: Why is the queue always full?

**A**: Queue is too small or sink is too slow:

```yaml
# Solution 1: Increase queue size
dispatcher:
  queue_size: 100000  # Doubled

# Solution 2: Increase workers
dispatcher:
  worker_count: 12  # Increased

# Solution 3: Optimize batching
sinks:
  loki:
    batch_size: 1000
    batch_timeout: "5s"
    adaptive_batching:
      enabled: true
```

---

### Q: How do I reduce memory usage?

**A**: Multiple approaches:

```yaml
# 1. Reduce queue sizes
dispatcher:
  queue_size: 25000
sinks:
  loki:
    queue_size: 10000

# 2. Reduce batch sizes
dispatcher:
  batch_size: 250

# 3. Reduce dedup cache
dispatcher:
  deduplication_config:
    max_cache_size: 50000
    ttl: "30m"

# 4. Enable GC optimization
resource_monitoring:
  enable_gc_optimization: true

# 5. Set memory limits
# docker-compose.yml
deploy:
  resources:
    limits:
      memory: 512M
```

---

### Q: How do I handle log bursts?

**A**: Configure buffering and backpressure:

```yaml
# 1. Enable disk buffer for bursts
disk_buffer:
  enabled: true
  max_total_size: 5368709120  # 5GB

# 2. Enable backpressure
sinks:
  loki:
    backpressure_config:
      enabled: true
      timeout_escalation: true

# 3. Increase queues temporarily
dispatcher:
  queue_size: 200000  # Large queue for bursts
```

---

### Q: Logs are delayed by 30+ seconds, why?

**A**: Batching timeout too high:

```yaml
# Reduce batch timeout for lower latency
dispatcher:
  batch_timeout: "2s"  # Reduced from 10s

sinks:
  loki:
    batch_timeout: "2s"

# Or enable adaptive batching
sinks:
  loki:
    adaptive_batching:
      enabled: true
      latency_threshold: "500ms"
```

---

## Getting Help

### Before Asking for Help

Collect this information:

```bash
# 1. Version
docker exec log_capturer_go /app/ssw-logs-capture --version

# 2. Configuration (sanitized)
curl http://localhost:8401/config > config-sanitized.json

# 3. Recent logs
docker logs log_capturer_go --tail 200 > logs.txt

# 4. Health status
curl http://localhost:8401/health > health.json

# 5. Statistics
curl http://localhost:8401/stats > stats.json

# 6. Metrics
curl http://localhost:8001/metrics > metrics.txt

# 7. System info
docker exec log_capturer_go uname -a
docker stats log_capturer_go --no-stream
df -h /app
```

### Support Channels

1. **GitHub Issues**: https://github.com/your-org/log-capturer/issues
2. **Documentation**: https://docs.example.com/log-capturer
3. **Internal Support**: support@example.com

### Creating a Good Bug Report

Include:
- **Environment**: OS, Docker version, deployment method
- **Configuration**: Sanitized config.yaml
- **Symptoms**: What's happening vs what should happen
- **Logs**: Relevant error messages
- **Metrics**: Key metrics showing the issue
- **Reproduction**: Steps to reproduce

---

**Last Updated**: 2025-11-01
**Version**: 1.0
**Maintained By**: SSW Logs Capture Team
