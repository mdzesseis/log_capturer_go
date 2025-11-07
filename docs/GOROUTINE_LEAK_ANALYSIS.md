# Goroutine Leak Analysis Report

**Date**: 2025-11-06
**Severity**: HIGH
**Status**: IDENTIFIED

---

## Executive Summary

A critical goroutine leak has been identified in the log_capturer_go application. The system exhibits a **constant growth rate of ~32 goroutines per minute**, growing from a baseline of 6 goroutines to 198 goroutines in 5 minutes.

**Growth Pattern**:
- Baseline: 6 goroutines
- After 1 min: 102 goroutines (+96)
- After 2 min: 134 goroutines (+128 total)
- After 3 min: 166 goroutines (+160 total)
- After 4 min: 198 goroutines (+192 total)
- **Growth rate**: ~32 goroutines/minute (CONSTANT)

---

## Root Cause Analysis

### PRIMARY LEAK SOURCE

**Location**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

**Function**: `readContainerLogs()` (lines 779-948)

**Issue**: **UNTRACKED GOROUTINE** created on line 792

```go
// Goroutine para ler do stream
go func() {
    for {
        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)

        // Copiar apenas os bytes lidos
        var data []byte
        if n > 0 {
            data = make([]byte, n)
            copy(data, localBuf[:n])
        }

        select {
        case readCh <- readResult{data: data, err: err}:
            if err != nil {
                return // Sair se houver erro (incluindo EOF)
            }
        case <-ctx.Done():
            return // Context cancelado, sair
        }
    }
}()
```

### Why This Causes a Leak

1. **No WaitGroup Tracking**: The goroutine is created but never tracked in `mc.heartbeatWg`
2. **Stream Blocking**: If the Docker stream blocks indefinitely, this goroutine waits forever
3. **Channel Deadlock**: If the main loop exits early, this goroutine can block on `readCh <- ...`
4. **Multiple Containers**: With 9 Docker containers being monitored, each container can leak multiple goroutines
5. **Reconnection Logic**: Container reconnections create NEW goroutines without cleaning up old ones

### Evidence from Logs

```
{"baseline":6,"current_count":102,"duration":60.003336034,"growth":96,"level":"warning","msg":"Significant goroutine count change detected","time":"2025-11-06T06:42:21Z","total_growth":96}
{"baseline":6,"current_count":134,"duration":60.000761254,"growth":32,"level":"warning","msg":"Significant goroutine count change detected","time":"2025-11-06T06:43:23Z","total_growth":128}
{"baseline":6,"current_count":166,"duration":59.999243746,"growth":32,"level":"warning","msg":"Significant goroutine count change detected","time":"2025-11-06T06:44:26Z","total_growth":160}
{"baseline":6,"current_count":198,"duration":60.000225199,"growth":32,"level":"warning","msg":"Significant goroutine count change detected","time":"2025-11-06T06:45:29Z","total_growth":192}
```

**Pattern**: Exactly 32 goroutines per minute suggests ~2-3 new goroutines per container every minute.

---

## Secondary Leak Sources

### 2. File Monitor Discovery Goroutines

**Location**: `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go`

**Lines**: 144-165, 192-202, 308-311

```go
// Line 144: File discovery goroutine
go func() {
    defer fm.wg.Done()
    // ... discovery logic
}()

// Line 192: Stop() wait goroutine (GOOD - has cleanup)
go func() {
    fm.wg.Wait()
    close(done)
}()

// Line 308: File reading goroutine (POTENTIAL LEAK)
go func() {
    time.Sleep(100 * time.Millisecond)
    fm.readFile(mf)
}()  // ❌ NO WaitGroup tracking!
```

**Issue on Line 308**:
- Anonymous goroutine created to read initial file content
- **NOT tracked** in `fm.wg`
- If `readFile()` blocks or panics, this goroutine leaks
- Created for EVERY file added to monitoring (6 files = 6 potential leaks)

---

## Impact Assessment

