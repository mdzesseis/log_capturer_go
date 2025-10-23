# Módulo Sinks (internal/sinks)

## Estrutura e Responsabilidades

O módulo `sinks` é responsável pela **entrega final** dos logs processados para seus destinos configurados. Este módulo implementa diferentes tipos de sinks especializados para diferentes sistemas de armazenamento e análise de logs, garantindo entrega confiável e performance otimizada.

### Arquivos Principais:
- `common.go` - Utilitários e funcionalidades compartilhadas
- `loki_sink.go` - Sink para Grafana Loki
- `local_file_sink.go` - Sink para arquivos locais
- `elasticsearch_sink.go` - Sink para Elasticsearch
- `splunk_sink.go` - Sink para Splunk

## Funcionamento

### Arquitetura de Sinks:
```
[Processed Logs] -> [Sink Selection] -> [Batching] -> [Compression] -> [Delivery]
        |               |                  |            |              |
   LogEntry         Multiple Sinks     Adaptive      HTTP/File      Target System
   Metadata         Parallel Send      Batching      Compression    Loki/ES/File
   Labels           Load Balance       Queue Mgmt    Circuit Break  Splunk/Others
   Message          Error Handling     Backpressure  Retry Logic    Custom
```

### Tipos de Sinks:

#### 1. **LokiSink**
- **Propósito**: Entrega de logs para Grafana Loki
- **Características**:
  - Stream-based delivery seguindo modelo Loki
  - Compressão HTTP adaptativa (gzip/zstd)
  - Circuit breaker para proteção
  - Dead Letter Queue para logs com falha
  - Adaptive batching para otimização

#### 2. **LocalFileSink**
- **Propósito**: Armazenamento em arquivos locais
- **Características**:
  - Rotação automática de arquivos
  - Compressão de arquivos antigos
  - Proteção contra disco cheio
  - Múltiplos formatos de saída (JSON, texto)
  - Cleanup automático baseado em espaço

#### 3. **ElasticsearchSink**
- **Propósito**: Indexação em Elasticsearch
- **Características**:
  - Bulk indexing para performance
  - Index template management
  - Mapping customizável
  - ILM (Index Lifecycle Management)

#### 4. **SplunkSink**
- **Propósito**: Envio para Splunk via HEC
- **Características**:
  - HTTP Event Collector protocol
  - Source type mapping
  - Index routing
  - Acknowledgment tracking

### Estrutura Principal:
```go
type LokiSink struct {
    config       types.LokiConfig
    logger       *logrus.Logger
    httpClient   *http.Client
    breaker      *circuit.Breaker
    compressor   *compression.HTTPCompressor
    deadLetterQueue *dlq.DeadLetterQueue

    queue        chan types.LogEntry
    batch        []types.LogEntry
    batchMutex   sync.Mutex
    lastSent     time.Time

    adaptiveBatcher *batching.AdaptiveBatcher
    useAdaptiveBatching bool

    ctx          context.Context
    cancel       context.CancelFunc
    isRunning    bool
    mutex        sync.RWMutex

    backpressureCount int64
    droppedCount      int64
}

type LocalFileSink struct {
    config    types.LocalFileConfig
    logger    *logrus.Logger
    compressor *compression.HTTPCompressor

    queue     chan types.LogEntry
    files     map[string]*logFile
    filesMutex sync.RWMutex

    ctx       context.Context
    cancel    context.CancelFunc
    isRunning bool
    mutex     sync.RWMutex

    lastDiskCheck time.Time
    diskSpaceMutex sync.RWMutex
}

type logFile struct {
    path         string
    file         *os.File
    writer       io.Writer
    currentSize  int64
    lastWrite    time.Time
    mutex        sync.Mutex
    useCompression bool
    compressor   *compression.HTTPCompressor
}
```

## Papel e Importância

### Delivery Layer:
- **Final Destination**: Ponto final da pipeline de processamento
- **Multi-Target**: Suporte a múltiplos destinos simultaneamente
- **Reliability**: Garantia de entrega com retry e DLQ

