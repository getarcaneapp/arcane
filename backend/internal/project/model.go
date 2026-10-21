package project

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
)

type ProjectStatus string

const (
	ProjectStatusRunning          ProjectStatus = types.ProjectStatusRunning
	ProjectStatusStopped          ProjectStatus = types.ProjectStatusStopped
	ProjectStatusPartiallyRunning ProjectStatus = types.ProjectStatusPartiallyRunning
	ProjectStatusUnknown          ProjectStatus = types.ProjectStatusUnknown
	ProjectStatusDeploying        ProjectStatus = types.ProjectStatusDeploying
	ProjectStatusStopping         ProjectStatus = types.ProjectStatusStopping
	ProjectStatusRestarting       ProjectStatus = types.ProjectStatusRestarting
)

type Project struct {
	database.BaseModel

	Name               string        `json:"name" sortable:"true" gorm:"index:idx_projects_name"`
	DirName            *string       `json:"dir_name"`
	Path               string        `json:"path" sortable:"true" gorm:"uniqueIndex"`
	Status             ProjectStatus `json:"status" sortable:"true"`
	StatusReason       *string       `json:"status_reason"`
	ServiceCount       int           `json:"service_count" sortable:"true"`
	RunningCount       int           `json:"running_count" sortable:"true"`
	GitOpsManagedBy    *string       `json:"gitops_managed_by,omitempty" gorm:"column:gitops_managed_by"`
	ComposeProjectName *string       `json:"compose_project_name,omitempty" gorm:"column:compose_project_name"`
	ImageRefsJSON      string        `json:"image_refs_json,omitempty" gorm:"column:image_refs_json"`
	BuildImageRefsJSON *string       `json:"-" gorm:"column:build_image_refs_json"`
	IsArchived         bool          `json:"is_archived" gorm:"column:is_archived;default:false;index"`
	ArchivedAt         *time.Time    `json:"archived_at,omitempty" gorm:"column:archived_at"`
}

func (Project) TableName() string {
	return "projects"
}

// ProjectTag stores one normalized tag association and its management source.
type ProjectTag struct {
	ProjectID string `json:"projectId" gorm:"column:project_id;primaryKey"`
	Name      string `json:"name" gorm:"column:name;primaryKey"`
	Source    string `json:"source" gorm:"column:source;primaryKey"`
	Color     string `json:"color" gorm:"column:color;not null;default:gray"`
}

// TableName returns the database table used for project tag associations.
func (ProjectTag) TableName() string {
	return "project_tags"
}

