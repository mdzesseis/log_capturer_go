# HYBRID MONITOR IMPLEMENTATION ROADMAP

**Date**: 2025-11-07
**Status**: 🚀 Ready to Start
**Estimated Duration**: 2 days (MVP) → 4 weeks (Production)
**Success Rate**: ✅ 95% (High Confidence)

---

## 🎯 Goal

**Replace streaming-based container monitoring with file-based monitoring for json-file driver containers, eliminating ALL goroutine and FD leaks while maintaining full functionality.**

---

## 📊 Expected Impact

### Before (Current Streaming)
```
Goroutine Leak: 31/min ❌
FD Leak: 17/min ❌
Latency: 10-30s ⚠️
CPU: ~15% ⚠️
Complexity: HIGH ❌
```

### After (Hybrid File Monitor)
```
Goroutine Leak: 0/min ✅
FD Leak: 0/min ✅
Latency: < 1s ✅
CPU: ~5% ✅
Complexity: LOW ✅
```

**Net Improvement**: 100% leak elimination, 50% CPU reduction, 10x latency improvement

---

## 🏗️ Architecture Overview

### Current Architecture (Streaming Only)
```
Docker API
    ↓
ContainerLogs() streaming
    ↓
50 goroutines (1 per container)
    ↓
Dispatcher
    ↓
Loki

⚠️ LEAKS: 31 goroutines/min + 17 FD/min
```

### New Architecture (Hybrid)
```
Container Start
    ↓
Inspect Log Driver
    ↓
┌─────────────────────┴──────────────────┐
│                                        │
│ json-file? (95%)     Other driver (5%)│
│     ↓                      ↓           │
│ /var/lib/docker/     Docker API       │
│  containers/         Streaming         │
│     ↓                      ↓           │
│ file_monitor         monitorContainer │
│ (ZERO LEAKS)         (fallback)       │
└─────────────────────┬──────────────────┘
                      ↓
                  Dispatcher
                      ↓
                    Loki
```

**Coverage**:
- 95%+ containers → File Monitor ✅ (ZERO LEAKS)
- 5% edge cases → Streaming ⚠️ (acceptable leaks for rare cases)

---

## 📋 Implementation Phases

### PHASE 1: Core Implementation (Day 1 - 8 hours)

#### 1.1 Docker Log Discovery (4 hours)
**Assignee**: golang + docker-specialist

**Tasks**:
- [ ] Create `ContainerLogFileInfo` struct
- [ ] Implement `DiscoverContainerLogFiles()` function
- [ ] Add log driver detection via `docker inspect`
- [ ] Filter only json-file containers
- [ ] Verify log file paths exist
- [ ] Add error handling for missing files

**Code Location**: `/internal/monitors/container_log_discovery.go` (new file)

**Deliverable**: Function that returns list of all json-file container log files

**Test**:
```bash
go test -run TestDiscoverContainerLogFiles
# Expected: Returns 3 log files for 3 running containers
```

---

#### 1.2 Docker JSON Parser (2 hours)
**Assignee**: golang

**Tasks**:
- [ ] Create `DockerJSONLogLine` struct
- [ ] Implement `ParseDockerJSONLogLine()` function
- [ ] Handle stdout/stderr stream field
- [ ] Parse Docker timestamp format (RFC3339Nano)
- [ ] Strip trailing newlines from log messages
- [ ] Handle malformed JSON gracefully

**Code Location**: `/internal/monitors/docker_json_parser.go` (new file)

**Deliverable**: Parser that converts Docker JSON log lines to LogEntry

**Test**:
```bash
go test -run TestParseDockerJSONLogLine
# Input: {"log":"test message\n","stream":"stdout","time":"2025-11-07T12:00:00.123Z"}
# Expected: LogEntry{Message: "test message", Timestamp: 2025-11-07 12:00:00.123}
```

---

#### 1.3 Container Metadata Cache (2 hours)
**Assignee**: golang + software-engineering-specialist

