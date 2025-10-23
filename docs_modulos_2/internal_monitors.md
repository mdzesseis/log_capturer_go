# Módulo Monitors (internal/monitors)

## Estrutura e Responsabilidades

O módulo `monitors` é responsável pela **captura de logs de múltiplas fontes** do sistema. Este módulo implementa diferentes tipos de monitores especializados para diferentes fontes de logs, proporcionando uma interface unificada para coleta de dados.

### Arquivos Principais:
- `file_monitor.go` - Monitor para arquivos de log do sistema de arquivos
- `container_monitor.go` - Monitor para logs de containers Docker

## Funcionamento

### Arquitetura de Monitoramento:
```
[Sources] -> [Monitors] -> [Validation] -> [Processing] -> [Dispatcher]
    |           |             |              |              |
Arquivos    FileMonitor   TimestampVal   SelfGuard     Submit()
Containers  ContainerMon  FeedbackGuard  Validation    Queue
Network     NetworkMon    Position       Transform     Sinks
APIs        APIMon        Management     Filter
```

### Tipos de Monitores:

#### 1. **FileMonitor**
- **Propósito**: Monitora arquivos de log no sistema de arquivos
- **Tecnologia**: fsnotify para eventos de sistema de arquivos
- **Características**:
  - Auto-descoberta de arquivos baseada em padrões
  - Rastreamento de posição para recuperação sem perda
  - Rate limiting para arquivos muito ativos
  - Suporte a rotação de arquivos

#### 2. **ContainerMonitor**
- **Propósito**: Monitora logs de containers Docker
- **Tecnologia**: Docker API com connection pooling
- **Características**:
  - Stream em tempo real de logs de containers
  - Filtros por labels e nomes de containers
  - Pool de conexões para alta performance
  - Auto-descoberta de novos containers

### Estrutura Principal:
```go
type FileMonitor struct {
    config         types.FileConfig
    dispatcher     types.Dispatcher
    logger         *logrus.Logger
    taskManager    types.TaskManager
    positionManager    *positions.PositionBufferManager
    timestampValidator *validation.TimestampValidator
    feedbackGuard      *selfguard.FeedbackGuard

    watcher         *fsnotify.Watcher
    files           map[string]*monitoredFile
    lastQuietLogTime map[string]time.Time
    specificFiles   map[string]bool
}

type ContainerMonitor struct {
    config          types.DockerConfig
    dispatcher      types.Dispatcher
    logger          *logrus.Logger
    taskManager     types.TaskManager
    positionManager *positions.PositionBufferManager
    timestampValidator *validation.TimestampValidator
    feedbackGuard   *selfguard.FeedbackGuard

    dockerPool    *docker.PoolManager
    containers    map[string]*monitoredContainer
}
```

### Estruturas de Dados:
```go
type monitoredFile struct {
    path        string
    file        *os.File
    reader      *bufio.Reader
    position    int64
    labels      map[string]string
    lastModTime time.Time
    lastRead    time.Time
}

type monitoredContainer struct {
    id         string
    name       string
    image      string
    labels     map[string]string
    since      time.Time
    stream     io.ReadCloser
    lastRead   time.Time
    cancel     context.CancelFunc
}
```

## Papel e Importância

### Fonte de Dados:
- **Ingestion Point**: Ponto de entrada de todos os logs no sistema
- **Multi-Source**: Suporte a diferentes tipos de fontes simultaneamente
- **Scalability**: Capacidade de escalar para milhares de fontes

### Confiabilidade:
- **Position Tracking**: Rastreamento de posição para recuperação sem perda
- **Error Recovery**: Recuperação automática de falhas de leitura
- **Self-Protection**: Proteção contra feedback loops

### Performance:
- **Efficient Reading**: Leitura eficiente com buffering apropriado
- **Connection Pooling**: Pool de conexões para reduzir overhead
- **Resource Management**: Gestão cuidadosa de file descriptors

## Configurações Aplicáveis

### File Monitor Configuration:
```yaml
file_monitor_service:
  enabled: true
  paths:
    - "/var/log/*.log"
    - "/app/logs/*.log"
    - "/tmp/*.log"

  exclude_paths:
    - "*.gz"
    - "*.zip"
    - "/var/log/btmp"

  # Arquivo de pipeline específico
  pipeline_file: "file_pipeline.yml"

  # Rate limiting
  rate_limit:
    enabled: true
    max_lines_per_second: 1000
    quiet_period: "5m"

  # Position tracking
  position_tracking:
    enabled: true
    sync_interval: "10s"
    checkpoint_interval: "1m"
```

