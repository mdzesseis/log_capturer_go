package load

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"
)

// SustainedLoadStats tracks detailed statistics over a long period
type SustainedLoadStats struct {
	LoadTestStats
	Snapshots      []StatsSnapshot
	SnapshotMutex  sync.Mutex
	MemoryBaseline uint64
	MemoryPeak     uint64
	GoroutineMin   int
	GoroutineMax   int
}

// StatsSnapshot represents system state at a point in time
type StatsSnapshot struct {
	Timestamp      time.Time
	Sent           int64
	Success        int64
	Errors         int64
	Throughput     float64
	AvgLatency     time.Duration
	MemoryMB       float64
	GoroutineCount int
	NumGC          uint32
}

// TestSustainedLoad_24h runs a 24-hour sustained load test
func TestSustainedLoad_24h(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 24h sustained load test in short mode")
	}

	runSustainedLoadTest(t, 20000, 24*time.Hour)
}

// TestSustainedLoad_1h runs a 1-hour sustained load test (for quicker validation)
func TestSustainedLoad_1h(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sustained load test in short mode")
	}

	runSustainedLoadTest(t, 20000, 1*time.Hour)
}

// TestSustainedLoad_10min runs a 10-minute sustained load test (for quick validation)
func TestSustainedLoad_10min(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sustained load test in short mode")
	}

	runSustainedLoadTest(t, 10000, 10*time.Minute)
}

func runSustainedLoadTest(t *testing.T, targetRPS int, duration time.Duration) {
	apiURL := "http://localhost:8401/api/v1/logs"

	// Check if server is running
	resp, err := http.Get("http://localhost:8401/health")
	if err != nil {
		t.Skipf("Server not running on localhost:8401, skipping test: %v", err)
		return
	}
	resp.Body.Close()

	t.Logf("=== SUSTAINED LOAD TEST ===")
	t.Logf("Target: %d logs/sec for %v", targetRPS, duration)
	t.Logf("This test will run for %v. Monitoring every 1 minute...", duration)

	// Get baseline memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	var baselineMemStats runtime.MemStats
	runtime.ReadMemStats(&baselineMemStats)

	stats := &SustainedLoadStats{
		LoadTestStats: LoadTestStats{
			StartTime: time.Now(),
		},
		Snapshots:      make([]StatsSnapshot, 0, int(duration.Minutes())+10),
		MemoryBaseline: baselineMemStats.Alloc,
		GoroutineMin:   runtime.NumGoroutine(),
	}

	t.Logf("Baseline Memory: %.2f MB", float64(baselineMemStats.Alloc)/(1024*1024))
	t.Logf("Baseline Goroutines: %d", runtime.NumGoroutine())

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), duration+1*time.Minute)
	defer cancel()

	// Create worker pool
	numWorkers := 20
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
			sendLogs(ctx, apiURL, workerID, logsPerWorker, interval, stopChan, &stats.LoadTestStats)
		}(i)
	}

	// Monitor progress every minute
	monitorTicker := time.NewTicker(1 * time.Minute)
	defer monitorTicker.Stop()

	// Take snapshots every 5 minutes
	snapshotTicker := time.NewTicker(5 * time.Minute)
	defer snapshotTicker.Stop()

	startTime := time.Now()
	lastReportTime := startTime

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)

		for {
			select {
			case <-ctx.Done():
				return
			case <-monitorTicker.C:
				elapsed := time.Since(startTime)
				timeSinceLastReport := time.Since(lastReportTime)
				lastReportTime = time.Now()

				sent := stats.TotalSent.Load()
				success := stats.TotalSuccess.Load()
				errors := stats.TotalErrors.Load()
				currentThroughput := float64(success) / elapsed.Seconds()
				recentThroughput := float64(success) / timeSinceLastReport.Seconds()

				var memStats runtime.MemStats
				runtime.ReadMemStats(&memStats)

				goroutines := runtime.NumGoroutine()

				// Track peaks
				if memStats.Alloc > stats.MemoryPeak {
					stats.MemoryPeak = memStats.Alloc
				}
				if goroutines > stats.GoroutineMax {
					stats.GoroutineMax = goroutines
				}
				if goroutines < stats.GoroutineMin {
					stats.GoroutineMin = goroutines
				}

				memMB := float64(memStats.Alloc) / (1024 * 1024)
				memGrowth := float64(memStats.Alloc-stats.MemoryBaseline) / (1024 * 1024)

				t.Logf("[%s] Throughput: %.0f logs/sec (recent: %.0f), Errors: %d (%.2f%%), Mem: %.0f MB (+%.1f MB), Goroutines: %d, GC: %d",
					elapsed.Round(time.Second),
					currentThroughput,
					recentThroughput,
					errors,
					float64(errors)/float64(sent)*100,
					memMB,
					memGrowth,
					goroutines,
					memStats.NumGC)

				// Check for memory leak
				hourlyGrowthMB := memGrowth / elapsed.Hours()
				if hourlyGrowthMB > 10 && elapsed > 10*time.Minute {
					t.Logf("⚠️  WARNING: Potential memory leak detected (%.1f MB/hour growth)", hourlyGrowthMB)
				}

				// Check for goroutine leak
				if goroutines > stats.GoroutineMin+20 && elapsed > 10*time.Minute {
					t.Logf("⚠️  WARNING: Goroutine count increasing (%d -> %d)", stats.GoroutineMin, goroutines)
				}

				if elapsed >= duration {
					close(stopChan)
					return
				}

			case <-snapshotTicker.C:
				takeSnapshot(stats, startTime)
			}
		}
	}()

	// Wait for completion
	<-monitorDone
	wg.Wait()
	stats.EndTime = time.Now()

	// Take final snapshot
	takeSnapshot(stats, startTime)

	// Final report
	printSustainedLoadReport(t, targetRPS, duration, stats)
}

