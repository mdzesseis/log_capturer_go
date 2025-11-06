# Go Code Review

Use the Task tool to launch the golang agent with the following prompt:

You are performing a comprehensive code review for the log_capturer_go project. Follow the project's CLAUDE.md guidelines strictly.

## Code Review Checklist:

### 1. Concurrency Safety ✓
- [ ] No goroutine leaks (proper lifecycle management)
- [ ] No race conditions (test with -race)
- [ ] Maps are deep copied when shared between goroutines
- [ ] Mutexes protect all shared state
- [ ] Context propagation in all long operations
- [ ] Proper lock ordering to avoid deadlocks

### 2. Resource Management ✓
- [ ] All resources have defer cleanup
- [ ] File descriptors are closed
- [ ] Contexts are cancelled
- [ ] Timers/Tickers are stopped
- [ ] Database connections are returned to pool
- [ ] HTTP bodies are closed

### 3. Error Handling ✓
- [ ] Errors are properly wrapped with context
- [ ] No ignored errors (or explicitly with _)
- [ ] Sentinel errors for known conditions
- [ ] Error messages are descriptive
- [ ] Panic recovery where appropriate

### 4. Performance ✓
- [ ] No unnecessary allocations
- [ ] String operations optimized
- [ ] Appropriate use of pointers vs values
- [ ] sync.Pool for frequently allocated objects
- [ ] Batching for bulk operations
- [ ] Proper buffer sizes

### 5. Testing ✓
- [ ] Test coverage ≥ 70%
- [ ] Table-driven tests
- [ ] Race condition tests
- [ ] Benchmark tests for critical paths
- [ ] Error scenarios tested
- [ ] Cleanup in tests

### 6. Code Quality ✓
- [ ] Follows Go idioms
- [ ] Clear variable/function names
- [ ] Appropriate comments
- [ ] No naked returns
- [ ] No global state
- [ ] Dependency injection used
- [ ] Interfaces are small and focused

### 7. Security ✓
- [ ] Input validation
- [ ] No sensitive data in logs
- [ ] SQL injection prevention
- [ ] Command injection prevention
- [ ] Proper authentication/authorization
- [ ] TLS for network communication

## Files/Package to Review:
{{FILE_OR_PACKAGE_PATH}}

## Review Process:
1. Check the code against each checklist item
2. Identify specific issues with line numbers
3. Provide fixes for critical issues
4. Suggest improvements for non-critical items
5. Highlight good practices found

## Expected Output:
```markdown
## Code Review Report

### Critical Issues 🔴
1. [File:Line] Description of issue
   - Impact: ...
   - Fix: ```go ... ```

### Important Issues 🟡
1. [File:Line] Description
   - Recommendation: ...

### Minor Issues 🟢
1. [File:Line] Description
   - Suggestion: ...

### Good Practices Found ✨
- ...

### Overall Score: X/10
```

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Perform comprehensive Go code review"
```

Focus on actionable feedback with specific code examples for fixes.