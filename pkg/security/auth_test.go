package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAuthManager tests creating a new auth manager
func TestNewAuthManager(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"testuser": {
				Username:     "testuser",
				PasswordHash: hashPassword("testpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    3,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	assert.NotNil(t, authManager)
	assert.Equal(t, config.Enabled, authManager.config.Enabled)
	assert.Equal(t, config.Method, authManager.config.Method)
}

// TestBasicAuthentication tests basic authentication
func TestBasicAuthentication(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"testuser": {
				Username:     "testuser",
				PasswordHash: hashPassword("testpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    3,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	// Test valid credentials
	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("testuser", "testpass")

	authCtx, err := authManager.Authenticate(req)
	assert.NoError(t, err)
	assert.NotNil(t, authCtx)
	assert.True(t, authCtx.Authenticated)
	assert.Equal(t, "testuser", authCtx.Username)
	assert.Contains(t, authCtx.Roles, "admin")

	// Test invalid credentials
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.SetBasicAuth("testuser", "wrongpass")

	authCtx2, err2 := authManager.Authenticate(req2)
	assert.Error(t, err2)
	assert.Nil(t, authCtx2)
}

// TestTokenAuthentication tests token-based authentication
func TestTokenAuthentication(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "token",
		Tokens: map[string]string{
			"valid-token": "testuser",
		},
		Users: map[string]User{
			"testuser": {
				Username:     "testuser",
				PasswordHash: hashPassword("testpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    3,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	// Test valid token
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")

	authCtx, err := authManager.Authenticate(req)
	assert.NoError(t, err)
	assert.NotNil(t, authCtx)
	assert.True(t, authCtx.Authenticated)
	assert.Equal(t, "testuser", authCtx.Username)

	// Test invalid token
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("Authorization", "Bearer invalid-token")

	authCtx2, err2 := authManager.Authenticate(req2)
	assert.Error(t, err2)
	assert.Nil(t, authCtx2)
}

// TestAuthenticationDisabled tests behavior when authentication is disabled
func TestAuthenticationDisabled(t *testing.T) {
	config := AuthConfig{
		Enabled: false,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	req := httptest.NewRequest("GET", "/test", nil)

	authCtx, err := authManager.Authenticate(req)
	assert.NoError(t, err)
	assert.NotNil(t, authCtx)
	assert.True(t, authCtx.Authenticated) // Should be authenticated when auth is disabled
}

// TestUserDisabled tests authentication with disabled user
func TestUserDisabled(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"disableduser": {
				Username:     "disableduser",
				PasswordHash: hashPassword("testpass"),
				Roles:        []string{"admin"},
				Enabled:      false, // User is disabled
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    3,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("disableduser", "testpass")

	authCtx, err := authManager.Authenticate(req)
	assert.Error(t, err)
	assert.Nil(t, authCtx)
}

// TestRateLimiting tests rate limiting functionality
func TestRateLimiting(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"testuser": {
				Username:     "testuser",
				PasswordHash: hashPassword("wronghash"), // Wrong password to trigger failures
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    2, // Low limit for testing
		LockoutTime:    1 * time.Second,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	// Make failed attempts to trigger rate limiting
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.SetBasicAuth("testuser", "wrongpass")

		authCtx, err := authManager.Authenticate(req)
		assert.Error(t, err)
		assert.Nil(t, authCtx)
	}

	// Next attempt should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("testuser", "wrongpass")

	authCtx, err := authManager.Authenticate(req)
	assert.Error(t, err)
	assert.Nil(t, authCtx)
	assert.Contains(t, err.Error(), "locked")
}

// TestSecurityManager tests the security manager
func TestSecurityManager(t *testing.T) {
	config := SecurityConfig{
		Enabled: true,
		Authentication: AuthConfig{
			Enabled: true,
			Method:  "basic",
			Users: map[string]User{
				"testuser": {
					Username:     "testuser",
					PasswordHash: hashPassword("testpass"),
					Roles:        []string{"admin"},
					Enabled:      true,
				},
			},
		},
		Authorization: AuthorizationConfig{
			Enabled: true,
			Roles: map[string]Role{
				"admin": {
					Name: "admin",
					Permissions: []Permission{
						{Resource: "*", Action: "*"},
					},
				},
			},
		},
	}

	logger := logrus.New()
	securityManager, err := NewSecurityManager(config, logger)
	require.NoError(t, err)
	assert.NotNil(t, securityManager)
}

// TestAuthenticationMiddleware tests the authentication middleware
func TestAuthenticationMiddleware(t *testing.T) {
	config := SecurityConfig{
		Enabled: true,
		Authentication: AuthConfig{
			Enabled: true,
			Method:  "basic",
			Users: map[string]User{
				"testuser": {
					Username:     "testuser",
					PasswordHash: hashPassword("testpass"),
					Roles:        []string{"admin"},
					Enabled:      true,
				},
			},
		},
		Authorization: AuthorizationConfig{
			Enabled: true,
			Roles: map[string]Role{
				"admin": {
					Name: "admin",
					Permissions: []Permission{
						{Resource: "*", Action: "*"},
					},
				},
			},
		},
	}

	logger := logrus.New()
	securityManager, err := NewSecurityManager(config, logger)
	require.NoError(t, err)

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Apply middleware
	middleware := securityManager.AuthenticationMiddleware()
	wrappedHandler := middleware(testHandler)

	// Test with valid credentials
	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("testuser", "testpass")
	rr := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Test with invalid credentials
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.SetBasicAuth("testuser", "wrongpass")
	rr2 := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusUnauthorized, rr2.Code)
}

// TestAuthorization tests authorization functionality
func TestAuthorization(t *testing.T) {
	config := SecurityConfig{
		Enabled: true,
		Authentication: AuthConfig{
			Enabled: true,
			Method:  "basic",
			Users: map[string]User{
				"admin": {
					Username:     "admin",
					PasswordHash: hashPassword("adminpass"),
					Roles:        []string{"admin"},
					Enabled:      true,
				},
				"viewer": {
					Username:     "viewer",
					PasswordHash: hashPassword("viewpass"),
					Roles:        []string{"viewer"},
					Enabled:      true,
				},
			},
		},
		Authorization: AuthorizationConfig{
			Enabled: true,
			Roles: map[string]Role{
				"admin": {
					Name: "admin",
					Permissions: []Permission{
						{Resource: "*", Action: "*"},
					},
				},
				"viewer": {
					Name: "viewer",
					Permissions: []Permission{
						{Resource: "health", Action: "read"},
						{Resource: "metrics", Action: "read"},
					},
				},
			},
		},
	}

	logger := logrus.New()
	securityManager, err := NewSecurityManager(config, logger)
	require.NoError(t, err)

	// Test admin access
	adminCtx := &AuthContext{
		Username:      "admin",
		Roles:         []string{"admin"},
		Authenticated: true,
	}

	hasPermission := securityManager.HasPermission(adminCtx, "config", "write")
	assert.True(t, hasPermission)

	// Test viewer access
	viewerCtx := &AuthContext{
		Username:      "viewer",
		Roles:         []string{"viewer"},
		Authenticated: true,
	}

	hasPermission = securityManager.HasPermission(viewerCtx, "health", "read")
	assert.True(t, hasPermission)

	hasPermission = securityManager.HasPermission(viewerCtx, "config", "write")
	assert.False(t, hasPermission)
}

// TestConcurrentAuthentication tests concurrent authentication requests
func TestConcurrentAuthentication(t *testing.T) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"testuser": {
				Username:     "testuser",
				PasswordHash: hashPassword("testpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    10,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	// Test concurrent authentication
	numGoroutines := 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/test", nil)
			req.SetBasicAuth("testuser", "testpass")

			authCtx, err := authManager.Authenticate(req)
			if err != nil {
				results <- err
				return
			}
			if !authCtx.Authenticated {
				results <- assert.AnError
				return
			}
			results <- nil
		}()
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

// Helper function to hash passwords for testing
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// BenchmarkAuthentication benchmarks authentication performance
func BenchmarkAuthentication(b *testing.B) {
	config := AuthConfig{
		Enabled: true,
		Method:  "basic",
		Users: map[string]User{
			"benchuser": {
				Username:     "benchuser",
				PasswordHash: hashPassword("benchpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    100,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req.SetBasicAuth("benchuser", "benchpass")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authCtx, err := authManager.Authenticate(req)
		if err != nil {
			b.Fatal(err)
		}
		if !authCtx.Authenticated {
			b.Fatal("authentication failed")
		}
	}
}

// BenchmarkTokenAuthentication benchmarks token authentication
func BenchmarkTokenAuthentication(b *testing.B) {
	config := AuthConfig{
		Enabled: true,
		Method:  "token",
		Tokens: map[string]string{
			"bench-token": "benchuser",
		},
		Users: map[string]User{
			"benchuser": {
				Username:     "benchuser",
				PasswordHash: hashPassword("benchpass"),
				Roles:        []string{"admin"},
				Enabled:      true,
			},
		},
		SessionTimeout: 24 * time.Hour,
		MaxAttempts:    100,
		LockoutTime:    15 * time.Minute,
	}

	logger := logrus.New()
	authManager := NewAuthManager(config, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer bench-token")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authCtx, err := authManager.Authenticate(req)
		if err != nil {
			b.Fatal(err)
		}
		if !authCtx.Authenticated {
			b.Fatal("authentication failed")
		}
	}
}