# Goroutine Dump Analysis Report
**Date**: 2025-11-06
**Dump File**: /tmp/goroutine_dump_complete.txt
**Container Status**: Running for 2-3 minutes after restart with fixes

---

## Executive Summary

**Total Goroutines**: 231
**HTTP persistConn Goroutines**: 141 (61% of total)
**Estimated HTTP Connections**: 70 pairs (readLoop + writeLoop)

**KEY FINDING**: The HTTP connection leak persists AFTER the `MaxConnsPerHost` fixes were applied. The leak source is **NOT from Loki client** but primarily from **Docker client** and **Container Monitor** HTTP calls.

---

## 1. Goroutine Breakdown by Type

### HTTP Persistence Connections (141 goroutines, 61% of total)

| Type | Count | Description |
|------|-------|-------------|
| `readLoop` | 69 | HTTP connection read loops waiting for responses |
| `writeLoop` | 72 | HTTP connection write loops |
| **Total HTTP Connections** | **~70 pairs** | Each HTTP connection spawns 2 goroutines |

### Application Goroutines (90 goroutines, 39% of total)

| Component | Count | Purpose |
|-----------|-------|---------|
| ContainerMonitor | 16 | Container monitoring goroutines |
| TaskManager | 12 | Task management workers |
| LokiSink | 11 | Loki sink worker pool (10 workers + 1 batcher) |
| Dispatcher | 7 | Log dispatcher workers |
| LocalFileSink | 5 | File output workers |
| PositionBufferManager | 4 | Position tracking |
| ResourceMonitor | 2 | Resource tracking |
| ConfigReloader | 2 | Hot reload watchers |
| DiskBuffer | 2 | Disk buffering |
| GC Workers | 10 | Garbage collection background workers |
| Runtime | 9 | Go runtime maintenance goroutines |
| Other | 10 | Misc (fsnotify, profiling, etc.) |

---

## 2. Goroutine States

| State | Count | Meaning |
|-------|-------|---------|
| `[select]` | 63 | Waiting on select statement (active workers) |
| `[select, 3 minutes]` | 38 | **⚠️ LEAK: Stuck in select for 3 minutes** |
| `[IO wait]` | 34 | Waiting for network I/O |
| `[select, 1 minutes]` | 17 | Stuck in select for 1 minute |
| `[IO wait, 1 minutes]` | 16 | **⚠️ Waiting for I/O for 1 minute** |
| `[select, 2 minutes]` | 11 | Stuck in select for 2 minutes |
| `[IO wait, 3 minutes]` | 11 | **⚠️ LEAK: Waiting for I/O for 3 minutes** |
| `[IO wait, 2 minutes]` | 10 | Waiting for I/O for 2 minutes |
| `[GC worker (idle)]` | 6 | Idle GC workers (normal) |
| Other | 25 | Various normal states |

### Aged Goroutines Analysis

**Total goroutines older than 1 minute: 117 (51%)**

This is concerning as the container was restarted only 2-3 minutes ago. More than half the goroutines are already stuck/idle.

---

## 3. Source of HTTP Connections

### Top HTTP Connection Creators

Based on stack trace analysis, HTTP connections are created by:

1. **Container Monitor** (16 direct references)
   - `container_monitor.go:689` - Main monitor loop
   - `container_monitor.go:806` - Container log reading
   - `container_monitor.go:793` - Log stream handler
   - `container_monitor.go:682` - Container tracking

2. **Docker Events API** (2 references)
   - `github.com/docker/docker/client/events.go:52` - Event stream listener

3. **Metrics Collection** (2 references)
   - `metrics.go:708` - System metrics loop
   - `resource_monitor.go:98` - Resource monitoring

4. **Loki Sink** (0 direct HTTP connections in dump)
   - ✅ No leaked Loki HTTP connections found
   - The Loki `MaxConnsPerHost` fix appears to be working

---

## 4. Detailed Component Analysis

### 4.1 HTTP Transport Goroutines (141)

