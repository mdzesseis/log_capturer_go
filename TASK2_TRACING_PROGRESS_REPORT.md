# Task 2: OpenTelemetry Tracing Integration - Progress Report

**Date**: 2025-11-07
**Status**: 70% Complete (API + Config + Dispatcher Integration)
**Estimated Remaining**: 4-6 hours

---

## COMPLETED WORK

### 1. Tracing API Endpoints (✅ 100%)

**File**: `internal/app/handlers.go`

Added three comprehensive API endpoints for tracing control:

#### POST /api/tracing/enable
- Enable on-demand tracing for specific sources
- Parameters: source_id, rate (0.0-1.0), duration
- Validation: rate range, duration limits (1m-24h)
- Response: expiration timestamp
- Full documentation with examples

#### POST /api/tracing/disable
- Disable on-demand tracing for specific sources
- Parameters: source_id
- Response: confirmation

#### GET /api/tracing/status
- Get current tracing status and configuration
- Returns: mode, sampling rates, logs traced, spans created, active rules
- No parameters required

**Routes Registered**: Lines 165-173 in handlers.go
- Conditional registration based on tracing enabled
- On-demand endpoints only in hybrid mode

---

### 2. Dispatcher Integration (✅ 100%)

**File**: `internal/dispatcher/dispatcher.go`

#### Added TracingManager Field
- Line 105: `tracingManager *tracing.EnhancedTracingManager`
- Import added (line 53)

#### Updated Constructor
- Line 223: Added `tracingMgr` parameter
- Line 323: Assigned to struct
- All benchmark files updated (sed replacement)

#### Instrumented Handle() Method
- Lines 584-601: Log-level tracing
- Creates span only if TracingManager decides to sample
- Preserves context through call chain
- Trace IDs automatically added to labels

#### Instrumented processBatch() Method
- Lines 883-896: System-level tracing
- Always traced (except ModeOff)
- Batch metadata added to span
- Context preserved

**File**: `internal/app/initialization.go`
- Line 76: Updated to pass tracingManager to Dispatcher constructor

---

### 3. Configuration (✅ 100%)

**File**: `configs/config.yaml` (Lines 644-807)

Comprehensive tracing configuration with:

#### Mode Selection (4 options)
```yaml
mode: "hybrid"  # off, system-only, hybrid, full-e2e
```

#### Log Tracing Rate
```yaml
log_tracing_rate: 0.0  # 0.0-1.0 (0% = rely on adaptive/on-demand)
```

#### Adaptive Sampling
```yaml
adaptive_sampling:
  enabled: true
  latency_threshold: "1s"      # Trigger when P99 > 1s
  sample_rate: 0.1             # Sample 10% when triggered
  window_size: "5m"
  check_interval: "30s"
```

#### On-Demand Control
```yaml
on_demand:
  enabled: true
  max_rules: 100
  default_duration: "10m"
```

#### Exporter Configuration
```yaml
exporter:
  type: "jaeger"
  endpoint: "http://jaeger:14268/api/traces"
  batch_timeout: "5s"
  max_export_batch_size: 512
  max_queue_size: 2048
```

#### Sampler Configuration
```yaml
sampler:
  type: "parent_based"  # Respect parent trace decisions
```

#### Resource Attributes
```yaml
resource:
  service_name: "log_capturer_go"
  service_version: "v0.2.0"
  environment: "production"
  deployment_id: "${HOSTNAME}"
```

#### Documentation
- Detailed comments for each mode (overhead, use cases)
- Examples for production, dev/staging, troubleshooting
- Performance impact documentation

---

## REMAINING WORK (30%)

### 4. Fix Type Mismatches (⏱️ 1-2 hours)

**Issue**: App uses `TracingManager` but dispatcher expects `EnhancedTracingManager`

**Files to Fix**:
1. `internal/app/handlers.go`:
   - Line 169: `app.config.Tracing.Mode` → config struct missing Mode field
   - Line 169: `app.config.Tracing.OnDemand` → config struct missing OnDemand field
   - Lines 1282, 1288, 1289: `GetMode()` method missing
   - Line 1334: `EnableOnDemandTracing()` method missing
   - Line 1410: `DisableOnDemandTracing()` method missing
   - Line 1470: `GetStatus()` method missing

2. `internal/app/initialization.go`:
   - Line 76: Type mismatch: `*TracingManager` vs `*EnhancedTracingManager`

**Solution Options**:
A. **Use EnhancedTracingManager Everywhere** (Recommended)
   - Update app.go to use EnhancedTracingManager
   - Update types/enterprise.go TracingConfig to match EnhancedTracingConfig
   - Benefits: Full feature set, hybrid modes, adaptive sampling

B. **Extend Existing TracingManager**
   - Add missing methods to pkg/tracing/tracing.go
   - Benefits: Backward compatible

