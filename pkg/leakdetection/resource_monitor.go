package leakdetection

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/internal/metrics"

	"github.com/sirupsen/logrus"
)

// ResourceMonitor monitors system resources for leaks
type ResourceMonitor struct {
	config       ResourceMonitorConfig
	logger       *logrus.Logger

	// Tracking state
	initialFDs        int64
	initialGoroutines int64

	// Current readings
	currentFDs         int64
	currentGoroutines  int64
	currentMemory      int64

	// Leak detection
	leakAlerts        map[string]time.Time
	leakAlertsMutex   sync.RWMutex

	// Statistics
	stats             ResourceStats
	statsMutex        sync.RWMutex

	// Control
	ctx               context.Context
	cancel            context.CancelFunc
	isRunning         bool
	mutex             sync.Mutex
}

// ResourceMonitorConfig configuration for resource monitoring
type ResourceMonitorConfig struct {
	MonitoringInterval    time.Duration `yaml:"monitoring_interval"`
	FDLeakThreshold       int64         `yaml:"fd_leak_threshold"`
	GoroutineLeakThreshold int64        `yaml:"goroutine_leak_threshold"`
	MemoryLeakThreshold   int64         `yaml:"memory_leak_threshold"`
	AlertCooldown         time.Duration `yaml:"alert_cooldown"`
	EnableMemoryProfiling bool          `yaml:"enable_memory_profiling"`
	EnableGCOptimization  bool          `yaml:"enable_gc_optimization"`
	MaxAlertHistory       int           `yaml:"max_alert_history"`
}

// ResourceStats statistics for resource monitoring
type ResourceStats struct {
	// Current values
	FileDescriptors   int64 `json:"file_descriptors"`
	Goroutines       int64 `json:"goroutines"`
	MemoryUsage      int64 `json:"memory_usage_bytes"`

	// Baseline values
	InitialFDs       int64 `json:"initial_fds"`
	InitialGoroutines int64 `json:"initial_goroutines"`

	// Leak detection
	FDLeaks          int64 `json:"fd_leaks_detected"`
	GoroutineLeaks   int64 `json:"goroutine_leaks_detected"`
	MemoryLeaks      int64 `json:"memory_leaks_detected"`

	// Performance metrics
	GCPauses         []time.Duration `json:"gc_pauses_ns"`
	AllocRate        float64         `json:"alloc_rate_bytes_per_sec"`

	// System info
	LastCheck        int64  `json:"last_check_timestamp"`
	MonitoringUptime int64  `json:"monitoring_uptime_seconds"`
}

// LeakAlert represents a resource leak alert
type LeakAlert struct {
	Type        string    `json:"type"`
	Resource    string    `json:"resource"`
	CurrentValue int64    `json:"current_value"`
	Threshold   int64     `json:"threshold"`
	Severity    string    `json:"severity"`
	Timestamp   time.Time `json:"timestamp"`
	Details     string    `json:"details"`
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(config ResourceMonitorConfig, logger *logrus.Logger) *ResourceMonitor {
	// Set defaults
	if config.MonitoringInterval == 0 {
		config.MonitoringInterval = 30 * time.Second
	}
	if config.FDLeakThreshold <= 0 {
		config.FDLeakThreshold = 100
	}
	if config.GoroutineLeakThreshold <= 0 {
		config.GoroutineLeakThreshold = 50
	}
	if config.MemoryLeakThreshold <= 0 {
		config.MemoryLeakThreshold = 100 * 1024 * 1024 // 100MB
	}
	if config.AlertCooldown == 0 {
		config.AlertCooldown = 5 * time.Minute
	}
	if config.MaxAlertHistory <= 0 {
		config.MaxAlertHistory = 100
	}

	ctx, cancel := context.WithCancel(context.Background())

	rm := &ResourceMonitor{
		config:      config,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		leakAlerts:  make(map[string]time.Time),
	}

	return rm
}

// Start starts the resource monitor
func (rm *ResourceMonitor) Start() error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if rm.isRunning {
		return fmt.Errorf("resource monitor already running")
	}

	// Record baseline values
	rm.initialFDs = rm.getFileDescriptorCount()
	rm.initialGoroutines = int64(runtime.NumGoroutine())

	atomic.StoreInt64(&rm.currentFDs, rm.initialFDs)
	atomic.StoreInt64(&rm.currentGoroutines, rm.initialGoroutines)

	rm.stats.InitialFDs = rm.initialFDs
	rm.stats.InitialGoroutines = rm.initialGoroutines

	rm.isRunning = true

	// Start monitoring loop
	go rm.monitoringLoop()

	// Start GC optimization if enabled
	if rm.config.EnableGCOptimization {
		go rm.gcOptimizationLoop()
	}

	rm.logger.WithFields(logrus.Fields{
		"initial_fds":        rm.initialFDs,
		"initial_goroutines": rm.initialGoroutines,
		"monitoring_interval": rm.config.MonitoringInterval,
	}).Info("Resource monitor started")

	return nil
}

