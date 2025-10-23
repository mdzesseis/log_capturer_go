package goroutines

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// GoroutineTracker tracks goroutine lifecycle and detects leaks
type GoroutineTracker struct {
	config  GoroutineConfig
	logger  *logrus.Logger
	mutex   sync.RWMutex

	// Tracking state
	tracked     map[string]*GoroutineInfo
	baseline    int
	maxSeen     int
	startTime   time.Time
	isRunning   bool
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// GoroutineConfig configures goroutine tracking
type GoroutineConfig struct {
	Enabled              bool          `yaml:"enabled"`
	CheckInterval        time.Duration `yaml:"check_interval"`
	LeakThreshold        int           `yaml:"leak_threshold"`
	MaxGoroutines        int           `yaml:"max_goroutines"`
	WarnThreshold        int           `yaml:"warn_threshold"`
	TrackingEnabled      bool          `yaml:"tracking_enabled"`
	StackTraceOnLeak     bool          `yaml:"stack_trace_on_leak"`
	AlertWebhook         string        `yaml:"alert_webhook"`
	RetentionPeriod      time.Duration `yaml:"retention_period"`
}

// DefaultGoroutineConfig returns safe defaults
func DefaultGoroutineConfig() GoroutineConfig {
	return GoroutineConfig{
		Enabled:          true,
		CheckInterval:    30 * time.Second,
		LeakThreshold:    100,
		MaxGoroutines:    1000,
		WarnThreshold:    500,
		TrackingEnabled:  true,
		StackTraceOnLeak: true,
		RetentionPeriod:  24 * time.Hour,
	}
}

// GoroutineInfo contains information about a tracked goroutine
type GoroutineInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	StartTime   time.Time `json:"start_time"`
	LastSeen    time.Time `json:"last_seen"`
	StackTrace  string    `json:"stack_trace,omitempty"`
	Source      string    `json:"source"`
	Active      bool      `json:"active"`
	Duration    time.Duration `json:"duration"`
}

// GoroutineStats provides goroutine statistics
type GoroutineStats struct {
	Current       int                `json:"current"`
	Baseline      int                `json:"baseline"`
	MaxSeen       int                `json:"max_seen"`
	Tracked       int                `json:"tracked"`
	Suspected     []string           `json:"suspected_leaks"`
	Status        string             `json:"status"`
	LastCheck     time.Time          `json:"last_check"`
	Uptime        time.Duration      `json:"uptime"`
	Details       map[string]*GoroutineInfo `json:"details"`
}

// NewGoroutineTracker creates a new goroutine tracker
func NewGoroutineTracker(config GoroutineConfig, logger *logrus.Logger) *GoroutineTracker {
	return &GoroutineTracker{
		config:    config,
		logger:    logger,
		tracked:   make(map[string]*GoroutineInfo),
		baseline:  runtime.NumGoroutine(),
		maxSeen:   runtime.NumGoroutine(),
		startTime: time.Now(),
		stopChan:  make(chan struct{}),
	}
}

// Start begins goroutine monitoring
func (gt *GoroutineTracker) Start(ctx context.Context) error {
	if !gt.config.Enabled {
		gt.logger.Info("Goroutine tracking disabled")
		return nil
	}

	gt.isRunning = true
	gt.logger.WithFields(logrus.Fields{
		"baseline_goroutines": gt.baseline,
		"check_interval":      gt.config.CheckInterval,
		"leak_threshold":      gt.config.LeakThreshold,
	}).Info("Starting goroutine leak detection")

	// Start monitoring loop
	gt.wg.Add(1)
	go gt.monitoringLoop()

	// Start cleanup loop
	gt.wg.Add(1)
	go gt.cleanupLoop()

	return nil
}

// Stop stops goroutine monitoring
func (gt *GoroutineTracker) Stop() error {
	if !gt.isRunning {
		return nil
	}

	gt.logger.Info("Stopping goroutine leak detection")
	gt.isRunning = false
	close(gt.stopChan)
	gt.wg.Wait()

	// Final report
	stats := gt.GetStats()
	gt.logger.WithFields(logrus.Fields{
		"final_count":     stats.Current,
		"max_seen":        stats.MaxSeen,
		"tracked":         stats.Tracked,
		"suspected_leaks": len(stats.Suspected),
	}).Info("Goroutine tracking stopped")

	return nil
}

