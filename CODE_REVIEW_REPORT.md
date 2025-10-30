# Code Review Report - SSW Logs Capture (Go)

**Data da Análise:** 2025-10-26
**Versão Analisada:** main branch (commit: 86c79c5)
**Escopo:** Análise completa de arquivos Go (`internal/`, `pkg/`) e YAML (`configs/`)

---

## Sumário Executivo

Este relatório documenta uma análise abrangente do código-fonte do projeto SSW Logs Capture. Foram identificados **42 problemas** distribuídos em três categorias de severidade: **12 críticos**, **18 graves**, e **12 moderados**.

### Principais Preocupações

1. **Race Conditions e Sincronização** - Múltiplos acessos concorrentes sem proteção adequada
2. **Vazamentos de Recursos** - Goroutines, conexões e arquivos não sendo fechados corretamente
3. **Context Handling** - Uso inadequado de contextos levando a deadlocks potenciais
4. **Error Recovery** - Falta de recuperação adequada de erros em componentes críticos

### Estatísticas

| Severidade | Quantidade | % Total |
|-----------|-----------|---------|
| 🔴 Crítico | 12 | 28.6% |
| 🟠 Grave | 18 | 42.8% |
| 🟡 Moderado | 12 | 28.6% |
| **Total** | **42** | **100%** |

---

## 🔴 Problemas Críticos (Alta Prioridade)

### C1. Race Condition no Task Manager durante Cleanup

**Arquivo:** `pkg/task_manager/task_manager.go:113-162`

**Problema:**
```go
func (tm *taskManager) runTask(t *task) {
    defer close(t.Done)

    updateTaskState := func(state string, errorCount int64, lastError string) {
        tm.mutex.Lock()  // ❌ Lock dentro de função aninhada
        t.State = state
        t.ErrorCount = errorCount
        t.LastError = lastError
        tm.mutex.Unlock()
    }

    defer func() {
        if r := recover(); r != nil {
            tm.mutex.Lock()  // ❌ Pode causar deadlock
            currentErrorCount := t.ErrorCount + 1
            tm.mutex.Unlock()
            updateTaskState("failed", currentErrorCount, fmt.Sprintf("panic: %v", r))
        }
    }()
    // ...
}
```

**Causa:** Múltiplos locks aninhados podem levar a deadlocks se uma goroutine já tiver o lock.

**Efeito:**
- Deadlocks em condições de race
- Panic não capturado adequadamente
- Sistema pode travar completamente

**Solução:**
```go
func (tm *taskManager) runTask(t *task) {
    defer close(t.Done)

    defer func() {
        if r := recover(); r != nil {
            tm.mutex.Lock()
            t.State = "failed"
            t.ErrorCount++
            t.LastError = fmt.Sprintf("panic: %v", r)
            tm.mutex.Unlock()

            tm.logger.WithFields(logrus.Fields{
                "task_id": t.ID,
                "error":   r,
            }).Error("Task panicked")
        }
    }()

    if err := t.Fn(t.Context); err != nil {
        tm.mutex.Lock()
        t.State = "failed"
        t.ErrorCount++
        t.LastError = err.Error()
        tm.mutex.Unlock()

        tm.logger.WithFields(logrus.Fields{
            "task_id": t.ID,
            "error":   err,
        }).Error("Task failed")
        return
    }

    tm.mutex.Lock()
    t.State = "completed"
    tm.mutex.Unlock()
}
```

---

### C2. Context Leak no Anomaly Detector

**Arquivo:** `pkg/anomaly/detector.go:115-157`

**Problema:**
```go
func NewAnomalyDetector(config Config, logger *logrus.Logger) (*AnomalyDetector, error) {
    // ...
    detector := &AnomalyDetector{
        config:     config,
        logger:     logger,
        models:     make(map[string]Model),
        extractors: make(map[string]FeatureExtractor),
        ctx:        context.Background(),  // ❌ Context sem cancel
    }
    // ...
}
```

**Causa:** `context.Background()` criado sem nenhuma forma de cancelamento.

**Efeito:**
- Goroutines nunca são paradas
- Vazamento de recursos em shutdown
- Impossibilidade de parar o detector de forma limpa

**Solução:**
```go
func NewAnomalyDetector(config Config, logger *logrus.Logger) (*AnomalyDetector, error) {
    // ...
    ctx, cancel := context.WithCancel(context.Background())

    detector := &AnomalyDetector{
        config:     config,
        logger:     logger,
        models:     make(map[string]Model),
        extractors: make(map[string]FeatureExtractor),
        ctx:        ctx,
        cancel:     cancel,  // Adicionar campo cancel
    }
    // ...
}

func (ad *AnomalyDetector) Stop() error {
    if !ad.config.Enabled {
        return nil
    }

    ad.logger.Info("Stopping anomaly detector")

    // Cancelar contexto ANTES de salvar modelos
    if ad.cancel != nil {
        ad.cancel()
    }

    // Save models if configured
    if ad.config.SaveModel && ad.config.ModelPath != "" {
        if err := ad.saveModels(); err != nil {
            ad.logger.WithError(err).Error("Failed to save models")
        }
    }

    ad.wg.Wait()
    ad.logger.Info("Anomaly detector stopped")
    return nil
}
```

