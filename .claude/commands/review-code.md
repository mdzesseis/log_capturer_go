# Code Review Command

Use the Task tool to launch the code-reviewer agent:

```
Task(
    subagent_type="code-reviewer",
    description="Perform code review",
    prompt=[Code or file to review]
)
```

## When to use this command:

- Review code before committing
- Check for security vulnerabilities
- Validate Go best practices
- Assess performance implications
- Review architecture decisions
- Check test coverage

## Example requests:

1. **Full package review**:
   "Review all code in internal/dispatcher package"

2. **Security review**:
   "Security review for authentication implementation"

3. **Performance review**:
   "Review code for performance bottlenecks"

4. **PR review**:
   "Review changes in feature/add-metrics branch"

The reviewer provides severity levels: BLOCKER, CRITICAL, MAJOR, MINOR