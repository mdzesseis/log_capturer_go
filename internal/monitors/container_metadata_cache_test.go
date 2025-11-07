package monitors

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMetadataCache tests cache creation
func TestNewMetadataCache(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewMetadataCache(ttl)

	require.NotNil(t, cache)
	assert.Equal(t, ttl, cache.ttl)
	assert.NotNil(t, cache.cache)
	assert.NotNil(t, cache.lastUpdate)
	assert.Equal(t, uint64(0), cache.hits)
	assert.Equal(t, uint64(0), cache.misses)
}

// TestMetadataCache_GetSet tests basic get/set operations
func TestMetadataCache_GetSet(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "test-container",
		Image: "nginx:latest",
		Labels: map[string]string{
			"env": "prod",
		},
	}

	t.Run("get from empty cache", func(t *testing.T) {
		result, found := cache.Get("abc123")
		assert.False(t, found)
		assert.Nil(t, result)
	})

	t.Run("set and get", func(t *testing.T) {
		cache.Set("abc123", metadata)

		result, found := cache.Get("abc123")
		assert.True(t, found)
		require.NotNil(t, result)
		assert.Equal(t, "abc123", result.ID)
		assert.Equal(t, "test-container", result.Name)
		assert.Equal(t, "nginx:latest", result.Image)
		assert.Equal(t, "prod", result.Labels["env"])
	})

	t.Run("deep copy on get", func(t *testing.T) {
		result, found := cache.Get("abc123")
		assert.True(t, found)

		// Modify retrieved metadata
		result.Name = "modified"
		result.Labels["env"] = "dev"

		// Original should be unchanged in cache
		result2, found2 := cache.Get("abc123")
		assert.True(t, found2)
		assert.Equal(t, "test-container", result2.Name)
		assert.Equal(t, "prod", result2.Labels["env"])
	})

	t.Run("deep copy on set", func(t *testing.T) {
		original := &ContainerMetadata{
			ID:   "def456",
			Name: "original",
			Labels: map[string]string{
				"key": "value",
			},
		}

		cache.Set("def456", original)

		// Modify original after setting
		original.Name = "modified"
		original.Labels["key"] = "changed"

		// Cached version should be unchanged
		result, found := cache.Get("def456")
		assert.True(t, found)
		assert.Equal(t, "original", result.Name)
		assert.Equal(t, "value", result.Labels["key"])
	})
}

// TestMetadataCache_TTL tests TTL expiration
func TestMetadataCache_TTL(t *testing.T) {
	shortTTL := 100 * time.Millisecond
	cache := NewMetadataCache(shortTTL)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "test",
		Image: "test:latest",
	}

	t.Run("entry valid before TTL", func(t *testing.T) {
		cache.Set("abc123", metadata)

		time.Sleep(50 * time.Millisecond) // Half of TTL

		result, found := cache.Get("abc123")
		assert.True(t, found, "entry should still be valid")
		require.NotNil(t, result)
	})

	t.Run("entry expires after TTL", func(t *testing.T) {
		cache.Clear()
		cache.Set("abc123", metadata)

		time.Sleep(150 * time.Millisecond) // Exceed TTL

		result, found := cache.Get("abc123")
		assert.False(t, found, "entry should be expired")
		assert.Nil(t, result)
	})
}

// TestMetadataCache_Delete tests deletion
func TestMetadataCache_Delete(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "test",
		Image: "test:latest",
	}

	cache.Set("abc123", metadata)

	// Verify it exists
	_, found := cache.Get("abc123")
	assert.True(t, found)

	// Delete it
	cache.Delete("abc123")

	// Verify it's gone
	_, found = cache.Get("abc123")
	assert.False(t, found)
}

