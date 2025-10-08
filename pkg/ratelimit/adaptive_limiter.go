package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AdaptiveRateLimiter implementa rate limiting adaptativo baseado em latência
type AdaptiveRateLimiter struct {
	config Config
	logger *logrus.Logger

	// Estado atual
	currentRPS       float64
	currentBurst     int
	tokens           float64
	lastRefill       time.Time
	latencyHistory   *LatencyWindow

	// Estatísticas
	stats Stats
	mutex sync.RWMutex

	// Controle de adaptação
	lastAdaptation    time.Time
	adaptationCooldown time.Duration

	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração do rate limiter adaptativo
type Config struct {
	// Habilitar rate limiting
	Enabled bool `yaml:"enabled"`

	// RPS inicial
	InitialRPS float64 `yaml:"initial_rps"`

	// RPS mínimo
	MinRPS float64 `yaml:"min_rps"`

	// RPS máximo
	MaxRPS float64 `yaml:"max_rps"`

	// Burst inicial
	InitialBurst int `yaml:"initial_burst"`

	// Burst mínimo
	MinBurst int `yaml:"min_burst"`

	// Burst máximo
	MaxBurst int `yaml:"max_burst"`

	// Target de latência (ms)
	LatencyTargetMS int `yaml:"latency_target_ms"`

	// Tolerance de latência (% acima do target)
	LatencyTolerance float64 `yaml:"latency_tolerance"`

	// Bytes por token (para rate limiting por bytes)
	BytesPerToken int64 `yaml:"bytes_per_token"`

	// Intervalo de adaptação
	AdaptationInterval time.Duration `yaml:"adaptation_interval"`

	// Janela de medição de latência
	LatencyWindowSize int `yaml:"latency_window_size"`

	// Fator de agressividade da adaptação
	AdaptationFactor float64 `yaml:"adaptation_factor"`

	// Suavização de adaptação
	SmoothingFactor float64 `yaml:"smoothing_factor"`
}

// Stats estatísticas do rate limiter
type Stats struct {
	TotalRequests     int64   `json:"total_requests"`
	AllowedRequests   int64   `json:"allowed_requests"`
	BlockedRequests   int64   `json:"blocked_requests"`
	BytesProcessed    int64   `json:"bytes_processed"`
	CurrentRPS        float64 `json:"current_rps"`
	CurrentBurst      int     `json:"current_burst"`
	AverageLatencyMS  float64 `json:"average_latency_ms"`
	AdaptationCount   int64   `json:"adaptation_count"`
	LastAdaptation    time.Time `json:"last_adaptation"`
}

// LatencyWindow mantém janela deslizante de latências
type LatencyWindow struct {
	samples []time.Duration
	index   int
	size    int
	mutex   sync.Mutex
}

// NewLatencyWindow cria nova janela de latência
func NewLatencyWindow(size int) *LatencyWindow {
	return &LatencyWindow{
		samples: make([]time.Duration, size),
		size:    size,
	}
}

// Add adiciona sample de latência
func (lw *LatencyWindow) Add(latency time.Duration) {
	lw.mutex.Lock()
	defer lw.mutex.Unlock()

	lw.samples[lw.index] = latency
	lw.index = (lw.index + 1) % lw.size
}

// Average calcula latência média
func (lw *LatencyWindow) Average() time.Duration {
	lw.mutex.Lock()
	defer lw.mutex.Unlock()

	var total time.Duration
	count := 0

	for _, sample := range lw.samples {
		if sample > 0 {
			total += sample
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / time.Duration(count)
}

// NewAdaptiveRateLimiter cria novo rate limiter adaptativo
func NewAdaptiveRateLimiter(config Config, logger *logrus.Logger) *AdaptiveRateLimiter {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.InitialRPS == 0 {
		config.InitialRPS = 10
	}
	if config.MinRPS == 0 {
		config.MinRPS = 1
	}
	if config.MaxRPS == 0 {
		config.MaxRPS = 1000
	}
	if config.InitialBurst == 0 {
		config.InitialBurst = int(config.InitialRPS * 2)
	}
	if config.MinBurst == 0 {
		config.MinBurst = 1
	}
	if config.MaxBurst == 0 {
		config.MaxBurst = int(config.MaxRPS * 2)
	}
	if config.LatencyTargetMS == 0 {
		config.LatencyTargetMS = 500
	}
	if config.LatencyTolerance == 0 {
		config.LatencyTolerance = 0.2 // 20%
	}
	if config.BytesPerToken == 0 {
		config.BytesPerToken = 65536 // 64KB
	}
	if config.AdaptationInterval == 0 {
		config.AdaptationInterval = 30 * time.Second
	}
	if config.LatencyWindowSize == 0 {
		config.LatencyWindowSize = 100
	}
	if config.AdaptationFactor == 0 {
		config.AdaptationFactor = 0.1 // 10% de mudança por adaptação
	}
	if config.SmoothingFactor == 0 {
		config.SmoothingFactor = 0.8 // Suavização exponencial
	}

	rl := &AdaptiveRateLimiter{
		config:             config,
		logger:             logger,
		currentRPS:         config.InitialRPS,
		currentBurst:       config.InitialBurst,
		tokens:             float64(config.InitialBurst),
		lastRefill:         time.Now(),
		latencyHistory:     NewLatencyWindow(config.LatencyWindowSize),
		adaptationCooldown: config.AdaptationInterval,
		ctx:                ctx,
		cancel:             cancel,
	}

	// Iniciar loop de adaptação
	go rl.adaptationLoop()

	return rl
}

// Allow verifica se requisição é permitida
func (rl *AdaptiveRateLimiter) Allow() bool {
	if !rl.config.Enabled {
		return true
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	rl.stats.TotalRequests++

	// Refill tokens baseado no tempo decorrido
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now

	// Calcular tokens a adicionar
	tokensToAdd := elapsed * rl.currentRPS
	rl.tokens = math.Min(rl.tokens+tokensToAdd, float64(rl.currentBurst))

	// Verificar se há tokens disponíveis
	if rl.tokens >= 1 {
		rl.tokens--
		rl.stats.AllowedRequests++
		return true
	}

	rl.stats.BlockedRequests++
	return false
}

// AllowN verifica se N requisições são permitidas
func (rl *AdaptiveRateLimiter) AllowN(n int) bool {
	if !rl.config.Enabled {
		return true
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	rl.stats.TotalRequests += int64(n)

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.lastRefill = now

	tokensToAdd := elapsed * rl.currentRPS
	rl.tokens = math.Min(rl.tokens+tokensToAdd, float64(rl.currentBurst))

	// Verificar se há tokens suficientes
	if rl.tokens >= float64(n) {
		rl.tokens -= float64(n)
		rl.stats.AllowedRequests += int64(n)
		return true
	}

	rl.stats.BlockedRequests += int64(n)
	return false
}

// AllowBytes verifica se bytes são permitidos
func (rl *AdaptiveRateLimiter) AllowBytes(bytes int64) bool {
	if !rl.config.Enabled || rl.config.BytesPerToken == 0 {
		return true
	}

	tokens := int(math.Ceil(float64(bytes) / float64(rl.config.BytesPerToken)))
	if rl.AllowN(tokens) {
		rl.mutex.Lock()
		rl.stats.BytesProcessed += bytes
		rl.mutex.Unlock()
		return true
	}

	return false
}

// RecordLatency registra latência para adaptação
func (rl *AdaptiveRateLimiter) RecordLatency(latency time.Duration) {
	if !rl.config.Enabled {
		return
	}

	rl.latencyHistory.Add(latency)
}

// adaptationLoop loop principal de adaptação
func (rl *AdaptiveRateLimiter) adaptationLoop() {
	ticker := time.NewTicker(rl.config.AdaptationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.ctx.Done():
			return
		case <-ticker.C:
			rl.performAdaptation()
		}
	}
}

// performAdaptation executa adaptação baseada na latência
func (rl *AdaptiveRateLimiter) performAdaptation() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	avgLatency := rl.latencyHistory.Average()
	if avgLatency == 0 {
		// Sem dados de latência, não adaptar
		return
	}

	targetLatency := time.Duration(rl.config.LatencyTargetMS) * time.Millisecond
	toleranceThreshold := float64(targetLatency) * (1 + rl.config.LatencyTolerance)

	rl.logger.WithFields(logrus.Fields{
		"avg_latency_ms":    avgLatency.Milliseconds(),
		"target_latency_ms": targetLatency.Milliseconds(),
		"current_rps":       rl.currentRPS,
		"current_burst":     rl.currentBurst,
	}).Debug("Performing rate limit adaptation")

	var adaptationNeeded bool
	var newRPS float64
	var newBurst int

	if float64(avgLatency) > toleranceThreshold {
		// Latência alta - reduzir RPS
		reductionFactor := 1 - rl.config.AdaptationFactor
		newRPS = rl.currentRPS * reductionFactor
		adaptationNeeded = true

		rl.logger.WithFields(logrus.Fields{
			"reason":       "high_latency",
			"avg_latency":  avgLatency.Milliseconds(),
			"target":       targetLatency.Milliseconds(),
			"old_rps":      rl.currentRPS,
			"new_rps":      newRPS,
		}).Info("Reducing RPS due to high latency")

	} else if float64(avgLatency) < float64(targetLatency)*0.8 {
		// Latência baixa - aumentar RPS
		increaseFactor := 1 + rl.config.AdaptationFactor
		newRPS = rl.currentRPS * increaseFactor
		adaptationNeeded = true

		rl.logger.WithFields(logrus.Fields{
			"reason":       "low_latency",
			"avg_latency":  avgLatency.Milliseconds(),
			"target":       targetLatency.Milliseconds(),
			"old_rps":      rl.currentRPS,
			"new_rps":      newRPS,
		}).Info("Increasing RPS due to low latency")
	}

	if adaptationNeeded {
		// Aplicar limites
		newRPS = math.Max(newRPS, rl.config.MinRPS)
		newRPS = math.Min(newRPS, rl.config.MaxRPS)

		// Calcular novo burst proporcional
		burstRatio := float64(rl.currentBurst) / rl.currentRPS
		newBurst = int(newRPS * burstRatio)
		newBurst = int(math.Max(float64(newBurst), float64(rl.config.MinBurst)))
		newBurst = int(math.Min(float64(newBurst), float64(rl.config.MaxBurst)))

		// Aplicar suavização exponencial
		if rl.stats.AdaptationCount > 0 {
			newRPS = rl.currentRPS*rl.config.SmoothingFactor + newRPS*(1-rl.config.SmoothingFactor)
		}

		// Atualizar valores
		rl.currentRPS = newRPS
		rl.currentBurst = newBurst
		rl.stats.AdaptationCount++
		rl.stats.LastAdaptation = time.Now()

		rl.logger.WithFields(logrus.Fields{
			"new_rps":          rl.currentRPS,
			"new_burst":        rl.currentBurst,
			"adaptation_count": rl.stats.AdaptationCount,
		}).Info("Rate limits adapted")
	}

	// Atualizar stats
	rl.stats.CurrentRPS = rl.currentRPS
	rl.stats.CurrentBurst = rl.currentBurst
	rl.stats.AverageLatencyMS = float64(avgLatency.Milliseconds())
}

// Wait aguarda até que requisição seja permitida
func (rl *AdaptiveRateLimiter) Wait(ctx context.Context) error {
	if !rl.config.Enabled {
		return nil
	}

	for {
		if rl.Allow() {
			return nil
		}

		// Calcular tempo de espera baseado no déficit de tokens
		rl.mutex.RLock()
		waitTime := time.Duration(1000/rl.currentRPS) * time.Millisecond
		rl.mutex.RUnlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			continue
		}
	}
}

// GetCurrentLimits retorna limites atuais
func (rl *AdaptiveRateLimiter) GetCurrentLimits() (rps float64, burst int) {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()
	return rl.currentRPS, rl.currentBurst
}

// GetStats retorna estatísticas
func (rl *AdaptiveRateLimiter) GetStats() Stats {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	stats := rl.stats
	stats.CurrentRPS = rl.currentRPS
	stats.CurrentBurst = rl.currentBurst
	stats.AverageLatencyMS = float64(rl.latencyHistory.Average().Milliseconds())

	return stats
}

// GetInfo retorna informações detalhadas
func (rl *AdaptiveRateLimiter) GetInfo() map[string]interface{} {
	stats := rl.GetStats()

	allowRate := float64(0)
	if stats.TotalRequests > 0 {
		allowRate = float64(stats.AllowedRequests) / float64(stats.TotalRequests) * 100
	}

	return map[string]interface{}{
		"enabled":                rl.config.Enabled,
		"current_rps":            stats.CurrentRPS,
		"current_burst":          stats.CurrentBurst,
		"min_rps":                rl.config.MinRPS,
		"max_rps":                rl.config.MaxRPS,
		"latency_target_ms":      rl.config.LatencyTargetMS,
		"latency_tolerance":      rl.config.LatencyTolerance,
		"bytes_per_token":        rl.config.BytesPerToken,
		"adaptation_interval":    rl.config.AdaptationInterval.String(),
		"total_requests":         stats.TotalRequests,
		"allowed_requests":       stats.AllowedRequests,
		"blocked_requests":       stats.BlockedRequests,
		"bytes_processed":        stats.BytesProcessed,
		"average_latency_ms":     stats.AverageLatencyMS,
		"adaptation_count":       stats.AdaptationCount,
		"last_adaptation":        stats.LastAdaptation,
		"allow_rate_percent":     allowRate,
	}
}

// Reset reseta o rate limiter para configuração inicial
func (rl *AdaptiveRateLimiter) Reset() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	rl.currentRPS = rl.config.InitialRPS
	rl.currentBurst = rl.config.InitialBurst
	rl.tokens = float64(rl.config.InitialBurst)
	rl.lastRefill = time.Now()
	rl.stats = Stats{}
	rl.latencyHistory = NewLatencyWindow(rl.config.LatencyWindowSize)

	rl.logger.Info("Rate limiter reset to initial configuration")
}

// Stop para o rate limiter
func (rl *AdaptiveRateLimiter) Stop() {
	rl.cancel()
}