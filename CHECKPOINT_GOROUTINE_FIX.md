# 🔄 Checkpoint: Goroutine Leak Fix Progress

**Data de Início**: 2025-11-06 10:00 BRT
**Objetivo**: Corrigir vazamento de goroutines no log_capturer_go
**Status**: EM ANDAMENTO

---

## 📍 DESCOBERTAS CRÍTICAS

### Root Cause Identificado
O vazamento de goroutines **NÃO é falta de tracking com WaitGroup**. O problema é **arquitetural**:

**Arquivo**: `/internal/sinks/loki_sink.go`
- **Linha 447**: `go ls.sendBatch(entries)` - spawna goroutine para cada batch
- **Linha 922**: `go ls.sendBatch(batch)` - spawna goroutine no adaptive batcher

**Problema**:
1. Loki está **rejeitando batches** (erro de timestamp: "timestamp too new")
2. Cada `sendBatch` goroutine faz retry ao falhar, demorando para completar
3. Novos batches chegam a cada 5s → novas goroutines são criadas
4. **Acumulação**: Goroutines criadas mais rápido do que completam

**Evidências**:
- Antes do fix: 1525 goroutines (crescendo 36/min)
- Depois do fix parcial: 950 goroutines (crescendo 32/min)
- File descriptors: 752/1024 (73%) → 467/4096 (11%) ✅ Melhorou
- Container: UNHEALTHY
- Timezone: Container (UTC) vs Host (UTC-3) = 3h diferença

---

## ✅ FASE 1: FIXES APLICADOS (COMPLETO)

### 1.1 Goroutine Tracking nos Monitors
**Arquivo**: `internal/monitors/container_monitor.go`
- Linha 789: Buffer do canal aumentado 1→10
- Linha 792: Adicionado `mc.heartbeatWg.Add(1)`
- Linha 794: Adicionado `defer mc.heartbeatWg.Done()`

**Arquivo**: `internal/monitors/file_monitor.go`
- Linha 308: Adicionado `fm.wg.Add(1)`
- Linha 310: Adicionado `defer fm.wg.Done()`

### 1.2 File Descriptor Limits
**Arquivo**: `docker-compose.yml`
```yaml
ulimits:
  nofile:
    soft: 4096
    hard: 8192
```

### 1.3 Loki Batch Size
**Arquivo**: `configs/config.yaml`
- `batch_size`: 20000 → 500
- `batch_timeout`: 40s → 5s

### 1.4 Build e Deploy
- Build: ✅ Sucesso (sem erros)
- Docker: ✅ Todos containers rodando
- Health: ⚠️ UNHEALTHY (esperado, leak ainda presente)

---

## ✅ FASE 2: RESTART E TIMEZONE FIX (COMPLETO)

### 2.1 Restart Container
**Status**: ✅ Completo

**Resultado**:
- Container reiniciado com sucesso
- Goroutines resetadas: 950 → ~245-263
- Container status: HEALTHY
- Baseline limpo estabelecido

### 2.2 Fix Timezone Issue
**Status**: ✅ Completo

**Problema Identificado**:
- Rejected timestamp: `2025-11-06T15:29:47Z` (3 PM UTC)
- Actual log time: `2025-11-06T12:29:56Z` (12 PM UTC)
- **Diferença**: Exatamente 3 horas (timestamps no futuro)
- Loki rejeita com erro "timestamp too new"

**Solução Aplicada**:
1. ✅ Alterado `time.Now()` → `time.Now().UTC()` em 21 locais
2. ✅ Arquivos modificados:
   - `internal/monitors/container_monitor.go` (Timestamp + ProcessedAt)
   - `internal/monitors/file_monitor.go` (Timestamp + ProcessedAt)
   - `internal/dispatcher/dispatcher.go` (5 locais)
   - `pkg/dlq/dead_letter_queue.go` (4 locais)
   - `pkg/errors/errors.go` (1 local)
   - `pkg/profiling/goroutine_tracker.go` (1 local)
   - `pkg/buffer/disk_buffer.go` (1 local)
   - `pkg/slo/slo.go` (1 local)
   - `pkg/monitoring/resource_monitor.go` (6 locais)

3. ✅ Build e deploy realizados com sucesso

**Validação**:
- Timestamp errors: **2** (antes: centenas)
- Loki success rate: **50%** (2/4 batches)
- Redução de **99%+ nas rejeições**
- Goroutines: 199 (baseline limpo)

---

## 🔄 FASE 3: SEMAPHORE PATTERN (COMPLETO - PARCIAL)

**Status**: ✅ Implementado, mas **NÃO resolveu o leak**

### 3.1 Implementação
**Arquivo**: `internal/sinks/loki_sink.go`

**Mudanças**:
1. ✅ Adicionado campo `sendSemaphore chan struct{}` (max 15 concurrent)
2. ✅ Inicializado em `NewLokiSink()`
3. ✅ Wrapping de ambas chamadas `sendBatch`:
   - Linha 453: `flushBatch()` - wrapped
   - Linha 939: `adaptiveBatchLoop()` - wrapped

```go
// Acquire semaphore
ls.sendSemaphore <- struct{}{}

ls.sendWg.Add(1)
go func() {
    defer func() {
        <-ls.sendSemaphore // Release
    }()
    ls.sendBatch(batch)
}()
```

