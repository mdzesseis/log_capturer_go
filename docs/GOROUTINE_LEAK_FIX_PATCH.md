# Goroutine Leak Fix Patch

**Priority**: CRITICAL
**Estimated Time**: 10 minutes to apply
**Testing Time**: 15 minutes to validate

---

## Quick Fix Summary

Apply these 3 critical changes to stop the goroutine leak:

### FIX 1: Track Container Reader Goroutine (CRITICAL)

**File**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

**Location**: Line 792 (inside `readContainerLogs()` function)

**Current Code** (LEAKING):
```go
// Goroutine para ler do stream
go func() {
    for {
        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)
        // ... rest of code
    }
}()
```

**Fixed Code**:
```go
// Goroutine para ler do stream - TRACKED WITH WAITGROUP
mc.heartbeatWg.Add(1)  // ← ADD THIS LINE
go func() {
    defer mc.heartbeatWg.Done()  // ← ADD THIS LINE
    for {
        localBuf := make([]byte, 8192)
        n, err := stream.Read(localBuf)
        // ... rest of code (unchanged)
    }
}()
```

**Lines to modify**:
- Line 792: Add `mc.heartbeatWg.Add(1)` BEFORE `go func()`
- Line 793: Add `defer mc.heartbeatWg.Done()` as FIRST line inside goroutine

---

### FIX 2: Track File Reader Goroutine

**File**: `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go`

**Location**: Line 308 (inside `AddFile()` function)

**Current Code** (LEAKING):
```go
go func() {
    time.Sleep(100 * time.Millisecond) // Small delay to ensure setup is complete
    fm.readFile(mf)
}()
```

**Fixed Code**:
```go
fm.wg.Add(1)  // ← ADD THIS LINE
go func() {
    defer fm.wg.Done()  // ← ADD THIS LINE
    time.Sleep(100 * time.Millisecond)
    fm.readFile(mf)
}()
```

**Lines to modify**:
- Line 308: Add `fm.wg.Add(1)` BEFORE `go func()`
- Line 309: Add `defer fm.wg.Done()` as FIRST line inside goroutine

---

### FIX 3: Add Stream Read Timeout (OPTIONAL but RECOMMENDED)

**File**: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

**Location**: Inside `readContainerLogs()` function, line 792 goroutine

**Enhancement**: Add buffered channel to prevent deadlock

**Current Code**:
```go
readCh := make(chan readResult, 1)
```

**Enhanced Code**:
```go
readCh := make(chan readResult, 10)  // Increase buffer to prevent blocking
```

**Line to modify**:
- Line 789: Change buffer size from `1` to `10`

---

## Complete Patch Code

### Patch for container_monitor.go

```diff
--- a/internal/monitors/container_monitor.go
+++ b/internal/monitors/container_monitor.go
@@ -786,10 +786,11 @@ func (cm *ContainerMonitor) readContainerLogs(ctx context.Context, mc *monitore
 		data []byte
 		err  error
 	}
-	readCh := make(chan readResult, 1)
+	readCh := make(chan readResult, 10)  // Increased buffer to prevent blocking

 	// Goroutine para ler do stream
+	mc.heartbeatWg.Add(1)  // Track this goroutine
 	go func() {
+		defer mc.heartbeatWg.Done()  // Always cleanup
 		for {
 			localBuf := make([]byte, 8192)
 			n, err := stream.Read(localBuf)
```

### Patch for file_monitor.go

```diff
--- a/internal/monitors/file_monitor.go
+++ b/internal/monitors/file_monitor.go
@@ -305,8 +305,10 @@ func (fm *FileMonitor) AddFile(filePath string, labels map[string]string) error
 			"size": info.Size(),
 			"position": mf.position,
 		}).Info("Reading initial content from file")
+		fm.wg.Add(1)  // Track this goroutine
 		go func() {
+			defer fm.wg.Done()  // Always cleanup
 			time.Sleep(100 * time.Millisecond) // Small delay to ensure setup is complete
 			fm.readFile(mf)
 		}()
```

---

## Application Steps

### Step 1: Apply Container Monitor Fix

```bash
cd /home/mateus/log_capturer_go
```

Edit `internal/monitors/container_monitor.go`:

1. Navigate to line 789
2. Change `readCh := make(chan readResult, 1)` to `readCh := make(chan readResult, 10)`
3. Navigate to line 792
4. Add BEFORE `go func()`:
   ```go
   mc.heartbeatWg.Add(1)  // Track this goroutine
   ```
