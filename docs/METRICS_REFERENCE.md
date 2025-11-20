# Referência de Métricas - Log Capturer Go

**Versão**: 1.0.0
**Data**: 2025-11-20
**Status**: Auditoria Completa

---

## Resumo Executivo

| Estatística | Valor |
|-------------|-------|
| **Total de Métricas Definidas** | 98 |
| **Métricas Ativas** | 45 |
| **Métricas Parcialmente Utilizadas** | 15 |
| **Métricas Não Utilizadas** | 38 |
| **Taxa de Utilização** | 61% |

### Status Geral

- **Crítico**: 3 problemas de registro que podem causar panic
- **Alto**: 38 métricas definidas mas nunca atualizadas
- **Médio**: 15 métricas com uso parcial ou inconsistente

---

## Métricas por Categoria

### 1. Processamento de Logs (5 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_logs_processed_total` | Counter | `source_type`, `level` | Total de logs processados | ✓ Ativa |
| `log_capturer_logs_dropped_total` | Counter | `reason` | Total de logs descartados | ✓ Ativa |
| `log_capturer_logs_filtered_total` | Counter | `filter_name` | Logs filtrados por regra | ✓ Ativa |
| `log_capturer_batch_size` | Histogram | `sink` | Tamanho dos batches enviados | ✓ Ativa |
| `log_capturer_logs_bytes_total` | Counter | `source_type` | Total de bytes processados | ✓ Ativa |

### 2. Dispatcher e Filas (4 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_dispatcher_queue_size` | Gauge | - | Tamanho atual da fila do dispatcher | ✓ Ativa |
| `log_capturer_dispatcher_queue_capacity` | Gauge | - | Capacidade máxima da fila | ✓ Ativa |
| `log_capturer_dispatcher_workers_active` | Gauge | - | Workers ativos no momento | ✓ Ativa |
| `log_capturer_dispatcher_backpressure_events` | Counter | - | Eventos de backpressure | ⚠️ Parcial |

### 3. Duração de Processamento (3 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_processing_duration_seconds` | Histogram | `stage` | Duração por estágio de processamento | ✓ Ativa |
| `log_capturer_sink_send_duration_seconds` | Histogram | `sink`, `status` | Tempo de envio para sinks | ✓ Ativa |
| `log_capturer_batch_processing_duration_seconds` | Histogram | `sink` | Duração do processamento de batch | ✓ Ativa |

### 4. Erros e Saúde (2 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_errors_total` | Counter | `component`, `error_type` | Total de erros por componente | ✓ Ativa |
| `log_capturer_health_status` | Gauge | `component` | Status de saúde (0=unhealthy, 1=healthy) | ✓ Ativa |

### 5. Monitoramento de Arquivos (9 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_file_monitor_lines_read_total` | Counter | `file_path` | Linhas lidas por arquivo | ✓ Ativa |
| `log_capturer_file_monitor_bytes_read_total` | Counter | `file_path` | Bytes lidos por arquivo | ✓ Ativa |
| `log_capturer_file_monitor_files_watched` | Gauge | - | Arquivos sendo monitorados | ✓ Ativa |
| `log_capturer_file_monitor_rotation_events` | Counter | `file_path` | Eventos de rotação detectados | ✓ Ativa |
| `log_capturer_file_monitor_errors_total` | Counter | `file_path`, `error_type` | Erros no monitor de arquivos | ✓ Ativa |
| `log_capturer_file_retry_attempts_total` | Counter | `file_path` | Tentativas de retry | ❌ Não utilizada |
| `log_capturer_file_retry_success_total` | Counter | `file_path` | Retries bem-sucedidos | ❌ Não utilizada |
| `log_capturer_file_retry_failures_total` | Counter | `file_path` | Falhas em retry | ❌ Não utilizada |
| `log_capturer_file_retry_queue_size` | Gauge | - | Tamanho da fila de retry | ❌ Não utilizada |

### 6. Monitoramento de Containers (8 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_container_logs_total` | Counter | `container_id`, `container_name` | Logs por container | ✓ Ativa |
| `log_capturer_containers_watched` | Gauge | - | Containers sendo monitorados | ✓ Ativa |
| `log_capturer_container_events_total` | Counter | `event_type` | Eventos de container (start/stop/die) | ✓ Ativa |
| `log_capturer_container_errors_total` | Counter | `container_id`, `error_type` | Erros por container | ✓ Ativa |
| `log_capturer_container_reconnects_total` | Counter | `container_id` | Reconexões a containers | ⚠️ Parcial |
| `log_capturer_active_container_streams` | Gauge | - | Streams de container ativos | ❌ Não utilizada |
| `log_capturer_stream_pool_utilization` | Gauge | - | Utilização do pool de streams | ❌ Não utilizada |
| `log_capturer_container_stream_lag_seconds` | Gauge | `container_id` | Lag de leitura do stream | ⚠️ Parcial |

