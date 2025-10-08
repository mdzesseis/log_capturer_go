package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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

	// Carregar file pipeline se configurado
	if err := loadFilePipeline(config); err != nil {
		fmt.Printf("Warning: Failed to load file pipeline: %v\n", err)
	}

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

	var pipeline types.FilePipelineConfig
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		return fmt.Errorf("failed to parse pipeline file: %w", err)
	}

	// Armazenar configuração do pipeline no FileConfig
	config.File.PipelineConfig = &pipeline

	fmt.Printf("Loaded file pipeline from: %s (version: %s)\n", pipelineFile, pipeline.Version)
	fmt.Printf("Pipeline: %d files, %d directories configured\n", len(pipeline.Files), len(pipeline.Directories))

	return nil
}

// applyDefaults aplica valores padrão à configuração
func applyDefaults(config *types.Config) {
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
	if len(config.FilesConfig.WatchDirectories) == 0 {
		config.FilesConfig.WatchDirectories = []string{"/var/log"}
	}
	if len(config.FilesConfig.IncludePatterns) == 0 {
		config.FilesConfig.IncludePatterns = []string{"*.log", "*.txt"}
	}
	if len(config.FilesConfig.ExcludePatterns) == 0 {
		config.FilesConfig.ExcludePatterns = []string{"*.gz", "*.zip"}
	}
	if len(config.FilesConfig.ExcludeDirectories) == 0 {
		config.FilesConfig.ExcludeDirectories = []string{"/var/log/monitoring_data", "/app/logs"}
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
	if config.Sinks.Loki.PushEndpoint == "" {
		config.Sinks.Loki.PushEndpoint = "/loki/api/v1/push"
	}
	if config.Sinks.Loki.BatchSize == 0 {
		config.Sinks.Loki.BatchSize = 1000
	}
	if config.Sinks.Loki.BatchTimeout == "" {
		config.Sinks.Loki.BatchTimeout = "10s"
	}
	if config.Sinks.Loki.Timeout == "" {
		config.Sinks.Loki.Timeout = "30s"
	}
	config.Sinks.Loki.Enabled = true

	if config.Sinks.LocalFile.Directory == "" {
		config.Sinks.LocalFile.Directory = "/app/logs/output"
	}
	if config.Sinks.LocalFile.FilenamePattern == "" {
		config.Sinks.LocalFile.FilenamePattern = "logs-{date}-{hour}.log"
	}
	if config.Sinks.LocalFile.OutputFormat == "" {
		config.Sinks.LocalFile.OutputFormat = "text"
	}
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

	// Legacy compatibility - copy new config to old fields
	config.API.Port = config.Server.Port
	config.API.Host = config.Server.Host
	config.API.Enabled = true

	config.Docker.Enabled = config.ContainerMonitor.Enabled
	config.Docker.SocketPath = config.ContainerMonitor.SocketPath
	config.Docker.MaxConcurrent = config.ContainerMonitor.MaxConcurrent
	if d, err := time.ParseDuration(config.ContainerMonitor.ReconnectInterval); err == nil {
		config.Docker.ReconnectInterval = d
	}
	if d, err := time.ParseDuration(config.ContainerMonitor.HealthCheckDelay); err == nil {
		config.Docker.HealthCheckDelay = d
	}
	config.Docker.IncludeLabels = config.ContainerMonitor.IncludeLabels
	config.Docker.ExcludeLabels = config.ContainerMonitor.ExcludeLabels
	config.Docker.IncludeNames = config.ContainerMonitor.IncludeNames
	config.Docker.ExcludeNames = config.ContainerMonitor.ExcludeNames

	// Usar file_monitor_service como fonte principal
	config.File.Enabled = config.FileMonitorService.Enabled

	// Se não tem pipeline configurado, usar files_config como default
	config.File.WatchDirectories = config.FilesConfig.WatchDirectories
	config.File.IncludePatterns = config.FilesConfig.IncludePatterns
	config.File.ExcludePatterns = config.FilesConfig.ExcludePatterns
	config.File.ExcludeDirectories = config.FilesConfig.ExcludeDirectories

	// Configurações de serviço
	if d, err := time.ParseDuration(config.FileMonitorService.PollInterval); err == nil {
		config.File.PollInterval = d
	}
	if d, err := time.ParseDuration(config.FileMonitorService.ReadInterval); err == nil {
		config.File.ReadInterval = d
	}
	config.File.BufferSize = config.FileMonitorService.ReadBufferSize
	config.File.Recursive = config.FileMonitorService.Recursive
	config.File.FollowSymlinks = config.FileMonitorService.FollowSymlinks
	config.File.PositionsPath = "/app/data/positions"

	// Compatibilidade: se file_monitor legado estiver habilitado, usar ele
	if config.FileMonitor.Enabled {
		config.File.Enabled = config.FileMonitor.Enabled
		if len(config.FileMonitor.WatchDirectories) > 0 {
			config.File.WatchDirectories = config.FileMonitor.WatchDirectories
		}
		if len(config.FileMonitor.IncludePatterns) > 0 {
			config.File.IncludePatterns = config.FileMonitor.IncludePatterns
		}
		if len(config.FileMonitor.ExcludePatterns) > 0 {
			config.File.ExcludePatterns = config.FileMonitor.ExcludePatterns
		}
		if len(config.FileMonitor.ExcludeDirectories) > 0 {
			config.File.ExcludeDirectories = config.FileMonitor.ExcludeDirectories
		}
	}

	config.Logging.Level = config.App.LogLevel
	config.Logging.Format = config.App.LogFormat

	config.Pipeline.Enabled = config.Processing.Enabled
	// Fix path construction to avoid duplicate prefix
	if strings.HasPrefix(config.Processing.PipelinesFile, "/") {
		config.Pipeline.File = config.Processing.PipelinesFile
	} else {
		config.Pipeline.File = "/app/configs/" + config.Processing.PipelinesFile
	}
}

// applyEnvironmentOverrides aplica sobrescritas de variáveis de ambiente
func applyEnvironmentOverrides(config *types.Config) {
	// Server/API overrides
	if port := getEnvInt("API_PORT", 0); port != 0 {
		config.Server.Port = port
		config.API.Port = port
	}
	if host := getEnvString("API_HOST", ""); host != "" {
		config.Server.Host = host
		config.API.Host = host
	}
	if enabled := getEnvBool("API_ENABLED", config.API.Enabled); enabled != config.API.Enabled {
		config.API.Enabled = enabled
	}

	// Metrics overrides
	if port := getEnvInt("METRICS_PORT", 0); port != 0 {
		config.Metrics.Port = port
	}
	if path := getEnvString("METRICS_PATH", ""); path != "" {
		config.Metrics.Path = path
	}
	if enabled := getEnvBool("METRICS_ENABLED", config.Metrics.Enabled); enabled != config.Metrics.Enabled {
		config.Metrics.Enabled = enabled
	}

	// Container Monitor overrides
	if enabled := getEnvBool("CONTAINER_MONITOR_ENABLED", config.ContainerMonitor.Enabled); enabled != config.ContainerMonitor.Enabled {
		config.ContainerMonitor.Enabled = enabled
		config.Docker.Enabled = enabled
	}
	if socket := getEnvString("DOCKER_SOCKET_PATH", ""); socket != "" {
		config.ContainerMonitor.SocketPath = socket
		config.Docker.SocketPath = socket
	}
	if maxCon := getEnvInt("DOCKER_MAX_CONCURRENT", 0); maxCon != 0 {
		config.ContainerMonitor.MaxConcurrent = maxCon
		config.Docker.MaxConcurrent = maxCon
	}

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

	return nil
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

// ValidateConfig valida a configuração
func ValidateConfig(config *types.Config) error {
	if config.API.Enabled && (config.API.Port <= 0 || config.API.Port > 65535) {
		return fmt.Errorf("invalid API port: %d", config.API.Port)
	}

	if config.Metrics.Enabled && (config.Metrics.Port <= 0 || config.Metrics.Port > 65535) {
		return fmt.Errorf("invalid metrics port: %d", config.Metrics.Port)
	}

	if config.Docker.Enabled && config.Docker.SocketPath == "" {
		return fmt.Errorf("docker socket path cannot be empty when docker monitoring is enabled")
	}

	if config.File.Enabled && config.File.PositionsPath == "" {
		return fmt.Errorf("file positions path cannot be empty when file monitoring is enabled")
	}

	if config.Sinks.Loki.Enabled && config.Sinks.Loki.URL == "" {
		return fmt.Errorf("loki URL cannot be empty when loki sink is enabled")
	}

	if config.Sinks.LocalFile.Enabled && config.Sinks.LocalFile.Directory == "" {
		return fmt.Errorf("local file directory cannot be empty when local file sink is enabled")
	}

	// Verificar se pelo menos um sink está habilitado
	anyEnabled := config.Sinks.Loki.Enabled ||
				  config.Sinks.LocalFile.Enabled ||
				  config.Sinks.Elasticsearch.Enabled ||
				  config.Sinks.Splunk.Enabled

	if !anyEnabled {
		return fmt.Errorf("at least one sink must be enabled")
	}

	return nil
}