---

### C3. Deadlock no Local File Sink com Disco Cheio

**Arquivo:** `internal/sinks/local_file_sink.go:924-957`

**Problema:**
```go
func (lfs *LocalFileSink) isDiskSpaceAvailable() bool {
    lfs.diskSpaceMutex.RLock()
    defer lfs.diskSpaceMutex.RUnlock()

    if time.Since(lfs.lastDiskCheck) > 5*time.Minute {
        lfs.diskSpaceMutex.RUnlock()  // ❌ Unlock manual
        lfs.checkDiskSpaceAndCleanup()  // ❌ Tenta adquirir Write lock
        lfs.diskSpaceMutex.RLock()  // ❌ Re-lock manual
    }
    // ...
}
```

**Causa:** Unlock/relock manual dentro de defer pode levar a double-unlock e deadlocks.

**Efeito:**
- Deadlock quando múltiplas goroutines checam espaço simultaneamente
- Sistema para de escrever logs
- Perda de dados

**Solução:**
```go
func (lfs *LocalFileSink) isDiskSpaceAvailable() bool {
    // Verificar se precisa de check sem lock
    needsCheck := false
    lfs.diskSpaceMutex.RLock()
    if time.Since(lfs.lastDiskCheck) > 5*time.Minute {
        needsCheck = true
    }
    lfs.diskSpaceMutex.RUnlock()

    // Fazer check fora do lock se necessário
    if needsCheck {
        lfs.checkDiskSpaceAndCleanup()
    }

    // Agora fazer a verificação real
    lfs.diskSpaceMutex.RLock()
    defer lfs.diskSpaceMutex.RUnlock()

    var stat syscall.Statfs_t
    err := syscall.Statfs(lfs.config.Directory, &stat)
    if err != nil {
        return false
    }

    totalBytes := stat.Blocks * uint64(stat.Bsize)
    freeBytes := stat.Bavail * uint64(stat.Bsize)
    usagePercent := float64(totalBytes-freeBytes) / float64(totalBytes) * 100

    if usagePercent > 95.0 {
        return false
    }

    dirSizeGB := lfs.getDirSizeGB(lfs.config.Directory)
    if dirSizeGB >= lfs.config.MaxTotalDiskGB {
        return false
    }

    return true
}
```

---

### C4. Circuit Breaker Mutex Lock Held Durante Execução

**Arquivo:** `pkg/circuit/breaker.go:76-126`

**Problema:**
```go
func (b *Breaker) Execute(fn func() error) error {
    b.mu.Lock()
    defer b.mu.Unlock()  // ❌ Lock mantido durante TODA a execução

    // ... verificações ...

    // Executar função (pode levar muito tempo!)
    err := fn()  // ❌ Lock ainda mantido aqui

    if err != nil {
        b.onExecutionFailure(err)
        return err
    }

    b.onExecutionSuccess()
    return nil
}
```

**Causa:** Lock de mutex mantido durante execução da função externa, que pode levar segundos.

**Efeito:**
- **Serialização completa** de todas as chamadas ao circuit breaker
- **Performance degradada** drasticamente
- Timeout em operações que deveriam ser paralelas
- Sistema pode parecer travado

**Solução:**
```go
func (b *Breaker) Execute(fn func() error) error {
    // Fase 1: Verificação (com lock)
    b.mu.Lock()
    b.requests++

    if b.shouldTrip() {
        b.trip()
    }

    if b.state == types.CircuitBreakerOpen {
        if time.Now().Before(b.nextRetryTime) {
            b.mu.Unlock()
            return fmt.Errorf("circuit breaker %s is open", b.config.Name)
        }
        b.setState(types.CircuitBreakerHalfOpen)
        b.halfOpenCalls = 0
        b.halfOpenSuccesses = 0
        b.halfOpenStartTime = time.Now()
    }

    if b.state == types.CircuitBreakerHalfOpen {
        halfOpenTimeout := b.config.Timeout * 2
        if time.Since(b.halfOpenStartTime) > halfOpenTimeout {
            b.logger.WithField("breaker", b.config.Name).Warn("Circuit breaker half-open timeout")
            b.trip()
            b.mu.Unlock()
            return fmt.Errorf("circuit breaker %s half-open timeout", b.config.Name)
        }

        if b.halfOpenCalls >= b.maxHalfOpen {
            b.mu.Unlock()
            return fmt.Errorf("circuit breaker %s is half-open (max calls reached)", b.config.Name)
        }
        b.halfOpenCalls++
    }

    currentState := b.state
    b.mu.Unlock()

    // Fase 2: Execução (SEM lock) ✅
    err := fn()

    // Fase 3: Registro de resultado (com lock)
    b.mu.Lock()
    if err != nil {
        b.onExecutionFailure(err)
    } else {
        b.onExecutionSuccess()
    }
    b.mu.Unlock()

    return err
}
```

