```bash
GET /health
```
**Response**:
```json
{
  "status": "healthy|degraded|unhealthy",
  "components": {
    "file_monitor": {"status": "healthy", "last_check": "2024-01-15T10:30:00Z"},
    "container_monitor": {"status": "healthy", "last_check": "2024-01-15T10:30:00Z"},
    "dispatcher": {"status": "healthy", "queue_size": 150},
    "loki_sink": {"status": "healthy", "queue_util": 0.45},
    "position_manager": {"status": "healthy", "positions": 1250}
  },
  "issues": [],
  "check_time": "2024-01-15T10:30:05Z"
}
```

### 📊 **Metrics API**:

```bash
GET /metrics
```
**Response**: Prometheus formatted metrics

### 📈 **Stats API**:

```bash
GET /stats
```
**Response**:
```json
{
  "dispatcher": {
    "total_processed": 1234567,
    "error_count": 45,
    "queue_size": 150,
    "duplicates_detected": 8901,
    "throughput_per_sec": 125.5
  },
  "file_monitor": {
    "files_watched": 245,
    "total_logs": 567890,
    "errors": 12
  },
  "container_monitor": {
    "containers_monitored": 67,
    "total_logs": 890123,
    "reconnections": 3
  },
  "sinks": {
    "loki": {"queue_util": 0.45, "errors": 2},
    "file": {"queue_util": 0.20, "errors": 0}
  }
}
```

### 🔧 **Configuration API**:

```bash
GET /config
```
**Response**: Configuração atual (sanitizada)

```bash
POST /config/reload
```
**Action**: Reload configuração sem restart

### 📍 **Positions API**:

```bash
GET /positions
```
**Response**:
```json
{
  "files": [
    {
      "path": "/var/log/syslog",
      "position": 1048576,
      "last_update": "2024-01-15T10:30:00Z"
    }
  ],
  "containers": [
    {
      "id": "abc123",
      "name": "web-app",
      "last_timestamp": "2024-01-15T10:30:00Z"
    }
  ]
}
```

### 🗃️ **DLQ API**:

```bash
GET /dlq/stats
```
**Response**:
```json
{
  "total_entries": 145,
  "size_mb": 12.5,
  "oldest_entry": "2024-01-15T09:00:00Z",
  "retry_queue_size": 23
}
```

```bash
POST /dlq/reprocess
```
**Action**: Força reprocessamento da DLQ

### 🎯 **Debug APIs**:

```bash
GET /debug/goroutines
GET /debug/memory
GET /debug/positions/validate
```

---