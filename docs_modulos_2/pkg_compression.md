# Módulo Compression (pkg/compression)

## Estrutura e Responsabilidades

O módulo `compression` é responsável pela **compressão adaptativa** de dados HTTP e payloads de logs para otimizar bandwidth, storage e performance. Este módulo oferece múltiplos algoritmos de compressão com seleção automática baseada no contexto e características dos dados.

### Arquivos Principais:
- `http_compressor.go` - Compressor principal com suporte a múltiplos algoritmos
- `http_compression.go` - Manager de compressão HTTP e interface

## Funcionamento

### Arquitetura de Compressão:
```
[Raw Data] -> [Algorithm Selection] -> [Compression] -> [Compressed Data] -> [HTTP Transport]
     |               |                     |                |                    |
  Payload         Auto Select          Pool Mgmt        Size Check         Network Transfer
  Headers         Accept-Encoding      Gzip/Zstd       Ratio Check        Bandwidth Save
  Content         Data Analysis        LZ4/Snappy      Error Handle       Storage Opt
  Metadata        Performance         Pool Reuse       Metrics Track      Performance
```

### Algoritmos Suportados:

#### 1. **Gzip**
- **Uso**: Compressão padrão web, amplamente suportada
- **Características**: Boa compressão, moderate speed
- **Casos**: HTTP APIs, general purpose

#### 2. **Zstd (Zstandard)**
- **Uso**: Compressão moderna, alta performance
- **Características**: Excellent compression + speed
- **Casos**: High throughput logs, modern systems

#### 3. **LZ4**
- **Uso**: Ultra-fast compression
- **Características**: Very fast, moderate compression
- **Casos**: Real-time streaming, low latency

#### 4. **Snappy**
- **Uso**: Google's fast compression
- **Características**: Fast compression/decompression
- **Casos**: Internal systems, high frequency

#### 5. **Zlib**
- **Uso**: Standard compression library
- **Características**: Good compression, moderate speed
- **Casos**: Legacy compatibility

### Estrutura Principal:
```go
type HTTPCompressor struct {
    config  Config
    logger  *logrus.Logger
    pools   map[Algorithm]*compressionPool
    mutex   sync.RWMutex

    // Metrics
    compressionRatio   *prometheus.HistogramVec
    compressionLatency *prometheus.HistogramVec
    compressionErrors  *prometheus.CounterVec
    algorithmsUsed     *prometheus.CounterVec
}

type compressionPool struct {
    gzipPool   sync.Pool
    zlibPool   sync.Pool
    zstdPool   sync.Pool
    lz4Pool    sync.Pool
    snappyPool sync.Pool
}

type CompressionResult struct {
    Data           []byte
    Algorithm      Algorithm
    OriginalSize   int
    CompressedSize int
    Ratio          float64
    ContentType    string
    Encoding       string
}
```

### Seleção Adaptativa de Algoritmo:
```go
type AlgorithmSelector struct {
    payloadAnalyzer  *PayloadAnalyzer
    performanceData  *PerformanceTracker
    networkConditions *NetworkMonitor
    serverCapabilities map[string][]Algorithm
}

func (as *AlgorithmSelector) SelectOptimal(data []byte, context CompressionContext) Algorithm {
    // Analyze payload characteristics
    characteristics := as.payloadAnalyzer.Analyze(data)

    // Consider network conditions
    bandwidth := as.networkConditions.GetAvailableBandwidth()
    latency := as.networkConditions.GetLatency()

    // Historical performance
    performance := as.performanceData.GetPerformance(characteristics.Type)

    return as.selectBest(characteristics, bandwidth, latency, performance)
}
```

## Papel e Importância

### Bandwidth Optimization:
- **Network Efficiency**: Redução significativa de uso de bandwidth
- **Cost Reduction**: Economia em custos de transferência de dados
- **Speed Improvement**: Menor tempo de transferência

### Storage Optimization:
- **Disk Space**: Redução de espaço necessário para logs
- **Archival**: Compressão eficiente para armazenamento long-term
- **Backup**: Otimização de backups e replicas

