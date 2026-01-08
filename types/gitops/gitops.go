package gitops

import "time"

// GitRepository represents a reusable Git repository with credentials.
type GitRepository struct {
	// ID of the git repository.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the git repository.
	//
	// Required: true
	Name string `json:"name"`

	// URL of the git repository.
	//
	// Required: true
	URL string `json:"url"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: true
	AuthType string `json:"authType"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username string `json:"username,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// CreatedAt is the date and time at which the repository was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the repository was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

// GitOpsSync represents a GitOps sync configuration.
type GitOpsSync struct {
	// ID of the gitops sync.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the sync configuration.
	//
	// Required: true
	Name string `json:"name"`

	// EnvironmentID is the ID of the environment this sync belongs to.
	//
	// Required: true
	EnvironmentID string `json:"environmentId"`

	// RepositoryID is the ID of the git repository to sync from.
	//
	// Required: true
	RepositoryID string `json:"repositoryId"`

	// Repository is the associated git repository.
	//
	// Required: false
	Repository *GitRepository `json:"repository,omitempty"`

	// Branch to sync from.
	//
	// Required: true
	Branch string `json:"branch"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: true
	ComposePath string `json:"composePath"`

	// ProjectName is the name used to create/identify the project.
	//
	// Required: true
	ProjectName string `json:"projectName"`

	// ProjectID is the ID of the linked project (set after first sync).
	//
	// Required: false
	ProjectID *string `json:"projectId,omitempty"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: true
	AutoSync bool `json:"autoSync"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: true
	SyncInterval int `json:"syncInterval"`

	// LastSyncAt is the date and time of the last successful sync.
	//
	// Required: false
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`

	// LastSyncStatus is the status of the last sync attempt.
	//
	// Required: false
	LastSyncStatus *string `json:"lastSyncStatus,omitempty"`

	// LastSyncError is the error message from the last sync attempt if it failed.
	//
	// Required: false
	LastSyncError *string `json:"lastSyncError,omitempty"`

	// LastSyncCommit is the commit hash from the last successful sync.
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty"`

	// Enabled indicates if the sync is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// CreatedAt is the date and time at which the sync was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the sync was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateRepositoryRequest represents the request to create a git repository.
type CreateRepositoryRequest struct {
	// Name of the git repository.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// URL of the git repository.
	//
	// Required: true
	URL string `json:"url" binding:"required"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: true
	AuthType string `json:"authType" binding:"required"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username string `json:"username,omitempty"`

	// Token for HTTP authentication.
	//
	// Required: false
	Token string `json:"token,omitempty"`

	// SSHKey for SSH authentication.
	//
	// Required: false
	SSHKey string `json:"sshKey,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`
}

// UpdateRepositoryRequest represents the request to update a git repository.
type UpdateRepositoryRequest struct {
	// Name of the git repository.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// URL of the git repository.
	//
	// Required: false
	URL *string `json:"url,omitempty"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: false
	AuthType *string `json:"authType,omitempty"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username *string `json:"username,omitempty"`

	// Token for HTTP authentication.
	//
	// Required: false
	Token *string `json:"token,omitempty"`

	// SSHKey for SSH authentication.
	//
	// Required: false
	SSHKey *string `json:"sshKey,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`
}

// CreateSyncRequest represents the request to create a gitops sync.
type CreateSyncRequest struct {
	// Name of the sync configuration.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// RepositoryID is the ID of the git repository to sync from.
	//
	// Required: true
	RepositoryID string `json:"repositoryId" binding:"required"`

	// Branch to sync from.
	//
	// Required: true
	Branch string `json:"branch" binding:"required"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: true
	ComposePath string `json:"composePath" binding:"required"`

	// ProjectName is the name of the project to create/update.
	// The actual project will be created on first sync, and ProjectID will be set then.
	// If not provided, defaults to the sync name.
	//
	// Required: false
	ProjectName string `json:"projectName,omitempty"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: false
	AutoSync *bool `json:"autoSync,omitempty"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: false
	SyncInterval *int `json:"syncInterval,omitempty"`

	// Enabled indicates if the sync is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`
}

// UpdateSyncRequest represents the request to update a gitops sync.
type UpdateSyncRequest struct {
	// Name of the sync configuration.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// RepositoryID is the ID of the git repository to sync from.
	//
	// Required: false
	RepositoryID *string `json:"repositoryId,omitempty"`

	// Branch to sync from.
	//
	// Required: false
	Branch *string `json:"branch,omitempty"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: false
	ComposePath *string `json:"composePath,omitempty"`

	// ProjectName is the name of the project to create/update.
	//
	// Required: false
	ProjectName *string `json:"projectName,omitempty"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: false
	AutoSync *bool `json:"autoSync,omitempty"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: false
	SyncInterval *int `json:"syncInterval,omitempty"`

	// Enabled indicates if the sync is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitempty"`
}

// SyncResult represents the result of a sync operation.
type SyncResult struct {
	// Success indicates if the sync was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Message contains a human-readable message about the sync result.
	//
	// Required: true
	Message string `json:"message"`

	// Error contains error details if the sync failed.
	//
	// Required: false
	Error *string `json:"error,omitempty"`

	// SyncedAt is the timestamp of the sync.
	//
	// Required: true
	SyncedAt time.Time `json:"syncedAt"`
}

// FileTreeNodeType represents the type of a file tree node.
type FileTreeNodeType string

const (
	// FileTreeNodeTypeFile represents a file node.
	FileTreeNodeTypeFile FileTreeNodeType = "file"
	// FileTreeNodeTypeDirectory represents a directory node.
	FileTreeNodeTypeDirectory FileTreeNodeType = "directory"
)

// FileTreeNode represents a file or directory in the repository.
type FileTreeNode struct {
	// Name of the file or directory.
	//
	// Required: true
	Name string `json:"name"`

	// Path is the full path of the file or directory.
	//
	// Required: true
	Path string `json:"path"`

	// Type indicates if this is a file or directory (use FileTreeNodeTypeFile or FileTreeNodeTypeDirectory).
	//
	// Required: true
	Type FileTreeNodeType `json:"type"`

	// Size of the file in bytes (0 for directories).
	//
	// Required: false
	Size int64 `json:"size,omitempty"`

	// Children contains child nodes for directories.
	//
	// Required: false
	Children []FileTreeNode `json:"children,omitempty"`
}

// BrowseRequest represents a request to browse repository files.
type BrowseRequest struct {
	// Path to browse in the repository.
	//
	// Required: false
	Path string `json:"path,omitempty"`
}

// BrowseResponse represents the response for browsing repository files.
type BrowseResponse struct {
	// Path that was browsed.
	//
	// Required: true
	Path string `json:"path"`

	// Files and directories at the path.
	//
	// Required: true
	Files []FileTreeNode `json:"files"`
}

// BranchInfo represents information about a git branch.
type BranchInfo struct {
	// Name of the branch.
	//
	// Required: true
	Name string `json:"name"`

	// IsDefault indicates if this is the default branch.
	//
	// Required: true
	IsDefault bool `json:"isDefault"`
}

// BranchesResponse represents the response for listing repository branches.
type BranchesResponse struct {
	// Branches available in the repository.
	//
	// Required: true
	Branches []BranchInfo `json:"branches"`
}

// RepositorySync represents a git repository for syncing to remote environments.
type RepositorySync struct {
	// ID of the git repository.
	//
	// Required: true
	ID string `json:"id" binding:"required"`

	// Name of the git repository.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// URL of the git repository.
	//
	// Required: true
	URL string `json:"url" binding:"required"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: true
	AuthType string `json:"authType" binding:"required"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username string `json:"username,omitempty"`

	// Token for HTTP authentication (decrypted).
	//
	// Required: false
	Token string `json:"token,omitempty"`

	// SSHKey for SSH authentication (decrypted).
	//
	// Required: false
	SSHKey string `json:"sshKey,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// CreatedAt is the date and time at which the repository was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the repository was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

// RepositorySyncRequest represents a request to sync git repositories to an agent.
type RepositorySyncRequest struct {
	// Repositories is a list of git repositories to sync.
	//
	// Required: true
	Repositories []RepositorySync `json:"repositories" binding:"required"`
}

// SyncStatus represents the current status of a sync configuration.
type SyncStatus struct {
	// ID of the sync configuration.
	//
	// Required: true
	ID string `json:"id"`

	// Enabled indicates if the sync is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`

	// AutoSync indicates if automatic sync is enabled.
	//
	// Required: true
	AutoSync bool `json:"autoSync"`

	// NextSyncAt is the estimated time of the next automatic sync.
	//
	// Required: false
	NextSyncAt *time.Time `json:"nextSyncAt,omitempty"`

	// LastSyncAt is the time of the last sync.
	//
	// Required: false
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`

	// LastSyncStatus is the status of the last sync.
	//
	// Required: false
	LastSyncStatus *string `json:"lastSyncStatus,omitempty"`

	// LastSyncError is the error from the last sync if it failed.
	//
	// Required: false
	LastSyncError *string `json:"lastSyncError,omitempty"`

	// LastSyncCommit is the commit hash from the last successful sync.
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty"`
}

// ImportGitOpsSyncRequest represents the request to import gitops syncs.
type ImportGitOpsSyncRequest struct {
	// SyncName is the name of the sync configuration.
	//
	// Required: true
	SyncName string `json:"syncName"`

	// GitRepo is the repository identifier or URL.
	//
	// Required: true
	GitRepo string `json:"gitRepo"`

	// Branch to sync from.
	//
	// Required: true
	Branch string `json:"branch"`

	// DockerComposePath is the path to the docker-compose file.
	//
	// Required: true
	DockerComposePath string `json:"dockerComposePath"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: true
	AutoSync bool `json:"autoSync"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: true
	SyncInterval int `json:"syncInterval"`

	// Enabled indicates if the sync is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`
}

// ImportGitOpsSyncResponse represents the response for importing gitops syncs.
type ImportGitOpsSyncResponse struct {
	// SuccessCount is the number of successfully imported syncs.
	//
	// Required: true
	SuccessCount int `json:"successCount"`

	// FailedCount is the number of failed imports.
	//
	// Required: true
	FailedCount int `json:"failedCount"`

	// Errors contains error messages for failed imports.
	//
	// Required: true
	Errors []string `json:"errors"`
}
