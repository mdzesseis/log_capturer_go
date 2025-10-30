package selfguard

import (
	"os"
	"regexp"
	"strings"
	"sync"

	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// FeedbackGuard previne loops de feedback ao monitorar próprios logs
type FeedbackGuard struct {
	config Config
	logger *logrus.Logger

	selfIdentifiers   []string
	containerPatterns []*regexp.Regexp
	pathPatterns      []*regexp.Regexp
	messagePatterns   []*regexp.Regexp

	stats Stats
	mutex sync.RWMutex
}

// Config configuração do feedback guard
type Config struct {
	// Habilitar proteção
	Enabled bool `yaml:"enabled"`

	// ID curto para auto-identificação
	SelfIDShort string `yaml:"self_id_short"`

	// Nome do container atual
	SelfContainerName string `yaml:"self_container_name"`

	// Namespace/prefix dos containers próprios
	SelfNamespace string `yaml:"self_namespace"`

	// Padrões de path para excluir (próprios logs)
	ExcludePathPatterns []string `yaml:"exclude_path_patterns"`

	// Padrões de container para excluir
	ExcludeContainerPatterns []string `yaml:"exclude_container_patterns"`

	// Padrões de mensagem para excluir
	ExcludeMessagePatterns []string `yaml:"exclude_message_patterns"`

	// Auto-detectar container próprio
	AutoDetectSelf bool `yaml:"auto_detect_self"`

	// Ação para logs próprios: "drop", "tag", "warn"
	SelfLogAction string `yaml:"self_log_action"`

	// Tag para marcar logs próprios (se action = "tag")
	SelfLogTag string `yaml:"self_log_tag"`
}

// Stats estatísticas do feedback guard
type Stats struct {
	TotalChecked       int64 `json:"total_checked"`
	SelfLogsDetected   int64 `json:"self_logs_detected"`
	SelfLogsDropped    int64 `json:"self_logs_dropped"`
	SelfLogsTagged     int64 `json:"self_logs_tagged"`
	ContainerFiltered  int64 `json:"container_filtered"`
	PathFiltered       int64 `json:"path_filtered"`
	MessageFiltered    int64 `json:"message_filtered"`
}

// GuardResult resultado da verificação
type GuardResult struct {
	IsSelfLog    bool   `json:"is_self_log"`
	Action       string `json:"action"`        // "allow", "drop", "tag"
	Reason       string `json:"reason"`
	MatchPattern string `json:"match_pattern,omitempty"`
}

// NewFeedbackGuard cria novo feedback guard
func NewFeedbackGuard(config Config, logger *logrus.Logger) *FeedbackGuard {
	// Valores padrão
	if config.SelfLogAction == "" {
		config.SelfLogAction = "drop"
	}
	if config.SelfLogTag == "" {
		config.SelfLogTag = "self_log"
	}

	fg := &FeedbackGuard{
		config: config,
		logger: logger,
	}

	// Inicializar identificadores
	fg.initializeSelfIdentifiers()

	// Compilar padrões regex
	fg.compilePatterns()

	return fg
}

// initializeSelfIdentifiers inicializa identificadores próprios
func (fg *FeedbackGuard) initializeSelfIdentifiers() {
	fg.selfIdentifiers = []string{}

	// Adicionar ID curto se configurado
	if fg.config.SelfIDShort != "" {
		fg.selfIdentifiers = append(fg.selfIdentifiers, fg.config.SelfIDShort)
	}

	// Adicionar nome do container se configurado
	if fg.config.SelfContainerName != "" {
		fg.selfIdentifiers = append(fg.selfIdentifiers, fg.config.SelfContainerName)
	}

	// Auto-detectar se habilitado
	if fg.config.AutoDetectSelf {
		// Tentar detectar nome do container através de variáveis de ambiente
		if hostname := os.Getenv("HOSTNAME"); hostname != "" {
			fg.selfIdentifiers = append(fg.selfIdentifiers, hostname)
		}
		if containerName := os.Getenv("CONTAINER_NAME"); containerName != "" {
			fg.selfIdentifiers = append(fg.selfIdentifiers, containerName)
		}
		if podName := os.Getenv("POD_NAME"); podName != "" {
			fg.selfIdentifiers = append(fg.selfIdentifiers, podName)
		}
	}

	// Adicionar namespace se configurado
	if fg.config.SelfNamespace != "" {
		fg.selfIdentifiers = append(fg.selfIdentifiers, fg.config.SelfNamespace)
	}

	fg.logger.WithField("self_identifiers", fg.selfIdentifiers).Debug("Self identifiers initialized")
}

// compilePatterns compila padrões regex
func (fg *FeedbackGuard) compilePatterns() {
	// Compilar padrões de container
	for _, pattern := range fg.config.ExcludeContainerPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			fg.containerPatterns = append(fg.containerPatterns, compiled)
		} else {
			fg.logger.WithError(err).WithField("pattern", pattern).Warn("Failed to compile container pattern")
		}
	}

	// Compilar padrões de path
	for _, pattern := range fg.config.ExcludePathPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			fg.pathPatterns = append(fg.pathPatterns, compiled)
		} else {
			fg.logger.WithError(err).WithField("pattern", pattern).Warn("Failed to compile path pattern")
		}
	}

	// Compilar padrões de mensagem
	for _, pattern := range fg.config.ExcludeMessagePatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			fg.messagePatterns = append(fg.messagePatterns, compiled)
		} else {
			fg.logger.WithError(err).WithField("pattern", pattern).Warn("Failed to compile message pattern")
		}
	}
}

