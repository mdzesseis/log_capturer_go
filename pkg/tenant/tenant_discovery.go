package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"

	"github.com/your-org/log_capturer_go/pkg/types"
)

// TenantDiscovery sistema de descoberta automática de tenants
type TenantDiscovery struct {
	manager         *TenantManager
	discoveryConfig *TenantDiscoveryConfig
	watcher         *fsnotify.Watcher
	cancel          context.CancelFunc
	mu              sync.RWMutex
	running         bool
}

// TenantDiscoveryConfig configuração do sistema de descoberta
type TenantDiscoveryConfig struct {
	Enabled               bool     `yaml:"enabled" json:"enabled"`
	UpdateInterval        string   `yaml:"update_interval" json:"update_interval"`
	ConfigPaths           []string `yaml:"config_paths" json:"config_paths"`
	AutoCreateTenants     bool     `yaml:"auto_create_tenants" json:"auto_create_tenants"`
	AutoUpdateTenants     bool     `yaml:"auto_update_tenants" json:"auto_update_tenants"`
	AutoDeleteTenants     bool     `yaml:"auto_delete_tenants" json:"auto_delete_tenants"`
	DefaultTenantTemplate string   `yaml:"default_tenant_template" json:"default_tenant_template"`
	FileFormats           []string `yaml:"file_formats" json:"file_formats"` // ["yaml", "json"]
	ValidationEnabled     bool     `yaml:"validation_enabled" json:"validation_enabled"`
}

// TenantConfigFile representa um arquivo de configuração de tenant
type TenantConfigFile struct {
	Path         string
	LastModified time.Time
	TenantConfig *TenantConfig
}

// NewTenantDiscovery cria um novo sistema de descoberta de tenants
func NewTenantDiscovery(manager *TenantManager, config *TenantDiscoveryConfig) (*TenantDiscovery, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %v", err)
	}

	return &TenantDiscovery{
		manager:         manager,
		discoveryConfig: config,
		watcher:         watcher,
		running:         false,
	}, nil
}

// Start inicia o sistema de descoberta
func (td *TenantDiscovery) Start(ctx context.Context) error {
	if !td.discoveryConfig.Enabled {
		return nil
	}

	td.mu.Lock()
	defer td.mu.Unlock()

	if td.running {
		return fmt.Errorf("tenant discovery already running")
	}

	// Criar contexto cancelável
	ctx, cancel := context.WithCancel(ctx)
	td.cancel = cancel

	// Descoberta inicial
	if err := td.performInitialDiscovery(); err != nil {
		return fmt.Errorf("initial discovery failed: %v", err)
	}

	// Configurar watchers para os diretórios
	if err := td.setupFileWatchers(); err != nil {
		return fmt.Errorf("failed to setup file watchers: %v", err)
	}

	// Iniciar goroutines de monitoramento
	go td.watchFileChanges(ctx)
	go td.periodicDiscovery(ctx)

	td.running = true
	return nil
}

// Stop para o sistema de descoberta
func (td *TenantDiscovery) Stop() error {
	td.mu.Lock()
	defer td.mu.Unlock()

	if !td.running {
		return nil
	}

	if td.cancel != nil {
		td.cancel()
	}

	if td.watcher != nil {
		td.watcher.Close()
	}

	td.running = false
	return nil
}

// performInitialDiscovery executa descoberta inicial de tenants
func (td *TenantDiscovery) performInitialDiscovery() error {
	for _, configPath := range td.discoveryConfig.ConfigPaths {
		if err := td.discoverTenantsInPath(configPath); err != nil {
			return fmt.Errorf("failed to discover tenants in %s: %v", configPath, err)
		}
	}
	return nil
}

// discoverTenantsInPath descobre tenants em um caminho específico
func (td *TenantDiscovery) discoverTenantsInPath(configPath string) error {
	// Buscar arquivos de configuração
	files, err := td.findConfigFiles(configPath)
	if err != nil {
		return err
	}

	// Processar cada arquivo encontrado
	for _, file := range files {
		if err := td.processConfigFile(file); err != nil {
			// Log erro mas continue processando outros arquivos
			fmt.Printf("Error processing config file %s: %v\n", file, err)
		}
	}

	return nil
}

