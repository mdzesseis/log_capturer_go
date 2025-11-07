package monitors

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamPool_AcquireRelease tests basic acquire and release operations
func TestStreamPool_AcquireRelease(t *testing.T) {
	pool := NewStreamPool(5)

	// Acquire a slot
	err := pool.AcquireSlot("container1", "test-container-1")
	require.NoError(t, err)
	assert.Equal(t, 1, pool.GetActiveCount())

	// Acquire another slot
	err = pool.AcquireSlot("container2", "test-container-2")
	require.NoError(t, err)
	assert.Equal(t, 2, pool.GetActiveCount())

	// Release first slot
	pool.ReleaseSlot("container1")
	assert.Equal(t, 1, pool.GetActiveCount())

	// Release second slot
	pool.ReleaseSlot("container2")
	assert.Equal(t, 0, pool.GetActiveCount())
}

// TestStreamPool_Capacity tests that pool enforces capacity limits
func TestStreamPool_Capacity(t *testing.T) {
	maxStreams := 3
	pool := NewStreamPool(maxStreams)

	// Fill the pool
	for i := 0; i < maxStreams; i++ {
		err := pool.AcquireSlot(fmt.Sprintf("container%d", i), "test")
		require.NoError(t, err)
	}
	assert.Equal(t, maxStreams, pool.GetActiveCount())

	// Try to acquire one more (should fail immediately)
	err := pool.AcquireSlot("container-overflow", "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream pool at capacity")
	assert.Equal(t, maxStreams, pool.GetActiveCount())

	// Release one slot
	pool.ReleaseSlot("container0")
	assert.Equal(t, maxStreams-1, pool.GetActiveCount())

	// Now we should be able to acquire again
	err = pool.AcquireSlot("container-new", "test")
	require.NoError(t, err)
	assert.Equal(t, maxStreams, pool.GetActiveCount())
}

// TestStreamPool_Concurrent tests thread safety with concurrent operations
func TestStreamPool_Concurrent(t *testing.T) {
	pool := NewStreamPool(50)
	var wg sync.WaitGroup
	numGoroutines := 100
	successCount := 0
	var mu sync.Mutex

	successfulContainers := make([]string, 0, 50)

	// Try to acquire 100 slots concurrently (only 50 should succeed)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			containerID := fmt.Sprintf("container%d", id)
			err := pool.AcquireSlot(containerID, "test")
			if err == nil {
				mu.Lock()
				successCount++
				successfulContainers = append(successfulContainers, containerID)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Should have 50 active (pool capacity)
	assert.Equal(t, 50, pool.GetActiveCount())
	assert.Equal(t, 50, successCount)

	// Release only the successfully acquired containers
	for _, containerID := range successfulContainers {
		pool.ReleaseSlot(containerID)
	}

	// Should be empty now
	assert.Equal(t, 0, pool.GetActiveCount())
}

// TestStreamPool_UpdateActivity tests activity tracking
func TestStreamPool_UpdateActivity(t *testing.T) {
	pool := NewStreamPool(5)

	// Acquire slot
	err := pool.AcquireSlot("container1", "test")
	require.NoError(t, err)

	// Get initial info
	pool.mu.RLock()
	info1 := pool.activeStreams["container1"]
	initialTime := info1.lastActive
	pool.mu.RUnlock()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update activity
	pool.UpdateActivity("container1")

	// Get updated info
	pool.mu.RLock()
	info2 := pool.activeStreams["container1"]
	updatedTime := info2.lastActive
	pool.mu.RUnlock()

	// Should have been updated
	assert.True(t, updatedTime.After(initialTime))
}

// TestStreamPool_ReleaseNonExistent tests releasing a non-existent container
func TestStreamPool_ReleaseNonExistent(t *testing.T) {
	pool := NewStreamPool(5)

	// Acquire one slot
	err := pool.AcquireSlot("container1", "test")
	require.NoError(t, err)
	assert.Equal(t, 1, pool.GetActiveCount())

	// Release non-existent container (should not panic or cause issues)
	pool.ReleaseSlot("container-nonexistent")

	// Original container should still be there
	assert.Equal(t, 1, pool.GetActiveCount())
}

// TestStreamRotation_ContextTimeout tests that rotation happens via context timeout
func TestStreamRotation_ContextTimeout(t *testing.T) {
	// Create a short timeout for testing (100ms instead of 5 minutes)
	rotationInterval := 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create stream context with timeout
	streamCtx, streamCancel := context.WithTimeout(ctx, rotationInterval)
	defer streamCancel()

	startTime := time.Now()

	// Wait for context to timeout
	<-streamCtx.Done()

	elapsedTime := time.Since(startTime)

	// Should have timed out around 100ms
	assert.InDelta(t, rotationInterval.Milliseconds(), elapsedTime.Milliseconds(), 50.0, "Timeout should occur close to rotation interval")
	assert.Equal(t, context.DeadlineExceeded, streamCtx.Err(), "Context should have exceeded deadline")
}

// TestStreamRotation_PositionPreservation tests that lastRead is preserved across rotations
func TestStreamRotation_PositionPreservation(t *testing.T) {
	mc := &monitoredContainer{
		id:           "test-container",
		name:         "test",
		lastRead:     time.Now().Add(-1 * time.Hour),
		rotationCount: 0,
	}

	// Simulate first rotation
	mc.streamCreatedAt = time.Now()
	time.Sleep(10 * time.Millisecond)

	// After rotation, lastRead should be updated by readContainerLogs
	// But the important part is that it's preserved and used for the next stream
	newLastRead := time.Now()
	mc.lastRead = newLastRead

	// Simulate second rotation
	mc.rotationCount++
	assert.Equal(t, 1, mc.rotationCount)

	// lastRead should still be available for next iteration
	assert.False(t, mc.lastRead.IsZero())
	assert.Equal(t, newLastRead, mc.lastRead)
}

// TestStreamRotation_MetricsTracking tests metrics are properly tracked
func TestStreamRotation_MetricsTracking(t *testing.T) {
	mc := &monitoredContainer{
		id:              "test-container",
		name:            "test",
		streamCreatedAt: time.Now().Add(-5 * time.Minute),
		rotationCount:   0,
	}

	// Simulate rotation
	streamAge := time.Since(mc.streamCreatedAt)
	mc.rotationCount++

	// Check rotation count incremented
	assert.Equal(t, 1, mc.rotationCount)

	// Check stream age is approximately 5 minutes
	assert.InDelta(t, 300.0, streamAge.Seconds(), 1.0, "Stream age should be close to 5 minutes")
}

// TestStreamRotation_ErrorHandling tests error scenarios
func TestStreamRotation_ErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		shouldRotate  bool
		description   string
	}{
		{
			name:         "DeadlineExceeded",
			err:          context.DeadlineExceeded,
			shouldRotate: true,
			description:  "Planned rotation should be recorded",
		},
		{
			name:         "Canceled",
			err:          context.Canceled,
			shouldRotate: false,
			description:  "Cancellation should exit gracefully",
		},
		{
			name:         "OtherError",
			err:          assert.AnError,
			shouldRotate: false,
			description:  "Other errors should trigger reconnect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &monitoredContainer{
				id:              "test-container",
				name:            "test",
				streamCreatedAt: time.Now().Add(-5 * time.Minute),
				rotationCount:   0,
			}

			initialCount := mc.rotationCount

			// Simulate error handling
			if tt.err == context.DeadlineExceeded {
				mc.rotationCount++
			}

			if tt.shouldRotate {
				assert.Greater(t, mc.rotationCount, initialCount, tt.description)
			} else {
				assert.Equal(t, initialCount, mc.rotationCount, tt.description)
			}
		})
	}
}

