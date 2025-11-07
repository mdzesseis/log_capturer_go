# FASE 6H - Timing Diagrams and Flow Analysis

**Date**: 2025-11-07
**Related**: FASE6H_CODE_REVIEW.md

---

## Diagram 1: Task Timeout Problem (Issue #1)

### Current Behavior (BROKEN)

```
Timeline:
0s        Container task starts
          ├── monitorContainer() begins
          ├── Creates 30s stream
          └── Reads logs...

30s       Stream times out (DeadlineExceeded)
          ├── Rotates to new stream
          └── Reads logs...
          ⚠️  NO HEARTBEAT SENT

60s       Second stream rotation
          └── Still no heartbeat...

90s       Third stream rotation
          └── Still no heartbeat...

120s      Fourth stream rotation
          └── Still no heartbeat...

150s      Fifth stream rotation
          └── Still no heartbeat...

180s      Sixth stream rotation
          └── Still no heartbeat...

5min      Task Manager cleanup runs
(300s)    ├── Checks: last_heartbeat = 0s (task start time)
          ├── Timeout = now - last_heartbeat = 300s > 300s ❌
          ├── Decision: TIMEOUT!
          └── Calls task.Cancel()

~6min     Container context cancelled
(360s)    ├── monitorContainer() receives ctx.Done()
          ├── Returns nil
          └── Container monitoring STOPS

Result: Container stops being monitored despite working correctly!
```

### Fixed Behavior (CORRECT)

```
Timeline:
0s        Container task starts
          ├── monitorContainer() begins
          ├── Creates heartbeat ticker (30s)
          └── Creates 30s stream

30s       ✅ HEARTBEAT SENT (ticker fires)
          └── Task Manager: last_heartbeat = 30s

30s       Stream times out (DeadlineExceeded)
          ├── ✅ HEARTBEAT SENT (after rotation)
          ├── Rotates to new stream
          └── Reads logs...

60s       ✅ HEARTBEAT SENT (ticker fires)
          └── Task Manager: last_heartbeat = 60s

60s       Stream times out
          ├── ✅ HEARTBEAT SENT (after rotation)
          └── Rotates to new stream

90s       ✅ HEARTBEAT SENT
          ...continues indefinitely...

5min      Task Manager cleanup runs
(300s)    ├── Checks: last_heartbeat = 270s
          ├── Timeout = now - last_heartbeat = 30s < 300s ✅
          └── Decision: Task healthy, continue

Result: Container monitoring continues indefinitely!
```

---

## Diagram 2: Goroutine Lifecycle Problem (Issue #2)

### Expected Behavior (DOESN'T WORK)

```
monitorContainer()
│
├─ Creates 30s stream
│
├─ Calls readContainerLogsShortLived()
│  │
│  ├─ Extracts net.Conn via extractNetConn()
│  │  └─ ✅ Returns connection
│  │
│  ├─ Sets read deadline: conn.SetReadDeadline(now + 30s)
│  │  └─ ✅ Kernel-level timeout configured
│  │
│  └─ Spawns reader goroutine:
│     │
│     ├─ buf := make([]byte, 8192)
│     ├─ n, err := stream.Read(buf)
│     │  │
│     │  └─ [Blocks in kernel syscall]
│     │     │
│     │     30s later...
│     │     │
│     │     └─ Kernel wakes up: "read deadline exceeded"
│     │
│     ├─ if netErr.Timeout() {
│     │     readCh <- result{err: timeout}
│     │     return  // ✅ Goroutine exits cleanly at 30s
│     │  }
│     │
│     └─ DONE (goroutine terminates)
│
└─ Main loop receives timeout, rotates stream

Result: No goroutine leak!
```

### Actual Behavior (BROKEN)

```
monitorContainer()
│
├─ Creates 30s stream
│
├─ Calls readContainerLogsShortLived()
│  │
│  ├─ Attempts to extract net.Conn
│  │  ├─ stream.(net.Conn) → ❌ false (Docker wraps it)
│  │  ├─ stream.(connGetter) → ❌ false (interface doesn't exist)
│  │  ├─ stream.(getConnInterface) → ❌ false (interface doesn't exist)
│  │  └─ Returns nil ⚠️
│  │
│  ├─ if conn != nil { ... }
│  │  └─ ❌ SKIPPED (conn is nil)
│  │
│  └─ Spawns reader goroutine:
│     │
│     ├─ buf := make([]byte, 8192)
│     ├─ n, err := stream.Read(buf)
│     │  │
│     │  └─ [Blocks in kernel syscall]
│     │     │
│     │     ⚠️ NO DEADLINE SET
│     │     │
│     │     │ Wait indefinitely for data...
│     │     │ Context expires at 30s...
│     │     │ Main loop moves on...
│     │     │ Goroutine still blocked!
│     │     │
│     │     Eventually Docker closes connection
│     │     │
│     │     └─ Read returns EOF
│     │
│     ├─ if err == io.EOF {
│     │     readCh <- result{err: EOF}
│     │     return  // ⚠️ Goroutine exits after indefinite time
│     │  }
│     │
│     └─ DONE (goroutine finally terminates)
│
└─ Main loop already moved on (temporary goroutine leak)

Result: Goroutine leak for duration of context timeout!
```

