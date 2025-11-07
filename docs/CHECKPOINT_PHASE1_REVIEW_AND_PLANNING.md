# Phase 1 Checkpoint: Review and Planning

**Date**: 2025-11-06
**Phase**: Review and Planning
**Status**: IN PROGRESS
**Coordinator**: workflow-coordinator agent
**Duration**: 15 minutes

---

## Executive Summary

This checkpoint documents the completion of the investigation review phase and the creation of a comprehensive implementation plan for fixing the goroutine leak in Container Monitor.

### Investigation Summary

The investigation definitively identified Container Monitor as the sole source of a 32 goroutines/min leak:

- **Root Cause**: Persistent HTTP streaming connections created by `ContainerLogs(Follow: true)` over Docker Unix socket
- **Impact**: Each monitored container spawns 2-3 persistent goroutines that never terminate
- **Verification**: Isolation test confirmed 0 goroutines/min leak when Container Monitor is disabled
- **Current State**: Container Monitor is DISABLED for stability

### Key Findings from Investigation

1. **Leak Mechanism**:
   - Line 702: `Follow: true` creates never-closing streams
   - Lines 689, 806, 793, 682: Goroutines accumulate from stream read operations
   - No stream rotation or lifecycle management
   - No explicit connection closing

2. **Why Standard Fixes Failed**:
   - `MaxConnsPerHost`: Only applies to TCP, not Unix sockets
   - `DisableKeepAlives`: Only prevents idle connection reuse, not active streams
   - HTTP Transport timeouts: Streaming connections designed to be long-lived

3. **Recommended Solution**: Connection Rotation (Hybrid Approach)
   - Rotate streams every 5 minutes
   - Limit concurrent streams to 50
   - Add proper context cancellation
   - Implement stream monitoring metrics

---

## Current Code Analysis

### File: `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`

**Size**: 1,144 lines
**Key Components**:

1. **ContainerMonitor struct** (lines 28-44):
   ```go
   type ContainerMonitor struct {
       dockerPool    *docker.PoolManager
       containers    map[string]*monitoredContainer
       mutex         sync.RWMutex
       ctx           context.Context
       cancel        context.CancelFunc
   }
   ```

2. **monitoredContainer struct** (lines 47-57):
   ```go
   type monitoredContainer struct {
       id           string
       stream       io.ReadCloser
       lastRead     time.Time
       cancel       context.CancelFunc
       heartbeatWg  sync.WaitGroup
   }
   ```
   - Already has `cancel` function for context
   - Already tracks `stream` for closing
   - Has `heartbeatWg` for goroutine tracking

3. **monitorContainer function** (lines 670-776):
   - Creates persistent stream at line 713
   - Reads logs continuously in infinite loop
   - **MISSING**: Stream rotation mechanism
   - **MISSING**: Stream lifetime limits

4. **readContainerLogs function** (lines 779-950):
   - Lines 792-815: Creates goroutine for reading stream
   - **ISSUE**: No rotation mechanism
   - **POSITIVE**: Already uses WaitGroup for tracking (line 792)

### Existing Goroutine Management

**Good practices already in place**:
- Uses `sync.WaitGroup` for goroutine tracking (lines 56, 676, 792)
- Uses `context.Context` for cancellation (lines 671-677)
- Proper cleanup in defer statements (line 673-677)
- Heartbeat goroutine properly tracked (lines 681-696)

**What needs to be added**:
- Stream rotation timer
- Stream age tracking
- Stream pool with limits
- Metrics for stream lifecycle

---

## Implementation Plan

### Architecture Decision: Hybrid Approach

We will implement a defense-in-depth strategy combining:

1. **Connection Rotation** (Primary fix)
2. **Stream Pooling with Limits** (Resource protection)
3. **Enhanced Context Cancellation** (Clean shutdown)
4. **Comprehensive Metrics** (Observability)

### Detailed Design

#### 1. Connection Rotation Mechanism

**Goal**: Ensure no stream lives longer than 5 minutes

**Implementation**:
```go
type streamRotation struct {
    rotationInterval time.Duration  // 5 minutes
    streamCreatedAt  time.Time
    rotationTimer    *time.Timer
}

func (cm *ContainerMonitor) monitorContainerWithRotation(ctx context.Context, mc *monitoredContainer) error {
    rotationInterval := 5 * time.Minute

    for {
        select {
        case <-ctx.Done():
            return nil
        default:
            // Create stream with rotation timeout
            streamCtx, streamCancel := context.WithTimeout(ctx, rotationInterval)
            defer streamCancel()

            stream, err := cm.openLogStream(streamCtx, mc)
            if err != nil {
                time.Sleep(5 * time.Second)
                continue
            }

            // Read logs until rotation time or error
            cm.readLogsWithRotation(streamCtx, mc, stream)

            // Clean up stream
            stream.Close()
            streamCancel()

            // Brief pause before next rotation
            time.Sleep(100 * time.Millisecond)
        }
    }
}
```

**Key points**:
- Use `context.WithTimeout` to force stream closure after 5 minutes
- Explicitly close stream after timeout
- Brief pause between rotations to prevent tight loops
- Preserve `mc.lastRead` across rotations for position tracking

