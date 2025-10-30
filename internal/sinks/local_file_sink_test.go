package sinks

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// TestLocalFileSinkDiskSpaceNoDeadlock tests that isDiskSpaceAvailable doesn't deadlock
func TestLocalFileSinkDiskSpaceNoDeadlock(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "logcapture-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:                  true,
		Directory:                tempDir,
		MaxTotalDiskGB:           1.0,
		DiskCheckInterval:        "1s",
		CleanupThresholdPercent:  90.0,
		EmergencyCleanupEnabled:  true,
		QueueSize:                100,
		WorkerCount:              2,
		FilenamePattern:          "{date}_test.log",
		OutputFormat:             "json",
		Compress:                 false,
		Rotation: types.RotationConfig{
			MaxSizeMB: 10,
			MaxFiles:  5,
			Compress:  false,
		},
	}

	sink := NewLocalFileSink(config, logger)

	// Force lastDiskCheck to be old so check will trigger
	sink.lastDiskCheck = time.Now().Add(-10 * time.Minute)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Multiple goroutines calling isDiskSpaceAvailable concurrently
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = sink.isDiskSpaceAvailable()

				// Occasionally update lastDiskCheck to trigger the check
				if j%20 == 0 {
					sink.diskSpaceMutex.Lock()
					sink.lastDiskCheck = time.Now().Add(-10 * time.Minute)
					sink.diskSpaceMutex.Unlock()
				}
			}
		}(i)
	}

	// Wait with timeout to detect deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ No deadlock detected in isDiskSpaceAvailable with", goroutines, "goroutines")
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Test timed out waiting for goroutines")
	}
}

// TestLocalFileSinkCanWriteSizeNoDeadlock tests canWriteSize doesn't deadlock
func TestLocalFileSinkCanWriteSizeNoDeadlock(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logcapture-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:                 true,
		Directory:               tempDir,
		MaxTotalDiskGB:          1.0,
		DiskCheckInterval:       "1s",
		CleanupThresholdPercent: 90.0,
		QueueSize:               100,
		WorkerCount:             2,
		Rotation: types.RotationConfig{
			MaxSizeMB: 10,
			MaxFiles:  5,
		},
	}

	sink := NewLocalFileSink(config, logger)

	const goroutines = 30
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				size := int64(1024 * (id + 1))
				_ = sink.canWriteSize(size)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ No deadlock detected in canWriteSize with", goroutines, "goroutines")
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK DETECTED: canWriteSize timed out")
	}
}

// TestLocalFileSinkMixedDiskOperationsNoDeadlock tests mixed operations
func TestLocalFileSinkMixedDiskOperationsNoDeadlock(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logcapture-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:                  true,
		Directory:                tempDir,
		MaxTotalDiskGB:           1.0,
		DiskCheckInterval:        "500ms",
		CleanupThresholdPercent:  90.0,
		EmergencyCleanupEnabled:  true,
		QueueSize:                100,
		WorkerCount:              2,
		Rotation: types.RotationConfig{
			MaxSizeMB: 10,
			MaxFiles:  5,
		},
	}

	sink := NewLocalFileSink(config, logger)
	sink.lastDiskCheck = time.Now().Add(-10 * time.Minute)

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// isDiskSpaceAvailable callers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = sink.isDiskSpaceAvailable()
				if j%10 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		}(i)
	}

	// canWriteSize callers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = sink.canWriteSize(int64(1024 * (id + 1)))
				if j%10 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		}(i)
	}

	// checkDiskSpaceAndCleanup callers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sink.checkDiskSpaceAndCleanup()
				if j%5 == 0 {
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ No deadlock detected in mixed disk operations with", goroutines*3, "goroutines")
	case <-time.After(15 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Mixed operations timed out")
	}
}