// CheckEntry verifica se uma entrada é própria (self-log)
func (fg *FeedbackGuard) CheckEntry(entry *types.LogEntry) *GuardResult {
	// Temporarily force allow all entries
	return &GuardResult{
		IsSelfLog: false,
		Action:    "allow",
		Reason:    "temporary_disabled",
	}

	if !fg.config.Enabled {
		return &GuardResult{
			IsSelfLog: false,
			Action:    "allow",
			Reason:    "guard_disabled",
		}
	}

	fg.mutex.Lock()
	fg.stats.TotalChecked++
	fg.mutex.Unlock()

	result := &GuardResult{
		IsSelfLog: false,
		Action:    "allow",
	}

	// 1. Verificar source_id contra identificadores próprios
	if fg.checkSelfIdentifiers(entry.SourceID) {
		result.IsSelfLog = true
		result.Reason = "self_source_id"
		result.MatchPattern = entry.SourceID
		return fg.handleSelfLog(result, "source_id")
	}

	// 2. Verificar labels do container (thread-safe)
	if containerName, exists := entry.GetLabel("container"); exists {
		if fg.checkSelfIdentifiers(containerName) {
			result.IsSelfLog = true
			result.Reason = "self_container_label"
			result.MatchPattern = containerName
			return fg.handleSelfLog(result, "container")
		}

		// Verificar padrões de container
		for _, pattern := range fg.containerPatterns {
			if pattern.MatchString(containerName) {
				result.IsSelfLog = true
				result.Reason = "container_pattern_match"
				result.MatchPattern = pattern.String()
				return fg.handleSelfLog(result, "container")
			}
		}
	}

	// 3. Verificar labels de path/source (thread-safe)
	if sourcePath, exists := entry.GetLabel("source"); exists {
		for _, pattern := range fg.pathPatterns {
			if pattern.MatchString(sourcePath) {
				result.IsSelfLog = true
				result.Reason = "path_pattern_match"
				result.MatchPattern = pattern.String()
				return fg.handleSelfLog(result, "path")
			}
		}
	}

	// 4. Verificar conteúdo da mensagem
	for _, pattern := range fg.messagePatterns {
		if pattern.MatchString(entry.Message) {
			result.IsSelfLog = true
			result.Reason = "message_pattern_match"
			result.MatchPattern = pattern.String()
			return fg.handleSelfLog(result, "message")
		}
	}

	// 5. Verificar se é log do próprio log capturer
	if fg.isLogCapturerLog(entry) {
		result.IsSelfLog = true
		result.Reason = "log_capturer_self_log"
		return fg.handleSelfLog(result, "log_capturer")
	}

	return result
}

// checkSelfIdentifiers verifica se string corresponde a identificador próprio
func (fg *FeedbackGuard) checkSelfIdentifiers(identifier string) bool {
	for _, selfID := range fg.selfIdentifiers {
		if strings.Contains(identifier, selfID) || strings.Contains(selfID, identifier) {
			return true
		}
	}
	return false
}

// isLogCapturerLog verifica se é log do próprio log capturer
func (fg *FeedbackGuard) isLogCapturerLog(entry *types.LogEntry) bool {
	// Verificar se mensagem contém indicadores de log capturer
	message := strings.ToLower(entry.Message)

	logCapturerIndicators := []string{
		"log_capturer",
		"logs_capture",
		"ssw-logs-capture",
		"dispatcher",
		"file_monitor",
		"container_monitor",
		"loki_sink",
		"dead letter queue",
		"deduplication",
		"position_manager",
	}

	for _, indicator := range logCapturerIndicators {
		if strings.Contains(message, indicator) {
			return true
		}
	}

	// Verificar labels específicas do log capturer (thread-safe)
	if service, exists := entry.GetLabel("service"); exists {
		if strings.Contains(strings.ToLower(service), "log") &&
		   (strings.Contains(strings.ToLower(service), "capture") ||
		    strings.Contains(strings.ToLower(service), "capturer")) {
			return true
		}
	}

	return false
}

