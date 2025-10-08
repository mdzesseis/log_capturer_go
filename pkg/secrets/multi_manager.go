package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SecretManager interface para gerenciadores de secrets
type SecretManager interface {
	GetSecret(ctx context.Context, key string) (string, error)
	SetSecret(ctx context.Context, key, value string) error
	DeleteSecret(ctx context.Context, key string) error
	ListSecrets(ctx context.Context) ([]string, error)
	IsHealthy() bool
	Close() error
}

// MultiSecretsManager gerencia múltiplos backends de secrets
type MultiSecretsManager struct {
	config   Config
	logger   *logrus.Logger
	backends map[string]SecretManager

	// Cache de secrets
	cache      map[string]*CachedSecret
	cacheMutex sync.RWMutex

	// Estatísticas
	stats Stats
	mutex sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// Config configuração do secrets manager
type Config struct {
	// Backend padrão: "env", "vault", "aws", "k8s"
	DefaultBackend string `yaml:"default_backend"`

	// Configurações por backend
	Backends map[string]BackendConfig `yaml:"backends"`

	// Cache de secrets
	CacheEnabled bool          `yaml:"cache_enabled"`
	CacheTTL     time.Duration `yaml:"cache_ttl"`
	CacheSize    int           `yaml:"cache_size"`

	// Rotação automática
	RotationEnabled  bool          `yaml:"rotation_enabled"`
	RotationInterval time.Duration `yaml:"rotation_interval"`

	// Fallback entre backends
	FallbackEnabled bool     `yaml:"fallback_enabled"`
	FallbackOrder   []string `yaml:"fallback_order"`

	// Prefixos para diferentes tipos de secrets
	Prefixes map[string]string `yaml:"prefixes"`
}

// BackendConfig configuração específica de backend
type BackendConfig struct {
	Type     string            `yaml:"type"`
	Enabled  bool              `yaml:"enabled"`
	Options  map[string]string `yaml:"options"`
	Priority int               `yaml:"priority"`
}

// CachedSecret secret em cache
type CachedSecret struct {
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
	Backend   string    `json:"backend"`
}

// Stats estatísticas do secrets manager
type Stats struct {
	TotalRequests    int64             `json:"total_requests"`
	CacheHits        int64             `json:"cache_hits"`
	CacheMisses      int64             `json:"cache_misses"`
	BackendRequests  map[string]int64  `json:"backend_requests"`
	BackendErrors    map[string]int64  `json:"backend_errors"`
	LastRotation     time.Time         `json:"last_rotation"`
	RotationCount    int64             `json:"rotation_count"`
}

// NewMultiSecretsManager cria novo gerenciador multi-backend
func NewMultiSecretsManager(config Config, logger *logrus.Logger) (*MultiSecretsManager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Valores padrão
	if config.DefaultBackend == "" {
		config.DefaultBackend = "env"
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}
	if config.CacheSize == 0 {
		config.CacheSize = 1000
	}
	if config.RotationInterval == 0 {
		config.RotationInterval = 24 * time.Hour
	}

	msm := &MultiSecretsManager{
		config:   config,
		logger:   logger,
		backends: make(map[string]SecretManager),
		cache:    make(map[string]*CachedSecret),
		stats: Stats{
			BackendRequests: make(map[string]int64),
			BackendErrors:   make(map[string]int64),
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Inicializar backends
	if err := msm.initializeBackends(); err != nil {
		return nil, err
	}

	// Iniciar loops de manutenção
	go msm.cacheCleanupLoop()
	if config.RotationEnabled {
		go msm.rotationLoop()
	}

	return msm, nil
}

// initializeBackends inicializa todos os backends configurados
func (msm *MultiSecretsManager) initializeBackends() error {
	for name, backendConfig := range msm.config.Backends {
		if !backendConfig.Enabled {
			continue
		}

		backend, err := msm.createBackend(backendConfig)
		if err != nil {
			msm.logger.WithError(err).WithField("backend", name).Error("Failed to create backend")
			continue
		}

		msm.backends[name] = backend
		msm.logger.WithField("backend", name).Info("Secret backend initialized")
	}

	if len(msm.backends) == 0 {
		return fmt.Errorf("no secret backends available")
	}

	return nil
}

// createBackend cria backend específico
func (msm *MultiSecretsManager) createBackend(config BackendConfig) (SecretManager, error) {
	switch config.Type {
	case "env":
		return NewEnvBackend(config.Options, msm.logger), nil
	case "vault":
		return NewVaultBackend(config.Options, msm.logger)
	case "aws":
		return NewAWSBackend(config.Options, msm.logger)
	case "k8s":
		return NewK8sBackend(config.Options, msm.logger)
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", config.Type)
	}
}

// GetSecret obtém secret usando fallback se necessário
func (msm *MultiSecretsManager) GetSecret(ctx context.Context, key string) (string, error) {
	msm.mutex.Lock()
	msm.stats.TotalRequests++
	msm.mutex.Unlock()

	// Verificar cache primeiro
	if msm.config.CacheEnabled {
		if cached := msm.getFromCache(key); cached != nil {
			msm.mutex.Lock()
			msm.stats.CacheHits++
			msm.mutex.Unlock()
			return cached.Value, nil
		}
		msm.mutex.Lock()
		msm.stats.CacheMisses++
		msm.mutex.Unlock()
	}

	// Tentar backend padrão primeiro
	if backend, exists := msm.backends[msm.config.DefaultBackend]; exists {
		if value, err := msm.getFromBackend(ctx, backend, msm.config.DefaultBackend, key); err == nil {
			msm.addToCache(key, value, msm.config.DefaultBackend)
			return value, nil
		}
	}

	// Fallback para outros backends se habilitado
	if msm.config.FallbackEnabled {
		for _, backendName := range msm.config.FallbackOrder {
			if backend, exists := msm.backends[backendName]; exists && backendName != msm.config.DefaultBackend {
				if value, err := msm.getFromBackend(ctx, backend, backendName, key); err == nil {
					msm.addToCache(key, value, backendName)
					return value, nil
				}
			}
		}
	}

	return "", fmt.Errorf("secret not found in any backend: %s", key)
}

// getFromBackend obtém secret de backend específico
func (msm *MultiSecretsManager) getFromBackend(ctx context.Context, backend SecretManager, backendName, key string) (string, error) {
	msm.mutex.Lock()
	msm.stats.BackendRequests[backendName]++
	msm.mutex.Unlock()

	value, err := backend.GetSecret(ctx, key)
	if err != nil {
		msm.mutex.Lock()
		msm.stats.BackendErrors[backendName]++
		msm.mutex.Unlock()
		return "", err
	}

	return value, nil
}

// SetSecret define secret no backend padrão
func (msm *MultiSecretsManager) SetSecret(ctx context.Context, key, value string) error {
	backend, exists := msm.backends[msm.config.DefaultBackend]
	if !exists {
		return fmt.Errorf("default backend not available: %s", msm.config.DefaultBackend)
	}

	if err := backend.SetSecret(ctx, key, value); err != nil {
		msm.mutex.Lock()
		msm.stats.BackendErrors[msm.config.DefaultBackend]++
		msm.mutex.Unlock()
		return err
	}

	// Invalidar cache
	msm.removeFromCache(key)

	return nil
}

// DeleteSecret remove secret do backend padrão
func (msm *MultiSecretsManager) DeleteSecret(ctx context.Context, key string) error {
	backend, exists := msm.backends[msm.config.DefaultBackend]
	if !exists {
		return fmt.Errorf("default backend not available: %s", msm.config.DefaultBackend)
	}

	if err := backend.DeleteSecret(ctx, key); err != nil {
		return err
	}

	// Invalidar cache
	msm.removeFromCache(key)

	return nil
}

// GetSecretWithPrefix obtém secret com prefixo específico
func (msm *MultiSecretsManager) GetSecretWithPrefix(ctx context.Context, prefix, key string) (string, error) {
	fullKey := fmt.Sprintf("%s/%s", prefix, key)
	return msm.GetSecret(ctx, fullKey)
}

// GetDatabaseSecret obtém secret de banco de dados
func (msm *MultiSecretsManager) GetDatabaseSecret(ctx context.Context, key string) (string, error) {
	if dbPrefix, exists := msm.config.Prefixes["database"]; exists {
		return msm.GetSecretWithPrefix(ctx, dbPrefix, key)
	}
	return msm.GetSecret(ctx, "db/"+key)
}

// GetAPISecret obtém secret de API
func (msm *MultiSecretsManager) GetAPISecret(ctx context.Context, key string) (string, error) {
	if apiPrefix, exists := msm.config.Prefixes["api"]; exists {
		return msm.GetSecretWithPrefix(ctx, apiPrefix, key)
	}
	return msm.GetSecret(ctx, "api/"+key)
}

// getFromCache obtém secret do cache
func (msm *MultiSecretsManager) getFromCache(key string) *CachedSecret {
	msm.cacheMutex.RLock()
	defer msm.cacheMutex.RUnlock()

	cached, exists := msm.cache[key]
	if !exists {
		return nil
	}

	if time.Now().After(cached.ExpiresAt) {
		// Cache expirado
		return nil
	}

	return cached
}

// addToCache adiciona secret ao cache
func (msm *MultiSecretsManager) addToCache(key, value, backend string) {
	if !msm.config.CacheEnabled {
		return
	}

	msm.cacheMutex.Lock()
	defer msm.cacheMutex.Unlock()

	// Verificar limite de cache
	if len(msm.cache) >= msm.config.CacheSize {
		msm.evictOldestFromCache()
	}

	msm.cache[key] = &CachedSecret{
		Value:     value,
		ExpiresAt: time.Now().Add(msm.config.CacheTTL),
		Backend:   backend,
	}
}

// removeFromCache remove secret do cache
func (msm *MultiSecretsManager) removeFromCache(key string) {
	msm.cacheMutex.Lock()
	defer msm.cacheMutex.Unlock()
	delete(msm.cache, key)
}

// evictOldestFromCache remove entrada mais antiga do cache
func (msm *MultiSecretsManager) evictOldestFromCache() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range msm.cache {
		if oldestKey == "" || cached.ExpiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.ExpiresAt
		}
	}

	if oldestKey != "" {
		delete(msm.cache, oldestKey)
	}
}

// cacheCleanupLoop loop de limpeza do cache
func (msm *MultiSecretsManager) cacheCleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-msm.ctx.Done():
			return
		case <-ticker.C:
			msm.cleanupExpiredCache()
		}
	}
}