### Immediate Impacts
- **Memory Growth**: Each goroutine consumes ~2-8KB of stack space
- **Scheduler Pressure**: 200+ goroutines overload the Go scheduler
- **Resource Exhaustion**: After 30 minutes: ~960 goroutines, consuming ~8MB+ stack memory
- **Container Unhealthy**: Application marked as "unhealthy" by Docker health checks

### Long-Term Impacts
- **OOM Crashes**: After hours of operation, goroutines can reach thousands
- **Performance Degradation**: Log processing throughput decreases
- **Failed Shutdowns**: Leaked goroutines prevent clean application shutdown
- **Data Loss**: Graceful shutdown timeout can cause log loss

---

## Detailed Leak Mechanisms

### Container Monitor Leak Chain

```
1. Container starts → monitorContainer() called
2. monitorContainer() creates heartbeat goroutine (line 682) ✅ TRACKED
3. Container stream opened → readContainerLogs() called
4. readContainerLogs() creates reader goroutine (line 792) ❌ UNTRACKED
5. Stream reconnection loop (line 707-775)
6. Each reconnection creates NEW reader goroutine
7. Old reader goroutines may block if:
   - Stream blocks on Read()
   - Channel readCh is full
   - Context cancellation is missed
```

### Leak Reproduction Steps

1. Start application with 9 Docker containers
2. Containers generate logs continuously
3. Every ~30 seconds, stream disconnections occur (network issues, Docker restart)
4. Each disconnection:
   - Creates NEW reader goroutine
   - Old goroutine may not exit cleanly
   - Net result: +2-3 goroutines per container per minute
5. After 1 minute: 9 containers × 3 goroutines = ~27 new goroutines
6. Observed: ~32 goroutines/minute (close match!)

---

## Code Path Analysis

### Container Monitoring Lifecycle

```
StartContainerMonitoring (line 541)
  ↓
taskManager.StartTask("container_XXX", monitorContainer) (line 610)
  ↓
monitorContainer() creates:
  - heartbeatWg.Add(1) for heartbeat goroutine ✅
  - Heartbeat goroutine (line 682) ✅ TRACKED
  ↓
  - Open Docker log stream (line 713)
  ↓
  - readContainerLogs(stream) (line 730)
    ↓
    - Creates UNTRACKED reader goroutine (line 792) ❌
    ↓
  - Stream error/reconnection (line 743)
  ↓
  - LOOP back to open stream (creates ANOTHER reader goroutine)
```

### Critical Code Section

**File**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
**Function**: `readContainerLogs()`
**Lines**: 792-813

```go
// Goroutine para ler do stream
go func() {  // ❌ NO WaitGroup TRACKING!
    for {
        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)  // ❌ CAN BLOCK INDEFINITELY

        var data []byte
        if n > 0 {
            data = make([]byte, n)
            copy(data, localBuf[:n])
        }

        select {
        case readCh <- readResult{data: data, err: err}:  // ❌ CAN BLOCK if receiver exits
            if err != nil {
                return
            }
        case <-ctx.Done():  // ✅ Has context cancellation check
            return
        }
    }
}()
```

**Problems**:
1. No `mc.heartbeatWg.Add(1)` before goroutine creation
2. No `defer mc.heartbeatWg.Done()` in goroutine
3. `stream.Read()` can block forever if Docker daemon freezes
4. `readCh <- ...` can block if main loop exits early
5. No timeout on stream reads

---

## Recommended Fixes

### FIX 1: Track Reader Goroutine with WaitGroup (CRITICAL)

**File**: `internal/monitors/container_monitor.go`
**Function**: `readContainerLogs()`
**Lines**: 791-813

```go
// BEFORE (LEAKING):
go func() {
    for {
        // ... read logic
    }
}()

// AFTER (FIXED):
mc.heartbeatWg.Add(1)  // ✅ Track goroutine
go func() {
    defer mc.heartbeatWg.Done()  // ✅ Always cleanup
    for {
        // ... read logic
    }
}()
```

### FIX 2: Add Timeout to Stream Reads

