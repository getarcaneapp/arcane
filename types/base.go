package types

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

type (
	EnvironmentInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
	}

	EnvironmentRef struct {
		ID   string `json:"id" doc:"Environment ID"`
		Name string `json:"name" doc:"Environment display name"`
	}

	EnvironmentData[T any] struct {
		Data        T              `json:"data" doc:"Data reported by the environment"`
		Environment EnvironmentRef `json:"environment" doc:"Environment the data came from"`
	}

	EnvironmentFailure struct {
		Environment EnvironmentRef `json:"environment" doc:"Environment that failed"`
		Message     string         `json:"message" doc:"Why the environment's results are missing"`
	}

	//nolint:recvcheck // Rows read the tag by value and set it through a pointer.
	EnvironmentTag struct {
		EnvironmentID   string `json:"environmentId,omitempty" doc:"Environment the row came from in a multi-environment list"`
		EnvironmentName string `json:"environmentName,omitempty" doc:"Display name of the environment the row came from"`
	}

	// FieldError identifies the input field associated with an error.
	FieldError struct {
		Err   error
		Field string
	}

	// JsonObject is a map-based representation of a JSON object that can be stored
	// in a database. It implements the sql.Valuer and sql.Scanner interfaces for
	// seamless database integration.
	//
	//nolint:recvcheck
	JsonObject map[string]any

	// ErrorResponse represents an error response.
	ErrorResponse struct {
		Error string `json:"error" doc:"Error message describing what went wrong"`
	}

	// MessageResponse represents a simple message response.
	MessageResponse struct {
		ActivityID *string `json:"activityId,omitempty" doc:"Background activity ID tracking this action"`
		Message    string  `json:"message" doc:"Response message"`
	}

	// PaginationResponse contains pagination metadata.
	PaginationResponse struct {
		TotalPages      int64 `json:"totalPages" doc:"Total number of pages"`
		TotalItems      int64 `json:"totalItems" doc:"Total number of items in the current filtered set"`
		CurrentPage     int   `json:"currentPage" doc:"Current page number (1-indexed)"`
		ItemsPerPage    int   `json:"itemsPerPage" doc:"Number of items per page"`
		GrandTotalItems int64 `json:"grandTotalItems,omitempty" doc:"Total number of items without filters"`
		HasNextPage     bool  `json:"hasNextPage" doc:"Whether another page exists after this one"`
	}

	// ApiResponse is a generic wrapper for API responses.
	ApiResponse[T any] struct {
		Data               T                    `json:"data" doc:"Response data"`
		Environments       []EnvironmentData[T] `json:"environments,omitempty" doc:"Per-environment data behind a multi-environment response"`
		FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
		Success            bool                 `json:"success" doc:"Whether the request was successful"`
	}

	// Paginated is a generic wrapper for paginated responses.
	Paginated[T any] struct {
		Data               []T                  `json:"data" doc:"Array of items for the current page"`
		FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
		Pagination         PaginationResponse   `json:"pagination" doc:"Pagination metadata"`
		Success            bool                 `json:"success" doc:"Whether the request was successful"`
	}

	// PaginatedWithWarnings is a generic wrapper for paginated responses with warnings.
	PaginatedWithWarnings[T any] struct {
		Data       []T                `json:"data"`
		Warnings   []string           `json:"warnings,omitempty"`
		Pagination PaginationResponse `json:"pagination"`
		Success    bool               `json:"success"`
	}

	// PaginatedWithCounts is a generic wrapper for paginated responses with aggregate counts.
	PaginatedWithCounts[T, C any] struct {
		Counts             C                    `json:"counts" doc:"Aggregate counts for the filtered collection"`
		Data               []T                  `json:"data" doc:"Array of items for the current page"`
		FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
		Pagination         PaginationResponse   `json:"pagination" doc:"Pagination metadata"`
		Success            bool                 `json:"success" doc:"Whether the request was successful"`
	}

	SubmittedBody struct {
		RawBody []byte
	}

	Create[T any] struct {
		Body T
		SubmittedBody
	}

	Update[T any] struct {
		Body T
		SubmittedBody
	}
)

func (t EnvironmentTag) Environment() EnvironmentRef {
	return EnvironmentRef{ID: t.EnvironmentID, Name: t.EnvironmentName}
}

func (t *EnvironmentTag) SetEnvironment(ref EnvironmentRef) {
	t.EnvironmentID, t.EnvironmentName = ref.ID, ref.Name
}

func (e *FieldError) Error() string { return e.Err.Error() }

func (e *FieldError) Unwrap() error { return e.Err }

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
