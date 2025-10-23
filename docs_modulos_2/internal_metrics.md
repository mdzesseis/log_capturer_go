# Módulo Metrics (internal/metrics)

## Estrutura e Responsabilidades

O módulo `metrics` é responsável pela **observabilidade completa** do log_capturer_go através da coleta, agregação e exposição de métricas Prometheus. Este módulo fornece visibilidade detalhada sobre performance, saúde e comportamento de todos os componentes do sistema.

### Arquivos Principais:
- `metrics.go` - Definições de métricas Prometheus e servidor de métricas

## Funcionamento

### Arquitetura de Métricas:
```
[Components] -> [Metrics Collection] -> [Prometheus Registry] -> [HTTP Server] -> [Monitoring Systems]
     |               |                        |                     |                |
FileMonitor    RecordLogProcessed     Global Metrics       /metrics        Grafana
Dispatcher     RecordError           Local Metrics        /health         AlertManager
Sinks          SetComponentHealth    Custom Metrics       HTTP API        Prometheus
Containers     RecordDuration        System Metrics       JSON Export     DataDog
```

### Categorias de Métricas:

#### 1. **Throughput Metrics**
- `logs_processed_total` - Total de logs processados por fonte
- `logs_per_second` - Taxa atual de processamento (gauge)
- `logs_sent_total` - Logs enviados para sinks com status

#### 2. **Performance Metrics**
- `processing_duration_seconds` - Latência de processamento por componente
- `processing_step_duration_seconds` - Duração de cada step do pipeline
- `sink_send_duration_seconds` - Latência de envio para sinks

#### 3. **Queue & Capacity Metrics**
- `dispatcher_queue_utilization` - Utilização da fila principal
- `sink_queue_utilization` - Utilização das filas de sinks
- `queue_size` - Tamanho atual das filas por componente

#### 4. **Health & Status Metrics**
- `component_health` - Status de saúde (1=healthy, 0=unhealthy)
- `files_monitored` - Arquivos sendo monitorados
- `containers_monitored` - Containers sendo monitorados
- `errors_total` - Total de erros por componente e tipo

#### 5. **System Metrics**
- `memory_usage_bytes` - Uso de memória por tipo
- `cpu_usage_percent` - Percentual de uso de CPU
- `goroutines` - Número de goroutines ativas
- `gc_runs_total` - Total de execuções do garbage collector

#### 6. **Task Management Metrics**
- `task_heartbeats_total` - Heartbeats de tarefas em background
- `active_tasks` - Tarefas ativas por tipo e estado

### Estrutura Principal:
```go
type MetricsServer struct {
    server *http.Server
    logger *logrus.Logger
}

type EnhancedMetrics struct {
    logger *logrus.Logger

    // Advanced metrics
    diskUsage            *prometheus.GaugeVec
    responseTime         *prometheus.HistogramVec
    connectionPoolStats  *prometheus.GaugeVec
    compressionRatio     *prometheus.GaugeVec
    batchingStats        *prometheus.GaugeVec
    leakDetection        *prometheus.GaugeVec

    // Custom metrics registry
    customMetrics map[string]prometheus.Metric
    customMutex   sync.RWMutex
}
```

## Papel e Importância

### Observabilidade Completa:
- **Performance Monitoring**: Identificação de gargalos e otimizações
- **Capacity Planning**: Dados para dimensionamento de recursos
- **Health Monitoring**: Detecção proativa de problemas

### Alerting e SLA:
- **Error Rate Monitoring**: Acompanhamento de taxa de erros
- **Latency Tracking**: Monitoramento de SLAs de latência
- **Resource Utilization**: Alertas de utilização de recursos

### Debugging e Troubleshooting:
- **Component Visibility**: Estado detalhado de cada componente
- **Pipeline Analysis**: Análise de performance do pipeline
- **Resource Leak Detection**: Identificação de vazamentos

## Configurações Aplicáveis

### Configuração Básica:
```yaml
metrics:
  enabled: true
  port: 8090
  path: "/metrics"
  namespace: "ssw_logs_capture"
```

### Configuração Avançada:
```yaml
metrics:
  enabled: true
  port: 8090
  path: "/metrics"
  namespace: "ssw_logs_capture"

  # Enhanced metrics
  enhanced:
    enabled: true
    system_metrics_interval: "30s"
    custom_metrics: true

  # Collection intervals
  collection:
    default_interval: "15s"
    high_frequency_interval: "5s"

  # Retention and storage
  retention:
    default_retention: "24h"
    high_resolution_retention: "1h"
```

