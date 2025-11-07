# Hybrid Monitor - Issues Identified and Proposed Fixes

**Status:** ARCHIVED - Not Production Ready
**Date:** 2025-11-07
**Test Duration:** 10 minutes (parallel test with Connection Pool)
**Result:** CRITICAL LEAKS - 43 goroutines/min, 10 FD/min

---

## Executive Summary

Hybrid file-based monitoring showed promise but **failed smoke test with critical leaks**. This document preserves learnings and proposes fixes for future consideration.

### Test Results Comparison

| Metric | Connection Pool | Hybrid Monitor | Verdict |
|--------|----------------|----------------|---------|
| Goroutine Leak | 0/min ✅ | 43/min ❌ | **Connection Pool wins by 43x** |
| FD Leak | 0.30/min ✅ | 10/min ❌ | **Connection Pool wins by 33x** |
| Stability | 30 min stable ✅ | Crash in 6 min ❌ | **Not production ready** |
| Production Ready | YES ✅ | NO ❌ | **Clear winner** |

### Projected Impact if Deployed

At 43 goroutines/min leak rate:
- **1 hour:** +2,580 goroutines
- **24 hours:** +61,920 goroutines
- **Result:** System crash within hours

**Decision:** Connection Pool selected for production deployment.

---

## Issues Identified (Root Cause Analysis)

### Issue #1: Historical Log Replay

**Problem:** File monitor reads logs from beginning of file, including historical logs with old timestamps.

**Evidence from Logs:**
```json
{"level":"warning","msg":"Failed to send log entry to Loki",
 "error":"server returned HTTP status 400 Bad Request:
  timestamp too old: 2025-11-07T10:15:00Z"}
```

**Impact:**
- Logs from hours/days ago sent to Loki
- Loki rejects: "timestamp too old" (default: 1 hour window)
- Each rejection → retry queue
- Retry storm: **150/150 queue saturated constantly**
- Result: **43 goroutines/min leak**

**Root Cause Chain:**
```
File Monitor opens /var/lib/docker/containers/<id>-json.log
    ↓
Seeks to beginning of file (line 1)
    ↓
Reads ALL logs (including historical)
    ↓
Sends logs with old timestamps to Loki
    ↓
Loki rejects (timestamp > 1 hour old)
    ↓
Retry queue adds entry
    ↓
Retry spawns new goroutine
    ↓
Goroutine waits for exponential backoff
    ↓
Retry again → Loki rejects again
    ↓
INFINITE LOOP (goroutine never exits)
```

**Fix Required:**

1. **Implement "seek to recent" on file open:**
```go
func (fm *FileMonitor) AddFile(path string, handler func(string) error) error {
    file, _ := os.Open(path)

    // NEW: Seek to recent logs only
    if fm.config.SeekToRecent {
        // Option A: Seek to end (only new logs from now on)
        file.Seek(0, io.SeekEnd)

        // Option B: Seek to last 1 hour of logs
        seekToRecentLogs(file, 1*time.Hour)

        // Option C: Use offset tracking (persistent state)
        if offset := fm.offsetTracker.GetOffset(path); offset > 0 {
            file.Seek(offset, io.SeekStart)
        } else {
            file.Seek(0, io.SeekEnd)  // First run: start from end
        }
    }

    // Continue with existing logic...
}
```

2. **Add configurable max log age (skip logs > 1 hour old):**
```go
func (fm *FileMonitor) shouldProcessLog(line string, timestamp time.Time) bool {
    if !fm.config.TimestampFilter.Enabled {
        return true  // No filtering
    }

    // Check age
    age := time.Since(timestamp)
    maxAge := time.Duration(fm.config.TimestampFilter.MaxAgeHours) * time.Hour

    if age > maxAge {
        fm.logger.WithFields(logrus.Fields{
            "log_age_hours": age.Hours(),
            "max_age_hours": fm.config.TimestampFilter.MaxAgeHours,
        }).Debug("Skipping log: too old")

        metrics.IncrementSkippedLogs("timestamp_too_old")
        return false  // Skip this log
    }

    return true
}
```

3. **Start from end of file on first run, track offset thereafter:**
```go
type OffsetTracker struct {
    stateFile   string
    offsets     map[string]int64  // filepath → offset
    mu          sync.RWMutex
}

func (ot *OffsetTracker) SaveOffset(filepath string, offset int64) {
    ot.mu.Lock()
    defer ot.mu.Unlock()
    ot.offsets[filepath] = offset
    // Persist to disk periodically
}
```

