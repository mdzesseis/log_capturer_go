package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// BatchPersistence gerencia persistência de batches para recovery
type BatchPersistence struct {
	config Config
	logger *logrus.Logger

	pendingBatches map[string]*PersistedBatch
	mutex          sync.RWMutex
	stats          Stats

	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração da persistência de batches
type Config struct {
	// Habilitar persistência
	Enabled bool `yaml:"enabled"`

	// Diretório para armazenar batches
	Directory string `yaml:"directory"`

	// Máximo de batches pendentes em memória
	MaxPendingBatches int `yaml:"max_pending_batches"`

	// Timeout para considerar batch como falha
	BatchTimeout time.Duration `yaml:"batch_timeout"`

	// Máximo de tentativas de recovery
	MaxRecoveryRetries int `yaml:"max_recovery_retries"`

	// Backoff base para retry
	RecoveryBackoffBase time.Duration `yaml:"recovery_backoff_base"`

	// Backoff máximo para retry
	RecoveryBackoffMax time.Duration `yaml:"recovery_backoff_max"`

	// Intervalo de limpeza de batches antigos
	CleanupInterval time.Duration `yaml:"cleanup_interval"`

	// TTL para batches persistidos
	BatchTTL time.Duration `yaml:"batch_ttl"`
}

// PersistedBatch representa um batch persistido
type PersistedBatch struct {
	ID           string            `json:"id"`
	Entries      []types.LogEntry  `json:"entries"`
	SinkType     string            `json:"sink_type"`
	CreatedAt    time.Time         `json:"created_at"`
	LastAttempt  time.Time         `json:"last_attempt"`
	RetryCount   int               `json:"retry_count"`
	FailureReason string           `json:"failure_reason,omitempty"`
	Context      map[string]string `json:"context,omitempty"`
}

// Stats estatísticas da persistência
type Stats struct {
	BatchesPersisted   int64 `json:"batches_persisted"`
	BatchesRecovered   int64 `json:"batches_recovered"`
	BatchesFailed      int64 `json:"batches_failed"`
	PendingBatches     int   `json:"pending_batches"`
	RecoveryAttempts   int64 `json:"recovery_attempts"`
	LastCleanup        time.Time `json:"last_cleanup"`
}

// NewBatchPersistence cria nova instância de persistência
func NewBatchPersistence(config Config, logger *logrus.Logger) *BatchPersistence {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.Directory == "" {
		config.Directory = "./batch_persistence"
	}
	if config.MaxPendingBatches == 0 {
		config.MaxPendingBatches = 1000
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 30 * time.Minute
	}
	if config.MaxRecoveryRetries == 0 {
		config.MaxRecoveryRetries = 10
	}
	if config.RecoveryBackoffBase == 0 {
		config.RecoveryBackoffBase = 1 * time.Second
	}
	if config.RecoveryBackoffMax == 0 {
		config.RecoveryBackoffMax = 30 * time.Second
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = time.Hour
	}
	if config.BatchTTL == 0 {
		config.BatchTTL = 24 * time.Hour
	}

	return &BatchPersistence{
		config:         config,
		logger:         logger,
		pendingBatches: make(map[string]*PersistedBatch),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start inicia a persistência de batches
func (bp *BatchPersistence) Start() error {
	if !bp.config.Enabled {
		bp.logger.Info("Batch persistence disabled")
		return nil
	}

	bp.logger.WithFields(logrus.Fields{
		"directory":            bp.config.Directory,
		"max_pending_batches":  bp.config.MaxPendingBatches,
		"batch_timeout":        bp.config.BatchTimeout,
		"max_recovery_retries": bp.config.MaxRecoveryRetries,
	}).Info("Starting batch persistence")

	// Criar diretório se não existir
	if err := os.MkdirAll(bp.config.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create persistence directory: %w", err)
	}

	// Carregar batches existentes
	if err := bp.loadPersistedBatches(); err != nil {
		bp.logger.WithError(err).Warn("Failed to load persisted batches")
	}

	// Iniciar loops de manutenção
	go bp.cleanupLoop()
	go bp.recoveryLoop()

	return nil
}

// Stop para a persistência
func (bp *BatchPersistence) Stop() error {
	if !bp.config.Enabled {
		return nil
	}

	bp.logger.Info("Stopping batch persistence")
	bp.cancel()

	// Persistir batches pendentes
	bp.mutex.RLock()
	for _, batch := range bp.pendingBatches {
		bp.persistBatchToDisk(batch)
	}
	bp.mutex.RUnlock()

	return nil
}

// PersistBatch persiste um batch antes do envio
func (bp *BatchPersistence) PersistBatch(batchID string, entries []types.LogEntry, sinkType string) error {
	if !bp.config.Enabled {
		return nil
	}

	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	// Verificar limite de batches pendentes
	if len(bp.pendingBatches) >= bp.config.MaxPendingBatches {
		bp.logger.Warn("Max pending batches reached, dropping batch")
		return fmt.Errorf("max pending batches reached")
	}

	batch := &PersistedBatch{
		ID:        batchID,
		Entries:   entries,
		SinkType:  sinkType,
		CreatedAt: time.Now(),
		RetryCount: 0,
		Context: map[string]string{
			"created_by": "batch_persistence",
		},
	}

	// Adicionar à memória
	bp.pendingBatches[batchID] = batch

	// Persistir no disco
	if err := bp.persistBatchToDisk(batch); err != nil {
		delete(bp.pendingBatches, batchID)
		return fmt.Errorf("failed to persist batch to disk: %w", err)
	}

	bp.stats.BatchesPersisted++

	bp.logger.WithFields(logrus.Fields{
		"batch_id":    batchID,
		"sink_type":   sinkType,
		"entry_count": len(entries),
	}).Debug("Batch persisted")

	return nil
}

// MarkBatchSuccess marca batch como enviado com sucesso
func (bp *BatchPersistence) MarkBatchSuccess(batchID string) {
	if !bp.config.Enabled {
		return
	}

	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	if batch, exists := bp.pendingBatches[batchID]; exists {
		// Remover da memória
		delete(bp.pendingBatches, batchID)

		// Remover do disco
		bp.removeBatchFromDisk(batch)

		bp.logger.WithField("batch_id", batchID).Debug("Batch marked as successful")
	}
}

// MarkBatchFailed marca batch como falha
func (bp *BatchPersistence) MarkBatchFailed(batchID, reason string) {
	if !bp.config.Enabled {
		return
	}

	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	if batch, exists := bp.pendingBatches[batchID]; exists {
		batch.LastAttempt = time.Now()
		batch.RetryCount++
		batch.FailureReason = reason

		// Verificar se excedeu máximo de tentativas
		if batch.RetryCount >= bp.config.MaxRecoveryRetries {
			bp.logger.WithFields(logrus.Fields{
				"batch_id":    batchID,
				"retry_count": batch.RetryCount,
				"reason":      reason,
			}).Error("Batch exceeded max recovery retries")

			delete(bp.pendingBatches, batchID)
			bp.removeBatchFromDisk(batch)
			bp.stats.BatchesFailed++
		} else {
			// Atualizar no disco
			bp.persistBatchToDisk(batch)

			bp.logger.WithFields(logrus.Fields{
				"batch_id":    batchID,
				"retry_count": batch.RetryCount,
				"reason":      reason,
			}).Debug("Batch marked for retry")
		}
	}
}

// GetPendingBatches retorna batches pendentes para recovery
func (bp *BatchPersistence) GetPendingBatches() []*PersistedBatch {
	if !bp.config.Enabled {
		return nil
	}

	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	var batches []*PersistedBatch
	now := time.Now()

	for _, batch := range bp.pendingBatches {
		// Verificar se está pronto para retry (baseado em backoff)
		backoff := bp.calculateBackoff(batch.RetryCount)
		if now.Sub(batch.LastAttempt) >= backoff {
			batches = append(batches, batch)
		}
	}

	return batches
}

// persistBatchToDisk persiste batch no disco
func (bp *BatchPersistence) persistBatchToDisk(batch *PersistedBatch) error {
	filename := fmt.Sprintf("batch_%s.json", batch.ID)
	filepath := filepath.Join(bp.config.Directory, filename)

	data, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// removeBatchFromDisk remove batch do disco
func (bp *BatchPersistence) removeBatchFromDisk(batch *PersistedBatch) {
	filename := fmt.Sprintf("batch_%s.json", batch.ID)
	filepath := filepath.Join(bp.config.Directory, filename)
	os.Remove(filepath)
}

// loadPersistedBatches carrega batches do disco
func (bp *BatchPersistence) loadPersistedBatches() error {
	pattern := filepath.Join(bp.config.Directory, "batch_*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	loadedCount := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			bp.logger.WithError(err).WithField("file", file).Warn("Failed to read batch file")
			continue
		}

		var batch PersistedBatch
		if err := json.Unmarshal(data, &batch); err != nil {
			bp.logger.WithError(err).WithField("file", file).Warn("Failed to unmarshal batch")
			continue
		}

		// Verificar se batch não expirou
		if time.Since(batch.CreatedAt) > bp.config.BatchTTL {
			os.Remove(file)
			continue
		}

		bp.pendingBatches[batch.ID] = &batch
		loadedCount++
	}

	if loadedCount > 0 {
		bp.logger.WithField("loaded_count", loadedCount).Info("Loaded persisted batches")
	}

	return nil
}

// calculateBackoff calcula backoff exponencial
func (bp *BatchPersistence) calculateBackoff(retryCount int) time.Duration {
	backoff := bp.config.RecoveryBackoffBase * time.Duration(1<<uint(retryCount))
	if backoff > bp.config.RecoveryBackoffMax {
		backoff = bp.config.RecoveryBackoffMax
	}
	return backoff
}

// cleanupLoop loop de limpeza de batches antigos
func (bp *BatchPersistence) cleanupLoop() {
	ticker := time.NewTicker(bp.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.performCleanup()
		}
	}
}

// performCleanup remove batches expirados
func (bp *BatchPersistence) performCleanup() {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	now := time.Now()
	removedCount := 0

	for id, batch := range bp.pendingBatches {
		if now.Sub(batch.CreatedAt) > bp.config.BatchTTL {
			delete(bp.pendingBatches, id)
			bp.removeBatchFromDisk(batch)
			removedCount++
		}
	}

	bp.stats.LastCleanup = now

	if removedCount > 0 {
		bp.logger.WithField("removed_count", removedCount).Info("Cleanup completed")
	}
}

// recoveryLoop loop de recovery de batches
func (bp *BatchPersistence) recoveryLoop() {
	ticker := time.NewTicker(30 * time.Second) // Recovery check a cada 30s
	defer ticker.Stop()

	for {
		select {
		case <-bp.ctx.Done():
			return
		case <-ticker.C:
			bp.attemptRecovery()
		}
	}
}

// attemptRecovery tenta recovery de batches pendentes
func (bp *BatchPersistence) attemptRecovery() {
	pendingBatches := bp.GetPendingBatches()
	if len(pendingBatches) == 0 {
		return
	}

	bp.logger.WithField("pending_count", len(pendingBatches)).Debug("Attempting batch recovery")

	for _, batch := range pendingBatches {
		bp.stats.RecoveryAttempts++
		bp.logger.WithFields(logrus.Fields{
			"batch_id":    batch.ID,
			"retry_count": batch.RetryCount,
			"sink_type":   batch.SinkType,
		}).Info("Batch ready for recovery")
	}
}

// GetStats retorna estatísticas
func (bp *BatchPersistence) GetStats() Stats {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	stats := bp.stats
	stats.PendingBatches = len(bp.pendingBatches)
	return stats
}

// IsHealthy verifica se a persistência está saudável
func (bp *BatchPersistence) IsHealthy() bool {
	if !bp.config.Enabled {
		return true
	}

	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	// Verificar se não há muitos batches pendentes
	return len(bp.pendingBatches) < bp.config.MaxPendingBatches
}