package deduplication

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/sirupsen/logrus"
	"ssw-logs-capture/internal/metrics"
)

// DeduplicationManager gerencia cache de deduplicação com LRU e TTL
type DeduplicationManager struct {
	config Config
	logger *logrus.Logger

	cache     map[string]*CacheEntry
	lruHead   *CacheEntry
	lruTail   *CacheEntry
	mutex     sync.RWMutex

	stats Stats

	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração do gerenciador de deduplicação
type Config struct {
	// Tamanho máximo do cache
	MaxCacheSize int `yaml:"max_cache_size"`

	// TTL para entradas do cache
	TTL time.Duration `yaml:"ttl"`

	// Intervalo de limpeza automática
	CleanupInterval time.Duration `yaml:"cleanup_interval"`

	// Threshold para limpeza baseada em uso
	CleanupThreshold float64 `yaml:"cleanup_threshold"`

	// Algoritmo de hash (md5, sha1, sha256)
	HashAlgorithm string `yaml:"hash_algorithm"`

	// Incluir timestamp no hash
	IncludeTimestamp bool `yaml:"include_timestamp"`

	// Incluir source_id no hash
	IncludeSourceID bool `yaml:"include_source_id"`
}

// CacheEntry entrada do cache LRU com TTL
type CacheEntry struct {
	Key       string
	Hash      string
	CreatedAt time.Time
	LastSeen  time.Time
	HitCount  int64

	// Ponteiros para lista duplamente ligada (LRU)
	prev *CacheEntry
	next *CacheEntry
}

// Stats estatísticas do cache
type Stats struct {
	TotalChecks    int64
	CacheHits      int64
	CacheMisses    int64
	Duplicates     int64
	CacheSize      int
	EvictedEntries int64
	CleanupRuns    int64
}

// NewDeduplicationManager cria novo gerenciador de deduplicação
func NewDeduplicationManager(config Config, logger *logrus.Logger) *DeduplicationManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.MaxCacheSize == 0 {
		config.MaxCacheSize = 100000
	}
	if config.TTL == 0 {
		config.TTL = time.Hour
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 10 * time.Minute
	}
	if config.CleanupThreshold == 0 {
		config.CleanupThreshold = 0.8
	}
	if config.HashAlgorithm == "" {
		config.HashAlgorithm = "xxhash"
	}

	dm := &DeduplicationManager{
		config: config,
		logger: logger,
		cache:  make(map[string]*CacheEntry),
		ctx:    ctx,
		cancel: cancel,
	}

	// Inicializar lista LRU
	dm.lruHead = &CacheEntry{}
	dm.lruTail = &CacheEntry{}
	dm.lruHead.next = dm.lruTail
	dm.lruTail.prev = dm.lruHead

	return dm
}

// Start inicia o gerenciador de deduplicação
func (dm *DeduplicationManager) Start() error {
	dm.logger.WithFields(logrus.Fields{
		"max_cache_size":     dm.config.MaxCacheSize,
		"ttl":               dm.config.TTL,
		"cleanup_interval":   dm.config.CleanupInterval,
		"hash_algorithm":     dm.config.HashAlgorithm,
		"include_timestamp":  dm.config.IncludeTimestamp,
		"include_source_id":  dm.config.IncludeSourceID,
	}).Info("Starting deduplication manager")

	// Iniciar loop de limpeza
	go dm.cleanupLoop()

	return nil
}

// Stop para o gerenciador
func (dm *DeduplicationManager) Stop() error {
	dm.logger.Info("Stopping deduplication manager")
	dm.cancel()
	return nil
}

// IsDuplicate verifica se uma mensagem é duplicada
func (dm *DeduplicationManager) IsDuplicate(sourceID, message string, timestamp time.Time) bool {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	dm.stats.TotalChecks++

	// Gerar hash da mensagem
	hash := dm.generateHash(sourceID, message, timestamp)
	key := fmt.Sprintf("%s_%s", sourceID, hash)

	// Verificar se existe no cache
	entry, exists := dm.cache[key]
	if exists {
		dm.stats.CacheHits++

		// Verificar TTL
		if time.Since(entry.CreatedAt) > dm.config.TTL {
			// Entrada expirada, remover
			dm.removeEntry(entry)
			dm.stats.CacheMisses++

			// Adicionar nova entrada
			dm.addEntry(key, hash)
			return false
		}

		// Atualizar estatísticas da entrada
		entry.LastSeen = time.Now()
		entry.HitCount++

		// Mover para frente da lista LRU
		dm.moveToFront(entry)

		dm.stats.Duplicates++
		dm.logger.WithFields(logrus.Fields{
			"source_id": sourceID,
			"hash":      hash[:8],
			"hit_count": entry.HitCount,
		}).Debug("Duplicate message detected")

		return true
	}

	dm.stats.CacheMisses++

	// Verificar se precisa fazer cleanup por tamanho
	if len(dm.cache) >= dm.config.MaxCacheSize {
		dm.evictLeastRecentlyUsed()
	}

	// Adicionar nova entrada
	dm.addEntry(key, hash)

	return false
}

