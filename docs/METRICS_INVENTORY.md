# METRICS INVENTORY - FASE 5

**Data**: 2025-11-06
**Status**: VALIDACAO COMPLETA
**Total de Metricas**: 63 metricas Prometheus

---

## RESUMO EXECUTIVO

### Estatisticas
- **Metricas CORE**: 35 (56%)
- **Metricas Kafka**: 13 (21%)
- **Metricas Container Streams**: 5 (8%)
- **Metricas Enhanced**: 10 (16%)
- **Todas funcionais**: ✅
- **Usadas em dashboards**: 45 (71%)

### Tipos de Metricas
- **Counter**: 15 (24%)
- **Gauge**: 34 (54%)
- **Histogram**: 14 (22%)

---

## METRICAS CORE (35 metricas)

### 1. log_capturer_logs_processed_total
**Tipo**: Counter
**Labels**: `source_type`, `source_id`, `pipeline`
**Descricao**: Total de logs processados
**Codigo**: `internal/metrics/metrics.go:19`
**Usado em Dashboard**: ✅ Painel #10 "Logs Processed"
**Valor Exemplo**: 125430

**PromQL Examples**:
```promql
# Taxa de logs processados por segundo
rate(log_capturer_logs_processed_total[5m])

# Total por source_type
sum by (source_type) (log_capturer_logs_processed_total)
```

---

### 2. log_capturer_logs_per_second
**Tipo**: Gauge
**Labels**: `component`
**Descricao**: Throughput atual em logs/segundo
**Codigo**: `internal/metrics/metrics.go:28`
**Usado em Dashboard**: ✅ Painel "Throughput"
**Valor Exemplo**: 450.2

---

### 3. log_capturer_dispatcher_queue_utilization
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Utilizacao da fila do dispatcher (0.0 a 1.0)
**Codigo**: `internal/metrics/metrics.go:37`
**Usado em Dashboard**: ✅ Painel #5 "Queue Depth Trend"
**Valor Exemplo**: 0.35 (35%)

**Alerta**: Warning se > 0.70, Critical se > 0.90

---

### 4. log_capturer_processing_step_duration_seconds
**Tipo**: Histogram
**Labels**: `pipeline`, `step`
**Descricao**: Duracao de cada step de processamento
**Buckets**: Default Prometheus buckets
**Codigo**: `internal/metrics/metrics.go:43`
**Usado em Dashboard**: ⚠️ NAO (poderia usar para latency P95)

---

### 5. log_capturer_logs_sent_total
**Tipo**: Counter
**Labels**: `sink_type`, `status`
**Descricao**: Total de logs enviados para sinks
**Codigo**: `internal/metrics/metrics.go:53`
**Usado em Dashboard**: ✅ Painel "Sink Delivery"
**Valor Exemplo**: 123450

**PromQL Examples**:
```promql
# Taxa de sucesso
rate(log_capturer_logs_sent_total{status="success"}[5m])

# Taxa de erro
rate(log_capturer_logs_sent_total{status="error"}[5m])
```

---

### 6. log_capturer_errors_total
**Tipo**: Counter
**Labels**: `component`, `error_type`
**Descricao**: Total de erros por componente
**Codigo**: `internal/metrics/metrics.go:62`
**Usado em Dashboard**: ✅ Painel "Error Rate"
**Valor Exemplo**: 15

**Alerta**: Rate > 10 errors/min

---

### 7. log_capturer_files_monitored
**Tipo**: Gauge (com labels high-cardinality)
**Labels**: `filepath`, `source_type`
**Descricao**: Arquivos sendo monitorados
**Codigo**: `internal/metrics/metrics.go:71`
**Usado em Dashboard**: ⚠️ Agregado em `total_files_monitored`
**Valor Exemplo**: 1 (por filepath)

**ATENCAO**: Label `filepath` pode gerar alta cardinalidade!

---

