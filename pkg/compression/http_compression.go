package compression

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Compressor interface for HTTP compression algorithms
type Compressor interface {
	Compress(data []byte) ([]byte, error)
	ContentEncoding() string
	MinSize() int
}

// HTTPCompressionManager manages HTTP compression
type HTTPCompressionManager struct {
	compressors map[string]Compressor
	mutex       sync.RWMutex // Protege o map compressors
	defaultAlgo string
	autoSelect  bool
}

// NewHTTPCompressionManager creates a new compression manager
func NewHTTPCompressionManager() *HTTPCompressionManager {
	manager := &HTTPCompressionManager{
		compressors: make(map[string]Compressor),
		defaultAlgo: "gzip",
		autoSelect:  true,
	}

	// Register default compressors
	manager.RegisterCompressor("gzip", &GzipCompressor{})
	manager.RegisterCompressor("zstd", &ZstdCompressor{})

	return manager
}

// RegisterCompressor registers a new compressor
func (hcm *HTTPCompressionManager) RegisterCompressor(name string, compressor Compressor) {
	hcm.mutex.Lock()
	defer hcm.mutex.Unlock()
	hcm.compressors[name] = compressor
}

// CompressRequest compresses HTTP request body
func (hcm *HTTPCompressionManager) CompressRequest(req *http.Request, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// Select best compressor
	algo := hcm.selectBestCompressor(data, req.Header.Get("Accept-Encoding"))

	hcm.mutex.RLock()
	compressor, exists := hcm.compressors[algo]
	hcm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("compressor %s not found", algo)
	}

	// Check minimum size threshold
	if len(data) < compressor.MinSize() {
		return nil // Don't compress small payloads
	}

	// Compress data
	compressed, err := compressor.Compress(data)
	if err != nil {
		return fmt.Errorf("compression failed: %w", err)
	}

	// Only use compression if it actually reduces size
	if len(compressed) >= len(data) {
		return nil
	}

	// Update request
	req.Body = io.NopCloser(bytes.NewReader(compressed))
	req.ContentLength = int64(len(compressed))
	req.Header.Set("Content-Encoding", compressor.ContentEncoding())
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(compressed)))

	return nil
}

// selectBestCompressor selects the best compressor based on data size and server support
func (hcm *HTTPCompressionManager) selectBestCompressor(data []byte, acceptEncoding string) string {
	if !hcm.autoSelect {
		return hcm.defaultAlgo
	}

	// Parse Accept-Encoding header to see what server supports
	supportedAlgorithms := parseAcceptEncoding(acceptEncoding)

	// Select best algorithm based on data characteristics and server support
	dataSize := len(data)

	// For small payloads, prefer gzip (lower CPU overhead)
	if dataSize < 1024 {
		if contains(supportedAlgorithms, "gzip") {
			return "gzip"
		}
	}

	// For larger payloads, prefer zstd if supported (better compression)
	if dataSize >= 1024 {
		if contains(supportedAlgorithms, "zstd") {
			return "zstd"
		}
		if contains(supportedAlgorithms, "gzip") {
			return "gzip"
		}
	}

	// Fallback to default
	return hcm.defaultAlgo
}

// parseAcceptEncoding parses Accept-Encoding header
func parseAcceptEncoding(acceptEncoding string) []string {
	if acceptEncoding == "" {
		return []string{}
	}

	var algorithms []string
	parts := bytes.Split([]byte(acceptEncoding), []byte(","))
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		// Extract algorithm name (ignore quality values)
		if idx := bytes.Index(part, []byte(";")); idx != -1 {
			part = part[:idx]
		}
		algorithms = append(algorithms, string(bytes.TrimSpace(part)))
	}

	return algorithms
}

// contains checks if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GzipCompressor implements gzip compression
type GzipCompressor struct{}

func (g *GzipCompressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, fmt.Errorf("gzip write failed: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("gzip close failed: %w", err)
	}

	return buf.Bytes(), nil
}

func (g *GzipCompressor) ContentEncoding() string {
	return "gzip"
}

func (g *GzipCompressor) MinSize() int {
	return 256 // Don't compress payloads smaller than 256 bytes
}

// ZstdCompressor implements zstd compression
type ZstdCompressor struct {
	encoder *zstd.Encoder
}

func (z *ZstdCompressor) Compress(data []byte) ([]byte, error) {
	if z.encoder == nil {
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
		}
		z.encoder = encoder
	}

	compressed := z.encoder.EncodeAll(data, nil)
	return compressed, nil
}

func (z *ZstdCompressor) ContentEncoding() string {
	return "zstd"
}

func (z *ZstdCompressor) MinSize() int {
	return 512 // zstd works better with larger payloads
}

// SetDefaultAlgorithm sets the default compression algorithm
func (hcm *HTTPCompressionManager) SetDefaultAlgorithm(algo string) {
	hcm.defaultAlgo = algo
}

// SetAutoSelect enables or disables automatic algorithm selection
func (hcm *HTTPCompressionManager) SetAutoSelect(enabled bool) {
	hcm.autoSelect = enabled
}