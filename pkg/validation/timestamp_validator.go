package validation

import (
	"fmt"
	"sync"
	"time"

	"ssw-logs-capture/pkg/dlq"
	"ssw-logs-capture/pkg/types"

	"github.com/sirupsen/logrus"
)

// TimestampValidator valida e ajusta timestamps de logs
type TimestampValidator struct {
	config Config
	logger *logrus.Logger
	dlq    *dlq.DeadLetterQueue

	stats Stats
	mutex sync.RWMutex
}

// Config configuração do validador de timestamp
type Config struct {
	// Habilitar validação
	Enabled bool `yaml:"enabled"`

	// Idade máxima permitida para timestamps no passado (segundos)
	MaxPastAgeSeconds int `yaml:"max_past_age_seconds"`

	// Idade máxima permitida para timestamps no futuro (segundos)
	MaxFutureAgeSeconds int `yaml:"max_future_age_seconds"`

	// Habilitar clamping automático
	ClampEnabled bool `yaml:"clamp_enabled"`

	// Enviar timestamps inválidos para DLQ
	ClampDLQ bool `yaml:"clamp_dlq"`

	// Ação para timestamps inválidos: "clamp", "reject", "warn"
	InvalidAction string `yaml:"invalid_action"`

	// Timezone padrão para parsing de timestamps
	DefaultTimezone string `yaml:"default_timezone"`

	// Formatos de timestamp aceitos para parsing
	AcceptedFormats []string `yaml:"accepted_formats"`
}

// Stats estatísticas do validador
type Stats struct {
	TotalValidated     int64 `json:"total_validated"`
	ValidTimestamps    int64 `json:"valid_timestamps"`
	InvalidTimestamps  int64 `json:"invalid_timestamps"`
	ClampedTimestamps  int64 `json:"clamped_timestamps"`
	RejectedTimestamps int64 `json:"rejected_timestamps"`
	FutureTimestamps   int64 `json:"future_timestamps"`
	PastTimestamps     int64 `json:"past_timestamps"`
}

// ValidationResult resultado da validação
type ValidationResult struct {
	Valid         bool      `json:"valid"`
	OriginalTime  time.Time `json:"original_time"`
	ValidatedTime time.Time `json:"validated_time"`
	Action        string    `json:"action"`        // "valid", "clamped", "rejected"
	Reason        string    `json:"reason"`
	Severity      string    `json:"severity"`      // "info", "warning", "error"
}

// NewTimestampValidator cria novo validador de timestamp
func NewTimestampValidator(config Config, logger *logrus.Logger, dlq *dlq.DeadLetterQueue) *TimestampValidator {
	// Valores padrão
	if config.MaxPastAgeSeconds == 0 {
		config.MaxPastAgeSeconds = 21600 // 6 horas
	}
	if config.MaxFutureAgeSeconds == 0 {
		config.MaxFutureAgeSeconds = 60 // 1 minuto
	}
	if config.InvalidAction == "" {
		config.InvalidAction = "clamp"
	}
	if config.DefaultTimezone == "" {
		config.DefaultTimezone = "UTC"
	}
	if len(config.AcceptedFormats) == 0 {
		config.AcceptedFormats = []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
		}
	}

	return &TimestampValidator{
		config: config,
		logger: logger,
		dlq:    dlq,
	}
}