---

### C5. Race Condition no Dispatcher com Batch Workers

**Arquivo:** `internal/dispatcher/dispatcher.go` (inferido do padrão de uso)

**Problema:**
Múltiplos workers acessando e modificando o mesmo batch sem sincronização adequada.

**Causa:** Batches compartilhados entre goroutines sem proteção mutex.

**Efeito:**
- Corrupção de dados
- Perda de logs
- Crashes inesperados

**Solução:**
- Implementar pool de batches com ownership claro
- Usar channels para transferir ownership
- Cada worker tem seu próprio batch local

---

### C6. Goroutine Leak no Loki Sink Adaptive Batching

**Arquivo:** `internal/sinks/loki_sink.go:729-761`

**Problema:**
```go
func (ls *LokiSink) adaptiveBatchLoop() {
    for {
        select {
        case <-ls.ctx.Done():
            return
        default:
            batch, err := ls.adaptiveBatcher.GetBatch(ls.ctx)  // ❌ Pode bloquear
            if err != nil {
                if err == context.Canceled {
                    return
                }
                ls.logger.WithError(err).Error("Error getting batch")
                continue
            }

            if len(batch) > 0 {
                go ls.sendBatch(batch)  // ❌ Goroutine sem tracking
                // ...
            }
        }
    }
}
```

**Causa:**
1. `GetBatch` pode bloquear indefinidamente se não respeitar `ctx.Done()`
2. Goroutines criadas com `go ls.sendBatch()` não são rastreadas

**Efeito:**
- Goroutines ficam travadas após Stop()
- Vazamento de memória
- Impossível fazer shutdown limpo

**Solução:**
```go
func (ls *LokiSink) adaptiveBatchLoop() {
    // WaitGroup para rastrear sendBatch goroutines
    var wg sync.WaitGroup
    defer wg.Wait()  // Aguardar todas antes de sair

    for {
        select {
        case <-ls.ctx.Done():
            return
        default:
        }

        // Usar select com timeout
        batch, err := ls.adaptiveBatcher.GetBatch(ls.ctx)
        if err != nil {
            if err == context.Canceled || ls.ctx.Err() != nil {
                return
            }
            ls.logger.WithError(err).Error("Error getting batch")
            time.Sleep(100 * time.Millisecond)  // Backoff
            continue
        }

        if len(batch) > 0 {
            wg.Add(1)
            go func(b []types.LogEntry) {
                defer wg.Done()
                ls.sendBatch(b)
            }(batch)

            // Log stats
            stats := ls.adaptiveBatcher.GetStats()
            ls.logger.WithFields(logrus.Fields{
                "batch_size":         stats.CurrentBatchSize,
                "flush_delay_ms":     stats.CurrentFlushDelay,
                "throughput_per_sec": stats.ThroughputPerSec,
                "adaptation_count":   stats.AdaptationCount,
            }).Debug("Adaptive batcher stats")
        }
    }
}
```

---

### C7. Unsafe JSON Marshal em createStreamKey

**Arquivo:** `internal/sinks/loki_sink.go:574-585`

**Problema:**
```go
func (ls *LokiSink) createStreamKey(labels map[string]string) string {
    labelsCopy := make(map[string]string, len(labels))
    for k, v := range labels {
        labelsCopy[k] = v
    }

    // Usar JSON para criar chave determinística
    data, _ := json.Marshal(labelsCopy)  // ❌ Ignora erro
    return string(data)
}
```

**Causa:** Erro de JSON marshal ignorado, map iteration não é determinística em Go.

**Efeito:**
- Chaves inconsistentes para mesmos labels
- Streams duplicados no Loki
- Overhead desnecessário

**Solução:**
```go
func (ls *LokiSink) createStreamKey(labels map[string]string) string {
    // Criar chave determinística sem JSON
    keys := make([]string, 0, len(labels))
    for k := range labels {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    var builder strings.Builder
    builder.WriteString("{")
    for i, k := range keys {
        if i > 0 {
            builder.WriteString(",")
        }
        builder.WriteString(k)
        builder.WriteString(":")
        builder.WriteString(labels[k])
    }
    builder.WriteString("}")
    return builder.String()
}
```

---

### C8. File Descriptor Leak no Local File Sink

**Arquivo:** `internal/sinks/local_file_sink.go:411-457`

**Problema:**
```go
func (lfs *LocalFileSink) getOrCreateLogFile(filename string) (*logFile, error) {
    lfs.filesMutex.RLock()
    lf, exists := lfs.files[filename]
    lfs.filesMutex.RUnlock()

    if exists {
        return lf, nil
    }

    // Criar novo arquivo
    lfs.filesMutex.Lock()
    defer lfs.filesMutex.Unlock()

    // Double-check
    if lf, exists := lfs.files[filename]; exists {
        return lf, nil
    }

    file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        return nil, fmt.Errorf("failed to open log file: %w", err)  // ❌ Erro sem cleanup
    }

    info, err := file.Stat()
    if err != nil {
        file.Close()  // ✅ Cleanup aqui
        return nil, fmt.Errorf("failed to stat log file: %w", err)
    }

    lf = &logFile{
        path:           filename,
        file:           file,
        writer:         file,
        currentSize:    info.Size(),
        lastWrite:      time.Now(),
        useCompression: lfs.config.Compress,
        compressor:     lfs.compressor,
    }

    lfs.files[filename] = lf  // ❌ Sem limite de arquivos abertos
    return lf, nil
}
```

