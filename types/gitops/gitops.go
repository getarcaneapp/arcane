package gitops

import "time"

const (
	// SyncModeDeploy pulls configuration from the repository into Arcane.
	SyncModeDeploy = "deploy"
	// SyncModeBackup commits saved project configuration to the repository.
	SyncModeBackup = "backup"
)

const (
	BackupStateNever          = "never"
	BackupStatePending        = "pending"
	BackupStateBackingUp      = "backing_up"
	BackupStateBackedUp       = "backed_up"
	BackupStatePaused         = "paused"
	BackupStateFailed         = "failed"
	BackupStateNeedsAttention = "needs_attention"
)

const (
	BackupFailureRepository          = "repository"
	BackupFailureAuth                = "auth"
	BackupFailureProjectMissing      = "project_missing"
	BackupFailureSnapshot            = "snapshot"
	BackupFailureUnreadableFiles     = "unreadable_files"
	BackupFailureLimits              = "limits"
	BackupFailureConflict            = "conflict"
	BackupFailureDestinationOccupied = "destination_occupied"
	BackupFailurePushRejected        = "push_rejected"
)

const (
	// BackupConflictUseArcane replaces the remote backup files with Arcane's current files.
	BackupConflictUseArcane = "use_arcane"
)

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

	// HasToken indicates whether an HTTP token is stored.
	HasToken bool `json:"hasToken"`

	// HasSshKey indicates whether an SSH private key is stored.
	HasSshKey bool `json:"hasSshKey"`

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
	CommitAuthorName string `json:"commitAuthorName,omitempty"`

	// CommitAuthorEmail is the author email on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorEmail string `json:"commitAuthorEmail,omitempty"`

	// HasSigningKey indicates whether an OpenPGP signing key is stored.
	HasSigningKey bool `json:"hasSigningKey"`

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

	// CreatedAt is the date and time at which the sync was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the date and time at which the sync was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// PreDeployEnv is the KEY=VALUE env config exposed to the script, one
	// entry per line; same format as a .env file.
	//
	// Required: false
	PreDeployEnv *string `json:"preDeployEnv,omitempty"`

	// PreDeployLastRunOutput is the truncated combined stdout+stderr from
	// the most recent pre-deploy lifecycle hook run.
	//
	// Required: false
	PreDeployLastRunOutput *string `json:"preDeployLastRunOutput,omitempty"`

	// Repository is the associated git repository.
	//
	// Required: false
	Repository *GitRepository `json:"repository,omitempty"`

	// PreDeployLastRunStatus is the status of the most recent pre-deploy
	// lifecycle hook run: "success", "failed", or "timeout".
	//
	// Required: false
	PreDeployLastRunStatus *string `json:"preDeployLastRunStatus,omitempty"`

	// PreDeployLastRunAt is the timestamp of the most recent pre-deploy
	// lifecycle hook run on this sync.
	//
	// Required: false
	PreDeployLastRunAt *time.Time `json:"preDeployLastRunAt,omitempty"`

	// PreDeployExtraMounts is the bind-mount config added to the runner
	// container, one entry per line in docker -v "src:tgt[:ro|:rw]" form.
	//
	// Required: false
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty"`

	// LastSyncAt is the date and time of the last successful sync.
	//
	// Required: false
	LastSyncAt *time.Time `json:"lastSyncAt,omitempty"`

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
	LastSyncCommit *string `json:"lastSyncCommit,omitempty"`

	// SyncedFiles is a JSON-encoded list of file paths that were synced in the last successful sync.
	// Only populated when SyncDirectory is true. Parse as JSON array of strings.
	//
	// Required: false
	SyncedFiles *string `json:"syncedFiles,omitempty"`

	// LastSyncError is the error message from the last sync attempt if it failed.
	//
	// Required: false
	LastSyncError *string `json:"lastSyncError,omitempty"`

	// LastSyncStatus is the status of the last sync attempt.
	//
	// Required: false
	LastSyncStatus *string `json:"lastSyncStatus,omitempty"`

	// ProjectName is the name used to create/identify the project or stack.
	//
	// Required: true
	ProjectName string `json:"projectName"`

	// PreDeployNetworkMode is the Docker network mode passed to the runner
	// container. Defaults to "none" so scripts run with no network access
	// unless explicitly opted in. Set to "bridge", "host", or a named
	// network when the script needs outbound or compose-network access.
	//
	// Required: true
	PreDeployNetworkMode string `json:"preDeployNetworkMode"`

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

	// Branch to sync from.
	//
	// Required: true
	Branch string `json:"branch"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: true
	ComposePath string `json:"composePath"`
	// ID of the gitops sync.
	//
	// Required: true
	ID string `json:"id"`

	// TargetType indicates what entity is being deployed (e.g. "project" or "swarm_stack").
	//
	// Required: true
	TargetType string `json:"targetType"`

	// Mode is the direction of the sync: "deploy" pulls from Git, "backup" commits to Git.
	//
	// Required: true
	Mode string `json:"mode"`

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
	BackupState string `json:"backupState,omitempty"`

	// BackupFailureReason is the typed reason for the last backup failure.
	//
	// Required: false
	BackupFailureReason *string `json:"backupFailureReason,omitempty"`

	// LastBackupAt is the time the remote last received a new backup commit.
	//
	// Required: false
	LastBackupAt *time.Time `json:"lastBackupAt,omitempty"`

	// PreDeployTimeoutSec bounds the script execution. Capped by the
	// lifecycleMaxTimeoutSec global setting at run time.
	//
	// Required: true
	PreDeployTimeoutSec int `json:"preDeployTimeoutSec"`

	// MaxSyncBinarySize is the maximum size in bytes for individual binary files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: true
	MaxSyncBinarySize int64 `json:"maxSyncBinarySize"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: true
	SyncInterval int `json:"syncInterval"`

	// MaxSyncFiles is the maximum number of files to sync.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: true
	MaxSyncFiles int `json:"maxSyncFiles"`

	// MaxSyncTotalSize is the maximum total size in bytes for all synced files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: true
	MaxSyncTotalSize int64 `json:"maxSyncTotalSize"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: true
	AutoSync bool `json:"autoSync"`

	// SyncDirectory indicates if the entire directory containing the compose file should be synced.
	// When true, all files in the compose file's directory (and subdirectories) are synced.
	// When false, only the compose file itself is synced.
	//
	// Required: true
	SyncDirectory bool `json:"syncDirectory"`

	// PullImageAfterSync indicates whether each service's image should be pulled
	// right after a sync that changes managed content, even if the project is
	// currently stopped.
	//
	// Required: true
	PullImageAfterSync bool `json:"pullImageAfterSync"`

	// RedeployAfterSync indicates whether the project should be redeployed
	// (recreated with freshly pulled images) right after a sync that changes
	// managed content, regardless of whether it's currently running or stopped.
	//
	// Required: true
	RedeployAfterSync bool `json:"redeployAfterSync"`

	// BackupOnSave indicates whether saved project changes trigger a backup.
	//
	// Required: true
	BackupOnSave bool `json:"backupOnSave"`

	// BackupPending indicates that saved changes have not reached the repository yet.
	//
	// Required: true
	BackupPending bool `json:"backupPending"`
}

// SyncCounts contains counts of syncs by status within the current filtered set.
type SyncCounts struct {
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

// CreateRepositoryRequest represents the request to create a git repository.
type CreateRepositoryRequest struct {
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
	AuthType string `json:"authType" binding:"required,oneof=none http ssh"`

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

	// SSHHostKeyVerification specifies how SSH host keys are verified.
	// Options: strict (require known_hosts), accept_new (auto-add new hosts), skip (disable verification).
	// Default: accept_new
	//
	// Required: false
	SSHHostKeyVerification string `json:"sshHostKeyVerification,omitempty" binding:"omitempty,oneof=strict accept_new skip"`

	// CommitAuthorName is the author name on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorName string `json:"commitAuthorName,omitempty" unorm:"nfc" trim:"true"`

	// CommitAuthorEmail is the author email on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorEmail string `json:"commitAuthorEmail,omitempty" binding:"omitempty,email" trim:"true"`

	// SigningKey is an armored OpenPGP private key used to sign commits.
	//
	// Required: false
	SigningKey string `json:"signingKey,omitempty"`

	// SigningKeyPassphrase unlocks SigningKey when it is passphrase-protected.
	//
	// Required: false
	SigningKeyPassphrase string `json:"signingKeyPassphrase,omitempty"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitempty" unorm:"nfc" trim:"true"`

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
	Name *string `json:"name,omitzero" unorm:"nfc" trim:"true"`

	// URL of the git repository.
	//
	// Required: false
	URL *string `json:"url,omitzero"`

	// AuthType specifies the authentication method (none, http, ssh).
	//
	// Required: false
	AuthType *string `json:"authType,omitzero" binding:"omitempty,oneof=none http ssh"`

	// Username for HTTP authentication.
	//
	// Required: false
	Username *string `json:"username,omitzero"`

	// Token for HTTP authentication.
	//
	// Required: false
	Token *string `json:"token,omitzero"`

	// SSHKey for SSH authentication.
	//
	// Required: false
	SSHKey *string `json:"sshKey,omitzero"`

	// SSHHostKeyVerification specifies how SSH host keys are verified.
	// Options: strict (require known_hosts), accept_new (auto-add new hosts), skip (disable verification).
	//
	// Required: false
	SSHHostKeyVerification *string `json:"sshHostKeyVerification,omitzero" binding:"omitempty,oneof=strict accept_new skip"`

	// CommitAuthorName is the author name on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorName *string `json:"commitAuthorName,omitzero" unorm:"nfc" trim:"true"`

	// CommitAuthorEmail is the author email on commits Arcane pushes.
	//
	// Required: false
	CommitAuthorEmail *string `json:"commitAuthorEmail,omitzero" binding:"omitempty,email" trim:"true"`

	// SigningKey is an armored OpenPGP private key used to sign commits. Empty clears it.
	//
	// Required: false
	SigningKey *string `json:"signingKey,omitzero"`

	// SigningKeyPassphrase unlocks SigningKey when it is passphrase-protected. Empty clears it.
	//
	// Required: false
	SigningKeyPassphrase *string `json:"signingKeyPassphrase,omitzero"`

	// Description of the git repository.
	//
	// Required: false
	Description *string `json:"description,omitzero" unorm:"nfc" trim:"true"`

	// Enabled indicates if the repository is enabled.
	//
	// Required: false
	Enabled *bool `json:"enabled,omitzero"`
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
	// Required for "deploy" mode; derived from the backup directory in "backup" mode.
	//
	// Required: false
	ComposePath string `json:"composePath,omitempty"`

	// Mode selects the sync direction: "deploy" (default) or "backup".
	//
	// Required: false
	Mode string `json:"mode,omitempty" binding:"omitempty,oneof=deploy backup"`

	// ProjectID links an existing project. Required in "backup" mode; optional in
	// "deploy" mode, where the sync adopts the project instead of creating one.
	//
	// Required: false
	ProjectID string `json:"projectId,omitempty"`

	// BackupDirectory is the repository directory that receives backed-up files.
	// Required in "backup" mode.
	//
	// Required: false
	BackupDirectory string `json:"backupDirectory,omitempty"`

	// BackupPaths lists project-relative files and directories to back up. The
	// project's compose files are always included. Environment files are only
	// included when listed explicitly.
	//
	// Required: false
	BackupPaths []string `json:"backupPaths,omitempty"`

	// BackupOnSave triggers a backup after project changes are saved. Default: true.
	//
	// Required: false
	BackupOnSave *bool `json:"backupOnSave,omitempty"`

	// TargetType specifies if this sync targets a "project" or "swarm_stack".
	//
	// Required: false
	TargetType string `json:"targetType,omitempty"`

	// ProjectName is the name of the project or stack to create/update.
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

	// SyncDirectory indicates if the entire directory containing the compose file should be synced.
	// When true (default), all files in the compose file's directory are synced.
	// When false, only the compose file itself is synced.
	//
	// Required: false
	SyncDirectory *bool `json:"syncDirectory,omitempty"`

	// PullImageAfterSync indicates whether each service's image should be pulled
	// right after a sync that changes managed content, even if the project is
	// currently stopped. Default: false.
	//
	// Required: false
	PullImageAfterSync *bool `json:"pullImageAfterSync,omitempty"`

	// RedeployAfterSync indicates whether the project should be redeployed
	// (recreated with freshly pulled images) right after a sync that changes
	// managed content, regardless of whether it's currently running or stopped.
	// Default: false.
	//
	// Required: false
	RedeployAfterSync *bool `json:"redeployAfterSync,omitempty"`

	// MaxSyncFiles is the maximum number of files to sync.
	// 0 means unlimited; env var overrides take precedence.
	// Default: 0
	//
	// Required: false
	MaxSyncFiles *int `json:"maxSyncFiles,omitempty"`

	// MaxSyncTotalSize is the maximum total size in bytes for all synced files.
	// 0 means unlimited; env var overrides take precedence.
	// Default: 0
	//
	// Required: false
	MaxSyncTotalSize *int64 `json:"maxSyncTotalSize,omitempty"`

	// MaxSyncBinarySize is the maximum size in bytes for individual binary files.
	// 0 means unlimited; env var overrides take precedence.
	// Default: 0
	//
	// Required: false
	MaxSyncBinarySize *int64 `json:"maxSyncBinarySize,omitempty"`

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
	// entry per line; same format as a .env file. Keys must match POSIX
	// identifier syntax.
	//
	// Required: false
	PreDeployEnv *string `json:"preDeployEnv,omitempty"`

	// PreDeployExtraMounts is the bind-mount config added to the runner
	// container, one entry per line in docker -v "src:tgt[:ro|:rw]" form.
	// Source and target must be absolute paths.
	//
	// Required: false
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty"`

	// PreDeployTimeoutSec bounds the script execution. Capped by the
	// lifecycleMaxTimeoutSec global setting at validation time. Defaults to 60.
	//
	// Required: false
	PreDeployTimeoutSec *int `json:"preDeployTimeoutSec,omitempty"`

	// PreDeployNetworkMode is the Docker network mode for the runner
	// container. Defaults to "none" (no network access). Set to "bridge",
	// "host", or a named Docker network to grant outbound or compose-network
	// access.
	//
	// Required: false
	PreDeployNetworkMode *string `json:"preDeployNetworkMode,omitempty"`
}

