# 🔍 Final Goroutine Leak Investigation Report

**Data**: 2025-11-06
**Duração**: ~3 horas de análise e fixes
**Status**: ⚠️  **LEAK PERSISTE** - Causa raiz ainda não resolvida

---

## 📊 Executive Summary

Foram aplicadas **múltiplas fixes** para resolver o vazamento de goroutines no `log_capturer_go`, mas o leak **persiste** a uma taxa de **~31 goroutines/min**.

### Métricas Finais
| Métrica | Antes | Depois Fixes | Target | Status |
|---------|-------|--------------|--------|--------|
| Goroutines | 1525 | 700+ | <100 | ❌ |
| Growth Rate | 36/min | 31/min | 0/min | ❌ |
| File Descriptors | 752/1024 (73%) | ~400/4096 (10%) | <20% | ✅ |
| Loki Success Rate | 4.3% | 97.5% (80/82) | >99% | ✅ |
| Timestamp Errors | Centenas | 2 | 0 | ✅ |
| Container Health | UNHEALTHY | UNHEALTHY | HEALTHY | ❌ |

**Sucessos Parciais**:
- ✅ Timezone fix reduziu rejeições do Loki de 95% → 2%
- ✅ File descriptors melhoraram drasticamente
- ❌ Goroutine leak AINDA ATIVO (~14% redução de taxa)

---

## ✅ FIXES APLICADOS

### 1. Timezone UTC Fix
**Problema**: Timestamps 3h no futuro causavam rejeição do Loki
**Solução**: Alterado `time.Now()` → `time.Now().UTC()` em 21 locais

**Arquivos**:
- `internal/monitors/container_monitor.go`
- `internal/monitors/file_monitor.go`
- `internal/dispatcher/dispatcher.go`
- `pkg/*` (diversos)

**Resultado**: ✅ Rejeições reduziram 99%+

### 2. File Descriptor Limits
**Problema**: Limite de 1024 FDs muito baixo
**Solução**: Aumentado para 4096 (soft) / 8192 (hard)

**Arquivo**: `docker-compose.yml`
```yaml
ulimits:
  nofile:
    soft: 4096
    hard: 8192
```

**Resultado**: ✅ Utilização caiu de 73% → 10%

### 3. Loki Batch Size
**Problema**: Batches gigantes (20K) rejeitados
**Solução**: Reduzido para 500 entries, timeout 40s → 5s

**Arquivo**: `configs/config.yaml`
**Resultado**: ✅ Loki aceitando batches

### 4. Semaphore Pattern
**Problema**: Goroutines ilimitadas de `sendBatch`
**Solução**: Adicionado semáforo limitando a 15 concurrent

**Arquivo**: `internal/sinks/loki_sink.go`
- Campo `sendSemaphore chan struct{}`
- Acquire/release ao redor de spawns

**Resultado**: ❌ Sem impacto (31 → 31/min)

### 5. Worker Pool Architecture
**Problema**: Spawn ilimitado de goroutines
**Solução**: Pool fixo de 10 workers processando fila de batches

**Implementação**:
```go
type LokiSink struct {
    batchQueue  chan []types.LogEntry  // Queue de batches
    workerCount int                     // 10 workers fixos
    workersWg   sync.WaitGroup         // Track workers
}

func (ls *LokiSink) startWorkers() {
    for i := 0; i < 10; i++ {
        go ls.worker(i)  // Fixed workers
    }
}

func (ls *LokiSink) worker(id int) {
    for batch := range ls.batchQueue {
        ls.sendBatch(batch)  // Process from queue
    }
}
```

**Refatorações**:
- `flushBatch()`: Enfileira ao invés de spawnar
- `adaptiveBatchLoop()`: Enfileira ao invés de spawnar

**Resultado**: ❌ Sem impacto (31 → 31/min)

---

## 🔍 ROOT CAUSE ANALYSIS

### Por Que as Fixes Não Funcionaram?

**Hipótese Original** (INCORRETA):
- Pensávamos que goroutines eram criadas ilimitadamente
- Semáforo/worker pool limitariam criação

