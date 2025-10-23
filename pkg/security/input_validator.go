package security

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"ssw-logs-capture/pkg/errors"
)

// InputValidator provides comprehensive input validation and sanitization
type InputValidator struct {
	config ValidationConfig
}

// ValidationConfig configures the input validator
type ValidationConfig struct {
	MaxPathLength    int      `yaml:"max_path_length"`
	MaxStringLength  int      `yaml:"max_string_length"`
	AllowedPathChars string   `yaml:"allowed_path_chars"`
	BlockedPatterns  []string `yaml:"blocked_patterns"`
	RequireAbsolute  bool     `yaml:"require_absolute"`
}

// DefaultValidationConfig returns safe default configuration
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxPathLength:    4096,
		MaxStringLength:  65536,
		AllowedPathChars: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./",
		BlockedPatterns: []string{
			"\\.\\.",      // Path traversal
			"/etc/",       // System directories
			"/proc/",      // System directories
			"/sys/",       // System directories
			"/dev/",       // Device files
			"/root/",      // Root home
			"\\$\\{",      // Variable expansion
			"`",           // Command execution
			"\\|",         // Pipe commands
			";",           // Command separation
			"&",           // Background execution
		},
		RequireAbsolute: true,
	}
}

// NewInputValidator creates a new input validator
func NewInputValidator(config ValidationConfig) *InputValidator {
	return &InputValidator{config: config}
}

// ValidatePath validates and sanitizes file/directory paths
func (v *InputValidator) ValidatePath(path string) error {
	if path == "" {
		return errors.SecurityError("validate_path", "path cannot be empty")
	}

	// Check length
	if len(path) > v.config.MaxPathLength {
		return errors.SecurityError("validate_path", fmt.Sprintf("path too long: %d chars (max %d)", len(path), v.config.MaxPathLength))
	}

	// Clean the path
	cleanPath := filepath.Clean(path)

	// Check for path traversal
	if strings.Contains(cleanPath, "..") {
		return errors.SecurityError("validate_path", "path traversal detected").WithMetadata("path", path)
	}

	// Require absolute paths for security
	if v.config.RequireAbsolute && !filepath.IsAbs(cleanPath) {
		return errors.SecurityError("validate_path", "path must be absolute").WithMetadata("path", path)
	}

	// Check against blocked patterns
	for _, pattern := range v.config.BlockedPatterns {
		if matched, _ := regexp.MatchString(pattern, cleanPath); matched {
			return errors.SecurityError("validate_path", "path contains blocked pattern").
				WithMetadata("path", path).
				WithMetadata("pattern", pattern)
		}
	}

	// Validate characters
	for _, char := range cleanPath {
		if !strings.ContainsRune(v.config.AllowedPathChars, char) {
			return errors.SecurityError("validate_path", "path contains invalid character").
				WithMetadata("path", path).
				WithMetadata("char", string(char))
		}
	}

	return nil
}

// ValidateURL validates and sanitizes URLs
func (v *InputValidator) ValidateURL(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, errors.SecurityError("validate_url", "URL cannot be empty")
	}

	// Parse URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.SecurityError("validate_url", "invalid URL format").Wrap(err)
	}

	// Validate scheme
	allowedSchemes := map[string]bool{
		"http":  true,
		"https": true,
	}

	if !allowedSchemes[parsedURL.Scheme] {
		return nil, errors.SecurityError("validate_url", "unsupported URL scheme").
			WithMetadata("scheme", parsedURL.Scheme)
	}

	// Validate host
	if parsedURL.Host == "" {
		return nil, errors.SecurityError("validate_url", "URL host cannot be empty")
	}

	// Block localhost/private IPs in production
	if v.isPrivateHost(parsedURL.Host) {
		return nil, errors.SecurityError("validate_url", "private/localhost URLs not allowed").
			WithMetadata("host", parsedURL.Host)
	}

	return parsedURL, nil
}

// ValidateString validates and sanitizes general string input
func (v *InputValidator) ValidateString(input, fieldName string) (string, error) {
	if len(input) > v.config.MaxStringLength {
		return "", errors.SecurityError("validate_string", fmt.Sprintf("%s too long: %d chars (max %d)", fieldName, len(input), v.config.MaxStringLength))
	}

	// Remove null bytes
	cleaned := strings.ReplaceAll(input, "\x00", "")

	// Check for control characters (except newline, tab, carriage return)
	for _, char := range cleaned {
		if unicode.IsControl(char) && char != '\n' && char != '\t' && char != '\r' {
			return "", errors.SecurityError("validate_string", fmt.Sprintf("%s contains control characters", fieldName)).
				WithMetadata("char_code", fmt.Sprintf("%d", char))
		}
	}

	// Check against blocked patterns
	for _, pattern := range v.config.BlockedPatterns {
		if matched, _ := regexp.MatchString(pattern, cleaned); matched {
			return "", errors.SecurityError("validate_string", fmt.Sprintf("%s contains blocked pattern", fieldName)).
				WithMetadata("pattern", pattern)
		}
	}

	return cleaned, nil
}

