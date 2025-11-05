package deduplication

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDeduplicationManager_NewDeduplicationManager(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: true,
		IncludeSourceID:  true,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.Equal(t, logger, manager.logger)
	assert.NotNil(t, manager.cache)
}

func TestDeduplicationManager_IsDuplicate_NewEntry(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Test with new entry
	isDup := manager.IsDuplicate("test log message", "source1", time.Now())

	assert.False(t, isDup, "First occurrence should not be duplicate")
	assert.Equal(t, int64(1), manager.stats.TotalChecks, "Stats should track 1 entry")
	assert.Equal(t, int64(0), manager.stats.Duplicates, "No duplicates yet")
}

func TestDeduplicationManager_IsDuplicate_DuplicateEntry(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	message := "duplicate test message"
	source := "source1"
	timestamp := time.Now()

	// First occurrence
	isDup1 := manager.IsDuplicate(message, source, timestamp)
	assert.False(t, isDup1, "First occurrence should not be duplicate")

	// Second occurrence (duplicate)
	isDup2 := manager.IsDuplicate(message, source, timestamp)
	assert.True(t, isDup2, "Second occurrence should be duplicate")

	assert.Equal(t, int64(2), manager.stats.TotalChecks, "Stats should track 2 entries")
	assert.Equal(t, int64(1), manager.stats.Duplicates, "Should find 1 duplicate")
}

func TestDeduplicationManager_HashAlgorithms(t *testing.T) {
	algorithms := []string{"sha256", "md5", "sha1"}

	for _, algo := range algorithms {
		t.Run(algo, func(t *testing.T) {
			config := Config{
				MaxCacheSize: 1000,
				TTL:          5 * time.Minute,
				HashAlgorithm: algo,
				IncludeTimestamp: false,
				IncludeSourceID:  false,
			}

			logger := logrus.New()

			manager := NewDeduplicationManager(config, logger)

			message := "test message for " + algo

			// Should not be duplicate on first call
			isDup1 := manager.IsDuplicate(message, "source", time.Now())
			assert.False(t, isDup1, "First occurrence should not be duplicate for %s", algo)

			// Should be duplicate on second call
			isDup2 := manager.IsDuplicate(message, "source", time.Now())
			assert.True(t, isDup2, "Second occurrence should be duplicate for %s", algo)
		})
	}
}

func TestDeduplicationManager_IncludeTimestamp(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: true, // Include timestamp in hash
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	message := "same message"
	source := "source1"
	time1 := time.Now()
	time2 := time1.Add(1 * time.Second)

	// Same message but different timestamps
	isDup1 := manager.IsDuplicate(message, source, time1)
	assert.False(t, isDup1, "First occurrence should not be duplicate")

	isDup2 := manager.IsDuplicate(message, source, time2)
	assert.False(t, isDup2, "Different timestamp should create different hash")

}

func TestDeduplicationManager_IncludeSourceID(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  true, // Include source ID in hash
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	message := "same message"
	timestamp := time.Now()

	// Same message but different sources
	isDup1 := manager.IsDuplicate(message, "source1", timestamp)
	assert.False(t, isDup1, "First occurrence should not be duplicate")

	isDup2 := manager.IsDuplicate(message, "source2", timestamp)
	assert.False(t, isDup2, "Different source should create different hash")

}

func TestDeduplicationManager_TTLExpiration(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          100 * time.Millisecond, // Short TTL for testing
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	message := "expiring message"
	source := "source1"
	timestamp := time.Now()

	// First occurrence
	isDup1 := manager.IsDuplicate(message, source, timestamp)
	assert.False(t, isDup1, "First occurrence should not be duplicate")

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Run cleanup to remove expired entries
	manager.performCleanup()

	// Same message after expiration should not be duplicate
	isDup2 := manager.IsDuplicate(message, source, timestamp)
	assert.False(t, isDup2, "Message should not be duplicate after TTL expiration")
}

