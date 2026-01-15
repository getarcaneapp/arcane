package swarm

import "time"

type StackSummary struct {
	// ID is the unique identifier for the stack (uses namespace).
	//
	// Required: true
	ID string `json:"id"`

	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// Namespace is the stack namespace label value.
	//
	// Required: true
	Namespace string `json:"namespace"`

	// Services is the number of services in the stack.
	//
	// Required: true
	Services int `json:"services"`

	// CreatedAt is the earliest service creation time in the stack.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the latest service update time in the stack.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}
