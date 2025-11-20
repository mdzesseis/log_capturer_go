# Referência Completa de Métricas - SSW Logs Capture

**Versão:** v0.0.2
**Última Atualização:** 2024-11-20
**Total de Métricas:** 98

---

## Índice

1. [Métricas de Processamento de Logs](#1-métricas-de-processamento-de-logs)
2. [Métricas do Dispatcher](#2-métricas-do-dispatcher)
3. [Métricas de Sinks](#3-métricas-de-sinks)
4. [Métricas do Kafka Sink](#4-métricas-do-kafka-sink)
5. [Métricas de Sistema/Recursos](#5-métricas-de-sistemarecursos)
6. [Métricas de Monitores](#6-métricas-de-monitores)
7. [Métricas de Erros](#7-métricas-de-erros)
8. [Métricas de Performance](#8-métricas-de-performance)
9. [Métricas de Deduplicação](#9-métricas-de-deduplicação)
10. [Métricas de DLQ](#10-métricas-de-dlq-dead-letter-queue)
11. [Métricas de Timestamp Learning](#11-métricas-de-timestamp-learning)
12. [Métricas do Sistema de Posições](#12-métricas-do-sistema-de-posições)
13. [Métricas Docker](#13-métricas-docker)
14. [Problemas Identificados](#problemas-identificados)

---

## 1. Métricas de Processamento de Logs

### `log_capturer_logs_processed_total`
- **Tipo:** Counter
- **Labels:** `source_type`, `source_id`, `pipeline`
- **Descrição:** Total de logs processados pelo sistema
- **Uso:** Monitorar throughput de processamento por fonte
- **Exemplo:** `log_capturer_logs_processed_total{source_type="docker",source_id="nginx",pipeline="main"}`

### `log_capturer_logs_per_second`
- **Tipo:** Gauge
- **Labels:** `component`
- **Descrição:** Taxa atual de logs por segundo
- **Uso:** Monitorar throughput em tempo real
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_logs_collected_total`
- **Tipo:** Counter
- **Labels:** `stream`, `container`
- **Descrição:** Total de linhas de log coletadas de containers
- **Uso:** Rastrear volume de coleta por container/stream
- **Status:** ⚠️ Definida mas não atualizada diretamente

### `log_capturer_logs_deduplicated_total`
- **Tipo:** Counter
- **Labels:** `source_type`, `source_id`
- **Descrição:** Total de logs deduplicados (descartados por duplicação)
- **Uso:** Monitorar eficiência da deduplicação

---

## 2. Métricas do Dispatcher

### `log_capturer_dispatcher_queue_depth`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Número atual de entries na fila do dispatcher
- **Uso:** Monitorar acúmulo na fila - valores altos indicam backpressure
- **Alerta sugerido:** > 80% da capacidade

### `log_capturer_dispatcher_queue_utilization`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Utilização da fila do dispatcher (0.0 a 1.0)
- **Uso:** Porcentagem de uso da fila
- **Alerta sugerido:** > 0.9

### `log_capturer_queue_size`
- **Tipo:** Gauge
- **Labels:** `component`, `queue_type`
- **Descrição:** Tamanho atual de filas específicas
- **Uso:** Monitorar diferentes filas do sistema

---

## 3. Métricas de Sinks

### `log_capturer_logs_sent_total`
- **Tipo:** Counter
- **Labels:** `sink_type`, `status`
- **Descrição:** Total de logs enviados para sinks
- **Uso:** Rastrear entregas bem-sucedidas vs falhas
- **Status values:** `success`, `error`, `dropped`

### `log_capturer_sink_queue_utilization`
- **Tipo:** Gauge
- **Labels:** `sink_type`
- **Descrição:** Utilização da fila interna do sink (0.0 a 1.0)
- **Uso:** Detectar backpressure em sinks específicos

### `log_capturer_sink_send_duration_seconds`
- **Tipo:** Histogram
- **Labels:** `sink_type`
- **Descrição:** Tempo gasto enviando logs para sinks
- **Buckets:** Padrão Prometheus
- **Uso:** Monitorar latência de entrega

### `log_capturer_component_health`
- **Tipo:** Gauge
- **Labels:** `component_type`, `component_name`
- **Descrição:** Status de saúde do componente (1=healthy, 0=unhealthy)
- **Uso:** Health checks e alertas

---

## 4. Métricas do Kafka Sink

### `kafka_messages_produced_total`
- **Tipo:** Counter
- **Labels:** `topic`, `status`
- **Descrição:** Total de mensagens produzidas para Kafka
- **Status values:** `success`, `error`

### `kafka_producer_errors_total`
- **Tipo:** Counter
- **Labels:** `topic`, `error_type`
- **Descrição:** Total de erros do produtor Kafka
- **Error types:** `serialization`, `network`, `timeout`, etc.

### `kafka_batch_size_messages`
- **Tipo:** Histogram
- **Labels:** `topic`
- **Descrição:** Número de mensagens em cada batch Kafka
- **Uso:** Otimizar configuração de batching

### `kafka_batch_send_duration_seconds`
- **Tipo:** Histogram
- **Labels:** `topic`
- **Descrição:** Tempo para enviar um batch para Kafka
- **Uso:** Monitorar latência do Kafka

### `kafka_queue_size`
- **Tipo:** Gauge
- **Labels:** `sink_name`
- **Descrição:** Tamanho atual da fila interna do Kafka

### `kafka_queue_utilization`
- **Tipo:** Gauge
- **Labels:** `sink_name`
- **Descrição:** Utilização da fila Kafka (0.0 a 1.0)

### `kafka_partition_messages_total`
- **Tipo:** Counter
- **Labels:** `topic`, `partition`
- **Descrição:** Total de mensagens por partição
- **Uso:** Verificar balanceamento entre partições

### `kafka_compression_ratio`
- **Tipo:** Gauge
- **Labels:** `topic`, `compression_type`
- **Descrição:** Taxa de compressão das mensagens
- **Uso:** Monitorar eficiência de compressão

### `kafka_backpressure_events_total`
- **Tipo:** Counter
- **Labels:** `sink_name`, `threshold_level`
- **Descrição:** Total de eventos de backpressure
- **Uso:** Detectar sobrecarga do produtor

### `kafka_circuit_breaker_state`
- **Tipo:** Gauge
- **Labels:** `sink_name`
- **Descrição:** Estado do circuit breaker
- **Valores:** 0=closed, 1=half-open, 2=open

### `kafka_message_size_bytes`
- **Tipo:** Histogram
- **Labels:** `topic`
- **Descrição:** Tamanho das mensagens Kafka em bytes

### `kafka_dlq_messages_total`
- **Tipo:** Counter
- **Labels:** `topic`, `reason`
- **Descrição:** Total de mensagens enviadas para DLQ do Kafka

### `kafka_connection_status`
- **Tipo:** Gauge
- **Labels:** `broker`, `sink_name`
- **Descrição:** Status de conexão com broker
- **Valores:** 1=connected, 0=disconnected

---

## 5. Métricas de Sistema/Recursos

### `log_capturer_memory_usage_bytes`
- **Tipo:** Gauge
- **Labels:** `type`
- **Descrição:** Uso de memória em bytes
- **Types:** `heap_alloc`, `heap_sys`, `heap_idle`, `heap_inuse`, `stack_inuse`

### `log_capturer_cpu_usage_percent`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Porcentagem de uso de CPU
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_gc_runs_total`
- **Tipo:** Counter
- **Labels:** nenhum
- **Descrição:** Total de execuções do Garbage Collector

### `log_capturer_goroutines`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Número atual de goroutines
- **Alerta sugerido:** Crescimento contínuo indica leak

### `log_capturer_file_descriptors_open`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** File descriptors abertos
- **Alerta sugerido:** Próximo do limite do sistema

### `log_capturer_gc_pause_duration_seconds`
- **Tipo:** Histogram
- **Labels:** nenhum
- **Descrição:** Duração das pausas do GC
- **Uso:** Detectar pausas longas que afetam latência

---

## 6. Métricas de Monitores

### `log_capturer_files_monitored`
- **Tipo:** Gauge
- **Labels:** `filepath`, `source_type`
- **Descrição:** Arquivos sendo monitorados

### `log_capturer_containers_monitored`
- **Tipo:** Gauge
- **Labels:** `container_id`, `container_name`, `image`
- **Descrição:** Containers sendo monitorados

### `log_capturer_total_files_monitored`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Total agregado de arquivos monitorados

### `log_capturer_total_containers_monitored`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Total agregado de containers monitorados

### `log_capturer_container_streams_active`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Streams de log de containers ativos

### `log_capturer_container_stream_rotations_total`
- **Tipo:** Counter
- **Labels:** `container_id`, `container_name`
- **Descrição:** Rotações de stream por container

### `log_capturer_container_stream_age_seconds`
- **Tipo:** Histogram
- **Labels:** `container_id`
- **Descrição:** Idade do stream quando rotacionado

### `log_capturer_container_stream_errors_total`
- **Tipo:** Counter
- **Labels:** `error_type`, `container_id`
- **Descrição:** Erros de stream por tipo

### `log_capturer_container_stream_pool_utilization`
- **Tipo:** Gauge
- **Labels:** nenhum
- **Descrição:** Utilização do pool de streams

---

## 7. Métricas de Erros

### `log_capturer_errors_total`
- **Tipo:** Counter
- **Labels:** `component`, `error_type`
- **Descrição:** Total de erros por componente e tipo
- **Uso:** Principal métrica de erros do sistema
- **Components:** `dispatcher`, `file_monitor`, `container_monitor`, `loki_sink`, `kafka_sink`, etc.

---

## 8. Métricas de Performance

### `log_capturer_processing_duration_seconds`
- **Tipo:** Histogram
- **Labels:** `component`, `operation`
- **Descrição:** Tempo gasto processando logs
- **Uso:** Identificar gargalos de performance

### `log_capturer_processing_step_duration_seconds`
- **Tipo:** Histogram
- **Labels:** `pipeline`, `step`
- **Descrição:** Tempo em cada etapa do pipeline
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_response_time_seconds`
- **Tipo:** Histogram
- **Labels:** `endpoint`, `method`
- **Descrição:** Tempo de resposta de endpoints HTTP

---

## 9. Métricas de Deduplicação

### `log_capturer_deduplication_cache_size`
- **Tipo:** Gauge
- **Descrição:** Tamanho atual do cache de deduplicação
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_deduplication_hit_rate`
- **Tipo:** Gauge
- **Descrição:** Taxa de hits no cache (0.0 a 1.0)
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_deduplication_duplicate_rate`
- **Tipo:** Gauge
- **Descrição:** Taxa de logs duplicados detectados
- **Status:** ⚠️ Definida mas não atualizada

### `log_capturer_deduplication_cache_evictions_total`
- **Tipo:** Counter
- **Descrição:** Total de evictions do cache
- **Status:** ⚠️ Definida mas não atualizada

---

## 10. Métricas de DLQ (Dead Letter Queue)

### `log_capturer_dlq_stored_total`
- **Tipo:** Counter
- **Labels:** `sink`, `reason`
- **Descrição:** Total de entries armazenadas na DLQ
- **Reasons:** `max_retries`, `timeout`, `invalid`, etc.

### `log_capturer_dlq_entries_total`
- **Tipo:** Gauge
- **Labels:** `sink`
- **Descrição:** Número atual de entries na DLQ

### `log_capturer_dlq_size_bytes`
- **Tipo:** Gauge
- **Labels:** `sink`
- **Descrição:** Tamanho da DLQ em bytes

### `log_capturer_dlq_reprocess_attempts_total`
- **Tipo:** Counter
- **Labels:** `sink`, `result`
- **Descrição:** Tentativas de reprocessamento da DLQ
- **Results:** `success`, `failure`

---

## 11. Métricas de Timestamp Learning

### `log_capturer_timestamp_rejection_total`
- **Tipo:** Counter
- **Labels:** `sink`, `reason`
- **Descrição:** Rejeições por timestamp inválido
- **Reasons:** `too_old`, `future`, `invalid_format`

### `log_capturer_timestamp_clamped_total`
- **Tipo:** Counter
- **Labels:** `sink`
- **Descrição:** Timestamps ajustados (clamped) para limites aceitáveis

### `log_capturer_timestamp_max_acceptable_age_seconds`
- **Tipo:** Gauge
- **Labels:** `sink`
- **Descrição:** Idade máxima aprendida para timestamps
- **Uso:** Monitorar adaptação do sistema

### `log_capturer_loki_error_type_total`
- **Tipo:** Counter
- **Labels:** `sink`, `error_type`
- **Descrição:** Erros do Loki por tipo

### `log_capturer_timestamp_learning_events_total`
- **Tipo:** Counter
- **Labels:** `sink`
- **Descrição:** Eventos de aprendizado de timestamp

---

## 12. Métricas do Sistema de Posições

### Detecção de Eventos

#### `log_capturer_position_rotation_detected_total`
- **Tipo:** Counter
- **Labels:** `file_path`
- **Descrição:** Rotações de arquivo detectadas

#### `log_capturer_position_truncation_detected_total`
- **Tipo:** Counter
- **Labels:** `file_path`
- **Descrição:** Truncamentos de arquivo detectados

### Persistência

#### `log_capturer_position_save_success_total`
- **Tipo:** Counter
- **Descrição:** Salvamentos de posição bem-sucedidos

#### `log_capturer_position_save_failed_total`
- **Tipo:** Counter
- **Labels:** `error_type`
- **Descrição:** Falhas ao salvar posição

#### `log_capturer_position_lag_seconds`
- **Tipo:** Gauge
- **Labels:** `manager_type`
- **Descrição:** Segundos desde último save

### Health & Status

#### `log_capturer_position_file_size_bytes`
- **Tipo:** Gauge
- **Labels:** `file_type`
- **Descrição:** Tamanho do arquivo de posições

#### `log_capturer_checkpoint_health`
- **Tipo:** Gauge
- **Labels:** `component`
- **Descrição:** Saúde do sistema de checkpoint (1=healthy)

#### `log_capturer_position_corruption_detected_total`
- **Tipo:** Counter
- **Labels:** `file_type`, `recovery_action`
- **Descrição:** Corrupções detectadas e ações de recuperação

#### `log_capturer_position_backpressure`
- **Tipo:** Gauge
- **Labels:** `manager_type`
- **Descrição:** Backpressure no sistema de posições

---

## 13. Métricas Docker

### HTTP Client

#### `log_capturer_docker_http_idle_connections`
- **Tipo:** Gauge
- **Descrição:** Conexões HTTP ociosas com Docker daemon

#### `log_capturer_docker_http_active_connections`
- **Tipo:** Gauge
- **Descrição:** Conexões HTTP ativas com Docker daemon

#### `log_capturer_docker_http_requests_total`
- **Tipo:** Counter
- **Descrição:** Total de requisições HTTP ao Docker

#### `log_capturer_docker_http_errors_total`
- **Tipo:** Counter
- **Descrição:** Total de erros HTTP

### Connection Pool

#### `ssw_logs_capture_docker_pool_total_connections`
- **Tipo:** Gauge
- **Descrição:** Total de conexões no pool Docker

#### `ssw_logs_capture_docker_pool_active_connections`
- **Tipo:** Gauge
- **Descrição:** Conexões ativas no pool

#### `ssw_logs_capture_docker_pool_available_connections`
- **Tipo:** Gauge
- **Descrição:** Conexões disponíveis no pool

#### `ssw_logs_capture_docker_pool_connection_duration_seconds`
- **Tipo:** Histogram
- **Descrição:** Tempo para adquirir conexão

---

## Problemas Identificados

### Métricas Definidas mas Não Atualizadas

| Métrica | Arquivo:Linha | Ação Necessária |
|---------|---------------|-----------------|
| `log_capturer_cpu_usage_percent` | metrics.go:208 | Implementar coleta de CPU |
| `log_capturer_logs_per_second` | metrics.go:28 | Atualizar periodicamente |
| `log_capturer_logs_collected_total` | metrics.go:508 | Conectar ao container_monitor |
| `log_capturer_deduplication_cache_size` | metrics.go:170 | Conectar ao DeduplicationManager |
| `log_capturer_deduplication_hit_rate` | metrics.go:177 | Conectar ao DeduplicationManager |
| `log_capturer_deduplication_duplicate_rate` | metrics.go:184 | Conectar ao DeduplicationManager |
| `log_capturer_deduplication_cache_evictions_total` | metrics.go:191 | Conectar ao DeduplicationManager |
| `log_capturer_processing_step_duration_seconds` | metrics.go:49 | Instrumentar pipeline steps |
| `log_capturer_container_events_total` | metrics.go:517 | Conectar ao container_monitor |
| `log_capturer_position_checkpoint_created_total` | metrics.go:805 | Conectar ao checkpoint manager |
| `log_capturer_position_checkpoint_size_bytes` | metrics.go:813 | Conectar ao checkpoint manager |
| `log_capturer_position_checkpoint_age_seconds` | metrics.go:821 | Conectar ao checkpoint manager |

### Inconsistências de Nomenclatura

- Docker Connection Pool usa prefixo `ssw_logs_capture_` ao invés de `log_capturer_`
- Recomendação: Padronizar para `log_capturer_` em todo o projeto

### Métricas de Alto Valor Não Utilizadas

As métricas de deduplicação e checkpoint são valiosas para troubleshooting mas não estão sendo alimentadas. Priorizar implementação.

---

## Queries Úteis para Grafana

### Taxa de Processamento
```promql
rate(log_capturer_logs_processed_total[5m])
```

### Utilização de Fila
```promql
log_capturer_dispatcher_queue_utilization * 100
```

### Taxa de Erros
```promql
rate(log_capturer_errors_total[5m])
```

### Latência P99
```promql
histogram_quantile(0.99, rate(log_capturer_processing_duration_seconds_bucket[5m]))
```

### Goroutines (Leak Detection)
```promql
increase(log_capturer_goroutines[1h])
```

### DLQ Growing
```promql
delta(log_capturer_dlq_entries_total[1h]) > 0
```

---

## Endpoints de Métricas

- **Prometheus metrics:** `http://localhost:8001/metrics`
- **Health check:** `http://localhost:8401/health`
- **pprof:** `http://localhost:6060/debug/pprof/`

---

*Documento gerado em 2024-11-20*