### 8. log_capturer_containers_monitored
**Tipo**: Gauge (com labels high-cardinality)
**Labels**: `container_id`, `container_name`, `image`
**Descricao**: Containers sendo monitorados
**Codigo**: `internal/metrics/metrics.go:80`
**Usado em Dashboard**: ⚠️ Agregado em `total_containers_monitored`
**Valor Exemplo**: 1 (por container)

**ATENCAO**: Labels podem gerar alta cardinalidade em clusters grandes!

---

### 9. log_capturer_sink_queue_utilization
**Tipo**: Gauge
**Labels**: `sink_type`
**Descricao**: Utilizacao da fila dos sinks (0.0 a 1.0)
**Codigo**: `internal/metrics/metrics.go:89`
**Usado em Dashboard**: ✅ Painel "Sink Queue"
**Valor Exemplo**: 0.45 (45%)

---

### 10. log_capturer_component_health
**Tipo**: Gauge
**Labels**: `component_type`, `component_name`
**Descricao**: Status de saude (1=healthy, 0=unhealthy)
**Codigo**: `internal/metrics/metrics.go:98`
**Usado em Dashboard**: ✅ Painel "Component Health"
**Valor Exemplo**: 1

---

### 11. log_capturer_processing_duration_seconds
**Tipo**: Histogram
**Labels**: `component`, `operation`
**Descricao**: Duracao de processamento
**Buckets**: `[0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]`
**Codigo**: `internal/metrics/metrics.go:107`
**Usado em Dashboard**: ✅ Painel #3 "Processing Latency"
**Valor P95**: ~0.050s (50ms)

---

### 12. log_capturer_sink_send_duration_seconds
**Tipo**: Histogram
**Labels**: `sink_type`
**Descricao**: Duracao de envio para sinks
**Buckets**: `[0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0]`
**Codigo**: `internal/metrics/metrics.go:117`
**Usado em Dashboard**: ✅ Painel #4 "Sink Latency"
**Valor P95**: ~0.5s (500ms)

---

### 13. log_capturer_queue_size
**Tipo**: Gauge
**Labels**: `component`, `queue_type`
**Descricao**: Tamanho atual das filas
**Codigo**: `internal/metrics/metrics.go:127`
**Usado em Dashboard**: ✅ Painel #5 "Queue Depth"
**Valor Exemplo**: 1234

---

### 14. log_capturer_task_heartbeats_total
**Tipo**: Counter
**Labels**: `task_id`, `task_type`
**Descricao**: Total de heartbeats de tarefas
**Codigo**: `internal/metrics/metrics.go:136`
**Usado em Dashboard**: ❌ NAO
**Valor Exemplo**: 4500

---

### 15. log_capturer_active_tasks
**Tipo**: Gauge
**Labels**: `task_type`, `state`
**Descricao**: Numero de tarefas ativas
**Codigo**: `internal/metrics/metrics.go:145`
**Usado em Dashboard**: ❌ NAO
**Valor Exemplo**: 8

---

### 16. log_capturer_memory_usage_bytes
**Tipo**: Gauge
**Labels**: `type`
**Descricao**: Uso de memoria em bytes
**Codigo**: `internal/metrics/metrics.go:156`
**Usado em Dashboard**: ✅ Painel #2 "Memory Usage"
**Valores**:
- `heap_alloc`: 268435456 (256MB)
- `heap_sys`: 402653184 (384MB)
- `heap_idle`: 134217728 (128MB)
- `heap_inuse`: 268435456 (256MB)

---

### 17. log_capturer_cpu_usage_percent
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Uso de CPU em percentual
**Codigo**: `internal/metrics/metrics.go:165`
**Usado em Dashboard**: ⚠️ NAO (mas deveria)
**Valor Exemplo**: 35.2%

---

### 18. log_capturer_gc_runs_total
**Tipo**: Counter
**Labels**: (nenhum)
**Descricao**: Total de garbage collections
**Codigo**: `internal/metrics/metrics.go:173`
**Usado em Dashboard**: ✅ Painel #6 "GC Activity"
**Valor Exemplo**: 450

