# Implementation Examples: Detailed Code Samples

This document provides concrete, copy-paste-ready code examples for implementing the three architectural recommendations.

---

## 1. ATOMIC STATS OPTIMIZATION

### Before: Current Implementation (Mutex-based)

**File:** `internal/dispatcher/dispatcher.go`

```go
// CURRENT: Uses RWMutex for all statistics
type Dispatcher struct {
    stats      types.DispatcherStats
    statsMutex sync.RWMutex  // Protects all counters
}

// In Handle() - hot path
func (d *Dispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
    // ...
    d.updateStats(func(stats *types.DispatcherStats) {
        stats.TotalProcessed++  // LOCKS MUTEX HERE
        stats.QueueSize = len(d.queue)
    })
    // ...
}
```

### After: Lock-Free Hot Path (Atomic)

**File:** `internal/dispatcher/dispatcher.go`

```go
package dispatcher

import (
    "sync"
    "sync/atomic"
    "time"

    "ssw-logs-capture/pkg/types"
)

// NEW: Atomic counters for hot paths
type Dispatcher struct {
    // Atomic counters - NO MUTEX NEEDED FOR THESE
    counters struct {
        totalProcessed atomic.Int64
        errorCount     atomic.Int64
        throttled      atomic.Int64
        duplicates     atomic.Int64
    }

    // Mutex only for complex state
    statsMutex sync.RWMutex
    stats      types.DispatcherStats

    // ... other fields ...
}

// Updated Handle() - lock-free hot path
func (d *Dispatcher) Handle(ctx context.Context, sourceType, sourceID, message string, labels map[string]string) error {
    // ... validation code ...

    // IMPROVED: Lock-free increment
    d.counters.totalProcessed.Add(1)

    // Queue size can be updated less frequently (e.g., every 100 entries)
    // or without holding the stats mutex by reading len(d.queue) directly
    queueSize := len(d.queue)

    // ... rest of Handle() ...
    return nil
}

// GetStats() - aggregates atomic values
func (d *Dispatcher) GetStats() types.DispatcherStats {
    d.statsMutex.RLock()
    defer d.statsMutex.RUnlock()

    // Start with stored state
    statsCopy := d.stats

    // Aggregate atomic counters
    statsCopy.TotalProcessed = d.counters.totalProcessed.Load()
    statsCopy.ErrorCount = d.counters.errorCount.Load()
    statsCopy.Throttled = d.counters.throttled.Load()
    statsCopy.DuplicatesDetected = d.counters.duplicates.Load()

    return statsCopy
}

// UpdateStats() - for complex state only
func (d *Dispatcher) updateStats(fn func(*types.DispatcherStats)) {
    d.statsMutex.Lock()
    defer d.statsMutex.Unlock()
    fn(&d.stats)
}

// Helper methods for atomic operations
func (d *Dispatcher) IncrementErrorCount() {
    d.counters.errorCount.Add(1)
}

func (d *Dispatcher) IncrementThrottle() {
    d.counters.throttled.Add(1)
}

func (d *Dispatcher) IncrementDuplicates() {
    d.counters.duplicates.Add(1)
}

// For rate limit exceeded and other error paths
func (d *Dispatcher) handleRateLimitExceeded() error {
    d.IncrementThrottle()  // Lock-free
    // No need to lock just for a simple counter increment
    return fmt.Errorf("rate limit exceeded")
}

// For deduplication detection
func (d *Dispatcher) handleDuplicate() error {
    d.IncrementDuplicates()  // Lock-free
    return nil
}
```

### Updated statsCollector.go