**Recommended**: Option A - Use EnhancedTracingManager throughout

---

### 5. Prometheus Metrics (⏱️ 2 hours)

**File to Create**: `pkg/tracing/metrics.go`

**Metrics Needed**:
```go
// Mode tracking
log_capturer_tracing_mode{mode="hybrid"} = 1

// Logs traced
log_capturer_tracing_logs_traced_total = counter

// Spans created
log_capturer_tracing_spans_created_total{span_type="log|system"} = counter

// Sampling rate (current)
log_capturer_tracing_sampling_rate = gauge

// On-demand rules
log_capturer_tracing_on_demand_rules_active = gauge

// Adaptive sampling status
log_capturer_tracing_adaptive_sampling_active = gauge

// Spans exported/dropped
log_capturer_tracing_spans_exported_total = counter
log_capturer_tracing_spans_dropped_total{reason} = counter
```

**Integration Points**:
- `tracing_enhanced.go`: Call metrics functions in:
  - `CreateLogSpan()` → RecordLogTraced()
  - `CreateSystemSpan()` → RecordSystemSpan()
  - `ReloadConfig()` → UpdateMode()
  - `EnableOnDemandTracing()` → UpdateOnDemandRules()
  - AdaptiveSampler → SetAdaptiveSamplingActive()

---

### 6. Grafana Dashboard (⏱️ 1 hour)

**File to Create**: `provisioning/dashboards/tracing-monitoring.json`

**Panels Needed**:
1. Current Tracing Mode (Stat)
2. Logs Traced Rate (Graph)
3. Spans Created Rate by Type (Graph - log vs system)
4. Current Sampling Rate (Gauge)
5. On-Demand Rules Active (Stat)
6. Adaptive Sampling Status (Stat)
7. Spans Exported vs Dropped (Graph)
8. Tracing Overhead Estimation (Calculated)

**Queries**:
```promql
# Logs traced rate
rate(log_capturer_tracing_logs_traced_total[5m])

# Spans by type
rate(log_capturer_tracing_spans_created_total[5m])

# Current sampling rate
log_capturer_tracing_sampling_rate

# Overhead estimation
(log_capturer_tracing_logs_traced_total / log_capturer_logs_processed_total) * 100
```

---

### 7. Testing (⏱️ 2-3 hours)

**File to Create**: `pkg/tracing/tracing_test.go`

**Test Coverage Needed**:
```go
// Mode switching
TestModeOff()
TestModeSystemOnly()
TestModeHybrid()
TestModeFullE2E()

// Sampling decisions
TestShouldTraceLog_Off()
TestShouldTraceLog_SystemOnly()
TestShouldTraceLog_Hybrid_BaseRate()
TestShouldTraceLog_Hybrid_Adaptive()
TestShouldTraceLog_Hybrid_OnDemand()
TestShouldTraceLog_FullE2E()

// Span creation
TestCreateLogSpan()
TestCreateSystemSpan()
TestSpanAttributes()
TestContextPropagation()

// Adaptive sampler
TestAdaptiveSampler_Trigger()
TestAdaptiveSampler_Reset()

// On-demand controller
TestOnDemandController_Enable()
TestOnDemandController_Disable()
TestOnDemandController_Expiration()

// Configuration reload
TestReloadConfig()
TestReloadConfig_ModeChange()
```

**File to Create**: `pkg/tracing/tracing_bench_test.go`

**Benchmarks Needed**:
```go
BenchmarkModeOff()             // Baseline
BenchmarkModeSystemOnly()      // System overhead
BenchmarkModeHybrid_0Percent() // Hybrid with 0% log sampling
BenchmarkModeHybrid_1Percent() // Hybrid with 1% log sampling
BenchmarkModeHybrid_10Percent()// Hybrid with 10% log sampling
BenchmarkModeFullE2E()         // Full 100% tracing

// Document expected results:
// - ModeOff: 0% overhead
// - ModeSystemOnly: <1% overhead
// - ModeHybrid(0%): 1-2% overhead
// - ModeHybrid(10%): 3-5% overhead
// - ModeFullE2E: 5-10% overhead
```

---

### 8. Documentation (⏱️ 2 hours)

**File to Create**: `docs/TRACING_GUIDE.md`

**Sections Needed**:
1. Overview
   - What is hybrid tracing
   - When to use each mode
   - Performance impact table

2. Configuration
   - Complete config.yaml walkthrough
   - Mode selection guide
   - Adaptive sampling tuning
   - On-demand control setup

3. Operational Guide
   - Switching modes via hot reload
   - Using on-demand API
   - Interpreting traces in Jaeger/Tempo
   - Performance monitoring

4. API Reference
   - POST /api/tracing/enable (with curl examples)
   - POST /api/tracing/disable (with curl examples)
   - GET /api/tracing/status (with response examples)

