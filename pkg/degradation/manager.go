package degradation

import (
	"sync"
	"time"

	"ssw-logs-capture/pkg/backpressure"

	"github.com/sirupsen/logrus"
)

// Feature representa uma funcionalidade que pode ser degradada
type Feature string

const (
	// Funcionalidades não críticas que podem ser degradadas
	FeatureDeduplication    Feature = "deduplication"
	FeatureProcessing       Feature = "processing"
	FeatureCompression      Feature = "compression"
	FeatureMetricsDetailed  Feature = "metrics_detailed"
	FeatureVerboseLogging   Feature = "verbose_logging"
	FeatureHealthChecks     Feature = "health_checks"
	FeatureBatchOptimization Feature = "batch_optimization"
)

// FeatureState representa o estado de uma funcionalidade
type FeatureState struct {
	Enabled     bool      `json:"enabled"`
	DegradedAt  time.Time `json:"degraded_at,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Level       backpressure.Level `json:"level"`
}

// Config configuração do degradation manager
type Config struct {
	// Funcionalidades a degradar por nível de backpressure
	DegradeAtLow      []Feature `yaml:"degrade_at_low"`
	DegradeAtMedium   []Feature `yaml:"degrade_at_medium"`
	DegradeAtHigh     []Feature `yaml:"degrade_at_high"`
	DegradeAtCritical []Feature `yaml:"degrade_at_critical"`

	// Configurações de timing
	GracePeriod       time.Duration `yaml:"grace_period"`       // Período de graça antes de degradar
	RestoreDelay      time.Duration `yaml:"restore_delay"`      // Delay para restaurar funcionalidades
	MinDegradedTime   time.Duration `yaml:"min_degraded_time"`  // Tempo mínimo degradado
}

// Manager gerencia degradação graceful de funcionalidades
type Manager struct {
	config       Config
	logger       *logrus.Logger

	// Estado das funcionalidades
	features     map[Feature]*FeatureState
	featuresMu   sync.RWMutex

	// Controle de nível atual
	currentLevel  backpressure.Level
	levelChanged  time.Time

	// Callbacks
	onFeatureToggle func(Feature, bool, string)

	mu sync.RWMutex
}

// NewManager cria um novo degradation manager
func NewManager(config Config, logger *logrus.Logger) *Manager {
	// Valores padrão
	if config.GracePeriod == 0 {
		config.GracePeriod = 30 * time.Second
	}
	if config.RestoreDelay == 0 {
		config.RestoreDelay = 60 * time.Second
	}
	if config.MinDegradedTime == 0 {
		config.MinDegradedTime = 30 * time.Second
	}

	// Configurações padrão para degradação
	if len(config.DegradeAtLow) == 0 {
		config.DegradeAtLow = []Feature{}
	}
	if len(config.DegradeAtMedium) == 0 {
		config.DegradeAtMedium = []Feature{
			FeatureVerboseLogging,
			FeatureMetricsDetailed,
		}
	}
	if len(config.DegradeAtHigh) == 0 {
		config.DegradeAtHigh = []Feature{
			FeatureVerboseLogging,
			FeatureMetricsDetailed,
			FeatureHealthChecks,
			FeatureCompression,
		}
	}
	if len(config.DegradeAtCritical) == 0 {
		config.DegradeAtCritical = []Feature{
			FeatureVerboseLogging,
			FeatureMetricsDetailed,
			FeatureHealthChecks,
			FeatureCompression,
			FeatureDeduplication,
			FeatureProcessing,
		}
	}

	// Inicializar estado das funcionalidades
	features := make(map[Feature]*FeatureState)
	allFeatures := []Feature{
		FeatureDeduplication,
		FeatureProcessing,
		FeatureCompression,
		FeatureMetricsDetailed,
		FeatureVerboseLogging,
		FeatureHealthChecks,
		FeatureBatchOptimization,
	}

	for _, feature := range allFeatures {
		features[feature] = &FeatureState{
			Enabled: true,
			Level:   backpressure.LevelNone,
		}
	}

	return &Manager{
		config:       config,
		logger:       logger,
		features:     features,
		currentLevel: backpressure.LevelNone,
	}
}

// UpdateLevel atualiza o nível de backpressure e aplica degradações
func (m *Manager) UpdateLevel(newLevel backpressure.Level) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if newLevel == m.currentLevel {
		return
	}

	oldLevel := m.currentLevel
	m.currentLevel = newLevel
	m.levelChanged = time.Now()

	m.logger.WithFields(logrus.Fields{
		"old_level": oldLevel.String(),
		"new_level": newLevel.String(),
	}).Info("Backpressure level changed, evaluating degradations")

	// Aplicar degradações baseado no novo nível
	m.applyDegradationForLevel(newLevel)

	// Se o nível diminuiu, considerar restaurar funcionalidades
	if newLevel < oldLevel {
		m.scheduleRestore()
	}
}

// applyDegradationForLevel aplica degradações para um nível específico
func (m *Manager) applyDegradationForLevel(level backpressure.Level) {
	var featuresToDegrade []Feature

	switch level {
	case backpressure.LevelLow:
		featuresToDegrade = m.config.DegradeAtLow
	case backpressure.LevelMedium:
		featuresToDegrade = append(m.config.DegradeAtLow, m.config.DegradeAtMedium...)
	case backpressure.LevelHigh:
		featuresToDegrade = append(append(m.config.DegradeAtLow, m.config.DegradeAtMedium...), m.config.DegradeAtHigh...)
	case backpressure.LevelCritical:
		featuresToDegrade = append(append(append(m.config.DegradeAtLow, m.config.DegradeAtMedium...), m.config.DegradeAtHigh...), m.config.DegradeAtCritical...)
	default:
		// LevelNone - restaurar todas as funcionalidades
		m.restoreAllFeatures()
		return
	}

	// Aplicar degradações com período de graça
	gracePeriodExpired := time.Since(m.levelChanged) > m.config.GracePeriod

	for _, feature := range featuresToDegrade {
		if gracePeriodExpired {
			m.degradeFeature(feature, level, "system_overload")
		}
	}
}

// degradeFeature degrada uma funcionalidade específica
func (m *Manager) degradeFeature(feature Feature, level backpressure.Level, reason string) {
	m.featuresMu.Lock()
	defer m.featuresMu.Unlock()

	state, exists := m.features[feature]
	if !exists {
		return
	}

	if state.Enabled {
		state.Enabled = false
		state.DegradedAt = time.Now()
		state.Reason = reason
		state.Level = level

		m.logger.WithFields(logrus.Fields{
			"feature": string(feature),
			"level":   level.String(),
			"reason":  reason,
		}).Warn("Feature degraded due to system load")

		// Notificar callback
		if m.onFeatureToggle != nil {
			m.onFeatureToggle(feature, false, reason)
		}
	}
}

// scheduleRestore agenda restauração de funcionalidades
func (m *Manager) scheduleRestore() {
	go func() {
		time.Sleep(m.config.RestoreDelay)
		m.restoreFeatures()
	}()
}

// restoreFeatures restaura funcionalidades que podem ser restauradas
func (m *Manager) restoreFeatures() {
	m.featuresMu.Lock()
	defer m.featuresMu.Unlock()

	now := time.Now()

	for feature, state := range m.features {
		if !state.Enabled {
			// Verificar se passou o tempo mínimo degradado
			if now.Sub(state.DegradedAt) >= m.config.MinDegradedTime {
				// Verificar se o nível atual ainda requer degradação
				if !m.shouldDegradeAtCurrentLevel(feature) {
					m.restoreFeature(feature)
				}
			}
		}
	}
}

// shouldDegradeAtCurrentLevel verifica se uma funcionalidade deve estar degradada no nível atual
func (m *Manager) shouldDegradeAtCurrentLevel(feature Feature) bool {
	switch m.currentLevel {
	case backpressure.LevelLow:
		return m.containsFeature(m.config.DegradeAtLow, feature)
	case backpressure.LevelMedium:
		return m.containsFeature(m.config.DegradeAtLow, feature) ||
			   m.containsFeature(m.config.DegradeAtMedium, feature)
	case backpressure.LevelHigh:
		return m.containsFeature(m.config.DegradeAtLow, feature) ||
			   m.containsFeature(m.config.DegradeAtMedium, feature) ||
			   m.containsFeature(m.config.DegradeAtHigh, feature)
	case backpressure.LevelCritical:
		return m.containsFeature(m.config.DegradeAtLow, feature) ||
			   m.containsFeature(m.config.DegradeAtMedium, feature) ||
			   m.containsFeature(m.config.DegradeAtHigh, feature) ||
			   m.containsFeature(m.config.DegradeAtCritical, feature)
	default:
		return false
	}
}

// containsFeature verifica se uma feature está na lista
func (m *Manager) containsFeature(features []Feature, target Feature) bool {
	for _, f := range features {
		if f == target {
			return true
		}
	}
	return false
}

// restoreFeature restaura uma funcionalidade específica
func (m *Manager) restoreFeature(feature Feature) {
	state, exists := m.features[feature]
	if !exists {
		return
	}

	if !state.Enabled {
		state.Enabled = true
		state.DegradedAt = time.Time{}
		state.Reason = ""
		state.Level = backpressure.LevelNone

		m.logger.WithFields(logrus.Fields{
			"feature": string(feature),
		}).Info("Feature restored")

		// Notificar callback
		if m.onFeatureToggle != nil {
			m.onFeatureToggle(feature, true, "system_recovered")
		}
	}
}

// restoreAllFeatures restaura todas as funcionalidades
func (m *Manager) restoreAllFeatures() {
	m.featuresMu.Lock()
	defer m.featuresMu.Unlock()

	for feature := range m.features {
		m.restoreFeature(feature)
	}
}

// IsFeatureEnabled verifica se uma funcionalidade está habilitada
func (m *Manager) IsFeatureEnabled(feature Feature) bool {
	m.featuresMu.RLock()
	defer m.featuresMu.RUnlock()

	state, exists := m.features[feature]
	if !exists {
		return true // Funcionalidade desconhecida é considerada habilitada
	}

	return state.Enabled
}

// GetFeatureState retorna o estado de uma funcionalidade
func (m *Manager) GetFeatureState(feature Feature) *FeatureState {
	m.featuresMu.RLock()
	defer m.featuresMu.RUnlock()

	state, exists := m.features[feature]
	if !exists {
		return &FeatureState{Enabled: true}
	}

	// Retornar cópia para evitar modificações concorrentes
	return &FeatureState{
		Enabled:    state.Enabled,
		DegradedAt: state.DegradedAt,
		Reason:     state.Reason,
		Level:      state.Level,
	}
}

// GetAllFeatures retorna o estado de todas as funcionalidades
func (m *Manager) GetAllFeatures() map[Feature]*FeatureState {
	m.featuresMu.RLock()
	defer m.featuresMu.RUnlock()

	result := make(map[Feature]*FeatureState)
	for feature, state := range m.features {
		result[feature] = &FeatureState{
			Enabled:    state.Enabled,
			DegradedAt: state.DegradedAt,
			Reason:     state.Reason,
			Level:      state.Level,
		}
	}

	return result
}

// SetFeatureToggleCallback define callback para mudanças de funcionalidades
func (m *Manager) SetFeatureToggleCallback(fn func(Feature, bool, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFeatureToggle = fn
}

// ForceDegrade força degradação de uma funcionalidade
func (m *Manager) ForceDegrade(feature Feature, reason string) {
	m.degradeFeature(feature, m.currentLevel, reason)
}

// ForceRestore força restauração de uma funcionalidade
func (m *Manager) ForceRestore(feature Feature) {
	m.featuresMu.Lock()
	defer m.featuresMu.Unlock()
	m.restoreFeature(feature)
}

// GetStats retorna estatísticas do degradation manager
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	m.featuresMu.RLock()
	defer m.mu.RUnlock()
	defer m.featuresMu.RUnlock()

	degradedCount := 0
	enabledCount := 0

	for _, state := range m.features {
		if state.Enabled {
			enabledCount++
		} else {
			degradedCount++
		}
	}

	return map[string]interface{}{
		"current_level":     m.currentLevel.String(),
		"level_changed":     m.levelChanged,
		"enabled_features":  enabledCount,
		"degraded_features": degradedCount,
		"total_features":    len(m.features),
		"features":          m.GetAllFeatures(),
	}
}