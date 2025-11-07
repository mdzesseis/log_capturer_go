package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ssw-logs-capture/pkg/errors"
	"ssw-logs-capture/pkg/types"

	"gopkg.in/yaml.v2"
)

// LoadConfig carrega a configuração a partir de arquivo YAML e variáveis de ambiente
func LoadConfig(configFile string) (*types.Config, error) {
	// Inicializar configuração com valores padrão
	config := &types.Config{}

	// Se um arquivo de configuração foi especificado, carregá-lo primeiro
	if configFile != "" {
		if err := loadConfigFile(configFile, config); err != nil {
			fmt.Printf("Warning: Failed to load config file %s: %v\n", configFile, err)
		} else {
			fmt.Printf("Loaded configuration from file: %s\n", configFile)
		}
	}

	// Aplicar valores padrão e sobrescrever com variáveis de ambiente
	applyDefaults(config)
	applyEnvironmentOverrides(config)

	// Debug: Print server config
	fmt.Printf("DEBUG: Server config - Enabled: %v, Host: %s, Port: %d\n",
		config.Server.Enabled, config.Server.Host, config.Server.Port)

	// Carregar file pipeline se configurado
	if err := loadFilePipeline(config); err != nil {
		fmt.Printf("Warning: Failed to load file pipeline: %v\n", err)
	}

	// C12: Configuration Validation - Validate all config before starting
	if err := ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	fmt.Println("✓ Configuration validation passed")
	return config, nil
}

// loadFilePipeline carrega o arquivo file_pipeline.yml se configurado
func loadFilePipeline(config *types.Config) error {
	// Determinar caminho do pipeline file
	pipelineFile := config.FileMonitorService.PipelineFile
	if pipelineFile == "" {
		// Tentar variável de ambiente
		pipelineFile = getEnvString("SSW_FILE_CONFIG", "")
	}

	if pipelineFile == "" {
		fmt.Println("No file pipeline configured, using defaults from files_config")
		return nil
	}

	// Verificar se arquivo existe
	if _, err := os.Stat(pipelineFile); os.IsNotExist(err) {
		return fmt.Errorf("pipeline file not found: %s", pipelineFile)
	}

	// Carregar arquivo
	data, err := os.ReadFile(pipelineFile)
	if err != nil {
		return fmt.Errorf("failed to read pipeline file: %w", err)
	}

	// Parse the YAML into a generic map instead of FilePipelineConfig
	// This allows us to pass the raw configuration to the FileMonitor
	var pipelineMap map[string]interface{}
	if err := yaml.Unmarshal(data, &pipelineMap); err != nil {
		return fmt.Errorf("failed to parse pipeline file: %w", err)
	}

	// Store the pipeline configuration in FileMonitorService
	config.FileMonitorService.PipelineConfig = pipelineMap

	fmt.Printf("Loaded file pipeline from: %s\n", pipelineFile)

	return nil
}

// shouldApplyDefaults determina se devemos aplicar valores default
// Retorna true se default_configs está habilitado (true ou não especificado)
func shouldApplyDefaults(config *types.Config) bool {
	// Primeiro verifica variável de ambiente
	if envValue := os.Getenv("SSW_DEFAULT_CONFIGS"); envValue != "" {
		if enabled, err := strconv.ParseBool(envValue); err == nil {
			return enabled
		}
	}

	// Se DefaultConfigs for nil (não especificado no YAML), retorna true (comportamento padrão)
	if config.App.DefaultConfigs == nil {
		return true
	}

	// Caso contrário, usa o valor especificado no YAML
	return *config.App.DefaultConfigs
}