#### 2. Stream Pool with Limits

**Goal**: Never exceed 50 concurrent streams system-wide

**Implementation**:
```go
type StreamPool struct {
    maxStreams      int                           // 50
    activeStreams   map[string]*streamInfo
    streamSemaphore chan struct{}
    mu              sync.RWMutex
}

type streamInfo struct {
    containerID string
    createdAt   time.Time
    lastActive  time.Time
}

func (sp *StreamPool) AcquireSlot(containerID string) error {
    select {
    case sp.streamSemaphore <- struct{}{}:
        sp.mu.Lock()
        sp.activeStreams[containerID] = &streamInfo{
            containerID: containerID,
            createdAt:   time.Now(),
            lastActive:  time.Now(),
        }
        sp.mu.Unlock()
        return nil
    default:
        return fmt.Errorf("stream pool at capacity")
    }
}

func (sp *StreamPool) ReleaseSlot(containerID string) {
    sp.mu.Lock()
    delete(sp.activeStreams, containerID)
    sp.mu.Unlock()
    <-sp.streamSemaphore
}
```

**Integration**:
- Initialize in `NewContainerMonitor` with max 50 slots
- Acquire slot before opening stream
- Release slot in defer after closing stream
- Reject new containers if at capacity

#### 3. Enhanced Metrics

**New Prometheus metrics**:
```go
var (
    activeContainerStreams = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "log_capturer_container_streams_active",
        Help: "Number of active container log streams",
    })

    streamRotationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "log_capturer_container_stream_rotations_total",
        Help: "Total number of stream rotations",
    }, []string{"container_id", "container_name"})

    streamAgeSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name: "log_capturer_container_stream_age_seconds",
        Help: "Age of container streams when rotated",
        Buckets: []float64{60, 120, 180, 240, 300, 360},
    }, []string{"container_id"})

    streamErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "log_capturer_container_stream_errors_total",
        Help: "Total stream errors by type",
    }, []string{"error_type"})
)
```

#### 4. Modified Functions

**Changes to existing functions**:

1. **`monitoredContainer` struct** - Add fields:
   ```go
   type monitoredContainer struct {
       id              string
       name            string
       image           string
       labels          map[string]string
       since           time.Time
       stream          io.ReadCloser
       lastRead        time.Time
       cancel          context.CancelFunc
       heartbeatWg     sync.WaitGroup
       // NEW FIELDS
       streamCreatedAt time.Time
       rotationCount   int
   }
   ```

2. **`monitorContainer` function** - Complete rewrite with rotation logic

3. **`readContainerLogs` function** - Add rotation awareness:
   - Check for context timeout (rotation trigger)
   - Update metrics on rotation
   - Preserve position across rotations

4. **`NewContainerMonitor` function** - Initialize stream pool

### Implementation Steps

#### Step 1: Add Stream Pool (30 min)
- Create `StreamPool` type
- Add to `ContainerMonitor` struct
- Initialize in constructor
- Add acquire/release logic

#### Step 2: Add Metrics (15 min)
- Define new Prometheus metrics
- Register in metrics package
- Add metric updates in rotation logic

#### Step 3: Implement Rotation Logic (45 min)
- Modify `monitoredContainer` struct
- Rewrite `monitorContainer` with rotation
- Update `readContainerLogs` for rotation awareness
- Add proper cleanup

#### Step 4: Testing (30 min)
- Unit tests for rotation timing
- Unit tests for stream pool
- Integration test with multiple containers
- Goroutine growth test

#### Step 5: Validation (10 min)
- Re-enable Container Monitor
- Monitor for 10 minutes
- Verify goroutine stability

**Total estimated time**: 2 hours 10 minutes

---

## Files to Modify

### 1. `/home/mateus/log_capturer_go/internal/monitors/container_monitor.go`
**Changes**:
- Add `StreamPool` type (new, ~100 lines)
- Modify `monitoredContainer` struct (+2 fields)
- Rewrite `monitorContainer` function (~150 lines)
- Update `readContainerLogs` function (~50 lines modified)
- Update `NewContainerMonitor` (+10 lines)

**Total**: ~300 lines added/modified

### 2. `/home/mateus/log_capturer_go/internal/metrics/metrics.go`
**Changes**:
- Add 4 new Prometheus metrics
- Add helper functions for stream metrics
- Register new metrics

**Total**: ~80 lines added

### 3. `/home/mateus/log_capturer_go/internal/monitors/container_monitor_rotation_test.go` (NEW)
**Purpose**: Unit tests for rotation logic
**Content**:
- Test rotation timing
- Test stream pool capacity
- Test metrics updates
- Test concurrent rotations

**Total**: ~300 lines new

### 4. `/home/mateus/log_capturer_go/provisioning/dashboards/container_monitor_streams.json` (NEW)
**Purpose**: Grafana dashboard for stream monitoring
**Content**:
- Active streams gauge
- Rotation rate graph
- Stream age histogram
- Error rate graph

**Total**: ~500 lines JSON