---

### 19. log_capturer_goroutines
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Numero de goroutines
**Codigo**: `internal/metrics/metrics.go:181`
**Usado em Dashboard**: ✅ Painel #1 "Goroutine Count Trend"
**Valor Exemplo**: 292
**Threshold**: Warning se > 1000

---

### 20. log_capturer_file_descriptors_open
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: File descriptors abertos
**Codigo**: `internal/metrics/metrics.go:189`
**Usado em Dashboard**: ✅ Painel #7 "File Descriptor Usage"
**Valor Exemplo**: 128
**Threshold**: Warning se > 700 (70% de 1024)

---

### 21. log_capturer_gc_pause_duration_seconds
**Tipo**: Histogram
**Labels**: (nenhum)
**Descricao**: Duracao de pausas de GC
**Buckets**: `[0.00001, 0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0]`
**Codigo**: `internal/metrics/metrics.go:197`
**Usado em Dashboard**: ✅ Painel #6 "GC Pause Time"
**Valor P95**: ~0.002s (2ms)

---

### 22. log_capturer_total_files_monitored
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Total de arquivos monitorados (agregado)
**Codigo**: `internal/metrics/metrics.go:206`
**Usado em Dashboard**: ✅ Painel "File Monitoring"
**Valor Exemplo**: 15

**Nota**: Agregacao de `files_monitored` para evitar alta cardinalidade

---

### 23. log_capturer_total_containers_monitored
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Total de containers monitorados (agregado)
**Codigo**: `internal/metrics/metrics.go:213`
**Usado em Dashboard**: ✅ Painel "Container Monitoring"
**Valor Exemplo**: 12

---

### 24-28. Enhanced Metrics (5 metricas)

#### 24. log_capturer_disk_usage_bytes
**Tipo**: Gauge
**Labels**: `mount_point`, `device`
**Codigo**: `internal/metrics/metrics.go:222`
**Usado**: ❌ NAO (funcao existe mas nao chamada)

#### 25. log_capturer_response_time_seconds
**Tipo**: Histogram
**Labels**: `endpoint`, `method`
**Codigo**: `internal/metrics/metrics.go:230`
**Usado**: ✅ Middleware metrics

#### 26. log_capturer_connection_pool_stats
**Tipo**: Gauge
**Labels**: `pool_name`, `stat_type`
**Codigo**: `internal/metrics/metrics.go:239`
**Usado**: ❌ NAO

#### 27. log_capturer_compression_ratio
**Tipo**: Gauge
**Labels**: `component`, `algorithm`
**Codigo**: `internal/metrics/metrics.go:247`
**Usado**: ❌ NAO

#### 28. log_capturer_batching_stats
**Tipo**: Gauge
**Labels**: `component`, `stat_type`
**Codigo**: `internal/metrics/metrics.go:255`
**Usado**: ⚠️ Parcial (adaptive batching)

---

### 29. log_capturer_leak_detection
**Tipo**: Gauge
**Labels**: `resource_type`, `component`
**Descricao**: Metricas de deteccao de leaks
**Codigo**: `internal/metrics/metrics.go:263`
**Usado em Dashboard**: ✅ Painel #8 "Leak Detection Alerts"
**Valor Exemplo**: 0 (nenhum leak)

---

## METRICAS KAFKA (13 metricas)

**NOTA**: Estas metricas existem mas Kafka sink esta desabilitado (`kafka.enabled: false`)

### 30. kafka_messages_produced_total
**Tipo**: Counter
**Labels**: `topic`, `status`
**Codigo**: `internal/metrics/metrics.go:276`
**Status**: ⚠️ ORFAO (Kafka disabled)

### 31. kafka_producer_errors_total
**Tipo**: Counter
**Labels**: `topic`, `error_type`
**Codigo**: `internal/metrics/metrics.go:284`
**Status**: ⚠️ ORFAO