### Performance Impact:
- **CPU vs Network Trade-off**: Balance entre CPU usage e network savings
- **Memory Efficiency**: Uso eficiente de pools para reduzir alocações
- **Adaptive Selection**: Seleção automática baseada em condições

## Configurações Aplicáveis

### Configuração Básica:
```yaml
compression:
  enabled: true
  default_algorithm: "gzip"
  adaptive_enabled: true
  min_bytes: 1024
  level: 6
  pool_size: 10

  # Algorithm-specific settings
  algorithms:
    gzip:
      enabled: true
      level: 6
      min_size: 1024
    zstd:
      enabled: true
      level: 3
      min_size: 512
    lz4:
      enabled: true
      level: 0
      min_size: 256
    snappy:
      enabled: true
      min_size: 512

  # Per-sink compression
  per_sink:
    loki:
      algorithm: "gzip"
      enabled: true
      level: 6
    elasticsearch:
      algorithm: "zstd"
      enabled: true
      level: 3
    local_file:
      algorithm: "zstd"
      enabled: true
      level: 6
```

### Configuração Avançada:
```yaml
compression:
  # Adaptive selection
  adaptive:
    enabled: true
    min_compression_ratio: 0.1
    performance_weight: 0.6
    size_weight: 0.4

  # Performance tuning
  performance:
    buffer_size: 65536
    workers: 4
    queue_size: 1000
    timeout: "5s"

  # Quality settings
  quality:
    target_ratio: 0.3
    max_cpu_percent: 20
    prefer_speed: false

  # Pool management
  pools:
    max_idle: 10
    max_active: 50
    idle_timeout: "5m"
    cleanup_interval: "1m"

  # Network awareness
  network:
    bandwidth_threshold_mbps: 100
    latency_threshold_ms: 50
    adaptive_level: true

  # Content-type specific
  content_types:
    "application/json":
      preferred: ["zstd", "gzip"]
      min_size: 512
    "text/plain":
      preferred: ["gzip", "zlib"]
      min_size: 1024
    "application/x-protobuf":
      preferred: ["lz4", "snappy"]
      min_size: 256
```

## Algoritmos de Compressão

### Gzip Implementation:
```go
type GzipCompressor struct {
    level    int
    poolSize int
    pool     sync.Pool
}

func (gc *GzipCompressor) Compress(data []byte) ([]byte, error) {
    var buf bytes.Buffer

    // Get writer from pool
    writer := gc.pool.Get().(*gzip.Writer)
    defer gc.pool.Put(writer)

    writer.Reset(&buf)

    if _, err := writer.Write(data); err != nil {
        return nil, err
    }

    if err := writer.Close(); err != nil {
        return nil, err
    }

    return buf.Bytes(), nil
}
```

### Zstd Implementation:
```go
type ZstdCompressor struct {
    encoder *zstd.Encoder
    level   zstd.EncoderLevel
    pool    sync.Pool
}

func (zc *ZstdCompressor) Compress(data []byte) ([]byte, error) {
    encoder := zc.pool.Get().(*zstd.Encoder)
    defer zc.pool.Put(encoder)

    return encoder.EncodeAll(data, nil), nil
}
```

### Adaptive Selection Logic:
```go
func (hc *HTTPCompressor) SelectAlgorithm(data []byte, context CompressionContext) Algorithm {
    // Skip compression for small payloads
    if len(data) < hc.config.MinBytes {
        return AlgorithmNone
    }

    // Check per-sink configuration
    if sinkConfig, exists := hc.config.PerSink[context.SinkName]; exists {
        if sinkConfig.Enabled {
            return sinkConfig.Algorithm
        }
        return AlgorithmNone
    }

    // Adaptive selection based on data characteristics
    if hc.config.AdaptiveEnabled {
        return hc.adaptiveSelect(data, context)
    }

    return hc.config.DefaultAlgorithm
}

func (hc *HTTPCompressor) adaptiveSelect(data []byte, context CompressionContext) Algorithm {
    // Analyze payload type
    payloadType := hc.analyzePayloadType(data)

    // Get historical performance data
    performance := hc.getPerformanceData(payloadType)

    // Consider network conditions
    networkConditions := hc.getNetworkConditions()

    // Select best algorithm
    switch {
    case networkConditions.HighLatency:
        return AlgorithmLZ4  // Prioritize speed
    case networkConditions.LowBandwidth:
        return AlgorithmZstd // Prioritize compression ratio
    case payloadType == "json":
        return AlgorithmGzip // Good for JSON
    case payloadType == "binary":
        return AlgorithmSnappy // Good for binary data
    default:
        return hc.config.DefaultAlgorithm
    }
}
```