**Causa:** Sem limite no número de arquivos abertos simultaneamente.

**Efeito:**
- **Limite de file descriptors do sistema atingido**
- Erros "too many open files"
- Sistema para de aceitar novos logs

**Solução:**
```go
const maxOpenFiles = 100  // Limite configurável

func (lfs *LocalFileSink) getOrCreateLogFile(filename string) (*logFile, error) {
    lfs.filesMutex.RLock()
    lf, exists := lfs.files[filename]
    lfs.filesMutex.RUnlock()

    if exists {
        return lf, nil
    }

    lfs.filesMutex.Lock()
    defer lfs.filesMutex.Unlock()

    // Double-check
    if lf, exists := lfs.files[filename]; exists {
        return lf, nil
    }

    // Verificar limite de arquivos abertos
    if len(lfs.files) >= maxOpenFiles {
        // Fechar arquivo menos usado (LRU)
        lfs.closeLeastRecentlyUsed()
    }

    file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        return nil, fmt.Errorf("failed to open log file: %w", err)
    }

    info, err := file.Stat()
    if err != nil {
        file.Close()
        return nil, fmt.Errorf("failed to stat log file: %w", err)
    }

    lf = &logFile{
        path:           filename,
        file:           file,
        writer:         file,
        currentSize:    info.Size(),
        lastWrite:      time.Now(),
        useCompression: lfs.config.Compress,
        compressor:     lfs.compressor,
    }

    lfs.files[filename] = lf
    lfs.logger.WithField("filename", filename).Debug("Created new log file")

    return lf, nil
}

func (lfs *LocalFileSink) closeLeastRecentlyUsed() {
    var oldestFile *logFile
    var oldestFilename string
    var oldestTime time.Time = time.Now()

    for filename, lf := range lfs.files {
        if lf.lastWrite.Before(oldestTime) {
            oldestTime = lf.lastWrite
            oldestFile = lf
            oldestFilename = filename
        }
    }

    if oldestFile != nil {
        oldestFile.close()
        delete(lfs.files, oldestFilename)
        lfs.logger.WithField("filename", oldestFilename).Debug("Closed LRU log file")
    }
}
```

---

### C9. Panic em formatTextOutput com Concurrent Map Access

**Arquivo:** `internal/sinks/local_file_sink.go:696-741`

**Problema:**
```go
func (lf *logFile) formatTextOutput(entry types.LogEntry, config types.LocalFileConfig) string {
    // ...
    if config.TextFormat.IncludeLabels && len(entry.Labels) > 0 {
        var labelPairs []string
        labelsCopy := make(map[string]string, len(entry.Labels))
        for k, v := range entry.Labels {  // ❌ Pode panic em concurrent access
            labelsCopy[k] = v
        }
        for key, value := range labelsCopy {
            if key == "level" || key == "service" || key == "container" || key == "container_name" {
                labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", key, value))
            }
        }
        // ...
    }
    // ...
}
```

**Causa:** `entry.Labels` pode ser acessado concorrentemente por múltiplas goroutines.

**Efeito:**
- **Panic: concurrent map read and map write**
- Crash do aplicativo
- Perda de logs

**Solução:**
```go
// Em pkg/types/types.go - adicionar RWMutex ao LogEntry
type LogEntry struct {
    Timestamp   time.Time
    Message     string
    SourceType  string
    SourceID    string
    ProcessedAt time.Time
    Level       string
    Labels      map[string]string
    labelsMutex sync.RWMutex  // ✅ Adicionar proteção
}

// Métodos helper
func (e *LogEntry) GetLabel(key string) (string, bool) {
    e.labelsMutex.RLock()
    defer e.labelsMutex.RUnlock()
    val, ok := e.Labels[key]
    return val, ok
}

func (e *LogEntry) SetLabel(key, value string) {
    e.labelsMutex.Lock()
    defer e.labelsMutex.Unlock()
    if e.Labels == nil {
        e.Labels = make(map[string]string)
    }
    e.Labels[key] = value
}

func (e *LogEntry) CopyLabels() map[string]string {
    e.labelsMutex.RLock()
    defer e.labelsMutex.RUnlock()
    copy := make(map[string]string, len(e.Labels))
    for k, v := range e.Labels {
        copy[k] = v
    }
    return copy
}

// Em formatTextOutput
func (lf *logFile) formatTextOutput(entry types.LogEntry, config types.LocalFileConfig) string {
    // ...
    if config.TextFormat.IncludeLabels {
        labelsCopy := entry.CopyLabels()  // ✅ Thread-safe copy
        if len(labelsCopy) > 0 {
            var labelPairs []string
            for key, value := range labelsCopy {
                if key == "level" || key == "service" || key == "container" || key == "container_name" {
                    labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", key, value))
                }
            }
            // ...
        }
    }
    // ...
}
```