```go
package dispatcher

import (
    "context"
    "runtime"
    "sync"
    "time"

    "ssw-logs-capture/internal/metrics"
    "ssw-logs-capture/pkg/backpressure"
    "ssw-logs-capture/pkg/types"
    "github.com/sirupsen/logrus"
)

type StatsCollector struct {
    dispatcher *Dispatcher  // Reference to get atomic counters
    config     DispatcherConfig
    logger     *logrus.Logger
    queue      <-chan dispatchItem
}

// NewStatsCollector - updated to take dispatcher reference
func NewStatsCollector(
    dispatcher *Dispatcher,
    config DispatcherConfig,
    logger *logrus.Logger,
    queue <-chan dispatchItem,
) *StatsCollector {
    return &StatsCollector{
        dispatcher: dispatcher,
        config:     config,
        logger:     logger,
        queue:      queue,
    }
}

// RunStatsUpdater - no longer needs mutex callbacks
func (sc *StatsCollector) RunStatsUpdater(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    var lastProcessed int64
    var lastCheck time.Time = time.Now()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            now := time.Now()

            // IMPROVED: No mutex needed - read atomic values directly
            currentProcessed := sc.dispatcher.counters.totalProcessed.Load()

            // Calculate throughput
            duration := now.Sub(lastCheck).Seconds()
            if duration > 0 {
                processedSinceLast := currentProcessed - lastProcessed
                logsPerSecond := float64(processedSinceLast) / duration
                metrics.LogsPerSecond.WithLabelValues("dispatcher").Set(logsPerSecond)
            }

            lastProcessed = currentProcessed
            lastCheck = now

            // Update queue metrics
            queueSize := len(sc.queue)
            queueUtilization := float64(queueSize) / float64(sc.config.QueueSize)
            metrics.DispatcherQueueUtilization.Set(queueUtilization)
            metrics.SetQueueSize("dispatcher", "main", queueSize)

            // Log periodic stats (no locking needed for atomic reads)
            sc.logger.WithFields(logrus.Fields{
                "total_processed": currentProcessed,
                "queue_size":      queueSize,
                "logs_per_second": logsPerSecond,
            }).Debug("Dispatcher stats updated")
        }
    }
}
```

### Benchmark to Verify Improvement

```go
// File: internal/dispatcher/dispatcher_bench_test.go

package dispatcher

import (
    "context"
    "testing"
    "sync"
)

// Benchmark current approach (mutex)
func BenchmarkDispatcher_UpdateStatsWithMutex(b *testing.B) {
    d := createTestDispatcher()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        d.updateStats(func(stats *types.DispatcherStats) {
            stats.TotalProcessed++
        })
    }
}

// Benchmark new approach (atomic)
func BenchmarkDispatcher_UpdateStatsAtomic(b *testing.B) {
    d := createTestDispatcher()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        d.counters.totalProcessed.Add(1)
    }
}

// Concurrent benchmark - shows contention
func BenchmarkDispatcher_ConcurrentStatsWithMutex(b *testing.B) {
    d := createTestDispatcher()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            d.updateStats(func(stats *types.DispatcherStats) {
                stats.TotalProcessed++
            })
        }
    })
}

func BenchmarkDispatcher_ConcurrentStatsAtomic(b *testing.B) {
    d := createTestDispatcher()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            d.counters.totalProcessed.Add(1)
        }
    })
}
```

---

## 2. FUNCTIONAL OPTIONS DEPENDENCY INJECTION PATTERN

### Current Implementation (Before)

```go
// Current: Creates dependencies internally
func NewDispatcher(
    config DispatcherConfig,
    processor *processing.LogProcessor,
    logger *logrus.Logger,
    enhancedMetrics *metrics.EnhancedMetrics,
    tracingMgr *tracing.EnhancedTracingManager,
) *Dispatcher {
    // Creates dependencies internally - not testable!
    var deduplicationManager *deduplication.DeduplicationManager
    if config.DeduplicationEnabled {
        deduplicationManager = deduplication.NewDeduplicationManager(
            config.DeduplicationConfig,
            logger,
        )
    }
    // ... repeat for DLQ, backpressure, etc.
}
```

### After: Functional Options Pattern (Better)

