package types

import (
	"time"
)

type (
	GitRepository struct {
		CreatedAt              time.Time `json:"createdAt" readOnly:"true"`
		UpdatedAt              time.Time `json:"updatedAt" readOnly:"true"`
		Description            *string   `json:"description,omitempty" unorm:"nfc" trim:"true"`
		ID                     string    `json:"id" readOnly:"true"`
		Name                   string    `json:"name" required:"false" unorm:"nfc" trim:"true" doc:"Display name of the repository; required on create"`
		URL                    string    `json:"url" required:"false" doc:"http, https, ssh or git URL, or scp-like user@host:path (https if no scheme); a change needs a stored token or sshKey resent"`
		Provider               string    `json:"provider,omitempty" doc:"Forge used to build web links; empty detects it from the URL host" enum:",github,gitlab,gitea,bitbucket"`
		AuthType               string    `json:"authType" required:"false" doc:"Authentication method: none, http or ssh; required on create"`
		Username               string    `json:"username,omitempty" doc:"Username for http authentication, sent with the token"`
		SSHHostKeyVerification string    `json:"sshHostKeyVerification,omitempty" doc:"strict (known_hosts only), accept_new (trust an unknown host on first use; default) or skip"`
		CommitAuthorName       string    `json:"commitAuthorName,omitempty" unorm:"nfc" trim:"true" doc:"Author name on commits Arcane pushes; defaults to Arcane"`
		CommitAuthorEmail      string    `json:"commitAuthorEmail,omitempty" trim:"true" doc:"Author email on commits Arcane pushes; defaults to arcane@localhost"`

		Token                string `json:"token,omitempty" writeOnly:"true" copier:"-" doc:"Password or access token for http authentication; stored encrypted, empty on update removes it"`
		SSHKey               string `json:"sshKey,omitempty" writeOnly:"true" copier:"-" doc:"Private key for ssh authentication; stored encrypted, empty on update removes it"`
		SigningKey           string `json:"signingKey,omitempty" writeOnly:"true" copier:"-" doc:"Armored OpenPGP private key that signs the commits Arcane pushes; empty on update removes it"`
		SigningKeyPassphrase string `json:"signingKeyPassphrase,omitempty" writeOnly:"true" copier:"-" doc:"Needed only when the signing key is passphrase-protected"`
		HasToken             bool   `json:"hasToken" readOnly:"true"`
		HasSshKey            bool   `json:"hasSshKey" readOnly:"true"`
		HasSigningKey        bool   `json:"hasSigningKey" readOnly:"true"`
		Enabled              bool   `json:"enabled" required:"false" doc:"Defaults to true"`
	}

	GitOpsSync struct {
		CreatedAt time.Time `json:"createdAt" readOnly:"true"`
		UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

		PreDeployEnv           *string        `json:"preDeployEnv,omitempty" doc:"KEY=VALUE lines in .env format, passed to the pre-deploy script as environment variables"`
		EnvContent             *string        `json:"envContent,omitempty" doc:"KEY=VALUE lines merged over the repository .env on every deploy sync"`
		PreDeployLastRunOutput *string        `json:"preDeployLastRunOutput,omitempty" readOnly:"true"`
		Repository             *GitRepository `json:"repository,omitempty" readOnly:"true"`
		PreDeployLastRunStatus *string        `json:"preDeployLastRunStatus,omitempty" readOnly:"true"`
		PreDeployLastRunAt     *time.Time     `json:"preDeployLastRunAt,omitempty" readOnly:"true"`
		PreDeployExtraMounts   *string        `json:"preDeployExtraMounts,omitempty" doc:"Extra bind mounts for the pre-deploy runner, one src:target[:ro|:rw] per line, both paths absolute"`
		LastSyncAt             *time.Time     `json:"lastSyncAt,omitempty" readOnly:"true"`
		ProjectID              *string        `json:"projectId,omitempty" doc:"Existing project: required for a backup or bidirectional sync, optional for a deploy sync to adopt it; ignored on update"`

		PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty" doc:"Image that runs the pre-deploy script; empty uses the lifecycleDefaultRunnerImage setting"`

		PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty" doc:"Script run in a throwaway container before each deploy, relative to the project; needs lifecycleEnabled and syncDirectory"`
		LastSyncCommit      *string `json:"lastSyncCommit,omitempty" readOnly:"true"`

		SyncedFiles         *string    `json:"syncedFiles,omitempty" readOnly:"true"`
		LastSyncError       *string    `json:"lastSyncError,omitempty" readOnly:"true"`
		LastSyncStatus      *string    `json:"lastSyncStatus,omitempty" readOnly:"true"`
		BackupFailureReason *string    `json:"backupFailureReason,omitempty" readOnly:"true"`
		LastBackupAt        *time.Time `json:"lastBackupAt,omitempty" readOnly:"true"`
		ProjectName         string     `json:"projectName" required:"false" doc:"Project or swarm stack name the sync deploys; defaults to name, or the project's name if projectId is set"`

		PreDeployNetworkMode string `json:"preDeployNetworkMode" required:"false" doc:"Network mode of the pre-deploy runner: none (default), bridge, host or a network name"`
		Name                 string `json:"name" required:"false" doc:"Display name of the sync; required on create"`
		EnvironmentID        string `json:"environmentId" readOnly:"true"`

		ComposeOverridePath string `json:"composeOverridePath,omitempty" required:"false" doc:"Repository path of a compose override file; empty auto-detects one beside the compose file"`

		EnvironmentName string   `json:"environmentName,omitempty" readOnly:"true" doc:"Display name of the environment the sync belongs to"`
		RepositoryID    string   `json:"repositoryId" required:"false" doc:"ID of the Git repository to use; required on create"`
		Branch          string   `json:"branch" required:"false" doc:"Branch to sync from or back up to; required on create"`
		ComposePath     string   `json:"composePath" required:"false" doc:"Compose file path in the repository; required for a deploy sync, set by Arcane for a backup or bidirectional sync"`
		ID              string   `json:"id" readOnly:"true"`
		TargetType      string   `json:"targetType" required:"false" doc:"What the sync deploys: project (default) or, for a deploy sync only, swarm_stack, which takes no projectId or pre-deploy script"`
		Mode            string   `json:"mode" required:"false" doc:"deploy (default) applies the repository to the project, backup commits the project to it, bidirectional does both; ignored on update"`
		BackupDirectory string   `json:"backupDirectory,omitempty" doc:"Repository directory for the project files, not the root; required for backup and bidirectional syncs only, ignored on update"`
		BackupState     string   `json:"backupState,omitempty" readOnly:"true"`
		BackupPaths     []string `json:"backupPaths,omitempty" doc:"Project-relative files and directories included in the sync; backup and bidirectional syncs only"`

		PreDeployTimeoutSec int `json:"preDeployTimeoutSec" required:"false" doc:"Pre-deploy script timeout in seconds, default 60; at least 1 and at most the lifecycleMaxTimeoutSec setting"`

		MaxSyncBinarySize int64 `json:"maxSyncBinarySize" required:"false" doc:"Directory syncs skip binary files over this many bytes, 0 for none (only 0 if bidirectional); default gitSyncMaxBinarySizeMb"`

		SyncInterval int `json:"syncInterval" required:"false" doc:"Minutes between automatic syncs; defaults to 60, or 5 for a backup or bidirectional sync"`

		MaxSyncFiles int `json:"maxSyncFiles" required:"false" doc:"Max files a directory sync or backup copies, 0 for no limit; defaults to gitSyncMaxFiles, env var wins"`

		MaxSyncTotalSize int64 `json:"maxSyncTotalSize" required:"false" doc:"Max total bytes a directory sync or backup copies, 0 for no limit; defaults to gitSyncMaxTotalSizeMb, env var wins"`
		AutoSync         bool  `json:"autoSync" required:"false" doc:"Run the sync on its interval; defaults to false, or true for a backup or bidirectional sync"`
		SyncDirectory    bool  `json:"syncDirectory" required:"false" doc:"Copy the compose file's whole directory, not only compose, override and .env; bidirectional also pulls files outside backupPaths"`

		PullImageAfterSync bool `json:"pullImageAfterSync" required:"false" doc:"Pull the project images after a sync that changed files when no redeploy follows"`

		RedeployAfterSync bool `json:"redeployAfterSync" required:"false" doc:"Redeploy the project after a sync that changed files even when it is stopped; a running project always is"`
		BackupOnSave      bool `json:"backupOnSave" required:"false" doc:"Run the sync when project files are saved in Arcane; defaults to true, backup and bidirectional syncs only"`
		BackupPending     bool `json:"backupPending" readOnly:"true"`
	}

	GitOpsSyncCounts struct {
		TotalSyncs         int `json:"totalSyncs"`
		ActiveSyncs        int `json:"activeSyncs"`
		SuccessfulSyncs    int `json:"successfulSyncs"`
		DeploySyncs        int `json:"deploySyncs"`
		BackupSyncs        int `json:"backupSyncs"`
		BidirectionalSyncs int `json:"bidirectionalSyncs"`
	}

	GitOpsPreDeployConfigRequest struct {
		PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty" doc:"Script run in a throwaway container before each deploy, relative to the project; needs lifecycleEnabled and syncDirectory"`

		PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty" doc:"Image that runs the pre-deploy script; empty uses the lifecycleDefaultRunnerImage setting"`

		PreDeployEnv         *string `json:"preDeployEnv,omitempty" doc:"KEY=VALUE lines in .env format, passed to the pre-deploy script as environment variables"`
		PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty" doc:"Extra bind mounts for the pre-deploy runner, one src:target[:ro|:rw] per line, both paths absolute"`

		PreDeployTimeoutSec *int `json:"preDeployTimeoutSec,omitempty" doc:"Pre-deploy script timeout in seconds, default 60; at least 1 and at most the lifecycleMaxTimeoutSec setting"`

		PreDeployNetworkMode *string `json:"preDeployNetworkMode,omitempty" doc:"Network mode of the pre-deploy runner: none (default), bridge, host or a network name"`
	}

	GitOpsSyncResult struct {
		SyncedAt time.Time `json:"syncedAt"`
		Error    *string   `json:"error,omitempty"`
		Message  string    `json:"message"`
		Success  bool      `json:"success"`
	}

	FileTreeNodeType string

	FileTreeNode struct {
		Name     string           `json:"name"`
		Path     string           `json:"path"`
		Type     FileTreeNodeType `json:"type"`
		Children []FileTreeNode   `json:"children,omitempty"`

		Size int64 `json:"size,omitempty"`
	}

	GitOpsBrowseResponse struct {
		Path  string         `json:"path"`
		Files []FileTreeNode `json:"files"`
	}

	GitBranchInfo struct {
		Name      string `json:"name"`
		IsDefault bool   `json:"isDefault"`
	}

	GitBranchesResponse struct {
		Branches []GitBranchInfo `json:"branches"`
	}

	GitRepositorySync struct {
		CreatedAt              time.Time `json:"createdAt"`
		UpdatedAt              time.Time `json:"updatedAt"`
		Description            *string   `json:"description,omitempty" unorm:"nfc" trim:"true"`
		ID                     string    `json:"id" binding:"required"`
		Name                   string    `json:"name" binding:"required" unorm:"nfc" trim:"true"`
		URL                    string    `json:"url" binding:"required"`
		Provider               string    `json:"provider,omitempty"`
		AuthType               string    `json:"authType" binding:"required"`
		Username               string    `json:"username,omitempty"`
		Token                  string    `json:"token,omitempty"`
		SSHKey                 string    `json:"sshKey,omitempty"`
		SSHHostKeyVerification string    `json:"sshHostKeyVerification,omitempty"`
		CommitAuthorName       string    `json:"commitAuthorName,omitempty"`
		CommitAuthorEmail      string    `json:"commitAuthorEmail,omitempty"`
		SigningKey             string    `json:"signingKey,omitempty"`
		SigningKeyPassphrase   string    `json:"signingKeyPassphrase,omitempty"`
		Enabled                bool      `json:"enabled"`
	}

	GitRepositorySyncRequest struct {
		Repositories []GitRepositorySync `json:"repositories" binding:"required"`
	}

	GitOpsSyncStatus struct {
		NextSyncAt          *time.Time `json:"nextSyncAt,omitempty"`
		LastSyncAt          *time.Time `json:"lastSyncAt,omitempty"`
		LastSyncStatus      *string    `json:"lastSyncStatus,omitempty"`
		LastSyncError       *string    `json:"lastSyncError,omitempty"`
		LastSyncCommit      *string    `json:"lastSyncCommit,omitempty"`
		BackupFailureReason *string    `json:"backupFailureReason,omitempty"`
		LastBackupAt        *time.Time `json:"lastBackupAt,omitempty"`
		ID                  string     `json:"id"`
		Mode                string     `json:"mode"`
		BackupState         string     `json:"backupState,omitempty"`
		AutoSync            bool       `json:"autoSync"`
		BackupPending       bool       `json:"backupPending"`
	}

	ImportGitOpsSyncRequest struct {
		GitOpsPreDeployConfigRequest

		SyncDirectory *bool `json:"syncDirectory,omitempty" doc:"Copy the compose file's whole directory tree, not only the compose, override and .env files"`

		MaxSyncFiles *int `json:"maxSyncFiles,omitempty" doc:"Max files a directory sync copies, 0 for no limit; defaults to gitSyncMaxFiles, env var wins"`

		MaxSyncTotalSize *int64 `json:"maxSyncTotalSize,omitempty" doc:"Max total bytes a directory sync copies, 0 for no limit; defaults to gitSyncMaxTotalSizeMb, env var wins"`

		MaxSyncBinarySize  *int64 `json:"maxSyncBinarySize,omitempty" doc:"Directory syncs skip binary files over this many bytes, 0 for no limit; defaults to gitSyncMaxBinarySizeMb, env var wins"`
		PullImageAfterSync *bool  `json:"pullImageAfterSync,omitempty" doc:"Pull the project images after a sync that changed files when no redeploy follows"`
		RedeployAfterSync  *bool  `json:"redeployAfterSync,omitempty" doc:"Redeploy the project after a sync that changed files even when it is stopped; a running project always is"`
		SyncName           string `json:"syncName"`
		GitRepo            string `json:"gitRepo" doc:"Name of an existing Git repository to sync from"`
		Branch             string `json:"branch"`
		DockerComposePath  string `json:"dockerComposePath" doc:"Compose file path in the repository"`

		ProjectName string `json:"projectName,omitempty" doc:"Name of the project the sync deploys; defaults to syncName"`

		SyncInterval int  `json:"syncInterval" doc:"Minutes between automatic syncs"`
		AutoSync     bool `json:"autoSync"`
	}

	ImportGitOpsSyncResponse struct {
		Errors       []string `json:"errors"`
		SuccessCount int      `json:"successCount"`
		FailedCount  int      `json:"failedCount"`
	}

	GitOpsBackupFileChange struct {
		Path   string `json:"path"`
		Change string `json:"change"`
	}

	GitOpsBackupPreview struct {
		State        string                   `json:"state"`
		RemoteCommit string                   `json:"remoteCommit,omitempty"`
		Changes      []GitOpsBackupFileChange `json:"changes"`
		Conflicts    []GitOpsBackupFileChange `json:"conflicts"`
		Files        []string                 `json:"files"`
	}

	GitOpsBackupHistoryEntry struct {
		Commit  string    `json:"commit"`
		Author  string    `json:"author"`
		Message string    `json:"message"`
		Date    time.Time `json:"date"`
		Files   []string  `json:"files"`
	}

	GitOpsBackupHistoryResponse struct {
		Entries []GitOpsBackupHistoryEntry `json:"entries"`
	}

	GitOpsBackupFileDiff struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}

	GitOpsBackupRevision struct {
		Entry GitOpsBackupHistoryEntry `json:"entry"`
		Diffs []GitOpsBackupFileDiff   `json:"diffs"`
	}

	GitOpsResolveBackupConflictRequest struct {
		Strategy string `json:"strategy" binding:"required,oneof=use_arcane use_repository" enum:"use_arcane,use_repository" doc:"Which side's files win; use_repository needs a bidirectional sync"`
	}
)

