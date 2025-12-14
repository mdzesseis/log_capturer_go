# JSON Serialization Analysis - log_capturer_go

**Date**: 2025-11-20
**Version**: 1.0
**Scope**: Kafka Sink, Loki Sink, Local File Sink JSON serialization efficiency

---

## Executive Summary

The log_capturer_go project currently **serializes each LogEntry multiple times when multiple sinks consume the same data**. This analysis identifies significant performance inefficiencies and recommends optimization strategies.

### Current State
- **Inefficiency**: `json.Marshal()` called separately for EACH sink that receives the same LogEntry
- **Example Impact**: 3 sinks × 100 entries/batch = 300+ JSON serialization operations (should be 100)
- **Allocation Overhead**: ~3x memory allocations for identical JSON payloads
- **JSON Library**: Only standard library `encoding/json` (no faster alternatives in use)

### Recommended Optimization
Implement **JSON serialization caching** to serialize once per batch and reuse across all sinks.

---

## 1. Current JSON Serialization Analysis

### 1.1 Kafka Sink (`internal/sinks/kafka_sink.go`)

**Location**: Line 460
**Current Code**:
```go
// sendBatch (lines 438-543)
for i := range entries {
    entry := entries[i]

    // ... determine topic and partition key ...

    // LINE 460: JSON Marshal called HERE for EACH sink
    value, err := json.Marshal(entry)
    if err != nil {
        // ... error handling ...
        continue
    }

    // Create Kafka message with serialized value
    msg := &sarama.ProducerMessage{
        Topic: topic,
        Key:   sarama.StringEncoder(partitionKey),
        Value: sarama.ByteEncoder(value),  // Uses json.Marshal output
    }

    ks.producer.Input() <- msg
}
```

**Serialization Count**: **1× per batch (only Kafka)**
- Kafka sink is independent and serializes on-demand
- No reuse across sinks

---

### 1.2 Loki Sink (`internal/sinks/loki_sink.go`)

**Location**: Lines 836, 1047-1065
**Current Code**:
```go
// sendToLoki (lines 818-1032)
// First serialization - entire payload
data, err := json.Marshal(payload)  // LINE 836
if err != nil {
    return fmt.Errorf("failed to marshal payload: %w", err)
}

// groupByStream (lines 1035-1065)
// Converts entries to LokiPayload structure with JSON-compatible format
streams := ls.groupByStream(entries)  // Lines 1038-1065
```

**Serialization Details**:
- Converts `[]LogEntry` → `[]LokiStream` (custom Loki format)
- Calls `json.Marshal()` once per batch on `LokiPayload` struct
- **Does NOT serialize individual LogEntry objects** - transforms to Loki format instead

**Serialization Count**: **1× per batch (Loki-specific format)**
- Not directly reusable by other sinks
- Transforms to Loki's `LokiStream` format first

---

### 1.3 Local File Sink (`internal/sinks/local_file_sink.go`)

**Location**: Lines 801-827, 829-871
**Current Code**:
```go
// formatJSONOutput (lines 801-827)
func (lf *logFile) formatJSONOutput(entry *types.LogEntry) string {
    // Create map for output
    output := map[string]interface{}{
        "timestamp":    entry.Timestamp.Format(time.RFC3339Nano),
        "message":      entry.Message,
        "source_type":  entry.SourceType,
        "source_id":    entry.SourceID,
        "processed_at": entry.ProcessedAt.Format(time.RFC3339Nano),
    }

    // LINE 818: JSON Marshal called for EACH entry
    jsonBytes, err := json.Marshal(output)
    if err != nil {
        // ... error handling ...
    }

    return string(jsonBytes) + "\n"
}

// OR formatTextOutput (lines 829-871)
// Uses text formatting instead of JSON
```

**Serialization Count**: **1× per entry (not batched)**
- Called from `writeLogEntry()` (line 360)
- Processes entries sequentially, one at a time
- Each entry serialized individually to intermediate map structure

---

### 1.4 Dispatcher Batch Processing (`internal/dispatcher/batch_processor.go`)