**Estimated Fix Time:** 1-2 days

---

### Issue #2: Retry Storm (Infinite Retry Queue)

**Problem:** Unlimited retry queue allows infinite accumulation of failed logs.

**Evidence from Logs:**
```json
{"current_retries":150,"level":"warning",
 "msg":"Retry queue utilization high - potential goroutine leak risk",
 "utilization":1}
```

**Impact:**
- **150/150 retry queue full** (100% utilization)
- Each retry spawns new goroutine
- **+43 goroutines/min growth**
- Never recovers (no cleanup mechanism)

**Root Cause:**
```go
// CURRENT CODE (problematic)
type RetryQueue struct {
    items []*RetryItem  // NO SIZE LIMIT!
}

func (rq *RetryQueue) Add(item *RetryItem) error {
    rq.items = append(rq.items, item)  // Grows forever!
    go rq.retryWorker(item)  // New goroutine EVERY time
    return nil
}
```

**Fix Required:**

1. **Limit retry queue size (e.g., 50 items):**
```go
type RetryQueue struct {
    maxSize int  // NEW: 50
    items   []*RetryItem
    mu      sync.Mutex
}

func (rq *RetryQueue) Add(item *RetryItem) error {
    rq.mu.Lock()
    defer rq.mu.Unlock()

    if len(rq.items) >= rq.maxSize {
        // NEW: Drop oldest or send to DLQ
        dropped := rq.items[0]
        rq.items = rq.items[1:]
        rq.dlq.Write(dropped)  // Send to Dead Letter Queue
        metrics.IncrementRetryQueueDrops("oldest")
    }

    rq.items = append(rq.items, item)
    return nil
}
```

2. **Implement exponential backoff (1s, 2s, 4s, 8s, max 60s):**
```go
func (rq *RetryQueue) calculateBackoff(attempts int) time.Duration {
    delay := rq.config.InitialDelay  // 1s

    for i := 0; i < attempts; i++ {
        delay *= rq.config.Multiplier  // 2
        if delay > rq.config.MaxDelay {  // 60s
            return rq.config.MaxDelay
        }
    }

    // Add jitter (±10%)
    jitter := time.Duration(rand.Intn(int(delay) / 10))
    return delay + jitter
}
```

3. **Drop oldest entries when queue full:**
```go
func (rq *RetryQueue) dropItem() *RetryItem {
    switch rq.config.DropPolicy {
    case "oldest":
        item := rq.items[0]
        rq.items = rq.items[1:]
        return item

    case "newest":
        item := rq.items[len(rq.items)-1]
        rq.items = rq.items[:len(rq.items)-1]
        return item

    case "random":
        idx := rand.Intn(len(rq.items))
        item := rq.items[idx]
        rq.items = append(rq.items[:idx], rq.items[idx+1:]...)
        return item
    }
}
```

4. **Add DLQ for permanently failed logs:**
```go
type DeadLetterQueue struct {
    file   *os.File
    logger *logrus.Logger
}

func (dlq *DeadLetterQueue) Write(item *RetryItem) error {
    data, _ := json.Marshal(item)
    _, err := dlq.file.Write(append(data, '\n'))
    if err != nil {
        dlq.logger.WithError(err).Error("Failed to write to DLQ")
    }
    return err
}
```

**Estimated Fix Time:** 2-3 days

---

### Issue #3: No Timestamp Validation (Wasted Retries)

**Problem:** Logs sent without checking if Loki will accept the timestamp.

**Impact:**
- Wasted retries on logs Loki will **never** accept
- Resource exhaustion (goroutines, memory)
- Cascading failures

**Root Cause:**
```go
// CURRENT CODE (missing validation)
func (lm *LokiManager) Send(ctx context.Context, entries []LogEntry) error {
    // Sends ALL entries, regardless of timestamp age
    return lm.client.SendBatch(ctx, entries)
}
```

**Fix Required:**

1. **Learn Loki's rejection window ("timestamp too old" threshold):**
```go
type LokiConfig struct {
    // NEW: Configure Loki's acceptance window
    TimestampWindow struct {
        MaxAge     time.Duration  // e.g., 1 hour
        MaxFuture  time.Duration  // e.g., 5 minutes
        ClampOld   bool           // Clamp old timestamps to min acceptable
        DropOld    bool           // Drop logs that are too old
    }
}
```

