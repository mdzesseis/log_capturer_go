package buffer

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// DiskBuffer implements a persistent disk-based buffer for log entries
type DiskBuffer struct {
	config       DiskBufferConfig
	logger       *logrus.Logger

	// File management
	baseDir      string
	currentFile  *os.File
	writer       *bufio.Writer
	gzipWriter   *gzip.Writer
	currentSize  int64
	fileIndex    int

	// Recovery state
	recoveryFiles []string
	recoveryIndex int

	// Statistics
	stats      BufferStats
	statsMutex sync.RWMutex

	// Control
	mutex     sync.Mutex
	isRunning bool
}

// DiskBufferConfig configuration for disk buffer
type DiskBufferConfig struct {
	BaseDir           string        `yaml:"base_dir"`
	MaxFileSize       int64         `yaml:"max_file_size"`       // bytes
	MaxTotalSize      int64         `yaml:"max_total_size"`      // bytes
	MaxFiles          int           `yaml:"max_files"`
	CompressionEnabled bool         `yaml:"compression_enabled"`
	SyncInterval      time.Duration `yaml:"sync_interval"`
	CleanupInterval   time.Duration `yaml:"cleanup_interval"`
	RetentionPeriod   time.Duration `yaml:"retention_period"`
	FilePermissions   os.FileMode   `yaml:"file_permissions"`
	DirPermissions    os.FileMode   `yaml:"dir_permissions"`
}

// BufferStats statistics for disk buffer
type BufferStats struct {
	TotalWrites       int64 `json:"total_writes"`
	TotalReads        int64 `json:"total_reads"`
	TotalBytes        int64 `json:"total_bytes"`
	CurrentFiles      int   `json:"current_files"`
	CurrentSize       int64 `json:"current_size"`
	CompressionRatio  float64 `json:"compression_ratio"`
	RecoveryFiles     int   `json:"recovery_files"`
	LastRotation      int64 `json:"last_rotation"`
	LastCleanup       int64 `json:"last_cleanup"`
}

// BufferEntry represents a single entry in the disk buffer
// P1 FIX: Entry é ponteiro para evitar pass lock by value
type BufferEntry struct {
	Timestamp  time.Time        `json:"timestamp"`
	Entry      *types.LogEntry  `json:"entry"`
	Checksum   [32]byte         `json:"checksum"`
}

// NewDiskBuffer creates a new disk buffer
func NewDiskBuffer(config DiskBufferConfig, logger *logrus.Logger) (*DiskBuffer, error) {
	// Set defaults
	if config.BaseDir == "" {
		config.BaseDir = "/tmp/disk_buffer"
	}
	if config.MaxFileSize <= 0 {
		config.MaxFileSize = 100 * 1024 * 1024 // 100MB
	}
	if config.MaxTotalSize <= 0 {
		config.MaxTotalSize = 1024 * 1024 * 1024 // 1GB
	}
	if config.MaxFiles <= 0 {
		config.MaxFiles = 50
	}
	if config.SyncInterval == 0 {
		config.SyncInterval = 5 * time.Second
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 1 * time.Hour
	}
	if config.RetentionPeriod == 0 {
		config.RetentionPeriod = 24 * time.Hour
	}
	if config.FilePermissions == 0 {
		config.FilePermissions = 0644
	}
	if config.DirPermissions == 0 {
		config.DirPermissions = 0755
	}

	// Create base directory
	if err := os.MkdirAll(config.BaseDir, config.DirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create buffer directory %s: %w", config.BaseDir, err)
	}

	db := &DiskBuffer{
		config:  config,
		logger:  logger,
		baseDir: config.BaseDir,
	}

	// Initialize buffer
	if err := db.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize disk buffer: %w", err)
	}

	return db, nil
}

// initialize initializes the disk buffer
func (db *DiskBuffer) initialize() error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	// Scan for existing files
	if err := db.scanExistingFiles(); err != nil {
		return fmt.Errorf("failed to scan existing files: %w", err)
	}

	// Create new write file
	if err := db.rotateFile(); err != nil {
		return fmt.Errorf("failed to create initial write file: %w", err)
	}

	db.isRunning = true

	// Start background tasks
	go db.syncLoop()
	go db.cleanupLoop()

	db.logger.WithFields(logrus.Fields{
		"base_dir":      db.baseDir,
		"max_file_size": db.config.MaxFileSize,
		"compression":   db.config.CompressionEnabled,
	}).Info("Disk buffer initialized")

	return nil
}

// scanExistingFiles scans for existing buffer files for recovery
func (db *DiskBuffer) scanExistingFiles() error {
	files, err := filepath.Glob(filepath.Join(db.baseDir, "buffer_*.dat"))
	if err != nil {
		return err
	}

	// Sort files by creation time (newest first for recovery)
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	// Find highest file index
	maxIndex := -1
	for _, file := range files {
		var index int
		if _, err := fmt.Sscanf(filepath.Base(file), "buffer_%d.dat", &index); err == nil {
			if index > maxIndex {
				maxIndex = index
			}
		}
	}

	db.fileIndex = maxIndex + 1
	db.recoveryFiles = files
	db.stats.RecoveryFiles = len(files)

	return nil
}

