# Módulo Positions (pkg/positions)

## Estrutura e Responsabilidades

O módulo `positions` é responsável pelo **rastreamento persistente** de posições de leitura de logs para garantir que não haja perda ou duplicação de dados em caso de reinicializações ou falhas do sistema. Este módulo mantém estado confiável para recuperação sem perda de dados.

### Arquivos Principais:
- `buffer_manager.go` - Gerenciador principal de posições com buffer em memória
- `file_positions.go` - Rastreamento de posições para arquivos de log
- `container_positions.go` - Rastreamento de posições para logs de containers
- `position_manager_test.go` - Testes unitários

## Funcionamento

### Arquitetura de Position Management:
```
[Sources] -> [Position Updates] -> [Memory Buffer] -> [Persistent Storage] -> [Recovery]
     |             |                    |                   |                   |
FileMonitor    UpdatePosition      BufferManager      JSON Files         LoadPositions
ContainerMon   GetPosition         FlushInterval      Atomic Write       RestoreState
Readers        SetPosition         MemoryLimit        Backup Files       ContinueReading
```

### Conceitos Principais:

#### 1. **Position Tracking**
- **File Positions**: Offset, inode, device para arquivos
- **Container Positions**: Stream position, timestamp para containers
- **Metadata**: Last modified, bytes read, log count para estatísticas

#### 2. **Buffer Management**
- **Memory Buffer**: Cache em memória para performance
- **Periodic Flush**: Flush automático para disco
- **Cleanup**: Limpeza de posições antigas
- **Memory Limits**: Proteção contra uso excessivo de memória

#### 3. **Persistence Strategy**
- **Atomic Writes**: Escritas atômicas para evitar corrupção
- **JSON Format**: Formato human-readable para debugging
- **Backup Files**: Backup para recuperação em caso de corrupção

### Estrutura Principal:
```go
type PositionBufferManager struct {
    containerManager *ContainerPositionManager
    fileManager      *FilePositionManager
    config           *BufferConfig
    logger           *logrus.Logger

    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup

    flushTicker   *time.Ticker
    cleanupTicker *time.Ticker
    tickerMutex   sync.RWMutex

    stats struct {
        totalFlushes         int64
        totalCleanups        int64
        lastFlushDuration    time.Duration
        lastCleanupDuration  time.Duration
        totalUpdates         int64
        totalErrors          int64
        memoryLimitReached   int64
        positionsDropped     int64
    }
}

type FilePosition struct {
    FilePath     string    `json:"file_path"`
    Offset       int64     `json:"offset"`
    Size         int64     `json:"size"`
    LastModified time.Time `json:"last_modified"`
    LastRead     time.Time `json:"last_read"`
    Inode        uint64    `json:"inode"`
    Device       uint64    `json:"device"`
    LogCount     int64     `json:"log_count"`
    BytesRead    int64     `json:"bytes_read"`
    Status       string    `json:"status"`
}

type ContainerPosition struct {
    ContainerID   string    `json:"container_id"`
    ContainerName string    `json:"container_name"`
    ImageName     string    `json:"image_name"`
    Since         time.Time `json:"since"`
    LastRead      time.Time `json:"last_read"`
    LogCount      int64     `json:"log_count"`
    BytesRead     int64     `json:"bytes_read"`
    Status        string    `json:"status"`
}
```

## Papel e Importância

### Data Integrity:
- **No Data Loss**: Garantia de que nenhum log seja perdido
- **No Duplication**: Prevenção de processamento duplicado
- **Crash Recovery**: Recuperação automática após falhas

### Performance:
- **Memory Buffering**: Buffer em memória para reduzir I/O
- **Batch Operations**: Operações em lote para eficiência
- **Lazy Persistence**: Persistência lazy para otimização

### Reliability:
- **Atomic Operations**: Operações atômicas para consistência
- **Corruption Protection**: Proteção contra corrupção de dados
- **Backup Strategy**: Estratégia de backup para recovery

## Configurações Aplicáveis

### Buffer Configuration:
```yaml
positions:
  enabled: true
  directory: "/app/data/positions"

  buffer:
    flush_interval: "30s"
    max_memory_buffer: 1000
    max_memory_positions: 5000
    force_flush_on_exit: true
    cleanup_interval: "5m"
    max_position_age: "24h"

  file_positions:
    enabled: true
    filename: "file_positions.json"
    backup_count: 3
    compression: false

  container_positions:
    enabled: true
    filename: "container_positions.json"
    backup_count: 3
    track_inactive: "1h"
```

