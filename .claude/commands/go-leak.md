# Go Resource Leak Detection and Prevention

Use the Task tool to launch the golang agent with the following prompt:

You are tasked with detecting and fixing resource leaks in the log_capturer_go project. This is CRITICAL for long-running stability.

## Resource Leak Detection Tasks:

1. **Goroutine Leak Detection**:
   - Profile goroutines: `curl http://localhost:6060/debug/pprof/goroutine?debug=2`
   - Check for increasing goroutine count over time
   - Find goroutines without proper lifecycle management
   - Verify all goroutines can be stopped
   - Use goleak in tests:
     ```go
     func TestNoLeak(t *testing.T) {
         defer goleak.VerifyNone(t)
         // test code
     }
     ```

2. **Memory Leak Analysis**:
   - Profile heap: `curl http://localhost:6060/debug/pprof/heap`
   - Look for:
     - Slice reslicing that keeps underlying arrays
     - Maps that grow indefinitely
     - Uncleaned sync.Pool objects
     - Circular references preventing GC
   - Common patterns to fix:
     ```go
     // WRONG - keeps entire array
     batch = batch[n:]

     // CORRECT - reallocate
     newBatch := make([]LogEntry, len(batch)-n)
     copy(newBatch, batch[n:])
     batch = newBatch
     ```

3. **File Descriptor Leaks**:
   - Check all file operations have defer close()
   - Verify HTTP client/server cleanup
   - Check database connection pools
   - Monitor with: `lsof -p <pid> | wc -l`

4. **Channel Leaks**:
   - Find channels that are never closed
   - Check for goroutines blocked on channels
   - Verify proper select with context.Done()

5. **Timer/Ticker Leaks**:
   - Ensure all time.Timer and time.Ticker are stopped
   - Check for:
     ```go
     timer := time.NewTimer(duration)
     defer timer.Stop() // MUST have this
     ```

6. **Context Leak Prevention**:
   - Verify all contexts are cancelled
   - Check for:
     ```go
     ctx, cancel := context.WithCancel(parent)
     defer cancel() // MUST have this
     ```

## Analysis Focus Areas:
- internal/dispatcher/dispatcher.go (retry mechanism)
- internal/monitors/container_monitor.go (Docker API)
- internal/monitors/file_monitor.go (file handles)
- internal/sinks/*.go (network connections)
- pkg/task_manager/task_manager.go (goroutine management)

## Tools to Use:
```bash
# Goroutine profiling
go test -run TestLeak -memprofile mem.prof
go tool pprof mem.prof

# Race detection
go test -race ./...

# Leak detection
go test -tags leak ./...
```

## Deliverables:
1. Complete resource leak audit report
2. Fixed code for all leaks found
3. Test cases using goleak
4. Monitoring recommendations
5. Performance impact analysis

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Detect and fix resource leaks"
```

CRITICAL: Every resource acquisition MUST have a corresponding cleanup with defer!