// cleanupExpiredCache remove entradas expiradas do cache
func (msm *MultiSecretsManager) cleanupExpiredCache() {
	msm.cacheMutex.Lock()
	defer msm.cacheMutex.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	for key, cached := range msm.cache {
		if now.After(cached.ExpiresAt) {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		delete(msm.cache, key)
	}

	if len(expiredKeys) > 0 {
		msm.logger.WithField("expired_count", len(expiredKeys)).Debug("Cleaned up expired cache entries")
	}
}

// rotationLoop loop de rotação de secrets
func (msm *MultiSecretsManager) rotationLoop() {
	ticker := time.NewTicker(msm.config.RotationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-msm.ctx.Done():
			return
		case <-ticker.C:
			msm.performRotation()
		}
	}
}

// performRotation executa rotação de secrets
func (msm *MultiSecretsManager) performRotation() {
	msm.logger.Info("Starting secret rotation")

	// Invalidar todo o cache para forçar re-fetch
	msm.cacheMutex.Lock()
	msm.cache = make(map[string]*CachedSecret)
	msm.cacheMutex.Unlock()

	msm.mutex.Lock()
	msm.stats.RotationCount++
	msm.stats.LastRotation = time.Now()
	msm.mutex.Unlock()

	msm.logger.Info("Secret rotation completed")
}