// applyDefaults aplica valores padrão à configuração
func applyDefaults(config *types.Config) {
	// Determina se devemos aplicar defaults
	applyDefaultValues := shouldApplyDefaults(config)

	if !applyDefaultValues {
		fmt.Println("⚙ Default configurations disabled - using only explicitly configured values")
		return
	}

	fmt.Println("⚙ Default configurations enabled - applying defaults for missing values")

	// App defaults
	if config.App.Name == "" {
		config.App.Name = "ssw-logs-capture"
	}
	if config.App.Version == "" {
		config.App.Version = "v0.0.2"
	}
	if config.App.Environment == "" {
		config.App.Environment = "production"
	}
	if config.App.LogLevel == "" {
		config.App.LogLevel = "info"
	}
	if config.App.LogFormat == "" {
		config.App.LogFormat = "json"
	}

	// Server defaults
	if config.Server.Port == 0 {
		config.Server.Port = 8401
	}
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}

	// Metrics defaults
	if config.Metrics.Port == 0 {
		config.Metrics.Port = 8001
	}
	if config.Metrics.Path == "" {
		config.Metrics.Path = "/metrics"
	}
	config.Metrics.Enabled = true // Default enabled

	// Files Config defaults (padrões de arquivos)
	// IMPORTANTE: Apenas aplica defaults se WatchDirectories e IncludePatterns forem nil
	// Se forem slices vazios (não nil), significa que foram definidos explicitamente como vazios
	// e NÃO devemos aplicar defaults (usuário não quer monitorar nada)
	if config.FilesConfig.WatchDirectories == nil {
		config.FilesConfig.WatchDirectories = []string{"/var/log"}
	}
	if config.FilesConfig.IncludePatterns == nil {
		config.FilesConfig.IncludePatterns = []string{"*.log", "*.txt"}
	}

	// Exclude patterns sempre tem defaults se não especificados (segurança)
	// Mesmo que o usuário não queira monitorar nada, é seguro ter exclusões padrão
	if len(config.FilesConfig.ExcludePatterns) == 0 {
		config.FilesConfig.ExcludePatterns = []string{"*.gz", "*.zip"}
	}
	if len(config.FilesConfig.ExcludeDirectories) == 0 {
		config.FilesConfig.ExcludeDirectories = []string{"/var/log/monitoring_data_suite", "/app/logs"}
	}

	// File Monitor Service defaults
	if config.FileMonitorService.PollInterval == "" {
		config.FileMonitorService.PollInterval = "1s"
	}
	if config.FileMonitorService.ReadBufferSize == 0 {
		config.FileMonitorService.ReadBufferSize = 65536
	}
	if config.FileMonitorService.ReadInterval == "" {
		config.FileMonitorService.ReadInterval = "100ms"
	}
	config.FileMonitorService.Recursive = true // Default enabled

	// Legacy File Monitor defaults (compatibilidade)
	if len(config.FileMonitor.WatchDirectories) == 0 {
		config.FileMonitor.WatchDirectories = config.FilesConfig.WatchDirectories
	}
	if len(config.FileMonitor.IncludePatterns) == 0 {
		config.FileMonitor.IncludePatterns = config.FilesConfig.IncludePatterns
	}
	if config.FileMonitor.PollInterval == "" {
		config.FileMonitor.PollInterval = config.FileMonitorService.PollInterval
	}

	// Task 2: File Monitor new features defaults
	// Default: Don't ignore old timestamps (backward compatible)
	// config.FileMonitorService.IgnoreOldTimestamps defaults to false

	// Default seek strategy: beginning (backward compatible)
	if config.FileMonitorService.SeekStrategy == "" {
		config.FileMonitorService.SeekStrategy = "beginning"
	}
	// Default seek recent bytes: 1MB
	if config.FileMonitorService.SeekRecentBytes == 0 {
		config.FileMonitorService.SeekRecentBytes = 1048576 // 1MB
	}
	// Default max retry queue size: 50
	if config.FileMonitorService.MaxRetryQueueSize == 0 {
		config.FileMonitorService.MaxRetryQueueSize = 50
	}
	// Retry config defaults
	if config.FileMonitorService.RetryConfig.InitialDelay == 0 {
		config.FileMonitorService.RetryConfig.InitialDelay = 1 * time.Second
	}
	if config.FileMonitorService.RetryConfig.MaxDelay == 0 {
		config.FileMonitorService.RetryConfig.MaxDelay = 60 * time.Second
	}
	if config.FileMonitorService.RetryConfig.Multiplier == 0 {
		config.FileMonitorService.RetryConfig.Multiplier = 2.0
	}
	if config.FileMonitorService.RetryConfig.DropPolicy == "" {
		config.FileMonitorService.RetryConfig.DropPolicy = "oldest"
	}

	// Apply to legacy FileMonitor config as well for backward compatibility
	config.FileMonitor.IgnoreOldTimestamps = config.FileMonitorService.IgnoreOldTimestamps
	config.FileMonitor.SeekStrategy = config.FileMonitorService.SeekStrategy
	config.FileMonitor.SeekRecentBytes = config.FileMonitorService.SeekRecentBytes
	config.FileMonitor.MaxRetryQueueSize = config.FileMonitorService.MaxRetryQueueSize
	config.FileMonitor.RetryConfig = config.FileMonitorService.RetryConfig

	// Container Monitor defaults
	if config.ContainerMonitor.SocketPath == "" {
		config.ContainerMonitor.SocketPath = "unix:///var/run/docker.sock"
	}
	if config.ContainerMonitor.HealthCheckDelay == "" {
		config.ContainerMonitor.HealthCheckDelay = "30s"
	}
	if config.ContainerMonitor.ReconnectInterval == "" {
		config.ContainerMonitor.ReconnectInterval = "30s"
	}
	if config.ContainerMonitor.MaxConcurrent == 0 {
		config.ContainerMonitor.MaxConcurrent = 50
	}
	config.ContainerMonitor.Enabled = true
	config.ContainerMonitor.IncludeStdout = true
	config.ContainerMonitor.IncludeStderr = true
	config.ContainerMonitor.Follow = true

	// Dispatcher defaults
	if config.Dispatcher.QueueSize == 0 {
		config.Dispatcher.QueueSize = 10000
	}
	if config.Dispatcher.WorkerCount == 0 {
		config.Dispatcher.WorkerCount = 4
	}
	if config.Dispatcher.BatchSize == 0 {
		config.Dispatcher.BatchSize = 100
	}
	if config.Dispatcher.BatchTimeout == "" {
		config.Dispatcher.BatchTimeout = "5s"
	}

	// Sinks defaults
	if config.Sinks.Loki.URL == "" {
		config.Sinks.Loki.URL = "http://loki:3100"
	}
	// Temporarily disabled due to missing field
	// if config.Sinks.Loki.PushEndpoint == "" {
	//	config.Sinks.Loki.PushEndpoint = "/loki/api/v1/push"
	// }
	if config.Sinks.Loki.BatchSize == 0 {
		config.Sinks.Loki.BatchSize = 1000
	}
	// Temporarily disabled due to missing field
	// if config.Sinks.Loki.BatchTimeout == "" {
	//	config.Sinks.Loki.BatchTimeout = "10s"
	// }
	if config.Sinks.Loki.Timeout == "" {
		config.Sinks.Loki.Timeout = "30s"
	}
	config.Sinks.Loki.Enabled = true

	if config.Sinks.LocalFile.Directory == "" {
		config.Sinks.LocalFile.Directory = "/app/logs/output"
	}
	// Temporarily disabled due to missing fields
	// if config.Sinks.LocalFile.FilenamePattern == "" {
	//	config.Sinks.LocalFile.FilenamePattern = "logs-{date}-{hour}.log"
	// }
	// if config.Sinks.LocalFile.OutputFormat == "" {
	//	config.Sinks.LocalFile.OutputFormat = "text"
	// }
	config.Sinks.LocalFile.Enabled = true

	// Processing defaults
	if config.Processing.PipelinesFile == "" {
		config.Processing.PipelinesFile = "pipelines.yaml"
	}
	config.Processing.Enabled = true

	// Positions defaults
	if config.Positions.Directory == "" {
		config.Positions.Directory = "/app/data/positions"
	}
	if config.Positions.FlushInterval == "" {
		config.Positions.FlushInterval = "30s"
	}
	if config.Positions.MaxMemoryBuffer == 0 {
		config.Positions.MaxMemoryBuffer = 1000
	}
	config.Positions.ForceFlushOnExit = true
	if config.Positions.CleanupInterval == "" {
		config.Positions.CleanupInterval = "5m"
	}
	if config.Positions.MaxPositionAge == "" {
		config.Positions.MaxPositionAge = "24h"
	}
	config.Positions.Enabled = true



	// File Monitor overrides
	if enabled := getEnvBool("FILE_MONITOR_ENABLED", config.FileMonitor.Enabled); enabled != config.FileMonitor.Enabled {
		config.FileMonitor.Enabled = enabled
		config.File.Enabled = enabled
	}

	// Sinks overrides
	if enabled := getEnvBool("LOKI_SINK_ENABLED", config.Sinks.Loki.Enabled); enabled != config.Sinks.Loki.Enabled {
		config.Sinks.Loki.Enabled = enabled
	}
	if url := getEnvString("LOKI_URL", ""); url != "" {
		config.Sinks.Loki.URL = url
	}
	if batchSize := getEnvInt("LOKI_BATCH_SIZE", 0); batchSize != 0 {
		config.Sinks.Loki.BatchSize = batchSize
	}

	if enabled := getEnvBool("LOCALFILE_SINK_ENABLED", config.Sinks.LocalFile.Enabled); enabled != config.Sinks.LocalFile.Enabled {
		config.Sinks.LocalFile.Enabled = enabled
	}
	if dir := getEnvString("LOCALFILE_DIRECTORY", ""); dir != "" {
		config.Sinks.LocalFile.Directory = dir
	}

	// Processing overrides
	if enabled := getEnvBool("PROCESSING_ENABLED", config.Processing.Enabled); enabled != config.Processing.Enabled {
		config.Processing.Enabled = enabled
		config.Pipeline.Enabled = enabled
	}

	// Positions overrides
	if enabled := getEnvBool("POSITIONS_ENABLED", config.Positions.Enabled); enabled != config.Positions.Enabled {
		config.Positions.Enabled = enabled
	}
	if dir := getEnvString("POSITIONS_DIRECTORY", ""); dir != "" {
		config.Positions.Directory = dir
	}
	if interval := getEnvString("POSITIONS_FLUSH_INTERVAL", ""); interval != "" {
		config.Positions.FlushInterval = interval
	}
	if maxBuffer := getEnvInt("POSITIONS_MAX_MEMORY_BUFFER", 0); maxBuffer != 0 {
		config.Positions.MaxMemoryBuffer = maxBuffer
	}

	// Pipeline file path overrides
	if pipelines := getEnvString("SSW_PIPELINES_FILE", ""); pipelines != "" {
		config.Processing.PipelinesFile = pipelines
		// Construct full path for pipeline config - avoid duplicate prefix
		if strings.HasPrefix(pipelines, "/") {
			config.Pipeline.File = pipelines
		} else {
			config.Pipeline.File = "/app/configs/" + pipelines
		}
	}

	// File pipeline override
	if fileConfig := getEnvString("SSW_FILE_CONFIG", ""); fileConfig != "" {
		config.FileMonitorService.PipelineFile = fileConfig
	}

	// Logging overrides
	if level := getEnvString("LOG_LEVEL", ""); level != "" {
		config.App.LogLevel = level
		config.Logging.Level = level
	}
	if format := getEnvString("LOG_FORMAT", ""); format != "" {
		config.App.LogFormat = format
		config.Logging.Format = format
	}
}

