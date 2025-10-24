# Módulo Types (pkg/types)

## Estrutura e Responsabilidades

O módulo `types` é o **núcleo de definições** do log_capturer_go, fornecendo todas as estruturas de dados, interfaces e configurações utilizadas em todo o sistema. Este módulo define o contrato entre componentes e garante type safety em toda a aplicação.

### Arquivos Principais:
- `types.go` - Estruturas de dados principais (LogEntry, ProcessingStep)
- `config.go` - Configurações de todos os componentes
- `interfaces.go` - Interfaces para componentes plugáveis
- `statistics.go` - Estruturas para métricas e estatísticas
- `enterprise.go` - Tipos para recursos enterprise

## Funcionamento

### Hierarquia de Tipos:
```
[Core Types] -> [Config Types] -> [Interface Types] -> [Statistics Types] -> [Enterprise Types]
     |               |                 |                   |                    |
  LogEntry         Config           Sink Interface     DispatcherStats     SecurityConfig
  ProcessingStep   AppConfig        Dispatcher         SinkStats           TracingConfig
  Metadata         SinkConfig       TaskManager        HealthStatus        SLOConfig
  Labels           MonitorConfig    Monitor            Metrics             ComplianceData
```

### Conceitos Principais:

#### 1. **LogEntry - Estrutura Central**
```go
type LogEntry struct {
    // Distributed tracing
    TraceID      string    `json:"trace_id"`
    SpanID       string    `json:"span_id"`
    ParentSpanID string    `json:"parent_span_id,omitempty"`

    // Timing and performance
    Timestamp   time.Time     `json:"timestamp"`
    Duration    time.Duration `json:"duration,omitempty"`
    ProcessedAt time.Time     `json:"processed_at"`

    // Content and context
    Message string `json:"message"`
    Level   string `json:"level"`

    // Source identification
    SourceType string `json:"source_type"`
    SourceID   string `json:"source_id"`

    // Metadata and routing
    Tags   []string              `json:"tags,omitempty"`
    Labels map[string]string     `json:"labels"`
    Fields map[string]interface{} `json:"fields"`

    // Processing pipeline
    ProcessingSteps []ProcessingStep `json:"processing_steps,omitempty"`
    Pipeline        string           `json:"pipeline,omitempty"`

    // Enterprise features
    DataClassification string `json:"data_classification,omitempty"`
    RetentionPolicy    string `json:"retention_policy,omitempty"`
    SanitizedFields    []string `json:"sanitized_fields,omitempty"`
}
```

#### 2. **Configuration Hierarchy**
```go
type Config struct {
    // Core settings
    App                 AppConfig
    Server              ServerConfig
    Metrics             MetricsConfig

    // Processing pipeline
    Processing          ProcessingConfig
    Dispatcher          DispatcherConfig

    // Input sources
    FileMonitorService  FileMonitorServiceConfig
    ContainerMonitor    ContainerMonitorConfig

    // Output destinations
    Sinks               SinksConfig

    // Storage and persistence
    Positions           PositionsConfig
    DiskBuffer          DiskBufferConfig

    // Enterprise features
    Security            SecurityConfig
    Tracing             TracingConfig
    SLO                 SLOConfig
    ServiceDiscovery    ServiceDiscoveryConfig
}
```

#### 3. **Core Interfaces**
```go
type Sink interface {
    Start(ctx context.Context) error
    Stop() error
    Send(entries []LogEntry) error
    IsHealthy() bool
    GetStats() SinkStats
}

type Dispatcher interface {
    Start(ctx context.Context) error
    Stop() error
    Submit(ctx context.Context, entry LogEntry) error
    GetStats() DispatcherStats
    Health() HealthStatus
}

type TaskManager interface {
    StartTask(ctx context.Context, taskID string, taskFunc TaskFunc) error
    StopTask(taskID string) error
    GetTaskStatus(taskID string) TaskStatus
    Cleanup() error
    IsHealthy() bool
}
```

## Papel e Importância

### Type Safety:
- **Strong Typing**: Garantia de type safety em tempo de compilação
- **Interface Contracts**: Contratos bem definidos entre componentes
- **Configuration Validation**: Validação de configuração estruturada

### Interoperability:
- **Component Decoupling**: Separação clara entre componentes
- **Plugin Architecture**: Suporte a componentes plugáveis
- **API Consistency**: APIs consistentes em todo o sistema

### Performance:
- **Memory Efficiency**: Estruturas otimizadas para performance
- **JSON Serialization**: Serialização eficiente para storage e APIs
- **Zero-Copy Operations**: Design para operações zero-copy onde possível

## Estruturas Principais

### LogEntry Fields:

#### Distributed Tracing:
- `TraceID` - Identificador único de trace OpenTelemetry
- `SpanID` - Identificador único de span
- `ParentSpanID` - ID do span pai para hierarquia

#### Timing & Performance:
- `Timestamp` - Timestamp original do log
- `Duration` - Duração da operação (se disponível)
- `ProcessedAt` - Quando foi processado pelo sistema