## Problemas Conhecidos

### Performance:
- **CPU Overhead**: Compressão pode consumir significativo CPU
- **Memory Usage**: Buffers de compressão podem usar muita memória
- **Latency Impact**: Compressão adiciona latência no processing

### Quality Trade-offs:
- **Compression vs Speed**: Trade-off entre ratio e velocidade
- **Algorithm Selection**: Seleção subótima pode degradar performance
- **Small Payloads**: Overhead pode ser maior que benefício

### Operational:
- **Pool Management**: Pools podem vazar memória se mal configurados
- **Error Handling**: Falhas de compressão podem afetar delivery
- **Configuration Complexity**: Muitas opções podem confundir usuários

## Melhorias Propostas

### Machine Learning Selection:
```go
type MLCompressionSelector struct {
    model           *MLModel
    featureExtractor *FeatureExtractor
    trainingData    *TrainingDataset
    predictor       *CompressionPredictor
}

type CompressionFeatures struct {
    PayloadSize      float64
    Entropy          float64
    ContentType      string
    RepetitionRatio  float64
    NetworkLatency   float64
    CPULoad          float64
    HistoricalRatio  float64
}

func (mls *MLCompressionSelector) PredictBestAlgorithm(data []byte) Algorithm {
    features := mls.featureExtractor.Extract(data)
    prediction := mls.model.Predict(features)
    return Algorithm(prediction.Algorithm)
}
```

### Streaming Compression:
```go
type StreamingCompressor struct {
    algorithm     Algorithm
    chunkSize     int
    buffer        *ChunkedBuffer
    compressor    io.WriteCloser
    output        chan CompressedChunk
}

type CompressedChunk struct {
    Data     []byte
    Sequence int
    Final    bool
    Error    error
}

func (sc *StreamingCompressor) CompressStream(input io.Reader) <-chan CompressedChunk {
    output := make(chan CompressedChunk, 10)

    go func() {
        defer close(output)

        buffer := make([]byte, sc.chunkSize)
        sequence := 0

        for {
            n, err := input.Read(buffer)
            if n > 0 {
                compressed, compErr := sc.compressChunk(buffer[:n])
                output <- CompressedChunk{
                    Data:     compressed,
                    Sequence: sequence,
                    Final:    err == io.EOF,
                    Error:    compErr,
                }
                sequence++
            }

            if err != nil {
                break
            }
        }
    }()

    return output
}
```

### Content-Aware Compression:
```go
type ContentAwareCompressor struct {
    analyzers    map[string]ContentAnalyzer
    strategies   map[string]CompressionStrategy
    cache        *AnalysisCache
}

type ContentAnalyzer interface {
    Analyze(data []byte) ContentCharacteristics
    CanHandle(contentType string) bool
}

type JSONAnalyzer struct {
    parser     *JSONParser
    statistics *FieldStatistics
}

func (ja *JSONAnalyzer) Analyze(data []byte) ContentCharacteristics {
    parsed, err := ja.parser.Parse(data)
    if err != nil {
        return ContentCharacteristics{Type: "unknown"}
    }

    return ContentCharacteristics{
        Type:           "json",
        FieldCount:     ja.statistics.CountFields(parsed),
        Repetition:     ja.statistics.CalculateRepetition(parsed),
        Compressibility: ja.statistics.EstimateCompressibility(parsed),
    }
}
```

