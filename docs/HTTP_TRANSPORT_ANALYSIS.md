# HTTP Transport Configuration Analysis

**Date**: 2025-11-06
**Status**: Diagnostic Tool Created
**Purpose**: Verify MaxConnsPerHost configuration and prevent goroutine leaks

---

## Executive Summary

This document analyzes the HTTP Transport configuration in the log_capturer_go project, specifically focusing on the `MaxConnsPerHost` setting which is critical for preventing goroutine leaks in HTTP clients.

---

## Current Configuration Analysis

### 1. Loki Sink HTTP Transport (`internal/sinks/loki_sink.go:111-124`)

```go
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        MaxConnsPerHost:       50,  // ✅ CORRECTLY SET
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: timeout,
        DisableKeepAlives:     false,
        ForceAttemptHTTP2:     false,
    },
}
```

**Analysis**:
- ✅ `MaxConnsPerHost` is set to 50 (prevents unlimited connection growth)
- ✅ `MaxIdleConnsPerHost` (10) < `MaxConnsPerHost` (50) - correct ratio
- ✅ `DisableKeepAlives` is false - enables connection pooling
- ✅ `ForceAttemptHTTP2` is false - uses HTTP/1.1 (simpler connection model)
- ✅ Comprehensive timeouts configured

**Effectiveness**: **HIGH** - Configuration is correct and should prevent goroutine leaks

---

### 2. Docker Connection Pool (`pkg/docker/connection_pool.go:279-289`)

```go
httpClient := &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        MaxConnsPerHost:       50,  // ✅ CORRECTLY SET
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
    },
}
```

**Analysis**:
- ✅ `MaxConnsPerHost` is set to 50 (limits Docker daemon connections)
- ✅ Prevents goroutine accumulation from Docker API calls
- ✅ Identical configuration to Loki sink (consistency)

**Effectiveness**: **HIGH** - Configuration is correct

---

## How MaxConnsPerHost Works

### Connection Lifecycle

Each HTTP connection in Go spawns **2 goroutines**:
1. **Read loop** (`readLoop`) - reads from connection
2. **Write loop** (`writeLoop`) - writes to connection

**Without `MaxConnsPerHost`**:
```
Request 1 → Connection 1 → 2 goroutines
Request 2 → Connection 2 → 2 goroutines
Request 3 → Connection 3 → 2 goroutines
...
Request N → Connection N → 2N goroutines (LEAK!)
```

**With `MaxConnsPerHost=50`**:
```
Request 1-50  → Connections 1-50 → 100 goroutines
Request 51+   → Waits for available connection → Reuses existing
```

### Why Setting Matters

| Setting | Effect | Goroutine Count |
|---------|--------|-----------------|
| `MaxConnsPerHost=0` (default) | Unlimited connections | Unbounded growth |
| `MaxConnsPerHost=50` | Max 50 connections | Capped at 100 goroutines |
| `DisableKeepAlives=true` | No pooling | New connection per request |

---

## Diagnostic Tool

### Purpose

The diagnostic tool (`tools/http_transport_diagnostic.go`) verifies:

1. **Configuration Analysis** - Reads actual code and validates settings
2. **Runtime Enforcement** - Tests if `MaxConnsPerHost` is actually enforced
3. **Connection Reuse** - Verifies connection pooling is working
4. **Goroutine Leak Detection** - Monitors goroutine count during load
5. **Performance Benchmarks** - Compares different configurations

### Running the Diagnostic

```bash
# Build the diagnostic tool
cd /home/mateus/log_capturer_go
go build -o bin/http_transport_diagnostic tools/http_transport_diagnostic.go

# Run diagnostic
./bin/http_transport_diagnostic

# Save report
./bin/http_transport_diagnostic > reports/http_transport_diagnostic.json
```

### Expected Output

```json
{
  "timestamp": "2025-11-06T...",
  "go_version": "go1.21.x",
  "initial_goroutines": 5,
  "results": [
    {
      "test_name": "Loki Sink HTTP Transport Analysis",
      "status": "PASS",
      "details": {
        "max_conns_per_host_set": true,
        "max_conns_per_host_value": 50,
        "configuration_valid": true
      }
    },
    {
      "test_name": "MaxConnsPerHost Enforcement Test",
      "status": "PASS",
      "details": {
        "max_conns_limit": 5,
        "concurrent_requests": 20,
        "max_concurrent_connections_observed": 5,
        "enforcement": "MaxConnsPerHost is being enforced correctly"
      }
    }
  ],
  "overall_status": "PASS"
}
```

---

## Alternative Approaches (If MaxConnsPerHost Doesn't Work)

### Approach 1: Custom RoundTripper with Connection Counting

```go
type LimitedRoundTripper struct {
    base     http.RoundTripper
    sem      chan struct{}
    maxConns int
}

func NewLimitedRoundTripper(maxConns int) *LimitedRoundTripper {
    return &LimitedRoundTripper{
        base:     &http.Transport{/* config */},
        sem:      make(chan struct{}, maxConns),
        maxConns: maxConns,
    }
}

func (rt *LimitedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
    // Acquire semaphore slot
    rt.sem <- struct{}{}
    defer func() { <-rt.sem }()

    // Execute request
    return rt.base.RoundTrip(req)
}

// Usage
client := &http.Client{
    Transport: NewLimitedRoundTripper(50),
}
```

**Pros**:
- Explicit control over concurrent requests
- Easy to understand and debug
- Works with any Go version

**Cons**:
- Limits concurrent requests, not connections
- Higher overhead (semaphore acquire/release per request)
- Doesn't leverage connection pooling as effectively

---

