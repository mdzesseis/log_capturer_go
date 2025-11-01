package security

import (
	"net/url"
	"regexp"
	"strings"
)

// Sanitizer provides methods for sanitizing sensitive data before logging or storage.
//
// This component is critical for compliance with data protection regulations like
// GDPR, LGPD, and CCPA. It automatically detects and redacts sensitive information
// including:
//   - Passwords in URLs and connection strings
//   - API keys and tokens (Bearer, API-Key, etc.)
//   - Credit card numbers
//   - Email addresses (optional)
//   - IP addresses (optional)
//   - Social security numbers and similar identifiers
//
// The Sanitizer is designed to be fast and non-blocking, making it safe to use
// in hot paths of the application.
type Sanitizer struct {
	// Patterns for detecting sensitive data
	patterns map[string]*regexp.Regexp

	// Configuration options
	redactEmails    bool
	redactIPs       bool
	redactCreditCards bool
	customPatterns  map[string]*regexp.Regexp
}

// SanitizerConfig configures the behavior of the Sanitizer.
type SanitizerConfig struct {
	RedactEmails      bool                       // Redact email addresses
	RedactIPs         bool                       // Redact IP addresses
	RedactCreditCards bool                       // Redact credit card numbers
	CustomPatterns    map[string]string          // Custom regex patterns to redact
}

// DefaultSanitizerConfig returns a sanitizer configuration with secure defaults.
func DefaultSanitizerConfig() SanitizerConfig {
	return SanitizerConfig{
		RedactEmails:      false, // Often needed for debugging
		RedactIPs:         false, // Often needed for debugging
		RedactCreditCards: true,  // Always redact by default
		CustomPatterns:    make(map[string]string),
	}
}

// NewSanitizer creates a new Sanitizer with the given configuration.
func NewSanitizer(config SanitizerConfig) *Sanitizer {
	s := &Sanitizer{
		patterns:          make(map[string]*regexp.Regexp),
		customPatterns:    make(map[string]*regexp.Regexp),
		redactEmails:      config.RedactEmails,
		redactIPs:         config.RedactIPs,
		redactCreditCards: config.RedactCreditCards,
	}

	// Compile built-in patterns
	s.compileBuiltInPatterns()

	// Compile custom patterns
	for name, pattern := range config.CustomPatterns {
		if re, err := regexp.Compile(pattern); err == nil {
			s.customPatterns[name] = re
		}
	}

	return s
}

// compileBuiltInPatterns compiles all built-in regex patterns for sensitive data detection.
func (s *Sanitizer) compileBuiltInPatterns() {
	// Password patterns in URLs and connection strings (non-greedy match before @)
	s.patterns["url_password"] = regexp.MustCompile(`(://[^:@]+:)([^@]+?)(@)`)

	// Bearer tokens
	s.patterns["bearer_token"] = regexp.MustCompile(`(?i)(bearer\s+)([a-zA-Z0-9\-._~+/]+=*)`)

	// API keys (various formats)
	s.patterns["api_key_header"] = regexp.MustCompile(`(?i)(api[_-]?key\s*[=:]\s*)([a-zA-Z0-9\-._~+/]+)`)
	s.patterns["x_api_key"] = regexp.MustCompile(`(?i)(x-api-key\s*[=:]\s*)([a-zA-Z0-9\-._~+/]+)`)

	// Authorization headers (match everything after colon/equals, more greedy)
	s.patterns["authorization"] = regexp.MustCompile(`(?i)(authorization\s*[=:]\s*)(.+?)(\s|$)`)

	// JWT tokens
	s.patterns["jwt"] = regexp.MustCompile(`(eyJ[a-zA-Z0-9\-._~+/]+=*\.eyJ[a-zA-Z0-9\-._~+/]+=*\.[a-zA-Z0-9\-._~+/]+=*)`)

	// AWS credentials
	s.patterns["aws_access_key"] = regexp.MustCompile(`(?i)(aws[_-]?access[_-]?key[_-]?id\s*[=:]\s*)([A-Z0-9]{20})`)
	s.patterns["aws_secret_key"] = regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key\s*[=:]\s*)([A-Za-z0-9/+=]{40})`)

	// Generic passwords
	s.patterns["password"] = regexp.MustCompile(`(?i)(password\s*[=:]\s*)([^\s,&]+)`)
	s.patterns["passwd"] = regexp.MustCompile(`(?i)(passwd\s*[=:]\s*)([^\s,&]+)`)
	s.patterns["pwd"] = regexp.MustCompile(`(?i)(pwd\s*[=:]\s*)([^\s,&]+)`)

	// Tokens
	s.patterns["token"] = regexp.MustCompile(`(?i)(token\s*[=:]\s*)([a-zA-Z0-9\-._~+/]{16,})`)
	s.patterns["secret"] = regexp.MustCompile(`(?i)(secret\s*[=:]\s*)([a-zA-Z0-9\-._~+/]{16,})`)

	// Credit cards (if enabled)
	if s.redactCreditCards {
		s.patterns["credit_card"] = regexp.MustCompile(`\b(?:\d{4}[-\s]?){3}\d{4}\b`)
	}

	// Email addresses (if enabled)
	if s.redactEmails {
		s.patterns["email"] = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)
	}

	// IP addresses (if enabled)
	if s.redactIPs {
		s.patterns["ipv4"] = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
		s.patterns["ipv6"] = regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b`)
	}

	// Social security numbers (US format)
	s.patterns["ssn"] = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

	// CPF (Brazilian format)
	s.patterns["cpf"] = regexp.MustCompile(`\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`)
}

