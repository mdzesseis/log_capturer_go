package security

import (
	"strings"
	"testing"
)

func TestSanitizer_Sanitize_URLPasswords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "postgres URL with password",
			input:    "postgres://user:secret123@localhost:5432/db",
			expected: "postgres://user:****@localhost:5432/db",
		},
		{
			name:     "mysql URL with password and special chars",
			input:    "mysql://admin:Passw0rd@db.example.com/database",
			expected: "mysql://admin:****@db.example.com/database",
		},
		{
			name:     "redis URL with password (no username)",
			input:    "redis://user:myredispass@redis:6379/0",
			expected: "redis://user:****@redis:6379/0",
		},
		{
			name:     "HTTP basic auth",
			input:    "https://apiuser:apipass123@api.example.com/endpoint",
			expected: "https://apiuser:****@api.example.com/endpoint",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_BearerTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bearer token in header (both patterns applied)",
			input:    "Bearer abc123token",
			expected: "Bearer ****",
		},
		{
			name:     "bearer token lowercase",
			input:    "bearer abc123def456ghi789",
			expected: "bearer ****",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_APIKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "API key with equals",
			input:    "api_key=sk_live_1234567890abcdef",
			expected: "api_key=****",
		},
		{
			name:     "API key with colon",
			input:    "api-key: pk_test_abcdefghijklmnop",
			expected: "api-key: ****",
		},
		{
			name:     "X-API-Key header",
			input:    "X-API-Key: 1234-5678-90ab-cdef",
			expected: "X-API-Key: ****",
		},
		{
			name:     "Authorization header",
			input:    "Authorization: sk_live_1234567890",
			expected: "Authorization: ****",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_Passwords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "password with equals",
			input:    "password=mySecretPass123",
			expected: "password=****",
		},
		{
			name:     "pwd with colon",
			input:    "pwd: admin123",
			expected: "pwd: ****",
		},
		{
			name:     "passwd in config",
			input:    "passwd=P@ssw0rd!",
			expected: "passwd=****",
		},
		{
			name:     "Password capitalized",
			input:    "Password: VerySecret123",
			expected: "Password: ****",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_Tokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "token parameter",
			input:    "token=ghp_1234567890abcdefghijklmnopqrstu",
			expected: "token=****",
		},
		{
			name:     "secret parameter",
			input:    "secret: mysecrettoken1234567890",
			expected: "secret: ****",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_AWSCredentials(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "AWS access key",
			input:    "aws_access_key_id=AKIAIOSFODNN7EXAMPLE",
			expected: "aws_access_key_id=****",
		},
		{
			name:     "AWS secret key",
			input:    "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expected: "aws_secret_access_key=****",
		},
	}

	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_Sanitize_CreditCards(t *testing.T) {
	config := DefaultSanitizerConfig()
	config.RedactCreditCards = true
	sanitizer := NewSanitizer(config)

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "credit card with spaces",
			input:    "Card: 4532 1234 5678 9010",
			contains: "****-****-****-9010",
		},
		{
			name:     "credit card with dashes",
			input:    "Card: 4532-1234-5678-9010",
			contains: "****-****-****-9010",
		},
		{
			name:     "credit card no separators",
			input:    "Card: 4532123456789010",
			contains: "****-****-****-9010",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("Sanitize() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestSanitizer_Sanitize_Emails(t *testing.T) {
	config := DefaultSanitizerConfig()
	config.RedactEmails = true
	sanitizer := NewSanitizer(config)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "email in log",
			input:    "User email: user@example.com",
			expected: "User email: u****@example.com",
		},
		{
			name:     "email with dots",
			input:    "Contact: john.doe@company.co.uk",
			expected: "Contact: j****@company.co.uk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_SanitizeURL(t *testing.T) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with password (URL encoded)",
			input:    "https://user:password@example.com/path",
			expected: "https://user:%2A%2A%2A%2A@example.com/path", // Go's url.URL encodes **** as %2A%2A%2A%2A
		},
		{
			name:     "URL with token query param",
			input:    "https://api.example.com/endpoint?token=secret123",
			expected: "https://api.example.com/endpoint?token=%2A%2A%2A%2A",
		},
		{
			name:     "URL with API key query param",
			input:    "https://api.example.com/data?api_key=sk_live_1234567890",
			expected: "https://api.example.com/data?api_key=%2A%2A%2A%2A",
		},
		{
			name:     "URL with multiple sensitive params",
			input:    "https://api.com/v1?apikey=abc123&secret=xyz789&data=public",
			expected: "https://api.com/v1?apikey=%2A%2A%2A%2A&data=public&secret=%2A%2A%2A%2A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.SanitizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizer_SanitizeMap(t *testing.T) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	input := map[string]string{
		"username":      "john",
		"password":      "password=secret123",
		"api_key":       "api_key=sk_live_abc123",
		"Authorization": "Authorization: Bearer token123",
		"data":          "public_data",
	}

	result := sanitizer.SanitizeMap(input)

	// Check that sensitive values are sanitized
	if !strings.Contains(result["password"], "****") {
		t.Errorf("password not sanitized: %v", result["password"])
	}
	if !strings.Contains(result["api_key"], "****") {
		t.Errorf("api_key not sanitized: %v", result["api_key"])
	}
	if !strings.Contains(result["Authorization"], "****") {
		t.Errorf("Authorization not sanitized: %v", result["Authorization"])
	}

	// Check that non-sensitive values are preserved
	if result["username"] != "john" {
		t.Errorf("username should not be sanitized: %v", result["username"])
	}
	if result["data"] != "public_data" {
		t.Errorf("data should not be sanitized: %v", result["data"])
	}
}

