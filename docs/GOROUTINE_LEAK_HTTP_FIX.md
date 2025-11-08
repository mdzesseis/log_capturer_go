# HTTP Connection Leak - Analysis and Partial Fix

**Date**: 2025-11-08  
**Status**: 🔄 IN PROGRESS (HTTP fix implemented, leak persists - needs deeper investigation)  
**Time Invested**: ~1.5 hours  

## Problem Identified

**Primary Leak Source**: HTTP connection goroutines (`net/http.(*Transport).dialConn`)  
**Leak Rate**: ~32 goroutines/min (consistent)  
**Goroutines Created**: 350+ dialConn goroutines accumulating  

## Root Cause Analysis

### HTTP Connection Pooling Requirements

For HTTP/1.1 connection reuse to work properly, **ALL** of the following must be true:

1. ✅ Response body must be **FULLY READ** (even if empty)
2. ✅ Response body must be **CLOSED**
3. ❌ No context cancellation **DURING** body reading
4. ❌ Request must **COMPLETE SUCCESSFULLY** (not timeout)

### Findings

1. **Loki Configuration**:
   - Endpoint: `http://loki:3100/loki/api/v1/push`
   - Timeout: 240s
   - Response: 204 No Content (empty body)
   - Manual curl test: ✅ Works perfectly

2. **Success Rate Issue**:
   - Total Loki requests: Unknown (high volume)
   - Successful requests: 4 (in 30 minutes!)
   - **Conclusion**: Most requests are timing out or failing

3. **HTTP Transport Config**:
   ```go
   MaxConnsPerHost: 50  // Limit connections
   IdleConnTimeout: 90s
   ResponseHeaderTimeout: 240s
   DisableKeepAlives: false  // Pooling enabled
   ```

4. **dialConn Goroutine Growth**:
   - Baseline: 36 → Final: 198 (in 5 min)
   - Growth rate: 32/min (linear, consistent)
   - This suggests ~32 failed requests per minute

## Fixes Applied

### Fix 1: Read Response Body for 2xx Responses

**File**: `internal/sinks/loki_sink.go` (lines 933-963)

**Before** (leak):
```go
defer resp.Body.Close()
// ... check status ...
return nil  // Body not read for 2xx!
```

**After** (fixed):
```go
defer resp.Body.Close()

// Read body based on status
if resp.StatusCode >= 300 {
    bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
    errorBody = string(bodyBytes)
} else {
    // SUCCESS: drain body completely
    io.Copy(io.Discard, resp.Body)  // CRITICAL for connection reuse!
}
```

**Rationale**: HTTP connections can only be reused if the response body is fully read. Even empty (204 No Content) bodies must be drained.

### Fix 2: Added Debug Logging

Added logging to confirm body drainage:
```go
ls.logger.WithFields(logrus.Fields{
    "status":     resp.StatusCode,
    "bytes_read": bytesRead,
}).Debug("Loki request successful, body drained")
```

**Result**: No logs appeared → **Requests are NOT succeeding!**

## Why the Fix Didn't Work (Yet)

The HTTP body drainage fix is **correct** but incomplete because:

1. **Most requests are failing/timing out** (only 4 successes)
2. **Failed requests** don't reach the body-read code path
3. **Timeout/cancellation** might occur before `Do()` returns
4. **Connection pool exhaustion** when all 50 connections are stuck

### Likely Actual Cause

**Hypothesis**: Requests are **blocking on connection acquisition** because:
- All 50 connections are used
- Those connections are **waiting for response** but never completing
- New requests create new connections (beyond limit) and leak

## Next Steps (TODO)

1. **Enable verbose HTTP logging**:
   ```go
   Transport: &http.Transport{
       // ... existing config ...
       DisableCompression: false,  // Check if compression causes issues
   }
   ```

2. **Add request/response lifecycle logging**:
   - Log BEFORE `httpClient.Do(req)`
   - Log AFTER (success/failure)
   - Log body read operation

3. **Check Loki connectivity**:
   - DNS resolution (`ping loki` from container)
   - Network latency
   - Loki capacity/rate limiting

4. **Investigate timeout source**:
   - Is 240s timeout being hit?
   - Is circuit breaker opening?
   - Are requests being cancelled by sink shutdown?

5. **Consider alternative approaches**:
   - Disable keep-alives temporarily (`DisableKeepAlives: true`)
   - Reduce `MaxConnsPerHost` to force earlier backpressure
   - Add request queuing/throttling before HTTP client

6. **Monitor Loki side**:
   - Check Loki logs for errors
   - Check Loki metrics for dropped connections
   - Verify Loki is not overwhelmed

## Metrics Summary

**Before Fix**:
- Leak rate: 32/min
- dialConn goroutines: Growing linearly

**After Fix**:
- Leak rate: 32/min (no change)
- dialConn goroutines: Still growing
- **Conclusion**: Fix is necessary but not sufficient

## Files Modified

1. `/home/mateus/log_capturer_go/internal/sinks/loki_sink.go` (lines 933-963)
   - Added proper body drainage for 2xx responses
   - Added debug logging

## Production Readiness

**Status**: ❌ NOT READY

**Blockers**:
1. HTTP connection leak persists
2. Most Loki requests failing (only 4 successes)
3. Root cause not fully identified

**Safe to deploy**: NO - will cause goroutine exhaustion in production

## Time Spent

- Phase 1 (file_monitor): 15 min - ✅ Already fixed
- Phase 2 (pprof profiling): 20 min - ✅ Identified HTTP leak
- Phase 3 (HTTP fix attempts): 55 min - ⚠️ Fix applied but ineffective
- **Total**: 1.5 hours

## Recommendation

**PAUSE and reassess approach**:
1. Need to understand WHY requests are failing
2. May need to investigate Loki configuration/capacity
3. Consider if issue is environmental (Docker networking, etc.)
4. Might need to add request timeout/retry logic at a higher level

**Alternative quick win**:
- Temporarily set `DisableKeepAlives: true` to eliminate pooling
- This will reduce performance but stop the leak
- Use as interim solution while investigating root cause

---

**Next Session Goal**: Identify why Loki requests are failing/timing out  
**Estimated Time**: 1-2 hours
