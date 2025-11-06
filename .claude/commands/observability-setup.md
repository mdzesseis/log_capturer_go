# Observability Setup Command

Use the Task tool to launch the observability agent:

```
Task(
    subagent_type="observability",
    description="Set up observability and monitoring",
    prompt=[Your observability request]
)
```

## When to use this command:

- Implement structured logging
- Set up metrics collection
- Configure distributed tracing
- Define SLIs and SLOs
- Set up health checks
- Implement anomaly detection

## Example requests:

1. **Metrics implementation**:
   "Add Prometheus metrics for all critical paths"

2. **Tracing setup**:
   "Implement OpenTelemetry tracing across services"

3. **SLO definition**:
   "Define SLOs for 99.9% availability and <100ms p99 latency"

4. **Health monitoring**:
   "Create comprehensive health check endpoints"

Works with grafana-specialist for visualization!