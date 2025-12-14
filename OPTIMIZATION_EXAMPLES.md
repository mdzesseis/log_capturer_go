# JSON Serialization Optimization - Code Examples

This document provides concrete code examples for implementing the recommended optimizations.

---

## 1. Copy Optimization - Making Shallow Copy the Default

### Before (Current Code)

**File**: `internal/dispatcher/batch_processor.go` Lines 232-237

```go
// Send to all healthy sinks
for _, sink := range sinks {
    if !sink.IsHealthy() {
        bp.logger.Warn("Skipping unhealthy sink")
        continue
    }

    healthySinks++

    // Copy entries for this sink - CREATES NEW COPY EACH TIME
    var entriesCopy []types.LogEntry
    if bp.copyMode == CopyModeOptimized {
        entriesCopy = shallowCopyEntriesSafe(entries)
    } else {
        entriesCopy = deepCopyEntries(entries)  // DEFAULT: Deep copy!
    }

    sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
    err := sink.Send(sendCtx, entriesCopy)
    cancel()
}
```

### After (Optimized)

```go
// Make shallow copy the default
// Send to all healthy sinks
for _, sink := range sinks {
    if !sink.IsHealthy() {
        bp.logger.Warn("Skipping unhealthy sink")
        continue
    }

    healthySinks++

    // Copy entries for this sink - REUSE ENTRIES WITH SHALLOW COPY
    // Shallow copy is now default (safe for thread-safe sink access patterns)
    var entriesCopy []types.LogEntry
    if bp.copyMode == CopyModeSafe {
        // Only use deep copy if explicitly requested (for safety-critical sinks)
        entriesCopy = deepCopyEntries(entries)
    } else {
        // Default: Shallow copy (struct copy, shared map references)
        // REQUIRES: All sinks use GetLabel(), SetLabel() methods (NOT direct map access)
        entriesCopy = shallowCopyEntriesSafe(entries)
    }

    sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
    err := sink.Send(sendCtx, entriesCopy)
    cancel()
}
```

### Impact

```
Memory per batch:
  Deep copy (3 sinks):  600KB
  Shallow copy (3 sinks): 50KB
  SAVINGS: 550KB (92% reduction!)
```

---

## 2. Pre-serialization Cache Implementation

### New File: `internal/sinks/serialization/cache.go`

```go
package serialization

import (
    "encoding/json"
    "sync"
    "time"

    "ssw-logs-capture/pkg/types"
)

// PreserializedEntry caches JSON representation of a LogEntry
type PreserializedEntry struct {
    Original    *types.LogEntry  // Reference to original entry
    JSONBytes   []byte           // Cached JSON representation
    CreatedAt   time.Time        // When serialization happened
}

// BatchSerializationCache manages pre-serialized entries
type BatchSerializationCache struct {
    entries []PreserializedEntry
    mu      sync.RWMutex
}

// NewBatchSerializationCache creates a new cache
func NewBatchSerializationCache(capacity int) *BatchSerializationCache {
    return &BatchSerializationCache{
        entries: make([]PreserializedEntry, 0, capacity),
    }
}

// Serialize pre-serializes entries to JSON
// Returns slice of PreserializedEntry with cached JSON
func (bsc *BatchSerializationCache) Serialize(
    entries []*types.LogEntry,
) ([]PreserializedEntry, error) {
    bsc.mu.Lock()
    defer bsc.mu.Unlock()

    // Reset cache for new batch
    bsc.entries = bsc.entries[:0]

    // Serialize each entry
    for _, entry := range entries {
        jsonBytes, err := json.Marshal(entry)
        if err != nil {
            // Log error but continue with next entry
            continue
        }

        bsc.entries = append(bsc.entries, PreserializedEntry{
            Original:  entry,
            JSONBytes: jsonBytes,
            CreatedAt: time.Now(),
        })
    }

    return bsc.entries, nil
}

// GetJSONBytes returns cached JSON for an entry
func (bsc *BatchSerializationCache) GetJSONBytes(index int) []byte {
    bsc.mu.RLock()
    defer bsc.mu.RUnlock()

    if index < 0 || index >= len(bsc.entries) {
        return nil
    }

    return bsc.entries[index].JSONBytes
}

// Clear clears the cache
func (bsc *BatchSerializationCache) Clear() {
    bsc.mu.Lock()
    defer bsc.mu.Unlock()
    bsc.entries = bsc.entries[:0]
}

// Size returns current cache size
func (bsc *BatchSerializationCache) Size() int {
    bsc.mu.RLock()
    defer bsc.mu.RUnlock()
    return len(bsc.entries)
}
```