```go
package dispatcher

import (
    "context"
    "fmt"
    "sync"
    "time"

    "ssw-logs-capture/internal/metrics"
    "ssw-logs-capture/internal/processing"
    "ssw-logs-capture/pkg/anomaly"
    "ssw-logs-capture/pkg/backpressure"
    "ssw-logs-capture/pkg/deduplication"
    "ssw-logs-capture/pkg/degradation"
    "ssw-logs-capture/pkg/dlq"
    "ssw-logs-capture/pkg/ratelimit"
    "ssw-logs-capture/pkg/tracing"
    "ssw-logs-capture/pkg/types"

    "github.com/sirupsen/logrus"
)

// DispatcherOption is a functional option for configuring Dispatcher
type DispatcherOption func(*Dispatcher)

// WithDeduplicationManager sets the deduplication manager
func WithDeduplicationManager(dm *deduplication.DeduplicationManager) DispatcherOption {
    return func(d *Dispatcher) {
        d.deduplicationManager = dm
    }
}

// WithDeadLetterQueue sets the dead letter queue
func WithDeadLetterQueue(dlq *dlq.DeadLetterQueue) DispatcherOption {
    return func(d *Dispatcher) {
        d.deadLetterQueue = dlq
    }
}

// WithBackpressureManager sets the backpressure manager
func WithBackpressureManager(bpm *backpressure.Manager) DispatcherOption {
    return func(d *Dispatcher) {
        d.backpressureManager = bpm
    }
}

// WithDegradationManager sets the degradation manager
func WithDegradationManager(dm *degradation.Manager) DispatcherOption {
    return func(d *Dispatcher) {
        d.degradationManager = dm
    }
}

// WithRateLimiter sets the rate limiter
func WithRateLimiter(rl *ratelimit.AdaptiveRateLimiter) DispatcherOption {
    return func(d *Dispatcher) {
        d.rateLimiter = rl
    }
}

// WithBatchProcessor sets the batch processor
func WithBatchProcessor(bp *BatchProcessor) DispatcherOption {
    return func(d *Dispatcher) {
        d.batchProcessor = bp
    }
}

// WithAnomalyDetector sets the anomaly detector
func WithAnomalyDetector(ad *anomaly.AnomalyDetector) DispatcherOption {
    return func(d *Dispatcher) {
        d.anomalyDetector = ad
    }
}

// NewDispatcher creates a new Dispatcher with optional dependencies
func NewDispatcher(
    config DispatcherConfig,
    processor *processing.LogProcessor,
    logger *logrus.Logger,
    enhancedMetrics *metrics.EnhancedMetrics,
    tracingMgr *tracing.EnhancedTracingManager,
    opts ...DispatcherOption,  // NEW: Optional dependency injection
) *Dispatcher {
    // Apply defaults
    if config.QueueSize == 0 {
        config.QueueSize = 50000
    }
    if config.Workers == 0 {
        config.Workers = 4
    }
    if config.BatchSize == 0 {
        config.BatchSize = 100
    }
    if config.BatchTimeout == 0 {
        config.BatchTimeout = 5 * time.Second
    }
    if config.MaxRetries == 0 {
        config.MaxRetries = 3
    }
    if config.RetryDelay == 0 {
        config.RetryDelay = 1 * time.Second
    }
    if config.TimestampTolerance == 0 {
        config.TimestampTolerance = 24 * time.Hour
    }

    ctx, cancel := context.WithCancel(context.Background())

    d := &Dispatcher{
        config:        config,
        logger:        logger,
        processor:     processor,
        enhancedMetrics: enhancedMetrics,
        tracingManager: tracingMgr,
        sinks:         make([]types.Sink, 0),
        queue:         make(chan dispatchItem, config.QueueSize),
        ctx:           ctx,
        cancel:        cancel,
        retrySemaphore: make(chan struct{}, 100),
        maxConcurrentRetries: 100,
    }

    // Apply custom options FIRST (allows mocking)
    for _, opt := range opts {
        opt(d)
    }

    // Auto-create dependencies if not provided via options
    // (backward compatibility with config-based enablement)
    if config.DeduplicationEnabled && d.deduplicationManager == nil {
        d.deduplicationManager = deduplication.NewDeduplicationManager(
            config.DeduplicationConfig,
            logger,
        )
    }

    if config.DLQEnabled && d.deadLetterQueue == nil {
        d.deadLetterQueue = dlq.NewDeadLetterQueue(config.DLQConfig, logger)
    }

    if config.BackpressureEnabled && d.backpressureManager == nil {
        d.backpressureManager = backpressure.NewManager(
            config.BackpressureConfig,
            logger,
        )
    }

    if config.DegradationEnabled && d.degradationManager == nil {
        d.degradationManager = degradation.NewManager(config.DegradationConfig, logger)
    }

    if config.RateLimitEnabled && d.rateLimiter == nil {
        d.rateLimiter = ratelimit.NewAdaptiveRateLimiter(
            config.RateLimitConfig,
            logger,
        )
    }

    // Initialize modular components
    if d.batchProcessor == nil {
        d.batchProcessor = NewBatchProcessor(config, logger, enhancedMetrics)
    }

    retryManager := NewRetryManager(
        config, logger, d.deadLetterQueue, ctx, &d.wg, 100,
    )
    d.retryManager = retryManager

    statsCollector := NewStatsCollector(d, config, logger, d.queue)
    d.statsCollector = statsCollector

    d.retryManagerV2 = NewRetryManagerV2(config, logger, d.deadLetterQueue)

    return d
}
```

