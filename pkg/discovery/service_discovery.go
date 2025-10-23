package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// ServiceDiscovery gerencia a descoberta automática de serviços
type ServiceDiscovery struct {
	config       Config
	logger       *logrus.Logger
	dockerClient *client.Client

	// Discovered services
	services     map[string]*DiscoveredService
	servicesMux  sync.RWMutex

	// Callbacks for changes
	onServiceAdded   func(*DiscoveredService)
	onServiceRemoved func(string)
	onServiceUpdated func(*DiscoveredService, *DiscoveredService)

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	stats Stats
}

// Config configuração do service discovery
type Config struct {
	Enabled         bool          `yaml:"enabled"`
	UpdateInterval  time.Duration `yaml:"update_interval"`
	DockerEnabled   bool          `yaml:"docker_enabled"`
	FileEnabled     bool          `yaml:"file_enabled"`

	// Docker discovery config
	DockerConfig DockerDiscoveryConfig `yaml:"docker"`

	// File discovery config
	FileConfig FileDiscoveryConfig `yaml:"file"`

	// Kubernetes discovery (future)
	KubernetesEnabled bool                      `yaml:"kubernetes_enabled"`
	KubernetesConfig  KubernetesDiscoveryConfig `yaml:"kubernetes"`
}

// DockerDiscoveryConfig configuração para descoberta Docker
type DockerDiscoveryConfig struct {
	SocketPath      string            `yaml:"socket_path"`
	RequiredLabels  map[string]string `yaml:"required_labels"`
	ExcludeLabels   map[string]string `yaml:"exclude_labels"`
	RequireLabel    string            `yaml:"require_label"`
	PipelineLabel   string            `yaml:"pipeline_label"`
	ComponentLabel  string            `yaml:"component_label"`
	TenantLabel     string            `yaml:"tenant_label"`
}

// FileDiscoveryConfig configuração para descoberta de arquivos
type FileDiscoveryConfig struct {
	WatchPaths      []string          `yaml:"watch_paths"`
	ConfigFiles     []string          `yaml:"config_files"`
	RequiredLabels  map[string]string `yaml:"required_labels"`
	AutoDetectLogs  bool              `yaml:"auto_detect_logs"`
}

// KubernetesDiscoveryConfig configuração para descoberta Kubernetes
type KubernetesDiscoveryConfig struct {
	Namespace           string            `yaml:"namespace"`
	RequiredAnnotations map[string]string `yaml:"required_annotations"`
	RequiredLabels      map[string]string `yaml:"required_labels"`
	ServiceAccount      string            `yaml:"service_account"`
}

// DiscoveredService representa um serviço descoberto
type DiscoveredService struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // docker, file, kubernetes
	Status      string                 `json:"status"` // active, inactive, pending
	Source      string                 `json:"source"`
	Labels      map[string]string      `json:"labels"`
	Metadata    map[string]interface{} `json:"metadata"`
	Pipeline    string                 `json:"pipeline"`
	Component   string                 `json:"component"`
	Tenant      string                 `json:"tenant"`
	LastSeen    time.Time              `json:"last_seen"`
	FirstSeen   time.Time              `json:"first_seen"`
	UpdateCount int64                  `json:"update_count"`
}

// Stats estatísticas do service discovery
type Stats struct {
	TotalServices     int64                    `json:"total_services"`
	ServicesByType    map[string]int64         `json:"services_by_type"`
	ServicesByStatus  map[string]int64         `json:"services_by_status"`
	DiscoveryRuns     int64                    `json:"discovery_runs"`
	LastDiscoveryTime time.Time                `json:"last_discovery_time"`
	ErrorCount        int64                    `json:"error_count"`
	LastError         string                   `json:"last_error,omitempty"`
}

// NewServiceDiscovery cria uma nova instância do service discovery
func NewServiceDiscovery(config Config, logger *logrus.Logger) (*ServiceDiscovery, error) {
	ctx, cancel := context.WithCancel(context.Background())

	sd := &ServiceDiscovery{
		config:   config,
		logger:   logger,
		services: make(map[string]*DiscoveredService),
		ctx:      ctx,
		cancel:   cancel,
		stats: Stats{
			ServicesByType:   make(map[string]int64),
			ServicesByStatus: make(map[string]int64),
		},
	}

	// Initialize Docker client if enabled
	if config.DockerEnabled {
		dockerClient, err := client.NewClientWithOpts(
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client: %w", err)
		}
		sd.dockerClient = dockerClient
	}

	return sd, nil
}