type GitOpsSync struct {
	database.BaseModel

	Environment    *environment.Environment `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID"`
	Repository     *gitrepo.GitRepository   `json:"repository,omitempty" gorm:"foreignKey:RepositoryID"`
	ProjectID      *string                  `json:"projectId,omitempty" sortable:"true"` // Set after project is created
	Project        *Project                 `json:"project,omitempty" gorm:"foreignKey:ProjectID"`
	SyncedFiles    *string                  `json:"syncedFiles,omitempty" gorm:"column:synced_files"` // JSON array of synced file paths
	LastSyncAt     *time.Time               `json:"lastSyncAt,omitempty" sortable:"true"`
	LastSyncStatus *string                  `json:"lastSyncStatus,omitempty" search:"status,success,failed,pending,error"`
	LastSyncError  *string                  `json:"lastSyncError,omitempty"`
	LastSyncCommit *string                  `json:"lastSyncCommit,omitempty" search:"commit,hash,sha,revision"`

	// Pre-deploy lifecycle hook (configuration)
	// When PreDeployScriptPath is set, the named script is executed in a
	// throwaway container before each deploy of the linked project. The script,
	// runner image, and execution context together act as repo-trusted code —
	// any push to the repo that changes the script will run unreviewed on the
	// next deploy. See docs for details.
	PreDeployScriptPath  *string `json:"preDeployScriptPath,omitempty" gorm:"column:pre_deploy_script_path" search:"lifecycle,hook,pre-deploy,script,path"`
	PreDeployRunnerImage *string `json:"preDeployRunnerImage,omitempty" gorm:"column:pre_deploy_runner_image"`
	PreDeployEnv         *string `json:"preDeployEnv,omitempty" gorm:"column:pre_deploy_env"`                  // KEY=VALUE lines, one per line; same format as .env files
	PreDeployExtraMounts *string `json:"preDeployExtraMounts,omitempty" gorm:"column:pre_deploy_extra_mounts"` // docker -v style "src:tgt[:ro|:rw]" entries, one per line

	// Pre-deploy lifecycle hook (last-run state)
	PreDeployLastRunAt     *time.Time `json:"preDeployLastRunAt,omitempty" gorm:"column:pre_deploy_last_run_at" sortable:"true"`
	PreDeployLastRunStatus *string    `json:"preDeployLastRunStatus,omitempty" gorm:"column:pre_deploy_last_run_status" sortable:"true"` // "success" | "failed" | "timeout"
	PreDeployLastRunOutput *string    `json:"preDeployLastRunOutput,omitempty" gorm:"column:pre_deploy_last_run_output"`                 // truncated stdout+stderr
	Name                   string     `json:"name" sortable:"true" search:"sync,gitops,automation,deploy,deployment,continuous"`
	EnvironmentID          string     `json:"environmentId" sortable:"true"`
	RepositoryID           string     `json:"repositoryId" sortable:"true"`
	Branch                 string     `json:"branch" sortable:"true" search:"branch,main,master,develop,feature,release"`
	ComposePath            string     `json:"composePath" sortable:"true" search:"compose,docker-compose,path,file,yaml,yml"`
	TargetType             string     `json:"targetType" gorm:"column:target_type;default:'project'"`                      // "project" or "swarm_stack"
	ProjectName            string     `json:"projectName" sortable:"true" search:"project,name,stack,application,service"` // Name of project to create/update
	// Docker network mode for the runner: "none" denies access; "bridge", "host", or a named network enables it.
	PreDeployNetworkMode string `json:"preDeployNetworkMode" gorm:"column:pre_deploy_network_mode;default:'none'"`
	SyncInterval         int    `json:"syncInterval" sortable:"true" search:"interval,frequency,schedule,cron,minutes"` // in minutes
	MaxSyncFiles         int    `json:"maxSyncFiles" gorm:"column:max_sync_files;default:500"`                          // 0 = unlimited; env var overrides take precedence
	MaxSyncTotalSize     int64  `json:"maxSyncTotalSize" gorm:"column:max_sync_total_size;default:52428800"`            // bytes; 0 = unlimited; env var overrides take precedence
	MaxSyncBinarySize    int64  `json:"maxSyncBinarySize" gorm:"column:max_sync_binary_size;default:10485760"`          // bytes; 0 = unlimited; env var overrides take precedence
	PreDeployTimeoutSec  int    `json:"preDeployTimeoutSec" gorm:"column:pre_deploy_timeout_sec;default:60"`
	AutoSync             bool   `json:"autoSync" sortable:"true" search:"auto,automatic,sync,continuous,scheduled"`
	SyncDirectory        bool   `json:"syncDirectory" gorm:"column:sync_directory"` // Sync entire directory containing compose file
	PullImageAfterSync   bool   `json:"pullImageAfterSync" gorm:"column:pull_image_after_sync;default:false"`
	RedeployAfterSync    bool   `json:"redeployAfterSync" gorm:"column:redeploy_after_sync;default:false"`

	// Backup mode ("backup" commits saved project files to the repository;
	// "deploy" is the original pull direction).
	Mode                string               `json:"mode" gorm:"column:mode;default:'deploy'" sortable:"true" search:"mode,backup,deploy,direction"`
	BackupDirectory     string               `json:"backupDirectory" gorm:"column:backup_directory" sortable:"true" search:"backup,directory,destination,folder"`
	BackupPaths         database.StringSlice `json:"backupPaths" gorm:"column:backup_paths;type:text"`
	BackupOnSave        bool                 `json:"backupOnSave" gorm:"column:backup_on_save;default:true"`
	BackupPending       bool                 `json:"backupPending" gorm:"column:backup_pending;default:false"`
	BackupPendingSince  *time.Time           `json:"backupPendingSince,omitempty" gorm:"column:backup_pending_since"`
	BackupConflict      bool                 `json:"backupConflict" gorm:"column:backup_conflict;default:false"`
	BackupFailureReason *string              `json:"backupFailureReason,omitempty" gorm:"column:backup_failure_reason"`
	LastBackupAt        *time.Time           `json:"lastBackupAt,omitempty" gorm:"column:last_backup_at" sortable:"true"`
	LastBackupSnapshot  *string              `json:"-" gorm:"column:last_backup_snapshot"`
}

// BackupState derives the persisted backup lifecycle state.
func (s GitOpsSync) BackupState() string {
	if s.Mode != types.GitOpsSyncModeBackup {
		return ""
	}
	status := kit.FromPtr(s.LastSyncStatus)
	switch {
	case status == "running":
		return types.GitOpsBackupStateBackingUp
	case s.BackupConflict:
		return types.GitOpsBackupStateNeedsAttention
	case status == "failed":
		return types.GitOpsBackupStateFailed
	case !s.AutoSync:
		return types.GitOpsBackupStatePaused
	case s.BackupPending:
		return types.GitOpsBackupStatePending
	case s.LastBackupAt != nil:
		return types.GitOpsBackupStateBackedUp
	default:
		return types.GitOpsBackupStateNever
	}
}

func (GitOpsSync) TableName() string {
	return "gitops_syncs"
}

// SyncedFileList decodes the tracked synced-file paths.
func (s GitOpsSync) SyncedFileList() []string {
	if kit.FromPtr(s.SyncedFiles) == "" {
		return nil
	}
	var files []string
	if err := json.Unmarshal([]byte(*s.SyncedFiles), &files); err != nil {
		return nil
	}
	return files
}

// BackupSnapshot decodes the file hashes recorded by the last backup push.
func (s GitOpsSync) BackupSnapshot() map[string]string {
	if kit.FromPtr(s.LastBackupSnapshot) == "" {
		return nil
	}
	var hashes map[string]string
	if err := json.Unmarshal([]byte(*s.LastBackupSnapshot), &hashes); err != nil {
		return nil
	}
	return hashes
}

// EncodeSyncedFiles converts tracked synced-file paths to their stored JSON form.
func EncodeSyncedFiles(files []string) *string {
	if len(files) == 0 {
		return nil
	}
	data, err := json.Marshal(files)
	if err != nil {
		return nil
	}
	return new(string(data))
}

// LockProjectForSync row-locks the project so the one-Git-relationship check and the insert are atomic.
func LockProjectForSync(tx *gorm.DB, projectID string) (*Project, error) {
	var project Project
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrProjectNotFound
		}
		return nil, fmt.Errorf("failed to get project %s: %w", projectID, err)
	}
	return &project, nil
}
