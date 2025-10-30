# Plano de Ação - Correção de Problemas Críticos
## SSW Logs Capture - Sistema 100% Funcional

**Data de Início:** 2025-10-26
**Objetivo:** Corrigir os 12 problemas críticos identificados no code review
**Meta:** Sistema 100% funcional, testado e documentado

---

## 📋 Status Geral

| Fase | Problemas | Status | Progresso |
|------|-----------|--------|-----------|
| Fase 1 | C4, C1 | ⏳ Pendente | 0/2 |
| Fase 2 | C3, C9, C8 | ⏳ Pendente | 0/3 |
| Fase 3 | C2, C6, C10 | ⏳ Pendente | 0/3 |
| Fase 4 | C7, C11, C5, C12 | ⏳ Pendente | 0/4 |
| Validação | Testes | ⏳ Pendente | 0/5 |
| **TOTAL** | **12 + 5 Testes** | **0%** | **0/17** |

---

## 🎯 FASE 1: Problemas de Sincronização Crítica (Prioridade Máxima)

### ✅ C4: Circuit Breaker Mutex Lock Durante Execução
**Arquivo:** `pkg/circuit/breaker.go`
**Impacto:** CRÍTICO - Serializa todas as chamadas, destrói performance
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 1.1. Ler arquivo atual e entender implementação
- [ ] 1.2. Usar gopls para analisar referências ao método Execute
- [ ] 1.3. Refatorar Execute em 3 fases (pré-check, execução, pós-registro)
- [ ] 1.4. Garantir que mutex não é mantido durante fn()
- [ ] 1.5. Adicionar testes de concorrência
- [ ] 1.6. Validar com race detector

#### Critérios de Sucesso:
- ✓ Execute não mantém lock durante fn()
- ✓ Testes de concorrência passam
- ✓ `go test -race` sem erros
- ✓ Performance melhora em benchmark

#### Testes de Validação:
```go
// Test: Concurrent executions should run in parallel
// Test: State transitions are thread-safe
// Test: Half-open state works correctly
// Benchmark: Compare before/after throughput
```

---

### ✅ C1: Race Condition no Task Manager
**Arquivo:** `pkg/task_manager/task_manager.go`
**Impacto:** CRÍTICO - Deadlocks e panics não capturados
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 1.7. Analisar runTask e identificar todos os acessos a task
- [ ] 1.8. Remover funções aninhadas com locks
- [ ] 1.9. Implementar atualizações atômicas de estado
- [ ] 1.10. Corrigir defer com panic recovery
- [ ] 1.11. Adicionar testes de race conditions
- [ ] 1.12. Validar cleanup funciona corretamente

#### Critérios de Sucesso:
- ✓ Sem nested locks
- ✓ Panic recovery funciona
- ✓ `go test -race` sem erros
- ✓ Tasks completam corretamente sob carga

#### Testes de Validação:
```go
// Test: Panic recovery updates state correctly
// Test: Concurrent task operations are safe
// Test: Cleanup doesn't deadlock
```

---

## 🎯 FASE 2: Problemas de Resource Management

### ✅ C3: Deadlock no Local File Sink
**Arquivo:** `internal/sinks/local_file_sink.go`
**Impacto:** CRÍTICO - Sistema para quando disco cheio
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 2.1. Revisar isDiskSpaceAvailable e checkDiskSpaceAndCleanup
- [ ] 2.2. Eliminar unlock/relock manual dentro de defer
- [ ] 2.3. Refatorar para verificação sem lock + operação com lock
- [ ] 2.4. Adicionar testes de disco cheio
- [ ] 2.5. Testar com múltiplas goroutines verificando espaço
- [ ] 2.6. Validar com race detector

#### Critérios de Sucesso:
- ✓ Sem unlock/relock manual
- ✓ Operações de disco thread-safe
- ✓ Sistema continua funcionando com disco cheio
- ✓ Emergency cleanup funciona

---

### ✅ C9: Concurrent Map Access em LogEntry.Labels
**Arquivo:** `pkg/types/types.go`, `internal/sinks/local_file_sink.go`
**Impacto:** CRÍTICO - Panic em produção
**Tempo Estimado:** 3 horas

#### Passos:
- [ ] 2.7. Adicionar sync.RWMutex ao LogEntry
- [ ] 2.8. Implementar métodos thread-safe: GetLabel, SetLabel, CopyLabels
- [ ] 2.9. Usar gopls para encontrar TODOS os acessos a entry.Labels
- [ ] 2.10. Refatorar todos os acessos para usar métodos thread-safe
- [ ] 2.11. Atualizar formatTextOutput e outros formatadores
- [ ] 2.12. Adicionar testes de concurrent access