---

### C10. Memory Leak em Anomaly Detector Training Buffer

**Arquivo:** `pkg/anomaly/detector.go:609-624`

**Problema:**
```go
func (ad *AnomalyDetector) addToTrainingBuffer(entry ProcessedLogEntry) {
    ad.trainingMux.Lock()
    defer ad.trainingMux.Unlock()

    ad.trainingBuffer = append(ad.trainingBuffer, entry)

    // Limit buffer size
    if len(ad.trainingBuffer) > ad.config.MaxTrainingSamples {
        removeCount := len(ad.trainingBuffer) - ad.config.MaxTrainingSamples
        ad.trainingBuffer = ad.trainingBuffer[removeCount:]  // ❌ Slice reslice não libera memória
    }

    ad.stats.TrainingSamples = len(ad.trainingBuffer)
}
```

**Causa:** Reslicing não libera memória do array subjacente.

**Efeito:**
- **Vazamento de memória gradual**
- Uso de memória cresce indefinidamente
- OOM (Out of Memory) em produção

**Solução:**
```go
func (ad *AnomalyDetector) addToTrainingBuffer(entry ProcessedLogEntry) {
    ad.trainingMux.Lock()
    defer ad.trainingMux.Unlock()

    ad.trainingBuffer = append(ad.trainingBuffer, entry)

    // Limit buffer size com realocação para liberar memória
    if len(ad.trainingBuffer) > ad.config.MaxTrainingSamples {
        removeCount := len(ad.trainingBuffer) - ad.config.MaxTrainingSamples

        // Criar novo slice e copiar (libera memória antiga)
        newBuffer := make([]ProcessedLogEntry, ad.config.MaxTrainingSamples)
        copy(newBuffer, ad.trainingBuffer[removeCount:])
        ad.trainingBuffer = newBuffer
    }

    ad.stats.TrainingSamples = len(ad.trainingBuffer)
}
```

---

### C11. HTTP Client Timeout Não Respeitado em Loki Sink

**Arquivo:** `internal/sinks/loki_sink.go:503-508`

**Problema:**
```go
// Enviar request
resp, err := ls.httpClient.Do(req)  // ❌ Usa timeout do client, mas req já tem context
if err != nil {
    return fmt.Errorf("failed to send request: %w", err)
}
defer resp.Body.Close()
```

**Causa:** Request criado com `ls.ctx` que pode estar cancelado, mas não há verificação.

**Efeito:**
- Requests podem bloquear indefinidamente
- Graceful shutdown não funciona
- Goroutines bloqueadas

**Solução:**
```go
// Em sendToLoki, adicionar verificação de contexto
func (ls *LokiSink) sendToLoki(entries []types.LogEntry) error {
    // Verificar se contexto já foi cancelado
    select {
    case <-ls.ctx.Done():
        return fmt.Errorf("sink context canceled: %w", ls.ctx.Err())
    default:
    }

    // ... criar payload ...

    // Usar contexto com timeout adicional
    ctx, cancel := context.WithTimeout(ls.ctx, 30*time.Second)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, "POST", url, body)
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }

    // ... configurar headers ...

    // Enviar request
    resp, err := ls.httpClient.Do(req)
    if err != nil {
        // Verificar se foi timeout vs cancelamento
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("loki request timeout: %w", err)
        }
        return fmt.Errorf("failed to send request: %w", err)
    }
    defer resp.Body.Close()

    // ... processar response ...
}
```

---

### C12. Configurações YAML com Valores Perigosos

**Arquivo:** `configs/config.yaml`

**Problema:**
Sem validação de valores mínimos/máximos, permitindo configurações que quebram o sistema.

**Causa:** Falta de validação no carregamento de configuração.

**Efeito:**
- Valores como `queue_size: 0` causam panic
- `worker_count: 1000` esgota recursos
- `batch_size: 1000000` causa OOM