#### Content & Context:
- `Message` - Conteúdo da mensagem de log
- `Level` - Nível padronizado (trace, debug, info, warn, error, fatal, panic)

#### Source Identification:
- `SourceType` - Tipo da fonte ("file", "docker", "api", "internal")
- `SourceID` - Identificador único da fonte

#### Metadata & Routing:
- `Tags` - Tags de classificação para filtros
- `Labels` - Labels chave-valor estilo Prometheus
- `Fields` - Campos estruturados extraídos

#### Processing Pipeline:
- `ProcessingSteps` - Histórico detalhado de processamento
- `Pipeline` - Identificador do pipeline usado

#### Enterprise Features:
- `DataClassification` - Classificação de sensibilidade
- `RetentionPolicy` - Política de retenção
- `SanitizedFields` - Campos sanitizados para compliance

### Configuration Types:

#### Core Configs:
```go
type AppConfig struct {
    Name        string `yaml:"name"`
    Version     string `yaml:"version"`
    Environment string `yaml:"environment"`
    LogLevel    string `yaml:"log_level"`
    LogFormat   string `yaml:"log_format"`
}

type ServerConfig struct {
    Enabled      bool   `yaml:"enabled"`
    Host         string `yaml:"host"`
    Port         int    `yaml:"port"`
    ReadTimeout  string `yaml:"read_timeout"`
    WriteTimeout string `yaml:"write_timeout"`
}
```

#### Sink Configs:
```go
type LokiConfig struct {
    Enabled     bool              `yaml:"enabled"`
    URL         string            `yaml:"url"`
    BatchSize   int               `yaml:"batch_size"`
    BatchTimeout string           `yaml:"batch_timeout"`
    Auth        AuthConfig        `yaml:"auth"`
    Headers     map[string]string `yaml:"headers"`
    Labels      map[string]string `yaml:"labels"`
}

type LocalFileConfig struct {
    Enabled      bool            `yaml:"enabled"`
    Directory    string          `yaml:"directory"`
    OutputFormat string          `yaml:"output_format"`
    Rotation     RotationConfig  `yaml:"rotation"`
}
```

#### Monitor Configs:
```go
type FileConfig struct {
    Enabled      bool     `yaml:"enabled"`
    Paths        []string `yaml:"paths"`
    ExcludePaths []string `yaml:"exclude_paths"`
    PipelineFile string   `yaml:"pipeline_file"`
}

type DockerConfig struct {
    Enabled           bool              `yaml:"enabled"`
    SocketPath        string            `yaml:"socket_path"`
    IncludeLabels     map[string]string `yaml:"include_labels"`
    ExcludeLabels     map[string]string `yaml:"exclude_labels"`
    IncludeContainers []string          `yaml:"include_containers"`
    ExcludeContainers []string          `yaml:"exclude_containers"`
}
```

### Statistics Types:

#### Component Stats:
```go
type DispatcherStats struct {
    QueueSize           int           `json:"queue_size"`
    QueueUtilization    float64       `json:"queue_utilization"`
    ThroughputPerSecond float64       `json:"throughput_per_second"`
    AverageLatency      time.Duration `json:"average_latency"`
    ProcessedTotal      int64         `json:"processed_total"`
    ErrorsTotal         int64         `json:"errors_total"`
    WorkersActive       int           `json:"workers_active"`
}

type SinkStats struct {
    QueueSize        int           `json:"queue_size"`
    SentTotal        int64         `json:"sent_total"`
    ErrorsTotal      int64         `json:"errors_total"`
    AverageLatency   time.Duration `json:"average_latency"`
    CompressionRatio float64       `json:"compression_ratio"`
    IsHealthy        bool          `json:"is_healthy"`
}
```

#### Health Status:
```go
type HealthStatus struct {
    Status      string                 `json:"status"`
    Message     string                 `json:"message"`
    LastCheck   time.Time              `json:"last_check"`
    Details     map[string]interface{} `json:"details"`
    Components  map[string]HealthStatus `json:"components"`
}
```

### Enterprise Types:

#### Security Configuration:
```go
type SecurityConfig struct {
    Enabled        bool                    `yaml:"enabled"`
    Authentication AuthenticationConfig   `yaml:"authentication"`
    Authorization  AuthorizationConfig    `yaml:"authorization"`
    Audit          AuditConfig            `yaml:"audit"`
    Encryption     EncryptionConfig       `yaml:"encryption"`
}

type AuthenticationConfig struct {
    Enabled        bool              `yaml:"enabled"`
    Method         string            `yaml:"method"`
    JWT            JWTConfig         `yaml:"jwt"`
    SessionTimeout string            `yaml:"session_timeout"`
    MaxAttempts    int               `yaml:"max_attempts"`
    LockoutTime    string            `yaml:"lockout_time"`
}
```

#### Tracing Configuration:
```go
type TracingConfig struct {
    Enabled     bool              `yaml:"enabled"`
    Provider    string            `yaml:"provider"`
    Endpoint    string            `yaml:"endpoint"`
    ServiceName string            `yaml:"service_name"`
    SampleRate  float64           `yaml:"sample_rate"`
    Headers     map[string]string `yaml:"headers"`
}
```

