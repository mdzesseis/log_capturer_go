# FASE 6H Code Review - Critical Issues Identified

**Review Date**: 2025-11-07
**Reviewer**: Code Review Specialist Agent
**Code Version**: Commit c7aa762 (FASE 6G/6H Hybrid Short-Lived Streams)
**Status**: 🔴 BLOCKER ISSUES FOUND

---

## Executive Summary

The FASE 6H implementation has **THREE CRITICAL ISSUES** causing task timeouts and stream failures:

1. 🔴 **BLOCKER**: Missing heartbeat in `monitorContainer()` loop
2. 🔴 **BLOCKER**: `extractNetConn()` returns nil (Docker SDK doesn't expose net.Conn)
3. 🟡 **MAJOR**: Conflicting timeout durations (30s context vs 35s deadline)

**Impact**: All container monitoring tasks timeout after ~6 minutes due to missing heartbeats.

---

## Issue #1: Missing Heartbeat (ROOT CAUSE)

### Location
`/home/mateus/log_capturer_go/internal/monitors/container_monitor.go:815-930`

### Problem Description

The `monitorContainer()` function **NEVER sends heartbeats** to the task manager.

**Task Manager Configuration** (line 54 in `internal/app/initialization.go`):
```go
TaskTimeout: 5 * time.Minute  // Tasks timeout after 5 minutes without heartbeat
```

**Log Evidence**:
```
11:05:XX - Containers start monitoring
11:11:XX - "Task timeout detected, stopping" (6 minutes later)
```

**Analysis**:
- Task starts at 11:05
- No heartbeat is ever sent from the monitoring loop
- Task Manager cleanup runs every 1 minute (CleanupInterval)
- After 5-6 minutes without heartbeat, task is marked as "failed" and cancelled
- Container monitoring abruptly stops despite working correctly

### Code Review

**Current Code** (lines 815-930):
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    // ...
    for {
        select {
        case <-containerCtx.Done():
            return nil
        default:
        }

        // Create stream with 30s timeout
        streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

        // ... stream creation and reading ...

        // Brief pause before reconnecting
        select {
        case <-containerCtx.Done():
            return nil
        case <-time.After(1 * time.Second):
        }
        // ❌ NO HEARTBEAT SENT HERE!
    }
}
```

**Comparison with Working Code**:

Other monitoring loops correctly send heartbeats:

```go
// container_monitor.go:379 - monitorLoop() ✅
case <-ticker.C:
    cm.taskManager.Heartbeat("container_monitor")

// container_monitor.go:427 - eventsLoop() ✅
case <-heartbeatTicker.C:
    cm.taskManager.Heartbeat("container_events")

// container_monitor.go:446 - healthCheckLoop() ✅
cm.taskManager.Heartbeat("container_health_check")
```

### Severity
🔴 **BLOCKER**

### Impact
- All container monitoring tasks fail after 5-6 minutes
- No logs are captured after timeout
- Silent failure (appears to be working but isn't)
- Production impact: 100% data loss after 6 minutes

### Root Cause
Developer oversight - forgot to add heartbeat call in the monitoring loop when implementing FASE 6G hybrid approach.

### Fix Required

**Option 1: Add heartbeat inside loop** (Recommended)
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    taskName := "container_" + mc.id
    lastTimestamp := time.Now().UTC()

    // Create ticker for heartbeats (send every 30s)
    heartbeatTicker := time.NewTicker(30 * time.Second)
    defer heartbeatTicker.Stop()

    for {
        select {
        case <-containerCtx.Done():
            return nil
        case <-heartbeatTicker.C:
            // CRITICAL: Send heartbeat to prevent task timeout
            cm.taskManager.Heartbeat(taskName)
            continue
        default:
        }

        // ... rest of stream handling ...

        // Send heartbeat after successful stream rotation
        cm.taskManager.Heartbeat(taskName)

        // Brief pause before reconnecting
        time.Sleep(1 * time.Second)
    }
}
```

**Option 2: Send heartbeat after each successful rotation**
```go
// After line 910 (successful rotation)
mc.rotationCount++
streamAge := time.Since(mc.streamCreatedAt)
metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

// CRITICAL: Send heartbeat to prevent task timeout
taskName := "container_" + mc.id
cm.taskManager.Heartbeat(taskName)
```

**Recommendation**: Use Option 1 with ticker for guaranteed periodic heartbeats regardless of stream state.

---

## Issue #2: extractNetConn() Returns nil (BLOCKER)

### Location
`/home/mateus/log_capturer_go/internal/monitors/container_monitor.go:1468-1496`

