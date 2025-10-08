package circuit_breaker

import (
	"errors"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"
)

var (
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
)

// Config configuração do circuit breaker
type Config struct {
	MaxFailures   int64         `yaml:"max_failures"`
	ResetTimeout  time.Duration `yaml:"reset_timeout"`
	CheckInterval time.Duration `yaml:"check_interval"`
}

// circuitBreaker implementação do pattern circuit breaker
type circuitBreaker struct {
	config    Config
	state     string
	failures  int64
	successes int64
	requests  int64
	lastFailureTime time.Time
	lastSuccessTime time.Time
	nextRetryTime   time.Time
	mutex     sync.RWMutex
}

// New cria uma nova instância do circuit breaker
func New(config Config) types.CircuitBreaker {
	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}
	if config.ResetTimeout == 0 {
		config.ResetTimeout = 30 * time.Second
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Second
	}

	return &circuitBreaker{
		config: config,
		state:  types.CircuitBreakerClosed,
	}
}

// Execute executa uma função através do circuit breaker
func (cb *circuitBreaker) Execute(fn func() error) error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.requests++

	// Verificar se o circuit breaker está aberto
	if cb.state == types.CircuitBreakerOpen {
		// Verificar se é hora de tentar novamente
		if time.Now().Before(cb.nextRetryTime) {
			return ErrCircuitBreakerOpen
		}
		// Transição para half-open
		cb.state = types.CircuitBreakerHalfOpen
	}

	// Executar a função
	err := fn()

	if err != nil {
		cb.failures++
		cb.lastFailureTime = time.Now()

		// Verificar se deve abrir o circuit breaker
		if cb.failures >= cb.config.MaxFailures {
			cb.state = types.CircuitBreakerOpen
			cb.nextRetryTime = time.Now().Add(cb.config.ResetTimeout)
		}

		return err
	}

	// Sucesso
	cb.successes++
	cb.lastSuccessTime = time.Now()

	// Se estava half-open e teve sucesso, fechar o circuit breaker
	if cb.state == types.CircuitBreakerHalfOpen {
		cb.state = types.CircuitBreakerClosed
		cb.failures = 0 // Reset do contador de falhas
	}

	return nil
}

// State retorna o estado atual do circuit breaker
func (cb *circuitBreaker) State() string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// IsOpen verifica se o circuit breaker está aberto
func (cb *circuitBreaker) IsOpen() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state == types.CircuitBreakerOpen
}

// Reset reseta o circuit breaker para o estado fechado
func (cb *circuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = types.CircuitBreakerClosed
	cb.failures = 0
	cb.nextRetryTime = time.Time{}
}

// GetStats retorna as estatísticas do circuit breaker
func (cb *circuitBreaker) GetStats() types.CircuitBreakerStats {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return types.CircuitBreakerStats{
		State:         cb.state,
		Failures:      cb.failures,
		Successes:     cb.successes,
		Requests:      cb.requests,
		LastFailure:   cb.lastFailureTime,
		LastSuccess:   cb.lastSuccessTime,
		NextRetryTime: cb.nextRetryTime,
	}
}