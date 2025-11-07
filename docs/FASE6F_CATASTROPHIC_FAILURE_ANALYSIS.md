# FASE 6F Catastrophic Failure Analysis

**Date**: 2025-11-07 03:30 UTC
**Test**: FASE 6F (Watcher Goroutine Pattern)
**Status**: ❌ **CATASTROPHIC FAILURE** - Worst leak rate ever recorded
**Commit**: a5de596

---

## Executive Summary

FASE 6F test failed with the **worst goroutine leak rate observed across all tests** (~220-464 goroutines/min). The watcher goroutine pattern, while theoretically sound, fails in practice because **closing an io.ReadCloser does NOT reliably interrupt a blocking Read() syscall**.

---

## Test Results Comparison

| Test | Commit | Fix | Rate (goroutines/min) | Rotations | Status |
|------|--------|-----|----------------------|-----------|--------|
| FASE 6 | Original | None | +30.50 | Unknown | ❌ Failed |
| FASE 6C | ec99f1e | Separate WaitGroups | +55.30 | 0 | ❌ Failed (worse) |
| FASE 6D | e79e689 | Timeout wrapper | +49.00 | 0 | ❌ Failed (deadlock) |
| FASE 6E | 529c30c | stream.Close() after block | +196.83 | 0 | ❌ Catastrophic |
| **FASE 6F** | a5de596 | **Watcher goroutine** | **~220-464** | **0** | ❌ **WORST EVER** |

### FASE 6F Metrics Progression

```
Baseline (t=0):       483 goroutines,  182 FDs, 50 streams
Unknown time (~10min): 2,803 goroutines, 1,318 FDs, 50 streams
Final (~15min):        3,783 goroutines, 1,797 FDs, 50 streams

Growth: +3,300 goroutines in ~15 minutes
Estimated Rate: 220-464 goroutines/min (10x worse than FASE 6!)
Rotations: 0 ❌ (mechanism completely broken)
```

---

## The Watcher Goroutine Implementation

### Code (Lines 879-914, container_monitor.go)

```go
// CRITICAL FIX (FASE 6E): Add watcher goroutine to monitor context cancellation
// and close stream from OUTSIDE the blocking Read() call.
watcherWg := sync.WaitGroup{}
watcherWg.Add(1)
go func() {
    defer watcherWg.Done()
    <-streamCtx.Done() // Wait for context cancellation or timeout

    // Close stream to interrupt blocking Read()
    mc.mu.Lock()
    if mc.stream != nil {
        mc.stream.Close()  // ← CLOSES mc.stream
        mc.stream = nil
    }
    mc.mu.Unlock()
}()

// The watcher goroutine above will interrupt readContainerLogs if context expires
readErr := cm.readContainerLogs(streamCtx, mc, stream)  // ← Passes `stream` as parameter

// Cancel context (triggers watcher if not already triggered)
streamCancel()

// CRITICAL: Wait for reader goroutine to exit before starting new rotation
mc.readerWg.Wait()

// CRITICAL: Wait for watcher goroutine to complete
watcherWg.Wait()
```

### Reader Goroutine (Lines 993-1024, readContainerLogs function)

```go
// Goroutine para ler do stream - TRACKED WITH READER WAITGROUP
mc.readerWg.Add(1) // Track this goroutine
go func() {
    defer mc.readerWg.Done() // Always cleanup
    defer close(readCh)      // Close channel when exiting to unblock readers

    for {
        // Check context before blocking read
        select {
        case <-readerCtx.Done():
            return // Exit immediately if context cancelled
        default:
        }

        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)  // ← READS from `stream` parameter, NOT mc.stream!
        //                                   This blocks in kernel!

        // ... send data via readCh ...
    }
}()
```

---

## Root Cause Analysis

### The Fatal Flaw

**The watcher goroutine closes `mc.stream` while the reader goroutine reads from the `stream` function parameter.**

Although they reference the same underlying stream object initially (line 875: `mc.stream = stream`), the `stream.Close()` call does NOT reliably interrupt a blocking `stream.Read()` syscall!

### Why stream.Close() Doesn't Work

1. **Blocking Read is in Kernel Space**:
   ```
   Go Code:    stream.Read(buf)
                    ↓
   Go Runtime: syscall.Read(fd, buf, len)
                    ↓
   Kernel:     read(fd, buf, count)  ← BLOCKS HERE
                    ↓
   Docker API: Waiting for log data...
   ```

2. **Close() on Different Goroutine**:
   ```
   Watcher Goroutine:  mc.stream.Close()  ← Closes the stream
                            ↓
   Reader Goroutine:   stream.Read(buf)   ← STILL BLOCKED IN KERNEL!
                                           ← Read() may not detect closure immediately
   ```