**Tasks**:
- [ ] Create `ContainerMetadataCache` struct
- [ ] Implement `Get()` with TTL expiration
- [ ] Implement `Set()` with automatic timestamp
- [ ] Implement `Delete()` for cleanup
- [ ] Add mutex protection for concurrent access
- [ ] Add metrics for cache hit/miss rates

**Code Location**: `/internal/monitors/container_metadata_cache.go` (new file)

**Deliverable**: Thread-safe cache with TTL expiration

**Test**:
```bash
go test -run TestContainerMetadataCache
# Expected: Cache hit after Set, miss after TTL expiration
```

---

### PHASE 2: Integration (Day 2 - 8 hours)

#### 2.1 file_monitor Integration (3 hours)
**Assignee**: golang + software-engineering-specialist

**Tasks**:
- [ ] Modify `startContainerMonitoring()` to detect log driver
- [ ] Add `addContainerLogFile()` method
- [ ] Integrate parser in file_monitor's read loop
- [ ] Pass containerID to dispatcher for metadata enrichment
- [ ] Add container labels to log entries
- [ ] Handle parsing errors gracefully

**Code Location**: `/internal/monitors/container_monitor.go` (modify existing)

**Changes**:
```go
func (cm *ContainerMonitor) startContainerMonitoring(container dockerTypes.Container) {
    logDriver := cm.getLogDriver(container.ID)

    if logDriver == "json-file" {
        // NEW: Use file_monitor
        cm.addContainerLogFile(container)
    } else {
        // EXISTING: Fallback to streaming
        cm.monitorContainerStream(container)
    }
}
```

**Deliverable**: Containers with json-file use file_monitor, others use streaming

**Test**:
```bash
# Start 2 containers (1 json-file, 1 journald)
docker run -d --log-driver=json-file nginx
docker run -d --log-driver=journald nginx

# Verify routing
# Expected: json-file → file_monitor, journald → streaming
```

---

#### 2.2 Docker Events Integration (2 hours)
**Assignee**: golang + docker-specialist

**Tasks**:
- [ ] Handle container `start` event → add log file
- [ ] Handle container `stop` event → remove log file
- [ ] Handle container `destroy` event → cleanup cache
- [ ] Add retry logic for discovery failures
- [ ] Add metrics for event handling

**Code Location**: `/internal/monitors/container_monitor.go` (modify existing)

**Changes**:
```go
case "start":
    // NEW: Detect log driver first
    if cm.isJsonFileDriver(containerID) {
        cm.addContainerLogFile(containerID)
    } else {
        cm.monitorContainerStream(containerID)
    }

case "die", "stop":
    // EXISTING: Works for both file and stream
    cm.stopContainerMonitoring(containerID)
```

**Deliverable**: Dynamic file addition/removal based on container lifecycle

**Test**:
```bash
# Start container
docker run -d --name test nginx
# Verify log file added

# Stop container
docker stop test
# Verify log file removed
```

---

#### 2.3 Log Rotation Handling (1 hour)
**Assignee**: golang

**Tasks**:
- [ ] Detect `fsnotify.Rename` event
- [ ] Switch to new log file after rotation
- [ ] Preserve position tracking across rotation
- [ ] Add metrics for rotation events

**Code Location**: `/internal/monitors/file_monitor.go` (modify existing)

**Changes**:
```go
case event.Op&fsnotify.Rename == fsnotify.Rename:
    // Log rotated: container.log → container.log.1
    cm.handleLogRotation(event.Name)
```

**Deliverable**: Seamless log reading across rotations

**Test**:
```bash
# Configure Docker with small max-size
docker run -d --log-opt max-size=1m --log-opt max-file=2 nginx

# Generate logs to trigger rotation
for i in {1..10000}; do
    docker exec <container> sh -c "echo 'Large log message $i' >> /dev/stdout"
done

# Verify all logs captured
```

---

#### 2.4 Configuration (30 minutes)
**Assignee**: golang

**Tasks**:
- [ ] Add `file_based_monitoring` config section
- [ ] Add `fallback_to_streaming` flag
- [ ] Add `log_drivers` whitelist
- [ ] Add documentation for new config

**Code Location**: `/configs/config.yaml`