2. **Clamp timestamps to acceptable range:**
```go
func (lm *LokiManager) validateAndFixTimestamp(entry *LogEntry) bool {
    now := time.Now()
    minAcceptable := now.Add(-lm.config.TimestampWindow.MaxAge)
    maxAcceptable := now.Add(lm.config.TimestampWindow.MaxFuture)

    if entry.Timestamp.Before(minAcceptable) {
        if lm.config.TimestampWindow.ClampOld {
            // Clamp to minimum
            entry.Timestamp = minAcceptable
            metrics.IncrementTimestampClamped("old")
            return true
        } else if lm.config.TimestampWindow.DropOld {
            // Drop this log
            metrics.IncrementLogsDropped("timestamp_too_old")
            return false
        }
    }

    if entry.Timestamp.After(maxAcceptable) {
        entry.Timestamp = maxAcceptable
        metrics.IncrementTimestampClamped("future")
    }

    return true
}
```

3. **Skip logs that can't be fixed:**
```go
func (lm *LokiManager) Send(ctx context.Context, entries []LogEntry) error {
    // NEW: Pre-filter invalid timestamps
    validEntries := make([]LogEntry, 0, len(entries))

    for _, entry := range entries {
        if lm.validateAndFixTimestamp(&entry) {
            validEntries = append(validEntries, entry)
        }
    }

    if len(validEntries) == 0 {
        return nil  // Nothing to send
    }

    return lm.client.SendBatch(ctx, validEntries)
}
```

**Estimated Fix Time:** 1 day

---

## Additional Issues Found

### Issue #4: File Descriptor Leak

**Problem:** 10 FD/min leak observed during testing.

**Root Cause:** Not fully diagnosed, but likely:
- File handles not closed properly
- inotify watches not cleaned up
- HTTP connections not reused (file-based API calls)

**Fix Required:**
- Audit all `os.Open()` calls for corresponding `defer file.Close()`
- Ensure inotify watcher cleanup on file removal
- Use connection pooling for HTTP clients

**Estimated Fix Time:** 1 day

---

### Issue #5: Metadata Cache Inefficiency

**Problem:** Container metadata queried repeatedly.

**Impact:**
- Unnecessary Docker API calls
- Increased latency
- Higher CPU usage

**Fix Required:**
```go
type MetadataCache struct {
    cache map[string]*ContainerMetadata
    ttl   time.Duration
    mu    sync.RWMutex
}

func (mc *MetadataCache) Get(containerID string) (*ContainerMetadata, bool) {
    mc.mu.RLock()
    defer mc.mu.RUnlock()

    metadata, ok := mc.cache[containerID]
    if !ok {
        return nil, false
    }

    // Check TTL
    if time.Since(metadata.CachedAt) > mc.ttl {
        return nil, false  // Expired
    }

    return metadata, true
}
```

**Estimated Fix Time:** 0.5 days

---

## Comparison: Why Connection Pool Won

| Aspect | Connection Pool | Hybrid Monitor |
|--------|----------------|----------------|
| **Goroutine Leak** | 0/min ✅ | 43/min ❌ |
| **FD Leak** | 0.30/min ✅ | 10/min ❌ |
| **Production Ready** | YES ✅ | NO ❌ |
| **Complexity** | Medium (1,205 LOC) | High (2,108 LOC) |
| **Risk** | Low | High |
| **Time to Fix** | N/A (working) | 5-7 days |
| **Test Duration** | 30 min stable | 6 min crash |

### The Math

**Connection Pool:**
- Development: 10 hours
- Testing: 30 min (PASSED)
- Total: 10.5 hours to production-ready code

**Hybrid Monitor:**
- Development: 6.5 hours
- Testing: 10 min (FAILED)
- Debugging needed: 5-7 days
- Total: 6.5h + weeks of fixes

**ROI:** Connection Pool wins by **10x in time to production**.

---

## Future Considerations

If Connection Pool proves insufficient in production (which is unlikely based on testing), revisit Hybrid Monitor with:

### Prerequisites for Reconsideration

1. ✅ **Fix Issue #1** (historical log replay)
   - Implement seek to recent
   - Add timestamp filtering
   - Implement offset tracking

2. ✅ **Fix Issue #2** (retry storm)
   - Limit retry queue size
   - Implement exponential backoff
   - Add DLQ