// generateHash gera hash para a mensagem
func (dm *DeduplicationManager) generateHash(sourceID, message string, timestamp time.Time) string {
	var input string

	// Construir string para hash baseado na configuração
	input = message

	if dm.config.IncludeSourceID {
		input = sourceID + "_" + input
	}

	if dm.config.IncludeTimestamp {
		// Usar timestamp truncado para segundo (fix: was minute, causing test failure)
		truncated := timestamp.Truncate(time.Second)
		input = input + "_" + truncated.Format(time.RFC3339)
	}

	// Gerar hash
	switch dm.config.HashAlgorithm {
	case "xxhash":
		// xxHash: 20x faster than SHA256, perfect for deduplication
		h := xxhash.New()
		h.Write([]byte(input))
		return strconv.FormatUint(h.Sum64(), 16)
	case "sha256":
		hash := sha256.Sum256([]byte(input))
		return fmt.Sprintf("%x", hash)
	default:
		// Fallback para xxhash (novo padrão)
		h := xxhash.New()
		h.Write([]byte(input))
		return strconv.FormatUint(h.Sum64(), 16)
	}
}

// addEntry adiciona nova entrada ao cache
func (dm *DeduplicationManager) addEntry(key, hash string) {
	entry := &CacheEntry{
		Key:       key,
		Hash:      hash,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		HitCount:  1,
	}

	dm.cache[key] = entry
	dm.addToFront(entry)
}

// removeEntry remove entrada do cache
func (dm *DeduplicationManager) removeEntry(entry *CacheEntry) {
	delete(dm.cache, entry.Key)
	dm.removeFromList(entry)
	dm.stats.EvictedEntries++
	metrics.DeduplicationCacheEvictions.Inc()
}

// addToFront adiciona entrada na frente da lista LRU
func (dm *DeduplicationManager) addToFront(entry *CacheEntry) {
	entry.prev = dm.lruHead
	entry.next = dm.lruHead.next
	dm.lruHead.next.prev = entry
	dm.lruHead.next = entry
}

// removeFromList remove entrada da lista LRU
func (dm *DeduplicationManager) removeFromList(entry *CacheEntry) {
	entry.prev.next = entry.next
	entry.next.prev = entry.prev
}

// moveToFront move entrada para frente da lista LRU
func (dm *DeduplicationManager) moveToFront(entry *CacheEntry) {
	dm.removeFromList(entry)
	dm.addToFront(entry)
}

// evictLeastRecentlyUsed remove a entrada menos recentemente usada
func (dm *DeduplicationManager) evictLeastRecentlyUsed() {
	if dm.lruTail.prev != dm.lruHead {
		dm.removeEntry(dm.lruTail.prev)
	}
}

// cleanupLoop loop de limpeza automática
func (dm *DeduplicationManager) cleanupLoop() {
	ticker := time.NewTicker(dm.config.CleanupInterval)
	defer ticker.Stop()

	// Metrics update ticker (every 10 seconds)
	metricsTicker := time.NewTicker(10 * time.Second)
	defer metricsTicker.Stop()

	for {
		select {
		case <-dm.ctx.Done():
			return
		case <-ticker.C:
			dm.performCleanup()
		case <-metricsTicker.C:
			dm.updateMetrics()
		}
	}
}