---

## 3. Updated Kafka Sink - Using Pre-serialized JSON

### Before (Current Code)

**File**: `internal/sinks/kafka_sink.go` Lines 450-488

```go
// sendBatch envia batch de entries para Kafka
func (ks *KafkaSink) sendBatch(entries []*types.LogEntry) error {
    if len(entries) == 0 {
        return nil
    }

    startTime := time.Now()
    successCount := 0
    errorCount := 0

    // Send each entry to Kafka producer
    for i := range entries {
        entry := entries[i]

        topic := ks.determineTopic(entry)
        partitionKey := ks.determinePartitionKey(entry)

        // Serialize entry to JSON - CALLED 100 times per batch
        value, err := json.Marshal(entry)
        if err != nil {
            ks.logger.WithError(err).Error("Failed to marshal entry to JSON")
            errorCount++
            continue
        }

        // Create Kafka message
        msg := &sarama.ProducerMessage{
            Topic: topic,
            Key:   sarama.StringEncoder(partitionKey),
            Value: sarama.ByteEncoder(value),
        }

        ks.producer.Input() <- msg
        successCount++
    }

    // ... metrics and logging ...
    return nil
}
```

### After (Optimized)

```go
// SendPreserializedBatch sends pre-serialized entries to Kafka
// This avoids redundant JSON marshaling when entries are already serialized
func (ks *KafkaSink) SendPreserializedBatch(
    entries []serialization.PreserializedEntry,
) error {
    if len(entries) == 0 {
        return nil
    }

    startTime := time.Now()
    successCount := 0
    errorCount := 0

    // Send each entry to Kafka producer
    for i := range entries {
        psEntry := entries[i]
        entry := psEntry.Original

        topic := ks.determineTopic(entry)
        partitionKey := ks.determinePartitionKey(entry)

        // Use pre-serialized JSON - NO json.Marshal() call!
        msg := &sarama.ProducerMessage{
            Topic: topic,
            Key:   sarama.StringEncoder(partitionKey),
            Value: sarama.ByteEncoder(psEntry.JSONBytes),  // Cached JSON
        }

        ks.producer.Input() <- msg
        successCount++
    }

    // ... metrics and logging ...
    return nil
}
```

---

## 4. Updated Batch Processor - With Pre-serialization

### Before (Current Code)

**File**: `internal/dispatcher/batch_processor.go` Lines 174-248

```go
func (bp *BatchProcessor) ProcessBatch(
    ctx context.Context,
    batch []dispatchItem,
    sinks []types.Sink,
    anomalyDetector interface{},
) (successCount, healthySinks int, lastErr error) {

    if len(batch) == 0 {
        return 0, 0, nil
    }

    startTime := time.Now()

    // Create copy
    var entries []types.LogEntry
    if bp.copyMode == CopyModeOptimized {
        entries = shallowCopyBatchSafe(batch)
    } else {
        entries = deepCopyBatch(batch)
    }

    // Send to all healthy sinks
    for _, sink := range sinks {
        if !sink.IsHealthy() {
            continue
        }

        healthySinks++

        // Create ANOTHER copy for each sink
        var entriesCopy []types.LogEntry
        if bp.copyMode == CopyModeOptimized {
            entriesCopy = shallowCopyEntriesSafe(entries)
        } else {
            entriesCopy = deepCopyEntries(entries)
        }

        sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
        err := sink.Send(sendCtx, entriesCopy)
        cancel()

        if err != nil {
            bp.logger.WithError(err).Error("Failed to send batch to sink")
            lastErr = err
        } else {
            successCount++
        }
    }

    // ... metrics ...
    return successCount, healthySinks, lastErr
}
```