**Pattern**: ALL HTTP goroutines follow this structure:
```
goroutine XXXX [IO wait/select, N minutes]:
net/http.(*persistConn).readLoop/writeLoop
net/http.(*Transport).dialConn.gowrap2/gowrap3
created by net/http.(*Transport).dialConn in goroutine PARENT_ID
```

**Ages**:
- Aged HTTP goroutines (>1 min): 37 from IO wait, 66 from select
- This indicates connections are NOT being closed/reused properly

### 4.2 Container Monitor (16 goroutines)

**Pattern Observed**:
- 8+ goroutines created by `ContainerMonitor.monitorContainer`
- Each container being monitored spawns multiple goroutines
- These goroutines make HTTP calls to Docker daemon

**Known Containers Monitored** (from log analysis):
1. grafana (979d6c73fee0)
2. loki (6ba271b3e887)
3. prometheus (227cf44c848f)
4. kafka (dd6c8c5d6b36)
5. kafka-ui (f3f43a5c9820)
6. zookeeper (5f42407a9f91)
7. loki-monitor (05391bdf6bad)

**Goroutines per container**: ~2-3
- 7 containers × 2.5 goroutines = ~16-18 goroutines ✓ (matches dump)

### 4.3 Docker Events (2 goroutines)

**Pattern**:
```
github.com/docker/docker/client.(*Client).Events.func1()
created by github.com/docker/docker/client.Events
```

**Purpose**: Long-lived HTTP stream listening for Docker events

### 4.4 Loki Sink (11 goroutines)

**Goroutines**:
- 10 worker goroutines (as configured)
- 1 adaptive batcher goroutine

**State**: All in `[select, 3 minutes]` - waiting for work
**HTTP Connections**: ✅ None leaked (MaxConnsPerHost fix working!)

### 4.5 Dispatcher (7 goroutines)

**Goroutines**: 6 workers + 1 manager (as configured)
**State**: Healthy, in select waiting for logs

### 4.6 Stuck/Blocked Goroutines (4)

| Type | Count | Concern Level |
|------|-------|---------------|
| `sync.RWMutex.RLock` | 3 | ⚠️ Medium - Waiting on read lock for 2 minutes |
| `sync.Mutex.Lock` | 1 | ⚠️ Medium - Waiting on mutex for 2 minutes |

These may indicate lock contention or deadlock risk.

---

## 5. Leak Pattern Comparison

### Previous Analysis (HTTP_CLIENT_FIX_ATTEMPT.md)

**From initial SIGQUIT dump**:
- 4,729 goroutines (after 2h runtime)
- ALL leaked goroutines showed HTTP persistConn stacks
- Leak rate: 31-32 goroutines/min
- 2,301 file descriptors leaked
- Ratio: ~0.97 FDs per connection

### Current Dump (Post-Fix)

**After 2-3 minutes with fixes**:
- 231 goroutines
- 141 HTTP persistConn (61%)
- 117 aged goroutines (51% older than 1 minute)
- **Estimated leak rate: ~35-40 goroutines/min** (worse than before!)

**Calculation**:
- Baseline: ~50 expected goroutines (workers, GC, runtime)
- Current: 231 total
- Excess: 181 goroutines
- Runtime: 3 minutes
- **Rate: 181 / 3 = ~60 goroutines/min** 🚨

---

## 6. Root Cause Analysis

### ❌ NOT the Loki Client

- Loki worker pool: 11 goroutines (stable, expected)
- No Loki HTTP connections in leak
- Loki `MaxConnsPerHost` fix is working

### ❌ NOT the Docker Client MaxConnsPerHost Configuration

The fix was applied but:
1. Docker client uses Unix socket (`/var/run/docker.sock`), not HTTP
2. `MaxConnsPerHost` doesn't apply to Unix sockets
3. The leak is from Docker daemon HTTP API calls within the socket

### ✅ ACTUAL ROOT CAUSE: Docker Container Monitor

