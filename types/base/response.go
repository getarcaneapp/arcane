package base

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error" doc:"Error message describing what went wrong"`
}

// MessageResponse represents a simple message response.
type MessageResponse struct {
	Message string `json:"message" doc:"Response message"`
}

// PaginationResponse contains pagination metadata.
type PaginationResponse struct {
	TotalPages      int64 `json:"totalPages" doc:"Total number of pages"`
	TotalItems      int64 `json:"totalItems" doc:"Total number of items in the current filtered set"`
	CurrentPage     int   `json:"currentPage" doc:"Current page number (1-indexed)"`
	ItemsPerPage    int   `json:"itemsPerPage" doc:"Number of items per page"`
	GrandTotalItems int64 `json:"grandTotalItems,omitempty" doc:"Total number of items without filters"`
}

// ApiResponse is a generic wrapper for API responses.
type ApiResponse[T any] struct {
	Success bool `json:"success" doc:"Whether the request was successful"`
	Data    T    `json:"data" doc:"Response data"`
}

// Paginated is a generic wrapper for paginated responses.
type Paginated[T any] struct {
	Success    bool               `json:"success" doc:"Whether the request was successful"`
	Data       []T                `json:"data" doc:"Array of items for the current page"`
	Pagination PaginationResponse `json:"pagination" doc:"Pagination metadata"`
}