### Container Monitor Configuration:
```yaml
container_monitor:
  enabled: true
  socket_path: "/var/run/docker.sock"

  # Connection pooling
  pool:
    max_connections: 10
    connection_timeout: "30s"
    idle_timeout: "5m"
    health_check_interval: "1m"

  # Container filtering
  include_labels:
    environment: "production"
    logging: "enabled"

  exclude_labels:
    internal: "true"
    system: "monitoring"

  include_containers:
    - "web-*"
    - "api-*"

  exclude_containers:
    - "log_capturer_go"
    - "*-sidecar"

  # Streaming configuration
  streaming:
    follow: true
    timestamps: true
    since: "1h"
    tail: 100
```

### Timestamp Validation:
```yaml
timestamp_validation:
  enabled: true
  max_past_age_seconds: 86400    # 24 hours
  max_future_age_seconds: 300    # 5 minutes
  clamp_enabled: true
  clamp_dlq: false
  invalid_action: "clamp"        # clamp, drop, dlq
  default_timezone: "UTC"
  accepted_formats:
    - "2006-01-02T15:04:05Z"
    - "2006-01-02 15:04:05"
    - "Jan _2 15:04:05"
```

### Self-Guard Configuration:
```yaml
selfguard:
  enabled: true
  self_id_short: "log_capturer_go"
  self_container_name: "log_capturer_go"
  self_namespace: "ssw"
  auto_detect_self: true
  self_log_action: "drop"        # drop, tag, dlq

  exclude_path_patterns:
    - ".*/app/logs/.*"
    - ".*/var/log/capturer/.*"

  exclude_message_patterns:
    - ".*ssw-logs-capture.*"
    - ".*log_capturer_go.*"

  exclude_container_patterns:
    - "log_capturer_go"
    - "*-capturer"
```

## Problemas Conhecidos

### File Monitor:
- **File Descriptor Leaks**: Arquivos não fechados adequadamente em rotação
- **High IO Load**: Leitura intensiva pode impactar performance do sistema
- **Position Drift**: Posições podem se dessincronizar em falhas abruptas
- **Discovery Overhead**: Auto-descoberta pode ser custosa em sistemas com muitos arquivos

### Container Monitor:
- **Connection Exhaustion**: Pool de conexões pode se esgotar em alta carga
- **Stream Interruption**: Streams Docker podem ser interrompidos sem aviso
- **API Rate Limiting**: Docker API pode rate-limit requests intensivos
- **Memory Growth**: Streams longas podem causar crescimento de memória

### Gerais:
- **Feedback Loops**: Logs do próprio capturer podem causar loops infinitos
- **Timestamp Validation**: Validação pode ser muito restritiva em alguns cenários
- **Resource Competition**: Múltiplos monitores competindo por recursos

## Melhorias Propostas

### Advanced File Discovery:
```go
type FileDiscovery struct {
    patterns        []string
    exclusions      []string
    scanInterval    time.Duration
    depthLimit      int
    followSymlinks  bool
    indexer         *FileIndexer
}

type FileIndexer struct {
    index     map[string]FileMetadata
    watchers  map[string]*fsnotify.Watcher
    bloom     *BloomFilter
}

func (fd *FileDiscovery) ScanWithIndex() ([]string, error)
func (fi *FileIndexer) UpdateIndex(path string, metadata FileMetadata)
```

### Intelligent Position Management:
```go
type SmartPositionManager struct {
    checkpoints    map[string]Checkpoint
    snapshots      *SnapshotManager
    recovery       *RecoveryEngine
    consistency    *ConsistencyChecker
}

type Checkpoint struct {
    Position    int64
    Hash        string
    Timestamp   time.Time
    LineCount   int64
    Confidence  float64
}

func (spm *SmartPositionManager) CreateSnapshot(fileID string) error
func (spm *SmartPositionManager) RecoverPosition(fileID string) (int64, error)
```

### Docker API Optimization:
```go
type OptimizedDockerClient struct {
    connectionPool  *ConnectionPool
    circuitBreaker  *CircuitBreaker
    rateLimit       *RateLimiter
    cacheManager    *ResponseCache
    eventAggregator *EventAggregator
}

type EventAggregator struct {
    buffer       []dockerTypes.Event
    flushTimeout time.Duration
    processor    EventProcessor
}

func (odc *OptimizedDockerClient) StreamLogsWithRetry(ctx context.Context, containerID string) (io.ReadCloser, error)
```

### Adaptive Rate Limiting:
```go
type AdaptiveRateLimit struct {
    currentLimit    int64
    targetLatency   time.Duration
    adjustmentRate  float64
    measurementWindow time.Duration
    latencyHistory  []time.Duration
}

func (arl *AdaptiveRateLimit) AdjustLimit(observedLatency time.Duration)
func (arl *AdaptiveRateLimit) ShouldAllow(sourceID string) bool
```

### Multi-Protocol Support:
```go
type NetworkMonitor struct {
    protocols   map[string]ProtocolHandler
    listeners   map[string]net.Listener
    parsers     map[string]MessageParser
}

type ProtocolHandler interface {
    Accept(conn net.Conn) error
    Parse(data []byte) (types.LogEntry, error)
    Close() error
}

// Implementations: SyslogHandler, FluentdHandler, CustomHandler
```