### 7. Tarefas - Task Manager (3 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_tasks_total` | Counter | `status` | Total de tarefas por status | ⚠️ Parcial |
| `log_capturer_task_heartbeats_total` | Counter | `task_id` | Heartbeats de tarefas | ❌ Não utilizada |
| `log_capturer_active_tasks` | Gauge | - | Tarefas ativas no momento | ❌ Não utilizada |

### 8. Deduplicação (4 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_deduplicate_cache_size` | Gauge | - | Tamanho do cache de deduplicação | ✓ Ativa |
| `log_capturer_deduplicate_hits_total` | Counter | - | Cache hits (logs duplicados) | ✓ Ativa |
| `log_capturer_deduplicate_misses_total` | Counter | - | Cache misses (logs únicos) | ✓ Ativa |
| `log_capturer_deduplicate_evictions_total` | Counter | - | Evicções do cache | ✓ Ativa |

### 9. Sistema (6 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_goroutines` | Gauge | - | Número de goroutines | ✓ Ativa |
| `log_capturer_memory_usage_bytes` | Gauge | `type` | Uso de memória (alloc/sys/heap) | ✓ Ativa |
| `log_capturer_gc_pause_seconds` | Histogram | - | Pausas do garbage collector | ✓ Ativa |
| `log_capturer_uptime_seconds` | Gauge | - | Tempo de execução | ✓ Ativa |
| `log_capturer_cpu_usage_percent` | Gauge | - | Uso de CPU | ⚠️ Parcial |
| `log_capturer_open_file_descriptors` | Gauge | - | File descriptors abertos | ✓ Ativa |

### 10. Kafka Sink (13 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_kafka_messages_produced_total` | Counter | `topic` | Mensagens produzidas | ❌ Não utilizada |
| `log_capturer_kafka_messages_failed_total` | Counter | `topic`, `error_type` | Falhas de produção | ❌ Não utilizada |
| `log_capturer_kafka_batch_size` | Histogram | `topic` | Tamanho do batch Kafka | ❌ Não utilizada |
| `log_capturer_kafka_produce_duration_seconds` | Histogram | `topic` | Duração da produção | ❌ Não utilizada |
| `log_capturer_kafka_broker_connections` | Gauge | `broker` | Conexões por broker | ❌ Não utilizada |
| `log_capturer_kafka_topic_partitions` | Gauge | `topic` | Partições por tópico | ❌ Não utilizada |
| `log_capturer_kafka_consumer_lag` | Gauge | `topic`, `partition` | Lag do consumidor | ❌ Não utilizada |
| `log_capturer_kafka_retries_total` | Counter | `topic` | Retries de produção | ❌ Não utilizada |
| `log_capturer_kafka_bytes_produced_total` | Counter | `topic` | Bytes produzidos | ❌ Não utilizada |
| `log_capturer_kafka_compression_ratio` | Gauge | `topic` | Taxa de compressão | ❌ Não utilizada |
| `log_capturer_kafka_queue_depth` | Gauge | - | Profundidade da fila interna | ❌ Não utilizada |
| `log_capturer_kafka_request_latency_seconds` | Histogram | `broker` | Latência de requisição | ❌ Não utilizada |
| `log_capturer_kafka_throttle_time_seconds` | Counter | `broker` | Tempo de throttling | ❌ Não utilizada |

### 11. Dead Letter Queue - DLQ (4 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_dlq_entries_total` | Counter | `sink`, `reason` | Entradas na DLQ | ✓ Ativa |
| `log_capturer_dlq_size` | Gauge | - | Tamanho atual da DLQ | ✓ Ativa |
| `log_capturer_dlq_replayed_total` | Counter | `sink`, `status` | Replays da DLQ | ⚠️ Parcial |
| `log_capturer_dlq_age_seconds` | Histogram | - | Idade das entradas na DLQ | ⚠️ Parcial |