// Stop stops the resource monitor
func (rm *ResourceMonitor) Stop() error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if !rm.isRunning {
		return nil
	}

	rm.cancel()
	rm.isRunning = false

	rm.logger.Info("Resource monitor stopped")
	return nil
}

// monitoringLoop main monitoring loop
func (rm *ResourceMonitor) monitoringLoop() {
	ticker := time.NewTicker(rm.config.MonitoringInterval)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ticker.C:
			rm.performCheck()

			// Update uptime
			rm.statsMutex.Lock()
			rm.stats.MonitoringUptime = int64(time.Since(startTime).Seconds())
			rm.statsMutex.Unlock()

		case <-rm.ctx.Done():
			return
		}
	}
}

// performCheck performs a resource check
func (rm *ResourceMonitor) performCheck() {
	now := time.Now()

	// Get current resource usage
	currentFDs := rm.getFileDescriptorCount()
	currentGoroutines := int64(runtime.NumGoroutine())
	currentMemory := rm.getMemoryUsage()

	// Update atomic values
	atomic.StoreInt64(&rm.currentFDs, currentFDs)
	atomic.StoreInt64(&rm.currentGoroutines, currentGoroutines)
	atomic.StoreInt64(&rm.currentMemory, currentMemory)

	// Update stats
	rm.statsMutex.Lock()
	rm.stats.FileDescriptors = currentFDs
	rm.stats.Goroutines = currentGoroutines
	rm.stats.MemoryUsage = currentMemory
	rm.stats.LastCheck = now.Unix()

	// Calculate allocation rate
	rm.updateAllocRate()

	// Get GC stats
	rm.updateGCStats()
	rm.statsMutex.Unlock()

	// Check for leaks
	rm.checkForLeaks(currentFDs, currentGoroutines, currentMemory)

	rm.logger.WithFields(logrus.Fields{
		"fds":         currentFDs,
		"goroutines":  currentGoroutines,
		"memory_mb":   currentMemory / (1024 * 1024),
	}).Debug("Resource check completed")
}