#### Critérios de Sucesso:
- ✓ Todos os acessos a Labels são thread-safe
- ✓ `go test -race` sem erros em todos os pacotes
- ✓ Testes de stress com 10k+ logs/segundo passam

---

### ✅ C8: File Descriptor Leak
**Arquivo:** `internal/sinks/local_file_sink.go`
**Impacto:** CRÍTICO - "too many open files"
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 2.13. Adicionar constante maxOpenFiles configurável
- [ ] 2.14. Implementar closeLeastRecentlyUsed (LRU)
- [ ] 2.15. Adicionar verificação de limite em getOrCreateLogFile
- [ ] 2.16. Adicionar métrica de arquivos abertos
- [ ] 2.17. Testar com 1000+ diferentes arquivos
- [ ] 2.18. Verificar que arquivos são reabertos conforme necessário

#### Critérios de Sucesso:
- ✓ Número de FDs limitado a maxOpenFiles
- ✓ LRU funciona corretamente
- ✓ Sem perda de logs
- ✓ Métricas mostram FD count estável

---

## 🎯 FASE 3: Problemas de Lifecycle e Memory

### ✅ C2: Context Leak no Anomaly Detector
**Arquivo:** `pkg/anomaly/detector.go`
**Impacto:** CRÍTICO - Goroutines não param
**Tempo Estimado:** 1.5 horas

#### Passos:
- [ ] 3.1. Adicionar campo cancel ao AnomalyDetector
- [ ] 3.2. Criar context com WithCancel em NewAnomalyDetector
- [ ] 3.3. Implementar cancelamento em Stop()
- [ ] 3.4. Verificar que periodicTraining respeita ctx.Done()
- [ ] 3.5. Adicionar testes de shutdown
- [ ] 3.6. Verificar que goroutines param com pprof

#### Critérios de Sucesso:
- ✓ Context é cancelado em Stop()
- ✓ Goroutines param dentro de 5 segundos
- ✓ Sem goroutine leaks (verificar com pprof)

---

### ✅ C6: Goroutine Leak no Loki Sink
**Arquivo:** `internal/sinks/loki_sink.go`
**Impacto:** CRÍTICO - Vazamento de memória
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 3.7. Adicionar WaitGroup para rastrear sendBatch goroutines
- [ ] 3.8. Modificar adaptiveBatchLoop para aguardar goroutines
- [ ] 3.9. Adicionar timeout em GetBatch
- [ ] 3.10. Garantir que loop sai em ctx.Done()
- [ ] 3.11. Testar shutdown com logs pendentes
- [ ] 3.12. Verificar com pprof que goroutines param

#### Critérios de Sucesso:
- ✓ Todas as goroutines param em Stop()
- ✓ Sem goroutine leaks
- ✓ Logs pendentes são processados ou salvos em DLQ

---

### ✅ C10: Memory Leak em Training Buffer
**Arquivo:** `pkg/anomaly/detector.go`
**Impacto:** CRÍTICO - OOM em produção
**Tempo Estimado:** 1.5 horas

#### Passos:
- [ ] 3.13. Refatorar addToTrainingBuffer
- [ ] 3.14. Usar realocação ao invés de reslice
- [ ] 3.15. Criar novo slice e copiar dados
- [ ] 3.16. Adicionar testes de memory usage
- [ ] 3.17. Verificar com pprof que memória é liberada
- [ ] 3.18. Testar com 100k+ entries

#### Critérios de Sucesso:
- ✓ Memória não cresce indefinidamente
- ✓ Buffer mantém tamanho correto
- ✓ pprof mostra memória liberada após limite

---

## 🎯 FASE 4: Problemas de Robustez e Validação

### ✅ C7: Unsafe JSON Marshal
**Arquivo:** `internal/sinks/loki_sink.go`
**Impacto:** MÉDIO-ALTO - Streams duplicados
**Tempo Estimado:** 1 hora

#### Passos:
- [ ] 4.1. Implementar createStreamKey sem JSON
- [ ] 4.2. Usar ordenação determinística de keys
- [ ] 4.3. StringBuilder para performance
- [ ] 4.4. Adicionar testes de determinismo
- [ ] 4.5. Benchmark comparativo

#### Critérios de Sucesso:
- ✓ Mesmos labels geram mesma key sempre
- ✓ Performance igual ou melhor
- ✓ Testes passam

---

### ✅ C11: HTTP Client Timeout
**Arquivo:** `internal/sinks/loki_sink.go`
**Impacto:** MÉDIO-ALTO - Requests bloqueados
**Tempo Estimado:** 1 hora

