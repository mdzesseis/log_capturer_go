package backpressure

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Level representa o nível de backpressure
type Level int

const (
	LevelNone Level = iota
	LevelLow
	LevelMedium
	LevelHigh
	LevelCritical
)

func (l Level) String() string {
	switch l {
	case LevelNone:
		return "none"
	case LevelLow:
		return "low"
	case LevelMedium:
		return "medium"
	case LevelHigh:
		return "high"
	case LevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Config configuração do manager de backpressure
type Config struct {
	// Thresholds para diferentes níveis
	LowThreshold      float64 `yaml:"low_threshold"`      // 0.6 = 60%
	MediumThreshold   float64 `yaml:"medium_threshold"`   // 0.75 = 75%
	HighThreshold     float64 `yaml:"high_threshold"`     // 0.9 = 90%
	CriticalThreshold float64 `yaml:"critical_threshold"` // 0.95 = 95%

	// Configurações de tempo
	CheckInterval    time.Duration `yaml:"check_interval"`    // Intervalo de verificação
	StabilizeTime    time.Duration `yaml:"stabilize_time"`    // Tempo para estabilizar nível
	CooldownTime     time.Duration `yaml:"cooldown_time"`     // Tempo de cooldown entre mudanças

	// Fatores de redução por nível
	LowReduction      float64 `yaml:"low_reduction"`      // 0.9 = 90% da capacidade
	MediumReduction   float64 `yaml:"medium_reduction"`   // 0.7 = 70% da capacidade
	HighReduction     float64 `yaml:"high_reduction"`     // 0.5 = 50% da capacidade
	CriticalReduction float64 `yaml:"critical_reduction"` // 0.2 = 20% da capacidade
}

// Metrics métricas para cálculo do backpressure
type Metrics struct {
	QueueUtilization  float64 // 0.0 - 1.0
	MemoryUtilization float64 // 0.0 - 1.0
	CPUUtilization    float64 // 0.0 - 1.0
	IOUtilization     float64 // 0.0 - 1.0
	ErrorRate         float64 // 0.0 - 1.0
}

// Manager gerencia backpressure baseado em métricas do sistema
type Manager struct {
	config Config
	logger *logrus.Logger

	// Estado atual
	currentLevel     Level
	currentFactor    float64
	lastLevelChange  time.Time
	lastCheck        time.Time
	stabilizeUntil   time.Time

	// Callbacks
	onLevelChange func(Level, Level, float64)

	// Métricas coletadas
	metrics Metrics

	mu sync.RWMutex
}

// NewManager cria um novo manager de backpressure
func NewManager(config Config, logger *logrus.Logger) *Manager {
	// Valores padrão
	if config.LowThreshold == 0 {
		config.LowThreshold = 0.6
	}
	if config.MediumThreshold == 0 {
		config.MediumThreshold = 0.75
	}
	if config.HighThreshold == 0 {
		config.HighThreshold = 0.9
	}
	if config.CriticalThreshold == 0 {
		config.CriticalThreshold = 0.95
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Second
	}
	if config.StabilizeTime == 0 {
		config.StabilizeTime = 30 * time.Second
	}
	if config.CooldownTime == 0 {
		config.CooldownTime = 10 * time.Second
	}
	if config.LowReduction == 0 {
		config.LowReduction = 0.9
	}
	if config.MediumReduction == 0 {
		config.MediumReduction = 0.7
	}
	if config.HighReduction == 0 {
		config.HighReduction = 0.5
	}
	if config.CriticalReduction == 0 {
		config.CriticalReduction = 0.2
	}

	return &Manager{
		config:        config,
		logger:        logger,
		currentLevel:  LevelNone,
		currentFactor: 1.0,
	}
}

// UpdateMetrics atualiza as métricas do sistema
func (m *Manager) UpdateMetrics(metrics Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.metrics = metrics
	m.lastCheck = time.Now()

	// Verificar se precisa ajustar o nível
	m.evaluateLevel()
}

// evaluateLevel avalia e ajusta o nível de backpressure
func (m *Manager) evaluateLevel() {
	// Calcular score geral (média ponderada)
	overallScore := (m.metrics.QueueUtilization * 0.3) +
		(m.metrics.MemoryUtilization * 0.25) +
		(m.metrics.CPUUtilization * 0.2) +
		(m.metrics.IOUtilization * 0.15) +
		(m.metrics.ErrorRate * 0.1)

	// Determinar novo nível baseado no score
	newLevel := m.calculateLevel(overallScore)

	// Verificar cooldown
	if time.Since(m.lastLevelChange) < m.config.CooldownTime {
		return
	}

	// Verificar se precisa estabilizar
	if time.Now().Before(m.stabilizeUntil) && newLevel != m.currentLevel {
		return
	}

	// Aplicar mudança de nível se necessário
	if newLevel != m.currentLevel {
		m.changeLevel(newLevel)
	}
}

// calculateLevel calcula o nível baseado no score
func (m *Manager) calculateLevel(score float64) Level {
	switch {
	case score >= m.config.CriticalThreshold:
		return LevelCritical
	case score >= m.config.HighThreshold:
		return LevelHigh
	case score >= m.config.MediumThreshold:
		return LevelMedium
	case score >= m.config.LowThreshold:
		return LevelLow
	default:
		return LevelNone
	}
}

// changeLevel muda o nível de backpressure
func (m *Manager) changeLevel(newLevel Level) {
	oldLevel := m.currentLevel
	m.currentLevel = newLevel
	m.lastLevelChange = time.Now()
	m.stabilizeUntil = time.Now().Add(m.config.StabilizeTime)

	// Calcular novo fator de redução
	switch newLevel {
	case LevelNone:
		m.currentFactor = 1.0
	case LevelLow:
		m.currentFactor = m.config.LowReduction
	case LevelMedium:
		m.currentFactor = m.config.MediumReduction
	case LevelHigh:
		m.currentFactor = m.config.HighReduction
	case LevelCritical:
		m.currentFactor = m.config.CriticalReduction
	}

	m.logger.WithFields(logrus.Fields{
		"old_level":     oldLevel.String(),
		"new_level":     newLevel.String(),
		"factor":        m.currentFactor,
		"queue_util":    m.metrics.QueueUtilization,
		"memory_util":   m.metrics.MemoryUtilization,
		"cpu_util":      m.metrics.CPUUtilization,
		"io_util":       m.metrics.IOUtilization,
		"error_rate":    m.metrics.ErrorRate,
	}).Info("Backpressure level changed")

	// Notificar callback
	if m.onLevelChange != nil {
		m.onLevelChange(oldLevel, newLevel, m.currentFactor)
	}
}

// GetLevel retorna o nível atual de backpressure
func (m *Manager) GetLevel() Level {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel
}

// GetFactor retorna o fator de redução atual
func (m *Manager) GetFactor() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentFactor
}

// IsActive verifica se o backpressure está ativo
func (m *Manager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel != LevelNone
}

// ShouldThrottle verifica se deve aplicar throttling
func (m *Manager) ShouldThrottle() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel >= LevelMedium
}

