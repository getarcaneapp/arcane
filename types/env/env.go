package env

import "time"

type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Summary is the materialized variable set pushed to an environment.
// NoInjectKeys lists keys that are only available for ${VAR} interpolation.
// It is omitted when every key is injected so the payload stays readable by
// agents that predate the field, and an absent list means every key is injected.
type Summary struct {
	Variables    []Variable `json:"variables"`
	NoInjectKeys []string   `json:"noInjectKeys,omitempty"`
}

// GlobalVariable is the manager-level variable resource. Value is empty on
// reads when IsSecret is true.
type GlobalVariable struct {
	ID                   string     `json:"id"`
	Key                  string     `json:"key"`
	Value                string     `json:"value"`
	IsSecret             bool       `json:"isSecret"`
	AllEnvironments      bool       `json:"allEnvironments"`
	InjectIntoContainers bool       `json:"injectIntoContainers"`
	EnvironmentIDs       []string   `json:"environmentIds"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            *time.Time `json:"updatedAt,omitempty"`
}

type CreateGlobalVariableRequest struct {
	Key                  string   `json:"key"`
	Value                string   `json:"value"`
	IsSecret             bool     `json:"isSecret,omitempty"`
	AllEnvironments      bool     `json:"allEnvironments,omitempty"`
	InjectIntoContainers bool     `json:"injectIntoContainers,omitempty"`
	EnvironmentIDs       []string `json:"environmentIds,omitempty"`
}

// UpdateGlobalVariableRequest updates a variable; nil fields keep the current
// value. A nil Value on a secret variable keeps the stored ciphertext.
type UpdateGlobalVariableRequest struct {
	Key                  *string   `json:"key,omitzero"`
	Value                *string   `json:"value,omitzero"`
	IsSecret             *bool     `json:"isSecret,omitzero"`
	AllEnvironments      *bool     `json:"allEnvironments,omitzero"`
	InjectIntoContainers *bool     `json:"injectIntoContainers,omitzero"`
	EnvironmentIDs       *[]string `json:"environmentIds,omitzero"`
}

type EnvironmentSyncStatus struct {
	EnvironmentID   string     `json:"environmentId"`
	EnvironmentName string     `json:"environmentName,omitempty"`
	Status          string     `json:"status"` // "synced" | "pending" | "error"
	Error           string     `json:"error,omitempty"`
	LastSyncedAt    *time.Time `json:"lastSyncedAt,omitempty"`
}

type GlobalVariableMutationResponse struct {
	Variable    *GlobalVariable         `json:"variable,omitempty"`
	SyncResults []EnvironmentSyncStatus `json:"syncResults,omitempty"`
}