// Sanitize sanitizes a string by redacting sensitive information.
//
// This method applies all configured patterns to the input string and replaces
// matched sensitive data with asterisks or the configured redaction string.
//
// Examples:
//   - "postgres://user:secret123@localhost" → "postgres://user:****@localhost"
//   - "Bearer abc123token" → "Bearer ****"
//   - "password=mypass123" → "password=****"
func (s *Sanitizer) Sanitize(input string) string {
	if input == "" {
		return input
	}

	result := input

	// Apply URL password sanitization first
	if re, ok := s.patterns["url_password"]; ok {
		result = re.ReplaceAllString(result, "${1}****${3}")
	}

	// Apply bearer token sanitization
	if re, ok := s.patterns["bearer_token"]; ok {
		result = re.ReplaceAllString(result, "${1}****")
	}

	// Apply JWT sanitization
	if re, ok := s.patterns["jwt"]; ok {
		result = re.ReplaceAllString(result, "****")
	}

	// Apply API key sanitization
	for _, patternName := range []string{"api_key_header", "x_api_key"} {
		if re, ok := s.patterns[patternName]; ok {
			result = re.ReplaceAllString(result, "${1}****")
		}
	}

	// Apply authorization sanitization (has 3 capture groups)
	if re, ok := s.patterns["authorization"]; ok {
		result = re.ReplaceAllString(result, "${1}****${3}")
	}

	// Apply AWS credentials sanitization
	if re, ok := s.patterns["aws_access_key"]; ok {
		result = re.ReplaceAllString(result, "${1}****")
	}
	if re, ok := s.patterns["aws_secret_key"]; ok {
		result = re.ReplaceAllString(result, "${1}****")
	}

	// Apply password sanitization
	for _, patternName := range []string{"password", "passwd", "pwd", "token", "secret"} {
		if re, ok := s.patterns[patternName]; ok {
			result = re.ReplaceAllString(result, "${1}****")
		}
	}

	// Apply credit card sanitization
	if re, ok := s.patterns["credit_card"]; ok {
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			// Keep last 4 digits
			if len(match) >= 4 {
				cleaned := strings.ReplaceAll(strings.ReplaceAll(match, "-", ""), " ", "")
				if len(cleaned) >= 4 {
					return "****-****-****-" + cleaned[len(cleaned)-4:]
				}
			}
			return "****"
		})
	}

	// Apply email sanitization
	if re, ok := s.patterns["email"]; ok {
		result = re.ReplaceAllStringFunc(result, func(email string) string {
			parts := strings.Split(email, "@")
			if len(parts) == 2 {
				return parts[0][:1] + "****@" + parts[1]
			}
			return "****@****.***"
		})
	}

	// Apply IP sanitization
	if re, ok := s.patterns["ipv4"]; ok {
		result = re.ReplaceAllStringFunc(result, func(ip string) string {
			parts := strings.Split(ip, ".")
			if len(parts) == 4 {
				return parts[0] + "." + parts[1] + ".***.**"
			}
			return "***.***.***. ***"
		})
	}

	// Apply SSN/CPF sanitization
	if re, ok := s.patterns["ssn"]; ok {
		result = re.ReplaceAllString(result, "***-**-****")
	}
	if re, ok := s.patterns["cpf"]; ok {
		result = re.ReplaceAllString(result, "***.***. ***-**")
	}

	// Apply custom patterns
	for _, re := range s.customPatterns {
		result = re.ReplaceAllString(result, "****")
	}

	return result
}

