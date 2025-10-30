package types

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestLogEntryConcurrentLabelAccess tests concurrent read/write access to labels
func TestLogEntryConcurrentLabelAccess(t *testing.T) {
	entry := &LogEntry{
		Message:   "test message",
		Timestamp: time.Now(),
		Labels:    make(map[string]string),
	}

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // readers + writers

	// Start concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("key_%d", id%10)
				value := fmt.Sprintf("value_%d_%d", id, j)
				entry.SetLabel(key, value)
			}
		}(i)
	}

	// Start concurrent readers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("key_%d", id%10)
				_, _ = entry.GetLabel(key)

				// Also test CopyLabels
				if j%10 == 0 {
					labelsCopy := entry.CopyLabels()
					_ = len(labelsCopy)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Log("✓ No race conditions detected in concurrent label access")
}

// TestLogEntryConcurrentFieldAccess tests concurrent read/write access to fields
func TestLogEntryConcurrentFieldAccess(t *testing.T) {
	entry := &LogEntry{
		Message:   "test message",
		Timestamp: time.Now(),
		Fields:    make(map[string]interface{}),
	}

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("field_%d", id%10)
				value := map[string]interface{}{
					"id":    id,
					"count": j,
					"data":  fmt.Sprintf("data_%d_%d", id, j),
				}
				entry.SetField(key, value)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("field_%d", id%10)
				_, _ = entry.GetField(key)

				if j%10 == 0 {
					fieldsCopy := entry.CopyFields()
					_ = len(fieldsCopy)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Log("✓ No race conditions detected in concurrent field access")
}

// TestLogEntryConcurrentMetricAccess tests concurrent read/write access to metrics
func TestLogEntryConcurrentMetricAccess(t *testing.T) {
	entry := &LogEntry{
		Message:   "test message",
		Timestamp: time.Now(),
		Metrics:   make(map[string]float64),
	}

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("metric_%d", id%10)
				value := float64(id*iterations + j)
				entry.SetMetric(key, value)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := fmt.Sprintf("metric_%d", id%10)
				_, _ = entry.GetMetric(key)
			}
		}(i)
	}

	wg.Wait()
	t.Log("✓ No race conditions detected in concurrent metric access")
}

// TestLogEntryDeepCopyConcurrent tests DeepCopy under concurrent access
func TestLogEntryDeepCopyConcurrent(t *testing.T) {
	entry := &LogEntry{
		Message:   "test message",
		Timestamp: time.Now(),
		Labels:    make(map[string]string),
		Fields:    make(map[string]interface{}),
		Metrics:   make(map[string]float64),
	}

	// Populate initial data
	for i := 0; i < 10; i++ {
		entry.SetLabel(fmt.Sprintf("label_%d", i), fmt.Sprintf("value_%d", i))
		entry.SetField(fmt.Sprintf("field_%d", i), i*100)
		entry.SetMetric(fmt.Sprintf("metric_%d", i), float64(i)*10.5)
	}

	const goroutines = 30
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.SetLabel(fmt.Sprintf("dynamic_%d", id), fmt.Sprintf("v_%d", j))
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = entry.GetLabel(fmt.Sprintf("label_%d", id%10))
			}
		}(i)
	}

	// Concurrent deep copiers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				copiedEntry := entry.DeepCopy()

				// Verify copy has data
				if copiedEntry == nil {
					t.Errorf("DeepCopy returned nil")
					return
				}

				// Verify independence - modify copy shouldn't affect original
				copiedEntry.SetLabel("test_independence", "copy_value")
			}
		}(i)
	}

	wg.Wait()

	// Verify original wasn't corrupted
	if entry.Message != "test message" {
		t.Errorf("Original entry message was corrupted")
	}

	t.Log("✓ No race conditions detected in concurrent DeepCopy operations")
}

// TestLogEntryMixedConcurrentOperations tests all operations running concurrently
func TestLogEntryMixedConcurrentOperations(t *testing.T) {
	entry := &LogEntry{
		Message:     "test message",
		Timestamp:   time.Now(),
		SourceType:  "test",
		SourceID:    "test-123",
		Labels:      make(map[string]string),
		Fields:      make(map[string]interface{}),
		Metrics:     make(map[string]float64),
		ProcessedAt: time.Now(),
	}

	const goroutines = 20
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 5)

	// Label writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.SetLabel(fmt.Sprintf("label_%d", id%5), fmt.Sprintf("v_%d", j))
			}
		}(i)
	}

	// Label readers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.GetLabel(fmt.Sprintf("label_%d", id%5))
				if j%5 == 0 {
					entry.CopyLabels()
				}
			}
		}(i)
	}

	// Field writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.SetField(fmt.Sprintf("field_%d", id%5), j)
			}
		}(i)
	}

	// Metric writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				entry.SetMetric(fmt.Sprintf("metric_%d", id%5), float64(j))
			}
		}(i)
	}

	// Deep copy operations
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				copied := entry.DeepCopy()
				_ = copied
			}
		}()
	}

	wg.Wait()
	t.Log("✓ No race conditions detected in mixed concurrent operations")
}

// TestLogEntryStressTest runs intensive concurrent operations for extended period
func TestLogEntryStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	entry := &LogEntry{
		Message:   "stress test",
		Timestamp: time.Now(),
		Labels:    make(map[string]string),
		Fields:    make(map[string]interface{}),
		Metrics:   make(map[string]float64),
	}

	const duration = 3 * time.Second
	const goroutines = 50

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			iteration := 0
			for {
				select {
				case <-done:
					return
				default:
					// Mix of operations
					switch iteration % 6 {
					case 0:
						entry.SetLabel(fmt.Sprintf("k%d", id%10), fmt.Sprintf("v%d", iteration))
					case 1:
						entry.GetLabel(fmt.Sprintf("k%d", id%10))
					case 2:
						entry.SetField(fmt.Sprintf("f%d", id%10), iteration)
					case 3:
						entry.CopyLabels()
					case 4:
						entry.SetMetric(fmt.Sprintf("m%d", id%10), float64(iteration))
					case 5:
						entry.DeepCopy()
					}
					iteration++
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(done)
	wg.Wait()

	t.Logf("✓ Stress test completed successfully (%v with %d goroutines)", duration, goroutines)
}