**Realidade Descoberta**:
- Workers são LIMITADOS (10 para Loki, 6 para dispatcher, 3 para file)
- Mas goroutines AINDA crescem 31/min
- **Conclusão**: O problema não é CRIAÇÃO, é que goroutines **não estão terminando**

### Evidências

1. **Workers iniciando corretamente**:
```
"Starting Loki sink worker pool", "worker_count":10
"Dispatcher worker started", "worker_id":0...5
"Started local file sink workers", "worker_count":3
```

2. **Baseline alto suspeito**:
- Após restart: ~165 goroutines (esperado: <50)
- Após 5 min: ~700 goroutines
- **730+ goroutines** além dos workers fixos!

3. **Loki funcionando bem**:
- Success rate: 97.5% (80/82 batches)
- Apenas 2 timestamp errors
- Workers processando normalmente

### 🎯 Causa Raiz Provável

**As goroutines que vazam NÃO SÃO dos sinks!**

Possíveis fontes:
1. **Monitor de containers**: `readContainerLogs()` spawna goroutine por container, mas pode não estar terminando
2. **File monitor**: `readFile()` pode spawnar múltiplas goroutines
3. **Dispatcher**: Algum loop ou retry mechanism
4. **Circuit breaker**: Pode estar criando goroutines de monitoring
5. **Adaptive batcher**: Pode ter workers próprios
6. **Metrics/monitoring**: Coletores podem estar spawnando

---

## 🔬 PRÓXIMAS INVESTIGAÇÕES NECESSÁRIAS

### Análise com pprof

```bash
# Obter profile de goroutines
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof
go tool pprof -top goroutine.prof

# Ver stacks das goroutines
go tool pprof -http=:8080 goroutine.prof
```

**Procurar por**:
- Stacks com >50 goroutines do mesmo tipo
- Funções bloqueadas (chan receive, mutex, select)
- Goroutines em loops infinitos

### Análise de Código

**Verificar todos `go func()` em**:
```bash
grep -rn "go func\|go .*\.\|\.Add(1)" internal/ pkg/ --include="*.go" | grep -v "_test"
```

**Focar em**:
- Monitors (container_monitor.go, file_monitor.go)
- Adaptive batcher
- Circuit breaker
- Retry managers
- Metrics collectors

### Teste de Isolamento

1. **Desabilitar sinks um por um**:
```yaml
sinks:
  loki:
    enabled: false  # Test sem Loki
  local_file:
    enabled: false  # Test sem file sink
```

2. **Desabilitar monitors**:
```yaml
monitors:
  containers:
    enabled: false  # Test sem container monitoring
  files:
    enabled: false  # Test sem file monitoring
```

3. **Reduzir workers**:
```yaml
dispatcher:
  workers: 1  # Minimal workers
```

**Objetivo**: Identificar qual componente está vazando

---

## 📝 ARQUITETURA ATUAL

### Goroutines Legítimas (Esperadas)

| Componente | Goroutines | Nota |
|------------|------------|------|
| Loki Sink Workers | 10 | Fixed pool |
| Local File Sink Workers | 3 | Fixed pool |
| Dispatcher Workers | 6 | Fixed pool |
| Adaptive Batcher | 1-2 | Loop + timer |
| Container Monitor Heartbeats | 8 | 1 por container |
| Container Log Readers | 8 | 1 por container |
| File Monitor Readers | 6 | 1 por arquivo |
| Metrics Collectors | ~5 | Goroutine tracker, etc |
| HTTP Servers | ~10 | API + metrics |
| **Total Esperado** | **~60** | ✅ Razoável |
| **Realidade** | **700+** | ❌ **640+ leaked!** |

### Goroutines Vazadas (640+)

**Origem desconhecida**. Necessário pprof para identificar.

---

## 🛠️ PRÓXIMOS PASSOS RECOMENDADOS

### URGENTE (Próximas Horas)

1. **Habilitar pprof** (se não estiver)
```go
import _ "net/http/pprof"
```

2. **Capturar goroutine profile**
```bash
curl http://localhost:6060/debug/pprof/goroutine > /tmp/goroutines.prof
go tool pprof -top /tmp/goroutines.prof | head -30
```

