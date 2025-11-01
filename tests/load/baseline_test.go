package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// LogEntry represents a log entry to send to the API
type LogEntry struct {
	Message    string            `json:"message"`
	Level      string            `json:"level"`
	SourceType string            `json:"source_type"`
	SourceID   string            `json:"source_id"`
	Labels     map[string]string `json:"labels,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// LoadTestStats tracks statistics during load testing
type LoadTestStats struct {
	TotalSent       atomic.Int64
	TotalSuccess    atomic.Int64
	TotalErrors     atomic.Int64
	TotalTimeout    atomic.Int64
	StartTime       time.Time
	EndTime         time.Time
	MinLatency      atomic.Int64 // nanoseconds
	MaxLatency      atomic.Int64 // nanoseconds
	TotalLatency    atomic.Int64 // nanoseconds
	LatencySamples  atomic.Int64
}

func (s *LoadTestStats) RecordLatency(d time.Duration) {
	ns := d.Nanoseconds()
	s.TotalLatency.Add(ns)
	s.LatencySamples.Add(1)

	// Update min
	for {
		old := s.MinLatency.Load()
		if old != 0 && ns >= old {
			break
		}
		if s.MinLatency.CompareAndSwap(old, ns) {
			break
		}
	}

	// Update max
	for {
		old := s.MaxLatency.Load()
		if ns <= old {
			break
		}
		if s.MaxLatency.CompareAndSwap(old, ns) {
			break
		}
	}
}

func (s *LoadTestStats) AvgLatency() time.Duration {
	samples := s.LatencySamples.Load()
	if samples == 0 {
		return 0
	}
	return time.Duration(s.TotalLatency.Load() / samples)
}

func (s *LoadTestStats) Throughput() float64 {
	duration := s.EndTime.Sub(s.StartTime).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(s.TotalSuccess.Load()) / duration
}

// TestLoadBaseline_10K tests system with 10,000 logs/sec
func TestLoadBaseline_10K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runBaselineTest(t, "10K", 10000, 60*time.Second)
}

// TestLoadBaseline_25K tests system with 25,000 logs/sec
func TestLoadBaseline_25K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runBaselineTest(t, "25K", 25000, 60*time.Second)
}

// TestLoadBaseline_50K tests system with 50,000 logs/sec
func TestLoadBaseline_50K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runBaselineTest(t, "50K", 50000, 60*time.Second)
}

// TestLoadBaseline_100K tests system with 100,000 logs/sec
func TestLoadBaseline_100K(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	runBaselineTest(t, "100K", 100000, 60*time.Second)
}

func runBaselineTest(t *testing.T, name string, targetRPS int, duration time.Duration) {
	// Get API endpoint from environment or use default
	apiURL := "http://localhost:8401/api/v1/logs"

	// Check if server is running
	resp, err := http.Get("http://localhost:8401/health")
	if err != nil {
		t.Skipf("Server not running on localhost:8401, skipping test: %v", err)
		return
	}
	resp.Body.Close()

	t.Logf("=== BASELINE LOAD TEST: %s logs/sec ===", name)
	t.Logf("Target: %d logs/sec for %v", targetRPS, duration)

	stats := &LoadTestStats{
		StartTime: time.Now(),
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), duration+10*time.Second)
	defer cancel()

	// Create worker pool
	numWorkers := 10
	logsPerWorker := targetRPS / numWorkers
	interval := time.Second / time.Duration(logsPerWorker)

	t.Logf("Starting %d workers, each sending %d logs/sec", numWorkers, logsPerWorker)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			sendLogs(ctx, apiURL, workerID, logsPerWorker, interval, stopChan, stats)
		}(i)
	}

	// Monitor progress
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)
				sent := stats.TotalSent.Load()
				success := stats.TotalSuccess.Load()
				errors := stats.TotalErrors.Load()
				currentThroughput := float64(success) / elapsed.Seconds()

				var memStats runtime.MemStats
				runtime.ReadMemStats(&memStats)

				t.Logf("[%s] Sent: %d, Success: %d, Errors: %d, Throughput: %.0f logs/sec, Mem: %.0f MB, Goroutines: %d",
					elapsed.Round(time.Second),
					sent,
					success,
					errors,
					currentThroughput,
					float64(memStats.Alloc)/(1024*1024),
					runtime.NumGoroutine())

				if elapsed >= duration {
					close(stopChan)
					return
				}
			}
		}
	}()

	// Wait for completion
	<-stopChan
	wg.Wait()
	stats.EndTime = time.Now()

	// Final report
	printLoadTestReport(t, name, targetRPS, stats)
}

func sendLogs(ctx context.Context, apiURL string, workerID, logsPerSec int, interval time.Duration, stopChan chan struct{}, stats *LoadTestStats) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	logNum := 0
	for {
		select {
		case <-stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			logNum++
			entry := LogEntry{
				Message:    fmt.Sprintf("Load test log from worker %d, log %d", workerID, logNum),
				Level:      "info",
				SourceType: "load-test",
				SourceID:   fmt.Sprintf("worker-%d", workerID),
				Labels: map[string]string{
					"test":      "baseline",
					"worker_id": fmt.Sprintf("%d", workerID),
					"log_num":   fmt.Sprintf("%d", logNum),
				},
				Timestamp: time.Now(),
			}

			stats.TotalSent.Add(1)

			// Send log
			start := time.Now()
			err := sendLogEntry(client, apiURL, entry)
			latency := time.Since(start)

			if err != nil {
				stats.TotalErrors.Add(1)
			} else {
				stats.TotalSuccess.Add(1)
				stats.RecordLatency(latency)
			}
		}
	}
}

func sendLogEntry(client *http.Client, apiURL string, entry LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Drain response body
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func printLoadTestReport(t *testing.T, name string, targetRPS int, stats *LoadTestStats) {
	duration := stats.EndTime.Sub(stats.StartTime)

	t.Logf("\n=== LOAD TEST RESULTS: %s ===", name)
	t.Logf("Duration: %v", duration)
	t.Logf("Target RPS: %d logs/sec", targetRPS)
	t.Logf("")
	t.Logf("THROUGHPUT:")
	t.Logf("  Total Sent: %d", stats.TotalSent.Load())
	t.Logf("  Total Success: %d", stats.TotalSuccess.Load())
	t.Logf("  Total Errors: %d", stats.TotalErrors.Load())
	t.Logf("  Actual Throughput: %.0f logs/sec", stats.Throughput())
	t.Logf("  Target Achievement: %.1f%%", (stats.Throughput()/float64(targetRPS))*100)
	t.Logf("")
	t.Logf("LATENCY:")
	t.Logf("  Min: %v", time.Duration(stats.MinLatency.Load()))
	t.Logf("  Max: %v", time.Duration(stats.MaxLatency.Load()))
	t.Logf("  Avg: %v", stats.AvgLatency())
	t.Logf("")
	t.Logf("ERROR RATE:")
	errorRate := float64(stats.TotalErrors.Load()) / float64(stats.TotalSent.Load()) * 100
	t.Logf("  Error Rate: %.2f%%", errorRate)
	t.Logf("")

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	t.Logf("SYSTEM:")
	t.Logf("  Final Memory: %.2f MB", float64(memStats.Alloc)/(1024*1024))
	t.Logf("  Total Allocated: %.2f MB", float64(memStats.TotalAlloc)/(1024*1024))
	t.Logf("  Goroutines: %d", runtime.NumGoroutine())
	t.Logf("  Num GC: %d", memStats.NumGC)

	// Validate results
	achievementPercent := (stats.Throughput() / float64(targetRPS)) * 100
	if achievementPercent < 80 {
		t.Errorf("❌ FAILED: Only achieved %.1f%% of target throughput", achievementPercent)
	} else if achievementPercent < 95 {
		t.Logf("⚠️  WARNING: Achieved %.1f%% of target throughput", achievementPercent)
	} else {
		t.Logf("✅ SUCCESS: Achieved %.1f%% of target throughput", achievementPercent)
	}

	if errorRate > 5.0 {
		t.Errorf("❌ FAILED: Error rate too high: %.2f%%", errorRate)
	} else if errorRate > 1.0 {
		t.Logf("⚠️  WARNING: Error rate: %.2f%%", errorRate)
	} else {
		t.Logf("✅ SUCCESS: Low error rate: %.2f%%", errorRate)
	}

	avgLatency := stats.AvgLatency()
	if avgLatency > 1*time.Second {
		t.Errorf("❌ FAILED: Average latency too high: %v", avgLatency)
	} else if avgLatency > 500*time.Millisecond {
		t.Logf("⚠️  WARNING: Average latency: %v", avgLatency)
	} else {
		t.Logf("✅ SUCCESS: Good average latency: %v", avgLatency)
	}
}
