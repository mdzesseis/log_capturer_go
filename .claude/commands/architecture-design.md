# Architecture Design Command

Use the Task tool to launch the architecture agent:

```
Task(
    subagent_type="architecture",
    description="Design system architecture",
    prompt=[Your architecture request]
)
```

## When to use this command:

- Design system architecture
- Choose design patterns
- Plan scalability solutions
- Make architectural decisions
- Design APIs and interfaces
- Plan microservices decomposition
- Implement resilience patterns

## Example requests:

1. **System design**:
   "Design architecture for handling 1M logs/second"

2. **Pattern selection**:
   "Implement circuit breaker pattern for external services"

3. **API design**:
   "Design RESTful API for log management"

4. **Scalability**:
   "Plan horizontal scaling strategy for the dispatcher"

Provides architectural decision records (ADRs) and implementation guidance!