// loadConfigFile carrega configuração de um arquivo YAML
func loadConfigFile(filename string, config *types.Config) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Marcar que a configuração foi carregada de arquivo
	// Isso permite distinguir entre "não configurado" e "configurado como vazio"
	markConfigAsLoaded(config, data)

	return nil
}

// markConfigAsLoaded analisa o YAML raw para marcar quais seções foram explicitamente definidas
func markConfigAsLoaded(config *types.Config, yamlData []byte) {
	yamlStr := string(yamlData)

	// Verifica se files_config está presente no YAML (mesmo que comentado ou vazio)
	// Se estiver presente, significa que o usuário quis configurá-lo explicitamente
	if strings.Contains(yamlStr, "files_config:") {
		// Marca internamente que files_config foi definido
		// Usamos um marcador especial: se watch_directories estiver nil mas files_config existir,
		// inicializamos como slice vazio para indicar "definido mas vazio"
		if config.FilesConfig.WatchDirectories == nil {
			config.FilesConfig.WatchDirectories = []string{}
		}
		if config.FilesConfig.IncludePatterns == nil {
			config.FilesConfig.IncludePatterns = []string{}
		}
		// Nota: ExcludePatterns e ExcludeDirectories sempre terão defaults por segurança
	}
}

// Funções auxiliares para variáveis de ambiente

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}

