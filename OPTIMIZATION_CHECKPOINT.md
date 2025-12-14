# Checkpoint de Otimizações - log_capturer_go

**Data de Início:** 2025-11-20
**Status:** 🟡 EM PROGRESSO
**Branch:** new_teste

---

## Tarefas Planejadas

### 1. ✅ Ativar CopyModeOptimized
- **Arquivo:** `internal/dispatcher/batch_processor.go`
- **Mudança:** Alterar default de `CopyModeSafe` para `CopyModeOptimized`
- **Status:** ⬜ PENDENTE
- **Impacto:** ~45% redução de memória por batch

### 2. ⬜ Implementar Atomic Stats
- **Arquivo:** `internal/dispatcher/dispatcher.go`
- **Mudança:** Substituir `statsMutex` por `sync/atomic` para contadores
- **Status:** ⬜ PENDENTE
- **Impacto:** 5-15% melhoria de throughput

### 3. ⬜ Remover código retry antigo
- **Arquivo:** `internal/dispatcher/dispatcher.go`
- **Mudança:** Remover fallback goroutine-per-retry (linhas ~1097-1172)
- **Status:** ⬜ PENDENTE
- **Impacto:** Código mais limpo, menos complexidade

### 4. ⬜ Migrar LabelsCOW para LogEntry
- **Arquivo:** `pkg/types/types.go`
- **Mudança:** Substituir `Labels map[string]string` por `*LabelsCOW`
- **Status:** ⬜ PENDENTE
- **Impacto:** Economia de memória em toda pipeline

---

## Arquivos a Modificar

```
internal/dispatcher/batch_processor.go   - Tarefa 1
internal/dispatcher/dispatcher.go        - Tarefas 2, 3
pkg/types/types.go                       - Tarefa 4
pkg/types/labels_cow.go                  - Tarefa 4 (já existe)
```

---

## Progresso Detalhado

### Tarefa 1: Ativar CopyModeOptimized

**Antes:**
```go
// batch_processor.go linha ~42
copyMode: CopyModeSafe, // Default to safe mode
```

**Depois:**
```go
copyMode: CopyModeOptimized, // Default to optimized mode (shallow copy)
```

**Checklist:**
- [ ] Alterar default em NewBatchProcessor
- [ ] Verificar documentação do CopyMode
- [ ] Rodar testes com race detector

---

### Tarefa 2: Implementar Atomic Stats

**Campos a migrar para atomic:**
- [ ] `TotalProcessed`
- [ ] `TotalErrors`
- [ ] `QueueSize`
- [ ] `DroppedLogs`
- [ ] Outros contadores no DispatcherStats

**Padrão:**
```go
// Antes
d.statsMutex.Lock()
d.stats.TotalProcessed++
d.statsMutex.Unlock()

// Depois
atomic.AddInt64(&d.stats.TotalProcessed, 1)
```

**Checklist:**
- [ ] Identificar todos os campos de stats
- [ ] Criar nova struct com campos atomic
- [ ] Substituir todos os acessos Lock/Unlock
- [ ] Atualizar métodos GetStats()
- [ ] Rodar testes com race detector

---

### Tarefa 3: Remover código retry antigo

**Localização:** `dispatcher.go` linhas ~1097-1172

**Código a remover:**
- Fallback que cria goroutine por retry
- Semáforo de retry (`retrySemaphore`)
- Lógica de `time.AfterFunc` para retries

**Checklist:**
- [ ] Identificar todo código do retry antigo
- [ ] Verificar que RetryManagerV2 cobre todos os casos
- [ ] Remover código morto
- [ ] Remover campo retrySemaphore se não usado
- [ ] Rodar testes

---

### Tarefa 4: Migrar LabelsCOW para LogEntry

**Mudança em types.go:**
```go
// Antes
type LogEntry struct {
    Labels map[string]string
    // ...
}

// Depois
type LogEntry struct {
    Labels *LabelsCOW
    // ...
}
```

**Impacto em cascata:**
- [ ] Atualizar todos os acessos a entry.Labels
- [ ] Usar entry.Labels.Get(key) em vez de entry.Labels[key]
- [ ] Usar entry.Labels.Set(key, value) em vez de entry.Labels[key] = value
- [ ] Atualizar DeepCopy() de LogEntry
- [ ] Atualizar serialização JSON
- [ ] Atualizar todos os sinks
- [ ] Atualizar todos os monitors
- [ ] Atualizar testes

---

## Comandos de Verificação

```bash
# Rodar testes com race detector
go test -race ./internal/dispatcher/... -timeout 60s
go test -race ./pkg/types/... -timeout 60s

# Verificar build
go build ./...

# Verificar diagnósticos gopls
# (usar MCP gopls tools)

# Benchmark comparativo (após implementação)
go test -bench=. -benchmem ./internal/dispatcher/...
```

---

## Rollback

Se algo der errado, usar git para reverter:

```bash
# Ver mudanças
git diff

# Reverter arquivo específico
git checkout -- <arquivo>

# Reverter tudo
git checkout -- .
```

---

## Notas de Implementação

### Ordem de Implementação Recomendada

1. **Tarefa 1** (CopyModeOptimized) - Mudança simples, alto impacto
2. **Tarefa 2** (Atomic Stats) - Mudança média, melhora performance
3. **Tarefa 3** (Remover retry antigo) - Limpeza, depende de validar V2
4. **Tarefa 4** (LabelsCOW) - Mudança maior, mais arquivos afetados

### Riscos

- **Tarefa 1:** Sinks que modificam entries diretamente podem quebrar
- **Tarefa 2:** Race conditions se não migrar todos os acessos
- **Tarefa 3:** Perda de fallback se RetryManagerV2 tiver bug
- **Tarefa 4:** Muitos arquivos afetados, maior chance de regressão

---

## Log de Execução

| Data/Hora | Tarefa | Ação | Resultado |
|-----------|--------|------|-----------|
| 2025-11-20 | Setup | Criado checkpoint | ✅ |
| | | | |

---

## Última Atualização

**Timestamp:** 2025-11-20
**Próxima ação:** Iniciar Tarefa 1 (Ativar CopyModeOptimized)