## Endpoints HTTP Disponíveis

### Endpoints Principais:
- `GET /metrics` - Métricas Prometheus (formato padrão)
- `GET /health` - Health check do servidor de métricas

### Endpoints Futuros:
- `GET /metrics/json` - Métricas em formato JSON
- `GET /metrics/summary` - Resumo das métricas principais
- `GET /metrics/debug` - Métricas de debug detalhadas

## Funções Auxiliares Principais

### Recording Functions:
```go
func RecordLogProcessed(sourceType, sourceID, pipeline string)
func RecordLogSent(sinkType, status string)
func RecordError(component, errorType string)
func RecordProcessingDuration(component, operation string, duration time.Duration)
func RecordSinkSendDuration(sinkType string, duration time.Duration)
func RecordTaskHeartbeat(taskID, taskType string)
```

### State Functions:
```go
func SetFileMonitored(filepath, sourceType string, monitored bool)
func SetContainerMonitored(containerID, containerName, image string, monitored bool)
func SetSinkQueueUtilization(sinkType string, utilization float64)
func SetComponentHealth(componentType, componentName string, healthy bool)
func SetQueueSize(component, queueType string, size int)
func SetActiveTasks(taskType, state string, count int)
```

### Enhanced Metrics Functions:
```go
func (em *EnhancedMetrics) UpdateSystemMetrics()
func (em *EnhancedMetrics) RecordDiskUsage(mountPoint, device string, usage int64)
func (em *EnhancedMetrics) RecordResponseTime(endpoint, method string, duration time.Duration)
func (em *EnhancedMetrics) RecordConnectionPoolStats(poolName, statType string, value float64)
func (em *EnhancedMetrics) RecordCompressionRatio(component, algorithm string, ratio float64)
func (em *EnhancedMetrics) RecordBatchingStats(component, statType string, value float64)
func (em *EnhancedMetrics) RecordLeakDetection(resourceType, component string, count float64)
```

## Problemas Conhecidos

### Performance:
- **Metric Overhead**: Coleta excessiva de métricas pode impactar performance
- **Memory Usage**: Métricas com muitas labels podem consumir muita memória
- **Collection Frequency**: Coleta muito frequente pode gerar pressão no sistema

### Cardinality:
- **High Cardinality Labels**: Labels com muitos valores únicos (ex: IDs)
- **Label Explosion**: Crescimento descontrolado de combinações de labels
- **Storage Impact**: Alto cardinality impacta storage do Prometheus

### Observabilidade:
- **Metric Naming**: Nomes inconsistentes entre componentes
- **Label Consistency**: Labels diferentes para métricas similares
- **Missing Context**: Falta de contexto em algumas métricas

## Melhorias Propostas

### Metric Governance:
```go
type MetricDefinition struct {
    Name        string
    Type        MetricType
    Help        string
    Labels      []string
    Buckets     []float64  // Para histogramas
    Objectives  map[float64]float64  // Para summaries
}

type MetricRegistry struct {
    definitions map[string]MetricDefinition
    policies    []MetricPolicy
}

func (mr *MetricRegistry) ValidateMetric(name string, labels []string) error
func (mr *MetricRegistry) RegisterMetric(def MetricDefinition) error
```

### Adaptive Collection:
```go
type AdaptiveCollector struct {
    baseInterval    time.Duration
    currentLoad     float64
    adaptiveConfig  AdaptiveConfig
}

type AdaptiveConfig struct {
    MinInterval     time.Duration
    MaxInterval     time.Duration
    LoadThreshold   float64
    AdaptationRate  float64
}

func (ac *AdaptiveCollector) AdjustInterval(systemLoad float64)
```

### Custom Metrics Framework:
```go
type CustomMetric struct {
    Definition MetricDefinition
    Collector  func() interface{}
    Validator  func(interface{}) error
    Transformer func(interface{}) prometheus.Metric
}

type MetricsFramework struct {
    customMetrics map[string]CustomMetric
    middlewares   []MetricMiddleware
}

func (mf *MetricsFramework) RegisterCustom(name string, metric CustomMetric) error
func (mf *MetricsFramework) CollectAll() map[string]prometheus.Metric
```

### Alerting Integration:
```go
type AlertRule struct {
    Name        string
    Query       string
    Threshold   float64
    Duration    time.Duration
    Severity    AlertSeverity
    Actions     []AlertAction
}

type AlertManager struct {
    rules       []AlertRule
    evaluator   *RuleEvaluator
    notifier    *AlertNotifier
}

func (am *AlertManager) EvaluateRules(metrics map[string]float64) []Alert
```

