package anomaly

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestAnomalyDetectorStartStop tests basic Start/Stop functionality (C2)
func TestAnomalyDetectorStartStop(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "1h",
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Start detector
	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	// Wait a bit to ensure goroutine is running
	time.Sleep(100 * time.Millisecond)

	// Stop detector - should NOT hang (C2 fix verification)
	done := make(chan struct{})
	go func() {
		err := detector.Stop()
		if err != nil {
			t.Errorf("Failed to stop detector: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ Detector stopped successfully without hanging")
	case <-time.After(6 * time.Second):
		t.Fatal("CONTEXT LEAK: Stop() timed out - goroutines didn't stop!")
	}
}

// TestAnomalyDetectorContextCancellation tests that context is canceled (C2)
func TestAnomalyDetectorContextCancellation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "100ms", // Short interval for testing
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Verify context is not canceled initially
	select {
	case <-detector.ctx.Done():
		t.Fatal("Context should not be canceled before Stop()")
	default:
		// Good - context not canceled
	}

	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	// Verify context is still not canceled after Start
	select {
	case <-detector.ctx.Done():
		t.Fatal("Context should not be canceled after Start()")
	default:
		// Good
	}

	// Stop detector
	err = detector.Stop()
	if err != nil {
		t.Fatalf("Failed to stop detector: %v", err)
	}

	// Verify context IS canceled after Stop (C2 fix)
	select {
	case <-detector.ctx.Done():
		t.Log("✓ Context properly canceled after Stop()")
	case <-time.After(1 * time.Second):
		t.Fatal("CONTEXT LEAK: Context was not canceled after Stop()")
	}
}

// TestAnomalyDetectorGoroutineCleanup tests that goroutines actually stop (C2)
func TestAnomalyDetectorGoroutineCleanup(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Get initial goroutine count
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "50ms",
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	// Wait for goroutine to be running
	time.Sleep(100 * time.Millisecond)
	afterStartGoroutines := runtime.NumGoroutine()

	if afterStartGoroutines <= initialGoroutines {
		t.Logf("Warning: Expected more goroutines after Start (before: %d, after: %d)",
			initialGoroutines, afterStartGoroutines)
	}

	// Stop detector
	err = detector.Stop()
	if err != nil {
		t.Fatalf("Failed to stop detector: %v", err)
	}

	// Wait for goroutines to fully clean up
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()

	// Allow small variance due to test framework goroutines
	goroutineLeak := finalGoroutines - initialGoroutines
	if goroutineLeak > 2 {
		t.Errorf("GOROUTINE LEAK: Started with %d goroutines, ended with %d (%d leaked)",
			initialGoroutines, finalGoroutines, goroutineLeak)
	} else {
		t.Logf("✓ No goroutine leak detected (initial: %d, final: %d, diff: %d)",
			initialGoroutines, finalGoroutines, goroutineLeak)
	}
}

// TestAnomalyDetectorMultipleStartStop tests multiple Start/Stop cycles (C2)
func TestAnomalyDetectorMultipleStartStop(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "100ms",
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Try multiple Start/Stop cycles
	for i := 0; i < 3; i++ {
		t.Logf("Cycle %d: Starting detector", i+1)

		err = detector.Start()
		if err != nil {
			t.Fatalf("Cycle %d: Failed to start: %v", i+1, err)
		}

		time.Sleep(150 * time.Millisecond)

		t.Logf("Cycle %d: Stopping detector", i+1)

		done := make(chan struct{})
		go func() {
			err := detector.Stop()
			if err != nil {
				t.Errorf("Cycle %d: Failed to stop: %v", i+1, err)
			}
			close(done)
		}()

		select {
		case <-done:
			t.Logf("✓ Cycle %d: Stopped successfully", i+1)
		case <-time.After(6 * time.Second):
			t.Fatalf("Cycle %d: Stop() timed out", i+1)
		}

		// Need to recreate detector for next cycle since context is canceled
		if i < 2 {
			detector, err = NewAnomalyDetector(config, logger)
			if err != nil {
				t.Fatalf("Cycle %d: Failed to recreate detector: %v", i+1, err)
			}
		}
	}

	t.Log("✓ Multiple Start/Stop cycles completed without hanging")
}