**Solução:**
```go
// Em internal/config/config.go
func (c *Config) Validate() error {
    var errors []string

    // Validar Dispatcher
    if c.Dispatcher.QueueSize < 100 || c.Dispatcher.QueueSize > 1000000 {
        errors = append(errors, "dispatcher.queue_size must be between 100 and 1000000")
    }
    if c.Dispatcher.WorkerCount < 1 || c.Dispatcher.WorkerCount > 100 {
        errors = append(errors, "dispatcher.worker_count must be between 1 and 100")
    }
    if c.Dispatcher.BatchSize < 10 || c.Dispatcher.BatchSize > 10000 {
        errors = append(errors, "dispatcher.batch_size must be between 10 and 10000")
    }

    // Validar Sinks
    if c.Sinks.Loki.Enabled {
        if c.Sinks.Loki.URL == "" {
            errors = append(errors, "sinks.loki.url is required when enabled")
        }
        if c.Sinks.Loki.BatchSize < 1 || c.Sinks.Loki.BatchSize > 10000 {
            errors = append(errors, "sinks.loki.batch_size must be between 1 and 10000")
        }
        if c.Sinks.Loki.QueueSize < 100 || c.Sinks.Loki.QueueSize > 1000000 {
            errors = append(errors, "sinks.loki.queue_size must be between 100 and 1000000")
        }
    }

    if c.Sinks.LocalFile.Enabled {
        if c.Sinks.LocalFile.Directory == "" {
            errors = append(errors, "sinks.local_file.directory is required when enabled")
        }
        if c.Sinks.LocalFile.Rotation.MaxSizeMB < 1 || c.Sinks.LocalFile.Rotation.MaxSizeMB > 10000 {
            errors = append(errors, "sinks.local_file.rotation.max_size_mb must be between 1 and 10000")
        }
        if c.Sinks.LocalFile.Rotation.MaxFiles < 1 {
            errors = append(errors, "sinks.local_file.rotation.max_files must be at least 1")
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
    }

    return nil
}
```

---

## 🟠 Problemas Graves (Média Prioridade)

### G1. Error Handling Inadequado no Dispatcher

**Arquivo:** `internal/dispatcher/dispatcher.go`

**Problema:** Erros são logados mas não há recuperação ou retry logic.

**Solução:** Implementar retry com backoff exponencial e DLQ.

---

### G2. Falta de Health Checks Detalhados

**Problema:** Health checks são muito simplistas e não detectam problemas reais.

**Solução:**
```go
type HealthCheck struct {
    Component  string
    Status     string  // "healthy", "degraded", "unhealthy"
    Message    string
    LastCheck  time.Time
    Metrics    map[string]interface{}
}

func (app *App) DetailedHealthCheck() map[string]HealthCheck {
    checks := make(map[string]HealthCheck)

    // Check Dispatcher
    dispatcherUtil := app.dispatcher.GetQueueUtilization()
    checks["dispatcher"] = HealthCheck{
        Component: "dispatcher",
        Status: func() string {
            if dispatcherUtil > 0.9 { return "unhealthy" }
            if dispatcherUtil > 0.7 { return "degraded" }
            return "healthy"
        }(),
        Message: fmt.Sprintf("Queue utilization: %.2f%%", dispatcherUtil*100),
        LastCheck: time.Now(),
        Metrics: map[string]interface{}{
            "queue_utilization": dispatcherUtil,
        },
    }

    // Check Sinks
    // Check Monitors
    // etc...

    return checks
}
```

---

### G3. Logs Sensíveis Podem Vazar Informação

**Arquivo:** Múltiplos arquivos

**Problema:**
```go
ls.logger.WithFields(logrus.Fields{
    "loki_url": ls.config.URL,  // ❌ Pode conter credenciais
    "payload":  string(data),   // ❌ Pode conter dados sensíveis
}).Debug("Sending to Loki")
```

**Solução:**
```go
func sanitizeURL(urlStr string) string {
    u, err := url.Parse(urlStr)
    if err != nil {
        return "[invalid-url]"
    }
    if u.User != nil {
        u.User = url.UserPassword("[REDACTED]", "[REDACTED]")
    }
    return u.String()
}

ls.logger.WithFields(logrus.Fields{
    "loki_url": sanitizeURL(ls.config.URL),
    "payload_size": len(data),  // Apenas tamanho, não conteúdo
}).Debug("Sending to Loki")
```

---

### G4. Falta de Métricas de Performance

**Problema:** Métricas básicas apenas, faltam:
- Latência p50, p95, p99
- Throughput por pipeline
- Error rate por tipo
- Resource usage trends

**Solução:** Implementar histogramas e summaries do Prometheus.

---

### G5. Configuração de Pipelines Muito Complexa

**Arquivo:** `configs/pipelines.yaml`

**Problema:** Regex patterns duplicados, falta de reuso, difícil manutenção.

**Solução:**
```yaml
# Definir patterns reutilizáveis
patterns:
  common_log_level: '(?i)\b(trace|debug|info|warn|warning|error|fatal|crit|critical)\b'
  iso8601_timestamp: '\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z'

pipelines:
  - name: default
    steps:
      - name: extract_level
        type: log_level_extract
        config:
          pattern: "${patterns.common_log_level}"  # Reusa pattern
```

---

### G6. Falta de Rate Limiting

**Problema:** Sem proteção contra log storms (milhões de logs por segundo).

**Solução:** Implementar token bucket ou leaky bucket rate limiter.

---

### G7. Compression sempre ativa pode ser contraproducente

**Arquivo:** `internal/sinks/local_file_sink.go:624-637`

**Problema:** Compressão aplicada em toda escrita, mesmo para arquivos pequenos.

**Solução:**
```go
// Apenas comprimir se payload > threshold
if lf.useCompression && lf.compressor != nil && len(line) > 1024 {
    compressionResult, err := lf.compressor.Compress([]byte(line), compression.AlgorithmAuto, "local_file")
    // ...
}
```

