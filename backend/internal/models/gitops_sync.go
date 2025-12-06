package models

import (
	"time"
)

type GitOpsSync struct {
	Name           string     `json:"name" sortable:"true"`
	RepositoryID   string     `json:"repositoryId" sortable:"true"`
	Repository     *GitRepository `json:"repository,omitempty" gorm:"foreignKey:RepositoryID"`
	Branch         string     `json:"branch" sortable:"true"`
	ComposePath    string     `json:"composePath" sortable:"true"`
	ProjectID      string     `json:"projectId" sortable:"true"`
	Project        *Project   `json:"project,omitempty" gorm:"foreignKey:ProjectID"`
	AutoSync       bool       `json:"autoSync" sortable:"true"`
	SyncInterval   int        `json:"syncInterval" sortable:"true"` // in minutes
	LastSyncAt     *time.Time `json:"lastSyncAt,omitempty" sortable:"true"`
	LastSyncStatus *string    `json:"lastSyncStatus,omitempty"`
	LastSyncError  *string    `json:"lastSyncError,omitempty"`
	Enabled        bool       `json:"enabled" sortable:"true"`
	CreatedAt      time.Time  `json:"createdAt" sortable:"true"`
	UpdatedAt      time.Time  `json:"updatedAt" sortable:"true"`
	BaseModel
}

func (GitOpsSync) TableName() string {
	return "gitops_syncs"
}

type CreateGitOpsSyncRequest struct {
	Name         string `json:"name" binding:"required"`
	RepositoryID string `json:"repositoryId" binding:"required"`
	Branch       string `json:"branch" binding:"required"`
	ComposePath  string `json:"composePath" binding:"required"`
	ProjectID    string `json:"projectId" binding:"required"`
	AutoSync     *bool  `json:"autoSync"`
	SyncInterval *int   `json:"syncInterval"`
	Enabled      *bool  `json:"enabled"`
}

type UpdateGitOpsSyncRequest struct {
	Name         *string `json:"name"`
	RepositoryID *string `json:"repositoryId"`
	Branch       *string `json:"branch"`
	ComposePath  *string `json:"composePath"`
	ProjectID    *string `json:"projectId"`
	AutoSync     *bool   `json:"autoSync"`
	SyncInterval *int    `json:"syncInterval"`
	Enabled      *bool   `json:"enabled"`
}
