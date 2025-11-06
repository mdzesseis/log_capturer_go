# Go Development Assistant

Use the Task tool to launch the golang agent for Go-specific development tasks.

## When to use this command:
- Writing new Go code for the project
- Refactoring existing Go code
- Implementing Go best practices
- Solving Go-specific problems
- Optimizing Go performance
- Fixing concurrency issues

## How to invoke:
Use the Task tool with the following parameters:

```
subagent_type: "golang"
description: "Help with Go development"
prompt: [Your specific Go development request]
```

## Example prompts:
1. "Implement a worker pool pattern for the dispatcher"
2. "Refactor this code to use Go idioms and best practices"
3. "Add proper error handling to this function"
4. "Optimize this code for better performance"
5. "Fix the race condition in this concurrent code"
6. "Write table-driven tests for this package"

## Agent Capabilities:
The golang agent specializes in:
- **Concurrency patterns**: goroutines, channels, sync primitives
- **Memory optimization**: reducing allocations, using sync.Pool
- **Error handling**: proper error wrapping and propagation
- **Testing**: table-driven tests, benchmarks, race detection
- **Performance**: profiling, optimization, GC tuning
- **Code organization**: package structure, interfaces, dependency injection
- **Resource management**: preventing leaks, proper cleanup

## Related Commands:
- `/go-optimize` - Performance optimization focus
- `/go-test` - Testing and coverage improvements
- `/go-concurrency` - Concurrency analysis and fixes
- `/go-leak` - Resource leak detection

The agent will provide Go-specific improvements focused on performance, correctness, and maintainability following the patterns defined in the project's CLAUDE.md guide.