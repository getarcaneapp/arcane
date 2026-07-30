package models

import (
	stderrors "errors"
	"net/http"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
)

type APIErrorCode string

const (
	APIErrorCodeBadRequest          APIErrorCode = "BAD_REQUEST"
	APIErrorCodeUnauthorized        APIErrorCode = "UNAUTHORIZED"
	APIErrorCodeForbidden           APIErrorCode = "FORBIDDEN"
	APIErrorCodeNotFound            APIErrorCode = "NOT_FOUND"
	APIErrorCodeConflict            APIErrorCode = "CONFLICT"
	APIErrorCodeInternalServerError APIErrorCode = "INTERNAL_SERVER_ERROR"
	APIErrorCodeDockerAPIError      APIErrorCode = "DOCKER_API_ERROR"
	APIErrorCodeValidationError     APIErrorCode = "VALIDATION_ERROR"
	APIErrorCodeTimeout             APIErrorCode = "TIMEOUT"
)

type APIErrorResponse struct {
	Success bool         `json:"success"`
	Error   string       `json:"error"`
	Code    APIErrorCode `json:"code"`
	Details any          `json:"details,omitempty"`
}

type APISuccessResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type APIError struct {
	Message    string       `json:"message"`
	Code       APIErrorCode `json:"code"`
	StatusCode int          `json:"statusCode"`
	Details    any          `json:"details,omitempty"`
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
	Message    string
	StatusCode int
	Details    any
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
	if apiErr, ok := stderrors.AsType[*APIError](err); ok {
		return apiErr
	}
	if dockerAPIErr, ok := stderrors.AsType[*DockerAPIError](err); ok {
		return NewAPIErrorWithDetails(dockerAPIErr.Message, APIErrorCodeDockerAPIError, dockerAPIErr.HTTPStatus(), dockerAPIErr.Details)
	}

	switch {
	case errors.Is(err, common.ErrValidation):
		details := make(map[string]any)
		keyValues := errors.GetDetails(err)
		for i := 0; i+1 < len(keyValues); i += 2 {
			key, ok := keyValues[i].(string)
			if ok {
				details[key] = keyValues[i+1]
			}
		}
		if len(details) == 0 {
			details = nil
		}
		return NewAPIErrorWithDetails(err.Error(), APIErrorCodeValidationError, http.StatusBadRequest, details)
	case errors.Is(err, common.ErrBadRequest):
		return NewAPIError(err.Error(), APIErrorCodeBadRequest, http.StatusBadRequest)
	case errors.Is(err, common.ErrUnauthorized):
		return NewAPIError(err.Error(), APIErrorCodeUnauthorized, http.StatusUnauthorized)
	case errors.Is(err, common.ErrForbidden):
		return NewAPIError(err.Error(), APIErrorCodeForbidden, http.StatusForbidden)
	case errors.Is(err, common.ErrNotFound):
		return NewAPIError(err.Error(), APIErrorCodeNotFound, http.StatusNotFound)
	case errors.Is(err, common.ErrConflict):
		return NewAPIError(err.Error(), APIErrorCodeConflict, http.StatusConflict)
	case errors.Is(err, common.ErrTimeout):
		return NewAPIError(err.Error(), APIErrorCodeTimeout, http.StatusGatewayTimeout)
	case errors.Is(err, common.ErrUnavailable):
		return NewAPIError(err.Error(), APIErrorCodeInternalServerError, http.StatusServiceUnavailable)
	}

	return NewAPIError(err.Error(), APIErrorCodeInternalServerError, http.StatusInternalServerError)
}