func getEnvStringMap(key string, defaultValue map[string]string) map[string]string {
	if value := os.Getenv(key); value != "" {
		result := make(map[string]string)
		pairs := strings.Split(value, ",")
		for _, pair := range pairs {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				result[kv[0]] = kv[1]
			} else if len(kv) == 1 {
				// Allow key without value (empty string value)
				result[kv[0]] = ""
			}
		}
		return result
	}
	return defaultValue
}

// ValidateConfig performs comprehensive configuration validation
func ValidateConfig(config *types.Config) error {
	validator := &ConfigValidator{config: config}
	return validator.Validate()
}

// ConfigValidator provides comprehensive configuration validation
type ConfigValidator struct {
	config *types.Config
	errors []error
}

// Validate performs comprehensive validation
func (v *ConfigValidator) Validate() error {
	// App validation
	v.validateApp()

	// Server validation
	v.validateServer()

	// Metrics validation
	v.validateMetrics()

	// Monitoring validation
	v.validateMonitoring()

	// Sinks validation
	v.validateSinks()

	// Processing validation
	v.validateProcessing()

	// Resource validation
	v.validateResources()

	// Security validation
	v.validateSecurity()

	if len(v.errors) > 0 {
		return v.buildValidationError()
	}

	return nil
}

func (v *ConfigValidator) addError(component, operation, message string) {
	err := errors.ConfigError(operation, message).WithMetadata("component", component)
	v.errors = append(v.errors, err)
}