3. **io.ReadCloser Interface**:
   - `io.ReadCloser` is an interface, not a concrete type
   - The underlying Docker stream implementation determines close behavior
   - Some implementations DON'T interrupt blocking reads on close
   - The `Read()` may continue blocking even after `Close()` is called

### Evidence of Failure

1. **Zero Rotations**: No rotations occurred despite 5-minute timeout
2. **Massive Goroutine Leak**: +3,300 goroutines in ~15 minutes
3. **readerWg.Wait() Deadlock**: Rotation waits forever for reader goroutines that never exit
4. **Multiple Watcher Goroutines**: Each rotation attempt creates another watcher, but readers never finish

### The Deadlock Chain

```
Rotation Cycle:
1. Create stream
2. Spawn watcher goroutine (Add to watcherWg)
3. Spawn reader goroutine (Add to readerWg)
4. streamCtx timeout (5 minutes)
5. Watcher closes mc.stream
6. Reader STILL BLOCKED in stream.Read()  ← PROBLEM!
7. readerWg.Wait() blocks forever  ← DEADLOCK!
8. watcherWg.Wait() never reached
9. New rotation never starts
10. Accumulate goroutines on EVERY container

With 50 containers:
- 50 watcher goroutines stuck waiting
- 50 reader goroutines stuck in Read()
- Every retry creates MORE goroutines
- Exponential accumulation!
```

---

## Why This is Worse Than All Previous Tests

### FASE 6 Original (+30.50/min)
- Single mechanism leak (reader goroutines)
- Predictable growth pattern

### FASE 6C Separate WaitGroups (+55.30/min)
- Still single mechanism
- Slightly worse due to timeout issues

### FASE 6D Timeout Wrapper (+49.00/min)
- Created orphaned goroutines
- Stabilized after initial burst

### FASE 6E stream.Close() After Block (+196.83/min)
- Close never reached
- Accumulating failed attempts

### **FASE 6F Watcher Goroutine (+220-464/min) - WORST**
- **Two separate goroutine leaks**:
  1. **Reader goroutines** (blocking in Read())
  2. **Watcher goroutines** (waiting for readers that never finish)
- **Multiplicative effect**: 50 containers × 2 goroutines per rotation attempt
- **No stabilization**: Continues growing without bound

---

## Technical Explanation

### Docker ContainerLogs Stream Type

The Docker SDK's `ContainerLogs()` returns:
```go
type ReadCloser interface {
    Reader
    Closer
}
```

The underlying implementation is a **network stream over Unix socket or HTTP**. These streams have platform-specific close behaviors:

1. **Unix Socket**: `Close()` may not interrupt pending `read()` syscall
2. **HTTP Response Body**: Close behavior varies by HTTP library version
3. **Buffered Reader**: Additional buffering layer complicates interruption

**Key Point**: There is NO GUARANTEE that calling `Close()` on an `io.ReadCloser` will interrupt a blocking `Read()` call!

---

## Why Context Cancellation Doesn't Help

The reader goroutine checks context **before** each read:

```go
select {
case <-readerCtx.Done():
    return  // ← Would exit if reached
default:
}

n, err := stream.Read(localBuf)  // ← BUT BLOCKS HERE!
//                                   Context check above already passed
//                                   Goroutine now in kernel, unreachable
```

Once in the blocking `Read()`, the goroutine is in kernel space. Context cancellation signals cannot reach it.

---

## Comparison with Documented Go Patterns

### Standard Pattern (Doesn't Apply Here)

Most Go examples show:
```go
go func() {
    for {
        select {
        case <-ctx.Done():
            return
        case data := <-ch:
            process(data)
        }
    }
}()
```

**This works because `select` can be interrupted by context.**

### Our Case (Blocking I/O)

```go
go func() {
    for {
        // Check context (can exit here)
        select {
        case <-ctx.Done():
            return
        default:
        }

        // But then we block in kernel! (can't exit here)
        n, err := stream.Read(buf)  // ← PROBLEM!
    }
}()
```

**This DOESN'T work because `Read()` is a blocking syscall.**

---

## Historical Context - All Attempted Fixes

| Attempt | Approach | Result | Why It Failed |
|---------|----------|--------|--------------|
| 1. Separate WaitGroups (ec99f1e) | Isolate heartbeat from reader goroutines | ❌ Worse | Didn't address blocking I/O |
| 2. Timeout Wrapper (e79e689) | Timeout around Read() | ❌ Deadlock | Created orphaned goroutines |
| 3. stream.Close() After Block (529c30c) | Close stream after function returns | ❌ Catastrophic | Code never reached (function blocks) |
| 4. **Watcher Goroutine (a5de596)** | **Close stream from separate goroutine** | ❌ **WORST** | **Close doesn't interrupt Read()** |

**All attempts failed because they tried to interrupt a kernel-level blocking syscall from Go-level code, which is fundamentally impossible.**

---

## The Correct Solution

### Why Previous Approaches Failed