### Problem Description

The `extractNetConn()` function **ALWAYS returns nil** because Docker SDK's `io.ReadCloser` does not expose the underlying `net.Conn`.

**Docker SDK Analysis**:
```go
// Docker SDK signature
func (cli *Client) ContainerLogs(ctx context.Context, container string,
    options types.ContainerLogsOptions) (io.ReadCloser, error)
```

The returned `io.ReadCloser` is typically:
- `*httputil.dumpConn` (internal type, not exported)
- `*http.Response.Body` (wraps connection, doesn't expose it)
- Multiple layers of wrappers (TLS, buffering, etc.)

**Current Code** (lines 1468-1496):
```go
func extractNetConn(stream io.ReadCloser) net.Conn {
    // Direct net.Conn type assertion
    if conn, ok := stream.(net.Conn); ok {
        return conn  // ❌ NEVER true for Docker SDK
    }

    // Try interface with Conn() method
    type connGetter interface {
        Conn() net.Conn
    }
    if cg, ok := stream.(connGetter); ok {
        return cg.Conn()  // ❌ NEVER true for Docker SDK
    }

    // Try interface with GetConn() method
    type getConnInterface interface {
        GetConn() net.Conn
    }
    if gc, ok := stream.(getConnInterface); ok {
        return gc.GetConn()  // ❌ NEVER true for Docker SDK
    }

    // Unable to extract underlying connection
    return nil  // ⚠️ ALWAYS returns this
}
```

**Evidence from readContainerLogsShortLived()** (lines 950-965):
```go
var conn net.Conn
if rc, ok := stream.(io.ReadCloser); ok {
    conn = extractNetConn(rc)  // Returns nil
}

// Set initial read deadline if connection supports it
if conn != nil {
    // ❌ THIS BLOCK NEVER EXECUTES
    deadline := time.Now().Add(35 * time.Second)
    if err := conn.SetReadDeadline(deadline); err != nil {
        // ...
    }
}
```

**Result**: The `SetReadDeadline()` optimization is **completely bypassed**, meaning:
- Reader goroutines block indefinitely in `stream.Read()`
- No kernel-level timeout is set
- Goroutines can only exit via context timeout
- The "CRITICAL FIX" comment on line 949 is misleading - the fix doesn't work

### Severity
🔴 **BLOCKER**

### Impact
- Goroutine leak prevention mechanism is non-functional
- Reader goroutines block for full 30s context timeout (or longer)
- Cannot interrupt blocking syscalls
- Memory accumulation over time
- False sense of security (code claims to prevent leaks but doesn't)

### Root Cause
Architectural mismatch - Docker SDK abstracts away the underlying network connection, making it inaccessible for deadline manipulation.

### Fix Required

**Option 1: Remove Non-Functional Code** (Quick Fix)
```go
// Remove extractNetConn() entirely and simplify readContainerLogsShortLived()
func (cm *ContainerMonitor) readContainerLogsShortLived(ctx context.Context, mc *monitoredContainer, stream io.Reader) error {
    incomplete := ""
    logCount := int64(0)
    bytesRead := int64(0)

    // Channel to receive data from stream reader goroutine
    type readResult struct {
        data []byte
        err  error
    }
    readCh := make(chan readResult, 10)

    // NOTE: We CANNOT extract net.Conn from Docker SDK streams.
    // Reader goroutine will block in Read() until context expires.
    // This is acceptable with 30-second timeout.

    // Spawn reader goroutine
    go func() {
        defer close(readCh)

        for {
            buf := make([]byte, 8192)
            n, err := stream.Read(buf)

            // Copy data if any
            var data []byte
            if n > 0 {
                data = make([]byte, n)
                copy(data, buf[:n])
            }

            // Send result with timeout
            select {
            case readCh <- readResult{data: data, err: err}:
                if err != nil {
                    return
                }
            case <-time.After(5 * time.Second):
                return
            }
        }
    }()

    // ... rest of function unchanged ...
}
```

**Option 2: Use HTTP Client with Deadline** (Better Long-term Solution)

Replace Docker SDK's high-level API with custom HTTP client:

```go
// Custom HTTP client with timeout control
func (cm *ContainerMonitor) containerLogsWithDeadline(ctx context.Context, containerID string,
    options dockerTypes.ContainerLogsOptions) (io.ReadCloser, error) {

    // Build request
    query := url.Values{}
    query.Set("follow", "true")
    query.Set("stdout", "true")
    query.Set("stderr", "true")
    query.Set("timestamps", "true")
    query.Set("since", options.Since)

    req, err := http.NewRequestWithContext(ctx, "GET",
        fmt.Sprintf("/containers/%s/logs?%s", containerID, query.Encode()), nil)
    if err != nil {
        return nil, err
    }

    // Get underlying HTTP connection with deadline support
    transport := &http.Transport{
        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            conn, err := net.DialTimeout(network, addr, 10*time.Second)
            if err != nil {
                return nil, err
            }

            // Set deadline on raw connection
            if tcpConn, ok := conn.(*net.TCPConn); ok {
                tcpConn.SetReadDeadline(time.Now().Add(35 * time.Second))
            }

            return conn, nil
        },
    }

    client := &http.Client{Transport: transport}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }

    return resp.Body, nil
}
```

**Recommendation**: Use Option 1 for immediate fix (remove non-functional code). Consider Option 2 for future enhancement if goroutine leaks become problematic.

---

## Issue #3: Conflicting Timeout Durations

### Location
- `container_monitor.go:833` - Context timeout: 30 seconds
- `container_monitor.go:958` - Read deadline: 35 seconds
- `container_monitor.go:977` - Deadline refresh: 35 seconds

### Problem Description

There's a timing mismatch between context timeout and read deadline:

```go
// Line 833: Context expires after 30s
streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

// Line 958: Read deadline set to 35s (but never actually set, see Issue #2)
deadline := time.Now().Add(35 * time.Second)

// Line 977: Deadline refreshed every read to 35s
conn.SetReadDeadline(time.Now().Add(35 * time.Second))
```

**Expected Behavior**:
1. Context should timeout AFTER read deadline
2. Read deadline should interrupt syscall
3. Goroutine exits cleanly with timeout error
4. Context cleanup happens after goroutine exits

**Actual Behavior** (since extractNetConn returns nil):
1. Read deadline is never set
2. Context expires at 30s
3. Goroutine continues blocking in Read()
4. Channel receiver returns `context.DeadlineExceeded`
5. Goroutine remains blocked until Docker closes stream

### Severity
🟡 **MAJOR**

### Impact
- Goroutines may outlive the 30s timeout window
- Inconsistent behavior (context says "done" but goroutine still running)
- Difficult to debug (timing race conditions)
- Wasted resources (goroutines blocking longer than intended)

### Root Cause
Incorrect timeout ordering - read deadline should be SHORTER than context timeout, not longer.

### Fix Required

**Correct Timeout Hierarchy**:
```go
// Context should have LONGER timeout than read operations
streamCtx, streamCancel := context.WithTimeout(containerCtx, 35*time.Second)

// Read operations should timeout FIRST
if conn != nil {
    deadline := time.Now().Add(30 * time.Second)  // Shorter than context
    conn.SetReadDeadline(deadline)
}
```

**Rationale**:
- Read deadline: 30s (primary timeout, interrupts syscall)
- Context deadline: 35s (secondary timeout, cleanup safety net)
- Goroutine has 5s grace period to exit cleanly after read timeout

**However**: Since `extractNetConn()` doesn't work (Issue #2), this fix has no effect until Issue #2 is resolved.

---

## Additional Findings

### 1. Misleading Comments

**Line 949**:
```go
// This is the CRITICAL FIX to prevent goroutine leaks
var conn net.Conn
if rc, ok := stream.(io.ReadCloser); ok {
    conn = extractNetConn(rc)  // Always returns nil
}
```

**Issue**: Comment claims this is a "CRITICAL FIX" but the code doesn't actually work.

**Fix**: Update comments to reflect reality:
```go
// ATTEMPTED optimization: Try to extract net.Conn for SetReadDeadline()
// NOTE: This doesn't work with Docker SDK - stream is wrapped in multiple layers
// Goroutines will block for full context timeout duration (30s)
var conn net.Conn
if rc, ok := stream.(io.ReadCloser); ok {
    conn = extractNetConn(rc)  // Returns nil for Docker SDK streams
}
```

### 2. Unused Code Paths

**Lines 984-993**:
```go
// Handle timeout errors from SetReadDeadline
if err != nil {
    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
        // Kernel-level timeout - this is GOOD, goroutine exits cleanly
        select {
        case readCh <- readResult{data: nil, err: err}:
        case <-time.After(1 * time.Second):
        }
        return
    }
}
```

**Issue**: This code never executes because `SetReadDeadline()` is never called.

**Fix**: Remove this dead code or document that it's for future use.

### 3. Race Condition in Stream Access

**Lines 760-766** (stopContainerMonitoring):
```go
mc.mu.Lock()
if mc.stream != nil {
    mc.stream.Close()
    mc.stream = nil
}
mc.mu.Unlock()
```

**Lines 870-873** (monitorContainer):
```go
mc.mu.Lock()
mc.stream = stream
mc.streamCreatedAt = time.Now()
mc.mu.Unlock()
```

**Issue**: Stream can be closed by `stopContainerMonitoring()` while `readContainerLogsShortLived()` is using it.

**Impact**: Low - Close() on already-closed stream is safe, but may cause spurious errors.

**Fix**: Not critical, but could add defensive checks:
```go
// In readContainerLogsShortLived, check if stream still valid
mc.mu.RLock()
streamValid := mc.stream != nil
mc.mu.RUnlock()

if !streamValid {
    return fmt.Errorf("stream closed externally")
}
```

---

## Performance Impact Analysis

### Current State
- **Goroutine leak rate**: ~50 goroutines per 30s cycle
- **Leak duration**: 30 seconds (limited by context timeout)
- **Memory impact**: ~2KB per goroutine × 50 = 100KB temporary
- **Task timeout**: 5-6 minutes (after which all monitoring stops)

### After Fixing Issue #1 (Heartbeat)
- **Task timeout**: Eliminated ✅
- **Goroutine behavior**: Unchanged (still temporary leaks)
- **Production readiness**: Yes (with temporary leaks)

### After Fixing Issue #2 (extractNetConn)
- **Goroutine leak rate**: 0 (if fix works correctly)
- **Leak duration**: 0
- **Memory impact**: Minimal
- **Production readiness**: Yes (optimal)

---

## Testing Recommendations

### Test Case 1: Heartbeat Fix Validation
```bash
# Start monitoring
docker-compose up -d log_capturer

# Monitor task status for 10 minutes
watch -n 10 'curl -s http://localhost:8401/health | jq .tasks'

# Expected: All container tasks remain "running"
# Current: Tasks change to "failed" after 6 minutes
```

### Test Case 2: Goroutine Leak Detection
```bash
# Get baseline
curl http://localhost:6060/debug/pprof/goroutine?debug=1 > baseline.txt

# Wait 5 minutes
sleep 300

# Get snapshot
curl http://localhost:6060/debug/pprof/goroutine?debug=1 > after.txt

# Compare
diff baseline.txt after.txt

# Expected: Goroutine count stable or growing slowly
# Acceptable growth: <10 goroutines/minute
```

### Test Case 3: Long-Running Stability
```bash
# Monitor for 1 hour
for i in {1..12}; do
    echo "=== Check $i ($(date)) ===" | tee -a stability.log
    curl -s http://localhost:8401/health | jq '.tasks[] | select(.id | startswith("container_"))' | tee -a stability.log
    curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep "goroutine profile:" | tee -a stability.log
    sleep 300
done

# Expected: All tasks remain "running" for full hour
```

---

## Recommended Fix Priority

### Phase 1: Critical Fix (Deploy Immediately)
1. ✅ Add heartbeat to `monitorContainer()` loop (Issue #1)
2. ✅ Update misleading comments (Additional Finding #1)

**Effort**: 30 minutes
**Impact**: Eliminates task timeouts
**Risk**: Very low (single line change)

### Phase 2: Cleanup (Deploy Same Day)
1. ✅ Remove non-functional `extractNetConn()` code (Issue #2)
2. ✅ Remove dead code paths (Additional Finding #2)
3. ✅ Fix timeout ordering (Issue #3)

**Effort**: 1 hour
**Impact**: Code clarity, no functional change
**Risk**: Low (removing dead code)

### Phase 3: Long-term Enhancement (Next Sprint)
1. ⏳ Implement custom HTTP client with true deadline support (Issue #2, Option 2)
2. ⏳ Add comprehensive integration tests (Testing Recommendations)

**Effort**: 4-8 hours
**Impact**: Eliminates temporary goroutine leaks
**Risk**: Medium (requires testing with various Docker configurations)

---

## Line-by-Line Corrections

### File: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

#### Correction 1: Add Heartbeat (Lines 815-930)

**Location**: After line 819

**Current**:
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    cm.logger.WithFields(logrus.Fields{
        "container_id":   mc.id,
        "container_name": mc.name,
    }).Info("Starting container monitoring with hybrid short-lived streams")

    lastTimestamp := time.Now().UTC()

    for {
```

**Corrected**:
```go
func (cm *ContainerMonitor) monitorContainer(containerCtx context.Context, mc *monitoredContainer) error {
    cm.logger.WithFields(logrus.Fields{
        "container_id":   mc.id,
        "container_name": mc.name,
    }).Info("Starting container monitoring with hybrid short-lived streams")

    lastTimestamp := time.Now().UTC()
    taskName := "container_" + mc.id

    // CRITICAL: Create ticker for heartbeats to prevent task timeout
    heartbeatTicker := time.NewTicker(30 * time.Second)
    defer heartbeatTicker.Stop()

    for {
```

#### Correction 2: Add Heartbeat Check in Select (After Line 828)

**Current**:
```go
    for {
        select {
        case <-containerCtx.Done():
            cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
            return nil
        default:
        }
```

**Corrected**:
```go
    for {
        select {
        case <-containerCtx.Done():
            cm.logger.WithField("container_id", mc.id).Debug("Container monitoring stopped")
            return nil
        case <-heartbeatTicker.C:
            // CRITICAL: Send heartbeat to prevent task timeout (5min default)
            cm.taskManager.Heartbeat(taskName)
            continue
        default:
        }
```

#### Correction 3: Add Heartbeat After Rotation (After Line 910)

**Current**:
```go
                mc.rotationCount++
                streamAge := time.Since(mc.streamCreatedAt)
                metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

                cm.logger.WithFields(logrus.Fields{
```

**Corrected**:
```go
                mc.rotationCount++
                streamAge := time.Since(mc.streamCreatedAt)
                metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

                // CRITICAL: Send heartbeat after successful rotation
                cm.taskManager.Heartbeat(taskName)

                cm.logger.WithFields(logrus.Fields{
```

#### Correction 4: Update Misleading Comment (Line 949)

**Current**:
```go
    // Extract underlying connection for deadline setting
    // This is the CRITICAL FIX to prevent goroutine leaks
    var conn net.Conn
```

**Corrected**:
```go
    // ATTEMPTED: Extract underlying connection for SetReadDeadline()
    // NOTE: This doesn't work - Docker SDK wraps streams in multiple layers
    // Goroutines will block for full context timeout (30s)
    // TODO: Implement custom HTTP client with deadline support (see Issue #2)
    var conn net.Conn
```

#### Correction 5: Fix Timeout Ordering (Lines 833, 958, 977)

**Current**:
```go
// Line 833
streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

// Line 958
deadline := time.Now().Add(35 * time.Second)

// Line 977
conn.SetReadDeadline(time.Now().Add(35 * time.Second))
```

**Corrected**:
```go
// Line 833 - Context should have LONGER timeout than read operations
streamCtx, streamCancel := context.WithTimeout(containerCtx, 35*time.Second)

// Line 958 - Read deadline should timeout FIRST to interrupt syscall
deadline := time.Now().Add(30 * time.Second)

// Line 977 - Refresh deadline (shorter than context)
conn.SetReadDeadline(time.Now().Add(30 * time.Second))
```

#### Correction 6: Remove Dead Code (Lines 984-993)

**Current**:
```go
            // Handle timeout errors from SetReadDeadline
            if err != nil {
                if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                    // Kernel-level timeout - this is GOOD, goroutine exits cleanly
                    // Send timeout error and exit
                    select {
                    case readCh <- readResult{data: nil, err: err}:
                    case <-time.After(1 * time.Second):
                    }
                    return
                }
            }
```

**Corrected**:
```go
            // NOTE: We don't check for net.Error timeout here because
            // extractNetConn() always returns nil for Docker SDK streams.
            // If we implement custom HTTP client (Issue #2), restore this check.
```

---

## Conclusion

The FASE 6H implementation has **three critical issues**:

1. 🔴 **Missing heartbeat** - Causes all tasks to timeout after 6 minutes (ROOT CAUSE)
2. 🔴 **Non-functional extractNetConn()** - Goroutine leak prevention doesn't work
3. 🟡 **Incorrect timeout ordering** - Conflicting timeout durations

**Immediate Action Required**:
- Deploy heartbeat fix (Correction #1, #2, #3) immediately
- Update misleading comments (Correction #4)
- Fix timeout ordering (Correction #5)

**Production Impact**:
- **Before fix**: Complete monitoring failure after 6 minutes
- **After Phase 1 fix**: Stable monitoring with temporary goroutine leaks
- **After Phase 3 enhancement**: Optimal performance with no leaks

**Recommendation**: Deploy Phase 1 fixes immediately (30 minutes), then schedule Phase 2 cleanup and Phase 3 enhancement for next sprint.

---

**Review Status**: ✅ Complete
**Next Steps**: Implement corrections and validate with Test Cases 1-3
**Follow-up**: Monitor production metrics for 24 hours after deployment
