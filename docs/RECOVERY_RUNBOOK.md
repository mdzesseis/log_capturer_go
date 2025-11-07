# Recovery Runbook - SSW Log Capturer

**Version**: 1.0
**Last Updated**: 2025-11-07
**Owner**: Operations Team

---

## Table of Contents

1. [Quick Reference](#quick-reference)
2. [Common Failure Scenarios](#common-failure-scenarios)
3. [Recovery Procedures](#recovery-procedures)
4. [Health Check Procedures](#health-check-procedures)
5. [Escalation Matrix](#escalation-matrix)
6. [Post-Recovery Validation](#post-recovery-validation)

---

## Quick Reference

### Emergency Contacts
- **Primary On-Call**: [Your Team Contact]
- **Secondary On-Call**: [Backup Contact]
- **Escalation**: [Manager Contact]

### Critical Commands
```bash
# Health check
curl http://localhost:8401/health | jq '.'

# Restart service
docker-compose restart log_capturer

# View logs (last 100 lines)
docker logs --tail 100 log_capturer_go

# Check metrics
curl http://localhost:8001/metrics | grep log_capturer

# Stop gracefully
docker-compose stop log_capturer

# Force stop (if hung)
docker-compose kill log_capturer && docker-compose rm -f log_capturer
```

### Key Metrics Endpoints
- **Health**: `http://localhost:8401/health`
- **Metrics**: `http://localhost:8001/metrics`
- **Grafana Dashboards**: `http://localhost:3000/dashboards`
- **Prometheus**: `http://localhost:9090`

---

## Common Failure Scenarios

### 1. Goroutine Leak (High Memory/CPU)

**Symptoms**:
- Goroutine count > 8000 (critical) or > 5000 (warning)
- Memory usage > 80% or steadily increasing
- CPU usage > 80%

**Root Causes**:
- Container monitor stream not properly closed
- HTTP connection pool leak
- Unclosed file handles
- Retry queue saturation

**Immediate Actions**:
```bash
# 1. Check current goroutine count
curl http://localhost:8001/metrics | grep "go_goroutines"

# 2. Check goroutine growth rate
curl http://localhost:8001/metrics | grep "log_capturer_goroutines"

# 3. Get goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof -http=:8080 goroutine.prof

# 4. Check for open file descriptors
curl http://localhost:8001/metrics | grep "log_capturer_file_descriptors"

# 5. Restart service (if critical)
docker-compose restart log_capturer
```

**Recovery**:
1. Identify leak source from goroutine profile
2. Check container monitor active streams: `log_capturer_container_streams_active`
3. Check for stuck HTTP connections: `log_capturer_connection_pool_stats`
4. If leak confirmed, restart service
5. Monitor for recurrence over next 2 hours

**Prevention**:
- Monitor `log_capturer_goroutines` metric with alerts
- Ensure stream rotation occurs every 5 minutes
- Check connection pool health regularly

---

### 2. Position File Corruption

**Symptoms**:
- `log_capturer_position_corruptions_detected_total` > 0
- Logs replayed from beginning of file
- Data duplication in Loki

**Root Causes**:
- Unclean shutdown (SIGKILL)
- Disk I/O errors
- File system corruption
- Race condition during write (fixed in Phase 1)

**Immediate Actions**:
```bash
# 1. Check corruption detection metric
curl http://localhost:8001/metrics | grep "log_capturer_position_corruptions"

# 2. Verify checkpoint availability
ls -lh /app/data/positions/checkpoints/

# 3. Check latest checkpoint age
stat /app/data/positions/checkpoints/positions_checkpoint_gen_*.json

# 4. Check position validation failures
curl http://localhost:8001/metrics | grep "log_capturer_position_validation_failures"
```

**Recovery**:

**Option A: Automatic Recovery (Preferred)**
```bash
# System automatically recovers from latest checkpoint
# No action needed - monitor recovery progress

# Verify recovery completed
curl http://localhost:8001/metrics | grep "log_capturer_position_checkpoint_restores_total"
```

**Option B: Manual Recovery from Checkpoint**
```bash
# 1. Stop service
docker-compose stop log_capturer

# 2. List available checkpoints (3 generations)
ls -lh /app/data/positions/checkpoints/

# 3. Find latest valid checkpoint
cat /app/data/positions/checkpoints/positions_checkpoint_gen_0.json | jq '.'

# 4. Restore checkpoint (copy to main position file)
cp /app/data/positions/checkpoints/positions_checkpoint_gen_0.json \
   /app/data/positions/positions.json

# 5. Validate JSON integrity
cat /app/data/positions/positions.json | jq '.' > /dev/null && echo "Valid JSON"

# 6. Start service
docker-compose start log_capturer

# 7. Verify recovery
docker logs --tail 50 log_capturer_go | grep "position.*restored"
```

**Option C: Full Reset (Data Loss - Last Resort)**
```bash
# WARNING: This will replay ALL logs from file beginning
# Only use if all checkpoints are corrupted

# 1. Stop service
docker-compose stop log_capturer

# 2. Backup corrupt position file
mv /app/data/positions/positions.json /app/data/positions/positions.json.corrupt.$(date +%s)

# 3. Remove checkpoints
rm -f /app/data/positions/checkpoints/*

# 4. Start service (will start from beginning)
docker-compose start log_capturer

# 5. Monitor for duplicates
curl http://localhost:8001/metrics | grep "log_capturer_deduplication_duplicate_rate"
```

**Post-Recovery**:
- Monitor deduplication rate (expect spike if logs replayed)
- Check Loki for duplicate entries
- Verify position saves are working: `log_capturer_position_saves_total`

**Prevention**:
- Never use `docker kill` (use `docker stop` for graceful shutdown)
- Ensure `force_flush_on_exit: true` in config
- Monitor checkpoint saves: alert if `time() - log_capturer_position_checkpoint_last_saved_timestamp > 600`

---

### 3. DLQ (Dead Letter Queue) Overflow

**Symptoms**:
- `log_capturer_dlq_entries_total` > 10,000 (critical)
- `log_capturer_queue_size{component="dlq"}` > 8000
- DLQ write failures occurring
- Disk space decreasing

**Root Causes**:
- Loki sink down or unreachable
- Timestamp rejection storm (old logs)
- Network connectivity issues
- Persistent sink errors

**Immediate Actions**:
```bash
# 1. Check DLQ size
curl http://localhost:8001/metrics | grep "log_capturer_dlq_entries_total"

# 2. Check DLQ directory size
du -sh /tmp/dlq/

# 3. Check recent DLQ writes
ls -lth /tmp/dlq/ | head -20

# 4. Check sink health
curl http://localhost:8401/health | jq '.services.sinks'

# 5. Check Loki connectivity
curl http://loki:3100/ready
```

**Recovery**:

**Step 1: Identify Root Cause**
```bash
# Check Loki errors
curl http://localhost:8001/metrics | grep "log_capturer_loki_error_type"

# Check timestamp rejections
curl http://localhost:8001/metrics | grep "log_capturer_timestamp_rejection"

# Check sink latency
curl http://localhost:8001/metrics | grep "log_capturer_sink_send_duration"
```

**Step 2: Fix Root Cause**
- If Loki down: Restart Loki, verify with `curl http://loki:3100/ready`
- If timestamp errors: Enable timestamp learning or adjust retention
- If network issues: Check Docker network, firewall rules

**Step 3: Reprocess DLQ (Automatic)**
```bash
# DLQ reprocessing is automatic (every 5 minutes)
# Monitor reprocessing progress

# Check reprocessing metrics
curl http://localhost:8001/metrics | grep "log_capturer_dlq_reprocessing"

# Expected metrics:
# - log_capturer_dlq_reprocessing_attempts_total (increasing)
# - log_capturer_dlq_reprocessing_successes_total (increasing)
# - log_capturer_dlq_entries_total (decreasing)
```

**Step 4: Manual DLQ Reprocessing (if automatic fails)**
```bash
# Use recovery script
cd /home/mateus/log_capturer_go
./scripts/recovery/reprocess_dlq.sh

# Or manual reprocessing:
# 1. Stop automatic reprocessing (set reprocessing_enabled: false in config)
# 2. Send DLQ entries directly to Loki
for file in /tmp/dlq/*.json; do
  curl -X POST http://loki:3100/loki/api/v1/push \
    -H "Content-Type: application/json" \
    -d @"$file"
done
# 3. Re-enable automatic reprocessing
```

**Step 5: DLQ Purge (if logs are unrecoverable)**
```bash
# WARNING: This deletes logs permanently
# Only use after verifying logs are in Loki or not needed

# 1. Archive DLQ (for forensics)
tar -czf /tmp/dlq_backup_$(date +%s).tar.gz /tmp/dlq/

# 2. Purge DLQ
rm -f /tmp/dlq/*.json

# 3. Verify metrics reset
curl http://localhost:8001/metrics | grep "log_capturer_dlq_entries_total"
```

**Prevention**:
- Monitor Loki health continuously
- Alert on DLQ growth: `increase(log_capturer_dlq_entries_total[10m]) > 100`
- Enable timestamp learning to prevent rejection storms
- Configure DLQ max size limits

---

### 4. Dispatcher Queue Saturation

**Symptoms**:
- `log_capturer_queue_size{component="dispatcher"}` > 45,000 (90% full)
- Slow log processing
- Increasing latency
- Potential log drops

**Root Causes**:
- Sink latency (Loki slow or down)
- Worker pool undersized
- Batch size misconfigured
- Upstream log volume spike

**Immediate Actions**:
```bash
# 1. Check queue utilization
curl http://localhost:8001/metrics | grep 'log_capturer_queue_size{.*dispatcher'

# 2. Check sink latency
curl http://localhost:8001/metrics | grep "log_capturer_sink_send_duration"

# 3. Check worker activity
curl http://localhost:8001/metrics | grep "log_capturer_active_tasks"

# 4. Check log ingestion rate
curl http://localhost:8001/metrics | grep "log_capturer_logs_per_second"
```

**Recovery**:

**Option A: Scale Workers (Quick Fix)**
```bash
# Edit config.yaml
vim /app/configs/config.yaml

# Increase worker_count (temporarily)
dispatcher:
  worker_count: 12  # was 6

# Reload config (hot reload)
curl -X POST http://localhost:8401/config/reload
```

**Option B: Increase Batch Size (if sink can handle)**
```bash
# Edit config.yaml
dispatcher:
  batch_size: 1000  # was 500
  batch_timeout: "5s"

# Reload config
curl -X POST http://localhost:8401/config/reload
```

**Option C: Enable Adaptive Batching**
```bash
# Edit config.yaml
sinks:
  loki:
    adaptive_batching:
      enabled: true

# Reload config
curl -X POST http://localhost:8401/config/reload
```

**Option D: Restart Service (if stuck)**
```bash
docker-compose restart log_capturer
```

**Post-Recovery**:
- Monitor queue utilization (should drop < 70%)
- Check for dropped logs
- Validate sink latency normalized

**Prevention**:
- Alert on queue utilization > 70%
- Monitor sink health proactively
- Size worker pool appropriately (see Task 9 tuning guide)

---

### 5. Timestamp Rejection Storm

**Symptoms**:
- `log_capturer_timestamp_rejection_total` rapidly increasing
- `log_capturer_loki_error_type_total{error_type="timestamp_too_old"}` high
- DLQ growing quickly
- "Timestamp too old" errors in logs

**Root Causes**:
- File monitor reading historical logs (days/weeks old)
- Loki retention too short
- Timestamp learning disabled
- File rotation causing replay

**Immediate Actions**:
```bash
# 1. Check rejection rate
curl http://localhost:8001/metrics | grep "log_capturer_timestamp_rejection_total"

# 2. Check Loki timestamp errors
curl http://localhost:8001/metrics | grep 'log_capturer_loki_error_type.*timestamp'

# 3. Check timestamp max acceptable age
curl http://localhost:8001/metrics | grep "log_capturer_timestamp_max_acceptable_age"

# 4. Check file monitor start time
docker logs log_capturer_go | grep "file_monitor.*started"
```

**Recovery**:

**Option A: Enable Timestamp Learning (Preferred)**
```bash
# Edit config.yaml
sinks:
  loki:
    timestamp_learning:
      enabled: true
      learn_from_errors: true
      clamp_enabled: false  # Reject instead of modify

# Reload config
curl -X POST http://localhost:8401/config/reload

# Verify learning is working
curl http://localhost:8001/metrics | grep "log_capturer_timestamp_max_acceptable_age"
# Should auto-adjust based on Loki rejections
```

**Option B: Enable Timestamp Clamping (if preservation not critical)**
```bash
# Edit config.yaml
sinks:
  loki:
    timestamp_learning:
      enabled: true
      clamp_enabled: true  # Adjust old timestamps

# Reload config
curl -X POST http://localhost:8401/config/reload
```

**Option C: Ignore Old Logs at Source**
```bash
# Edit config.yaml
file_monitor_service:
  ignore_old_timestamps: true
  seek_strategy: "recent"  # Skip old logs in files

# Reload config
curl -X POST http://localhost:8401/config/reload
```

**Option D: Increase Loki Retention (if possible)**
```bash
# Edit Loki config (requires Loki restart)
# loki-config.yaml:
limits_config:
  reject_old_samples: false
  reject_old_samples_max_age: 168h  # 7 days

# Restart Loki
docker-compose restart loki
```

**Post-Recovery**:
- Monitor rejection rate (should drop to near zero)
- Check DLQ stops growing
- Verify learned timestamp threshold is reasonable

**Prevention**:
- Always enable timestamp learning in production
- Configure appropriate Loki retention
- Use `seek_strategy: "recent"` for historical files

---

### 6. High Deduplication Rate

**Symptoms**:
- `log_capturer_deduplication_duplicate_rate` > 30%
- High cache hit rate but many duplicates
- Possible log source issues

**Root Causes**:
- Application retry logic
- Container restarts/crashes
- File rotation with overlap
- Network-induced retransmission

**Immediate Actions**:
```bash
# 1. Check duplicate rate
curl http://localhost:8001/metrics | grep "log_capturer_deduplication_duplicate_rate"

# 2. Check duplicates by source
curl http://localhost:8001/metrics | grep "log_capturer_logs_deduplicated_total"

# 3. Check cache statistics
curl http://localhost:8001/metrics | grep "log_capturer_deduplication_cache"

# 4. Identify problematic source
# Look for specific source_id with high duplicates
```

**Recovery**:
1. Identify source with highest duplicate rate
2. Investigate source behavior (container logs, application logs)
3. If acceptable, no action needed (deduplication working as intended)
4. If excessive, investigate application retry logic

**Prevention**:
- Alert on duplicate rate > 30% sustained for 10 minutes
- Monitor source-specific duplicate rates
- Tune TTL if needed (default: 10s)

---

## Recovery Procedures

### Graceful Restart
```bash
# 1. Notify team
echo "Starting graceful restart at $(date)"

# 2. Check current health
curl http://localhost:8401/health

# 3. Graceful stop (allows flush)
docker-compose stop log_capturer
# Wait up to 30s for graceful shutdown

# 4. Verify stopped
docker ps | grep log_capturer

# 5. Check position file saved
ls -lh /app/data/positions/positions.json

# 6. Start service
docker-compose start log_capturer

# 7. Wait for health check
sleep 10
curl http://localhost:8401/health

# 8. Verify metrics flowing
curl http://localhost:8001/metrics | grep "log_capturer_logs_processed_total"
```

### Force Restart (Emergency)
```bash
# WARNING: Use only if graceful restart fails

# 1. Force kill
docker-compose kill log_capturer

# 2. Remove container
docker-compose rm -f log_capturer

# 3. Recreate and start
docker-compose up -d log_capturer

# 4. Monitor recovery
docker logs -f log_capturer_go
```

### Configuration Reload (Hot Reload)
```bash
# 1. Validate new config
cat /app/configs/config.yaml | yq '.' > /dev/null

# 2. Trigger reload
curl -X POST http://localhost:8401/config/reload

# 3. Check reload status
docker logs --tail 20 log_capturer_go | grep "config.*reload"

# 4. Verify new config applied
curl http://localhost:8401/health
```

### Database/State Recovery
```bash
# Recovery script location
cd /home/mateus/log_capturer_go/scripts/recovery

# Available scripts:
# - reprocess_dlq.sh         : Reprocess DLQ entries
# - restore_positions.sh     : Restore position file from checkpoint
# - validate_positions.sh    : Validate position file integrity
# - cleanup_dlq.sh           : Archive and purge DLQ
# - health_check.sh          : Comprehensive health check

# Example: Restore positions
./restore_positions.sh --generation 0

# Example: Validate positions
./validate_positions.sh

# Example: Health check
./health_check.sh --verbose
```

---

## Health Check Procedures

### Quick Health Check
```bash
#!/bin/bash
# Quick health check - use scripts/recovery/health_check.sh for full check

echo "=== Log Capturer Health Check ==="

# Service Status
echo "1. Service Status:"
docker ps | grep log_capturer

# Health Endpoint
echo "2. Health Endpoint:"
curl -s http://localhost:8401/health | jq '.status'

# Goroutines
echo "3. Goroutines:"
curl -s http://localhost:8001/metrics | grep "go_goroutines" | tail -1

# Queue Size
echo "4. Queue Utilization:"
QUEUE_SIZE=$(curl -s http://localhost:8001/metrics | grep 'log_capturer_queue_size{.*dispatcher' | awk '{print $2}')
echo "$QUEUE_SIZE / 50000 = $(echo "scale=2; $QUEUE_SIZE / 50000 * 100" | bc)%"

# Logs Processed
echo "5. Logs Processed (last minute):"
curl -s http://localhost:8001/metrics | grep "log_capturer_logs_processed_total" | tail -1

# Errors
echo "6. Recent Errors:"
curl -s http://localhost:8001/metrics | grep "log_capturer_errors_total" | tail -1

# Position System
echo "7. Position System:"
echo "  - Corruptions: $(curl -s http://localhost:8001/metrics | grep 'position_corruptions_detected_total' | awk '{print $2}')"
echo "  - Checkpoint age: $(( $(date +%s) - $(curl -s http://localhost:8001/metrics | grep 'position_checkpoint_last_saved_timestamp' | awk '{print $2}') )) seconds"

# DLQ
echo "8. DLQ Status:"
echo "  - Entries: $(curl -s http://localhost:8001/metrics | grep 'log_capturer_dlq_entries_total' | awk '{print $2}')"
echo "  - Queue size: $(curl -s http://localhost:8001/metrics | grep 'log_capturer_queue_size{.*dlq' | awk '{print $2}')"

echo "=== Health Check Complete ==="
```

### Comprehensive Health Check
```bash
# Use the provided health check script
/home/mateus/log_capturer_go/scripts/recovery/health_check.sh --verbose --output /tmp/health_report.txt

# Or manually:
./scripts/recovery/health_check.sh \
  --checks all \
  --threshold-goroutines 5000 \
  --threshold-queue 35000 \
  --threshold-dlq 5000 \
  --output health_report_$(date +%Y%m%d_%H%M%S).txt
```

---

## Escalation Matrix

### Severity Levels

**P0 - Critical (Escalate Immediately)**
- Service completely down (> 5 minutes)
- Data loss occurring
- DLQ write failures
- All sinks failing

**P1 - Urgent (Escalate within 30 minutes)**
- Goroutine leak detected
- Queue > 90% full
- DLQ > 10,000 entries
- Position corruption recurring

**P2 - High (Escalate within 2 hours)**
- Performance degradation
- High error rate (> 1%)
- Sink latency issues
- Timestamp rejection storm

**P3 - Medium (Next business day)**
- Configuration tuning needed
- Metric anomalies
- Dashboard issues
- Documentation updates

### Escalation Contacts
1. **On-Call Engineer** (P0, P1) - [Contact]
2. **Team Lead** (P0, P1 unresolved after 1 hour) - [Contact]
3. **Manager** (P0 unresolved after 2 hours) - [Contact]

---

## Post-Recovery Validation

### Validation Checklist
- [ ] Service is running: `docker ps | grep log_capturer`
- [ ] Health endpoint returns "healthy": `curl http://localhost:8401/health`
- [ ] Goroutine count < 5000: Check `log_capturer_goroutines`
- [ ] Queue utilization < 70%: Check `log_capturer_queue_size`
- [ ] Logs being processed: Check `log_capturer_logs_processed_total` increasing
- [ ] No recent errors: Check `log_capturer_errors_total` not increasing rapidly
- [ ] Position system working: Check `log_capturer_position_saves_total` increasing
- [ ] Checkpoints being saved: Check last checkpoint < 10 minutes old
- [ ] DLQ stable or decreasing: Check `log_capturer_dlq_entries_total`
- [ ] Sinks healthy: Check `log_capturer_component_health{component_type="sink"}`
- [ ] Deduplication working: Check `log_capturer_deduplication_cache_size` > 0
- [ ] Timestamp learning active (if enabled): Check `log_capturer_timestamp_max_acceptable_age_seconds`

### Monitoring Period
After recovery, monitor for:
- **First 10 minutes**: Watch for immediate issues (crashes, errors)
- **First hour**: Monitor for goroutine leaks, queue saturation
- **First 24 hours**: Monitor for position corruption recurrence, DLQ growth
- **First week**: Monitor for performance degradation, unexpected behavior

### Documentation
After each incident:
1. Document what happened in incident log
2. Document recovery steps taken
3. Identify root cause
4. Update this runbook if new procedures discovered
5. Create preventive measures/alerts if needed
6. Share learnings with team

---

## Additional Resources

### Scripts
- `/home/mateus/log_capturer_go/scripts/recovery/` - Recovery automation scripts
- `/home/mateus/log_capturer_go/scripts/health_check.sh` - Health monitoring

### Documentation
- `/home/mateus/log_capturer_go/docs/CLAUDE.md` - Developer guide
- `/home/mateus/log_capturer_go/docs/CONFIGURATION.md` - Config reference
- `/home/mateus/log_capturer_go/docs/DEDUPLICATION_ANALYSIS.md` - Deduplication system
- `/home/mateus/log_capturer_go/docs/POSITION_SYSTEM_ANALYSIS.md` - Position system deep-dive

### Dashboards
- **Critical Metrics**: http://localhost:3000/d/log-capturer-critical
- **Deduplication**: http://localhost:3000/d/log-capturer-deduplication
- **Timestamp Learning**: http://localhost:3000/d/log-capturer-timestamp
- **Position System**: http://localhost:3000/d/log-capturer-position

### Alerts
- Prometheus alerts: http://localhost:9090/alerts
- Alert rules: `/home/mateus/log_capturer_go/provisioning/alerts/rules.yml`

---

**Document Version**: 1.0
**Last Reviewed**: 2025-11-07
**Next Review**: 2025-12-07
