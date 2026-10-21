package types

import (
	"time"
)

const (
	// GitOpsSyncModeDeploy pulls configuration from the repository into Arcane.
	GitOpsSyncModeDeploy = "deploy"
	// GitOpsSyncModeBackup commits saved project configuration to the repository.
	GitOpsSyncModeBackup = "backup"

	GitOpsBackupStateNever          = "never"
	GitOpsBackupStatePending        = "pending"
	GitOpsBackupStateBackingUp      = "backing_up"
	GitOpsBackupStateBackedUp       = "backed_up"
	GitOpsBackupStatePaused         = "paused"
	GitOpsBackupStateFailed         = "failed"
	GitOpsBackupStateNeedsAttention = "needs_attention"

	GitOpsBackupFailureRepository          = "repository"
	GitOpsBackupFailureAuth                = "auth"
	GitOpsBackupFailureProjectMissing      = "project_missing"
	GitOpsBackupFailureSnapshot            = "snapshot"
	GitOpsBackupFailureUnreadableFiles     = "unreadable_files"
	GitOpsBackupFailureLimits              = "limits"
	GitOpsBackupFailureConflict            = "conflict"
	GitOpsBackupFailureDestinationOccupied = "destination_occupied"
	GitOpsBackupFailurePushRejected        = "push_rejected"

	// GitOpsBackupConflictUseArcane replaces the remote backup files with Arcane's current files.
	GitOpsBackupConflictUseArcane = "use_arcane"

	// FileTreeNodeTypeFile represents a file node.
	FileTreeNodeTypeFile FileTreeNodeType = "file"
	// FileTreeNodeTypeDirectory represents a directory node.
	FileTreeNodeTypeDirectory FileTreeNodeType = "directory"
)

// GitRepository represents a reusable Git repository with credentials.
type GitRepository struct {
	// ID of the git repository.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// Name of the git repository.
	//
	// Required: false
	Name string `json:"name" required:"false" unorm:"nfc" trim:"true"`

	// URL of the git repository.
	//
	// Required: false
	URL string `json:"url" required:"false"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: false
	AuthType string `json:"authType" required:"false"`

	// HasToken indicates whether an HTTP token is stored.
	HasToken bool `json:"hasToken" readOnly:"true"`

	// HasSshKey indicates whether an SSH private key is stored.
	HasSshKey bool `json:"hasSshKey" readOnly:"true"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username string `json:"username,omitempty"`

	// SSHHostKeyVerification specifies how SSH host keys are verified (strict, accept_new, skip).
	//
	// Required: false
	SSHHostKeyVerification string `json:"sshHostKeyVerification,omitempty"`

	// CommitAuthorName is the author name on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorName string `json:"commitAuthorName,omitempty" unorm:"nfc" trim:"true"`

	// CommitAuthorEmail is the author email on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorEmail string `json:"commitAuthorEmail,omitempty" trim:"true"`

	// HasSigningKey indicates whether an OpenPGP signing key is stored.
	HasSigningKey bool `json:"hasSigningKey" readOnly:"true"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty" unorm:"nfc" trim:"true"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: false
	Enabled bool `json:"enabled" required:"false"`

	// CreatedAt is the date and time at which the repository was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt" readOnly:"true"`

	// UpdatedAt is the date and time at which the repository was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

	Token                string `json:"token,omitempty" writeOnly:"true" copier:"-"`
	SSHKey               string `json:"sshKey,omitempty" writeOnly:"true" copier:"-"`
	SigningKey           string `json:"signingKey,omitempty" writeOnly:"true" copier:"-"`
	SigningKeyPassphrase string `json:"signingKeyPassphrase,omitempty" writeOnly:"true" copier:"-"`
}

