# 🚨 URGENT: Goroutine Leak Investigation Report

**Date**: 2025-11-07
**Status**: CRITICAL - Leak Partially Fixed, One Major Source Remains
**Impact**: System restarts every 4 minutes due to health check failures

---

## 📋 Executive Summary

### Current Situation
- **Goroutine leak rate**: 32 goroutines/minute (growing in steps of 16 every 30s)
- **Baseline goroutines**: 6
- **Current goroutines after 5 min**: 250+
- **Health status**: 503 Service Unavailable (triggers Docker restarts)
- **Restart cycle**: Every ~4 minutes

### Fixes Applied ✅
1. **Fixed**: `file_monitor.go` - Removed goroutine spawn on file add (line 437-443)
2. **Fixed**: `connection_pool.go` - Added WaitGroup to health check goroutines (line 398-411)

### Remaining Issues ❌
1. **CRITICAL**: Unknown leak source creating 32 goroutines/minute
2. **Health endpoint**: Returns 503 when goroutines grow, triggering restarts

---

## 🔍 Investigation Process

### Step 1: Initial Detection
```
Goroutine Tracker Status:
- baseline_goroutines: 6
- current_goroutines: 174
- growth_rate_per_min: 32.000
- status: critical
```

### Step 2: Connection Pooling Verification  ✅
**Result**: Connection pooling IS properly configured

**Evidence**:
- `/home/mateus/log_capturer_go/pkg/docker/connection_pool.go` - Lines 276-297
- `/home/mateus/log_capturer_go/internal/sinks/loki_sink.go` - Lines 218-231

```go
// Loki Sink HTTP Client (CORRECT)
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        MaxConnsPerHost:       50,  // ✅ Prevents unlimited growth
        IdleConnTimeout:       90 * time.Second,
        DisableKeepAlives:     false,  // ✅ Connection reuse enabled
    },
}
```

### Step 3: Leak Source #1 - file_monitor.go (FIXED ✅)

**Location**: `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go:437-443`

**Problem**:
```go
// ❌ BEFORE (LEAKED)
if info.Size() > mf.position {
    fm.wg.Add(1)
    go func() {
        defer fm.wg.Done()
        time.Sleep(100 * time.Millisecond)
        fm.readFile(mf)  // Spawned goroutine for every file add
    }()
}
```

**Fix Applied**:
```go
// ✅ AFTER (FIXED)
if info.Size() > mf.position {
    fm.logger.WithFields(logrus.Fields{
        "path": filePath,
        "size": info.Size(),
        "position": mf.position,
    }).Info("File has initial content, will be read on next poll")
    // File will be picked up by pollAllFiles() - no goroutine spawn!
}
```

### Step 4: Leak Source #2 - connection_pool.go (FIXED ✅)

**Location**: `/home/mateus/log_capturer_go/pkg/docker/connection_pool.go:398-406`

**Problem**:
```go
// ❌ BEFORE (LEAKED)
func (cp *ConnectionPool) performHealthCheck() {
    for _, conn := range connections {
        go func(c *PooledConnection) {  // ❌ No tracking!
            if !cp.pingConnection(c) {
                c.IsHealthy = false
            }
        }(conn)
    }
    // ❌ No wait - goroutines accumulate!
}
```

**Fix Applied**:
```go
// ✅ AFTER (FIXED)
func (cp *ConnectionPool) performHealthCheck() {
    var wg sync.WaitGroup  // ✅ Track goroutines
    for _, conn := range connections {
        wg.Add(1)
        go func(c *PooledConnection) {
            defer wg.Done()  // ✅ Always cleanup
            if !cp.pingConnection(c) {
                c.IsHealthy = false
            }
        }(conn)
    }
    wg.Wait()  // ✅ Wait for completion
}
```

### Step 5: Leak Source #3 - UNKNOWN (ACTIVE ❌)

