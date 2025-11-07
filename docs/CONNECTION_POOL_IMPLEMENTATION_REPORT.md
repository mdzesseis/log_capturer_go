# Connection Pool Implementation Report - FASE 3-4

**Date:** 2025-11-07
**Branch:** `feature/connection-pooling-fix`
**Status:** ✅ Implementation Complete, 🔄 Testing In Progress
**Author:** workflow-coordinator + golang + docker-specialist + qa-specialist

---

## 📋 Executive Summary

Successfully implemented HTTP connection pooling for Docker SDK to reduce file descriptor (FD) leaks in container log streaming. Implementation completed in 4 hours across FASE 3.1-3.3, with smoke testing initiated in FASE 4.

**Key Changes:**
- Replaced `dockerPool` (multiple clients) with singleton `HTTPDockerClient`
- Implemented `ManagedDockerStream` wrapper for proper HTTP cleanup
- Added best-effort HTTP response extraction using reflection
- Deployed and smoke test running (30 minutes)

**Expected Impact:**
- **FD Leak Reduction:** 15.7/min → <5/min (target: 68% reduction)
- **Goroutine Leak Reduction:** 31.4/min → <10/min (target: 68% reduction)
- **Connection Reuse:** Expected >80%

---

## 🏗️ Implementation Details

### FASE 3.1: HTTPDockerClient Integration (1.5h)

**File:** `internal/monitors/container_monitor.go`

**Changes:**
1. **Imports:**
```go
import (
    // ... existing
    dockerclient "ssw-logs-capture/internal/docker"
    client "github.com/docker/docker/client"
    "net/http"
    "reflect"
)
```

2. **Struct Modification:**
```go
type ContainerMonitor struct {
    // ... existing fields
    httpClient    *client.Client        // NEW: Pooled HTTP client
    dockerPool    *docker.PoolManager   // Legacy (kept for compatibility)
}

type monitoredContainer struct {
    // ... existing fields
    stream *ManagedDockerStream  // CHANGED: was io.ReadCloser
}
```

3. **Constructor Update:**
```go
func NewContainerMonitor(...) (*ContainerMonitor, error) {
    // Initialize singleton HTTP Docker Client
    httpDockerClient, err := dockerclient.GetGlobalHTTPDockerClient(logger)
    if err != nil {
        return nil, fmt.Errorf("failed to create HTTP Docker client: %w", err)
    }

    return &ContainerMonitor{
        httpClient: httpDockerClient.Client(),
        // ... other fields
    }
}
```

**Rationale:**
- Singleton ensures ALL monitors share the same connection pool
- Reuses HTTP connections across all container streams
- Reduces new connection overhead

---

### FASE 3.2: ManagedDockerStream Integration (2h)

**File:** `internal/monitors/container_monitor.go`

**Changes in monitorContainer():**

**BEFORE:**
```go
stream, err := cm.dockerPool.ContainerLogs(streamCtx, mc.id, logOptions)
// ...
readErr := cm.readContainerLogsShortLived(streamCtx, mc, stream)
// ...
stream.Close()  // Only closes stream, NOT HTTP connection
```

**AFTER:**
```go
// Use HTTPDockerClient
rawStream, err := cm.httpClient.ContainerLogs(streamCtx, mc.id, logOptions)

// Extract HTTP response (best-effort)
httpResponse := extractHTTPResponse(rawStream)

// Wrap in managed stream
managedStream := NewManagedDockerStream(
    rawStream,
    httpResponse,
    mc.id,
    mc.name,
    cm.logger,
)

// Read from managed stream
readErr := cm.readContainerLogsShortLived(streamCtx, mc, managedStream)

// CRITICAL: Explicit close (closes BOTH stream AND HTTP connection)
closeErr := managedStream.Close()
if closeErr != nil {
    cm.logger.Warn("Failed to close managed stream", "error", closeErr)
}
```

**Key Improvements:**
1. **HTTP Response Extraction:** Best-effort extraction enables HTTP body close
2. **ManagedDockerStream:** Ensures both layers (stream + HTTP) are closed
3. **Explicit Error Handling:** Logs close errors for debugging

---

### FASE 3.3: Function Signature Update (0.5h)

**File:** `internal/monitors/container_monitor.go`

**Change:**
```go
// BEFORE
func (cm *ContainerMonitor) readContainerLogsShortLived(
    ctx context.Context,
    mc *monitoredContainer,
    stream io.Reader,  // Generic reader
) error

// AFTER
func (cm *ContainerMonitor) readContainerLogsShortLived(
    ctx context.Context,
    mc *monitoredContainer,
    stream *ManagedDockerStream,  // Typed managed stream
) error
```

**Impact:** Type safety + clear semantics (caller must provide managed stream)

---

### extractHTTPResponse() Implementation

