package compression

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	metricsOnce sync.Once
	globalCompressionRatio   *prometheus.HistogramVec
	globalCompressionLatency *prometheus.HistogramVec
	globalCompressionErrors  *prometheus.CounterVec
	globalAlgorithmsUsed     *prometheus.CounterVec
)

// Algorithm represents compression algorithms
type Algorithm string

const (
	AlgorithmGzip   Algorithm = "gzip"
	AlgorithmZlib   Algorithm = "zlib"
	AlgorithmZstd   Algorithm = "zstd"
	AlgorithmLZ4    Algorithm = "lz4"
	AlgorithmSnappy Algorithm = "snappy"
	AlgorithmAuto   Algorithm = "auto"
	AlgorithmNone   Algorithm = "none"
)

// Config configuration for HTTP compression
type Config struct {
	DefaultAlgorithm Algorithm `yaml:"default_algorithm"`
	AdaptiveEnabled  bool      `yaml:"adaptive_enabled"`
	MinBytes         int       `yaml:"min_bytes"`
	Level            int       `yaml:"level"`
	PoolSize         int       `yaml:"pool_size"`

	// Algorithm-specific configurations
	Algorithms map[Algorithm]AlgorithmConfig `yaml:"algorithms"`

	// Per-sink compression settings
	PerSink map[string]SinkCompressionConfig `yaml:"per_sink"`
}

// AlgorithmConfig configuration for specific algorithms
type AlgorithmConfig struct {
	Enabled bool `yaml:"enabled"`
	Level   int  `yaml:"level"`
	MinSize int  `yaml:"min_size"`
}

// SinkCompressionConfig compression configuration per sink
type SinkCompressionConfig struct {
	Algorithm Algorithm `yaml:"algorithm"`
	Enabled   bool      `yaml:"enabled"`
	Level     int       `yaml:"level"`
}

// HTTPCompressor handles HTTP compression for different algorithms
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

// compressionPool manages reusable compression writers
type compressionPool struct {
	gzipPool   sync.Pool
	zlibPool   sync.Pool
	zstdPool   sync.Pool
	lz4Pool    sync.Pool
	snappyPool sync.Pool
}

// CompressionResult contains the result of compression
type CompressionResult struct {
	Data           []byte
	Algorithm      Algorithm
	OriginalSize   int
	CompressedSize int
	Ratio          float64
	ContentType    string
	Encoding       string
}

// NewHTTPCompressor creates a new HTTP compressor
func NewHTTPCompressor(config Config, logger *logrus.Logger) *HTTPCompressor {
	// Set defaults
	if config.DefaultAlgorithm == "" {
		config.DefaultAlgorithm = AlgorithmGzip
	}
	if config.MinBytes == 0 {
		config.MinBytes = 1024 // 1KB
	}
	if config.Level == 0 {
		config.Level = 6 // Default compression level
	}
	if config.PoolSize == 0 {
		config.PoolSize = 10
	}

	// Set default algorithm configs
	if config.Algorithms == nil {
		config.Algorithms = make(map[Algorithm]AlgorithmConfig)
	}

	defaultAlgorithms := map[Algorithm]AlgorithmConfig{
		AlgorithmGzip:   {Enabled: true, Level: 6, MinSize: 1024},
		AlgorithmZlib:   {Enabled: true, Level: 6, MinSize: 1024},
		AlgorithmZstd:   {Enabled: true, Level: 3, MinSize: 1024},
		AlgorithmLZ4:    {Enabled: true, Level: 1, MinSize: 1024},
		AlgorithmSnappy: {Enabled: true, Level: 0, MinSize: 1024},
	}

	for alg, cfg := range defaultAlgorithms {
		if _, exists := config.Algorithms[alg]; !exists {
			config.Algorithms[alg] = cfg
		}
	}

	compressor := &HTTPCompressor{
		config: config,
		logger: logger,
		pools:  make(map[Algorithm]*compressionPool),
	}

	// Initialize compression pools
	compressor.initializePools()

	// Initialize metrics
	compressor.initMetrics()

	return compressor
}

