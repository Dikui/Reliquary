package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode defines the error code type
type ErrorCode int

// System error codes
const (
	// Common error codes (1000-1999)
	ErrBadRequest         ErrorCode = 1000
	ErrUnauthorized       ErrorCode = 1001
	ErrForbidden          ErrorCode = 1002
	ErrNotFound           ErrorCode = 1003
	ErrMethodNotAllowed   ErrorCode = 1004
	ErrConflict           ErrorCode = 1005
	ErrTooManyRequests    ErrorCode = 1006
	ErrInternalServer     ErrorCode = 1007
	ErrServiceUnavailable ErrorCode = 1008
	ErrTimeout            ErrorCode = 1009
	ErrValidation         ErrorCode = 1010

	// Space related error codes (2000-2099)
	ErrSpaceNotFound         ErrorCode = 2000
	ErrSpaceAlreadyExists    ErrorCode = 2001
	ErrSpaceInactive         ErrorCode = 2002
	ErrSpaceNameRequired     ErrorCode = 2003
	ErrSpaceInvalidStatus    ErrorCode = 2004
	ErrSpaceCreationDisabled ErrorCode = 2005

	// Model lifecycle errors (2300-2399).
	// The dedicated code lets clients render structured usage details without
	// parsing the backwards-compatible English error message.
	ErrModelInUse ErrorCode = 2300

	ErrKnowledgeSearchUnavailable ErrorCode = 2400

	// Add more error codes here
)

// AppError defines the application error structure
type AppError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Details  any       `json:"details,omitempty"`
	HTTPCode int       `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	return fmt.Sprintf("error code: %d, error message: %s", e.Code, e.Message)
}

// WithDetails adds error details
func (e *AppError) WithDetails(details any) *AppError {
	e.Details = details
	return e
}

// NewBadRequestError creates a bad request error
func NewBadRequestError(message string) *AppError {
	return &AppError{
		Code:     ErrBadRequest,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// NewModelInUseError creates the typed HTTP 400 returned when active space
// resources still reference a model.
func NewModelInUseError(message string, details any) *AppError {
	return &AppError{
		Code:     ErrModelInUse,
		Message:  message,
		Details:  details,
		HTTPCode: http.StatusBadRequest,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string) *AppError {
	return &AppError{
		Code:     ErrUnauthorized,
		Message:  message,
		HTTPCode: http.StatusUnauthorized,
	}
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(message string) *AppError {
	return &AppError{
		Code:     ErrForbidden,
		Message:  message,
		HTTPCode: http.StatusForbidden,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(message string) *AppError {
	return &AppError{
		Code:     ErrNotFound,
		Message:  message,
		HTTPCode: http.StatusNotFound,
	}
}

// NewConflictError creates a conflict error
func NewConflictError(message string) *AppError {
	return &AppError{
		Code:     ErrConflict,
		Message:  message,
		HTTPCode: http.StatusConflict,
	}
}

// NewTooManyRequestsError creates a 429 error, used by quota-style
// guards (e.g. per-user self-service space creation cap).
func NewTooManyRequestsError(message string) *AppError {
	if message == "" {
		message = "too many requests"
	}
	return &AppError{
		Code:     ErrTooManyRequests,
		Message:  message,
		HTTPCode: http.StatusTooManyRequests,
	}
}

// NewInternalServerError creates an internal server error
func NewInternalServerError(message string) *AppError {
	if message == "" {
		message = "服务器内部错误"
	}
	return &AppError{
		Code:     ErrInternalServer,
		Message:  message,
		HTTPCode: http.StatusInternalServerError,
	}
}

// NewServiceUnavailableError creates a service unavailable (503)
// error. Used for transient failures where the caller can retry.
func NewServiceUnavailableError(message string) *AppError {
	if message == "" {
		message = "服务暂时不可用"
	}
	return &AppError{
		Code:     ErrServiceUnavailable,
		Message:  message,
		HTTPCode: http.StatusServiceUnavailable,
	}
}

// NewValidationError creates a validation error
func NewValidationError(message string) *AppError {
	return &AppError{
		Code:     ErrValidation,
		Message:  message,
		HTTPCode: http.StatusBadRequest,
	}
}

// Space related errors
func NewSpaceNotFoundError() *AppError {
	return &AppError{
		Code:     ErrSpaceNotFound,
		Message:  "空间不存在",
		HTTPCode: http.StatusNotFound,
	}
}

// NewSpaceAlreadyExistsError creates a space already exists error
func NewSpaceAlreadyExistsError() *AppError {
	return &AppError{
		Code:     ErrSpaceAlreadyExists,
		Message:  "空间已存在",
		HTTPCode: http.StatusConflict,
	}
}

// NewSpaceInactiveError creates a space inactive error
func NewSpaceInactiveError() *AppError {
	return &AppError{
		Code:     ErrSpaceInactive,
		Message:  "空间已停用",
		HTTPCode: http.StatusForbidden,
	}
}

// NewSpaceCreationDisabledError reports a deployment-policy denial for
// ordinary self-service space creation.
func NewSpaceCreationDisabledError() *AppError {
	return &AppError{
		Code:     ErrSpaceCreationDisabled,
		Message:  "only the personal workspace is available",
		HTTPCode: http.StatusForbidden,
	}
}

// IsAppError checks if the error is an AppError type
func IsAppError(err error) (*AppError, bool) {
	appErr, ok := err.(*AppError)
	return appErr, ok
}

// NewKnowledgeSearchUnavailableError hides dependency details from search clients.
func NewKnowledgeSearchUnavailableError() *AppError {
	return &AppError{Code: ErrKnowledgeSearchUnavailable, Message: "Knowledge search is temporarily unavailable. Please retry.", HTTPCode: http.StatusServiceUnavailable}
}