// GitOpsSync represents a GitOps sync configuration.
type GitOpsSync struct {
	// CreatedAt is the date and time at which the sync was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt" readOnly:"true"`

	// UpdatedAt is the date and time at which the sync was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

	// PreDeployEnv is the KEY=VALUE env config exposed to the script, one
	// entry per line; same format as a .env file.
	//
	// Required: false
	PreDeployEnv *string `json:"preDeployEnv,omitempty"`

	// PreDeployLastRunOutput is the truncated combined stdout+stderr from
	// the most recent pre-deploy lifecycle hook run.
	//
	// Required: false
	PreDeployLastRunOutput *string `json:"preDeployLastRunOutput,omitempty" readOnly:"true"`

	// Repository is the associated git repository.
	//
	// Required: false
	Repository *GitRepository `json:"repository,omitempty" readOnly:"true"`

	// PreDeployLastRunStatus is the status of the most recent pre-deploy
	// lifecycle hook run: "success", "failed", or "timeout".
	//
	// Required: false
	PreDeployLastRunStatus *string `json:"preDeployLastRunStatus,omitempty" readOnly:"true"`

	// PreDeployLastRunAt is the timestamp of the most recent pre-deploy
	// lifecycle hook run on this sync.
	//
	// Required: false
	PreDeployLastRunAt *time.Time `json:"preDeployLastRunAt,omitempty" readOnly:"true"`

	// PreDeployExtraMounts is the bind-mount config added to the runner
	// container, one entry per line in docker -v "src:tgt[:ro|:rw]" form.
	//
	// Required: false
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty"`

	// LastSyncAt is the date and time of the last successful sync.
	//
	// Required: false
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty" readOnly:"true"`

	// ProjectID is the ID of the linked project (set after first sync).
	//
	// Required: false
	ProjectID *string `json:"projectId,omitempty"`

	// PreDeployRunnerImage is the image used to run the pre-deploy script.
	// Required whenever PreDeployScriptPath is set.
	//
	// Required: false
	PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty"`

	// PreDeployScriptPath is the optional path inside the synced repo to a
	// script executed in a throwaway container before each deploy. Scripts
	// are repo-trusted code; the configured runner image, env, and mounts
	// shape the runtime context (admin-managed, never from repo data).
	//
	// Required: false
	PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty"`

	// LastSyncCommit is the commit hash from the last successful sync.
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty" readOnly:"true"`

	// SyncedFiles is a JSON-encoded list of file paths that were synced in the last successful sync.
	// Only populated when SyncDirectory is true. Parse as JSON array of strings.
	//
	// Required: false
	SyncedFiles *string `json:"syncedFiles,omitempty" readOnly:"true"`

	// LastSyncError is the error message from the last sync attempt if it failed.
	//
	// Required: false
	LastSyncError *string `json:"lastSyncError,omitempty" readOnly:"true"`

	// LastSyncStatus is the status of the last sync attempt.
	//
	// Required: false
	LastSyncStatus *string `json:"lastSyncStatus,omitempty" readOnly:"true"`

	// ProjectName is the name used to create/identify the project or stack.
	//
	// Required: false
	ProjectName string `json:"projectName" required:"false"`

	// PreDeployNetworkMode is the Docker network mode passed to the runner
	// container. Defaults to "none" so scripts run with no network access
	// unless explicitly opted in. Set to "bridge", "host", or a named
	// network when the script needs outbound or compose-network access.
	//
	// Required: false
	PreDeployNetworkMode string `json:"preDeployNetworkMode" required:"false"`

	// Name of the sync configuration.
	//
	// Required: false
	Name string `json:"name" required:"false"`

	// EnvironmentID is the ID of the environment this sync belongs to.
	//
	// Required: true
	EnvironmentID string `json:"environmentId" readOnly:"true"`

	// RepositoryID is the ID of the git repository to sync from.
	//
	// Required: false
	RepositoryID string `json:"repositoryId" required:"false"`

	// Branch to sync from.
	//
	// Required: false
	Branch string `json:"branch" required:"false"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: false
	ComposePath string `json:"composePath" required:"false"`
	// ID of the gitops sync.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// TargetType indicates what entity is being deployed (e.g. "project" or "swarm_stack").
	//
	// Required: false
	TargetType string `json:"targetType" required:"false"`

	// Mode is the direction of the sync: "deploy" pulls from Git, "backup" commits to Git.
	//
	// Required: false
	Mode string `json:"mode" required:"false"`

	// BackupDirectory is the repository directory that receives backed-up files.
	//
	// Required: false
	BackupDirectory string `json:"backupDirectory,omitempty"`

	// BackupPaths lists the project-relative files and directories included in backups.
	//
	// Required: false
	BackupPaths []string `json:"backupPaths,omitempty"`

	// BackupState summarizes the backup lifecycle (never, pending, backing_up, backed_up, paused, failed, needs_attention).
	//
	// Required: false
	BackupState string `json:"backupState,omitempty" readOnly:"true"`

	// BackupFailureReason is the typed reason for the last backup failure.
	//
	// Required: false
	BackupFailureReason *string `json:"backupFailureReason,omitempty" readOnly:"true"`

	// LastBackupAt is the time the remote last received a new backup commit.
	//
	// Required: false
	LastBackupAt *time.Time `json:"lastBackupAt,omitempty" readOnly:"true"`

	// PreDeployTimeoutSec bounds the script execution. Capped by the
	// lifecycleMaxTimeoutSec global setting at run time.
	//
	// Required: false
	PreDeployTimeoutSec int `json:"preDeployTimeoutSec" required:"false"`

	// MaxSyncBinarySize is the maximum size in bytes for individual binary files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncBinarySize int64 `json:"maxSyncBinarySize" required:"false"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: false
	SyncInterval int `json:"syncInterval" required:"false"`

	// MaxSyncFiles is the maximum number of files to sync.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncFiles int `json:"maxSyncFiles" required:"false"`

	// MaxSyncTotalSize is the maximum total size in bytes for all synced files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncTotalSize int64 `json:"maxSyncTotalSize" required:"false"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: false
	AutoSync bool `json:"autoSync" required:"false"`

	// SyncDirectory indicates if the entire directory containing the compose file should be synced.
	// When true, all files in the compose file's directory (and subdirectories) are synced.
	// When false, only the compose file itself is synced.
	//
	// Required: false
	SyncDirectory bool `json:"syncDirectory" required:"false"`

	// PullImageAfterSync indicates whether each service's image should be pulled
	// right after a sync that changes managed content, even if the project is
	// currently stopped.
	//
	// Required: false
	PullImageAfterSync bool `json:"pullImageAfterSync" required:"false"`

	// RedeployAfterSync indicates whether the project should be redeployed
	// (recreated with freshly pulled images) right after a sync that changes
	// managed content, regardless of whether it's currently running or stopped.
	//
	// Required: false
	RedeployAfterSync bool `json:"redeployAfterSync" required:"false"`

	// BackupOnSave indicates whether saved project changes trigger a backup.
	//
	// Required: false
	BackupOnSave bool `json:"backupOnSave" required:"false"`

	// BackupPending indicates that saved changes have not reached the repository yet.
	//
	// Required: true
	BackupPending bool `json:"backupPending" readOnly:"true"`
}