### Performance Optimization:
- **Batching**: Agrupamento eficiente para reduzir overhead
- **Compression**: Redução de bandwidth e storage
- **Connection Pooling**: Reutilização de conexões HTTP

### Operational Excellence:
- **Circuit Breaking**: Proteção contra falhas em cascade
- **Backpressure**: Proteção contra sobrecarga de destinos
- **Monitoring**: Métricas detalhadas de entrega

## Configurações Aplicáveis

### Loki Sink Configuration:
```yaml
sinks:
  loki:
    enabled: true
    url: "http://loki:3100"
    batch_size: 100
    batch_timeout: "10s"
    queue_size: 5000
    timeout: "30s"
    max_retries: 3
    retry_delay: "2s"

    # Authentication
    auth:
      type: "basic"      # basic, bearer, none
      username: "user"
      password: "pass"
      token: "bearer_token"

    # Custom headers
    headers:
      X-Scope-OrgID: "tenant1"
      X-Custom-Header: "value"

    # TLS configuration
    tls:
      enabled: true
      cert_file: "/path/to/cert.pem"
      key_file: "/path/to/key.pem"
      ca_file: "/path/to/ca.pem"
      insecure_skip_verify: false

    # Compression
    compression:
      enabled: true
      algorithm: "gzip"   # gzip, zstd
      level: 6
      adaptive: true

    # Circuit breaker
    circuit_breaker:
      failure_threshold: 5
      success_threshold: 3
      timeout: "30s"
      half_open_max_calls: 10

    # Labels
    default_labels:
      service: "log-capturer"
      environment: "production"

    # Tenant routing
    tenant_routing:
      enabled: true
      header: "X-Scope-OrgID"
      field: "tenant_id"
      default: "default"
```

### Local File Sink Configuration:
```yaml
sinks:
  local_file:
    enabled: true
    directory: "/var/log/captured"
    output_format: "json"    # json, text
    queue_size: 3000
    filename_pattern: "logs-%Y%m%d-%H%M.log"

    # File rotation
    rotation:
      max_size_mb: 100
      max_files: 10
      compress: true
      retention_hours: 168  # 7 days

    # Disk space protection
    max_total_disk_gb: 5.0
    disk_check_interval: "60s"
    cleanup_threshold_percent: 90.0

    # Text format configuration
    text_format:
      timestamp_format: "2006-01-02T15:04:05.000Z"
      include_level: true
      include_source: true
      field_separator: " | "

    # Compression
    compress: true
    compression_level: 6

    # Buffer settings
    buffer_size: 65536
    flush_interval: "5s"
```

### Elasticsearch Sink Configuration:
```yaml
sinks:
  elasticsearch:
    enabled: true
    urls:
      - "http://elasticsearch:9200"
    index_pattern: "logs-%Y.%m.%d"
    document_type: "_doc"
    batch_size: 500
    batch_timeout: "30s"
    queue_size: 10000

    # Authentication
    auth:
      type: "basic"
      username: "elastic"
      password: "password"

    # Index management
    index_template:
      enabled: true
      template_name: "logs-template"
      pattern: "logs-*"
      settings:
        number_of_shards: 1
        number_of_replicas: 0

    # ILM policy
    ilm:
      enabled: true
      policy_name: "logs-policy"
      rollover_size: "50gb"
      retention_days: 30

    # Mapping
    mapping:
      dynamic: false
      properties:
        "@timestamp":
          type: "date"
        message:
          type: "text"
        level:
          type: "keyword"
        source:
          type: "keyword"
```

### Splunk Sink Configuration:
```yaml
sinks:
  splunk:
    enabled: true
    url: "https://splunk:8088"
    token: "splunk-hec-token"
    batch_size: 100
    batch_timeout: "10s"
    queue_size: 5000

    # HEC settings
    hec:
      index: "main"
      source: "log-capturer"
      sourcetype: "json"
      host_override: "log-capturer-host"

    # Acknowledgment
    ack_enabled: true
    ack_timeout: "30s"

    # TLS
    tls:
      enabled: true
      insecure_skip_verify: false
```