func (v *ConfigValidator) validateApp() {
	if v.config.App.Name == "" {
		v.addError("app", "validate_name", "application name cannot be empty")
	}

	if v.config.App.Version == "" {
		v.addError("app", "validate_version", "application version cannot be empty")
	}

	validLogLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLogLevels[v.config.App.LogLevel] {
		v.addError("app", "validate_log_level", fmt.Sprintf("invalid log level: %s", v.config.App.LogLevel))
	}

	validLogFormats := map[string]bool{"json": true, "text": true}
	if !validLogFormats[v.config.App.LogFormat] {
		v.addError("app", "validate_log_format", fmt.Sprintf("invalid log format: %s", v.config.App.LogFormat))
	}
}

func (v *ConfigValidator) validateServer() {
	if v.config.Server.Enabled {
		if v.config.Server.Port <= 0 || v.config.Server.Port > 65535 {
			v.addError("server", "validate_port", fmt.Sprintf("invalid server port: %d", v.config.Server.Port))
		}

		if v.config.Server.Host == "" {
			v.addError("server", "validate_host", "server host cannot be empty when enabled")
		}

		// Validate timeouts
		if v.config.Server.ReadTimeout != "" {
			if _, err := time.ParseDuration(v.config.Server.ReadTimeout); err != nil {
				v.addError("server", "validate_read_timeout", fmt.Sprintf("invalid read timeout: %s", v.config.Server.ReadTimeout))
			}
		}

		if v.config.Server.WriteTimeout != "" {
			if _, err := time.ParseDuration(v.config.Server.WriteTimeout); err != nil {
				v.addError("server", "validate_write_timeout", fmt.Sprintf("invalid write timeout: %s", v.config.Server.WriteTimeout))
			}
		}
	}
}

func (v *ConfigValidator) validateMetrics() {
	if v.config.Metrics.Enabled {
		if v.config.Metrics.Port <= 0 || v.config.Metrics.Port > 65535 {
			v.addError("metrics", "validate_port", fmt.Sprintf("invalid metrics port: %d", v.config.Metrics.Port))
		}

		if v.config.Metrics.Path == "" {
			v.addError("metrics", "validate_path", "metrics path cannot be empty when enabled")
		}

		// Check for port conflicts
		if v.config.Server.Enabled && v.config.Server.Port == v.config.Metrics.Port {
			v.addError("metrics", "validate_port_conflict", "metrics port conflicts with server port")
		}
	}
}

func (v *ConfigValidator) validateMonitoring() {
	// Container monitoring validation
	if v.config.ContainerMonitor.Enabled {
		if v.config.ContainerMonitor.SocketPath == "" {
			v.addError("container_monitor", "validate_socket", "docker socket path cannot be empty when enabled")
		}

		if v.config.ContainerMonitor.MaxConcurrent <= 0 {
			v.addError("container_monitor", "validate_max_concurrent", "max concurrent must be positive")
		}

		// Validate duration strings
		durations := map[string]string{
			"health_check_delay":  v.config.ContainerMonitor.HealthCheckDelay,
			"reconnect_interval":  v.config.ContainerMonitor.ReconnectInterval,
		}

		for field, duration := range durations {
			if duration != "" {
				if _, err := time.ParseDuration(duration); err != nil {
					v.addError("container_monitor", "validate_duration", fmt.Sprintf("invalid %s: %s", field, duration))
				}
			}
		}
	}

	// File monitoring validation
	if v.config.FileMonitorService.Enabled {
		if v.config.FileMonitorService.ReadBufferSize <= 0 {
			v.addError("file_monitor", "validate_buffer_size", "read buffer size must be positive")
		}

		// C12: Validate directory paths are absolute (existence checked at runtime)
		for _, dir := range v.config.FilesConfig.WatchDirectories {
			if dir != "" && !filepath.IsAbs(dir) {
				v.addError("file_monitor", "validate_watch_dir", fmt.Sprintf("watch directory must be absolute path: %s", dir))
			}
		}
	}
}