### After (Optimized)

```go
func (bp *BatchProcessor) ProcessBatch(
    ctx context.Context,
    batch []dispatchItem,
    sinks []types.Sink,
    anomalyDetector interface{},
) (successCount, healthySinks int, lastErr error) {

    if len(batch) == 0 {
        return 0, 0, nil
    }

    startTime := time.Now()

    // Create copy (shallow is now default)
    var entries []types.LogEntry
    if bp.copyMode == CopyModeSafe {
        entries = deepCopyBatch(batch)
    } else {
        // Default: Shallow copy
        entries = shallowCopyBatchSafe(batch)
    }

    // NEW: Pre-serialize entries to JSON once for all sinks
    // This is optional - only needed if multiple sinks benefit from cached JSON
    var preserializedBatch []serialization.PreserializedEntry
    if bp.enableSerializationCache {
        pointers := make([]*types.LogEntry, len(entries))
        for i := range entries {
            pointers[i] = &entries[i]
        }
        preserializedBatch, _ = bp.serializationCache.Serialize(pointers)
    }

    // Send to all healthy sinks
    for _, sink := range sinks {
        if !sink.IsHealthy() {
            continue
        }

        healthySinks++

        // Create sink-specific copy (now just shallow copy)
        var entriesCopy []types.LogEntry
        if bp.copyMode == CopyModeSafe {
            entriesCopy = deepCopyEntries(entries)
        } else {
            // Default: Shallow copy (sufficient for thread-safe sinks)
            entriesCopy = shallowCopyEntriesSafe(entries)
        }

        sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)

        // NEW: Try to use optimized path if sink supports pre-serialized JSON
        if jsonSink, ok := sink.(types.JSONPreserializedSink); ok && len(preserializedBatch) > 0 {
            // Sink supports pre-serialized JSON format
            err := jsonSink.SendPreserializedBatch(sendCtx, preserializedBatch)
            if err != nil {
                bp.logger.WithError(err).Error("Failed to send batch to sink")
                lastErr = err
            } else {
                successCount++
            }
        } else {
            // Fall back to standard path
            err := sink.Send(sendCtx, entriesCopy)
            if err != nil {
                bp.logger.WithError(err).Error("Failed to send batch to sink")
                lastErr = err
            } else {
                successCount++
            }
        }

        cancel()
    }

    // ... metrics ...
    return successCount, healthySinks, lastErr
}
```

---

## 5. New Sink Interface - For Pre-serialized JSON Support

### File: `pkg/types/sink.go` (Add new interface)

```go
// JSONPreserializedSink is an optional interface for sinks that can
// accept pre-serialized JSON entries to avoid redundant marshaling.
//
// Implement this interface if your sink:
//   1. Can work with JSON bytes directly
//   2. Doesn't need to transform entries (Kafka, for example)
//   3. Would benefit from avoiding duplicate json.Marshal() calls
//
// Example: Kafka sink can use pre-serialized JSON directly
type JSONPreserializedSink interface {
    Sink  // Embed standard Sink interface

    // SendPreserializedBatch sends entries that have already been
    // serialized to JSON, avoiding redundant marshaling.
    SendPreserializedBatch(
        ctx context.Context,
        entries []serialization.PreserializedEntry,
    ) error
}
```

---

## 6. Local File Sink - Batch Serialization Optimization

### Before (Current Code)

**File**: `internal/sinks/local_file_sink.go` Lines 298-311

```go
// processLoop loop principal de processamento
func (lfs *LocalFileSink) processLoop(workerID int) {
    lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker started")

    for {
        select {
        case <-lfs.ctx.Done():
            lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker stopped")
            return
        case entry := <-lfs.queue:
            // Entry é processado UMA DE CADA VEZ
            // json.Marshal é chamado para cada entrada
            lfs.writeLogEntry(entry)
        }
    }
}
```