// initializePools initializes compression writer pools
func (hc *HTTPCompressor) initializePools() {
	for algorithm := range hc.config.Algorithms {
		pool := &compressionPool{}

		switch algorithm {
		case AlgorithmGzip:
			pool.gzipPool = sync.Pool{
				New: func() interface{} {
					w, _ := gzip.NewWriterLevel(nil, hc.config.Algorithms[algorithm].Level)
					return w
				},
			}

		case AlgorithmZlib:
			pool.zlibPool = sync.Pool{
				New: func() interface{} {
					w, _ := zlib.NewWriterLevel(nil, hc.config.Algorithms[algorithm].Level)
					return w
				},
			}

		case AlgorithmZstd:
			pool.zstdPool = sync.Pool{
				New: func() interface{} {
					w, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(hc.config.Algorithms[algorithm].Level)))
					return w
				},
			}

		case AlgorithmLZ4:
			pool.lz4Pool = sync.Pool{
				New: func() interface{} {
					return lz4.NewWriter(nil)
				},
			}

		case AlgorithmSnappy:
			// Snappy doesn't need a pool as it's stateless
		}

		hc.pools[algorithm] = pool
	}
}

// Compress compresses data using the specified algorithm or auto-selection
func (hc *HTTPCompressor) Compress(data []byte, algorithm Algorithm, sinkType string) (*CompressionResult, error) {
	if len(data) < hc.config.MinBytes {
		return &CompressionResult{
			Data:           data,
			Algorithm:      AlgorithmNone,
			OriginalSize:   len(data),
			CompressedSize: len(data),
			Ratio:          1.0,
			ContentType:    "application/json",
			Encoding:       "",
		}, nil
	}

	// Check sink-specific configuration
	if sinkConfig, exists := hc.config.PerSink[sinkType]; exists {
		if !sinkConfig.Enabled {
			return &CompressionResult{
				Data:           data,
				Algorithm:      AlgorithmNone,
				OriginalSize:   len(data),
				CompressedSize: len(data),
				Ratio:          1.0,
				ContentType:    "application/json",
				Encoding:       "",
			}, nil
		}
		algorithm = sinkConfig.Algorithm
	}

	// Auto-select algorithm if needed
	if algorithm == AlgorithmAuto {
		algorithm = hc.selectOptimalAlgorithm(data)
	}

	// Use default if not specified
	if algorithm == "" {
		algorithm = hc.config.DefaultAlgorithm
	}

	// Check if algorithm is enabled
	if algConfig, exists := hc.config.Algorithms[algorithm]; !exists || !algConfig.Enabled {
		return &CompressionResult{
			Data:           data,
			Algorithm:      AlgorithmNone,
			OriginalSize:   len(data),
			CompressedSize: len(data),
			Ratio:          1.0,
			ContentType:    "application/json",
			Encoding:       "",
		}, nil
	}

	// Perform compression
	compressedData, err := hc.compressWithAlgorithm(data, algorithm)
	if err != nil {
		if hc.compressionErrors != nil {
			hc.compressionErrors.WithLabelValues(string(algorithm)).Inc()
		}
		return nil, fmt.Errorf("compression failed with %s: %w", algorithm, err)
	}

	ratio := float64(len(compressedData)) / float64(len(data))

	// Update metrics (only if initialized)
	if hc.compressionRatio != nil {
		hc.compressionRatio.WithLabelValues(string(algorithm)).Observe(ratio)
	}
	if hc.algorithmsUsed != nil {
		hc.algorithmsUsed.WithLabelValues(string(algorithm)).Inc()
	}

	return &CompressionResult{
		Data:           compressedData,
		Algorithm:      algorithm,
		OriginalSize:   len(data),
		CompressedSize: len(compressedData),
		Ratio:          ratio,
		ContentType:    "application/json",
		Encoding:       hc.getContentEncoding(algorithm),
	}, nil
}

// selectOptimalAlgorithm selects the best compression algorithm based on data characteristics
func (hc *HTTPCompressor) selectOptimalAlgorithm(data []byte) Algorithm {
	dataSize := len(data)

	// For small data, use fast algorithms
	if dataSize < 4*1024 { // < 4KB
		return AlgorithmLZ4
	}

	// For medium data, balance compression and speed
	if dataSize < 64*1024 { // < 64KB
		return AlgorithmGzip
	}

	// For large data, prioritize compression ratio
	if dataSize < 1024*1024 { // < 1MB
		return AlgorithmZstd
	}

	// For very large data, use fastest
	return AlgorithmLZ4
}

