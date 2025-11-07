# HTTP Transport Quick Reference Guide

## TL;DR - Quick Commands

```bash
# Run diagnostic
./tools/run_http_diagnostic.sh

# Check if MaxConnsPerHost is working
curl http://localhost:6060/debug/pprof/goroutine?debug=2 | grep -c "net/http"

# Monitor goroutine count (should stabilize, not grow)
watch -n 1 'curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1'
```

---

## HTTP Transport Settings Cheat Sheet

### Recommended Configuration (Current Setup)

```go
httpClient := &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:          100,  // Total idle connections
        MaxIdleConnsPerHost:   10,   // Idle per host
        MaxConnsPerHost:       50,   // ⭐ CRITICAL: Limit total connections
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
        DisableKeepAlives:     false, // Keep pooling enabled
        ForceAttemptHTTP2:     false, // Stick to HTTP/1.1
    },
    Timeout: 30 * time.Second,
}
```

---

## What Each Setting Does

| Setting | Purpose | Impact on Goroutines |
|---------|---------|---------------------|
| `MaxIdleConns` | Total idle connections in pool | Indirect - limits reusable connections |
| `MaxIdleConnsPerHost` | Idle connections per host | Indirect - limits per-host reuse |
| **`MaxConnsPerHost`** | **Max total connections per host** | **Direct - each connection = 2 goroutines** |
| `IdleConnTimeout` | How long idle connections live | Indirect - cleanup stale connections |
| `DisableKeepAlives` | Disable connection reuse | Direct - creates new connection per request |

---

## Goroutine Math

Each HTTP connection spawns **2 goroutines**:
- `readLoop` (reading responses)
- `writeLoop` (writing requests)

**Example**:
```
MaxConnsPerHost=0 (unlimited) + 1000 requests = ~2000 goroutines 📈 LEAK!
MaxConnsPerHost=50 + 1000 requests = ~100 goroutines ✅ STABLE
```

---

## Common Scenarios

### Scenario 1: Goroutine Count Growing

**Symptom**:
```bash
$ curl http://localhost:6060/debug/pprof/goroutine?debug=1
goroutine profile: total 5432  # Keeps growing!
```

**Fix**:
1. Check `MaxConnsPerHost` is set (not 0)
2. Ensure all `resp.Body.Close()` are called
3. Verify no HTTP/2 (use `ForceAttemptHTTP2=false`)

---

### Scenario 2: High Latency, Low Throughput

**Symptom**: Requests are slow despite low CPU

**Possible Cause**: `MaxConnsPerHost` too low (queueing requests)

**Fix**:
```go
MaxConnsPerHost: 100,  // Increase from 50
```

**Trade-off**: More goroutines vs higher throughput

---

### Scenario 3: Connection Refused Errors

**Symptom**: `connection refused` or `too many open files`

**Possible Cause**: Too many connections to target host

**Fix**:
```go
MaxConnsPerHost: 25,  // Decrease from 50
```

Or increase OS limits:
```bash
ulimit -n 4096
```

---

## How to Verify Configuration

### 1. Static Code Check

```bash
# Check Loki Sink
grep -A 10 "MaxConnsPerHost" internal/sinks/loki_sink.go

# Check Docker Pool
grep -A 10 "MaxConnsPerHost" pkg/docker/connection_pool.go
```

**Expected Output**:
```go
MaxConnsPerHost:       50,  // Should NOT be 0
```

---

### 2. Runtime Verification

```bash
# Run diagnostic tool
./tools/run_http_diagnostic.sh
```

**Look for**:
```
[PASS] MaxConnsPerHost Enforcement Test
  max_conns_limit: 5
  max_concurrent_connections_observed: 5  # Should be <= limit
```

---

### 3. Load Test Verification

```bash
# Before load test
BEFORE=$(curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1 | grep -oE '[0-9]+')

# Run load test
go test -v ./tests/load -run TestSustainedLoad

# After load test (wait 30s for cleanup)
sleep 30
AFTER=$(curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1 | grep -oE '[0-9]+')

# Compare
echo "Before: $BEFORE, After: $AFTER, Delta: $((AFTER - BEFORE))"
```

**Expected**: Delta should be small (<20 goroutines)

---

## Troubleshooting

### Problem: Diagnostic shows FAIL

**Check**:
1. Go version (`go version`) - need 1.11+
2. Code hasn't been modified
3. Server is actually limiting connections

**Debug**:
```bash
# Check actual TCP connections
netstat -an | grep ESTABLISHED | grep <loki_port> | wc -l
# Should be <= MaxConnsPerHost
```

---

### Problem: Goroutines still leaking

**Other causes** (not MaxConnsPerHost):
1. Response bodies not closed
   ```go
   resp, err := client.Get(url)
   if err != nil {
       return err
   }
   defer resp.Body.Close() // ⭐ MUST HAVE THIS
   io.ReadAll(resp.Body)
   ```