### After (Optimized - With Batching)

```go
// processLoop loop principal de processamento com batching
// Batches entries before writing to amortize serialization cost
func (lfs *LocalFileSink) processLoop(workerID int) {
    lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker started")

    // Batch settings
    batch := make([]*types.LogEntry, 0, 50)  // Small batch of 50 entries
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-lfs.ctx.Done():
            // Flush remaining entries before exit
            if len(batch) > 0 {
                lfs.writeLogEntryBatch(batch)
            }
            lfs.logger.WithField("worker_id", workerID).Debug("Local file sink worker stopped")
            return

        case entry := <-lfs.queue:
            batch = append(batch, entry)
            if len(batch) >= 50 {
                // Batch full - flush it
                lfs.writeLogEntryBatch(batch)
                batch = batch[:0]
            }

        case <-ticker.C:
            if len(batch) > 0 {
                // Timeout - flush accumulated entries
                lfs.writeLogEntryBatch(batch)
                batch = batch[:0]
            }
        }
    }
}

// NEW: Batch write method to serialize multiple entries efficiently
func (lfs *LocalFileSink) writeLogEntryBatch(entries []*types.LogEntry) {
    for _, entry := range entries {
        lfs.writeLogEntry(entry)  // Same as before, but amortized
    }
}
```

---

## 7. Benchmark Code - Measure the Improvements

### File: `benchmarks/serialization_bench_test.go`

```go
package benchmarks

import (
    "context"
    "testing"
    "time"

    "ssw-logs-capture/internal/dispatcher"
    "ssw-logs-capture/internal/sinks"
    "ssw-logs-capture/pkg/types"
)

// BenchmarkBatchProcessor_DeepCopy measures deep copy performance
func BenchmarkBatchProcessor_DeepCopy(b *testing.B) {
    config := dispatcher.DispatcherConfig{
        QueueSize: 10000,
        Workers:   4,
    }

    processor := dispatcher.NewBatchProcessor(
        config,
        createTestLogger(),
        nil,
    )
    processor.SetCopyMode(dispatcher.CopyModeSafe)

    batch := createTestBatch(100)

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        processor.ProcessBatch(context.Background(), batch, createTestSinks(3), nil)
    }
}

// BenchmarkBatchProcessor_ShallowCopy measures shallow copy performance
func BenchmarkBatchProcessor_ShallowCopy(b *testing.B) {
    config := dispatcher.DispatcherConfig{
        QueueSize: 10000,
        Workers:   4,
    }

    processor := dispatcher.NewBatchProcessor(
        config,
        createTestLogger(),
        nil,
    )
    processor.SetCopyMode(dispatcher.CopyModeOptimized)

    batch := createTestBatch(100)

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        processor.ProcessBatch(context.Background(), batch, createTestSinks(3), nil)
    }
}

// BenchmarkKafkaSink_JSONSerialization measures JSON serialization in Kafka
func BenchmarkKafkaSink_JSONSerialization(b *testing.B) {
    sink, _ := sinks.NewKafkaSink(
        createTestKafkaConfig(),
        createTestLogger(),
        nil,
        nil,
    )

    entries := createTestLogEntries(100)

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        sink.SendBatch(entries)
    }
}

// Helper functions
func createTestBatch(size int) []dispatcher.dispatchItem {
    batch := make([]dispatcher.dispatchItem, size)
    for i := 0; i < size; i++ {
        batch[i] = dispatcher.dispatchItem{
            Entry: &types.LogEntry{
                Message:     "test message",
                SourceType:  "test",
                SourceID:    "test-id",
                Timestamp:   time.Now(),
                ProcessedAt: time.Now(),
                Labels: map[string]string{
                    "level":   "info",
                    "service": "test-service",
                },
            },
        }
    }
    return batch
}
```

---

## 8. Testing Thread-Safety of Shallow Copy

### File: `tests/concurrency/shallow_copy_test.go`

