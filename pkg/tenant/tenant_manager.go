package tenant

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"
)

// TenantManager gerencia múltiplos tenants no sistema
type TenantManager struct {
	tenants        map[string]*Tenant
	configs        map[string]*TenantConfig
	mu             sync.RWMutex
	defaultTenant  string
	isolationMode  string // "soft", "hard"
	updateCallback func(tenantID string, config *TenantConfig)
}

// Tenant representa um tenant individual com seus recursos isolados
type Tenant struct {
	ID            string
	Config        *TenantConfig
	Dispatcher    types.Dispatcher
	Processors    map[string]types.Processor
	Sinks         map[string]types.Sink
	Monitors      map[string]types.Monitor
	CreatedAt     time.Time
	LastActivity  time.Time
	IsActive      bool
	ResourceUsage *TenantResourceUsage
	mu            sync.RWMutex
}

// TenantConfig configuração específica de um tenant
type TenantConfig struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Enabled     bool                   `yaml:"enabled" json:"enabled"`
	CreatedAt   time.Time              `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time              `yaml:"updated_at" json:"updated_at"`

	// Configurações específicas do tenant
	Dispatcher      TenantDispatcherConfig      `yaml:"dispatcher" json:"dispatcher"`
	Sinks          map[string]interface{}      `yaml:"sinks" json:"sinks"`
	Processing     TenantProcessingConfig      `yaml:"processing" json:"processing"`
	Monitoring     TenantMonitoringConfig      `yaml:"monitoring" json:"monitoring"`
	ResourceLimits TenantResourceLimitsConfig  `yaml:"resource_limits" json:"resource_limits"`
	Security       TenantSecurityConfig        `yaml:"security" json:"security"`

	// Labels e metadados
	Labels      map[string]string `yaml:"labels" json:"labels"`
	Annotations map[string]string `yaml:"annotations" json:"annotations"`
}

// TenantDispatcherConfig configuração do dispatcher por tenant
type TenantDispatcherConfig struct {
	QueueSize    int    `yaml:"queue_size" json:"queue_size"`
	WorkerCount  int    `yaml:"worker_count" json:"worker_count"`
	BatchSize    int    `yaml:"batch_size" json:"batch_size"`
	BatchTimeout string `yaml:"batch_timeout" json:"batch_timeout"`
	MaxRetries   int    `yaml:"max_retries" json:"max_retries"`
}

// TenantProcessingConfig configuração de processamento por tenant
type TenantProcessingConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	PipelinesFile string `yaml:"pipelines_file" json:"pipelines_file"`
	WorkerCount   int    `yaml:"worker_count" json:"worker_count"`
	QueueSize     int    `yaml:"queue_size" json:"queue_size"`
}

// TenantMonitoringConfig configuração de monitoramento por tenant
type TenantMonitoringConfig struct {
	MetricsEnabled  bool              `yaml:"metrics_enabled" json:"metrics_enabled"`
	MetricsPrefix   string            `yaml:"metrics_prefix" json:"metrics_prefix"`
	CustomLabels    map[string]string `yaml:"custom_labels" json:"custom_labels"`
	HealthChecks    bool              `yaml:"health_checks" json:"health_checks"`
}

// TenantResourceLimitsConfig limites de recursos por tenant
type TenantResourceLimitsConfig struct {
	MaxMemoryMB      int64   `yaml:"max_memory_mb" json:"max_memory_mb"`
	MaxCPUPercent    float64 `yaml:"max_cpu_percent" json:"max_cpu_percent"`
	MaxDiskMB        int64   `yaml:"max_disk_mb" json:"max_disk_mb"`
	MaxConnections   int     `yaml:"max_connections" json:"max_connections"`
	MaxEventsPerSec  int     `yaml:"max_events_per_sec" json:"max_events_per_sec"`
	MaxFileDescriptors int   `yaml:"max_file_descriptors" json:"max_file_descriptors"`
}

// TenantSecurityConfig configuração de segurança por tenant
type TenantSecurityConfig struct {
	IsolationLevel     string   `yaml:"isolation_level" json:"isolation_level"` // "none", "soft", "hard"
	AllowedSources     []string `yaml:"allowed_sources" json:"allowed_sources"`
	DeniedSources      []string `yaml:"denied_sources" json:"denied_sources"`
	EncryptionEnabled  bool     `yaml:"encryption_enabled" json:"encryption_enabled"`
	AuthenticationRequired bool `yaml:"authentication_required" json:"authentication_required"`
	APIKeyRequired     bool     `yaml:"api_key_required" json:"api_key_required"`
}

// TenantResourceUsage uso atual de recursos de um tenant
type TenantResourceUsage struct {
	MemoryUsageMB    float64   `json:"memory_usage_mb"`
	CPUUsagePercent  float64   `json:"cpu_usage_percent"`
	DiskUsageMB      float64   `json:"disk_usage_mb"`
	ConnectionCount  int       `json:"connection_count"`
	EventsPerSec     float64   `json:"events_per_sec"`
	FileDescriptors  int       `json:"file_descriptors"`
	LastUpdated      time.Time `json:"last_updated"`
}

// NewTenantManager cria um novo gerenciador de tenants
func NewTenantManager(defaultTenant, isolationMode string) *TenantManager {
	return &TenantManager{
		tenants:       make(map[string]*Tenant),
		configs:       make(map[string]*TenantConfig),
		defaultTenant: defaultTenant,
		isolationMode: isolationMode,
	}
}

// CreateTenant cria um novo tenant
func (tm *TenantManager) CreateTenant(config *TenantConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.tenants[config.ID]; exists {
		return fmt.Errorf("tenant %s already exists", config.ID)
	}

	// Validar configuração
	if err := tm.validateTenantConfig(config); err != nil {
		return fmt.Errorf("invalid tenant config: %v", err)
	}

	// Criar tenant
	tenant := &Tenant{
		ID:            config.ID,
		Config:        config,
		Processors:    make(map[string]types.Processor),
		Sinks:         make(map[string]types.Sink),
		Monitors:      make(map[string]types.Monitor),
		CreatedAt:     time.Now(),
		LastActivity:  time.Now(),
		IsActive:      config.Enabled,
		ResourceUsage: &TenantResourceUsage{
			LastUpdated: time.Now(),
		},
	}

	// Inicializar recursos do tenant
	if err := tm.initializeTenantResources(tenant); err != nil {
		return fmt.Errorf("failed to initialize tenant resources: %v", err)
	}

	tm.tenants[config.ID] = tenant
	tm.configs[config.ID] = config

	// Notificar callback se configurado
	if tm.updateCallback != nil {
		tm.updateCallback(config.ID, config)
	}

	return nil
}

// GetTenant obtém um tenant por ID
func (tm *TenantManager) GetTenant(tenantID string) (*Tenant, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tenantID == "" {
		tenantID = tm.defaultTenant
	}

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	return tenant, nil
}

// ListTenants lista todos os tenants
func (tm *TenantManager) ListTenants() []*Tenant {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(tm.tenants))
	for _, tenant := range tm.tenants {
		tenants = append(tenants, tenant)
	}

	return tenants
}

// UpdateTenant atualiza configuração de um tenant
func (tm *TenantManager) UpdateTenant(tenantID string, config *TenantConfig) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	// Validar nova configuração
	if err := tm.validateTenantConfig(config); err != nil {
		return fmt.Errorf("invalid tenant config: %v", err)
	}

	// Atualizar configuração
	config.UpdatedAt = time.Now()
	oldConfig := tenant.Config
	tenant.Config = config
	tm.configs[tenantID] = config

	// Re-inicializar recursos se necessário
	if tm.configurationChanged(oldConfig, config) {
		if err := tm.reinitializeTenantResources(tenant); err != nil {
			// Rollback se falhar
			tenant.Config = oldConfig
			tm.configs[tenantID] = oldConfig
			return fmt.Errorf("failed to update tenant resources: %v", err)
		}
	}

	// Notificar callback
	if tm.updateCallback != nil {
		tm.updateCallback(tenantID, config)
	}

	return nil
}

// DeleteTenant remove um tenant
func (tm *TenantManager) DeleteTenant(tenantID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	// Não permitir deletar tenant padrão
	if tenantID == tm.defaultTenant {
		return fmt.Errorf("cannot delete default tenant")
	}

	// Parar recursos do tenant
	if err := tm.stopTenantResources(tenant); err != nil {
		return fmt.Errorf("failed to stop tenant resources: %v", err)
	}

	delete(tm.tenants, tenantID)
	delete(tm.configs, tenantID)

	return nil
}

// GetTenantForLogEntry determina qual tenant deve processar uma entrada de log
func (tm *TenantManager) GetTenantForLogEntry(entry *types.LogEntry) string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Verificar labels da entrada de log (thread-safe)
	if tenantID, exists := entry.GetLabel("tenant"); exists {
		if _, tenantExists := tm.tenants[tenantID]; tenantExists {
			return tenantID
		}
	}

	// Verificar por source type/ID
	for _, tenant := range tm.tenants {
		if tm.matchesTenantCriteria(tenant, entry) {
			return tenant.ID
		}
	}

	// Retornar tenant padrão
	return tm.defaultTenant
}

// validateTenantConfig valida a configuração de um tenant
func (tm *TenantManager) validateTenantConfig(config *TenantConfig) error {
	if config.ID == "" {
		return fmt.Errorf("tenant ID is required")
	}

	if config.Name == "" {
		return fmt.Errorf("tenant name is required")
	}

	// Validar limites de recursos
	if config.ResourceLimits.MaxMemoryMB < 0 {
		return fmt.Errorf("max memory must be >= 0")
	}

	if config.ResourceLimits.MaxCPUPercent < 0 || config.ResourceLimits.MaxCPUPercent > 100 {
		return fmt.Errorf("max CPU percent must be between 0 and 100")
	}

	// Validar configuração do dispatcher
	if config.Dispatcher.QueueSize < 0 {
		return fmt.Errorf("queue size must be >= 0")
	}

	if config.Dispatcher.WorkerCount < 1 {
		return fmt.Errorf("worker count must be >= 1")
	}

	return nil
}

// initializeTenantResources inicializa os recursos de um tenant
func (tm *TenantManager) initializeTenantResources(tenant *Tenant) error {
	// Implementar inicialização de recursos específicos do tenant
	// Este é um placeholder - a implementação real dependeria dos componentes específicos

	// Inicializar dispatcher personalizado
	// tenant.Dispatcher = createTenantDispatcher(tenant.Config.Dispatcher)

	// Inicializar sinks personalizados
	// for sinkName, sinkConfig := range tenant.Config.Sinks {
	//     sink := createTenantSink(sinkName, sinkConfig, tenant.ID)
	//     tenant.Sinks[sinkName] = sink
	// }

	return nil
}

// reinitializeTenantResources re-inicializa recursos após mudança de configuração
func (tm *TenantManager) reinitializeTenantResources(tenant *Tenant) error {
	// Parar recursos atuais
	if err := tm.stopTenantResources(tenant); err != nil {
		return err
	}

	// Re-inicializar com nova configuração
	return tm.initializeTenantResources(tenant)
}

// stopTenantResources para todos os recursos de um tenant
func (tm *TenantManager) stopTenantResources(tenant *Tenant) error {
	var errors []error

	// Parar dispatcher
	if tenant.Dispatcher != nil {
		if err := tenant.Dispatcher.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop dispatcher: %v", err))
		}
	}

	// Parar sinks
	for name, sink := range tenant.Sinks {
		if err := sink.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop sink %s: %v", name, err))
		}
	}

	// Parar monitors
	for name, monitor := range tenant.Monitors {
		if err := monitor.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop monitor %s: %v", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping tenant resources: %v", errors)
	}

	return nil
}

// configurationChanged verifica se houve mudanças significativas na configuração
func (tm *TenantManager) configurationChanged(old, new *TenantConfig) bool {
	// Comparar configurações críticas que requerem reinicialização
	if old.Dispatcher.QueueSize != new.Dispatcher.QueueSize ||
		old.Dispatcher.WorkerCount != new.Dispatcher.WorkerCount ||
		old.Processing.Enabled != new.Processing.Enabled ||
		old.Processing.WorkerCount != new.Processing.WorkerCount {
		return true
	}

	// Comparar sinks (implementação simplificada)
	if len(old.Sinks) != len(new.Sinks) {
		return true
	}

	return false
}

// matchesTenantCriteria verifica se uma entrada de log corresponde aos critérios de um tenant
func (tm *TenantManager) matchesTenantCriteria(tenant *Tenant, entry *types.LogEntry) bool {
	// Implementar lógica de matching baseada em:
	// - Labels específicos
	// - Source patterns
	// - Configurações de segurança

	// Verificar sources permitidos
	for _, allowedSource := range tenant.Config.Security.AllowedSources {
		if entry.SourceType == allowedSource || entry.SourceID == allowedSource {
			return true
		}
	}

	// Verificar sources negados
	for _, deniedSource := range tenant.Config.Security.DeniedSources {
		if entry.SourceType == deniedSource || entry.SourceID == deniedSource {
			return false
		}
	}

	return false
}

// UpdateResourceUsage atualiza o uso de recursos de um tenant
func (t *Tenant) UpdateResourceUsage(usage *TenantResourceUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	usage.LastUpdated = time.Now()
	t.ResourceUsage = usage
	t.LastActivity = time.Now()
}

// IsWithinLimits verifica se o tenant está dentro dos limites de recursos
func (t *Tenant) IsWithinLimits() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	limits := t.Config.ResourceLimits

	if limits.MaxMemoryMB > 0 && t.ResourceUsage.MemoryUsageMB > float64(limits.MaxMemoryMB) {
		return false
	}

	if limits.MaxCPUPercent > 0 && t.ResourceUsage.CPUUsagePercent > limits.MaxCPUPercent {
		return false
	}

	if limits.MaxDiskMB > 0 && t.ResourceUsage.DiskUsageMB > float64(limits.MaxDiskMB) {
		return false
	}

	if limits.MaxConnections > 0 && t.ResourceUsage.ConnectionCount > limits.MaxConnections {
		return false
	}

	if limits.MaxEventsPerSec > 0 && t.ResourceUsage.EventsPerSec > float64(limits.MaxEventsPerSec) {
		return false
	}

	return true
}

// GetStatus retorna o status atual do tenant
func (t *Tenant) GetStatus() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"id":             t.ID,
		"name":           t.Config.Name,
		"enabled":        t.Config.Enabled,
		"active":         t.IsActive,
		"created_at":     t.CreatedAt,
		"last_activity":  t.LastActivity,
		"resource_usage": t.ResourceUsage,
		"within_limits":  t.IsWithinLimits(),
		"sinks_count":    len(t.Sinks),
		"monitors_count": len(t.Monitors),
	}
}

// SetUpdateCallback define callback para mudanças de configuração
func (tm *TenantManager) SetUpdateCallback(callback func(tenantID string, config *TenantConfig)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.updateCallback = callback
}