package swarm

import (
	"time"

	"github.com/moby/moby/api/types/swarm"
)

type RuntimeStatus struct {
	Enabled bool `json:"enabled"`
}

type SwarmInfo struct {
	// ID is the swarm ID.
	//
	// Required: true
	ID string `json:"id"`

	// CreatedAt is when the swarm was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the swarm was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// Spec is the swarm specification.
	//
	// Required: true
	Spec swarm.Spec `json:"spec"`

	// RootRotationInProgress indicates if a root rotation is in progress.
	//
	// Required: true
	RootRotationInProgress bool `json:"rootRotationInProgress"`
}

// NewSwarmInfo converts a Docker swarm inspection result into the API-facing SwarmInfo shape.
//
// It copies the cluster identifiers, timestamps, spec, and root-rotation state
// from the Docker SDK type without mutating the source value.
//
// s is the Docker swarm value returned by the Docker client.
//
// Returns the serialized swarm metadata used by the Arcane API.
func NewSwarmInfo(s swarm.Swarm) SwarmInfo {
	return SwarmInfo{
		ID:                     s.ID,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
		Spec:                   s.Spec,
		RootRotationInProgress: s.RootRotationInProgress,
	}
}
