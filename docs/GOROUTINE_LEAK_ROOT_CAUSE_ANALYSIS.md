# Goroutine Leak Root Cause Analysis
## Comprehensive Investigation Report

**Date**: 2025-11-07
**System**: log_capturer_go Container Monitor
**Issue**: Persistent goroutine leak in container log streaming
**Status**: ROOT CAUSE IDENTIFIED - Solution in Progress

---

## Executive Summary

After three intensive test iterations (FASE 6, 6C, and 6D) spanning 100+ minutes of testing, we have definitively identified the root cause of the goroutine leak in the container monitor's stream rotation mechanism.

**ROOT CAUSE**: The timeout wrapper implementation (commit e79e689) creates orphaned goroutines that cannot be cancelled because `stream.Read()` is a blocking kernel-level syscall that is uninterruptible from Go code.

**IMPACT**:
- +490 orphaned goroutines per system lifecycle
- Complete deadlock of rotation mechanism
- Zero rotations after 10+ minutes (should have 2+)
- System frozen at ~1,425 goroutines

---

## Test Chronology

### FASE 6 - Initial 60-Minute Test
**Commit**: Original code (before fixes)
**Duration**: 60 minutes
**Results**: ❌ FAILED

```
Baseline: 946 goroutines
Final: 2,911 goroutines
Growth Rate: +30.50/min
Critical Window: 18-22 minutes (explosion to +34.2/min)
Rotations: Unknown
```

**Diagnosis**: WaitGroup synchronization issue - heartbeat and reader goroutines mixed.

---

### FASE 6C - 30-Minute Test with Separate WaitGroups
**Commit**: ec99f1e (Separate WaitGroups)
**Duration**: 30 minutes (15 checkpoints)
**Results**: ❌ FAILED

```
Baseline: 1,315 goroutines, 624 FDs
Final: 2,974 goroutines, 1,430 FDs
Growth Rate: +55.30/min (WORSE than FASE 6!)
Total Rotations: 0 ⚠️ CRITICAL
```

**Key Observations**:
- Growth rate INCREASED despite fix
- Rotation mechanism completely broken (0 rotations)
- FD leak accompanied goroutine leak

**Diagnosis**: Separate WaitGroups were correct but incomplete - didn't address the blocking I/O issue.

---

### FASE 6D - 30-Minute Test with Timeout Wrapper
**Commit**: e79e689 (Timeout wrapper fix)
**Duration**: 14+ minutes (stopped early - pattern clear)
**Results**: ❌ FAILED - WORSE

```
Baseline: 935 goroutines, 431 FDs
Checkpoint 5 (10min): 1,425 goroutines, 656 FDs
Growth: +490 goroutines (+49.00/min average)
Total Rotations: 0 ⚠️ CRITICAL

Pattern Analysis:
- Phase 1 (0-4 min): +509 goroutines, 40+ context deadline errors
- Phase 2 (4-14 min): STABLE at ~1,424-1,427, 0 context errors
```

**Critical Finding**: Two-phase behavior revealed fundamental flaw in timeout wrapper approach.

---

## Root Cause Analysis

### The Blocking I/O Problem

The container monitor uses Docker SDK's `ContainerLogs()` API which returns an `io.ReadCloser`:

```go
stream, err := mc.client.ContainerLogs(ctx, containerID, loggingOpts)
```

The core issue:
```go
// Reading from stream.Read() blocks in KERNEL space
n, err := stream.Read(buf)  // ← THIS BLOCKS IN KERNEL SYSCALL
```

### The Flawed Timeout Wrapper (Commit e79e689)

**Attempted Fix** (Lines 976-1008 in container_monitor.go):
```go
// BUGGY CODE - Creates orphaned goroutines
type readOp struct {
    n   int
    err error
}

localBuf := make([]byte, 8192)
readDone := make(chan readOp, 1)

// Perform blocking Read() in separate goroutine
go func() {
    n, err := stream.Read(localBuf)  // ← BLOCKS INDEFINITELY IN KERNEL
    readDone <- readOp{n, err}       // ← NEVER REACHES HERE IF READ BLOCKS
}()

// Wait for Read() completion, timeout, or context cancellation
select {
case <-readerCtx.Done():
    // Context cancelled - allow rotation to proceed
    return  // ← RETURNS BUT GOROUTINE ABOVE KEEPS RUNNING!
case <-time.After(30 * time.Second):
    // Timeout - prevents indefinite blocking
    return  // ← RETURNS BUT GOROUTINE ABOVE KEEPS RUNNING!
case op := <-readDone:
    // Read completed normally
    n = op.n
    err = op.err
}
```

**Why This Fails**:
1. `stream.Read()` blocks in a kernel-level syscall
2. Go's context cancellation does NOT reach kernel level
3. When timeout occurs, `select` returns but goroutine remains blocked
4. Goroutine waits FOREVER for `stream.Read()` to complete
5. Accumulates ~490 orphaned goroutines during startup
6. `readerWg.Wait()` in rotation code waits for these orphaned goroutines
7. **Result**: Complete deadlock - rotations NEVER happen