// ValidateTimestamp valida um timestamp
func (tv *TimestampValidator) ValidateTimestamp(entry *types.LogEntry) *ValidationResult {
	if !tv.config.Enabled {
		return &ValidationResult{
			Valid:         true,
			OriginalTime:  entry.Timestamp,
			ValidatedTime: entry.Timestamp,
			Action:        "valid",
			Reason:        "validation_disabled",
			Severity:      "info",
		}
	}

	tv.mutex.Lock()
	tv.stats.TotalValidated++
	tv.mutex.Unlock()

	now := time.Now()
	originalTime := entry.Timestamp

	result := &ValidationResult{
		OriginalTime:  originalTime,
		ValidatedTime: originalTime,
		Valid:         true,
		Action:        "valid",
		Severity:      "info",
	}

	// Verificar se timestamp está no futuro
	maxFuture := now.Add(time.Duration(tv.config.MaxFutureAgeSeconds) * time.Second)
	if originalTime.After(maxFuture) {
		tv.mutex.Lock()
		tv.stats.InvalidTimestamps++
		tv.stats.FutureTimestamps++
		tv.mutex.Unlock()

		result.Valid = false
		result.Reason = "timestamp_too_far_future"
		result.Severity = "warning"

		drift := originalTime.Sub(now)
		tv.logger.WithFields(logrus.Fields{
			"source_type":        entry.SourceType,
			"source_id":          entry.SourceID,
			"original_timestamp": originalTime,
			"current_time":       now,
			"drift_seconds":      drift.Seconds(),
		}).Warn("Timestamp too far in future")

		return tv.handleInvalidTimestamp(entry, result, now)
	}

	// Verificar se timestamp está muito no passado
	maxPast := now.Add(-time.Duration(tv.config.MaxPastAgeSeconds) * time.Second)
	if originalTime.Before(maxPast) {
		tv.mutex.Lock()
		tv.stats.InvalidTimestamps++
		tv.stats.PastTimestamps++
		tv.mutex.Unlock()

		result.Valid = false
		result.Reason = "timestamp_too_old"
		result.Severity = "warning"

		drift := now.Sub(originalTime)
		tv.logger.WithFields(logrus.Fields{
			"source_type":        entry.SourceType,
			"source_id":          entry.SourceID,
			"original_timestamp": originalTime,
			"current_time":       now,
			"drift_seconds":      drift.Seconds(),
		}).Warn("Timestamp too old")

		return tv.handleInvalidTimestamp(entry, result, now)
	}

	// Timestamp válido
	tv.mutex.Lock()
	tv.stats.ValidTimestamps++
	tv.mutex.Unlock()

	return result
}

// handleInvalidTimestamp trata timestamp inválido baseado na configuração
func (tv *TimestampValidator) handleInvalidTimestamp(entry *types.LogEntry, result *ValidationResult, now time.Time) *ValidationResult {
	switch tv.config.InvalidAction {
	case "clamp":
		if tv.config.ClampEnabled {
			entry.Timestamp = now
			result.ValidatedTime = now
			result.Action = "clamped"
			result.Valid = true

			tv.mutex.Lock()
			tv.stats.ClampedTimestamps++
			tv.mutex.Unlock()

			tv.logger.WithFields(logrus.Fields{
				"source_type":     entry.SourceType,
				"source_id":       entry.SourceID,
				"original_time":   result.OriginalTime,
				"clamped_time":    now,
			}).Debug("Timestamp clamped to current time")

			// Enviar para DLQ se configurado
			if tv.config.ClampDLQ && tv.dlq != nil {
				context := map[string]string{
					"validation_action": "clamped",
					"original_timestamp": result.OriginalTime.Format(time.RFC3339),
					"clamped_timestamp":  now.Format(time.RFC3339),
					"reason":            result.Reason,
				}
				tv.dlq.AddEntry(entry, "timestamp_clamped", "timestamp_validation", "timestamp_validator", 0, context)
			}
		} else {
			result.Action = "rejected"
			result.Valid = false
			tv.mutex.Lock()
			tv.stats.RejectedTimestamps++
			tv.mutex.Unlock()
		}

	case "reject":
		result.Action = "rejected"
		result.Valid = false
		result.Severity = "error"

		tv.mutex.Lock()
		tv.stats.RejectedTimestamps++
		tv.mutex.Unlock()

		tv.logger.WithFields(logrus.Fields{
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
			"timestamp":     result.OriginalTime,
			"reason":        result.Reason,
		}).Error("Timestamp rejected")

	case "warn":
		result.Action = "warned"
		result.Valid = true
		result.Severity = "warning"

		tv.logger.WithFields(logrus.Fields{
			"source_type":   entry.SourceType,
			"source_id":     entry.SourceID,
			"timestamp":     result.OriginalTime,
			"reason":        result.Reason,
		}).Warn("Invalid timestamp detected but allowed")

	default:
		// Padrão: clamp
		entry.Timestamp = now
		result.ValidatedTime = now
		result.Action = "clamped"
		result.Valid = true

		tv.mutex.Lock()
		tv.stats.ClampedTimestamps++
		tv.mutex.Unlock()
	}

	return result
}