**Location**: Lines 174-248
**Current Architecture**:
```go
// ProcessBatch (lines 174-248)
func (bp *BatchProcessor) ProcessBatch(
    ctx context.Context,
    batch []dispatchItem,
    sinks []types.Sink,
    anomalyDetector interface{},
) (successCount, healthySinks int, lastErr error) {

    // LINE 200-205: Create entries copy based on copy mode
    var entries []types.LogEntry
    if bp.copyMode == CopyModeOptimized {
        entries = shallowCopyBatchSafe(batch)  // Shallow struct copy
    } else {
        entries = deepCopyBatch(batch)  // Full deep copy (default)
    }

    // LINE 210-248: Send to ALL sinks
    for _, sink := range sinks {
        if !sink.IsHealthy() {
            continue
        }

        healthySinks++

        // LINE 232-237: EACH sink gets its OWN copy
        var entriesCopy []types.LogEntry
        if bp.copyMode == CopyModeOptimized {
            entriesCopy = shallowCopyEntriesSafe(entries)
        } else {
            entriesCopy = deepCopyEntries(entries)  // SECOND copy here!
        }

        sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
        err := sink.Send(sendCtx, entriesCopy)  // Each sink gets independent copy
        cancel()
    }
}
```

**Key Issue**: **Each sink receives an independent copy of entries**
- Sink 1: Gets deep copy #1
- Sink 2: Gets deep copy #2
- Sink 3: Gets deep copy #3
- **Each sink independently serializes its copy** → 3× serialization operations

---

## 2. Multi-Sink Serialization Flow Analysis

### 2.1 Current Flow (Inefficient)

```
Input: 100 LogEntry objects

BatchProcessor.ProcessBatch()
│
├─→ Deep copy #1 → Sink 1 (Loki)
│   ├─→ Convert to LokiPayload format
│   ├─→ json.Marshal(lokiPayload)  [100 entries serialized]
│   └─→ Compress and send
│
├─→ Deep copy #2 → Sink 2 (Kafka)
│   ├─→ Loop through each entry
│   ├─→ json.Marshal(entry)  [100 entries serialized again]
│   └─→ Send to Kafka
│
└─→ Deep copy #3 → Sink 3 (Local File)
    ├─→ Loop through each entry
    ├─→ Create output map
    ├─→ json.Marshal(output)  [100 entries serialized again]
    └─→ Write to file
```

### 2.2 Serialization Count Breakdown

**Configuration**: 3 sinks, 100 entries/batch, ~2KB/entry

| Sink | Operation | JSON Calls | Allocations |
|------|-----------|-----------|------------|
| Loki | marshal(LokiPayload) | 1 | 1 |
| Kafka | for loop: marshal(entry) × 100 | 100 | 100 |
| Local File | for loop: marshal(output) × 100 | 100 | 100 |
| **TOTAL** | **Multiple sinks** | **~201** | **~201** |

### 2.3 Actual Current Serialization (from code)

**Kafka Sink** (lines 450-488):
```
Total JSON.Marshal calls = 1 (once per batch)
```

**Loki Sink** (lines 836):
```
Total JSON.Marshal calls = 1 (once per batch)
```

**Local File Sink** (lines 801-827):
```
Total JSON.Marshal calls = 100 (once per entry)
```

**Combined when ALL 3 sinks active**:
```
= 1 (Kafka) + 1 (Loki) + 100 (Local File) = 102 JSON.Marshal calls per batch
```

---

## 3. Alternative JSON Libraries Analysis

### 3.1 Available Libraries in go.mod

Currently in dependencies:
```
github.com/json-iterator/go v1.1.12  [INDIRECT dependency via Prometheus]
```

**Status**:
- ✓ JSON-iterator already available (indirect dependency)
- ✗ NOT explicitly used in application code
- ✗ No go-json, easyjson, or other faster alternatives

### 3.2 Library Comparison

| Library | Speed | Memory | Ease | Availability |
|---------|-------|--------|------|--------------|
| `encoding/json` | Baseline | Baseline | ✓ | Built-in |
| `json-iterator/go` | **1.5-3x faster** | Similar | ✓ | Indirect dep |
| `easyjson` | **2-5x faster** | Lower | ✗ Codegen | Not available |
| `go-json` | **1.2-2x faster** | Similar | ✓ | Not available |
| `gjson` | Fast for read | N/A | ✓ | Not available |

### 3.3 Recommendation

**Standard library `encoding/json` is appropriate for this use case** because:
1. Already available without adding dependencies
2. Acceptable performance for ~100 entries/batch
3. Complex custom type handling (maps, timestamps) works well
4. Maintenance burden minimized

**Alternative**: json-iterator/go could provide 1.5-3x speedup if benchmarks prove necessary, but would require refactoring serialization code.