func (v *ConfigValidator) validateSinks() {
	enabledSinks := 0

	// Loki sink validation
	if v.config.Sinks.Loki.Enabled {
		enabledSinks++

		if v.config.Sinks.Loki.URL == "" {
			v.addError("loki_sink", "validate_url", "URL cannot be empty when enabled")
		} else {
			if _, err := url.Parse(v.config.Sinks.Loki.URL); err != nil {
				v.addError("loki_sink", "validate_url", fmt.Sprintf("invalid URL: %v", err))
			}
		}

		if v.config.Sinks.Loki.BatchSize <= 0 {
			v.addError("loki_sink", "validate_batch_size", "batch size must be positive")
		}

		if v.config.Sinks.Loki.BatchTimeout != "" {
			if _, err := time.ParseDuration(v.config.Sinks.Loki.BatchTimeout); err != nil {
				v.addError("loki_sink", "validate_batch_timeout", fmt.Sprintf("invalid batch timeout: %s", v.config.Sinks.Loki.BatchTimeout))
			}
		}
	}

	// Local file sink validation
	if v.config.Sinks.LocalFile.Enabled {
		enabledSinks++

		if v.config.Sinks.LocalFile.Directory == "" {
			v.addError("localfile_sink", "validate_directory", "directory cannot be empty when enabled")
		} else {
			// C12: Validate path is absolute (writability checked at runtime)
			if !filepath.IsAbs(v.config.Sinks.LocalFile.Directory) {
				v.addError("localfile_sink", "validate_directory", "directory must be absolute path")
			}
		}

		// C12: Allow empty format (will use default), but validate if specified
		if v.config.Sinks.LocalFile.OutputFormat != "" {
			validFormats := map[string]bool{"json": true, "text": true, "csv": true}
			if !validFormats[v.config.Sinks.LocalFile.OutputFormat] {
				v.addError("localfile_sink", "validate_format", fmt.Sprintf("invalid output format: %s", v.config.Sinks.LocalFile.OutputFormat))
			}
		}
	}

	// Elasticsearch sink validation
	if v.config.Sinks.Elasticsearch.Enabled {
		enabledSinks++

		if len(v.config.Sinks.Elasticsearch.URLs) == 0 {
			v.addError("elasticsearch_sink", "validate_urls", "URLs cannot be empty when enabled")
		} else {
			for i, elasticURL := range v.config.Sinks.Elasticsearch.URLs {
				if _, err := url.Parse(elasticURL); err != nil {
					v.addError("elasticsearch_sink", "validate_urls", fmt.Sprintf("invalid URL[%d]: %v", i, err))
				}
			}
		}
	}

	// Splunk sink validation
	if v.config.Sinks.Splunk.Enabled {
		enabledSinks++

		if v.config.Sinks.Splunk.URL == "" {
			v.addError("splunk_sink", "validate_url", "URL cannot be empty when enabled")
		}

		if v.config.Sinks.Splunk.Token == "" {
			v.addError("splunk_sink", "validate_token", "token cannot be empty when enabled")
		}
	}

	if enabledSinks == 0 {
		v.addError("sinks", "validate_enabled", "at least one sink must be enabled")
	}
}

func (v *ConfigValidator) validateProcessing() {
	if v.config.Processing.Enabled {
		if v.config.Processing.PipelinesFile == "" {
			v.addError("processing", "validate_pipelines_file", "pipelines file cannot be empty when enabled")
		}
		// C12: File existence validated at runtime (allows relative paths for flexibility)
	}
}

func (v *ConfigValidator) validateResources() {
	// Positions validation
	if v.config.Positions.Enabled {
		if v.config.Positions.Directory == "" {
			v.addError("positions", "validate_directory", "directory cannot be empty when enabled")
		}

		if v.config.Positions.MaxMemoryBuffer <= 0 {
			v.addError("positions", "validate_memory_buffer", "max memory buffer must be positive")
		}
	}

	// Dispatcher validation
	if v.config.Dispatcher.QueueSize <= 0 {
		v.addError("dispatcher", "validate_queue_size", "queue size must be positive")
	}

	if v.config.Dispatcher.WorkerCount <= 0 {
		v.addError("dispatcher", "validate_worker_count", "worker count must be positive")
	}

	if v.config.Dispatcher.BatchSize <= 0 {
		v.addError("dispatcher", "validate_batch_size", "batch size must be positive")
	}

	// Validate reasonable limits
	if v.config.Dispatcher.QueueSize > 1000000 {
		v.addError("dispatcher", "validate_queue_size", "queue size too large (max 1,000,000)")
	}

	if v.config.Dispatcher.WorkerCount > 100 {
		v.addError("dispatcher", "validate_worker_count", "worker count too large (max 100)")
	}
}

