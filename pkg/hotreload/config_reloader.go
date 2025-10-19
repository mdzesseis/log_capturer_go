package hotreload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ssw-logs-capture/internal/config"
	"ssw-logs-capture/pkg/types"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

// ConfigReloader gerencia o reload automático de configurações
type ConfigReloader struct {
	config       Config
	logger       *logrus.Logger
	configFile   string
	currentHash  string
	lastModTime  time.Time

	// File watcher
	watcher    *fsnotify.Watcher
	watchedFiles map[string]bool

	// Callbacks
	onConfigChanged func(*types.Config, *types.Config) error
	onReloadSuccess func(*types.Config)
	onReloadError   func(error)

	// Current config
	currentConfig atomic.Value // *types.Config
	configMux     sync.RWMutex

	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  atomic.Bool

	// Stats
	stats Stats
}

// Config configuração do hot reload
type Config struct {
	Enabled           bool          `yaml:"enabled"`
	WatchInterval     time.Duration `yaml:"watch_interval"`
	DebounceInterval  time.Duration `yaml:"debounce_interval"`
	WatchFiles        []string      `yaml:"watch_files"`
	ValidateOnReload  bool          `yaml:"validate_on_reload"`
	BackupOnReload    bool          `yaml:"backup_on_reload"`
	BackupDirectory   string        `yaml:"backup_directory"`
	MaxBackups        int           `yaml:"max_backups"`
	NotifyWebhook     string        `yaml:"notify_webhook"`
	FailsafeMode      bool          `yaml:"failsafe_mode"`
}

// Stats estatísticas do config reloader
type Stats struct {
	TotalReloads      int64     `json:"total_reloads"`
	SuccessfulReloads int64     `json:"successful_reloads"`
	FailedReloads     int64     `json:"failed_reloads"`
	LastReloadTime    time.Time `json:"last_reload_time"`
	LastSuccessTime   time.Time `json:"last_success_time"`
	LastError         string    `json:"last_error,omitempty"`
	ConfigVersion     string    `json:"config_version"`
	FilesWatched      int       `json:"files_watched"`
	IsWatching        bool      `json:"is_watching"`
}

