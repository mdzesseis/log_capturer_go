package security

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"ssw-logs-capture/pkg/errors"

	"github.com/sirupsen/logrus"
)

// AuthManager handles authentication and authorization
type AuthManager struct {
	config AuthConfig
	logger *logrus.Logger
}

// AuthConfig configures authentication
type AuthConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Method         string            `yaml:"method"` // "basic", "token", "jwt"
	Users          map[string]User   `yaml:"users"`
	Tokens         map[string]string `yaml:"tokens"` // token -> username
	JWTSecret      string            `yaml:"jwt_secret"`
	SessionTimeout time.Duration     `yaml:"session_timeout"`
	MaxAttempts    int               `yaml:"max_attempts"`
	LockoutTime    time.Duration     `yaml:"lockout_time"`
}

// User represents a user account
type User struct {
	Username     string   `yaml:"username"`
	PasswordHash string   `yaml:"password_hash"`
	Roles        []string `yaml:"roles"`
	Enabled      bool     `yaml:"enabled"`
}

// Permission represents an authorization permission
type Permission struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

// Role represents a user role with permissions
type Role struct {
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions"`
}

// AuthContext contains authentication information
type AuthContext struct {
	Username      string    `json:"username"`
	Roles         []string  `json:"roles"`
	Authenticated bool      `json:"authenticated"`
	LoginTime     time.Time `json:"login_time"`
	LastActivity  time.Time `json:"last_activity"`
}

// FailedAttempt tracks failed authentication attempts
type FailedAttempt struct {
	Count     int       `json:"count"`
	LastTry   time.Time `json:"last_try"`
	LockedUntil time.Time `json:"locked_until"`
}

// NewAuthManager creates a new authentication manager
func NewAuthManager(config AuthConfig, logger *logrus.Logger) *AuthManager {
	return &AuthManager{
		config: config,
		logger: logger,
	}
}

// Authenticate validates credentials and returns auth context
func (am *AuthManager) Authenticate(req *http.Request) (*AuthContext, error) {
	if !am.config.Enabled {
		return &AuthContext{Authenticated: true, Username: "anonymous"}, nil
	}

	switch am.config.Method {
	case "basic":
		return am.authenticateBasic(req)
	case "token":
		return am.authenticateToken(req)
	case "jwt":
		return am.authenticateJWT(req)
	default:
		return nil, errors.SecurityError("authenticate", "unsupported authentication method")
	}
}

// authenticateBasic handles HTTP Basic authentication
func (am *AuthManager) authenticateBasic(req *http.Request) (*AuthContext, error) {
	username, password, ok := req.BasicAuth()
	if !ok {
		return nil, errors.SecurityError("authenticate_basic", "basic auth credentials missing")
	}

	// Check rate limiting
	if err := am.checkRateLimit(username); err != nil {
		return nil, err
	}

	// Validate credentials
	user, exists := am.config.Users[username]
	if !exists || !user.Enabled {
		am.recordFailedAttempt(username)
		return nil, errors.SecurityError("authenticate_basic", "invalid credentials")
	}

	// Verify password
	if !am.verifyPassword(password, user.PasswordHash) {
		am.recordFailedAttempt(username)
		return nil, errors.SecurityError("authenticate_basic", "invalid credentials")
	}

	// Reset failed attempts on successful login
	am.resetFailedAttempts(username)

	return &AuthContext{
		Username:      username,
		Roles:         user.Roles,
		Authenticated: true,
		LoginTime:     time.Now(),
		LastActivity:  time.Now(),
	}, nil
}

// authenticateToken handles token-based authentication
func (am *AuthManager) authenticateToken(req *http.Request) (*AuthContext, error) {
	token := am.extractToken(req)
	if token == "" {
		return nil, errors.SecurityError("authenticate_token", "token missing")
	}

	username, exists := am.config.Tokens[token]
	if !exists {
		return nil, errors.SecurityError("authenticate_token", "invalid token")
	}

	user, exists := am.config.Users[username]
	if !exists || !user.Enabled {
		return nil, errors.SecurityError("authenticate_token", "user not found or disabled")
	}

	return &AuthContext{
		Username:      username,
		Roles:         user.Roles,
		Authenticated: true,
		LoginTime:     time.Now(),
		LastActivity:  time.Now(),
	}, nil
}

// authenticateJWT handles JWT authentication (placeholder)
func (am *AuthManager) authenticateJWT(req *http.Request) (*AuthContext, error) {
	// JWT implementation would go here
	return nil, errors.SecurityError("authenticate_jwt", "JWT authentication not implemented")
}

// Authorize checks if the user has permission for the requested action
func (am *AuthManager) Authorize(authCtx *AuthContext, resource, action string) error {
	if !am.config.Enabled || !authCtx.Authenticated {
		return errors.SecurityError("authorize", "user not authenticated")
	}

	// Anonymous user has limited permissions
	if authCtx.Username == "anonymous" {
		if resource == "health" && action == "read" {
			return nil
		}
		return errors.SecurityError("authorize", "anonymous access denied")
	}

	// Check role-based permissions
	for _, role := range authCtx.Roles {
		if am.checkRolePermission(role, resource, action) {
			return nil
		}
	}

	am.logger.WithFields(logrus.Fields{
		"username": authCtx.Username,
		"resource": resource,
		"action":   action,
		"roles":    authCtx.Roles,
	}).Warn("Authorization denied")

	return errors.SecurityError("authorize", "insufficient permissions").
		WithMetadata("resource", resource).
		WithMetadata("action", action)
}