// ValidateLogMessage validates log message content
func (v *InputValidator) ValidateLogMessage(message string) (string, error) {
	if message == "" {
		return "", nil // Empty messages are allowed
	}

	// Basic string validation
	cleaned, err := v.ValidateString(message, "log_message")
	if err != nil {
		return "", err
	}

	// Additional log-specific validation
	// Check for potential injection attacks
	injectionPatterns := []string{
		"<script",
		"javascript:",
		"data:text/html",
		"vbscript:",
		"onload=",
		"onerror=",
	}

	lowerMessage := strings.ToLower(cleaned)
	for _, pattern := range injectionPatterns {
		if strings.Contains(lowerMessage, pattern) {
			return "", errors.SecurityError("validate_log_message", "log message contains potential injection").
				WithMetadata("pattern", pattern)
		}
	}

	return cleaned, nil
}

// ValidateLabels validates label keys and values
func (v *InputValidator) ValidateLabels(labels map[string]string) (map[string]string, error) {
	if labels == nil {
		return nil, nil
	}

	validated := make(map[string]string)

	for key, value := range labels {
		// Validate key
		cleanKey, err := v.ValidateString(key, "label_key")
		if err != nil {
			return nil, err
		}

		// Additional key validation
		if !v.isValidLabelKey(cleanKey) {
			return nil, errors.SecurityError("validate_labels", "invalid label key format").
				WithMetadata("key", cleanKey)
		}

		// Validate value
		cleanValue, err := v.ValidateString(value, "label_value")
		if err != nil {
			return nil, err
		}

		validated[cleanKey] = cleanValue
	}

	return validated, nil
}

// SanitizeForLogging sanitizes data for safe logging
func (v *InputValidator) SanitizeForLogging(data string) string {
	// Remove potential secrets
	secretPatterns := []string{
		`password["\s]*[:=]["\s]*[^"\s,}]+`,
		`token["\s]*[:=]["\s]*[^"\s,}]+`,
		`secret["\s]*[:=]["\s]*[^"\s,}]+`,
		`key["\s]*[:=]["\s]*[^"\s,}]+`,
		`authorization["\s]*:["\s]*[^"\s,}]+`,
	}

	sanitized := data
	for _, pattern := range secretPatterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		sanitized = re.ReplaceAllString(sanitized, "${1}[REDACTED]")
	}

	// Truncate if too long
	if len(sanitized) > 1000 {
		sanitized = sanitized[:997] + "..."
	}

	return sanitized
}

// isPrivateHost checks if host is localhost or private IP
func (v *InputValidator) isPrivateHost(host string) bool {
	// Remove port if present
	if colonIndex := strings.LastIndex(host, ":"); colonIndex > 0 {
		host = host[:colonIndex]
	}

	privateHosts := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
	}

	for _, private := range privateHosts {
		if host == private {
			return true
		}
	}

	// Check private IP ranges
	privateRanges := []string{
		"10.",
		"172.16.", "172.17.", "172.18.", "172.19.", "172.20.",
		"172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"192.168.",
		"169.254.", // Link-local
	}

	for _, prefix := range privateRanges {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}

	return false
}

// isValidLabelKey validates label key format
func (v *InputValidator) isValidLabelKey(key string) bool {
	if key == "" || len(key) > 63 {
		return false
	}

	// Label keys should start with letter and contain only alphanumeric and underscores
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`).MatchString(key) {
		return false
	}

	return true
}

// ResourceLimiter prevents resource exhaustion attacks
type ResourceLimiter struct {
	maxFileDescriptors int
	maxMemoryMB       int
	maxGoroutines     int
	currentFDs        int
	currentMemoryMB   int
	currentGoroutines int
}

// NewResourceLimiter creates a new resource limiter
func NewResourceLimiter(maxFDs, maxMemoryMB, maxGoroutines int) *ResourceLimiter {
	return &ResourceLimiter{
		maxFileDescriptors: maxFDs,
		maxMemoryMB:       maxMemoryMB,
		maxGoroutines:     maxGoroutines,
	}
}

// CheckResourceLimits validates current resource usage
func (rl *ResourceLimiter) CheckResourceLimits() error {
	if rl.currentFDs > rl.maxFileDescriptors {
		return errors.ResourceError("check_limits", fmt.Sprintf("too many file descriptors: %d (max %d)", rl.currentFDs, rl.maxFileDescriptors))
	}

	if rl.currentMemoryMB > rl.maxMemoryMB {
		return errors.ResourceError("check_limits", fmt.Sprintf("too much memory used: %dMB (max %dMB)", rl.currentMemoryMB, rl.maxMemoryMB))
	}

	if rl.currentGoroutines > rl.maxGoroutines {
		return errors.ResourceError("check_limits", fmt.Sprintf("too many goroutines: %d (max %d)", rl.currentGoroutines, rl.maxGoroutines))
	}

	return nil
}

// UpdateResourceUsage updates current resource usage
func (rl *ResourceLimiter) UpdateResourceUsage(fds, memoryMB, goroutines int) {
	rl.currentFDs = fds
	rl.currentMemoryMB = memoryMB
	rl.currentGoroutines = goroutines
}