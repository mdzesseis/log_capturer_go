# 🚀 SSW Logs Capture - Deployment Guide

## 📋 Table of Contents
1. [Overview](#overview)
2. [System Requirements](#system-requirements)
3. [Installation Methods](#installation-methods)
4. [Configuration](#configuration)
5. [Production Deployment](#production-deployment)
6. [Security Configuration](#security-configuration)
7. [Monitoring Setup](#monitoring-setup)
8. [Troubleshooting](#troubleshooting)
9. [Scaling Guidelines](#scaling-guidelines)
10. [Maintenance](#maintenance)

---

## 📖 Overview

The SSW Logs Capture system is designed for enterprise-grade log collection, processing, and distribution. This guide covers deployment strategies from development to production environments.

### 🎯 Deployment Scenarios

- **Development**: Single-node setup for testing and development
- **Staging**: Multi-component setup with observability stack
- **Production**: High-availability, scalable deployment
- **Enterprise**: Multi-tenant, security-hardened deployment

---

## 💻 System Requirements

### 🖥️ Minimum Requirements

| Component | Development | Production |
|-----------|-------------|------------|
| **CPU** | 2 cores | 4+ cores |
| **Memory** | 4 GB | 8+ GB |
| **Disk** | 50 GB | 200+ GB SSD |
| **Network** | 100 Mbps | 1+ Gbps |
| **OS** | Linux/macOS/Windows | Linux (Ubuntu 20.04+, RHEL 8+) |

### 🔧 Software Dependencies

```bash
# Required
- Docker 20.10+
- Docker Compose 2.0+
- Go 1.21+ (for source builds)

# Optional
- Kubernetes 1.20+ (for K8s deployment)
- Helm 3.0+ (for Helm charts)
- Prometheus (for metrics)
- Grafana (for dashboards)
```

### 📊 Capacity Planning

**Per 1000 log sources:**
- CPU: ~0.5 cores
- Memory: ~500 MB
- Disk I/O: ~50 IOPS
- Network: ~5 MB/s (varies with log volume)

**Storage Requirements:**
```
Daily Storage = (Log Volume GB/day) × (Retention Days) × 1.2 (overhead)
```

---

## 🛠️ Installation Methods

### 📦 Method 1: Docker Compose (Recommended)

**Step 1: Clone Repository**
```bash
git clone https://github.com/ssw/log_capturer_go.git
cd log_capturer_go
```

**Step 2: Configure Environment**
```bash
# Copy example configuration
cp configs/config.yaml.example configs/config.yaml

# Set environment variables
export SSW_CONFIG_FILE=/app/configs/config.yaml
export SSW_LOG_LEVEL=info
export SSW_ENVIRONMENT=production
```

**Step 3: Deploy with Docker Compose**
```bash
# Basic deployment
docker-compose up -d

# With full observability stack
docker-compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# Enterprise deployment with security
docker-compose -f docker-compose.yml -f docker-compose.enterprise.yml up -d
```

**Step 4: Verify Deployment**
```bash
# Check health
curl http://localhost:8401/health

# View logs
docker-compose logs -f log-capturer

# Check metrics
curl http://localhost:8001/metrics
```

### 🐳 Method 2: Docker

**Build Image**
```bash
# Build from source
docker build -t ssw-logs-capture:latest .

# Or use pre-built image
docker pull sswlabs/logs-capture:latest
```

**Run Container**
```bash
docker run -d \
  --name log-capturer \
  -p 8401:8401 \
  -p 8001:8001 \
  -v /var/log:/var/log:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v ./configs:/app/configs \
  -v ./data:/app/data \
  --restart unless-stopped \
  ssw-logs-capture:latest
```

### ⚙️ Method 3: Binary Installation

**Download Binary**
```bash
# Download latest release
wget https://github.com/ssw/log_capturer_go/releases/latest/download/log_capturer_linux_amd64.tar.gz

# Extract
tar -xzf log_capturer_linux_amd64.tar.gz
sudo mv log_capturer /usr/local/bin/

# Create systemd service
sudo cp scripts/log-capturer.service /etc/systemd/system/
sudo systemctl enable log-capturer
sudo systemctl start log-capturer
```

### ☸️ Method 4: Kubernetes

**Using Helm Chart**
```bash
# Add Helm repository
helm repo add ssw-logs https://charts.ssw.com
helm repo update

# Install with default values
helm install log-capturer ssw-logs/log-capturer

# Install with custom values
helm install log-capturer ssw-logs/log-capturer -f values-production.yaml
```

**Using kubectl**
```bash
# Apply manifests
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
```

---

## ⚙️ Configuration

### 📁 Configuration File Structure

```yaml
# /app/configs/config.yaml
app:
  name: "ssw-logs-capture"
  version: "v2.0.0"
  environment: "production"        # development, staging, production
  log_level: "info"               # trace, debug, info, warn, error
  log_format: "json"              # json, text
  operation_timeout: "1h"

server:
  enabled: true
  port: 8401
  host: "0.0.0.0"
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "60s"

metrics:
  enabled: true
  port: 8001
  host: "0.0.0.0"
  path: "/metrics"

# ... (see configs/enterprise-config.yaml for full example)
```

### 🔑 Environment Variables

```bash
# Core configuration
SSW_CONFIG_FILE=/app/configs/config.yaml
SSW_LOG_LEVEL=info
SSW_ENVIRONMENT=production

# Security
SSW_JWT_SECRET=your-jwt-secret-key
SSW_ENCRYPTION_KEY=your-encryption-key

# External services
SSW_LOKI_URL=http://loki:3100
SSW_PROMETHEUS_URL=http://prometheus:9090
SSW_JAEGER_ENDPOINT=http://jaeger:14268/api/traces

# Performance
SSW_WORKER_COUNT=8
SSW_QUEUE_SIZE=100000
SSW_BATCH_SIZE=500
```

### 📋 Configuration Validation

```bash
# Validate configuration before deployment
./log_capturer --config configs/config.yaml --validate

# Check configuration syntax
yamllint configs/config.yaml

# Test configuration with dry-run
./log_capturer --config configs/config.yaml --dry-run
```

---

## 🏭 Production Deployment

### 🔄 High Availability Setup

**Load Balancer Configuration**
```nginx
# nginx.conf
upstream log_capturer {
    server log-capturer-1:8401 max_fails=2 fail_timeout=30s;
    server log-capturer-2:8401 max_fails=2 fail_timeout=30s;
    server log-capturer-3:8401 max_fails=2 fail_timeout=30s;
}

server {
    listen 80;
    server_name logs.company.com;

    location / {
        proxy_pass http://log_capturer;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_connect_timeout 30s;
        proxy_send_timeout 30s;
        proxy_read_timeout 30s;
    }

    location /health {
        proxy_pass http://log_capturer;
        access_log off;
    }
}
```

**Docker Compose HA**
```yaml
# docker-compose.ha.yml
version: '3.8'
services:
  log-capturer-1:
    image: ssw-logs-capture:latest
    environment:
      - SSW_NODE_ID=node-1
    volumes:
      - ./configs:/app/configs
      - ./data/node1:/app/data
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.role == worker

  log-capturer-2:
    image: ssw-logs-capture:latest
    environment:
      - SSW_NODE_ID=node-2
    volumes:
      - ./configs:/app/configs
      - ./data/node2:/app/data
    deploy:
      replicas: 1
      placement:
        constraints:
          - node.labels.role == worker

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
    depends_on:
      - log-capturer-1
      - log-capturer-2
```

### 🗄️ Database Setup

**Persistent Storage Configuration**
```yaml
# docker-compose.yml - Storage volumes
volumes:
  positions_data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/log-capturer/positions

  dlq_data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/log-capturer/dlq

  buffer_data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /data/log-capturer/buffer
```

**Backup Strategy**
```bash
#!/bin/bash
# backup.sh - Daily backup script

BACKUP_DIR="/backup/log-capturer/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# Backup positions
cp -r /data/log-capturer/positions "$BACKUP_DIR/"

# Backup configuration
cp -r /app/configs "$BACKUP_DIR/"

# Backup DLQ (if needed)
cp -r /data/log-capturer/dlq "$BACKUP_DIR/"

# Compress and upload
tar -czf "$BACKUP_DIR.tar.gz" -C "$BACKUP_DIR" .
aws s3 cp "$BACKUP_DIR.tar.gz" s3://backups/log-capturer/

# Cleanup old backups (keep 30 days)
find /backup/log-capturer -name "*.tar.gz" -mtime +30 -delete
```

### 🔒 Security Hardening

**File Permissions**
```bash
# Set secure permissions
sudo chown -R logcap:logcap /app
sudo chmod 755 /app
sudo chmod 644 /app/configs/config.yaml
sudo chmod 600 /app/configs/secrets.yaml
sudo chmod 755 /app/data
sudo chmod 755 /app/logs
```

**Network Security**
```bash
# Firewall rules (UFW)
sudo ufw allow 22/tcp      # SSH
sudo ufw allow 8401/tcp    # API (from load balancer only)
sudo ufw allow 8001/tcp    # Metrics (from monitoring only)
sudo ufw deny 9090/tcp     # Block direct Prometheus access
sudo ufw enable

# Or using iptables
iptables -A INPUT -p tcp --dport 8401 -s 10.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8401 -j DROP
```

---

## 🛡️ Security Configuration

### 🔐 Authentication Setup

**Basic Authentication**
```yaml
security:
  enabled: true
  authentication:
    enabled: true
    method: "basic"
    session_timeout: "24h"
    max_attempts: 3
    lockout_time: "15m"
    users:
      admin:
        username: "admin"
        password_hash: "bcrypt-hash-here"  # Use bcrypt, not SHA256
        roles: ["admin"]
        enabled: true
      operator:
        username: "operator"
        password_hash: "bcrypt-hash-here"
        roles: ["operator"]
        enabled: true
```

**Token Authentication**
```yaml
security:
  authentication:
    method: "token"
    tokens:
      "api-token-production": "admin"
      "monitoring-token": "operator"
      "readonly-token": "viewer"
```

**JWT Authentication**
```yaml
security:
  authentication:
    method: "jwt"
    jwt_secret: "${JWT_SECRET}"  # From environment
    jwt_expiry: "1h"
    jwt_refresh_expiry: "24h"
```

### 🛂 Authorization (RBAC)

```yaml
security:
  authorization:
    enabled: true
    default_role: "viewer"
    roles:
      admin:
        name: "admin"
        permissions:
          - resource: "*"
            action: "*"
      operator:
        name: "operator"
        permissions:
          - resource: "health"
            action: "read"
          - resource: "metrics"
            action: "read"
          - resource: "stats"
            action: "read"
          - resource: "config"
            action: "read"
          - resource: "dlq"
            action: "reprocess"
      viewer:
        name: "viewer"
        permissions:
          - resource: "health"
            action: "read"
          - resource: "metrics"
            action: "read"
```

### 🔍 Audit Logging

```yaml
security:
  audit:
    enabled: true
    log_file: "/app/logs/audit.log"
    log_level: "info"
    include_auth: true
    include_access: true
    include_config: true
    retention_days: 90
```

---

## 📊 Monitoring Setup

### 🎯 Prometheus Configuration

**prometheus.yml**
```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

rule_files:
  - "/etc/prometheus/alerts/*.yml"

scrape_configs:
  - job_name: 'log-capturer'
    static_configs:
      - targets: ['log-capturer:8001']
    metrics_path: '/metrics'
    scrape_interval: 15s
    scrape_timeout: 10s

  - job_name: 'log-capturer-health'
    static_configs:
      - targets: ['log-capturer:8401']
    metrics_path: '/health'
    scrape_interval: 30s

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

### 📈 Grafana Dashboards

**Dashboard Import**
```bash
# Import pre-built dashboards
curl -X POST \
  http://grafana:3000/api/dashboards/db \
  -H 'Content-Type: application/json' \
  -d @dashboards/log-capturer-overview.json

# Or via Grafana UI
# Dashboard ID: 15847 (Log Capturer Overview)
# Dashboard ID: 15848 (Log Capturer Performance)
```

### 🚨 Alerting Rules

**alerts.yml**
```yaml
groups:
  - name: log-capturer-critical
    rules:
      - alert: LogCapturerDown
        expr: up{job="log-capturer"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Log Capturer is down"
          description: "Log Capturer has been down for more than 1 minute"

      - alert: HighQueueUtilization
        expr: log_capturer_queue_utilization > 0.9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High queue utilization"
          description: "Queue utilization is {{ $value }} for 5 minutes"

      - alert: HighErrorRate
        expr: rate(log_capturer_errors_total[5m]) > 10
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
          description: "Error rate is {{ $value }} errors/sec"

      - alert: DiskSpaceLow
        expr: log_capturer_disk_free_bytes / log_capturer_disk_total_bytes < 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low disk space"
          description: "Disk space is {{ $value | humanizePercentage }} full"
```

### 📊 SLO Monitoring

```yaml
slo:
  enabled: true
  prometheus_url: "http://prometheus:9090"
  evaluation_interval: "1m"
  slos:
    - name: "availability"
      description: "Service availability"
      error_budget: 0.01  # 99% availability
      window: "30d"
      slis:
        - name: "uptime"
          query: "up{job='log-capturer'}"
          target: 0.99

    - name: "latency"
      description: "Processing latency"
      error_budget: 0.05  # 95% under threshold
      window: "7d"
      slis:
        - name: "p95_latency"
          query: "histogram_quantile(0.95, processing_duration_seconds_bucket)"
          target: 0.5  # 500ms
```

---

## 🔧 Troubleshooting

### 🚨 Common Issues

**Issue 1: High Memory Usage**
```bash
# Symptoms
- Memory usage > 80%
- OOMKilled containers
- Slow processing

# Diagnosis
kubectl top pods
docker stats
curl http://localhost:8401/stats

# Solutions
- Increase memory limits
- Reduce batch sizes
- Enable disk buffering
- Check for memory leaks
```

**Issue 2: Queue Backlog**
```bash
# Symptoms
- Queue utilization > 90%
- Increasing latency
- DLQ growth

# Diagnosis
curl http://localhost:8401/stats | jq '.dispatcher.queue_size'
curl http://localhost:8001/metrics | grep queue_utilization

# Solutions
- Increase worker count
- Optimize sink performance
- Enable backpressure
- Scale horizontally
```

**Issue 3: Connection Issues**
```bash
# Symptoms
- Docker connection errors
- File access denied
- Network timeouts

# Diagnosis
docker logs log-capturer
tail -f /app/logs/application.log

# Solutions
- Check Docker socket permissions
- Verify file system access
- Configure network policies
- Check firewall rules
```

### 🔍 Debug Commands

```bash
# Health check
curl -s http://localhost:8401/health | jq

# Detailed stats
curl -s http://localhost:8401/stats | jq

# Position status
curl -s http://localhost:8401/positions | jq

# DLQ status
curl -s http://localhost:8401/dlq/stats | jq

# Container logs
docker logs --tail 100 -f log-capturer

# System resources
docker stats log-capturer

# Performance profiling
go tool pprof http://localhost:8401/debug/pprof/profile

# Memory analysis
go tool pprof http://localhost:8401/debug/pprof/heap
```

### 📋 Log Analysis

```bash
# Application logs
tail -f /app/logs/application.log | jq

# Audit logs
tail -f /app/logs/audit.log | jq

# Error patterns
grep -E "(ERROR|FATAL)" /app/logs/application.log | tail -20

# Performance metrics
grep "processing_duration" /app/logs/application.log | tail -10
```

---

## 📈 Scaling Guidelines

### 🔄 Vertical Scaling

**CPU Scaling**
```yaml
# docker-compose.yml
services:
  log-capturer:
    deploy:
      resources:
        limits:
          cpus: '4.0'      # Scale up from 2.0
          memory: 8G       # Scale up from 4G
        reservations:
          cpus: '2.0'
          memory: 4G
```

**Memory Optimization**
```yaml
# config.yaml
dispatcher:
  queue_size: 200000        # Increase from 100000
  worker_count: 16          # Increase from 8

positions:
  max_memory_buffer: 10000  # Increase from 2000

deduplication_config:
  max_cache_size: 500000    # Increase from 100000
```

### ↔️ Horizontal Scaling

**Docker Swarm**
```yaml
# docker-compose.swarm.yml
version: '3.8'
services:
  log-capturer:
    image: ssw-logs-capture:latest
    deploy:
      replicas: 3
      placement:
        constraints:
          - node.role == worker
      resources:
        limits:
          cpus: '2'
          memory: 4G
```

**Kubernetes HPA**
```yaml
# hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: log-capturer-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: log-capturer
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
    - type: Pods
      pods:
        metric:
          name: queue_utilization
        target:
          type: AverageValue
          averageValue: "0.7"
```

### 🎯 Performance Tuning

**High-Volume Configuration**
```yaml
# High-throughput optimizations
app:
  log_level: "warn"  # Reduce logging overhead

dispatcher:
  worker_count: 32
  queue_size: 1000000
  batch_size: 1000
  batch_timeout: "2s"

file_monitor_service:
  read_buffer_size: 262144    # 256KB
  poll_interval: "5s"

container_monitor:
  max_concurrent: 100

sinks:
  loki:
    batch_size: 2000
    batch_timeout: "1s"
    queue_size: 200000
```

---

## 🔧 Maintenance

### 🔄 Updates and Upgrades

**Rolling Update Process**
```bash
# 1. Backup current state
./scripts/backup.sh

# 2. Update one instance at a time
docker-compose up -d --no-deps log-capturer-1

# 3. Verify health
curl http://log-capturer-1:8401/health

# 4. Continue with other instances
docker-compose up -d --no-deps log-capturer-2
docker-compose up -d --no-deps log-capturer-3

# 5. Verify cluster health
curl http://load-balancer/health
```

**Zero-Downtime Deployment**
```bash
# Using Blue-Green deployment
# 1. Deploy to green environment
docker-compose -f docker-compose.green.yml up -d

# 2. Run smoke tests
./scripts/smoke-test.sh green

# 3. Switch traffic
./scripts/switch-traffic.sh blue green

# 4. Monitor for issues
./scripts/monitor-deployment.sh

# 5. Cleanup old environment
docker-compose -f docker-compose.blue.yml down
```

### 🧹 Cleanup Tasks

**Daily Maintenance**
```bash
#!/bin/bash
# daily-maintenance.sh

# Cleanup old logs
find /app/logs -name "*.log" -mtime +7 -delete

# Cleanup old positions
curl -X POST http://localhost:8401/positions/cleanup

# Reprocess DLQ if needed
if [ $(curl -s http://localhost:8401/dlq/stats | jq '.total_entries') -gt 1000 ]; then
  curl -X POST http://localhost:8401/dlq/reprocess -d '{"max_entries": 1000}'
fi

# Health check
if ! curl -f http://localhost:8401/health; then
  echo "Health check failed" | mail -s "Log Capturer Alert" admin@company.com
fi
```

**Weekly Maintenance**
```bash
#!/bin/bash
# weekly-maintenance.sh

# Update metrics retention
docker exec prometheus /bin/prometheus \
  --config.file=/etc/prometheus/prometheus.yml \
  --storage.tsdb.retention.time=30d \
  --web.console.libraries=/etc/prometheus/console_libraries \
  --web.console.templates=/etc/prometheus/consoles

# Optimize disk usage
docker system prune -f

# Check for security updates
apt list --upgradable | grep -i security

# Generate weekly report
./scripts/generate-report.sh weekly
```

### 📊 Monitoring and Alerting

**Health Monitoring**
```bash
# Automated health check script
#!/bin/bash
# health-monitor.sh

ENDPOINTS=(
  "http://localhost:8401/health"
  "http://localhost:8401/stats"
  "http://localhost:8001/metrics"
)

for endpoint in "${ENDPOINTS[@]}"; do
  if ! curl -f -s "$endpoint" > /dev/null; then
    echo "ALERT: $endpoint is not responding" | \
      mail -s "Log Capturer Health Alert" monitoring@company.com
  fi
done
```

**Performance Monitoring**
```bash
# performance-check.sh
#!/bin/bash

# Check queue utilization
QUEUE_UTIL=$(curl -s http://localhost:8401/stats | jq '.dispatcher.queue_utilization')
if (( $(echo "$QUEUE_UTIL > 0.8" | bc -l) )); then
  echo "WARNING: Queue utilization is $QUEUE_UTIL"
fi

# Check error rate
ERROR_RATE=$(curl -s http://localhost:8001/metrics | grep error_total | awk '{sum+=$2} END {print sum}')
if [ "$ERROR_RATE" -gt 100 ]; then
  echo "WARNING: Error rate is $ERROR_RATE"
fi

# Check disk space
DISK_FREE=$(df /app/data | awk 'NR==2{print $4/$2*100}')
if (( $(echo "$DISK_FREE < 20" | bc -l) )); then
  echo "WARNING: Disk space is low: $DISK_FREE% free"
fi
```

---

## 🎯 Best Practices

### 🔒 Security Best Practices

1. **Always use HTTPS in production**
2. **Implement proper RBAC**
3. **Regular security audits**
4. **Keep dependencies updated**
5. **Use secrets management**
6. **Enable audit logging**
7. **Network segmentation**
8. **Regular backups**

### ⚡ Performance Best Practices

1. **Monitor key metrics continuously**
2. **Use appropriate batch sizes**
3. **Implement backpressure handling**
4. **Regular performance testing**
5. **Capacity planning**
6. **Optimize configuration per environment**
7. **Use SSD storage for positions**
8. **Implement proper indexing**

### 🔧 Operational Best Practices

1. **Automated deployment pipelines**
2. **Infrastructure as Code**
3. **Comprehensive monitoring**
4. **Disaster recovery planning**
5. **Regular maintenance windows**
6. **Change management process**
7. **Documentation updates**
8. **Team training**

---

## 📞 Support and Resources

### 🆘 Getting Help

- **Documentation**: `docs/`
- **API Reference**: `api/swagger.yaml`
- **Issues**: GitHub Issues
- **Discussions**: GitHub Discussions
- **Email**: support@ssw.com

### 📚 Additional Resources

- **Architecture Guide**: `docs/ARCHITECTURE.md`
- **Configuration Reference**: `docs/CONFIGURATION.md`
- **Troubleshooting Guide**: `docs/TROUBLESHOOTING.md`
- **Performance Tuning**: `docs/PERFORMANCE.md`
- **Security Guide**: `docs/SECURITY.md`

---

*This deployment guide is maintained by the SSW Logs Capture team. Last updated: 2024-01-15*