// SanitizeURL sanitizes a URL string, redacting passwords and query parameters
// that may contain sensitive data.
//
// Examples:
//   - "https://user:pass@example.com" → "https://user:****@example.com"
//   - "https://api.com?token=secret123" → "https://api.com?token=****"
func (s *Sanitizer) SanitizeURL(rawURL string) string {
	// Parse URL to handle query parameters
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		// If parsing fails, apply general sanitization
		return s.Sanitize(rawURL)
	}

	// Redact password in user info
	if parsedURL.User != nil {
		username := parsedURL.User.Username()
		parsedURL.User = url.UserPassword(username, "****")
	}

	// Redact sensitive query parameters
	query := parsedURL.Query()
	sensitiveParams := []string{"token", "api_key", "apikey", "key", "secret", "password", "pwd", "auth"}

	for _, param := range sensitiveParams {
		if query.Has(param) {
			query.Set(param, "****")
		}
	}

	parsedURL.RawQuery = query.Encode()

	return parsedURL.String()
}

// SanitizeMap sanitizes a map of strings, applying sanitization to both keys and values.
// This is useful for sanitizing headers, metadata, and configuration objects.
func (s *Sanitizer) SanitizeMap(data map[string]string) map[string]string {
	if data == nil {
		return nil
	}

	result := make(map[string]string, len(data))

	for key, value := range data {
		// Sanitize both key and value
		sanitizedKey := s.Sanitize(key)
		sanitizedValue := s.Sanitize(value)
		result[sanitizedKey] = sanitizedValue
	}

	return result
}

// SanitizeBytes sanitizes a byte slice by converting to string, sanitizing,
// and converting back. This is useful for sanitizing log entries or message payloads.
func (s *Sanitizer) SanitizeBytes(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	sanitized := s.Sanitize(string(data))
	return []byte(sanitized)
}

// IsSensitive checks if a string contains any sensitive data patterns.
// This can be used to decide whether to sanitize or skip logging entirely.
func (s *Sanitizer) IsSensitive(input string) bool {
	if input == "" {
		return false
	}

	// Check against all patterns
	for _, re := range s.patterns {
		if re.MatchString(input) {
			return true
		}
	}

	// Check against custom patterns
	for _, re := range s.customPatterns {
		if re.MatchString(input) {
			return true
		}
	}

	return false
}

// Default global sanitizer instance with secure defaults
var defaultSanitizer = NewSanitizer(DefaultSanitizerConfig())

// Sanitize is a convenience function that uses the default sanitizer.
func Sanitize(input string) string {
	return defaultSanitizer.Sanitize(input)
}

// SanitizeURL is a convenience function that uses the default sanitizer.
func SanitizeURL(rawURL string) string {
	return defaultSanitizer.SanitizeURL(rawURL)
}

// SanitizeMap is a convenience function that uses the default sanitizer.
func SanitizeMap(data map[string]string) map[string]string {
	return defaultSanitizer.SanitizeMap(data)
}