### 3.2 Teste e Validação
- Deploy: ✅ Sucesso (build sem erros)
- Baseline: 165 goroutines
- T+3min: 263 goroutines (+98)
- **Taxa**: 32 goroutines/min (INALTERADO!)

### 3.3 Análise do Fracasso
**Por que não funcionou**:
1. Semáforo limita **NOVAS** goroutines sendo criadas
2. Problema real: goroutines existentes **não estão terminando**
3. `sendBatch` trava em:
   - Retries bloqueantes quando Loki rejeita
   - Timeouts longos (30-240s)
   - Circuit breaker checks
   - Loki lento respondendo (400 errors)

**Evidência dos logs**:
```
"timestamp too new: 2025-11-06T15:29:37Z"
"entry too far behind... oldest acceptable: 2025-11-06T13:25:02Z"
```

**Conclusão**: Semáforo é útil, mas insuficiente. Precisamos worker pool.

---

## 🏗️ FASE 4: WORKER POOL REFACTOR (EM ANDAMENTO)

**Objetivo**: Limitar goroutines concorrentes de `sendBatch`

**Localização**: `internal/sinks/loki_sink.go`

**Implementação Planejada**:
```go
type LokiSink struct {
    // ... campos existentes
    sendSemaphore chan struct{} // Limitar a max 10-20 sends concorrentes
}

func NewLokiSink(...) {
    ls := &LokiSink{
        sendSemaphore: make(chan struct{}, 15), // Max 15 concurrent sends
    }
}

func (ls *LokiSink) Send(entries []types.LogEntry) error {
    // Adquirir semaphore
    ls.sendSemaphore <- struct{}{}

    ls.sendWg.Add(1)
    go func() {
        defer func() {
            <-ls.sendSemaphore // Liberar semaphore
        }()
        ls.sendBatch(entries)
    }()
}
```

---

## 🏗️ FASE 4: WORKER POOL REFACTOR (PENDENTE)

**Objetivo**: Substituir spawn ilimitado por worker pool fixo

**Design Proposto**:
```go
type LokiSink struct {
    batchQueue    chan []types.LogEntry // Canal para enfileirar batches
    workerCount   int                    // Número fixo de workers
    // ... outros campos
}

// Start pool de workers
func (ls *LokiSink) startWorkers() {
    for i := 0; i < ls.workerCount; i++ {
        ls.sendWg.Add(1)
        go ls.worker(i)
    }
}

// Worker processa batches da fila
func (ls *LokiSink) worker(id int) {
    defer ls.sendWg.Done()
    for {
        select {
        case <-ls.ctx.Done():
            return
        case batch := <-ls.batchQueue:
            ls.sendBatch(batch)
        }
    }
}

// Send apenas enfileira
func (ls *LokiSink) Send(entries []types.LogEntry) error {
    select {
    case ls.batchQueue <- entries:
        return nil
    case <-time.After(5 * time.Second):
        return ErrQueueFull
    }
}
```

---

## 🛡️ FASE 5: BACK-PRESSURE (PENDENTE)

**Objetivo**: Degradar gracefully quando Loki está lento

**Componentes**:
1. **Circuit Breaker**: Já existe, verificar se está funcionando
2. **Rate Limiting**: Reduzir taxa de envio quando Loki rejeita
3. **Queue Monitoring**: Alertar quando fila está cheia
4. **Fallback**: Enviar para DLQ quando Loki indisponível

---

## 📊 MÉTRICAS ESPERADAS (PÓS-FIX)

| Métrica | Antes | Atual | Target |
|---------|-------|-------|--------|
| Goroutines | 1525 | 950 | <100 |
| Growth Rate | 36/min | 32/min | 0/min |
| File Descriptors | 752/1024 | 467/4096 | <500/4096 |
| Memory | 115MB | ? | <200MB |
| Container Health | UNHEALTHY | UNHEALTHY | HEALTHY |
| Loki Success Rate | 4.3% | ? | >99% |

---

## 🚨 ISSUES CONHECIDOS

1. **Timezone Mismatch**: Container UTC vs Host UTC-3
2. **Loki Rejections**: "timestamp too new" errors
3. **Goroutine Accumulation**: Design arquitetural do Loki sink
4. **Slow Completions**: Retries tornam sendBatch lento

---

## 📝 COMANDOS ÚTEIS PARA DEBUGGING

```bash
# Check goroutines
curl -s http://localhost:8001/metrics | grep log_capturer_goroutines

# Check health
curl -s http://localhost:8401/health | jq '.services.goroutine_tracker.stats'

# Check container status
docker-compose ps log_capturer_go

# Check logs
docker logs log_capturer_go --tail 100

# Get goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof -http=:8080 goroutine.prof

# Check timezone
docker exec log_capturer_go date
date
```

---

## 🔄 PRÓXIMOS PASSOS

**AGORA**:
- [ ] Restart container (Fase 2.1)
- [ ] Verificar baseline de goroutines
- [ ] Fix timezone issue (Fase 2.2)

**DEPOIS**:
- [ ] Implementar semaphore (Fase 3)
- [ ] Refactor para worker pool (Fase 4)
- [ ] Adicionar back-pressure (Fase 5)
- [ ] Validar por 48h uptime

---

**Última Atualização**: 2025-11-06 10:03 BRT
**Próximo Checkpoint**: Após Fase 2 completa
