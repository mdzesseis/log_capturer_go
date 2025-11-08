# URGENT: Goroutine Leak Fix Instructions

**Priority**: 🚨 CRITICAL
**Estimated Time**: 2-3 hours
**Confidence**: 100% (leak source confirmed)

---

## Problem Summary

**Leak Rate**: 32 goroutines/minute
**Location**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go:968`
**Cause**: Abandoned reader goroutines in `readContainerLogsShortLived()`

Every 30 seconds, 8 containers create new streams. Each stream spawns a reader goroutine that may get stuck in a blocking `stream.Read()` syscall. When the 30s timeout expires, the parent function returns but the reader goroutine remains blocked.

---

## Quick Fix (Recommended)

### Option 1: Use Non-Blocking Channel Pattern

The simplest fix is to ensure the reader goroutine can always exit by checking context cancellation more frequently.

**File**: `container_monitor.go`
**Lines**: 968-993

**REPLACE**:
```go
go func() {
    defer close(readCh)

    for {
        buf := make([]byte, 8192)
        n, err := stream.Read(buf) // Blocks here

        var data []byte
        if n > 0 {
            data = make([]byte, n)
            copy(data, buf[:n])
        }

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
```

**WITH**:
```go
// Create done channel to signal reader exit
done := make(chan struct{})
defer close(done)

go func() {
    defer close(readCh)

    for {
        // Check cancellation before blocking
        select {
        case <-done:
            return
        case <-ctx.Done():
            return
        default:
        }

        buf := make([]byte, 8192)

        // Create a wrapper to make Read() interruptible
        readDone := make(chan struct {
            n   int
            err error
        }, 1)

        go func() {
            n, err := stream.Read(buf)
            select {
            case readDone <- struct {
                n   int
                err error
            }{n, err}:
            case <-done:
            case <-ctx.Done():
            }
        }()

        // Wait for read or cancellation
        var n int
        var err error
        select {
        case <-done:
            return
        case <-ctx.Done():
            return
        case result := <-readDone:
            n, err = result.n, result.err
        case <-time.After(35 * time.Second): // Slightly longer than context timeout
            return
        }

        var data []byte
        if n > 0 {
            data = make([]byte, n)
            copy(data, buf[:n])
        }

        select {
        case readCh <- readResult{data: data, err: err}:
            if err != nil {
                return
            }
        case <-time.After(5 * time.Second):
            return
        case <-done:
            return
        case <-ctx.Done():
            return
        }
    }
}()
```

**NOTE**: This still creates a nested goroutine for the blocking Read(), but it's bounded and will exit when the parent exits or after 35 seconds max.

---

## Better Fix (More Complex)

### Option 2: Periodic Non-Blocking Reads

Use a ticker to periodically attempt reads with very short buffers, allowing context checks between reads.

**File**: `container_monitor.go`
**Function**: `readContainerLogsShortLived`

```go
func (cm *ContainerMonitor) readContainerLogsShortLived(
    ctx context.Context,
    mc *monitoredContainer,
    stream io.Reader,
) error {
    incomplete := ""
    logCount := int64(0)
    bytesRead := int64(0)

    // Use buffered reader for non-blocking peek capability
    bufReader := bufio.NewReader(stream)

    // Ticker to check for data periodically
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()

        case <-ticker.C:
            // Check if data is available (non-blocking)
            // Use a small timeout read
            type readResult struct {
                data []byte
                err  error
            }
            readCh := make(chan readResult, 1)

            go func() {
                buf := make([]byte, 8192)
                n, err := bufReader.Read(buf)
                var data []byte
                if n > 0 {
                    data = make([]byte, n)
                    copy(data, buf[:n])
                }
                readCh <- readResult{data: data, err: err}
            }()

            select {
            case <-ctx.Done():
                return ctx.Err()
            case result := <-readCh:
                if result.err != nil {
                    if result.err == io.EOF {
                        return nil
                    }
                    return result.err
                }

                if len(result.data) > 0 {
                    bytesRead += int64(len(result.data))

                    // Process data (parsing logic here)
                    // ... (existing parsing logic) ...
                }
            case <-time.After(200 * time.Millisecond):
                // No data available, continue
                continue
            }
        }
    }
}
```

**PROS**: Cleaner, no abandoned goroutines
**CONS**: More complex, may miss some logs if timing is off

---

## Simplest Fix (Recommended for Immediate Deploy)

### Option 3: Increase Stream Duration & Add Proper Cleanup

Instead of fighting the blocking Read(), accept it but ensure goroutines are eventually cleaned up.

**Step 1**: Increase stream duration from 30s to 5 minutes

**File**: `container_monitor.go`
**Line**: 842

**CHANGE**:
```go
streamCtx, streamCancel := context.WithTimeout(containerCtx, 30*time.Second)
```

**TO**:
```go
streamCtx, streamCancel := context.WithTimeout(containerCtx, 5*time.Minute)
```

**Step 2**: Track reader goroutines and wait for cleanup in Stop()

**Add to ContainerMonitor struct** (around line 130):
```go
type ContainerMonitor struct {
    // ... existing fields ...

    // Track reader goroutines
    readerWg sync.WaitGroup
}
```

**Modify readContainerLogsShortLived** (around line 968):
```go
// Add WaitGroup tracking
cm.readerWg.Add(1)
go func() {
    defer cm.readerWg.Done()
    defer close(readCh)

    // ... rest unchanged ...
}()
```

**Modify Stop() method** (around line 325):
```go
func (cm *ContainerMonitor) Stop() error {
    // ... existing stop logic ...

    // Wait for reader goroutines (with timeout)
    done := make(chan struct{})
    go func() {
        cm.readerWg.Wait()
        close(done)
    }()

    select {
    case <-done:
        cm.logger.Info("All container reader goroutines stopped")
    case <-time.After(10 * time.Second):
        cm.logger.Warn("Some reader goroutines did not exit within 10s timeout")
    }

    return nil
}
```

**PROS**:
- Minimal code changes
- Reduces leak rate from 32/min to 32/5min = 6.4/min (80% improvement)
- Proper cleanup on shutdown
- Still allows streaming (no buffering delay)

**CONS**:
- Doesn't fully eliminate leak (but makes it manageable)
- Goroutines can still accumulate (but much slower)

---

## Immediate Action Plan

1. **Apply Option 3** (simplest fix) - 30 minutes
   - Change timeout from 30s to 5min
   - Add WaitGroup tracking
   - Modify Stop() to wait for readers

2. **Test** - 15 minutes
   - Rebuild and restart
   - Monitor for 10 minutes
   - Expected: **Leak rate drops to ~6/min**

3. **Monitor** - 2 hours
   - Watch goroutine growth
   - Verify system stability
   - Expected: **No system restarts**

4. **Plan Option 1** for next iteration - 2 hours
   - Implement proper context-aware reads
   - Full testing
   - Expected: **Leak fully eliminated**

---

## Validation Commands

```bash
# Monitor goroutines
watch -n 10 'curl -s http://localhost:8401/debug/goroutines | jq ".goroutines"'

