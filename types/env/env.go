package env

import "time"

type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Summary struct {
	Variables []Variable `json:"variables"`
}

// GlobalVariable is the manager-level variable resource. Value is empty on
// reads when IsSecret is true.
type GlobalVariable struct {
	ID              string     `json:"id"`
	Key             string     `json:"key"`
	Value           string     `json:"value"`
	IsSecret        bool       `json:"isSecret"`
	AllEnvironments bool       `json:"allEnvironments"`
	EnvironmentIDs  []string   `json:"environmentIds"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

type CreateGlobalVariableRequest struct {
	Key             string   `json:"key"`
	Value           string   `json:"value"`
	IsSecret        bool     `json:"isSecret,omitempty"`
	AllEnvironments bool     `json:"allEnvironments,omitempty"`
	EnvironmentIDs  []string `json:"environmentIds,omitempty"`
}

// UpdateGlobalVariableRequest updates a variable; nil fields keep the current
// value. A nil Value on a secret variable keeps the stored ciphertext.
type UpdateGlobalVariableRequest struct {
	Key             *string   `json:"key,omitempty"`
	Value           *string   `json:"value,omitempty"`
	IsSecret        *bool     `json:"isSecret,omitempty"`
	AllEnvironments *bool     `json:"allEnvironments,omitempty"`
	EnvironmentIDs  *[]string `json:"environmentIds,omitempty"`
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