func TestDeduplicationManager_CacheEviction(t *testing.T) {
	config := Config{
		MaxCacheSize: 3, // Small cache for testing eviction
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Fill cache to capacity
	for i := 0; i < 3; i++ {
		message := fmt.Sprintf("message %d", i)
		isDup := manager.IsDuplicate(message, "source", time.Now())
		assert.False(t, isDup, "Message %d should not be duplicate", i)
	}


	// Add one more to trigger eviction
	isDup := manager.IsDuplicate("message 4", "source", time.Now())
	assert.False(t, isDup, "New message should not be duplicate")

	assert.Equal(t, int64(1), manager.stats.EvictedEntries, "Should have 1 eviction")

	// The first message should have been evicted (LRU)
	isDup = manager.IsDuplicate("message 0", "source", time.Now())
	assert.False(t, isDup, "Evicted message should not be duplicate when re-added")
}

func TestDeduplicationManager_LRUOrdering(t *testing.T) {
	config := Config{
		MaxCacheSize: 3,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Add entries
	manager.IsDuplicate("message 1", "source", time.Now())
	manager.IsDuplicate("message 2", "source", time.Now())
	manager.IsDuplicate("message 3", "source", time.Now())

	// Access message 1 to make it most recently used
	isDup := manager.IsDuplicate("message 1", "source", time.Now())
	assert.True(t, isDup, "Message 1 should be duplicate")

	// Add new message, should evict message 2 (least recently used)
	manager.IsDuplicate("message 4", "source", time.Now())

	// Message 1 should still be in cache (was recently accessed)
	isDup = manager.IsDuplicate("message 1", "source", time.Now())
	assert.True(t, isDup, "Message 1 should still be in cache")

	// Message 2 should have been evicted
	isDup = manager.IsDuplicate("message 2", "source", time.Now())
	assert.False(t, isDup, "Message 2 should have been evicted")
}

func TestDeduplicationManager_GetStatistics(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Generate some test data
	manager.IsDuplicate("message 1", "source", time.Now()) // Not duplicate
	manager.IsDuplicate("message 2", "source", time.Now()) // Not duplicate
	manager.IsDuplicate("message 1", "source", time.Now()) // Duplicate
	manager.IsDuplicate("message 3", "source", time.Now()) // Not duplicate

	stats := manager.GetStats()

	assert.Equal(t, int64(4), stats.TotalChecks, "Should track 4 total entries")
	assert.Equal(t, int64(1), stats.Duplicates, "Should find 1 duplicate")
	assert.Equal(t, 3, stats.CacheSize, "Cache should contain 3 unique entries")
}

func TestDeduplicationManager_CleanupExpired(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          50 * time.Millisecond, // Very short TTL
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Add several entries
	for i := 0; i < 5; i++ {
		message := fmt.Sprintf("message %d", i)
		manager.IsDuplicate(message, "source", time.Now())
	}


	// Wait for TTL expiration
	time.Sleep(100 * time.Millisecond)

	// Cleanup expired entries
	manager.performCleanup()

	// Verify cache size reduced after cleanup
	stats := manager.GetStats()
	assert.Less(t, stats.CacheSize, 5, "Cache should have fewer entries after cleanup")
}

func TestDeduplicationManager_StartStop(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          1 * time.Second,
		CleanupInterval: 100 * time.Millisecond, // Fast cleanup for testing
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Start background cleanup
	go manager.Start()

	// Add some entries that will expire
	for i := 0; i < 3; i++ {
		message := fmt.Sprintf("expiring message %d", i)
		manager.IsDuplicate(message, "source", time.Now())
	}

	// Wait for cleanup to run
	time.Sleep(300 * time.Millisecond)

	// Stop the manager
	manager.Stop()

	// Test passes if no panic occurs
	assert.True(t, true)
}

func TestDeduplicationManager_DisabledConfig(t *testing.T) {
	config := Config{
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// When disabled, should always return false (no duplicates)
	isDup1 := manager.IsDuplicate("message", "source", time.Now())
	assert.False(t, isDup1, "Should return false when disabled")

	isDup2 := manager.IsDuplicate("message", "source", time.Now())
	assert.False(t, isDup2, "Should return false when disabled (even for same message)")

	stats := manager.GetStats()
	assert.Equal(t, int64(0), stats.TotalChecks, "No stats when disabled")
}

func TestDeduplicationManager_EmptyMessage(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "sha256",
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Test with empty message
	isDup1 := manager.IsDuplicate("", "source", time.Now())
	assert.False(t, isDup1, "Empty message should not be duplicate on first occurrence")

	isDup2 := manager.IsDuplicate("", "source", time.Now())
	assert.True(t, isDup2, "Empty message should be duplicate on second occurrence")
}

func TestDeduplicationManager_InvalidHashAlgorithm(t *testing.T) {
	config := Config{
		MaxCacheSize: 1000,
		TTL:          5 * time.Minute,
		HashAlgorithm: "invalid", // Invalid algorithm
		IncludeTimestamp: false,
		IncludeSourceID:  false,
	}

	logger := logrus.New()

	manager := NewDeduplicationManager(config, logger)

	// Should handle invalid algorithm gracefully (probably fallback to default)
	isDup := manager.IsDuplicate("test message", "source", time.Now())
	assert.False(t, isDup, "Should handle invalid hash algorithm gracefully")
}