// UpdateSyncRequest represents the request to update a gitops sync.
type UpdateSyncRequest struct {
	// Name of the sync configuration.
	//
	// Required: false
	Name *string `json:"name,omitzero"`

	// RepositoryID is the ID of the git repository to sync from.
	//
	// Required: false
	RepositoryID *string `json:"repositoryId,omitzero"`

	// Branch to sync from.
	//
	// Required: false
	Branch *string `json:"branch,omitzero"`

	// ComposePath is the path to the docker-compose file in the repository.
	//
	// Required: false
	ComposePath *string `json:"composePath,omitzero"`

	// TargetType specifies if this sync targets a "project" or "swarm_stack".
	//
	// Required: false
	TargetType *string `json:"targetType,omitzero"`

	// ProjectName is the name of the project or stack to create/update.
	//
	// Required: false
	ProjectName *string `json:"projectName,omitzero"`

	// BackupPaths replaces the backed-up file selection. Omitted leaves it unchanged;
	// an empty list keeps only the compose files.
	//
	// Required: false
	BackupPaths []string `json:"backupPaths,omitzero"`

	// BackupOnSave toggles backing up after project changes are saved.
	//
	// Required: false
	BackupOnSave *bool `json:"backupOnSave,omitzero"`

	// AutoSync indicates if the sync should run automatically.
	//
	// Required: false
	AutoSync *bool `json:"autoSync,omitzero"`

	// SyncInterval is the interval in minutes between automatic syncs.
	//
	// Required: false
	SyncInterval *int `json:"syncInterval,omitzero"`

	// SyncDirectory indicates if the entire directory containing the compose file should be synced.
	// When true, all files in the compose file's directory are synced.
	// When false, only the compose file itself is synced.
	//
	// Required: false
	SyncDirectory *bool `json:"syncDirectory,omitzero"`

	// PullImageAfterSync indicates whether each service's image should be pulled
	// right after a sync that changes managed content, even if the project is
	// currently stopped. Default: false.
	//
	// Required: false
	PullImageAfterSync *bool `json:"pullImageAfterSync,omitzero"`

	// RedeployAfterSync indicates whether the project should be redeployed
	// (recreated with freshly pulled images) right after a sync that changes
	// managed content, regardless of whether it's currently running or stopped.
	// Default: false.
	//
	// Required: false
	RedeployAfterSync *bool `json:"redeployAfterSync,omitzero"`

	// MaxSyncFiles is the maximum number of files to sync.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncFiles *int `json:"maxSyncFiles,omitzero"`

	// MaxSyncTotalSize is the maximum total size in bytes for all synced files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncTotalSize *int64 `json:"maxSyncTotalSize,omitzero"`

	// MaxSyncBinarySize is the maximum size in bytes for individual binary files.
	// 0 means unlimited; env var overrides take precedence.
	//
	// Required: false
	MaxSyncBinarySize *int64 `json:"maxSyncBinarySize,omitzero"`

	// PreDeployScriptPath is the optional path inside the synced repo to a
	// script executed in a throwaway container before each deploy. Set to
	// an empty string to clear an existing configuration.
	//
	// Required: false
	PreDeployScriptPath *string `json:"preDeployScriptPath,omitzero"`

	// PreDeployRunnerImage is the image used to run the pre-deploy script.
	// When omitted, the lifecycleDefaultRunnerImage setting is used.
	//
	// Required: false
	PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitzero"`

	// PreDeployEnv is the env config exposed to the script, one KEY=VALUE
	// entry per line; same format as a .env file. Keys must match POSIX
	// identifier syntax.
	//
	// Required: false
	PreDeployEnv *string `json:"preDeployEnv,omitzero"`

	// PreDeployExtraMounts is the bind-mount config added to the runner
	// container, one entry per line in docker -v "src:tgt[:ro|:rw]" form.
	// Source and target must be absolute paths.
	//
	// Required: false
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitzero"`

	// PreDeployTimeoutSec bounds the script execution. Capped by the
	// lifecycleMaxTimeoutSec global setting at validation time.
	//
	// Required: false
	PreDeployTimeoutSec *int `json:"preDeployTimeoutSec,omitzero"`

	// PreDeployNetworkMode is the Docker network mode for the runner
	// container. Set to "none", "bridge", "host", or a named Docker
	// network. Empty string resets to the default ("none").
	//
	// Required: false
	PreDeployNetworkMode *string `json:"preDeployNetworkMode,omitzero"`
}

