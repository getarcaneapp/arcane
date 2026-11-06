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
		Name                   string    `json:"name" required:"false" unorm:"nfc" trim:"true"`
		URL                    string    `json:"url" required:"false"`
		AuthType               string    `json:"authType" required:"false"`
		Username               string    `json:"username,omitempty"`
		SSHHostKeyVerification string    `json:"sshHostKeyVerification,omitempty"`
		CommitAuthorName       string    `json:"commitAuthorName,omitempty" unorm:"nfc" trim:"true"`
		CommitAuthorEmail      string    `json:"commitAuthorEmail,omitempty" trim:"true"`

		Token                string `json:"token,omitempty" writeOnly:"true" copier:"-"`
		SSHKey               string `json:"sshKey,omitempty" writeOnly:"true" copier:"-"`
		SigningKey           string `json:"signingKey,omitempty" writeOnly:"true" copier:"-"`
		SigningKeyPassphrase string `json:"signingKeyPassphrase,omitempty" writeOnly:"true" copier:"-"`
		HasToken             bool   `json:"hasToken" readOnly:"true"`
		HasSshKey            bool   `json:"hasSshKey" readOnly:"true"`
		HasSigningKey        bool   `json:"hasSigningKey" readOnly:"true"`
		Enabled              bool   `json:"enabled" required:"false"`
	}

	GitOpsSync struct {
		CreatedAt time.Time `json:"createdAt" readOnly:"true"`
		UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

		PreDeployEnv           *string        `json:"preDeployEnv,omitempty"`
		PreDeployLastRunOutput *string        `json:"preDeployLastRunOutput,omitempty" readOnly:"true"`
		Repository             *GitRepository `json:"repository,omitempty" readOnly:"true"`
		PreDeployLastRunStatus *string        `json:"preDeployLastRunStatus,omitempty" readOnly:"true"`
		PreDeployLastRunAt     *time.Time     `json:"preDeployLastRunAt,omitempty" readOnly:"true"`
		PreDeployExtraMounts   *string        `json:"preDeployExtraMounts,omitempty"`
		LastSyncAt             *time.Time     `json:"lastSyncAt,omitempty" readOnly:"true"`
		ProjectID              *string        `json:"projectId,omitempty"`

		// Required whenever PreDeployScriptPath is set.
		PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty"`

		PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty"`
		LastSyncCommit      *string `json:"lastSyncCommit,omitempty" readOnly:"true"`

		SyncedFiles         *string    `json:"syncedFiles,omitempty" readOnly:"true"`
		LastSyncError       *string    `json:"lastSyncError,omitempty" readOnly:"true"`
		LastSyncStatus      *string    `json:"lastSyncStatus,omitempty" readOnly:"true"`
		BackupFailureReason *string    `json:"backupFailureReason,omitempty" readOnly:"true"`
		LastBackupAt        *time.Time `json:"lastBackupAt,omitempty" readOnly:"true"`
		ProjectName         string     `json:"projectName" required:"false"`

		PreDeployNetworkMode string `json:"preDeployNetworkMode" required:"false"`
		Name                 string `json:"name" required:"false"`
		EnvironmentID        string `json:"environmentId" readOnly:"true"`

		EnvironmentName string   `json:"environmentName,omitempty" readOnly:"true" doc:"Display name of the environment the sync belongs to"`
		RepositoryID    string   `json:"repositoryId" required:"false"`
		Branch          string   `json:"branch" required:"false"`
		ComposePath     string   `json:"composePath" required:"false"`
		ID              string   `json:"id" readOnly:"true"`
		TargetType      string   `json:"targetType" required:"false"`
		Mode            string   `json:"mode" required:"false"`
		BackupDirectory string   `json:"backupDirectory,omitempty"`
		BackupState     string   `json:"backupState,omitempty" readOnly:"true"`
		BackupPaths     []string `json:"backupPaths,omitempty"`

		PreDeployTimeoutSec int `json:"preDeployTimeoutSec" required:"false"`

		MaxSyncBinarySize int64 `json:"maxSyncBinarySize" required:"false"`

		SyncInterval int `json:"syncInterval" required:"false"`

		// 0 means unlimited; env var overrides take precedence.
		MaxSyncFiles int `json:"maxSyncFiles" required:"false"`

		MaxSyncTotalSize int64 `json:"maxSyncTotalSize" required:"false"`
		AutoSync         bool  `json:"autoSync" required:"false"`
		SyncDirectory    bool  `json:"syncDirectory" required:"false"`

		PullImageAfterSync bool `json:"pullImageAfterSync" required:"false"`

		RedeployAfterSync bool `json:"redeployAfterSync" required:"false"`
		BackupOnSave      bool `json:"backupOnSave" required:"false"`
		BackupPending     bool `json:"backupPending" readOnly:"true"`
	}

	GitOpsSyncCounts struct {
		TotalSyncs      int `json:"totalSyncs"`
		ActiveSyncs     int `json:"activeSyncs"`
		SuccessfulSyncs int `json:"successfulSyncs"`
		DeploySyncs     int `json:"deploySyncs"`
		BackupSyncs     int `json:"backupSyncs"`
	}

	GitOpsPreDeployConfigRequest struct {
		PreDeployScriptPath *string `json:"preDeployScriptPath,omitempty"`

		// When omitted, the lifecycleDefaultRunnerImage setting is used.
		PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty"`

		PreDeployEnv         *string `json:"preDeployEnv,omitempty"`
		PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty"`

		PreDeployTimeoutSec *int `json:"preDeployTimeoutSec,omitempty"`

		PreDeployNetworkMode *string `json:"preDeployNetworkMode,omitempty"`
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

		SyncDirectory *bool `json:"syncDirectory,omitempty"`

		// 0 means unlimited; env var overrides take precedence.
		MaxSyncFiles *int `json:"maxSyncFiles,omitempty"`

		MaxSyncTotalSize *int64 `json:"maxSyncTotalSize,omitempty"`

		MaxSyncBinarySize  *int64 `json:"maxSyncBinarySize,omitempty"`
		PullImageAfterSync *bool  `json:"pullImageAfterSync,omitempty"`
		RedeployAfterSync  *bool  `json:"redeployAfterSync,omitempty"`
		SyncName           string `json:"syncName"`
		GitRepo            string `json:"gitRepo"`
		Branch             string `json:"branch"`
		DockerComposePath  string `json:"dockerComposePath"`

		ProjectName string `json:"projectName,omitempty"`

		SyncInterval int  `json:"syncInterval"`
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
		Strategy string `json:"strategy" binding:"required,oneof=use_arcane"`
	}
)

const (
	GitOpsSyncModeDeploy                                    = "deploy"
	GitOpsSyncModeBackup                                    = "backup"
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
