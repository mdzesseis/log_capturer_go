# Análise Completa do Sistema de Posições (Position Tracking System)

**Data**: 2025-11-07
**Versão**: 1.0
**Status**: Análise Completa
**Analista**: Workflow Coordinator (22-agent team)

---

## 📋 Sumário Executivo

Este documento apresenta uma análise detalhada do sistema de position tracking do log_capturer_go, identificando sua arquitetura atual, problemas existentes, gaps de teste e recomendações de melhorias.

### Principais Achados

✅ **Pontos Fortes:**
- Sistema modular bem organizado (`pkg/positions/`)
- Thread-safety implementado com RWMutex
- Suporte a inode/device tracking para detecção de rotação
- Persistência atômica (write-to-temp + rename)
- Buffer manager com flush periódico e cleanup automático
- Detecção automática de truncamento de arquivo

⚠️ **Problemas Críticos Identificados:**
1. **Potencial perda de dados em crash** - Flush interval de 30s permite perda de até 30s de posições
2. **Falta de validação de offset** - Offset inválido após truncate pode causar bloqueio
3. **Race condition teórica** - UpdatePosition seguido de crash antes do flush
4. **Falta de métricas** - Não há métricas para file rotation, offset reset, ou position save failures
5. **Test coverage de position system < 50%** - Faltam testes de edge cases críticos

---

## 📚 Índice