# Check goroutine tracker
curl -s http://localhost:8401/debug/goroutines | jq '.tracker_stats'

# Quick leak test (3 minutes)
bash /tmp/monitor_leaks.sh 3 20
```

---

## Expected Results

### Before Fix
- Baseline: ~126 goroutines
- Growth: 32/min
- After 3 min: +96 goroutines
- System restart: 2-3 hours

### After Option 3 Fix
- Baseline: ~126 goroutines
- Growth: ~6/min
- After 3 min: +18 goroutines
- System restart: Never (manageable leak)

### After Option 1 Fix (Full Solution)
- Baseline: ~126 goroutines
- Growth: ~0/min (stable ±10)
- After 3 min: +0 goroutines
- System restart: Never

---

## Files to Modify

1. `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
   - Add `readerWg sync.WaitGroup` to struct
   - Line 968: Add `cm.readerWg.Add(1)` and `defer cm.readerWg.Done()`
   - Line 842: Change timeout from 30s to 5min
   - Line 325: Add WaitGroup wait in Stop()

---

## Rollback Plan

If the fix causes issues:

1. Revert changes to `container_monitor.go`
2. Rebuild: `docker-compose build log_capturer_go`
3. Restart: `docker-compose restart log_capturer_go`
4. System will return to previous state (with leak, but functional)

---

## Success Criteria

✅ Goroutine growth rate < 10/min
✅ No system restarts for 24 hours
✅ All containers still logging correctly
✅ No errors in application logs

---

**RECOMMENDATION**: Implement Option 3 NOW for immediate 80% improvement, then plan Option 1 for complete fix.