// TestAnomalyDetectorDisabledNoLeak tests that disabled detector doesn't leak (C2)
func TestAnomalyDetectorDisabledNoLeak(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled: false, // Disabled
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create disabled detector: %v", err)
	}

	// Start should be no-op
	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start disabled detector: %v", err)
	}

	// Stop should not hang even when disabled
	done := make(chan struct{})
	go func() {
		err := detector.Stop()
		if err != nil {
			t.Errorf("Failed to stop disabled detector: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ Disabled detector Stop() doesn't hang")
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() timed out on disabled detector")
	}

	// Verify context and cancel are set even when disabled (C2 fix)
	if detector.ctx == nil {
		t.Error("Context should be set even for disabled detector")
	}
	if detector.cancel == nil {
		t.Error("Cancel function should be set even for disabled detector")
	}
}

// TestAnomalyDetectorStopWithTimeout tests timeout mechanism in Stop (C2)
func TestAnomalyDetectorStopWithTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Allow warning logs

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "10ms",
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Stop should complete even with timeout safety mechanism
	startTime := time.Now()
	err = detector.Stop()
	duration := time.Since(startTime)

	if err != nil {
		t.Fatalf("Failed to stop detector: %v", err)
	}

	// Should stop quickly (well under 5 second timeout)
	if duration > 5*time.Second {
		t.Errorf("Stop took too long: %v", duration)
	} else {
		t.Logf("✓ Stop completed in %v (well under 5s timeout)", duration)
	}
}

// TestAnomalyDetectorConcurrentStops tests concurrent Stop calls (C2)
func TestAnomalyDetectorConcurrentStops(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "100ms",
		MinTrainingSamples: 10,
		MaxTrainingSamples: 100,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	err = detector.Start()
	if err != nil {
		t.Fatalf("Failed to start detector: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Call Stop from multiple goroutines concurrently
	var wg sync.WaitGroup
	stopCount := 5

	for i := 0; i < stopCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := detector.Stop()
			if err != nil {
				t.Logf("Goroutine %d: Stop returned error: %v", id, err)
			}
		}(i)
	}

	// Wait for all Stops with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Logf("✓ All %d concurrent Stop() calls completed", stopCount)
	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent Stop() calls timed out")
	}
}

// TestTrainingBufferSizeLimit tests that buffer doesn't exceed MaxTrainingSamples (C10)
func TestTrainingBufferSizeLimit(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	maxSamples := 100
	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "1h",
		MinTrainingSamples: 10,
		MaxTrainingSamples: maxSamples,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Add many more entries than the limit
	entriesToAdd := maxSamples * 3

	for i := 0; i < entriesToAdd; i++ {
		entry := ProcessedLogEntry{
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   "test-001",
			Message:    "test message",
			Features:   make(map[string]float64),
		}
		detector.addToTrainingBuffer(entry)

		// Check buffer never exceeds limit
		detector.trainingMux.RLock()
		currentSize := len(detector.trainingBuffer)
		detector.trainingMux.RUnlock()

		if currentSize > maxSamples {
			t.Fatalf("Buffer size %d exceeds max %d after adding entry %d",
				currentSize, maxSamples, i+1)
		}
	}

	// Verify final size
	detector.trainingMux.RLock()
	finalSize := len(detector.trainingBuffer)
	detector.trainingMux.RUnlock()

	if finalSize != maxSamples {
		t.Errorf("Expected final buffer size %d, got %d", maxSamples, finalSize)
	}

	t.Logf("✓ Buffer size correctly limited to %d after adding %d entries",
		finalSize, entriesToAdd)
}