#### SLO Configuration:
```go
type SLOConfig struct {
    Enabled     bool        `yaml:"enabled"`
    Objectives  []Objective `yaml:"objectives"`
    ErrorBudget ErrorBudget `yaml:"error_budget"`
}

type Objective struct {
    Name       string  `yaml:"name"`
    Target     float64 `yaml:"target"`
    Window     string  `yaml:"window"`
    MetricType string  `yaml:"metric_type"`
}
```

## Validação e Constraints

### Field Validation:
```go
// LogEntry validation rules
func (le *LogEntry) Validate() error {
    if le.Message == "" {
        return errors.New("message cannot be empty")
    }
    if le.SourceType == "" {
        return errors.New("source_type is required")
    }
    if le.Timestamp.IsZero() {
        return errors.New("timestamp is required")
    }
    return nil
}

// Configuration validation
func (c *Config) Validate() error {
    if c.App.Name == "" {
        return errors.New("app.name is required")
    }
    // Additional validation logic...
    return nil
}
```

### Type Conversions:
```go
// String to duration conversion
func parseDuration(s string, defaultValue time.Duration) time.Duration {
    if s == "" {
        return defaultValue
    }
    d, err := time.ParseDuration(s)
    if err != nil {
        return defaultValue
    }
    return d
}

// Environment variable helpers
func getEnvString(key, defaultValue string) string
func getEnvInt(key string, defaultValue int) int
func getEnvBool(key string, defaultValue bool) bool
```

## Extensibilidade

### Custom Fields:
```go
// LogEntry supports custom fields through Fields map
entry.Fields["custom_metric"] = 42.0
entry.Fields["user_id"] = "user123"
entry.Fields["request_id"] = "req-abc-123"

// Labels for Prometheus-style querying
entry.Labels["service"] = "web-api"
entry.Labels["environment"] = "production"
entry.Labels["version"] = "v1.2.3"
```

### Plugin Interfaces:
```go
// Custom sink implementation
type CustomSink struct {
    config CustomSinkConfig
    client CustomClient
}

func (cs *CustomSink) Start(ctx context.Context) error {
    // Implementation
}

func (cs *CustomSink) Send(entries []LogEntry) error {
    // Implementation
}

// Custom processor
type CustomProcessor struct {
    config CustomProcessorConfig
}

func (cp *CustomProcessor) Process(entry *LogEntry) (*LogEntry, error) {
    // Implementation
}
```

### Configuration Extensions:
```go
// Custom configuration sections
type CustomConfig struct {
    Enabled  bool              `yaml:"enabled"`
    Settings map[string]string `yaml:"settings"`
}

// Integration with main config
type ExtendedConfig struct {
    Config        // Embed main config
    Custom CustomConfig `yaml:"custom"`
}
```

## Performance Considerations

### Memory Optimization:
- **String Interning**: Para labels e tags frequentes
- **Object Pooling**: Para LogEntry e estruturas temporárias
- **Zero Allocation**: Operações que evitam alocações

### Serialization:
- **JSON Tags**: Otimizados para performance de serialização
- **Omitempty**: Para reduzir tamanho de JSON
- **Custom Marshalers**: Para tipos específicos quando necessário

### Concurrent Safety:
- **Immutable Design**: Estruturas imutáveis onde possível
- **Copy Semantics**: Cópia segura entre goroutines
- **Channel Types**: Types seguros para channels

## Exemplo de Uso

### Creating LogEntry:
```go
entry := types.LogEntry{
    TraceID:    "trace-123",
    SpanID:     "span-456",
    Timestamp:  time.Now(),
    Message:    "User login successful",
    Level:      "info",
    SourceType: "api",
    SourceID:   "auth-service",
    Labels: map[string]string{
        "service":     "auth",
        "environment": "production",
        "user_id":     "user123",
    },
    Fields: map[string]interface{}{
        "response_time_ms": 45,
        "endpoint":         "/api/login",
        "user_agent":       "Mozilla/5.0...",
    },
}
```

### Configuration Usage:
```go
config := &types.Config{
    App: types.AppConfig{
        Name:        "log-capturer",
        Version:     "v1.0.0",
        Environment: "production",
        LogLevel:    "info",
        LogFormat:   "json",
    },
    Sinks: types.SinksConfig{
        Loki: types.LokiConfig{
            Enabled:   true,
            URL:       "http://loki:3100",
            BatchSize: 100,
        },
    },
}
```

## Dependências

### Bibliotecas Externas:
- `time` - Para tipos de tempo padrão
- `context` - Para context propagation
- `encoding/json` - Para serialização JSON

### Módulos Internos:
- Este é o módulo base - outros módulos dependem dele
- Não tem dependências internas para evitar import cycles

O módulo `types` é **fundamental** para todo o sistema, fornecendo a base type-safe sobre a qual todos os outros componentes são construídos. Sua correta definição e manutenção são críticas para a estabilidade e performance de toda a aplicação.