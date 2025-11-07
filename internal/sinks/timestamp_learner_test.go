package sinks

import (
	"ssw-logs-capture/pkg/types"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestampLearner_ValidateTimestamp_Valid(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Valid timestamp (1 hour old)
	entry := types.LogEntry{
		Timestamp: time.Now().Add(-1 * time.Hour),
		Message:   "test log",
	}

	err := learner.ValidateTimestamp(entry)
	assert.NoError(t, err, "Valid timestamp should not produce error")
}

func TestTimestampLearner_ValidateTimestamp_TooOld(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Timestamp 48 hours old (exceeds 24h threshold)
	entry := types.LogEntry{
		Timestamp: time.Now().Add(-48 * time.Hour),
		Message:   "old log",
	}

	err := learner.ValidateTimestamp(entry)
	require.Error(t, err, "Old timestamp should produce error")
	assert.ErrorIs(t, err, ErrTimestampTooOld)
}

func TestTimestampLearner_ValidateTimestamp_TooNew(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Timestamp 1 hour in future (exceeds 5min tolerance)
	entry := types.LogEntry{
		Timestamp: time.Now().Add(1 * time.Hour),
		Message:   "future log",
	}

	err := learner.ValidateTimestamp(entry)
	require.Error(t, err, "Future timestamp should produce error")
	assert.ErrorIs(t, err, ErrTimestampTooNew)
}

func TestTimestampLearner_ValidateTimestamp_Zero(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Zero timestamp
	entry := types.LogEntry{
		Timestamp: time.Time{},
		Message:   "no timestamp",
	}

	err := learner.ValidateTimestamp(entry)
	require.Error(t, err, "Zero timestamp should produce error")
	assert.ErrorIs(t, err, ErrTimestampZero)
}

func TestTimestampLearner_ClampTimestamp_TooOld(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
		ClampEnabled:  true,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Timestamp 48 hours old
	originalTimestamp := time.Now().Add(-48 * time.Hour)
	entry := types.LogEntry{
		Timestamp: originalTimestamp,
		Message:   "old log",
		Labels:    make(map[string]string),
	}

	clamped := learner.ClampTimestamp(&entry)
	assert.True(t, clamped, "Old timestamp should be clamped")

	// Verify timestamp was clamped
	age := time.Since(entry.Timestamp)
	assert.LessOrEqual(t, age.Hours(), 24.1, "Clamped timestamp should be within 24h")

	// Verify labels were added
	assert.Equal(t, "true", entry.Labels["_timestamp_clamped"])
	assert.NotEmpty(t, entry.Labels["_original_age_hours"])
	assert.NotEmpty(t, entry.Labels["_original_timestamp"])
}

func TestTimestampLearner_ClampTimestamp_Valid(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:       true,
		DefaultMaxAge: 24 * time.Hour,
		ClampEnabled:  true,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Valid timestamp (1 hour old)
	originalTimestamp := time.Now().Add(-1 * time.Hour)
	entry := types.LogEntry{
		Timestamp: originalTimestamp,
		Message:   "recent log",
	}

	clamped := learner.ClampTimestamp(&entry)
	assert.False(t, clamped, "Valid timestamp should not be clamped")
	assert.Equal(t, originalTimestamp, entry.Timestamp, "Timestamp should be unchanged")
}

func TestTimestampLearner_LearnFromRejection(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:           true,
		DefaultMaxAge:     24 * time.Hour,
		LearnFromErrors:   true,
		MinLearningWindow: 1 * time.Millisecond, // Allow fast learning for test
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Initial threshold
	initialThreshold := learner.GetMaxAcceptableAge()
	assert.Equal(t, 24*time.Hour, initialThreshold)

	// Simulate Loki rejection of 12-hour-old entry
	entry := types.LogEntry{
		Timestamp: time.Now().Add(-12 * time.Hour),
		Message:   "rejected log",
	}

	errorMsg := "timestamp too old: entry timestamp is 2025-11-05, stream time is 2025-11-07"
	err := learner.LearnFromRejection(errorMsg, entry)
	require.NoError(t, err)

	// Wait for learning window
	time.Sleep(10 * time.Millisecond)

	// Threshold should be updated (less than 12h)
	newThreshold := learner.GetMaxAcceptableAge()
	assert.Less(t, newThreshold, 12*time.Hour, "Threshold should be learned from rejection")
	assert.Greater(t, newThreshold, 10*time.Hour, "Threshold should have safety buffer")
}

func TestTimestampLearner_Concurrent(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:           true,
		DefaultMaxAge:     24 * time.Hour,
		LearnFromErrors:   true,
		MinLearningWindow: 1 * time.Millisecond,
	}
	learner := NewTimestampLearner(config, logrus.New())

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			entry := types.LogEntry{
				Timestamp: time.Now().Add(-1 * time.Hour),
				Message:   "concurrent log",
			}

			// Validate
			learner.ValidateTimestamp(entry)

			// Get threshold
			learner.GetMaxAcceptableAge()

			// Learn from rejection
			learner.LearnFromRejection("timestamp too old", entry)

			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No panics = success
}

func TestClassifyLokiError_Permanent(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorMsg   string
		expected   LokiErrorType
	}{
		{
			name:       "400 timestamp too old",
			statusCode: 400,
			errorMsg:   "timestamp too old",
			expected:   LokiErrorPermanent,
		},
		{
			name:       "400 out of order",
			statusCode: 400,
			errorMsg:   "out of order",
			expected:   LokiErrorPermanent,
		},
		{
			name:       "400 generic bad request",
			statusCode: 400,
			errorMsg:   "bad request",
			expected:   LokiErrorPermanent,
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			errorMsg:   "unauthorized",
			expected:   LokiErrorPermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyLokiError(tt.statusCode, tt.errorMsg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifyLokiError_RateLimit(t *testing.T) {
	result := classifyLokiError(429, "rate limit exceeded")
	assert.Equal(t, LokiErrorRateLimit, result)
}

func TestClassifyLokiError_Server(t *testing.T) {
	tests := []int{500, 502, 503, 504}

	for _, code := range tests {
		result := classifyLokiError(code, "server error")
		assert.Equal(t, LokiErrorServer, result)
	}
}

func TestClassifyLokiError_Temporary(t *testing.T) {
	// Network error (status code 0)
	result := classifyLokiError(0, "connection refused")
	assert.Equal(t, LokiErrorTemporary, result)
}

func TestErrorTypeToString(t *testing.T) {
	tests := []struct {
		errorType LokiErrorType
		expected  string
	}{
		{LokiErrorPermanent, "permanent"},
		{LokiErrorRateLimit, "rate_limit"},
		{LokiErrorServer, "server"},
		{LokiErrorTemporary, "temporary"},
	}

	for _, tt := range tests {
		result := errorTypeToString(tt.errorType)
		assert.Equal(t, tt.expected, result)
	}
}

func TestTimestampLearner_GetStats(t *testing.T) {
	config := TimestampLearnerConfig{
		Enabled:           true,
		DefaultMaxAge:     24 * time.Hour,
		ClampEnabled:      true,
		LearnFromErrors:   true,
	}
	learner := NewTimestampLearner(config, logrus.New())

	stats := learner.(*timestampLearner).GetStats()

	assert.NotNil(t, stats)
	assert.Equal(t, 24.0, stats["max_acceptable_age_hours"])
	assert.Equal(t, true, stats["clamp_enabled"])
	assert.Equal(t, true, stats["learn_enabled"])
	assert.Equal(t, int64(0), stats["learned_from_errors"])
}