// GitOpsSyncCounts contains counts of syncs by status within the current filtered set.
type GitOpsSyncCounts struct {
	// TotalSyncs is the total number of syncs in the current filtered set.
	//
	// Required: true
	TotalSyncs int `json:"totalSyncs"`

	// ActiveSyncs is the number of auto-sync enabled syncs in the current filtered set.
	//
	// Required: true
	ActiveSyncs int `json:"activeSyncs"`

	// SuccessfulSyncs is the number of syncs with last status "success" in the current filtered set.
	//
	// Required: true
	SuccessfulSyncs int `json:"successfulSyncs"`

	// DeploySyncs is the number of "deploy" mode syncs in the current filtered set.
	//
	// Required: true
	DeploySyncs int `json:"deploySyncs"`

	// BackupSyncs is the number of "backup" mode syncs in the current filtered set.
	//
	// Required: true
	BackupSyncs int `json:"backupSyncs"`
}

// GitOpsPreDeployConfigRequest carries the pre-deploy lifecycle hook fields shared by
// the create, update, and import sync requests. A nil pointer means the field
// is absent from the request body; on update, an empty string clears the value.
type GitOpsPreDeployConfigRequest struct {
	// PreDeployScriptPath is the optional path inside the synced repo to a
	// script executed in a throwaway container before each deploy.
	//
	// Required: false
	PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty"`

	// PreDeployRunnerImage is the image used to run the pre-deploy script.
	// When omitted, the lifecycleDefaultRunnerImage setting is used.
	//
	// Required: false
	PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty"`

	// PreDeployEnv is the env config exposed to the script, one KEY=VALUE
	// entry per line; same format as a .env file.
	//
	// Required: false
	PreDeployEnv *string `json:"preDeployEnv,omitempty"`

	// PreDeployExtraMounts is the bind-mount config added to the runner
	// container, one entry per line in docker -v "src:tgt[:ro|:rw]" form.
	//
	// Required: false
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty"`

	// PreDeployTimeoutSec bounds the script execution. Capped by the
	// lifecycleMaxTimeoutSec global setting. Defaults to 60.
	//
	// Required: false
	PreDeployTimeoutSec *int `json:"preDeployTimeoutSec,omitempty"`

	// PreDeployNetworkMode is the Docker network mode for the runner
	// container: "none" (default), "bridge", "host", or a named network.
	//
	// Required: false
	PreDeployNetworkMode *string `json:"preDeployNetworkMode,omitempty"`
}

