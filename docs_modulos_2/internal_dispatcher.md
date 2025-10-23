# Módulo Dispatcher (internal/dispatcher)

## Estrutura e Responsabilidades

O módulo `dispatcher` é o **núcleo de orquestração** do pipeline de processamento de logs. Este módulo recebe logs de múltiplas fontes, aplica processamento e distribui para os destinos configurados. É responsável por garantir throughput, confiabilidade e eficiência no processamento.

### Arquivos Principais:
- `dispatcher.go` - Lógica principal de dispatch e orquestração
- `worker_pool.go` - Pool de workers para processamento paralelo
- `batch_processor.go` - Processamento em lotes para otimização

## Funcionamento

### Arquitetura do Dispatcher:
```
[Sources] -> [Queue] -> [Workers] -> [Processor] -> [Sinks]
    |           |          |           |           |
FileMonitor    Buffer    Worker1    Transform    Loki
Container      Queue     Worker2    Filter       LocalFile
NetworkInput   DLQ       Worker3    Enrich       Custom
```

### Fluxo de Processamento:
1. **Recepção** (`Submit()`)
   - Recebe LogEntry de monitores
   - Adiciona à fila interna com backpressure
   - Aplica rate limiting se configurado

2. **Distribuição** (`workerLoop()`)
   - Distribui trabalho para workers disponíveis
   - Balanceamento de carga round-robin
   - Recuperação de falhas de workers

3. **Processamento** (`processEntry()`)
   - Aplica transformações configuradas
   - Executa filtros e enriquecimento
   - Valida dados processados

4. **Entrega** (`deliverToSinks()`)
   - Envia para todos os sinks habilitados
   - Controle de fluxo adaptativo
   - Retry logic para falhas temporárias

### Estrutura Principal:
```go
type Dispatcher struct {
    config      DispatcherConfig
    logger      *logrus.Logger
    processor   *processing.LogProcessor

    // Queue Management
    inputQueue  chan types.LogEntry
    dlq         *dlq.DeadLetterQueue

    // Worker Pool
    workers     []*Worker
    workerPool  *workerpool.WorkerPool

    // Sinks
    sinks       []types.Sink
    sinkStats   map[string]*SinkStats

    // Flow Control
    backpressure *backpressure.Manager
    circuitBreaker *circuit.Breaker
    rateLimiter  *ratelimit.Limiter

    // Metrics & Monitoring
    stats       DispatcherStats
    metrics     *metrics.DispatcherMetrics
}
```

## Papel e Importância

### Orquestração Central:
- **Traffic Controller**: Controla fluxo de dados entre componentes
- **Load Balancer**: Distribui carga entre workers e sinks
- **Quality Gateway**: Garante qualidade dos dados processados

### Confiabilidade:
- **Dead Letter Queue**: Isolamento de mensagens problemáticas
- **Circuit Breaker**: Proteção contra cascata de falhas
- **Retry Logic**: Recuperação automática de falhas temporárias

### Performance:
- **Parallel Processing**: Processamento paralelo via worker pool
- **Batching**: Agrupamento eficiente para sinks
- **Backpressure**: Proteção contra sobrecarga

### Observabilidade:
- **Rich Metrics**: Métricas detalhadas de throughput e latência
- **Health Monitoring**: Status de saúde de todos os componentes
- **Performance Profiling**: Identificação de gargalos

## Configurações Aplicáveis

### Configuração Básica:
```yaml
dispatcher:
  queue_size: 10000        # Tamanho da fila interna
  worker_count: 4          # Número de workers
  batch_size: 100          # Tamanho do lote
  batch_timeout: "5s"      # Timeout para formar lotes
  max_retries: 3           # Tentativas de retry
  retry_backoff: "1s"      # Backoff entre retries
```

### Backpressure e Flow Control:
```yaml
dispatcher:
  backpressure:
    enabled: true
    high_watermark: 0.8    # 80% da fila
    low_watermark: 0.6     # 60% da fila
    strategy: "drop_oldest" # drop_oldest, drop_newest, block

  flow_control:
    enabled: true
    target_latency: "100ms"
    adjustment_factor: 0.1
```

### Circuit Breaker:
```yaml
dispatcher:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 3
    timeout: "30s"
    half_open_max_calls: 10
```

### Dead Letter Queue:
```yaml
dispatcher:
  dlq:
    enabled: true
    max_size: 1000
    retention_period: "24h"
    auto_retry: true
    retry_interval: "5m"
```

### Worker Pool:
```yaml
dispatcher:
  worker_pool:
    min_workers: 2
    max_workers: 10
    idle_timeout: "5m"
    queue_size: 1000
    enable_metrics: true
```

## Métricas Expostas

### Throughput:
- `dispatcher_entries_received_total` - Total de entradas recebidas
- `dispatcher_entries_processed_total` - Total de entradas processadas
- `dispatcher_entries_failed_total` - Total de entradas com falha
- `dispatcher_entries_per_second` - Taxa de processamento

### Latência:
- `dispatcher_processing_duration_seconds` - Tempo de processamento
- `dispatcher_queue_wait_duration_seconds` - Tempo na fila
- `dispatcher_sink_delivery_duration_seconds` - Tempo de entrega

### Queue Health:
- `dispatcher_queue_size` - Tamanho atual da fila
- `dispatcher_queue_utilization` - Utilização da fila (0-1)
- `dispatcher_backpressure_triggered_total` - Ativações de backpressure

### Workers:
- `dispatcher_workers_active` - Workers ativos
- `dispatcher_workers_idle` - Workers ociosos
- `dispatcher_worker_task_duration_seconds` - Duração de tarefas