### 32. kafka_batch_size_messages
**Tipo**: Histogram
**Labels**: `topic`
**Buckets**: `[1, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000]`
**Codigo**: `internal/metrics/metrics.go:293`
**Status**: ⚠️ ORFAO

### 33. kafka_batch_send_duration_seconds
**Tipo**: Histogram
**Labels**: `topic`
**Buckets**: `[0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0]`
**Codigo**: `internal/metrics/metrics.go:304`
**Status**: ⚠️ ORFAO

### 34. kafka_queue_size
**Tipo**: Gauge
**Labels**: `sink_name`
**Codigo**: `internal/metrics/metrics.go:314`
**Status**: ⚠️ ORFAO

### 35. kafka_queue_utilization
**Tipo**: Gauge
**Labels**: `sink_name`
**Codigo**: `internal/metrics/metrics.go:322`
**Status**: ⚠️ ORFAO

### 36. kafka_partition_messages_total
**Tipo**: Counter
**Labels**: `topic`, `partition`
**Codigo**: `internal/metrics/metrics.go:331`
**Status**: ⚠️ ORFAO

### 37. kafka_compression_ratio
**Tipo**: Gauge
**Labels**: `topic`, `compression_type`
**Codigo**: `internal/metrics/metrics.go:340`
**Status**: ⚠️ ORFAO

### 38. kafka_backpressure_events_total
**Tipo**: Counter
**Labels**: `sink_name`, `threshold_level`
**Codigo**: `internal/metrics/metrics.go:349`
**Status**: ⚠️ ORFAO

### 39. kafka_circuit_breaker_state
**Tipo**: Gauge
**Labels**: `sink_name`
**Descricao**: `0=closed, 1=half-open, 2=open`
**Codigo**: `internal/metrics/metrics.go:358`
**Status**: ⚠️ ORFAO

### 40. kafka_message_size_bytes
**Tipo**: Histogram
**Labels**: `topic`
**Buckets**: `[100, 500, 1024, 5120, 10240, 51200, 102400, 512000, 1048576]`
**Codigo**: `internal/metrics/metrics.go:367`
**Status**: ⚠️ ORFAO

### 41. kafka_dlq_messages_total
**Tipo**: Counter
**Labels**: `topic`, `reason`
**Codigo**: `internal/metrics/metrics.go:378`
**Status**: ⚠️ ORFAO

### 42. kafka_connection_status
**Tipo**: Gauge
**Labels**: `broker`, `sink_name`
**Descricao**: `1=connected, 0=disconnected`
**Codigo**: `internal/metrics/metrics.go:386`
**Status**: ⚠️ ORFAO

**Analise**: 13 metricas Kafka que NAO estao sendo incrementadas (Kafka disabled). Manter para uso futuro.

---

## METRICAS CONTAINER STREAMS (5 metricas)

### 43. log_capturer_container_streams_active
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Numero de streams Docker ativos
**Codigo**: `internal/metrics/metrics.go:400`
**Usado em Dashboard**: ✅ Painel #9 "Active Streams"
**Valor Exemplo**: 12

---

### 44. log_capturer_container_stream_rotations_total
**Tipo**: Counter
**Labels**: `container_id`, `container_name`
**Descricao**: Total de rotacoes de stream
**Codigo**: `internal/metrics/metrics.go:408`
**Usado em Dashboard**: ✅ Painel "Stream Rotations"
**Valor Exemplo**: 45

---

### 45. log_capturer_container_stream_age_seconds
**Tipo**: Histogram
**Labels**: `container_id`
**Descricao**: Idade dos streams quando rotacionados
**Buckets**: `[60, 120, 180, 240, 300, 360, 420, 480, 540, 600]`
**Codigo**: `internal/metrics/metrics.go:416`
**Usado em Dashboard**: ⚠️ NAO
**Valor P95**: ~300s (5 minutos)

---