1. [Arquitetura Atual](#1-arquitetura-atual)
2. [Estruturas de Dados](#2-estruturas-de-dados)
3. [Fluxo de Position Tracking](#3-fluxo-de-position-tracking)
4. [Thread Safety Analysis](#4-thread-safety-analysis)
5. [Problemas Identificados](#5-problemas-identificados)
6. [Edge Cases e Failure Scenarios](#6-edge-cases-e-failure-scenarios)
7. [Test Coverage Analysis](#7-test-coverage-analysis)
8. [Métricas e Observabilidade](#8-métricas-e-observabilidade)
9. [Implementações Recomendadas](#9-implementações-recomendadas)
10. [Plano de Ação](#10-plano-de-ação)

---

## 1. Arquitetura Atual

### 1.1 Visão Geral do Sistema

```
┌─────────────────────────────────────────────────────────────────┐
│                    Position System Architecture                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │          PositionBufferManager (Orquestrador)            │  │
│  │  - FlushInterval: 30s (configurável)                      │  │
│  │  - CleanupInterval: 5min                                  │  │
│  │  - MaxPositionAge: 24h                                    │  │
│  │  - ForceFlushOnExit: true                                 │  │
│  └────────────┬──────────────────────────┬──────────────────┘  │
│               │                          │                      │
│               ▼                          ▼                      │
│  ┌────────────────────────┐   ┌────────────────────────┐      │
│  │ FilePositionManager    │   │ContainerPositionManager│      │
│  │                        │   │                        │      │
│  │ - positions: map       │   │ - positions: map       │      │
│  │ - mu: RWMutex          │   │ - mu: RWMutex          │      │
│  │ - dirty: bool          │   │ - dirty: bool          │      │
│  │ - lastFlush: time      │   │ - lastFlush: time      │      │
│  │                        │   │                        │      │
│  │ Tracks:                │   │ Tracks:                │      │
│  │ - Offset               │   │ - Since time           │      │
│  │ - Size                 │   │ - Log count            │      │
│  │ - Inode                │   │ - Bytes read           │      │
│  │ - Device               │   │ - Restart count        │      │
│  │ - LastModified         │   │ - Status               │      │
│  │ - LastRead             │   │ - LastLogTime          │      │
│  └────────────┬───────────┘   └────────────┬───────────┘      │
│               │                            │                   │
│               ▼                            ▼                   │
│  ┌───────────────────────────────────────────────────────┐    │
│  │         Persistent Storage (JSON Files)               │    │
│  │  /app/data/positions/file_positions.json              │    │
│  │  /app/data/positions/container_positions.json         │    │
│  │                                                         │    │
│  │  Format: Atomic write (temp + rename)                  │    │
│  │  Backup: Not implemented (TODO)                        │    │
│  └───────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 Arquivos e Responsabilidades

| Arquivo | Responsabilidade | LOC |
|---------|-----------------|-----|
| `pkg/positions/buffer_manager.go` | Orquestração, flush periódico, cleanup | 381 |
| `pkg/positions/file_positions.go` | Tracking de posições de arquivos | 330 |
| `pkg/positions/container_positions.go` | Tracking de posições de containers | 340 |
| `internal/monitors/file_monitor.go` | Consumer do position manager (files) | 1640 |
| `internal/monitors/container_monitor.go` | Consumer do position manager (containers) | ~2000 |

**Total LOC relacionado a positions**: ~4691 linhas

---

## 2. Estruturas de Dados

### 2.1 FilePosition

```go
type FilePosition struct {
    FilePath     string    `json:"file_path"`     // Caminho completo do arquivo
    Offset       int64     `json:"offset"`        // Byte offset onde parou a leitura
    Size         int64     `json:"size"`          // Tamanho do arquivo no último read
    LastModified time.Time `json:"last_modified"` // ModTime do arquivo
    LastRead     time.Time `json:"last_read"`     // Timestamp da última leitura
    Inode        uint64    `json:"inode"`         // Inode do arquivo (Unix)
    Device       uint64    `json:"device"`        // Device ID (Unix)
    LogCount     int64     `json:"log_count"`     // Contador de logs lidos
    BytesRead    int64     `json:"bytes_read"`    // Total de bytes lidos
    Status       string    `json:"status"`        // "active", "removed", "deleted"
}
```

**Campos Críticos:**
- `Offset`: Posição exata onde retomar leitura
- `Inode/Device`: Detecção de file rotation
- `Size`: Detecção de truncamento
- `LastModified`: Detecção de modificação

### 2.2 ContainerPosition

```go
type ContainerPosition struct {
    ContainerID   string    `json:"container_id"`   // Docker container ID
    Since         time.Time `json:"since"`          // Timestamp desde quando ler
    LastRead      time.Time `json:"last_read"`      // Última leitura bem-sucedida
    LastLogTime   time.Time `json:"last_log_time"`  // Timestamp do último log
    LogCount      int64     `json:"log_count"`      // Contador de logs
    BytesRead     int64     `json:"bytes_read"`     // Total de bytes
    Status        string    `json:"status"`         // "active", "stopped", "removed", "restarted"
    RestartCount  int       `json:"restart_count"`  // Contador de restarts
}
```

### 2.3 BufferConfig

```go
type BufferConfig struct {
    FlushInterval       time.Duration // Default: 30s
    MaxMemoryBuffer     int          // Default: 1000 (não usado)
    MaxMemoryPositions  int          // Default: 5000 - Limite de posições
    ForceFlushOnExit    bool         // Default: true
    CleanupInterval     time.Duration // Default: 5min
    MaxPositionAge      time.Duration // Default: 24h
}
```

---

## 3. Fluxo de Position Tracking

### 3.1 Lifecycle Completo

```
┌─────────────────────────────────────────────────────────────────┐
│                     Position Tracking Lifecycle                  │
└─────────────────────────────────────────────────────────────────┘

[1. INITIALIZATION]
    │
    ├─> PositionBufferManager.Start()
    │   ├─> LoadPositions() from disk
    │   │   ├─> FilePositionManager.LoadPositions()
    │   │   │   └─> Read /app/data/positions/file_positions.json
    │   │   └─> ContainerPositionManager.LoadPositions()
    │   │       └─> Read /app/data/positions/container_positions.json
    │   │
    │   ├─> Start periodic flush goroutine (every 30s)
    │   └─> Start periodic cleanup goroutine (every 5min)
    │
    └─> FileMonitor / ContainerMonitor starts reading

[2. NORMAL OPERATION - File Reading]
    │
    ├─> FileMonitor.readFile(mf *monitoredFile)
    │   │
    │   ├─> Open file OR reuse existing handle
    │   │
    │   ├─> [FIRST OPEN ONLY] Apply Seek Strategy
    │   │   ├─> Check saved position from PositionManager
    │   │   │   └─> GetFileOffset(filePath) → int64
    │   │   │
    │   │   ├─> If position > 0: file.Seek(position, 0)
    │   │   │   └─> Resume from saved position ✅
    │   │   │
    │   │   └─> If position == 0: Apply SeekStrategy
    │   │       ├─> "beginning": Start from 0
    │   │       ├─> "recent": Seek to (fileSize - SeekRecentBytes)
    │   │       └─> "end": Seek to EOF (only new logs)
    │   │
    │   ├─> Read lines loop
    │   │   ├─> reader.ReadString('\n')
    │   │   ├─> Process log entry
    │   │   ├─> dispatcher.Handle(...)
    │   │   └─> Update mf.position += len(line) + 1
    │   │
    │   └─> After reading N lines:
    │       └─> positionManager.UpdateFilePosition(
    │               path, offset, size, modTime, inode, device,
    │               bytesRead, logCount
    │           )

[3. POSITION UPDATE (In-Memory)]
    │
    ├─> FilePositionManager.UpdatePosition(...)
    │   │
    │   ├─> Lock mutex (Write lock)
    │   │
    │   ├─> Check for FILE ROTATION (inode/device changed)
    │   │   ├─> If rotated: Reset offset to 0
    │   │   └─> Log: "File rotation detected"
    │   │
    │   ├─> Check for TRUNCATION (size < stored size)
    │   │   ├─> If truncated: Reset offset to 0
    │   │   └─> Log: "File truncation detected"
    │   │
    │   ├─> Update position struct
    │   │   ├─> pos.Offset = newOffset
    │   │   ├─> pos.Size = newSize
    │   │   ├─> pos.Inode = newInode
    │   │   ├─> pos.Device = newDevice
    │   │   ├─> pos.LastRead = time.Now()
    │   │   ├─> pos.LogCount += logCount
    │   │   └─> pos.BytesRead += bytesRead
    │   │
    │   ├─> Mark as dirty: fpm.dirty = true
    │   │
    │   └─> Unlock mutex
    │
    └─> [IMPORTANT] Position NOT saved to disk yet!
        └─> Waits for periodic flush (default 30s)

[4. PERIODIC FLUSH (Every 30s)]
    │
    ├─> PositionBufferManager.flushLoop()
    │   │
    │   ├─> Ticker fires every FlushInterval
    │   │
    │   └─> Flush()
    │       │
    │       ├─> Check containerManager.IsDirty()
    │       │   └─> If dirty: SavePositions()
    │       │
    │       └─> Check fileManager.IsDirty()
    │           └─> If dirty: SavePositions()
    │
    └─> FilePositionManager.SavePositions()
        │
        ├─> Lock mutex (Read lock)
        │
        ├─> json.MarshalIndent(positions)
        │
        ├─> Write to TEMP file (.tmp)
        │   └─> os.WriteFile(filename + ".tmp", data, 0644)
        │
        ├─> Atomic rename (POSIX atomic operation)
        │   └─> os.Rename(tempFile, filename)
        │
        ├─> Mark as clean: fpm.dirty = false
        │
        └─> Update fpm.lastFlush = time.Now()

[5. PERIODIC CLEANUP (Every 5min)]
    │
    └─> PositionBufferManager.cleanupLoop()
        │
        └─> FilePositionManager.CleanupOldPositions(maxAge=24h)
            │
            ├─> Iterate over all positions
            │
            ├─> If pos.LastRead < cutoff AND status IN ["removed", "deleted"]
            │   └─> delete(positions, filePath)
            │
            └─> Mark dirty if any removed

[6. GRACEFUL SHUTDOWN]
    │
    ├─> FileMonitor.Stop()
    │   └─> positionManager.Stop()
    │
    └─> PositionBufferManager.Stop()
        │
        ├─> Cancel context (stops goroutines)
        ├─> Wait for goroutines (wg.Wait())
        │
        └─> If ForceFlushOnExit == true:
            └─> Flush() → Final save to disk ✅

[7. CRASH SCENARIO]
    │
    └─> System crashes WITHOUT calling Stop()
        │
        ├─> Last flush was N seconds ago (0 <= N <= 30)
        │
        ├─> Positions updated in memory since last flush: LOST ❌
        │
        └─> On restart:
            └─> LoadPositions() loads last saved state
                └─> Missing N seconds of position updates
                    └─> Logs MAY be read again (DUPLICATION) ⚠️
```

### 3.2 File Rotation Detection Flow

```
┌─────────────────────────────────────────────────────────────┐
│              File Rotation Detection Flow                    │
└─────────────────────────────────────────────────────────────┘

[SCENARIO: logrotate moves /var/log/app.log → app.log.1]

Time T0: Normal Reading
    │
    ├─> File: /var/log/app.log (inode: 123, size: 10MB)
    ├─> Position stored: offset=9,500,000, inode=123
    └─> Reading continues...

Time T1: Logrotate Executes
    │
    ├─> OS moves /var/log/app.log → /var/log/app.log.1
    ├─> OS creates new /var/log/app.log (inode: 456, size: 0)
    │
    └─> FileMonitor still has open handle to OLD inode 123
        └─> Continues reading from app.log.1 (OK for a while)

Time T2: FileMonitor Reads Again
    │
    ├─> readFile() called (triggered by write event or poll)
    │
    ├─> Get file stats: stat(/var/log/app.log)
    │   └─> Returns: inode=456, size=512 (NEW FILE)
    │
    └─> positionManager.UpdateFilePosition(
            "/var/log/app.log",
            offset=10000,
            size=512,
            inode=456,  ← CHANGED
            device=2049
        )

Time T3: UpdatePosition Detects Rotation
    │
    ├─> FilePositionManager.UpdatePosition()
    │   │
    │   ├─> Load stored position for "/var/log/app.log"
    │   │   └─> storedPos.Inode = 123 (old)
    │   │
    │   ├─> Compare: newInode (456) != storedInode (123)
    │   │
    │   ├─> ROTATION DETECTED! ✅
    │   │   └─> logger.Info("File rotation detected",
    │   │           "old_inode": 123, "new_inode": 456)
    │   │
    │   ├─> RESET OFFSET: pos.Offset = 0
    │   │
    │   └─> Update position:
    │       ├─> pos.Offset = 0 (reset)
    │       ├─> pos.Inode = 456 (new)
    │       ├─> pos.Device = 2049
    │       └─> pos.Size = 512
    │
    └─> Next readFile() will:
        ├─> Close old file handle (inode 123)
        ├─> Open new file (inode 456)
        └─> Seek to offset 0 (start of new file) ✅

[RESULT]
    ✅ No logs lost from new file
    ✅ Old file (app.log.1) no longer monitored
    ⚠️  If rotation happens DURING processing, last few lines
        from old file might be lost (acceptable)
```

### 3.3 Truncation Detection Flow

```
┌─────────────────────────────────────────────────────────────┐
│              Truncation Detection Flow                       │
└─────────────────────────────────────────────────────────────┘

[SCENARIO: File is truncated (> /var/log/app.log)]

Time T0: Normal Reading
    │
    ├─> File: /var/log/app.log (size: 10MB)
    ├─> Position stored: offset=9,500,000, size=10,000,000
    └─> Reading continues...

Time T1: File Truncated
    │
    ├─> External process: echo "" > /var/log/app.log
    ├─> New file size: 1 byte
    │
    └─> Inode UNCHANGED (same file, just truncated)

Time T2: FileMonitor Reads Again
    │
    ├─> stat(/var/log/app.log)
    │   └─> size=1 (NEW), inode=123 (SAME)
    │
    └─> positionManager.UpdateFilePosition(
            path="/var/log/app.log",
            offset=9,500,000, ← OLD OFFSET (wrong!)
            size=1,           ← NEW SIZE
            inode=123
        )

Time T3: UpdatePosition Detects Truncation
    │
    ├─> FilePositionManager.UpdatePosition()
    │   │
    │   ├─> Load stored position
    │   │   └─> storedPos.Size = 10,000,000
    │   │
    │   ├─> Compare: newSize (1) < storedSize (10,000,000)
    │   │
    │   ├─> TRUNCATION DETECTED! ✅
    │   │   └─> logger.Info("File truncation detected",
    │   │           "old_size": 10MB, "new_size": 1)
    │   │
    │   ├─> RESET OFFSET: pos.Offset = 0
    │   │
    │   └─> Update position:
    │       ├─> pos.Offset = 0 (reset)
    │       ├─> pos.Size = 1 (new)
    │       └─> pos.Inode = 123 (unchanged)
    │
    └─> Next readFile() will:
        └─> Seek to offset 0 (start from beginning) ✅

[RESULT]
    ✅ System recovers correctly
    ⚠️  Logs written BEFORE truncation are LOST (unavoidable)
    ✅ New logs after truncation are captured correctly
```

---

## 4. Thread Safety Analysis

### 4.1 Mutex Usage

#### FilePositionManager

```go
type FilePositionManager struct {
    positions    map[string]*FilePosition
    mu           sync.RWMutex  // ✅ Protects positions map
    positionsDir string
    filename     string
    logger       *logrus.Logger
    dirty        bool           // ⚠️ Protected by mu
    lastFlush    time.Time      // ⚠️ Protected by mu
}
```

**Lock Strategy:**
- `GetPosition()`: Uses RLock (read-only) ✅
- `UpdatePosition()`: Uses Lock (write) ✅
- `SavePositions()`: Uses RLock during marshal ✅ (read-only operation)
- `CleanupOldPositions()`: Uses Lock ✅

#### PositionBufferManager

```go
type PositionBufferManager struct {
    containerManager *ContainerPositionManager
    fileManager      *FilePositionManager

    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup  // ✅ Tracks goroutines

    flushTicker   *time.Ticker
    cleanupTicker *time.Ticker
    tickerMutex   sync.RWMutex  // ✅ Protects ticker access

    stats struct {
        mu sync.RWMutex  // ✅ Protects stats fields
        // ... stat fields
    }
}
```

**Goroutine Tracking:**
- `Start()`: Adds 2 to WaitGroup (flushLoop + cleanupLoop) ✅
- `Stop()`: Calls cancel(), then wg.Wait() ✅
- All goroutines check ctx.Done() ✅

### 4.2 Race Condition Analysis

#### ✅ SAFE: Concurrent Reads

```go
// Multiple goroutines can read positions concurrently
goroutine1: pos := manager.GetPosition("/file1.log")  // RLock
goroutine2: pos := manager.GetPosition("/file2.log")  // RLock
goroutine3: pos := manager.GetPosition("/file3.log")  // RLock
```

**Result**: SAFE - RWMutex allows multiple readers ✅

#### ✅ SAFE: Update + Read

```go
goroutine1: manager.UpdatePosition("/file1.log", ...) // Lock
goroutine2: pos := manager.GetPosition("/file1.log")  // RLock (waits)
```

**Result**: SAFE - Write lock blocks readers until complete ✅

#### ⚠️ POTENTIAL RACE: Multiple Updates to Same File

```go
// File monitor goroutine 1 (reading file A)
goroutine1:
    mf.position = 1000
    UpdateFilePosition("/file.log", 1000, ...)  // Lock acquired
    // ... unlock

// File monitor goroutine 2 (same file, different reader - ERROR!)
goroutine2:
    mf.position = 1500
    UpdateFilePosition("/file.log", 1500, ...)  // Lock acquired
    // ... unlock
```

**Result**: POTENTIALLY UNSAFE if same file is read by multiple goroutines ❌

**Mitigation**: FileMonitor tracks files in map and prevents duplicate monitoring:
```go
// In AddFile():
if _, exists := fm.files[filePath]; exists {
    return fmt.Errorf("file %s is already being monitored", filePath)
}
```

**Conclusion**: SAFE in practice due to single-monitor-per-file guarantee ✅

#### ✅ SAFE: Flush While Updating

```go
goroutine1: UpdatePosition("/file1.log", ...)  // Lock (write)
goroutine2: SavePositions()                    // RLock (read during marshal)
```

**Result**: SAFE - Flush waits for updates to complete, then reads consistent state ✅

#### ⚠️ RACE CONDITION: Dirty Flag

```go
// In UpdatePosition (file_positions.go:183)
fpm.dirty = true  // ⚠️ Write under Lock

// In SavePositions (file_positions.go:104)
fpm.dirty = false  // ⚠️ Write under RLock (read lock!)
```

**Analysis**:
- `UpdatePosition()` sets `dirty = true` under write Lock ✅
- `SavePositions()` sets `dirty = false` under read RLock ⚠️

**Issue**: Setting `dirty = false` under RLock violates mutex semantics!

**Impact**:
- LOW - dirty flag is advisory only (doesn't affect correctness)
- Multiple SavePositions() calls are unlikely (single flush goroutine)

**Recommendation**: Move `dirty = false` outside RLock OR change to Lock ⚠️

### 4.3 Goroutine Lifecycle Verification

```go
// PositionBufferManager.Start()
func (pbm *PositionBufferManager) Start() error {
    // ...
    pbm.wg.Add(2)              // ✅ Register goroutines
    go pbm.flushLoop()         // ✅ Goroutine 1
    go pbm.cleanupLoop()       // ✅ Goroutine 2
    return nil
}

func (pbm *PositionBufferManager) Stop() error {
    pbm.cancel()               // ✅ Signal goroutines to stop
    pbm.wg.Wait()              // ✅ Wait for completion

    if pbm.config.ForceFlushOnExit {
        pbm.Flush()            // ✅ Final flush
    }
    return nil
}

func (pbm *PositionBufferManager) flushLoop() {
    defer pbm.wg.Done()        // ✅ Always cleanup
    for {
        select {
        case <-pbm.ctx.Done():
            return             // ✅ Respect context
        case <-pbm.flushTicker.C:
            pbm.Flush()
        }
    }
}
```

**Verdict**: Goroutine lifecycle management is CORRECT ✅

---

## 5. Problemas Identificados

### 5.1 CRÍTICO: Perda de Dados em Crash (P0)

**Descrição**: Sistema pode perder até 30 segundos de position updates se crashar entre flushes.

**Cenário**:
```
T+0s:   Flush completo (positions.json atualizado)
T+10s:  File A lido até offset 500K, posição atualizada em memória
T+20s:  File B lido até offset 800K, posição atualizada em memória
T+25s:  [CRASH] Sistema cai antes do próximo flush (T+30s)

        Resultado ao reiniciar:
        - File A: Posição recuperada = offset do último flush (T+0s)
        - File B: Posição recuperada = offset do último flush (T+0s)
        - Logs entre T+0s e T+25s: Serão lidos NOVAMENTE (DUPLICAÇÃO)
```

**Impacto**:
- **Severidade**: ALTA
- **Probabilidade**: Média (crash durante operação normal)
- **Consequência**: Logs duplicados no Loki (até 30s de logs)

**Prova de Conceito**:
```bash
# Simular crash durante operação
1. Start log_capturer_go
2. Escrever logs continuamente para arquivo monitorado
3. Após 15 segundos (entre flushes), kill -9 do processo
4. Restart log_capturer_go
5. Observar: Últimos 15s de logs são re-enviados para Loki
```

**Recomendação**: Ver seção 9.1 (Flush mais frequente ou flush after N updates)

### 5.2 ALTO: Falta de Validação de Offset (P1)

**Descrição**: System não valida se offset é menor que filesize antes de seek.

**Código Atual** (`file_monitor.go:743-754`):
```go
if mf.position > 0 {
    if _, err := file.Seek(mf.position, 0); err != nil {
        fm.logger.WithError(err).WithField("path", mf.path).Warn("Failed to seek to saved position, falling back to strategy")
        mf.position = 0
    } else {
        // Successfully seeked to saved position
        fm.logger.WithFields(logrus.Fields{
            "path":     mf.path,
            "position": mf.position,
        }).Debug("Resumed from saved position")
    }
}
```

**Problema**: Se `position > fileSize` (e.g., após truncate não detectado), seek() falha mas código continua.

**Cenário**:
```
1. File /var/log/app.log, size=10MB, saved position=9MB
2. Sistema desligado
3. Truncate externo: echo "" > /var/log/app.log (size=1 byte)
4. Sistema reinicia
5. Tenta seek(9MB) em arquivo de 1 byte → ERRO
6. Cai no fallback (mf.position = 0) → OK neste caso
7. MAS: Próximo update pode salvar position=9MB novamente
```

**Impacto**:
- **Severidade**: MÉDIA
- **Probabilidade**: Baixa (truncate externo sem restart do monitor)
- **Consequência**: Possível loop de erro, posição inválida persistida

**Recomendação**: Adicionar validação em UpdatePosition():
```go
if offset > size {
    fm.logger.Warn("Offset > filesize, resetting to 0")
    offset = 0
}
```

### 5.3 ALTO: Race Condition em dirty Flag (P1)

**Descrição**: `SavePositions()` modifica `dirty = false` sob RLock (read lock).

**Código** (`file_positions.go:85-105`):
```go
func (fpm *FilePositionManager) SavePositions() error {
    fpm.mu.RLock()              // ⚠️ READ LOCK
    defer fpm.mu.RUnlock()

    data, err := json.MarshalIndent(fpm.positions, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal positions: %w", err)
    }

    // ... write to file ...

    fpm.dirty = false           // ⚠️ WRITE under READ LOCK!
    fpm.lastFlush = time.Now()  // ⚠️ WRITE under READ LOCK!

    return nil
}
```

**Problema**: RWMutex permite múltiplos leitores. Se outro goroutine chama `SavePositions()` concorrentemente, ambos podem modificar `dirty` simultaneamente (data race).

**Prova**:
```bash
go test -race ./pkg/positions/
# Pode detectar: "WARNING: DATA RACE"
```

**Impacto**:
- **Severidade**: BAIXA-MÉDIA (flag é advisory, não afeta persistência)
- **Probabilidade**: MUITO BAIXA (apenas 1 flush goroutine)
- **Consequência**: Race detector warning, possível flush extra

**Recomendação**:
```go
// Opção 1: Usar Lock completo
func (fpm *FilePositionManager) SavePositions() error {
    fpm.mu.Lock()  // ✅ WRITE LOCK
    defer fpm.mu.Unlock()
    // ... rest ...
}

// Opção 2: Separar locks
func (fpm *FilePositionManager) SavePositions() error {
    fpm.mu.RLock()
    data := json.MarshalIndent(fpm.positions, "", "  ")
    fpm.mu.RUnlock()

    // Write to disk (no lock needed)

    fpm.mu.Lock()  // ✅ WRITE LOCK for state changes
    fpm.dirty = false
    fpm.lastFlush = time.Now()
    fpm.mu.Unlock()
}
```

### 5.4 MÉDIO: Falta de Backup de Posições (P2)

**Descrição**: Não há backup automático de `positions.json`. Se arquivo for corrompido, todas as posições são perdidas.

**Risco**:
```
1. Sistema escreve positions.json
2. Disco cheio / filesystem error durante write
3. Arquivo fica corrompido (JSON inválido)
4. Próximo LoadPositions() falha
5. Sistema recomeça do zero (todas as posições perdidas)
```

**Impacto**:
- **Severidade**: MÉDIA
- **Probabilidade**: MUITO BAIXA (atomic write com rename mitiga)
- **Consequência**: Perda completa de posições, reprocessamento de logs

**Mitigação Atual**:
- Atomic write (temp + rename) reduz risco de corrupção ✅
- Mas sem backup, não há recovery se corrupção ocorrer ⚠️

**Recomendação**: Implementar rotação de backups (ver seção 9.2)

### 5.5 MÉDIO: Falta de Métricas de Position System (P2)

**Descrição**: Não há métricas Prometheus para monitorar saúde do position system.

**Métricas Ausentes**:
```
log_capturer_position_save_total (sucesso/falha)
log_capturer_position_save_duration_seconds
log_capturer_position_load_errors_total
log_capturer_file_rotation_detected_total
log_capturer_file_truncation_detected_total
log_capturer_position_offset_reset_total
log_capturer_position_flush_queue_size
log_capturer_position_age_seconds (por arquivo)
```

**Métrica Existente** (encontrada):
```
log_capturer_file_monitor_offset_restored_total
```

**Impacto**:
- **Severidade**: MÉDIA
- **Probabilidade**: ALTA (falta de observabilidade)
- **Consequência**: Difícil detectar problemas de position tracking em produção

**Recomendação**: Ver seção 8 (Métricas e Observabilidade)

### 5.6 BAIXO: Cleanup Pode Remover Posições Ativas (P3)

**Descrição**: Cleanup remove posições com `status IN ["removed", "deleted"]` E `LastRead < 24h ago`.

**Código** (`file_positions.go:240-267`):
```go
func (fpm *FilePositionManager) CleanupOldPositions(maxAge time.Duration) {
    cutoff := time.Now().Add(-maxAge)

    for filePath, pos := range fpm.positions {
        if pos.LastRead.Before(cutoff) && (pos.Status == "removed" || pos.Status == "deleted") {
            delete(fpm.positions, filePath)
        }
    }
}
```

**Problema**: Se `FileMonitor` não atualiza `LastRead` corretamente, arquivo ativo pode ser removido.

**Cenário**:
```
1. File /var/log/quiet.log é adicionado ao monitoring
2. Arquivo fica sem receber logs por > 24h (baixa atividade)
3. Cleanup detecta: LastRead > 24h ago
4. MAS: status = "active" (não "removed")
5. Resultado: NÃO é removido (código atual está CORRETO ✅)
```

**Conclusão**: Código atual é seguro, mas depende de status correto ✅

**Recomendação**: Adicionar teste para verificar que arquivos ativos nunca são limpos

---

## 6. Edge Cases e Failure Scenarios

### 6.1 Edge Case: Arquivo Deletado e Recriado com Mesmo Nome

**Cenário**:
```bash
# Estado inicial
/var/log/app.log (inode: 123, size: 10MB, position: 9MB)

# Alguém deleta e recria
rm /var/log/app.log
touch /var/log/app.log

# Novo arquivo
/var/log/app.log (inode: 456, size: 0, position: ???)
```

**Comportamento Esperado**: Reset position para 0 (novo arquivo)

**Comportamento Atual**:
1. FileMonitor tem handle aberto para inode 123 (arquivo deletado)
2. Próximo `readFile()`: stat() retorna inode 456 (novo arquivo)
3. `UpdatePosition()` detecta: `456 != 123` → File Rotation ✅
4. Reseta offset para 0 ✅
5. Abre novo arquivo (inode 456) ✅

**Resultado**: CORRETO ✅

### 6.2 Edge Case: Symlink para Arquivo Rotacionado

**Cenário**:
```bash
# Configuração
/var/log/current.log → /var/log/app-2024-11-07.log (inode: 123)

# Logrotate executa
/var/log/current.log → /var/log/app-2024-11-08.log (inode: 456)
```

**Comportamento Esperado**: Detectar que symlink aponta para novo arquivo

**Comportamento Atual**:
1. FileMonitor monitora `/var/log/current.log` (path do symlink)
2. Stat segue symlink: retorna inode do target (123)
3. Após rotation: stat retorna inode 456
4. `UpdatePosition()` detecta mudança de inode ✅
5. Reseta offset para 0 ✅

**Resultado**: CORRETO (stat() segue symlinks) ✅

### 6.3 Edge Case: Arquivo Sobrescrito (> operator)

**Cenário**:
```bash
# Estado inicial
/var/log/app.log (inode: 123, size: 10MB, position: 9MB)

# Sobrescrever arquivo (não truncate, mas overwrite completo)
cat /dev/null > /var/log/app.log
```

**Comportamento**:
- Inode: MANTIDO (mesmo arquivo)
- Size: 0 (novo)
- Stored position: 9MB

**Resultado**:
1. `stat()` retorna size=0, inode=123
2. `UpdatePosition()`: `size (0) < storedSize (10MB)` → Truncation detected ✅
3. Reseta offset para 0 ✅

**Resultado**: CORRETO ✅

### 6.4 Edge Case: Multiplas Rotações Rápidas

**Cenário**:
```bash
# T+0s: app.log (inode: 100)
# T+1s: Rotation 1 → app.log (inode: 101)
# T+2s: Rotation 2 → app.log (inode: 102)
# T+3s: Rotation 3 → app.log (inode: 103)
# FileMonitor poll interval: 5s (detecta apenas em T+5s)
```

**Comportamento**:
1. Em T+5s, stat() retorna inode=103
2. Stored inode=100
3. Detecta rotation ✅
4. Reseta offset=0 ✅
5. Lê arquivo atual (inode 103)

**Problema**: Arquivos 101 e 102 NUNCA foram lidos ❌

**Impacto**:
- **Severidade**: ALTA (perda de logs)
- **Probabilidade**: MUITO BAIXA (rotation a cada 1s é extremo)
- **Consequência**: Logs perdidos durante rotações intermediárias

**Mitigação**:
- Diminuir poll interval (atual: 2s, já razoável)
- Usar inotify para detecção imediata (fsnotify já usado ✅)

**Conclusão**: Risco aceitável com configuração atual ✅

### 6.5 Failure Scenario: Disco Cheio Durante Save

**Cenário**:
```bash
# Flush periódico tenta salvar posições
# Disco: 0 bytes livres
```

**Comportamento**:
```go
// SavePositions()
err := os.WriteFile(tempFile, data, 0644)
// err = "no space left on device"

if err != nil {
    return fmt.Errorf("failed to write temp positions file: %w", err)
}
```

**Resultado**:
1. Write falha com erro ✅
2. Temp file não é criado (ou parcialmente criado)
3. Rename não acontece
4. Original `positions.json` permanece intacto ✅
5. Próximo flush tentará novamente

**Conclusão**: Comportamento CORRETO (atomic write protege) ✅

### 6.6 Failure Scenario: Permissão Negada

**Cenário**:
```bash
chmod 444 /app/data/positions/file_positions.json  # Read-only
```

**Comportamento**:
```go
err := os.WriteFile(tempFile, data, 0644)
// err = "permission denied"
```

**Resultado**: Similar a disco cheio, erro é tratado ✅

**Problema**: Erro se repete a cada flush (log spam)

**Recomendação**: Adicionar backoff ou alerting se N falhas consecutivas

### 6.7 Failure Scenario: JSON Corrompido no Load

**Cenário**:
```bash
# Corromper positions.json manualmente
echo "{ invalid json" > /app/data/positions/file_positions.json
```

**Comportamento** (`file_positions.go:59-83`):
```go
func (fpm *FilePositionManager) LoadPositions() error {
    data, err := os.ReadFile(fpm.filename)
    if err != nil {
        if os.IsNotExist(err) {
            fpm.logger.Info("File positions file not found, starting fresh", nil)
            return nil  // ✅ OK se não existe
        }
        return fmt.Errorf("failed to read positions file: %w", err)
    }

    var positions map[string]*FilePosition
    if err := json.Unmarshal(data, &positions); err != nil {
        return fmt.Errorf("failed to unmarshal positions: %w", err)  // ❌ ERRO
    }

    fpm.positions = positions
    return nil
}
```

**Resultado**:
1. `json.Unmarshal()` falha com erro ❌
2. `PositionBufferManager.Start()` retorna erro
3. Application falha ao iniciar (se erro não tratado) ❌

**Problema**: Sem recovery automático, arquivo corrompido trava sistema

**Recomendação**:
```go
if err := json.Unmarshal(data, &positions); err != nil {
    fpm.logger.Error("Failed to unmarshal positions, starting fresh", map[string]interface{}{
        "error": err.Error(),
    })
    // Backup do arquivo corrompido
    os.Rename(fpm.filename, fpm.filename + ".corrupted")
    // Iniciar com mapa vazio
    fpm.positions = make(map[string]*FilePosition)
    return nil  // ✅ Recover gracefully
}
```

---

## 7. Test Coverage Analysis

### 7.1 Testes Existentes

**Arquivo**: `pkg/positions/position_manager_test.go`

| Teste | Linha | Cobre |
|-------|-------|-------|
| `TestPositionManager_NewPositionManager` | 16-33 | Constructor ✅ |
| `TestPositionManager_SetAndGetPosition` | 35-67 | Basic set/get ✅ |
| `TestPositionManager_GetPosition_NotExists` | 69-89 | Get non-existent ✅ |
| `TestPositionManager_DetectTruncation` | 91-122 | Truncation detection ✅ |
| `TestPositionManager_CheckTruncation_NoTruncation` | 124-153 | No truncation case ✅ |
| `TestPositionManager_SaveAndLoadPositions` | 155-205 | Persistence ✅ |
| `TestPositionManager_CleanupExpiredPositions` | 207-247 | Cleanup ✅ |
| `TestPositionManager_CreateBackup` | 249-288 | Backup creation ✅ |
| `TestPositionManager_AutoSave` | 290-331 | Auto-save loop ✅ |
| `TestPositionManager_ConcurrentAccess` | 333-392 | Concurrency ✅ |
| `TestPositionManager_DisabledConfig` | 394-417 | Disabled mode ✅ |
| `TestPositionManager_InvalidFilePath` | 419-436 | Error handling ✅ |

**Total de testes**: 12
**Cobertura estimada**: ~45% (baseado em LOC coberto vs total)

### 7.2 Gaps Críticos de Teste

#### ❌ GAP 1: File Rotation Detection

**Ausente**: Teste que simula mudança de inode/device

**Teste Necessário**:
```go
func TestFilePositionManager_DetectFileRotation(t *testing.T) {
    // Setup
    manager := NewFilePositionManager("/tmp/positions", logger)

    // Set initial position
    manager.UpdatePosition("/var/log/app.log", 1000, 2000, time.Now(),
        123, // inode
        2049, // device
        1000, 10)

    // Simulate rotation (new inode)
    manager.UpdatePosition("/var/log/app.log", 500, 1000, time.Now(),
        456, // NEW inode
        2049,
        500, 5)

    // Verify offset was reset
    pos := manager.GetPosition("/var/log/app.log")
    assert.Equal(t, int64(0), pos.Offset, "Offset should be reset on rotation")
    assert.Equal(t, uint64(456), pos.Inode, "Inode should be updated")
}
```

#### ❌ GAP 2: Offset Validation

**Ausente**: Teste para offset > filesize

**Teste Necessário**:
```go
func TestFilePositionManager_InvalidOffset(t *testing.T) {
    manager := NewFilePositionManager("/tmp/positions", logger)

    // Try to set offset > size
    manager.UpdatePosition("/var/log/app.log",
        10000, // offset
        1000,  // size (offset > size!)
        time.Now(), 123, 2049, 10000, 10)

    pos := manager.GetPosition("/var/log/app.log")
    assert.LessOrEqual(t, pos.Offset, pos.Size, "Offset should never exceed size")
}
```

#### ❌ GAP 3: Concurrent Update + Save

**Ausente**: Teste de race condition entre UpdatePosition e SavePositions

**Teste Necessário**:
```go
func TestFilePositionManager_ConcurrentUpdateAndSave(t *testing.T) {
    manager := NewFilePositionManager("/tmp/test_concurrent.json", logger)

    var wg sync.WaitGroup

    // Goroutine 1: Continuous updates
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 100; i++ {
            manager.UpdatePosition("/var/log/app.log", int64(i*100), int64(i*200),
                time.Now(), 123, 2049, 100, 1)
            time.Sleep(1 * time.Millisecond)
        }
    }()

    // Goroutine 2: Continuous saves
    wg.Add(1)
    go func() {
        defer wg.Done()
        for i := 0; i < 50; i++ {
            manager.SavePositions()
            time.Sleep(2 * time.Millisecond)
        }
    }()

    wg.Wait()

    // Verify final state is consistent
    pos := manager.GetPosition("/var/log/app.log")
    assert.NotNil(t, pos)
    assert.LessOrEqual(t, pos.Offset, pos.Size)
}
```

#### ❌ GAP 4: Corrupted JSON Recovery

**Ausente**: Teste de load com JSON corrompido

**Teste Necessário**:
```go
func TestFilePositionManager_LoadCorruptedJSON(t *testing.T) {
    posFile := "/tmp/test_corrupted.json"

    // Write corrupted JSON
    os.WriteFile(posFile, []byte("{ invalid json ]"), 0644)
    defer os.Remove(posFile)

    manager := NewFilePositionManager(filepath.Dir(posFile), logger)

    // Should not panic, should recover gracefully
    err := manager.LoadPositions()

    // Depending on implementation:
    // Option 1: Error returned
    // Option 2: Error logged, starts fresh

    // System should still be functional
    manager.UpdatePosition("/test.log", 100, 200, time.Now(), 123, 2049, 100, 1)
    pos := manager.GetPosition("/test.log")
    assert.NotNil(t, pos)
}
```

#### ❌ GAP 5: Force Flush on Exit

**Ausente**: Teste que verifica flush final no Stop()

**Teste Necessário**:
```go
func TestPositionBufferManager_ForceFlushOnExit(t *testing.T) {
    posFile := "/tmp/test_force_flush.json"
    defer os.Remove(posFile)

    config := &BufferConfig{
        FlushInterval:    10 * time.Second,  // Long interval
        ForceFlushOnExit: true,
    }

    manager := NewPositionBufferManager(
        NewContainerPositionManager(filepath.Dir(posFile), logger),
        NewFilePositionManager(filepath.Dir(posFile), logger),
        config,
        logger,
    )

    manager.Start()

    // Update position
    manager.UpdateFilePosition("/test.log", 1000, 2000, time.Now(), 123, 2049, 1000, 10)

    // Stop immediately (before flush interval)
    time.Sleep(100 * time.Millisecond)
    manager.Stop()

    // Verify position was saved
    manager2 := NewPositionBufferManager(
        NewContainerPositionManager(filepath.Dir(posFile), logger),
        NewFilePositionManager(filepath.Dir(posFile), logger),
        config,
        logger,
    )
    manager2.Start()

    pos := manager2.GetFilePosition("/test.log")
    assert.NotNil(t, pos)
    assert.Equal(t, int64(1000), pos.Offset)
}
```

#### ❌ GAP 6: Memory Limit Enforcement

**Ausente**: Teste do limite `MaxMemoryPositions`

**Teste Necessário**:
```go
func TestPositionBufferManager_MemoryLimitEnforcement(t *testing.T) {
    config := &BufferConfig{
        MaxMemoryPositions: 10,  // Low limit for testing
    }

    manager := NewPositionBufferManager(
        NewContainerPositionManager("/tmp/positions", logger),
        NewFilePositionManager("/tmp/positions", logger),
        config,
        logger,
    )
    manager.Start()
    defer manager.Stop()

    // Try to add more positions than limit
    for i := 0; i < 20; i++ {
        manager.UpdateFilePosition(
            fmt.Sprintf("/var/log/file%d.log", i),
            1000, 2000, time.Now(), uint64(i), 2049, 1000, 10,
        )
    }

    stats := manager.GetStats()
    bufferStats := stats["buffer_manager"].(map[string]interface{})

    // Should have triggered emergency flush
    assert.Greater(t, bufferStats["memory_limit_reached"], int64(0))

    // Total positions should not exceed limit significantly
    // (may exceed briefly during emergency flush)
}
```

### 7.3 Resumo de Test Coverage

| Componente | Testes Existentes | Testes Necessários | Coverage Estimada |
|------------|-------------------|-------------------|-------------------|
| BufferManager | 4 (basic) | 3 (edge cases) | ~40% |
| FilePositionManager | 8 | 6 (edge cases) | ~50% |
| ContainerPositionManager | 0 | 8 (full suite) | ~0% ❌ |
| Integration (FileMonitor + Positions) | 0 | 5 | ~0% ❌ |
| **TOTAL** | **12** | **22** | **~35%** ⚠️ |

**Meta de Coverage**: 70%
**Coverage Atual**: ~35%
**Gap**: 35% (precisa adicionar ~22 testes)

---

## 8. Métricas e Observabilidade

### 8.1 Métricas Existentes

**Encontradas em código**:
```go
// internal/metrics/metrics.go:232-233
log_capturer_file_monitor_offset_restored_total
```

### 8.2 Métricas Recomendadas

#### Categoria: Position Save/Load

```go
// Position save operations
log_capturer_position_save_total{manager="file|container", status="success|error"}
log_capturer_position_save_duration_seconds{manager="file|container"}
log_capturer_position_save_size_bytes{manager="file|container"}

// Position load operations
log_capturer_position_load_total{manager="file|container", status="success|error"}
log_capturer_position_load_duration_seconds{manager="file|container"}
log_capturer_position_load_count{manager="file|container"}  // Número de posições carregadas
```

**Implementação**:
```go
// In file_positions.go
func (fpm *FilePositionManager) SavePositions() error {
    start := time.Now()

    // ... existing save logic ...

    metrics.RecordPositionSave("file", len(fpm.positions), time.Since(start), err == nil)
    return err
}
```

#### Categoria: File Operations

```go
// File rotation detection
log_capturer_file_rotation_detected_total{file_path}
log_capturer_file_rotation_inode_changed_total{file_path}
log_capturer_file_rotation_device_changed_total{file_path}

// File truncation detection
log_capturer_file_truncation_detected_total{file_path}
log_capturer_file_truncation_bytes_lost{file_path}  // oldSize - newSize

// Offset reset
log_capturer_position_offset_reset_total{file_path, reason="rotation|truncation|invalid"}
```

**Implementação**:
```go
// In file_positions.go:151-162
func (fpm *FilePositionManager) UpdatePosition(...) {
    // ...

    if pos.Inode != 0 && (pos.Inode != inode || pos.Device != device) {
        fpm.logger.Info("File rotation detected", ...)
        metrics.RecordFileRotation(filePath, "inode", pos.Inode, inode)  // ← ADD
        pos.Offset = 0
        metrics.RecordOffsetReset(filePath, "rotation")  // ← ADD
    }

    if size < pos.Size {
        fpm.logger.Info("File truncation detected", ...)
        metrics.RecordFileTruncation(filePath, pos.Size, size)  // ← ADD
        pos.Offset = 0
        metrics.RecordOffsetReset(filePath, "truncation")  // ← ADD
    }

    // ...
}
```

#### Categoria: Position State

```go
// Current state
log_capturer_position_count{manager="file|container", status="active|stopped|removed"}
log_capturer_position_oldest_age_seconds{manager="file|container"}
log_capturer_position_offset_bytes{file_path}  // Current offset per file
log_capturer_position_size_bytes{file_path}    // Current size per file
log_capturer_position_lag_bytes{file_path}     // size - offset (bytes behind)

// Dirty tracking
log_capturer_position_dirty{manager="file|container"}  // 1 if dirty, 0 if clean
log_capturer_position_last_flush_seconds{manager="file|container"}  // Time since last flush
```

**Implementação**:
```go
// In buffer_manager.go - add to GetStats() or separate metrics function
func (pbm *PositionBufferManager) UpdateMetrics() {
    fileStats := pbm.fileManager.GetStats()
    containerStats := pbm.containerManager.GetStats()

    metrics.SetPositionCount("file", fileStats["total_positions"].(int))
    metrics.SetPositionDirty("file", pbm.fileManager.IsDirty())
    metrics.SetPositionLastFlush("file", time.Since(pbm.fileManager.GetLastFlushTime()).Seconds())

    // Per-file metrics
    for path, pos := range pbm.fileManager.GetAllPositions() {
        metrics.SetPositionOffset(path, pos.Offset)
        metrics.SetPositionSize(path, pos.Size)
        metrics.SetPositionLag(path, pos.Size - pos.Offset)
    }
}
```

#### Categoria: Errors & Warnings

```go
// Errors
log_capturer_position_save_error_total{manager="file|container", reason="disk_full|permission|corruption"}
log_capturer_position_load_error_total{manager="file|container", reason="not_found|corrupted|permission"}
log_capturer_position_invalid_offset_total{file_path}  // offset > size

// Warnings
log_capturer_position_memory_limit_reached_total
log_capturer_position_dropped_total{reason="memory_limit|error"}
```

#### Categoria: Cleanup

```go
// Cleanup operations
log_capturer_position_cleanup_total{manager="file|container"}
log_capturer_position_cleanup_removed_total{manager="file|container"}
log_capturer_position_cleanup_duration_seconds{manager="file|container"}
```

### 8.3 Alertas Recomendados

```yaml
# Prometheus Alert Rules
groups:
  - name: position_tracking
    interval: 30s
    rules:

    # CRITICAL: Position save failing repeatedly
    - alert: PositionSaveFailureHigh
      expr: rate(log_capturer_position_save_total{status="error"}[5m]) > 0.1
      for: 2m
      labels:
        severity: critical
      annotations:
        summary: "Position save failing repeatedly"
        description: "Position save error rate > 10% for 2 minutes. Data loss risk!"

    # WARNING: Last flush too old
    - alert: PositionFlushStale
      expr: log_capturer_position_last_flush_seconds > 60
      for: 1m
      labels:
        severity: warning
      annotations:
        summary: "Position flush is stale"
        description: "Last flush was {{ $value }}s ago (expected every 30s)"

    # WARNING: High file rotation rate
    - alert: FileRotationRateHigh
      expr: rate(log_capturer_file_rotation_detected_total[5m]) > 1
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "High file rotation rate detected"
        description: "File rotation rate > 1/min for 5 minutes. Check logrotate config."

    # CRITICAL: Position memory limit reached frequently
    - alert: PositionMemoryLimitReached
      expr: rate(log_capturer_position_memory_limit_reached_total[5m]) > 0
      for: 2m
      labels:
        severity: critical
      annotations:
        summary: "Position memory limit reached"
        description: "Position memory limit reached. May drop position updates!"

    # WARNING: Large lag between file size and offset
    - alert: PositionLagHigh
      expr: log_capturer_position_lag_bytes > 10485760  # 10MB
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Position lag is high for {{ $labels.file_path }}"
        description: "Lag = {{ $value }} bytes (file growing faster than reading)"

    # CRITICAL: No positions loaded
    - alert: NoPositionsLoaded
      expr: log_capturer_position_count == 0
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "No positions are being tracked"
        description: "Position count is 0. Check if monitoring is working."
```

### 8.4 Grafana Dashboard Recomendado

```json
{
  "dashboard": {
    "title": "Position Tracking Health",
    "rows": [
      {
        "title": "Position Saves",
        "panels": [
          {
            "title": "Save Rate",
            "targets": [
              {
                "expr": "rate(log_capturer_position_save_total[5m])",
                "legendFormat": "{{manager}} - {{status}}"
              }
            ]
          },
          {
            "title": "Save Duration",
            "targets": [
              {
                "expr": "log_capturer_position_save_duration_seconds",
                "legendFormat": "{{manager}}"
              }
            ]
          },
          {
            "title": "Last Flush Age",
            "targets": [
              {
                "expr": "log_capturer_position_last_flush_seconds",
                "legendFormat": "{{manager}}"
              }
            ]
          }
        ]
      },
      {
        "title": "File Operations",
        "panels": [
          {
            "title": "Rotation Events",
            "targets": [
              {
                "expr": "increase(log_capturer_file_rotation_detected_total[1h])",
                "legendFormat": "{{file_path}}"
              }
            ]
          },
          {
            "title": "Truncation Events",
            "targets": [
              {
                "expr": "increase(log_capturer_file_truncation_detected_total[1h])",
                "legendFormat": "{{file_path}}"
              }
            ]
          },
          {
            "title": "Offset Resets",
            "targets": [
              {
                "expr": "increase(log_capturer_position_offset_reset_total[1h])",
                "legendFormat": "{{file_path}} - {{reason}}"
              }
            ]
          }
        ]
      },
      {
        "title": "Position State",
        "panels": [
          {
            "title": "Position Count by Status",
            "targets": [
              {
                "expr": "log_capturer_position_count",
                "legendFormat": "{{manager}} - {{status}}"
              }
            ]
          },
          {
            "title": "Position Lag (Top 10)",
            "targets": [
              {
                "expr": "topk(10, log_capturer_position_lag_bytes)",
                "legendFormat": "{{file_path}}"
              }
            ]
          }
        ]
      }
    ]
  }
}
```

---

## 9. Implementações Recomendadas

### 9.1 Flush Mais Frequente ou Adaptativo

**Problema**: Flush interval de 30s permite perda de até 30s de posições em crash.

**Opção 1: Flush Adaptativo baseado em Volume**

```go
// In buffer_manager.go
type BufferConfig struct {
    FlushInterval       time.Duration
    FlushAfterUpdates   int  // Flush após N updates (novo)
    // ...
}

type PositionBufferManager struct {
    // ...
    updatesSinceFlush int
    updateMutex       sync.Mutex
}

func (pbm *PositionBufferManager) UpdateFilePosition(...) {
    pbm.fileManager.UpdatePosition(...)

    pbm.updateMutex.Lock()
    pbm.updatesSinceFlush++
    updates := pbm.updatesSinceFlush
    pbm.updateMutex.Unlock()

    // Trigger flush if threshold reached
    if updates >= pbm.config.FlushAfterUpdates {
        go pbm.Flush()  // Async flush
    }

    pbm.stats.mu.Lock()
    pbm.stats.totalUpdates++
    pbm.stats.mu.Unlock()
}

func (pbm *PositionBufferManager) Flush() error {
    // ... existing flush logic ...

    pbm.updateMutex.Lock()
    pbm.updatesSinceFlush = 0
    pbm.updateMutex.Unlock()

    return nil
}
```

**Configuração Recomendada**:
```yaml
position_tracking:
  flush_interval: 30s        # Flush time-based
  flush_after_updates: 100   # Flush após 100 updates
  # Whichever comes first
```

**Opção 2: Flush Configurável por Prioridade**

```go
// Critical logs flush immediately
// Normal logs flush after 30s
func (pbm *PositionBufferManager) UpdateFilePosition(filePath string, ..., priority string) {
    pbm.fileManager.UpdatePosition(...)

    if priority == "critical" || priority == "high" {
        go pbm.Flush()  // Immediate flush for critical files
    }
}
```

**Resultado Esperado**:
- Reduz janela de perda de posições de 30s → 5-10s
- Aumenta I/O (mais flushes), mas aceitável

### 9.2 Sistema de Backup de Posições

**Implementação**:

```go
// In file_positions.go
type FilePositionManager struct {
    // ...
    backupCount int  // Número de backups a manter
}

func (fpm *FilePositionManager) SavePositions() error {
    // Create backup before saving
    if err := fpm.createBackup(); err != nil {
        fpm.logger.Warn("Failed to create backup", map[string]interface{}{
            "error": err.Error(),
        })
        // Continue with save anyway (backup is nice-to-have)
    }

    // ... existing save logic ...
}

func (fpm *FilePositionManager) createBackup() error {
    // Check if current file exists
    if _, err := os.Stat(fpm.filename); os.IsNotExist(err) {
        return nil  // No file to backup
    }

    // Rotate existing backups
    // positions.json.3 → delete
    // positions.json.2 → positions.json.3
    // positions.json.1 → positions.json.2
    // positions.json → positions.json.1

    for i := fpm.backupCount - 1; i >= 1; i-- {
        oldBackup := fmt.Sprintf("%s.%d", fpm.filename, i)
        newBackup := fmt.Sprintf("%s.%d", fpm.filename, i+1)

        if i == fpm.backupCount - 1 {
            // Delete oldest backup
            os.Remove(newBackup)
        } else {
            // Rotate
            os.Rename(oldBackup, newBackup)
        }
    }

    // Create new backup
    backup1 := fmt.Sprintf("%s.1", fpm.filename)
    if err := copyFile(fpm.filename, backup1); err != nil {
        return fmt.Errorf("failed to create backup: %w", err)
    }

    fpm.logger.Debug("Created position backup", map[string]interface{}{
        "backup": backup1,
    })

    return nil
}

func copyFile(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    return os.WriteFile(dst, data, 0644)
}

// Recovery from backup
func (fpm *FilePositionManager) LoadPositions() error {
    // Try to load from main file
    err := fpm.loadFromFile(fpm.filename)
    if err == nil {
        return nil  // Success
    }

    fpm.logger.Warn("Failed to load positions from main file, trying backups", map[string]interface{}{
        "error": err.Error(),
    })

    // Try backups in order
    for i := 1; i <= fpm.backupCount; i++ {
        backupFile := fmt.Sprintf("%s.%d", fpm.filename, i)
        if err := fpm.loadFromFile(backupFile); err == nil {
            fpm.logger.Info("Successfully loaded from backup", map[string]interface{}{
                "backup": backupFile,
            })
            // Restore main file from backup
            copyFile(backupFile, fpm.filename)
            return nil
        }
    }

    // All backups failed, start fresh
    fpm.logger.Error("All position files corrupted, starting fresh", nil)
    fpm.positions = make(map[string]*FilePosition)
    return nil
}

func (fpm *FilePositionManager) loadFromFile(filename string) error {
    data, err := os.ReadFile(filename)
    if err != nil {
        return err
    }

    var positions map[string]*FilePosition
    if err := json.Unmarshal(data, &positions); err != nil {
        return err
    }

    fpm.positions = positions
    return nil
}
```

**Configuração**:
```yaml
position_tracking:
  backup_count: 3  # Manter 3 backups
```

**Resultado**:
- Proteção contra corrupção de arquivo
- Recovery automático de backups
- Custo: Espaço em disco (3x o tamanho de positions.json)

### 9.3 Validação de Offset

**Implementação**:

```go
// In file_positions.go:138
func (fpm *FilePositionManager) UpdatePosition(filePath string, offset int64, size int64, ...) {
    fpm.mu.Lock()
    defer fpm.mu.Unlock()

    // ... existing code ...

    // ADICIONAR: Validação de offset
    if offset > size {
        fpm.logger.Warn("Invalid offset (offset > size), resetting to size", map[string]interface{}{
            "file_path": filePath,
            "offset":    offset,
            "size":      size,
        })
        offset = size  // Clamp to size (or 0 if you prefer)
        metrics.RecordInvalidOffset(filePath)  // Metric
    }

    // Check for FILE ROTATION...
    // ...
}
```

**Também adicionar em FileMonitor**:

```go
// In file_monitor.go:743
if mf.position > 0 {
    // ADICIONAR: Validar offset antes de seek
    info, err := file.Stat()
    if err == nil && mf.position > info.Size() {
        fm.logger.WithFields(logrus.Fields{
            "path":         mf.path,
            "saved_offset": mf.position,
            "file_size":    info.Size(),
        }).Warn("Saved offset exceeds file size, resetting to 0")
        mf.position = 0
        metrics.RecordInvalidOffset(mf.path)
    }

    if _, err := file.Seek(mf.position, 0); err != nil {
        // ... existing error handling ...
    }
}
```

### 9.4 Fix Race Condition em dirty Flag

**Implementação** (Opção 2 - Separar locks):

```go
// In file_positions.go:85
func (fpm *FilePositionManager) SavePositions() error {
    // Phase 1: Marshal data (read-only, use RLock)
    fpm.mu.RLock()
    data, err := json.MarshalIndent(fpm.positions, "", "  ")
    fpm.mu.RUnlock()

    if err != nil {
        return fmt.Errorf("failed to marshal positions: %w", err)
    }

    // Phase 2: Write to disk (no lock needed)
    tempFile := fpm.filename + ".tmp"
    if err := os.WriteFile(tempFile, data, 0644); err != nil {
        return fmt.Errorf("failed to write temp positions file: %w", err)
    }

    if err := os.Rename(tempFile, fpm.filename); err != nil {
        os.Remove(tempFile)
        return fmt.Errorf("failed to rename positions file: %w", err)
    }

    // Phase 3: Update state (write, use Lock)
    fpm.mu.Lock()
    fpm.dirty = false
    fpm.lastFlush = time.Now()
    fpm.mu.Unlock()

    fpm.logger.Debug("Saved file positions", map[string]interface{}{
        "count": len(fpm.positions),
        "file":  fpm.filename,
    })

    return nil
}
```

**Resultado**: Elimina race condition no dirty flag ✅

### 9.5 Adicionar Métricas

**Implementação** (exemplo para file rotation):

```go
// In internal/metrics/metrics.go

// Adicionar counters
var (
    fileRotationDetected = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "log_capturer_file_rotation_detected_total",
            Help: "Total file rotations detected",
        },
        []string{"file_path", "change_type"},
    )

    fileTruncationDetected = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "log_capturer_file_truncation_detected_total",
            Help: "Total file truncations detected",
        },
        []string{"file_path"},
    )

    positionOffsetReset = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "log_capturer_position_offset_reset_total",
            Help: "Total position offset resets",
        },
        []string{"file_path", "reason"},
    )

    positionSaveDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "log_capturer_position_save_duration_seconds",
            Help:    "Duration of position save operations",
            Buckets: prometheus.DefBuckets,
        },
        []string{"manager", "status"},
    )
)

func init() {
    prometheus.MustRegister(fileRotationDetected)
    prometheus.MustRegister(fileTruncationDetected)
    prometheus.MustRegister(positionOffsetReset)
    prometheus.MustRegister(positionSaveDuration)
}

// Helper functions
func RecordFileRotation(filePath, changeType string, oldValue, newValue uint64) {
    fileRotationDetected.WithLabelValues(filePath, changeType).Inc()
}

func RecordFileTruncation(filePath string, oldSize, newSize int64) {
    fileTruncationDetected.WithLabelValues(filePath).Inc()
}

func RecordOffsetReset(filePath, reason string) {
    positionOffsetReset.WithLabelValues(filePath, reason).Inc()
}

func RecordPositionSave(manager string, count int, duration time.Duration, success bool) {
    status := "success"
    if !success {
        status = "error"
    }
    positionSaveDuration.WithLabelValues(manager, status).Observe(duration.Seconds())
}
```

**Uso** (em file_positions.go):

```go
func (fpm *FilePositionManager) UpdatePosition(...) {
    // ...

    if pos.Inode != 0 && (pos.Inode != inode || pos.Device != device) {
        fpm.logger.Info("File rotation detected", ...)
        metrics.RecordFileRotation(filePath, "inode", pos.Inode, inode)  // ← ADD
        pos.Offset = 0
        metrics.RecordOffsetReset(filePath, "rotation")  // ← ADD
    }

    // ...
}

func (fpm *FilePositionManager) SavePositions() error {
    start := time.Now()

    // ... existing save logic ...

    metrics.RecordPositionSave("file", len(fpm.positions), time.Since(start), err == nil)  // ← ADD
    return err
}
```

---

## 10. Plano de Ação

### Fase 1: Melhorias Críticas (P0) - Semana 1

| # | Tarefa | Responsável | Esforço | Prioridade |
|---|--------|-------------|---------|------------|
| 1.1 | Implementar flush adaptativo (9.1) | golang + software-engineering | 4h | P0 |
| 1.2 | Adicionar validação de offset (9.3) | golang | 2h | P0 |
| 1.3 | Fix race condition em dirty flag (9.4) | golang | 1h | P0 |
| 1.4 | Adicionar métricas básicas (rotation, truncation, save) | observability + golang | 3h | P0 |
| 1.5 | Testes de edge cases críticos (7.2 - GAP 1, 2, 4) | qa-specialist + golang | 6h | P0 |
| **Total Fase 1** | | | **16h (2 dias)** | |

**Critério de Sucesso Fase 1**:
- [ ] Flush adaptativo funcionando (flush a cada 100 updates OU 30s)
- [ ] Offset validation implementado e testado
- [ ] Race condition eliminado (go test -race passa)
- [ ] Métricas básicas disponíveis no /metrics endpoint
- [ ] Testes de file rotation e truncation passando

### Fase 2: Sistema de Backup e Recovery (P1) - Semana 2

| # | Tarefa | Responsável | Esforço | Prioridade |
|---|--------|-------------|---------|------------|
| 2.1 | Implementar backup rotation (9.2) | golang | 4h | P1 |
| 2.2 | Implementar recovery de backup (9.2) | golang | 3h | P1 |
| 2.3 | Adicionar tratamento de JSON corrompido (6.7) | golang | 2h | P1 |
| 2.4 | Testes de backup e recovery (7.2 - GAP 4) | qa-specialist | 4h | P1 |
| 2.5 | Documentar processo de recovery manual | documentation-specialist | 2h | P1 |
| **Total Fase 2** | | | **15h (2 dias)** | |

**Critério de Sucesso Fase 2**:
- [ ] Backup automático funcionando (3 gerações)
- [ ] Recovery automático de backup em caso de corrupção
- [ ] JSON corrompido não trava o sistema (start fresh)
- [ ] Testes de corrupted JSON recovery passando
- [ ] Runbook de recovery manual criado

### Fase 3: Observabilidade Completa (P2) - Semana 3

| # | Tarefa | Responsável | Esforço | Prioridade |
|---|--------|-------------|---------|------------|
| 3.1 | Adicionar métricas avançadas (8.2 - todas) | observability + golang | 6h | P2 |
| 3.2 | Criar Prometheus alert rules (8.3) | observability + devops | 3h | P2 |
| 3.3 | Criar Grafana dashboard (8.4) | grafana-specialist | 4h | P2 |
| 3.4 | Adicionar logging estruturado para eventos de position | golang | 2h | P2 |
| 3.5 | Integrar métricas com health check endpoint | golang | 2h | P2 |
| **Total Fase 3** | | | **17h (2 dias)** | |

**Critério de Sucesso Fase 3**:
- [ ] Todas as métricas recomendadas implementadas
- [ ] Alertas configurados no Prometheus
- [ ] Dashboard funcional no Grafana
- [ ] Position health visível em /health endpoint
- [ ] Logs estruturados para auditoria

### Fase 4: Testes e Documentação (P2) - Semana 4

| # | Tarefa | Responsável | Esforço | Prioridade |
|---|--------|-------------|---------|------------|
| 4.1 | Implementar todos os testes faltantes (7.2) | qa-specialist + golang | 12h | P2 |
| 4.2 | Testes de integração (FileMonitor + Positions) | qa-specialist | 6h | P2 |
| 4.3 | Testes de stress (position updates sob carga) | qa-specialist | 4h | P2 |
| 4.4 | Atualizar CLAUDE.md com position system | documentation-specialist | 3h | P2 |
| 4.5 | Criar troubleshooting guide para position issues | documentation-specialist | 3h | P2 |
| **Total Fase 4** | | | **28h (3.5 dias)** | |

**Critério de Sucesso Fase 4**:
- [ ] Test coverage de position system ≥ 70%
- [ ] Testes de integração passando
- [ ] Testes de stress passando (1000 updates/s por 5min)
- [ ] Documentação atualizada
- [ ] Troubleshooting guide completo

### Fase 5: Produção e Monitoramento (P3) - Semana 5

| # | Tarefa | Responsável | Esforço | Prioridade |
|---|--------|-------------|---------|------------|
| 5.1 | Deploy em staging com monitoramento | devops + infrastructure | 3h | P3 |
| 5.2 | Testes de carga em staging | qa-specialist | 4h | P3 |
| 5.3 | Validação de métricas e alertas | observability + grafana | 2h | P3 |
| 5.4 | Deploy gradual em produção (canary) | devops | 2h | P3 |
| 5.5 | Monitoramento pós-deploy (1 semana) | observability + grafana | 8h | P3 |
| **Total Fase 5** | | | **19h (2.5 dias)** | |

**Critério de Sucesso Fase 5**:
- [ ] Deploy em staging sem issues
- [ ] Métricas e alertas validados em staging
- [ ] Canary deployment em produção sem incidentes
- [ ] Monitoramento 24/7 por 1 semana
- [ ] Zero regressões detectadas

### Resumo do Plano

| Fase | Duração | Esforço Total | Agentes Principais |
|------|---------|---------------|-------------------|
| Fase 1 | Semana 1 | 16h (2 dias) | golang, software-engineering, qa, observability |
| Fase 2 | Semana 2 | 15h (2 dias) | golang, qa, documentation |
| Fase 3 | Semana 3 | 17h (2 dias) | observability, grafana, devops, golang |
| Fase 4 | Semana 4 | 28h (3.5 dias) | qa, golang, documentation |
| Fase 5 | Semana 5 | 19h (2.5 dias) | devops, infrastructure, observability, grafana, qa |
| **TOTAL** | **5 semanas** | **95h (~12 dias)** | **22 agents coordinated** |

---

## 11. Conclusões e Recomendações Finais

### 11.1 Avaliação Geral do Sistema

**Pontos Positivos** (✅):
1. **Arquitetura modular**: Separação clara entre BufferManager, FilePositions e ContainerPositions
2. **Thread-safety**: Uso correto de RWMutex na maioria dos casos
3. **Atomic writes**: Persistência com write-to-temp + rename (POSIX atomic)
4. **Detecção de eventos**: File rotation (inode tracking) e truncation (size tracking) bem implementados
5. **Lifecycle management**: Goroutines rastreadas corretamente com WaitGroup
6. **Graceful shutdown**: ForceFlushOnExit garante save final

**Problemas Principais** (⚠️):
1. **Janela de perda de dados**: Flush interval de 30s permite duplicação de logs em crash (P0)
2. **Falta de observabilidade**: Métricas insuficientes para monitorar saúde do sistema (P0)
3. **Test coverage baixo**: ~35% (meta: 70%) - faltam testes de edge cases (P1)
4. **Sem recovery automático**: JSON corrompido trava o sistema (P1)
5. **Race condition menor**: dirty flag modificado sob RLock (P1)

**Risco Geral**: MÉDIO
**Impacto de Falhas**: ALTO (perda de logs, duplicação)
**Recomendação**: IMPLEMENTAR FASE 1 IMEDIATAMENTE

### 11.2 Recomendações por Prioridade

#### Crítico (Implementar Já - Semana 1)
1. ✅ Flush adaptativo (reduz janela de perda para ~5s)
2. ✅ Validação de offset (previne loops de erro)
3. ✅ Métricas básicas (permite detectar problemas)
4. ✅ Fix race condition (elimina warning em -race)
5. ✅ Testes de rotation/truncation (valida comportamento)

#### Alto (Próximas 2 Semanas)
1. ✅ Sistema de backup (proteção contra corrupção)
2. ✅ Recovery automático (resiliência)
3. ✅ Métricas avançadas (observabilidade completa)
4. ✅ Alertas Prometheus (detecção proativa)
5. ✅ Grafana dashboard (visualização)

#### Médio (Próximo Mês)
1. ✅ Test coverage ≥ 70% (qualidade de código)
2. ✅ Testes de integração (validação end-to-end)
3. ✅ Documentação completa (manutenibilidade)
4. ✅ Troubleshooting guide (operação)

### 11.3 Expectativas de Resultado

**Após Fase 1** (Semana 1):
- Janela de perda de posições: 30s → 5-10s ✅
- Métricas básicas disponíveis ✅
- Race conditions eliminadas ✅
- Testes de edge cases críticos passando ✅

**Após Fase 2** (Semana 2):
- Zero risk de perda total de posições (backup) ✅
- Recovery automático de corrupção ✅
- Sistema resiliente a falhas de I/O ✅

**Após Fase 3** (Semana 3):
- Observabilidade completa ✅
- Alertas automáticos para problemas ✅
- Dashboard visual para operação ✅

**Após Fase 4** (Semana 4):
- Test coverage ≥ 70% ✅
- Confiança em code quality ✅
- Documentação completa para novos devs ✅

**Após Fase 5** (Semana 5):
- Sistema em produção com monitoramento ✅
- Zero incidentes detectados ✅
- Produção-ready ✅

### 11.4 Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|--------------|---------|-----------|
| Flush adaptativo aumenta I/O | ALTA | BAIXO | Configurar threshold adequado (100-500 updates) |
| Backup rotation usa mais disco | MÉDIA | BAIXO | Limitar a 3 backups (~600KB total) |
| Métricas aumentam overhead | BAIXA | BAIXO | Usar CounterVec (baixo overhead) |
| Mudanças introduzem regressões | MÉDIA | ALTO | Testes abrangentes + staging + canary |
| Tempo de implementação excede estimativa | MÉDIA | MÉDIO | Priorizar Fase 1, Fases 2-4 podem ser postergadas |

### 11.5 Métricas de Sucesso

**KPIs para Position System**:
```
1. Position Save Success Rate ≥ 99.9%
2. Position Load Success Rate ≥ 99.9%
3. Average Flush Latency < 50ms
4. Position Loss Window < 10s
5. Test Coverage ≥ 70%
6. Zero critical alerts per week
7. File Rotation Detection Rate = 100%
8. Truncation Detection Rate = 100%
```

**Monitorar após Deploy**:
- `log_capturer_position_save_total{status="error"}` → should be near 0
- `log_capturer_position_last_flush_seconds` → should be < 30s
- `log_capturer_file_rotation_detected_total` → should match logrotate events
- `log_capturer_position_memory_limit_reached_total` → should be 0

---

## Apêndice A: Referências

### Arquivos Analisados
- `/home/mateus/log_capturer_go/pkg/positions/buffer_manager.go` (381 LOC)
- `/home/mateus/log_capturer_go/pkg/positions/file_positions.go` (330 LOC)
- `/home/mateus/log_capturer_go/pkg/positions/container_positions.go` (340 LOC)
- `/home/mateus/log_capturer_go/internal/monitors/file_monitor.go` (1640 LOC)
- `/home/mateus/log_capturer_go/pkg/positions/position_manager_test.go` (436 LOC)

### Documentos Relacionados
- `CLAUDE.md` - Developer guide (seção de concurrency patterns)
- `README.md` - User guide
- `docs/CONFIGURATION.md` - Configuration reference
- `docs/TROUBLESHOOTING.md` - Troubleshooting guide

### Ferramentas Usadas
- Go Race Detector (`go test -race`)
- Coverage Tool (`go test -coverprofile`)
- Static Analysis (`golangci-lint`)
- Prometheus Metrics
- Grafana Dashboards

---

## Apêndice B: Exemplo de Posições JSON

### file_positions.json (Exemplo)
```json
{
  "/var/log/app.log": {
    "file_path": "/var/log/app.log",
    "offset": 9534872,
    "size": 10485760,
    "last_modified": "2025-11-07T10:30:45Z",
    "last_read": "2025-11-07T10:35:12Z",
    "inode": 12345678,
    "device": 2049,
    "log_count": 15234,
    "bytes_read": 9534872,
    "status": "active"
  },
  "/var/log/error.log": {
    "file_path": "/var/log/error.log",
    "offset": 512000,
    "size": 524288,
    "last_modified": "2025-11-07T09:15:30Z",
    "last_read": "2025-11-07T10:35:10Z",
    "inode": 87654321,
    "device": 2049,
    "log_count": 342,
    "bytes_read": 512000,
    "status": "active"
  }
}
```

### container_positions.json (Exemplo)
```json
{
  "abc123def456": {
    "container_id": "abc123def456",
    "since": "2025-11-07T08:00:00Z",
    "last_read": "2025-11-07T10:35:15Z",
    "last_log_time": "2025-11-07T10:35:14.523Z",
    "log_count": 8456,
    "bytes_read": 2456789,
    "status": "active",
    "restart_count": 0
  },
  "xyz789ghi012": {
    "container_id": "xyz789ghi012",
    "since": "2025-11-06T18:23:45Z",
    "last_read": "2025-11-07T10:34:58Z",
    "last_log_time": "2025-11-07T10:34:57.891Z",
    "log_count": 23456,
    "bytes_read": 8934567,
    "status": "active",
    "restart_count": 2
  }
}
```

---

## Apêndice C: Comandos Úteis

### Verificar Estado das Posições
```bash
# Ver posições de arquivos
cat /app/data/positions/file_positions.json | jq '.'

# Ver posições de containers
cat /app/data/positions/container_positions.json | jq '.'

# Ver estatísticas via API
curl -s http://localhost:8401/api/positions | jq '.'
```

### Verificar Métricas
```bash
# Todas as métricas de positions
curl -s http://localhost:8001/metrics | grep position

# Save rate
curl -s http://localhost:8001/metrics | grep log_capturer_position_save_total

# Rotation events
curl -s http://localhost:8001/metrics | grep log_capturer_file_rotation_detected_total
```

### Debug Position Issues
```bash
# Ver logs de position tracking
docker logs log_capturer_go 2>&1 | grep -i "position\|rotation\|truncation"

# Ver goroutines (detectar leaks)
curl -s http://localhost:6060/debug/pprof/goroutine?debug=2 | grep -A 5 "position"

# Forçar flush manual (se API disponível)
curl -X POST http://localhost:8401/api/positions/flush
```

### Testes
```bash
# Testes com race detector
go test -race ./pkg/positions/

# Testes com coverage
go test -coverprofile=coverage.out ./pkg/positions/
go tool cover -html=coverage.out

# Benchmark de SavePositions
go test -bench=BenchmarkSavePositions ./pkg/positions/
```

---

**FIM DO DOCUMENTO**

**Próximos Passos**:
1. ✅ Review deste documento com tech leads
2. ✅ Aprovação do plano de ação
3. ✅ Início da Fase 1 (16h de trabalho)

**Data de Criação**: 2025-11-07
**Última Atualização**: 2025-11-07
**Versão**: 1.0
**Status**: ✅ COMPLETO E PRONTO PARA IMPLEMENTAÇÃO