## Problemas Conhecidos

### Performance:
- **Queue Contention**: Contenção na fila principal com alta concorrência
- **Memory Usage**: Uso de memória pode crescer com filas grandes
- **GC Pressure**: Alocação frequente de structs pode pressionar GC

### Confiabilidade:
- **Worker Failures**: Workers podem falhar e afetar throughput
- **Sink Failures**: Falha de sink pode causar acúmulo na fila
- **Poison Messages**: Mensagens malformadas podem travar workers

### Monitoring:
- **Metric Overhead**: Coleta excessiva de métricas pode impactar performance
- **Alert Fatigue**: Muitos alertas podem reduzir efetividade
- **Debugging**: Difícil debuggar problemas em alta concorrência

## Melhorias Propostas

### Performance Optimization:
```go
// Lock-free queue implementation
type LockFreeQueue struct {
    head unsafe.Pointer
    tail unsafe.Pointer
}

func (q *LockFreeQueue) Enqueue(item *QueueItem) bool
func (q *LockFreeQueue) Dequeue() *QueueItem
```

### Advanced Batching:
```go
type AdaptiveBatcher struct {
    targetLatency   time.Duration
    currentBatchSize int
    latencyHistory  []time.Duration
    sizeHistory     []int
}

func (b *AdaptiveBatcher) OptimalBatchSize() int
func (b *AdaptiveBatcher) ShouldFlush() bool
```

### Smart Retry Logic:
```go
type ExponentialBackoff struct {
    baseDelay    time.Duration
    maxDelay     time.Duration
    multiplier   float64
    jitter       bool
}

type RetryPolicy struct {
    maxAttempts   int
    backoff      BackoffStrategy
    retryableErrors []error
}
```

### Circuit Breaker per Sink:
```go
type SinkCircuitBreaker struct {
    sinkName     string
    breaker      *circuit.Breaker
    healthChecker HealthChecker
}

func (scb *SinkCircuitBreaker) Execute(fn func() error) error
```

### Advanced Metrics:
```go
type AdvancedMetrics struct {
    latencyHistogram  prometheus.HistogramVec
    throughputGauge   prometheus.GaugeVec
    errorRateCounter  prometheus.CounterVec
    customMetrics     map[string]prometheus.Collector
}
```

### Resource Quotas:
```go
type ResourceQuota struct {
    maxMemory     int64
    maxGoroutines int
    maxFileDesc   int
    cpuLimit      float64
}

func (rq *ResourceQuota) Enforce() error
func (rq *ResourceQuota) GetUsage() ResourceUsage
```

## Worker Pool Avançado

### Dynamic Scaling:
```go
type DynamicWorkerPool struct {
    minWorkers    int
    maxWorkers    int
    currentWorkers int
    loadMetrics   *LoadMetrics
    scalePolicy   ScalingPolicy
}

func (dwp *DynamicWorkerPool) ScaleUp() error
func (dwp *DynamicWorkerPool) ScaleDown() error
```

### Worker Specialization:
```go
type SpecializedWorker struct {
    workerType   WorkerType
    capabilities []string
    taskQueue    chan SpecializedTask
}

const (
    GeneralWorker WorkerType = iota
    TransformWorker
    FilterWorker
    EnrichmentWorker
)
```

## API e Interfaces

### Core Interface:
```go
type Dispatcher interface {
    Start(ctx context.Context) error
    Stop() error
    Submit(ctx context.Context, entry types.LogEntry) error
    GetStats() DispatcherStats
    Health() HealthStatus
}
```

### Sink Interface:
```go
type Sink interface {
    Start(ctx context.Context) error
    Stop() error
    Send(ctx context.Context, entries []types.LogEntry) error
    IsHealthy() bool
}
```

### Worker Interface:
```go
type Worker interface {
    Start(ctx context.Context) error
    Stop() error
    Process(task Task) error
    IsIdle() bool
}
```

## Exemplo de Uso

### Configuração Básica:
```go
config := DispatcherConfig{
    QueueSize:    10000,
    WorkerCount:  4,
    BatchSize:    100,
    BatchTimeout: "5s",
}

dispatcher := NewDispatcher(config, logger)
dispatcher.AddSink(lokiSink)
dispatcher.AddSink(fileSink)

if err := dispatcher.Start(ctx); err != nil {
    log.Fatal(err)
}
```

### Submissão de Logs:
```go
entry := types.LogEntry{
    Timestamp: time.Now(),
    Message:   "Application started",
    SourceType: "application",
    Labels: map[string]string{
        "service": "web-api",
        "level":   "info",
    },
}

if err := dispatcher.Submit(ctx, entry); err != nil {
    log.Printf("Failed to submit log: %v", err)
}
```

### Monitoramento:
```go
stats := dispatcher.GetStats()
log.Printf("Queue utilization: %.2f%%", stats.QueueUtilization*100)
log.Printf("Throughput: %.0f entries/sec", stats.ThroughputPerSecond)
log.Printf("Average latency: %v", stats.AverageLatency)
```

## Dependências

### Módulos Obrigatórios:
- `internal/processing` - Processamento de logs
- `pkg/types` - Tipos e interfaces
- `pkg/dlq` - Dead Letter Queue

### Módulos Opcionais:
- `pkg/backpressure` - Controle de fluxo
- `pkg/circuit` - Circuit breaker
- `pkg/ratelimit` - Rate limiting
- `pkg/workerpool` - Worker pool avançado

O módulo `dispatcher` é o **motor central** do sistema, garantindo que todos os logs sejam processados eficientemente, com confiabilidade e observabilidade completa.