// compressWithAlgorithm compresses data with the specified algorithm
func (hc *HTTPCompressor) compressWithAlgorithm(data []byte, algorithm Algorithm) ([]byte, error) {
	switch algorithm {
	case AlgorithmGzip:
		return hc.compressGzip(data)
	case AlgorithmZlib:
		return hc.compressZlib(data)
	case AlgorithmZstd:
		return hc.compressZstd(data)
	case AlgorithmLZ4:
		return hc.compressLZ4(data)
	case AlgorithmSnappy:
		return hc.compressSnappy(data)
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
}

// compressGzip compresses data using gzip
func (hc *HTTPCompressor) compressGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	pool := hc.pools[AlgorithmGzip]
	writer := pool.gzipPool.Get().(*gzip.Writer)
	defer pool.gzipPool.Put(writer)

	writer.Reset(&buf)
	defer writer.Close()

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// compressZlib compresses data using zlib
func (hc *HTTPCompressor) compressZlib(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	pool := hc.pools[AlgorithmZlib]
	writer := pool.zlibPool.Get().(*zlib.Writer)
	defer pool.zlibPool.Put(writer)

	writer.Reset(&buf)
	defer writer.Close()

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// compressZstd compresses data using zstd
func (hc *HTTPCompressor) compressZstd(data []byte) ([]byte, error) {
	pool := hc.pools[AlgorithmZstd]
	encoder := pool.zstdPool.Get().(*zstd.Encoder)
	defer pool.zstdPool.Put(encoder)

	return encoder.EncodeAll(data, make([]byte, 0, len(data))), nil
}

// compressLZ4 compresses data using LZ4
func (hc *HTTPCompressor) compressLZ4(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	pool := hc.pools[AlgorithmLZ4]
	writer := pool.lz4Pool.Get().(*lz4.Writer)
	defer pool.lz4Pool.Put(writer)

	writer.Reset(&buf)
	defer writer.Close()

	if _, err := writer.Write(data); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// compressSnappy compresses data using Snappy
func (hc *HTTPCompressor) compressSnappy(data []byte) ([]byte, error) {
	return snappy.Encode(nil, data), nil
}

// getContentEncoding returns the appropriate Content-Encoding header value
func (hc *HTTPCompressor) getContentEncoding(algorithm Algorithm) string {
	switch algorithm {
	case AlgorithmGzip:
		return "gzip"
	case AlgorithmZlib:
		return "deflate"
	case AlgorithmZstd:
		return "zstd"
	case AlgorithmLZ4:
		return "lz4"
	case AlgorithmSnappy:
		return "snappy"
	default:
		return ""
	}
}

// Decompress decompresses data using the specified algorithm
func (hc *HTTPCompressor) Decompress(data []byte, algorithm Algorithm) ([]byte, error) {
	switch algorithm {
	case AlgorithmGzip:
		return hc.decompressGzip(data)
	case AlgorithmZlib:
		return hc.decompressZlib(data)
	case AlgorithmZstd:
		return hc.decompressZstd(data)
	case AlgorithmLZ4:
		return hc.decompressLZ4(data)
	case AlgorithmSnappy:
		return hc.decompressSnappy(data)
	default:
		return nil, fmt.Errorf("unsupported decompression algorithm: %s", algorithm)
	}
}

// decompressGzip decompresses gzip data
func (hc *HTTPCompressor) decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// decompressZlib decompresses zlib data
func (hc *HTTPCompressor) decompressZlib(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// decompressZstd decompresses zstd data
func (hc *HTTPCompressor) decompressZstd(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	return decoder.DecodeAll(data, nil)
}

// decompressLZ4 decompresses LZ4 data
func (hc *HTTPCompressor) decompressLZ4(data []byte) ([]byte, error) {
	reader := lz4.NewReader(bytes.NewReader(data))
	return io.ReadAll(reader)
}

// decompressSnappy decompresses Snappy data
func (hc *HTTPCompressor) decompressSnappy(data []byte) ([]byte, error) {
	return snappy.Decode(nil, data)
}

// GetCompressionInfo returns information about available compression algorithms
func (hc *HTTPCompressor) GetCompressionInfo() map[string]interface{} {
	info := make(map[string]interface{})

	for algorithm, config := range hc.config.Algorithms {
		info[string(algorithm)] = map[string]interface{}{
			"enabled":  config.Enabled,
			"level":    config.Level,
			"min_size": config.MinSize,
		}
	}

	return map[string]interface{}{
		"default_algorithm":  string(hc.config.DefaultAlgorithm),
		"adaptive_enabled":   hc.config.AdaptiveEnabled,
		"min_bytes":          hc.config.MinBytes,
		"algorithms":         info,
		"per_sink_settings":  hc.config.PerSink,
	}
}

// initMetrics initializes Prometheus metrics
func (hc *HTTPCompressor) initMetrics() {
	// Temporarily disable metrics to fix registration issue
	// TODO: Re-enable when metrics registration is fixed
	return
}