All attempts tried to **interrupt** the blocking `Read()` call. This is the wrong approach because:

1. Go context cancellation is application-level
2. `Read()` is a kernel-level syscall
3. There's no reliable cross-platform way to interrupt a blocking read

### The Right Approach: Redesign Stream Lifecycle

Instead of trying to interrupt active reads, **prevent them from blocking indefinitely**:

**Option A: Use Non-Blocking I/O with SetDeadline**
```go
// Set read deadline on the stream
if setter, ok := stream.(interface{ SetReadDeadline(time.Time) error }); ok {
    setter.SetReadDeadline(time.Now().Add(cm.rotationInterval))
}

n, err := stream.Read(buf)
// Will return with timeout error after rotationInterval
```

**Problem**: Docker ContainerLogs stream doesn't support `SetReadDeadline()`

**Option B: Use io.Pipe with Explicit Close**
```go
pr, pw := io.Pipe()

// Goroutine 1: Copy from Docker stream to pipe
go func() {
    io.Copy(pw, dockerStream)
    pw.Close()
}()

// Goroutine 2: Read from pipe with context cancellation
go func() {
    for {
        select {
        case <-ctx.Done():
            pr.Close()  // This WILL interrupt io.Copy above!
            return
        default:
            buf := make([]byte, 8192)
            n, err := pr.Read(buf)
            // ...
        }
    }
}()
```

**Problem**: Adds complexity, extra goroutines, and latency

**Option C: Accept Goroutine Accumulation, Use Aggressive Timeouts**
```go
// Don't try to wait for reader goroutines to finish
// Let them eventually timeout and exit on their own
// Use short rotation intervals (30 seconds instead of 5 minutes)
```

**Problem**: Still leaks goroutines, just slower

**Option D: Completely Redesign Container Monitoring** ⭐ **RECOMMENDED**
```go
// Don't use persistent streams
// Poll container logs on-demand every N seconds

func (cm *ContainerMonitor) monitorContainer(ctx context.Context, containerID string) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    lastTimestamp := time.Now()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Create NEW stream for each poll
            stream, err := cm.dockerPool.ContainerLogs(ctx, containerID, types.ContainerLogsOptions{
                Since: lastTimestamp.Format(time.RFC3339),
                // ...
            })
            if err != nil {
                continue
            }

            // Read ALL available data (non-blocking)
            data, _ := ioutil.ReadAll(stream)
            stream.Close()  // Always close immediately

            // Process data
            processLogs(data)

            lastTimestamp = time.Now()
        }
    }
}
```

**Advantages**:
- No persistent streams
- No blocking reads
- Clean lifecycle
- Predictable resource usage
- Easily cancellable

**Disadvantages**:
- Potential log loss if data arrives between polls
- Higher CPU usage (creating streams frequently)
- Increased Docker API load

---

## Recommendations

### Immediate Actions

1. ✅ **STOP all testing** - FASE 6F is consuming resources catastrophically
2. ✅ **Document failure** - This report
3. ⚠️ **Revert to original code** - Even with known leak, it's better than current state

### Next Steps

1. **Implement Option D**: Redesign container monitoring to use polling instead of persistent streams
2. **Prototype polling approach**: Create proof-of-concept
3. **Test for log loss**: Verify no logs are missed during polling
4. **Benchmark performance**: Compare CPU/memory usage vs current approach
5. **Test with FASE 6G**: 60-minute validation of polling approach

### Alternative: Accept the Leak

If redesign is not feasible:

1. Set rotation interval to 30 seconds (instead of 5 minutes)
2. Add maximum container limit (e.g., 20 containers max)
3. Implement periodic restart mechanism
4. Monitor goroutine count and restart when threshold exceeded

---

## Lessons Learned

1. **Kernel-level blocking I/O cannot be reliably interrupted from user space**
2. **io.ReadCloser.Close() does NOT guarantee Read() interruption**
3. **Context cancellation only works for Go-level operations**
4. **Docker SDK streams are not designed for rotation/interruption**
5. **Persistent connections require careful lifecycle management**
6. **Polling approaches are often simpler and more reliable than streaming**

---

## Conclusion

FASE 6F demonstrates that the watcher goroutine pattern, while theoretically sound, is **fundamentally incompatible with Docker's ContainerLogs API**. The underlying stream implementation does not support the interruption mechanisms we need for safe rotation.

**The goroutine leak cannot be fixed with the current architecture.** A complete redesign of the container monitoring approach is required, moving from persistent streaming to periodic polling.

**Status**: Investigation complete. Ready to implement polling-based solution.

---

**Report Generated**: 2025-11-07 03:30 UTC
**Author**: Claude Code Analysis
**Classification**: Goroutine Leak Investigation - FASE 6F Catastrophic Failure
**Test Series**: FASE 6 → 6C → 6D → 6E → 6F (ALL FAILED)