// performCleanup executa limpeza baseada em TTL e threshold
func (dm *DeduplicationManager) performCleanup() {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	dm.stats.CleanupRuns++
	now := time.Now()
	expiredCount := 0
	thresholdEvicted := 0

	// Limpar entradas expiradas - coletamos chaves primeiro para evitar concurrent map iteration/write
	expiredKeys := make([]string, 0)
	for key, entry := range dm.cache {
		if now.Sub(entry.CreatedAt) > dm.config.TTL {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Agora removemos as entradas expiradas
	for _, key := range expiredKeys {
		if entry, exists := dm.cache[key]; exists {
			delete(dm.cache, key)
			dm.removeFromList(entry)
			expiredCount++
			dm.stats.EvictedEntries++
		}
	}

	// Limpar por threshold se ainda estiver muito cheio
	currentUsage := float64(len(dm.cache)) / float64(dm.config.MaxCacheSize)
	if currentUsage > dm.config.CleanupThreshold {
		targetSize := int(float64(dm.config.MaxCacheSize) * (dm.config.CleanupThreshold - 0.1))

		// Remover as entradas menos recentemente usadas
		current := dm.lruTail.prev
		for len(dm.cache) > targetSize && current != dm.lruHead {
			next := current.prev
			dm.removeEntry(current)
			thresholdEvicted++
			current = next
		}
	}

	if expiredCount > 0 || thresholdEvicted > 0 {
		dm.logger.WithFields(logrus.Fields{
			"expired_entries":    expiredCount,
			"threshold_evicted":  thresholdEvicted,
			"cache_size":        len(dm.cache),
			"cache_usage_pct":   currentUsage * 100,
		}).Debug("Cache cleanup completed")
	}

	dm.stats.CacheSize = len(dm.cache)
}

// GetStats retorna estatísticas do cache
func (dm *DeduplicationManager) GetStats() Stats {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	stats := dm.stats
	stats.CacheSize = len(dm.cache)

	// Calcular hit rate
	if stats.TotalChecks > 0 {
		// Adicionar hit rate como campo calculado seria útil, mas Stats não tem
		// Por enquanto, o usuário pode calcular: CacheHits / TotalChecks
	}

	return stats
}

// GetCacheInfo retorna informações detalhadas do cache
func (dm *DeduplicationManager) GetCacheInfo() map[string]interface{} {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	stats := dm.GetStats()
	hitRate := float64(0)
	if stats.TotalChecks > 0 {
		hitRate = float64(stats.CacheHits) / float64(stats.TotalChecks) * 100
	}

	duplicateRate := float64(0)
	if stats.TotalChecks > 0 {
		duplicateRate = float64(stats.Duplicates) / float64(stats.TotalChecks) * 100
	}

	usage := float64(0)
	if dm.config.MaxCacheSize > 0 {
		usage = float64(len(dm.cache)) / float64(dm.config.MaxCacheSize) * 100
	}

	return map[string]interface{}{
		"cache_size":        len(dm.cache),
		"max_cache_size":    dm.config.MaxCacheSize,
		"cache_usage_pct":   usage,
		"total_checks":      stats.TotalChecks,
		"cache_hits":        stats.CacheHits,
		"cache_misses":      stats.CacheMisses,
		"hit_rate_pct":      hitRate,
		"duplicates":        stats.Duplicates,
		"duplicate_rate_pct": duplicateRate,
		"evicted_entries":   stats.EvictedEntries,
		"cleanup_runs":      stats.CleanupRuns,
		"ttl":              dm.config.TTL.String(),
		"hash_algorithm":    dm.config.HashAlgorithm,
	}
}

// Clear limpa todo o cache
func (dm *DeduplicationManager) Clear() {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	dm.cache = make(map[string]*CacheEntry)
	dm.lruHead.next = dm.lruTail
	dm.lruTail.prev = dm.lruHead

	dm.logger.Info("Deduplication cache cleared")
}

// updateMetrics atualiza métricas do Prometheus
func (dm *DeduplicationManager) updateMetrics() {
	stats := dm.GetStats()

	// Update cache size
	metrics.DeduplicationCacheSize.Set(float64(stats.CacheSize))

	// Update hit rate
	if stats.TotalChecks > 0 {
		hitRate := float64(stats.CacheHits) / float64(stats.TotalChecks)
		metrics.DeduplicationCacheHitRate.Set(hitRate)

		duplicateRate := float64(stats.Duplicates) / float64(stats.TotalChecks)
		metrics.DeduplicationDuplicateRate.Set(duplicateRate)
	}

	// Update evictions counter (only the delta)
	// Note: Prometheus Counter doesn't support Set(), so we track previous value
	// This is handled automatically by the Counter type - just increment when eviction happens
}