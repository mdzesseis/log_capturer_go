# Grafana & Loki Setup Command

Use the Task tool to launch the grafana-specialist agent:

```
Task(
    subagent_type="grafana-specialist",
    description="Configure Grafana and Loki",
    prompt=[Your Grafana/Loki request]
)
```

## When to use this command:

- Create Grafana dashboards
- Configure Loki for log aggregation
- Set up alerts and notifications
- Write LogQL queries
- Optimize log ingestion
- Design visualization panels

## Example requests:

1. **Dashboard creation**:
   "Create a comprehensive monitoring dashboard for log-capturer"

2. **Alert configuration**:
   "Set up alerts for high error rates and memory leaks"

3. **Query optimization**:
   "Write LogQL queries to find performance bottlenecks"

4. **Loki tuning**:
   "Optimize Loki configuration for high-volume log ingestion"

The specialist handles all Grafana/Loki stack components!