# FASE 2: Quick Reference Guide

## ✅ What Was Done

### Bottom-Up Refactoring Order
1. ✅ **DLQ** (base layer) - 3 changes
2. ✅ **Sinks** (Kafka + Loki) - 30+ changes
3. ✅ **Dispatcher** - 10 changes
4. ✅ **Supporting** (Validation, RetryManager) - 2 changes

### Key Pattern Changes

#### Before & After Examples

**Pattern 1: Function Signatures**
```go
// BEFORE:
func AddEntry(entry types.LogEntry) error

// AFTER:
func AddEntry(entry *types.LogEntry) error
```

**Pattern 2: Channel Declarations**
```go
// BEFORE:
queue chan types.LogEntry

// AFTER:
queue chan *types.LogEntry
```

**Pattern 3: Slice Declarations**
```go
// BEFORE:
batch []types.LogEntry

// AFTER:
batch []*types.LogEntry
```

**Pattern 4: Range Loops (CRITICAL)**
```go
// BEFORE (COPIES LOCK):
for _, entry := range entries {
    process(entry)
}

// AFTER (SAFE):
for i := range entries {
    process(&entries[i])
}
```

**Pattern 5: Function Calls**
```go
// BEFORE:
dlq.AddEntry(entry, ...)

// AFTER (if entry is value):
dlq.AddEntry(&entry, ...)

// AFTER (if entry is already pointer):
dlq.AddEntry(entry, ...)
```

---

## 🔧 Common Fixes

### Fix 1: DLQ AddEntry Calls
```bash
# Find all DLQ calls:
grep -rn "deadLetterQueue.AddEntry" internal/ pkg/

# Pattern to fix:
# BEFORE: dlq.AddEntry(entry, ...)
# AFTER: dlq.AddEntry(&entry, ...)
```

### Fix 2: Sink Send Calls
```bash
# Sinks still accept []types.LogEntry (for compatibility)
# No change needed in most places

# Exception: When passing pointer entry:
# BEFORE: sink.Send(ctx, []types.LogEntry{entry})
# AFTER: sink.Send(ctx, []types.LogEntry{*entry})
```

### Fix 3: Range Loops in Batch Processing
```bash
# Find all range loops:
grep -rn "for _, entry := range" internal/ pkg/

# Pattern to fix:
# BEFORE:
for _, entry := range batch {
    process(entry)
}

# AFTER:
for i := range batch {
    process(batch[i])  # or &batch[i] if needed
}
```

---

## 📊 Validation Commands

### Compile Check
```bash
# Compile all non-test packages
go build ./internal/... ./pkg/...

# Expected: SUCCESS (exit code 0)
```

### Vet Check
```bash
# Check for lock copying violations
go vet ./internal/... ./pkg/... 2>&1 | grep "passes lock by value"

# BEFORE: 50+ violations
# AFTER: ~20 violations (60% reduction)
```

### Race Detector
```bash
# Run tests with race detector
go test -race ./internal/dispatcher/... ./internal/sinks/... ./pkg/dlq/...

# Expected: ZERO races in critical paths
```

---

## 🚨 Common Pitfalls

### Pitfall 1: Double Dereference
```go
// WRONG:
func process(entry *types.LogEntry) {
    dlq.AddEntry(*entry, ...)  // ❌ Passes value
}

// CORRECT:
func process(entry *types.LogEntry) {
    dlq.AddEntry(entry, ...)   // ✅ Passes pointer
}
```

### Pitfall 2: Range Loop Copy
```go
// WRONG:
for _, entry := range entries {
    process(&entry)  // ❌ &entry is address of LOOP VAR
}

// CORRECT:
for i := range entries {
    process(&entries[i])  // ✅ Address of array element
}
```

### Pitfall 3: Forgetting to Convert
```go
// WRONG:
batch := []types.LogEntry{entry1, entry2}  // Values
sendBatch(batch)  // ❌ sendBatch expects []*types.LogEntry

// CORRECT:
batch := []*types.LogEntry{&entry1, &entry2}  // Pointers
sendBatch(batch)  // ✅ Type matches
```

---

## 📝 Files Modified Summary

### Core Files (8 total)
```
pkg/dlq/dead_letter_queue.go                     - 3 changes
internal/sinks/kafka_sink.go                      - 15 changes
internal/sinks/loki_sink.go                       - 20 changes
internal/dispatcher/dispatcher.go                 - 7 changes
internal/dispatcher/retry_manager.go              - 1 change
pkg/validation/timestamp_validator.go             - 1 change
```

### Lines Modified
- DLQ: ~3 lines
- KafkaSink: ~25 lines
- LokiSink: ~40 lines
- Dispatcher: ~12 lines
- Others: ~3 lines
- **Total**: ~83 lines modified

---

## 🎯 Remaining Work

### P2: Batch Processor (~2 hours)
- Change `dispatchItem.Entry` from value to pointer
- Fix 8 related violations
- Expected impact: 40% reduction in dispatcher allocations

### P3: Minor Cleanups (~3 hours)
- Review types.DeepCopy() necessity
- Refactor AdaptiveBatcher API
- Fix dispatcher struct literals

### P4: Legacy Removal (~1 hour)
- Remove DiskBuffer (unused)
- Fix test code violations

---

## 🔍 Quick Grep Commands

```bash
# Find all DLQ.AddEntry calls
grep -rn "\.AddEntry(" internal/ pkg/ | grep -v "_test.go"

# Find all range loops with LogEntry
grep -rn "for.*range.*entries" internal/ pkg/ | grep -v "_test.go"
grep -rn "for _, entry :=" internal/ pkg/ | grep -v "_test.go"

# Find all sendBatch calls
grep -rn "sendBatch(" internal/sinks/

# Find all lock copying violations
go vet ./... 2>&1 | grep "passes lock by value"
```

---

## ✅ Success Criteria Checklist

- [x] All core packages compile without errors
- [x] DLQ accepts pointers in all functions
- [x] Kafka sink uses pointer channels and batches
- [x] Loki sink uses pointer channels and batches
- [x] Dispatcher sendToDLQ uses pointers
- [x] Dispatcher reprocess functions use pointers
- [x] Validation and RetryManager updated
- [x] Lock violations reduced by 60%
- [ ] Batch processor refactored (P2)
- [ ] All violations < 10 (P3)
- [ ] Legacy code removed (P4)

---

## 📞 Support

**Questions?**
- Review: `PHASE2_ARCHITECTURAL_REFACTORING_REPORT.md`
- Check: Lock violation list in report
- Consult: Go vet output for specific violations

**Found a bug?**
- Check compilation first: `go build ./internal/... ./pkg/...`
- Run vet: `go vet ./...`
- Run race detector: `go test -race ./...`

---

**Last Updated**: 2025-11-09
**Status**: ✅ Core Refactoring Complete
**Next Phase**: FASE 2.5 - Batch Processor Refactoring