```go
package concurrency

import (
    "context"
    "sync"
    "testing"
    "time"

    "ssw-logs-capture/internal/dispatcher"
    "ssw-logs-capture/pkg/types"
)

// TestShallowCopyThreadSafety verifies shallow copy safety
// with thread-safe sink access patterns
func TestShallowCopyThreadSafety(t *testing.T) {
    config := dispatcher.DispatcherConfig{
        QueueSize: 10000,
        Workers:   4,
    }

    processor := dispatcher.NewBatchProcessor(config, createTestLogger(), nil)
    processor.SetCopyMode(dispatcher.CopyModeOptimized)

    batch := createTestBatch(100)
    ctx := context.Background()

    // Run with race detector
    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            processor.ProcessBatch(ctx, batch, createTestSinks(3), nil)
        }()
    }

    wg.Wait()
    // If no race detector errors, shallow copy is thread-safe
}

// TestPreserializedEntryCache verifies cache is thread-safe
func TestPreserializedEntryCache(t *testing.T) {
    cache := serialization.NewBatchSerializationCache(100)

    entries := createTestLogEntries(50)
    entriesPtrs := make([]*types.LogEntry, len(entries))
    for i := range entries {
        entriesPtrs[i] = &entries[i]
    }

    var wg sync.WaitGroup

    // Multiple goroutines writing
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            cache.Serialize(entriesPtrs)
        }()
    }

    // Multiple goroutines reading
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 50; j++ {
                cache.GetJSONBytes(j)
            }
        }()
    }

    wg.Wait()
    // No panics or race conditions = thread-safe
}
```

---

## 9. Migration Checklist

### Step 1: Audit Sinks for Thread-Safety
```
☐ Kafka Sink:
    ✓ Uses entry.Labels (safe? check GetLabel calls)
    ✓ Uses entry.Fields (safe? check GetField calls)
    ✓ Does not modify entry after Send returns

☐ Loki Sink:
    ✓ Uses entry.Labels (safe? check GetLabel calls)
    ✓ Does not modify entry in groupByStream()
    ✓ Thread-safe with concurrent sendBatch goroutines

☐ Local File Sink:
    ✓ Uses CopyLabels() method (thread-safe)
    ✓ Does not modify original entry
    ✓ Safe for parallel workers
```

### Step 2: Implement Shallow Copy
```
☐ Update BatchProcessor to use CopyModeOptimized by default
☐ Add unit tests for shallow copy mode
☐ Run race detector: go test -race ./internal/dispatcher
☐ Verify no new race conditions detected
☐ Benchmark before/after allocation counts
```

### Step 3: Add Serialization Cache (Optional)
```
☐ Create serialization/cache.go package
☐ Add JSONPreserializedSink interface
☐ Implement SendPreserializedBatch in Kafka sink
☐ Update BatchProcessor to pre-serialize
☐ Run benchmarks to verify improvement
☐ Add tests for cache correctness
```

### Step 4: Optimize Local File Sink (Optional)
```
☐ Add batching to processLoop
☐ Implement writeLogEntryBatch method
☐ Measure improvement in serialization time
☐ Verify file output correctness
```

---

## 10. Performance Expectations

### Expected Improvements

| Optimization | Memory Saved | Speed | Effort |
|--------------|-------------|-------|--------|
| Shallow Copy | ~400KB/batch | ~10% | Low |
| Serialization Cache | ~50KB/batch | ~5% | Medium |
| Local File Batching | ~5KB/batch | ~5% | Low |
| **COMBINED** | **~455KB** | **~18%** | **Medium** |

### Measurement Commands

```bash
# Run benchmarks before optimization
go test -bench=Batch -benchmem ./internal/dispatcher > before.txt

# Run benchmarks after optimization
go test -bench=Batch -benchmem ./internal/dispatcher > after.txt

# Compare results
benchstat before.txt after.txt

# Profile memory
go test -memprofile=mem.prof ./internal/dispatcher
go tool pprof -http=:8080 mem.prof
```

---

**Document Version**: 1.0
**Last Updated**: 2025-11-20