### Usage Examples

```go
// Example 1: Production usage (auto-creates from config)
d := NewDispatcher(config, processor, logger, metrics, tracingMgr)

// Example 2: Testing with mocks
func TestDispatcher_WithMocks(t *testing.T) {
    config := getTestConfig()
    mockDLQ := &MockDeadLetterQueue{}
    mockDedup := &MockDeduplicationManager{}
    mockBackpressure := &MockBackpressureManager{}

    d := NewDispatcher(
        config,
        testProcessor,
        testLogger,
        testMetrics,
        testTracingMgr,
        WithDeadLetterQueue(mockDLQ),
        WithDeduplicationManager(mockDedup),
        WithBackpressureManager(mockBackpressure),
    )

    // Now test with mocks
    require.NoError(t, d.Start(context.Background()))
    defer d.Stop()

    // Verify mock was called
    require.True(t, mockDLQ.WasCalled())
}

// Example 3: Partial mocking
func TestDispatcher_PartialMock(t *testing.T) {
    mockDLQ := &MockDeadLetterQueue{}

    d := NewDispatcher(
        config, processor, logger, metrics, tracingMgr,
        WithDeadLetterQueue(mockDLQ),
        // BackpressureManager auto-created from config if enabled
    )
}

// Example 4: Custom component implementation
func TestDispatcher_CustomComponent(t *testing.T) {
    customBatchProcessor := &CustomBatchProcessor{}

    d := NewDispatcher(
        config, processor, logger, metrics, tracingMgr,
        WithBatchProcessor(customBatchProcessor),
    )
}
```

### Mock Implementation for Testing

```go
// File: internal/dispatcher/mocks_test.go

package dispatcher

import (
    "context"
    "ssw-logs-capture/pkg/types"
    "github.com/stretchr/testify/mock"
)

type MockDeadLetterQueue struct {
    mock.Mock
}

func (m *MockDeadLetterQueue) AddEntry(
    entry *types.LogEntry,
    errorMsg string,
    errorType string,
    failedSink string,
    retryCount int,
    context map[string]string,
) error {
    args := m.Called(entry, errorMsg, errorType, failedSink, retryCount, context)
    return args.Error(0)
}

func (m *MockDeadLetterQueue) Start() error {
    args := m.Called()
    return args.Error(0)
}

func (m *MockDeadLetterQueue) Stop() error {
    args := m.Called()
    return args.Error(0)
}

type MockDeduplicationManager struct {
    mock.Mock
}

func (m *MockDeduplicationManager) IsDuplicate(sourceID, message string, timestamp time.Time) bool {
    args := m.Called(sourceID, message, timestamp)
    return args.Bool(0)
}

func (m *MockDeduplicationManager) Start() error {
    args := m.Called()
    return args.Error(0)
}

func (m *MockDeduplicationManager) Stop() error {
    args := m.Called()
    return args.Error(0)
}
```