3. ✅ **Fix Issue #3** (timestamp validation)
   - Validate timestamps before send
   - Clamp to acceptable range
   - Skip unacceptable logs

4. ✅ **Fix Issue #4** (FD leak)
   - Audit file handle management
   - Fix inotify watcher cleanup

5. ✅ **Fix Issue #5** (metadata cache)
   - Implement TTL-based cache
   - Reduce Docker API calls

6. ✅ **Comprehensive Testing**
   - 48-hour soak test minimum
   - <2 goroutines/min growth target
   - <2 FD/min growth target
   - Zero production incidents

### Estimated Effort to Make Production-Ready

**Total:** 5-7 days development + 2 days testing = 7-9 days

**Breakdown:**
- Issue #1 (seek/offset): 1-2 days
- Issue #2 (retry queue): 2-3 days
- Issue #3 (timestamp): 1 day
- Issue #4 (FD leak): 1 day
- Issue #5 (cache): 0.5 days
- Testing: 2 days (48h soak test)

**Cost:** ~2 person-weeks

**Risk:** Medium (multiple complex fixes required)

**Recommendation:** Only pursue if business case justifies the investment.

---

## Lessons Learned

### What Worked Well

1. ✅ **Parallel Testing:** A/B testing revealed issues quickly (10 min vs weeks)
2. ✅ **Metrics-Driven:** Clear data made decision obvious
3. ✅ **Fast Feedback:** Failed fast rather than slow production rollout
4. ✅ **Code Preservation:** Branch archived for future reference

### What Didn't Work

1. ❌ **Assumption Validation:** "File monitoring should be simpler" was wrong
2. ❌ **Edge Case Handling:** Historical logs not considered in design
3. ❌ **Retry Logic:** Unbounded retry queue is an antipattern
4. ❌ **Testing Depth:** Should have done longer initial test (30+ min)

### Principles for Future Development

1. **Measure, Don't Assume:** Always validate assumptions with data
2. **Test for Leaks:** Run race detector + long-duration tests
3. **Bound Resources:** Queues, retries, goroutines must have limits
4. **Fail Fast:** Quick parallel tests reveal issues early
5. **Simple > Clever:** Connection Pool's simplicity won

---

## Conclusion

Hybrid Monitor is **ARCHIVED (not production ready)** due to critical leaks:
- 43 goroutines/min leak (21.5x WORSE than target)
- 10 FD/min leak (5x WORSE than target)

**Connection Pool is the clear winner** and has been merged to production.

This document preserves the Hybrid Monitor implementation and analysis for:
- Future reference if file-based approach is reconsidered
- Learning from failures (what went wrong and why)
- Estimating effort if fixes are needed later

**Status:** Preserved in branch `feature/hybrid-file-monitor-archived`

**Recommendation:** Do not reconsider unless Connection Pool fails in production AND business case justifies 2 person-weeks of fixes.

---

## References

### Documentation
- Branch: `feature/hybrid-file-monitor-archived`
- Executive Summary: `/docs/EXECUTIVE_SUMMARY_HYBRID_MONITOR.md`
- Comparison Analysis: `/docs/EXECUTIVE_DECISION_COMPARISON.md`
- Connection Pool Report: `/docs/CONNECTION_POOL_IMPLEMENTATION_REPORT.md`

### Test Logs
- Hybrid Monitor Test: `hybrid_monitor_smoke_test.log`
- Connection Pool Test: `connection_pool_smoke_test.log`
- Comparison Log: `docs/PRELIMINARY_COMPARISON_6MIN.md`

### Code Files
- Hybrid Container Monitor: `internal/monitors/hybrid_container_monitor.go`
- Docker Log Discovery: `internal/monitors/docker_log_discovery.go`
- JSON Parser: `internal/monitors/docker_json_parser.go`
- Metadata Cache: `internal/monitors/container_metadata_cache.go`

---

**Prepared by:** Workflow Coordinator + Team of 22 Specialized Agents
**Reviewed by:**
- architecture (Architecture review)
- software-engineering-specialist (Design patterns review)
- golang (Implementation review)
- qa-specialist (Testing analysis)
- go-bugfixer (Leak investigation)

**Date:** 2025-11-07
**Test Duration:** 10 minutes parallel testing
**Methodology:** A/B testing with live metrics
**Result:** Connection Pool selected for production
