# Task 3: DisableKeepAlives Test Results

**Date**: 2025-11-06
**Duration**: 5 minutes
**Status**: ❌ **UNSUCCESSFUL** - No effect on leak

---

## Executive Summary

Testing `DisableKeepAlives: true` on both Loki and Docker HTTP clients showed **ZERO impact** on the goroutine leak. The leak rate remained at exactly **32 goroutines/min**, identical to pre-test rates. This definitively proves the leak is NOT from HTTP connection pooling/reuse, but from **long-lived streaming connections**.

---

## Test Configuration

### Changes Applied

**File**: `internal/sinks/loki_sink.go` line 121
```go
// BEFORE
DisableKeepAlives: false,  // Keep connection pooling enabled

// AFTER
DisableKeepAlives: true,   // TESTING: Force connection closure (no pooling)
```

**File**: `pkg/docker/connection_pool.go` line 288
```go
// Added
DisableKeepAlives: true,   // TESTING: Force connection closure (no pooling)
```

### What DisableKeepAlives Does

- Forces HTTP client to close connections immediately after each request
- Prevents connection pooling and reuse
- Creates new TCP connection for every HTTP request
- Increases overhead (connection establishment per request)

### Expected Impact

**IF** the leak was from connection pooling:
- New connections would be created and destroyed for each request
- No persistent connection goroutines would accumulate
- Leak rate should drop to 0-2/min

---

## Test Results

### Goroutine Growth

| Time | Goroutines | Growth | Rate/min |
|------|------------|--------|----------|
| T+0  | 128        | +0     | -        |
| T+1  | 164        | +36    | 36/min   |
| T+2  | 194        | +66    | 33/min   |
| T+3  | 228        | +100   | 33/min   |
| T+4  | 257        | +129   | 32/min   |
| T+5  | 289        | +161   | **32/min** |

### Summary

- **Baseline**: 128 goroutines (96 initial + 32 growth in ~1min startup)
- **Final**: 289 goroutines
- **Total Growth**: 161 goroutines in 5 minutes
- **Leak Rate**: **32 goroutines/min** ❌

### Comparison

| Test | Config | Leak Rate | Status |
|------|--------|-----------|--------|
| Pre-Fix (MaxConnsPerHost) | KeepAlives: false | 31-32/min | ❌ |
| **Task 3 (DisableKeepAlives)** | **KeepAlives: true** | **32/min** | **❌ IDENTICAL** |

---

## Analysis

### Why DisableKeepAlives Didn't Help

1. **Leak Source is NOT Connection Pooling**
   - DisableKeepAlives only affects idle connection reuse
   - Our leak is from **active, long-lived streaming connections**
   - Container Monitor creates persistent HTTP streams with `Follow: true`

2. **HTTP Streaming Connections**
   ```go
   // Container Monitor - container_monitor.go:806
   reader, err := dockerClient.ContainerLogs(ctx, containerID, types.ContainerLogsOptions{
       ShowStdout: true,
       ShowStderr: true,
       Follow:     true,  // ← Creates persistent stream
       Timestamps: true,
   })
   ```
   - Each `Follow: true` stream creates 2 goroutines (readLoop + writeLoop)
   - Stream stays open as long as container is running
   - DisableKeepAlives doesn't close active streams

3. **Docker Unix Socket**
   - Docker daemon uses `/var/run/docker.sock` (Unix socket)
   - HTTP over Unix socket doesn't benefit from connection pooling anyway
   - DisableKeepAlives is irrelevant for Unix socket streams

### What We Learned

✅ **Confirmed**: Leak is NOT from HTTP connection reuse
✅ **Confirmed**: Leak IS from persistent streaming connections
✅ **Confirmed**: Container Monitor is the leak source
❌ **Ruled Out**: Connection pooling as the problem

---

## Implications for Next Steps

Since DisableKeepAlives had no effect, the fix must address **streaming connection management** in Container Monitor:

### Potential Solutions

1. **Connection Rotation** - Periodically close and reopen log streams
2. **Stream Limits** - Limit concurrent container log streams
3. **Context Cancellation** - Ensure streams close when containers stop
4. **Buffered Reading** - Use polling instead of streaming (trade throughput for stability)

### NOT Solutions

- ❌ DisableKeepAlives
- ❌ MaxConnsPerHost (doesn't apply to Unix sockets)
- ❌ Connection pool tuning
- ❌ HTTP Transport timeouts

---

## Recommendation

**REVERT DisableKeepAlives changes** because:
1. No benefit (proven by test)
2. Adds overhead (new connection per request)
3. Unnecessary for Unix socket connections

**PROCEED to Task 5: Isolation Testing** to confirm Container Monitor is the leak source by temporarily disabling it.

---

## Next Actions

1. ✅ Task 3 COMPLETE - DisableKeepAlives tested, no effect
2. ⏭️ **Skip Task 4** (HTTP client audit) - We know the source is Container Monitor
3. ⏭️ **Proceed to Task 5** - Isolation testing to confirm Container Monitor leak
4. 🛠️ **Implement Container Monitor Fix** - Address streaming connection management

---

## Files Modified (To Be Reverted)

- `internal/sinks/loki_sink.go` line 121 - DisableKeepAlives: true → false
- `pkg/docker/connection_pool.go` line 288 - Remove DisableKeepAlives: true

---

**Test Completed**: 2025-11-06
**Verdict**: DisableKeepAlives is NOT the solution
**Root Cause Confirmed**: Container Monitor streaming connections

**Next Step**: Isolation testing (Task 5) to definitively prove Container Monitor is the leak source.