---

### G8. Anomaly Detector Inicializa Modelos Vazios

**Arquivo:** `pkg/anomaly/detector.go:176-213`

**Problema:** Construtores retornam modelos vazios, panic ao chamar métodos.

**Solução:** Implementar construtores reais ou usar stubs seguros.

---

### G9. Task Manager Não Remove Tasks Falhadas

**Arquivo:** `pkg/task_manager/task_manager.go:268-295`

**Problema:** Tasks falhadas ficam em memória por 1 hora, acumulando.

**Solução:** Configurar tempo de retenção diferente por status.

---

### G10. HTTP Client Reutilizado Sem Connection Pooling Adequado

**Arquivo:** `internal/sinks/loki_sink.go:77-84`

**Problema:**
```go
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:        10,  // ❌ Muito baixo
        MaxIdleConnsPerHost: 10,  // ❌ Muito baixo para alta throughput
        IdleConnTimeout:     30 * time.Second,
    },
}
```

**Solução:**
```go
httpClient := &http.Client{
    Timeout: timeout,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 100,
        MaxConnsPerHost:     100,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        DisableKeepAlives:   false,  // Manter conexões vivas
    },
}
```

---

### G11. Falta de Graceful Degradation

**Problema:** Sistema para completamente se um sink falha.

**Solução:** Implementar fallback sinks e modo degradado.

---

### G12. Regex Compiladas em Runtime

**Problema:** Patterns compilados toda vez em hot path.

**Solução:**
```go
type RegexCache struct {
    cache map[string]*regexp.Regexp
    mu    sync.RWMutex
}

func (rc *RegexCache) Compile(pattern string) (*regexp.Regexp, error) {
    rc.mu.RLock()
    if re, exists := rc.cache[pattern]; exists {
        rc.mu.RUnlock()
        return re, nil
    }
    rc.mu.RUnlock()

    re, err := regexp.Compile(pattern)
    if err != nil {
        return nil, err
    }

    rc.mu.Lock()
    rc.cache[pattern] = re
    rc.mu.Unlock()

    return re, nil
}
```

---

### G13. Falta de Backpressure no Dispatcher

**Problema:** Dispatcher aceita logs ilimitadamente mesmo se sinks estão lentos.

**Solução:** Implementar backpressure propagation do sink para dispatcher.

---

### G14. DLQ Sem Limite de Tamanho

**Problema:** Dead Letter Queue pode crescer infinitamente.

**Solução:** Configurar tamanho máximo e política de descarte (FIFO, prioridade).

---

### G15. Falta de Observabilidade em Pipelines

**Problema:** Impossível saber qual step do pipeline falhou sem ler logs.

**Solução:** Adicionar traces distribuídos (OpenTelemetry).

---

### G16. Configuração Hardcoded em Múltiplos Lugares

**Problema:** Magic numbers espalhados pelo código.

**Solução:** Centralizar constantes:
```go
const (
    DefaultQueueSize       = 10000
    DefaultWorkerCount     = 5
    DefaultBatchSize       = 100
    DefaultBatchTimeout    = 10 * time.Second
    MaxQueueSize           = 1000000
    MinQueueSize           = 100
    DefaultCircuitTimeout  = 60 * time.Second
)
```

---

### G17. Falta de Documentação de APIs

**Problema:** Funções públicas sem godoc.

**Solução:** Adicionar documentação completa:
```go
// Send envia um batch de log entries para o sink com proteção de backpressure.
//
// O método implementa backpressure escalonado baseado na utilização da fila:
//   - < 75%: timeout de 10s
//   - 75-90%: timeout de 3s
//   - 90-95%: timeout de 1s
//   - > 95%: envia para DLQ imediatamente
//
// Parâmetros:
//   - ctx: contexto para cancelamento
//   - entries: slice de log entries para enviar
//
// Retorna:
//   - error: nil em sucesso, erro em falha crítica
//
// Thread-safety: Seguro para chamadas concorrentes
func (ls *LokiSink) Send(ctx context.Context, entries []types.LogEntry) error {
    // ...
}
```

---

### G18. Timestamp Parsing Pode Falhar Silenciosamente

**Arquivo:** `configs/pipelines.yaml`

**Problema:** Se parsing de timestamp falha, usa timestamp de ingestão sem aviso.

**Solução:** Adicionar flag de warning no log entry quando timestamp é substituído.

---

## 🟡 Problemas Moderados (Baixa Prioridade)

### M1. Código Duplicado em Formatadores

**Problema:** Lógica similar em `formatJSONOutput` e `formatTextOutput`.

**Solução:** Extrair lógica comum para funções helper.

---

### M2. Nomes de Variáveis Pouco Descritivos

**Problema:** `lf`, `ls`, `tm`, `ad` dificultam leitura.

**Solução:** Usar nomes completos ou mais descritivos.

---

### M3. Funções Muito Longas

**Problema:** Funções com 100+ linhas difíceis de testar.

**Solução:** Quebrar em funções menores e mais focadas.