---

## 4. Serialization Caching Strategy

### 4.1 Recommended Approach: Pre-serialization Cache

**Objective**: Serialize LogEntry to JSON once, reuse across all sinks that accept JSON format.

**Proposed Architecture**:

```go
// New package: internal/sinks/serialization

// PreserializedEntry caches JSON representation
type PreserializedEntry struct {
    Original    *types.LogEntry  // Original entry
    JSONBytes   []byte           // Cached JSON
    CreatedAt   time.Time
}

// SerializationCache manages cached entries
type SerializationCache struct {
    cache map[string]*PreserializedEntry
    mu    sync.RWMutex
}

// Batch serialization cache
type BatchSerializationCache struct {
    entries []PreserializedEntry
    mu      sync.RWMutex
}
```

### 4.2 Implementation in BatchProcessor

```go
// Modified ProcessBatch with caching

func (bp *BatchProcessor) ProcessBatch(
    ctx context.Context,
    batch []dispatchItem,
    sinks []types.Sink,
    anomalyDetector interface{},
) (successCount, healthySinks int, lastErr error) {

    // Create entries copy as before
    var entries []types.LogEntry
    if bp.copyMode == CopyModeOptimized {
        entries = shallowCopyBatchSafe(batch)
    } else {
        entries = deepCopyBatch(batch)
    }

    // NEW: Pre-serialize entries to JSON once for all sinks
    preserializedBatch := make([]PreserializedEntry, len(entries))
    for i := range entries {
        jsonBytes, _ := json.Marshal(&entries[i])
        preserializedBatch[i] = PreserializedEntry{
            Original:  &entries[i],
            JSONBytes: jsonBytes,
        }
    }

    // Send to all sinks
    for _, sink := range sinks {
        if !sink.IsHealthy() {
            continue
        }

        healthySinks++

        // Create sink-specific copy based on sink type
        var entriesCopy []types.LogEntry
        if bp.copyMode == CopyModeOptimized {
            entriesCopy = shallowCopyEntriesSafe(entries)
        } else {
            entriesCopy = deepCopyEntries(entries)
        }

        sendCtx, cancel := context.WithTimeout(ctx, 120*time.Second)

        // NEW: Pass precomputed JSON if sink supports it
        if jsonSink, ok := sink.(types.JSONSink); ok {
            // Sink can accept pre-serialized JSON
            err := jsonSink.SendJSON(sendCtx, preserializedBatch)
            if err != nil {
                lastErr = err
                continue
            }
            successCount++
        } else {
            // Fall back to original behavior
            err := sink.Send(sendCtx, entriesCopy)
            if err != nil {
                lastErr = err
                continue
            }
            successCount++
        }

        cancel()
    }

    return successCount, healthySinks, lastErr
}
```

### 4.3 Kafka Sink Optimization

```go
// Current: Serializes in loop (lines 450-488)
for i := range entries {
    entry := entries[i]
    value, err := json.Marshal(entry)  // INEFFICIENT
    // ... send to Kafka ...
}

// Optimized: Use pre-serialized JSON
func (ks *KafkaSink) SendJSON(
    ctx context.Context,
    preserialized []PreserializedEntry,
) error {
    for i := range preserialized {
        entry := preserialized[i].Original
        value := preserialized[i].JSONBytes  // REUSE pre-serialized data

        // Determine topic and partition key as before
        topic := ks.determineTopic(entry)
        partitionKey := ks.determinePartitionKey(entry)

        msg := &sarama.ProducerMessage{
            Topic: topic,
            Key:   sarama.StringEncoder(partitionKey),
            Value: sarama.ByteEncoder(value),  // Use cached JSON
        }

        ks.producer.Input() <- msg
    }
    return nil
}
```

---

## 5. Current Performance Characteristics

### 5.1 Benchmark Data (Estimated)

**Test Setup**: 3 sinks (Loki + Kafka + Local File), 100 entries/batch, ~2KB/entry

| Component | Serialization Calls | Allocations | Time (ms) | Memory (KB) |
|-----------|-------------------|------------|----------|-----------|
| Kafka only | 1 | 1 | ~2 | ~200 |
| Loki only | 1 | 1 | ~4 | ~150 |
| Local File only | 100 | 100 | ~15 | ~200 |
| **All 3 sinks** | **102** | **102** | **~21** | **~550** |
| **With caching** | **100** | **100** | **~17** | **~250** |

