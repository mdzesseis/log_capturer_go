# Módulo Docker (pkg/docker)

## Estrutura e Responsabilidades

O módulo `docker` é responsável pelo **gerenciamento eficiente de conexões** com a Docker API para monitoramento de containers. Este módulo implementa connection pooling, health monitoring e retry logic para garantir alta performance e confiabilidade na integração com Docker.

### Arquivos Principais:
- `pool_manager.go` - Gerenciador principal de pool de conexões Docker
- `connection_pool.go` - Implementação do pool de conexões
- `client_manager.go` - Gerenciamento de clientes Docker individuais

## Funcionamento

### Arquitetura de Connection Pool:
```
[Container Monitor] -> [Pool Manager] -> [Pooled Clients] -> [Docker API] -> [Container Logs]
        |                   |               |                   |               |
    Log Requests        Client Selection   Health Check      API Calls       Log Streams
    Event Monitoring    Load Balancing     Retry Logic       Rate Limiting   Container Data
    Container Discovery Round Robin        Error Handling    Authentication  Event Streams
    Stream Management   Connection Reuse   Recovery          Timeouts        Metadata
```

### Conceitos Principais:

#### 1. **Connection Pooling**
- **Pool Size Management**: Número configurável de conexões simultâneas
- **Round Robin**: Distribuição uniforme de requests entre clients
- **Connection Reuse**: Reutilização eficiente de conexões TCP
- **Idle Management**: Gerenciamento de conexões ociosas

#### 2. **Health Monitoring**
- **Periodic Checks**: Verificação periódica de health dos clients
- **Failure Detection**: Detecção proativa de falhas de conexão
- **Auto Recovery**: Recuperação automática de clients com falha
- **Circuit Breaking**: Isolamento de clients com problemas

#### 3. **Error Handling & Retry**
- **Exponential Backoff**: Retry com backoff exponencial
- **Failed Client Isolation**: Isolamento de clients com falha
- **Graceful Degradation**: Operação com pool reduzido
- **Automatic Reconnection**: Reconexão automática após falhas

### Estrutura Principal:
```go
type PoolManager struct {
    clients     []*PooledClient
    currentIdx  int
    mutex       sync.RWMutex
    logger      *logrus.Logger
    poolSize    int
    socketPath  string
    maxRetries  int
    retryDelay  time.Duration

    // Health monitoring
    healthCheckInterval time.Duration
    unhealthyClients    map[int]time.Time
    healthMutex        sync.RWMutex
}

type PooledClient struct {
    client        *client.Client
    id            int
    inUse         bool
    lastUsed      time.Time
    usageCount    int64
    healthy       bool
    mutex         sync.RWMutex
}

type PoolConfig struct {
    PoolSize              int           `yaml:"pool_size"`
    SocketPath            string        `yaml:"socket_path"`
    MaxRetries            int           `yaml:"max_retries"`
    RetryDelay            time.Duration `yaml:"retry_delay"`
    HealthCheckInterval   time.Duration `yaml:"health_check_interval"`
    ConnectionTimeout     time.Duration `yaml:"connection_timeout"`
    IdleTimeout          time.Duration `yaml:"idle_timeout"`
}
```

## Papel e Importância

### Performance Optimization:
- **Connection Reuse**: Evita overhead de criação/destruição de conexões
- **Load Distribution**: Distribui carga entre múltiplas conexões
- **Resource Efficiency**: Uso eficiente de recursos de rede e sistema

### Reliability:
- **Fault Tolerance**: Tolerância a falhas de conexões individuais
- **High Availability**: Operação contínua mesmo com falhas parciais
- **Auto Recovery**: Recuperação automática sem intervenção manual

### Scalability:
- **Concurrent Access**: Suporte a múltiplos consumers simultâneos
- **Dynamic Scaling**: Ajuste dinâmico do tamanho do pool
- **Resource Limits**: Prevenção de esgotamento de recursos

## Configurações Aplicáveis

### Configuração Básica:
```yaml
docker:
  pool:
    pool_size: 10
    socket_path: "/var/run/docker.sock"
    max_retries: 3
    retry_delay: "5s"
    health_check_interval: "30s"
    connection_timeout: "30s"
    idle_timeout: "5m"

  # API configuration
  api:
    version: "1.40"
    timeout: "30s"
    tls_verify: false

  # Performance tuning
  performance:
    max_concurrent_streams: 100
    buffer_size: 65536
    keep_alive: true
    tcp_keep_alive: "7200s"
```

### Configuração Avançada:
```yaml
docker:
  pool:
    # Pool sizing
    min_pool_size: 5
    max_pool_size: 20
    initial_pool_size: 10
    pool_growth_factor: 1.5

    # Health monitoring
    health_check_interval: "30s"
    health_check_timeout: "10s"
    unhealthy_threshold: 3
    recovery_check_interval: "60s"

    # Connection management
    connection_timeout: "30s"
    idle_timeout: "5m"
    max_connection_age: "1h"
    connection_lifetime: "2h"

    # Retry and backoff
    max_retries: 5
    initial_retry_delay: "1s"
    max_retry_delay: "30s"
    retry_multiplier: 2.0
    jitter: true

  # Circuit breaker
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 3
    timeout: "60s"
    half_open_max_calls: 10

  # Rate limiting
  rate_limit:
    enabled: true
    requests_per_second: 100
    burst_size: 20

  # Metrics and monitoring
  monitoring:
    enabled: true
    metrics_interval: "15s"
    detailed_metrics: true
```

