# Workflow Coordinator Command

Use the Task tool to launch the workflow-coordinator agent:

```
Task(
    subagent_type="workflow-coordinator",
    description="Coordinate project workflow",
    prompt=[Your workflow coordination request]
)
```

## When to use this command:

- Starting a new feature or epic
- Managing multiple related tasks
- Coordinating between different agents
- Creating and tracking issues
- Planning sprints or releases
- Orchestrating complex workflows

## Example usage scenarios:

1. **New Feature Implementation**:
   "I need to implement a new authentication system with JWT tokens"

2. **Bug Fix Workflow**:
   "Critical bug reported: memory leak in dispatcher under high load"

3. **Performance Optimization**:
   "Optimize the entire log processing pipeline for 2x throughput"

4. **Release Planning**:
   "Prepare version 1.2.0 release with all pending features"

## The coordinator will:
- Break down complex tasks into issues
- Assign tasks to appropriate specialist agents
- Track progress across all work items
- Ensure proper sequencing of dependencies
- Monitor quality gates
- Generate status reports

This is your main entry point for complex project management tasks!