// findConfigFiles encontra arquivos de configuração de tenant
func (td *TenantDiscovery) findConfigFiles(basePath string) ([]string, error) {
	var files []string

	for _, format := range td.discoveryConfig.FileFormats {
		pattern := filepath.Join(basePath, fmt.Sprintf("*.%s", format))
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)

		// Buscar em subdiretórios
		subPattern := filepath.Join(basePath, "**", fmt.Sprintf("*.%s", format))
		subMatches, err := filepath.Glob(subPattern)
		if err == nil {
			files = append(files, subMatches...)
		}
	}

	return files, nil
}

// processConfigFile processa um arquivo de configuração de tenant
func (td *TenantDiscovery) processConfigFile(filePath string) error {
	// Ler arquivo
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// Determinar formato do arquivo
	ext := filepath.Ext(filePath)
	var config TenantConfig

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse YAML: %v", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse JSON: %v", err)
		}
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}

	// Validar configuração se habilitado
	if td.discoveryConfig.ValidationEnabled {
		if err := td.validateTenantConfig(&config); err != nil {
			return fmt.Errorf("config validation failed: %v", err)
		}
	}

	// Verificar se tenant já existe
	existingTenant, err := td.manager.GetTenant(config.ID)
	if err == nil {
		// Tenant existe - atualizar se auto-update estiver habilitado
		if td.discoveryConfig.AutoUpdateTenants {
			if err := td.manager.UpdateTenant(config.ID, &config); err != nil {
				return fmt.Errorf("failed to update tenant: %v", err)
			}
		}
	} else {
		// Tenant não existe - criar se auto-create estiver habilitado
		if td.discoveryConfig.AutoCreateTenants {
			if err := td.manager.CreateTenant(&config); err != nil {
				return fmt.Errorf("failed to create tenant: %v", err)
			}
		}
	}

	return nil
}

// validateTenantConfig valida configuração de tenant descoberta
func (td *TenantDiscovery) validateTenantConfig(config *TenantConfig) error {
	if config.ID == "" {
		return fmt.Errorf("tenant ID is required")
	}

	if config.Name == "" {
		return fmt.Errorf("tenant name is required")
	}

	// Validações adicionais específicas do discovery
	if config.ResourceLimits.MaxMemoryMB < 0 {
		return fmt.Errorf("invalid memory limit")
	}

	return nil
}

// setupFileWatchers configura watchers para os diretórios de configuração
func (td *TenantDiscovery) setupFileWatchers() error {
	for _, configPath := range td.discoveryConfig.ConfigPaths {
		if err := td.watcher.Add(configPath); err != nil {
			return fmt.Errorf("failed to watch path %s: %v", configPath, err)
		}
	}
	return nil
}

// watchFileChanges monitora mudanças nos arquivos de configuração
func (td *TenantDiscovery) watchFileChanges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-td.watcher.Events:
			if !ok {
				return
			}

			// Processar eventos de arquivo
			if td.shouldProcessEvent(event) {
				go td.handleFileEvent(event)
			}

		case err, ok := <-td.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("File watcher error: %v\n", err)
		}
	}
}

// shouldProcessEvent determina se um evento de arquivo deve ser processado
func (td *TenantDiscovery) shouldProcessEvent(event fsnotify.Event) bool {
	// Verificar se é um dos formatos suportados
	ext := filepath.Ext(event.Name)
	for _, format := range td.discoveryConfig.FileFormats {
		if ext == "."+format {
			return true
		}
	}
	return false
}