// ParseTimestamp tenta fazer parse de timestamp usando formatos configurados
func (tv *TimestampValidator) ParseTimestamp(timestampStr string) (time.Time, error) {
	// Tentar formatos configurados
	for _, format := range tv.config.AcceptedFormats {
		if parsed, err := time.Parse(format, timestampStr); err == nil {
			return parsed, nil
		}
	}

	// Tentar com timezone padrão
	location, err := time.LoadLocation(tv.config.DefaultTimezone)
	if err == nil {
		for _, format := range tv.config.AcceptedFormats {
			if parsed, err := time.ParseInLocation(format, timestampStr, location); err == nil {
				return parsed, nil
			}
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp '%s' with any configured format", timestampStr)
}

// ValidateAndParseTimestamp valida e faz parse de timestamp de string
func (tv *TimestampValidator) ValidateAndParseTimestamp(timestampStr string, entry *types.LogEntry) *ValidationResult {
	parsed, err := tv.ParseTimestamp(timestampStr)
	if err != nil {
		tv.mutex.Lock()
		tv.stats.TotalValidated++
		tv.stats.InvalidTimestamps++
		tv.stats.RejectedTimestamps++
		tv.mutex.Unlock()

		return &ValidationResult{
			Valid:         false,
			OriginalTime:  time.Time{},
			ValidatedTime: time.Now(), // Use current time as fallback
			Action:        "rejected",
			Reason:        "unparseable_timestamp",
			Severity:      "error",
		}
	}

	// Atualizar entry com timestamp parseado
	entry.Timestamp = parsed

	// Validar timestamp parseado
	return tv.ValidateTimestamp(entry)
}

// IsTimestampInWindow verifica se timestamp está dentro da janela aceitável
func (tv *TimestampValidator) IsTimestampInWindow(timestamp time.Time) bool {
	if !tv.config.Enabled {
		return true
	}

	now := time.Now()
	maxFuture := now.Add(time.Duration(tv.config.MaxFutureAgeSeconds) * time.Second)
	maxPast := now.Add(-time.Duration(tv.config.MaxPastAgeSeconds) * time.Second)

	return timestamp.After(maxPast) && timestamp.Before(maxFuture)
}

// GetStats retorna estatísticas do validador
func (tv *TimestampValidator) GetStats() Stats {
	tv.mutex.RLock()
	defer tv.mutex.RUnlock()
	return tv.stats
}

// GetInfo retorna informações de configuração
func (tv *TimestampValidator) GetInfo() map[string]interface{} {
	stats := tv.GetStats()

	validRate := float64(0)
	if stats.TotalValidated > 0 {
		validRate = float64(stats.ValidTimestamps) / float64(stats.TotalValidated) * 100
	}

	return map[string]interface{}{
		"enabled":                tv.config.Enabled,
		"max_past_age_seconds":   tv.config.MaxPastAgeSeconds,
		"max_future_age_seconds": tv.config.MaxFutureAgeSeconds,
		"clamp_enabled":          tv.config.ClampEnabled,
		"clamp_dlq":              tv.config.ClampDLQ,
		"invalid_action":         tv.config.InvalidAction,
		"default_timezone":       tv.config.DefaultTimezone,
		"accepted_formats":       tv.config.AcceptedFormats,
		"total_validated":        stats.TotalValidated,
		"valid_timestamps":       stats.ValidTimestamps,
		"invalid_timestamps":     stats.InvalidTimestamps,
		"clamped_timestamps":     stats.ClampedTimestamps,
		"rejected_timestamps":    stats.RejectedTimestamps,
		"future_timestamps":      stats.FutureTimestamps,
		"past_timestamps":        stats.PastTimestamps,
		"valid_rate_percent":     validRate,
	}
}

// ResetStats reseta as estatísticas
func (tv *TimestampValidator) ResetStats() {
	tv.mutex.Lock()
	defer tv.mutex.Unlock()

	tv.stats = Stats{}
	tv.logger.Info("Timestamp validator stats reset")
}