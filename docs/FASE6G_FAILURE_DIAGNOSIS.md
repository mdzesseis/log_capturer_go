# FASE 6G Failure Diagnosis

**Date**: 2025-11-07
**Test Duration**: 30 minutes
**Containers**: 55
**Result**: FAILED (+34.76 gor/min leak)

---

## Executive Summary

FASE 6G failed due to **two critical bugs introduced during refactoring**:

1. **Missing `metrics.RecordStreamRotation()` call** - Explains why rotations show as 0
2. **Blocking `stream.Read()` with ineffective timeout** - Explains why goroutine leak persists

The implementation attempted to sacrifice goroutines for simplicity, but **the 30s timeout doesn't actually interrupt kernel syscalls**, resulting in unbounded goroutine accumulation.

---

## 1. Root Cause Analysis

### 1.1 Missing Stream Rotation Metrics

**Location**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go:815-918`

**What Happened**:
During the FASE 6G refactoring (commit `c7aa762`), the code that recorded stream rotations was **accidentally deleted**:

```go
// FASE 6F (commit a5de596) - HAD rotation tracking
if readErr == context.DeadlineExceeded {
    // Planned rotation
    mc.rotationCount++
    metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

    cm.logger.WithFields(logrus.Fields{
        "container_id":     mc.id,
        "container_name":   mc.name,
        "rotation_count":   mc.rotationCount,
        "stream_age_secs":  int(streamAge.Seconds()),
    }).Debug("Stream rotated successfully")
}
```

```go
// FASE 6G (commit c7aa762) - MISSING rotation tracking
if readErr != nil {
    if readErr == context.DeadlineExceeded {
        // Expected timeout - this is NORMAL for short-lived streams
        cm.logger.WithFields(logrus.Fields{
            "container_id": mc.id,
            "timeout":      "30s",
        }).Debug("Stream timeout reached, reconnecting")
        // ⚠️ NO RecordStreamRotation() CALL!
        // ⚠️ NO mc.rotationCount++ INCREMENT!
    }
}
```

**Impact**:
- Prometheus metric `log_capturer_stream_rotations_total` never increments
- Test monitoring script reports "Total Rotations: 0" throughout entire test
- Impossible to verify if rotations are actually happening
- Expected ~3,080 rotations (55 containers × 2/min × 28 min) but recorded 0

**Evidence from Test Log**:
```
Total Rotations: 0
  ⚠️ WARNING: No rotations yet! (Expected ~3080)
```

### 1.2 Goroutine Leak Despite 30s Timeout

**Location**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go:920-1064`

**Critical Misconception**:
The implementation assumes that `context.WithTimeout()` will automatically kill blocked goroutines after 30 seconds:

```go
// Line 833-834: Create context with timeout
streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

// Line 878: Call read function
readErr := cm.readContainerLogsShortLived(streamCtx, mc, stream)

// Line 884-885: Cleanup
stream.Close()
streamCancel()
```

**The Reality**:

The reader goroutine (lines 940-965) spawns with this code:

```go
go func() {
    defer close(readCh)

    for {
        buf := make([]byte, 8192)
        n, err := stream.Read(buf) // ⚠️ BLOCKS IN KERNEL SYSCALL

        // ... attempt to send result
    }
}()
```

**Why the Timeout Doesn't Work**:

1. **Kernel-Level Blocking**:
   - `stream.Read()` translates to kernel syscall (e.g., `read(2)`, `recvfrom(2)`)
   - Goroutine scheduler CANNOT preempt goroutines blocked in syscalls
   - The goroutine is literally suspended at the OS level, not in Go runtime

2. **Context Only Affects Select Statements**:
   ```go
   // Main loop checks context (line 970)
   select {
   case <-ctx.Done():
       return ctx.Err()  // ✅ This works
   case result := <-readCh:
       // Process...
   }
   ```

   BUT the reader goroutine (line 945) doesn't check context:
   ```go
   n, err := stream.Read(buf)  // ❌ No context check!
   ```