### Health Monitoring:
```go
type MonitorHealth struct {
    monitors        map[string]Monitor
    healthCheckers  map[string]HealthChecker
    alertManager    *AlertManager
    recoveryManager *RecoveryManager
}

type HealthChecker interface {
    Check() HealthStatus
    Recover() error
    Dependencies() []string
}

func (mh *MonitorHealth) PerformHealthCheck() map[string]HealthStatus
func (mh *MonitorHealth) TriggerRecovery(monitorID string) error
```

## Integrações e Dependências

### Core Dependencies:
```go
// Required packages
"github.com/fsnotify/fsnotify"        // File system notifications
"github.com/docker/docker/client"    // Docker API client
"github.com/google/uuid"              // UUID generation

// Internal dependencies
"ssw-logs-capture/pkg/positions"      // Position management
"ssw-logs-capture/pkg/validation"     // Timestamp validation
"ssw-logs-capture/pkg/selfguard"      // Self-protection
"ssw-logs-capture/pkg/docker"         // Docker utilities
```

### Optional Integrations:
```go
// Kubernetes integration
type KubernetesMonitor struct {
    clientset   kubernetes.Interface
    namespace   string
    podWatcher  *PodWatcher
    logStreamer *LogStreamer
}

// Syslog integration
type SyslogMonitor struct {
    listener    net.Listener
    parser      *SyslogParser
    protocols   []string  // udp, tcp, unix
}

// Cloud integration
type CloudWatchMonitor struct {
    client      *cloudwatchlogs.Client
    logGroups   []string
    streams     map[string]*StreamReader
}
```

## Métricas Expostas

### File Monitor Metrics:
- `files_monitored` - Número de arquivos sendo monitorados
- `file_read_bytes_total` - Total de bytes lidos por arquivo
- `file_read_lines_total` - Total de linhas lidas por arquivo
- `file_read_duration_seconds` - Duração de operações de leitura
- `file_position_updates_total` - Total de atualizações de posição

### Container Monitor Metrics:
- `containers_monitored` - Número de containers sendo monitorados
- `container_stream_bytes_total` - Total de bytes de streams por container
- `container_stream_lines_total` - Total de linhas de streams por container
- `docker_api_calls_total` - Total de chamadas à API Docker
- `docker_connection_pool_active` - Conexões ativas no pool

### General Monitor Metrics:
- `monitor_health_status` - Status de saúde dos monitores
- `monitor_restart_total` - Total de restarts de monitores
- `timestamp_validation_failures_total` - Falhas de validação de timestamp
- `selfguard_blocks_total` - Total de bloqueios por self-guard

## Exemplo de Uso

### File Monitor Setup:
```go
// Configuração
fileConfig := types.FileConfig{
    Enabled: true,
    Paths: []string{"/var/log/*.log", "/app/logs/*.log"},
    ExcludePaths: []string{"*.gz"},
}

timestampConfig := types.TimestampValidationConfig{
    Enabled: true,
    MaxPastAgeSeconds: 86400,
    MaxFutureAgeSeconds: 300,
}

// Criação
fileMonitor, err := monitors.NewFileMonitor(
    fileConfig,
    timestampConfig,
    dispatcher,
    taskManager,
    positionManager,
    logger,
)

// Execução
if err := fileMonitor.Start(ctx); err != nil {
    log.Fatal(err)
}
```

### Container Monitor Setup:
```go
// Configuração
dockerConfig := types.DockerConfig{
    Enabled: true,
    SocketPath: "/var/run/docker.sock",
    IncludeLabels: map[string]string{
        "environment": "production",
    },
    ExcludeContainers: []string{"log_capturer_go"},
}

// Criação
containerMonitor, err := monitors.NewContainerMonitor(
    dockerConfig,
    timestampConfig,
    dispatcher,
    taskManager,
    positionManager,
    logger,
)

// Execução
if err := containerMonitor.Start(ctx); err != nil {
    log.Fatal(err)
}
```

## Dependências

### Módulos Obrigatórios:
- `internal/dispatcher` - Para envio de logs processados
- `pkg/positions` - Para rastreamento de posições
- `pkg/types` - Para tipos e interfaces

### Módulos Opcionais:
- `pkg/validation` - Para validação de timestamps
- `pkg/selfguard` - Para proteção contra feedback loops
- `pkg/docker` - Para otimizações Docker específicas
- `internal/metrics` - Para exposição de métricas

O módulo `monitors` é o **ponto de entrada** de todos os dados no sistema, sendo crítico para a confiabilidade e performance da captura de logs. Sua correta configuração e operação são fundamentais para o sucesso de toda a pipeline de processamento.