// ShouldReject verifica se deve rejeitar novas requisições
func (m *Manager) ShouldReject() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel >= LevelCritical
}

// ShouldDegrade verifica se deve degradar funcionalidades
func (m *Manager) ShouldDegrade() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentLevel >= LevelHigh
}

// GetMetrics retorna as métricas atuais
func (m *Manager) GetMetrics() Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metrics
}

// SetLevelChangeCallback define callback para mudanças de nível
func (m *Manager) SetLevelChangeCallback(fn func(Level, Level, float64)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onLevelChange = fn
}

// Start inicia o monitor de backpressure
func (m *Manager) Start(ctx context.Context) error {
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	m.logger.Info("Starting backpressure manager")

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping backpressure manager")
			return ctx.Err()
		case <-ticker.C:
			m.mu.Lock()
			// Re-evaluar com métricas atuais se passou tempo suficiente
			if time.Since(m.lastCheck) > m.config.CheckInterval {
				m.evaluateLevel()
			}
			m.mu.Unlock()
		}
	}
}

// ForceLevel força um nível específico de backpressure
func (m *Manager) ForceLevel(level Level) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeLevel(level)
}

// Reset reseta o backpressure para nível none
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeLevel(LevelNone)
}

// GetStats retorna estatísticas do manager
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"current_level":      m.currentLevel.String(),
		"current_factor":     m.currentFactor,
		"last_level_change":  m.lastLevelChange,
		"last_check":         m.lastCheck,
		"stabilize_until":    m.stabilizeUntil,
		"is_active":          m.currentLevel != LevelNone,
		"should_throttle":    m.currentLevel >= LevelMedium,
		"should_reject":      m.currentLevel >= LevelCritical,
		"should_degrade":     m.currentLevel >= LevelHigh,
		"metrics":            m.metrics,
	}
}