// Write writes a log entry to the disk buffer
// P1 FIX: Recebe ponteiro para evitar pass lock by value
func (db *DiskBuffer) Write(entry *types.LogEntry) error {
	if !db.isRunning {
		return fmt.Errorf("disk buffer is not running")
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()

	// Create buffer entry with checksum (usando ponteiro)
	bufferEntry := BufferEntry{
		Timestamp: time.Now().UTC(),
		Entry:     entry,
	}

	// Calculate checksum
	entryData, err := json.Marshal(bufferEntry.Entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}
	bufferEntry.Checksum = sha256.Sum256(entryData)

	// Serialize entry
	data, err := json.Marshal(bufferEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal buffer entry: %w", err)
	}

	// Write length prefix (4 bytes) + data
	lengthBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBuf, uint32(len(data)))

	// Write to buffer
	var writeErr error
	if db.config.CompressionEnabled && db.gzipWriter != nil {
		_, err1 := db.gzipWriter.Write(lengthBuf)
		_, err2 := db.gzipWriter.Write(data)
		writeErr = firstError(err1, err2)
	} else {
		_, err1 := db.writer.Write(lengthBuf)
		_, err2 := db.writer.Write(data)
		writeErr = firstError(err1, err2)
	}

	if writeErr != nil {
		return fmt.Errorf("failed to write to buffer: %w", writeErr)
	}

	// Update size and stats
	entrySize := int64(len(lengthBuf) + len(data))
	db.currentSize += entrySize
	db.stats.TotalWrites++
	db.stats.TotalBytes += entrySize

	// Check if we need to rotate
	if db.currentSize >= db.config.MaxFileSize {
		if err := db.rotateFile(); err != nil {
			db.logger.WithError(err).Error("Failed to rotate buffer file")
		}
	}

	return nil
}

// ReadAll reads all entries from the disk buffer (for recovery)
// P1 FIX: Retorna ponteiros para evitar pass lock by value
func (db *DiskBuffer) ReadAll(ctx context.Context) ([]*types.LogEntry, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	var allEntries []*types.LogEntry

	// Read from recovery files (oldest first)
	recoveryFiles := make([]string, len(db.recoveryFiles))
	copy(recoveryFiles, db.recoveryFiles)
	sort.Strings(recoveryFiles) // Sort oldest first

	for _, filename := range recoveryFiles {
		select {
		case <-ctx.Done():
			return allEntries, ctx.Err()
		default:
		}

		entries, err := db.readFromFile(filename)
		if err != nil {
			db.logger.WithError(err).WithField("file", filename).Error("Failed to read from recovery file")
			continue
		}

		allEntries = append(allEntries, entries...)
		db.stats.TotalReads += int64(len(entries))
	}

	db.logger.WithField("entries_recovered", len(allEntries)).Info("Recovered entries from disk buffer")
	return allEntries, nil
}

// readFromFile reads entries from a specific file
// P1 FIX: Retorna ponteiros para evitar pass lock by value
func (db *DiskBuffer) readFromFile(filename string) ([]*types.LogEntry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	var reader io.Reader = file

	// Check if compressed
	if db.config.CompressionEnabled && filepath.Ext(filename) == ".dat" {
		gzipReader, err := gzip.NewReader(file)
		if err == nil {
			defer gzipReader.Close()
			reader = gzipReader
		}
		// If gzip fails, fall back to uncompressed reading
	}

	var entries []*types.LogEntry
	bufReader := bufio.NewReader(reader)

	for {
		// Read length prefix
		lengthBuf := make([]byte, 4)
		if _, err := io.ReadFull(bufReader, lengthBuf); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read length: %w", err)
		}

		length := binary.LittleEndian.Uint32(lengthBuf)
		if length > 10*1024*1024 { // Sanity check: 10MB max per entry
			return nil, fmt.Errorf("invalid entry length: %d", length)
		}

		// Read data
		data := make([]byte, length)
		if _, err := io.ReadFull(bufReader, data); err != nil {
			return nil, fmt.Errorf("failed to read data: %w", err)
		}

		// Unmarshal buffer entry
		var bufferEntry BufferEntry
		if err := json.Unmarshal(data, &bufferEntry); err != nil {
			db.logger.WithError(err).Warn("Failed to unmarshal buffer entry, skipping")
			continue
		}

		// Verify checksum
		entryData, err := json.Marshal(bufferEntry.Entry)
		if err == nil {
			expectedChecksum := sha256.Sum256(entryData)
			if expectedChecksum != bufferEntry.Checksum {
				db.logger.Warn("Checksum mismatch in buffer entry, skipping")
				continue
			}
		}

		entries = append(entries, bufferEntry.Entry)
	}

	return entries, nil
}