3. **Identificar top stacks** com >50 goroutines

4. **Adicionar tracking detalhado** nesse código

### CURTO PRAZO (Esta Semana)

1. **Implementar goroutine naming**
```go
go func() {
    runtime.SetGoroutineName("loki-worker-1")
    // ...
}()
```

2. **Adicionar métricas de goroutines por tipo**
```go
metrics.SetGauge("goroutines_loki_workers", float64(count))
metrics.SetGauge("goroutines_monitors", float64(count))
```

3. **Teste de carga controlado**
- Monitorar com 1, 5, 10 containers
- Ver correlação entre containers e goroutines

### MÉDIO PRAZO (Próximo Sprint)

1. **Refatorar monitors** para usar pools como os sinks

2. **Implementar timeout agressivo** em todas goroutines
```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
```

3. **Adicionar circuit breaker** para spawn de goroutines
```go
if currentGoroutines > threshold {
    return ErrTooManyGoroutines
}
```

---

## 💡 LIÇÕES APRENDIDAS

### O Que Funcionou

1. **Timezone fix**: Simple but effective
2. **Worker pools**: Boa arquitetura, mas aplicado no lugar errado
3. **Documentação**: Checkpoint permitiu continuidade
4. **Instrumentação**: Métricas ajudaram a identificar problema

### O Que Não Funcionou

1. **Assumption-based fixes**: Assumimos que sinks eram o problema
2. **Semaphore**: Limita criação, não resolve goroutines travadas
3. **Falta de profiling**: Deveríamos ter começado com pprof
4. **Teste insuficiente**: Cada fix deveria ter 30min de monitoring

### Melhores Práticas Identificadas

1. **SEMPRE começar com profiling** antes de fixes
2. **Teste de isolamento** para identificar componente
3. **Métricas granulares** (goroutines por componente)
4. **Monitoring de longo prazo** (48h mínimo)

---

## 📊 DADOS HISTÓRICOS

### Timeline de Fixes

| Hora | Fix | Goroutines (baseline → +5min) | Taxa |
|------|-----|-------------------------------|------|
| 09:00 | Inicio | 1525 | 36/min |
| 09:30 | Timezone | 293 → 358 | 13/min (??) |
| 10:00 | Semaphore | 165 → 263 | 20/min |
| 10:30 | Worker Pool | 544 → 701 | 31/min |

**Observação estranha**: A taxa VARIOU entre 13-36/min, sugerindo:
- Restart limpa goroutines acumuladas
- Taxa de "leak puro" pode ser ~13-20/min
- Após certo ponto, taxa acelera (deadlock cascade?)

---

## 🎯 CRITÉRIOS DE SUCESSO

Para considerar o problema **resolvido**:

1. ✅ Goroutine count < 100 após 1h uptime
2. ✅ Growth rate < 1 goroutine/min
3. ✅ Container health: HEALTHY
4. ✅ 48h uptime sem restart
5. ✅ Memory usage estável (<200MB)
6. ✅ CPU usage <10%

**Status Atual**: 0/6 critérios atingidos ❌

---

## 📞 CONTATOS E RECURSOS

**Documentos Relacionados**:
- `CHECKPOINT_GOROUTINE_FIX.md` - Progresso detalhado
- `EXECUTIVE_SUMMARY.md` - Visão executiva
- `GOROUTINE_LEAK_FIX_PATCH.md` - Tentativa de fix original

**Comandos Úteis**:
```bash
# Monitor goroutines
watch -n 10 'curl -s http://localhost:8001/metrics | grep goroutines'

# Health check
curl -s http://localhost:8401/health | jq '.services.goroutine_tracker'

# Goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > /tmp/g.prof
go tool pprof -top /tmp/g.prof

# Restart limpo
docker-compose restart log_capturer_go
```

---

**Conclusão**: O problema é **mais profundo** do que sinks. Necessário análise com pprof para identificar fonte real do leak.

**Próximo Passo**: Capturar e analisar goroutine profile com pprof.

**Última Atualização**: 2025-11-06 15:35 UTC