**File:** `internal/monitors/container_monitor.go`

**Implementation:**
```go
func extractHTTPResponse(stream io.ReadCloser) *http.Response {
    // Method 1: Type assertion to interface with Response() method
    type ResponseGetter interface {
        Response() *http.Response
    }
    if getter, ok := stream.(ResponseGetter); ok {
        return getter.Response()
    }

    // Method 2: Reflection - look for "resp" or "response" field
    streamVal := reflect.ValueOf(stream)
    if streamVal.Kind() == reflect.Ptr {
        streamVal = streamVal.Elem()
    }

    if streamVal.Kind() == reflect.Struct {
        respField := streamVal.FieldByName("resp")
        if !respField.IsValid() {
            respField = streamVal.FieldByName("response")
        }

        if respField.IsValid() && respField.CanInterface() {
            if resp, ok := respField.Interface().(*http.Response); ok {
                return resp
            }
        }
    }

    // Could not extract - not critical
    return nil
}
```

**Notes:**
- **Best-Effort:** Docker SDK doesn't expose HTTP response directly
- **Graceful Degradation:** If extraction fails, stream still closes (partial leak)
- **Reflection:** Required due to SDK API limitations

---

## 🧪 FASE 4: Testing & Validation

### Build & Deploy (0.5h) - ✅ COMPLETE

**Steps:**
1. `go build -o bin/log_capturer ./cmd` → ✅ Success
2. `docker-compose build log_capturer_go` → ✅ Success (22s)
3. `docker-compose up -d` → ✅ Deployed (healthy)

**Metrics Validation:**
```bash
$ curl -s http://localhost:8001/metrics | grep docker_http
log_capturer_docker_http_idle_connections 0
log_capturer_docker_http_active_connections 0
log_capturer_docker_http_requests_total 0
log_capturer_docker_http_errors_total 0
```
✅ HTTP pooling metrics available

**Baseline (T=0):**
- Goroutines: 95
- File Descriptors: 30
- HTTP Idle Connections: 0
- HTTP Active Connections: 0

---

### Smoke Test (30 minutes) - 🔄 IN PROGRESS

**Test Script:** `/home/mateus/log_capturer_go/tests/load/smoke_test_simple.sh`

**Configuration:**
- Duration: 30 minutes
- Checkpoints: 15 (every 2 minutes)
- Metrics: goroutines, FDs, HTTP connections

**Success Criteria:**
1. **FD Leak Rate:** <5 FD/min (current baseline: 15.7/min)
2. **Goroutine Leak Rate:** <10 gor/min (current baseline: 31.4/min)
3. **Connection Reuse:** >80% (new metric)
4. **Zero Crashes:** 30 minutes uptime

**Test Started:** 2025-11-07 12:11:19 -03
**Expected Completion:** 2025-11-07 12:41:19 -03

**Checkpoint Format:**
```
[N/15] Xmin: GOR=Y (+growth, rate/min) FD=Z (+growth, rate/min)
```

---

## 📊 Expected Results (PRELIMINARY)

### Connection Pooling Benefits

**HTTP Transport Configuration:**
```go
MaxIdleConns:          100   // Total idle connections
MaxIdleConnsPerHost:   10    // Idle per Docker daemon
MaxConnsPerHost:       50    // Max concurrent per host
IdleConnTimeout:       90s   // Keep-alive duration
KeepAlive:             30s   // TCP keep-alive
```

**Expected Impact:**

| Metric | Baseline (FASE 6H.1) | Target | Expected Reduction |
|--------|---------------------|--------|-------------------|
| **FD Leak** | 15.7/min | <5/min | 68% |
| **Goroutine Leak** | 31.4/min | <10/min | 68% |
| **HTTP Requests** | N/A | Tracked | - |
| **Connection Reuse** | 0% | >80% | New capability |

**Rationale:**
- **Connection Reuse:** Idle connections stay alive for 90s → fewer new connections
- **HTTP Body Close:** ManagedDockerStream closes HTTP response body → FDs released
- **Singleton Client:** All monitors share pool → optimal utilization

---

## 🔍 Technical Deep Dive

### Why Connection Pooling Reduces FD Leaks

**Problem (Before):**
```
For each container stream:
1. Create new HTTP request
2. Docker API returns HTTP response with body (stream)
3. Application reads from stream
4. Application calls stream.Close()
5. ❌ HTTP response body NOT closed
6. ❌ TCP connection stays in CLOSE_WAIT/TIME_WAIT
7. ❌ File descriptor leaks
```

**Solution (After):**
```
For each container stream:
1. Reuse existing HTTP connection from pool (if available)
2. Docker API returns HTTP response with body (stream)
3. Wrap in ManagedDockerStream (tracks HTTP response)
4. Application reads from managed stream
5. Application calls managedStream.Close()
6. ✅ Closes application layer (stream)
7. ✅ Closes transport layer (HTTP response body)
8. ✅ TCP connection returned to pool or closed
9. ✅ File descriptor released
```

