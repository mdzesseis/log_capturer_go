# Go Commands Help - Quick Reference

## Available Go Commands for log_capturer_go

This is a quick reference for all Go-specific commands that utilize the golang agent in `.claude/agents/golang.md`.

### Core Commands

#### `/golang` - General Go Development
- General Go development assistance
- Writing new Go code
- Refactoring existing code
- Implementing best practices

#### `/go-optimize` - Performance Optimization
- Memory optimization and allocation reduction
- Concurrency improvements
- Performance profiling and hot path analysis
- Implement object pools and batching

#### `/go-test` - Testing & Coverage
- Improve test coverage (target: 70%+)
- Write table-driven tests
- Add benchmark tests
- Race condition testing
- Integration test creation

#### `/go-concurrency` - Concurrency Analysis
- **CRITICAL**: Fix race conditions
- Analyze map sharing issues
- Review mutex usage and deadlocks
- Verify goroutine lifecycle
- Context propagation checks

#### `/go-leak` - Resource Leak Detection
- **CRITICAL**: Detect goroutine leaks
- Find memory leaks
- Check file descriptor leaks
- Verify timer/ticker cleanup
- Context cancellation audit

#### `/go-review` - Code Review
- Comprehensive code review checklist
- Security analysis
- Performance review
- Best practices verification
- Test coverage assessment

#### `/go-debug` - Debugging & Profiling
- CPU profiling setup and analysis
- Memory profiling and leak detection
- Goroutine analysis
- Block and mutex profiling
- Execution tracing

### Usage Examples

```bash
# General Go help
/golang

# Optimize dispatcher performance
/go-optimize
# Then specify: internal/dispatcher/dispatcher.go

# Fix concurrency issues in monitors
/go-concurrency
# Focus on: internal/monitors/*.go

# Detect resource leaks
/go-leak

# Review code before committing
/go-review
# Specify package: ./internal/sinks

# Debug high CPU usage
/go-debug
# Describe issue: High CPU usage in production

# Improve test coverage
/go-test
# Target package: ./pkg/task_manager
```

### Quick Tips

1. **Always run with race detector**: `go test -race ./...`
2. **Profile before optimizing**: Use `/go-debug` first
3. **Fix concurrency issues immediately**: Use `/go-concurrency`
4. **Review critical packages**: dispatcher, sinks, monitors
5. **Target 70% test coverage minimum**: Use `/go-test`

### Project-Specific Focus Areas

Based on CLAUDE.md guidelines, prioritize:
- **Concurrency safety** (no race conditions)
- **Resource management** (no leaks)
- **Performance** (efficient memory usage)
- **Testing** (comprehensive coverage)
- **Error handling** (proper wrapping)

### How Commands Work

All these commands use the Task tool to invoke the golang agent:
```
Task(
    subagent_type="golang",
    description="[specific task]",
    prompt="[detailed instructions]"
)
```

The golang agent is specialized for the log_capturer_go project and follows all patterns defined in CLAUDE.md.

### Need More Help?

- Check `CLAUDE.md` for detailed patterns
- Review existing code in `internal/` and `pkg/`
- Run `/go-review` for code quality checks
- Use `/go-debug` for performance issues