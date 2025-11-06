# Go Testing and Coverage Analysis

Use the Task tool to launch the golang agent with the following prompt:

You are tasked with improving test coverage and test quality in the log_capturer_go project. Your mission includes:

1. **Test Coverage Analysis**:
   - Run `go test -coverprofile=coverage.out ./...`
   - Analyze current coverage (target: 70% minimum)
   - Identify untested code paths
   - Focus on critical packages (dispatcher, sinks, monitors)

2. **Write Missing Tests**:
   - Create table-driven tests for complex functions
   - Add edge case testing
   - Implement error scenario tests
   - Add benchmark tests for performance-critical code

3. **Race Condition Testing**:
   - Run tests with `-race` flag
   - Create concurrent access tests
   - Verify goroutine lifecycle management
   - Test mutex usage and lock ordering

4. **Test Improvements**:
   - Refactor existing tests for clarity
   - Add test helpers and fixtures
   - Implement proper mocking where needed
   - Ensure tests are deterministic

5. **Integration Tests**:
   - Create end-to-end test scenarios
   - Test component interactions
   - Verify error propagation
   - Test graceful shutdown scenarios

Specific files/packages to analyze: {{PACKAGE_PATH}}

Deliverables:
- Current test coverage report
- List of untested critical functions
- New test implementations
- Recommendations for test improvements
- Race condition test results

Use the following command to invoke the agent:
```
subagent_type: "golang"
description: "Improve test coverage and quality"
```

Remember:
- Tests should be maintainable and clear
- Use testify/require for assertions
- Clean up resources with defer or t.Cleanup()
- Run with race detector before committing