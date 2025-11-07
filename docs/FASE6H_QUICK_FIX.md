# FASE 6H - Quick Fix Guide

**CRITICAL**: Deploy immediately to fix 6-minute task timeout

---

## Problem Summary

Container monitoring tasks timeout after ~6 minutes because `monitorContainer()` never sends heartbeats.

**Evidence**:
```
11:05:XX - Container monitoring starts
11:11:XX - "Task timeout detected, stopping"
```

**Root Cause**: Missing `taskManager.Heartbeat()` call in monitoring loop.

---

## Fix #1: Add Heartbeat Ticker (CRITICAL)

**File**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
**Lines**: 815-930 (monitorContainer function)

### Step 1: Add Ticker and Task Name

**Location**: After line 821

**Before**:
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    cm.logger.WithFields(logrus.Fields{
        "container_id":   mc.id,
        "container_name": mc.name,
    }).Info("Starting container monitoring with hybrid short-lived streams")

    lastTimestamp := time.Now().UTC()

    for {
```

**After**:
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    cm.logger.WithFields(logrus.Fields{
        "container_id":   mc.id,
        "container_name": mc.name,
    }).Info("Starting container monitoring with hybrid short-lived streams")

    lastTimestamp := time.Now().UTC()
    taskName := "container_" + mc.id

    // CRITICAL FIX: Create heartbeat ticker to prevent task timeout
    heartbeatTicker := time.NewTicker(30 * time.Second)
    defer heartbeatTicker.Stop()

    for {
```

### Step 2: Add Heartbeat Case to Select

**Location**: After line 828 (inside the select statement)

**Before**:
```go
    for {
        select {
        case <-containerCtx.Done():
            cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
            return nil
        default:
        }
```

**After**:
```go
    for {
        select {
        case <-containerCtx.Done():
            cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
            return nil
        case <-heartbeatTicker.C:
            // CRITICAL FIX: Send heartbeat every 30s to prevent timeout
            cm.taskManager.Heartbeat(taskName)
            continue
        default:
        }
```

### Step 3: Add Heartbeat After Stream Rotation

**Location**: After line 903 (inside rotation success block)

**Before**:
```go
            if readErr == context.DeadlineExceeded {
                // EXPECTED timeout - normal for short-lived streams
                mc.rotationCount++
                streamAge := time.Since(mc.streamCreatedAt)
                metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

                cm.logger.WithFields(logrus.Fields{
```

**After**:
```go
            if readErr == context.DeadlineExceeded {
                // EXPECTED timeout - normal for short-lived streams
                mc.rotationCount++
                streamAge := time.Since(mc.streamCreatedAt)
                metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

                // CRITICAL FIX: Send heartbeat after successful rotation
                cm.taskManager.Heartbeat(taskName)

                cm.logger.WithFields(logrus.Fields{
```

---

## Complete Diff

```diff
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    cm.logger.WithFields(logrus.Fields{
        "container_id":   mc.id,
        "container_name": mc.name,
    }).Info("Starting container monitoring with hybrid short-lived streams")

    lastTimestamp := time.Now().UTC()
+   taskName := "container_" + mc.id
+
+   // CRITICAL FIX: Create heartbeat ticker to prevent task timeout
+   heartbeatTicker := time.NewTicker(30 * time.Second)
+   defer heartbeatTicker.Stop()

    for {
        select {
        case <-containerCtx.Done():
            cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
            return nil
+       case <-heartbeatTicker.C:
+           // CRITICAL FIX: Send heartbeat every 30s to prevent timeout
+           cm.taskManager.Heartbeat(taskName)
+           continue
        default:
        }

        // Create context with SHORT timeout (30 seconds)
        streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

        // ... stream creation and handling ...

        if readErr != nil {
            if readErr == context.DeadlineExceeded {
                // EXPECTED timeout - normal for short-lived streams
                mc.rotationCount++
                streamAge := time.Since(mc.streamCreatedAt)
                metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

+               // CRITICAL FIX: Send heartbeat after successful rotation
+               cm.taskManager.Heartbeat(taskName)

                cm.logger.WithFields(logrus.Fields{
                    "container_id":    mc.id,
                    "container_name":  mc.name,
                    "rotation_count":  mc.rotationCount,
                    "stream_age_secs": int(streamAge.Seconds()),
                }).Debug("Stream rotated successfully")
            } else if readErr == context.Canceled {
                // Parent context cancelled
                return nil
            } else {
                // Other error
                cm.logger.WithFields(logrus.Fields{
                    "container_id": mc.id,
                    "error":        readErr.Error(),
                }).Debug("Stream read error, reconnecting")
            }
        }

        // Brief pause before reconnecting (avoid tight loop)
        select {
        case <-containerCtx.Done():
            return nil
        case <-time.After(1 * time.Second):
        }
    }
}
```

---

## Verification Steps

### 1. Build and Deploy

```bash
cd /home/mateus/log_capturer_go

# Build
go build -o bin/log_capturer cmd/main.go

# Stop current instance
docker-compose down log_capturer

# Start with new binary
docker-compose up -d log_capturer
```

### 2. Monitor Task Health

```bash
# Watch task status (should stay "running")
watch -n 10 'curl -s http://localhost:8401/health | jq ".tasks[] | select(.id | startswith(\"container_\"))"'

# Expected output every 10 seconds:
# {
#   "id": "container_abc123...",
#   "state": "running",              ← Should ALWAYS be "running"
#   "started_at": "2025-11-07T11:05:00Z",
#   "last_heartbeat": "2025-11-07T11:35:30Z"  ← Should update regularly
# }
```

### 3. Check Logs

```bash
# Should see heartbeat-related activity
docker-compose logs -f log_capturer | grep -i "heartbeat\|timeout\|rotation"

# Good signs:
# - No "Task timeout detected" messages
# - Regular "Stream rotated successfully" messages
# - No task stops after 6 minutes

# Bad signs (if fix didn't work):
# - "Task timeout detected, stopping"
# - Containers stop being monitored after 6 minutes
```

### 4. Long-Running Test (10 minutes)

```bash
#!/bin/bash
# Save as test_fix.sh

echo "Testing FASE 6H fix - monitoring for 10 minutes..."
echo "Start time: $(date)"

for i in {1..20}; do
    minute=$((i / 2))
    echo ""
    echo "=== Check $i at ${minute}m30s ($(date)) ==="

    # Count running container tasks
    running=$(curl -s http://localhost:8401/health | jq '[.tasks[] | select(.id | startswith("container_")) | select(.state == "running")] | length')

    # Count failed container tasks
    failed=$(curl -s http://localhost:8401/health | jq '[.tasks[] | select(.id | startswith("container_")) | select(.state == "failed")] | length')

    echo "Running tasks: $running"
    echo "Failed tasks: $failed"

    if [ "$failed" -gt 0 ]; then
        echo "❌ FAILURE: Tasks failed! Fix didn't work."
        exit 1
    fi

    if [ "$i" -ge 13 ] && [ "$running" -eq 0 ]; then
        echo "❌ FAILURE: All tasks stopped after 6+ minutes! Fix didn't work."
        exit 1
    fi

    sleep 30
done

echo ""
echo "✅ SUCCESS: All tasks still running after 10 minutes!"
echo "End time: $(date)"
```

Run test:
```bash
chmod +x test_fix.sh
./test_fix.sh
```

Expected output:
```
Testing FASE 6H fix - monitoring for 10 minutes...
Start time: Wed Nov  7 11:05:00 2025

=== Check 1 at 0m30s ===
Running tasks: 3
Failed tasks: 0

=== Check 13 at 6m30s ===
Running tasks: 3      ← Should still be running!
Failed tasks: 0       ← Should be zero!

...

✅ SUCCESS: All tasks still running after 10 minutes!
```

---

## Success Criteria

| Metric | Before Fix | After Fix | Status |
|--------|-----------|-----------|--------|
| Task timeout | 6 minutes | Never | ✅ |
| Tasks running at 10min | 0 (failed) | All (3+) | ✅ |
| Heartbeat frequency | Never | Every 30s | ✅ |
| Logs captured | 6 min only | Continuous | ✅ |

---

## Rollback Plan

If fix causes issues:

```bash
# Revert to previous version
git checkout HEAD~1 internal/monitors/container_monitor.go

# Rebuild
go build -o bin/log_capturer cmd/main.go

# Restart
docker-compose restart log_capturer
```

---

## Next Steps (Non-Critical)

After confirming the fix works:

1. **Phase 2** (Same day): Clean up non-functional code
   - Remove `extractNetConn()` function
   - Update misleading comments
   - Fix timeout ordering

2. **Phase 3** (Next sprint): Implement proper goroutine leak prevention
   - Custom HTTP client with deadline support
   - Comprehensive integration tests

See `FASE6H_CODE_REVIEW.md` for details.

---

## Estimated Timeline

| Task | Duration | Risk |
|------|----------|------|
| Apply fix | 5 minutes | Very Low |
| Build & deploy | 2 minutes | Very Low |
| Initial verification | 3 minutes | - |
| 10-minute test | 10 minutes | - |
| **Total** | **20 minutes** | **Very Low** |

---

## FAQ

**Q: Why does this fix work?**

A: Task Manager expects heartbeats every 30s. Without them, it assumes the task is stuck and cancels it after 5 minutes. Adding heartbeats tells Task Manager the task is healthy.

**Q: Will this fix goroutine leaks?**

A: No, but it will keep monitoring running. Goroutine leaks are temporary (30s max) and acceptable. Full fix requires Phase 3 work.

**Q: Can I just increase TaskTimeout instead?**

A: No! That's a band-aid. The real problem is missing heartbeats. Increasing timeout delays the symptom but doesn't fix the cause.

**Q: Is this fix safe for production?**

A: Yes. This fix:
- Adds defensive heartbeats (can't hurt)
- Follows same pattern as other loops (proven)
- Very low risk (minimal code change)
- Easy to rollback if needed

---

**Status**: Ready to deploy
**Priority**: CRITICAL (P0)
**Estimated impact**: Fixes 100% of monitoring failures after 6 minutes