### Performance Profiling:
```go
type ProfilerMetrics struct {
    cpuProfile     *prometheus.GaugeVec
    memoryProfile  *prometheus.GaugeVec
    goroutineProfile *prometheus.GaugeVec
    blockProfile   *prometheus.GaugeVec
}

func (pm *ProfilerMetrics) StartProfiling() error
func (pm *ProfilerMetrics) StopProfiling() error
func (pm *ProfilerMetrics) GetProfile(profileType string) ([]byte, error)
```

### Multi-Dimensional Analysis:
```go
type MetricAggregator struct {
    timeWindows   []time.Duration
    aggregations  []AggregationType
    dimensions    []string
}

type AggregationType int
const (
    Sum AggregationType = iota
    Average
    Percentile
    Rate
    Delta
)

func (ma *MetricAggregator) Aggregate(metrics []MetricPoint) map[string]float64
```

## Integração com Ferramentas Externas

### Prometheus Configuration:
```yaml
scrape_configs:
  - job_name: 'ssw-logs-capture'
    static_configs:
      - targets: ['localhost:8090']
    scrape_interval: 15s
    metrics_path: /metrics
```

### Grafana Dashboard Examples:
```json
{
  "dashboard": {
    "title": "SSW Logs Capture Overview",
    "panels": [
      {
        "title": "Logs Processed Rate",
        "targets": [
          {"expr": "rate(logs_processed_total[5m])"}
        ]
      },
      {
        "title": "Queue Utilization",
        "targets": [
          {"expr": "dispatcher_queue_utilization"}
        ]
      }
    ]
  }
}
```

### AlertManager Rules:
```yaml
groups:
  - name: ssw-logs-capture
    rules:
      - alert: HighQueueUtilization
        expr: dispatcher_queue_utilization > 0.8
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High queue utilization detected"

      - alert: ComponentUnhealthy
        expr: component_health == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Component {{ $labels.component_name }} is unhealthy"
```

## Exemplo de Uso

### Uso Básico:
```go
// Criar servidor de métricas
metricsServer := metrics.NewMetricsServer(":8090", logger)
if err := metricsServer.Start(); err != nil {
    log.Fatal(err)
}

// Registrar métricas durante operação
metrics.RecordLogProcessed("file", "/var/log/app.log", "default")
metrics.RecordProcessingDuration("processor", "transform", 50*time.Millisecond)
metrics.SetComponentHealth("dispatcher", "main", true)
```

### Enhanced Metrics:
```go
// Criar enhanced metrics
enhancedMetrics := metrics.NewEnhancedMetrics(logger)
if err := enhancedMetrics.Start(); err != nil {
    log.Fatal(err)
}

// Registrar métricas avançadas
enhancedMetrics.RecordDiskUsage("/var/log", "/dev/sda1", 1024*1024*1024)
enhancedMetrics.RecordCompressionRatio("loki_sink", "gzip", 0.7)
enhancedMetrics.RecordConnectionPoolStats("docker", "active_connections", 10)
```

### Custom Metrics:
```go
// Registrar métrica customizada
customGauge := prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "custom_business_metric",
    Help: "Custom business logic metric",
})
prometheus.MustRegister(customGauge)

// Atualizar durante operação
customGauge.Set(42.0)
```

## Dependências

### Bibliotecas Externas:
- `github.com/prometheus/client_golang` - Cliente Prometheus
- `github.com/sirupsen/logrus` - Logging estruturado

### Módulos Internos:
- Integração com todos os módulos para coleta de métricas
- Dependência opcional de outros módulos (não bloqueia inicialização)

## Labels Padrão

### Consistent Labels:
- `component` - Nome do componente que gerou a métrica
- `source_type` - Tipo da fonte (file, container, network)
- `sink_type` - Tipo do sink (loki, local_file, custom)
- `status` - Status da operação (success, error, timeout)
- `pipeline` - Nome do pipeline de processamento

### Dynamic Labels:
- `source_id` - Identificador específico da fonte
- `container_id` - ID do container Docker
- `container_name` - Nome do container
- `filepath` - Caminho do arquivo monitorado
- `task_id` - ID da tarefa em execução

O módulo `metrics` é **essencial** para a operação e manutenção do sistema, fornecendo a observabilidade necessária para identificar problemas, otimizar performance e garantir SLAs em ambientes de produção.