---

## Diagram 3: Context vs Deadline Timing (Issue #3)

### Current (INCORRECT)

```
Time:     0s                              30s                              35s
          │                               │                                │
Context:  [─────────────────────────────────────────────────────►] EXPIRES
          │                               │                                │
Read      │                               │                                │
Deadline: [────────────────────────────────────────────────────────────►] Should expire
          │                               │                                │
          │                               │                                │
          │                               ▲                                │
          │                               │                                │
          │                        Context expires first                   │
          │                        Main loop returns                       │
          │                        Goroutine still blocking ⚠️             │
          │                                                                 │
          └─────────────────────────────────────────────────────────────────►
                           5s gap where goroutine is orphaned
```

**Problem**: Context timeout (30s) < Read deadline (35s)
- Main loop exits at 30s
- Goroutine should continue until 35s
- But deadline is never set anyway (Issue #2)!

### Fixed (CORRECT)

```
Time:     0s                              30s                              35s
          │                               │                                │
Read      │                               │                                │
Deadline: [─────────────────────────────────────────►] EXPIRES FIRST
          │                               │           │                    │
Context:  [───────────────────────────────────────────────────────────►] Still active
          │                               │           │                    │
          │                               │           │                    │
          │                               │           ▲                    │
          │                               │           │                    │
          │                               │    Read deadline triggers      │
          │                               │    Goroutine exits cleanly     │
          │                               │    Main loop waits             │
          │                               │                                │
          └───────────────────────────────────────────────────────────────────►
                                                    5s grace period
```

**Fixed**: Read deadline (30s) < Context timeout (35s)
- Read deadline expires first at 30s
- Goroutine exits cleanly
- Main loop still active (has 5s grace period)
- Context provides safety net for cleanup

---

## Diagram 4: Complete Flow Comparison

### FASE 6H Current (BROKEN)

```
┌─────────────────────────────────────────────────────────────────────┐
│ Container Task: "container_abc123"                                  │
│ ├─ Start: 11:05:00                                                  │
│ ├─ Last Heartbeat: 11:05:00 (initial)                              │
│ └─ Status: running                                                  │
└─────────────────────────────────────────────────────────────────────┘
         │
         ├─ 11:05:00  Stream #1 created (30s timeout)
         │            └─ Reader goroutine spawned (no deadline)
         │               └─ Blocks in Read()
         │
         ├─ 11:05:30  Context expires (DeadlineExceeded)
         │            ├─ Main loop: rotate stream ✅
         │            └─ Reader goroutine: still blocked ⚠️
         │
         ├─ 11:05:31  Stream #2 created (30s timeout)
         │            ├─ Orphaned goroutine #1 still running
         │            └─ Reader goroutine #2 spawned
         │
         ├─ 11:06:01  Context expires again
         │            ├─ Main loop: rotate stream ✅
         │            ├─ Orphaned goroutine #1 still running
         │            └─ Orphaned goroutine #2 now orphaned
         │
         │  ... pattern continues ...
         │
         ├─ 11:10:00  6th stream rotation
         │            └─ 6 orphaned goroutines (temporary)
         │
         ├─ 11:11:00  Task Manager cleanup runs
         │            ├─ Check: now - last_heartbeat = 360s
         │            ├─ Timeout: 360s > 300s ❌
         │            ├─ Decision: TIMEOUT!
         │            └─ Calls task.Cancel()
         │
         └─ 11:11:01  Container context cancelled
                      ├─ monitorContainer() exits
                      ├─ Container removed from map
                      └─ Monitoring STOPPED ❌

Result:
  - ❌ Monitoring stops after 6 minutes
  - ❌ Temporary goroutine leaks (6 goroutines)
  - ❌ Silent failure (no visible error)
  - ❌ Data loss after 6 minutes
```

### FASE 6H Fixed (WORKING)

```
┌─────────────────────────────────────────────────────────────────────┐
│ Container Task: "container_abc123"                                  │
│ ├─ Start: 11:05:00                                                  │
│ ├─ Last Heartbeat: 11:05:30 (updated regularly)                    │
│ └─ Status: running                                                  │
└─────────────────────────────────────────────────────────────────────┘
         │
         ├─ 11:05:00  Stream #1 created (35s context timeout)
         │            ├─ Heartbeat ticker created (30s)
         │            └─ Reader goroutine spawned (30s read deadline)
         │               └─ Blocks in Read()
         │
         ├─ 11:05:30  Read deadline expires (if fix #2 works)
         │            ├─ Kernel: wake up Read() with timeout error
         │            ├─ Reader goroutine: exits cleanly ✅
         │            └─ Heartbeat ticker fires ✅
         │               └─ task.Heartbeat("container_abc123")
         │                  └─ Task Manager: last_heartbeat = 11:05:30
         │
         ├─ 11:05:30  Main loop: rotate stream ✅
         │            └─ Heartbeat sent after rotation ✅
         │               └─ Task Manager: last_heartbeat = 11:05:30
         │
         ├─ 11:05:31  Stream #2 created
         │            └─ Reader goroutine #2 spawned
         │               └─ No orphaned goroutines ✅
         │
         ├─ 11:06:00  Heartbeat ticker fires ✅
         │            └─ Task Manager: last_heartbeat = 11:06:00
         │
         ├─ 11:06:01  Read deadline expires
         │            ├─ Reader goroutine exits cleanly ✅
         │            ├─ Heartbeat sent after rotation ✅
         │            └─ Stream #3 created
         │
         │  ... pattern continues indefinitely ...
         │
         ├─ 11:11:00  Task Manager cleanup runs
         │            ├─ Check: now - last_heartbeat = 30s
         │            ├─ Timeout: 30s < 300s ✅
         │            └─ Decision: Task healthy, continue ✅
         │
         ├─ 11:30:00  Still monitoring...
         │            └─ 50th stream rotation
         │
         └─ 12:00:00  Still monitoring...
                      └─ 120th stream rotation

Result:
  - ✅ Monitoring continues indefinitely
  - ✅ No persistent goroutine leaks (if fix #2 works)
  - ✅ Regular heartbeats prevent timeout
  - ✅ Stable long-term operation
```

---

## Diagram 5: Heartbeat Strategy Comparison

### Strategy A: Ticker Only (Recommended)

```go
heartbeatTicker := time.NewTicker(30 * time.Second)
defer heartbeatTicker.Stop()

for {
    select {
    case <-containerCtx.Done():
        return nil
    case <-heartbeatTicker.C:
        cm.taskManager.Heartbeat(taskName)
        continue  // Skip stream processing, just heartbeat
    default:
    }

    // Stream processing...
}
```

**Heartbeat Pattern**:
```
Time:   0s    30s   60s   90s   120s  150s  180s
        │     │     │     │     │     │     │
Ticker: ──────●─────●─────●─────●─────●─────●────►
              ▲     ▲     ▲     ▲     ▲     ▲
         Heartbeat sent every 30s (consistent)
```

**Pros**:
- ✅ Guaranteed periodic heartbeats
- ✅ Independent of stream state
- ✅ Works even if stream hangs
- ✅ Simple and predictable

**Cons**:
- ⚠️ Extra goroutine for ticker (minimal overhead)

### Strategy B: After Rotation Only

```go
for {
    // Stream processing...

    if readErr == context.DeadlineExceeded {
        // Successful rotation
        cm.taskManager.Heartbeat(taskName)  // Only here
    }
}
```

**Heartbeat Pattern**:
```
Time:   0s    30s   60s   90s   120s  150s  180s
        │     │     │     │     │     │     │
Rotate: ──────●─────●─────●─────●─────●─────●────►
              ▲     ▲     ▲     ▲     ▲     ▲
         Heartbeat only after successful rotation
```

**Pros**:
- ✅ No extra goroutine
- ✅ Heartbeat tied to actual work

**Cons**:
- ❌ If rotation fails, no heartbeat sent
- ❌ If stream hangs, task times out
- ❌ Vulnerable to edge cases

### Strategy C: Hybrid (Best)

```go
heartbeatTicker := time.NewTicker(30 * time.Second)
defer heartbeatTicker.Stop()

for {
    select {
    case <-containerCtx.Done():
        return nil
    case <-heartbeatTicker.C:
        cm.taskManager.Heartbeat(taskName)
        continue
    default:
    }

    // Stream processing...

    if readErr == context.DeadlineExceeded {
        // Also send after rotation (double redundancy)
        cm.taskManager.Heartbeat(taskName)
    }
}
```

**Heartbeat Pattern**:
```
Time:   0s    30s   60s   90s   120s  150s  180s
        │     │     │     │     │     │     │
Ticker: ──────●─────●─────●─────●─────●─────●────►
              ▲     ▲     ▲     ▲     ▲     ▲
Rotate: ──────●─────●─────●─────●─────●─────●────►
              ▲     ▲     ▲     ▲     ▲     ▲
         Double redundancy (ticker + rotation)
```

**Pros**:
- ✅ Maximum reliability (double redundancy)
- ✅ Handles all edge cases
- ✅ Clear in logs (two heartbeat sources)

**Cons**:
- ⚠️ Slightly more frequent heartbeats (acceptable)

**Recommendation**: Use Strategy C (Hybrid) for maximum reliability.

---

## Diagram 6: Memory Impact Analysis

### Current State (Before Fix)

```
Goroutine Memory Over Time:

Count
  │
  50├─────────────── TIMEOUT! Monitoring stops
  │                  ▲
  40│                │
  │                  │ All goroutines
  30│                │ eventually
  │                  │ terminate
  20│              ╱ │
  │            ╱   │ │
  10│        ╱     │ │
  │      ╱       │ │
  0├──╱─────────────┴─────────────────────────────►
   0m  1m  2m  3m  4m 5m  6m  7m  8m  9m  10m    Time

Problem: Monitoring stops at 6 minutes (task timeout)
Result: Zero goroutines after 6m (because nothing is running!)
```

### After Heartbeat Fix (Phase 1)

```
Goroutine Memory Over Time:

Count
  │
  60├────────────────────────────────────────────────
  │    ╱╲      ╱╲      ╱╲      ╱╲      ╱╲
  50├──╱──╲────╱──╲────╱──╲────╱──╲────╱──╲───────
  │  │    ╲  ╱    ╲  ╱    ╲  ╱    ╲  ╱    ╲
  40├──│────╲╱──────╲╱──────╲╱──────╲╱──────╲─────
  │  │
  30├──│ Sawtooth pattern (leak then cleanup)
  │  │
  20├──│
  │  │
  10├──│
  │  │
  0├──┴────────────────────────────────────────────►
   0m  5m  10m 15m 20m 25m 30m 35m 40m 45m 50m   Time

Pattern:
  - Leaks accumulate for 30s (up to ~50 goroutines)
  - Goroutines expire when context times out
  - Cycle repeats indefinitely
  - Stable over time (no continuous growth)

Memory Impact:
  - Peak: ~50 goroutines × 2KB = 100KB (temporary)
  - Average: ~25 goroutines × 2KB = 50KB
  - Acceptable for production ✅
```

### After Full Fix (Phase 3)

```
Goroutine Memory Over Time:

Count
  │
  60├────────────────────────────────────────────────
  │
  50├─────────────────────────────────────────────── Initial
  │                                                  container
  40├────────────────────────────────────────────── count
  │                                                  (~50)
  30├──────────────────────────────────────────────
  │
  20├──────────────────────────────────────────────
  │
  10├──────────────────────────────────────────────
  │
  0├────────────────────────────────────────────────►
   0m  5m  10m 15m 20m 25m 30m 35m 40m 45m 50m   Time

Pattern:
  - Stable goroutine count (one per container)
  - No leaks (SetReadDeadline works)
  - Clean goroutine exits at 30s
  - Optimal memory usage

Memory Impact:
  - Stable: ~50 goroutines (baseline)
  - No temporary spikes
  - Ideal for production ✅
```

---

## Summary Table

| Aspect | Current (Broken) | Phase 1 Fix | Phase 3 Fix |
|--------|-----------------|-------------|-------------|
| **Task Timeout** | ❌ 6 minutes | ✅ Never | ✅ Never |
| **Goroutine Leaks** | ⚠️ None (stops at 6m) | ⚠️ Temporary (~50) | ✅ None |
| **Monitoring Duration** | ❌ 6 minutes max | ✅ Indefinite | ✅ Indefinite |
| **Memory Impact** | ✅ Low (nothing runs) | ⚠️ ~100KB peaks | ✅ Minimal |
| **Production Ready** | ❌ No | ✅ Yes | ✅ Yes (optimal) |
| **Implementation Time** | - | 30 minutes | 8 hours |
| **Risk Level** | - | Very Low | Medium |

**Recommendation**: Deploy Phase 1 immediately, schedule Phase 3 for next sprint.

---

**Document End**