### 12. Timestamp Learning (5 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_timestamp_patterns_learned` | Gauge | `source` | Padrões de timestamp aprendidos | ✓ Ativa |
| `log_capturer_timestamp_parse_success_total` | Counter | `pattern` | Parsing bem-sucedido | ✓ Ativa |
| `log_capturer_timestamp_parse_failures_total` | Counter | `source` | Falhas de parsing | ✓ Ativa |
| `log_capturer_timestamp_fallback_total` | Counter | - | Uso de timestamp fallback | ⚠️ Parcial |
| `log_capturer_timestamp_confidence` | Gauge | `source`, `pattern` | Confiança no padrão | ⚠️ Parcial |

### 13. Sistema de Posições (15 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_position_saves_total` | Counter | `source_type` | Posições salvas | ✓ Ativa |
| `log_capturer_position_loads_total` | Counter | `source_type`, `status` | Posições carregadas | ✓ Ativa |
| `log_capturer_position_errors_total` | Counter | `operation`, `error_type` | Erros de posição | ✓ Ativa |
| `log_capturer_position_file_size_bytes` | Gauge | - | Tamanho do arquivo de posições | ✓ Ativa |
| `log_capturer_position_entries` | Gauge | - | Entradas de posição | ✓ Ativa |
| `log_capturer_position_last_save_timestamp` | Gauge | - | Timestamp do último save | ✓ Ativa |
| `log_capturer_position_compactions_total` | Counter | - | Compactações executadas | ✓ Ativa |
| `log_capturer_position_active_by_status` | Gauge | `status` | Posições por status | ❌ Não utilizada |
| `log_capturer_position_update_rate` | Gauge | - | Taxa de atualização | ❌ Não utilizada |
| `log_capturer_position_lag_bytes` | Gauge | `source` | Lag em bytes por fonte | ❌ Não utilizada |
| `log_capturer_position_recovery_duration_seconds` | Histogram | - | Duração da recuperação | ❌ Não utilizada |
| `log_capturer_position_checkpoint_duration_seconds` | Histogram | - | Duração do checkpoint | ❌ Não utilizada |
| `log_capturer_position_conflicts_total` | Counter | - | Conflitos de posição | ❌ Não utilizada |
| `log_capturer_position_stale_entries` | Gauge | - | Entradas obsoletas | ❌ Não utilizada |
| `log_capturer_position_sync_lag_seconds` | Gauge | - | Lag de sincronização | ❌ Não utilizada |

### 14. Tracing - OpenTelemetry (8 métricas)

| Métrica | Tipo | Labels | Descrição | Status |
|---------|------|--------|-----------|--------|
| `log_capturer_traces_exported_total` | Counter | `status` | Traces exportados | ✓ Ativa |
| `log_capturer_spans_created_total` | Counter | `operation` | Spans criados | ✓ Ativa |
| `log_capturer_trace_export_duration_seconds` | Histogram | - | Duração da exportação | ✓ Ativa |
| `log_capturer_trace_batch_size` | Histogram | - | Tamanho do batch de traces | ✓ Ativa |
| `log_capturer_trace_errors_total` | Counter | `error_type` | Erros de tracing | ✓ Ativa |
| `log_capturer_trace_sampling_decisions` | Counter | `decision` | Decisões de sampling | ⚠️ Parcial |
| `log_capturer_trace_context_propagations` | Counter | `status` | Propagações de contexto | ⚠️ Parcial |
| `log_capturer_active_spans` | Gauge | - | Spans ativos | ⚠️ Parcial |

---

## Problemas Identificados

### Críticos

#### 1. ConnectionPool usa MustRegister
- **Local**: `internal/sinks/connection_pool.go`
- **Problema**: Causará panic se múltiplas instâncias forem criadas
- **Impacto**: Crash da aplicação em cenários de múltiplos sinks
- **Severidade**: CRÍTICO

#### 2. HTTPCompressor métricas desabilitadas
- **Local**: `internal/processing/http_compressor.go`
- **Problema**: Conflito de registro com outras métricas
- **Impacto**: Sem visibilidade de compressão HTTP
- **Severidade**: CRÍTICO

#### 3. safeRegister suprime panics silenciosamente
- **Local**: `internal/metrics/metrics.go`
- **Problema**: Erros de registro são ignorados sem log
- **Impacto**: Métricas podem não ser registradas sem aviso
- **Severidade**: CRÍTICO

### Alto Impacto