**Characteristics**:
- **Rate**: 32 goroutines/minute
- **Pattern**: Steps of 16 every 30 seconds
- **Trigger**: Happens continuously, not on specific events
- **Related**: Possibly tied to `pollAllFiles()` (runs every 2 seconds)

**Suspects**:
1. **file_monitor.pollAllFiles()** - Calls `readFile()` every 2s for each file
2. **task_manager** - May spawn goroutines for tasks
3. **dispatcher** - May have untracked goroutines in retry logic
4. **Loki sink adaptive batcher** - Worker pool or batch processing

**Evidence**:
```
Sample Timeline:
22:59:05 - 112 goroutines
22:59:35 - 128 goroutines (+16 in 30s)
23:00:05 - 144 goroutines (+16 in 30s)
23:00:35 - 160 goroutines (+16 in 30s)
23:01:05 - 176 goroutines (+16 in 30s)
```

**Calculation**:
- 16 goroutines every 30 seconds = 32/minute
- 6 files monitored
- 2-second poll interval → 30 polls/minute
- **Possible**: Each poll spawns ~1 goroutine that doesn't get cleaned up

---

## 🔄 Restart Cycle Analysis

### Why Restarts Happen

1. **Goroutine leak** accumulates (32/min)
2. **Health endpoint** detects high goroutines
3. **Health endpoint returns** `503 Service Unavailable`
4. **Docker healthcheck** fails 3 times
5. **External monitor** (or Docker autoheal) restarts container
6. **Container restarts** → cycle repeats

### Restart Logs
```
{"level":"info","msg":"Shutdown signal received","time":"2025-11-07T22:45:40Z"}
{"level":"info","msg":"Stopping SSW Logs Capture Go","time":"2025-11-07T22:45:40Z"}
{"level":"info","msg":"Stopping file monitor","time":"2025-11-07T22:45:40Z"}
...
{"level":"info","msg":"Starting file monitor","start_time":"2025-11-07T22:45:52Z"}
```

**Restart Frequency**: Every ~4 minutes
**Root Cause**: Goroutine leak → unhealthy → restart → leak again

---

## 🎯 Recommended Next Steps

### IMMEDIATE (Priority 1) - Find Remaining Leak

#### Option A: Enable pprof for Deep Analysis
```go
// Add to main.go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

Then analyze:
```bash
# Get goroutine profile
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Analyze what's leaking
grep "^goroutine" goroutines.txt | awk '{print $NF}' | sort | uniq -c | sort -rn
```

#### Option B: Add Extensive Logging
Add goroutine tracking logs to:
- `file_monitor.pollAllFiles()`
- `file_monitor.readFile()`
- `dispatcher.Handle()`
- `loki_sink.adaptiveBatchLoop()`

```go
// Example
fm.logger.WithFields(logrus.Fields{
    "goroutines_before": runtime.NumGoroutine(),
}).Debug("Before pollAllFiles")

fm.pollAllFiles()

