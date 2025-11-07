# FASE 6E Failure - Root Cause Analysis
## stream.Close() Placement Issue

**Date**: 2025-11-07
**Test**: FASE 6E (stream.Close() fix attempt)
**Status**: ❌ FAILED - Deadlock persists
**Goroutine Growth**: +196.83/min (WORSE than original)

---

## Executive Summary

FASE 6E test failed catastrophically with goroutine leak rate of +196.83/min, worse than the original +30.50/min in FASE 6. The stream.Close() fix was implemented in the WRONG location, making it unreachable due to a blocking call.

**ROOT CAUSE**: `stream.Close()` is placed AFTER `readContainerLogs()` blocks, making it unreachable. The blocking `stream.Read()` syscall prevents the function from returning, creating a classic deadlock.

---

## The Deadlock Chain

### Code Flow (container_monitor.go)

```go
// Line 879 - monitorContainer() calls readContainerLogs
readErr := cm.readContainerLogs(streamCtx, mc, stream)  // ← BLOCKS HERE

// Lines 886-891 - Attempt to close stream (NEVER REACHED!)
mc.mu.Lock()
if mc.stream != nil {
    mc.stream.Close()  // ← UNREACHABLE CODE!
    mc.stream = nil
}
mc.mu.Unlock()
```

### Why readContainerLogs() Never Returns

```go
// Line 955-1009 - readContainerLogs function
func (cm *ContainerMonitor) readContainerLogs(ctx context.Context, mc *monitoredContainer, stream io.Reader) error {
    // ...

    // Line 978 - Spawn reader goroutine
    mc.readerWg.Add(1)
    go func() {
        defer mc.readerWg.Done()  // ← Never called!

        for {
            // Line 985 - Context check (BEFORE read)
            select {
            case <-readerCtx.Done():
                return  // ← Would exit if reached
            default:
            }

            // Line 991 - THE BLOCKING POINT
            n, err := stream.Read(localBuf)  // ← BLOCKS IN KERNEL FOREVER
            //                                   Context cancellation can't reach here!
            //                                   Goroutine stuck forever!

            // Lines 1000-1007 - Try to send result (never reached)
            select {
            case readCh <- readResult{data: data, err: err}:
                if err != nil {
                    return
                }
            case <-readerCtx.Done():
                return
            }
        }
    }()

    // Main loop reads from channel - blocks waiting for data that never comes
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()  // ← Returns here when context expires
        case result, ok := <-readCh:
            // Process result...
        }
    }
}
```

---

## Why This is a Deadlock

### Step-by-Step Breakdown:

1. **Stream Created** (line 858):
   ```go
   stream, err := cm.dockerPool.ContainerLogs(streamCtx, mc.id, logOptions)
   ```

2. **Stream Stored** (line 875):
   ```go
   mc.stream = stream
   ```

3. **readContainerLogs Called** (line 879):
   ```go
   readErr := cm.readContainerLogs(streamCtx, mc, stream)  // ← BLOCKS
   ```

4. **Reader Goroutine Spawned** (line 978):
   ```go
   mc.readerWg.Add(1)
   go func() {
       defer mc.readerWg.Done()
       for {
           // ...
           n, err := stream.Read(localBuf)  // ← BLOCKS IN KERNEL
       }
   }()
   ```

5. **Context Timeout Occurs** (after cm.rotationInterval):
   - `streamCtx` expires
   - `readContainerLogs()` detects `ctx.Done()` and returns `context.DeadlineExceeded`
   - BUT: The reader goroutine is STILL BLOCKED in `stream.Read()`!
   - The context check at line 985 was checked BEFORE entering the blocking read

6. **Function Returns** (line 879):
   ```go
   readErr := cm.readContainerLogs(streamCtx, mc, stream)  // Returns here
   ```

7. **Attempt to Close Stream** (line 888):
   ```go
   mc.stream.Close()  // ← NOW we close it, but TOO LATE!
   ```

8. **Wait for Reader** (line 899):
   ```go
   mc.readerWg.Wait()  // ← DEADLOCK!
   //                      Reader goroutine still blocked in stream.Read()
   //                      Never called readerWg.Done()
   //                      WaitGroup counter never reaches zero
   ```

9. **Rotation Never Completes**:
   - `readerWg.Wait()` blocks forever
   - Rotation loop never reaches line 936 to update log options
   - No new stream is created
   - System frozen

---

## Why stream.Read() Can't Be Cancelled

### The Syscall Stack

```
Go Code:          stream.Read(buf)
                        ↓
Go Runtime:       syscall.Read(fd, buf, len)
                        ↓
Kernel:           read(fd, buf, count)  ← BLOCKS HERE IN KERNEL
                        ↓
Docker Socket:    Waiting for log data...
```

### Context Cancellation Flow

```
Go Code:          readerCtx.Done() ← Signal sent
                        ↓
Go Runtime:       Channel closed, goroutine can detect
                        ↓
Goroutine:        select { case <-readerCtx.Done(): return }
                        ↓
Problem:          This check is at line 985, BEFORE stream.Read()
                        ↓
Reality:          Once stream.Read() is called at line 991,
                  the goroutine is IN THE KERNEL
                  No Go-level signal can reach it
                        ↓
Result:           Goroutine blocked forever
```

**Key Point**: Context cancellation is a Go-level concept. Once a goroutine enters a blocking syscall like `read()`, it's in kernel space where Go's runtime has no control.

