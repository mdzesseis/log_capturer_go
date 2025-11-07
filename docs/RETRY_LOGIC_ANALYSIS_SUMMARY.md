# Análise do Sistema de Retry - Resumo Executivo

**Data:** 2025-11-07
**Status:** Tasks 1-3 completadas, Task 4 in-progress

---

## Executive Summary

O sistema possui retry logic em 4 componentes principais:
1. **Dispatcher** - retry queue de alta/baixa prioridade
2. **Loki Sink** - retry com circuit breaker
3. **LocalFile Sink** - retry agora integrado com DLQ (Task 3)
4. **File Monitor** - retry queue implementado na Task 2

**Problema crítico identificado:** Retry sem validação de causa de falha leva a "retry storm" quando logs são rejeitados por razões permanentes (ex: timestamp too old).

---

## 1. Dispatcher Retry Logic

**Arquivo:** `internal/dispatcher/dispatcher.go`

### Arquitetura Atual
- Queue de alta prioridade (canal principal)
- Queue de baixa prioridade (retry)
- Workers processam de ambas as queues

### Fluxo de Retry
```
Entry → High Priority Queue → Worker → Sink.Send()
                                          ↓ FAIL
                                 Low Priority Queue (retry)
                                          ↓
                                    Worker → Sink.Send()
                                          ↓ FAIL (após N retries)
                                         DLQ
```

### Configuração
```yaml
dispatcher:
  worker_count: 12
  queue_size: 10000
  low_priority_queue_size: 5000
  retry_max_attempts: 10
```

### Problemas Identificados
- ❌ **Não distingue erros temporários vs permanentes**
- ❌ **Não valida timestamp antes de retry**
- ❌ **Backoff não configurável** (fixo em 1s)
- ✅ Deep-copy de labels (Task 2 fix)

---

## 2. Loki Sink Retry Logic

**Arquivo:** `internal/sinks/loki_sink.go`

### Arquitetura Atual
- Integrado com circuit breaker
- Retry em caso de falha de send
- **Agora com DLQ** (Task 3)

### Tratamento de Erros Específicos

| Erro | Código | Retry? | Status Atual |
|------|--------|--------|--------------|
| timestamp too old | 400 | ❌ SIM (problema!) | Deve ir direto para DLQ |
| timestamp too new | 400 | ❌ SIM (problema!) | Deve normalizar timestamp |
| out of order | 400 | ❌ SIM (problema!) | Precisa ordenação |
| rate limiting | 429 | ✅ SIM | Circuit breaker ativa |
| network timeout | - | ✅ SIM | OK |
| server error | 500 | ✅ SIM | OK |

### Configuração
```yaml
sinks:
  loki:
    retry_attempts: 5
    retry_delay: 1s
    circuit_breaker:
      threshold: 5
      timeout: 30s
```

### Problemas Identificados
- ❌ **Retry de erros 400 sem parsing da mensagem**
  - "timestamp too old" → deveria ir para DLQ imediatamente
  - "out of order" → precisa reordenar, não retry
- ❌ **Sem timestamp validation antes de send**
- ❌ **Sem timestamp normalization (timezone)**

---

## 3. LocalFile Sink Retry Logic

**Arquivo:** `internal/sinks/local_file_sink.go`

### Status: ✅ IMPLEMENTADO NA TASK 3

**Integração com DLQ:**
- Disk full → DLQ
- Write errors → DLQ
- Permission denied → DLQ

### Configuração
```yaml
sinks:
  local_file:
    retry_attempts: 3
    dlq_enabled: true
```

### Problemas
- ✅ **Resolvido:** Agora usa DLQ para failures permanentes

---

## 4. File Monitor Retry Logic

**Arquivo:** `internal/monitors/file_monitor.go`

### Status: ✅ IMPLEMENTADO NA TASK 2

**Features:**
- ✅ Retry queue com limite (50 entries)
- ✅ Exponential backoff (1s → 2s → 4s → 8s → max 60s)
- ✅ Drop policies (oldest/newest/random)
- ✅ Max retry attempts