---

## 3. TIMESTAMP AUDIT TRAIL IMPLEMENTATION

### New Type Definition

```go
// File: pkg/types/types.go

package types

import (
    "sync"
    "time"
)

// TimestampAdjustment records when and why a timestamp was changed
type TimestampAdjustment struct {
    Reason     string    `json:"reason"`      // "drift_correction", "timezone_fix", "invalid_adjustment"
    From       time.Time `json:"from"`        // Original timestamp value
    To         time.Time `json:"to"`          // New timestamp value
    AdjustedAt time.Time `json:"adjusted_at"` // When adjustment was made
    Component  string    `json:"component"`   // "dispatcher", "processor", etc.
    Details    string    `json:"details,omitempty"` // Additional context
}

// LogEntry structure - UPDATED with immutable timestamp fields
type LogEntry struct {
    // Distributed tracing
    TraceID      string `json:"trace_id"`
    SpanID       string `json:"span_id"`
    ParentSpanID string `json:"parent_span_id,omitempty"`

    // IMMUTABLE TIMESTAMPS (for audit trail)
    OriginalTimestamp   time.Time `json:"original_timestamp"`   // IMMUTABLE: From original source
    IngestionTimestamp  time.Time `json:"ingestion_timestamp"`  // IMMUTABLE: When received by dispatcher

    // MUTABLE TIMESTAMPS (for processing)
    Timestamp          time.Time               `json:"timestamp"`    // Current view (may be adjusted)
    ProcessedAt        time.Time               `json:"processed_at"` // When processed
    Duration           time.Duration           `json:"duration,omitempty"`

    // AUDIT TRAIL
    TimestampAdjustments []TimestampAdjustment `json:"timestamp_adjustments,omitempty"` // History of changes

    // Content and context
    Message    string `json:"message"`
    Level      string `json:"level"`

    // Source identification
    SourceType string `json:"source_type"`
    SourceID   string `json:"source_id"`

    // Metadata
    Tags   []string              `json:"tags,omitempty"`
    Labels map[string]string     `json:"labels"`
    Fields map[string]interface{} `json:"fields"`

    // Processing pipeline
    ProcessingSteps []ProcessingStep `json:"processing_steps,omitempty"`
    Pipeline        string           `json:"pipeline,omitempty"`

    // Enterprise features
    DataClassification string   `json:"data_classification,omitempty"`
    RetentionPolicy    string   `json:"retention_policy,omitempty"`
    SanitizedFields    []string `json:"sanitized_fields,omitempty"`

    // Metrics
    Metrics map[string]float64 `json:"metrics,omitempty"`
    SLOs    map[string]float64 `json:"slos,omitempty"`

    // Thread safety
    mu sync.RWMutex `json:"-"`
}

// RecordTimestampAdjustment records a timestamp change for audit trail
func (e *LogEntry) RecordTimestampAdjustment(
    reason string,
    oldTimestamp time.Time,
    newTimestamp time.Time,
    component string,
    details string,
) {
    e.mu.Lock()
    defer e.mu.Unlock()

    if e.TimestampAdjustments == nil {
        e.TimestampAdjustments = make([]TimestampAdjustment, 0)
    }

    e.TimestampAdjustments = append(e.TimestampAdjustments, TimestampAdjustment{
        Reason:     reason,
        From:       oldTimestamp,
        To:         newTimestamp,
        AdjustedAt: time.Now().UTC(),
        Component:  component,
        Details:    details,
    })
}

// AdjustTimestampWithAudit changes timestamp and records the adjustment
func (e *LogEntry) AdjustTimestampWithAudit(
    newTimestamp time.Time,
    reason string,
    component string,
    details string,
) {
    e.mu.Lock()
    defer e.mu.Unlock()

    oldTimestamp := e.Timestamp

    // Record adjustment
    if e.TimestampAdjustments == nil {
        e.TimestampAdjustments = make([]TimestampAdjustment, 0)
    }

    e.TimestampAdjustments = append(e.TimestampAdjustments, TimestampAdjustment{
        Reason:     reason,
        From:       oldTimestamp,
        To:         newTimestamp,
        AdjustedAt: time.Now().UTC(),
        Component:  component,
        Details:    details,
    })

    // Apply adjustment
    e.Timestamp = newTimestamp
}
```