// SetCallbacks define callbacks para mudanças de serviços
func (sd *ServiceDiscovery) SetCallbacks(
	onAdded func(*DiscoveredService),
	onRemoved func(string),
	onUpdated func(*DiscoveredService, *DiscoveredService),
) {
	sd.onServiceAdded = onAdded
	sd.onServiceRemoved = onRemoved
	sd.onServiceUpdated = onUpdated
}

// Start inicia o service discovery
func (sd *ServiceDiscovery) Start() error {
	if !sd.config.Enabled {
		sd.logger.Info("Service discovery disabled")
		return nil
	}

	sd.logger.Info("Starting service discovery")

	// Initial discovery
	if err := sd.runDiscovery(); err != nil {
		sd.logger.WithError(err).Error("Initial discovery failed")
	}

	// Start periodic discovery
	sd.wg.Add(1)
	go sd.discoveryLoop()

	sd.logger.Info("Service discovery started")
	return nil
}

// Stop para o service discovery
func (sd *ServiceDiscovery) Stop() error {
	sd.logger.Info("Stopping service discovery")

	sd.cancel()
	sd.wg.Wait()

	if sd.dockerClient != nil {
		sd.dockerClient.Close()
	}

	sd.logger.Info("Service discovery stopped")
	return nil
}

// discoveryLoop executa o loop de descoberta periódica
func (sd *ServiceDiscovery) discoveryLoop() {
	defer sd.wg.Done()

	ticker := time.NewTicker(sd.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sd.ctx.Done():
			return
		case <-ticker.C:
			if err := sd.runDiscovery(); err != nil {
				sd.stats.ErrorCount++
				sd.stats.LastError = err.Error()
				sd.logger.WithError(err).Error("Discovery run failed")
			}
		}
	}
}

// runDiscovery executa uma rodada de descoberta
func (sd *ServiceDiscovery) runDiscovery() error {
	sd.logger.Debug("Running service discovery")

	sd.stats.DiscoveryRuns++
	sd.stats.LastDiscoveryTime = time.Now()

	var allErrors []error

	// Docker discovery
	if sd.config.DockerEnabled && sd.dockerClient != nil {
		if err := sd.discoverDockerServices(); err != nil {
			allErrors = append(allErrors, fmt.Errorf("docker discovery: %w", err))
		}
	}

	// File discovery
	if sd.config.FileEnabled {
		if err := sd.discoverFileServices(); err != nil {
			allErrors = append(allErrors, fmt.Errorf("file discovery: %w", err))
		}
	}

	// Update stats
	sd.updateStats()

	if len(allErrors) > 0 {
		return fmt.Errorf("discovery errors: %v", allErrors)
	}

	sd.logger.WithField("services_count", len(sd.services)).Debug("Discovery run completed")
	return nil
}