### Approach 2: Disable Keep-Alive (NOT RECOMMENDED)

```go
httpClient := &http.Client{
    Transport: &http.Transport{
        DisableKeepAlives: true, // ❌ Don't do this
    },
}
```

**Pros**:
- Prevents connection accumulation
- Simple to implement

**Cons**:
- ❌ **Huge performance penalty** - new TCP connection per request
- ❌ Increases latency (TCP handshake + TLS handshake per request)
- ❌ Higher resource usage on both client and server
- ❌ Not suitable for high-throughput systems

---

### Approach 3: Connection Pool Manager

```go
type HTTPConnectionPool struct {
    clients  []*http.Client
    index    int32
    poolSize int
}

func NewHTTPConnectionPool(size int) *HTTPConnectionPool {
    pool := &HTTPConnectionPool{
        clients:  make([]*http.Client, size),
        poolSize: size,
    }

    for i := 0; i < size; i++ {
        pool.clients[i] = &http.Client{
            Transport: &http.Transport{
                MaxConnsPerHost: 10,
                // ... other settings
            },
        }
    }

    return pool
}

func (p *HTTPConnectionPool) GetClient() *http.Client {
    idx := atomic.AddInt32(&p.index, 1)
    return p.clients[int(idx)%p.poolSize]
}

// Usage
pool := NewHTTPConnectionPool(5) // 5 clients, each with max 10 conns = 50 total
client := pool.GetClient()
resp, err := client.Get(url)
```

**Pros**:
- Distributes load across multiple clients
- Easy to scale
- Can implement client-level policies

**Cons**:
- More complex
- Requires manual round-robin or load balancing
- Each client maintains separate connection pools

---

### Approach 4: Context-Based Request Limiting

```go
type RequestLimiter struct {
    sem chan struct{}
}

func NewRequestLimiter(maxConcurrent int) *RequestLimiter {
    return &RequestLimiter{
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (rl *RequestLimiter) Do(ctx context.Context, fn func() error) error {
    select {
    case rl.sem <- struct{}{}:
        defer func() { <-rl.sem }()
        return fn()
    case <-ctx.Done():
        return ctx.Err()
    }
}

// Usage
limiter := NewRequestLimiter(50)
err := limiter.Do(ctx, func() error {
    resp, err := httpClient.Get(url)
    // ... process response
    return err
})
```

**Pros**:
- Respects context cancellation
- Easy to integrate with existing code
- Works at application level

**Cons**:
- Limits concurrent requests, not connections
- Doesn't prevent connection pooling issues
- Requires refactoring all HTTP calls

---

## Recommendations

### ✅ Current Configuration is CORRECT

The current implementation using `MaxConnsPerHost=50` is the **best approach** for the following reasons:

1. **Native Go Support** - Uses built-in Transport settings (no custom code)
2. **Efficient Connection Pooling** - Reuses connections while limiting growth
3. **Low Overhead** - No additional semaphores or locking
4. **Well-Tested** - Standard library implementation
5. **Proven Effective** - Documented to prevent goroutine leaks

### Verification Steps

1. **Run Diagnostic Tool**:
   ```bash
   ./bin/http_transport_diagnostic
   ```

2. **Monitor in Production**:
   ```bash
   # Check goroutine count
   curl http://localhost:6060/debug/pprof/goroutine?debug=2 | grep "net/http"

   # Monitor metrics
   curl http://localhost:8001/metrics | grep goroutines
   ```

3. **Load Test**:
   ```bash
   # Run sustained load test
   go test -v ./tests/load -run TestSustainedLoad

   # Monitor goroutine growth
   watch -n 1 'curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep "goroutine profile:"'
   ```

### If Issues Persist

If goroutine leaks are still observed **despite** `MaxConnsPerHost=50`:

1. **Check Go Version** - Ensure Go 1.11+ (when MaxConnsPerHost was added)
2. **Verify HTTP/2** - Ensure `ForceAttemptHTTP2=false` (HTTP/2 uses different connection model)
3. **Check Response Body Closing** - Ensure all `resp.Body.Close()` calls are present
4. **Monitor Connection States** - Use `netstat` to check actual TCP connections:
   ```bash
   netstat -an | grep ESTABLISHED | grep <loki_port> | wc -l
   ```

5. **Use Diagnostic Tool** - Run full diagnostic to identify root cause

---

## Testing Checklist

- [ ] Run `http_transport_diagnostic` and verify all tests pass
- [ ] Monitor goroutine count during load test (should stabilize)
- [ ] Verify MaxConnsPerHost enforcement (max 50 connections observed)
- [ ] Check connection reuse (fewer connections than requests)
- [ ] Confirm no goroutine leak after requests complete
- [ ] Benchmark performance with current configuration

---

## References

- [Go HTTP Transport Documentation](https://pkg.go.dev/net/http#Transport)
- [MaxConnsPerHost Implementation](https://github.com/golang/go/blob/master/src/net/http/transport.go)
- [Go HTTP Connection Pooling](https://cs.opensource.google/go/go/+/refs/tags/go1.21.0:src/net/http/transport.go;l=80)
- [Goroutine Leak Detection](https://go.uber.org/goleak)

---

## Conclusion

The current HTTP Transport configuration in both **Loki Sink** and **Docker Connection Pool** is **CORRECT** and should effectively prevent goroutine leaks:

- ✅ `MaxConnsPerHost=50` is set
- ✅ Connection pooling is enabled
- ✅ Appropriate timeouts configured
- ✅ HTTP/1.1 used (simpler connection model)

The diagnostic tool can be used to **verify** this configuration is working as expected at runtime.