### Configuração
```yaml
file_monitor_service:
  max_retry_queue_size: 50
  retry:
    initial_delay: "1s"
    max_delay: "60s"
    multiplier: 2.0
    drop_policy: "oldest"
```

### Status
- ✅ **Totalmente funcional após Task 2**

---

## 5. Cenários de Logs Problemáticos

### 5.1 Timestamp Antigo (ROOT CAUSE do Hybrid Monitor Failure)

**Problema:**
```
1. File monitor lê logs históricos (dias/meses antigos)
2. Loki rejeita: "timestamp too old for stream" (400)
3. Dispatcher faz retry
4. Loki rejeita novamente (sempre rejeitará!)
5. Retry queue satura (150/150)
6. System: +43 goroutines/min → FAILURE
```

**Solução Necessária:**
1. ✅ Implementado na Task 2: `ignore_old_timestamps: true`
2. ⏳ **Pendente (Task 5):** Timestamp learning no Loki sink
   - Detectar "timestamp too old" pattern
   - Learn threshold (ex: 24h)
   - Clamp timestamps ou rejeitar antes de enviar

**Código necessário:**
```go
// Pseudo-code for Task 5
func (l *LokiSink) validateTimestamp(entry LogEntry) error {
    if time.Since(entry.Timestamp) > l.learnedMaxAge {
        return ErrTimestampTooOld // vai direto para DLQ
    }
    return nil
}
```

### 5.2 Timestamp Futuro

**Problema:**
```
Timezone offset (UTC-3 vs UTC) → timestamps 3h no futuro
Loki rejeita: "timestamp too new"
```

**Solução:**
- ✅ **RESOLVIDO:** `TZ=UTC` no docker-compose.yml
- ⏳ **Recomendado (Task 5):** Timestamp normalization no dispatcher

### 5.3 Out-of-Order Logs

**Problema:**
```
Logs chegam fora de ordem → Loki rejeita (exige ordem crescente por stream)
```

**Solução Necessária:**
- ⏳ **Task 5:** Ordenação de batch antes de enviar para Loki
- Alternativamente: usar `out_of_order_time_window` config no Loki

### 5.4 Rate Limiting (429)

**Status:** ✅ Circuit breaker já implementado

**Comportamento atual:**
```
429 Too Many Requests → Circuit breaker OPEN → Backoff 30s → Retry
```

---

## 6. Gaps e Melhorias Necessárias

### Prioridade 1 (Crítico - Task 5)

#### 6.1 Timestamp Validation Layer
**Onde:** Dispatcher ou Loki Sink
**O que:** Validar timestamp ANTES de retry
```go
type TimestampValidator interface {
    Validate(entry LogEntry) error
    LearnFromRejection(error)
    ClampTimestamp(entry *LogEntry)
}
```

#### 6.2 Error-Aware Retry (Smart Retry)
**Onde:** Todos os sinks
**O que:** Decidir se deve fazer retry baseado no tipo de erro
```go
func shouldRetry(err error) bool {
    // 400 timestamp errors → NO RETRY, vai para DLQ
    // 429 rate limit → RETRY com backoff
    // 500 server error → RETRY
    // Network timeout → RETRY
}
```

### Prioridade 2 (Importante)

#### 6.3 Retry Budget
**Conceito:** Limite global de retries por segundo
**Objetivo:** Prevenir retry storm mesmo com muitos logs problemáticos

```yaml
dispatcher:
  retry_budget:
    max_retries_per_second: 100
    burst: 200
```

#### 6.4 Metrics por Reason
**Adicionar:**
```
log_capturer_retry_attempts_total{reason="timestamp_too_old"}
log_capturer_retry_attempts_total{reason="rate_limit"}
log_capturer_retry_attempts_total{reason="network_error"}
```

### Prioridade 3 (Nice to have)

