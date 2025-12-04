package base

// ErrorResponse represents an error response.
//
//	@Description	Error response with error message
type ErrorResponse struct {
	Error string `json:"error"`
}

// MessageResponse represents a simple message response.
//
//	@Description	Simple message response
type MessageResponse struct {
	Message string `json:"message"`
}

// PaginationResponse contains pagination metadata.
//
//	@Description	Pagination metadata
type PaginationResponse struct {
	TotalPages      int64 `json:"totalPages"`
	TotalItems      int64 `json:"totalItems"`
	CurrentPage     int   `json:"currentPage"`
	ItemsPerPage    int   `json:"itemsPerPage"`
	GrandTotalItems int64 `json:"grandTotalItems,omitempty"`
}

// ApiResponse is a generic wrapper for API responses.
//
//	@Description	Generic API response wrapper
type ApiResponse[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// Paginated is a generic wrapper for paginated responses.
//
//	@Description	Generic paginated response wrapper
type Paginated[T any] struct {
	Success    bool               `json:"success"`
	Data       []T                `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}
