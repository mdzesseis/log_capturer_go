# GOROUTINE LEAK FIX - CODE DIFF

## File Modified

`/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

---

## Change 1: Add `readerWg` Field

**Location**: Line 159

```diff
 // monitoredContainer representa um container sendo monitorado
 type monitoredContainer struct {
 	id              string
 	name            string
 	image           string
 	labels          map[string]string
 	since           time.Time
 	stream          io.ReadCloser
 	lastRead        time.Time
 	cancel          context.CancelFunc
-	heartbeatWg     sync.WaitGroup // Rastreia goroutine de heartbeat
+	heartbeatWg     sync.WaitGroup // Rastreia goroutine de heartbeat (vida do container)
+	readerWg        sync.WaitGroup // Rastreia goroutine de reader (vida de cada stream)
 	streamCreatedAt time.Time      // When current stream was created
 	rotationCount   int            // Number of rotations performed
 }
```

**Why**: Separate WaitGroup for reader goroutines that are created per rotation.

---

## Change 2: Update Rotation Synchronization

**Location**: Line 883

```diff
 			// Fechar stream explicitamente
 			stream.Close()
 			streamCancel()

-		// CRITICAL: Wait for reader goroutine to exit before starting new rotation
-		// Without this, reader goroutines accumulate with each rotation!
-		mc.heartbeatWg.Wait()
+			// CRITICAL: Wait for reader goroutine to exit before starting new rotation
+			// This ensures the reader goroutine from THIS rotation completes before next rotation starts
+			// This prevents reader goroutine accumulation
+			mc.readerWg.Wait()
```

**Why**: Now waits only for the reader goroutine (which finishes after each rotation), not the heartbeat goroutine (which runs forever). This prevents indefinite blocking.

---

## Change 3: Update Heartbeat Comment

**Location**: Line 811-812

```diff
 	containerCtx, cancel := context.WithCancel(ctx)
 	mc.cancel = cancel
 	defer func() {
 		cancel()
-		// Aguardar heartbeat goroutine terminar
+		// Aguardar heartbeat goroutine terminar (roda durante toda a vida do container)
 		mc.heartbeatWg.Wait()
 	}()

 	// Enviar heartbeat em goroutine separada com ticker gerenciado internamente
+	// Esta goroutine roda durante TODA a vida do container (não é recriada nas rotações)
 	taskName := "container_" + mc.id
 	mc.heartbeatWg.Add(1)
```

**Why**: Clarify that heartbeat goroutine runs for the entire container lifetime, not per rotation.

---

## Change 4: Track Reader with readerWg

**Location**: Line 955-960

```diff
 	// Context for reader goroutine with explicit cleanup
 	readerCtx, readerCancel := context.WithCancel(ctx)
 	defer readerCancel() // Ensure reader goroutine is cancelled when function exits