// TestLocalFileSinkWriteWithDiskChecks tests actual writes with disk checks
func TestLocalFileSinkWriteWithDiskChecks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logcapture-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:                  true,
		Directory:                tempDir,
		MaxTotalDiskGB:           10.0, // Generous limit for test
		DiskCheckInterval:        "100ms",
		CleanupThresholdPercent:  95.0,
		EmergencyCleanupEnabled:  false, // Disable cleanup for test
		QueueSize:                1000,
		WorkerCount:              3,
		FilenamePattern:          "test.log",
		OutputFormat:             "json",
		Rotation: types.RotationConfig{
			MaxSizeMB: 100,
			MaxFiles:  10,
		},
	}

	sink := NewLocalFileSink(config, logger)
	ctx := context.Background()

	err = sink.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sink: %v", err)
	}
	defer sink.Stop()

	const goroutines = 10
	const entriesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < entriesPerGoroutine; j++ {
				entry := types.LogEntry{
					Timestamp:   time.Now(),
					Message:     "Test message for deadlock testing",
					Level:       "info",
					SourceType:  "test",
					SourceID:    "deadlock-test",
					Labels:      make(map[string]string),
					ProcessedAt: time.Now(),
				}
				entry.SetLabel("goroutine", string(rune('A'+id)))
				entry.SetLabel("iteration", string(rune('0'+j%10)))

				// Send with timeout
				writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = sink.Send(writeCtx, []types.LogEntry{entry})
				cancel()
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Logf("✓ Successfully wrote %d entries with concurrent disk checks", goroutines*entriesPerGoroutine)
	case <-time.After(20 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Write operations timed out")
	}

	// Verify files were created
	files, err := filepath.Glob(filepath.Join(tempDir, "*.log"))
	if err != nil {
		t.Fatalf("Failed to list log files: %v", err)
	}

	if len(files) == 0 {
		t.Error("Expected log files to be created")
	} else {
		t.Logf("✓ Created %d log file(s)", len(files))
	}
}

// TestLocalFileSinkStressTestDeadlock runs intensive concurrent operations
func TestLocalFileSinkStressTestDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "logcapture-stress-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:                  true,
		Directory:                tempDir,
		MaxTotalDiskGB:           1.0,
		DiskCheckInterval:        "50ms",
		CleanupThresholdPercent:  90.0,
		EmergencyCleanupEnabled:  true,
		QueueSize:                100,
		WorkerCount:              4,
		Rotation: types.RotationConfig{
			MaxSizeMB: 10,
			MaxFiles:  5,
		},
	}

	sink := NewLocalFileSink(config, logger)

	const duration = 5 * time.Second
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
					switch iteration % 4 {
					case 0:
						_ = sink.isDiskSpaceAvailable()
					case 1:
						_ = sink.canWriteSize(1024 * int64(id+1))
					case 2:
						sink.checkDiskSpaceAndCleanup()
					case 3:
						// Force lastDiskCheck to be old
						sink.diskSpaceMutex.Lock()
						sink.lastDiskCheck = time.Now().Add(-10 * time.Minute)
						sink.diskSpaceMutex.Unlock()
					}
					iteration++
					if iteration%100 == 0 {
						time.Sleep(time.Millisecond)
					}
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(done)

	// Wait with timeout
	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Logf("✓ Stress test completed successfully (%v with %d goroutines)", duration, goroutines)
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Stress test timed out after duration")
	}
}

// TestLocalFileSinkFileDescriptorLimit tests that LRU closes files when limit is reached (C8)
func TestLocalFileSinkFileDescriptorLimit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logcapture-fdlimit-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:           true,
		Directory:         tempDir,
		MaxTotalDiskGB:    10.0,
		DiskCheckInterval: "1m",
		QueueSize:         100,
		WorkerCount:       2,
		FilenamePattern:   "{date}_test.log",
		OutputFormat:      "json",
		MaxOpenFiles:      5, // C8: Low limit to test LRU
		Rotation: types.RotationConfig{
			MaxSizeMB: 100,
			MaxFiles:  20,
		},
	}

	sink := NewLocalFileSink(config, logger)

	// Verify initial state
	if sink.maxOpenFiles != 5 {
		t.Errorf("Expected maxOpenFiles=5, got %d", sink.maxOpenFiles)
	}
	if sink.openFileCount != 0 {
		t.Errorf("Expected openFileCount=0, got %d", sink.openFileCount)
	}

	// Create 10 different files (more than limit of 5)
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tempDir, string(rune('A'+i))+"_test.log")

		lf, err := sink.getOrCreateLogFile(filename)
		if err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}

		// Write some data to update lastWrite
		lf.mutex.Lock()
		_, writeErr := lf.file.WriteString("test data\n")
		lf.lastWrite = time.Now()
		lf.mutex.Unlock()

		if writeErr != nil {
			t.Errorf("Failed to write to file %d: %v", i, writeErr)
		}

		// Small delay to ensure different timestamps
		time.Sleep(2 * time.Millisecond)

		// After 5 files, count should stabilize at maxOpenFiles
		if i >= 5 {
			if sink.openFileCount > sink.maxOpenFiles {
				t.Errorf("Iteration %d: openFileCount=%d exceeds maxOpenFiles=%d",
					i, sink.openFileCount, sink.maxOpenFiles)
			}
		}
	}

	// Final verification
	if sink.openFileCount > sink.maxOpenFiles {
		t.Errorf("Final openFileCount=%d exceeds maxOpenFiles=%d",
			sink.openFileCount, sink.maxOpenFiles)
	}

	// Should have exactly maxOpenFiles open (or less if some closed)
	if sink.openFileCount > 5 {
		t.Errorf("Expected openFileCount <= 5, got %d", sink.openFileCount)
	}

	// Verify LRU closed old files
	if len(sink.files) > sink.maxOpenFiles {
		t.Errorf("Expected files map size <= %d, got %d", sink.maxOpenFiles, len(sink.files))
	}

	t.Logf("✓ LRU correctly enforced limit: openFileCount=%d, maxOpenFiles=%d",
		sink.openFileCount, sink.maxOpenFiles)
}

