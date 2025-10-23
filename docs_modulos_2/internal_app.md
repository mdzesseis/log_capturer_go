# Módulo App (internal/app)

## Estrutura e Responsabilidades

O módulo `app` é o **núcleo central** do log_capturer_go, funcionando como o **orquestrador principal** de toda a aplicação. Este módulo coordena todos os outros componentes e gerencia o ciclo de vida completo da aplicação.

### Arquivos Principais:
- `app.go` - Estrutura principal da aplicação e métodos de ciclo de vida
- `handlers.go` - Endpoints HTTP para monitoramento e controle
- `initialization.go` - Inicialização de todos os componentes

## Funcionamento

### Ciclo de Vida da Aplicação:
1. **Inicialização** (`New()`)
   - Carrega configuração do arquivo YAML
   - Valida configurações
   - Inicializa logging estruturado
   - Cria contexto principal com cancelamento

2. **Configuração de Componentes** (`initializeComponents()`)
   - Inicializa serviços core (dispatcher, processor, etc.)
   - Configura sinks de saída (Loki, LocalFile)
   - Configura monitores de entrada (FileMonitor, ContainerMonitor)
   - Inicializa recursos auxiliares (positions, disk buffer, etc.)
   - Configura recursos enterprise (security, tracing, SLO)

3. **Execução** (`Start()`)
   - Inicia todos os componentes na ordem correta
   - Configura servidores HTTP (API e métricas)
   - Inicia recursos enterprise se habilitados

4. **Encerramento** (`Stop()`)
   - Graceful shutdown com timeouts
   - Para componentes na ordem reversa
   - Aguarda finalização de todas as goroutines

### Estrutura da Aplicação:
```go
type App struct {
    config *types.Config
    logger *logrus.Logger

    // Componentes Core
    taskManager      types.TaskManager
    dispatcher       types.Dispatcher
    positionManager  *positions.PositionBufferManager
    processor        *processing.LogProcessor
    fileMonitor      *monitors.FileMonitor
    containerMonitor *monitors.ContainerMonitor

    // Recursos Enterprise
    securityManager   *security.AuthManager
    tracingManager    *tracing.TracingManager
    sloManager        *slo.SLOManager
    goroutineTracker  *goroutines.GoroutineTracker
    serviceDiscovery  *discovery.ServiceDiscovery

    // Infraestrutura
    httpServer      *http.Server
    metricsServer   *metrics.MetricsServer
    enhancedMetrics *metrics.EnhancedMetrics

    sinks []types.Sink
}
```

## Papel e Importância

### Coordenação Central:
- **Orquestração**: Gerencia a inicialização e parada de todos os componentes
- **Dependências**: Resolve ordem de inicialização baseada em dependências
- **Contexto Global**: Fornece contexto unificado para cancelamento

### Monitoramento e Observabilidade:
- **Health Checks**: Endpoints para verificar saúde dos componentes
- **Métricas**: Exposição de estatísticas operacionais
- **Configuração**: Endpoint para visualizar configuração atual
- **Reload**: Capacidade de recarregar configuração em runtime

### Lifecycle Management:
- **Graceful Shutdown**: Encerramento ordenado sem perda de dados
- **Error Handling**: Recuperação de falhas de componentes
- **Resource Management**: Coordenação de recursos compartilhados

## Configurações Aplicáveis

### Configuração da Aplicação:
```yaml
app:
  name: "ssw-logs-capture"
  version: "v1.0.0"
  environment: "production"
  log_level: "info"
  log_format: "json"
```

### Configuração do Servidor HTTP:
```yaml
server:
  enabled: true
  host: "0.0.0.0"
  port: 8080
  read_timeout: "30s"
  write_timeout: "30s"
```

### Métricas:
```yaml
metrics:
  enabled: true
  port: 8090
  path: "/metrics"
  namespace: "ssw_logs_capture"
```

## Endpoints HTTP Disponíveis