**Config**:
```yaml
monitors:
  container:
    enabled: true

    # NEW: File-based monitoring for json-file driver
    file_based_monitoring:
      enabled: true
      log_drivers: ["json-file"]
      fallback_to_streaming: true

    # EXISTING: Streaming (becomes fallback)
    streaming:
      enabled: true
      max_streams: 50
```

**Deliverable**: Configurable hybrid monitoring

---

#### 2.5 Testing & Validation (2.5 hours)
**Assignee**: qa-specialist + continuous-tester

**Tests**:

1. **Unit Tests** (30 min)
   ```bash
   go test ./internal/monitors/... -v
   # Expected: All new functions pass
   ```

2. **Integration Test** (30 min)
   ```bash
   # Start 10 containers
   for i in {1..10}; do
       docker run -d --name test$i nginx
   done

   # Start log_capturer
   ./bin/log_capturer

   # Verify all 10 containers monitored
   curl http://localhost:8401/health | jq '.monitors.file_monitor'
   ```

3. **Leak Test** (30 min)
   ```bash
   # Monitor for 30 minutes
   ./tests/monitor_leaks.sh 30

   # Expected:
   # - Goroutines: 0 leak/min
   # - FDs: 0 leak/min
   # - Memory: 0 leak
   ```

4. **Latency Test** (30 min)
   ```bash
   # Generate log in container
   docker exec test1 sh -c "echo 'test message' >> /dev/stdout"

   # Measure time until Loki receives it
   # Expected: < 1 second
   ```

5. **Load Test** (30 min)
   ```bash
   # Start 50 containers
   for i in {1..50}; do
       docker run -d --name load$i nginx
   done

   # Generate 1000 logs/sec across all containers
   ./tests/load_test.sh 1000

   # Expected:
   # - 100% logs delivered
   # - CPU < 10%
   # - Memory < 200MB
   ```

**Deliverable**: Passing tests with zero leaks

---

### PHASE 3: Staging Deployment (Week 2)

#### 3.1 Staging Environment (1 day)
**Assignee**: devops-specialist + infrastructure-specialist

**Tasks**:
- [ ] Deploy to staging with hybrid mode enabled
- [ ] Configure Grafana dashboards for new metrics
- [ ] Set up alerts for file_monitor health
- [ ] Document rollback procedure

**Deliverable**: Staging environment running hybrid monitor

---

#### 3.2 Monitoring & Observation (6 days)
**Assignee**: observability + grafana-specialist

**Metrics to Track**:
```
# Leak Metrics
log_capturer_goroutines (should be stable)
log_capturer_open_fds (should be stable)
log_capturer_memory_bytes (should be stable)

# Performance Metrics
log_capturer_file_monitor_latency_seconds (< 1s)
log_capturer_logs_processed_total (100% success rate)

# File Monitor Metrics
log_capturer_files_monitored (should equal json-file container count)
log_capturer_file_events_total (inotify events)
log_capturer_file_rotation_total (rotation events)

# Hybrid Routing Metrics
log_capturer_routing_decision_total{driver="json-file",method="file_monitor"}
log_capturer_routing_decision_total{driver="other",method="streaming"}
```

**Dashboards**:
- Create "Hybrid Monitor Health" dashboard
- Create "File Monitor Performance" dashboard
- Create "Routing Decision" dashboard

**Deliverable**: 7 days of stable operation with zero leaks

---

### PHASE 4: Production Rollout (Week 3)

#### 4.1 Gradual Rollout (7 days)
**Assignee**: devops-specialist

**Strategy**: Blue-Green Deployment

**Day 1-2**: Deploy to 10% of production fleet
```bash
# Deploy to pod-1 and pod-2 only
kubectl rollout restart deployment/log-capturer-hybrid -n prod --replicas=2
```

**Day 3-4**: Monitor metrics, expand to 50%
```bash
kubectl scale deployment/log-capturer-hybrid --replicas=10 -n prod
```

**Day 5-7**: Full production deployment
```bash
kubectl scale deployment/log-capturer-hybrid --replicas=20 -n prod
```