### Advanced Configuration:
```yaml
positions:
  # Performance tuning
  performance:
    batch_size: 100
    write_buffer_size: 65536
    sync_strategy: "periodic"  # immediate, periodic, lazy

  # Memory management
  memory:
    enable_memory_limit: true
    soft_limit_mb: 50
    hard_limit_mb: 100
    cleanup_threshold: 0.8

  # Persistence strategy
  persistence:
    format: "json"  # json, binary, msgpack
    compression: true
    compression_level: 6
    atomic_writes: true
    create_backups: true

  # Recovery settings
  recovery:
    enable_checksum: true
    corruption_strategy: "restore_backup"  # restore_backup, recreate, fail
    validation_on_load: true
```

## Algoritmos de Position Management

### File Position Tracking:
```go
func (fpm *FilePositionManager) UpdatePosition(filePath string, offset int64, fileInfo os.FileInfo) error {
    position := &FilePosition{
        FilePath:     filePath,
        Offset:       offset,
        Size:         fileInfo.Size(),
        LastModified: fileInfo.ModTime(),
        LastRead:     time.Now(),
        Inode:        getInode(fileInfo),
        Device:       getDevice(fileInfo),
        Status:       "active",
    }

    // Handle file rotation detection
    if existing := fpm.GetPosition(filePath); existing != nil {
        if existing.Inode != position.Inode ||
           existing.Size > position.Size {
            // File was rotated, reset position
            position.Offset = 0
            fpm.logger.Info("File rotation detected", map[string]interface{}{
                "file": filePath,
                "old_inode": existing.Inode,
                "new_inode": position.Inode,
            })
        }
    }

    return fpm.setPosition(filePath, position)
}
```

### Container Position Tracking:
```go
func (cpm *ContainerPositionManager) UpdatePosition(containerID string, since time.Time) error {
    position := &ContainerPosition{
        ContainerID:   containerID,
        ContainerName: cpm.getContainerName(containerID),
        ImageName:     cpm.getImageName(containerID),
        Since:         since,
        LastRead:      time.Now(),
        Status:        "active",
    }

    return cpm.setPosition(containerID, position)
}
```

### Memory Buffer Management:
```go
func (pbm *PositionBufferManager) checkMemoryLimits() {
    totalPositions := len(pbm.fileManager.positions) + len(pbm.containerManager.positions)

    if totalPositions > pbm.config.MaxMemoryPositions {
        // Cleanup old positions
        pbm.cleanupOldPositions()

        // If still over limit, force flush
        if totalPositions > pbm.config.MaxMemoryPositions {
            pbm.forceFlush()
        }
    }
}
```

## Problemas Conhecidos

### Performance:
- **I/O Overhead**: Flush frequente pode impactar performance
- **Memory Usage**: Muitas posições podem consumir memória excessiva
- **Lock Contention**: Contenção em alto paralelismo

### Reliability:
- **Corruption Risk**: Arquivos podem se corromper em falhas abruptas
- **Race Conditions**: Condições de corrida entre flush e update
- **Disk Space**: Posições podem consumir espaço significativo

### Operational:
- **File Rotation**: Detecção de rotação pode falhar em alguns cenários
- **Cleanup Complexity**: Cleanup de posições antigas pode ser complexo
- **Recovery Time**: Recovery pode ser lento com muitas posições

## Melhorias Propostas

### Advanced Position Storage:
```go
type PositionStore interface {
    Load() (map[string]Position, error)
    Save(positions map[string]Position) error
    Backup() error
    Restore(backupID string) error
}

type BinaryPositionStore struct {
    file       *os.File
    compressor *compression.Writer
    checksum   hash.Hash
}

type DatabasePositionStore struct {
    db     *sql.DB
    table  string
    schema PositionSchema
}
```

### Intelligent Cleanup:
```go
type PositionCleanupStrategy interface {
    ShouldCleanup(position Position) bool
    GetPriority(position Position) int
}

type LRUCleanupStrategy struct {
    maxAge    time.Duration
    maxCount  int
    usageData map[string]time.Time
}

type SmartCleanupStrategy struct {
    fileActivity    map[string]ActivityMetrics
    containerHealth map[string]HealthStatus
    policies        []CleanupPolicy
}
```

### High Availability:
```go
type DistributedPositionManager struct {
    localManager  PositionManager
    remoteStore   RemotePositionStore
    replicator    *PositionReplicator
    conflictResolver *ConflictResolver
}

type RemotePositionStore interface {
    Sync(positions map[string]Position) error
    Fetch(keys []string) (map[string]Position, error)
    Subscribe(callback PositionChangeCallback) error
}
```