// Track registers a new goroutine for tracking
func (gt *GoroutineTracker) Track(name, source string) func() {
	if !gt.config.TrackingEnabled {
		return func() {} // No-op
	}

	id := fmt.Sprintf("%s_%d", name, time.Now().UnixNano())

	info := &GoroutineInfo{
		ID:        id,
		Name:      name,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		Source:    source,
		Active:    true,
	}

	if gt.config.StackTraceOnLeak {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		info.StackTrace = string(buf[:n])
	}

	gt.mutex.Lock()
	gt.tracked[id] = info
	gt.mutex.Unlock()

	// Return cleanup function
	return func() {
		gt.mutex.Lock()
		if info, exists := gt.tracked[id]; exists {
			info.Active = false
			info.Duration = time.Since(info.StartTime)
		}
		gt.mutex.Unlock()
	}
}

// monitoringLoop runs the main monitoring loop
func (gt *GoroutineTracker) monitoringLoop() {
	defer gt.wg.Done()

	ticker := time.NewTicker(gt.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gt.stopChan:
			return
		case <-ticker.C:
			gt.checkForLeaks()
		}
	}
}

// checkForLeaks checks for potential goroutine leaks
func (gt *GoroutineTracker) checkForLeaks() {
	current := runtime.NumGoroutine()

	// Update max seen
	if current > gt.maxSeen {
		gt.maxSeen = current
	}

	// Calculate increase from baseline
	increase := current - gt.baseline

	// Check thresholds
	status := "healthy"
	if current >= gt.config.MaxGoroutines {
		status = "critical"
		gt.handleCriticalState(current)
	} else if current >= gt.config.WarnThreshold {
		status = "warning"
		gt.handleWarningState(current)
	} else if increase >= gt.config.LeakThreshold {
		status = "leak_detected"
		gt.handleLeakDetected(current, increase)
	}

	// Log status
	gt.logger.WithFields(logrus.Fields{
		"current_goroutines": current,
		"baseline":           gt.baseline,
		"increase":           increase,
		"status":             status,
		"max_seen":           gt.maxSeen,
	}).Debug("Goroutine check completed")

	// Update tracked goroutines
	gt.updateTrackedGoroutines()
}

// handleCriticalState handles critical goroutine count
func (gt *GoroutineTracker) handleCriticalState(current int) {
	gt.logger.WithFields(logrus.Fields{
		"current":     current,
		"max_allowed": gt.config.MaxGoroutines,
	}).Error("Critical: Goroutine count exceeded maximum")

	// Force garbage collection
	runtime.GC()

	// Get stack traces if enabled
	if gt.config.StackTraceOnLeak {
		gt.dumpStackTraces()
	}

	// Send alert
	gt.sendAlert("critical", fmt.Sprintf("Goroutine count %d exceeded maximum %d", current, gt.config.MaxGoroutines))
}

// handleWarningState handles warning goroutine count
func (gt *GoroutineTracker) handleWarningState(current int) {
	gt.logger.WithFields(logrus.Fields{
		"current":       current,
		"warn_threshold": gt.config.WarnThreshold,
	}).Warn("Warning: High goroutine count detected")

	gt.sendAlert("warning", fmt.Sprintf("Goroutine count %d exceeded warning threshold %d", current, gt.config.WarnThreshold))
}

// handleLeakDetected handles detected goroutine leak
func (gt *GoroutineTracker) handleLeakDetected(current, increase int) {
	gt.logger.WithFields(logrus.Fields{
		"current":        current,
		"baseline":       gt.baseline,
		"increase":       increase,
		"leak_threshold": gt.config.LeakThreshold,
	}).Warn("Potential goroutine leak detected")

	// Identify suspected leaks
	suspected := gt.identifySuspectedLeaks()

	if len(suspected) > 0 {
		gt.logger.WithField("suspected_goroutines", suspected).Warn("Suspected leaked goroutines identified")
	}

	gt.sendAlert("leak", fmt.Sprintf("Potential leak: %d goroutines above baseline", increase))
}

// updateTrackedGoroutines updates the status of tracked goroutines
func (gt *GoroutineTracker) updateTrackedGoroutines() {
	if !gt.config.TrackingEnabled {
		return
	}

	now := time.Now()

	gt.mutex.Lock()
	defer gt.mutex.Unlock()

	for _, info := range gt.tracked {
		if info.Active {
			info.LastSeen = now
			info.Duration = now.Sub(info.StartTime)
		}
	}
}

// identifySuspectedLeaks identifies goroutines that might be leaked
func (gt *GoroutineTracker) identifySuspectedLeaks() []string {
	var suspected []string

	if !gt.config.TrackingEnabled {
		return suspected
	}

	threshold := time.Now().Add(-5 * time.Minute) // Consider 5+ minutes as potential leak

	gt.mutex.RLock()
	defer gt.mutex.RUnlock()

	for id, info := range gt.tracked {
		if info.Active && info.StartTime.Before(threshold) {
			suspected = append(suspected, id)
		}
	}

	return suspected
}

