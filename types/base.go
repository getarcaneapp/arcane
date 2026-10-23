package types

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

type EnvironmentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type EnvironmentRef struct {
	ID   string `json:"id" doc:"Environment ID"`
	Name string `json:"name" doc:"Environment display name"`
}

type EnvironmentData[T any] struct {
	Environment EnvironmentRef `json:"environment" doc:"Environment the data came from"`
	Data        T              `json:"data" doc:"Data reported by the environment"`
}

type EnvironmentFailure struct {
	Environment EnvironmentRef `json:"environment" doc:"Environment that failed"`
	Message     string         `json:"message" doc:"Why the environment's results are missing"`
}

//nolint:recvcheck // Rows read the tag by value and set it through a pointer.
type EnvironmentTag struct {
	EnvironmentID   string `json:"environmentId,omitempty" doc:"Environment the row came from in a multi-environment list"`
	EnvironmentName string `json:"environmentName,omitempty" doc:"Display name of the environment the row came from"`
}

func (t EnvironmentTag) Environment() EnvironmentRef {
	return EnvironmentRef{ID: t.EnvironmentID, Name: t.EnvironmentName}
}

func (t *EnvironmentTag) SetEnvironment(ref EnvironmentRef) {
	t.EnvironmentID, t.EnvironmentName = ref.ID, ref.Name
}

// FieldError identifies the input field associated with an error.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }

func (e *FieldError) Unwrap() error { return e.Err }

// JsonObject is a map-based representation of a JSON object that can be stored
// in a database. It implements the sql.Valuer and sql.Scanner interfaces for
// seamless database integration.
//
//nolint:recvcheck
type JsonObject map[string]any

// Value implements the driver.Valuer interface for database storage.
// It marshals the JsonObject to JSON bytes for database insertion.
// If the JsonObject is nil, it returns nil.
func (j JsonObject) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface for database retrieval.
// It unmarshals JSON data from the database into the JsonObject.
// Supports scanning from []byte or string values.
// If the value is nil, the JsonObject is set to nil.
func (j *JsonObject) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	default:
		return json.Unmarshal(nil, j)
	}
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error" doc:"Error message describing what went wrong"`
}

// MessageResponse represents a simple message response.
type MessageResponse struct {
	Message    string  `json:"message" doc:"Response message"`
	ActivityID *string `json:"activityId,omitempty" doc:"Background activity ID tracking this action"`
}

// PaginationResponse contains pagination metadata.
type PaginationResponse struct {
	TotalPages      int64 `json:"totalPages" doc:"Total number of pages"`
	TotalItems      int64 `json:"totalItems" doc:"Total number of items in the current filtered set"`
	CurrentPage     int   `json:"currentPage" doc:"Current page number (1-indexed)"`
	ItemsPerPage    int   `json:"itemsPerPage" doc:"Number of items per page"`
	GrandTotalItems int64 `json:"grandTotalItems,omitempty" doc:"Total number of items without filters"`
	HasNextPage     bool  `json:"hasNextPage" doc:"Whether another page exists after this one"`
}

// ApiResponse is a generic wrapper for API responses.
type ApiResponse[T any] struct {
	Success            bool                 `json:"success" doc:"Whether the request was successful"`
	Data               T                    `json:"data" doc:"Response data"`
	Environments       []EnvironmentData[T] `json:"environments,omitempty" doc:"Per-environment data behind a multi-environment response"`
	FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
}

// Paginated is a generic wrapper for paginated responses.
type Paginated[T any] struct {
	Success            bool                 `json:"success" doc:"Whether the request was successful"`
	Data               []T                  `json:"data" doc:"Array of items for the current page"`
	Pagination         PaginationResponse   `json:"pagination" doc:"Pagination metadata"`
	FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
}

// PaginatedWithWarnings is a generic wrapper for paginated responses with warnings.
type PaginatedWithWarnings[T any] struct {
	Success    bool               `json:"success"`
	Data       []T                `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
	Warnings   []string           `json:"warnings,omitempty"`
}

// PaginatedWithCounts is a generic wrapper for paginated responses with aggregate counts.
type PaginatedWithCounts[T, C any] struct {
	Success            bool                 `json:"success" doc:"Whether the request was successful"`
	Data               []T                  `json:"data" doc:"Array of items for the current page"`
	Counts             C                    `json:"counts" doc:"Aggregate counts for the filtered collection"`
	Pagination         PaginationResponse   `json:"pagination" doc:"Pagination metadata"`
	FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
}

type SubmittedBody struct {
	RawBody []byte
}

type Create[T any] struct {
	Body T
	SubmittedBody
}

type Update[T any] struct {
	Body T
	SubmittedBody
}

func (s SubmittedBody) SubmittedFields() (map[string]bool, error) {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(s.RawBody, &fields); err != nil {
		return nil, err
	}
	submitted := make(map[string]bool, len(fields))
	for name := range fields {
		submitted[name] = true
	}
	return submitted, nil
}