### Performance Optimization:
```go
type AsyncPositionWriter struct {
    writeQueue chan PositionUpdate
    batcher    *PositionBatcher
    writer     io.Writer
    metrics    *WriterMetrics
}

type PositionBatcher struct {
    buffer      []PositionUpdate
    maxSize     int
    maxLatency  time.Duration
    compressor  CompressionAlgorithm
}

func (apw *AsyncPositionWriter) WriteAsync(update PositionUpdate) error {
    select {
    case apw.writeQueue <- update:
        return nil
    default:
        return ErrQueueFull
    }
}
```

### Advanced Recovery:
```go
type PositionRecovery struct {
    corruptionDetector *CorruptionDetector
    backupManager      *BackupManager
    validator          *PositionValidator
    repairEngine       *RepairEngine
}

type CorruptionDetector struct {
    checksumValidator  ChecksumValidator
    structureValidator StructureValidator
    crossValidator     CrossReferenceValidator
}

func (pr *PositionRecovery) RecoverFromCorruption(corruptedFile string) (*RecoveryResult, error) {
    // Detect corruption type
    corruption := pr.corruptionDetector.Analyze(corruptedFile)

    // Attempt repair
    if corruption.IsRepairable {
        return pr.repairEngine.Repair(corruptedFile, corruption)
    }

    // Restore from backup
    return pr.backupManager.RestoreLatest(corruptedFile)
}
```

## Métricas Expostas

### Position Metrics:
- `positions_total` - Total de posições rastreadas
- `positions_file_total` - Posições de arquivos
- `positions_container_total` - Posições de containers
- `positions_memory_usage_bytes` - Uso de memória para posições

### Performance Metrics:
- `positions_flush_duration_seconds` - Duração de operações de flush
- `positions_flush_total` - Total de operações de flush
- `positions_cleanup_duration_seconds` - Duração de cleanup
- `positions_updates_total` - Total de atualizações de posição

### Reliability Metrics:
- `positions_errors_total` - Total de erros por tipo
- `positions_corruption_detected_total` - Corrupções detectadas
- `positions_recovery_operations_total` - Operações de recovery
- `positions_backup_operations_total` - Operações de backup

## Exemplo de Uso

### Basic Usage:
```go
// Configuração
config := &positions.BufferConfig{
    FlushInterval:      30 * time.Second,
    MaxMemoryBuffer:    1000,
    MaxMemoryPositions: 5000,
    ForceFlushOnExit:   true,
}

// Criar managers
fileManager := positions.NewFilePositionManager("/app/data/positions", logger)
containerManager := positions.NewContainerPositionManager("/app/data/positions", logger)
bufferManager := positions.NewPositionBufferManager(containerManager, fileManager, config, logger)

// Iniciar
if err := bufferManager.Start(); err != nil {
    log.Fatal(err)
}

// Usar durante leitura de arquivo
position := fileManager.GetPosition("/var/log/app.log")
if position != nil {
    // Continuar leitura da posição salva
    file.Seek(position.Offset, 0)
}

// Atualizar posição após leitura
fileManager.UpdatePosition("/var/log/app.log", newOffset, fileInfo)
```

### Advanced Usage:
```go
// Custom position store
type CustomPositionStore struct {
    redis *redis.Client
    ttl   time.Duration
}

func (cps *CustomPositionStore) SavePosition(key string, position Position) error {
    data, err := json.Marshal(position)
    if err != nil {
        return err
    }

    return cps.redis.Set(key, data, cps.ttl).Err()
}

// Recovery example
func recoverPositions(manager *PositionBufferManager) error {
    if err := manager.LoadPositions(); err != nil {
        // Attempt recovery from backup
        if err := manager.RestoreFromBackup(); err != nil {
            // Start fresh if recovery fails
            logger.Warn("Position recovery failed, starting fresh")
            return manager.InitializeNew()
        }
    }
    return nil
}
```

## Dependências

### Bibliotecas Externas:
- `encoding/json` - Serialização de posições
- `os` - Operações de sistema de arquivos
- `time` - Timestamps e intervalos
- `sync` - Primitivas de sincronização

### Módulos Internos:
- `github.com/sirupsen/logrus` - Logging estruturado
- Integração com monitores de arquivo e container

O módulo `positions` é **crítico** para a confiabilidade do sistema, garantindo que nenhum log seja perdido ou duplicado durante operação normal ou recuperação de falhas. Sua correta implementação e operação são fundamentais para a integridade dos dados capturados.