func (v *ConfigValidator) validateSecurity() {
	// Add security-specific validations here
	// This will be expanded in the security hardening phase
}

func (v *ConfigValidator) validateDirectoryAccess(dir string) error {
	if dir == "" {
		return fmt.Errorf("directory path is empty")
	}

	// Check if path is absolute for security
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("directory path must be absolute: %s", dir)
	}

	// Check if directory exists
	stat, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return fmt.Errorf("cannot access directory: %v", err)
	}

	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	// Check read permission
	file, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("directory not readable: %v", err)
	}
	file.Close()

	return nil
}

func (v *ConfigValidator) validateDirectoryWritable(dir string) error {
	// First check if it's accessible
	if err := v.validateDirectoryAccess(dir); err != nil {
		// If directory doesn't exist, check if parent exists and is writable
		if os.IsNotExist(err) {
			parent := filepath.Dir(dir)
			return v.validateDirectoryWritable(parent)
		}
		return err
	}

	// Test write permission
	testFile := filepath.Join(dir, ".write_test")
	file, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("directory not writable: %v", err)
	}
	file.Close()
	os.Remove(testFile)

	return nil
}

func (v *ConfigValidator) validateFileReadable(path string) error {
	if path == "" {
		return fmt.Errorf("file path is empty")
	}

	// Check if path is absolute for security
	if !filepath.IsAbs(path) {
		return fmt.Errorf("file path must be absolute: %s", path)
	}

	// Check if file exists and is readable
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("file not readable: %v", err)
	}
	file.Close()

	return nil
}

func (v *ConfigValidator) buildValidationError() error {
	if len(v.errors) == 1 {
		return v.errors[0]
	}

	// Create a compound error
	var messages []string
	for _, err := range v.errors {
		messages = append(messages, err.Error())
	}

	return errors.ConfigError("validate", fmt.Sprintf("multiple validation errors: %s", strings.Join(messages, "; ")))
}

