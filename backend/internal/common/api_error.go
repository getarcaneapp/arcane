package common

import (
	"errors"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
)

type APIErrorCode string

const (
	APIErrorCodeBadRequest          APIErrorCode = "BAD_REQUEST"
	APIErrorCodeUnauthorized        APIErrorCode = "UNAUTHORIZED"
	APIErrorCodeForbidden           APIErrorCode = "FORBIDDEN"
	APIErrorCodeFeatureDisabled     APIErrorCode = "feature_disabled"
	APIErrorCodeNotFound            APIErrorCode = "NOT_FOUND"
	APIErrorCodeConflict            APIErrorCode = "CONFLICT"
	APIErrorCodeInternalServerError APIErrorCode = "INTERNAL_SERVER_ERROR"
	APIErrorCodeDockerAPIError      APIErrorCode = "DOCKER_API_ERROR"
	APIErrorCodeValidationError     APIErrorCode = "VALIDATION_ERROR"
	APIErrorCodeTimeout             APIErrorCode = "TIMEOUT"
)

type APIErrorResponse struct {
	Details any          `json:"details,omitempty"`
	Error   string       `json:"error"`
	Code    APIErrorCode `json:"code"`
	Success bool         `json:"success"`
}

type APISuccessResponse struct {
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Success bool   `json:"success"`
}

type APIError struct {
	Details    any          `json:"details,omitempty"`
	Message    string       `json:"message"`
	Code       APIErrorCode `json:"code"`
	StatusCode int          `json:"statusCode"`
}

func (e *APIError) Error() string {
	return e.Message
}

func (e *APIError) HTTPStatus() int {
	if e.StatusCode > 0 {
		return e.StatusCode
	}
	return http.StatusInternalServerError
}

// NewAPIError creates a new APIError
func NewAPIError(message string, code APIErrorCode, statusCode int) *APIError {
	return &APIError{
		Message:    message,
		Code:       code,
		StatusCode: statusCode,
	}
}

func NewAPIErrorWithDetails(message string, code APIErrorCode, statusCode int, details any) *APIError {
	return &APIError{
		Message:    message,
		Code:       code,
		StatusCode: statusCode,
		Details:    details,
	}
}

type DockerAPIError struct {
	Details    any
	Message    string
	StatusCode int
}

func (e *DockerAPIError) Error() string {
	return e.Message
}

func (e *DockerAPIError) HTTPStatus() int {
	if e.StatusCode > 0 {
		return e.StatusCode
	}
	return http.StatusInternalServerError
}

func ToAPIError(err error) *APIError {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr
	}
	if dockerAPIErr, ok := errors.AsType[*DockerAPIError](err); ok {
		return NewAPIErrorWithDetails(dockerAPIErr.Message, APIErrorCodeDockerAPIError, dockerAPIErr.HTTPStatus(), dockerAPIErr.Details)
	}

	switch {
	case errors.Is(err, ErrValidation):
		var details map[string]any
		if fieldErr, ok := errors.AsType[*types.FieldError](err); ok {
			details = map[string]any{"field": fieldErr.Field}
		}
		return NewAPIErrorWithDetails(err.Error(), APIErrorCodeValidationError, http.StatusBadRequest, details)
	case errors.Is(err, ErrBadRequest):
		return NewAPIError(err.Error(), APIErrorCodeBadRequest, http.StatusBadRequest)
	case errors.Is(err, ErrUnauthorized):
		return NewAPIError(err.Error(), APIErrorCodeUnauthorized, http.StatusUnauthorized)
	case errors.Is(err, ErrFeatureDisabled):
		return NewAPIError(err.Error(), APIErrorCodeFeatureDisabled, http.StatusForbidden)
	case errors.Is(err, ErrForbidden):
		return NewAPIError(err.Error(), APIErrorCodeForbidden, http.StatusForbidden)
	case errors.Is(err, ErrNotFound):
		return NewAPIError(err.Error(), APIErrorCodeNotFound, http.StatusNotFound)
	case errors.Is(err, ErrConflict):
		return NewAPIError(err.Error(), APIErrorCodeConflict, http.StatusConflict)
	case errors.Is(err, ErrTimeout):
		return NewAPIError(err.Error(), APIErrorCodeTimeout, http.StatusGatewayTimeout)
	case errors.Is(err, ErrUnavailable):
		return NewAPIError(err.Error(), APIErrorCodeInternalServerError, http.StatusServiceUnavailable)
	case errors.Is(err, ErrInsufficientStorage):
		return NewAPIError(err.Error(), APIErrorCodeInternalServerError, http.StatusInsufficientStorage)
	}

	return NewAPIError(err.Error(), APIErrorCodeInternalServerError, http.StatusInternalServerError)
}
