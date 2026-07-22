package swarm

import (
	"encoding/json"
	"time"

	"github.com/moby/moby/api/types/swarm"
)

type ConfigSummary struct {
	ID        string           `json:"id"`
	Version   swarm.Version    `json:"version"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Spec      swarm.ConfigSpec `json:"spec"`
}

type SecretSummary struct {
	ID        string           `json:"id"`
	Version   swarm.Version    `json:"version"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Spec      swarm.SecretSpec `json:"spec"`
}

type ConfigCreateRequest struct {
	Spec json.RawMessage `json:"spec" doc:"Config specification"`
}

type ConfigUpdateRequest struct {
	Version uint64          `json:"version,omitempty"`
	Spec    json.RawMessage `json:"spec" doc:"Updated config specification"`
}

type SecretCreateRequest struct {
	Spec json.RawMessage `json:"spec" doc:"Secret specification"`
}

type SecretUpdateRequest struct {
	Version uint64          `json:"version,omitempty"`
	Spec    json.RawMessage `json:"spec" doc:"Updated secret specification"`
}