#### 6.5 Adaptive Backoff
**Conceito:** Ajustar backoff baseado em taxa de sucesso
- Se 90% de sucesso → backoff curto (1s)
- Se 10% de sucesso → backoff longo (60s)

---

## 7. Trace Completo de Retry

```
[Container/File]
       ↓
   [Monitor] ←─┐ 
       ↓       │ RETRY (Task 2: exponential backoff, max 10 attempts)
   [Dispatcher Queue]
       ↓
   [Worker] ←──┐
       ↓       │ RETRY (low priority queue)
   [Sink]      │
       ↓       │
   [Loki API] ─┘
       ↓ (após max retries)
     [DLQ] ←───┐
       ↓       │ REPROCESS (Task 3: auto ou manual)
   [Reprocess Callback]
       ↓
   [Sink] (retry novamente)
```

**Pontos de falha:**
1. Monitor → retry queue (Task 2) ✅
2. Worker → low priority queue ⚠️ (não distingue erros)
3. Sink → DLQ (Task 3) ✅
4. DLQ → Reprocess → Sink (Task 3) ✅

---

## 8. Métricas de Retry Atuais

**Disponíveis:**
```
log_capturer_dispatcher_retries_total
log_capturer_file_monitor_retry_success_total
log_capturer_file_monitor_retry_failed_total
log_capturer_file_monitor_retry_giveup_total
log_capturer_dlq_reprocess_attempts_total{sink, result}
```

**Recomendadas para Task 5:**
```
log_capturer_retry_reason_total{component, reason}
log_capturer_timestamp_rejection_total{reason}
log_capturer_timestamp_clamped_total
log_capturer_retry_budget_exceeded_total
```

---

## 9. Recomendações para Task 5

### Task 5: Timestamp Learning e Clamping

**Implementar:**
1. **TimestampLearner** no Loki Sink:
   - Detectar "timestamp too old" responses
   - Extrair threshold do erro (se disponível)
   - Manter estado: `maxAcceptableAge time.Duration`
   
2. **Timestamp Validation** antes de send:
   - Check: `time.Since(entry.Timestamp) > maxAcceptableAge`
   - Se falhar: enviar direto para DLQ (não retry)
   
3. **Timestamp Clamping** (opcional):
   - Se timestamp muito antigo: clamp para `now - maxAcceptableAge`
   - Adicionar label: `_timestamp_clamped: "true"`
   - Configurable: `loki.clamp_old_timestamps: false` (default)

4. **Metrics:**
   - `loki_timestamp_too_old_total`
   - `loki_timestamp_clamped_total`
   - `loki_max_acceptable_age_seconds` (gauge)

**Arquivos a modificar:**
- `internal/sinks/loki_sink.go` - adicionar TimestampLearner
- `internal/metrics/metrics.go` - adicionar métricas
- `pkg/types/config.go` - adicionar `ClampOldTimestamps bool`
- `configs/config.yaml` - documentar opção

---

## 10. Conclusão

### ✅ O que está funcionando:
1. Dispatcher retry queue (alta/baixa prioridade)
2. Circuit breaker no Loki sink
3. **DLQ completo** (Task 3)
4. **File monitor retry** com exponential backoff (Task 2)

### ❌ Gaps críticos:
1. **Retry de erros permanentes** (timestamp too old)
   - Causa retry storm
   - Foi root cause do Hybrid Monitor failure
2. **Sem timestamp validation** antes de enviar
3. **Sem error parsing** nos sinks (400 sempre retries)

### 🎯 Next Steps (Task 5):
1. Implementar timestamp learning no Loki sink
2. Error-aware retry (parse 400 errors)
3. Timestamp clamping (opcional)
4. Métricas detalhadas por reason

**Prioridade máxima:** Resolver retry de "timestamp too old" para evitar repetir o failure do Hybrid Monitor.

---

**Documento gerado em:** 2025-11-07
**Próxima task:** Task 5 - Timestamp Learning e Clamping