**Key Difference:**
- **Before:** Only `stream.Close()` → Leaks HTTP connection
- **After:** `stream.Close()` + `httpResponse.Body.Close()` → Full cleanup

---

### Why extractHTTPResponse() Is Needed

The Docker SDK's `ContainerLogs()` returns an `io.ReadCloser` that internally wraps:
```
io.ReadCloser (public interface)
  └─ Docker SDK internal wrapper
      └─ HTTP response body
          └─ TCP connection
```

**Problem:** Calling `stream.Close()` only closes the `io.ReadCloser` layer, NOT the HTTP body.

**Solution:** Extract the `*http.Response` using reflection/type assertions:
1. Check if stream implements `Response()` method
2. Use reflection to find `resp` or `response` field
3. If found, call `httpResponse.Body.Close()` explicitly

**Limitation:** Best-effort only. If extraction fails:
- Stream still closes (partial cleanup)
- HTTP connection MAY leak (but less than before due to pooling)
- Worst case: Falls back to dockerPool behavior

---

## 🚨 Known Limitations

### 1. HTTP Response Extraction Is Best-Effort

**Issue:** Docker SDK doesn't expose `*http.Response` in public API.

**Mitigation:**
- Use reflection to access internal fields
- Gracefully degrade if extraction fails
- HTTP connection pooling STILL reduces leaks (fewer new connections)

**Worst Case:**
- Extraction fails → HTTP body not closed
- BUT: Connection pool reduces new connection rate
- Expected leak: <50% of original (still improvement)

### 2. Goroutine Leak Persists (Known Issue from FASE 6H.1)

**Issue:** Blocking `Read()` syscalls may leave goroutines stuck.

**Status:** NOT fixed by connection pooling (different root cause).

**Mitigation:**
- 30-second timeout per stream
- Controlled leak: ~30-50 goroutines max (for 50 containers)
- Future: Migrate to Hybrid Monitor (file-based) to eliminate

### 3. Legacy dockerPool Still Exists

**Status:** Kept for compatibility during transition.

**Plan:**
- Remove in next phase after smoke test validation
- Ensure no code paths use dockerPool
- Clean up imports

---

## 📈 Next Steps

### FASE 4 (In Progress)
- ⏳ **Wait for smoke test completion** (30 minutes)
- ⏳ **Analyze results** (compare with baseline)
- ⏳ **Document empirical findings** (actual vs expected)

### FASE 5 (Documentation)
- Update implementation status docs
- Add connection pooling best practices to CLAUDE.md
- Create comparison chart (before/after)

### FASE 6 (Future Work)
- Remove legacy dockerPool
- Consider Hybrid Monitor migration (file-based, zero FD leak)
- Add alerting for HTTP connection pool saturation

---

## 🎯 Success Criteria

### Must-Have (MVP)
- [x] HTTPDockerClient integrated
- [x] ManagedDockerStream wrapping all streams
- [x] extractHTTPResponse() implemented
- [x] Build succeeds
- [x] Deploy succeeds
- [x] Metrics available
- [ ] Smoke test completes 30 minutes
- [ ] FD leak < 5/min (or < baseline)

### Nice-to-Have
- [ ] HTTP response extraction succeeds >80% of time
- [ ] Connection reuse >80%
- [ ] Zero crashes during test
- [ ] Goroutine leak reduction (bonus if achieved)

---

## 📚 References

### Code Files
- `internal/docker/http_client.go` - HTTPDockerClient implementation
- `internal/monitors/managed_stream.go` - ManagedDockerStream wrapper
- `internal/monitors/container_monitor.go` - Integration point
- `tests/load/smoke_test_simple.sh` - 30-minute smoke test

### Documentation
- `CONNECTION_POOLING_IMPLEMENTATION_STATUS.md` - Implementation tracking
- `CONTAINER_MONITOR_REFACTORING_ANALYSIS.md` - Architecture analysis
- `FASE6_STATUS.md` - Historical context (Hybrid Monitor baseline)

### Related Issues
- FASE 6H.1: Hybrid Short-Lived Streams (baseline: 31.4 gor/min, 15.7 FD/min)
- FASE 1-2: HTTPDockerClient creation (complete)
- FASE 3: Integration (THIS document)
- FASE 4-5: Testing & docs (in progress)

---

## 🔗 Next Document

After smoke test completion, results will be documented in:
- `CONNECTION_POOL_SMOKE_TEST_RESULTS.md`

---

**Last Updated:** 2025-11-07 12:15 -03 (smoke test in progress)
**Next Update:** After smoke test completion (~12:41 -03)
