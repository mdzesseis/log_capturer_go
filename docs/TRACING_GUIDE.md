# OpenTelemetry Tracing Guide

**Version**: v0.2.0
**Last Updated**: 2025-11-07
**Target Audience**: Developers, DevOps, System Architects

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [The 4 Operational Modes](#2-the-4-operational-modes)
3. [Configuration Reference](#3-configuration-reference)
4. [Mode Selection Guide](#4-mode-selection-guide)
5. [Switching Modes](#5-switching-modes)
6. [Adaptive Sampling](#6-adaptive-sampling)
7. [On-Demand Tracing API](#7-on-demand-tracing-api)
8. [Performance Impact](#8-performance-impact)
9. [Viewing Traces in Jaeger](#9-viewing-traces-in-jaeger)
10. [Troubleshooting](#10-troubleshooting)
11. [Best Practices](#11-best-practices)
12. [Advanced Topics](#12-advanced-topics)

---

## 1. Introduction

SSW Logs Capture includes a comprehensive distributed tracing system built on **OpenTelemetry**, providing deep visibility into log processing pipelines with minimal performance overhead.

### Why Distributed Tracing?

Traditional logging shows **what** happened. Distributed tracing shows:
- **How long** each operation took
- **Where** bottlenecks occur
- **Which path** a log entry followed through the system
- **Why** certain logs failed or were delayed

### Key Features

✅ **4 Operational Modes** - from zero overhead to full audit trails
✅ **Adaptive Sampling** - automatically increases tracing when latency spikes
✅ **On-Demand Control** - enable tracing for specific sources via API
✅ **Hot Reload** - change modes without restarting
✅ **Low Overhead** - <1% CPU in production mode
✅ **Industry Standard** - OpenTelemetry compatible with Jaeger, Tempo, etc.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                  SSW Logs Capture                        │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  Container Monitor  ──▶  Dispatcher  ──▶  Processor    │
│                            │                │           │
│                            ▼                ▼           │
│                         [Trace]         [Trace]         │
│                            │                │           │
│                            └────────┬───────┘           │
│                                     ▼                    │
│                             OpenTelemetry SDK            │
│                                     │                    │
│                                     ▼                    │
│                           OTLP/Jaeger Exporter          │
│                                     │                    │
└─────────────────────────────────────┼───────────────────┘
                                      ▼
                           ┌───────────────────┐
                           │  Jaeger Backend   │
                           │  (or Tempo/OTLP)  │
                           └───────────────────┘
```

**Trace Span Hierarchy**:
```
log.process (individual log entry)
  ├─ dispatcher.process_batch (batch of logs)
  │    ├─ sink.loki.send (send to Loki)
  │    └─ sink.kafka.send (send to Kafka)
  └─ processor.enrich (enrichment pipeline)
```

---

## 2. The 4 Operational Modes

SSW Logs Capture provides **4 distinct tracing modes**, each with different overhead and visibility trade-offs.

### Mode 1: OFF

**Description**: Tracing completely disabled.

**Overhead**: 0%

**Use Case**: When you don't need any tracing at all, or during performance-critical operations.

**Configuration**:
```yaml
tracing:
  enabled: false
```

**What's Traced**: Nothing

**Example Scenarios**:
- Embedded systems with limited resources
- Benchmarking without tracing overhead
- Legacy systems not yet integrated with observability

---

### Mode 2: SYSTEM-ONLY

**Description**: Traces system operations (batches, sink calls, processor runs) but **NOT** individual log entries.

**Overhead**: <1% CPU, minimal memory

**Use Case**: Production monitoring of system performance without log-level visibility.

**Configuration**:
```yaml
tracing:
  enabled: true
  mode: "system-only"
  service_name: "ssw-logs-capture"
  service_version: "v1.0.0"
  environment: "production"
  exporter: "otlp"
  endpoint: "http://jaeger:4318/v1/traces"
```

**What's Traced**:
- ✅ Dispatcher batch processing
- ✅ Sink send operations (Loki, Kafka, etc.)
- ✅ Processor pipeline execution
- ❌ Individual log entries

**Trace Example**:
```
dispatcher.process_batch  [duration: 45ms]
  ├─ sink.loki.send       [duration: 30ms]
  └─ sink.kafka.send      [duration: 12ms]
```

**When to Use**:
- Production systems with >10k logs/sec
- When you need system-level visibility
- When per-log tracing cost is prohibitive

---

### Mode 3: HYBRID (🌟 RECOMMENDED)

**Description**: System operations always traced + individual logs traced based on:
1. **Configurable base rate** (0-100%)
2. **Adaptive sampling** (auto-increases when latency spikes)
3. **On-demand control** (API-driven per-source tracing)

**Overhead**: 1-10% CPU (depends on configuration)

**Use Case**: Production systems needing flexibility - low overhead by default, detailed tracing when needed.

**Configuration**:
```yaml
tracing:
  enabled: true
  mode: "hybrid"
  service_name: "ssw-logs-capture"
  service_version: "v1.0.0"
  environment: "production"
  exporter: "otlp"
  endpoint: "http://jaeger:4318/v1/traces"

  # Base sampling rate (0.0 = 0%, 1.0 = 100%)
  log_tracing_rate: 0.0  # Start at 0% for minimal overhead

  # Adaptive sampling (auto-increases when latency > threshold)
  adaptive_sampling:
    enabled: true
    latency_threshold: 1s      # Trigger when P99 > 1s
    sample_rate: 0.1           # Sample 10% when triggered
    window_size: 5m            # Evaluation window
    check_interval: 30s        # How often to check

  # On-demand tracing control via API
  on_demand:
    enabled: true
    api_endpoint: "/api/tracing"
```

**What's Traced**:
- ✅ All system operations (like SYSTEM-ONLY)
- ✅ Individual logs based on sampling decisions
- ✅ Automatically more logs when system is slow
- ✅ Specific sources when enabled via API

**Trace Example** (with 10% sampling):
```
log.process [log_id=abc123] [duration: 52ms]
  ├─ dispatcher.process_batch [duration: 45ms]
  │    ├─ sink.loki.send      [duration: 30ms]
  │    └─ sink.kafka.send     [duration: 12ms]
  └─ processor.enrich         [duration: 5ms]
```

**When to Use**:
- ✅ **Most production systems** (recommended default)
- ✅ When you need flexibility
- ✅ When debugging specific issues
- ✅ When cost-benefit ratio matters

---

### Mode 4: FULL-E2E

**Description**: Every log entry gets a unique `trace_id` and is traced end-to-end. 100% sampling.

**Overhead**: 5-10% CPU, +100-200 bytes per log entry

**Use Case**: Critical systems with compliance requirements, SLA tracking, or complete audit trails.

**Configuration**:
```yaml
tracing:
  enabled: true
  mode: "full-e2e"
  service_name: "ssw-logs-capture"
  service_version: "v1.0.0"
  environment: "production"
  exporter: "otlp"
  endpoint: "http://jaeger:4318/v1/traces"
  # log_tracing_rate is automatically set to 1.0 in this mode
```

**What's Traced**:
- ✅ Every single log entry end-to-end
- ✅ Complete audit trail
- ✅ Full waterfall diagrams for all logs

**Trace Example**:
```
log.process [log_id=xyz789, trace_id=unique] [duration: 52ms]
  ├─ container_monitor.read [duration: 2ms]
  ├─ dispatcher.handle      [duration: 1ms]
  ├─ dispatcher.process_batch [duration: 45ms]
  │    ├─ sink.loki.send      [duration: 30ms]
  │    └─ sink.kafka.send     [duration: 12ms]
  └─ processor.enrich         [duration: 5ms]
```

**When to Use**:
- Compliance requirements (HIPAA, PCI-DSS, SOC2)
- SLA tracking (prove 99.9% of logs processed in <100ms)
- Critical infrastructure logs
- Forensic analysis requirements
- When overhead is acceptable trade-off

---

## 3. Configuration Reference

### Complete Configuration Example

```yaml
tracing:
  # Enable/disable tracing entirely
  enabled: true

  # Operational mode: "off", "system-only", "hybrid", "full-e2e"
  mode: "hybrid"

  # Service identification (appears in Jaeger UI)
  service_name: "ssw-logs-capture"
  service_version: "v1.0.0"
  environment: "production"

  # Exporter type: "jaeger", "otlp", "console"
  exporter: "otlp"

  # Exporter endpoint
  endpoint: "http://jaeger:4318/v1/traces"

  # Export settings
  batch_timeout: 5s          # Max time to batch spans before export
  max_batch_size: 512        # Max spans per batch

  # Optional: custom headers for authentication
  headers:
    authorization: "Bearer ${JAEGER_TOKEN}"

  # Hybrid mode: base sampling rate (0.0 - 1.0)
  log_tracing_rate: 0.0

  # Adaptive sampling configuration
  adaptive_sampling:
    enabled: true
    latency_threshold: 1s    # Trigger threshold (duration)
    sample_rate: 0.1         # Sample rate when triggered (0.0 - 1.0)
    window_size: 5m          # Evaluation window
    check_interval: 30s      # How often to check latency

  # On-demand tracing configuration
  on_demand:
    enabled: true
    api_endpoint: "/api/tracing"  # API path for on-demand control
```

### Environment-Specific Configurations

#### Development
```yaml
tracing:
  enabled: true
  mode: "hybrid"
  log_tracing_rate: 0.10  # 10% sampling for dev
  environment: "development"
  exporter: "console"     # Print to stdout
```

#### Staging
```yaml
tracing:
  enabled: true
  mode: "hybrid"
  log_tracing_rate: 0.05  # 5% sampling
  environment: "staging"
  exporter: "otlp"
  endpoint: "http://jaeger-staging:4318/v1/traces"
```

#### Production
```yaml
tracing:
  enabled: true
  mode: "hybrid"
  log_tracing_rate: 0.0   # 0% base, use adaptive + on-demand
  environment: "production"
  exporter: "otlp"
  endpoint: "http://jaeger-prod:4318/v1/traces"
  adaptive_sampling:
    enabled: true
  on_demand:
    enabled: true
```

---

## 4. Mode Selection Guide

### Decision Tree

```
┌─────────────────────────────────────┐
│ Do you need tracing at all?        │
└──────────┬──────────────────────────┘
           │
      NO ──┴─▶ MODE: OFF
           │
      YES──┘
           │
┌──────────▼──────────────────────────┐
│ Do you need per-log visibility?    │
└──────────┬──────────────────────────┘
           │
      NO ──┴─▶ MODE: SYSTEM-ONLY
           │      (Best for high-volume systems)
      YES──┘
           │
┌──────────▼──────────────────────────┐
│ Need 100% compliance audit trail?  │
└──────────┬──────────────────────────┘
           │
      YES──┴─▶ MODE: FULL-E2E
           │      (Overhead acceptable)
      NO ──┘
           │
           └─▶ MODE: HYBRID 🌟
               (Flexible, recommended)
```

### Scenario-Based Recommendations

| Scenario | Recommended Mode | Configuration | Reason |
|----------|------------------|---------------|--------|
| **MVP/Development** | SYSTEM-ONLY | Default | Low overhead, good system visibility |
| **Production <10k logs/sec** | HYBRID (0%) | adaptive=true, on-demand=true | Minimal overhead, can enable when needed |
| **Production >10k logs/sec** | HYBRID (0%) or SYSTEM-ONLY | adaptive=true | Minimal cost, auto-scales with issues |
| **Compliance (HIPAA, PCI-DSS)** | FULL-E2E | All features enabled | Complete audit trail required |
| **SLA Tracking** | FULL-E2E | Store traces 30+ days | Prove performance guarantees |
| **Debugging Specific Issue** | HYBRID + On-Demand | Enable for problem source | Targeted tracing |
| **Cost-Conscious** | SYSTEM-ONLY or OFF | - | No additional infrastructure |
| **Microservices Architecture** | HYBRID (1-5%) | Propagate context | Trace across services |

### Log Volume vs Mode Matrix

| Logs/Second | Recommended Mode | CPU Overhead | Storage Cost (Jaeger) |
|-------------|------------------|--------------|----------------------|
| <1k | HYBRID (10%) | <1% | Low |
| 1k-10k | HYBRID (1-5%) | 1-2% | Medium |
| 10k-50k | HYBRID (0%) + adaptive | 1-2% | Medium |
| 50k-100k | SYSTEM-ONLY or HYBRID (0%) | <1% | Low |
| >100k | SYSTEM-ONLY | <1% | Low |

---

## 5. Switching Modes

### Method 1: Configuration File (Static)

Edit `configs/config.yaml`:

```yaml
tracing:
  mode: "full-e2e"  # Change this line
```

Restart the application:
```bash
systemctl restart ssw-logs-capture
# or
docker-compose restart log_capturer
```

### Method 2: Hot Reload (Dynamic, No Downtime)

1. **Edit configuration file**:
```bash
vim configs/config.yaml
# Change mode: "hybrid" to mode: "full-e2e"
```

2. **Trigger hot reload via API**:
```bash
curl -X POST http://localhost:8401/api/config/reload
```

3. **Verify new mode**:
```bash
curl http://localhost:8401/api/tracing/status | jq '.mode'
# Output: "full-e2e"
```

**Response Example**:
```json
{
  "config_reloaded": true,
  "timestamp": "2025-11-07T19:45:00Z",
  "previous_mode": "hybrid",
  "new_mode": "full-e2e",
  "changes": {
    "tracing_mode": "changed",
    "log_tracing_rate": "1.0 (auto-set for full-e2e)"
  }
}
```

### Method 3: Environment Variables (Container)

Set environment variable before starting:

```bash
export TRACING_MODE=full-e2e
docker-compose up -d
```

**docker-compose.yml**:
```yaml
services:
  log_capturer:
    environment:
      - TRACING_MODE=${TRACING_MODE:-hybrid}
```

### Observing Mode Changes

Check Grafana dashboard "OpenTelemetry Tracing Monitoring":
- **Panel**: "Current Tracing Mode"
- **Metric**: `log_capturer_tracing_mode`

Check Prometheus:
```promql
log_capturer_tracing_mode
```

---

## 6. Adaptive Sampling

### How It Works

Adaptive sampling **automatically increases** the tracing rate when the system detects performance degradation.

```
┌──────────────────────────────────────────────────┐
│                                                   │
│  1. Monitor P99 latency every 30s                │
│                                                   │
│  2. If P99 > threshold (default: 1s)             │
│     ├─▶ Adaptive sampling: ACTIVE                │
│     └─▶ Increase sample rate to 10%              │
│                                                   │
│  3. When latency returns to normal               │
│     ├─▶ Adaptive sampling: INACTIVE              │
│     └─▶ Return to base rate (0%)                 │
│                                                   │
└──────────────────────────────────────────────────┘
```

### Configuration

```yaml
tracing:
  mode: "hybrid"
  log_tracing_rate: 0.0  # Base rate: 0%

  adaptive_sampling:
    enabled: true

    # Trigger threshold: activate when P99 latency > 1s
    latency_threshold: 1s

    # Sample rate when active: trace 10% of logs
    sample_rate: 0.1

    # Evaluation window: check last 5 minutes
    window_size: 5m

    # Check interval: evaluate every 30 seconds
    check_interval: 30s
```

### Example Scenario

**Scenario**: Production system normally processes logs in 50ms P99, but suddenly slows to 2s P99.

1. **Before Issue** (T=0):
   - P99 latency: 50ms
   - Base sampling rate: 0%
   - Logs traced: 0%

2. **Issue Begins** (T=10s):
   - P99 latency: 2s (> 1s threshold)
   - Adaptive sampling: INACTIVE (not yet detected)

3. **Adaptive Sampling Activates** (T=30s):
   - System checks latency every 30s
   - P99 latency: 2s (> 1s threshold)
   - ✅ Adaptive sampling: ACTIVE
   - Sample rate increases: 0% → 10%
   - **Now tracing 10% of logs to investigate**

4. **Issue Resolved** (T=5min):
   - P99 latency: 60ms (< 1s threshold)
   - ✅ Adaptive sampling: INACTIVE
   - Sample rate returns: 10% → 0%

### Monitoring Adaptive Sampling

**Grafana Dashboard**: "OpenTelemetry Tracing Monitoring"
- Panel: "Adaptive Sampling Status"
- Metric: `log_capturer_tracing_adaptive_sampling_active`

**Prometheus Query**:
```promql
# Check if adaptive sampling is active
log_capturer_tracing_adaptive_sampling_active == 1
```

**Alert Rule** (Prometheus):
```yaml
groups:
  - name: tracing_alerts
    rules:
      - alert: AdaptiveSamplingTriggered
        expr: log_capturer_tracing_adaptive_sampling_active == 1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Adaptive sampling triggered - system latency high"
          description: "P99 latency exceeded threshold. Automatic trace sampling increased."
```

### Tuning Adaptive Sampling

#### Aggressive (Catch issues early):
```yaml
adaptive_sampling:
  latency_threshold: 500ms  # Lower threshold
  sample_rate: 0.20         # Higher sample rate (20%)
  check_interval: 15s       # Check more frequently
```

#### Conservative (Minimize overhead):
```yaml
adaptive_sampling:
  latency_threshold: 2s     # Higher threshold
  sample_rate: 0.05         # Lower sample rate (5%)
  check_interval: 1m        # Check less frequently
```

---

## 7. On-Demand Tracing API

On-demand tracing allows you to **enable full tracing for specific sources** via API, without changing the global configuration.

### API Endpoints

#### Enable On-Demand Tracing

**Endpoint**: `POST /api/tracing/enable`

**Request Body**:
```json
{
  "source_id": "container-xyz-12345",
  "rate": 1.0,
  "duration": "10m"
}
```

**Parameters**:
- `source_id` (required): Container ID, file path, or source identifier
- `rate` (required): Sampling rate (0.0 - 1.0, where 1.0 = 100%)
- `duration` (required): How long to keep tracing enabled (e.g., "5m", "1h", "30s")

**Response**:
```json
{
  "status": "enabled",
  "source_id": "container-xyz-12345",
  "rate": 1.0,
  "expires_at": "2025-11-07T20:15:00Z"
}
```

**Example**:
```bash
curl -X POST http://localhost:8401/api/tracing/enable \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "container-abc123",
    "rate": 1.0,
    "duration": "10m"
  }'
```

#### Check Status

**Endpoint**: `GET /api/tracing/status`

**Response**:
```json
{
  "mode": "hybrid",
  "base_rate": 0.0,
  "adaptive_sampling": {
    "enabled": true,
    "active": false,
    "threshold": "1s"
  },
  "on_demand_rules": [
    {
      "source_id": "container-abc123",
      "rate": 1.0,
      "expires_at": "2025-11-07T20:15:00Z",
      "remaining": "8m32s"
    }
  ]
}
```

**Example**:
```bash
curl http://localhost:8401/api/tracing/status | jq '.'
```

#### Disable On-Demand Tracing

**Endpoint**: `POST /api/tracing/disable`

**Request Body**:
```json
{
  "source_id": "container-abc123"
}
```

**Response**:
```json
{
  "status": "disabled",
  "source_id": "container-abc123"
}
```

**Example**:
```bash
curl -X POST http://localhost:8401/api/tracing/disable \
  -H "Content-Type: application/json" \
  -d '{"source_id": "container-abc123"}'
```

### Use Case Examples

#### Example 1: Debugging High Error Rate

```bash
# 1. Notice high error rate from specific container in logs
docker ps | grep problematic-service
# Output: container-abc123

# 2. Enable full tracing for that container
curl -X POST http://localhost:8401/api/tracing/enable \
  -d '{"source_id": "container-abc123", "rate": 1.0, "duration": "5m"}'

# 3. View traces in Jaeger to identify root cause
open http://localhost:16686

# 4. After fixing issue, tracing automatically disables after 5 minutes
# Or manually disable:
curl -X POST http://localhost:8401/api/tracing/disable \
  -d '{"source_id": "container-abc123"}'
```

#### Example 2: Investigating Slow Logs

```bash
# Enable tracing for a specific file
curl -X POST http://localhost:8401/api/tracing/enable \
  -d '{
    "source_id": "/var/log/app/slow.log",
    "rate": 1.0,
    "duration": "15m"
  }'

# Check which logs are slow in Jaeger
# Search for: service=log_capturer_go, minDuration=1s
```

#### Example 3: On-Call Incident Response

```bash
# During incident, enable tracing for all suspected containers
for container in $(docker ps -q); do
  curl -X POST http://localhost:8401/api/tracing/enable \
    -d "{\"source_id\": \"$container\", \"rate\": 0.5, \"duration\": \"10m\"}"
done

# Analyze traces to find culprit
# Disable after incident resolved
```

### Best Practices

1. **Short Durations**: Use 5-15 minute durations to avoid forgetting to disable
2. **Specific Sources**: Enable for specific containers, not all sources
3. **Monitor Overhead**: Check CPU/memory after enabling 100% tracing
4. **Auto-Expiration**: Always set a duration; don't leave on indefinitely
5. **Document**: Log when/why you enabled on-demand tracing in incident notes

---

## 8. Performance Impact

### Benchmark Results

From actual benchmarking on Intel Core i5-1345U:

| Mode | ns/op | B/op | allocs/op | CPU Overhead |
|------|-------|------|-----------|--------------|
| OFF | 12.24 ns | 0 | 0 | 0% |
| SYSTEM-ONLY | ~15 ns | 0 | 0 | <1% |
| HYBRID (0%) | ~18 ns | 0 | 0 | 1-2% |
| HYBRID (1%) | ~22 ns | 8 | 1 | 2-3% |
| HYBRID (10%) | ~30 ns | 16 | 1 | 3-4% |
| HYBRID (100%) | ~50 ns | 48 | 2 | 8-10% |
| FULL-E2E | ~50 ns | 48 | 2 | 8-10% |

**Span Creation** (when tracing is enabled):
- ~100-200 ns/op
- 200-400 B/op
- 5-10 allocs/op

### Real-World Impact

#### System Processing 10,000 logs/second:

| Mode | Total CPU Impact |
|------|------------------|
| OFF | 0.01% |
| HYBRID (0%) | 0.02% |
| HYBRID (1%) | 0.02% |
| HYBRID (10%) | 0.05% |
| FULL-E2E | 0.2% |

#### System Processing 100,000 logs/second:

| Mode | Total CPU Impact |
|------|------------------|
| OFF | 0.12% |
| HYBRID (0%) | 0.18% |
| HYBRID (1%) | 0.23% |
| HYBRID (10%) | 0.45% |
| FULL-E2E | 2.0% |

### Storage Impact (Jaeger Backend)

**Trace size per log** (full E2E):
- Metadata: ~200 bytes
- Attributes: ~100-500 bytes (depends on labels)
- Total: ~300-700 bytes per trace

**Storage calculation** (10k logs/sec, FULL-E2E):
- Per day: 10k logs/sec × 86400s × 500 bytes = ~432 GB/day
- Per month: ~13 TB/month

**Storage calculation** (10k logs/sec, HYBRID 1%):
- Per day: 100 logs/sec × 86400s × 500 bytes = ~4.3 GB/day
- Per month: ~130 GB/month

**Recommendation**: Use sampling (HYBRID mode) to control storage costs.

### Network Impact

**OTLP export bandwidth** (batch size=512, batch_timeout=5s):
- Batch frequency: Every 5 seconds (or when 512 spans accumulated)
- Bandwidth per batch: ~256-512 KB
- Sustained: ~50-100 KB/sec

**Jaeger export bandwidth** (similar):
- ~50-100 KB/sec sustained

**Impact**: Negligible on modern networks (1 Gbps = 125 MB/sec).

---

## 9. Viewing Traces in Jaeger

### Accessing Jaeger UI

**Default URL**: http://localhost:16686

**Docker Compose**:
```bash
docker-compose up -d jaeger
open http://localhost:16686
```

### Searching for Traces

1. **Select Service**: `log_capturer_go` (or your `service_name`)

2. **Select Operation**:
   - `log.process` - Individual log processing
   - `dispatcher.process_batch` - Batch processing
   - `sink.loki.send` - Loki sink operations
   - `sink.kafka.send` - Kafka sink operations

3. **Apply Filters**:
   - **Tags**: `source_id=container-abc123`
   - **Min Duration**: `1s` (find slow traces)
   - **Max Duration**: `10s`
   - **Limit Results**: `20`

4. **Click "Find Traces"**

### Understanding Trace Waterfall

**Example Trace**:
```
log.process [52ms total]
├─ [0-2ms]   container_monitor.read
├─ [2-3ms]   dispatcher.handle
├─ [3-48ms]  dispatcher.process_batch
│   ├─ [3-33ms]  sink.loki.send        ◀── Bottleneck!
│   └─ [33-45ms] sink.kafka.send
└─ [48-53ms] processor.enrich
```

**Analysis**:
- Total duration: 52ms
- Bottleneck: Loki sink (30ms, 58% of total)
- Action: Investigate Loki connectivity or increase batch size

### Common Searches

#### Find Slow Logs (>1s)
```
Service: log_capturer_go
Operation: log.process
Min Duration: 1s
```

#### Find Errors
```
Service: log_capturer_go
Tags: error=true
```

#### Find Specific Container
```
Service: log_capturer_go
Tags: source_id=container-abc123
```

#### Find Recently Processed
```
Service: log_capturer_go
Lookback: Last 1 hour
Limit: 100
```

### Trace Attributes Reference

| Attribute | Description | Example |
|-----------|-------------|---------|
| `log.message` | Log message | `"ERROR: connection failed"` |
| `log.source_id` | Source identifier | `"container-abc123"` |
| `log.timestamp` | Log timestamp | `"2025-11-07T19:00:00Z"` |
| `log.level` | Log level | `"ERROR"` |
| `batch.size` | Number of logs in batch | `100` |
| `sink.type` | Sink type | `"loki"` |
| `sink.endpoint` | Sink endpoint | `"http://loki:3100"` |

### Correlating Logs with Traces

Logs include `trace_id` when traced:

**Log Entry** (in Loki):
```json
{
  "message": "Processing log entry",
  "trace_id": "abc123def456",
  "source_id": "container-xyz"
}
```

**Find Trace in Jaeger**:
1. Copy `trace_id` from log
2. Go to Jaeger UI
3. Click "Trace" tab
4. Paste trace ID: `abc123def456`
5. View complete waterfall diagram

---

## 10. Troubleshooting

### Issue 1: No Traces Appearing in Jaeger

**Check 1**: Is tracing enabled?
```bash
curl http://localhost:8401/api/tracing/status | jq '.mode'
# Should NOT be "off"
```

**Check 2**: Is Jaeger running?
```bash
curl http://localhost:14268/api/traces
# Should return 200 OK
```

**Check 3**: Check metrics (spans created)
```promql
rate(log_capturer_tracing_spans_created_total[5m])
# Should be > 0
```

**Check 4**: Check metrics (spans dropped)
```promql
rate(log_capturer_tracing_spans_dropped_total[5m])
# Should be 0 or low
```

**Check 5**: Check logs for errors
```bash
docker logs log_capturer | grep -i "trace\|span\|export"
```

**Fix**: Verify endpoint configuration
```yaml
tracing:
  endpoint: "http://jaeger:4318/v1/traces"  # Correct OTLP endpoint
  # NOT: http://jaeger:14268  (old Jaeger protocol)
```

---

### Issue 2: High Span Drop Rate

**Symptom**:
```promql
rate(log_capturer_tracing_spans_dropped_total[5m]) > 100
```

**Causes**:
1. Export queue full
2. Jaeger endpoint unreachable
3. Network issues
4. Backpressure

**Solutions**:

**1. Increase Queue Size**:
```yaml
tracing:
  exporter:
    max_queue_size: 4096   # Increase from 2048
```

**2. Increase Batch Timeout**:
```yaml
tracing:
  batch_timeout: 10s       # Increase from 5s
```

**3. Reduce Sampling Rate** (if in HYBRID/FULL-E2E):
```yaml
tracing:
  mode: "hybrid"
  log_tracing_rate: 0.01   # Reduce from 0.1
```

**4. Check Jaeger Health**:
```bash
curl http://jaeger:14269/health
```

---

### Issue 3: Mode Not Switching After Reload

**Check 1**: Was reload successful?
```bash
curl -X POST http://localhost:8401/api/config/reload
# Check response
```

**Check 2**: Verify config file was saved
```bash
cat configs/config.yaml | grep "mode:"
```

**Check 3**: Check Prometheus metric
```promql
log_capturer_tracing_mode
```

**Fix**: Restart application
```bash
docker-compose restart log_capturer
```

---

### Issue 4: High CPU Usage After Enabling Tracing

**Check 1**: Which mode is active?
```bash
curl http://localhost:8401/api/tracing/status | jq '.mode'
```

**Check 2**: What's the sampling rate?
```bash
curl http://localhost:8401/api/tracing/status | jq '.base_rate'
```

**Check 3**: How many on-demand rules?
```bash
curl http://localhost:8401/api/tracing/status | jq '.on_demand_rules | length'
```

**Solutions**:

**1. Reduce Sampling Rate**:
```bash
# Edit config
vim configs/config.yaml
# Set log_tracing_rate: 0.0

# Reload
curl -X POST http://localhost:8401/api/config/reload
```

**2. Disable On-Demand Rules**:
```bash
# Get source IDs
curl http://localhost:8401/api/tracing/status | jq '.on_demand_rules[].source_id'

# Disable each
curl -X POST http://localhost:8401/api/tracing/disable \
  -d '{"source_id": "container-abc123"}'
```

**3. Switch to Lower-Overhead Mode**:
```bash
# Edit config
vim configs/config.yaml
# Set mode: "system-only"

# Reload
curl -X POST http://localhost:8401/api/config/reload
```

---

### Issue 5: Traces Missing Attributes

**Symptom**: Traces appear but don't include `log.message`, `source_id`, etc.

**Cause**: Attributes not set during span creation

**Check Code**:
```go
// Correct: Set attributes
span.SetAttributes(
    attribute.String("log.message", entry.Message),
    attribute.String("log.source_id", entry.SourceID),
)
```

**Fix**: Upgrade to latest version with complete attribute support.

---

## 11. Best Practices

### Production Recommendations

#### 1. Start Conservative, Scale Up as Needed

```yaml
# Day 1: Minimal overhead
tracing:
  mode: "hybrid"
  log_tracing_rate: 0.0
  adaptive_sampling:
    enabled: true

# Day 30: After validating, increase
tracing:
  log_tracing_rate: 0.01  # 1% sampling
```

#### 2. Use Adaptive Sampling

Always enable adaptive sampling in production:
```yaml
adaptive_sampling:
  enabled: true
  latency_threshold: 1s
  sample_rate: 0.1
```

Benefits:
- ✅ Auto-detects performance issues
- ✅ Increases tracing when needed
- ✅ Minimal cost when system is healthy

#### 3. Leverage On-Demand Tracing

Keep base rate at 0%, use on-demand for debugging:
```bash
# When issue occurs
curl -X POST http://localhost:8401/api/tracing/enable \
  -d '{"source_id": "problem-container", "rate": 1.0, "duration": "10m"}'
```

Benefits:
- ✅ Zero baseline overhead
- ✅ Full visibility when needed
- ✅ Auto-expires

#### 4. Monitor Tracing Overhead

Set alerts:
```yaml
# Prometheus alert
- alert: HighTracingOverhead
  expr: rate(log_capturer_tracing_spans_created_total[5m]) > 1000
  for: 10m
  annotations:
    summary: "High tracing overhead detected"
```

#### 5. Tune Sampling Based on Volume

| Volume | Recommended Rate |
|--------|------------------|
| <1k logs/sec | 10% |
| 1k-10k logs/sec | 1-5% |
| 10k-50k logs/sec | 0.1-1% |
| >50k logs/sec | 0% (use adaptive + on-demand) |

#### 6. Set Appropriate Jaeger Retention

```yaml
# Jaeger config
span-storage:
  type: elasticsearch
  retention: 7d    # Keep traces for 7 days
```

Balance:
- Longer retention = higher storage cost
- Shorter retention = less historical analysis

#### 7. Use Tags for Fast Searching

Always set meaningful tags:
```go
span.SetAttributes(
    attribute.String("environment", "production"),
    attribute.String("version", "v1.2.3"),
    attribute.String("customer_id", customerID),
)
```

#### 8. Document Mode Changes

Keep a changelog:
```markdown
# Tracing Configuration Changes

## 2025-11-07
- Changed mode: hybrid → full-e2e
- Reason: Compliance audit required
- Duration: 48 hours
- Owner: @ops-team

## 2025-11-05
- Enabled adaptive sampling
- Threshold: 1s
- Rate: 10%
```

---

### Development Recommendations

#### 1. Use Higher Sampling Rates

```yaml
# Development environment
tracing:
  mode: "hybrid"
  log_tracing_rate: 0.10  # 10% sampling
```

#### 2. Enable Console Exporter

```yaml
tracing:
  exporter: "console"  # Print traces to stdout
```

#### 3. Lower Adaptive Sampling Threshold

```yaml
adaptive_sampling:
  latency_threshold: 500ms  # More sensitive
```

#### 4. Test All Modes

```bash
# Test OFF mode
vim configs/config.yaml  # mode: "off"
./run_tests.sh

# Test SYSTEM-ONLY
vim configs/config.yaml  # mode: "system-only"
./run_tests.sh

# Test HYBRID
vim configs/config.yaml  # mode: "hybrid"
./run_tests.sh

# Test FULL-E2E
vim configs/config.yaml  # mode: "full-e2e"
./run_tests.sh
```

---

### Compliance Recommendations

#### 1. Use FULL-E2E Mode

```yaml
tracing:
  mode: "full-e2e"  # 100% sampling
```

#### 2. Set Long Retention

```yaml
# Jaeger/Tempo
span-storage:
  retention: 90d    # Or per compliance requirement
```

#### 3. Add Compliance Tags

```go
span.SetAttributes(
    attribute.String("compliance.regulation", "HIPAA"),
    attribute.String("compliance.customer_id", customerID),
    attribute.String("compliance.data_classification", "PHI"),
)
```

#### 4. Export Traces for Audit

```bash
# Periodic export from Jaeger
curl "http://jaeger:16686/api/traces?service=log_capturer_go&start=2025-11-01&end=2025-11-30" \
  > traces_november_2025.json
```

---

## 12. Advanced Topics

### Context Propagation

When integrating with microservices, propagate trace context:

**Incoming HTTP Request**:
```go
import "go.opentelemetry.io/otel/propagation"

// Extract context from incoming request
ctx := otel.GetTextMapPropagator().Extract(
    r.Context(),
    propagation.HeaderCarrier(r.Header),
)
```

**Outgoing HTTP Request**:
```go
// Inject context into outgoing request
otel.GetTextMapPropagator().Inject(
    ctx,
    propagation.HeaderCarrier(req.Header),
)
```

### Custom Span Attributes

Add domain-specific attributes:

```go
span.SetAttributes(
    attribute.String("customer.id", customerID),
    attribute.String("customer.tier", "enterprise"),
    attribute.Int("log.batch_size", len(batch)),
    attribute.Bool("log.filtered", wasFiltered),
)
```

### Baggage (Cross-Service Data)

Share data across service boundaries:

```go
import "go.opentelemetry.io/otel/baggage"

// Set baggage
member, _ := baggage.NewMember("user_id", "12345")
bag, _ := baggage.New(member)
ctx = baggage.ContextWithBaggage(ctx, bag)

// Get baggage (in another service)
bag = baggage.FromContext(ctx)
userID := bag.Member("user_id").Value()
```

### Sampling Strategies

#### Head Sampling (Current Implementation)
Decision made at trace creation time.

**Pros**:
- Low overhead
- Predictable cost

**Cons**:
- May miss interesting traces

#### Tail Sampling (Future)
Decision made after trace completion.

**Pros**:
- Keep all error traces
- Keep all slow traces
- Discard normal traces

**Cons**:
- Higher overhead
- More complex

### Multi-Exporter Setup

Send traces to multiple backends:

```yaml
# Future enhancement
tracing:
  exporters:
    - type: "otlp"
      endpoint: "http://jaeger:4318/v1/traces"
    - type: "otlp"
      endpoint: "http://tempo:4318/v1/traces"
```

### Trace Sampling Rules (Future)

Per-source sampling:

```yaml
tracing:
  mode: "hybrid"
  sampling_rules:
    - source_pattern: "critical-*"
      rate: 1.0    # 100% for critical services
    - source_pattern: "debug-*"
      rate: 0.0    # 0% for debug services
    - source_pattern: "*"
      rate: 0.01   # 1% for everything else
```

---

## Appendix A: Metrics Reference

### Tracing Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `log_capturer_tracing_mode` | Gauge | Current mode (0=off, 1=system-only, 2=hybrid, 3=full-e2e) |
| `log_capturer_tracing_logs_traced_total` | Counter | Total logs traced |
| `log_capturer_tracing_spans_created_total` | Counter | Total spans created by type |
| `log_capturer_tracing_sampling_rate` | Gauge | Current sampling rate (0.0-1.0) |
| `log_capturer_tracing_on_demand_rules_active` | Gauge | Number of active on-demand rules |
| `log_capturer_tracing_adaptive_sampling_active` | Gauge | Adaptive sampling status (0=inactive, 1=active) |
| `log_capturer_tracing_spans_exported_total` | Counter | Successfully exported spans |
| `log_capturer_tracing_spans_dropped_total` | Counter | Dropped spans by reason |

### Useful PromQL Queries

```promql
# Trace rate per second
rate(log_capturer_tracing_logs_traced_total[5m])

# Span export success rate
rate(log_capturer_tracing_spans_exported_total[5m])
/ rate(log_capturer_tracing_spans_created_total[5m])

# Current tracing overhead estimate (%)
(
  (log_capturer_tracing_mode{mode="off"} * 0) +
  (log_capturer_tracing_mode{mode="system-only"} * 1) +
  (log_capturer_tracing_mode{mode="hybrid"} * (1 + log_capturer_tracing_sampling_rate * 9)) +
  (log_capturer_tracing_mode{mode="full-e2e"} * 10)
)
```

---

## Appendix B: Glossary

**Term** | **Definition**
---------|---------------
**Span** | A single unit of work in a trace (e.g., "send to Loki")
**Trace** | A collection of spans representing end-to-end request flow
**Trace ID** | Unique identifier for a trace
**Span ID** | Unique identifier for a span within a trace
**Sampling** | Decision to trace or not trace a request
**Exporter** | Component that sends spans to backend (Jaeger, Tempo, etc.)
**OTLP** | OpenTelemetry Protocol (standard wire format)
**Baggage** | Key-value data propagated across service boundaries
**Context Propagation** | Passing trace context between services
**Waterfall Diagram** | Visual representation of span timeline
**P99 Latency** | 99th percentile latency (1% of requests are slower)

---

## Appendix C: Quick Reference

### Configuration Snippets

```yaml
# Minimal (OFF)
tracing:
  enabled: false

# Production (HYBRID 0%)
tracing:
  enabled: true
  mode: "hybrid"
  log_tracing_rate: 0.0
  endpoint: "http://jaeger:4318/v1/traces"
  adaptive_sampling:
    enabled: true
  on_demand:
    enabled: true

# Compliance (FULL-E2E)
tracing:
  enabled: true
  mode: "full-e2e"
  endpoint: "http://jaeger:4318/v1/traces"
```

### API Quick Reference

```bash
# Enable on-demand
curl -X POST http://localhost:8401/api/tracing/enable \
  -d '{"source_id": "X", "rate": 1.0, "duration": "10m"}'

# Check status
curl http://localhost:8401/api/tracing/status

# Disable on-demand
curl -X POST http://localhost:8401/api/tracing/disable \
  -d '{"source_id": "X"}'

# Hot reload config
curl -X POST http://localhost:8401/api/config/reload
```

---

**End of Guide**

For questions, issues, or contributions, see:
- [GitHub Issues](https://github.com/your-org/log-capturer/issues)
- [Internal Wiki](https://wiki.example.com/log-capturer)
- Slack: #log-capturer-support