### 5. `/home/mateus/log_capturer_go/configs/config.yaml`
**Changes**:
- Re-enable Container Monitor: `enabled: true`
- Add rotation config (optional):
  ```yaml
  docker:
    rotation_interval: "5m"
    max_streams: 50
  ```

**Total**: 2 lines changed, 2-3 lines added

---

## Acceptance Criteria

### Critical Success Metrics

1. **Goroutine Stability**:
   - Baseline: < 100 goroutines
   - Growth rate: < 2 goroutines/min over 10 minutes
   - No continuous growth trend

2. **Stream Rotation**:
   - Streams rotate every 5 minutes (±10 seconds)
   - No logs lost during rotation
   - Position preserved across rotations

3. **Resource Limits**:
   - Never exceed 50 concurrent streams
   - Graceful degradation when limit reached

4. **Functionality**:
   - Container logs captured successfully
   - All monitored containers reporting logs
   - No errors in application logs

5. **Metrics**:
   - `log_capturer_container_streams_active` showing accurate count
   - `log_capturer_container_stream_rotations_total` incrementing
   - `log_capturer_goroutines` stable

### Test Plan

#### Phase 1: Unit Tests (30 min)
```bash
# Test rotation logic
go test -v -run TestStreamRotation ./internal/monitors

# Test stream pool
go test -v -run TestStreamPool ./internal/monitors

# Test with race detector
go test -race -v ./internal/monitors
```

#### Phase 2: Integration Test (10 min)
```bash
# Start system with Container Monitor enabled
docker-compose up -d --build log_capturer_go

# Monitor goroutines for 10 minutes
for i in {1..10}; do
    echo "Minute $i:"
    curl -s http://localhost:8001/metrics | grep log_capturer_goroutines
    curl -s http://localhost:8001/metrics | grep log_capturer_container_streams_active
    sleep 60
done
```

#### Phase 3: Validation (10 min)
```bash
# Check container logs are being captured
curl -s "http://localhost:3100/loki/api/v1/query?query={source=\"docker\"}&limit=100"

# Check rotation metrics
curl -s http://localhost:8001/metrics | grep stream_rotations_total

# Check for errors
docker logs log_capturer_go --tail 100 | grep -i error
```

---

## Risk Assessment

### Risks and Mitigations

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Logs lost during rotation | Medium | High | Preserve `lastRead` timestamp, use Docker's `Since` parameter |
| Rotation too aggressive | Low | Medium | 5-minute interval is conservative, can tune if needed |
| Stream pool too restrictive | Low | Medium | 50 streams should handle typical workloads, alert if limit hit |
| Race conditions in pool | Medium | High | Use mutex for all pool operations, test with race detector |
| Performance impact | Low | Low | Rotation overhead is minimal (once per 5 min per container) |

### Rollback Plan

If the fix causes issues:

1. **Immediate**: Disable Container Monitor again
   ```bash
   # Edit config.yaml
   docker:
     enabled: false

   # Restart
   docker-compose restart log_capturer_go
   ```

2. **Revert code changes**:
   ```bash
   git checkout main internal/monitors/container_monitor.go
   git checkout main internal/metrics/metrics.go
   make build
   docker-compose up -d --build log_capturer_go
   ```

3. **Verify stability**:
   ```bash
   # Monitor for 5 minutes
   watch -n 10 'curl -s http://localhost:8001/metrics | grep log_capturer_goroutines'
   ```

---

## Next Steps

### Immediate Actions (Next 2 hours)

1. **Create Phase 2 Checkpoint** - Document before starting implementation
2. **Implement Stream Pool** - Start with pool management
3. **Add Metrics** - Define and register new metrics
4. **Implement Rotation** - Core rotation logic
5. **Create Tests** - Comprehensive test coverage
6. **Create Phase 3 Checkpoint** - Document implementation completion

### Delegation Strategy

- **golang agent**: Implement core rotation logic and stream pool
- **go-bugfixer agent**: Review code for race conditions and leaks
- **continuous-tester agent**: Run tests and validation
- **observability agent**: Implement metrics
- **grafana-specialist agent**: Create dashboard

### Communication

All agents will be coordinated through this checkpoint system. Each phase will have:
- Clear entry criteria
- Detailed task list
- Exit criteria with validation
- Checkpoint document for resumability

---

## How to Resume from This Checkpoint

If work is interrupted, resume by:

1. **Read this document** to understand the plan
2. **Check Todo List** to see current task status
3. **Review git status** to see what's been modified
4. **Continue with next pending task** from Implementation Steps
5. **Update checkpoint** when phase completes

---

## Conclusion

Phase 1 is complete. We have:

- Thoroughly reviewed the investigation findings
- Analyzed the current code structure
- Designed a comprehensive solution (connection rotation + stream pooling)
- Created a detailed implementation plan
- Identified all files to modify
- Defined clear acceptance criteria
- Assessed risks and created mitigation plans

**Phase 1 Status**: COMPLETE ✓

**Next Phase**: Phase 2 - Implementation

**Estimated Time to Complete Fix**: 2 hours

---

**Created**: 2025-11-06
**Last Updated**: 2025-11-06
**Status**: PHASE 1 COMPLETE - READY FOR PHASE 2