### Performance Monitoring:
```go
type CompressionProfiler struct {
    metrics        *CompressionMetrics
    sampler        *PerformanceSampler
    analyzer       *PerformanceAnalyzer
    optimizer      *CompressionOptimizer
}

type CompressionMetrics struct {
    AlgorithmPerformance map[Algorithm]*AlgorithmMetrics
    PayloadTypeMetrics   map[string]*PayloadMetrics
    NetworkImpact        *NetworkMetrics
}

type AlgorithmMetrics struct {
    CompressionRatio    prometheus.Histogram
    CompressionLatency  prometheus.Histogram
    CPUUsage           prometheus.Gauge
    MemoryUsage        prometheus.Gauge
    ThroughputBytes    prometheus.Counter
}

func (cp *CompressionProfiler) ProfileCompression(algorithm Algorithm, data []byte) *CompressionProfile {
    start := time.Now()

    result, err := cp.compress(algorithm, data)

    latency := time.Since(start)
    ratio := float64(len(result)) / float64(len(data))

    profile := &CompressionProfile{
        Algorithm:   algorithm,
        Latency:     latency,
        Ratio:       ratio,
        InputSize:   len(data),
        OutputSize:  len(result),
        Error:       err,
    }

    cp.updateMetrics(profile)

    return profile
}
```

## Métricas Expostas

### Compression Metrics:
- `compression_ratio` - Taxa de compressão por algoritmo e componente
- `compression_latency_seconds` - Latência de operações de compressão
- `compression_errors_total` - Total de erros de compressão
- `compression_algorithms_used_total` - Uso de algoritmos por tipo

### Performance Metrics:
- `compression_cpu_usage_percent` - Uso de CPU para compressão
- `compression_memory_usage_bytes` - Uso de memória para compressão
- `compression_throughput_bytes_per_second` - Throughput de compressão
- `compression_pool_utilization` - Utilização dos pools

### Quality Metrics:
- `compression_savings_bytes_total` - Total de bytes economizados
- `compression_overhead_seconds` - Overhead de processamento
- `compression_effectiveness_ratio` - Efetividade da compressão

## Exemplo de Uso

### Basic Usage:
```go
// Configuração
config := compression.Config{
    DefaultAlgorithm: compression.AlgorithmGzip,
    AdaptiveEnabled:  true,
    MinBytes:         1024,
    Level:            6,
}

// Criar compressor
compressor := compression.NewHTTPCompressor(config, logger)

// Comprimir dados
data := []byte("large log payload here...")
result, err := compressor.Compress(data, compression.CompressionContext{
    SinkName:    "loki",
    ContentType: "application/json",
})

if err != nil {
    log.Printf("Compression failed: %v", err)
    return
}

log.Printf("Compressed %d bytes to %d bytes (%.1f%% reduction)",
    result.OriginalSize,
    result.CompressedSize,
    (1.0-result.Ratio)*100)
```

### HTTP Request Compression:
```go
// Configurar request com compressão
req, err := http.NewRequest("POST", url, nil)
if err != nil {
    return err
}

// Aplicar compressão
payload := []byte(`{"logs": [...]}`)
if err := compressor.CompressRequest(req, payload); err != nil {
    log.Printf("Failed to compress request: %v", err)
    // Continue without compression
    req.Body = io.NopCloser(bytes.NewReader(payload))
}

// Executar request
resp, err := client.Do(req)
```

### Custom Algorithm:
```go
// Implementar algoritmo customizado
type CustomCompressor struct {
    config CustomConfig
}

func (cc *CustomCompressor) Compress(data []byte) ([]byte, error) {
    // Implementação customizada
    return customCompressionLogic(data), nil
}

func (cc *CustomCompressor) ContentEncoding() string {
    return "custom"
}

// Registrar no manager
manager.RegisterCompressor("custom", &CustomCompressor{})
```

## Dependências

### Bibliotecas Externas:
- `compress/gzip` - Compressão gzip padrão
- `github.com/klauspost/compress/zstd` - Compressão Zstandard
- `github.com/pierrec/lz4/v4` - Compressão LZ4
- `github.com/golang/snappy` - Compressão Snappy
- `github.com/prometheus/client_golang` - Métricas

### Módulos Internos:
- `github.com/sirupsen/logrus` - Logging estruturado
- Integração com sinks para compressão automática

O módulo `compression` é **essencial** para otimização de performance e custos, fornecendo compressão inteligente e adaptativa que pode resultar em economias significativas de bandwidth e storage em ambientes de produção.