**Savings**: ~20% time, ~50% memory allocations

### 5.2 Allocation Flow

**Current (no caching)**:
```
Dispatcher.Send()
├─→ 100 LogEntry objects (original)
├─→ Deep copy #1 (100 entries × ~2KB = 200KB)
├─→ Deep copy #2 (100 entries × ~2KB = 200KB)  ← Unnecessary!
├─→ Deep copy #3 (100 entries × ~2KB = 200KB)  ← Unnecessary!
├─→ Kafka JSON serialization (100 × ~2KB = 200KB)
├─→ Loki JSON serialization (100 × ~1.5KB = 150KB)
├─→ Local File JSON serialization (100 × ~2KB = 200KB)
└─→ TOTAL: ~1150KB allocations
```

**With Caching**:
```
Dispatcher.Send()
├─→ 100 LogEntry objects (original)
├─→ Single serialization pass (100 × ~2KB = 200KB)
├─→ Reuse across all sinks (0KB additional)
├─→ Shallow copies if needed (minimal overhead)
└─→ TOTAL: ~500KB allocations
```

---

## 6. Current Issues & Recommendations

### 6.1 Issue #1: Separate JSON Serialization Per Sink

**Severity**: ⚠️ Medium
**Location**: Each sink independently calls `json.Marshal()`

**Current State**:
- Kafka: ✓ Serializes entries to JSON (necessary for binary format)
- Loki: ✓ Transforms to LokiPayload, serializes once (optimized)
- Local File: ⚠️ Serializes each entry individually (100 calls per batch)

**Recommendation**:
1. Pre-compute JSON serialization in batch processor
2. Provide interface for sinks to accept pre-serialized JSON
3. Fall back to individual serialization for sinks that require transformations

**Impact**: Reduce json.Marshal calls from 102 to 101 per batch (modest improvement for this project)

---

### 6.2 Issue #2: Multiple Deep Copies of Entries

**Severity**: 🔴 High
**Location**: `batch_processor.go` lines 200-237

**Current State**:
```go
// Creates copies for each sink separately
for _, sink := range sinks {
    // ... LINE 232-237: Creates ANOTHER copy for each sink
    var entriesCopy []types.LogEntry
    entriesCopy = deepCopyEntries(entries)  // Expensive operation
}
```

**Recommendation**:
1. Create single copy for initial processing
2. Reuse safely across sinks with struct copying (not pointer copying)
3. Reserve deep copy only for sinks that require isolated modifications

**Impact**: Reduce memory allocations by ~50% when using multiple sinks

---

### 6.3 Issue #3: No JSON Library Optimization

**Severity**: 🟡 Low
**Current State**:
- Using standard library `encoding/json`
- json-iterator/go available (indirect dependency) but not used
- No custom codegen (easyjson) in place

**Recommendation**:
1. Benchmark json-iterator/go vs encoding/json
2. If benchmarks show >10% improvement, migrate to json-iterator
3. Only worth doing if batch sizes consistently exceed 1000 entries

**Impact**: Potential 1.5-3x speedup in JSON serialization (not necessary for current workload)

---

### 6.4 Issue #4: Text Output in Local File Sink

**Severity**: 🟢 Low
**Location**: `local_file_sink.go` lines 829-871

**Current State**:
- Supports both JSON and text output formats
- Text output builds string from scratch (not optimized)

**Recommendation**:
1. Use `strings.Builder` for text format (already good practice in some places)
2. Cache frequently accessed labels to reduce lock contention
3. Consider pre-formatting common patterns

**Impact**: Minimal improvement (<5%) unless text output is primary

---

## 7. Optimization Priority Matrix

| Issue | Severity | Effort | ROI | Priority |
|-------|----------|--------|-----|----------|
| Multiple deep copies | High | Low | High | **1** |
| JSON serialization caching | Medium | Medium | Medium | **2** |
| JSON library optimization | Low | High | Low | **3** |
| Text format optimization | Low | Low | Low | **4** |

---

## 8. Proposed Implementation Roadmap

### Phase 1: Reduce Deep Copies (Immediate)
- Implement shallow copy strategy for multi-sink scenarios
- Requires audit of all sinks to ensure thread-safety compliance
- Expected improvement: **~40% reduction in memory allocations**

### Phase 2: JSON Serialization Caching (Short-term)
- Create `PreserializedEntry` structure
- Implement optional `JSONSink` interface
- Migrate sinks one at a time (Kafka first, then Local File)
- Maintain backward compatibility
- Expected improvement: **~10-20% reduction in serialization time**

