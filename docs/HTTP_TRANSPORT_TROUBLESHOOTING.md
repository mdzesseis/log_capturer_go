# HTTP Transport Troubleshooting Guide

**Purpose**: Step-by-step troubleshooting for HTTP transport and connection issues

---

## Quick Diagnosis

### Run This First

```bash
# Run full diagnostic
./tools/run_http_diagnostic.sh

# If diagnostic shows issues, continue with specific tests below
```

---

## Problem 1: Goroutine Count Growing

### Symptoms

```bash
$ curl http://localhost:6060/debug/pprof/goroutine?debug=1
goroutine profile: total 5432  # Increases over time
```

### Step 1: Check MaxConnsPerHost Setting

```bash
# Check Loki Sink
grep -A 3 "MaxConnsPerHost" internal/sinks/loki_sink.go

# Expected output:
# MaxConnsPerHost:       50,
```

If you see:
- ❌ `MaxConnsPerHost: 0` - **PROBLEM FOUND**
- ❌ Line not present - **PROBLEM FOUND**
- ✅ `MaxConnsPerHost: 50` - Setting is correct

**Fix if needed**:
```go
Transport: &http.Transport{
    MaxConnsPerHost: 50,  // Add this line
    // ... other settings
}
```

### Step 2: Verify Enforcement

```bash
# Run enforcement test
./bin/http_transport_diagnostic | jq '.results[] | select(.test_name == "MaxConnsPerHost Enforcement Test")'

# Expected:
# "status": "PASS"
# "max_concurrent_connections_observed": 5  (should be <= limit)
```

If test **FAILS**:
- Check Go version: `go version` (need 1.11+)
- Check if HTTP/2 is being used (should be disabled)
- Check for custom RoundTripper overriding settings

### Step 3: Check Response Body Closing

```bash
# Search for HTTP requests without body close
grep -n "httpClient.Do\|http.Get\|http.Post" internal/sinks/*.go | while read line; do
  file=$(echo $line | cut -d: -f1)
  linenum=$(echo $line | cut -d: -f2)

  # Check next 10 lines for Close()
  sed -n "${linenum},$((linenum+10))p" "$file" | grep -q "Close()" || echo "Missing Close() at $file:$linenum"
done
```

**Fix pattern**:
```go
resp, err := httpClient.Do(req)
if err != nil {
    return err
}
defer resp.Body.Close()  // ⭐ MUST HAVE THIS
```

### Step 4: Monitor Goroutines by Source

```bash
# Detailed goroutine breakdown
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Count HTTP-related goroutines
grep -c "net/http" goroutines.txt

# If count is high (>200), you have an HTTP goroutine leak
```

### Step 5: Force GC and Check Again

```bash
# Before GC
BEFORE=$(curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep -oE 'total [0-9]+' | grep -oE '[0-9]+')

# Trigger GC
curl -X POST http://localhost:8401/debug/gc

# Wait 10 seconds
sleep 10

# After GC
AFTER=$(curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | grep -oE 'total [0-9]+' | grep -oE '[0-9]+')

echo "Before: $BEFORE, After: $AFTER, Delta: $((AFTER - BEFORE))"
```

If delta is large (>50), you have a leak.

---

## Problem 2: High Connection Count

### Symptoms

```bash
$ netstat -an | grep ESTABLISHED | grep <loki_port> | wc -l
150  # Much higher than MaxConnsPerHost (50)
```

### Step 1: Check Multiple Clients

```bash
# Count HTTP clients created
grep -rn "http.Client{" internal/ pkg/ | wc -l

# If >1, you may have multiple clients bypassing the limit
```

**Fix**: Use a single shared HTTP client:
```go
// Global or singleton
var httpClient *http.Client

func init() {
    httpClient = &http.Client{
        Transport: &http.Transport{
            MaxConnsPerHost: 50,
        },
    }
}
```

### Step 2: Check Connection States

```bash
# Breakdown of connection states
netstat -an | grep <loki_port> | awk '{print $6}' | sort | uniq -c

# Expected output:
#   50 ESTABLISHED  (active connections)
#   10 TIME_WAIT    (closing connections)
```

If you see many TIME_WAIT, connections are being closed frequently (not reused).

### Step 3: Verify Keep-Alive

```bash
# Check if keep-alive is disabled
grep "DisableKeepAlives" internal/sinks/loki_sink.go

# Expected:
# DisableKeepAlives: false,
```

If `true`, change to `false` to enable connection reuse.

### Step 4: Check Target Host Limits

```bash
# Test if Loki is limiting connections
curl -I http://<loki_host>:<loki_port>/ready

# Look for:
# Connection: keep-alive  ✅ Good
# Connection: close       ❌ Server forcing new connections
```

---

## Problem 3: High Latency

### Symptoms

Requests take longer than expected despite low CPU usage.

### Step 1: Check Connection Pool Saturation

```bash
# Run benchmark
./bin/http_transport_diagnostic | jq '.results[] | select(.test_name == "MaxConnsPerHost Configuration Benchmark")'

# Compare unlimited vs limited performance
```

If limited configs are **much slower**, you may need to increase MaxConnsPerHost.

### Step 2: Check Queue Depth