// TestMetadataCache_Stats tests statistics tracking
func TestMetadataCache_Stats(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "test",
		Image: "test:latest",
	}

	// Initial stats
	size, hits, misses := cache.GetStats()
	assert.Equal(t, 0, size)
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(0), misses)

	// Miss
	cache.Get("nonexistent")
	size, hits, misses = cache.GetStats()
	assert.Equal(t, 0, size)
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(1), misses)

	// Set
	cache.Set("abc123", metadata)
	size, hits, misses = cache.GetStats()
	assert.Equal(t, 1, size)

	// Hit
	cache.Get("abc123")
	size, hits, misses = cache.GetStats()
	assert.Equal(t, 1, size)
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(1), misses)

	// Another hit
	cache.Get("abc123")
	size, hits, misses = cache.GetStats()
	assert.Equal(t, uint64(2), hits)
	assert.Equal(t, uint64(1), misses)
}

// TestMetadataCache_Clear tests cache clearing
func TestMetadataCache_Clear(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	// Add some entries
	for i := 0; i < 5; i++ {
		metadata := &ContainerMetadata{
			ID:    string(rune('a' + i)),
			Name:  "test",
			Image: "test:latest",
		}
		cache.Set(string(rune('a'+i)), metadata)
	}

	size, _, _ := cache.GetStats()
	assert.Equal(t, 5, size)

	// Clear
	cache.Clear()

	size, hits, misses := cache.GetStats()
	assert.Equal(t, 0, size)
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(0), misses)
}

// TestMetadataCache_CleanupExpired tests manual cleanup
func TestMetadataCache_CleanupExpired(t *testing.T) {
	shortTTL := 50 * time.Millisecond
	cache := NewMetadataCache(shortTTL)

	// Add fresh entries
	for i := 0; i < 3; i++ {
		metadata := &ContainerMetadata{
			ID:    string(rune('a' + i)),
			Name:  "test",
			Image: "test:latest",
		}
		cache.Set(string(rune('a'+i)), metadata)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Add one fresh entry
	freshMetadata := &ContainerMetadata{
		ID:    "fresh",
		Name:  "fresh",
		Image: "fresh:latest",
	}
	cache.Set("fresh", freshMetadata)

	// Cleanup expired
	removed := cache.CleanupExpired()
	assert.Equal(t, 3, removed, "should remove 3 expired entries")

	size, _, _ := cache.GetStats()
	assert.Equal(t, 1, size, "only fresh entry should remain")

	_, found := cache.Get("fresh")
	assert.True(t, found, "fresh entry should still be accessible")
}

// TestMetadataCache_ConcurrentAccess tests thread-safety
func TestMetadataCache_ConcurrentAccess(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "test",
		Image: "test:latest",
		Labels: map[string]string{
			"key": "value",
		},
	}

	// Pre-populate
	cache.Set("abc123", metadata)

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				result, found := cache.Get("abc123")
				assert.True(t, found)
				assert.NotNil(t, result)
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				meta := &ContainerMetadata{
					ID:    "writer",
					Name:  "concurrent",
					Image: "test:latest",
				}
				cache.Set("writer", meta)
			}
		}(i)
	}

	wg.Wait()

	// Verify cache integrity
	result, found := cache.Get("abc123")
	assert.True(t, found)
	assert.Equal(t, "abc123", result.ID)
}

// TestMetadataCache_DetailedStats tests detailed statistics
func TestMetadataCache_DetailedStats(t *testing.T) {
	cache := NewMetadataCache(1 * time.Minute)

	// Add some entries
	for i := 0; i < 5; i++ {
		metadata := &ContainerMetadata{
			ID:    string(rune('a' + i)),
			Name:  "test",
			Image: "test:latest",
		}
		cache.Set(string(rune('a'+i)), metadata)
		if i < 4 {
			time.Sleep(10 * time.Millisecond) // Stagger timestamps
		}
	}

	// Trigger some hits and misses
	cache.Get("a") // hit
	cache.Get("b") // hit
	cache.Get("nonexistent") // miss

	stats := cache.GetDetailedStats()

	assert.Equal(t, 5, stats.Size)
	assert.Equal(t, uint64(2), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, 1*time.Minute, stats.TTL)

	// Hit rate should be 2 / (2 + 1) = 0.666...
	assert.InDelta(t, 0.666, stats.HitRate, 0.01)

	// Age assertions
	assert.Greater(t, stats.OldestAge, time.Duration(0))
	assert.Greater(t, stats.NewestAge, time.Duration(0))
	assert.GreaterOrEqual(t, stats.OldestAge, stats.NewestAge)
}