// HasPreDeployConfig reports whether the request carries any pre-deploy
// lifecycle hook field. Configuring the hook is gated behind the dedicated
// gitops:lifecycle permission (see the GitOps sync handlers), so callers use
// this to decide whether that authorization check applies. A nil pointer means
// the field is absent from the request body.
func (r CreateSyncRequest) HasPreDeployConfig() bool {
	return r.PreDeployScriptPath != nil ||
		r.PreDeployRunnerImage != nil ||
		r.PreDeployEnv != nil ||
		r.PreDeployExtraMounts != nil ||
		r.PreDeployNetworkMode != nil ||
		r.PreDeployTimeoutSec != nil
}

// HasPreDeployConfig reports whether the request carries any pre-deploy
// lifecycle hook field. See CreateSyncRequest.HasPreDeployConfig.
func (r UpdateSyncRequest) HasPreDeployConfig() bool {
	return r.PreDeployScriptPath != nil ||
		r.PreDeployRunnerImage != nil ||
		r.PreDeployEnv != nil ||
		r.PreDeployExtraMounts != nil ||
		r.PreDeployNetworkMode != nil ||
		r.PreDeployTimeoutSec != nil
}

// HasDeploymentOptions reports whether the request sets deployment-only
// options that a backup sync must reject.
func (r CreateSyncRequest) HasDeploymentOptions() bool {
	return (r.TargetType != "" && r.TargetType != "project") ||
		(r.SyncDirectory != nil && *r.SyncDirectory) ||
		(r.PullImageAfterSync != nil && *r.PullImageAfterSync) ||
		(r.RedeployAfterSync != nil && *r.RedeployAfterSync) ||
		r.HasPreDeployConfig()
}

