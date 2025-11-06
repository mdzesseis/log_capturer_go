# Bug Fixing Command

Use the Task tool to launch the go-bugfixer agent:

```
Task(
    subagent_type="go-bugfixer",
    description="Fix bugs in Go code",
    prompt=[Bug description or error message]
)
```

## When to use this command:

- Fix race conditions
- Resolve memory leaks
- Fix goroutine leaks
- Handle panic recovery
- Fix nil pointer errors
- Resolve deadlocks
- Performance bug fixes

## Example requests:

1. **Race condition**:
   "Fix race condition: concurrent map write in dispatcher.go:145"

2. **Memory leak**:
   "Fix memory leak: goroutines increasing over time"

3. **Panic fix**:
   "Fix panic: runtime error: index out of range"

4. **Deadlock**:
   "Resolve deadlock in worker pool shutdown"

The agent provides root cause analysis and verified fixes!