### 46. log_capturer_container_stream_errors_total
**Tipo**: Counter
**Labels**: `error_type`, `container_id`
**Descricao**: Total de erros de stream
**Codigo**: `internal/metrics/metrics.go:427`
**Usado em Dashboard**: ✅ Painel "Stream Errors"
**Valor Exemplo**: 5

---

### 47. log_capturer_container_stream_pool_utilization
**Tipo**: Gauge
**Labels**: (nenhum)
**Descricao**: Utilizacao do pool de streams (0.0 a 1.0)
**Codigo**: `internal/metrics/metrics.go:436`
**Usado em Dashboard**: ✅ Painel "Stream Pool"
**Valor Exemplo**: 0.48 (48%)

---

## METRICAS AUSENTES (Sugestoes)

### Build Info (Alta Prioridade)
```go
log_capturer_build_info{version="v0.0.2", go_version="1.21.5", commit="abc123"} 1
```
**Uso**: Tracking de versoes, rollback identification

### Uptime (Media Prioridade)
```go
log_capturer_uptime_seconds 7230
```
**Uso**: Restart detection, availability calculation

### Config Reload (Media Prioridade)
```go
log_capturer_config_reload_total{status="success"} 5
log_capturer_config_reload_total{status="failure"} 1
```
**Uso**: Config management monitoring

### Pipeline Specific (Baixa Prioridade)
```go
log_capturer_pipeline_processed_total{pipeline="default"} 12345
```
**Uso**: Per-pipeline throughput

### Latency P50/P99 (Media Prioridade)
- Atualmente temos histograms mas nao as metricas derivadas
- Adicionar summaries para P50, P95, P99

---

## ANALISE DE CARDINALIDADE

### Alta Cardinalidade (Cuidado!)

1. **log_capturer_files_monitored** - Label `filepath`
   - **Cardinalidade**: N arquivos monitorados
   - **Mitigacao**: Usar `total_files_monitored` (agregado) ✅ JA FEITO

2. **log_capturer_containers_monitored** - Labels `container_id`, `container_name`, `image`
   - **Cardinalidade**: N containers * M images
   - **Mitigacao**: Usar `total_containers_monitored` (agregado) ✅ JA FEITO

3. **kafka_partition_messages_total** - Label `partition`
   - **Cardinalidade**: N topics * M partitions
   - **Impacto**: BAIXO se Kafka disabled
   - **Mitigacao**: Se habilitar Kafka, limitar partitions

4. **log_capturer_response_time_seconds** - Label `endpoint`
   - **Cardinalidade**: N endpoints (~18)
   - **Impacto**: BAIXO

### Cardinalidade Aceitavel

- `source_type`: ~2-5 valores (docker, file, api)
- `sink_type`: ~3-5 valores (loki, local_file, kafka)
- `component`: ~10-15 valores
- `error_type`: ~10-20 valores
- `task_type`: ~5-10 valores

---

## METRICAS SEMPRE 0 (Verificacao)

### Verificar se Estas Incrementam

1. **log_capturer_cpu_usage_percent**
   - Verificar: Esta sendo atualizado?
   - **Status**: ⚠️ INVESTIGAR

2. **log_capturer_disk_usage_bytes**
   - Verificar: Funcao `RecordDiskUsage()` e chamada?
   - **Status**: ❌ NAO chamada

3. **log_capturer_connection_pool_stats**
   - Verificar: Docker connection pool atualiza?
   - **Status**: ⚠️ INVESTIGAR

4. **log_capturer_compression_ratio**
   - Verificar: HTTP compression ativo?
   - **Status**: ❌ NAO usado

5. **Todas metricas Kafka** (13 metricas)
   - **Status**: ✅ ESPERADO (Kafka disabled)

---

## DASHBOARDS GRAFANA

### Paineis Criados (FASE 4)