func TestSanitizer_IsSensitive(t *testing.T) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())

	tests := []struct {
		name      string
		input     string
		sensitive bool
	}{
		{
			name:      "contains password",
			input:     "password=secret123",
			sensitive: true,
		},
		{
			name:      "contains bearer token",
			input:     "Authorization: Bearer abc123",
			sensitive: true,
		},
		{
			name:      "contains API key",
			input:     "api_key=sk_1234567890",
			sensitive: true,
		},
		{
			name:      "normal log message",
			input:     "Processing 100 records successfully",
			sensitive: false,
		},
		{
			name:      "URL with password",
			input:     "Connecting to postgres://user:pass@localhost",
			sensitive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.IsSensitive(tt.input)
			if result != tt.sensitive {
				t.Errorf("IsSensitive() = %v, want %v", result, tt.sensitive)
			}
		})
	}
}

func TestSanitizer_CustomPatterns(t *testing.T) {
	config := DefaultSanitizerConfig()
	config.CustomPatterns = map[string]string{
		"custom_id": `CUST-\d{6}`,
	}

	sanitizer := NewSanitizer(config)

	input := "Customer ID: CUST-123456"
	result := sanitizer.Sanitize(input)

	if !strings.Contains(result, "****") {
		t.Errorf("Custom pattern not sanitized: %v", result)
	}
}

func TestSanitize_GlobalFunctions(t *testing.T) {
	// Test global convenience functions
	input := "password=secret123"
	result := Sanitize(input)

	if !strings.Contains(result, "****") {
		t.Errorf("Global Sanitize() not working: %v", result)
	}
}

func BenchmarkSanitizer_Sanitize(b *testing.B) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())
	input := "Connecting to postgres://user:password123@localhost:5432/db with api_key=sk_live_1234567890"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.Sanitize(input)
	}
}

func BenchmarkSanitizer_SanitizeURL(b *testing.B) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())
	input := "https://user:password@api.example.com/endpoint?token=secret123&api_key=abc"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.SanitizeURL(input)
	}
}

func BenchmarkSanitizer_IsSensitive(b *testing.B) {
	sanitizer := NewSanitizer(DefaultSanitizerConfig())
	input := "This is a log message with password=secret123 inside"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sanitizer.IsSensitive(input)
	}
}