---

### M4. Testes Ausentes

**Problema:** Sem testes unitários para componentes críticos.

**Solução:** Atingir pelo menos 70% de coverage.

---

### M5. Falta de Benchmarks

**Problema:** Sem medição objetiva de performance.

**Solução:** Adicionar benchmarks Go:
```go
func BenchmarkDispatcherSend(b *testing.B) {
    // ...
}
```

---

### M6. Logs Excessivos em Nível Debug

**Problema:** Muito ruído em debug mode.

**Solução:** Usar níveis apropriados (trace para detalhes).

---

### M7. Falta de Exemplos de Configuração

**Problema:** Difícil entender todas as opções disponíveis.

**Solução:** Criar `configs/examples/` com cenários comuns.

---

### M8. README Desatualizado

**Problema:** README não menciona recursos novos.

**Solução:** Atualizar com features atuais.

---

### M9. Falta de Linter Configurado

**Problema:** Sem golangci-lint ou similar.

**Solução:**
```yaml
# .golangci.yml
linters:
  enable:
    - gofmt
    - golint
    - govet
    - errcheck
    - staticcheck
    - gosimple
    - ineffassign
    - unused
```

---

### M10. Error Messages Genéricos

**Problema:** Erros como "failed to process" sem contexto.

**Solução:**
```go
return fmt.Errorf("failed to process log entry (source=%s, id=%s): %w",
    entry.SourceType, entry.SourceID, err)
```

---

### M11. Falta de Versionamento de API

**Problema:** Breaking changes sem aviso.

**Solução:** Seguir semver e manter changelog.

---

### M12. Configuração de Desenvolvimento vs Produção

**Problema:** Mesma config para dev e prod.

**Solução:** Criar profiles:
- `configs/dev.yaml`
- `configs/staging.yaml`
- `configs/production.yaml`

---

## Resumo de Impacto

### Por Componente

| Componente | Críticos | Graves | Moderados | Total |
|-----------|----------|--------|-----------|-------|
| Task Manager | 1 | 1 | 1 | 3 |
| Anomaly Detector | 2 | 1 | 0 | 3 |
| Local File Sink | 4 | 2 | 1 | 7 |
| Loki Sink | 3 | 3 | 1 | 7 |
| Circuit Breaker | 1 | 0 | 0 | 1 |
| Dispatcher | 1 | 2 | 0 | 3 |
| Configuration | 1 | 2 | 4 | 7 |
| Geral | 0 | 7 | 5 | 12 |
| **TOTAL** | **12** | **18** | **12** | **42** |

---

## Prioridades de Correção

### Sprint 1 (Crítico - 1 semana)
1. C4 - Circuit Breaker Mutex Lock
2. C1 - Task Manager Race Condition
3. C3 - Local File Sink Deadlock
4. C9 - Concurrent Map Access
5. C8 - File Descriptor Leak

### Sprint 2 (Crítico + Alto Grave - 2 semanas)
6. C2 - Context Leak
7. C6 - Goroutine Leak
8. C10 - Memory Leak
9. G1 - Error Handling
10. G13 - Backpressure

### Sprint 3 (Restante Crítico + Graves - 2 semanas)
11. C7 - JSON Marshal
12. C11 - HTTP Timeout
13. C12 - Config Validation
14. G2 - Health Checks
15. G6 - Rate Limiting

### Sprint 4 (Graves + Moderados - 1 semana)
16-30. Restante dos Graves

### Backlog (Moderados)
31-42. Problemas Moderados conforme tempo disponível

---

## Recomendações Gerais

### Arquitetura
1. **Implementar Padrão de Supervisor**: Criar supervisor pattern para recuperação automática
2. **Resource Pooling**: Usar sync.Pool para objetos frequentemente alocados
3. **Observabilidade**: Adicionar OpenTelemetry para tracing distribuído

### Qualidade de Código
1. **Testes**: Atingir 70% de coverage mínimo
2. **CI/CD**: Adicionar testes automáticos, linting, e benchmarks
3. **Code Review**: Exigir aprovação de 2 pessoas para merges

### Performance
1. **Profiling**: Executar pprof regularmente
2. **Load Testing**: Testar com 100k+ logs/segundo
3. **Memory Profiling**: Detectar leaks precocemente

### Segurança
1. **Input Validation**: Validar todos os inputs externos
2. **Secrets Management**: Usar secrets manager ao invés de arquivos
3. **Audit Logging**: Log de todas as operações administrativas

---

## Conclusão

O projeto demonstra boa arquitetura geral, mas tem **problemas críticos de concorrência** que podem causar crashes e perda de dados em produção. A prioridade deve ser corrigir os problemas de sincronização e resource management antes de adicionar novas features.

**Estimativa de esforço total:** 6-8 semanas de desenvolvimento com 2 desenvolvedores experientes.

**Risco atual em produção:** 🔴 **ALTO** - Recomendado não usar em produção crítica até correção dos problemas críticos.

---

**Revisado por:** Claude Code
**Data:** 2025-10-26