## Algoritmos de Pool Management

### Client Selection Strategy:
```go
func (pm *PoolManager) GetClient() (*PooledClient, error) {
    pm.mutex.Lock()
    defer pm.mutex.Unlock()

    // Find least used healthy client
    var bestClient *PooledClient
    minUsage := int64(-1)

    for _, client := range pm.clients {
        client.mutex.RLock()
        if client.healthy && !client.inUse {
            if minUsage == -1 || client.usageCount < minUsage {
                bestClient = client
                minUsage = client.usageCount
            }
        }
        client.mutex.RUnlock()
    }

    if bestClient == nil {
        return nil, ErrNoHealthyClients
    }

    // Mark as in use
    bestClient.mutex.Lock()
    bestClient.inUse = true
    bestClient.lastUsed = time.Now()
    bestClient.usageCount++
    bestClient.mutex.Unlock()

    return bestClient, nil
}
```

### Health Check Implementation:
```go
func (pm *PoolManager) healthMonitor() {
    ticker := time.NewTicker(pm.healthCheckInterval)
    defer ticker.Stop()

    for range ticker.C {
        pm.performHealthChecks()
    }
}

func (pm *PoolManager) performHealthChecks() {
    for i, client := range pm.clients {
        go func(idx int, c *PooledClient) {
            healthy := pm.checkClientHealth(c)

            c.mutex.Lock()
            wasHealthy := c.healthy
            c.healthy = healthy
            c.mutex.Unlock()

            if !healthy && wasHealthy {
                pm.logger.WithField("client_id", idx).Warn("Docker client became unhealthy")
                pm.markUnhealthy(idx)
            } else if healthy && !wasHealthy {
                pm.logger.WithField("client_id", idx).Info("Docker client recovered")
                pm.markHealthy(idx)
            }
        }(i, client)
    }
}

func (pm *PoolManager) checkClientHealth(client *PooledClient) bool {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    _, err := client.client.Ping(ctx)
    return err == nil
}
```

### Connection Recovery:
```go
func (pm *PoolManager) recoverClient(clientID int) error {
    pm.mutex.Lock()
    defer pm.mutex.Unlock()

    if clientID >= len(pm.clients) {
        return fmt.Errorf("invalid client ID: %d", clientID)
    }

    client := pm.clients[clientID]

    // Close existing client
    client.mutex.Lock()
    if client.client != nil {
        client.client.Close()
    }

    // Create new client
    newClient, err := pm.createClient()
    if err != nil {
        client.mutex.Unlock()
        return fmt.Errorf("failed to create new client: %w", err)
    }

    // Replace client
    client.client = newClient
    client.healthy = true
    client.inUse = false
    client.usageCount = 0
    client.lastUsed = time.Now()
    client.mutex.Unlock()

    pm.logger.WithField("client_id", clientID).Info("Docker client recovered successfully")

    return nil
}
```

## Problemas Conhecidos

### Performance:
- **Connection Overhead**: Muitas conexões podem sobrecarregar Docker daemon
- **Lock Contention**: Contenção em pools com alto paralelismo
- **Memory Usage**: Cada conexão consome memória e file descriptors

### Reliability:
- **Docker Daemon Failures**: Falhas do daemon afetam todo o pool
- **Network Issues**: Problemas de rede podem afetar múltiplas conexões
- **Resource Exhaustion**: Esgotamento de recursos no Docker host

### Operational:
- **Pool Sizing**: Tamanho inadequado pode afetar performance
- **Health Check Overhead**: Checks frequentes podem impactar performance
- **Recovery Time**: Tempo de recovery pode afetar availability

## Melhorias Propostas

### Dynamic Pool Sizing:
```go
type DynamicPoolManager struct {
    basePool      *PoolManager
    scaler        *PoolScaler
    metrics       *PoolMetrics
    loadMonitor   *LoadMonitor
}

type PoolScaler struct {
    minSize       int
    maxSize       int
    scaleUpRate   float64
    scaleDownRate float64
    loadThreshold float64
}

func (dpm *DynamicPoolManager) adjustPoolSize() {
    currentLoad := dpm.loadMonitor.GetCurrentLoad()
    currentSize := dpm.basePool.GetSize()

    if currentLoad > dpm.scaler.loadThreshold && currentSize < dpm.scaler.maxSize {
        dpm.scaleUp()
    } else if currentLoad < dpm.scaler.loadThreshold*0.5 && currentSize > dpm.scaler.minSize {
        dpm.scaleDown()
    }
}
```