// rotateFile rotates to a new buffer file
func (db *DiskBuffer) rotateFile() error {
	// Close current file
	if err := db.closeCurrentFile(); err != nil {
		return err
	}

	// Create new file
	filename := filepath.Join(db.baseDir, fmt.Sprintf("buffer_%06d.dat", db.fileIndex))
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, db.config.FilePermissions)
	if err != nil {
		return fmt.Errorf("failed to create buffer file %s: %w", filename, err)
	}

	db.currentFile = file
	db.writer = bufio.NewWriter(file)
	db.currentSize = 0
	db.fileIndex++

	// Setup compression if enabled
	if db.config.CompressionEnabled {
		db.gzipWriter = gzip.NewWriter(db.writer)
	}

	// Update stats
	db.stats.CurrentFiles++
	db.stats.LastRotation = time.Now().Unix()

	return nil
}

// closeCurrentFile closes the current write file
func (db *DiskBuffer) closeCurrentFile() error {
	var lastErr error

	if db.gzipWriter != nil {
		if err := db.gzipWriter.Close(); err != nil {
			lastErr = err
		}
		db.gzipWriter = nil
	}

	if db.writer != nil {
		if err := db.writer.Flush(); err != nil && lastErr == nil {
			lastErr = err
		}
		db.writer = nil
	}

	if db.currentFile != nil {
		if err := db.currentFile.Sync(); err != nil && lastErr == nil {
			lastErr = err
		}
		if err := db.currentFile.Close(); err != nil && lastErr == nil {
			lastErr = err
		}
		db.currentFile = nil
	}

	return lastErr
}

// syncLoop periodically syncs the buffer to disk
func (db *DiskBuffer) syncLoop() {
	ticker := time.NewTicker(db.config.SyncInterval)
	defer ticker.Stop()

	for range ticker.C {
		if !db.isRunning {
			break
		}

		db.mutex.Lock()
		if db.writer != nil {
			db.writer.Flush()
		}
		if db.gzipWriter != nil {
			db.gzipWriter.Flush()
		}
		if db.currentFile != nil {
			db.currentFile.Sync()
		}
		db.mutex.Unlock()
	}
}

// cleanupLoop periodically cleans up old buffer files
func (db *DiskBuffer) cleanupLoop() {
	ticker := time.NewTicker(db.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		if !db.isRunning {
			break
		}

		db.performCleanup()
	}
}

// performCleanup removes old buffer files
func (db *DiskBuffer) performCleanup() {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	files, err := filepath.Glob(filepath.Join(db.baseDir, "buffer_*.dat"))
	if err != nil {
		db.logger.WithError(err).Error("Failed to list buffer files for cleanup")
		return
	}

	now := time.Now()
	var totalSize int64
	var removedFiles int

	// Sort files by modification time (oldest first)
	type fileInfo struct {
		path    string
		modTime time.Time
		size    int64
	}

	var fileInfos []fileInfo
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{
			path:    file,
			modTime: info.ModTime(),
			size:    info.Size(),
		})
		totalSize += info.Size()
	}

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].modTime.Before(fileInfos[j].modTime)
	})

	// Remove files based on age and size limits
	for _, fileInfo := range fileInfos {
		shouldRemove := false

		// Remove if too old
		if now.Sub(fileInfo.modTime) > db.config.RetentionPeriod {
			shouldRemove = true
		}

		// Remove if total size exceeds limit
		if totalSize > db.config.MaxTotalSize {
			shouldRemove = true
		}

		// Remove if too many files
		if len(fileInfos)-removedFiles > db.config.MaxFiles {
			shouldRemove = true
		}

		if shouldRemove {
			if err := os.Remove(fileInfo.path); err != nil {
				db.logger.WithError(err).WithField("file", fileInfo.path).Error("Failed to remove old buffer file")
			} else {
				removedFiles++
				totalSize -= fileInfo.size
			}
		}
	}

	if removedFiles > 0 {
		db.logger.WithField("removed_files", removedFiles).Info("Cleaned up old buffer files")
	}

	db.stats.LastCleanup = now.Unix()
	db.stats.CurrentFiles = len(fileInfos) - removedFiles
	db.stats.CurrentSize = totalSize
}

// Clear removes all buffer files
func (db *DiskBuffer) Clear() error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	// Close current file
	db.closeCurrentFile()

	// Remove all buffer files
	files, err := filepath.Glob(filepath.Join(db.baseDir, "buffer_*.dat"))
	if err != nil {
		return err
	}

	var lastErr error
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			lastErr = err
		}
	}

	// Reset state
	db.recoveryFiles = nil
	db.fileIndex = 0
	db.stats = BufferStats{}

	// Create new file
	if err := db.rotateFile(); err != nil {
		return err
	}

	return lastErr
}

// GetStats returns current buffer statistics
func (db *DiskBuffer) GetStats() BufferStats {
	db.statsMutex.RLock()
	defer db.statsMutex.RUnlock()

	stats := db.stats
	if db.config.CompressionEnabled && stats.TotalBytes > 0 {
		// Calculate compression ratio (simplified)
		stats.CompressionRatio = 0.6 // Estimate ~40% compression
	}

	return stats
}

// Close closes the disk buffer
func (db *DiskBuffer) Close() error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	db.isRunning = false
	return db.closeCurrentFile()
}

// Helper function to get first non-nil error
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}