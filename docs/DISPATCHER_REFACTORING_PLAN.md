# Dispatcher Refactoring Plan

## 🎯 Objetivo

Reduzir a complexidade do `dispatcher.go` (1428 linhas) dividindo-o em componentes menores e mais testáveis, seguindo o **Single Responsibility Principle**.

---

## 📊 Análise Atual

### Problemas Identificados
- **1428 linhas** em um único arquivo
- **25 funções** com responsabilidades misturadas
- Dificulta testes unitários isolados
- Viola princípio de responsabilidade única
- Dificulta manutenção e evolução

### Responsabilidades Atuais do Dispatcher
1. ✅ Orquestração central (core)
2. ✅ Processamento de batches
3. ✅ Gerenciamento de retries
4. ✅ Integração com DLQ
5. ✅ Coleta de estatísticas
6. ✅ Gerenciamento de backpressure
7. ✅ Pool de workers
8. ✅ Integração com anomaly detection

---

## 🏗️ Nova Arquitetura

### Componentes Criados

#### 1. `batch_processor.go` (~190 linhas)
**Responsabilidade**: Processamento de batches de log entries

**Funções**:
- `ProcessBatch()` - Processa batch e envia para sinks
- `CollectBatch()` - Coleta itens da queue em batches
- `ValidateBatch()` - Valida batch antes de processar

**Benefícios**:
- Lógica de batching isolada e testável
- Fácil ajustar estratégias de batching
- Métricas de batching centralizadas

#### 2. `retry_manager.go` (~165 linhas)
**Responsabilidade**: Gerenciamento de retries e DLQ

**Funções**:
- `HandleFailedBatch()` - Processa batches que falharam
- `scheduleRetry()` - Agenda retry com backoff exponencial
- `retryWorker()` - Worker goroutine para retry
- `sendToDLQ()` - Envia para Dead Letter Queue
- `GetRetryStats()` - Estatísticas do retry queue
- `HandleCircuitBreaker()` - Circuit breaker para cascading failures

**Benefícios**:
- Previne goroutine explosion com semaphore
- Retry logic isolado para testes
- Integração DLQ encapsulada

#### 3. `stats_collector.go` (~185 linhas)
**Responsabilidade**: Coleta e reporte de estatísticas

**Funções**:
- `UpdateStats()` - Update thread-safe de stats
- `GetStats()` - Retorna cópia segura das stats
- `RunStatsUpdater()` - Goroutine de atualização periódica
- `UpdateBackpressureMetrics()` - Métricas para backpressure
- `IncrementProcessed/Errors/Throttled()` - Counters específicos

**Benefícios**:
- Estatísticas thread-safe
- Métricas Prometheus integradas
- Fácil adicionar novas métricas

---

## 🔄 Plano de Migração

### Fase 1: Componentes Criados (✅ COMPLETO)
- [x] Criar `batch_processor.go`
- [x] Criar `retry_manager.go`
- [x] Criar `stats_collector.go`

### Fase 2: Integração no Dispatcher Principal (PRÓXIMO)

#### 2.1. Atualizar estrutura do Dispatcher

```go
type Dispatcher struct {
	// Existing fields
	config          DispatcherConfig
	logger          *logrus.Logger
	processor       *processing.LogProcessor

	// NEW: Component instances
	batchProcessor  *BatchProcessor
	retryManager    *RetryManager
	statsCollector  *StatsCollector

	// Keep existing for compatibility
	sinks           []types.Sink
	queue           chan dispatchItem
	// ... rest of fields
}
```

#### 2.2. Atualizar NewDispatcher()

```go
func NewDispatcher(...) *Dispatcher {
	// ... existing initialization ...

	// Initialize new components
	batchProcessor := NewBatchProcessor(config, logger, enhancedMetrics)
	retryManager := NewRetryManager(config, logger, deadLetterQueue, ctx, &wg, maxConcurrentRetries)
	statsCollector := NewStatsCollector(&stats, &statsMutex, config, logger, queue)

	d := &Dispatcher{
		// ... existing fields ...
		batchProcessor: batchProcessor,
		retryManager:   retryManager,
		statsCollector: statsCollector,
	}

	return d
}
```

#### 2.3. Refatorar métodos existentes

**Antes** (dispatcher.go:768-816):
```go
func (d *Dispatcher) worker(workerID int) {
	// 50 linhas de lógica de worker
	batch := make([]dispatchItem, 0, d.config.BatchSize)
	// ... batching logic ...
	d.processBatch(batch, logger)
}
```

