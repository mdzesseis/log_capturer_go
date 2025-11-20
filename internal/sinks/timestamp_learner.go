package sinks

import (
	"fmt"
	"regexp"
	"ssw-logs-capture/pkg/types"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TimestampLearner learns acceptable timestamp ranges from Loki rejections
// and validates timestamps before sending to prevent permanent errors.
//
// Key Features:
//   - Learns maxAcceptableAge from Loki "timestamp too old" errors
//   - Validates timestamps before sending (prevents retry storm)
//   - Optional timestamp clamping to acceptable range
//   - Thread-safe for concurrent use
//   - Metrics integration for observability
//
// Problem Solved:
//   - File monitor reads old logs (days/months old)
//   - Loki rejects: "timestamp too old for stream" (400)
//   - System retries permanently failing entries → goroutine leak
//   - Solution: Validate BEFORE sending, reject with NO RETRY
type TimestampLearner interface {
	// LearnFromRejection learns from Loki rejection response
	LearnFromRejection(errorMsg string, entry *types.LogEntry) error

	// ValidateTimestamp validates timestamp before sending
	ValidateTimestamp(entry *types.LogEntry) error

	// GetMaxAcceptableAge returns current learned threshold
	GetMaxAcceptableAge() time.Duration

	// ClampTimestamp clamps timestamp to acceptable range (optional)
	ClampTimestamp(entry *types.LogEntry) bool
}

// timestampLearner implements TimestampLearner interface
type timestampLearner struct {
	mu                sync.RWMutex
	maxAcceptableAge  time.Duration
	defaultMaxAge     time.Duration
	learnedFromErrors int64
	lastLearned       time.Time
	config            TimestampLearnerConfig
	logger            *logrus.Logger

	// Regex patterns for parsing Loki errors
	timestampTooOldRegex *regexp.Regexp
}

// TimestampLearnerConfig configuration for timestamp learning
type TimestampLearnerConfig struct {
	Enabled           bool          // Enable timestamp learning (default: true)
	DefaultMaxAge     time.Duration // Default max acceptable age (default: 24h)
	ClampEnabled      bool          // Enable timestamp clamping (default: false)
	LearnFromErrors   bool          // Learn from Loki errors (default: true)
	MinLearningWindow time.Duration // Min interval between learning (default: 5m)
}

// Sentinel errors for timestamp validation
var (
	ErrTimestampTooOld = fmt.Errorf("timestamp too old for sink")
	ErrTimestampTooNew = fmt.Errorf("timestamp too far in the future")
	ErrTimestampZero   = fmt.Errorf("timestamp is zero value")
)

// NewTimestampLearner creates a new timestamp learner
func NewTimestampLearner(config TimestampLearnerConfig, logger *logrus.Logger) TimestampLearner {
	if logger == nil {
		logger = logrus.New()
	}

	// Set defaults if not configured
	if config.DefaultMaxAge == 0 {
		config.DefaultMaxAge = 24 * time.Hour
	}
	if config.MinLearningWindow == 0 {
		config.MinLearningWindow = 5 * time.Minute
	}

	tl := &timestampLearner{
		maxAcceptableAge: config.DefaultMaxAge,
		defaultMaxAge:    config.DefaultMaxAge,
		config:           config,
		logger:           logger,
	}

	// Compile regex patterns for error parsing
	// Example: "timestamp too old: entry timestamp is 2025-11-05, stream time is 2025-11-07"
	tl.timestampTooOldRegex = regexp.MustCompile(`timestamp too old|too old|out of order`)

	logger.WithFields(logrus.Fields{
		"enabled":           config.Enabled,
		"default_max_age":   config.DefaultMaxAge,
		"clamp_enabled":     config.ClampEnabled,
		"learn_from_errors": config.LearnFromErrors,
	}).Info("Timestamp learner initialized")

	return tl
}

// LearnFromRejection learns from Loki rejection errors
//
// Parses error messages like:
//   - "timestamp too old: entry timestamp is 2025-11-05, stream time is 2025-11-07"
//   - "entry too old, oldest acceptable timestamp is: 2025-11-06T12:00:00Z"
//
// Extracts the age difference and updates maxAcceptableAge if:
//   1. Pattern matches known rejection types
//   2. MinLearningWindow elapsed since last learning
//   3. New threshold is more restrictive than default
func (tl *timestampLearner) LearnFromRejection(errorMsg string, entry *types.LogEntry) error {
	if !tl.config.LearnFromErrors {
		return nil
	}

	tl.mu.Lock()
	defer tl.mu.Unlock()

	// Rate limit learning (don't update too frequently)
	if time.Since(tl.lastLearned) < tl.config.MinLearningWindow {
		return nil
	}

	// Check if error message indicates timestamp rejection
	if !tl.timestampTooOldRegex.MatchString(strings.ToLower(errorMsg)) {
		return nil
	}

	// Calculate age of rejected entry
	age := time.Since(entry.Timestamp)

	// If age is older than current threshold, update it
	// (Loki is telling us our threshold is too generous)
	if age < tl.maxAcceptableAge {
		oldThreshold := tl.maxAcceptableAge
		tl.maxAcceptableAge = age - (1 * time.Hour) // Add 1h buffer for safety
		tl.learnedFromErrors++
		tl.lastLearned = time.Now()

		tl.logger.WithFields(logrus.Fields{
			"old_threshold_hours": oldThreshold.Hours(),
			"new_threshold_hours": tl.maxAcceptableAge.Hours(),
			"rejected_age_hours":  age.Hours(),
			"learned_count":       tl.learnedFromErrors,
		}).Info("Learned new timestamp threshold from Loki rejection")
	}

	return nil
}

// ValidateTimestamp validates if timestamp is acceptable
//
// Returns:
//   - nil if timestamp is valid
//   - ErrTimestampTooOld if timestamp exceeds maxAcceptableAge
//   - ErrTimestampTooNew if timestamp is too far in future
//   - ErrTimestampZero if timestamp is zero value
func (tl *timestampLearner) ValidateTimestamp(entry *types.LogEntry) error {
	tl.mu.RLock()
	maxAge := tl.maxAcceptableAge
	tl.mu.RUnlock()

	// Check for zero timestamp
	if entry.Timestamp.IsZero() {
		return ErrTimestampZero
	}

	now := time.Now()

	// Check if too old
	age := now.Sub(entry.Timestamp)
	if age > maxAge {
		return fmt.Errorf("%w: age=%s, max=%s", ErrTimestampTooOld, age.Round(time.Second), maxAge.Round(time.Second))
	}

	// Check if too far in future (prevent clock drift issues)
	// Allow up to 5 minutes in future for clock skew
	if entry.Timestamp.After(now.Add(5 * time.Minute)) {
		future := entry.Timestamp.Sub(now)
		return fmt.Errorf("%w: %s in future", ErrTimestampTooNew, future.Round(time.Second))
	}

	return nil
}

// GetMaxAcceptableAge returns current learned threshold
func (tl *timestampLearner) GetMaxAcceptableAge() time.Duration {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return tl.maxAcceptableAge
}

// ClampTimestamp clamps timestamp to acceptable range
//
// If timestamp is too old, clamps it to (now - maxAcceptableAge).
// Adds labels to indicate clamping occurred:
//   - "_timestamp_clamped": "true"
//   - "_original_age_hours": "72.5"
//
// Returns:
//   - true if timestamp was clamped
//   - false if timestamp was already valid
//
// WARNING: This modifies the original LogEntry timestamp!
func (tl *timestampLearner) ClampTimestamp(entry *types.LogEntry) bool {
	if !tl.config.ClampEnabled {
		return false
	}

	tl.mu.RLock()
	maxAge := tl.maxAcceptableAge
	tl.mu.RUnlock()

	now := time.Now()
	age := now.Sub(entry.Timestamp)

	// If timestamp is too old, clamp it
	if age > maxAge {
		originalTimestamp := entry.Timestamp
		entry.Timestamp = now.Add(-maxAge)

		// Add labels to indicate clamping
		if entry.Labels == nil {
			entry.Labels = make(map[string]string)
		}
		entry.Labels["_timestamp_clamped"] = "true"
		entry.Labels["_original_age_hours"] = fmt.Sprintf("%.1f", age.Hours())
		entry.Labels["_original_timestamp"] = originalTimestamp.Format(time.RFC3339)

		tl.logger.WithFields(logrus.Fields{
			"original_timestamp": originalTimestamp.Format(time.RFC3339),
			"clamped_timestamp":  entry.Timestamp.Format(time.RFC3339),
			"original_age_hours": age.Hours(),
		}).Debug("Timestamp clamped to acceptable range")

		return true
	}

	// Also clamp future timestamps
	if entry.Timestamp.After(now.Add(5 * time.Minute)) {
		originalTimestamp := entry.Timestamp
		entry.Timestamp = now

		if entry.Labels == nil {
			entry.Labels = make(map[string]string)
		}
		entry.Labels["_timestamp_clamped"] = "true"
		entry.Labels["_original_timestamp"] = originalTimestamp.Format(time.RFC3339)
		entry.Labels["_future_seconds"] = strconv.FormatInt(int64(entry.Timestamp.Sub(now).Seconds()), 10)

		tl.logger.WithFields(logrus.Fields{
			"original_timestamp": originalTimestamp.Format(time.RFC3339),
			"clamped_timestamp":  entry.Timestamp.Format(time.RFC3339),
		}).Debug("Future timestamp clamped to current time")

		return true
	}

	return false
}

// GetStats returns learning statistics
func (tl *timestampLearner) GetStats() map[string]interface{} {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	return map[string]interface{}{
		"max_acceptable_age_hours": tl.maxAcceptableAge.Hours(),
		"learned_from_errors":      tl.learnedFromErrors,
		"last_learned":             tl.lastLearned,
		"clamp_enabled":            tl.config.ClampEnabled,
		"learn_enabled":            tl.config.LearnFromErrors,
	}
}