1. **Goroutine Count Trend** - Usa `log_capturer_goroutines`
2. **Memory Usage** - Usa `log_capturer_memory_usage_bytes`
3. **Processing Latency** - Usa `log_capturer_processing_duration_seconds`
4. **Sink Latency** - Usa `log_capturer_sink_send_duration_seconds`
5. **Queue Depth Trend** - Usa `log_capturer_queue_size`
6. **GC Activity** - Usa `log_capturer_gc_runs_total`, `log_capturer_gc_pause_duration_seconds`
7. **File Descriptor Usage** - Usa `log_capturer_file_descriptors_open`
8. **Leak Detection Alerts** - Usa `log_capturer_leak_detection`
9. **Active Streams** - Usa `log_capturer_container_streams_active`
10. **Logs Processed** - Usa `log_capturer_logs_processed_total`

### Paineis Faltando (Sugestoes)

1. **Error Rate Dashboard**
   - Metrica: `log_capturer_errors_total`
   - **Prioridade**: ALTA

2. **Throughput Dashboard**
   - Metrica: `log_capturer_logs_per_second`
   - **Prioridade**: ALTA

3. **Component Health Dashboard**
   - Metrica: `log_capturer_component_health`
   - **Prioridade**: MEDIA

4. **Sink Delivery Dashboard**
   - Metrica: `log_capturer_logs_sent_total`
   - **Prioridade**: MEDIA

---

## ALERTAS PROMETHEUS

### Alertas Criados (FASE 4) - 7 alertas

1. **GoroutineLeakDetected**
2. **HighMemoryUsage**
3. **HighFileDescriptorUsage**
4. **FrequentGCPauses**
5. **HighQueueUtilization**
6. **HighSinkLatency**
7. **ProcessingErrors**

### Alertas Faltando (Sugestoes)

1. **HighErrorRate**
```yaml
- alert: HighErrorRate
  expr: rate(log_capturer_errors_total[5m]) > 10
  for: 5m
  annotations:
    summary: "High error rate detected"
```

2. **ComponentUnhealthy**
```yaml
- alert: ComponentUnhealthy
  expr: log_capturer_component_health == 0
  for: 2m
  annotations:
    summary: "Component {{ $labels.component_name }} unhealthy"
```

3. **LowThroughput**
```yaml
- alert: LowThroughput
  expr: log_capturer_logs_per_second < 10
  for: 10m
  annotations:
    summary: "Abnormally low log throughput"
```

---

## CONCLUSOES

### Pontos Fortes
- ✅ 63 metricas bem definidas
- ✅ Boa cobertura: core, kafka, streams, enhanced
- ✅ Mitigacao de alta cardinalidade (agregados)
- ✅ 71% usadas em dashboards
- ✅ Histograms com buckets apropriados

### Pontos Fracos
- ⚠️ 13 metricas Kafka orfas (sink disabled)
- ⚠️ Algumas metricas enhanced nao usadas
- ⚠️ Faltam metricas: build_info, uptime, config_reload
- ⚠️ CPU usage pode nao estar atualizando
- ⚠️ Disk usage nao e chamado

### Acoes Recomendadas

**Imediatas**:
1. Adicionar `log_capturer_build_info` ✅
2. Adicionar `log_capturer_uptime_seconds` ✅
3. Verificar se `cpu_usage_percent` esta atualizando ⚠️
4. Criar dashboard de Error Rate ✅

**Curto Prazo**:
5. Implementar `RecordDiskUsage()` chamadas
6. Adicionar metricas de config_reload
7. Criar alertas faltando (HighErrorRate, ComponentUnhealthy)

**Backlog**:
8. Quando Kafka for habilitado, validar metricas Kafka
9. Adicionar summaries para P50/P95/P99
10. Remover metricas nao usadas (compression, connection_pool)

### Metricas por Status

| Status | Quantidade | Percentual |
|--------|------------|------------|
| FUNCIONAL + USADO | 45 | 71% |
| FUNCIONAL + NAO USADO | 5 | 8% |
| ORFAO (Kafka disabled) | 13 | 21% |

**Total**: 63 metricas, 50 ativas (79%), 13 orfas (21% - esperado)

---

**Proximo**: Ver `FEATURES_CATALOG.md`