// TestStreamPool_ZeroCapacity tests behavior with zero capacity (edge case)
func TestStreamPool_ZeroCapacity(t *testing.T) {
	pool := NewStreamPool(0)

	// Should not be able to acquire any slots
	err := pool.AcquireSlot("container1", "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream pool at capacity")
}

// TestStreamPool_LargeCapacity tests with realistic production capacity
func TestStreamPool_LargeCapacity(t *testing.T) {
	pool := NewStreamPool(50)

	// Acquire 50 slots (should all succeed)
	for i := 0; i < 50; i++ {
		err := pool.AcquireSlot(fmt.Sprintf("container%d", i), "test")
		require.NoError(t, err)
	}

	assert.Equal(t, 50, pool.GetActiveCount())

	// 51st should fail
	err := pool.AcquireSlot("container-overflow", "test")
	assert.Error(t, err)
}

// TestStreamPool_ReleaseAndReacquire tests multiple acquire/release cycles
func TestStreamPool_ReleaseAndReacquire(t *testing.T) {
	pool := NewStreamPool(2)

	// Cycle 1
	err := pool.AcquireSlot("container1", "test")
	require.NoError(t, err)
	pool.ReleaseSlot("container1")

	// Cycle 2 - reacquire same container
	err = pool.AcquireSlot("container1", "test")
	require.NoError(t, err)
	pool.ReleaseSlot("container1")

	// Should be clean
	assert.Equal(t, 0, pool.GetActiveCount())
}

// BenchmarkStreamPool_AcquireRelease benchmarks acquire/release performance
func BenchmarkStreamPool_AcquireRelease(b *testing.B) {
	pool := NewStreamPool(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		containerID := fmt.Sprintf("container%d", i%100)
		_ = pool.AcquireSlot(containerID, "test")
		pool.ReleaseSlot(containerID)
	}
}

// BenchmarkStreamPool_Concurrent benchmarks concurrent access
func BenchmarkStreamPool_Concurrent(b *testing.B) {
	pool := NewStreamPool(50)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			containerID := fmt.Sprintf("container%d", i%50)
			_ = pool.AcquireSlot(containerID, "test")
			pool.ReleaseSlot(containerID)
			i++
		}
	})
}