## Problemas Conhecidos

### Performance:
- **Batch Fragmentation**: Lotes pequenos podem reduzir eficiência
- **Memory Usage**: Queues grandes podem consumir muita memória
- **Connection Overhead**: Muitas conexões simultâneas podem sobrecarregar targets

### Reliability:
- **Target Downtime**: Falhas de destino podem causar backlog
- **Network Issues**: Problemas de rede podem interromper entrega
- **Data Loss**: Falhas durante entrega podem causar perda de dados

### Operational:
- **Configuration Complexity**: Muitas opções podem ser difíceis de configurar
- **Monitoring Gaps**: Algumas métricas podem não cobrir todos os cenários
- **Resource Leaks**: Conexões e files não fechados adequadamente

## Melhorias Propostas

### Smart Batching:
```go
type IntelligentBatcher struct {
    targetLatency    time.Duration
    maxBatchSize     int
    dynamicSizing    bool
    compressionRatio float64
    networkBandwidth int64
    optimizer        *BatchOptimizer
}

type BatchOptimizer struct {
    latencyHistory   []time.Duration
    sizeHistory      []int
    compressionStats []float64
    predictor        *LatencyPredictor
}

func (ib *IntelligentBatcher) OptimalBatchSize(currentConditions Conditions) int
func (ib *IntelligentBatcher) ShouldFlush(batch []types.LogEntry) bool
```

### Multi-Tier Storage:
```go
type TieredSink struct {
    hotTier    Sink      // Fast, expensive storage
    warmTier   Sink      // Medium speed, medium cost
    coldTier   Sink      // Slow, cheap storage
    classifier *TierClassifier
    lifecycle  *DataLifecycleManager
}

type TierClassifier struct {
    rules []TierRule
    ml    *MLClassifier
}

func (tc *TierClassifier) ClassifyLog(entry types.LogEntry) TierType
func (dlm *DataLifecycleManager) PromoteData(from, to TierType) error
```

### Advanced Error Handling:
```go
type SinkErrorHandler struct {
    strategies   map[ErrorType]RecoveryStrategy
    fallbackSink Sink
    dlq          *dlq.DeadLetterQueue
    analyzer     *ErrorAnalyzer
}

type ErrorAnalyzer struct {
    patterns    []ErrorPattern
    classifier  *ErrorClassifier
    predictor   *ErrorPredictor
}

func (ea *ErrorAnalyzer) AnalyzeError(err error) ErrorClassification
func (ea *ErrorAnalyzer) PredictFailureProbability() float64
```

### Adaptive Compression:
```go
type AdaptiveCompressor struct {
    algorithms      []CompressionAlgorithm
    selector        *AlgorithmSelector
    performanceData *CompressionPerformanceData
    networkMonitor  *NetworkMonitor
}

type AlgorithmSelector struct {
    criteria    SelectionCriteria
    weights     map[string]float64
    history     *PerformanceHistory
}

func (ac *AdaptiveCompressor) SelectAlgorithm(data []byte, context CompressionContext) CompressionAlgorithm
func (ac *AdaptiveCompressor) UpdatePerformanceData(result CompressionResult)
```

### Sink Health Management:
```go
type SinkHealthManager struct {
    healthCheckers map[string]HealthChecker
    failureDetector *FailureDetector
    recoveryManager *RecoveryManager
    routingTable    *RoutingTable
}

type FailureDetector struct {
    thresholds   map[MetricType]float64
    timeWindows  map[MetricType]time.Duration
    analyzer     *AnomalyDetector
}

func (shm *SinkHealthManager) MonitorHealth(sinkName string) HealthStatus
func (shm *SinkHealthManager) RerouteTraffic(failedSink, backupSink string) error
```

### Tenant Isolation:
```go
type MultiTenantSink struct {
    tenantSinks   map[string]Sink
    router        *TenantRouter
    isolator      *ResourceIsolator
    quotaManager  *QuotaManager
}

type TenantRouter struct {
    rules       []RoutingRule
    defaultSink string
    resolver    *TenantResolver
}

func (mts *MultiTenantSink) Route(entry types.LogEntry) (Sink, error)
func (qm *QuotaManager) CheckQuota(tenantID string, size int64) bool
```