// HasDeploymentOptions reports whether the request sets deployment-only
// options that a backup sync must reject.
func (r UpdateSyncRequest) HasDeploymentOptions() bool {
	return (r.TargetType != nil && *r.TargetType != "" && *r.TargetType != "project") ||
		r.ComposePath != nil ||
		r.ProjectName != nil ||
		(r.SyncDirectory != nil && *r.SyncDirectory) ||
		(r.PullImageAfterSync != nil && *r.PullImageAfterSync) ||
		(r.RedeployAfterSync != nil && *r.RedeployAfterSync) ||
		r.HasPreDeployConfig()
}

// HasBackupOptions reports whether the request sets backup-only options that
// a deploy sync must reject.
func (r CreateSyncRequest) HasBackupOptions() bool {
	return r.BackupDirectory != "" || len(r.BackupPaths) > 0 || r.BackupOnSave != nil
}

// HasBackupOptions reports whether the request sets backup-only options that
// a deploy sync must reject.
func (r UpdateSyncRequest) HasBackupOptions() bool {
	return r.BackupPaths != nil || r.BackupOnSave != nil
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

// BackupFileChange describes one file difference between Arcane and the repository.
type BackupFileChange struct {
	// Path relative to the backup directory.
	//
	// Required: true
	Path string `json:"path"`

	// Change is "added", "modified", or "removed" from the repository's point of view.
	//
	// Required: true
	Change string `json:"change"`
}

// BackupPreview describes what the next backup would commit.
type BackupPreview struct {
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
	Changes []BackupFileChange `json:"changes"`

	// Conflicts lists backup-owned files changed in the repository since the last successful backup.
	//
	// Required: true
	Conflicts []BackupFileChange `json:"conflicts"`

	// Files lists every path the snapshot includes.
	//
	// Required: true
	Files []string `json:"files"`
}

// BackupHistoryEntry is one revision affecting a backup directory.
type BackupHistoryEntry struct {
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

// BackupHistoryResponse lists revisions affecting a backup.
type BackupHistoryResponse struct {
	// Entries newest first.
	//
	// Required: true
	Entries []BackupHistoryEntry `json:"entries"`
}

// BackupFileDiff is the unified diff of one file in a revision.
type BackupFileDiff struct {
	// Path relative to the backup directory.
	//
	// Required: true
	Path string `json:"path"`

	// Patch is the unified diff text.
	//
	// Required: true
	Patch string `json:"patch"`
}

// BackupRevision is a revision with its per-file diffs.
type BackupRevision struct {
	// Entry describes the commit.
	//
	// Required: true
	Entry BackupHistoryEntry `json:"entry"`

	// Diffs contains one entry per changed file inside the backup directory.
	//
	// Required: true
	Diffs []BackupFileDiff `json:"diffs"`
}

// ResolveBackupConflictRequest chooses how to resolve a backup that needs attention.
type ResolveBackupConflictRequest struct {
	// Strategy is "use_arcane" to replace the repository's backup files with Arcane's.
	//
	// Required: true
	Strategy string `json:"strategy" binding:"required,oneof=use_arcane"`
}
