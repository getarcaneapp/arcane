package event

import "time"

type Response struct {
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
	Metadata map[string]interface{} `json:"metadata,omitempty"`

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

type Create struct {
	// Type of the event.
	//
	// Required: true
	Type string `json:"type" binding:"required"`

	// Severity level of the event.
	//
	// Required: false
	Severity string `json:"severity,omitempty"`

	// Title of the event.
	//
	// Required: true
	Title string `json:"title" binding:"required"`

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
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type ListReponse struct {
	// Events is a list of events.
	//
	// Required: true
	Events []Response `json:"events"`

	// Total number of events.
	//
	// Required: true
	Total int `json:"total"`

	// Page number of the current result set.
	//
	// Required: true
	Page int `json:"page"`

	// PageSize is the number of events per page.
	//
	// Required: true
	PageSize int `json:"pageSize"`

	// TotalPages is the total number of pages.
	//
	// Required: true
	TotalPages int `json:"totalPages"`

	// HasNext indicates if there is a next page.
	//
	// Required: true
	HasNext bool `json:"hasNext"`

	// HasPrevious indicates if there is a previous page.
	//
	// Required: true
	HasPrevious bool `json:"hasPrevious"`
}
