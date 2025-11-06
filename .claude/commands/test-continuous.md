# Continuous Testing Command

Use the Task tool to launch the continuous-tester agent:

```
Task(
    subagent_type="continuous-tester",
    description="Run continuous tests and monitoring",
    prompt=[Your testing request]
)
```

## When to use this command:

- Set up continuous testing for code changes
- Run comprehensive test suites
- Detect performance regressions
- Monitor for memory leaks
- Validate bug fixes
- Run load and stress tests

## Example requests:

1. **Monitor code changes**:
   "Set up continuous testing to run on every file change"

2. **Performance testing**:
   "Run performance benchmarks and detect regressions"

3. **Memory leak detection**:
   "Check for goroutine and memory leaks in the dispatcher"

4. **Race condition testing**:
   "Run race detector on all packages"

The agent will automatically report issues to the appropriate fixing agents!