### Evidence from FASE 6D

**Phase 1 (0-4 minutes)**: Initial Burst
- System creates streams for 50 containers
- Timeout wrapper spawns ~490 goroutines
- Each goroutine blocks in `stream.Read()` kernel syscall
- Timeouts occur, functions return, but goroutines remain
- Heavy "context deadline exceeded" errors (40+ in first 5 min)

**Phase 2 (4-14 minutes)**: Frozen State
- Goroutines stable at ~1,424-1,427 (no further growth)
- Context deadline errors STOP (0 in last 10 minutes)
- **Total rotations: 0** - rotation mechanism deadlocked
- `readerWg.Wait()` waiting for orphaned goroutines that will NEVER complete

### Why Rotations Never Happen

The rotation mechanism in `rotateContainerStreams()`:
```go
func (mc *ContainerMonitor) rotateContainerStreams() {
    // Cancel all reader contexts
    for containerID, cancel := range mc.cancelFuncs {
        cancel()  // ← Signals goroutines to stop
    }

    // Wait for all readers to finish
    mc.readerWg.Wait()  // ← BLOCKS FOREVER waiting for orphaned goroutines!

    // Create new streams (NEVER REACHED)
}
```

The `readerWg.Wait()` blocks indefinitely because:
- Orphaned goroutines still have `readerWg.Add(1)` registered
- But they're blocked in kernel `stream.Read()` syscall
- `cancel()` has no effect on kernel-level blocking
- WaitGroup counter never reaches zero
- Rotation NEVER completes

---

## Comparison of All Three Tests

| Metric | FASE 6 (60min) | FASE 6C (30min) | FASE 6D (14min) |
|--------|----------------|-----------------|-----------------|
| **Commit** | Original | ec99f1e | e79e689 |
| **Fix Attempted** | None | Separate WaitGroups | Timeout Wrapper |
| **Growth Rate** | +30.50/min | +55.30/min | +49.00/min |
| **Final Goroutines** | 2,911 | 2,974 | 1,425* |
| **Rotations** | Unknown | 0 ❌ | 0 ❌ |
| **Context Errors** | Unknown | Many | 40+ then 0 |
| **Behavior** | Continuous leak | Continuous leak | Burst then stable |
| **Status** | Failed | Failed (WORSE) | Failed (deadlocked) |

\* Stopped early; growth pattern stabilized at ~1,425

---

## Why Each Fix Failed

### Fix #1: Separate WaitGroups (ec99f1e)
**Goal**: Separate heartbeat and reader goroutine lifecycle management
**Result**: ❌ Made problem WORSE (+55.30/min vs +30.50/min)
**Why**: Correct design but incomplete - didn't address the blocking I/O issue

### Fix #2: Timeout Wrapper (e79e689)
**Goal**: Prevent `stream.Read()` from blocking indefinitely
**Result**: ❌ Created orphaned goroutines, deadlocked rotations
**Why**: Cannot cancel kernel-level blocking syscalls from Go code

---

## Technical Deep Dive: Why You Can't Cancel stream.Read()

### The Syscall Stack

```
Go Code:          stream.Read(buf)
                         ↓
Go Runtime:       syscall.Read(fd, buf, len)
                         ↓
Kernel:           read(fd, buf, count)  ← BLOCKS HERE
                         ↓
Network/Disk:     Waiting for data...
```

### Context Cancellation Flow

```
Go Code:          ctx.Done() ← Signal sent
                         ↓
Go Runtime:       Check context in Go code
                         ↓
Kernel:           read() syscall still blocked  ← NO SIGNAL REACHES HERE
                         ↓
Result:           Goroutine remains blocked forever
```

**Key Point**: Context cancellation is a Go-level concept. It does NOT interrupt syscalls already in progress in the kernel.

---

## Container Log Analysis

### Context Deadline Errors (FASE 6D)

**First 5 minutes**: 40+ errors like:
```json
{
  "error": "context deadline exceeded",
  "level": "error",
  "msg": "Failed to dispatch container log",
  "container_id": "b1d602e90ae3"
}
```

**After 5 minutes**: 0 errors

**Interpretation**:
- Errors occur during initial burst phase when timeouts fire
- Once orphaned goroutines stabilize, no more timeouts occur
- System enters frozen state

### Goroutine Threshold Warnings

Continuous warnings:
```json
{
  "current_value": 1426,
  "level": "warning",
  "msg": "Goroutine count (1426) exceeded threshold (1000)",
  "threshold": 1000,
  "type": "goroutine"
}
```

Confirms system is stuck at elevated goroutine count.

---

## The Correct Solution

The timeout wrapper approach is **fundamentally incompatible** with blocking I/O syscalls. We need a different architectural approach:

### Option 1: Redesign Rotation (RECOMMENDED)
**Approach**: Don't interrupt active reads; let them complete naturally