func takeSnapshot(stats *SustainedLoadStats, startTime time.Time) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	elapsed := time.Since(startTime)
	sent := stats.TotalSent.Load()
	success := stats.TotalSuccess.Load()
	throughput := float64(success) / elapsed.Seconds()

	snapshot := StatsSnapshot{
		Timestamp:      time.Now(),
		Sent:           sent,
		Success:        success,
		Errors:         stats.TotalErrors.Load(),
		Throughput:     throughput,
		AvgLatency:     stats.AvgLatency(),
		MemoryMB:       float64(memStats.Alloc) / (1024 * 1024),
		GoroutineCount: runtime.NumGoroutine(),
		NumGC:          memStats.NumGC,
	}

	stats.SnapshotMutex.Lock()
	stats.Snapshots = append(stats.Snapshots, snapshot)
	stats.SnapshotMutex.Unlock()
}

func printSustainedLoadReport(t *testing.T, targetRPS int, duration time.Duration, stats *SustainedLoadStats) {
	actualDuration := stats.EndTime.Sub(stats.StartTime)

	t.Logf("\n=== SUSTAINED LOAD TEST RESULTS ===")
	t.Logf("Planned Duration: %v", duration)
	t.Logf("Actual Duration: %v", actualDuration)
	t.Logf("Target RPS: %d logs/sec", targetRPS)
	t.Logf("")
	t.Logf("THROUGHPUT:")
	t.Logf("  Total Sent: %d", stats.TotalSent.Load())
	t.Logf("  Total Success: %d", stats.TotalSuccess.Load())
	t.Logf("  Total Errors: %d", stats.TotalErrors.Load())
	t.Logf("  Average Throughput: %.0f logs/sec", stats.Throughput())
	t.Logf("  Target Achievement: %.1f%%", (stats.Throughput()/float64(targetRPS))*100)
	t.Logf("")
	t.Logf("LATENCY:")
	t.Logf("  Min: %v", time.Duration(stats.MinLatency.Load()))
	t.Logf("  Max: %v", time.Duration(stats.MaxLatency.Load()))
	t.Logf("  Avg: %v", stats.AvgLatency())
	t.Logf("")
	t.Logf("STABILITY:")
	errorRate := float64(stats.TotalErrors.Load()) / float64(stats.TotalSent.Load()) * 100
	t.Logf("  Error Rate: %.4f%%", errorRate)
	t.Logf("  Memory Baseline: %.2f MB", float64(stats.MemoryBaseline)/(1024*1024))
	t.Logf("  Memory Peak: %.2f MB", float64(stats.MemoryPeak)/(1024*1024))

	var finalMemStats runtime.MemStats
	runtime.ReadMemStats(&finalMemStats)
	memGrowth := float64(finalMemStats.Alloc-stats.MemoryBaseline) / (1024 * 1024)
	hourlyGrowth := memGrowth / actualDuration.Hours()

	t.Logf("  Memory Growth: %.2f MB (%.2f MB/hour)", memGrowth, hourlyGrowth)
	t.Logf("  Goroutines (min/max): %d / %d", stats.GoroutineMin, stats.GoroutineMax)
	t.Logf("  Total GC Runs: %d", finalMemStats.NumGC)
	t.Logf("")

	// Analyze snapshots for trends
	if len(stats.Snapshots) >= 2 {
		t.Logf("TREND ANALYSIS:")
		firstSnapshot := stats.Snapshots[0]
		lastSnapshot := stats.Snapshots[len(stats.Snapshots)-1]

		throughputChange := ((lastSnapshot.Throughput - firstSnapshot.Throughput) / firstSnapshot.Throughput) * 100
		memChange := lastSnapshot.MemoryMB - firstSnapshot.MemoryMB
		goroutineChange := lastSnapshot.GoroutineCount - firstSnapshot.GoroutineCount

		t.Logf("  Throughput Change: %+.1f%%", throughputChange)
		t.Logf("  Memory Trend: %+.2f MB", memChange)
		t.Logf("  Goroutine Trend: %+d", goroutineChange)

		// Check for degradation
		if throughputChange < -10 {
			t.Logf("  ⚠️  WARNING: Throughput degraded by %.1f%%", -throughputChange)
		}
		if memChange > 50 {
			t.Logf("  ⚠️  WARNING: Memory increased by %.2f MB", memChange)
		}
		if goroutineChange > 10 {
			t.Logf("  ⚠️  WARNING: Goroutine count increased by %d", goroutineChange)
		}
	}

	t.Logf("")

	// VALIDATION
	t.Logf("VALIDATION:")

	passed := true

	// Check throughput
	achievementPercent := (stats.Throughput() / float64(targetRPS)) * 100
	if achievementPercent < 90 {
		t.Errorf("  ❌ FAILED: Only achieved %.1f%% of target throughput", achievementPercent)
		passed = false
	} else {
		t.Logf("  ✅ Throughput: %.1f%% of target", achievementPercent)
	}

	// Check error rate
	if errorRate > 1.0 {
		t.Errorf("  ❌ FAILED: Error rate too high: %.4f%%", errorRate)
		passed = false
	} else {
		t.Logf("  ✅ Error Rate: %.4f%%", errorRate)
	}

	// Check memory stability
	if hourlyGrowth > 10 {
		t.Errorf("  ❌ FAILED: Memory leak detected: %.2f MB/hour", hourlyGrowth)
		passed = false
	} else if hourlyGrowth > 5 {
		t.Logf("  ⚠️  Memory growth: %.2f MB/hour (acceptable but monitor)", hourlyGrowth)
	} else {
		t.Logf("  ✅ Memory Stable: %.2f MB/hour", hourlyGrowth)
	}

	// Check goroutine stability
	goroutineGrowth := stats.GoroutineMax - stats.GoroutineMin
	if goroutineGrowth > 20 {
		t.Errorf("  ❌ FAILED: Goroutine leak detected: %d growth", goroutineGrowth)
		passed = false
	} else {
		t.Logf("  ✅ Goroutines Stable: %d baseline, %d peak", stats.GoroutineMin, stats.GoroutineMax)
	}

	// Check latency
	avgLatency := stats.AvgLatency()
	if avgLatency > 1*time.Second {
		t.Errorf("  ❌ FAILED: Average latency too high: %v", avgLatency)
		passed = false
	} else {
		t.Logf("  ✅ Latency: %v average", avgLatency)
	}

	t.Logf("")
	if passed {
		t.Logf("✅ ✅ ✅ SUSTAINED LOAD TEST PASSED ✅ ✅ ✅")
		t.Logf("System is PRODUCTION READY for %d logs/sec", targetRPS)
	} else {
		t.Logf("❌ ❌ ❌ SUSTAINED LOAD TEST FAILED ❌ ❌ ❌")
		t.Logf("System needs optimization before production deployment")
	}
}
