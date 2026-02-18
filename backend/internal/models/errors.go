package models

import (
	"errors"
	"net/http"
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

func NewNotFoundError(message string) *APIError {
	return NewAPIError(message, APIErrorCodeNotFound, http.StatusNotFound)
}

func NewConflictError(message string) *APIError {
	return NewAPIError(message, APIErrorCodeConflict, http.StatusConflict)
}

func NewInternalServerError(message string) *APIError {
	return NewAPIError(message, APIErrorCodeInternalServerError, http.StatusInternalServerError)
}

func NewValidationError(message string, details any) *APIError {
	return NewAPIErrorWithDetails(message, APIErrorCodeValidationError, http.StatusBadRequest, details)
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
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

type ValidationError struct {
	Message string
	Field   string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func ToAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	var notFoundErr *NotFoundError
	if errors.As(err, &notFoundErr) {
		return NewNotFoundError(notFoundErr.Message)
	}
	var conflictErr *ConflictError
	if errors.As(err, &conflictErr) {
		return NewConflictError(conflictErr.Message)
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return NewValidationError(validationErr.Message, map[string]string{"field": validationErr.Field})
	}
	var dockerAPIErr *DockerAPIError
	if errors.As(err, &dockerAPIErr) {
		return NewAPIErrorWithDetails(dockerAPIErr.Message, APIErrorCodeDockerAPIError, dockerAPIErr.HTTPStatus(), dockerAPIErr.Details)
	}
	return NewInternalServerError(err.Error())
}