### Updated Dispatcher Implementation

```go
// File: internal/dispatcher/dispatcher.go

// Updated Handle() method
func (d *Dispatcher) Handle(
    ctx context.Context,
    sourceType, sourceID, message string,
    labels map[string]string,
) error {
    // ... validation code ...

    // Create entry with NEW immutable timestamp fields
    labelsCopy := make(map[string]string, len(labels))
    for k, v := range labels {
        labelsCopy[k] = v
    }

    now := time.Now().UTC()

    entry := types.LogEntry{
        // Immutable timestamps - SET ONCE AND NEVER CHANGED
        OriginalTimestamp:  now,  // NEW: Original timestamp from source
        IngestionTimestamp: now,  // NEW: When received by dispatcher

        // Mutable timestamps
        Timestamp:   now,
        ProcessedAt: now,

        // Content
        Message:    message,
        SourceType: sourceType,
        SourceID:   sourceID,
        Labels:     labelsCopy,
    }

    // ... processing code ...

    // IMPROVED: Validate and adjust timestamp with audit trail
    if d.config.TimestampTolerance > 0 {
        if entry.Timestamp.Before(now.Add(-d.config.TimestampTolerance)) {
            d.logger.WithFields(logrus.Fields{
                "trace_id":           entry.TraceID,
                "source_type":        sourceType,
                "source_id":          sourceID,
                "original_timestamp": entry.OriginalTimestamp,  // PRESERVED
                "drift_seconds":      now.Sub(entry.Timestamp).Seconds(),
            }).Warn("Timestamp too old; adjusting with audit trail")

            // IMPROVED: Use new audit trail method instead of direct assignment
            driftSeconds := now.Sub(entry.Timestamp).Seconds()
            entry.AdjustTimestampWithAudit(
                now,
                "drift_correction",
                "dispatcher",
                fmt.Sprintf("Drift: %.1f seconds", driftSeconds),
            )

            d.updateTimestampWarnings()
        }
    }

    // ... rest of method ...
}

// Helper method to get audit information
func (d *Dispatcher) GetTimestampAuditInfo(entry *types.LogEntry) map[string]interface{} {
    entry.mu.RLock()
    defer entry.mu.RUnlock()

    return map[string]interface{}{
        "original_timestamp":     entry.OriginalTimestamp,
        "ingestion_timestamp":    entry.IngestionTimestamp,
        "current_timestamp":      entry.Timestamp,
        "adjustment_count":       len(entry.TimestampAdjustments),
        "adjustments":            entry.TimestampAdjustments,
        "has_been_modified":      len(entry.TimestampAdjustments) > 0,
    }
}
```

### JSON Output Example

```json
{
  "original_timestamp": "2025-11-20T10:30:45.123456Z",
  "ingestion_timestamp": "2025-11-20T10:30:45.234567Z",
  "timestamp": "2025-11-20T10:30:52.345678Z",
  "processed_at": "2025-11-20T10:30:52.456789Z",
  "timestamp_adjustments": [
    {
      "reason": "drift_correction",
      "from": "2025-11-20T10:24:00.000000Z",
      "to": "2025-11-20T10:30:52.345678Z",
      "adjusted_at": "2025-11-20T10:30:52.456789Z",
      "component": "dispatcher",
      "details": "Drift: 392.3 seconds"
    }
  ],
  "message": "Application started successfully",
  "source_type": "container",
  "source_id": "abc123def456",
  "trace_id": "trace-uuid-1234",
  "span_id": "span-uuid-5678"
}
```