### Advanced Health Monitoring:
```go
type HealthMonitor struct {
    checks       []HealthCheck
    aggregator   *HealthAggregator
    predictor    *FailurePredictor
    alertManager *AlertManager
}

type HealthCheck interface {
    Check(client *PooledClient) HealthResult
    Name() string
    Weight() float64
}

type PingHealthCheck struct {
    timeout time.Duration
}

type ContainerListHealthCheck struct {
    maxLatency time.Duration
}

type EventStreamHealthCheck struct {
    streamTimeout time.Duration
}

func (hm *HealthMonitor) ComprehensiveHealthCheck(client *PooledClient) HealthScore {
    var results []HealthResult

    for _, check := range hm.checks {
        result := check.Check(client)
        results = append(results, result)
    }

    return hm.aggregator.Aggregate(results)
}
```

### Circuit Breaker Integration:
```go
type PoolCircuitBreaker struct {
    breakers map[int]*CircuitBreaker
    manager  *PoolManager
    config   CircuitBreakerConfig
}

type CircuitBreakerConfig struct {
    FailureThreshold    int
    SuccessThreshold    int
    Timeout             time.Duration
    HalfOpenMaxCalls    int
    FailureWindow       time.Duration
}

func (pcb *PoolCircuitBreaker) ExecuteWithBreaker(clientID int, operation func(*PooledClient) error) error {
    breaker := pcb.breakers[clientID]

    return breaker.Execute(func() error {
        client, err := pcb.manager.GetClientByID(clientID)
        if err != nil {
            return err
        }
        defer pcb.manager.ReleaseClient(client)

        return operation(client)
    })
}
```

### Connection Metrics:
```go
type PoolMetrics struct {
    activeConnections    prometheus.Gauge
    totalConnections     prometheus.Gauge
    connectionUtilization prometheus.Histogram
    connectionErrors     prometheus.Counter
    healthCheckDuration  prometheus.Histogram
    poolOperations       prometheus.Counter
}

func (pm *PoolMetrics) RecordConnectionUse(duration time.Duration) {
    pm.connectionUtilization.Observe(duration.Seconds())
    pm.poolOperations.Inc()
}

func (pm *PoolMetrics) RecordHealthCheck(duration time.Duration, success bool) {
    pm.healthCheckDuration.Observe(duration.Seconds())
    if !success {
        pm.connectionErrors.Inc()
    }
}
```

## Métricas Expostas

### Pool Metrics:
- `docker_pool_connections_active` - Conexões ativas no pool
- `docker_pool_connections_total` - Total de conexões no pool
- `docker_pool_connections_healthy` - Conexões saudáveis
- `docker_pool_utilization` - Utilização do pool (0-1)

### Performance Metrics:
- `docker_connection_duration_seconds` - Duração de uso de conexões
- `docker_health_check_duration_seconds` - Duração de health checks
- `docker_operation_latency_seconds` - Latência de operações Docker
- `docker_pool_wait_duration_seconds` - Tempo de espera por conexão

### Reliability Metrics:
- `docker_connection_errors_total` - Total de erros de conexão
- `docker_health_check_failures_total` - Falhas de health check
- `docker_recovery_operations_total` - Operações de recovery
- `docker_circuit_breaker_state` - Estado do circuit breaker

## Exemplo de Uso

### Basic Usage:
```go
// Configuração
config := docker.PoolConfig{
    PoolSize:            10,
    SocketPath:          "/var/run/docker.sock",
    MaxRetries:          3,
    RetryDelay:          5 * time.Second,
    HealthCheckInterval: 30 * time.Second,
}

// Criar pool manager
poolManager, err := docker.NewPoolManager(config, logger)
if err != nil {
    log.Fatal(err)
}

// Usar conexão
client, err := poolManager.GetClient()
if err != nil {
    log.Printf("Failed to get client: %v", err)
    return
}
defer poolManager.ReleaseClient(client)

// Fazer operação Docker
containers, err := client.ContainerList(ctx, types.ContainerListOptions{})
if err != nil {
    log.Printf("Failed to list containers: %v", err)
    return
}
```

### Advanced Usage with Retry:
```go
// Operação com retry automático
err := poolManager.ExecuteWithRetry(func(client *docker.PooledClient) error {
    // Operação que pode falhar
    logs, err := client.ContainerLogs(ctx, containerID, types.ContainerLogsOptions{
        ShowStdout: true,
        ShowStderr: true,
        Follow:     true,
        Timestamps: true,
    })
    if err != nil {
        return err
    }
    defer logs.Close()

    // Processar logs
    return processLogStream(logs)
})

if err != nil {
    log.Printf("Operation failed after retries: %v", err)
}
```

## Dependências

### Bibliotecas Externas:
- `github.com/docker/docker/client` - Cliente oficial Docker
- `github.com/docker/docker/api/types` - Tipos Docker API
- `github.com/sirupsen/logrus` - Logging estruturado

### Módulos Internos:
- Integração com `internal/monitors/container_monitor`
- Uso de métricas Prometheus
- Integração com sistema de configuração

O módulo `docker` é **crítico** para performance do container monitoring, fornecendo connection pooling eficiente que pode resultar em melhorias significativas de throughput e confiabilidade na integração com Docker API.