# Git Operations Command

Use the Task tool to launch the git-specialist agent:

```
Task(
    subagent_type="git-specialist",
    description="Manage Git operations",
    prompt=[Your Git operation request]
)
```

## When to use this command:

- Set up Git workflows
- Manage branches and releases
- Configure Git hooks
- Resolve merge conflicts
- Create release tags
- Set up CI/CD integration

## Example requests:

1. **Release management**:
   "Create release v1.2.0 from develop branch"

2. **Branch strategy**:
   "Set up GitFlow branching strategy"

3. **Conflict resolution**:
   "Help resolve merge conflicts in feature branch"

4. **Hook setup**:
   "Create pre-commit hooks for code quality"

Handles all version control operations!