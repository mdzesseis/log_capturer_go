# API Documentation - Log Capturer Go

**Version**: v0.0.2
**Base URL**: `http://localhost:8401` (default)
**Content-Type**: `application/json`

---

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Core Endpoints](#core-endpoints)
  - [Health Check](#health-check)
  - [Statistics](#statistics)
  - [Configuration](#configuration)
  - [Positions](#positions)
  - [Dead Letter Queue](#dead-letter-queue)
  - [Metrics](#metrics)
- [Debug Endpoints](#debug-endpoints)
- [Enterprise Endpoints](#enterprise-endpoints)
- [Error Responses](#error-responses)

---

## Overview

The Log Capturer Go API provides HTTP endpoints for monitoring, configuration, and control of the log capture system. All endpoints return JSON responses and follow RESTful principles.

### Default Ports

| Service | Port | Purpose |
|---------|------|---------|
| API Server | 8401 | Main HTTP API |
| Metrics Server | 8001 | Prometheus metrics |
| pprof Server | 6060 | Go profiling and debugging |

---

## Authentication

### Bearer Token (Optional)

If security is enabled, all API requests require a Bearer token:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN_HERE" \
  http://localhost:8401/health
```

### Configuration

```yaml
security:
  enabled: true
  auth_type: "bearer"
  jwt_secret: "${JWT_SECRET}"
```

**Response Codes**:
- `401 Unauthorized`: Missing or invalid token
- `403 Forbidden`: Valid token but insufficient permissions

---

## Core Endpoints

### Health Check

Provides comprehensive health status for the application and all components.

**Endpoint**: `GET /health`

**Response Codes**:
- `200 OK`: All components healthy
- `503 Service Unavailable`: One or more components degraded

**Example Request**:
```bash
curl http://localhost:8401/health
```

**Example Response**:
```json
{
  "status": "healthy",
  "timestamp": 1730476800,
  "version": "v1.0.0",
  "uptime": "2h30m15s",
  "services": {
    "dispatcher": {
      "status": "healthy",
      "stats": {
        "processed": 150000,
        "failed": 25,
        "queue_size": 42,
        "queue_capacity": 1000
      }
    },
    "file_monitor": {
      "status": "healthy",
      "enabled": true
    },
    "container_monitor": {
      "status": "healthy",
      "enabled": true
    }
  },
  "checks": {
    "queue_utilization": {
      "status": "healthy",
      "utilization": "4.20%",
      "size": 42,
      "capacity": 1000
    },
    "memory": {
      "status": "healthy",
      "alloc_mb": 256,
      "sys_mb": 512,
      "goroutines": 127
    },
    "disk_space": {
      "status": "healthy",
      "path": "/var/log/log-capturer"
    },
    "sink_connectivity": {
      "status": "healthy",
      "dlq_entries": {
        "total_entries": 10,
        "current_queue_size": 0
      }
    },
    "file_descriptors": {
      "status": "healthy",
      "open": 45,
      "max": 1024,
      "utilization": "4.39%"
    }
  }
}
```

**Health Status Values**:
- `healthy`: All systems operational
- `warning`: Minor issues detected
- `degraded`: Significant issues, service partially operational
- `critical`: Major issues, service may be non-functional

---

### Statistics

Returns detailed operational statistics for all application components.

**Endpoint**: `GET /stats`

**Response Code**: `200 OK`

**Example Request**:
```bash
curl http://localhost:8401/stats
```

**Example Response**:
```json
{
  "application": {
    "name": "ssw-logs-capture",
    "version": "v1.0.0",
    "uptime": "5h23m45s",
    "goroutines": 127,
    "timestamp": 1730476800
  },
  "dispatcher": {
    "processed": 500000,
    "failed": 125,
    "queue_size": 42,
    "queue_capacity": 1000,
    "processing_rate": 278.5,
    "average_latency": "15ms"
  },
  "positions": {
    "tracked_files": 25,
    "buffer_size": 1024,
    "last_flush": "2025-11-01T12:30:00Z"
  },
  "resources": {
    "memory_mb": 256,
    "cpu_percent": 45.2,
    "goroutines": 127,
    "file_descriptors": 45
  },
  "dlq": {
    "total_entries": 125,
    "entries_written": 125,
    "current_queue_size": 10,
    "reprocessing_attempts": 50,
    "reprocessing_successes": 45
  }
}
```

---

### Configuration

Retrieve or reload the application configuration.

#### Get Configuration

Returns sanitized configuration (no secrets).

**Endpoint**: `GET /config`

**Response Code**: `200 OK`

**Example Request**:
```bash
curl http://localhost:8401/config
```

**Example Response**:
```json
{
  "app": {
    "name": "ssw-logs-capture",
    "version": "v1.0.0",
    "log_level": "info"
  },
  "metrics": {
    "enabled": true,
    "port": 8001
  },
  "processing": {
    "workers": 4,
    "batch_size": 100
  },
  "dispatcher": {
    "queue_size": 1000,
    "worker_count": 4,
    "batch_size": 100,
    "batch_timeout": "5s"
  },
  "sinks": {
    "loki_enabled": true,
    "local_file_enabled": true
  }
}
```

#### Reload Configuration

Triggers a hot reload of the configuration.

**Endpoint**: `POST /config/reload`

**Response Codes**:
- `200 OK`: Reload triggered successfully
- `500 Internal Server Error`: Reload failed
- `503 Service Unavailable`: Hot reload not enabled

**Example Request**:
```bash
curl -X POST http://localhost:8401/config/reload
```

**Example Response**:
```json
{
  "status": "success",
  "message": "Configuration reload triggered successfully."
}
```

---

### Positions

Returns file position tracking status.

**Endpoint**: `GET /positions`

**Response Codes**:
- `200 OK`: Position statistics returned
- `503 Service Unavailable`: Position manager not available

**Example Request**:
```bash
curl http://localhost:8401/positions
```

**Example Response**:
```json
{
  "tracked_files": 25,
  "buffer_size": 1024,
  "flush_interval": "10s",
  "last_flush": "2025-11-01T12:30:00Z",
  "positions": {
    "/var/log/app.log": {
      "offset": 1048576,
      "last_read": "2025-11-01T12:35:00Z"
    },
    "/var/log/access.log": {
      "offset": 2097152,
      "last_read": "2025-11-01T12:35:05Z"
    }
  }
}
```

---

### Dead Letter Queue

Manage and monitor the Dead Letter Queue for failed log entries.

#### Get DLQ Statistics

**Endpoint**: `GET /dlq/stats`

**Response Codes**:
- `200 OK`: DLQ statistics returned
- `503 Service Unavailable`: DLQ not available

**Example Request**:
```bash
curl http://localhost:8401/dlq/stats
```

**Example Response**:
```json
{
  "total_entries": 125,
  "entries_written": 125,
  "write_errors": 0,
  "current_queue_size": 10,
  "files_created": 5,
  "last_flush": "2025-11-01T12:30:00Z",
  "reprocessing_attempts": 50,
  "reprocessing_successes": 45,
  "reprocessing_failures": 5,
  "last_reprocessing": "2025-11-01T12:35:00Z",
  "entries_reprocessed": 45
}
```

#### Reprocess DLQ

Triggers reprocessing of failed entries in the DLQ.

**Endpoint**: `POST /dlq/reprocess`

**Response Codes**:
- `200 OK`: Information returned (automatic reprocessing)
- `503 Service Unavailable`: DLQ not available

**Example Request**:
```bash
curl -X POST http://localhost:8401/dlq/reprocess
```

**Example Response**:
```json
{
  "status": "info",
  "message": "Manual DLQ reprocessing not implemented. Entries are automatically reprocessed by the background loop.",
  "timestamp": 1730476800,
  "dlq_stats": {
    "total_entries": 125,
    "current_queue_size": 10
  }
}
```

---

### Metrics

Proxy endpoint for Prometheus metrics.

**Endpoint**: `GET /metrics`

**Response Codes**:
- `200 OK`: Metrics returned
- `502 Bad Gateway`: Error proxying to metrics server
- `503 Service Unavailable`: Metrics server not available

**Example Request**:
```bash
curl http://localhost:8401/metrics
```

**Example Response** (Prometheus format):
```
# HELP logs_processed_total Total number of logs processed
# TYPE logs_processed_total counter
logs_processed_total{source_type="file",source_id="/var/log/app.log",pipeline="default"} 50000

# HELP dispatcher_queue_utilization Current utilization of the dispatcher queue
# TYPE dispatcher_queue_utilization gauge
dispatcher_queue_utilization 0.042

# HELP memory_usage_bytes Memory usage in bytes
# TYPE memory_usage_bytes gauge
memory_usage_bytes{type="heap_alloc"} 268435456
```

---

## Debug Endpoints

### Goroutine Information

Returns detailed goroutine statistics for debugging.

**Endpoint**: `GET /debug/goroutines`

**Response Code**: `200 OK`

**Example Request**:
```bash
curl http://localhost:8401/debug/goroutines
```

**Example Response**:
```json
{
  "goroutines": 127,
  "cpus": 8,
  "cgocalls": 42,
  "timestamp": 1730476800,
  "memory": {
    "alloc": 268435456,
    "total_alloc": 1073741824,
    "sys": 536870912,
    "num_gc": 25
  },
  "tracker_stats": {
    "status": "healthy",
    "current_count": 127,
    "peak_count": 145
  }
}
```

### Memory Statistics

Returns detailed memory usage and GC statistics.

**Endpoint**: `GET /debug/memory`

**Response Code**: `200 OK`

**Example Request**:
```bash
curl http://localhost:8401/debug/memory
```

**Example Response**:
```json
{
  "timestamp": 1730476800,
  "heap": {
    "alloc": 268435456,
    "total_alloc": 1073741824,
    "sys": 536870912,
    "heap_alloc": 268435456,
    "heap_sys": 536870912,
    "heap_idle": 201326592,
    "heap_inuse": 335544320,
    "heap_released": 167772160,
    "heap_objects": 125000
  },
  "stack": {
    "stack_inuse": 2097152,
    "stack_sys": 2097152
  },
  "gc": {
    "num_gc": 25,
    "num_forced_gc": 0,
    "gc_cpu_fraction": 0.002,
    "next_gc": 335544320,
    "last_gc": "2025-11-01T12:35:00Z"
  }
}
```

### Position Validation

Validates the integrity of position tracking data.

**Endpoint**: `GET /debug/positions/validate`

**Response Codes**:
- `200 OK`: Validation completed
- `503 Service Unavailable`: Position manager not available

**Example Request**:
```bash
curl http://localhost:8401/debug/positions/validate
```

**Example Response**:
```json
{
  "timestamp": 1730476800,
  "status": "healthy",
  "issues": [],
  "stats": {
    "tracked_files": 25,
    "buffer_size": 1024
  }
}
```

---

## Enterprise Endpoints

These endpoints are only available when enterprise features are enabled.

### SLO Status

Service Level Objective monitoring status.

**Endpoint**: `GET /slo/status`

**Response Codes**:
- `200 OK`: SLO status returned
- `503 Service Unavailable`: SLO manager not available

**Example Request**:
```bash
curl http://localhost:8401/slo/status
```

**Example Response**:
```json
{
  "slos": {
    "availability": {
      "target": 99.9,
      "current": 99.95,
      "status": "met",
      "error_budget_remaining": 75.0
    },
    "latency_p99": {
      "target": 500,
      "current": 245,
      "status": "met"
    }
  }
}
```

### Goroutine Tracker Stats

Detailed goroutine tracking and leak detection.

**Endpoint**: `GET /goroutines/stats`

**Response Codes**:
- `200 OK`: Goroutine stats returned
- `503 Service Unavailable`: Goroutine tracker not available

**Example Request**:
```bash
curl http://localhost:8401/goroutines/stats
```

**Example Response**:
```json
{
  "status": "healthy",
  "current_count": 127,
  "peak_count": 145,
  "average_count": 130,
  "leak_detected": false,
  "growth_rate": 0.05
}
```

### Security Audit

Security audit logs and authentication statistics.

**Endpoint**: `GET /security/audit`

**Response Codes**:
- `200 OK`: Audit data returned
- `503 Service Unavailable`: Security manager not available

**Example Request**:
```bash
curl http://localhost:8401/security/audit
```

**Example Response**:
```json
{
  "message": "Audit log feature not yet implemented",
  "status": "pending"
}
```

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "Description of the error",
  "code": "ERROR_CODE",
  "timestamp": 1730476800
}
```

### Common Error Codes

| Code | Description |
|------|-------------|
| `401` | Unauthorized - Missing or invalid authentication |
| `403` | Forbidden - Insufficient permissions |
| `404` | Not Found - Endpoint does not exist |
| `500` | Internal Server Error - Server-side error |
| `503` | Service Unavailable - Component not available |

### Example Error Response

```bash
curl http://localhost:8401/invalid-endpoint
```

```json
{
  "error": "Endpoint not found",
  "code": "NOT_FOUND",
  "timestamp": 1730476800
}
```

---

## Rate Limiting

API endpoints may be rate-limited when security is enabled.

**Response Code**: `429 Too Many Requests`

**Headers**:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1730476860
```

---

## Best Practices

### 1. Health Checks

Use `/health` for load balancer health checks:

```bash
# Simple health check
curl -f http://localhost:8401/health || exit 1

# Check specific status
curl -s http://localhost:8401/health | jq -e '.status == "healthy"'
```

### 2. Monitoring

Collect metrics from `/metrics` endpoint:

```bash
# Prometheus scrape config
scrape_configs:
  - job_name: 'log_capturer'
    static_configs:
      - targets: ['log_capturer:8001']
    metrics_path: /metrics
```

### 3. Configuration Management

Always validate configuration before reloading:

```bash
# Test configuration first
./log_capturer --config=config.yaml --validate

# Then reload if valid
curl -X POST http://localhost:8401/config/reload
```

### 4. Error Handling

Always check response codes:

```bash
response=$(curl -s -w "\n%{http_code}" http://localhost:8401/health)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" -eq 200 ]; then
  echo "Healthy: $body"
else
  echo "Unhealthy (HTTP $http_code): $body"
  exit 1
fi
```

---

## Integration Examples

### Python

```python
import requests

# Health check
response = requests.get('http://localhost:8401/health')
if response.status_code == 200:
    health = response.json()
    print(f"Status: {health['status']}")
    print(f"Uptime: {health['uptime']}")
else:
    print(f"Error: {response.status_code}")

# With authentication
headers = {'Authorization': 'Bearer YOUR_TOKEN'}
stats = requests.get('http://localhost:8401/stats', headers=headers).json()
print(f"Processed: {stats['dispatcher']['processed']}")
```

### Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

type HealthResponse struct {
    Status    string `json:"status"`
    Timestamp int64  `json:"timestamp"`
    Version   string `json:"version"`
}

func main() {
    resp, err := http.Get("http://localhost:8401/health")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    var health HealthResponse
    json.NewDecoder(resp.Body).Decode(&health)
    fmt.Printf("Status: %s\n", health.Status)
}
```

### Bash

```bash
#!/bin/bash

# Complete health check script
API_URL="http://localhost:8401"

echo "Checking API health..."
health=$(curl -sf "$API_URL/health")

if [ $? -eq 0 ]; then
    status=$(echo "$health" | jq -r '.status')
    uptime=$(echo "$health" | jq -r '.uptime')

    echo "Status: $status"
    echo "Uptime: $uptime"

    if [ "$status" != "healthy" ]; then
        echo "WARNING: System is $status"
        exit 1
    fi
else
    echo "ERROR: API is not responding"
    exit 1
fi
```

---

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2025-11-01 | Initial API documentation |

---

**Last Updated**: 2025-11-02
**Maintained By**: SSW Development Team
**Support**: See [TROUBLESHOOTING.md](./TROUBLESHOOTING.md)