// IsHealthy verifica se o manager está saudável
func (msm *MultiSecretsManager) IsHealthy() bool {
	healthyBackends := 0
	for _, backend := range msm.backends {
		if backend.IsHealthy() {
			healthyBackends++
		}
	}

	// Pelo menos um backend deve estar saudável
	return healthyBackends > 0
}

// GetStats retorna estatísticas
func (msm *MultiSecretsManager) GetStats() Stats {
	msm.mutex.RLock()
	defer msm.mutex.RUnlock()
	return msm.stats
}

// GetInfo retorna informações detalhadas
func (msm *MultiSecretsManager) GetInfo() map[string]interface{} {
	stats := msm.GetStats()

	cacheHitRate := float64(0)
	if stats.TotalRequests > 0 {
		cacheHitRate = float64(stats.CacheHits) / float64(stats.TotalRequests) * 100
	}

	msm.cacheMutex.RLock()
	cacheSize := len(msm.cache)
	msm.cacheMutex.RUnlock()

	backendHealth := make(map[string]bool)
	for name, backend := range msm.backends {
		backendHealth[name] = backend.IsHealthy()
	}

	return map[string]interface{}{
		"default_backend":     msm.config.DefaultBackend,
		"cache_enabled":       msm.config.CacheEnabled,
		"cache_ttl":           msm.config.CacheTTL.String(),
		"cache_size":          cacheSize,
		"cache_max_size":      msm.config.CacheSize,
		"rotation_enabled":    msm.config.RotationEnabled,
		"rotation_interval":   msm.config.RotationInterval.String(),
		"fallback_enabled":    msm.config.FallbackEnabled,
		"fallback_order":      msm.config.FallbackOrder,
		"total_requests":      stats.TotalRequests,
		"cache_hits":          stats.CacheHits,
		"cache_misses":        stats.CacheMisses,
		"cache_hit_rate_pct":  cacheHitRate,
		"backend_requests":    stats.BackendRequests,
		"backend_errors":      stats.BackendErrors,
		"backend_health":      backendHealth,
		"last_rotation":       stats.LastRotation,
		"rotation_count":      stats.RotationCount,
	}
}

