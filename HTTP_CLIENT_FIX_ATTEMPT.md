# HTTP Client Fix Attempt Report

**Date**: 2025-11-06
**Duration**: ~1.5 hours
**Status**: ❌ **UNSUCCESSFUL** - Leak persists at 31-32 goroutines/min

---

## Executive Summary

Applied HTTP Transport configuration fixes to both Loki and Docker clients to limit connection goroutines, but the leak **persists at the same rate** (31-32 goroutines/min). This indicates either:
1. The HTTP Transport configuration isn't being applied correctly
2. There are additional HTTP client sources not yet identified
3. The leak isn't from HTTP connections at all (contrary to initial goroutine dump analysis)

---

## Root Cause Investigation

### Initial Findings (from SIGQUIT goroutine dump)

ALL leaked goroutines showed stacks from HTTP connection handling:
```
goroutine 14188 [IO wait, 2 minutes]:
net/http.(*persistConn).readLoop
net/http.(*Transport).dialConn.gowrap2

goroutine 14220 [select, 2 minutes]:
net/http.(*persistConn).writeLoop
net/http.(*Transport).dialConn.gowrap3
```

**Correlation**:
- 4,729 goroutines ≈ 2,365 HTTP connections (2 goroutines per connection)
- 2,301 file descriptors leaked
- Ratio: ~0.97 FDs per connection (expected: 1.0)

**Conclusion**: HTTP connection leak confirmed.

---

## Fixes Applied

### Fix #1: Loki HTTP Client (`internal/sinks/loki_sink.go`)

**File**: `internal/sinks/loki_sink.go` lines 108-124
**Date**: 2025-11-06 14:30

```go
// BEFORE (missing MaxConnsPerHost)
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:        10,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     30 * time.Second,
    },
}

// AFTER (with connection limits)
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        MaxConnsPerHost:       50,  // ← CRITICAL: Limits total connections
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: timeout,
        DisableKeepAlives:     false,
        ForceAttemptHTTP2:     false,
    },
}
```

**Expected Impact**: Limit Loki HTTP connections to max 50, preventing unlimited goroutine creation.

**Actual Result**: ❌ No change in leak rate (still 31-32/min)

---

### Fix #2: Docker HTTP Client (`pkg/docker/connection_pool.go`)

**File**: `pkg/docker/connection_pool.go` lines 275-298
**Date**: 2025-11-06 15:15

```go
// BEFORE (using default HTTP client)
dockerClient, err := client.NewClientWithOpts(
    client.FromEnv,
    client.WithAPIVersionNegotiation(),
)

// AFTER (with custom HTTP client)
httpClient := &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        MaxConnsPerHost:       50,  // ← CRITICAL
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
    },
}

dockerClient, err := client.NewClientWithOpts(
    client.FromEnv,
    client.WithAPIVersionNegotiation(),
    client.WithHTTPClient(httpClient),  // ← Custom client
)
```

**Expected Impact**: Limit Docker daemon HTTP connections to max 50.

**Actual Result**: ❌ No change in leak rate (still 31-32/min)

---

## Test Results

### Test #1: Loki Fix Alone
- **Baseline**: 159 goroutines
- **T+5min**: 319 goroutines
- **Growth**: 160 goroutines in 5 minutes
- **Rate**: **32 goroutines/min**
- **Loki Success Rate**: 100% (62/62) ✅ (timezone fix working)
- **Verdict**: ❌ Leak unchanged

### Test #2: Loki + Docker Fixes Combined
- **Baseline**: 97 goroutines
- **T+2min**: 159 goroutines
- **Growth**: 62 goroutines in 2 minutes
- **Rate**: **31 goroutines/min**
- **Verdict**: ❌ Leak unchanged (test still running)

---

## Analysis: Why Didn't It Work?

### Hypothesis 1: Configuration Not Applied
The `MaxConnsPerHost` setting might not be working as expected:
- Go's HTTP Transport might override this setting
- Connection pooling logic might bypass the limit
- Need to verify with runtime inspection

