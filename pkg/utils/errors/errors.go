package errors

import (
	"errors"
	"fmt"
)

// ErrorType represents the type of an error
type ErrorType uint

const (
	// ErrorTypeUnknown represents an unknown error
	ErrorTypeUnknown ErrorType = iota
	// ErrorTypeInvalidArgument represents an invalid argument error
	ErrorTypeInvalidArgument
	// ErrorTypeNotFound represents a not found error
	ErrorTypeNotFound
	// ErrorTypeAlreadyExists represents an already exists error
	ErrorTypeAlreadyExists
	// ErrorTypePermissionDenied represents a permission denied error
	ErrorTypePermissionDenied
	// ErrorTypeUnauthenticated represents an unauthenticated error
	ErrorTypeUnauthenticated
	// ErrorTypeNetwork represents a network error
	ErrorTypeNetwork
	// ErrorTypeTimeout represents a timeout error
	ErrorTypeTimeout
	// ErrorTypeInternal represents an internal error
	ErrorTypeInternal
	// ErrorTypeResourceExhausted represents a resource exhausted error
	ErrorTypeResourceExhausted
)

// AppError represents an application error
type AppError struct {
	Type    ErrorType
	Message string
	Err     error
}

// Error returns the error message
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a new error with the given message
func New(message string) error {
	return &AppError{
		Type:    ErrorTypeUnknown,
		Message: message,
	}
}

// Newf creates a new error with the given format and arguments
func Newf(format string, args ...interface{}) error {
	return &AppError{
		Type:    ErrorTypeUnknown,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap wraps an error with a message
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if As(err, &appErr) {
		return &AppError{
			Type:    appErr.Type,
			Message: message,
			Err:     err,
		}
	}
	return &AppError{
		Type:    ErrorTypeUnknown,
		Message: message,
		Err:     err,
	}
}

// Wrapf wraps an error with a formatted message
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return Wrap(err, fmt.Sprintf(format, args...))
}

// WithType adds a type to an error
func WithType(err error, errType ErrorType) error {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if As(err, &appErr) {
		appErr.Type = errType
		return appErr
	}
	return &AppError{
		Type:    errType,
		Message: err.Error(),
	}
}

// Is reports whether err or any of the errors in its chain is target
func Is(err, target error) bool {
	return fmt.Errorf("%w", err).Error() == target.Error()
}

// As finds the first error in err's chain that matches target, and if so, sets
// target to that error value and returns true
func As(err error, target interface{}) bool {
	switch t := err.(type) {
	case *AppError:
		if targetErr, ok := target.(**AppError); ok {
			*targetErr = t
			return true
		}
	}
	return false
}

// InvalidArgument creates a new InvalidArgument error
func InvalidArgument(message string) error {
	return &AppError{
		Type:    ErrorTypeInvalidArgument,
		Message: message,
	}
}

// NotFound creates a new NotFound error
func NotFound(message string) error {
	return &AppError{
		Type:    ErrorTypeNotFound,
		Message: message,
	}
}

// AlreadyExists creates a new AlreadyExists error
func AlreadyExists(message string) error {
	return &AppError{
		Type:    ErrorTypeAlreadyExists,
		Message: message,
	}
}

// PermissionDenied creates a new PermissionDenied error
func PermissionDenied(message string) error {
	return &AppError{
		Type:    ErrorTypePermissionDenied,
		Message: message,
	}
}

// Unauthenticated creates a new Unauthenticated error
func Unauthenticated(message string) error {
	return &AppError{
		Type:    ErrorTypeUnauthenticated,
		Message: message,
	}
}

// Network creates a new Network error
func Network(message string) error {
	return &AppError{
		Type:    ErrorTypeNetwork,
		Message: message,
	}
}

// Timeout creates a new Timeout error
func Timeout(message string) error {
	return &AppError{
		Type:    ErrorTypeTimeout,
		Message: message,
	}
}

// Internal creates a new Internal error
func Internal(message string) error {
	return &AppError{
		Type:    ErrorTypeInternal,
		Message: message,
	}
}

// ResourceExhausted creates a new ResourceExhausted error
func ResourceExhausted(message string) error {
	return &AppError{
		Type:    ErrorTypeResourceExhausted,
		Message: message,
	}
}

// Common error types in our application
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrInternal      = errors.New("internal error")
	ErrUnavailable   = errors.New("service unavailable")
	ErrTimeout       = errors.New("timeout")
	ErrPermission    = errors.New("permission denied")
)