// HasPreDeployConfig reports whether any pre-deploy hook field is present.
// Configuring the hook is gated behind the gitops:lifecycle permission.
func (r GitOpsPreDeployConfigRequest) HasPreDeployConfig() bool {
	return r != GitOpsPreDeployConfigRequest{}
}

// GitOpsSyncResult represents the result of a sync operation.
type GitOpsSyncResult struct {
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

// GitOpsBrowseResponse represents the response for browsing repository files.
type GitOpsBrowseResponse struct {
	// Path that was browsed.
	//
	// Required: true
	Path string `json:"path"`

	// Files and directories at the path.
	//
	// Required: true
	Files []FileTreeNode `json:"files"`
}

// GitBranchInfo represents information about a git branch.
type GitBranchInfo struct {
	// Name of the branch.
	//
	// Required: true
	Name string `json:"name"`

	// IsDefault indicates if this is the default branch.
	//
	// Required: true
	IsDefault bool `json:"isDefault"`
}

// GitBranchesResponse represents the response for listing repository branches.
type GitBranchesResponse struct {
	// Branches available in the repository.
	//
	// Required: true
	Branches []GitBranchInfo `json:"branches"`
}

// GitRepositorySync represents a git repository for syncing to remote environments.
type GitRepositorySync struct {
	// ID of the git repository.
	//
	// Required: true
	ID string `json:"id" binding:"required"`

	// Name of the git repository.
	//
	// Required: true
	Name string `json:"name" binding:"required" unorm:"nfc" trim:"true"`

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

	// SSHHostKeyVerification specifies how SSH host keys are verified.
	//
	// Required: false
	SSHHostKeyVerification string `json:"sshHostKeyVerification,omitempty"`

	// CommitAuthorName is the author name on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorName string `json:"commitAuthorName,omitempty"`

	// CommitAuthorEmail is the author email on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorEmail string `json:"commitAuthorEmail,omitempty"`

	// SigningKey is the armored OpenPGP private key used to sign commits.
	//
	// Required: false
	SigningKey string `json:"signingKey,omitempty"`

	// SigningKeyPassphrase unlocks SigningKey.
	//
	// Required: false
	SigningKeyPassphrase string `json:"signingKeyPassphrase,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty" unorm:"nfc" trim:"true"`

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

// GitRepositorySyncRequest represents a request to sync git repositories to an agent.
type GitRepositorySyncRequest struct {
	// Repositories is a list of git repositories to sync.
	//
	// Required: true
	Repositories []GitRepositorySync `json:"repositories" binding:"required"`
}

// GitOpsSyncStatus represents the current status of a sync configuration.
type GitOpsSyncStatus struct {
	// ID of the sync configuration.
	//
	// Required: true
	ID string `json:"id"`

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

	// Mode is the sync direction ("deploy" or "backup").
	//
	// Required: true
	Mode string `json:"mode"`

	// BackupState summarizes the backup lifecycle for "backup" syncs.
	//
	// Required: false
	BackupState string `json:"backupState,omitempty"`

	// BackupPending indicates saved changes have not reached the repository yet.
	//
	// Required: true
	BackupPending bool `json:"backupPending"`

	// BackupFailureReason is the typed reason for the last backup failure.
	//
	// Required: false
	BackupFailureReason *string `json:"backupFailureReason,omitempty"`

	// LastBackupAt is the time the remote last received a new backup commit.
	//
	// Required: false
	LastBackupAt *time.Time `json:"lastBackupAt,omitempty"`
}

// ImportGitOpsSyncRequest represents the request to import gitops syncs.
type ImportGitOpsSyncRequest struct {
	GitOpsPreDeployConfigRequest

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

	// SyncDirectory indicates if the entire directory containing the compose file should be synced.
	// When true (default), all files in the compose file's directory are synced.
	//
	// Required: false
	SyncDirectory *bool `json:"syncDirectory,omitempty"`

	// MaxSyncFiles is the maximum number of files to sync.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncFiles *int `json:"maxSyncFiles,omitempty"`

	// MaxSyncTotalSize is the maximum total size in bytes for all synced files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncTotalSize *int64 `json:"maxSyncTotalSize,omitempty"`

	// MaxSyncBinarySize is the maximum size in bytes for individual binary files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncBinarySize *int64 `json:"maxSyncBinarySize,omitempty"`

	// ProjectName is the compose project name. Defaults to SyncName.
	//
	// Required: false
	ProjectName string `json:"projectName,omitempty"`

	// PullImageAfterSync pulls each service image after a sync that changes
	// managed content. Default: false.
	//
	// Required: false
	PullImageAfterSync *bool `json:"pullImageAfterSync,omitempty"`

	// RedeployAfterSync redeploys the project after a sync that changes
	// managed content. Default: false.
	//
	// Required: false
	RedeployAfterSync *bool `json:"redeployAfterSync,omitempty"`
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

// GitOpsBackupFileChange describes one file difference between Arcane and the repository.
type GitOpsBackupFileChange struct {
	// Path relative to the backup directory.
	//
	// Required: true
	Path string `json:"path"`

	// Change is "added", "modified", or "removed" from the repository's point of view.
	//
	// Required: true
	Change string `json:"change"`
}

// GitOpsBackupPreview describes what the next backup would commit.
type GitOpsBackupPreview struct {
	// State is "clean", "changes", "conflict", or "destination_occupied".
	//
	// Required: true
	State string `json:"state"`

	// RemoteCommit is the current head of the backup branch, when it exists.
	//
	// Required: false
	RemoteCommit string `json:"remoteCommit,omitempty"`

	// Changes lists files the next backup would add, modify, or remove.
	//
	// Required: true
	Changes []GitOpsBackupFileChange `json:"changes"`

	// Conflicts lists backup-owned files changed in the repository since the last successful backup.
	//
	// Required: true
	Conflicts []GitOpsBackupFileChange `json:"conflicts"`

	// Files lists every path the snapshot includes.
	//
	// Required: true
	Files []string `json:"files"`
}

// GitOpsBackupHistoryEntry is one revision affecting a backup directory.
type GitOpsBackupHistoryEntry struct {
	// Commit hash.
	//
	// Required: true
	Commit string `json:"commit"`

	// Author name of the commit.
	//
	// Required: true
	Author string `json:"author"`

	// Message of the commit.
	//
	// Required: true
	Message string `json:"message"`

	// Date of the commit.
	//
	// Required: true
	Date time.Time `json:"date"`

	// Files changed inside the backup directory, relative to it.
	//
	// Required: true
	Files []string `json:"files"`
}

// GitOpsBackupHistoryResponse lists revisions affecting a backup.
type GitOpsBackupHistoryResponse struct {
	// Entries newest first.
	//
	// Required: true
	Entries []GitOpsBackupHistoryEntry `json:"entries"`
}

// GitOpsBackupFileDiff is the unified diff of one file in a revision.
type GitOpsBackupFileDiff struct {
	// Path relative to the backup directory.
	//
	// Required: true
	Path string `json:"path"`

	// Patch is the unified diff text.
	//
	// Required: true
	Patch string `json:"patch"`
}

// GitOpsBackupRevision is a revision with its per-file diffs.
type GitOpsBackupRevision struct {
	// Entry describes the commit.
	//
	// Required: true
	Entry GitOpsBackupHistoryEntry `json:"entry"`

	// Diffs contains one entry per changed file inside the backup directory.
	//
	// Required: true
	Diffs []GitOpsBackupFileDiff `json:"diffs"`
}

// GitOpsResolveBackupConflictRequest chooses how to resolve a backup that needs attention.
type GitOpsResolveBackupConflictRequest struct {
	// Strategy is "use_arcane" to replace the repository's backup files with Arcane's.
	//
	// Required: true
	Strategy string `json:"strategy" binding:"required,oneof=use_arcane"`
}
