package swarm

import (
	"time"

	"github.com/docker/docker/api/types/swarm"
)

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

func NewSwarmInfo(s swarm.Swarm) SwarmInfo {
	return SwarmInfo{
		ID:                     s.ID,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
		Spec:                   s.Spec,
		RootRotationInProgress: s.RootRotationInProgress,
	}
}