// TestTrainingBufferMemoryReallocate tests that buffer is reallocated, not resliced (C10)
func TestTrainingBufferMemoryReallocate(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	maxSamples := 50
	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "1h",
		MinTrainingSamples: 10,
		MaxTrainingSamples: maxSamples,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Add entries to exceed limit
	for i := 0; i < maxSamples*2; i++ {
		entry := ProcessedLogEntry{
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   "test-001",
			Message:    "test message with some data",
			Features:   map[string]float64{"feature1": float64(i)},
		}
		detector.addToTrainingBuffer(entry)
	}

	// Check buffer capacity (should be equal to length after reallocation)
	detector.trainingMux.RLock()
	bufferLen := len(detector.trainingBuffer)
	bufferCap := cap(detector.trainingBuffer)
	detector.trainingMux.RUnlock()

	if bufferLen != maxSamples {
		t.Errorf("Expected buffer length %d, got %d", maxSamples, bufferLen)
	}

	// After reallocation, capacity should be close to length (not much bigger)
	// Allow some variance due to Go's append behavior
	if bufferCap > bufferLen*2 {
		t.Errorf("Buffer capacity %d is much larger than length %d - possible memory leak!",
			bufferCap, bufferLen)
	}

	t.Logf("✓ Buffer properly reallocated: len=%d, cap=%d (ratio: %.2fx)",
		bufferLen, bufferCap, float64(bufferCap)/float64(bufferLen))
}

// TestTrainingBufferConcurrentAccess tests concurrent adds to buffer (C10)
func TestTrainingBufferConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	maxSamples := 100
	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "1h",
		MinTrainingSamples: 10,
		MaxTrainingSamples: maxSamples,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Add entries from multiple goroutines
	const goroutines = 10
	const entriesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				entry := ProcessedLogEntry{
					Timestamp:  time.Now(),
					SourceType: "test",
					SourceID:   "test-concurrent",
					Message:    "concurrent test message",
					Features:   map[string]float64{"goroutine": float64(id)},
				}
				detector.addToTrainingBuffer(entry)
			}
		}(i)
	}

	wg.Wait()

	// Verify buffer size is still within limit
	detector.trainingMux.RLock()
	finalSize := len(detector.trainingBuffer)
	detector.trainingMux.RUnlock()

	if finalSize > maxSamples {
		t.Errorf("Buffer size %d exceeds max %d after concurrent adds", finalSize, maxSamples)
	}

	if finalSize != maxSamples {
		t.Errorf("Expected buffer size %d, got %d", maxSamples, finalSize)
	}

	t.Logf("✓ Concurrent access safe: %d goroutines added %d entries each, final size: %d",
		goroutines, entriesPerGoroutine, finalSize)
}

// TestTrainingBufferOldestEntriesRemoved tests that oldest entries are removed (C10)
func TestTrainingBufferOldestEntriesRemoved(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	maxSamples := 10
	config := Config{
		Enabled:            true,
		Algorithm:          "statistical",
		TrainingInterval:   "1h",
		MinTrainingSamples: 5,
		MaxTrainingSamples: maxSamples,
	}

	detector, err := NewAnomalyDetector(config, logger)
	if err != nil {
		t.Fatalf("Failed to create detector: %v", err)
	}

	// Add entries with unique IDs
	totalEntries := maxSamples * 2
	for i := 0; i < totalEntries; i++ {
		entry := ProcessedLogEntry{
			Timestamp:  time.Now(),
			SourceType: "test",
			SourceID:   "test-" + string(rune('A'+i)), // A, B, C, ...
			Message:    "test message",
			Features:   map[string]float64{"id": float64(i)},
		}
		detector.addToTrainingBuffer(entry)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	// Verify that only the LAST maxSamples entries remain
	detector.trainingMux.RLock()
	bufferCopy := make([]ProcessedLogEntry, len(detector.trainingBuffer))
	copy(bufferCopy, detector.trainingBuffer)
	detector.trainingMux.RUnlock()

	if len(bufferCopy) != maxSamples {
		t.Fatalf("Expected %d entries, got %d", maxSamples, len(bufferCopy))
	}

	// First entry in buffer should have id >= (totalEntries - maxSamples)
	firstID := int(bufferCopy[0].Features["id"])
	expectedFirstID := totalEntries - maxSamples

	if firstID != expectedFirstID {
		t.Errorf("Expected first entry to have id %d, got %d", expectedFirstID, firstID)
	}

	// Last entry should have id == totalEntries-1
	lastID := int(bufferCopy[len(bufferCopy)-1].Features["id"])
	expectedLastID := totalEntries - 1

	if lastID != expectedLastID {
		t.Errorf("Expected last entry to have id %d, got %d", expectedLastID, lastID)
	}

	t.Logf("✓ Oldest entries correctly removed: first_id=%d, last_id=%d, count=%d",
		firstID, lastID, len(bufferCopy))
}
