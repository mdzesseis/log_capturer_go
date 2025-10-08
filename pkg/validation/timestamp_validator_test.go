package validation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimestampValidator_NewTimestampValidator(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339, "2006-01-02 15:04:05"},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	assert.NotNil(t, validator)
	assert.Equal(t, config, validator.config)
	assert.Equal(t, logger, validator.logger)
}

func TestTimestampValidator_ValidateEntry_ValidTimestamp(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with valid timestamp (current time)
	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   "test message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid)
	assert.Equal(t, entry.Timestamp, result.OriginalTimestamp)
	assert.Equal(t, entry.Timestamp, result.ProcessedTimestamp)
	assert.Equal(t, "valid", result.ValidationResult)
}

func TestTimestampValidator_ValidateEntry_PastTimestamp_Clamp(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        1, // Only 1 hour in the past
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with timestamp too far in the past
	pastTime := time.Now().Add(-5 * time.Hour)
	entry := LogEntry{
		Timestamp: pastTime,
		Message:   "old message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid) // Should be valid after clamping
	assert.Equal(t, pastTime, result.OriginalTimestamp)
	assert.NotEqual(t, pastTime, result.ProcessedTimestamp) // Should be clamped
	assert.Equal(t, "clamped_past", result.ValidationResult)
	assert.WithinDuration(t, time.Now(), result.ProcessedTimestamp, 1*time.Minute)
}

func TestTimestampValidator_ValidateEntry_FutureTimestamp_Clamp(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        1,
		MaxFutureHours:      1, // Only 1 hour in the future
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with timestamp too far in the future
	futureTime := time.Now().Add(5 * time.Hour)
	entry := LogEntry{
		Timestamp: futureTime,
		Message:   "future message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid) // Should be valid after clamping
	assert.Equal(t, futureTime, result.OriginalTimestamp)
	assert.NotEqual(t, futureTime, result.ProcessedTimestamp) // Should be clamped
	assert.Equal(t, "clamped_future", result.ValidationResult)
	assert.WithinDuration(t, time.Now(), result.ProcessedTimestamp, 1*time.Minute)
}

func TestTimestampValidator_ValidateEntry_Reject(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        1,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "reject", // Reject invalid timestamps
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  false,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with timestamp too far in the past
	pastTime := time.Now().Add(-5 * time.Hour)
	entry := LogEntry{
		Timestamp: pastTime,
		Message:   "old message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.False(t, result.IsValid) // Should be invalid
	assert.Equal(t, pastTime, result.OriginalTimestamp)
	assert.Equal(t, pastTime, result.ProcessedTimestamp) // No change when rejecting
	assert.Equal(t, "rejected_past", result.ValidationResult)
}

func TestTimestampValidator_ValidateEntry_Warn(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        1,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "warn", // Just warn but keep timestamp
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  false,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with timestamp too far in the past
	pastTime := time.Now().Add(-5 * time.Hour)
	entry := LogEntry{
		Timestamp: pastTime,
		Message:   "old message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid) // Should be valid (just warned)
	assert.Equal(t, pastTime, result.OriginalTimestamp)
	assert.Equal(t, pastTime, result.ProcessedTimestamp) // No change when warning
	assert.Equal(t, "warned_past", result.ValidationResult)
}

func TestTimestampValidator_ParseTimestamp_RFC3339(t *testing.T) {
	config := Config{
		Enabled:             true,
		SupportedFormats:    []string{time.RFC3339},
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test RFC3339 format
	timeStr := "2023-12-25T15:30:45Z"
	expectedTime, _ := time.Parse(time.RFC3339, timeStr)

	parsedTime, err := validator.parseTimestamp(timeStr)
	require.NoError(t, err)
	assert.Equal(t, expectedTime, parsedTime)
}

func TestTimestampValidator_ParseTimestamp_MultipleFormats(t *testing.T) {
	config := Config{
		Enabled:             true,
		SupportedFormats:    []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006/01/02 15:04:05",
		},
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	testCases := []struct {
		timeStr  string
		format   string
	}{
		{"2023-12-25T15:30:45Z", time.RFC3339},
		{"2023-12-25 15:30:45", "2006-01-02 15:04:05"},
		{"2023/12/25 15:30:45", "2006/01/02 15:04:05"},
	}

	for _, tc := range testCases {
		expectedTime, _ := time.Parse(tc.format, tc.timeStr)

		parsedTime, err := validator.parseTimestamp(tc.timeStr)
		require.NoError(t, err, "Should parse %s", tc.timeStr)
		assert.Equal(t, expectedTime, parsedTime, "Time should match for %s", tc.timeStr)
	}
}

func TestTimestampValidator_ParseTimestamp_InvalidFormat(t *testing.T) {
	config := Config{
		Enabled:             true,
		SupportedFormats:    []string{time.RFC3339},
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with invalid format
	_, err := validator.parseTimestamp("invalid-timestamp")
	assert.Error(t, err, "Should return error for invalid timestamp")
}

func TestTimestampValidator_ValidateEntry_StringTimestamp(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with string timestamp that needs parsing
	currentTime := time.Now()
	timeStr := currentTime.Format(time.RFC3339)

	entry := LogEntry{
		Message:   "test message",
		SourceID:  "test-source",
		Labels:    map[string]string{"timestamp": timeStr},
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid)
	assert.WithinDuration(t, currentTime, result.ProcessedTimestamp, time.Second)
}

func TestTimestampValidator_GetStatistics(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        1,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Process various types of entries
	entries := []LogEntry{
		{Timestamp: time.Now(), Message: "valid", SourceID: "test"},                           // valid
		{Timestamp: time.Now().Add(-5 * time.Hour), Message: "past", SourceID: "test"},       // past (clamped)
		{Timestamp: time.Now().Add(5 * time.Hour), Message: "future", SourceID: "test"},      // future (clamped)
		{Timestamp: time.Now().Add(-30 * time.Minute), Message: "valid2", SourceID: "test"},  // valid
	}

	for _, entry := range entries {
		validator.ValidateEntry(entry)
	}

	stats := validator.GetStatistics()

	assert.Equal(t, int64(4), stats.TotalProcessed, "Should process 4 entries")
	assert.Equal(t, int64(2), stats.ValidEntries, "Should have 2 valid entries")
	assert.Equal(t, int64(1), stats.ClampedPast, "Should clamp 1 past entry")
	assert.Equal(t, int64(1), stats.ClampedFuture, "Should clamp 1 future entry")
	assert.Equal(t, int64(0), stats.RejectedEntries, "Should reject 0 entries (clamping enabled)")
}

func TestTimestampValidator_Timezone_Handling(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "America/New_York",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test with different timezone
	entry := LogEntry{
		Timestamp: time.Now(),
		Message:   "timezone test",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid)
}

func TestTimestampValidator_DisabledConfig(t *testing.T) {
	config := Config{
		Enabled: false, // Disabled
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// When disabled, should pass through without validation
	pastTime := time.Now().Add(-24 * time.Hour)
	entry := LogEntry{
		Timestamp: pastTime,
		Message:   "disabled test",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid, "Should be valid when disabled")
	assert.Equal(t, pastTime, result.ProcessedTimestamp, "Timestamp should not change when disabled")
	assert.Equal(t, "disabled", result.ValidationResult)

	stats := validator.GetStatistics()
	assert.Equal(t, int64(0), stats.TotalProcessed, "No stats when disabled")
}

func TestTimestampValidator_EdgeCases(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	testCases := []struct {
		name      string
		entry     LogEntry
		expectErr bool
	}{
		{
			name: "Zero timestamp",
			entry: LogEntry{
				Timestamp: time.Time{},
				Message:   "zero time",
				SourceID:  "test",
			},
			expectErr: false, // Should be clamped to current time
		},
		{
			name: "Empty message",
			entry: LogEntry{
				Timestamp: time.Now(),
				Message:   "",
				SourceID:  "test",
			},
			expectErr: false,
		},
		{
			name: "Empty source ID",
			entry: LogEntry{
				Timestamp: time.Now(),
				Message:   "test",
				SourceID:  "",
			},
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validator.ValidateEntry(tc.entry)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestTimestampValidator_ConcurrentAccess(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        24,
		MaxFutureHours:      1,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  true,
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	// Test concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 10; j++ {
				entry := LogEntry{
					Timestamp: time.Now().Add(time.Duration(j) * time.Minute),
					Message:   fmt.Sprintf("concurrent test %d-%d", id, j),
					SourceID:  fmt.Sprintf("source-%d", id),
				}

				result, err := validator.ValidateEntry(entry)
				assert.NoError(t, err)
				assert.True(t, result.IsValid)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	stats := validator.GetStatistics()
	assert.Equal(t, int64(100), stats.TotalProcessed, "Should process all 100 entries")
}

func TestTimestampValidator_ClampToRange(t *testing.T) {
	config := Config{
		Enabled:             true,
		MaxPastHours:        2,
		MaxFutureHours:      2,
		Timezone:            "UTC",
		OnInvalidAction:     "clamp",
		SupportedFormats:    []string{time.RFC3339},
		ClampToCurrentTime:  false, // Clamp to range boundaries, not current time
	}

	logger := logrus.New()
	ctx := context.Background()

	validator := NewTimestampValidator(config, logger, ctx)

	now := time.Now()

	// Test clamping to past boundary
	pastTime := now.Add(-5 * time.Hour)
	entry := LogEntry{
		Timestamp: pastTime,
		Message:   "past message",
		SourceID:  "test-source",
	}

	result, err := validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid)

	expectedPastBoundary := now.Add(-2 * time.Hour)
	assert.WithinDuration(t, expectedPastBoundary, result.ProcessedTimestamp, 5*time.Minute)

	// Test clamping to future boundary
	futureTime := now.Add(5 * time.Hour)
	entry = LogEntry{
		Timestamp: futureTime,
		Message:   "future message",
		SourceID:  "test-source",
	}

	result, err = validator.ValidateEntry(entry)
	require.NoError(t, err)
	assert.True(t, result.IsValid)

	expectedFutureBoundary := now.Add(2 * time.Hour)
	assert.WithinDuration(t, expectedFutureBoundary, result.ProcessedTimestamp, 5*time.Minute)
}