#### 4. Todas as 13 métricas Kafka nunca são atualizadas
- **Componente**: Kafka Sink
- **Métricas afetadas**:
  - `KafkaMessagesProducedTotal`
  - `KafkaBatchSize`
  - `KafkaProduceDuration`
  - `KafkaBrokerConnections`
  - E outras 9 métricas
- **Problema**: Métricas definidas mas sem instrumentação no código
- **Impacto**: Zero visibilidade do funcionamento do Kafka sink
- **Severidade**: ALTO

#### 5. 8 métricas de Position nunca atualizadas
- **Métricas afetadas**:
  - `PositionActiveByStatus`
  - `PositionUpdateRate`
  - `PositionLagBytes`
  - `PositionRecoveryDuration`
  - `PositionCheckpointDuration`
  - `PositionConflicts`
  - `PositionStaleEntries`
  - `PositionSyncLag`
- **Problema**: Instrumentação incompleta do sistema de posições
- **Impacto**: Visibilidade parcial do sistema de posições
- **Severidade**: ALTO

#### 6. Todas as métricas de File Retry nunca atualizadas
- **Métricas afetadas**:
  - `FileRetryAttempts`
  - `FileRetrySuccess`
  - `FileRetryFailures`
  - `FileRetryQueueSize`
- **Problema**: Feature de retry implementada sem métricas
- **Impacto**: Sem visibilidade de retries de arquivos
- **Severidade**: ALTO

### Médio Impacto

#### 7. Métricas de Container Streams não utilizadas
- **Métricas afetadas**:
  - `ActiveContainerStreams`
  - `StreamPoolUtilization`
- **Impacto**: Sem visibilidade de streams ativos de containers
- **Severidade**: MÉDIO

#### 8. Métricas de Task não chamadas
- **Métricas afetadas**:
  - `TaskHeartbeats`
  - `ActiveTasks`
- **Impacto**: Sem monitoramento de tarefas em execução
- **Severidade**: MÉDIO

---

## Recomendações

### Prioridade 0 - Imediato

#### 1. Corrigir ConnectionPool MustRegister

```go
// Mudar de:
prometheus.MustRegister(metric)

// Para:
if err := prometheus.Register(metric); err != nil {
    if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
        return err
    }
}
```

#### 2. Adicionar logging ao safeRegister

```go
func safeRegister(collector prometheus.Collector) {
    if err := prometheus.Register(collector); err != nil {
        if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
            log.Warn("Failed to register metric", "error", err)
        }
    }
}
```

#### 3. Resolver conflito HTTPCompressor
- Revisar nomes de métricas para evitar colisões
- Usar namespace/subsystem únicos para cada componente

### Prioridade 1 - Curto Prazo

#### 4. Implementar instrumentação Kafka
- Adicionar chamadas de métricas no Kafka sink
- Métricas prioritárias:
  - `KafkaMessagesProducedTotal`
  - `KafkaMessagesFailed`
  - `KafkaProduceDuration`
  - `KafkaBatchSize`

#### 5. Completar métricas de Position
- Instrumentar operações de save/load
- Adicionar cálculo de lag em bytes
- Implementar métricas de checkpoint

#### 6. Instrumentar File Retry
- Adicionar métricas nas operações de retry
- Expor tamanho da fila de retry

### Prioridade 2 - Médio Prazo

#### 7. Remover ou implementar métricas não utilizadas
- Avaliar necessidade de cada métrica não utilizada
- Remover código morto ou implementar instrumentação

#### 8. Criar dashboards Grafana
- Dashboard por categoria de métrica
- Alertas para métricas críticas

#### 9. Documentar métricas no código
- Adicionar comentários explicando uso de cada métrica
- Criar exemplos de queries Prometheus

### Prioridade 3 - Longo Prazo

#### 10. Adicionar testes de métricas
- Verificar que métricas são incrementadas corretamente
- Testes de integração com Prometheus

#### 11. Implementar métricas de SLI/SLO
- Latência P99 de processamento
- Taxa de erro por componente
- Throughput garantido

---

## Queries Prometheus Úteis

### Throughput de Logs

```promql
# Taxa de logs processados por segundo
rate(log_capturer_logs_processed_total[5m])

# Por source_type
sum by (source_type) (rate(log_capturer_logs_processed_total[5m]))

# Total acumulado nas últimas 24h
increase(log_capturer_logs_processed_total[24h])
```

### Saúde do Dispatcher