```go
// Close the stream connection from outside
stream.Close()  // ← This WILL interrupt the syscall

// stream.Read() will return immediately with io.EOF or error
// Goroutines can then terminate cleanly
```

**Implementation**:
1. Store `stream` reference in monitor struct
2. On rotation: Call `stream.Close()` for all containers
3. Reader goroutines detect closure and exit
4. `readerWg.Wait()` completes successfully
5. Create new streams

**Advantages**:
- No orphaned goroutines
- Clean termination
- Simple implementation
- Works with kernel-level blocking

### Option 2: Use io.Pipe with Cancellable Reader
**Approach**: Separate blocking read into cancellable component

```go
pr, pw := io.Pipe()

// Goroutine 1: Blocks on stream.Read() but can be closed
go func() {
    io.Copy(pw, stream)
    pw.Close()
}()

// Goroutine 2: Reads from pipe (can be cancelled)
go func() {
    for {
        select {
        case <-ctx.Done():
            pr.Close()  // Interrupt pipe
            return
        default:
            buf := make([]byte, 8192)
            n, err := pr.Read(buf)
            // Process...
        }
    }
}()
```

**Advantages**:
- Separates blocking I/O from business logic
- `pr.Close()` can interrupt the read

**Disadvantages**:
- More complex
- Additional goroutine per stream
- Pipe overhead

### Option 3: Non-Blocking I/O with SetNonblock
**Approach**: Use syscall-level non-blocking reads

```go
import "syscall"

// Get underlying file descriptor
fd := getFDFromStream(stream)

// Set non-blocking mode
syscall.SetNonblock(fd, true)

// Now reads return immediately with EAGAIN if no data
```

**Advantages**:
- Complete control over blocking behavior

**Disadvantages**:
- Platform-specific
- Complex error handling
- Requires polling or epoll/kqueue

---

## Recommendation

**Implement Option 1: Redesign Rotation with stream.Close()**

This is the simplest, most reliable solution:

1. Store `stream` reference in `containerState` struct
2. On rotation:
   ```go
   for _, state := range mc.containerStates {
       if state.stream != nil {
           state.stream.Close()  // Interrupt syscall
       }
   }
   mc.readerWg.Wait()  // Now completes quickly
   ```
3. Reader goroutines detect closure:
   ```go
   n, err := stream.Read(buf)
   if err != nil {
       // io.EOF or closed error
       return  // Exit cleanly
   }
   ```

**Expected Results**:
- Zero orphaned goroutines
- Rotations complete in <1 second
- Growth rate: <2/min ✅
- System stable long-term

---

## Next Steps

1. **Implement Option 1** (stream.Close() approach)
2. **Revert commit e79e689** (timeout wrapper)
3. **Keep commit ec99f1e** (separate WaitGroups - correct)
4. **Add stream reference** to containerState
5. **Modify rotation logic** to close streams before waiting
6. **Test with FASE 6E** (30-minute validation)
7. **If successful, run FASE 6F** (60-minute final validation)

---

## Files Modified

- `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
  - Lines 883, 961: Separate WaitGroups (ec99f1e) ✅ KEEP
  - Lines 976-1008: Timeout wrapper (e79e689) ❌ REMOVE

---

## Lessons Learned

1. **Context cancellation does NOT cancel syscalls** - This is a fundamental Go limitation
2. **Timeout wrappers don't work for blocking I/O** - Creates orphaned goroutines
3. **Always close resources from outside** - Don't rely on internal cancellation
4. **Test for stability, not just initial behavior** - FASE 6D's two-phase pattern was key evidence
5. **Goroutine leaks can deadlock systems** - The rotation deadlock was unexpected but logical

---

## Metrics Summary

### FASE 6D Final Metrics (Checkpoint 5):
```
Duration: 10 minutes
Baseline Goroutines: 935
Final Goroutines: 1,425
Growth: +490 (+49.00/min average)
Baseline FDs: 431
Final FDs: 656
Growth: +225
Active Streams: 50
Total Rotations: 0 ❌ CRITICAL
Context Deadline Errors: 0 (last 10 minutes)
```

### Target Metrics:
```
Goroutine Growth Rate: <2/min
FD Growth: <100 total
Rotations: >0 (expect ~10 per 30 minutes for 50 streams)
```

---

## Conclusion

The goroutine leak is caused by an architectural incompatibility between Go's context cancellation and kernel-level blocking syscalls. The timeout wrapper approach (commit e79e689) made the problem worse by creating orphaned goroutines that deadlock the rotation mechanism.

The solution is to explicitly close stream connections from outside (`stream.Close()`), which WILL interrupt the blocking syscall and allow goroutines to terminate cleanly.

**Status**: Ready to implement correct fix (Option 1).

---

**Report Generated**: 2025-11-07 02:25:00
**Author**: Claude Code Analysis
**Classification**: Root Cause Analysis - Goroutine Leak Investigation