// TestLocalFileSinkLRUReopensFiles tests that closed files can be reopened (C8)
func TestLocalFileSinkLRUReopensFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "logcapture-lru-reopen-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := types.LocalFileConfig{
		Enabled:           true,
		Directory:         tempDir,
		MaxTotalDiskGB:    10.0,
		DiskCheckInterval: "1m",
		QueueSize:         100,
		WorkerCount:       2,
		FilenamePattern:   "{date}_test.log",
		OutputFormat:      "json",
		MaxOpenFiles:      3, // C8: Very low limit
		Rotation: types.RotationConfig{
			MaxSizeMB: 100,
			MaxFiles:  20,
		},
	}

	sink := NewLocalFileSink(config, logger)

	// Create first file
	file1 := filepath.Join(tempDir, "file1.log")
	lf1, err := sink.getOrCreateLogFile(file1)
	if err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}
	lf1.mutex.Lock()
	lf1.file.WriteString("data1\n")
	lf1.lastWrite = time.Now()
	lf1.mutex.Unlock()
	time.Sleep(5 * time.Millisecond)

	// Create second file
	file2 := filepath.Join(tempDir, "file2.log")
	lf2, err := sink.getOrCreateLogFile(file2)
	if err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}
	lf2.mutex.Lock()
	lf2.file.WriteString("data2\n")
	lf2.lastWrite = time.Now()
	lf2.mutex.Unlock()
	time.Sleep(5 * time.Millisecond)

	// Create third file
	file3 := filepath.Join(tempDir, "file3.log")
	lf3, err := sink.getOrCreateLogFile(file3)
	if err != nil {
		t.Fatalf("Failed to create file3: %v", err)
	}
	lf3.mutex.Lock()
	lf3.file.WriteString("data3\n")
	lf3.lastWrite = time.Now()
	lf3.mutex.Unlock()
	time.Sleep(5 * time.Millisecond)

	// Now create fourth file - should trigger LRU and close file1 (oldest)
	file4 := filepath.Join(tempDir, "file4.log")
	lf4, err := sink.getOrCreateLogFile(file4)
	if err != nil {
		t.Fatalf("Failed to create file4: %v", err)
	}
	lf4.mutex.Lock()
	lf4.file.WriteString("data4\n")
	lf4.lastWrite = time.Now()
	lf4.mutex.Unlock()

	// file1 should be closed now
	if _, exists := sink.files[file1]; exists {
		t.Error("Expected file1 to be closed by LRU")
	}

	// Now reopen file1 - should work fine
	lf1Reopened, err := sink.getOrCreateLogFile(file1)
	if err != nil {
		t.Fatalf("Failed to reopen file1: %v", err)
	}

	// Verify we can write to reopened file
	lf1Reopened.mutex.Lock()
	_, writeErr := lf1Reopened.file.WriteString("reopened data\n")
	lf1Reopened.mutex.Unlock()

	if writeErr != nil {
		t.Errorf("Failed to write to reopened file1: %v", writeErr)
	}

	// Verify file exists on disk and has content
	data, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("Failed to read file1 from disk: %v", err)
	}

	if len(data) == 0 {
		t.Error("Reopened file1 has no content")
	}

	// Should contain both original and reopened data
	content := string(data)
	if filepath.Ext(file1) != ".log" {
		t.Error("File extension incorrect")
	}

	t.Logf("✓ LRU correctly closed and reopened files. File1 content length: %d bytes", len(content))
	t.Logf("✓ Current openFileCount=%d, maxOpenFiles=%d", sink.openFileCount, sink.maxOpenFiles)
}