// TestCopyMetadata tests metadata deep copy
func TestCopyMetadata(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		result := copyMetadata(nil)
		assert.Nil(t, result)
	})

	t.Run("full metadata", func(t *testing.T) {
		original := &ContainerMetadata{
			ID:       "abc123",
			Name:     "test",
			Image:    "nginx:latest",
			Created:  "2023-01-01T00:00:00Z",
			Started:  "2023-01-01T00:00:05Z",
			State:    "running",
			Status:   "running (true)",
			Platform: "linux",
			Hostname: "web-01",
			Command:  "nginx",
			Labels: map[string]string{
				"env":     "prod",
				"service": "web",
			},
			Networks:    []string{"bridge", "custom"},
			IPAddresses: map[string]string{"bridge": "172.17.0.2"},
		}

		copy := copyMetadata(original)

		// Verify all fields copied
		assert.Equal(t, original.ID, copy.ID)
		assert.Equal(t, original.Name, copy.Name)
		assert.Equal(t, original.Image, copy.Image)
		assert.Equal(t, original.Created, copy.Created)
		assert.Equal(t, original.Started, copy.Started)
		assert.Equal(t, original.State, copy.State)
		assert.Equal(t, original.Status, copy.Status)
		assert.Equal(t, original.Platform, copy.Platform)
		assert.Equal(t, original.Hostname, copy.Hostname)
		assert.Equal(t, original.Command, copy.Command)

		// Verify deep copy of maps
		assert.Equal(t, original.Labels, copy.Labels)
		copy.Labels["env"] = "dev"
		assert.Equal(t, "prod", original.Labels["env"], "original should not be modified")

		// Verify deep copy of slices
		assert.Equal(t, original.Networks, copy.Networks)
		copy.Networks[0] = "modified"
		assert.Equal(t, "bridge", original.Networks[0], "original should not be modified")

		// Verify deep copy of IP addresses
		assert.Equal(t, original.IPAddresses, copy.IPAddresses)
		copy.IPAddresses["bridge"] = "10.0.0.1"
		assert.Equal(t, "172.17.0.2", original.IPAddresses["bridge"], "original should not be modified")
	})

	t.Run("nil fields", func(t *testing.T) {
		original := &ContainerMetadata{
			ID:          "abc123",
			Name:        "test",
			Image:       "test:latest",
			Labels:      nil,
			Networks:    nil,
			IPAddresses: nil,
		}

		copy := copyMetadata(original)

		assert.Equal(t, original.ID, copy.ID)
		assert.Nil(t, copy.Labels)
		assert.Nil(t, copy.Networks)
		assert.Nil(t, copy.IPAddresses)
	})
}

// BenchmarkMetadataCache_Get benchmarks cache reads
func BenchmarkMetadataCache_Get(b *testing.B) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "bench-test",
		Image: "nginx:latest",
		Labels: map[string]string{
			"env":     "prod",
			"service": "web",
			"version": "1.0.0",
		},
	}

	cache.Set("abc123", metadata)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get("abc123")
	}
}

// BenchmarkMetadataCache_Set benchmarks cache writes
func BenchmarkMetadataCache_Set(b *testing.B) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "bench-test",
		Image: "nginx:latest",
		Labels: map[string]string{
			"env":     "prod",
			"service": "web",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("abc123", metadata)
	}
}

// BenchmarkMetadataCache_ConcurrentGet benchmarks concurrent reads
func BenchmarkMetadataCache_ConcurrentGet(b *testing.B) {
	cache := NewMetadataCache(1 * time.Minute)

	metadata := &ContainerMetadata{
		ID:    "abc123",
		Name:  "bench-test",
		Image: "nginx:latest",
		Labels: map[string]string{
			"env": "prod",
		},
	}

	cache.Set("abc123", metadata)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = cache.Get("abc123")
		}
	})
}