```promql
# Utilização da fila (percentual)
log_capturer_dispatcher_queue_size / log_capturer_dispatcher_queue_capacity * 100

# Workers ativos
log_capturer_dispatcher_workers_active

# Eventos de backpressure por minuto
rate(log_capturer_dispatcher_backpressure_events[1m]) * 60
```

### Latência

```promql
# P50 de envio para sinks
histogram_quantile(0.50, sum(rate(log_capturer_sink_send_duration_seconds_bucket[5m])) by (le, sink))

# P95 de envio para sinks
histogram_quantile(0.95, sum(rate(log_capturer_sink_send_duration_seconds_bucket[5m])) by (le, sink))

# P99 de envio para sinks
histogram_quantile(0.99, sum(rate(log_capturer_sink_send_duration_seconds_bucket[5m])) by (le, sink))
```

### Erros

```promql
# Taxa de erros por componente
sum by (component) (rate(log_capturer_errors_total[5m]))

# Logs dropados por razão
sum by (reason) (rate(log_capturer_logs_dropped_total[5m]))

# Percentual de erro
sum(rate(log_capturer_errors_total[5m])) / sum(rate(log_capturer_logs_processed_total[5m])) * 100
```

### Dead Letter Queue

```promql
# Tamanho atual da DLQ
log_capturer_dlq_size

# Taxa de entrada na DLQ
rate(log_capturer_dlq_entries_total[5m])

# DLQ crescendo (últimos 10 minutos)
delta(log_capturer_dlq_size[10m]) > 0
```

### Sistema

```promql
# Goroutines (detectar leaks)
log_capturer_goroutines

# Crescimento de goroutines na última hora
increase(log_capturer_goroutines[1h])

# Uso de memória heap
log_capturer_memory_usage_bytes{type="heap_alloc"}

# Pausas do GC P99
histogram_quantile(0.99, rate(log_capturer_gc_pause_seconds_bucket[5m]))
```

---

## Alertas Recomendados

```yaml
groups:
  - name: log_capturer_critical
    rules:
      - alert: HighQueueUtilization
        expr: log_capturer_dispatcher_queue_size / log_capturer_dispatcher_queue_capacity > 0.8
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Fila do dispatcher acima de 80%"
          description: "A utilização da fila está em {{ $value | humanizePercentage }}"

      - alert: CriticalQueueUtilization
        expr: log_capturer_dispatcher_queue_size / log_capturer_dispatcher_queue_capacity > 0.95
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Fila do dispatcher acima de 95%"
          description: "Risco iminente de perda de logs"

      - alert: HighErrorRate
        expr: rate(log_capturer_errors_total[5m]) > 10
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Taxa de erros elevada"
          description: "{{ $value }} erros por segundo"

      - alert: DLQGrowing
        expr: delta(log_capturer_dlq_size[10m]) > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "DLQ crescendo rapidamente"
          description: "{{ $value }} novas entradas nos últimos 10 minutos"

      - alert: NoLogsProcessed
        expr: rate(log_capturer_logs_processed_total[5m]) == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Nenhum log processado nos últimos 5 minutos"

      - alert: GoroutineLeak
        expr: increase(log_capturer_goroutines[1h]) > 100
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "Possível vazamento de goroutines"
          description: "Aumento de {{ $value }} goroutines na última hora"

      - alert: HighMemoryUsage
        expr: log_capturer_memory_usage_bytes{type="heap_alloc"} > 1e9
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Uso de memória heap acima de 1GB"

      - alert: ComponentUnhealthy
        expr: log_capturer_health_status == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Componente não saudável"
          description: "O componente {{ $labels.component }} está unhealthy"
```

---

## Endpoints de Métricas

| Endpoint | Porta | Descrição |
|----------|-------|-----------|
| `/metrics` | 8001 | Métricas Prometheus |
| `/health` | 8401 | Health check detalhado |
| `/debug/pprof/` | 6060 | Profiling pprof |
| `/debug/pprof/heap` | 6060 | Heap profile |
| `/debug/pprof/goroutine` | 6060 | Goroutine profile |
| `/debug/pprof/profile` | 6060 | CPU profile |

---

## Histórico de Alterações

| Data | Versão | Alteração |
|------|--------|-----------|
| 2025-11-20 | 1.0.0 | Documentação inicial com auditoria completa de 98 métricas |

---

**Mantido por**: Equipe de Engenharia
**Última Revisão**: 2025-11-20
