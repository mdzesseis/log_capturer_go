package monitors

import (
	"sync"
	"sync/atomic"
	"time"
)

// MetadataCache provides thread-safe caching of container metadata with TTL support
//
// This cache is critical for the hybrid monitor because:
// - Container metadata is relatively static (doesn't change often)
// - Docker API calls are expensive (network overhead, CPU, rate limits)
// - Log parsing happens at high frequency (thousands of lines/second)
// - Metadata enrichment must be fast to avoid bottlenecks
//
// Cache Strategy:
// - Lazy invalidation (check TTL on read, not proactively)
// - Per-container TTL tracking (fine-grained expiration)
// - Thread-safe for concurrent access from multiple readers
// - Minimal locking (RWMutex for read-heavy workload)
//
// Performance Characteristics:
// - Get: O(1) with RLock (concurrent reads)
// - Set: O(1) with Lock
// - Memory: O(n) where n = number of containers
//
// Usage Pattern:
//
//	cache := NewMetadataCache(5 * time.Minute)
//
//	// First access: cache miss, fetch from Docker
//	metadata, found := cache.Get(containerID)
//	if !found {
//	    metadata = fetchFromDocker(containerID)
//	    cache.Set(containerID, metadata)
//	}
//
//	// Subsequent accesses: cache hit (until TTL expires)
//	metadata, found := cache.Get(containerID)
type MetadataCache struct {
	mu         sync.RWMutex
	cache      map[string]*ContainerMetadata
	lastUpdate map[string]time.Time
	ttl        time.Duration

	// Statistics (atomic counters for thread-safe access)
	hits   uint64
	misses uint64
}

// NewMetadataCache creates a new metadata cache with specified TTL
//
// Parameters:
//   - ttl: Time-to-live for cached metadata. Recommended: 5 minutes
//     - Too short: Excessive Docker API calls, increased latency
//     - Too long: Stale metadata (container labels/state changes)
//     - Sweet spot: 5-15 minutes for typical workloads
//
// Returns:
//   - *MetadataCache: Ready-to-use cache instance
func NewMetadataCache(ttl time.Duration) *MetadataCache {
	return &MetadataCache{
		cache:      make(map[string]*ContainerMetadata),
		lastUpdate: make(map[string]time.Time),
		ttl:        ttl,
		hits:       0,
		misses:     0,
	}
}

// Get retrieves cached metadata for a container
//
// This method:
// - Checks if metadata exists in cache
// - Validates TTL (returns not-found if expired)
// - Returns deep copy to prevent external modification
// - Updates hit/miss statistics
//
// Thread-safety: Uses RLock for concurrent reads
//
// Parameters:
//   - containerID: Full or short container ID
//
// Returns:
//   - *ContainerMetadata: Cached metadata (deep copy)
//   - bool: true if found and not expired, false otherwise
func (mc *MetadataCache) Get(containerID string) (*ContainerMetadata, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	metadata, exists := mc.cache[containerID]
	if !exists {
		atomic.AddUint64(&mc.misses, 1)
		return nil, false
	}

	// Check TTL (lazy expiration)
	lastUpdate, hasTimestamp := mc.lastUpdate[containerID]
	if !hasTimestamp || time.Since(lastUpdate) > mc.ttl {
		// Expired - treat as miss
		// Note: We don't delete here to avoid Lock promotion
		// Cleanup happens on Set() or explicit Delete()
		atomic.AddUint64(&mc.misses, 1)
		return nil, false
	}

	// Cache hit - return deep copy for thread-safety
	atomic.AddUint64(&mc.hits, 1)
	return copyMetadata(metadata), true
}

// Set stores metadata in the cache with current timestamp
//
// This method:
// - Stores a deep copy of metadata (prevents external modification)
// - Records current timestamp for TTL tracking
// - Updates cache statistics
// - Performs lazy cleanup of expired entries (if detected)
//
// Thread-safety: Uses Lock for exclusive write access
//
// Parameters:
//   - containerID: Full or short container ID
//   - metadata: Container metadata to cache
func (mc *MetadataCache) Set(containerID string, metadata *ContainerMetadata) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Store deep copy to prevent external modification
	mc.cache[containerID] = copyMetadata(metadata)
	mc.lastUpdate[containerID] = time.Now()

	// Lazy cleanup: If cache is large, opportunistically remove one expired entry
	// This prevents unbounded growth without expensive full scans
	if len(mc.cache) > 100 {
		mc.lazyCleanupOneLocked()
	}
}

// Delete removes metadata from cache
//
// This method:
// - Removes entry from both cache and timestamp maps
// - Safe to call even if entry doesn't exist (idempotent)
// - Useful when container is stopped/removed
//
// Thread-safety: Uses Lock for exclusive write access
//
// Parameters:
//   - containerID: Full or short container ID
func (mc *MetadataCache) Delete(containerID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	delete(mc.cache, containerID)
	delete(mc.lastUpdate, containerID)
}

// GetStats returns cache statistics
//
// Useful for monitoring cache effectiveness:
// - Hit rate = hits / (hits + misses)
// - High hit rate (>90%) indicates good caching
// - Low hit rate (<50%) may indicate TTL too short or high churn
//
// Returns:
//   - size: Current number of entries in cache
//   - hits: Total cache hits since creation
//   - misses: Total cache misses since creation
func (mc *MetadataCache) GetStats() (size int, hits, misses uint64) {
	mc.mu.RLock()
	size = len(mc.cache)
	mc.mu.RUnlock()

	hits = atomic.LoadUint64(&mc.hits)
	misses = atomic.LoadUint64(&mc.misses)

	return size, hits, misses
}