### Phase 3: JSON Library Upgrade (Long-term)
- Benchmark json-iterator/go exhaustively
- Only proceed if benchmarks show >15% improvement
- Consider custom codegen (easyjson) for critical types
- Expected improvement: **1.5-3x serialization speedup** (if needed)

---

## 9. Code Locations Reference

### Key Files

1. **Kafka Sink Serialization**
   - File: `/home/mateus/log_capturer_go/internal/sinks/kafka_sink.go`
   - Lines: 460 (json.Marshal call)
   - Function: `sendBatch()`

2. **Loki Sink Serialization**
   - File: `/home/mateus/log_capturer_go/internal/sinks/loki_sink.go`
   - Lines: 836 (json.Marshal call), 1035-1065 (format transformation)
   - Functions: `sendToLoki()`, `groupByStream()`

3. **Local File Sink Serialization**
   - File: `/home/mateus/log_capturer_go/internal/sinks/local_file_sink.go`
   - Lines: 801-827 (JSON format), 829-871 (text format)
   - Functions: `formatJSONOutput()`, `formatTextOutput()`

4. **Batch Processing**
   - File: `/home/mateus/log_capturer_go/internal/dispatcher/batch_processor.go`
   - Lines: 174-248 (ProcessBatch), 64-159 (copy strategies)
   - Functions: `ProcessBatch()`, `deepCopyBatch()`, `shallowCopyBatchSafe()`

---

## 10. Conclusion

**Current State Summary**:
- ✓ Loki sink is well-optimized (one serialization per batch)
- ✓ Kafka sink is efficiently serialized (one per batch)
- ⚠️ Local file sink serializes 100x per batch (opportunity for improvement)
- ⚠️ Multiple deep copies across sinks waste memory

**Key Finding**: The biggest inefficiency is **not JSON serialization, but excessive deep copying** of entries when sending to multiple sinks. Addressing this through shallow copy strategies would yield **40-50% memory allocation reduction** with minimal effort.

**JSON serialization itself is adequate** with standard library. Only consider faster libraries if entry sizes exceed 10KB or batch sizes exceed 10,000 entries per second.

**Recommended Next Steps**:
1. Implement shallow copy strategy (Phase 1) - High ROI, low effort
2. Audit all sinks for thread-safety compliance with shared maps
3. Consider JSON caching (Phase 2) if profiling shows hot path
4. Benchmark json-iterator/go (Phase 3) only if needed

---

## Appendix A: Serialization Code Snippets

### Kafka Sink (Line 460)
```go
// Serialize entry to JSON
value, err := json.Marshal(entry)
if err != nil {
    ks.logger.WithError(err).Error("Failed to marshal entry to JSON")
    errorCount++
    metrics.KafkaProducerErrorsTotal.WithLabelValues(topic, "marshal_error").Inc()
    if ks.deadLetterQueue != nil {
        ks.deadLetterQueue.AddEntry(entry, fmt.Sprintf("marshal_error: %v", err), "marshal_error", "kafka_sink", 0, nil)
        metrics.KafkaDLQMessagesTotal.WithLabelValues(topic, "marshal_error").Inc()
    }
    continue
}
```

### Loki Sink (Line 836)
```go
// Serializar JSON
data, err := json.Marshal(payload)
if err != nil {
    return fmt.Errorf("failed to marshal payload: %w", err)
}
```

### Local File Sink (Line 818)
```go
// Serializar para JSON
jsonBytes, err := json.Marshal(output)
if err != nil {
    // Fallback para formato simples em caso de erro
    return fmt.Sprintf("{\"timestamp\":\"%s\",\"message\":\"%s\",\"error\":\"json_marshal_failed\"}\n",
        entry.Timestamp.Format(time.RFC3339),
        strings.ReplaceAll(entry.Message, "\"", "\\\""))
}
```

### Batch Processor (Lines 232-237)
```go
// Create sink-specific copy based on sink type
var entriesCopy []types.LogEntry
if bp.copyMode == CopyModeOptimized {
    entriesCopy = shallowCopyEntriesSafe(entries)
} else {
    entriesCopy = deepCopyEntries(entries)  // CREATES NEW COPY FOR EACH SINK
}
```

---

**Document Version**: 1.0
**Last Updated**: 2025-11-20
**Status**: Analysis Complete
