package types

import (
	"time"
)

type (
	EnvVariable struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	EnvSummary struct {
		Variables []EnvVariable `json:"variables"`
	}

	// GlobalVariable is the manager-level variable resource. Value is empty on
	// reads when IsSecret is true.
	GlobalVariable struct {
		CreatedAt       time.Time  `json:"createdAt" readOnly:"true"`
		UpdatedAt       *time.Time `json:"updatedAt,omitempty" readOnly:"true"`
		ID              string     `json:"id" readOnly:"true"`
		Key             string     `json:"key" required:"false"`
		Value           string     `json:"value" required:"false"`
		EnvironmentIDs  []string   `json:"environmentIds" required:"false"`
		IsSecret        bool       `json:"isSecret" required:"false"`
		AllEnvironments bool       `json:"allEnvironments" required:"false"`
	}

	EnvironmentSyncStatus struct {
		LastSyncedAt    *time.Time `json:"lastSyncedAt,omitempty"`
		EnvironmentID   string     `json:"environmentId"`
		EnvironmentName string     `json:"environmentName,omitempty"`
		Status          string     `json:"status"` // "synced" | "pending" | "error"
		Error           string     `json:"error,omitempty"`
	}

	GlobalVariableMutationResponse struct {
		Variable    *GlobalVariable         `json:"variable,omitempty"`
		SyncResults []EnvironmentSyncStatus `json:"syncResults,omitempty"`
	}
)