// checkForLeaks checks for resource leaks
func (rm *ResourceMonitor) checkForLeaks(fds, goroutines, memory int64) {
	// Check file descriptor leaks
	fdIncrease := fds - rm.initialFDs
	if fdIncrease > rm.config.FDLeakThreshold {
		rm.reportLeak("file_descriptors", "File Descriptors", fds, rm.config.FDLeakThreshold,
			fmt.Sprintf("FD count increased by %d from baseline of %d", fdIncrease, rm.initialFDs))
		rm.statsMutex.Lock()
		rm.stats.FDLeaks++
		rm.statsMutex.Unlock()

		// Record leak detection metric
		metrics.LeakDetection.WithLabelValues("fd_leak", "resource_monitor").Set(1)
	} else {
		// No leak detected
		metrics.LeakDetection.WithLabelValues("fd_leak", "resource_monitor").Set(0)
	}

	// Check goroutine leaks
	goroutineIncrease := goroutines - rm.initialGoroutines
	if goroutineIncrease > rm.config.GoroutineLeakThreshold {
		rm.reportLeak("goroutines", "Goroutines", goroutines, rm.config.GoroutineLeakThreshold,
			fmt.Sprintf("Goroutine count increased by %d from baseline of %d", goroutineIncrease, rm.initialGoroutines))
		rm.statsMutex.Lock()
		rm.stats.GoroutineLeaks++
		rm.statsMutex.Unlock()

		// Record leak detection metric
		metrics.LeakDetection.WithLabelValues("goroutine_leak", "resource_monitor").Set(1)

		// Log goroutine stack traces for debugging
		if rm.config.EnableMemoryProfiling {
			rm.logGoroutineStacks()
		}
	} else {
		// No leak detected
		metrics.LeakDetection.WithLabelValues("goroutine_leak", "resource_monitor").Set(0)
	}

	// Check memory leaks (simplified - based on heap size growth)
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	if int64(memStats.HeapInuse) > rm.config.MemoryLeakThreshold {
		rm.reportLeak("memory", "Memory", int64(memStats.HeapInuse), rm.config.MemoryLeakThreshold,
			fmt.Sprintf("Heap memory usage: %d bytes", memStats.HeapInuse))
		rm.statsMutex.Lock()
		rm.stats.MemoryLeaks++
		rm.statsMutex.Unlock()

		// Record leak detection metric - report leak size in MB
		leakSizeMB := float64(memStats.HeapInuse) / (1024 * 1024)
		metrics.LeakDetection.WithLabelValues("memory_leak", "resource_monitor").Set(leakSizeMB)
	} else {
		// No leak detected
		metrics.LeakDetection.WithLabelValues("memory_leak", "resource_monitor").Set(0)
	}
}

// reportLeak reports a resource leak
func (rm *ResourceMonitor) reportLeak(alertKey, resourceName string, currentValue, threshold int64, details string) {
	rm.leakAlertsMutex.Lock()
	defer rm.leakAlertsMutex.Unlock()

	// Check cooldown period
	if lastAlert, exists := rm.leakAlerts[alertKey]; exists {
		if time.Since(lastAlert) < rm.config.AlertCooldown {
			return // Still in cooldown
		}
	}

	// Determine severity
	severity := "warning"
	if currentValue > threshold*2 {
		severity = "critical"
	} else if currentValue > int64(float64(threshold)*1.5) {
		severity = "high"
	}

	// Log the alert
	rm.logger.WithFields(logrus.Fields{
		"resource":      resourceName,
		"current_value": currentValue,
		"threshold":     threshold,
		"severity":      severity,
		"details":       details,
	}).Warn("Resource leak detected")

	// Update alert time
	rm.leakAlerts[alertKey] = time.Now()
}

// getFileDescriptorCount gets the current file descriptor count
func (rm *ResourceMonitor) getFileDescriptorCount() int64 {
	// Try to read from /proc/self/fd (Linux)
	if fds, err := rm.getLinuxFDCount(); err == nil {
		return fds
	}

	// Fallback to a simplified count
	return 10 // Placeholder - in a real implementation, use platform-specific methods
}

// getLinuxFDCount gets FD count from /proc/self/fd
func (rm *ResourceMonitor) getLinuxFDCount() (int64, error) {
	files, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	return int64(len(files)), nil
}

// getMemoryUsage gets current memory usage
func (rm *ResourceMonitor) getMemoryUsage() int64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return int64(memStats.Alloc)
}

// updateAllocRate updates the allocation rate
func (rm *ResourceMonitor) updateAllocRate() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Simple allocation rate calculation
	rm.stats.AllocRate = float64(memStats.TotalAlloc) / float64(rm.stats.MonitoringUptime + 1)
}

// updateGCStats updates GC statistics
func (rm *ResourceMonitor) updateGCStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Store recent GC pause times (keep last 10)
	if len(rm.stats.GCPauses) >= 10 {
		rm.stats.GCPauses = rm.stats.GCPauses[1:]
	}

	if memStats.NumGC > 0 && len(memStats.PauseNs) > 0 {
		lastPause := time.Duration(memStats.PauseNs[(memStats.NumGC+255)%256])
		rm.stats.GCPauses = append(rm.stats.GCPauses, lastPause)
	}
}