// applyEnvironmentOverrides applies environment variable overrides to configuration
func applyEnvironmentOverrides(config *types.Config) {
	// App-level overrides
	config.App.Name = getEnvString("SSW_APP_NAME", config.App.Name)
	config.App.Version = getEnvString("SSW_APP_VERSION", config.App.Version)
	config.App.Environment = getEnvString("SSW_APP_ENVIRONMENT", config.App.Environment)
	config.App.LogLevel = getEnvString("SSW_LOG_LEVEL", config.App.LogLevel)
	config.App.LogFormat = getEnvString("SSW_LOG_FORMAT", config.App.LogFormat)

	// Server configuration overrides
	config.Server.Enabled = getEnvBool("SSW_SERVER_ENABLED", config.Server.Enabled)
	config.Server.Host = getEnvString("SSW_SERVER_HOST", config.Server.Host)
	config.Server.Port = getEnvInt("SSW_SERVER_PORT", config.Server.Port)

	// Metrics configuration overrides
	config.Metrics.Enabled = getEnvBool("SSW_METRICS_ENABLED", config.Metrics.Enabled)
	config.Metrics.Port = getEnvInt("SSW_METRICS_PORT", config.Metrics.Port)
	config.Metrics.Path = getEnvString("SSW_METRICS_PATH", config.Metrics.Path)
	config.Metrics.Namespace = getEnvString("SSW_METRICS_NAMESPACE", config.Metrics.Namespace)

	// Dispatcher configuration overrides
	config.Dispatcher.QueueSize = getEnvInt("SSW_DISPATCHER_QUEUE_SIZE", config.Dispatcher.QueueSize)
	config.Dispatcher.WorkerCount = getEnvInt("SSW_DISPATCHER_WORKER_COUNT", config.Dispatcher.WorkerCount)
	config.Dispatcher.BatchSize = getEnvInt("SSW_DISPATCHER_BATCH_SIZE", config.Dispatcher.BatchSize)
	config.Dispatcher.BatchTimeout = getEnvString("SSW_DISPATCHER_BATCH_TIMEOUT", config.Dispatcher.BatchTimeout)

	// Loki sink overrides
	config.Sinks.Loki.Enabled = getEnvBool("SSW_LOKI_ENABLED", config.Sinks.Loki.Enabled)
	config.Sinks.Loki.URL = getEnvString("SSW_LOKI_URL", config.Sinks.Loki.URL)
	config.Sinks.Loki.BatchSize = getEnvInt("SSW_LOKI_BATCH_SIZE", config.Sinks.Loki.BatchSize)
	config.Sinks.Loki.BatchTimeout = getEnvString("SSW_LOKI_BATCH_TIMEOUT", config.Sinks.Loki.BatchTimeout)
	config.Sinks.Loki.Timeout = getEnvString("SSW_LOKI_TIMEOUT", config.Sinks.Loki.Timeout)
	config.Sinks.Loki.TenantID = getEnvString("SSW_LOKI_TENANT_ID", config.Sinks.Loki.TenantID)
	config.Sinks.Loki.Compression = getEnvBool("SSW_LOKI_COMPRESSION", config.Sinks.Loki.Compression)

	// Local file sink overrides
	config.Sinks.LocalFile.Enabled = getEnvBool("SSW_LOCAL_FILE_ENABLED", config.Sinks.LocalFile.Enabled)
	config.Sinks.LocalFile.Directory = getEnvString("SSW_LOCAL_FILE_DIRECTORY", config.Sinks.LocalFile.Directory)
	config.Sinks.LocalFile.OutputFormat = getEnvString("SSW_LOCAL_FILE_FORMAT", config.Sinks.LocalFile.OutputFormat)

	// File monitor overrides
	config.FileMonitorService.Enabled = getEnvBool("SSW_FILE_MONITOR_ENABLED", config.FileMonitorService.Enabled)
	config.FileMonitorService.PipelineFile = getEnvString("SSW_FILE_PIPELINE", config.FileMonitorService.PipelineFile)

	// Container monitor overrides
	config.ContainerMonitor.Enabled = getEnvBool("SSW_CONTAINER_MONITOR_ENABLED", config.ContainerMonitor.Enabled)
	config.ContainerMonitor.SocketPath = getEnvString("SSW_DOCKER_SOCKET", config.ContainerMonitor.SocketPath)

	// Security overrides
	config.Security.Enabled = getEnvBool("SSW_SECURITY_ENABLED", config.Security.Enabled)

	// Service Discovery overrides
	config.ServiceDiscovery.Enabled = getEnvBool("SSW_SERVICE_DISCOVERY_ENABLED", config.ServiceDiscovery.Enabled)
	config.ServiceDiscovery.UpdateInterval = getEnvString("SSW_SERVICE_DISCOVERY_INTERVAL", config.ServiceDiscovery.UpdateInterval)
	config.ServiceDiscovery.DockerEnabled = getEnvBool("SSW_SERVICE_DISCOVERY_DOCKER", config.ServiceDiscovery.DockerEnabled)

	// Authentication overrides for Loki
	if lokiUser := getEnvString("SSW_LOKI_USER", ""); lokiUser != "" {
		config.Sinks.Loki.Auth.Type = "basic"
		config.Sinks.Loki.Auth.Username = lokiUser
		config.Sinks.Loki.Auth.Password = getEnvString("SSW_LOKI_PASSWORD", "")
	}
	if lokiToken := getEnvString("SSW_LOKI_TOKEN", ""); lokiToken != "" {
		config.Sinks.Loki.Auth.Type = "bearer"
		config.Sinks.Loki.Auth.Token = lokiToken
	}

	// Custom headers for Loki (format: KEY1=value1,KEY2=value2)
	if headers := getEnvString("SSW_LOKI_HEADERS", ""); headers != "" {
		if config.Sinks.Loki.Headers == nil {
			config.Sinks.Loki.Headers = make(map[string]string)
		}
		headerMap := getEnvStringMap("SSW_LOKI_HEADERS", nil)
		for k, v := range headerMap {
			config.Sinks.Loki.Headers[k] = v
		}
	}

	// Default labels for Loki (format: KEY1=value1,KEY2=value2)
	if labels := getEnvString("SSW_LOKI_LABELS", ""); labels != "" {
		if config.Sinks.Loki.DefaultLabels == nil {
			config.Sinks.Loki.DefaultLabels = make(map[string]string)
		}
		labelMap := getEnvStringMap("SSW_LOKI_LABELS", nil)
		for k, v := range labelMap {
			config.Sinks.Loki.DefaultLabels[k] = v
		}
	}
}