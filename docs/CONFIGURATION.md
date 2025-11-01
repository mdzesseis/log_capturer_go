# Configuration Guide

**SSW Logs Capture** - Complete Configuration Reference

---

## Table of Contents

1. [Overview](#overview)
2. [Configuration File Structure](#configuration-file-structure)
3. [Core Application Settings](#core-application-settings)
4. [Server Configuration](#server-configuration)
5. [Metrics Configuration](#metrics-configuration)
6. [Input Sources](#input-sources)
   - [File Monitoring](#file-monitoring)
   - [Container Monitoring](#container-monitoring)
7. [Output Sinks](#output-sinks)
   - [Loki Sink](#loki-sink)
   - [Local File Sink](#local-file-sink)
   - [Elasticsearch Sink](#elasticsearch-sink)
   - [Splunk Sink](#splunk-sink)
8. [Dispatcher & Processing](#dispatcher--processing)
9. [Storage & Persistence](#storage--persistence)
10. [Dead Letter Queue (DLQ)](#dead-letter-queue-dlq)
11. [Enterprise Features](#enterprise-features)
12. [Common Scenarios](#common-scenarios)
13. [Environment Variables](#environment-variables)
14. [Validation Rules](#validation-rules)
15. [Best Practices](#best-practices)

---

## Overview

SSW Logs Capture uses YAML configuration files for all settings. The main configuration file (`config.yaml`) controls all aspects of log collection, processing, and delivery.

### Configuration File Locations

```bash
# Primary configuration
/app/configs/config.yaml           # Main application configuration

# Pipeline configurations
/app/configs/file_pipeline.yml     # File monitoring pipelines
/app/configs/pipelines.yaml        # Log processing pipelines
```

### Configuration Hierarchy

The application follows this configuration precedence (highest to lowest):

1. **Environment Variables** (highest priority)
2. **Configuration File** (config.yaml)
3. **Default Values** (when `default_configs: true`)

---

## Configuration File Structure

```yaml
app:                      # Core application settings
server:                   # HTTP server configuration
metrics:                  # Prometheus metrics
file_monitor_service:     # File monitoring service
container_monitor:        # Docker container monitoring
dispatcher:               # Core log dispatcher
sinks:                    # Output destinations
  loki:                   # Grafana Loki sink
  local_file:             # Local file sink
  elasticsearch:          # Elasticsearch sink
  splunk:                 # Splunk sink
processing:               # Log processing pipelines
positions:                # Position tracking
disk_buffer:              # Disk buffering
cleanup:                  # Disk cleanup
resource_monitoring:      # Resource leak detection
hot_reload:               # Configuration hot reload
timestamp_validation:     # Timestamp validation
multi_tenant:             # Multi-tenancy (Enterprise)
security:                 # Security settings (Enterprise)
tracing:                  # Distributed tracing (Enterprise)
slo:                      # SLO monitoring (Enterprise)
```

---

## Core Application Settings

### `app` Section

Controls core application behavior, logging, and environment.

```yaml
app:
  name: "ssw-logs-capture"          # Application identifier
  version: "v0.0.2"                 # Application version
  environment: "production"         # Environment: "development", "staging", "production"
  log_level: "info"                 # Logging level: "trace", "debug", "info", "warn", "error"
  log_format: "json"                # Log format: "json" or "text"
  log_file: ""                      # Log file path (empty = stdout)
  operation_timeout: "1h"           # Global operation timeout
  default_configs: true             # Apply default values for unspecified configs
```

#### Key Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `name` | string | "ssw-logs-capture" | Application name for identification |
| `version` | string | - | Application version |
| `environment` | string | "production" | Deployment environment |
| `log_level` | string | "info" | Application logging level |
| `log_format` | string | "json" | Log output format |
| `operation_timeout` | duration | "1h" | Global operation timeout |
| `default_configs` | bool | true | Enable automatic default values |

#### Log Levels

- **trace**: Most verbose, includes all debug information
- **debug**: Detailed debugging information
- **info**: General informational messages (recommended for production)
- **warn**: Warning messages for potential issues
- **error**: Error messages for failures
- **fatal**: Fatal errors that cause application termination

#### Environment

Environment setting affects:
- Default log levels
- Profiling defaults
- Sampling rates
- Resource limits

Recommended values:
- **development**: Local development with verbose logging
- **staging**: Pre-production testing with moderate logging
- **production**: Production deployment with optimized settings

#### Default Configs Behavior

When `default_configs: true`:
- Unspecified configuration fields receive automatic defaults
- Empty or commented fields are **NOT** automatically filled (respects your intent)
- Only completely absent fields receive defaults

When `default_configs: false`:
- Only explicitly configured values are used
- No automatic defaults applied
- Requires complete configuration

**Override via environment**: `SSW_DEFAULT_CONFIGS=true/false`

---

## Server Configuration

### `server` Section

HTTP server for health checks, stats, and management APIs.

```yaml
server:
  enabled: true                     # Enable HTTP server
  port: 8401                        # Server port
  host: "0.0.0.0"                   # Bind address (0.0.0.0 = all interfaces)
  read_timeout: "30s"               # HTTP read timeout
  write_timeout: "30s"              # HTTP write timeout
  idle_timeout: "60s"               # Keep-alive timeout
  max_header_bytes: 1048576         # Maximum header size (1MB)
```

#### Available Endpoints

When server is enabled, the following endpoints are available:

- `GET /health` - Basic health check
- `GET /health/detailed` - Detailed component health
- `GET /stats` - Operational statistics
- `GET /config` - Current configuration (sanitized)
- `POST /config/reload` - Reload configuration
- `GET /positions` - File position tracking status
- `GET /dlq/stats` - Dead Letter Queue statistics
- `POST /dlq/reprocess` - Reprocess DLQ entries
- `GET /metrics` - Prometheus metrics (proxied from metrics server)

See [API Documentation](API.md) for detailed endpoint information.

#### Security Considerations

```yaml
server:
  enabled: true
  host: "127.0.0.1"                 # Listen only on localhost (more secure)
  port: 8401

security:
  enabled: true                     # Enable authentication
  authentication:
    enabled: true
    method: "bearer"                # Require Bearer tokens
    # ... (see Security section)
```

---

## Metrics Configuration

### `metrics` Section

Prometheus metrics export for monitoring and alerting.

```yaml
metrics:
  enabled: true                     # Enable metrics collection
  port: 8001                        # Metrics server port
  path: "/metrics"                  # Metrics endpoint path
```

#### Exposed Metrics

**Log Processing Metrics**:
```
logs_received_total                 # Total logs received
logs_processed_total                # Total logs successfully processed
logs_failed_total                   # Total failed logs
logs_dropped_total                  # Total dropped logs
log_processing_duration_seconds     # Processing latency histogram
```

**Dispatcher Metrics**:
```
dispatcher_queue_size               # Current queue size
dispatcher_queue_utilization        # Queue utilization percentage
dispatcher_workers_active           # Active worker count
dispatcher_batch_size               # Current batch size
```

**Sink Metrics** (per sink):
```
sink_logs_sent_total{sink="loki"}  # Total logs sent to sink
sink_errors_total{sink="loki"}     # Total sink errors
sink_latency_seconds{sink="loki"}  # Sink delivery latency
```

**Resource Metrics**:
```
goroutines_count                    # Current goroutine count
memory_alloc_bytes                  # Allocated memory
memory_sys_bytes                    # System memory
file_descriptors_open               # Open file descriptors
```

#### Grafana Integration

The application includes a pre-built Grafana dashboard:

```bash
# Import dashboard
provisioning/dashboards/log_capturer_processing_dashboard.json
```

See [Monitoring Documentation](../provisioning/README.md) for Grafana setup.

---

## Input Sources

### File Monitoring

Monitor log files and directories for changes.

#### `file_monitor_service` Section

```yaml
file_monitor_service:
  enabled: true                                 # Enable file monitoring
  pipeline_file: "/app/configs/file_pipeline.yml"  # Pipeline configuration
  poll_interval: "30s"                          # Filesystem polling interval
  read_buffer_size: 65536                       # Read buffer size (64KB)
  read_interval: "100ms"                        # File reading interval
  recursive: true                               # Monitor subdirectories
  follow_symlinks: false                        # Follow symbolic links (security risk!)
```

#### File Pipeline Configuration

The `file_pipeline.yml` defines which files to monitor:

```yaml
# file_pipeline.yml
enabled: true
version: "1.0"

# Monitor specific files
files:
  - path: "/var/log/app.log"
    labels:
      service: "myapp"
      environment: "production"
    enabled: true

  - path: "/var/log/auth.log"
    labels:
      service: "system"
      type: "security"
    enabled: true

# Monitor directories
directories:
  - path: "/var/log/apps"
    patterns:
      - "*.log"
      - "*.txt"
    exclude_patterns:
      - "*.gz"
      - "*.bak"
    recursive: true
    default_labels:
      source: "app_logs"
    enabled: true

# Monitoring settings
monitoring:
  directory_scan_interval: 300      # Scan directories every 5 minutes
  max_files: 1000                   # Maximum files to monitor
  read_buffer_size: 65536           # 64KB buffer
  poll_interval: 100                # Poll files every 100ms
  follow_symlinks: false
  include_hidden: false
  max_file_size: 1073741824         # 1GB maximum file size
  rotation_action: "reopen"         # Action on file rotation
```

#### File Patterns

Supported glob patterns:
- `*.log` - All files ending in .log
- `app-*.log` - Files starting with app-
- `**/*.log` - All .log files in subdirectories (with `recursive: true`)
- `[abc].log` - Matches a.log, b.log, c.log
- `app.log.{1,2,3}` - Matches rotated files

#### Default Files Configuration

The `files_config` section provides defaults when no file_pipeline.yml exists:

```yaml
files_config:
  watch_directories:
    - "/var/log"
  include_patterns:
    - "*.log"
    - "syslog"
  exclude_patterns:
    - "*.gz"
    - "*.zip"
    - "*.old"
  exclude_directories:
    - "/var/log/monitoring_data_suite"
```

---

### Container Monitoring

Monitor Docker container logs in real-time.

#### `container_monitor` Section

```yaml
container_monitor:
  enabled: true                                 # Enable container monitoring
  socket_path: "unix:///var/run/docker.sock"   # Docker socket path
  health_check_delay: "30s"                     # Initial health check delay
  reconnect_interval: "30s"                     # Reconnection interval
  max_concurrent: 25                            # Max concurrent container connections

  # Container filtering
  include_labels: {}                            # Include containers with labels
    # logs.enabled: "true"                      # Example: only containers with this label
  exclude_labels: {}                            # Exclude containers with labels
    # logs.disabled: "true"                     # Example: exclude containers with this label
  include_names: []                             # Include specific container names
  exclude_names:                                # Exclude specific container names
    - "log_capturer_go"                         # Don't capture own logs

  # Stream settings
  include_stdout: true                          # Capture stdout logs
  include_stderr: true                          # Capture stderr logs
  tail_lines: 50                                # Initial lines to read per container
  follow: true                                  # Follow log stream
```

#### Container Label Filtering

Use Docker labels to control log capture:

```yaml
# Include only containers with specific labels
include_labels:
  environment: "production"
  logs.capture: "true"

# Exclude containers with specific labels
exclude_labels:
  logs.exclude: "true"
  maintenance: "true"
```

Example Docker container with labels:

```bash
docker run -d \
  --label environment=production \
  --label logs.capture=true \
  --label service=myapp \
  myapp:latest
```

Labels are automatically added to log entries:

```json
{
  "message": "Application started",
  "labels": {
    "container_name": "myapp-prod",
    "environment": "production",
    "service": "myapp"
  }
}
```

#### Event-Driven Discovery

The container monitor uses Docker events for real-time container discovery:

- ✅ **Instant detection**: New containers detected immediately (no polling)
- ✅ **Automatic reconnection**: Handles Docker daemon restarts
- ✅ **Low overhead**: Event-driven vs polling reduces CPU usage
- ✅ **Graceful handling**: Stops monitoring when containers are removed

---

## Output Sinks

### Loki Sink

Send logs to Grafana Loki for storage and querying.

#### `sinks.loki` Section

```yaml
sinks:
  loki:
    enabled: true                               # Enable Loki sink
    url: "http://loki:3100"                     # Loki server URL
    push_endpoint: "/loki/api/v1/push"          # Push API endpoint
    tenant_id: ""                               # Multi-tenant ID (empty for single-tenant)
    timeout: "120s"                             # Request timeout

    # Batching configuration
    batch_size: 500                             # Logs per batch
    batch_timeout: "15s"                        # Maximum wait time for batch
    max_request_size: 2097152                   # 2MB max request size
    queue_size: 25000                           # Internal queue size

    # Labels
    default_labels:
      service: "ssw-log-capturer"
      environment: "production"

    # Authentication
    auth:
      type: "none"                              # "none", "basic", "bearer"
      username: ""
      password: ""
      token: ""

    # TLS configuration
    tls:
      enabled: false                            # Enable TLS/HTTPS
      verify_certificate: true                  # Verify server certificate
      ca_file: ""                               # CA certificate path
      cert_file: ""                             # Client certificate (for mTLS)
      key_file: ""                              # Client private key (for mTLS)

    # Backpressure management
    backpressure_config:
      enabled: true
      queue_warning_threshold: 0.75             # Warn at 75% capacity
      queue_critical_threshold: 0.90            # Critical at 90%
      queue_emergency_threshold: 0.95           # Emergency at 95% (send to DLQ)
      timeout_escalation: true                  # Increase timeout under load

    # DLQ integration
    dlq_config:
      enabled: true
      send_on_queue_full: false                 # Don't send to DLQ on queue full
      send_on_timeout: true                     # Send to DLQ on timeout
      send_on_error: true                       # Send to DLQ on error

    # Adaptive batching (dynamic optimization)
    adaptive_batching:
      enabled: true                             # Enable adaptive batching
      min_batch_size: 10
      max_batch_size: 1000
      initial_batch_size: 100
      min_flush_delay: "50ms"
      max_flush_delay: "10s"
      initial_flush_delay: "1s"
      adaptation_interval: "30s"                # Adjust every 30 seconds
      latency_threshold: "500ms"                # Target latency
      throughput_target: 1000                   # Target logs/sec
```

#### Loki Authentication

**No Authentication**:
```yaml
auth:
  type: "none"
```

**Basic Authentication**:
```yaml
auth:
  type: "basic"
  username: "admin"
  password: "${LOKI_PASSWORD}"                  # Use environment variable
```

**Bearer Token**:
```yaml
auth:
  type: "bearer"
  token: "${LOKI_TOKEN}"
```

#### Loki TLS Configuration

**TLS without client certificates**:
```yaml
url: "https://loki.example.com:3100"
tls:
  enabled: true
  verify_certificate: true
  ca_file: "/path/to/ca.crt"
```

**Mutual TLS (mTLS)**:
```yaml
url: "https://loki.example.com:3100"
tls:
  enabled: true
  verify_certificate: true
  ca_file: "/path/to/ca.crt"
  cert_file: "/path/to/client.crt"
  key_file: "/path/to/client.key"
  server_name: "loki.example.com"               # SNI server name
```

**Development mode** (⚠️ insecure):
```yaml
tls:
  enabled: true
  verify_certificate: false                     # ONLY for development!
```

#### Adaptive Batching

Adaptive batching dynamically adjusts batch size and flush delay based on:

- **Latency**: Reduces batch size if Loki latency increases
- **Throughput**: Increases batch size for higher throughput
- **Error rate**: Reduces batch size on errors
- **Queue utilization**: Adjusts based on queue pressure

Benefits:
- ✅ Automatic optimization under varying load
- ✅ Better latency during low traffic
- ✅ Higher throughput during high traffic
- ✅ Graceful degradation under errors

---

### Local File Sink

Write logs to local files with rotation.

#### `sinks.local_file` Section

```yaml
sinks:
  local_file:
    enabled: true                                           # Enable local file sink
    directory: "/app/logs/output"                           # Output directory

    # Filename patterns
    filename_pattern: "logs-{date}-{hour}.log"              # Fallback pattern
    filename_pattern_files: "logs-{nomedoarquivomonitorado}-{date}-{hour}.log"
    filename_pattern_containers: "logs-{nomedocontainer}-{date}-{hour}.log"

    # Output format
    output_format: "text"                                   # "text", "json", "csv"

    # Text format configuration
    text_format:
      include_timestamp: false                              # Don't add extra timestamp
      timestamp_format: "2006-01-02 15:04:05.000"
      include_labels: false                                 # Don't include labels
      field_separator: " | "
      raw_message_only: true                                # Only original message

    # File rotation
    rotation:
      enabled: true
      max_size_mb: 100                                      # Rotate at 100MB
      max_files: 10                                         # Keep 10 rotated files
      retention_days: 7                                     # Delete after 7 days
      compress: true                                        # Compress rotated files

    # Performance tuning
    auto_sync: true                                         # Sync to disk periodically
    sync_interval: "60s"                                    # Sync every 60 seconds
    file_permissions: "0644"                                # File permissions
    dir_permissions: "0755"                                 # Directory permissions
    queue_size: 25000                                       # Internal queue size
    worker_count: 4                                         # Concurrent writers
```

#### Filename Patterns

Available placeholders:

| Placeholder | Description | Example |
|-------------|-------------|---------|
| `{date}` | Current date (YYYY-MM-DD) | 2025-11-01 |
| `{hour}` | Current hour (HH) | 14 |
| `{minute}` | Current minute (MM) | 30 |
| `{nomedoarquivomonitorado}` | Source filename (file sources) | app.log |
| `{nomedocontainer}` | Container name (container sources) | myapp-prod |
| `{tenant}` | Tenant ID (multi-tenant mode) | prod |

Examples:
```yaml
# Hourly rotation by date and hour
filename_pattern: "logs-{date}-{hour}.log"
# Output: logs-2025-11-01-14.log

# Per-container files
filename_pattern_containers: "{nomedocontainer}-{date}.log"
# Output: myapp-prod-2025-11-01.log

# Per-source files
filename_pattern_files: "{nomedoarquivomonitorado}-{date}-{hour}.log"
# Output: app.log-2025-11-01-14.log
```

#### Output Formats

**Text Format** (human-readable):
```yaml
output_format: "text"
text_format:
  raw_message_only: true                        # Only log message
```

Output example:
```
2025-11-01 14:30:15 Application started successfully
2025-11-01 14:30:16 Connected to database
```

**JSON Format** (structured):
```yaml
output_format: "json"
```

Output example:
```json
{"timestamp":"2025-11-01T14:30:15Z","message":"Application started","level":"info","labels":{"service":"myapp"}}
```

**CSV Format** (spreadsheet-compatible):
```yaml
output_format: "csv"
```

Output example:
```
timestamp,level,message,service
2025-11-01T14:30:15Z,info,Application started,myapp
```

---

### Elasticsearch Sink

Send logs to Elasticsearch for indexing and search.

#### `sinks.elasticsearch` Section

```yaml
sinks:
  elasticsearch:
    enabled: false                              # Enable Elasticsearch sink
    urls:                                       # Elasticsearch cluster URLs
      - "http://elasticsearch:9200"
      - "http://elasticsearch2:9200"
    index: "logs-{date}"                        # Index name pattern
    username: "elastic"                         # Basic auth username
    password: "${ES_PASSWORD}"                  # Basic auth password

    # Performance tuning
    batch_size: 500                             # Documents per bulk request
    batch_timeout: "10s"                        # Maximum batch wait time
    timeout: "30s"                              # Request timeout
    compression: true                           # Enable gzip compression
```

#### Index Patterns

```yaml
# Daily indices
index: "logs-{date}"                            # logs-2025-11-01

# Monthly indices
index: "logs-{year}-{month}"                    # logs-2025-11

# Per-service indices
index: "logs-{service}-{date}"                  # logs-myapp-2025-11-01
```

---

### Splunk Sink

Send logs to Splunk HTTP Event Collector (HEC).

#### `sinks.splunk` Section

```yaml
sinks:
  splunk:
    enabled: false                              # Enable Splunk sink
    url: "https://splunk:8088"                  # Splunk HEC URL
    token: "${SPLUNK_HEC_TOKEN}"                # HEC token
    index: "main"                               # Splunk index
    source: "ssw-log-capturer"                  # Source identifier
    source_type: "_json"                        # Source type

    # Performance tuning
    batch_size: 100                             # Events per batch
    batch_timeout: "10s"                        # Maximum batch wait time
    timeout: "30s"                              # Request timeout
    compression: true                           # Enable gzip compression
```

---

## Dispatcher & Processing

### `dispatcher` Section

The dispatcher is the core component that receives logs and delivers them to sinks.

```yaml
dispatcher:
  queue_size: 50000                             # Internal queue capacity
  worker_count: 6                               # Number of worker goroutines
  send_timeout: "120s"                          # Sink delivery timeout

  # Batching configuration
  batch_size: 500                               # Logs per batch
  batch_timeout: "10s"                          # Maximum wait for batch

  # Retry configuration
  max_retries: 3                                # Maximum retry attempts
  retry_base_delay: "5s"                        # Initial retry delay
  retry_multiplier: 2                           # Exponential backoff multiplier
  retry_max_delay: "60s"                        # Maximum retry delay

  # Deduplication
  deduplication_enabled: true                   # Enable duplicate detection
  deduplication_config:
    max_cache_size: 100000                      # Maximum dedup cache entries
    ttl: "1h"                                   # Entry TTL in cache
    cleanup_interval: "10m"                     # Cache cleanup interval
    cleanup_threshold: 0.8                      # Cleanup at 80% full
    hash_algorithm: "sha256"                    # Hashing algorithm
    include_timestamp: false                    # Include timestamp in hash
    include_source_id: true                     # Include source ID in hash

  # Dead Letter Queue
  dlq_enabled: true                             # Enable DLQ
  dlq_config:
    # ... (see DLQ section below)
```

#### Queue Sizing

Queue size should be based on your traffic patterns:

**Low traffic** (< 100 logs/sec):
```yaml
queue_size: 10000
worker_count: 2
```

**Medium traffic** (100-1000 logs/sec):
```yaml
queue_size: 50000
worker_count: 6
```

**High traffic** (> 1000 logs/sec):
```yaml
queue_size: 100000
worker_count: 12
```

#### Deduplication

Prevents duplicate logs from being sent multiple times:

```yaml
deduplication_enabled: true
deduplication_config:
  hash_algorithm: "sha256"                      # Fast, collision-resistant
  include_timestamp: false                      # Ignore timestamp in dedup
  include_source_id: true                       # Consider source in dedup
  ttl: "1h"                                     # Consider duplicates within 1 hour
```

Hash is calculated from:
- Log message
- Source ID (if `include_source_id: true`)
- Timestamp (if `include_timestamp: true`)
- Selected labels

Use cases:
- **Container restarts**: Prevent duplicate logs during container restart
- **Network retries**: Avoid duplicates from failed send retries
- **Multi-path**: Deduplicate when same log arrives via multiple paths

---

### `processing` Section

Log processing pipelines for transformation and enrichment.

```yaml
processing:
  enabled: true                                 # Enable processing pipelines
  pipelines_file: "/app/configs/pipelines.yaml"  # Pipeline configuration file
  worker_count: 2                               # Processing workers
  queue_size: 5000                              # Processing queue size
  processing_timeout: "5s"                      # Per-log timeout
  skip_failed_logs: true                        # Skip logs that fail processing
  enrich_logs: true                             # Enable log enrichment
```

#### Processing Pipelines

The `pipelines.yaml` file defines processing steps:

```yaml
pipelines:
  - name: "json_parser"
    enabled: true
    steps:
      - type: "parse_json"
        config:
          field: "message"
          target: "parsed"

      - type: "extract_fields"
        config:
          source: "parsed"
          fields:
            - "level"
            - "timestamp"
            - "user_id"

      - type: "add_labels"
        config:
          labels:
            parsed: "true"
            pipeline: "json_parser"
```

---

## Storage & Persistence

### `positions` Section

Track file reading positions to resume after restarts.

```yaml
positions:
  enabled: true                                 # Enable position tracking
  directory: "/app/data/positions"              # Position files directory
  flush_interval: "10s"                         # Write positions every 10s
  max_memory_buffer: 2000                       # Max buffered position updates
  max_memory_positions: 10000                   # Max positions in memory
  force_flush_on_exit: true                     # Flush positions on shutdown
  cleanup_interval: "1m"                        # Cleanup stale positions
  max_position_age: "12h"                       # Remove positions older than 12h
```

Position files are stored as JSON:

```json
{
  "/var/log/app.log": {
    "offset": 1234567,
    "last_update": "2025-11-01T14:30:15Z",
    "checksum": "sha256:abcdef..."
  }
}
```

---

### `disk_buffer` Section

Persistent buffering when sinks are unavailable.

```yaml
disk_buffer:
  enabled: true                                 # Enable disk buffering
  directory: "/app/buffer"                      # Buffer directory
  max_file_size: 104857600                      # 100MB per file
  max_total_size: 1073741824                    # 1GB total buffer
  max_files: 50                                 # Maximum buffer files
  compression_enabled: true                     # Compress buffer files
  sync_interval: "5s"                           # Fsync interval
  cleanup_interval: "1h"                        # Cleanup old buffers
  retention_period: "24h"                       # Buffer retention
  file_permissions: "0644"
  dir_permissions: "0755"
```

Buffer is used when:
- Sink is unavailable
- Sink returns errors
- Timeout occurs
- Backpressure threshold reached

---

### `cleanup` Section

Automated disk space management.

```yaml
cleanup:
  enabled: true                                 # Enable disk cleanup
  check_interval: "30m"                         # Check every 30 minutes
  critical_space_threshold: 5.0                 # Critical at 5% free
  warning_space_threshold: 15.0                 # Warning at 15% free

  directories:
    - path: "/app/logs"
      max_size_mb: 1024                         # 1GB maximum
      retention_days: 7                         # Keep 7 days
      file_patterns:
        - "*.log"
        - "*.txt"
      max_files: 100
      cleanup_age_seconds: 86400                # 24 hours

    - path: "/app/dlq"
      max_size_mb: 512                          # 512MB maximum
      retention_days: 7
      file_patterns:
        - "*.json"
      max_files: 50
```

---

## Dead Letter Queue (DLQ)

### `dispatcher.dlq_config` Section

Failed logs are stored in the DLQ for later reprocessing.

```yaml
dispatcher:
  dlq_enabled: true
  dlq_config:
    enabled: true
    directory: "/app/dlq"                       # DLQ storage directory
    max_size_mb: 100                            # Maximum DLQ size
    max_files: 10                               # Maximum DLQ files
    retention_days: 7                           # DLQ retention period
    write_timeout: "5s"                         # Write timeout
    retry_delay: "30s"                          # Delay before first retry
    max_retries: 3                              # Maximum reprocess attempts

    # Automatic reprocessing
    reprocessing_config:
      enabled: true                             # Enable auto-reprocessing
      interval: "5m"                            # Check every 5 minutes
      max_retries: 3                            # Max reprocess attempts
      initial_delay: "2m"                       # Wait 2 min before first retry
      delay_multiplier: 2.0                     # Exponential backoff
      max_delay: "30m"                          # Max 30 min delay
      batch_size: 50                            # Reprocess 50 entries/batch
      timeout: "30s"                            # Timeout per batch
      min_entry_age: "2m"                       # Wait at least 2 min before retrying

    # Alerting
    alert_config:
      enabled: true
      entries_per_minute_threshold: 50          # Alert if > 50 entries/min
      total_entries_threshold: 1000             # Alert if > 1000 total entries
      queue_size_threshold: 8000                # Alert if queue > 8000
      check_interval: "1m"                      # Check every minute
      cooldown_period: "5m"                     # Cooldown between alerts
      webhook_url: ""                           # Webhook for alerts
      email_to: ""                              # Email for alerts
      include_stats: true                       # Include stats in alerts
```

#### DLQ Entry Format

```json
{
  "entry_id": "dlq-1730469015-abc123",
  "original_entry": {
    "message": "Log message",
    "timestamp": "2025-11-01T14:30:15Z",
    "labels": {...}
  },
  "failure_reason": "timeout after 120s",
  "failure_time": "2025-11-01T14:30:15Z",
  "retry_count": 2,
  "last_retry_time": "2025-11-01T14:35:15Z",
  "sink_name": "loki"
}
```

#### DLQ Management

**View DLQ statistics**:
```bash
curl http://localhost:8401/dlq/stats
```

**Reprocess entries manually**:
```bash
curl -X POST http://localhost:8401/dlq/reprocess \
  -H "Content-Type: application/json" \
  -d '{"entry_ids": ["dlq-123", "dlq-456"]}'
```

**Reprocess all entries**:
```bash
curl -X POST http://localhost:8401/dlq/reprocess \
  -H "Content-Type: application/json" \
  -d '{"reprocess_all": true}'
```

---

## Enterprise Features

### Multi-Tenancy

#### `multi_tenant` Section

Isolate logs by tenant for multi-customer deployments.

```yaml
multi_tenant:
  enabled: true                                 # Enable multi-tenancy
  default_tenant: "default"                     # Default tenant
  isolation_mode: "soft"                        # "soft" or "hard"

  # Tenant discovery
  tenant_discovery:
    enabled: true                               # Auto-discover tenants
    update_interval: "30s"                      # Scan for tenants every 30s
    config_paths:
      - "/app/tenants"
      - "/etc/tenants"
    auto_create_tenants: true
    auto_update_tenants: true
    file_formats: ["yaml", "json"]

  # Resource isolation
  resource_isolation:
    enabled: true
    cpu_isolation: true
    memory_isolation: true
    enforcement_mode: "throttle"                # "warn", "throttle", "block"

    default_limits:
      max_memory_mb: 512                        # 512MB per tenant
      max_cpu_percent: 25.0                     # 25% CPU per tenant
      max_events_per_sec: 1000                  # 1000 logs/sec per tenant
      max_goroutines: 1000

  # Routing
  tenant_routing:
    enabled: true
    routing_strategy: "label"                   # "label", "header", "source"
    tenant_header: "X-Tenant-ID"
    tenant_label: "tenant"
    fallback_tenant: "default"

    routing_rules:
      - name: "production_logs"
        priority: 100
        conditions:
          label_env: "production"
        tenant_id: "prod"
        enabled: true
```

---

### Security

#### `security` Section

Authentication, authorization, and encryption.

```yaml
security:
  enabled: false                                # Enable security features

  # Authentication
  authentication:
    enabled: false
    method: "bearer"                            # "none", "basic", "bearer", "jwt", "mtls"
    session_timeout: "24h"
    max_attempts: 5
    lockout_time: "15m"

  # Authorization (RBAC)
  authorization:
    enabled: false
    default_role: "viewer"

  # TLS
  tls:
    enabled: false
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"
    ca_file: "/path/to/ca.pem"
    verify_client: false                        # Enable for mTLS

  # Rate limiting
  rate_limiting:
    enabled: true
    requests_per_second: 1000
    burst_size: 2000
    per_ip: false

  # CORS
  cors:
    enabled: false
    allowed_origins: ["*"]
    allowed_methods: ["GET", "POST"]
    allowed_headers: ["Content-Type"]
```

See [Security Documentation](SECURITY.md) for detailed security configuration.

---

### Distributed Tracing

#### `tracing` Section

Integrate with Jaeger, Zipkin, or OpenTelemetry.

```yaml
tracing:
  enabled: false                                # Enable distributed tracing
  service_name: "ssw-logs-capture"
  service_version: "v0.0.2"
  environment: "production"
  exporter: "otlp"                              # "jaeger", "otlp", "zipkin"
  endpoint: "http://jaeger:4318/v1/traces"
  sample_rate: 0.01                             # 1% sampling
  batch_timeout: "5s"
  max_batch_size: 512
```

---

### SLO Monitoring

#### `slo` Section

Service Level Objective monitoring and error budget tracking.

```yaml
slo:
  enabled: false                                # Enable SLO monitoring
  prometheus_url: "http://prometheus:9090"
  evaluation_interval: "1m"
  retention_period: "30d"
```

---

### Resource Monitoring

#### `resource_monitoring` Section

Detect goroutine and file descriptor leaks.

```yaml
resource_monitoring:
  enabled: true                                 # Enable resource monitoring
  check_interval: "15s"                         # Check every 15 seconds
  fd_leak_threshold: 20                         # Alert if FD increases by 20
  goroutine_leak_threshold: 20                  # Alert if goroutines increase by 20
  memory_leak_threshold: 52428800               # Alert if memory increases by 50MB
  alert_cooldown: "2m"                          # Cooldown between alerts
  enable_memory_profiling: true                 # Enable memory profiling
  enable_gc_optimization: true                  # Enable GC optimization
```

---

## Common Scenarios

### Development Environment

Minimal configuration for local development:

```yaml
app:
  environment: "development"
  log_level: "debug"
  log_format: "text"

server:
  enabled: true
  port: 8401

metrics:
  enabled: true
  port: 8001

container_monitor:
  enabled: true
  socket_path: "unix:///var/run/docker.sock"
  exclude_names:
    - "log_capturer_go"

dispatcher:
  queue_size: 10000
  worker_count: 2

sinks:
  local_file:
    enabled: true
    directory: "./logs"
    output_format: "text"

  loki:
    enabled: false
```

---

### Production Environment

Optimized configuration for production:

```yaml
app:
  environment: "production"
  log_level: "info"
  log_format: "json"

server:
  enabled: true
  host: "0.0.0.0"
  port: 8401

metrics:
  enabled: true
  port: 8001

container_monitor:
  enabled: true
  max_concurrent: 50
  include_stdout: true
  include_stderr: true

dispatcher:
  queue_size: 100000
  worker_count: 12
  deduplication_enabled: true
  dlq_enabled: true

sinks:
  loki:
    enabled: true
    url: "https://loki-prod.example.com:3100"
    batch_size: 1000
    adaptive_batching:
      enabled: true
    tls:
      enabled: true
      verify_certificate: true
      ca_file: "/etc/ssl/certs/ca.crt"
    auth:
      type: "bearer"
      token: "${LOKI_TOKEN}"

resource_monitoring:
  enabled: true
  check_interval: "15s"
```

---

### High Availability Setup

Configuration for HA deployment:

```yaml
dispatcher:
  queue_size: 200000
  worker_count: 24

disk_buffer:
  enabled: true                                 # Buffer during outages
  directory: "/app/buffer"
  max_total_size: 5368709120                    # 5GB buffer

sinks:
  loki:
    enabled: true
    url: "https://loki-ha.example.com:3100"
    backpressure_config:
      enabled: true
    dlq_config:
      enabled: true
      send_on_timeout: true
      send_on_error: true
      reprocessing_config:
        enabled: true
        interval: "5m"

hot_reload:
  enabled: true                                 # Dynamic reconfiguration
  validate_on_reload: true
  failsafe_mode: true
```

---

## Environment Variables

All configuration values can be overridden with environment variables:

### Syntax

```bash
# Format: SSW_<SECTION>_<FIELD>
export SSW_APP_LOG_LEVEL=debug
export SSW_SERVER_PORT=8401
export SSW_METRICS_ENABLED=true

# Nested fields use underscores
export SSW_SINKS_LOKI_URL=http://loki:3100
export SSW_DISPATCHER_QUEUE_SIZE=50000
```

### Common Variables

```bash
# Application
export SSW_APP_ENVIRONMENT=production
export SSW_APP_LOG_LEVEL=info
export SSW_DEFAULT_CONFIGS=true

# Server
export SSW_SERVER_PORT=8401
export SSW_METRICS_PORT=8001

# Loki
export SSW_SINKS_LOKI_ENABLED=true
export SSW_SINKS_LOKI_URL=http://loki:3100
export LOKI_TOKEN=secret_token                  # Referenced as ${LOKI_TOKEN}

# Docker
export SSW_CONTAINER_MONITOR_ENABLED=true
export DOCKER_HOST=unix:///var/run/docker.sock
```

### Secrets in Configuration

Use environment variable substitution for secrets:

```yaml
sinks:
  loki:
    auth:
      token: "${LOKI_TOKEN}"                    # Replaced at runtime

  elasticsearch:
    password: "${ES_PASSWORD}"

  splunk:
    token: "${SPLUNK_HEC_TOKEN}"
```

---

## Validation Rules

### Required Fields

When `enabled: true`, these fields are required:

**Loki Sink**:
- `url`
- `push_endpoint`

**Elasticsearch Sink**:
- `urls` (at least one URL)
- `index`

**Splunk Sink**:
- `url`
- `token`

### Value Ranges

| Field | Minimum | Maximum | Default |
|-------|---------|---------|---------|
| `dispatcher.queue_size` | 1000 | 1000000 | 50000 |
| `dispatcher.worker_count` | 1 | 100 | 6 |
| `dispatcher.batch_size` | 1 | 10000 | 500 |
| `container_monitor.max_concurrent` | 1 | 1000 | 25 |

### Duration Formats

```yaml
timeout: "30s"                                  # 30 seconds
timeout: "5m"                                   # 5 minutes
timeout: "1h"                                   # 1 hour
timeout: "1h30m"                                # 1 hour 30 minutes
```

---

## Best Practices

### General Configuration

1. **Always enable metrics**: Essential for monitoring and alerting
2. **Use environment variables for secrets**: Never commit secrets to Git
3. **Enable DLQ in production**: Prevents log loss during failures
4. **Configure disk cleanup**: Prevent disk space exhaustion
5. **Enable resource monitoring**: Detect leaks early

### Performance Tuning

1. **Queue sizing**: Set `queue_size` to handle 2-5 minutes of peak traffic
2. **Worker count**: Start with `2 * CPU cores`, adjust based on metrics
3. **Batch size**: Larger batches = better throughput, higher latency
4. **Enable adaptive batching**: Let the system optimize automatically

### Security

1. **Enable authentication** in production
2. **Use TLS for all external connections**
3. **Minimize exposed endpoints**: Bind to `127.0.0.1` when possible
4. **Configure rate limiting**: Prevent abuse
5. **Enable sanitization**: Prevent credential leakage in logs

### High Availability

1. **Enable disk buffering**: Handle temporary sink outages
2. **Configure DLQ with auto-reprocessing**: Automatic recovery
3. **Enable hot reload**: Update configuration without downtime
4. **Monitor queue utilization**: Alert before capacity is reached
5. **Set appropriate timeouts**: Balance reliability and latency

### File Monitoring

1. **Use specific file patterns**: Avoid monitoring unnecessary files
2. **Exclude archived files**: `*.gz`, `*.zip`, `*.old`
3. **Set `max_file_size`**: Prevent monitoring huge files
4. **Enable position tracking**: Resume after restarts
5. **Use `recursive: false`** when possible: Better performance

### Container Monitoring

1. **Use label filtering**: Only capture relevant containers
2. **Exclude monitoring containers**: Avoid capturing own logs
3. **Set reasonable `tail_lines`**: Avoid reading entire log history
4. **Monitor `max_concurrent`**: Adjust based on container count

---

## Configuration Validation

### Validate Configuration

```bash
# Syntax check
yamllint /app/configs/config.yaml

# Validation via API
curl -X POST http://localhost:8401/config/validate \
  -H "Content-Type: application/yaml" \
  --data-binary @config.yaml
```

### Test Configuration

```bash
# Dry-run mode (validation only, doesn't start)
./ssw-logs-capture --config config.yaml --dry-run --validate

# Check configuration reload
curl -X POST http://localhost:8401/config/reload
```

---

## Troubleshooting Configuration

### Common Issues

**"Queue full" warnings**:
```yaml
# Solution: Increase queue size or worker count
dispatcher:
  queue_size: 100000                            # Increased from 50000
  worker_count: 12                              # Increased from 6
```

**High memory usage**:
```yaml
# Solution: Reduce queue sizes and batch sizes
dispatcher:
  queue_size: 25000
  batch_size: 250
sinks:
  loki:
    queue_size: 10000
    batch_size: 250
```

**Slow log delivery**:
```yaml
# Solution: Increase batch size and timeout
sinks:
  loki:
    batch_size: 1000                            # Increased from 500
    batch_timeout: "5s"                         # Reduced from 15s
```

**Connection timeouts**:
```yaml
# Solution: Increase timeouts
sinks:
  loki:
    timeout: "180s"                             # Increased from 120s
dispatcher:
  send_timeout: "180s"
```

### Debug Configuration

Enable debug logging to troubleshoot configuration issues:

```yaml
app:
  log_level: "debug"                            # Most verbose logging
```

Check configuration via API:

```bash
# View current configuration (sanitized)
curl http://localhost:8401/config

# View specific section
curl http://localhost:8401/config?section=dispatcher
```

---

## See Also

- [API Documentation](API.md) - HTTP API reference
- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common problems and solutions
- [Security Guide](SECURITY.md) - Security best practices
- [README](../README.md) - Getting started guide

---

**Last Updated**: 2025-11-01
**Version**: 1.0
**Maintained By**: SSW Logs Capture Team