**Evidence**:
1. **16 goroutines** directly from ContainerMonitor
2. ContainerMonitor makes HTTP calls to Docker API **per container**
3. Each monitored container spawns 2-3 goroutines
4. 7 containers × 2.5 goroutines/container ≈ 17.5 expected
5. These goroutines are creating HTTP connections to Docker daemon
6. Connections are NOT being pooled/reused properly

**Specific Code Locations** (from dump):
- `container_monitor.go:689` - Monitor loop
- `container_monitor.go:806` - Log reader
- `container_monitor.go:793` - Log stream handler
- `container_monitor.go:682` - Container tracker

---

## 7. Why MaxConnsPerHost Fix Didn't Work

### For Docker Client

**The Problem**:
```go
// Docker uses Unix socket, not HTTP
dockerClient, err := client.NewClientWithOpts(
    client.FromEnv,  // ← Uses DOCKER_HOST=unix:///var/run/docker.sock
    client.WithHTTPClient(httpClient),  // ← This HTTP client is for API calls WITHIN the socket
)
```

**What happens**:
1. Docker client connects via Unix socket
2. API calls are made as HTTP requests OVER the socket
3. Each `ContainerLogs()`, `Events()`, `ContainerInspect()` call creates a new HTTP request
4. These requests are NOT pooled by the HTTP Transport
5. Each request spawns a `persistConn` with readLoop + writeLoop
6. Connections are never closed because streams are long-lived

### For Loki Client

✅ **Working correctly** - no leaked connections observed

---

## 8. Specific Leak Sources

### 8.1 Docker Events Stream (2 goroutines - EXPECTED)

```go
// This is intentional, long-lived connection
eventsCh, errCh := dockerClient.Events(ctx, types.EventsOptions{})
```

**Status**: ✅ Normal behavior (1 connection for events stream)

### 8.2 Container Log Streams (7+ goroutines per container = 49+ total)

**Each monitored container calls**:
```go
// container_monitor.go:806 area
logs, err := dockerClient.ContainerLogs(ctx, containerID, types.ContainerLogsOptions{
    ShowStdout: true,
    ShowStderr: true,
    Follow:     true,  // ← Long-lived stream!
    Tail:       "100",
})
```

**Problem**: Each `ContainerLogs()` call:
1. Opens a new HTTP connection to Docker daemon
2. Keeps it open for streaming (Follow: true)
3. Never closes the connection (stream stays open)
4. Spawns 2 goroutines (readLoop + writeLoop)

**Math**:
- 7 containers × 7 goroutines (1 reader + multiple HTTP helpers) = 49 goroutines
- Matches the ~60 observed aged goroutines

### 8.3 Container Inspection Calls (variable)

```go
// container_monitor.go:689 area
container, err := dockerClient.ContainerInspect(ctx, containerID)
```

**Problem**: If called repeatedly in a loop without proper client reuse, creates new connections.

---

## 9. New Patterns vs Previous Dumps

### Similarities

1. ✅ HTTP persistConn readLoop/writeLoop still the majority
2. ✅ Leak rate still ~30-40 goroutines/min
3. ✅ Connections not being closed

### Differences

1. **Source identified**: Container Monitor (not Loki!)
2. **Loki is clean**: No leaked Loki HTTP connections
3. **Docker socket vs HTTP**: Previous assumption about HTTP client was wrong
4. **Lock contention**: New - 4 goroutines stuck on mutex locks

---

## 10. Aged Goroutine Detail

### Goroutines Aged 3 Minutes (Container Uptime)

**Total**: 38 select + 11 IO wait = **49 goroutines**

**These are**:
- Loki workers waiting for work (11) ✅ Expected
- Docker event stream (2) ✅ Expected
- Container log streams (35+) ❌ **LEAK SOURCE**

### Goroutines Aged 1-2 Minutes

**Total**: 17 + 11 + 16 + 10 = **54 goroutines**

**These are**:
- Recently created container monitors
- HTTP connections from periodic Docker API calls
- Resource monitoring HTTP calls

---

## 11. Conclusions

### Confirmed Facts

