package swarm

import (
	"time"
)

type TaskSummary struct {
	// ID is the unique identifier of the task.
	//
	// Required: true
	ID string `json:"id"`

	// Name is the task name.
	//
	// Required: true
	Name string `json:"name"`

	// ServiceID is the service ID the task belongs to.
	//
	// Required: true
	ServiceID string `json:"serviceId"`

	// ServiceName is the service name the task belongs to.
	//
	// Required: true
	ServiceName string `json:"serviceName"`

	// NodeID is the node ID running the task.
	//
	// Required: true
	NodeID string `json:"nodeId"`

	// NodeName is the node name running the task.
	//
	// Required: true
	NodeName string `json:"nodeName"`

	// DesiredState is the desired state for the task.
	//
	// Required: true
	DesiredState string `json:"desiredState"`

	// CurrentState is the current runtime state for the task.
	//
	// Required: true
	CurrentState string `json:"currentState"`

	// Error is any error message reported by the task.
	//
	// Required: false
	Error string `json:"error,omitempty"`

	// ContainerID is the container ID backing the task.
	//
	// Required: false
	ContainerID string `json:"containerId,omitempty"`

	// Image is the container image used by the task.
	//
	// Required: false
	Image string `json:"image,omitempty"`

	// Slot is the task slot for replicated services.
	//
	// Required: false
	Slot int `json:"slot,omitempty"`

	// CreatedAt is when the task was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the task was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}
