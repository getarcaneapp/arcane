package types

import (
	"time"
)

// EventSeverityCounts holds global event counts per severity.
type EventSeverityCounts struct {
	Total   int64 `json:"total"`
	Info    int64 `json:"info"`
	Success int64 `json:"success"`
	Warning int64 `json:"warning"`
	Error   int64 `json:"error"`
}

// Event represents an event in API responses.
type Event struct {
	// ID of the event.
	//
	// Required: true
	ID string `json:"id"`

	// Type of the event.
	//
	// Required: true
	Type string `json:"type"`

	// Severity level of the event.
	//
	// Required: true
	Severity string `json:"severity"`

	// Title of the event.
	//
	// Required: true
	Title string `json:"title"`

	// Description of the event.
	//
	// Required: false
	Description string `json:"description,omitempty"`

	// ResourceType is the type of the resource associated with the event.
	//
	// Required: false
	ResourceType *string `json:"resourceType,omitempty"`

	// ResourceID is the ID of the resource associated with the event.
	//
	// Required: false
	ResourceID *string `json:"resourceId,omitempty"`

	// ResourceName is the name of the resource associated with the event.
	//
	// Required: false
	ResourceName *string `json:"resourceName,omitempty"`

	// UserID is the ID of the user who triggered the event.
	//
	// Required: false
	UserID *string `json:"userId,omitempty"`

	// Username is the username of the user who triggered the event.
	//
	// Required: false
	Username *string `json:"username,omitempty"`

	// EnvironmentID is the ID of the environment associated with the event.
	//
	// Required: false
	EnvironmentID *string `json:"environmentId,omitempty"`

	// Metadata contains additional key-value data associated with the event.
	//
	// Required: false
	Metadata map[string]any `json:"metadata,omitempty"`

	// Timestamp is when the event occurred.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`

	// CreatedAt is the date and time at which the event was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the event was last updated.
	//
	// Required: false
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// EventStreamEvent invalidates event queries after persistence changes.
type EventStreamEvent struct {
	// Type is "changed"; clients reload their current event query.
	Type string `json:"type"`
	// Timestamp is when the invalidation was emitted.
	Timestamp time.Time            `json:"timestamp"`
	Changes   []EnvironmentChanges `json:"changes"`
}

type EnvironmentChanges struct {
	EnvironmentID string            `json:"environmentId"`
	Epoch         string            `json:"epoch"`
	Kinds         map[string]uint64 `json:"kinds"`
}