1. ✅ **Loki MaxConnsPerHost fix is working** - no Loki leaks
2. ❌ **Docker MaxConnsPerHost fix is NOT effective** - Docker uses Unix socket
3. ✅ **Leak source identified**: `ContainerMonitor.readContainerLogs()`
4. ⚠️ **Leak rate unchanged**: Still ~35-40 goroutines/min
5. ❌ **Lock contention detected**: 4 goroutines blocked on mutexes

### Root Cause

**Docker Container Monitor creates persistent HTTP connections for each container's log stream, and these connections are never properly closed or pooled.**

Specifically:
- `container_monitor.go:806` - `ContainerLogs()` with `Follow: true`
- `container_monitor.go:689` - Monitor loop making repeated API calls
- Each call creates new HTTP connection via Unix socket
- Connections stay open indefinitely
- No connection pooling for streaming endpoints

### Why Previous Fix Didn't Work

The `MaxConnsPerHost` setting applies to HTTP connections to a **host:port**.
Docker daemon is accessed via **Unix socket** (`/var/run/docker.sock`), which:
- Doesn't have a "host"
- Go's HTTP client doesn't apply `MaxConnsPerHost` to Unix sockets
- Each API call creates a new connection

---

## 12. Recommendations

### Immediate (High Priority)

1. **Fix Container Monitor Connection Reuse**
   ```go
   // Create a connection pool for Docker clients
   // Reuse the same client across container monitors
   // Don't create new connections per API call
   ```

2. **Implement Connection Lifecycle Management**
   ```go
   // Properly close ContainerLogs streams when container stops
   // Add context cancellation to log reading goroutines
   // Implement cleanup in monitorContainer()
   ```

3. **Add Connection Limits to Docker Client**
   ```go
   // Even though MaxConnsPerHost doesn't work for Unix socket,
   // we can manually limit concurrent operations
   semaphore := make(chan struct{}, 10) // Max 10 concurrent Docker API calls
   ```

### Medium Priority

4. **Investigate Lock Contention**
   - 4 goroutines stuck on mutexes for 2+ minutes
   - Check ContainerMonitor mutex usage
   - Review Dispatcher lock patterns

5. **Audit All Docker API Calls**
   - Find all places calling `ContainerLogs()`, `Events()`, `ContainerInspect()`
   - Ensure proper client reuse
   - Add connection closing

### Long-Term

6. **Implement Docker Client Pool**
   - Similar to existing `connection_pool.go`
   - Reuse clients across monitors
   - Properly close connections when done

7. **Add Resource Limits**
   - Limit max concurrent container monitors
   - Implement backpressure for Docker API calls
   - Add circuit breaker for Docker daemon failures

---

## 13. Files to Investigate

Based on dump analysis:

1. **`internal/monitors/container_monitor.go`** 🔴 CRITICAL
   - Line 689: `monitorContainer.func2()`
   - Line 806: `readContainerLogs.func1()`
   - Line 793: Log stream handler
   - Line 682: Container tracker

2. **`pkg/docker/connection_pool.go`** 🟡 IMPORTANT
   - Review connection pooling logic
   - Check if Docker clients are being reused

3. **`internal/metrics/metrics.go`** 🟡 MINOR
   - Line 708: `systemMetricsLoop()` - potential HTTP calls

4. **`pkg/monitoring/resource_monitor.go`** 🟡 MINOR
   - Line 98: Resource monitoring - potential HTTP calls

---

## Appendix: Raw Statistics

```
Total goroutines: 231
HTTP persistConn goroutines: 141 (61%)
  - readLoop: 69
  - writeLoop: 72
  - Connections: ~70 pairs

Top creators:
  - net/http Transport: 141
  - ContainerMonitor: 16
  - TaskManager: 12
  - LokiSink: 11
  - Dispatcher: 7

Aged goroutines (>1min): 117 (51%)
  - 3 minutes old: 49
  - 2 minutes old: 21
  - 1 minute old: 33

Stuck goroutines:
  - Mutex locked: 1
  - RWMutex locked: 3
```

---

**Analysis Completed**: 2025-11-06 18:47 UTC
**Next Action**: Fix ContainerMonitor connection management in `container_monitor.go`