// handleFileEvent processa eventos de mudança em arquivos
func (td *TenantDiscovery) handleFileEvent(event fsnotify.Event) {
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		// Arquivo criado
		if err := td.processConfigFile(event.Name); err != nil {
			fmt.Printf("Error processing new config file %s: %v\n", event.Name, err)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		// Arquivo modificado
		if err := td.processConfigFile(event.Name); err != nil {
			fmt.Printf("Error processing modified config file %s: %v\n", event.Name, err)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		// Arquivo removido
		if td.discoveryConfig.AutoDeleteTenants {
			// Tentar determinar ID do tenant pelo nome do arquivo
			tenantID := td.extractTenantIDFromPath(event.Name)
			if tenantID != "" {
				if err := td.manager.DeleteTenant(tenantID); err != nil {
					fmt.Printf("Error deleting tenant %s: %v\n", tenantID, err)
				}
			}
		}
	}
}

// extractTenantIDFromPath extrai ID do tenant do caminho do arquivo
func (td *TenantDiscovery) extractTenantIDFromPath(filePath string) string {
	// Implementação simples - usar nome do arquivo sem extensão como ID
	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	return filename[:len(filename)-len(ext)]
}

// periodicDiscovery executa descoberta periódica
func (td *TenantDiscovery) periodicDiscovery(ctx context.Context) {
	interval, err := time.ParseDuration(td.discoveryConfig.UpdateInterval)
	if err != nil {
		fmt.Printf("Invalid update interval: %v\n", err)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := td.performInitialDiscovery(); err != nil {
				fmt.Printf("Periodic discovery error: %v\n", err)
			}
		}
	}
}

// GetDiscoveryStatus retorna status do sistema de descoberta
func (td *TenantDiscovery) GetDiscoveryStatus() map[string]interface{} {
	td.mu.RLock()
	defer td.mu.RUnlock()

	return map[string]interface{}{
		"enabled":      td.discoveryConfig.Enabled,
		"running":      td.running,
		"config_paths": td.discoveryConfig.ConfigPaths,
		"auto_create":  td.discoveryConfig.AutoCreateTenants,
		"auto_update":  td.discoveryConfig.AutoUpdateTenants,
		"auto_delete":  td.discoveryConfig.AutoDeleteTenants,
		"file_formats": td.discoveryConfig.FileFormats,
	}
}

// CreateTenantTemplate cria um template de configuração de tenant
func (td *TenantDiscovery) CreateTenantTemplate(tenantID, outputPath string) error {
	template := &TenantConfig{
		ID:          tenantID,
		Name:        fmt.Sprintf("Tenant %s", tenantID),
		Description: fmt.Sprintf("Auto-generated tenant configuration for %s", tenantID),
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Dispatcher: TenantDispatcherConfig{
			QueueSize:    10000,
			WorkerCount:  2,
			BatchSize:    100,
			BatchTimeout: "5s",
			MaxRetries:   3,
		},
		Processing: TenantProcessingConfig{
			Enabled:     true,
			WorkerCount: 1,
			QueueSize:   1000,
		},
		Monitoring: TenantMonitoringConfig{
			MetricsEnabled: true,
			MetricsPrefix:  fmt.Sprintf("tenant_%s", tenantID),
			HealthChecks:   true,
		},
		ResourceLimits: TenantResourceLimitsConfig{
			MaxMemoryMB:     512,
			MaxCPUPercent:   50.0,
			MaxDiskMB:       1024,
			MaxConnections:  100,
			MaxEventsPerSec: 1000,
		},
		Security: TenantSecurityConfig{
			IsolationLevel:        "soft",
			EncryptionEnabled:     false,
			AuthenticationRequired: false,
			APIKeyRequired:        false,
		},
		Labels: map[string]string{
			"environment": "production",
			"version":     "v1",
		},
		Annotations: map[string]string{
			"created_by": "tenant-discovery",
		},
	}

	// Serializar para YAML
	data, err := yaml.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %v", err)
	}

	// Escrever arquivo
	if err := ioutil.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write template file: %v", err)
	}

	return nil
}

// LoadTenantsFromDirectory carrega todos os tenants de um diretório
func (td *TenantDiscovery) LoadTenantsFromDirectory(dirPath string) ([]TenantConfig, error) {
	files, err := td.findConfigFiles(dirPath)
	if err != nil {
		return nil, err
	}

	var configs []TenantConfig
	for _, file := range files {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			continue
		}

		var config TenantConfig
		ext := filepath.Ext(file)

		switch ext {
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(data, &config); err != nil {
				continue
			}
		case ".json":
			if err := json.Unmarshal(data, &config); err != nil {
				continue
			}
		default:
			continue
		}

		configs = append(configs, config)
	}

	return configs, nil
}