// discoverDockerServices descobre serviços Docker
func (sd *ServiceDiscovery) discoverDockerServices() error {
	containers, err := sd.dockerClient.ContainerList(sd.ctx, dockertypes.ContainerListOptions{
		All: true, // Include stopped containers
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	discoveredIDs := make(map[string]bool)

	for _, container := range containers {
		// Check if container should be discovered
		if !sd.shouldDiscoverContainer(container) {
			continue
		}

		service := sd.createServiceFromContainer(container)
		discoveredIDs[service.ID] = true

		sd.servicesMux.Lock()
		existing, exists := sd.services[service.ID]

		if exists {
			// Update existing service
			if sd.hasServiceChanged(existing, service) {
				old := *existing
				sd.services[service.ID] = service
				sd.servicesMux.Unlock()

				if sd.onServiceUpdated != nil {
					sd.onServiceUpdated(&old, service)
				}

				sd.logger.WithFields(logrus.Fields{
					"service_id":   service.ID,
					"service_name": service.Name,
				}).Debug("Service updated")
			} else {
				// Just update last seen
				existing.LastSeen = time.Now()
				existing.UpdateCount++
				sd.servicesMux.Unlock()
			}
		} else {
			// New service
			sd.services[service.ID] = service
			sd.servicesMux.Unlock()

			if sd.onServiceAdded != nil {
				sd.onServiceAdded(service)
			}

			sd.logger.WithFields(logrus.Fields{
				"service_id":   service.ID,
				"service_name": service.Name,
				"service_type": service.Type,
			}).Info("New service discovered")
		}
	}

	// Remove services that are no longer present
	sd.servicesMux.Lock()
	for serviceID, service := range sd.services {
		if service.Type == "docker" && !discoveredIDs[serviceID] {
			delete(sd.services, serviceID)
			sd.servicesMux.Unlock()

			if sd.onServiceRemoved != nil {
				sd.onServiceRemoved(serviceID)
			}

			sd.logger.WithFields(logrus.Fields{
				"service_id":   serviceID,
				"service_name": service.Name,
			}).Info("Service removed")

			sd.servicesMux.Lock()
		}
	}
	sd.servicesMux.Unlock()

	return nil
}

// shouldDiscoverContainer verifica se um container deve ser descoberto
func (sd *ServiceDiscovery) shouldDiscoverContainer(container dockertypes.Container) bool {
	labels := container.Labels

	// Check required label
	if sd.config.DockerConfig.RequireLabel != "" {
		if value, exists := labels[sd.config.DockerConfig.RequireLabel]; !exists || value != "true" {
			return false
		}
	}

	// Check required labels
	for key, expectedValue := range sd.config.DockerConfig.RequiredLabels {
		if value, exists := labels[key]; !exists || value != expectedValue {
			return false
		}
	}

	// Check exclude labels
	for key, excludeValue := range sd.config.DockerConfig.ExcludeLabels {
		if value, exists := labels[key]; exists && value == excludeValue {
			return false
		}
	}

	return true
}

// createServiceFromContainer cria um DiscoveredService a partir de um container Docker
func (sd *ServiceDiscovery) createServiceFromContainer(container dockertypes.Container) *DiscoveredService {
	labels := container.Labels
	now := time.Now()

	// Determine pipeline
	pipeline := "default"
	if pipelineLabel := sd.config.DockerConfig.PipelineLabel; pipelineLabel != "" {
		if value, exists := labels[pipelineLabel]; exists {
			pipeline = value
		}
	}

	// Determine component
	component := "application"
	if componentLabel := sd.config.DockerConfig.ComponentLabel; componentLabel != "" {
		if value, exists := labels[componentLabel]; exists {
			component = value
		}
	}

	// Determine tenant
	tenant := "default"
	if tenantLabel := sd.config.DockerConfig.TenantLabel; tenantLabel != "" {
		if value, exists := labels[tenantLabel]; exists {
			tenant = value
		}
	}

	// Determine status
	status := "inactive"
	if container.State == "running" {
		status = "active"
	}

	return &DiscoveredService{
		ID:        container.ID,
		Name:      container.Names[0][1:], // Remove leading slash
		Type:      "docker",
		Status:    status,
		Source:    container.Image,
		Labels:    labels,
		Pipeline:  pipeline,
		Component: component,
		Tenant:    tenant,
		Metadata: map[string]interface{}{
			"image":        container.Image,
			"image_id":     container.ImageID,
			"state":        container.State,
			"status":       container.Status,
			"ports":        container.Ports,
			"networks":     container.NetworkSettings,
			"created":      container.Created,
		},
		LastSeen:    now,
		FirstSeen:   now,
		UpdateCount: 1,
	}
}

// discoverFileServices descobre serviços baseados em arquivos
func (sd *ServiceDiscovery) discoverFileServices() error {
	// Implementation for file-based service discovery
	// This would scan for config files, log files with specific patterns, etc.

	for _, configFile := range sd.config.FileConfig.ConfigFiles {
		service, err := sd.parseServiceFromFile(configFile)
		if err != nil {
			sd.logger.WithError(err).WithField("file", configFile).Error("Failed to parse service from file")
			continue
		}

		if service == nil {
			continue
		}

		sd.servicesMux.Lock()
		existing, exists := sd.services[service.ID]

		if exists {
			existing.LastSeen = time.Now()
			existing.UpdateCount++
		} else {
			sd.services[service.ID] = service

			if sd.onServiceAdded != nil {
				sd.onServiceAdded(service)
			}

			sd.logger.WithFields(logrus.Fields{
				"service_id":   service.ID,
				"service_name": service.Name,
				"source_file":  configFile,
			}).Info("File-based service discovered")
		}
		sd.servicesMux.Unlock()
	}

	return nil
}

// parseServiceFromFile analisa um arquivo para extrair configuração de serviço
func (sd *ServiceDiscovery) parseServiceFromFile(filePath string) (*DiscoveredService, error) {
	// Simplified file parsing - in reality this would be more sophisticated
	// Could parse JSON/YAML files with service definitions

	// For now, return nil (no service found)
	return nil, nil
}

// hasServiceChanged verifica se um serviço mudou
func (sd *ServiceDiscovery) hasServiceChanged(old, new *DiscoveredService) bool {
	if old.Status != new.Status {
		return true
	}

	if old.Pipeline != new.Pipeline {
		return true
	}

	if old.Component != new.Component {
		return true
	}

	if old.Tenant != new.Tenant {
		return true
	}

	// Check if labels changed
	if len(old.Labels) != len(new.Labels) {
		return true
	}

	for key, value := range old.Labels {
		if newValue, exists := new.Labels[key]; !exists || newValue != value {
			return true
		}
	}

	return false
}

// updateStats atualiza as estatísticas
func (sd *ServiceDiscovery) updateStats() {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	// Reset counters
	sd.stats.TotalServices = int64(len(sd.services))
	sd.stats.ServicesByType = make(map[string]int64)
	sd.stats.ServicesByStatus = make(map[string]int64)

	// Count by type and status
	for _, service := range sd.services {
		sd.stats.ServicesByType[service.Type]++
		sd.stats.ServicesByStatus[service.Status]++
	}
}

// GetServices retorna todos os serviços descobertos
func (sd *ServiceDiscovery) GetServices() []*DiscoveredService {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	services := make([]*DiscoveredService, 0, len(sd.services))
	for _, service := range sd.services {
		services = append(services, service)
	}

	return services
}

// GetServiceByID retorna um serviço específico pelo ID
func (sd *ServiceDiscovery) GetServiceByID(id string) (*DiscoveredService, bool) {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	service, exists := sd.services[id]
	return service, exists
}

// GetServicesByType retorna serviços filtrados por tipo
func (sd *ServiceDiscovery) GetServicesByType(serviceType string) []*DiscoveredService {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	var services []*DiscoveredService
	for _, service := range sd.services {
		if service.Type == serviceType {
			services = append(services, service)
		}
	}

	return services
}

// GetServicesByTenant retorna serviços filtrados por tenant
func (sd *ServiceDiscovery) GetServicesByTenant(tenant string) []*DiscoveredService {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	var services []*DiscoveredService
	for _, service := range sd.services {
		if service.Tenant == tenant {
			services = append(services, service)
		}
	}

	return services
}

// GetStats retorna as estatísticas atuais
func (sd *ServiceDiscovery) GetStats() Stats {
	sd.servicesMux.RLock()
	defer sd.servicesMux.RUnlock()

	// Create a copy to avoid race conditions
	stats := sd.stats
	stats.ServicesByType = make(map[string]int64)
	stats.ServicesByStatus = make(map[string]int64)

	for k, v := range sd.stats.ServicesByType {
		stats.ServicesByType[k] = v
	}

	for k, v := range sd.stats.ServicesByStatus {
		stats.ServicesByStatus[k] = v
	}

	return stats
}

// ExportServices exporta todos os serviços em formato JSON
func (sd *ServiceDiscovery) ExportServices() ([]byte, error) {
	services := sd.GetServices()
	return json.Marshal(services)
}

// IsHealthy verifica se o service discovery está saudável
func (sd *ServiceDiscovery) IsHealthy() bool {
	if !sd.config.Enabled {
		return true
	}

	// Check if discovery is running recently
	if time.Since(sd.stats.LastDiscoveryTime) > sd.config.UpdateInterval*2 {
		return false
	}

	// Check if Docker client is accessible (if enabled)
	if sd.config.DockerEnabled && sd.dockerClient != nil {
		if _, err := sd.dockerClient.Ping(sd.ctx); err != nil {
			return false
		}
	}

	return true
}