**Depois**:
```go
func (d *Dispatcher) worker(workerID int) {
	logger := d.logger.WithField("worker_id", workerID)
	logger.Info("Dispatcher worker started")
	defer logger.Info("Dispatcher worker stopped")

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			// Use BatchProcessor to collect batch
			batch, timedOut := d.batchProcessor.CollectBatch(d.ctx, d.queue)
			if len(batch) > 0 {
				d.processBatchWrapper(batch, logger)
			}
		}
	}
}

func (d *Dispatcher) processBatchWrapper(batch []dispatchItem, logger *logrus.Entry) {
	// Use BatchProcessor
	successCount, healthySinks, lastErr := d.batchProcessor.ProcessBatch(
		d.ctx,
		batch,
		d.sinks,
		d.anomalyDetector,
	)

	// Handle failures with RetryManager
	if healthySinks > 0 && successCount == 0 {
		d.retryManager.HandleCircuitBreaker(batch, lastErr)
	} else if successCount < healthySinks {
		d.retryManager.HandleFailedBatch(batch, lastErr, d.queue)
	}
}
```

#### 2.4. Migrar estatísticas

**Antes**:
```go
func (d *Dispatcher) updateStats(fn func(*types.DispatcherStats)) {
	d.statsMutex.Lock()
	defer d.statsMutex.Unlock()
	fn(&d.stats)
}
```

**Depois**:
```go
func (d *Dispatcher) updateStats(fn func(*types.DispatcherStats)) {
	d.statsCollector.UpdateStats(fn)
}

func (d *Dispatcher) GetStats() types.DispatcherStats {
	return d.statsCollector.GetStats()
}
```

### Fase 3: Componentes Adicionais (FUTURO)

#### 3.1. `worker_pool.go`
Gerenciamento de workers:
- Inicialização de workers
- Escalamento dinâmico
- Health monitoring de workers

#### 3.2. `backpressure_handler.go`
Gerenciamento de backpressure:
- Verificação de thresholds
- Throttling logic
- Low priority queue handling

---

## ✅ Benefícios da Refatoração

### Testabilidade
- ✅ Cada componente pode ser testado isoladamente
- ✅ Mocks mais simples e focados
- ✅ Coverage mais fácil de atingir 70%

### Manutenibilidade
- ✅ Código mais fácil de entender (< 200 linhas por arquivo)
- ✅ Mudanças isoladas em componentes específicos
- ✅ Responsabilidades claras

### Performance
- ✅ Sem overhead - componentes são structs leves
- ✅ Mesma performance do código original
- ✅ Mais fácil identificar gargalos

### Extensibilidade
- ✅ Fácil adicionar novos processadores de batch
- ✅ Fácil implementar novas estratégias de retry
- ✅ Fácil adicionar novas métricas

---

## 📋 Checklist de Implementação

### Fase 2 - Integração
- [ ] Atualizar struct `Dispatcher` com novos componentes
- [ ] Atualizar `NewDispatcher()` para criar componentes
- [ ] Refatorar `worker()` para usar `BatchProcessor`
- [ ] Refatorar `processBatch()` para usar `BatchProcessor`
- [ ] Refatorar `handleFailedBatch()` para usar `RetryManager`
- [ ] Refatorar `statsUpdater()` para usar `StatsCollector`
- [ ] Atualizar `GetStats()` para usar `StatsCollector`
- [ ] Atualizar `GetRetryQueueStats()` para usar `RetryManager`

### Validação
- [ ] Executar todos os testes: `go test -v ./internal/dispatcher`
- [ ] Executar race detector: `go test -race ./internal/dispatcher`
- [ ] Verificar métricas continuam funcionando
- [ ] Verificar DLQ continua funcionando
- [ ] Verificar retries funcionam corretamente

### Documentação
- [ ] Atualizar comentários no código
- [ ] Atualizar CLAUDE.md com nova arquitetura
- [ ] Criar exemplos de uso dos componentes
- [ ] Documentar padrões de teste

---

## 🚀 Próximos Passos

1. **Integrar componentes no dispatcher.go** principal
2. **Validar com testes** existentes
3. **Criar testes unitários** para cada componente
4. **Medir impacto** na cobertura de testes
5. **Documentar** arquitetura refatorada

---

## 📊 Métricas de Sucesso

| Métrica | Antes | Meta | Status |
|---------|-------|------|--------|
| Linhas por arquivo | 1428 | < 300 | 🔄 Em Progresso |
| Funções por arquivo | 25 | < 10 | 🔄 Em Progresso |
| Cobertura de testes | ~12% | 70% | ⏳ Pendente |
| Complexidade ciclomática | Alta | Média | 🔄 Em Progresso |

---

## 💡 Lições Aprendidas

- **Manter compatibilidade**: Componentes são additive, não substituem
- **Iterativo**: Refatoração em fases, não big bang
- **Testável**: Cada componente tem interface clara
- **Pragmático**: Foco em resolver problemas reais de manutenção

---

**Última atualização**: 2025-11-06
**Status**: Fase 1 Completa, Fase 2 Planejada