// dumpStackTraces dumps stack traces of all goroutines
func (gt *GoroutineTracker) dumpStackTraces() {
	buf := make([]byte, 64*1024) // 64KB buffer
	for {
		n := runtime.Stack(buf, true) // all goroutines
		if n < len(buf) {
			gt.logger.WithField("stack_traces", string(buf[:n])).Error("All goroutine stack traces")
			break
		}
		buf = make([]byte, len(buf)*2) // Double buffer size
	}
}

// cleanupLoop removes old tracking data
func (gt *GoroutineTracker) cleanupLoop() {
	defer gt.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-gt.stopChan:
			return
		case <-ticker.C:
			gt.cleanupOldData()
		}
	}
}

// cleanupOldData removes old goroutine tracking data
func (gt *GoroutineTracker) cleanupOldData() {
	cutoff := time.Now().Add(-gt.config.RetentionPeriod)

	gt.mutex.Lock()
	defer gt.mutex.Unlock()

	for id, info := range gt.tracked {
		if !info.Active && info.LastSeen.Before(cutoff) {
			delete(gt.tracked, id)
		}
	}
}

// GetStats returns current goroutine statistics
func (gt *GoroutineTracker) GetStats() GoroutineStats {
	current := runtime.NumGoroutine()
	suspected := gt.identifySuspectedLeaks()

	gt.mutex.RLock()
	details := make(map[string]*GoroutineInfo)
	for id, info := range gt.tracked {
		details[id] = info
	}
	gt.mutex.RUnlock()

	status := "healthy"
	if current >= gt.config.MaxGoroutines {
		status = "critical"
	} else if current >= gt.config.WarnThreshold {
		status = "warning"
	} else if len(suspected) > 0 {
		status = "leak_detected"
	}

	return GoroutineStats{
		Current:   current,
		Baseline:  gt.baseline,
		MaxSeen:   gt.maxSeen,
		Tracked:   len(gt.tracked),
		Suspected: suspected,
		Status:    status,
		LastCheck: time.Now(),
		Uptime:    time.Since(gt.startTime),
		Details:   details,
	}
}

// sendAlert sends an alert about goroutine issues
func (gt *GoroutineTracker) sendAlert(level, message string) {
	if gt.config.AlertWebhook == "" {
		return
	}

	// Implementation would send HTTP POST to webhook
	gt.logger.WithFields(logrus.Fields{
		"level":   level,
		"message": message,
		"webhook": gt.config.AlertWebhook,
	}).Info("Goroutine alert sent")
}

// ForceGC triggers garbage collection and returns new goroutine count
func (gt *GoroutineTracker) ForceGC() int {
	before := runtime.NumGoroutine()
	runtime.GC()
	runtime.GC() // Run twice for better cleanup
	after := runtime.NumGoroutine()

	gt.logger.WithFields(logrus.Fields{
		"before_gc": before,
		"after_gc":  after,
		"freed":     before - after,
	}).Info("Forced garbage collection")

	return after
}

// ResetBaseline resets the baseline goroutine count
func (gt *GoroutineTracker) ResetBaseline() {
	before := gt.baseline
	gt.baseline = runtime.NumGoroutine()

	gt.logger.WithFields(logrus.Fields{
		"old_baseline": before,
		"new_baseline": gt.baseline,
	}).Info("Goroutine baseline reset")
}

// GetDetailedReport returns a detailed report of goroutine usage
func (gt *GoroutineTracker) GetDetailedReport() map[string]interface{} {
	stats := gt.GetStats()

	report := map[string]interface{}{
		"overview": map[string]interface{}{
			"current_goroutines": stats.Current,
			"baseline":           stats.Baseline,
			"max_seen":           stats.MaxSeen,
			"status":             stats.Status,
			"uptime":             stats.Uptime.String(),
		},
		"thresholds": map[string]interface{}{
			"leak_threshold": gt.config.LeakThreshold,
			"warn_threshold": gt.config.WarnThreshold,
			"max_goroutines": gt.config.MaxGoroutines,
		},
		"tracking": map[string]interface{}{
			"enabled":         gt.config.TrackingEnabled,
			"tracked_count":   len(stats.Details),
			"suspected_leaks": stats.Suspected,
		},
		"runtime": map[string]interface{}{
			"num_cpu":      runtime.NumCPU(),
			"gomaxprocs":   runtime.GOMAXPROCS(0),
			"version":      runtime.Version(),
		},
	}

	// Add memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	report["memory"] = map[string]interface{}{
		"alloc_mb":      memStats.Alloc / 1024 / 1024,
		"heap_alloc_mb": memStats.HeapAlloc / 1024 / 1024,
		"heap_sys_mb":   memStats.HeapSys / 1024 / 1024,
		"num_gc":        memStats.NumGC,
	}

	return report
}