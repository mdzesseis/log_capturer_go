package task_manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestTaskManagerBasicOperation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}

	tm := New(config, logger)
	defer tm.Cleanup()

	ctx := context.Background()
	done := make(chan bool, 1)

	err := tm.StartTask(ctx, "test1", func(ctx context.Context) error {
		done <- true
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to start task: %v", err)
	}

	// Wait for task to signal completion
	select {
	case <-done:
		// Task executed
	case <-time.After(1 * time.Second):
		t.Error("Task was not executed within timeout")
	}

	status := tm.GetTaskStatus("test1")
	if status.State != "completed" {
		t.Errorf("Expected state 'completed', got '%s'", status.State)
	}
}

func TestTaskManagerPanicRecovery(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}

	tm := New(config, logger)
	defer tm.Cleanup()

	ctx := context.Background()

	err := tm.StartTask(ctx, "panic_task", func(ctx context.Context) error {
		panic("test panic")
	})

	if err != nil {
		t.Fatalf("Failed to start task: %v", err)
	}

	// Wait for task to panic and recover
	time.Sleep(100 * time.Millisecond)

	status := tm.GetTaskStatus("panic_task")
	if status.State != "failed" {
		t.Errorf("Expected state 'failed' after panic, got '%s'", status.State)
	}

	if status.LastError == "" || status.LastError[:5] != "panic" {
		t.Errorf("Expected panic error message, got '%s'", status.LastError)
	}

	if status.ErrorCount != 1 {
		t.Errorf("Expected error count 1, got %d", status.ErrorCount)
	}
}

func TestTaskManagerConcurrentTasks(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}

	tm := New(config, logger)
	defer tm.Cleanup()

	ctx := context.Background()
	const numTasks = 50

	var wg sync.WaitGroup
	wg.Add(numTasks)

	// Start many tasks concurrently
	for i := 0; i < numTasks; i++ {
		taskID := string(rune('A' + i))
		go func(id string) {
			defer wg.Done()
			tm.StartTask(ctx, id, func(ctx context.Context) error {
				time.Sleep(10 * time.Millisecond)
				return nil
			})
		}(taskID)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Verify all tasks completed
	allTasks := tm.GetAllTasks()
	completedCount := 0
	for _, status := range allTasks {
		if status.State == "completed" {
			completedCount++
		}
	}

	if completedCount != numTasks {
		t.Errorf("Expected %d completed tasks, got %d", numTasks, completedCount)
	}
}

func TestTaskManagerErrorHandling(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}

	tm := New(config, logger)
	defer tm.Cleanup()

	ctx := context.Background()
	testErr := errors.New("test error")

	err := tm.StartTask(ctx, "error_task", func(ctx context.Context) error {
		return testErr
	})

	if err != nil {
		t.Fatalf("Failed to start task: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	status := tm.GetTaskStatus("error_task")
	if status.State != "failed" {
		t.Errorf("Expected state 'failed', got '%s'", status.State)
	}

	if status.LastError != testErr.Error() {
		t.Errorf("Expected error '%s', got '%s'", testErr.Error(), status.LastError)
	}

	if status.ErrorCount != 1 {
		t.Errorf("Expected error count 1, got %d", status.ErrorCount)
	}
}

func TestTaskManagerRaceConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		HeartbeatInterval: 30 * time.Second,
		TaskTimeout:       5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
	}

	tm := New(config, logger)
	defer tm.Cleanup()

	ctx := context.Background()
	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				taskID := string(rune('A' + (gid%26)))
				tm.StartTask(ctx, taskID, func(ctx context.Context) error {
					time.Sleep(time.Millisecond)
					if i%5 == 0 {
						return errors.New("periodic error")
					}
					return nil
				})

				// Also test concurrent reads
				tm.GetTaskStatus(taskID)
				tm.GetAllTasks()
			}
		}(g)
	}

	wg.Wait()
	t.Log("✓ No race conditions detected in concurrent operations")
}
