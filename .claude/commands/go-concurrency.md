# Go Concurrency Analysis and Fixes

Use the Task tool to launch the golang agent with the following prompt:

You are tasked with analyzing and fixing concurrency issues in the log_capturer_go project. This is CRITICAL for system stability.

## Primary Analysis Tasks:

1. **Race Condition Detection**:
   - Run `go test -race ./...` on all packages
   - Identify data races in the codebase
   - Pay special attention to shared maps and state
   - Check dispatcher.go, monitors, and sinks

2. **Map Sharing Issues** (CRITICAL):
   - Find all instances where maps are passed between goroutines
   - Ensure proper deep copying of maps (especially LogEntry.Labels)
   - Look for patterns like:
     ```go
     entry := types.LogEntry{Labels: labels} // WRONG if labels is shared
     ```
   - Fix with proper copying:
     ```go
     labelsCopy := make(map[string]string, len(labels))
     for k, v := range labels {
         labelsCopy[k] = v
     }
     ```

3. **Mutex Analysis**:
   - Review all mutex usage
   - Check for proper lock/unlock patterns
   - Verify lock ordering to prevent deadlocks
   - Ensure RWMutex is used for read-heavy operations
   - Critical sections should be SHORT

4. **Goroutine Lifecycle**:
   - Verify all goroutines have proper shutdown mechanisms
   - Check for goroutine leaks
   - Ensure context propagation
   - Verify WaitGroup usage
   - Look for missing defer statements

5. **Context Propagation**:
   - Ensure all long-running operations accept context
   - Check ctx.Done() in loops
   - Verify timeout handling
   - Proper context cancellation

6. **Channel Usage**:
   - Review buffered vs unbuffered channels
   - Check for channel deadlocks
   - Verify proper channel closing
   - Look for select statement issues

Files to prioritize:
- internal/dispatcher/dispatcher.go
- internal/monitors/*.go
- internal/sinks/*.go
- pkg/task_manager/task_manager.go

## Deliverables:
1. List of all race conditions found
2. Fixed code for each race condition
3. Goroutine leak analysis report
4. Recommendations for concurrency improvements
5. Test cases to verify fixes

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Analyze and fix concurrency issues"
```

IMPORTANT: All fixes MUST be tested with `go test -race -count=100` to ensure stability!