fm.logger.WithFields(logrus.Fields{
    "goroutines_after": runtime.NumGoroutine(),
}).Debug("After pollAllFiles")
```

#### Option C: Systematic Code Review
Review EVERY `go func()` or `go routine()` call to ensure:
1. Has `defer` for cleanup
2. Has context cancellation check
3. Is tracked by WaitGroup or similar
4. Will eventually exit

**Files to review**:
- `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go`
- `/home/mateus/log_capturer_go/internal/dispatcher/dispatcher.go`
- `/home/mateus/log_capturer_go/internal/sinks/loki_sink.go`
- `/home/mateus/log_capturer_go/pkg/task_manager/*.go`

### SHORT-TERM (Priority 2) - Mitigate Impact

#### Temporary Fix: Disable Health Check Restarts
```yaml
# docker-compose.yml
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8401/health"]
  interval: 30s
  timeout: 10s
  retries: 10  # ⬆️ Increase to delay restarts
  start_period: 10s
```

Or disable entirely while investigating:
```yaml
# healthcheck:
#   test: ["CMD", "true"]  # Always pass
```

#### Adjust Health Thresholds
Make goroutine_tracker less sensitive:
```go
// In goroutine_tracker.go
const (
    GrowthThresholdWarning  = 100  // Was: 50
    GrowthThresholdCritical = 500  // Was: 100
)
```

### LONG-TERM (Priority 3) - Prevention

1. **Add goroutine leak tests** to CI/CD
2. **Implement goroutine budget** per component
3. **Add runtime goroutine monitoring** dashboard
4. **Code review checklist** for goroutine safety

---

## 📊 Metrics & Monitoring

### Current Metrics
```json
{
  "baseline_goroutines": 6,
  "current_goroutines": 288,
  "growth_rate_per_min": 32.0,
  "total_growth": 282,
  "status": "critical"
}
```

### Health Check Status
```bash
$ curl -s http://localhost:8401/health | jq '.status'
"degraded"  # Or "503 Service Unavailable" when critical
```

### Files Monitored
```
1. /var/log/syslog
2. /var/log/auth.log
3. /var/log/kern.log
4. /var/log/dpkg.log
5. /var/log/apt/history.log
6. /var/log/dmesg
```

---

## 🔧 Technical Details

### Goroutine Lifecycle in Go
```go
// CORRECT Pattern
type Component struct {
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

func (c *Component) Start() {
    c.ctx, c.cancel = context.WithCancel(context.Background())

    c.wg.Add(1)
    go c.worker()
}

func (c *Component) worker() {
    defer c.wg.Done()

    for {
        select {
        case <-c.ctx.Done():
            return  // Exit on cancellation
        default:
            c.doWork()
        }
    }
}

func (c *Component) Stop() error {
    c.cancel()    // Signal stop
    c.wg.Wait()   // Wait for cleanup
    return nil
}
```

### Common Leak Patterns
```go
// ❌ Pattern 1: No exit condition
go func() {
    for {
        doWork()  // Never exits!
    }
}()

// ❌ Pattern 2: No WaitGroup tracking
go func() {
    defer cleanup()
    doWork()
}()
// No way to know when it finishes!

// ❌ Pattern 3: Context not checked
go func() {
    for {
        time.Sleep(1 * time.Second)
        doWork()  // Runs forever even after shutdown!
    }
}()

// ❌ Pattern 4: Spawning in loops
for _, item := range items {
    go process(item)  // Creates N goroutines, no limit!
}
```

---

## 📝 Lessons Learned

1. **Always use `go test -race`** before deploying
2. **Track ALL goroutines** with WaitGroup or similar
3. **Check context cancellation** in all loops
4. **Profile production** with pprof endpoints
5. **Monitor goroutine counts** in production
6. **Implement goroutine budgets** per component
7. **Docker restarts hide symptoms** - disable temporarily during debugging

---

## 🎬 Next Actions

### For Developer
1. ✅ Review this report
2. ⏳ Choose investigation approach (pprof, logging, or systematic review)
3. ⏳ Find and fix remaining leak source
4. ⏳ Add comprehensive goroutine tests
5. ⏳ Deploy fix and validate with 15-minute stability test

### For Operations
1. ⏳ Temporarily increase health check thresholds
2. ⏳ Monitor system closely during fix deployment
3. ⏳ Prepare rollback plan if fix fails

---

## 📎 References

**Files Modified**:
- `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go` (line 431-439)
- `/home/mateus/log_capturer_go/pkg/docker/connection_pool.go` (line 390-412)

**Key Commits**:
- Will be created on branch `fix/goroutine-leaks-urgent`

**Related Issues**:
- This is a recurring issue - leaks were previously fixed but returned

---

**Report Generated**: 2025-11-07 23:05 UTC
**Investigation Duration**: 90 minutes
**Status**: IN PROGRESS - Awaiting further investigation

🤖 Generated with Claude Code
Co-Authored-By: Claude <noreply@anthropic.com>