## Métricas Expostas

### Delivery Metrics:
- `sink_logs_sent_total` - Total de logs enviados por sink
- `sink_logs_failed_total` - Total de logs com falha por sink
- `sink_batch_size` - Tamanho dos lotes enviados
- `sink_delivery_duration_seconds` - Duração de entrega por sink

### Performance Metrics:
- `sink_queue_size` - Tamanho atual da fila por sink
- `sink_queue_utilization` - Utilização da fila (0-1)
- `sink_compression_ratio` - Taxa de compressão por sink
- `sink_throughput_bytes_per_second` - Throughput em bytes por segundo

### Reliability Metrics:
- `sink_circuit_breaker_state` - Estado do circuit breaker
- `sink_retry_attempts_total` - Total de tentativas de retry
- `sink_backpressure_events_total` - Eventos de backpressure
- `sink_dlq_entries_total` - Entradas na Dead Letter Queue

### Resource Metrics:
- `sink_connections_active` - Conexões ativas por sink
- `sink_memory_usage_bytes` - Uso de memória por sink
- `sink_file_handles` - File handles abertos (LocalFile)
- `sink_disk_usage_bytes` - Uso de disco (LocalFile)

## Exemplo de Uso

### Setup Multi-Sink:
```go
// Configurar múltiplos sinks
lokiConfig := types.LokiConfig{
    Enabled: true,
    URL: "http://loki:3100",
    BatchSize: 100,
}

fileConfig := types.LocalFileConfig{
    Enabled: true,
    Directory: "/var/log/captured",
    OutputFormat: "json",
}

// Criar sinks
dlq := dlq.NewDeadLetterQueue(dlqConfig, logger)
lokiSink := sinks.NewLokiSink(lokiConfig, logger, dlq)
fileSink := sinks.NewLocalFileSink(fileConfig, logger)

// Iniciar sinks
ctx := context.Background()
if err := lokiSink.Start(ctx); err != nil {
    log.Fatal(err)
}
if err := fileSink.Start(ctx); err != nil {
    log.Fatal(err)
}

// Enviar logs para ambos
entry := types.LogEntry{
    Timestamp: time.Now(),
    Message: "Test message",
    Labels: map[string]string{
        "service": "test",
    },
}

lokiSink.Send([]types.LogEntry{entry})
fileSink.Send([]types.LogEntry{entry})
```

### Custom Sink Implementation:
```go
type CustomSink struct {
    config   CustomConfig
    logger   *logrus.Logger
    client   *CustomClient
    queue    chan types.LogEntry
    // ... other fields
}

func (cs *CustomSink) Start(ctx context.Context) error {
    // Implementar inicialização
    go cs.processLoop()
    return nil
}

func (cs *CustomSink) Send(entries []types.LogEntry) error {
    // Implementar envio
    for _, entry := range entries {
        select {
        case cs.queue <- entry:
        default:
            // Handle backpressure
        }
    }
    return nil
}

func (cs *CustomSink) Stop() error {
    // Implementar shutdown graceful
    return nil
}
```

## Dependências

### Bibliotecas Externas:
- `net/http` - HTTP client para sinks remotos
- `compress/gzip` - Compressão de dados
- `encoding/json` - Serialização JSON

### Módulos Internos:
- `internal/metrics` - Para exposição de métricas
- `pkg/compression` - Para compressão avançada
- `pkg/circuit` - Para circuit breaker
- `pkg/dlq` - Para Dead Letter Queue
- `pkg/batching` - Para batching adaptativo
- `pkg/types` - Para tipos de dados

O módulo `sinks` é o **ponto de saída** final do sistema, sendo crítico para garantir que todos os logs processados sejam entregues de forma confiável e eficiente aos seus destinos finais. Sua performance e confiabilidade determinam o sucesso de toda a pipeline de log processing.