// checkRolePermission checks if a role has permission for resource/action
func (am *AuthManager) checkRolePermission(role, resource, action string) bool {
	// Define role permissions
	rolePermissions := map[string][]Permission{
		"admin": {
			{Resource: "*", Action: "*"},
		},
		"operator": {
			{Resource: "health", Action: "read"},
			{Resource: "metrics", Action: "read"},
			{Resource: "status", Action: "read"},
			{Resource: "config", Action: "read"},
			{Resource: "logs", Action: "read"},
		},
		"viewer": {
			{Resource: "health", Action: "read"},
			{Resource: "metrics", Action: "read"},
			{Resource: "status", Action: "read"},
		},
	}

	permissions, exists := rolePermissions[role]
	if !exists {
		return false
	}

	for _, perm := range permissions {
		if (perm.Resource == "*" || perm.Resource == resource) &&
		   (perm.Action == "*" || perm.Action == action) {
			return true
		}
	}

	return false
}

// AuthMiddleware creates HTTP middleware for authentication/authorization
func (am *AuthManager) AuthMiddleware(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Authenticate
			authCtx, err := am.Authenticate(r)
			if err != nil {
				am.logger.WithError(err).WithField("remote_addr", r.RemoteAddr).Warn("Authentication failed")
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Authorize
			if err := am.Authorize(authCtx, resource, action); err != nil {
				am.logger.WithError(err).WithFields(logrus.Fields{
					"username":    authCtx.Username,
					"remote_addr": r.RemoteAddr,
					"resource":    resource,
					"action":      action,
				}).Warn("Authorization failed")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Add auth context to request
			ctx := context.WithValue(r.Context(), "auth", authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractToken extracts token from Authorization header
func (am *AuthManager) extractToken(req *http.Request) string {
	auth := req.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	// Handle "Bearer <token>" format
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Handle "Token <token>" format
	if strings.HasPrefix(auth, "Token ") {
		return strings.TrimPrefix(auth, "Token ")
	}

	return auth
}

// verifyPassword verifies password against hash
func (am *AuthManager) verifyPassword(password, hash string) bool {
	// Simple SHA256 hash verification (in production, use bcrypt)
	h := sha256.New()
	h.Write([]byte(password))
	computed := hex.EncodeToString(h.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

// HashPassword creates a password hash
func HashPassword(password string) string {
	h := sha256.New()
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

// Rate limiting for failed attempts
var failedAttempts = make(map[string]*FailedAttempt)

// checkRateLimit checks if user is rate limited
func (am *AuthManager) checkRateLimit(username string) error {
	attempt, exists := failedAttempts[username]
	if !exists {
		return nil
	}

	// Check if user is locked out
	if time.Now().Before(attempt.LockedUntil) {
		return errors.SecurityError("rate_limit", "account temporarily locked").
			WithMetadata("unlock_time", attempt.LockedUntil.Format(time.RFC3339))
	}

	return nil
}

// recordFailedAttempt records a failed authentication attempt
func (am *AuthManager) recordFailedAttempt(username string) {
	now := time.Now()

	attempt, exists := failedAttempts[username]
	if !exists {
		attempt = &FailedAttempt{}
		failedAttempts[username] = attempt
	}

	attempt.Count++
	attempt.LastTry = now

	// Lock account if too many attempts
	if attempt.Count >= am.config.MaxAttempts {
		attempt.LockedUntil = now.Add(am.config.LockoutTime)
		am.logger.WithFields(logrus.Fields{
			"username":     username,
			"attempts":     attempt.Count,
			"locked_until": attempt.LockedUntil,
		}).Warn("Account locked due to failed attempts")
	}
}

// resetFailedAttempts resets failed attempt counter
func (am *AuthManager) resetFailedAttempts(username string) {
	delete(failedAttempts, username)
}

// GetAuthContext extracts auth context from request
func GetAuthContext(r *http.Request) *AuthContext {
	if authCtx := r.Context().Value("auth"); authCtx != nil {
		if ctx, ok := authCtx.(*AuthContext); ok {
			return ctx
		}
	}
	return &AuthContext{Authenticated: false}
}

// AuditLogger logs security events
type AuditLogger struct {
	logger *logrus.Logger
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(logger *logrus.Logger) *AuditLogger {
	return &AuditLogger{logger: logger}
}

// LogAuthEvent logs authentication events
func (al *AuditLogger) LogAuthEvent(event string, username, remoteAddr string, success bool, metadata map[string]interface{}) {
	fields := logrus.Fields{
		"event":       event,
		"username":    username,
		"remote_addr": remoteAddr,
		"success":     success,
		"timestamp":   time.Now(),
	}

	for k, v := range metadata {
		fields[k] = v
	}

	if success {
		al.logger.WithFields(fields).Info("Security event")
	} else {
		al.logger.WithFields(fields).Warn("Security event failed")
	}
}

// LogAccessEvent logs access events
func (al *AuditLogger) LogAccessEvent(username, resource, action, remoteAddr string, allowed bool) {
	al.logger.WithFields(logrus.Fields{
		"event":       "access_control",
		"username":    username,
		"resource":    resource,
		"action":      action,
		"remote_addr": remoteAddr,
		"allowed":     allowed,
		"timestamp":   time.Now(),
	}).Info("Access control event")
}