3. **Stream.Close() Doesn't Help**:
   - Line 884: `stream.Close()` is called
   - Docker SDK's `stream.Close()` only closes the HTTP response body
   - If goroutine is blocked in `Read()`, closing the stream **may or may not** interrupt it
   - Depends on TCP stack, kernel buffering, and timing

**Observed Behavior**:

Test results show **consistent goroutine growth**:
```
Baseline:      1454 goroutines
After 30 min:  2497 goroutines
Growth:        +1043 goroutines
Rate:          +34.76 gor/min
```

**Math Check**:
```
55 containers × 2 reconnections/min = 110 reconnections/min
If each reconnection leaks 1 goroutine temporarily:
  Expected leak rate IF timeouts worked: 0 gor/min (they expire)
  Actual leak rate: 34.76 gor/min

Conclusion: Goroutines are NOT expiring after 30s
```

---

## 2. Why Rotations Show as 0

### Analysis of `monitorContainer()` Function

**Current Code Flow** (lines 815-918):

```go
for {
    // 1. Create 30s timeout context
    streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)

    // 2. Create Docker log stream
    stream, err := cm.dockerPool.ContainerLogs(streamCtx, mc.id, logOptions)

    // 3. Store stream reference
    mc.mu.Lock()
    mc.stream = stream
    mc.streamCreatedAt = time.Now()
    mc.mu.Unlock()

    // 4. Read from stream (may timeout)
    readErr := cm.readContainerLogsShortLived(streamCtx, mc, stream)

    // 5. Update timestamp
    lastTimestamp = time.Now().UTC()

    // 6. Cleanup
    stream.Close()
    streamCancel()

    // 7. Handle errors - BUT NO ROTATION TRACKING!
    if readErr != nil {
        if readErr == context.DeadlineExceeded {
            cm.logger.Debug("Stream timeout reached, reconnecting")
            // ⚠️ MISSING: metrics.RecordStreamRotation()
            // ⚠️ MISSING: mc.rotationCount++
        }
    }

    // 8. Pause before reconnecting
    time.Sleep(1 * time.Second)
}
```

**What's Missing**:

After line 893 (where timeout is detected), should be:

```go
if readErr == context.DeadlineExceeded {
    // Calculate stream age
    streamAge := time.Since(mc.streamCreatedAt)

    // Record rotation
    mc.rotationCount++
    metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())

    cm.logger.WithFields(logrus.Fields{
        "container_id":     mc.id,
        "rotation_count":   mc.rotationCount,
        "stream_age_secs":  int(streamAge.Seconds()),
    }).Debug("Stream rotated successfully")
}
```

**Git History**:

```bash
$ git diff a5de596..c7aa762 -- internal/monitors/container_monitor.go
```

Shows that the entire rotation tracking block was deleted during refactoring.

**Why This Matters**:

1. **Observability**: Cannot verify rotations are happening
2. **Metrics**: Prometheus dashboards show 0 rotations
3. **Debugging**: No way to distinguish between:
   - Rotations not happening (bug in timeout logic)
   - Rotations happening but not recorded (metric bug)

---

## 3. Why Goroutine Leak Persists

### The Fundamental Problem

**Docker SDK Stream Behavior**:

```go
type ReadCloser interface {
    Read(p []byte) (n int, err error)  // May block indefinitely
    Close() error                       // Best-effort, doesn't interrupt Read()
}
```

**Go's Syscall Behavior**:

When `stream.Read()` is called:
1. Go runtime calls `syscall.Read()` → enters kernel mode
2. Kernel waits for data on socket
3. **Goroutine is suspended at OS level**
4. Go scheduler cannot preempt it (not in Go code anymore)

**Context Cancellation Limitation**:

```go
ctx, cancel := context.WithTimeout(parent, 30*time.Second)

// After 30 seconds:
// - ctx.Done() channel closes ✅
// - Main select loop exits ✅
// - BUT: Goroutine in Read() stays blocked ❌
```

### Detailed Trace of Goroutine Lifecycle

**Timeline for 1 Container**:

```
T+0s:     Create stream, spawn reader goroutine
          Goroutine count: +1

T+0-30s:  Reader goroutine may:
          - Read data successfully (exits normally)
          - Block in Read() waiting for data

T+30s:    Context timeout expires
          - Main loop exits
          - stream.Close() called
          - streamCancel() called

          IF goroutine is blocked in Read():
            - Close() might interrupt it (unreliable)
            - Context cancellation has NO EFFECT
            - Goroutine stays alive

T+31s:    Create NEW stream, spawn NEW reader goroutine
          Goroutine count: +2 (old one still alive)

T+60s:    Another timeout...
          Goroutine count: +3

RESULT:   Unbounded accumulation
```

**Observed Leak Rate**:

```
Test: 55 containers, 30 minutes
Leak: +34.76 gor/min

Expected rotations: 55 × 2/min = 110 rotations/min
Leak per rotation: 34.76 / 110 = 0.316 (31.6% of rotations leak)

Interpretation:
- ~31% of stream.Read() calls don't get interrupted by Close()
- These goroutines remain blocked indefinitely
- Accumulate at ~35 gor/min
```

### Code Evidence

**Reader Goroutine** (line 940-965):

```go
go func() {
    defer close(readCh)

    for {
        buf := make([]byte, 8192)
        n, err := stream.Read(buf)  // ⚠️ BLOCKING POINT

        // ⚠️ NO context check here!
        // ⚠️ NO timeout on Read() itself!

        // Try to send (has 5s timeout)
        select {
        case readCh <- readResult{data: data, err: err}:
            if err != nil {
                return  // Only exits on Read() error
            }
        case <-time.After(5 * time.Second):
            return  // Exits if channel blocked
        }
    }
}()
```

**Main Loop** (line 968-1063):

```go
for {
    select {
    case <-ctx.Done():
        // Context expired (30s timeout)
        return ctx.Err()  // ⚠️ Exits, but reader goroutine still alive!

    case result, ok := <-readCh:
        // Process data...
    }
}
```