const (
	GitOpsSyncModeDeploy                                    = "deploy"
	GitOpsSyncModeBackup                                    = "backup"
	GitOpsSyncModeBidirectional                             = "bidirectional"
	GitOpsBackupStateNever                                  = "never"
	GitOpsBackupStatePending                                = "pending"
	GitOpsBackupStateBackingUp                              = "backing_up"
	GitOpsBackupStateBackedUp                               = "backed_up"
	GitOpsBackupStatePaused                                 = "paused"
	GitOpsBackupStateFailed                                 = "failed"
	GitOpsBackupStateNeedsAttention                         = "needs_attention"
	GitOpsBackupFailureRepository                           = "repository"
	GitOpsBackupFailureAuth                                 = "auth"
	GitOpsBackupFailureProjectMissing                       = "project_missing"
	GitOpsBackupFailureSnapshot                             = "snapshot"
	GitOpsBackupFailureUnreadableFiles                      = "unreadable_files"
	GitOpsBackupFailureLimits                               = "limits"
	GitOpsBackupFailureConflict                             = "conflict"
	GitOpsBackupFailureDestinationOccupied                  = "destination_occupied"
	GitOpsBackupFailurePushRejected                         = "push_rejected"
	GitOpsBackupConflictUseArcane                           = "use_arcane"
	GitOpsBackupConflictUseRepository                       = "use_repository"
	FileTreeNodeTypeFile                   FileTreeNodeType = "file"
	FileTreeNodeTypeDirectory              FileTreeNodeType = "directory"
)

func (s GitOpsSync) Environment() EnvironmentRef {
	return EnvironmentRef{ID: s.EnvironmentID, Name: s.EnvironmentName}
}

func (s *GitOpsSync) SetEnvironment(ref EnvironmentRef) {
	s.EnvironmentID, s.EnvironmentName = ref.ID, ref.Name
}

// Configuring the hook is gated behind the gitops:lifecycle permission.
func (r GitOpsPreDeployConfigRequest) HasPreDeployConfig() bool {
	return r != GitOpsPreDeployConfigRequest{}
}