### Endpoints Core:
- `GET /health` - Status de saúde detalhado de todos os componentes
- `GET /stats` - Estatísticas operacionais em tempo real
- `GET /config` - Configuração atual (sanitizada)
- `POST /config/reload` - Recarga de configuração (se habilitado)
- `GET /positions` - Status do gerenciamento de posições
- `GET /dlq/stats` - Estatísticas da Dead Letter Queue

### Endpoints Enterprise:
- `GET /slo/status` - Status dos Service Level Objectives
- `GET /goroutines/stats` - Estatísticas de goroutines e detecção de vazamentos
- `GET /security/audit` - Logs de auditoria de segurança

## Problemas Conhecidos

### Gestão de Recursos:
- **Memory Leaks**: Possível vazamento em shutdowns abruptos
- **Goroutine Leaks**: Componentes que não respeitam contexto de cancelamento
- **File Descriptors**: Acúmulo de FDs em alta rotação de arquivos

### Inicialização:
- **Ordem de Dependências**: Alguns componentes podem falhar se dependências não estão prontas
- **Timeouts**: Componentes lentos podem causar timeout na inicialização
- **Configuration Validation**: Validação pode não cobrir todos os cenários edge-case

### Enterprise Features:
- **Type Mismatches**: Incompatibilidades entre tipos de configuração diferentes
- **Missing Features**: Algumas funcionalidades enterprise não implementadas completamente

## Melhorias Propostas

### Gestão de Dependências:
```go
// Implementar dependency injection container
type Container struct {
    services map[string]interface{}
    dependencies map[string][]string
}

func (c *Container) RegisterService(name string, service interface{}, deps ...string) {
    c.services[name] = service
    c.dependencies[name] = deps
}
```

### Health Check Avançado:
```go
type HealthChecker interface {
    HealthCheck() HealthStatus
    Dependencies() []string
}

type HealthStatus struct {
    Status   string
    Details  map[string]interface{}
    Duration time.Duration
}
```

### Graceful Shutdown Melhorado:
```go
type GracefulShutdown struct {
    shutdownOrder []string
    maxWaitTime   time.Duration
    callbacks     map[string]func() error
}
```

### Observabilidade:
- **Distributed Tracing**: Rastreamento completo de requests
- **Structured Logging**: Logs mais estruturados com contexto
- **Advanced Metrics**: Métricas customizadas por módulo
- **Alerting**: Sistema de alertas integrado

### Performance:
- **Connection Pooling**: Pool de conexões para recursos externos
- **Async Initialization**: Inicialização paralela de componentes independentes
- **Resource Quotas**: Limitação de recursos por componente
- **Circuit Breakers**: Proteção contra cascata de falhas

### Configuração:
- **Environment Variables**: Override completo via variáveis de ambiente ✅ (Implementado)
- **Configuration Validation**: Validação mais robusta com schemas
- **Hot Reload**: Recarga segura de configuração ✅ (Implementado)
- **Configuration Templates**: Templates para diferentes ambientes

## Exemplo de Uso

```go
// Criar aplicação
app, err := app.New("/path/to/config.yaml")
if err != nil {
    log.Fatal(err)
}

// Executar aplicação (bloqueia até sinal de shutdown)
if err := app.Run(); err != nil {
    log.Fatal(err)
}
```

## Dependências

### Componentes Obrigatórios:
- `internal/config` - Carregamento de configuração
- `internal/dispatcher` - Orquestração de logs
- `pkg/types` - Tipos e interfaces

### Componentes Opcionais:
- `internal/monitors` - Monitoramento de fontes
- `internal/sinks` - Destinos de logs
- `pkg/security` - Funcionalidades enterprise
- `pkg/tracing` - Rastreamento distribuído
- `pkg/slo` - Monitoramento de SLO

O módulo `app` é verdadeiramente o **coração** do sistema, garantindo que todos os componentes trabalhem harmoniosamente para fornecer uma solução robusta e escalável de captura e processamento de logs.