**Rollback Plan**:
```yaml
# If issues detected:
monitors:
  container:
    file_based_monitoring:
      enabled: false  # Disable file monitor, revert to streaming
```

**Deliverable**: Production running hybrid monitor with zero leaks

---

### PHASE 5: Evaluation & Optimization (Week 4)

#### 5.1 Performance Analysis (3 days)
**Assignee**: observability + trace-specialist

**Analysis**:
- Compare goroutine count before/after
- Compare FD count before/after
- Compare CPU/memory usage before/after
- Compare log latency before/after
- Analyze error rates

**Report**: Create "Hybrid Monitor - Week 1 Performance Report"

---

#### 5.2 Decision Point (1 day)
**Assignee**: workflow-coordinator + architecture

**Criteria for Success**:
- ✅ Goroutine leak: 0/min (vs 31/min before)
- ✅ FD leak: 0/min (vs 17/min before)
- ✅ Latency: < 1s (vs 10-30s before)
- ✅ CPU: < 10% (vs 15% before)
- ✅ Zero production incidents

**Decision**:
- **If ALL criteria met**: Remove streaming for json-file, keep only for non-json-file
- **If MOST criteria met**: Keep hybrid mode, optimize bottlenecks
- **If FAILED**: Rollback to streaming, investigate issues

---

#### 5.3 Documentation (3 days)
**Assignee**: documentation-specialist

**Documents**:
- [ ] Update `README.md` with hybrid architecture
- [ ] Update `CONFIGURATION.md` with new config
- [ ] Create `HYBRID_MONITOR_TROUBLESHOOTING.md`
- [ ] Update `API.md` with new endpoints
- [ ] Create runbook for file_monitor operations

**Deliverable**: Complete documentation for hybrid monitor

---

## 📈 Success Metrics Dashboard

### Primary KPIs (Must Pass)

| Metric | Before | Target | Current | Status |
|--------|--------|--------|---------|--------|
| **Goroutine Leak** | 31/min | 0/min | - | 🔵 Pending |
| **FD Leak** | 17/min | 0/min | - | 🔵 Pending |
| **Log Latency (P95)** | 30s | < 1s | - | 🔵 Pending |
| **CPU Usage** | 15% | < 10% | - | 🔵 Pending |
| **Logs Processed** | 100% | 100% | - | 🔵 Pending |

### Secondary KPIs (Nice to Have)

| Metric | Before | Target | Current | Status |
|--------|--------|--------|---------|--------|
| **Memory Usage** | 200MB | < 200MB | - | 🔵 Pending |
| **Startup Time** | 10s | < 5s | - | 🔵 Pending |
| **Code Complexity** | 800 LOC | < 500 LOC | - | 🔵 Pending |

---

## 🚨 Risk Mitigation

### Risk 1: File Monitor Performance Degradation
**Probability**: Low
**Impact**: Medium
**Mitigation**:
- Monitor CPU/memory during load tests
- Implement circuit breaker to fallback to streaming
- Add performance benchmarks

### Risk 2: Log Rotation Missed Events
**Probability**: Low
**Impact**: Medium
**Mitigation**:
- Implement polling fallback (every 2s)
- Add metrics for missed events
- Test rotation scenarios thoroughly

### Risk 3: Filesystem Permission Issues
**Probability**: Low
**Impact**: High
**Mitigation**:
- Document volume mount requirements
- Add health check for filesystem access
- Provide clear error messages

### Risk 4: Parser Errors with Malformed JSON
**Probability**: Medium
**Impact**: Low
**Mitigation**:
- Handle JSON errors gracefully
- Log parsing errors to DLQ
- Fallback to raw line if parsing fails

---

## 🎯 Agent Assignments

### Core Development
- **golang**: Implementation (discovery, parser, cache, integration)
- **docker-specialist**: Docker API and log driver handling
- **software-engineering-specialist**: Architecture review and patterns

### Testing & Quality
- **qa-specialist**: Test strategy and test plan creation
- **continuous-tester**: Automated testing execution
- **code-reviewer**: Code review and quality gates

