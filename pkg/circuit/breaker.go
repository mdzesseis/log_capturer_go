package circuit

import (
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// BreakerConfig configuração do circuit breaker
type BreakerConfig struct {
	Name               string        `yaml:"name"`
	FailureThreshold   int           `yaml:"failure_threshold"`   // Falhas consecutivas para abrir
	SuccessThreshold   int           `yaml:"success_threshold"`   // Sucessos para fechar
	Timeout            time.Duration `yaml:"timeout"`             // Tempo no estado aberto
	HalfOpenMaxCalls   int           `yaml:"half_open_max_calls"` // Máximo de calls no estado half-open
	ResetTimeout       time.Duration `yaml:"reset_timeout"`       // Timeout para reset automático
}

// Breaker implementa o padrão Circuit Breaker
type Breaker struct {
	config BreakerConfig
	logger *logrus.Logger

	state         string
	failures      int64
	successes     int64
	requests      int64
	lastFailure   time.Time
	lastSuccess   time.Time
	nextRetryTime time.Time

	// Controle de half-open
	halfOpenCalls int
	maxHalfOpen   int

	// Callbacks para eventos
	onStateChange func(from, to string)
	onFailure     func(error)
	onSuccess     func()

	mu sync.RWMutex
}

// NewBreaker cria um novo circuit breaker
func NewBreaker(config BreakerConfig, logger *logrus.Logger) *Breaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 3
	}
	if config.Timeout <= 0 {
		config.Timeout = 60 * time.Second
	}
	if config.HalfOpenMaxCalls <= 0 {
		config.HalfOpenMaxCalls = 10
	}
	if config.ResetTimeout <= 0 {
		config.ResetTimeout = 10 * time.Minute
	}

	return &Breaker{
		config:        config,
		logger:        logger,
		state:         types.CircuitBreakerClosed,
		maxHalfOpen:   config.HalfOpenMaxCalls,
	}
}

// Execute executa uma função com proteção do circuit breaker
func (b *Breaker) Execute(fn func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.requests++

	// Verificar se deve abrir o circuit
	if b.shouldTrip() {
		b.trip()
	}

	// Verificar se pode tentar novamente
	if b.state == types.CircuitBreakerOpen {
		if time.Now().Before(b.nextRetryTime) {
			return fmt.Errorf("circuit breaker %s is open", b.config.Name)
		}
		// Transição para half-open
		b.setState(types.CircuitBreakerHalfOpen)
		b.halfOpenCalls = 0
	}

	// Verificar limite de half-open
	if b.state == types.CircuitBreakerHalfOpen {
		if b.halfOpenCalls >= b.maxHalfOpen {
			return fmt.Errorf("circuit breaker %s is half-open (max calls reached)", b.config.Name)
		}
		b.halfOpenCalls++
	}

	// Executar função
	err := fn()

	if err != nil {
		b.onExecutionFailure(err)
		return err
	}

	b.onExecutionSuccess()
	return nil
}

// shouldTrip verifica se o circuit deve ser aberto
func (b *Breaker) shouldTrip() bool {
	if b.state != types.CircuitBreakerClosed {
		return false
	}

	return b.failures >= int64(b.config.FailureThreshold)
}

// trip abre o circuit breaker
func (b *Breaker) trip() {
	if b.state == types.CircuitBreakerOpen {
		return
	}

	b.setState(types.CircuitBreakerOpen)
	b.nextRetryTime = time.Now().Add(b.config.Timeout)

	b.logger.WithFields(logrus.Fields{
		"breaker":         b.config.Name,
		"failures":        b.failures,
		"next_retry_time": b.nextRetryTime,
	}).Warn("Circuit breaker opened")
}

// onExecutionFailure manipula falha na execução
func (b *Breaker) onExecutionFailure(err error) {
	b.failures++
	b.lastFailure = time.Now()

	if b.onFailure != nil {
		b.onFailure(err)
	}

	// Em half-open, volta para open imediatamente
	if b.state == types.CircuitBreakerHalfOpen {
		b.trip()
	}
}

// onExecutionSuccess manipula sucesso na execução
func (b *Breaker) onExecutionSuccess() {
	b.successes++
	b.lastSuccess = time.Now()

	if b.onSuccess != nil {
		b.onSuccess()
	}

	// Em half-open, verificar se pode fechar
	if b.state == types.CircuitBreakerHalfOpen {
		if b.halfOpenCalls >= b.config.SuccessThreshold {
			b.setState(types.CircuitBreakerClosed)
			b.reset()
		}
	} else if b.state == types.CircuitBreakerClosed {
		// Reset contador de falhas em sucessos
		if b.failures > 0 {
			b.failures = max(0, b.failures-1)
		}
	}
}

// reset reseta contadores do circuit breaker
func (b *Breaker) reset() {
	b.failures = 0
	b.halfOpenCalls = 0
	b.nextRetryTime = time.Time{}

	b.logger.WithFields(logrus.Fields{
		"breaker":   b.config.Name,
		"successes": b.successes,
	}).Info("Circuit breaker reset")
}

// setState muda o estado do circuit breaker
func (b *Breaker) setState(newState string) {
	if b.state == newState {
		return
	}

	oldState := b.state
	b.state = newState

	if b.onStateChange != nil {
		b.onStateChange(oldState, newState)
	}

	b.logger.WithFields(logrus.Fields{
		"breaker":    b.config.Name,
		"old_state":  oldState,
		"new_state":  newState,
		"failures":   b.failures,
		"successes":  b.successes,
	}).Info("Circuit breaker state changed")
}

// State retorna o estado atual do circuit breaker
func (b *Breaker) State() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// IsOpen verifica se o circuit breaker está aberto
func (b *Breaker) IsOpen() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state == types.CircuitBreakerOpen
}

// Reset força o reset do circuit breaker
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.setState(types.CircuitBreakerClosed)
	b.reset()
}

// GetStats retorna estatísticas do circuit breaker
func (b *Breaker) GetStats() types.CircuitBreakerStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return types.CircuitBreakerStats{
		State:         b.state,
		Failures:      b.failures,
		Successes:     b.successes,
		Requests:      b.requests,
		LastFailure:   b.lastFailure,
		LastSuccess:   b.lastSuccess,
		NextRetryTime: b.nextRetryTime,
	}
}

// SetStateChangeCallback define callback para mudanças de estado
func (b *Breaker) SetStateChangeCallback(fn func(from, to string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onStateChange = fn
}

// SetFailureCallback define callback para falhas
func (b *Breaker) SetFailureCallback(fn func(error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onFailure = fn
}

// SetSuccessCallback define callback para sucessos
func (b *Breaker) SetSuccessCallback(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onSuccess = fn
}

// CanExecute verifica se pode executar uma operação
func (b *Breaker) CanExecute() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	switch b.state {
	case types.CircuitBreakerClosed:
		return true
	case types.CircuitBreakerOpen:
		return time.Now().After(b.nextRetryTime)
	case types.CircuitBreakerHalfOpen:
		return b.halfOpenCalls < b.maxHalfOpen
	default:
		return false
	}
}

// ForceOpen força abertura do circuit breaker
func (b *Breaker) ForceOpen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trip()
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}