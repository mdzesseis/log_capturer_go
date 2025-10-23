package errors

import (
	"fmt"
	"runtime"
	"time"
)

// AppError represents a standardized application error
type AppError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Component  string                 `json:"component"`
	Operation  string                 `json:"operation"`
	Cause      error                  `json:"cause,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Severity   Severity               `json:"severity"`
}

// Severity levels for errors
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Error codes
const (
	// Configuration errors
	CodeConfigInvalid    = "CONFIG_INVALID"
	CodeConfigNotFound   = "CONFIG_NOT_FOUND"
	CodeConfigValidation = "CONFIG_VALIDATION_FAILED"

	// Resource errors
	CodeResourceExhausted = "RESOURCE_EXHAUSTED"
	CodeResourceNotFound  = "RESOURCE_NOT_FOUND"
	CodeResourceLeak      = "RESOURCE_LEAK"

	// Processing errors
	CodeProcessingFailed  = "PROCESSING_FAILED"
	CodeProcessingTimeout = "PROCESSING_TIMEOUT"
	CodeProcessingInvalid = "PROCESSING_INVALID_DATA"

	// Network errors
	CodeNetworkTimeout     = "NETWORK_TIMEOUT"
	CodeNetworkUnavailable = "NETWORK_UNAVAILABLE"
	CodeNetworkRefused     = "NETWORK_CONNECTION_REFUSED"

	// Security errors
	CodeSecurityUnauthorized = "SECURITY_UNAUTHORIZED"
	CodeSecurityForbidden    = "SECURITY_FORBIDDEN"
	CodeSecurityValidation   = "SECURITY_VALIDATION_FAILED"

	// System errors
	CodeSystemOverload = "SYSTEM_OVERLOAD"
	CodeSystemFailure  = "SYSTEM_FAILURE"
	CodeSystemTimeout  = "SYSTEM_TIMEOUT"
)

// New creates a new standardized error
func New(code, component, operation, message string) *AppError {
	_, file, line, _ := runtime.Caller(1)

	return &AppError{
		Code:       code,
		Message:    message,
		Component:  component,
		Operation:  operation,
		StackTrace: fmt.Sprintf("%s:%d", file, line),
		Metadata:   make(map[string]interface{}),
		Timestamp:  time.Now(),
		Severity:   SeverityMedium, // Default severity
	}
}

// NewCritical creates a critical error
func NewCritical(code, component, operation, message string) *AppError {
	err := New(code, component, operation, message)
	err.Severity = SeverityCritical
	return err
}

// NewWithSeverity creates an error with specific severity
func NewWithSeverity(severity Severity, code, component, operation, message string) *AppError {
	err := New(code, component, operation, message)
	err.Severity = severity
	return err
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %s: %v", e.Component, e.Operation, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s: %s", e.Component, e.Operation, e.Code, e.Message)
}

// Wrap wraps another error as the cause
func (e *AppError) Wrap(cause error) *AppError {
	e.Cause = cause
	return e
}

// WithMetadata adds metadata to the error
func (e *AppError) WithMetadata(key string, value interface{}) *AppError {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithSeverity sets the severity level
func (e *AppError) WithSeverity(severity Severity) *AppError {
	e.Severity = severity
	return e
}

// IsCritical returns true if the error is critical
func (e *AppError) IsCritical() bool {
	return e.Severity == SeverityCritical
}

// IsRecoverable returns true if the error might be recoverable
func (e *AppError) IsRecoverable() bool {
	switch e.Severity {
	case SeverityCritical, SeverityHigh:
		return false
	default:
		return true
	}
}

// ToMap converts the error to a map for structured logging
func (e *AppError) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"error_code":      e.Code,
		"error_message":   e.Message,
		"error_component": e.Component,
		"error_operation": e.Operation,
		"error_severity":  string(e.Severity),
		"error_timestamp": e.Timestamp,
	}

	if e.StackTrace != "" {
		result["error_stack_trace"] = e.StackTrace
	}

	if e.Cause != nil {
		result["error_cause"] = e.Cause.Error()
	}

	for k, v := range e.Metadata {
		result[fmt.Sprintf("error_meta_%s", k)] = v
	}

	return result
}

// Convenience functions for common error types

// ConfigError creates a configuration error
func ConfigError(operation, message string) *AppError {
	return New(CodeConfigInvalid, "config", operation, message)
}

// ResourceError creates a resource error
func ResourceError(operation, message string) *AppError {
	return New(CodeResourceExhausted, "resource", operation, message)
}

// ProcessingError creates a processing error
func ProcessingError(operation, message string) *AppError {
	return New(CodeProcessingFailed, "processing", operation, message)
}

// NetworkError creates a network error
func NetworkError(operation, message string) *AppError {
	return New(CodeNetworkTimeout, "network", operation, message)
}

// SecurityError creates a security error
func SecurityError(operation, message string) *AppError {
	return NewCritical(CodeSecurityUnauthorized, "security", operation, message)
}

// SystemError creates a system error
func SystemError(operation, message string) *AppError {
	return NewCritical(CodeSystemFailure, "system", operation, message)
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// AsAppError converts an error to AppError if possible
func AsAppError(err error) (*AppError, bool) {
	appErr, ok := err.(*AppError)
	return appErr, ok
}

// WrapError wraps a standard error into an AppError
func WrapError(err error, component, operation, message string) *AppError {
	if err == nil {
		return nil
	}

	if appErr, ok := AsAppError(err); ok {
		return appErr
	}

	return New("WRAPPED_ERROR", component, operation, message).Wrap(err)
}