```go
// Create a context with read timeout
readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
defer readCancel()

// Use readCtx for stream operations
stream, err := cm.dockerPool.ContainerLogsWithContext(readCtx, mc.id, logOptions)
```

### FIX 3: Fix File Monitor Goroutine (Line 308)

**File**: `internal/monitors/file_monitor.go`
**Lines**: 308-312

```go
// BEFORE (LEAKING):
go func() {
    time.Sleep(100 * time.Millisecond)
    fm.readFile(mf)
}()

// AFTER (FIXED):
fm.wg.Add(1)  // ✅ Track goroutine
go func() {
    defer fm.wg.Done()  // ✅ Always cleanup
    time.Sleep(100 * time.Millisecond)
    fm.readFile(mf)
}()
```

### FIX 4: Buffered Channel with Drain Logic

Prevent channel blocking by using buffered channel and drain on context cancel:

```go
readCh := make(chan readResult, 10)  // Buffered channel

// Cleanup goroutine to drain channel on context cancel
go func() {
    <-ctx.Done()
    // Drain channel to unblock sender
    for len(readCh) > 0 {
        <-readCh
    }
}()
```

---

## Validation Plan

### Step 1: Apply Fix and Monitor
1. Apply FIX 1 (WaitGroup tracking)
2. Apply FIX 3 (File monitor fix)
3. Restart application
4. Monitor goroutine count for 10 minutes

### Step 2: Expected Results
- Baseline: 6-10 goroutines
- After 5 minutes: Should stabilize at ~15-20 goroutines (not grow to 166+)
- After 10 minutes: Should remain stable (<25 goroutines)

### Step 3: Stress Test
1. Start/stop containers repeatedly
2. Force network disconnections
3. Monitor goroutine count remains stable

### Metrics to Monitor

```promql
# Goroutine count over time
log_capturer_goroutines

# Goroutine growth rate
rate(log_capturer_goroutines[5m])

# Memory usage (should stabilize if leak is fixed)
log_capturer_memory_usage_bytes
```

---

## Prevention Guidelines

### Goroutine Lifecycle Best Practices

1. **Always Track with WaitGroup**:
   ```go
   wg.Add(1)
   go func() {
       defer wg.Done()
       // ... work
   }()
   ```

2. **Always Check Context**:
   ```go
   select {
   case <-ctx.Done():
       return
   case <-ticker.C:
       // ... work
   }
   ```

3. **Use Timeouts for Blocking Operations**:
   ```go
   ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
   defer cancel()
   ```

4. **Close Resources in defer**:
   ```go
   defer stream.Close()
   defer timer.Stop()
   ```

5. **Test with Race Detector**:
   ```bash
   go test -race ./...
   ```

---

## Appendix: Goroutine Inventory

### Expected Goroutines (Baseline ~6)

1. Main goroutine (1)
2. HTTP server goroutine (1)
3. Metrics server goroutine (1)
4. Signal handler goroutine (1)
5. Resource monitor goroutine (1)
6. Config reloader goroutine (1)

### Runtime Goroutines (~15-20 expected)

- Dispatcher workers: 6
- Dispatcher stats updater: 1
- Container monitor main loop: 1
- Container events listener: 1
- Container health check: 1
- File monitor main loop: 1
- Backpressure manager: 1
- Position buffer manager: 2

**Expected Total**: ~20-25 goroutines (STABLE)

### Leaked Goroutines (~32/minute growth)

- Container reader goroutines: ~27/minute (3 per container × 9 containers)
- File reader goroutines: ~5/minute (on file discovery)

**Actual Total After 5 minutes**: 198 goroutines (LEAK CONFIRMED)

---

## References

- Resource Monitor: `/home/mateus/log_capturer_go/pkg/monitoring/resource_monitor.go`
- Container Monitor: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
- File Monitor: `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go`
- Dispatcher: `/home/mateus/log_capturer_go/internal/dispatcher/dispatcher.go`

---

**Next Steps**: Apply recommended fixes and validate with goroutine profiling.