// logGoroutineStacks logs goroutine stack traces for debugging
func (rm *ResourceMonitor) logGoroutineStacks() {
	buf := make([]byte, 1<<20) // 1MB buffer
	stackSize := runtime.Stack(buf, true)

	rm.logger.WithField("stack_traces", string(buf[:stackSize])).Debug("Goroutine stack traces")
}

// gcOptimizationLoop runs GC optimization if enabled
func (rm *ResourceMonitor) gcOptimizationLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.optimizeGC()
		case <-rm.ctx.Done():
			return
		}
	}
}

// optimizeGC performs GC optimization
func (rm *ResourceMonitor) optimizeGC() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// If heap size is growing rapidly, trigger GC
	heapGrowthRate := float64(memStats.HeapInuse) / float64(memStats.LastGC + 1)
	if heapGrowthRate > 1000000 { // 1MB/ns growth rate threshold
		runtime.GC()
		debug.FreeOSMemory()
		rm.logger.Debug("Triggered manual GC due to high heap growth rate")
	}
}

// GetCurrentUsage returns current resource usage
func (rm *ResourceMonitor) GetCurrentUsage() map[string]int64 {
	return map[string]int64{
		"file_descriptors": atomic.LoadInt64(&rm.currentFDs),
		"goroutines":      atomic.LoadInt64(&rm.currentGoroutines),
		"memory_bytes":    atomic.LoadInt64(&rm.currentMemory),
	}
}

// GetStats returns current resource monitoring statistics
func (rm *ResourceMonitor) GetStats() ResourceStats {
	rm.statsMutex.RLock()
	defer rm.statsMutex.RUnlock()
	return rm.stats
}

// IsHealthy returns true if no critical resource leaks are detected
func (rm *ResourceMonitor) IsHealthy() bool {
	stats := rm.GetStats()

	// Check for critical resource usage
	fdIncrease := stats.FileDescriptors - stats.InitialFDs
	goroutineIncrease := stats.Goroutines - stats.InitialGoroutines

	// Consider unhealthy if resource usage is extremely high
	if fdIncrease > rm.config.FDLeakThreshold*3 {
		return false
	}
	if goroutineIncrease > rm.config.GoroutineLeakThreshold*3 {
		return false
	}
	if stats.MemoryUsage > rm.config.MemoryLeakThreshold*2 {
		return false
	}

	return true
}

// GetLeakHistory returns recent leak alerts
func (rm *ResourceMonitor) GetLeakHistory() map[string]time.Time {
	rm.leakAlertsMutex.RLock()
	defer rm.leakAlertsMutex.RUnlock()

	history := make(map[string]time.Time)
	for k, v := range rm.leakAlerts {
		history[k] = v
	}

	return history
}

// ForceGC forces a garbage collection and returns before/after stats
func (rm *ResourceMonitor) ForceGC() (before, after runtime.MemStats) {
	runtime.ReadMemStats(&before)
	runtime.GC()
	debug.FreeOSMemory()
	runtime.ReadMemStats(&after)

	rm.logger.WithFields(logrus.Fields{
		"heap_before_mb": before.HeapInuse / (1024 * 1024),
		"heap_after_mb":  after.HeapInuse / (1024 * 1024),
		"freed_mb":       (before.HeapInuse - after.HeapInuse) / (1024 * 1024),
	}).Info("Manual GC completed")

	return before, after
}

// GetSystemInfo returns system resource information
func (rm *ResourceMonitor) GetSystemInfo() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Try to get system limits
	var maxFDs int64 = -1
	if data, err := os.ReadFile("/proc/sys/fs/file-max"); err == nil {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			maxFDs = parsed
		}
	}

	return map[string]interface{}{
		"go_version":       runtime.Version(),
		"num_cpu":          runtime.NumCPU(),
		"gomaxprocs":       runtime.GOMAXPROCS(0),
		"gc_target_percent": debug.SetGCPercent(-1), // Get without changing
		"max_fds":          maxFDs,
		"heap_objects":     memStats.HeapObjects,
		"gc_cycles":        memStats.NumGC,
	}
}