// handleSelfLog trata log identificado como próprio
func (fg *FeedbackGuard) handleSelfLog(result *GuardResult, filterType string) *GuardResult {
	fg.mutex.Lock()
	fg.stats.SelfLogsDetected++

	switch filterType {
	case "container":
		fg.stats.ContainerFiltered++
	case "path":
		fg.stats.PathFiltered++
	case "message":
		fg.stats.MessageFiltered++
	}
	fg.mutex.Unlock()

	switch fg.config.SelfLogAction {
	case "drop":
		result.Action = "drop"
		fg.mutex.Lock()
		fg.stats.SelfLogsDropped++
		fg.mutex.Unlock()

		// fg.logger.WithFields(logrus.Fields{
		//	"reason":        result.Reason,
		//	"match_pattern": result.MatchPattern,
		//	"filter_type":   filterType,
		// }).Debug("Self-log dropped")

	case "tag":
		result.Action = "tag"
		fg.mutex.Lock()
		fg.stats.SelfLogsTagged++
		fg.mutex.Unlock()

		// Adicionar tag indicando self-log (seria aplicado pelo caller)
		fg.logger.WithFields(logrus.Fields{
			"reason":        result.Reason,
			"match_pattern": result.MatchPattern,
			"filter_type":   filterType,
		}).Debug("Self-log tagged")

	case "warn":
		result.Action = "allow"
		fg.logger.WithFields(logrus.Fields{
			"reason":        result.Reason,
			"match_pattern": result.MatchPattern,
			"filter_type":   filterType,
		}).Warn("Self-log detected but allowed")

	default:
		result.Action = "drop"
		fg.mutex.Lock()
		fg.stats.SelfLogsDropped++
		fg.mutex.Unlock()
	}

	return result
}

// ShouldProcessEntry verifica se entrada deve ser processada
func (fg *FeedbackGuard) ShouldProcessEntry(entry *types.LogEntry) bool {
	result := fg.CheckEntry(entry)
	return result.Action != "drop"
}

// TagSelfEntry adiciona tag de self-log se necessário
func (fg *FeedbackGuard) TagSelfEntry(entry *types.LogEntry) bool {
	result := fg.CheckEntry(entry)
	if result.Action == "tag" {
		// Thread-safe label updates
		entry.SetLabel(fg.config.SelfLogTag, "true")
		entry.SetLabel("self_log_reason", result.Reason)
		return true
	}
	return false
}

// GetStats retorna estatísticas
func (fg *FeedbackGuard) GetStats() Stats {
	fg.mutex.RLock()
	defer fg.mutex.RUnlock()
	return fg.stats
}

// GetInfo retorna informações de configuração
func (fg *FeedbackGuard) GetInfo() map[string]interface{} {
	stats := fg.GetStats()

	selfLogRate := float64(0)
	if stats.TotalChecked > 0 {
		selfLogRate = float64(stats.SelfLogsDetected) / float64(stats.TotalChecked) * 100
	}

	return map[string]interface{}{
		"enabled":                    fg.config.Enabled,
		"self_identifiers":           fg.selfIdentifiers,
		"self_container_name":        fg.config.SelfContainerName,
		"self_namespace":             fg.config.SelfNamespace,
		"auto_detect_self":           fg.config.AutoDetectSelf,
		"self_log_action":            fg.config.SelfLogAction,
		"self_log_tag":               fg.config.SelfLogTag,
		"exclude_path_patterns":      fg.config.ExcludePathPatterns,
		"exclude_container_patterns": fg.config.ExcludeContainerPatterns,
		"exclude_message_patterns":   fg.config.ExcludeMessagePatterns,
		"total_checked":              stats.TotalChecked,
		"self_logs_detected":         stats.SelfLogsDetected,
		"self_logs_dropped":          stats.SelfLogsDropped,
		"self_logs_tagged":           stats.SelfLogsTagged,
		"container_filtered":         stats.ContainerFiltered,
		"path_filtered":              stats.PathFiltered,
		"message_filtered":           stats.MessageFiltered,
		"self_log_rate_percent":      selfLogRate,
	}
}

// ResetStats reseta estatísticas
func (fg *FeedbackGuard) ResetStats() {
	fg.mutex.Lock()
	defer fg.mutex.Unlock()

	fg.stats = Stats{}
	fg.logger.Info("Feedback guard stats reset")
}

// UpdateConfig atualiza configuração em tempo de execução
func (fg *FeedbackGuard) UpdateConfig(newConfig Config) {
	fg.mutex.Lock()
	defer fg.mutex.Unlock()

	fg.config = newConfig
	fg.initializeSelfIdentifiers()
	fg.compilePatterns()

	fg.logger.Info("Feedback guard configuration updated")
}