// Close fecha todos os backends
func (msm *MultiSecretsManager) Close() error {
	msm.cancel()

	var lastError error
	for name, backend := range msm.backends {
		if err := backend.Close(); err != nil {
			msm.logger.WithError(err).WithField("backend", name).Error("Failed to close backend")
			lastError = err
		}
	}

	return lastError
}

// EnvBackend implementação simples usando variáveis de ambiente
type EnvBackend struct {
	prefix string
	logger *logrus.Logger
}

// NewEnvBackend cria backend de variáveis de ambiente
func NewEnvBackend(options map[string]string, logger *logrus.Logger) *EnvBackend {
	prefix := options["prefix"]
	if prefix == "" {
		prefix = "SECRET_"
	}

	return &EnvBackend{
		prefix: prefix,
		logger: logger,
	}
}

// GetSecret obtém secret de variável de ambiente
func (eb *EnvBackend) GetSecret(ctx context.Context, key string) (string, error) {
	envKey := eb.prefix + strings.ToUpper(strings.ReplaceAll(key, "/", "_"))
	value := os.Getenv(envKey)
	if value == "" {
		return "", fmt.Errorf("environment variable not found: %s", envKey)
	}
	return value, nil
}

// SetSecret define variável de ambiente (não persistente)
func (eb *EnvBackend) SetSecret(ctx context.Context, key, value string) error {
	envKey := eb.prefix + strings.ToUpper(strings.ReplaceAll(key, "/", "_"))
	return os.Setenv(envKey, value)
}

// DeleteSecret remove variável de ambiente
func (eb *EnvBackend) DeleteSecret(ctx context.Context, key string) error {
	envKey := eb.prefix + strings.ToUpper(strings.ReplaceAll(key, "/", "_"))
	return os.Unsetenv(envKey)
}

// ListSecrets lista variáveis de ambiente com prefixo
func (eb *EnvBackend) ListSecrets(ctx context.Context) ([]string, error) {
	var secrets []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, eb.prefix) {
			key := strings.SplitN(env, "=", 2)[0]
			secrets = append(secrets, strings.TrimPrefix(key, eb.prefix))
		}
	}
	return secrets, nil
}

// IsHealthy verifica se backend está saudável
func (eb *EnvBackend) IsHealthy() bool {
	return true // Environment variables sempre disponíveis
}

// Close fecha backend
func (eb *EnvBackend) Close() error {
	return nil // Nada para fechar
}

// Implementações stub para outros backends (Vault, AWS, K8s)
// Estas seriam implementações completas em um sistema real

// NewVaultBackend cria backend Vault (stub)
func NewVaultBackend(options map[string]string, logger *logrus.Logger) (SecretManager, error) {
	return nil, fmt.Errorf("vault backend not implemented")
}

// NewAWSBackend cria backend AWS Secrets Manager (stub)
func NewAWSBackend(options map[string]string, logger *logrus.Logger) (SecretManager, error) {
	return nil, fmt.Errorf("aws backend not implemented")
}

// NewK8sBackend cria backend Kubernetes Secrets (stub)
func NewK8sBackend(options map[string]string, logger *logrus.Logger) (SecretManager, error) {
	return nil, fmt.Errorf("k8s backend not implemented")
}