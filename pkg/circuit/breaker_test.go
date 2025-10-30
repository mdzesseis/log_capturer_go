package circuit

import (
	"errors"
	"fmt"
	"ssw-logs-capture/pkg/types"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestCircuitBreakerBasicOperation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	// Test: successful execution
	err := breaker.Execute(func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if breaker.State() != types.CircuitBreakerClosed {
		t.Errorf("Expected state CLOSED, got %v", breaker.State())
	}
}

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	// Execute 3 failures to trigger opening
	testErr := errors.New("test error")
	for i := 0; i < 3; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	// Circuit should be open now
	if breaker.State() != types.CircuitBreakerOpen {
		t.Errorf("Expected state OPEN after %d failures, got %v", 3, breaker.State())
	}

	// Attempt should fail immediately
	err := breaker.Execute(func() error {
		t.Error("Function should not be executed when circuit is open")
		return nil
	})

	if err == nil {
		t.Error("Expected error when circuit is open")
	}
}

func TestCircuitBreakerHalfOpenTransition(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	// Trigger opening
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	if breaker.State() != types.CircuitBreakerOpen {
		t.Fatalf("Expected state OPEN, got %v", breaker.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Next execution should transition to HALF_OPEN
	var executedCount int32
	breaker.Execute(func() error {
		atomic.AddInt32(&executedCount, 1)
		return nil
	})

	if breaker.State() != types.CircuitBreakerHalfOpen {
		t.Errorf("Expected state HALF_OPEN after timeout, got %v", breaker.State())
	}

	if executedCount != 1 {
		t.Errorf("Expected function to execute once, got %d", executedCount)
	}
}

func TestCircuitBreakerClosesAfterSuccesses(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	// Open the circuit
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute successful calls to close it
	for i := 0; i < 2; i++ {
		err := breaker.Execute(func() error {
			return nil
		})
		if err != nil {
			t.Fatalf("Unexpected error in success call %d: %v", i, err)
		}
	}

	// Should be closed now
	if breaker.State() != types.CircuitBreakerClosed {
		t.Errorf("Expected state CLOSED after successes, got %v", breaker.State())
	}
}

// CRITICAL TEST: Verify concurrent executions run in parallel
func TestCircuitBreakerConcurrentExecutions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 100,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
		HalfOpenMaxCalls: 50,
	}

	breaker := NewBreaker(config, logger)

	const concurrentCalls = 10
	const sleepDuration = 100 * time.Millisecond

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(concurrentCalls)

	for i := 0; i < concurrentCalls; i++ {
		go func() {
			defer wg.Done()
			breaker.Execute(func() error {
				time.Sleep(sleepDuration)
				return nil
			})
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// If executions were serial, it would take concurrentCalls * sleepDuration
	// If parallel, it should take roughly sleepDuration
	maxExpectedTime := sleepDuration * 3 // Allow 3x for overhead

	if elapsed > maxExpectedTime {
		t.Errorf("Concurrent executions appear to be serial. Took %v, expected ~%v",
			elapsed, sleepDuration)
		t.Errorf("This suggests the mutex is being held during fn() execution!")
	}

	t.Logf("✓ %d concurrent calls completed in %v (expected ~%v)",
		concurrentCalls, elapsed, sleepDuration)
}

// Test that failures in half-open state return to open
func TestCircuitBreakerHalfOpenFailureReturnsToOpen(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	// Open the circuit
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute one to go to HALF_OPEN
	breaker.Execute(func() error {
		return nil
	})

	if breaker.State() != types.CircuitBreakerHalfOpen {
		t.Fatalf("Expected HALF_OPEN, got %v", breaker.State())
	}

	// Now fail - should go back to OPEN
	breaker.Execute(func() error {
		return testErr
	})

	if breaker.State() != types.CircuitBreakerOpen {
		t.Errorf("Expected state OPEN after failure in HALF_OPEN, got %v", breaker.State())
	}
}

// Test that max calls in half-open is respected
func TestCircuitBreakerHalfOpenMaxCalls(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 5,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 3,
	}

	breaker := NewBreaker(config, logger)

	// Open the circuit
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute max calls
	var executedCount int32
	for i := 0; i < 5; i++ {
		err := breaker.Execute(func() error {
			atomic.AddInt32(&executedCount, 1)
			return nil
		})

		if i >= config.HalfOpenMaxCalls && err == nil {
			t.Errorf("Call %d should have been rejected (max=%d)", i, config.HalfOpenMaxCalls)
		}
	}

	if executedCount > int32(config.HalfOpenMaxCalls) {
		t.Errorf("Executed %d calls, expected max %d", executedCount, config.HalfOpenMaxCalls)
	}
}

// Test race conditions with concurrent state changes
func TestCircuitBreakerRaceConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          10 * time.Millisecond,
		HalfOpenMaxCalls: 10,
	}

	breaker := NewBreaker(config, logger)

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Mix of successes and failures
				err := breaker.Execute(func() error {
					time.Sleep(time.Microsecond)
					if i%10 == 0 {
						return fmt.Errorf("error %d", i)
					}
					return nil
				})
				_ = err // Ignore result
			}
		}(g)
	}

	wg.Wait()

	stats := breaker.GetStats()
	totalRequests := stats.Requests

	expectedRequests := int64(goroutines * iterations)
	if totalRequests < expectedRequests/2 {
		t.Errorf("Request count too low: %d, expected around %d", totalRequests, expectedRequests)
	}

	t.Logf("✓ Processed %d requests across %d goroutines without race conditions",
		totalRequests, goroutines)
}

// Test callbacks are called correctly
func TestCircuitBreakerCallbacks(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}

	breaker := NewBreaker(config, logger)

	var stateChanges []string
	var failures int32
	var successes int32

	breaker.SetStateChangeCallback(func(from, to types.CircuitBreakerState) {
		stateChanges = append(stateChanges, fmt.Sprintf("%v->%v", from, to))
	})

	breaker.SetFailureCallback(func(err error) {
		atomic.AddInt32(&failures, 1)
	})

	breaker.SetSuccessCallback(func() {
		atomic.AddInt32(&successes, 1)
	})

	// Trigger state changes
	testErr := errors.New("test error")
	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return testErr
		})
	}

	time.Sleep(60 * time.Millisecond)

	for i := 0; i < 2; i++ {
		breaker.Execute(func() error {
			return nil
		})
	}

	if failures != 2 {
		t.Errorf("Expected 2 failure callbacks, got %d", failures)
	}

	if successes != 2 {
		t.Errorf("Expected 2 success callbacks, got %d", successes)
	}

	if len(stateChanges) < 2 {
		t.Errorf("Expected at least 2 state changes, got %d: %v", len(stateChanges), stateChanges)
	}

	t.Logf("✓ State changes: %v", stateChanges)
}

// Benchmark to measure performance improvement
func BenchmarkCircuitBreakerSerial(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "bench",
		FailureThreshold: 1000,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}

	breaker := NewBreaker(config, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		breaker.Execute(func() error {
			// Simulate work
			time.Sleep(10 * time.Microsecond)
			return nil
		})
	}
}

func BenchmarkCircuitBreakerParallel(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := BreakerConfig{
		Name:             "bench",
		FailureThreshold: 1000,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}

	breaker := NewBreaker(config, logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			breaker.Execute(func() error {
				// Simulate work
				time.Sleep(10 * time.Microsecond)
				return nil
			})
		}
	})
}