-	// Goroutine para ler do stream - TRACKED WITH WAITGROUP
-	mc.heartbeatWg.Add(1) // Track this goroutine
+	// Goroutine para ler do stream - TRACKED WITH READER WAITGROUP
+	// Esta goroutine é recriada a cada rotação de stream
+	mc.readerWg.Add(1) // Track this goroutine
 	go func() {
-		defer mc.heartbeatWg.Done() // Always cleanup
-		defer close(readCh)          // Close channel when exiting to unblock readers
+		defer mc.readerWg.Done() // Always cleanup
+		defer close(readCh)      // Close channel when exiting to unblock readers
```

**Why**: Reader goroutines are now tracked with their own WaitGroup, separate from the heartbeat goroutine.

---

## Summary of Changes

| Change | Line | Before | After |
|--------|------|--------|-------|
| 1. Add field | 159 | `heartbeatWg sync.WaitGroup` | + `readerWg sync.WaitGroup` |
| 2. Rotation sync | 883 | `mc.heartbeatWg.Wait()` | `mc.readerWg.Wait()` |
| 3. Reader tracking | 957 | `mc.heartbeatWg.Add(1)` | `mc.readerWg.Add(1)` |
| 4. Reader cleanup | 959 | `mc.heartbeatWg.Done()` | `mc.readerWg.Done()` |

**Total Lines Changed**: 4 functional lines + 3 comment lines = **7 lines**

---

## Before vs After: Execution Flow

### BEFORE (Buggy)

```
monitorContainer():
  ├─ heartbeatWg.Add(1)
  ├─ START: Heartbeat goroutine [LONG-LIVED]
  │    └─ defer heartbeatWg.Done() ← Never called (runs forever)
  │
  └─ Rotation Loop:
       ├─ Open stream
       ├─ heartbeatWg.Add(1)
       ├─ START: Reader goroutine [SHORT-LIVED]
       │    └─ defer heartbeatWg.Done() ← Called after 5 min
       │
       ├─ Wait for rotation timeout (5 min)
       ├─ Close stream
       │
       └─ heartbeatWg.Wait() ← ❌ BLOCKS FOREVER
                                 (waiting for heartbeat to finish)
                                 (heartbeat never finishes)

       Result: Reader goroutines accumulate!
```

### AFTER (Fixed)

```
monitorContainer():
  ├─ heartbeatWg.Add(1)
  ├─ START: Heartbeat goroutine [LONG-LIVED]
  │    └─ defer heartbeatWg.Done() ← Never called (runs forever)
  │
  └─ Rotation Loop:
       ├─ Open stream
       ├─ readerWg.Add(1)
       ├─ START: Reader goroutine [SHORT-LIVED]
       │    └─ defer readerWg.Done() ← Called after 5 min
       │
       ├─ Wait for rotation timeout (5 min)
       ├─ Close stream
       │
       └─ readerWg.Wait() ← ✅ COMPLETES IMMEDIATELY
                             (only waiting for reader)
                             (reader already finished)

       Result: Clean rotation, no accumulation!
```

---

## WaitGroup Counter States

### BEFORE (Buggy)

```
Time    | Action                  | heartbeatWg | State
--------|-------------------------|-------------|------------------
0:00    | heartbeatWg.Add(1)     |      1      | Heartbeat starts
0:00    | heartbeatWg.Add(1)     |      2      | Reader #1 starts
5:00    | heartbeatWg.Done()     |      1      | Reader #1 ends
5:00    | heartbeatWg.Wait()     |      1      | ❌ BLOCKS (≠ 0)
        |                         |             |
        | (Wait never completes) |      1      | Heartbeat still running
        | (Bypass via error)     |      1      |
        |                         |             |
5:01    | heartbeatWg.Add(1)     |      2      | Reader #2 starts
        |                         |             | Reader #1 LEAKED!
10:00   | heartbeatWg.Done()     |      1      | Reader #2 ends
10:00   | heartbeatWg.Wait()     |      1      | ❌ BLOCKS
        |                         |             |
        | ... continues leaking  |      1      | +N leaked readers
```

### AFTER (Fixed)

```
Time    | Action                  | heartbeatWg | readerWg | State
--------|-------------------------|-------------|----------|------------------
0:00    | heartbeatWg.Add(1)     |      1      |    0     | Heartbeat starts
0:00    | readerWg.Add(1)        |      1      |    1     | Reader #1 starts
5:00    | readerWg.Done()        |      1      |    0     | Reader #1 ends
5:00    | readerWg.Wait()        |      1      |    0     | ✅ COMPLETES (= 0)
        |                         |             |          |
5:00    | readerWg.Add(1)        |      1      |    1     | Reader #2 starts
        |                         |             |          | No leak!
10:00   | readerWg.Done()        |      1      |    0     | Reader #2 ends
10:00   | readerWg.Wait()        |      1      |    0     | ✅ COMPLETES
        |                         |             |          |
        | ... continues cleanly  |      1      |    0-1   | No leaks
```

---

## Testing the Fix

### 1. Verify Code Changes

```bash
# Check readerWg was added
grep "readerWg.*sync.WaitGroup" internal/monitors/container_monitor.go

# Check rotation uses readerWg
grep "mc.readerWg.Wait()" internal/monitors/container_monitor.go

# Check reader tracking uses readerWg
grep "mc.readerWg.Add(1)" internal/monitors/container_monitor.go
grep "mc.readerWg.Done()" internal/monitors/container_monitor.go
```

### 2. Build and Test

```bash
# Build
go build -o bin/log_capturer cmd/main.go

# Run tests
go test -race ./internal/monitors/... -v

# Run full suite
go test -race ./... -v
```

### 3. Validate Fix

```bash
# Run validation script
chmod +x tests/goroutine_leak_fix_validation.sh
./tests/goroutine_leak_fix_validation.sh
```

---

## Expected Behavior After Fix

### FASE 3 (8 containers)
- **Before**: -0.50 goroutines/min ✅
- **After**: 0.00 to +0.50 goroutines/min ✅
- **Result**: No regression

### FASE 6 (55 containers)
- **Before**: +30.50 goroutines/min ❌ (15.25× over limit)
- **After**: < 2.00 goroutines/min ✅ (within target)
- **Result**: Leak eliminated

---

**Date**: 2025-11-07
**Bug ID**: GOROUTINE-LEAK-001
**Lines Changed**: 7
**Files Modified**: 1
**Confidence**: 95% (HIGH)
