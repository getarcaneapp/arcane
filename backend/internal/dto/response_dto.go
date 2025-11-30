package dto

import "github.com/getarcaneapp/arcane/backend/internal/utils/pagination"

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// ApiResponse is a generic wrapper for API responses (used for swagger docs)
//
//	@Description	Generic API response wrapper
type ApiResponse[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// Paginated is a generic wrapper for paginated responses (used for swagger docs)
//
//	@Description	Generic paginated response wrapper
type Paginated[T any] struct {
	Success    bool                `json:"success"`
	Data       []T                 `json:"data"`
	Pagination pagination.Response `json:"pagination"`
}
