# Go Debugging and Profiling

Use the Task tool to launch the golang agent with the following prompt:

You are debugging and profiling the log_capturer_go application to identify and resolve performance issues and bugs.

## Debugging Tasks:

### 1. Profiling Setup
First, ensure profiling is enabled:
```go
import _ "net/http/pprof"

func main() {
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
    // ... rest of main
}
```

### 2. CPU Profiling
```bash
# Capture 30-second CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8080 cpu.prof
```
Analyze:
- Hot paths (functions consuming most CPU)
- Unnecessary loops
- Inefficient algorithms
- Excessive string operations

### 3. Memory Profiling
```bash
# Heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8080 heap.prof

# Allocs profile
curl http://localhost:6060/debug/pprof/allocs > allocs.prof
go tool pprof -http=:8080 allocs.prof
```
Look for:
- Memory leaks (growing heap)
- Excessive allocations
- Large objects in memory
- Inefficient data structures

### 4. Goroutine Analysis
```bash
# Goroutine dump
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt

# Goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof goroutine.prof
```
Check for:
- Goroutine leaks (count increasing)
- Blocked goroutines
- Deadlocks
- Excessive goroutine creation

### 5. Block Profiling
Enable in code:
```go
runtime.SetBlockProfileRate(1)
```
Then:
```bash
curl http://localhost:6060/debug/pprof/block > block.prof
go tool pprof block.prof
```
Identify:
- Lock contention
- Channel blocking
- I/O wait times

### 6. Mutex Profiling
Enable in code:
```go
runtime.SetMutexProfileFraction(1)
```
Then:
```bash
curl http://localhost:6060/debug/pprof/mutex > mutex.prof
go tool pprof mutex.prof
```

### 7. Trace Analysis
```bash
# Capture execution trace
curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```
Analyze:
- Goroutine scheduling
- GC behavior
- System calls
- Network/Block I/O

### 8. Memory Stats
```go
// Add endpoint for runtime stats
func memStats(w http.ResponseWriter, r *http.Request) {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)

    fmt.Fprintf(w, "Alloc = %v MB\n", m.Alloc / 1024 / 1024)
    fmt.Fprintf(w, "TotalAlloc = %v MB\n", m.TotalAlloc / 1024 / 1024)
    fmt.Fprintf(w, "Sys = %v MB\n", m.Sys / 1024 / 1024)
    fmt.Fprintf(w, "NumGC = %v\n", m.NumGC)
    fmt.Fprintf(w, "NumGoroutine = %v\n", runtime.NumGoroutine())
}
```

### 9. Debug Logging
Add detailed logging for debugging:
```go
// Conditional debug logging
if log.IsLevelEnabled(log.DebugLevel) {
    log.WithFields(log.Fields{
        "goroutines": runtime.NumGoroutine(),
        "queue_size": len(queue),
        "workers":    activeWorkers,
    }).Debug("dispatcher status")
}
```

### 10. Race Detection
```bash
# Build with race detector
go build -race

# Run tests with race detector
go test -race -count=100 ./...
```

## Problem to Debug:
{{DESCRIBE_ISSUE}}

## Expected Deliverables:
1. Root cause analysis
2. Profile analysis with screenshots
3. Specific bottlenecks identified
4. Fix recommendations with code
5. Before/after performance metrics
6. Test to prevent regression

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Debug and profile Go application"
```

Remember to provide specific metrics and measurable improvements.