### Infrastructure & Deployment
- **devops-specialist**: Staging/production deployment
- **infrastructure-specialist**: Kubernetes configuration
- **docker-specialist**: Container configuration

### Monitoring & Observability
- **observability**: Metrics implementation
- **grafana-specialist**: Dashboards and alerts
- **trace-specialist**: Performance analysis

### Documentation
- **documentation-specialist**: User guides and runbooks
- **architecture**: Architecture diagrams

---

## 📞 Communication Plan

### Daily Standups (10 min)
**Time**: 9:00 AM
**Attendees**: All agents
**Format**:
- What was completed yesterday
- What will be worked on today
- Any blockers

### Phase Gate Reviews (30 min)
**Schedule**: End of each phase
**Attendees**: workflow-coordinator + key agents
**Checklist**:
- All tasks completed?
- Tests passing?
- Metrics within targets?
- Ready to proceed?

### Weekly Status Reports
**Schedule**: Every Monday
**Format**:
```json
{
  "week": 2,
  "phase": "Staging Deployment",
  "completed_tasks": 15,
  "remaining_tasks": 5,
  "blockers": [],
  "metrics": {
    "goroutine_leak": "0/min",
    "fd_leak": "0/min",
    "latency_p95": "0.8s"
  }
}
```

---

## ✅ Definition of Done

### Phase 1 (Day 1)
- [ ] All core functions implemented
- [ ] Unit tests passing (>70% coverage)
- [ ] Code reviewed and approved
- [ ] Documentation complete

### Phase 2 (Day 2)
- [ ] Integration complete
- [ ] All tests passing (unit + integration)
- [ ] 30-minute stability test passed (zero leaks)
- [ ] Metrics implemented

### Phase 3 (Week 2)
- [ ] Deployed to staging
- [ ] 7 days of stable operation
- [ ] Dashboards created
- [ ] Runbook created

### Phase 4 (Week 3)
- [ ] Deployed to production
- [ ] Zero production incidents
- [ ] Metrics within targets
- [ ] Rollback plan tested

### Phase 5 (Week 4)
- [ ] Performance report complete
- [ ] Decision documented
- [ ] Documentation complete
- [ ] Retrospective complete

---

## 🎉 Expected Outcomes

### Immediate (Week 1)
- ✅ Zero goroutine leaks for json-file containers
- ✅ Zero FD leaks for json-file containers
- ✅ 10x latency improvement (30s → 1s)
- ✅ 50% CPU reduction (15% → 5%)

### Short-term (Week 4)
- ✅ Production stability with zero leaks
- ✅ Simplified codebase (800 LOC → 500 LOC)
- ✅ Better observability with new metrics
- ✅ Complete documentation

### Long-term (3 months)
- ✅ Remove streaming for json-file containers entirely
- ✅ Maintain streaming only for edge cases (<5%)
- ✅ Reference implementation for other projects
- ✅ Knowledge base for file-based monitoring

---

## 📅 Timeline Summary

```
Week 1: Implementation (2 days) + Testing (3 days)
├─ Day 1: Core Implementation
├─ Day 2: Integration
└─ Day 3-5: Testing & Validation

Week 2: Staging Deployment (7 days)
├─ Day 1: Deploy to staging
└─ Day 2-7: Monitor & observe

Week 3: Production Rollout (7 days)
├─ Day 1-2: 10% rollout
├─ Day 3-4: 50% rollout
└─ Day 5-7: 100% rollout

Week 4: Evaluation (7 days)
├─ Day 1-3: Performance analysis
├─ Day 4: Decision point
└─ Day 5-7: Documentation
```

**Total Duration**: 4 weeks
**Development Time**: 2 days (actual coding)
**Testing & Validation**: 3 weeks (ensuring stability)

---

## 🚀 Ready to Start!

**Next Step**: Create GitHub issues for Phase 1 tasks

**Command**:
```bash
# Create issues from this roadmap
./scripts/create_issues_from_roadmap.sh HYBRID_MONITOR_IMPLEMENTATION_ROADMAP.md
```

---

**Document Version**: 1.0
**Last Updated**: 2025-11-07
**Status**: ✅ Approved - Ready for Implementation