#### Passos:
- [ ] 4.6. Adicionar verificação de ctx.Done() em sendToLoki
- [ ] 4.7. Criar context com timeout adicional
- [ ] 4.8. Diferenciar timeout de cancelamento
- [ ] 4.9. Adicionar testes de timeout
- [ ] 4.10. Testar graceful shutdown

#### Critérios de Sucesso:
- ✓ Requests respeitam timeout
- ✓ Shutdown não trava
- ✓ Erros claros entre timeout e cancelamento

---

### ✅ C5: Race Condition no Dispatcher
**Arquivo:** `internal/dispatcher/dispatcher.go`
**Impacto:** CRÍTICO - Corrupção de dados
**Tempo Estimado:** 3 horas

#### Passos:
- [ ] 4.11. Ler e analisar dispatcher completo
- [ ] 4.12. Identificar compartilhamento de batches
- [ ] 4.13. Implementar ownership claro de batches
- [ ] 4.14. Usar channels para transferência
- [ ] 4.15. Cada worker tem batch próprio
- [ ] 4.16. Testes de race conditions

#### Critérios de Sucesso:
- ✓ Sem race conditions em batches
- ✓ `go test -race` passa
- ✓ Throughput mantido ou melhorado

---

### ✅ C12: Validação de Configuração
**Arquivo:** `internal/config/config.go`
**Impacto:** CRÍTICO - Crashes por config inválida
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 4.17. Criar método Validate() em Config
- [ ] 4.18. Validar todos os campos críticos
- [ ] 4.19. Ranges min/max para valores numéricos
- [ ] 4.20. Validar URLs e paths
- [ ] 4.21. Adicionar testes de validação
- [ ] 4.22. Chamar Validate() no load da config

#### Critérios de Sucesso:
- ✓ Configs inválidas são rejeitadas no startup
- ✓ Mensagens de erro claras
- ✓ Valores padrão aplicados quando faltam

---

## 🧪 FASE 5: Validação e Testes Integrados

### ✅ V1: Testes de Race Conditions
**Tempo Estimado:** 1 hora

#### Passos:
- [ ] 5.1. Executar `go test -race ./...` em todos os pacotes
- [ ] 5.2. Corrigir qualquer race detectada
- [ ] 5.3. Adicionar testes específicos de concorrência
- [ ] 5.4. Documentar resultados

#### Critérios:
- ✓ Zero race conditions detectadas
- ✓ Todos os testes passam com -race

---

### ✅ V2: Testes de Resource Leaks
**Tempo Estimado:** 1.5 horas

#### Passos:
- [ ] 5.5. Executar aplicação com pprof
- [ ] 5.6. Gerar carga de 10k logs/segundo por 5 minutos
- [ ] 5.7. Verificar goroutines com pprof
- [ ] 5.8. Verificar heap com pprof
- [ ] 5.9. Verificar file descriptors
- [ ] 5.10. Fazer shutdown e verificar cleanup

#### Critérios:
- ✓ Goroutines voltam a baseline após shutdown
- ✓ Memória estabiliza (não cresce indefinidamente)
- ✓ FDs retornam ao normal

---

### ✅ V3: Testes de Carga e Performance
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 5.11. Criar script de load testing
- [ ] 5.12. Testar com 10k logs/segundo
- [ ] 5.13. Testar com 50k logs/segundo
- [ ] 5.14. Testar com 100k logs/segundo
- [ ] 5.15. Monitorar métricas (latência, throughput, erros)
- [ ] 5.16. Verificar backpressure funciona
- [ ] 5.17. Verificar circuit breakers funcionam

#### Critérios:
- ✓ Throughput ≥ 50k logs/segundo
- ✓ Latência p99 < 100ms
- ✓ Zero crashes
- ✓ Backpressure ativa acima de 90% utilização

---

### ✅ V4: Testes de Failure Scenarios
**Tempo Estimado:** 1.5 horas

#### Passos:
- [ ] 5.18. Testar com Loki down
- [ ] 5.19. Testar com disco cheio
- [ ] 5.20. Testar com network lenta
- [ ] 5.21. Testar com configs inválidas
- [ ] 5.22. Testar shutdown durante alto load
- [ ] 5.23. Verificar DLQ funciona
- [ ] 5.24. Verificar recovery após falhas

#### Critérios:
- ✓ Sistema não crasha em falhas
- ✓ Logs vão para DLQ quando sinks falham
- ✓ Recovery automático funciona
- ✓ Graceful shutdown completo em < 30s

---

### ✅ V5: Testes de Integração End-to-End
**Tempo Estimado:** 2 horas

