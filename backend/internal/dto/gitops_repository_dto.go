package dto

import "time"

type GitOpsRepositoryDto struct {
	ID              string     `json:"id"`
	URL             string     `json:"url"`
	Branch          string     `json:"branch"`
	Username        string     `json:"username"`
	ComposePath     string     `json:"composePath"`
	Description     *string    `json:"description,omitempty"`
	AutoSync        bool       `json:"autoSync"`
	SyncInterval    int        `json:"syncInterval"`
	Enabled         bool       `json:"enabled"`
	LastSyncedAt    *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncStatus  *string    `json:"lastSyncStatus,omitempty"`
	LastSyncError   *string    `json:"lastSyncError,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type GitOpsRepositorySyncDto struct {
	ID              string     `json:"id" binding:"required"`
	URL             string     `json:"url" binding:"required"`
	Branch          string     `json:"branch" binding:"required"`
	Username        string     `json:"username"`
	Token           string     `json:"token"`
	ComposePath     string     `json:"composePath" binding:"required"`
	Description     *string    `json:"description,omitempty"`
	AutoSync        bool       `json:"autoSync"`
	SyncInterval    int        `json:"syncInterval"`
	Enabled         bool       `json:"enabled"`
	LastSyncedAt    *time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncStatus  *string    `json:"lastSyncStatus,omitempty"`
	LastSyncError   *string    `json:"lastSyncError,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type SyncGitOpsRepositoriesRequest struct {
	Repositories []GitOpsRepositorySyncDto `json:"repositories" binding:"required"`
}