// Clear removes all entries from cache
//
// Useful for:
// - Testing (reset state between tests)
// - Manual cache invalidation
// - Memory pressure situations
//
// Thread-safety: Uses Lock for exclusive write access
func (mc *MetadataCache) Clear() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.cache = make(map[string]*ContainerMetadata)
	mc.lastUpdate = make(map[string]time.Time)
	atomic.StoreUint64(&mc.hits, 0)
	atomic.StoreUint64(&mc.misses, 0)
}

// CleanupExpired removes all expired entries from cache
//
// This is a manual cleanup operation. Normally not needed because:
// - Get() performs lazy expiration checks
// - Set() performs opportunistic cleanup
//
// However, useful for:
// - Periodic cleanup goroutines
// - Reducing memory footprint
// - Debugging/testing
//
// Returns:
//   - int: Number of entries removed
func (mc *MetadataCache) CleanupExpired() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	removed := 0

	// Collect expired container IDs
	expiredIDs := make([]string, 0)
	for containerID, lastUpdate := range mc.lastUpdate {
		if now.Sub(lastUpdate) > mc.ttl {
			expiredIDs = append(expiredIDs, containerID)
		}
	}

	// Remove expired entries
	for _, containerID := range expiredIDs {
		delete(mc.cache, containerID)
		delete(mc.lastUpdate, containerID)
		removed++
	}

	return removed
}

// lazyCleanupOneLocked removes one expired entry (internal helper)
//
// Called by Set() when cache grows large. Removes at most one entry
// to avoid long lock hold times.
//
// IMPORTANT: Caller must hold mc.mu Lock
func (mc *MetadataCache) lazyCleanupOneLocked() {
	now := time.Now()

	// Find one expired entry
	for containerID, lastUpdate := range mc.lastUpdate {
		if now.Sub(lastUpdate) > mc.ttl {
			// Found expired entry - remove it
			delete(mc.cache, containerID)
			delete(mc.lastUpdate, containerID)
			return // Remove only one entry
		}
	}
}

// copyMetadata creates a deep copy of ContainerMetadata
//
// This is critical for thread-safety:
// - Prevents external code from modifying cached data
// - Allows cache to safely store shared metadata
// - Avoids race conditions between readers and writers
//
// Performance: O(n) where n = number of labels + networks
//
// Parameters:
//   - metadata: Original metadata (may be nil)
//
// Returns:
//   - *ContainerMetadata: Deep copy
func copyMetadata(metadata *ContainerMetadata) *ContainerMetadata {
	if metadata == nil {
		return nil
	}

	// Copy struct fields
	result := &ContainerMetadata{
		ID:       metadata.ID,
		Name:     metadata.Name,
		Image:    metadata.Image,
		Created:  metadata.Created,
		Started:  metadata.Started,
		State:    metadata.State,
		Status:   metadata.Status,
		Platform: metadata.Platform,
		Hostname: metadata.Hostname,
		Command:  metadata.Command,
	}

	// Deep copy labels map
	if metadata.Labels != nil {
		result.Labels = make(map[string]string, len(metadata.Labels))
		for k, v := range metadata.Labels {
			result.Labels[k] = v
		}
	}

	// Deep copy networks slice
	if metadata.Networks != nil {
		result.Networks = make([]string, len(metadata.Networks))
		copy(result.Networks, metadata.Networks)
	}

	// Deep copy IP addresses map
	if metadata.IPAddresses != nil {
		result.IPAddresses = make(map[string]string, len(metadata.IPAddresses))
		for k, v := range metadata.IPAddresses {
			result.IPAddresses[k] = v
		}
	}

	return result
}

// ContainerMetadataCacheStats provides detailed cache statistics
type ContainerMetadataCacheStats struct {
	Size       int
	Hits       uint64
	Misses     uint64
	HitRate    float64
	TTL        time.Duration
	OldestAge  time.Duration
	NewestAge  time.Duration
}

// GetDetailedStats returns comprehensive cache statistics
//
// Useful for monitoring dashboards and performance analysis.
//
// Returns:
//   - ContainerMetadataCacheStats: Detailed statistics
func (mc *MetadataCache) GetDetailedStats() ContainerMetadataCacheStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	stats := ContainerMetadataCacheStats{
		Size:    len(mc.cache),
		Hits:    atomic.LoadUint64(&mc.hits),
		Misses:  atomic.LoadUint64(&mc.misses),
		TTL:     mc.ttl,
	}

	// Calculate hit rate
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	// Find oldest and newest entries
	now := time.Now()
	var oldestAge, newestAge time.Duration
	first := true

	for _, lastUpdate := range mc.lastUpdate {
		age := now.Sub(lastUpdate)
		if first {
			oldestAge = age
			newestAge = age
			first = false
		} else {
			if age > oldestAge {
				oldestAge = age
			}
			if age < newestAge {
				newestAge = age
			}
		}
	}

	stats.OldestAge = oldestAge
	stats.NewestAge = newestAge

	return stats
}
