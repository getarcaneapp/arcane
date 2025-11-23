package models

import (
	"time"
)

type GitOpsRepository struct {
	URL               string    `json:"url" sortable:"true"`
	Branch            string    `json:"branch" sortable:"true"`
	Username          string    `json:"username" sortable:"true"`
	Token             string    `json:"token"`
	ComposePath       string    `json:"composePath" sortable:"true"`
	Description       *string   `json:"description,omitempty" sortable:"true"`
	AutoSync          bool      `json:"autoSync" sortable:"true"`
	SyncInterval      int       `json:"syncInterval" sortable:"true"` // in minutes
	Enabled           bool      `json:"enabled" sortable:"true"`
	LastSyncedAt      *time.Time `json:"lastSyncedAt,omitempty" sortable:"true"`
	LastSyncStatus    *string   `json:"lastSyncStatus,omitempty"`
	LastSyncError     *string   `json:"lastSyncError,omitempty"`
	CreatedAt         time.Time `json:"createdAt" sortable:"true"`
	UpdatedAt         time.Time `json:"updatedAt" sortable:"true"`
	BaseModel
}

func (GitOpsRepository) TableName() string {
	return "gitops_repositories"
}

type CreateGitOpsRepositoryRequest struct {
	URL          string  `json:"url" binding:"required"`
	Branch       string  `json:"branch"`
	Username     string  `json:"username"`
	Token        string  `json:"token"`
	ComposePath  string  `json:"composePath" binding:"required"`
	Description  *string `json:"description"`
	AutoSync     *bool   `json:"autoSync"`
	SyncInterval *int    `json:"syncInterval"`
	Enabled      *bool   `json:"enabled"`
}

type UpdateGitOpsRepositoryRequest struct {
	URL          *string `json:"url"`
	Branch       *string `json:"branch"`
	Username     *string `json:"username"`
	Token        *string `json:"token"`
	ComposePath  *string `json:"composePath"`
	Description  *string `json:"description"`
	AutoSync     *bool   `json:"autoSync"`
	SyncInterval *int    `json:"syncInterval"`
	Enabled      *bool   `json:"enabled"`
}