5. Troubleshooting
   - Common issues
   - Performance degradation
   - Missing traces
   - High overhead

6. Best Practices
   - Production recommendations (mode=hybrid, rate=0.0)
   - Development recommendations (mode=hybrid, rate=0.1)
   - Troubleshooting recommendations (mode=full-e2e temporarily)

---

## COMPILATION STATUS

**Current State**: Compilation fails due to type mismatches

**Error Summary**:
1. `app.config.Tracing.Mode` undefined (TracingConfig missing field)
2. `app.config.Tracing.OnDemand` undefined (TracingConfig missing field)
3. `app.tracingManager.GetMode()` undefined (method missing)
4. `app.tracingManager.EnableOnDemandTracing()` undefined (method missing)
5. `app.tracingManager.DisableOnDemandTracing()` undefined (method missing)
6. `app.tracingManager.GetStatus()` undefined (method missing)
7. Type mismatch: `*TracingManager` vs `*EnhancedTracingManager`

**Fix Required**: Align types between app and tracing package

---

## FILES MODIFIED

### Modified
1. `internal/app/handlers.go` (added 3 endpoints)
2. `internal/dispatcher/dispatcher.go` (added tracing)
3. `internal/app/initialization.go` (updated dispatcher init)
4. `configs/config.yaml` (comprehensive tracing config)
5. `pkg/tracing/tracing_enhanced.go` (removed unused import)
6. All `benchmarks/*.go` files (updated NewDispatcher calls)

### Created
1. `TASK2_TRACING_PROGRESS_REPORT.md` (this file)

---

## NEXT STEPS (Priority Order)

### HIGH PRIORITY (Must Do)
1. **Fix Type Mismatches** (1-2h)
   - Align TracingManager/EnhancedTracingManager
   - Update TracingConfig struct
   - Ensure compilation succeeds

2. **Create Metrics** (2h)
   - Write pkg/tracing/metrics.go
   - Integrate metrics into TracingManager
   - Test metrics export

### MEDIUM PRIORITY (Should Do)
3. **Create Grafana Dashboard** (1h)
   - Design dashboard layout
   - Write PromQL queries
   - Test visualization

4. **Write Tests** (2-3h)
   - Unit tests for all modes
   - Benchmark tests for overhead
   - Integration tests

### LOW PRIORITY (Nice to Have)
5. **Write Documentation** (2h)
   - Complete TRACING_GUIDE.md
   - Add API examples
   - Document troubleshooting

6. **Create Example Configs** (30min)
   - configs/examples/tracing-off.yaml
   - configs/examples/tracing-system-only.yaml
   - configs/examples/tracing-hybrid-minimal.yaml
   - configs/examples/tracing-full-e2e.yaml

---

## PERFORMANCE IMPACT ESTIMATE

Based on implementation:

| Mode | Log Sampling | System Tracing | Overhead | Use Case |
|------|--------------|----------------|----------|----------|
| off | 0% | No | 0% | Maximum performance |
| system-only | 0% | Yes | <1% | Basic observability |
| hybrid (0%) | 0% (baseline) | Yes | 1-2% | Production (adaptive/on-demand only) |
| hybrid (1%) | 1% | Yes | 2-3% | Light sampling |
| hybrid (10%) | 10% | Yes | 3-5% | Development |
| hybrid (100%) | 100% | Yes | 5-10% | Same as full-e2e |
| full-e2e | 100% | Yes | 5-10% | Deep troubleshooting |

**Recommended Production**:
```yaml
mode: "hybrid"
log_tracing_rate: 0.0  # Zero baseline overhead
adaptive_sampling.enabled: true  # Auto-sample when slow
on_demand.enabled: true  # Manual control via API
```

---

## DEPLOYMENT READINESS

### Ready ✅
- Configuration structure
- API endpoints (logic complete)
- Dispatcher integration
- Core tracing infrastructure (EnhancedTracingManager exists)

### Not Ready ❌
- Type alignment (compilation fails)
- Metrics collection
- Grafana dashboard
- Comprehensive testing
- Documentation

### Estimated Time to Production Ready
- **Minimum** (fix compilation + basic testing): 2-3 hours
- **Complete** (all features + docs): 8-10 hours

---

## RECOMMENDATIONS

### Immediate Actions
1. Fix type mismatches to get clean compilation
2. Run basic smoke tests
3. Verify tracing works in Jaeger

### Before Production
1. Create and validate Prometheus metrics
2. Build Grafana dashboard
3. Run performance benchmarks
4. Document operational procedures

### Optional Enhancements
1. Create example configuration files
2. Add trace sampling strategies
3. Implement trace filtering
4. Add trace annotation support

---

**Report Generated**: 2025-11-07
**Next Update**: After compilation fixes complete