### Hypothesis 2: Additional HTTP Client Sources
Other components may be creating HTTP clients without our configuration:
- `pkg/tracing/tracing.go` - OpenTelemetry HTTP exporters
- `pkg/slo/slo.go` - SLO monitoring HTTP calls
- `pkg/discovery/service_discovery.go` - Service discovery HTTP calls
- `internal/sinks/elasticsearch_sink.go` - Elasticsearch HTTP client
- `internal/sinks/splunk_sink.go` - Splunk HTTP client

### Hypothesis 3: Not Actually HTTP Connections
The initial goroutine dump may have been misleading:
- HTTP goroutines might be symptoms, not the cause
- Something else might be spawning goroutines that then make HTTP calls
- Need fresh goroutine dump after fixes to verify

---

## Next Steps

### Immediate (Next Session)
1. **Capture fresh goroutine dump** after fixes applied
   ```bash
   docker-compose exec -T log_capturer_go sh -c 'kill -QUIT 1'
   docker logs log_capturer_go --tail 1000 > /tmp/goroutine_dump_after_fix.txt
   ```

2. **Analyze dump** to see if:
   - HTTP goroutines are still the majority
   - Same stack traces as before
   - New patterns emerge

3. **Runtime inspection** of HTTP Transport:
   ```go
   // Add debug logging to see actual connection count
   t := httpClient.Transport.(*http.Transport)
   log.Printf("Active connections: %d", t.IdleConnN())
   ```

### Investigative (This Week)
1. **Audit ALL HTTP client creations**:
   ```bash
   grep -rn "http\.Client\|NewClient" --include="*.go" | grep -v test
   ```

2. **Add `DisableKeepAlives: true`** to test if connection reuse is the problem

3. **Test with minimal config** (disable all sinks except Loki)

4. **Add per-client metrics**:
   ```go
   prometheus.NewGaugeVec("http_active_connections", []string{"client_name"})
   ```

---

## Partial Successes

### ✅ Loki Timestamp Fix
- **Before**: 4.3% success rate (timestamp errors)
- **After**: 100% success rate (62/62 batches)
- **Fix**: Changed `time.Now()` → `time.Now().UTC()` in 21 locations
- **Impact**: Eliminated "timestamp too new" Loki rejections

### ✅ File Descriptor Improvement
- **Before**: 752/1024 (73% utilization, approaching limit)
- **After**: <100/4096 (minimal utilization)
- **Fix**: Increased ulimits in `docker-compose.yml`

### ✅ Baseline Reduction
- **Before**: 4,729 goroutines (after 2h runtime)
- **After**: 97-159 goroutines (after restart with fixes)
- **Note**: Baseline is acceptable, but leak persists

---

## Lessons Learned

1. **HTTP Transport `MaxConnsPerHost` alone is insufficient** - need to investigate why
2. **Goroutine dump analysis can be misleading** - symptoms vs root cause
3. **Need runtime observability** - can't rely on static code analysis alone
4. **Multiple leak sources possible** - need systematic elimination testing

---

## Files Modified

1. `internal/sinks/loki_sink.go` - HTTP client configuration (lines 108-124)
2. `pkg/docker/connection_pool.go` - Docker HTTP client (lines 3-13, 275-298)

---

## Current Status

| Metric | Before All Fixes | After HTTP Fixes | Target | Status |
|--------|------------------|------------------|--------|--------|
| Goroutines (baseline) | 4,729 | 97-159 | <100 | ⚠️ |
| Growth Rate | 32-36/min | 31-32/min | 0/min | ❌ |
| Loki Success Rate | 4.3% | 100% | >99% | ✅ |
| File Descriptors | 752/1024 (73%) | <100/4096 (<3%) | <20% | ✅ |
| Container Health | UNHEALTHY | DEGRADED | HEALTHY | ❌ |

---

**Conclusion**: HTTP Transport configuration fixes did NOT resolve the goroutine leak. Further investigation required with fresh profiling data.

**Next Action**: Capture and analyze new goroutine dump to identify actual leak source.

**Last Updated**: 2025-11-06 15:35 UTC