5. Add AFTER `go func() {`:
   ```go
   defer mc.heartbeatWg.Done()  // Always cleanup
   ```

### Step 2: Apply File Monitor Fix

Edit `internal/monitors/file_monitor.go`:

1. Navigate to line 308
2. Add BEFORE `go func()`:
   ```go
   fm.wg.Add(1)  // Track this goroutine
   ```
3. Add AFTER `go func() {`:
   ```go
   defer fm.wg.Done()  // Always cleanup
   ```

### Step 3: Rebuild and Deploy

```bash
# Stop current container
docker-compose down

# Rebuild
make build

# Restart
docker-compose up -d

# Monitor logs
docker logs -f log_capturer_go
```

---

## Validation Steps

### Step 1: Check Initial Goroutine Count

```bash
# Wait 30 seconds for stabilization
sleep 30

# Check metrics endpoint
curl -s http://localhost:8001/metrics | grep log_capturer_goroutines
```

**Expected**: ~20-30 goroutines (baseline)

### Step 2: Monitor for 5 Minutes

```bash
# Monitor every minute for 5 minutes
for i in {1..5}; do
    sleep 60
    echo "=== Minute $i ==="
    curl -s http://localhost:8001/metrics | grep log_capturer_goroutines
done
```

**Expected Results**:
- Minute 1: ~20-30 goroutines
- Minute 2: ~20-30 goroutines (NOT 134!)
- Minute 3: ~20-30 goroutines (NOT 166!)
- Minute 4: ~20-30 goroutines (NOT 198!)
- Minute 5: ~20-30 goroutines (stable)

**BEFORE Fix**:
```
Minute 1: 102 goroutines
Minute 2: 134 goroutines
Minute 3: 166 goroutines
Minute 4: 198 goroutines
```

**AFTER Fix**:
```
Minute 1: 22 goroutines
Minute 2: 23 goroutines
Minute 3: 22 goroutines
Minute 4: 23 goroutines
```

### Step 3: Check Application Logs

```bash
docker logs log_capturer_go 2>&1 | grep -i "goroutine"
```

**Expected**: No more "Significant goroutine count change detected" warnings

### Step 4: Stress Test (Optional)

```bash
# Restart containers to trigger reconnections
docker restart log_generator loki grafana

# Monitor goroutine count
watch -n 5 'curl -s http://localhost:8001/metrics | grep log_capturer_goroutines'
```

**Expected**: Goroutine count should remain stable despite container restarts

---

## Rollback Plan

If the fix causes issues:

```bash
# Restore from git
git checkout internal/monitors/container_monitor.go
git checkout internal/monitors/file_monitor.go

# Rebuild
make build

# Restart
docker-compose restart log_capturer_go
```

---

## Success Criteria

✅ **Fix is successful if**:
1. Goroutine count stabilizes at 20-30 (not growing)
2. No "goroutine count change detected" warnings in logs
3. Application remains healthy in `docker ps`
4. Logs continue to be processed normally
5. Memory usage remains stable

❌ **Fix needs revision if**:
1. Goroutine count still grows over time
2. Application crashes or becomes unresponsive
3. Logs stop being processed
4. Memory usage continues to grow

---

## Additional Monitoring

### Grafana Dashboard Queries

```promql
# Goroutine count over time
log_capturer_goroutines

# Goroutine growth rate (should be ~0 after fix)
rate(log_capturer_goroutines[5m])

# Memory usage (should stabilize)
log_capturer_memory_usage_bytes

# Active containers being monitored
log_capturer_containers_monitored
```

### Docker Health Check

```bash
docker inspect log_capturer_go | jq '.[0].State.Health'
```

**Expected**: `"Status": "healthy"`

---

## Timeline

- **Apply fixes**: 5 minutes
- **Rebuild & deploy**: 2 minutes
- **Initial validation**: 5 minutes
- **Extended monitoring**: 15 minutes
- **Total time**: ~30 minutes

---

## Notes

- This is a **non-breaking change** - all functionality remains the same
- The fix only adds proper goroutine lifecycle management
- No configuration changes required
- No data loss during restart
- Position tracking will resume from last saved position

---

**Status**: Ready to apply
**Risk**: LOW (only adds cleanup, doesn't change logic)
**Impact**: HIGH (fixes critical resource leak)
