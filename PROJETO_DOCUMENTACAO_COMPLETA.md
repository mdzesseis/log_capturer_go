# 📋 LOG_CAPTURER_GO - Documentação Técnica Completa

## 📑 Índice
1. [Visão Geral](#visão-geral)
2. [Arquitetura do Sistema](#arquitetura-do-sistema)
3. [Componentes Principais](#componentes-principais)
4. [Fluxo de Dados](#fluxo-de-dados)
5. [Sistema de Posições](#sistema-de-posições)
6. [Gestão de Performance](#gestão-de-performance)
7. [Prevenção de Problemas](#prevenção-de-problemas)
8. [Métricas e Monitoramento](#métricas-e-monitoramento)
9. [APIs Disponíveis](#apis-disponíveis)
10. [Cenários de Uso](#cenários-de-uso)
11. [Escalabilidade](#escalabilidade)
12. [Configuração e Personalização](#configuração-e-personalização)

---

## 🎯 Visão Geral

O **log_capturer_go** é um sistema enterprise de captura, processamento e agregação de logs desenvolvido em Go, projetado para ambientes de alta performance com suporte a múltiplas fontes de logs (arquivos e containers Docker) e integração com stack de observabilidade moderna (Loki, Grafana, Prometheus).

### 🏗️ Características Principais:
- **Multi-source**: Captura logs de arquivos e containers Docker
- **High Performance**: Processamento assíncrono com workers configuráveis
- **Resilient**: Circuit breakers, DLQ, backpressure e rate limiting
- **Scalable**: Suporte a milhares de arquivos e containers
- **Observable**: Métricas detalhadas e alertas integrados
- **Self-Protected**: Prevenção de feedback loops e auto-detecção

---

## 🏛️ Arquitetura do Sistema

```
┌─────────────────────────────────────────────────────────────────┐
│                         LOG_CAPTURER_GO                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│  │File Monitor │    │Container    │    │HTTP Server  │         │
│  │             │    │Monitor      │    │& APIs       │         │
│  └─────────────┘    └─────────────┘    └─────────────┘         │
│         │                   │                   │              │
│         └───────────────────┼───────────────────┘              │
│                             │                                  │
│  ┌─────────────────────────────────────────────────────────────┤
│  │                    DISPATCHER                               │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐           │
│  │  │Deduplication│ │Processing   │ │Rate Limiting│           │
│  │  │             │ │Pipeline     │ │& Throttling │           │
│  │  └─────────────┘ └─────────────┘ └─────────────┘           │
│  └─────────────────────────────────────────────────────────────┤
│                             │                                  │
│  ┌─────────────────────────────────────────────────────────────┤
│  │                      SINKS                                  │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐           │
│  │  │Loki Sink    │ │File Sink    │ │Other Sinks  │           │
│  │  │             │ │             │ │(ES/Splunk)  │           │
│  │  └─────────────┘ └─────────────┘ └─────────────┘           │
│  └─────────────────────────────────────────────────────────────┤
│                                                                │
│  ┌─────────────────────────────────────────────────────────────┤
│  │                 SUPPORT SYSTEMS                            │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐           │
│  │  │Position     │ │Dead Letter  │ │Disk Buffer  │           │
│  │  │Management   │ │Queue        │ │             │           │
│  │  └─────────────┘ └─────────────┘ └─────────────┘           │
│  └─────────────────────────────────────────────────────────────┤
└─────────────────────────────────────────────────────────────────┘
```

### 🔧 Stack Tecnológico:
- **Backend**: Go 1.21+
- **Monitoring**: Prometheus + Grafana + Loki
- **Containerization**: Docker + Docker Compose
- **Storage**: Local files + DLQ persistence
- **APIs**: REST HTTP + Metrics endpoint

---

## 🧩 Componentes Principais

### 1. **File Monitor** (`internal/monitors/file_monitor.go`)

**Responsabilidade**: Monitoramento contínuo de arquivos de log no sistema de arquivos.

**Funcionalidades**:
- ✅ Detecção automática de novos arquivos
- ✅ Monitoramento de mudanças via polling
- ✅ Suporte a padrões glob para inclusão/exclusão
- ✅ Handling de rotação, truncamento e remoção de arquivos
- ✅ Position tracking para recuperação após restart

**Configuração Principal**:
```yaml
file_monitor_service:
  enabled: true
  pipeline_file: "/app/configs/file_pipeline.yml"
  poll_interval: "30s"
  read_buffer_size: 65536
  recursive: true
  follow_symlinks: false
```

### 2. **Container Monitor** (`internal/monitors/container_monitor.go`)

**Responsabilidade**: Monitoramento de logs de containers Docker em tempo real.

**Funcionalidades**:
- ✅ Conexão via Docker Socket
- ✅ Streaming de logs em tempo real
- ✅ Filtros por labels e nomes de containers
- ✅ Captura de stdout/stderr separadamente
- ✅ Reconexão automática em falhas

**Configuração Principal**:
```yaml
container_monitor:
  enabled: true
  socket_path: "unix:///var/run/docker.sock"
  max_concurrent: 25
  include_stdout: true
  include_stderr: true
  tail_lines: 50
```

### 3. **Dispatcher** (`internal/dispatcher/dispatcher.go`)

**Responsabilidade**: Orquestração central do fluxo de logs entre monitores e sinks.

**Funcionalidades**:
- ✅ Queue assíncrona com workers configuráveis
- ✅ Deduplicação de logs baseada em hash
- ✅ Rate limiting adaptativo
- ✅ Batching inteligente para performance
- ✅ Circuit breaker para proteção de sinks
- ✅ Dead Letter Queue para logs problemáticos

**Configuração Principal**:
```yaml
dispatcher:
  queue_size: 100000
  worker_count: 8
  batch_size: 150
  batch_timeout: "5s"
  max_retries: 2
  deduplication_enabled: true
```

### 4. **Position Management** (`pkg/positions/`)

**Responsabilidade**: Tracking de posições para garantir exactly-once delivery.

**Componentes**:
- `PositionBufferManager`: Coordenador geral
- `FilePositionManager`: Posições de arquivos (offset bytes)
- `ContainerPositionManager`: Posições de containers (timestamps)

**Funcionalidades**:
- ✅ Persistência automática de posições
- ✅ Recovery após restart/crash
- ✅ Cleanup automático de posições antigas
- ✅ Buffer em memória para performance

### 5. **Sinks** (`internal/sinks/`)

**Tipos Disponíveis**:
- **Loki Sink**: Integração com Grafana Loki
- **Local File Sink**: Escrita em arquivos locais
- **Elasticsearch Sink**: (Configurável mas não ativo)
- **Splunk Sink**: (Configurável mas não ativo)

**Características**:
- ✅ Processamento assíncrono
- ✅ Batching configurável
- ✅ Retry com exponential backoff
- ✅ Health checking
- ✅ Queue utilization monitoring

---

## 🔄 Fluxo de Dados

### 📊 Fluxo Completo de Processamento:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   SOURCES   │    │ PROCESSING  │    │DESTINATIONS │
├─────────────┤    ├─────────────┤    ├─────────────┤
│             │    │             │    │             │
│File Monitor │────┤             ├────│ Loki Sink   │
│             │    │             │    │             │
├─────────────┤    │ Dispatcher  │    ├─────────────┤
│             │    │             │    │             │
│Container    │────┤  - Queue    ├────│ File Sink   │
│Monitor      │    │  - Workers  │    │             │
│             │    │  - Dedupe   │    ├─────────────┤
└─────────────┘    │  - Pipeline │    │             │
                   │  - Batching │    │Other Sinks  │
                   │             │    │             │
                   └─────────────┘    └─────────────┘
```

### 🚀 Etapas Detalhadas:

1. **📥 Captura**:
   - File Monitor: Polling de arquivos + inotify events
   - Container Monitor: Docker API streaming

2. **🔄 Dispatcher Processing**:
   - Entry validation (timestamp, format)
   - Deduplication check (SHA-256 hash)
   - Self-log detection (feedback guard)
   - Processing pipeline execution
   - Rate limiting application

3. **📤 Sink Delivery**:
   - Batching por sink type
   - Parallel delivery com workers
   - Retry em falhas com exponential backoff
   - DLQ para logs não entregues

---

## 📍 Sistema de Posições

### 🎯 Objetivo:
Garantir **exactly-once delivery** mesmo após restarts, crashes ou rotação de logs.

### 📁 **File Positions**:

**Estrutura**:
```json
{
  "file_path": "/var/log/syslog",
  "file_size": 1048576,
  "last_position": 524288,
  "last_modified": "2024-01-15T10:30:00Z",
  "inode": 12345678,
  "checksum": "abc123def456"
}
```

**Cenários de Handling**:

1. **📝 Arquivo Normal**:
   - Tracking de offset em bytes
   - Update contínuo durante leitura
   - Flush periódico para disco

2. **🔄 Arquivo Rotacionado**:
   ```
   Antes: /var/log/app.log (inode: 123, size: 1MB)
   Depois: /var/log/app.log.1 (inode: 123, size: 1MB)
           /var/log/app.log (inode: 456, size: 0)
   ```
   - Detecção por mudança de inode
   - Finalização do arquivo antigo
   - Início do novo arquivo do offset 0

3. **✂️ Arquivo Truncado**:
   ```
   Antes: size=1MB, position=800KB
   Depois: size=100KB, position=800KB (inválido!)
   ```
   - Detecção: position > file_size
   - Reset position para 0
   - Log de warning sobre truncamento

4. **🗑️ Arquivo Removido**:
   - Grace period de 5 minutos
   - Cleanup da position se não retornar
   - Log de informação sobre remoção

### 🐳 **Container Positions**:

**Estrutura**:
```json
{
  "container_id": "abc123def456...",
  "container_name": "web-app-1",
  "last_timestamp": "2024-01-15T10:30:00.123Z",
  "log_stream": "stdout",
  "last_log_id": "01HKQM7..."
}
```

**Cenários de Handling**:

1. **🟢 Container Rodando**:
   - Streaming contínuo via Docker API
   - Update de timestamp a cada log
   - Flush periódico

2. **🔄 Container Reiniciado**:
   ```
   Antes: container_id=abc123, timestamp=T1
   Depois: container_id=def456, timestamp=T0
   ```
   - Detecção por mudança de container_id
   - Nova position para novo container
   - Manutenção da position antiga por 1h

3. **⏹️ Container Parado**:
   - Finalização do streaming
   - Manutenção da position por 24h
   - Cleanup automático após período

4. **🆕 Container Criado**:
   - Nova position com timestamp atual
   - Captura desde início (tail_lines config)

### ⚙️ **Position Buffer Manager**:

**Configuração**:
```yaml
positions:
  enabled: true
  directory: "/app/data/positions"
  flush_interval: "10s"
  max_memory_buffer: 2000
  max_memory_positions: 10000
  cleanup_interval: "1m"
  max_position_age: "12h"
```

**Funcionamento**:
- Buffer em memória para performance
- Flush periódico para disco
- Cleanup automático de positions antigas
- Recovery automático no startup

---

## ⚡ Gestão de Performance

### 🎛️ **Rate Limiting & Throttling**:

**Adaptive Rate Limiter** (`pkg/ratelimit/adaptive_limiter.go`):
```go
type Config struct {
    InitialRate     float64 // 1000 logs/sec inicial
    MaxRate         float64 // 10000 logs/sec máximo
    MinRate         float64 // 10 logs/sec mínimo
    BurstSize       int     // 100 logs burst
    AdaptationRate  float64 // 0.1 adaptation speed
}
```

**Funcionamento**:
- Rate inicial conservador
- Aumenta gradualmente se sistema suporta
- Diminui rapidamente em backpressure
- Métricas para tuning

### 🔄 **Adaptive Batching**:

**Configuração**:
```yaml
adaptive_batching:
  enabled: false  # Experimental
  min_batch_size: 10
  max_batch_size: 1000
  initial_batch_size: 100
  adaptation_interval: "30s"
  latency_threshold: "500ms"
  throughput_target: 1000
```

**Algoritmo**:
- Batch size adapta baseado em latência
- Flush delay ajusta por throughput
- Monitoring contínuo de performance

### 🛡️ **Backpressure Management**:

**Configuração por Sink**:
```yaml
backpressure_config:
  enabled: true
  queue_warning_threshold: 0.75   # 75%
  queue_critical_threshold: 0.90  # 90%
  queue_emergency_threshold: 0.95 # 95%
  timeout_escalation: true
```

**Comportamento**:
- **< 75%**: Operação normal
- **75-90%**: Warning logs, timeouts aumentados
- **90-95%**: Critical alerts, rate limiting agressivo
- **> 95%**: Emergency mode, DLQ redirect

### 🔌 **Circuit Breaker**:

**Configuração**:
```go
type Config struct {
    MaxFailures    int           // 5 falhas consecutivas
    Timeout        time.Duration // 30s timeout
    MaxRequests    int           // 3 requests no half-open
    ReadyToTrip    func(counts Counts) bool
}
```

**Estados**:
- **Closed**: Normal operation
- **Open**: Falha detectada, requests rejeitados
- **Half-Open**: Teste de recovery

---

## 🛡️ Prevenção de Problemas

### 🔄 **Prevenção de Logs Duplicados**:

**Deduplication Manager** (`pkg/deduplication/deduplication_manager.go`):

**Algoritmo**:
```go
type Entry struct {
    Hash      string    // SHA-256 do conteúdo
    Timestamp time.Time // Primeira ocorrência
    Count     int64     // Número de duplicatas
    SourceID  string    // ID da fonte
}
```

**Configuração**:
```yaml
deduplication_config:
  max_cache_size: 100000
  ttl: "1h"
  cleanup_interval: "10m"
  hash_algorithm: "sha256"
  include_timestamp: false  # Não incluir timestamp no hash
  include_source_id: true   # Incluir source_id no hash
```

**Comportamento**:
- Hash baseado em: message + source_id (opcionalmente timestamp)
- Cache LRU com TTL configurável
- Cleanup automático de entradas antigas
- Métricas de taxa de duplicação

### 🔒 **Feedback Guard (Self-Log Prevention)**:

**FeedbackGuard** (`pkg/selfguard/feedback_guard.go`):

**Configuração**:
```yaml
feedback_guard:
  enabled: true
  self_id_short: "log_capturer"
  self_container_name: "log_capturer_go"
  auto_detect_self: true
  self_log_action: "drop"  # ou "tag", "warn"
```

**Detecção**:
1. **Source ID matching**: Verifica se source_id contém identificadores próprios
2. **Container label matching**: Verifica labels de container
3. **Path pattern matching**: Regex patterns para paths próprios
4. **Message content analysis**: Keywords indicadores próprios
5. **Service label analysis**: Labels de serviço próprio

**Ações**:
- **Drop**: Descartar silenciosamente
- **Tag**: Marcar e permitir (para debug)
- **Warn**: Log warning mas permitir

### 📊 **Detecção de Falsos Positivos**:

**Container Low-Volume Detection**:
```go
type LowVolumeConfig struct {
    MinLogsPerHour     int    // 10 logs/hora mínimo
    MonitoringWindow   string // "24h" janela análise
    AlertThreshold     int    // 5 containers simultâneos
    GracePeriod        string // "2h" período graça novos containers
}
```

**Comportamento**:
- Tracking de volume por container
- Alertas para containers suspeitos
- Grace period para novos containers
- Whitelist automática para containers conhecidos

### 💾 **Gestão de Disco Cheio**:

**Disk Space Manager** (`pkg/cleanup/disk_manager.go`):

**Configuração**:
```yaml
cleanup:
  enabled: true
  check_interval: "30m"
  critical_space_threshold: 5.0   # 5% espaço livre
  warning_space_threshold: 15.0   # 15% espaço livre
  directories:
    - path: "/app/logs"
      max_size_mb: 1024
      retention_days: 7
      max_files: 100
```

**Estratégias**:
1. **Monitoring**: Check periódico de espaço
2. **Warning**: Logs quando < 15% livre
3. **Critical**: Cleanup agressivo quando < 5%
4. **Emergency**: Pausa de captura se < 2%

**Cleanup Priority**:
1. Arquivos mais antigos primeiro
2. Arquivos maiores em caso de empate
3. Patterns específicos (*.old, *.bak)
4. DLQ files se necessário

---

## 📊 Métricas e Monitoramento

### 🎯 **Métricas Core** (`internal/metrics/metrics.go`):

**Application Metrics**:
```
# Logs processados
log_capturer_logs_processed_total{source_type="file|container", status="success|error"}

# Queue utilization
log_capturer_queue_utilization{component="dispatcher|sink"}

# Processing latency
log_capturer_processing_duration_seconds{stage="capture|process|send"}

# Error rates
log_capturer_errors_total{component="file_monitor|container_monitor|dispatcher", type="io|network|timeout"}
```

**Performance Metrics**:
```
# Throughput
log_capturer_throughput_logs_per_second{source_type}

# Batch metrics
log_capturer_batch_size{sink_type}
log_capturer_batch_duration_seconds{sink_type}

# Deduplication
log_capturer_duplicates_detected_total
log_capturer_deduplication_cache_size

# Position tracking
log_capturer_positions_managed{type="file|container"}
log_capturer_position_flushes_total
```

**Resource Metrics**:
```
# Memory usage
log_capturer_memory_usage_bytes{component}
log_capturer_goroutines_count

# Disk usage
log_capturer_disk_usage_bytes{path}
log_capturer_disk_free_bytes{path}

# File descriptors
log_capturer_file_descriptors_count
```

### 📈 **Enhanced Metrics** (`pkg/monitoring/enhanced_metrics.go`):

**Business Metrics**:
```
# Log volume por hora
log_capturer_hourly_volume{source, level}

# Top talkers (containers/files mais ativos)
log_capturer_top_sources{source_id, type}

# SLA compliance
log_capturer_sla_delivery_time_seconds{percentile="50|95|99"}
log_capturer_sla_availability_percent

# Health scores
log_capturer_health_score{component}
```

### 🚨 **Alerting** (`provisioning/alerting/alerts.yml`):

**Critical Alerts**:
- Queue utilization > 90%
- Error rate > 5%
- Disk space < 10%
- Memory usage > 80%
- Goroutine leak (> 1000)

**Warning Alerts**:
- Queue utilization > 75%
- Latency > 1s (p95)
- DLQ growth > 100 entries/min
- Deduplication rate > 50%

**Info Alerts**:
- New containers detected
- File rotation events
- Configuration changes

### 📊 **Dashboards Grafana**:

**Disponíveis**:
1. **Overview Dashboard**: Métricas gerais do sistema
2. **Performance Dashboard**: Throughput, latency, queues
3. **Sources Dashboard**: File/container específico
4. **Errors Dashboard**: Error analysis e troubleshooting
5. **Capacity Dashboard**: Resource usage e planning

---

## 🌐 APIs Disponíveis

### 🏥 **Health Check API**:

```bash
GET /health
```
**Response**:
```json
{
  "status": "healthy|degraded|unhealthy",
  "components": {
    "file_monitor": {"status": "healthy", "last_check": "2024-01-15T10:30:00Z"},
    "container_monitor": {"status": "healthy", "last_check": "2024-01-15T10:30:00Z"},
    "dispatcher": {"status": "healthy", "queue_size": 150},
    "loki_sink": {"status": "healthy", "queue_util": 0.45},
    "position_manager": {"status": "healthy", "positions": 1250}
  },
  "issues": [],
  "check_time": "2024-01-15T10:30:05Z"
}
```

### 📊 **Metrics API**:

```bash
GET /metrics
```
**Response**: Prometheus formatted metrics

### 📈 **Stats API**:

```bash
GET /stats
```
**Response**:
```json
{
  "dispatcher": {
    "total_processed": 1234567,
    "error_count": 45,
    "queue_size": 150,
    "duplicates_detected": 8901,
    "throughput_per_sec": 125.5
  },
  "file_monitor": {
    "files_watched": 245,
    "total_logs": 567890,
    "errors": 12
  },
  "container_monitor": {
    "containers_monitored": 67,
    "total_logs": 890123,
    "reconnections": 3
  },
  "sinks": {
    "loki": {"queue_util": 0.45, "errors": 2},
    "file": {"queue_util": 0.20, "errors": 0}
  }
}
```

### 🔧 **Configuration API**:

```bash
GET /config
```
**Response**: Configuração atual (sanitizada)

```bash
POST /config/reload
```
**Action**: Reload configuração sem restart

### 📍 **Positions API**:

```bash
GET /positions
```
**Response**:
```json
{
  "files": [
    {
      "path": "/var/log/syslog",
      "position": 1048576,
      "last_update": "2024-01-15T10:30:00Z"
    }
  ],
  "containers": [
    {
      "id": "abc123",
      "name": "web-app",
      "last_timestamp": "2024-01-15T10:30:00Z"
    }
  ]
}
```

### 🗃️ **DLQ API**:

```bash
GET /dlq/stats
```
**Response**:
```json
{
  "total_entries": 145,
  "size_mb": 12.5,
  "oldest_entry": "2024-01-15T09:00:00Z",
  "retry_queue_size": 23
}
```

```bash
POST /dlq/reprocess
```
**Action**: Força reprocessamento da DLQ

### 🎯 **Debug APIs**:

```bash
GET /debug/goroutines
GET /debug/memory
GET /debug/positions/validate
```

---

## 🎬 Cenários de Uso

### 📁 **Ciclo de Vida de Arquivos**:

#### 1. **📝 Arquivo Normal**:
```
Estado: /var/log/app.log (1MB, inode: 123)
Position: 512KB lida

Fluxo:
1. File Monitor detecta mudanças via polling
2. Lê incremento desde última position (512KB)
3. Processa linhas novas
4. Atualiza position para 1MB
5. Flush position para disco
```

#### 2. **🔄 Rotação de Arquivo (logrotate)**:
```
Antes: /var/log/app.log (1MB, inode: 123) - position: 800KB
Ação:  mv /var/log/app.log /var/log/app.log.1
       touch /var/log/app.log
Depois: /var/log/app.log.1 (1MB, inode: 123)
        /var/log/app.log (0KB, inode: 456)

Fluxo:
1. Monitor detecta mudança de inode (123→456)
2. Finaliza leitura do arquivo antigo (800KB→1MB)
3. Marca position do arquivo antigo como completa
4. Inicia tracking do novo arquivo (position: 0)
5. Cleanup da position antiga após retention period
```

#### 3. **✂️ Truncamento de Arquivo**:
```
Antes: /var/log/app.log (1MB) - position: 800KB
Ação:  > /var/log/app.log (truncate)
Depois: /var/log/app.log (0KB) - position: 800KB (inválido!)

Fluxo:
1. Monitor detecta size < position
2. Log warning sobre truncamento detectado
3. Reset position para 0
4. Reinicia leitura do início
5. Marca evento de truncamento nas métricas
```

#### 4. **🗑️ Remoção de Arquivo**:
```
Estado: /var/log/app.log - sendo monitorado
Ação:  rm /var/log/app.log

Fluxo:
1. Monitor detecta erro de acesso ao arquivo
2. Inicia grace period de 5 minutos
3. Se arquivo não retorna, marca position para cleanup
4. Remove da lista de monitoramento
5. Cleanup position após período configurado
```

#### 5. **📁 Arquivo Renomeado**:
```
Antes: /var/log/app.log (sendo monitorado)
Ação:  mv /var/log/app.log /var/log/app-backup.log

Fluxo:
1. Monitor perde acesso ao path original
2. Se o arquivo está no mesmo diretório e incluído nos patterns:
   - Detecta como "novo" arquivo
   - Verifica inode para evitar duplicação
   - Continua leitura da position correta
3. Se não está incluído: trata como remoção
```

### 🐳 **Ciclo de Vida de Containers**:

#### 1. **🟢 Container Executando Normalmente**:
```
Estado: web-app-1 (running) - logs streaming

Fluxo:
1. Container Monitor mantém connection via Docker API
2. Streaming contínuo de stdout/stderr
3. Position atualizada a cada log (timestamp-based)
4. Flush periódico das positions
5. Health check da connection
```

#### 2. **🔄 Container Reiniciado**:
```
Antes: web-app-1 (container_id: abc123) - última position: T1
Ação:  docker restart web-app-1
Depois: web-app-1 (container_id: def456) - position: T0

Fluxo:
1. Monitor detecta disconnect da stream
2. Detecta novo container_id via Docker events
3. Finaliza position do container antigo
4. Inicia novo tracking com novo container_id
5. Position baseada em timestamp de início
6. Manutenção de ambas positions por período configurado
```

#### 3. **⏹️ Container Parado**:
```
Estado: web-app-1 (running)
Ação:  docker stop web-app-1
Resultado: web-app-1 (stopped)

Fluxo:
1. Monitor detecta fim da stream
2. Finaliza leitura de logs restantes
3. Marca position como "parada"
4. Mantém position por 24h (configurável)
5. Cleanup automático após retention
```

#### 4. **🗑️ Container Removido**:
```
Estado: web-app-1 (stopped)
Ação:  docker rm web-app-1
Resultado: container não existe

Fluxo:
1. Monitor detecta que container não existe mais
2. Finaliza position imediatamente
3. Remove da lista de monitoramento
4. Cleanup de positions órfãs
5. Métricas de containers removidos
```

#### 5. **🆕 Container Criado**:
```
Ação:  docker run --name web-app-2 nginx
Resultado: novo container detectado

Fluxo:
1. Monitor detecta novo container via Docker events
2. Verifica contra filtros (include/exclude names/labels)
3. Se incluído: inicia streaming
4. Nova position criada com timestamp atual
5. Opcionalmente lê tail_lines configurado
```

#### 6. **🔄 Container Atualizado (recreado)**:
```
Antes: web-app:v1 (abc123)
Ação:  docker-compose up --force-recreate
Depois: web-app:v1 (def456) - mesmo name, novo ID

Fluxo:
1. Monitor detecta remoção do container antigo
2. Detecta criação de novo container com mesmo nome
3. Verifica se é "update" baseado em labels/timing
4. Pode preservar context se configurado
5. Nova position para novo container
```

### ⏰ **Gestão de Timestamps**:

#### 1. **📅 Logs com Timestamp Futuro**:
```
Log recebido: timestamp=2024-01-15T15:00:00Z
Tempo atual:  timestamp=2024-01-15T10:00:00Z
Drift: +5 horas (> limite de 1 minuto)

Fluxo:
1. TimestampValidator detecta timestamp futuro
2. Log warning sobre drift temporal
3. Aplica ação configurada:
   - clamp: ajusta para tempo atual
   - reject: descarta log
   - warn: permite mas alerta
4. Atualiza métricas de timestamps inválidos
5. Opcionalmente envia para DLQ com contexto
```

#### 2. **🕰️ Logs com Timestamp Muito Antigo**:
```
Log recebido: timestamp=2024-01-14T02:00:00Z
Tempo atual:  timestamp=2024-01-15T10:00:00Z
Drift: -32 horas (> limite de 6 horas)

Fluxo:
1. Validator detecta timestamp muito antigo
2. Verifica se é log de arquivo rotacionado vs. erro
3. Aplica ação baseada na configuração
4. Logs de arquivo: mais tolerante (pode ser rotação)
5. Logs de container: mais restritivo (erro provável)
```

#### 3. **🔄 Logs Sem Timestamp**:
```
Log recebido: message="Error occurred" (sem timestamp)

Fluxo:
1. Sistema detecta ausência de timestamp
2. Aplica timestamp atual automaticamente
3. Marca na estrutura que timestamp foi inferido
4. Processa normalmente
5. Métricas de logs com timestamp inferido
```

---

## 📈 Escalabilidade

### 🏗️ **Scaling Vertical (Single Instance)**:

#### **Para Muitos Arquivos (1000+ files)**:
```yaml
file_monitor_service:
  poll_interval: "10s"        # Reduzir de 30s
  read_buffer_size: 131072    # Aumentar buffer (128KB)
  max_concurrent_files: 500   # Limitar concorrência

dispatcher:
  worker_count: 16            # Aumentar workers
  queue_size: 500000          # Aumentar queue
  batch_size: 300             # Aumentar batch
```

#### **Para Muitos Containers (500+ containers)**:
```yaml
container_monitor:
  max_concurrent: 100         # Aumentar concorrência
  reconnect_interval: "10s"   # Reduzir intervalo

dispatcher:
  worker_count: 20            # Mais workers para containers
  queue_size: 1000000         # Queue maior
```

### 🌐 **Scaling Horizontal (Multiple Instances)**:

#### **Estratégia por Sharding**:
```yaml
# Instance 1: Files only
file_monitor_service:
  enabled: true
container_monitor:
  enabled: false

# Instance 2: Containers only
file_monitor_service:
  enabled: false
container_monitor:
  enabled: true
```

#### **Estratégia por Path/Label**:
```yaml
# Instance 1: /var/log/app1/*
files_config:
  watch_directories: ["/var/log/app1"]

# Instance 2: /var/log/app2/*
files_config:
  watch_directories: ["/var/log/app2"]
```

### ⚡ **Optimizações de Performance**:

#### **Memory Tuning**:
```yaml
# Para high-volume environments
positions:
  max_memory_buffer: 10000      # Mais positions em memória
  flush_interval: "5s"          # Flush mais frequente

deduplication_config:
  max_cache_size: 500000        # Cache maior
  cleanup_interval: "5m"        # Cleanup mais frequente
```

#### **Disk I/O Optimization**:
```yaml
# Para SSDs
file_monitor_service:
  read_buffer_size: 262144      # 256KB buffer
  read_interval: "50ms"         # Leitura mais frequente

# Para HDDs
file_monitor_service:
  read_buffer_size: 65536       # 64KB buffer
  read_interval: "200ms"        # Leitura menos frequente
```

#### **Network Optimization**:
```yaml
sinks:
  loki:
    batch_size: 1000            # Batches maiores
    batch_timeout: "2s"         # Timeout menor
    max_request_size: 2097152   # 2MB requests
    queue_size: 100000          # Queue maior
```

### 📊 **Capacity Planning**:

#### **Estimativas de Recursos**:

**Por 1000 arquivos ativos**:
- CPU: ~0.5 cores
- Memory: ~500MB
- Disk I/O: ~50 IOPS
- Network: ~1MB/s (depends on log volume)

**Por 100 containers ativos**:
- CPU: ~0.3 cores
- Memory: ~200MB
- Network: ~2MB/s (streaming)

**Fórmulas de Sizing**:
```
CPU cores = (files/2000) + (containers/300) + 2
Memory MB = (files * 0.5) + (containers * 2) + 1024
Disk GB = log_retention_days * daily_log_volume_gb * 1.2
```

### 🚨 **Monitoring para Scale**:

**Key Metrics**:
- Queue utilization (target: <80%)
- Processing latency (target: <500ms p95)
- Memory usage (target: <80%)
- CPU usage (target: <70%)
- Disk I/O wait (target: <20%)

**Scale Triggers**:
- Queue util > 85% for 5min → Scale up workers
- Latency > 1s p95 for 5min → Scale up resources
- Memory > 90% → Scale up instance or shard
- Error rate > 2% → Investigate before scaling

---

## ⚙️ Configuração e Personalização

### 🎯 **Configuração por Ambiente**:

#### **Desenvolvimento**:
```yaml
app:
  log_level: "debug"
  environment: "development"

dispatcher:
  worker_count: 2
  queue_size: 1000
  batch_size: 10

container_monitor:
  max_concurrent: 5

sinks:
  loki:
    batch_size: 50
    batch_timeout: "1s"
```

#### **Produção**:
```yaml
app:
  log_level: "info"
  environment: "production"

dispatcher:
  worker_count: 8
  queue_size: 100000
  batch_size: 150

container_monitor:
  max_concurrent: 25

sinks:
  loki:
    batch_size: 800
    batch_timeout: "8s"
```

#### **High Volume**:
```yaml
app:
  log_level: "warn"
  environment: "production"

dispatcher:
  worker_count: 16
  queue_size: 500000
  batch_size: 500

container_monitor:
  max_concurrent: 50

sinks:
  loki:
    batch_size: 1000
    batch_timeout: "5s"
    queue_size: 100000
```

### 🔧 **Personalização por Casos de Uso**:

#### **Logs de Aplicação Web**:
```yaml
files_config:
  watch_directories: ["/var/log/nginx", "/var/log/apache"]
  include_patterns: ["access.log", "error.log"]

processing:
  pipelines_file: "web_pipelines.yaml"  # Pipeline customizado
```

**Pipeline Example**:
```yaml
pipelines:
  - name: "nginx_access"
    pattern: "access.log"
    processors:
      - type: "regex_parser"
        regex: '^(\S+) \S+ \S+ \[([^\]]+)\] "([^"]*)" (\d+) (\d+)'
        fields: ["ip", "timestamp", "request", "status", "size"]
      - type: "geoip"
        source: "ip"
        target: "geo"
```

#### **Logs de Database**:
```yaml
container_monitor:
  include_labels:
    app: "database"
  exclude_names: []

processing:
  pipelines_file: "db_pipelines.yaml"
```

#### **Logs de Microservices**:
```yaml
container_monitor:
  include_labels:
    service.type: "microservice"
  include_stdout: true
  include_stderr: true

sinks:
  loki:
    default_labels:
      environment: "prod"
      cluster: "k8s-prod"
```

### 🎛️ **Tuning Guidelines**:

#### **Para Baixo Volume (<1000 logs/min)**:
```yaml
dispatcher:
  batch_size: 10
  batch_timeout: "30s"
  worker_count: 2

file_monitor_service:
  poll_interval: "60s"

container_monitor:
  max_concurrent: 10
```

#### **Para Alto Volume (>10000 logs/min)**:
```yaml
dispatcher:
  batch_size: 500
  batch_timeout: "2s"
  worker_count: 16

file_monitor_service:
  poll_interval: "5s"
  read_buffer_size: 262144

container_monitor:
  max_concurrent: 50
```

### 🔐 **Configuração de Segurança**:

#### **Produção Segura**:
```yaml
server:
  host: "127.0.0.1"  # Bind only localhost
  port: 8401

metrics:
  enabled: true
  port: 8001         # Separate metrics port

container_monitor:
  socket_path: "unix:///var/run/docker.sock"  # Unix socket only

sinks:
  loki:
    tls:
      enabled: true
      verify_certificate: true
    auth:
      type: "basic"  # ou "bearer"
```

### 📋 **Configuração de Compliance**:

#### **GDPR/Privacy**:
```yaml
processing:
  enabled: true
  pipelines_file: "privacy_pipelines.yaml"

# Pipeline para redação de dados sensíveis
privacy_pipelines.yaml:
pipelines:
  - name: "privacy_filter"
    processors:
      - type: "redact"
        patterns: ["\\b\\d{3}-\\d{2}-\\d{4}\\b"]  # SSN
        replacement: "[REDACTED]"
      - type: "hash_pii"
        fields: ["email", "ip"]
```

#### **Audit Requirements**:
```yaml
sinks:
  local_file:
    enabled: true
    directory: "/audit/logs"
    filename_pattern: "audit-{date}-{hour}.log"
    retention_days: 2555  # 7 anos
```

---

## 🎯 Conclusão

O **log_capturer_go** é um sistema enterprise-grade de agregação de logs que oferece:

### ✅ **Pontos Fortes**:
- **Robustez**: Circuit breakers, DLQ, backpressure
- **Performance**: Processamento assíncrono, batching inteligente
- **Observabilidade**: Métricas detalhadas, health checks
- **Escalabilidade**: Suporte a milhares de sources
- **Flexibilidade**: Configuração granular, pipelines customizáveis

### 🔧 **Recomendações de Melhoria**:
1. **Configuração Externa**: Mover timestamp validation para config.yaml
2. **Service Discovery**: Auto-descoberta de containers/services
3. **Hot Reload**: Reload de configuração sem restart
4. **Advanced Processing**: ML-based anomaly detection
5. **Multi-tenant**: Suporte a múltiplos tenants

### 📊 **Métricas de Qualidade**:
- **Reliability**: 99.9% uptime target
- **Performance**: <500ms latency p95
- **Scalability**: 10K+ sources suportadas
- **Observability**: 360° monitoring coverage

O sistema está **production-ready** e atende aos requisitos enterprise para captura, processamento e agregação de logs em ambientes de alta demanda.

---

## 📚 Recursos Adicionais

- **Logs**: `/app/logs/` - Logs da aplicação
- **Metrics**: `http://localhost:8001/metrics` - Métricas Prometheus
- **Health**: `http://localhost:8401/health` - Health check
- **Grafana**: `http://localhost:3000` - Dashboards
- **Loki**: `http://localhost:3100` - Query interface

---

*Documentação gerada automaticamente pelo code review completo do log_capturer_go v0.0.2*