// ReloadEvent representa um evento de reload
type ReloadEvent struct {
	Timestamp    time.Time     `json:"timestamp"`
	ConfigFile   string        `json:"config_file"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	OldHash      string        `json:"old_hash"`
	NewHash      string        `json:"new_hash"`
	ReloadTime   time.Duration `json:"reload_time"`
	ChangedFiles []string      `json:"changed_files"`
}

// NewConfigReloader cria uma nova instância do config reloader
func NewConfigReloader(config Config, configFile string, logger *logrus.Logger) (*ConfigReloader, error) {
	if !config.Enabled {
		return &ConfigReloader{
			config:     config,
			logger:     logger,
			configFile: configFile,
		}, nil
	}

	// Create file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Set defaults
	if config.WatchInterval == 0 {
		config.WatchInterval = 5 * time.Second
	}
	if config.DebounceInterval == 0 {
		config.DebounceInterval = 1 * time.Second
	}
	if config.MaxBackups == 0 {
		config.MaxBackups = 5
	}

	ctx, cancel := context.WithCancel(context.Background())

	cr := &ConfigReloader{
		config:       config,
		logger:       logger,
		configFile:   configFile,
		watcher:      watcher,
		watchedFiles: make(map[string]bool),
		ctx:          ctx,
		cancel:       cancel,
		stats: Stats{
			FilesWatched: 0,
			IsWatching:   false,
		},
	}

	// Load initial config hash
	if err := cr.updateConfigHash(); err != nil {
		logger.WithError(err).Warn("Failed to calculate initial config hash")
	}

	return cr, nil
}

// SetCallbacks define callbacks para eventos de reload
func (cr *ConfigReloader) SetCallbacks(
	onChanged func(*types.Config, *types.Config) error,
	onSuccess func(*types.Config),
	onError func(error),
) {
	cr.onConfigChanged = onChanged
	cr.onReloadSuccess = onSuccess
	cr.onReloadError = onError
}

// Start inicia o config reloader
func (cr *ConfigReloader) Start() error {
	if !cr.config.Enabled {
		cr.logger.Info("Config reloader disabled")
		return nil
	}

	if cr.running.Load() {
		return fmt.Errorf("config reloader already running")
	}

	cr.logger.Info("Starting config reloader")

	// Load initial configuration
	if err := cr.loadInitialConfig(); err != nil {
		return fmt.Errorf("failed to load initial config: %w", err)
	}

	// Setup file watching
	if err := cr.setupFileWatching(); err != nil {
		return fmt.Errorf("failed to setup file watching: %w", err)
	}

	// Start watching goroutines
	cr.wg.Add(2)
	go cr.watchFileChanges()
	go cr.periodicCheck()

	cr.running.Store(true)
	cr.stats.IsWatching = true

	cr.logger.WithFields(logrus.Fields{
		"config_file":    cr.configFile,
		"watch_interval": cr.config.WatchInterval,
		"files_watched":  len(cr.watchedFiles),
	}).Info("Config reloader started")

	return nil
}

// Stop para o config reloader
func (cr *ConfigReloader) Stop() error {
	if !cr.running.Load() {
		return nil
	}

	cr.logger.Info("Stopping config reloader")

	cr.running.Store(false)
	cr.stats.IsWatching = false

	cr.cancel()

	if cr.watcher != nil {
		cr.watcher.Close()
	}

	cr.wg.Wait()

	cr.logger.Info("Config reloader stopped")
	return nil
}

// loadInitialConfig carrega a configuração inicial
func (cr *ConfigReloader) loadInitialConfig() error {
	config, err := config.LoadConfig(cr.configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cr.currentConfig.Store(config)
	cr.stats.ConfigVersion = cr.currentHash

	cr.logger.WithField("config_version", cr.currentHash[:8]).Info("Initial config loaded")
	return nil
}

// setupFileWatching configura o watching de arquivos
func (cr *ConfigReloader) setupFileWatching() error {
	// Watch main config file
	if err := cr.addFileToWatch(cr.configFile); err != nil {
		return fmt.Errorf("failed to watch main config file: %w", err)
	}

	// Watch additional files
	for _, file := range cr.config.WatchFiles {
		if err := cr.addFileToWatch(file); err != nil {
			cr.logger.WithError(err).WithField("file", file).Warn("Failed to watch additional file")
		}
	}

	// Watch config directory
	configDir := filepath.Dir(cr.configFile)
	if err := cr.watcher.Add(configDir); err != nil {
		cr.logger.WithError(err).WithField("directory", configDir).Warn("Failed to watch config directory")
	}

	return nil
}

// addFileToWatch adiciona um arquivo para watching
func (cr *ConfigReloader) addFileToWatch(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if cr.watchedFiles[absPath] {
		return nil // Already watching
	}

	if err := cr.watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to add file to watcher: %w", err)
	}

	cr.watchedFiles[absPath] = true
	cr.stats.FilesWatched = len(cr.watchedFiles)

	cr.logger.WithField("file", absPath).Debug("Added file to watch")
	return nil
}

// watchFileChanges monitora mudanças nos arquivos
func (cr *ConfigReloader) watchFileChanges() {
	defer cr.wg.Done()

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	pendingReload := false

	for {
		select {
		case <-cr.ctx.Done():
			return

		case event, ok := <-cr.watcher.Events:
			if !ok {
				return
			}

			if cr.shouldProcessEvent(event) {
				cr.logger.WithFields(logrus.Fields{
					"file":      event.Name,
					"operation": event.Op.String(),
				}).Debug("Config file change detected")

				// Debounce: reset timer
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer.Reset(cr.config.DebounceInterval)
				pendingReload = true
			}

		case err, ok := <-cr.watcher.Errors:
			if !ok {
				return
			}
			cr.logger.WithError(err).Error("File watcher error")

		case <-debounceTimer.C:
			if pendingReload {
				pendingReload = false
				if err := cr.performReload(); err != nil {
					cr.logger.WithError(err).Error("Config reload failed")
				}
			}
		}
	}
}

// periodicCheck executa verificação periódica
func (cr *ConfigReloader) periodicCheck() {
	defer cr.wg.Done()

	ticker := time.NewTicker(cr.config.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cr.ctx.Done():
			return

		case <-ticker.C:
			if err := cr.checkForChanges(); err != nil {
				cr.logger.WithError(err).Error("Periodic config check failed")
			}
		}
	}
}

// shouldProcessEvent verifica se um evento deve ser processado
func (cr *ConfigReloader) shouldProcessEvent(event fsnotify.Event) bool {
	// Check if it's a relevant operation
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return false
	}

	// Check if it's one of our watched files
	absPath, err := filepath.Abs(event.Name)
	if err != nil {
		return false
	}

	// Main config file
	if absPath == cr.configFile {
		return true
	}

	// Additional watched files
	if cr.watchedFiles[absPath] {
		return true
	}

	// Files in config directory with relevant extensions
	if filepath.Dir(absPath) == filepath.Dir(cr.configFile) {
		ext := filepath.Ext(absPath)
		return ext == ".yaml" || ext == ".yml" || ext == ".json"
	}

	return false
}

// checkForChanges verifica mudanças na configuração
func (cr *ConfigReloader) checkForChanges() error {
	newHash, err := cr.calculateConfigHash()
	if err != nil {
		return fmt.Errorf("failed to calculate config hash: %w", err)
	}

	if newHash != cr.currentHash {
		cr.logger.WithFields(logrus.Fields{
			"old_hash": cr.currentHash[:8],
			"new_hash": newHash[:8],
		}).Info("Config change detected via hash comparison")

		return cr.performReload()
	}

	return nil
}

// performReload executa o reload da configuração
func (cr *ConfigReloader) performReload() error {
	startTime := time.Now()
	cr.stats.TotalReloads++
	cr.stats.LastReloadTime = startTime

	event := &ReloadEvent{
		Timestamp:  startTime,
		ConfigFile: cr.configFile,
		OldHash:    cr.currentHash,
	}

	cr.logger.Info("Performing config reload")

	// Backup current config if enabled
	if cr.config.BackupOnReload {
		if err := cr.backupCurrentConfig(); err != nil {
			cr.logger.WithError(err).Warn("Failed to backup current config")
		}
	}

	// Load new configuration
	newConfig, err := config.LoadConfig(cr.configFile)
	if err != nil {
		cr.stats.FailedReloads++
		event.Success = false
		event.Error = err.Error()
		event.ReloadTime = time.Since(startTime)
		cr.stats.LastError = err.Error()

		if cr.onReloadError != nil {
			cr.onReloadError(err)
		}

		return fmt.Errorf("failed to load new config: %w", err)
	}

	// Validate new configuration if enabled
	if cr.config.ValidateOnReload {
		if err := config.ValidateConfig(newConfig); err != nil {
			cr.stats.FailedReloads++
			event.Success = false
			event.Error = fmt.Sprintf("validation failed: %v", err)
			event.ReloadTime = time.Since(startTime)
			cr.stats.LastError = event.Error

			if cr.onReloadError != nil {
				cr.onReloadError(fmt.Errorf("config validation failed: %w", err))
			}

			return fmt.Errorf("new config validation failed: %w", err)
		}
	}

	// Get current config for comparison
	var oldConfig *types.Config
	if current := cr.currentConfig.Load(); current != nil {
		oldConfig = current.(*types.Config)
	}

	// Apply configuration changes if callback is set
	if cr.onConfigChanged != nil {
		if err := cr.onConfigChanged(oldConfig, newConfig); err != nil {
			cr.stats.FailedReloads++
			event.Success = false
			event.Error = fmt.Sprintf("apply changes failed: %v", err)
			event.ReloadTime = time.Since(startTime)
			cr.stats.LastError = event.Error

			if cr.onReloadError != nil {
				cr.onReloadError(fmt.Errorf("failed to apply config changes: %w", err))
			}

			// In failsafe mode, don't fail completely
			if cr.config.FailsafeMode {
				cr.logger.WithError(err).Warn("Config apply failed, but continuing in failsafe mode")
			} else {
				return fmt.Errorf("failed to apply config changes: %w", err)
			}
		}
	}

	// Update current config
	cr.currentConfig.Store(newConfig)

	// Update hash
	if err := cr.updateConfigHash(); err != nil {
		cr.logger.WithError(err).Warn("Failed to update config hash")
	}

	// Update stats
	cr.stats.SuccessfulReloads++
	cr.stats.LastSuccessTime = time.Now()
	cr.stats.ConfigVersion = cr.currentHash
	cr.stats.LastError = ""

	// Complete event
	event.Success = true
	event.NewHash = cr.currentHash
	event.ReloadTime = time.Since(startTime)

	// Notify success
	if cr.onReloadSuccess != nil {
		cr.onReloadSuccess(newConfig)
	}

	cr.logger.WithFields(logrus.Fields{
		"reload_time":    event.ReloadTime,
		"config_version": cr.currentHash[:8],
	}).Info("Config reload completed successfully")

	return nil
}

// calculateConfigHash calcula o hash da configuração atual
func (cr *ConfigReloader) calculateConfigHash() (string, error) {
	file, err := os.Open(cr.configFile)
	if err != nil {
		return "", fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// updateConfigHash atualiza o hash da configuração atual
func (cr *ConfigReloader) updateConfigHash() error {
	hash, err := cr.calculateConfigHash()
	if err != nil {
		return err
	}

	cr.currentHash = hash

	// Update last modification time
	if stat, err := os.Stat(cr.configFile); err == nil {
		cr.lastModTime = stat.ModTime()
	}

	return nil
}

// backupCurrentConfig faz backup da configuração atual
func (cr *ConfigReloader) backupCurrentConfig() error {
	if cr.config.BackupDirectory == "" {
		return nil
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(cr.config.BackupDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("config_%s.yaml", timestamp)
	backupPath := filepath.Join(cr.config.BackupDirectory, backupName)

	// Copy current config to backup
	src, err := os.Open(cr.configFile)
	if err != nil {
		return fmt.Errorf("failed to open source config: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy config to backup: %w", err)
	}

	cr.logger.WithField("backup_file", backupPath).Info("Config backed up")

	// Cleanup old backups
	return cr.cleanupOldBackups()
}

// cleanupOldBackups remove backups antigos
func (cr *ConfigReloader) cleanupOldBackups() error {
	if cr.config.BackupDirectory == "" || cr.config.MaxBackups <= 0 {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(cr.config.BackupDirectory, "config_*.yaml"))
	if err != nil {
		return fmt.Errorf("failed to list backup files: %w", err)
	}

	if len(files) <= cr.config.MaxBackups {
		return nil
	}

	// Sort files by modification time (oldest first)
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var fileInfos []fileInfo
	for _, file := range files {
		if stat, err := os.Stat(file); err == nil {
			fileInfos = append(fileInfos, fileInfo{
				path:    file,
				modTime: stat.ModTime(),
			})
		}
	}

	// Remove oldest files
	filesToRemove := len(fileInfos) - cr.config.MaxBackups
	for i := 0; i < filesToRemove; i++ {
		if err := os.Remove(fileInfos[i].path); err != nil {
			cr.logger.WithError(err).WithField("file", fileInfos[i].path).Warn("Failed to remove old backup")
		} else {
			cr.logger.WithField("file", fileInfos[i].path).Debug("Removed old backup")
		}
	}

	return nil
}

// GetCurrentConfig retorna a configuração atual
func (cr *ConfigReloader) GetCurrentConfig() *types.Config {
	if config := cr.currentConfig.Load(); config != nil {
		return config.(*types.Config)
	}
	return nil
}

// GetStats retorna as estatísticas atuais
func (cr *ConfigReloader) GetStats() Stats {
	return cr.stats
}

// IsHealthy verifica se o reloader está saudável
func (cr *ConfigReloader) IsHealthy() bool {
	if !cr.config.Enabled {
		return true
	}

	if !cr.running.Load() {
		return false
	}

	// Check if we have recent activity
	if time.Since(cr.stats.LastReloadTime) > cr.config.WatchInterval*5 {
		// No recent reloads, check if config file exists and is readable
		if _, err := os.Stat(cr.configFile); err != nil {
			return false
		}
	}

	return true
}

// TriggerReload força um reload imediato
func (cr *ConfigReloader) TriggerReload() error {
	if !cr.config.Enabled {
		return fmt.Errorf("config reloader is disabled")
	}

	if !cr.running.Load() {
		return fmt.Errorf("config reloader is not running")
	}

	cr.logger.Info("Manual config reload triggered")
	return cr.performReload()
}