**The Gap**:
- Main loop exits when `ctx.Done()` fires (line 970-973)
- Reader goroutine (line 940) has **no way to know** context expired
- Reader stays blocked in `stream.Read()` until:
  - Data arrives (may never happen for idle containers)
  - TCP connection closes (unreliable, depends on Docker daemon)
  - Stream is closed (already tried, doesn't work reliably)

---

## 4. Comparison with Intended Design

### Design Intent (from FASE_6G_KEY_CHANGES.md)

**Goal**:
> "Accept temporary goroutine leaks, but ensure they expire after 30s maximum"

**Expected Behavior**:
```
Leak accumulation:
- Containers monitored: 50
- Reconnection interval: 30s
- Goroutines per container: 1 (may leak)
- Maximum leaked: 50 goroutines
- Leak lifetime: ≤30 seconds

Stable state:
- Active goroutines: ~500 (application baseline)
- Leaked goroutines: ≤50 (temporary)
- Total: ~550 goroutines
- Growth rate: 0 gor/min ✅
```

### Actual Behavior (from test results)

```
Leak accumulation:
- Containers monitored: 55
- Reconnection interval: 30s
- Goroutines per container: ~0.63/min (leak rate)
- Leaked goroutines: UNBOUNDED (don't expire)
- Leak lifetime: INDEFINITE

Actual state:
- Active goroutines: ~1454 (baseline)
- Leaked goroutines: +1043 (after 30 min)
- Total: 2497 goroutines
- Growth rate: +34.76 gor/min ❌
```

### Why the Design Failed

1. **Incorrect Assumption**: Context timeout would kill blocked goroutines
   - **Reality**: Context only affects select statements

2. **Incorrect Assumption**: stream.Close() would interrupt Read()
   - **Reality**: Close() doesn't interrupt kernel syscalls

3. **Missing Instrumentation**: Deleted metrics during refactoring
   - **Impact**: Cannot observe actual behavior

4. **No Actual Fix**: Simplified code without solving core issue
   - **Result**: Same leak, simpler code, no observability

---

## 5. Active Streams Showing 0

### Metric Implementation

**Location**: `/home/mateus/log_capturer_go/internal/metrics/metrics.go:805-807`

```go
// UpdateActiveStreams updates the count of active streams
func UpdateActiveStreams(count int) {
    ActiveContainerStreams.Set(float64(count))
}
```

**Usage Pattern**:

Should be called:
1. When stream is created: `metrics.UpdateActiveStreams(+1)`
2. When stream is closed: `metrics.UpdateActiveStreams(-1)`

**Search Results**:

```bash
$ grep -n "UpdateActiveStreams" internal/monitors/container_monitor.go
(no results)
```

**Conclusion**:
- `UpdateActiveStreams()` is **never called** in container monitor
- Metric always shows 0, regardless of actual stream count
- Another missing instrumentation from refactoring

**Expected Value**:
- Should show ~55 (one stream per container)
- Or ~0-110 during rotation (old stream closing, new stream opening)

**Actual Value**:
- Always shows 0 (never updated)

---

## 6. File Descriptor Leak

### Test Results

```
Baseline FD:    656
Final FD:       1241
Growth:         +585 FD
```

**Analysis**:

```
Expected FD usage:
- 55 containers × 1 stream = 55 FD (active streams)
- Docker event stream: 1 FD
- HTTP listeners: ~10 FD
- Total expected: ~70 FD

Actual FD growth: +585 FD
```

**Correlation with Goroutine Leak**:

```
Goroutine leak:  +1043 goroutines
FD leak:         +585 FD
Ratio:           1.78 goroutines per FD

Hypothesis:
- Each leaked goroutine holds a file descriptor
- FD is for the Docker log stream HTTP connection
- stream.Close() called, but FD not released because:
  - Goroutine still holding reference
  - TCP connection in kernel still open
```

**Evidence**:

If we assume each rotation leaks ~31% of the time:
```
Rotations: 55 containers × 2/min × 30 min = 3300 rotations
Leaked: 3300 × 0.31 = 1023 leaked streams
Expected FD leak: 1023 FD

Actual leak: 585 FD
Difference: Some FDs eventually released (TCP timeout, GC, etc.)
```

---

## 7. Technical Deep Dive: Why stream.Close() Fails

### Docker SDK Stream Implementation

**Source**: `github.com/docker/docker/client/request.go`

```go
type serverResponse struct {
    body       io.ReadCloser
    header     http.Header
    statusCode int
    reqURL     *url.URL
}

func (cli *Client) ContainerLogs(...) (io.ReadCloser, error) {
    resp, err := cli.get(ctx, "/containers/"+containerID+"/logs", query, headers)
    return resp.body, err
}
```

The returned `io.ReadCloser` is an HTTP response body.

### HTTP Response Body Behavior

**Go stdlib**: `net/http/response.go`

```go
type body struct {
    src          io.Reader
    hdr          interface{}
    r            *bufio.Reader
    closing      bool
    doEarlyClose bool
    mu           sync.Mutex
    sawEOF       bool
    closed       bool
    earlyClose   bool
    onHitEOF     func()
}

func (b *body) Close() error {
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.closed {
        return nil
    }
    b.closed = true

    // ⚠️ Closes connection, but doesn't interrupt pending Read()
    return b.src.Close()
}
```

**Key Point**:
- `Close()` sets `closed = true` flag
- Future `Read()` calls will return error
- BUT: If goroutine **already blocked** in `Read()`, it won't see the flag
- Goroutine suspended in kernel, can't check Go-level flags

### Kernel-Level Blocking

**Linux syscall**: `read(2)`

```c
ssize_t read(int fd, void *buf, size_t count);
```

**Behavior**:
1. Userspace calls `read(fd, buf, count)`
2. Kernel checks if data available in socket buffer
3. If no data: **process sleeps** (interruptible or uninterruptible)
4. Wakes up when:
   - Data arrives
   - Socket closed
   - Signal received (EINTR)
   - Timeout (if set with SO_RCVTIMEO)

**Go's Goroutine**:
- Go runtime cannot preempt goroutine in syscall
- Goroutine is at mercy of kernel
- Only kernel can wake it up

**Why Close() Sometimes Works**:
- If goroutine hasn't entered syscall yet (timing)
- If TCP connection closed from remote side
- If kernel buffers data causing immediate return
- **But not guaranteed if already blocked**

---

## 8. Recommended Fix Approach

### Option 1: SetReadDeadline (RECOMMENDED)

**Concept**: Set deadline on underlying net.Conn before Read()

```go
// Get underlying TCP connection
if tcpConn, ok := stream.(interface{ SetReadDeadline(time.Time) error }); ok {
    // Set deadline to match context timeout
    deadline := time.Now().Add(30 * time.Second)
    tcpConn.SetReadDeadline(deadline)
}

// Now Read() will return error after 30s, even if blocked in kernel
n, err := stream.Read(buf)
```

**Advantages**:
- Kernel-level timeout (actually interrupts syscall)
- Reliable goroutine cleanup
- No leaked goroutines
- No leaked file descriptors

**Challenges**:
- Docker SDK's `io.ReadCloser` may not expose `net.Conn`
- Need to unwrap response body to access connection
- May require type assertions and reflection

### Option 2: Periodically Kill and Recreate Streams

**Concept**: Don't wait for timeout, proactively rotate streams

```go
// Every 30 seconds:
1. Create NEW stream
2. Read from NEW stream
3. Close OLD stream
4. Wait 5s for old goroutine to exit
5. If still alive, abandon it (accept leak)
```

**Advantages**:
- Simple to implement
- Guaranteed stream rotation
- Measurable metrics

**Challenges**:
- Still leaks goroutines (just slower)
- More complex lifecycle management
- Potential log duplication or loss during rotation

### Option 3: Single Long-Lived Stream with Read Timeout

**Concept**: Use single stream with `io.ReadCloser` wrapper that implements timeout

```go
type timeoutReader struct {
    r       io.Reader
    timeout time.Duration
}

func (tr *timeoutReader) Read(p []byte) (n int, err error) {
    type result struct {
        n   int
        err error
    }

    ch := make(chan result, 1)
    go func() {
        n, err := tr.r.Read(p)
        ch <- result{n, err}
    }()

    select {
    case res := <-ch:
        return res.n, res.err
    case <-time.After(tr.timeout):
        // ⚠️ Still leaks goroutine!
        return 0, ErrTimeout
    }
}
```

**Advantages**:
- Timeout per Read() call
- Can implement retry logic

**Challenges**:
- Still leaks goroutines on timeout
- More complexity
- Performance overhead (goroutine per Read)

### Option 4: Accept Leak, Add Cleanup

**Concept**: Periodically detect and kill leaked goroutines

```go
// Every 5 minutes:
1. Enumerate all goroutines (runtime.Stack)
2. Find goroutines blocked in stream.Read()
3. Force-close their underlying connections
4. Clean up resources
```

**Advantages**:
- Can recover from leaks
- Provides observability

**Challenges**:
- Cannot reliably identify goroutines by stack trace
- No way to force-kill goroutine in Go
- Hacky and unreliable

### Recommendation: Option 1 + Metrics Fix

**Implementation Plan**:

1. **Fix metrics** (immediate, low-risk):
   ```go
   // In monitorContainer(), after line 893:
   if readErr == context.DeadlineExceeded {
       streamAge := time.Since(mc.streamCreatedAt)
       mc.rotationCount++
       metrics.RecordStreamRotation(mc.id, mc.name, streamAge.Seconds())
   }

   // After line 872 (stream created):
   metrics.UpdateActiveStreams(cm.getActiveStreamCount())

   // After line 884 (stream closed):
   metrics.UpdateActiveStreams(cm.getActiveStreamCount())
   ```

2. **Implement SetReadDeadline** (core fix):
   ```go
   // In readContainerLogsShortLived(), line 940:
   go func() {
       defer close(readCh)

       // Set read deadline before each Read()
       if deadline, ok := getDeadlineFromContext(ctx); ok {
           if conn := getUnderlyingConn(stream); conn != nil {
               conn.SetReadDeadline(deadline)
           }
       }

       for {
           buf := make([]byte, 8192)
           n, err := stream.Read(buf)

           // If deadline exceeded, Read() returns error
           // Goroutine exits cleanly

           // ... rest of logic
       }
   }()
   ```

3. **Add goroutine tracking** (observability):
   ```go
   // Track reader goroutines
   var readerGoroutines sync.Map // key: containerID, value: count

   // Before spawning:
   count, _ := readerGoroutines.LoadOrStore(mc.id, 0)
   readerGoroutines.Store(mc.id, count.(int)+1)
   metrics.RecordReaderGoroutines(mc.id, count.(int)+1)

   // In defer:
   count, _ := readerGoroutines.Load(mc.id)
   readerGoroutines.Store(mc.id, count.(int)-1)
   metrics.RecordReaderGoroutines(mc.id, count.(int)-1)
   ```

4. **Add integration test** (validation):
   ```go
   func TestStreamRotation_NoGoroutineLeak(t *testing.T) {
       baseline := runtime.NumGoroutine()

       // Run monitor for 2 minutes
       // Expect 4 rotations (30s each)

       time.Sleep(2 * time.Minute)

       final := runtime.NumGoroutine()
       leaked := final - baseline

       // Allow ±10 goroutines fluctuation
       assert.Less(t, leaked, 10, "Goroutine leak detected")
   }
   ```

---

## 9. Conclusion

### Summary of Findings

| Issue | Root Cause | Impact |
|-------|------------|--------|
| **Rotations = 0** | `metrics.RecordStreamRotation()` call deleted | Cannot verify rotations happening |
| **Active Streams = 0** | `metrics.UpdateActiveStreams()` never called | No visibility into stream state |
| **Goroutine Leak** | `stream.Read()` blocks in kernel, unreachable by context | +34.76 gor/min accumulation |
| **FD Leak** | Leaked goroutines hold stream file descriptors | +585 FD in 30 min |

### Why FASE 6G Failed

1. **Incorrect Assumption**: Context timeout would kill blocked goroutines
   - **Reality**: Context only affects select statements

2. **Incorrect Assumption**: stream.Close() would interrupt Read()
   - **Reality**: Close() doesn't interrupt kernel syscalls

3. **Missing Instrumentation**: Deleted metrics during refactoring
   - **Impact**: Cannot observe actual behavior

4. **No Actual Fix**: Simplified code without solving core issue
   - **Result**: Same leak, simpler code, no observability

### Next Steps

**Immediate** (Fix metrics):
1. Restore `metrics.RecordStreamRotation()` call
2. Add `metrics.UpdateActiveStreams()` calls
3. Re-run FASE 6G test to verify rotations happening
4. If rotations confirmed, goroutine leak is separate issue

**Short-term** (Fix goroutine leak):
1. Implement `SetReadDeadline()` on underlying connection
2. Test with isolated container (verify no leak)
3. Scale to 55 containers
4. Validate <2 gor/min growth

**Long-term** (Prevent regressions):
1. Add goroutine leak detector test
2. Add metrics validation in CI
3. Document syscall blocking behavior
4. Create decision log for concurrency patterns

---

## 10. References

### Code Locations

- Container monitor: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
  - `monitorContainer()`: Lines 815-918
  - `readContainerLogsShortLived()`: Lines 920-1064

- Metrics: `/home/mateus/log_capturer_go/internal/metrics/metrics.go`
  - `RecordStreamRotation()`: Lines 793-797
  - `UpdateActiveStreams()`: Lines 805-807

### Git History

- FASE 6G commit: `c7aa762`
- FASE 6F commit: `a5de596` (had rotation tracking)
- Critical deletion: `git diff a5de596..c7aa762 -- internal/monitors/container_monitor.go`

### Test Logs

- Test log: `/home/mateus/log_capturer_go/fase6g_30min.log`
- Key findings:
  - Baseline: 1454 goroutines
  - Final: 2497 goroutines
  - Growth: +34.76 gor/min
  - Rotations: 0 (expected ~3080)

### External References

- Go syscall blocking: https://go.dev/doc/faq#goroutines
- SetReadDeadline: https://pkg.go.dev/net#Conn
- Docker SDK: https://pkg.go.dev/github.com/docker/docker/client
- Context cancellation: https://go.dev/blog/context

---

**Report completed**: 2025-11-07
**Status**: Ready for implementation
**Priority**: CRITICAL (production blocker)