#### Passos:
- [ ] 5.25. Setup completo com Docker Compose
- [ ] 5.26. Testar monitoramento de containers
- [ ] 5.27. Testar monitoramento de arquivos
- [ ] 5.28. Testar pipelines de processamento
- [ ] 5.29. Verificar logs chegam no Loki
- [ ] 5.30. Verificar logs chegam em arquivos locais
- [ ] 5.31. Verificar métricas no Prometheus
- [ ] 5.32. Verificar dashboards no Grafana

#### Critérios:
- ✓ Todos os componentes funcionam juntos
- ✓ Logs fluem de ponta a ponta
- ✓ Métricas estão corretas
- ✓ Zero erros nos logs

---

## 📊 Métricas de Sucesso Final

### Performance
- [ ] Throughput ≥ 50,000 logs/segundo
- [ ] Latência p99 < 100ms
- [ ] CPU usage < 60% em carga normal
- [ ] Memory usage estável (não cresce)

### Confiabilidade
- [ ] Zero race conditions
- [ ] Zero goroutine leaks
- [ ] Zero memory leaks
- [ ] Zero file descriptor leaks
- [ ] Uptime > 99.9% em testes de 24h

### Funcionalidade
- [ ] Todos os sinks funcionando (Loki, Local File)
- [ ] Todos os monitors funcionando (Container, File)
- [ ] Todos os pipelines funcionando
- [ ] Anomaly detection funcionando
- [ ] Circuit breakers funcionando
- [ ] Backpressure funcionando
- [ ] DLQ funcionando
- [ ] Métricas completas

### Qualidade de Código
- [ ] `go test -race ./...` passa 100%
- [ ] Coverage ≥ 70%
- [ ] `go vet ./...` sem warnings
- [ ] `golangci-lint run` sem erros críticos
- [ ] Documentação atualizada

---

## 📝 Documentação Necessária

### Código
- [ ] Godoc em todas as funções públicas
- [ ] Comentários em código complexo
- [ ] Exemplos de uso

### Operacional
- [ ] README atualizado com novas features
- [ ] CHANGELOG com todas as correções
- [ ] Guia de troubleshooting atualizado
- [ ] Runbook de operação

### Desenvolvimento
- [ ] Guia de desenvolvimento atualizado
- [ ] Guia de testes
- [ ] Guia de debugging
- [ ] Arquitetura atualizada

---

## 🔧 Ferramentas e Comandos

### Desenvolvimento
```bash
# Build
go build -o ssw-logs-capture ./cmd/main.go

# Tests
go test ./...
go test -race ./...
go test -cover ./...
go test -coverprofile=coverage.out ./...

# Lint
go fmt ./...
go vet ./...
golangci-lint run

# Profile
go tool pprof http://localhost:8001/debug/pprof/heap
go tool pprof http://localhost:8001/debug/pprof/goroutine
```

### Validação
```bash
# Race detection
go test -race -count=10 ./pkg/circuit
go test -race -count=10 ./pkg/task_manager
go test -race -count=10 ./internal/sinks

# Memory profiling
go test -memprofile=mem.prof -bench=. ./...

# Load testing
./scripts/load-test.sh 10000  # 10k logs/sec
./scripts/load-test.sh 50000  # 50k logs/sec
```

---

## ⏱️ Cronograma Estimado

| Fase | Tempo | Acumulado |
|------|-------|-----------|
| Fase 1 | 4h | 4h |
| Fase 2 | 9h | 13h |
| Fase 3 | 5h | 18h |
| Fase 4 | 7h | 25h |
| Fase 5 | 8h | 33h |
| Documentação | 3h | 36h |
| Buffer | 4h | 40h |
| **TOTAL** | **40h** | **~5 dias** |

---

## 🎯 Definição de "DONE"

Uma correção está completa quando:
1. ✅ Código implementado e revisado
2. ✅ Testes unitários escritos e passando
3. ✅ `go test -race` passa sem erros
4. ✅ Validação manual realizada
5. ✅ Documentação atualizada
6. ✅ Code review por MCP gopls
7. ✅ Integrado e testado com resto do sistema

O projeto está completo quando:
1. ✅ Todos os 12 problemas críticos corrigidos
2. ✅ Todos os 5 grupos de testes de validação passam
3. ✅ Métricas de sucesso atingidas
4. ✅ Documentação completa
5. ✅ Sistema rodando 24h sem crashes em ambiente de teste
6. ✅ Load test de 50k logs/segundo passa
7. ✅ Code review final aprovado

---

## 🚀 Próximo Passo

**INICIAR FASE 1 - Problema C4: Circuit Breaker**

Status: ⏳ PRONTO PARA COMEÇAR