2. Context not cancelled
   ```go
   ctx, cancel := context.WithCancel(context.Background())
   defer cancel() // ⭐ MUST CANCEL
   ```

3. HTTP/2 multiplexing
   ```go
   ForceAttemptHTTP2: false, // ⭐ Disable HTTP/2
   ```

---

## When to Adjust MaxConnsPerHost

### Increase (50 → 100) if:
- ✅ High throughput requirements (>10k req/s)
- ✅ Low target host latency (<10ms)
- ✅ Target host can handle many connections
- ✅ Have plenty of RAM/file descriptors

### Decrease (50 → 25) if:
- ✅ Target host is rate-limiting
- ✅ Hitting file descriptor limits
- ✅ Want to limit memory usage
- ✅ Target host is a shared service

### Keep at 50 if:
- ✅ Current setup is working
- ✅ No goroutine leaks detected
- ✅ Performance is acceptable
- ✅ Resource usage is reasonable

---

## Monitoring in Production

### Metrics to Watch

```bash
# Goroutine count (should stabilize)
curl http://localhost:8001/metrics | grep 'go_goroutines'

# Active connections (should be <= MaxConnsPerHost)
netstat -an | grep ESTABLISHED | grep <target_host> | wc -l

# HTTP request duration (should be consistent)
curl http://localhost:8001/metrics | grep 'http_request_duration'
```

### Alerts to Set

```yaml
# Grafana alert
- alert: HighGoroutineCount
  expr: go_goroutines > 1000
  for: 5m
  annotations:
    description: "Goroutine count is {{ $value }}, possible leak"

- alert: HTTPConnectionPoolExhausted
  expr: rate(http_client_connection_errors_total[5m]) > 10
  annotations:
    description: "HTTP connection pool is exhausted"
```

---

## Code Examples

### ✅ CORRECT - Full Example

```go
func SendToLoki(entries []LogEntry) error {
    // Create client with proper limits
    httpClient := &http.Client{
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 10,
            MaxConnsPerHost:     50, // ⭐ Set limit
            IdleConnTimeout:     90 * time.Second,
        },
        Timeout: 30 * time.Second,
    }

    // Create request
    data, _ := json.Marshal(entries)
    req, err := http.NewRequest("POST", lokiURL, bytes.NewReader(data))
    if err != nil {
        return err
    }

    // Send request
    resp, err := httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close() // ⭐ Always close

    // Read response
    _, err = io.ReadAll(resp.Body)
    return err
}
```

---

### ❌ WRONG - Common Mistakes

```go
// Mistake 1: Missing MaxConnsPerHost
httpClient := &http.Client{
    Transport: &http.Transport{
        // MaxConnsPerHost not set - defaults to 0 (unlimited)!
        MaxIdleConns: 100,
    },
}

// Mistake 2: Not closing response body
resp, err := httpClient.Get(url)
if err != nil {
    return err
}
// Missing: defer resp.Body.Close()

// Mistake 3: Disabling keep-alive
httpClient := &http.Client{
    Transport: &http.Transport{
        DisableKeepAlives: true, // ❌ Creates new connection per request!
    },
}

// Mistake 4: Using default http.Client
resp, err := http.Get(url) // ❌ Uses http.DefaultClient (no limits)
```

---

## Files to Review

1. **Loki Sink**: `/home/mateus/log_capturer_go/internal/sinks/loki_sink.go:111-124`
2. **Docker Pool**: `/home/mateus/log_capturer_go/pkg/docker/connection_pool.go:279-289`
3. **Diagnostic Tool**: `/home/mateus/log_capturer_go/tools/http_transport_diagnostic.go`
4. **Analysis Doc**: `/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_ANALYSIS.md`

---

## Quick Diagnostic Checklist

- [ ] `MaxConnsPerHost` is set (not 0) ✅
- [ ] `MaxIdleConnsPerHost` <= `MaxConnsPerHost` ✅
- [ ] `DisableKeepAlives` is false ✅
- [ ] All `resp.Body.Close()` are called ✅
- [ ] `ForceAttemptHTTP2` is false ✅
- [ ] Diagnostic tool passes all tests ✅
- [ ] Goroutine count stabilizes under load ✅
- [ ] No TCP connection leaks (`netstat` check) ✅

---

## Further Reading

- [Go http.Transport Documentation](https://pkg.go.dev/net/http#Transport)
- [MaxConnsPerHost Source Code](https://cs.opensource.google/go/go/+/refs/tags/go1.21.0:src/net/http/transport.go;l=167)
- [HTTP Connection Pooling in Go](https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/)
- [Goroutine Leak Detection](https://go.uber.org/goleak)

---

**Last Updated**: 2025-11-06
**Maintained By**: log_capturer_go team