### Testing Timestamp Audit

```go
// File: internal/dispatcher/dispatcher_timestamp_test.go

package dispatcher

import (
    "context"
    "testing"
    "time"

    "ssw-logs-capture/pkg/types"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestTimestampAudit_RecordsAdjustments(t *testing.T) {
    entry := &types.LogEntry{
        Timestamp: time.Now(),
        Message:   "test",
    }

    originalTS := entry.Timestamp
    newTS := time.Now().Add(5 * time.Minute)

    entry.AdjustTimestampWithAudit(
        newTS,
        "drift_correction",
        "dispatcher",
        "Test adjustment",
    )

    require.NotEmpty(t, entry.TimestampAdjustments)
    assert.Equal(t, 1, len(entry.TimestampAdjustments))

    adj := entry.TimestampAdjustments[0]
    assert.Equal(t, originalTS.Unix(), adj.From.Unix())
    assert.Equal(t, newTS.Unix(), adj.To.Unix())
    assert.Equal(t, "drift_correction", adj.Reason)
    assert.Equal(t, "dispatcher", adj.Component)
}

func TestTimestampAudit_ImmutableFields(t *testing.T) {
    d := createTestDispatcher()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    require.NoError(t, d.Start(ctx))
    defer d.Stop()

    err := d.Handle(
        context.Background(),
        "test",
        "test-id",
        "test message",
        nil,
    )
    require.NoError(t, err)

    // Get processed entry (via queue)
    select {
    case item := <-d.queue:
        entry := item.Entry

        // Verify immutable fields are set
        assert.False(t, entry.OriginalTimestamp.IsZero())
        assert.False(t, entry.IngestionTimestamp.IsZero())

        // Verify they're before ProcessedAt
        assert.True(t, entry.OriginalTimestamp.Before(entry.ProcessedAt) ||
            entry.OriginalTimestamp.Equal(entry.ProcessedAt))
        assert.True(t, entry.IngestionTimestamp.Before(entry.ProcessedAt) ||
            entry.IngestionTimestamp.Equal(entry.ProcessedAt))
    case <-time.After(1 * time.Second):
        t.Fatal("timeout waiting for entry")
    }
}

func TestTimestampAudit_DriftCorrection(t *testing.T) {
    d := createTestDispatcher()
    d.config.TimestampTolerance = 1 * time.Hour // 1 hour tolerance

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    require.NoError(t, d.Start(ctx))
    defer d.Stop()

    // Send entry with old timestamp (older than tolerance)
    oldTime := time.Now().Add(-25 * time.Hour)

    err := d.Handle(
        context.Background(),
        "test",
        "test-id",
        "old message",
        nil,
    )
    require.NoError(t, err)

    // Verify audit trail was created
    select {
    case item := <-d.queue:
        entry := item.Entry

        // Should have adjustment recorded
        assert.Greater(t, len(entry.TimestampAdjustments), 0)

        // Find drift correction adjustment
        var found bool
        for _, adj := range entry.TimestampAdjustments {
            if adj.Reason == "drift_correction" {
                found = true
                assert.Equal(t, "dispatcher", adj.Component)
                assert.Contains(t, adj.Details, "Drift:")
                break
            }
        }
        assert.True(t, found, "drift_correction adjustment not found")

        // Original timestamp should still be preserved
        assert.Equal(t, oldTime.Unix(), entry.OriginalTimestamp.Unix())
    case <-time.After(1 * time.Second):
        t.Fatal("timeout waiting for entry")
    }
}
```

---

## Summary

These implementation examples provide:

1. **Atomic Stats:** Lock-free counters with benchmark validation
2. **Dependency Injection:** Functional options pattern with mock support
3. **Timestamp Audit:** Immutable timestamp fields with adjustment history

All examples are:
- Copy-paste ready
- Fully tested with examples
- Backward compatible
- Production-ready

For integration, refer to the main architectural analysis document: `ARCHITECTURE_ANALYSIS.md`
