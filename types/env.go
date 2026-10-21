package types

import (
	"time"
)

type EnvVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type EnvSummary struct {
	Variables []EnvVariable `json:"variables"`
}

// GlobalVariable is the manager-level variable resource. Value is empty on
// reads when IsSecret is true.
type GlobalVariable struct {
	ID              string     `json:"id" readOnly:"true"`
	Key             string     `json:"key" required:"false"`
	Value           string     `json:"value" required:"false"`
	IsSecret        bool       `json:"isSecret" required:"false"`
	AllEnvironments bool       `json:"allEnvironments" required:"false"`
	EnvironmentIDs  []string   `json:"environmentIds" required:"false"`
	CreatedAt       time.Time  `json:"createdAt" readOnly:"true"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
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