```bash
# Monitor Loki sink queue
curl http://localhost:8001/metrics | grep loki_queue_size

# If consistently high (>80% capacity), increase queue or workers
```

### Step 3: Profile Blocking

```bash
# Get blocking profile
curl http://localhost:6060/debug/pprof/block > block.prof

# Analyze
go tool pprof -http=:8080 block.prof

# Look for high blocking in HTTP transport
```

### Step 4: Increase MaxConnsPerHost (if needed)

```go
Transport: &http.Transport{
    MaxConnsPerHost: 100,  // Increase from 50
    // ... other settings
}
```

**Trade-off**: More connections = more goroutines + memory

---

## Problem 4: Connection Refused Errors

### Symptoms

```
failed to send request: dial tcp: connection refused
```

### Step 1: Check Target Host

```bash
# Test connectivity
curl -I http://<loki_host>:<loki_port>/ready

# If fails, Loki is down or unreachable
```

### Step 2: Check File Descriptor Limits

```bash
# Check current limits
ulimit -n

# If low (<1024), increase:
ulimit -n 4096

# Or in systemd service:
# LimitNOFILE=4096
```

### Step 3: Check MaxConnsPerHost

If MaxConnsPerHost is too high and Loki is limiting, reduce:

```go
MaxConnsPerHost: 25,  // Reduce from 50
```

### Step 4: Check Circuit Breaker

```bash
# Check if circuit breaker is open
curl http://localhost:8001/metrics | grep circuit_breaker_state

# If state=open, breaker is blocking requests
```

---

## Problem 5: Memory Growth

### Symptoms

Memory usage increases over time.

### Step 1: Check Connection Pooling

```bash
# Get heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof

# Analyze
go tool pprof -http=:8080 heap.prof

# Look for HTTP buffer allocations
```

### Step 2: Check MaxIdleConns

```bash
grep "MaxIdleConns" internal/sinks/loki_sink.go

# If very high (>1000), reduce:
MaxIdleConns: 100,
MaxIdleConnsPerHost: 10,
```

### Step 3: Force Connection Cleanup

```bash
# Close idle connections periodically
# Add to Loki Sink:

func (ls *LokiSink) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            ls.httpClient.CloseIdleConnections()
        case <-ls.ctx.Done():
            return
        }
    }
}
```

---

## Problem 6: Diagnostic Tool Failing

### Symptoms

```
[FAIL] MaxConnsPerHost Enforcement Test
```

### Step 1: Check Go Version

```bash
go version

# Need go1.11 or higher for MaxConnsPerHost
```

### Step 2: Check Test Server

The diagnostic uses `httptest.NewServer` which may behave differently than production.

**Action**: Focus on production metrics rather than test results.

### Step 3: Verify in Production

```bash
# Monitor actual connections during load
watch -n 1 'netstat -an | grep ESTABLISHED | grep <loki_port> | wc -l'

# Run load test
go test -v ./tests/load -run TestSustainedLoad

# Max connections should be <= MaxConnsPerHost
```

---

## Monitoring Commands

### Real-Time Goroutine Count

```bash
watch -n 1 'curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -1'
```

### Real-Time Connection Count

```bash
watch -n 1 'netstat -an | grep ESTABLISHED | grep <loki_port> | wc -l'
```

### HTTP Metrics

```bash
watch -n 5 'curl -s http://localhost:8001/metrics | grep -E "(http_requests_total|http_request_duration)"'
```

### Queue Utilization

```bash
watch -n 1 'curl -s http://localhost:8001/metrics | grep sink_queue_utilization'
```

---

## Quick Fixes

### Fix 1: Add MaxConnsPerHost

```go
httpClient := &http.Client{
    Transport: &http.Transport{
        MaxConnsPerHost: 50,  // Add this line
    },
}
```

### Fix 2: Ensure Body Close

```go
resp, err := client.Do(req)
if err != nil {
    return err
}
defer resp.Body.Close()  // Add this line
_, err = io.ReadAll(resp.Body)
```

### Fix 3: Add Connection Cleanup

```go
// In Stop() method
ls.httpClient.CloseIdleConnections()
```

### Fix 4: Add Timeout Context

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, "POST", url, body)
```

---

## When to Contact Maintainers

Contact if:
- Diagnostic shows FAIL and you've followed all steps
- Goroutine count >2000 despite MaxConnsPerHost=50
- Connection count exceeds MaxConnsPerHost by 2x
- Memory leak persists after fixes
- Performance degradation unexplained

Provide:
- Output of `./tools/run_http_diagnostic.sh`
- Goroutine profile: `curl http://localhost:6060/debug/pprof/goroutine?debug=2`
- Heap profile: `curl http://localhost:6060/debug/pprof/heap`
- Metrics snapshot: `curl http://localhost:8001/metrics`

---

## References

- [HTTP Transport Analysis](/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_ANALYSIS.md)
- [Quick Reference](/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_QUICK_REFERENCE.md)
- [Diagnostic Summary](/home/mateus/log_capturer_go/docs/HTTP_TRANSPORT_DIAGNOSTIC_SUMMARY.md)
- [Go HTTP Transport Docs](https://pkg.go.dev/net/http#Transport)

---

**Last Updated**: 2025-11-06
