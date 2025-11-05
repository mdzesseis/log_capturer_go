package profiling

import (
	"bytes"
	"fmt"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// GoroutineTracker tracks goroutine creation and leaks
type GoroutineTracker struct {
	logger       *logrus.Logger
	baseline     int
	lastCount    int
	lastCheck    time.Time
	samples      []Sample
	mu           sync.RWMutex
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// Sample represents a goroutine count sample with stack trace summary
type Sample struct {
	Timestamp time.Time
	Count     int
	Growth    int
	TopStacks []StackInfo
}

// StackInfo contains information about a goroutine stack
type StackInfo struct {
	Function string
	Count    int
}

// NewGoroutineTracker creates a new goroutine tracker
func NewGoroutineTracker(logger *logrus.Logger) *GoroutineTracker {
	baseline := runtime.NumGoroutine()

	logger.WithFields(logrus.Fields{
		"baseline_goroutines": baseline,
		"timestamp":          time.Now(),
	}).Info("Goroutine tracker initialized")

	return &GoroutineTracker{
		logger:    logger,
		baseline:  baseline,
		lastCount: baseline,
		lastCheck: time.Now(),
		samples:   make([]Sample, 0, 100),
		stopCh:    make(chan struct{}),
	}
}

// Start begins periodic goroutine monitoring
func (gt *GoroutineTracker) Start(interval time.Duration) {
	gt.wg.Add(1)
	go func() {
		defer gt.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-gt.stopCh:
				gt.logger.Info("Goroutine tracker stopped")
				return
			case <-ticker.C:
				gt.captureSnapshot()
			}
		}
	}()
}

// Stop stops the goroutine tracker
func (gt *GoroutineTracker) Stop() {
	close(gt.stopCh)
	gt.wg.Wait()
}

// captureSnapshot captures current goroutine state
func (gt *GoroutineTracker) captureSnapshot() {
	currentCount := runtime.NumGoroutine()
	growth := currentCount - gt.lastCount

	// Capture stack traces for analysis
	topStacks := gt.analyzeStacks()

	gt.mu.Lock()
	sample := Sample{
		Timestamp: time.Now(),
		Count:     currentCount,
		Growth:    growth,
		TopStacks: topStacks,
	}
	gt.samples = append(gt.samples, sample)

	// Keep only last 100 samples
	if len(gt.samples) > 100 {
		gt.samples = gt.samples[1:]
	}
	gt.mu.Unlock()

	// Log significant changes
	if growth >= 5 || growth <= -5 {
		gt.logger.WithFields(logrus.Fields{
			"current_count": currentCount,
			"growth":        growth,
			"baseline":      gt.baseline,
			"total_growth":  currentCount - gt.baseline,
			"duration":      time.Since(gt.lastCheck).Seconds(),
		}).Warn("Significant goroutine count change detected")

		// Log top goroutine stacks
		if len(topStacks) > 0 {
			gt.logger.WithFields(logrus.Fields{
				"top_stacks": topStacks,
			}).Info("Top goroutine stack traces")
		}
	}

	gt.lastCount = currentCount
	gt.lastCheck = time.Now()
}

// analyzeStacks analyzes goroutine stack traces and returns top functions
func (gt *GoroutineTracker) analyzeStacks() []StackInfo {
	// Get goroutine profile
	var buf bytes.Buffer
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return nil
	}

	// Write profile to buffer
	err := profile.WriteTo(&buf, 1)
	if err != nil {
		gt.logger.WithError(err).Error("Failed to write goroutine profile")
		return nil
	}

	// Parse stack traces
	stackCounts := make(map[string]int)
	lines := strings.Split(buf.String(), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for function calls in stack traces
		// Format: "function_name(args)"
		if strings.Contains(line, "(") && !strings.HasPrefix(line, "#") &&
		   !strings.HasPrefix(line, "goroutine") && line != "" {
			// Extract function name
			parts := strings.Split(line, "(")
			if len(parts) > 0 {
				funcName := strings.TrimSpace(parts[0])
				if funcName != "" {
					stackCounts[funcName]++
				}
			}
		}
	}

	// Convert to slice and sort by count
	stacks := make([]StackInfo, 0, len(stackCounts))
	for fn, count := range stackCounts {
		stacks = append(stacks, StackInfo{
			Function: fn,
			Count:    count,
		})
	}

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Count > stacks[j].Count
	})

	// Return top 10
	if len(stacks) > 10 {
		stacks = stacks[:10]
	}

	return stacks
}

// GetStats returns current tracking statistics
func (gt *GoroutineTracker) GetStats() map[string]interface{} {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	currentCount := runtime.NumGoroutine()
	growth := currentCount - gt.baseline
	growthRate := gt.GetRecentGrowthRate()

	// Determine status based on growth
	status := "healthy"
	if growthRate > 30 {  // More than 30 gor/min growth
		status = "critical"
	} else if growthRate > 10 {  // More than 10 gor/min growth
		status = "warning"
	}

	return map[string]interface{}{
		"baseline_goroutines": gt.baseline,
		"current_goroutines":  currentCount,
		"total_growth":        growth,
		"growth_rate_per_min": growthRate,
		"last_check":          gt.lastCheck,
		"samples_collected":   len(gt.samples),
		"status":              status,
	}
}

// DumpFullProfile dumps full goroutine profile to log
func (gt *GoroutineTracker) DumpFullProfile() {
	var buf bytes.Buffer
	profile := pprof.Lookup("goroutine")
	if profile == nil {
		gt.logger.Error("Goroutine profile not available")
		return
	}

	err := profile.WriteTo(&buf, 2) // debug=2 for full stacks
	if err != nil {
		gt.logger.WithError(err).Error("Failed to write goroutine profile")
		return
	}

	// Log in chunks to avoid truncation
	output := buf.String()
	lines := strings.Split(output, "\n")

	chunkSize := 100
	for i := 0; i < len(lines); i += chunkSize {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunk := strings.Join(lines[i:end], "\n")
		gt.logger.WithFields(logrus.Fields{
			"chunk": fmt.Sprintf("%d-%d", i, end),
			"total": len(lines),
		}).Debug(chunk)
	}
}

// GetRecentGrowthRate returns goroutines/minute growth rate
func (gt *GoroutineTracker) GetRecentGrowthRate() float64 {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	if len(gt.samples) < 2 {
		return 0
	}

	// Calculate growth rate over last 5 minutes or all samples
	lookback := 5
	if len(gt.samples) < lookback {
		lookback = len(gt.samples)
	}

	samples := gt.samples[len(gt.samples)-lookback:]
	firstSample := samples[0]
	lastSample := samples[len(samples)-1]

	growth := lastSample.Count - firstSample.Count
	duration := lastSample.Timestamp.Sub(firstSample.Timestamp).Minutes()

	if duration == 0 {
		return 0
	}

	return float64(growth) / duration
}