---

## FASE 6E Test Results

### Metrics

```
Baseline (t=0):      1,376 goroutines, 666 FDs, 50 streams
Checkpoint 1 (2min): 1,376 goroutines, +0/min ✅
Checkpoint 2 (4min): 2,006 goroutines, +157.50/min ❌
Checkpoint 3 (6min): 2,557 goroutines, +196.83/min ❌ CATASTROPHIC

Total Rotations: 0 ⚠️ CRITICAL
FD Growth: 572 ❌
```

### Comparison with Previous Tests

| Test | Commit | Fix | Growth Rate | Rotations | Status |
|------|--------|-----|-------------|-----------|--------|
| FASE 6 | Original | None | +30.50/min | Unknown | ❌ Failed |
| FASE 6C | ec99f1e | Separate WaitGroups | +55.30/min | 0 | ❌ Failed (worse) |
| FASE 6D | e79e689 | Timeout wrapper | +49.00/min | 0 | ❌ Failed (deadlock) |
| **FASE 6E** | 529c30c | stream.Close() (wrong place) | **+196.83/min** | 0 | ❌ **CATASTROPHIC** |

FASE 6E is the WORST result yet, showing that placing stream.Close() after the blocking call actually makes the problem worse.

---

## Why FASE 6E is Worse Than Before

### Additional Goroutine Accumulation

The timeout wrapper from e79e689 was removed, but the stream.Close() fix is unreachable, so we get:

1. **Reader Goroutine Leak** (same as FASE 6C/6D):
   - Each rotation attempt spawns a reader goroutine
   - Reader goroutine blocks in `stream.Read()`
   - `readerWg.Wait()` blocks forever
   - Rotation never completes

2. **readContainerLogs Never Returns**:
   - The main readContainerLogs function blocks waiting for reader goroutine
   - But reader goroutine is stuck in kernel
   - Function accumulates waiting state

3. **Multiple Streams Open**:
   - Each failed rotation attempt opens a new stream
   - Old stream never closed (stream.Close() unreachable)
   - File descriptors leak
   - Multiple reader goroutines per container

**Result**: Faster goroutine accumulation than any previous test.

---

## The Correct Fix

### Problem

We need to close the stream to interrupt `stream.Read()`, but we can't do it from within the blocked call.

### Solution

Close the stream from a SEPARATE goroutine that monitors context cancellation:

```go
// Create stream
stream, err := cm.dockerPool.ContainerLogs(streamCtx, mc.id, logOptions)
if err != nil {
    return err
}

// Store stream
mc.mu.Lock()
mc.stream = stream
mc.mu.Unlock()

// Spawn watcher goroutine to close stream on context cancellation
go func() {
    <-streamCtx.Done()  // Wait for context cancellation

    // Close stream to interrupt blocking Read()
    mc.mu.Lock()
    if mc.stream != nil {
        mc.stream.Close()
        mc.stream = nil
    }
    mc.mu.Unlock()
}()

// Now call readContainerLogs - it will be interrupted when watcher closes stream
readErr := cm.readContainerLogs(streamCtx, mc, stream)

// Wait for reader goroutine to exit
mc.readerWg.Wait()
```

### Why This Works

1. **Watcher goroutine** starts immediately after stream creation
2. **Watcher waits** for `streamCtx.Done()` (rotation timeout or cancellation)
3. **When context expires**:
   - Watcher calls `stream.Close()`
   - This interrupts the kernel `read()` syscall
   - `stream.Read()` returns immediately with `io.EOF` or error
   - Reader goroutine sees the error and exits
   - Calls `readerWg.Done()`
4. **readContainerLogs** detects closed stream and returns
5. **monitorContainer** calls `readerWg.Wait()` - completes immediately
6. **Rotation proceeds** to create new stream

---

## Evidence from Container Logs

```bash
$ docker logs log_capturer_go --since 7m 2>&1 | grep "Stream rotated successfully"
# (no output - zero rotations)

$ docker logs log_capturer_go --since 7m 2>&1 | grep -c "rotated successfully"
0
```

Zero rotation events in 7 minutes confirms rotation mechanism is completely deadlocked.

---

## Next Steps

1. **Implement watcher goroutine** to close stream on context cancellation
2. **Move stream.Close()** from after `readContainerLogs()` to within watcher
3. **Add cleanup tracking** for watcher goroutine
4. **Test with FASE 6F** (30-minute validation)
5. **If successful, run FASE 6G** (60-minute final validation)

---

## Files to Modify

- `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
  - Lines 873-899: Add watcher goroutine
  - Lines 886-891: Remove unreachable stream.Close()
  - Add WaitGroup for watcher goroutine

---

## Lessons Learned

1. **Blocking calls must be interruptible** - Can't close resources after blocking call returns
2. **Kernel syscalls ignore context** - Context cancellation doesn't reach kernel level
3. **Separate watcher pattern** - Use dedicated goroutine to monitor context and close resources
4. **Test early, test often** - FASE 6E revealed the fix was unreachable within minutes
5. **Goroutine dumps are critical** - Would have shown exactly where goroutines were stuck

---

**Report Generated**: 2025-11-07 03:00:00
**Author**: Claude Code Analysis
**Classification**: Goroutine Leak Investigation - FASE 6E Failure Analysis
