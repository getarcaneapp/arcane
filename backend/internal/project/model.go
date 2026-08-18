package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"

	"time"
)

type ProjectStatus string

const (
	ProjectStatusRunning          ProjectStatus = "running"
	ProjectStatusStopped          ProjectStatus = "stopped"
	ProjectStatusPartiallyRunning ProjectStatus = "partially running"
	ProjectStatusUnknown          ProjectStatus = "unknown"
	ProjectStatusDeploying        ProjectStatus = "deploying"
	ProjectStatusStopping         ProjectStatus = "stopping"
	ProjectStatusRestarting       ProjectStatus = "restarting"
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
	TargetType             string     `json:"targetType" gorm:"column:target_type;default:'project'"`                         // "project" or "swarm_stack"
	ProjectName            string     `json:"projectName" sortable:"true" search:"project,name,stack,application,service"`    // Name of project to create/update
	PreDeployNetworkMode   string     `json:"preDeployNetworkMode" gorm:"column:pre_deploy_network_mode;default:'none'"`      // Docker network mode passed to the runner container. Default "none" denies network access; set to "bridge", "host", or a named network when the script needs it.
	SyncInterval           int        `json:"syncInterval" sortable:"true" search:"interval,frequency,schedule,cron,minutes"` // in minutes
	MaxSyncFiles           int        `json:"maxSyncFiles" gorm:"column:max_sync_files;default:500"`                          // 0 = unlimited; env var overrides take precedence
	MaxSyncTotalSize       int64      `json:"maxSyncTotalSize" gorm:"column:max_sync_total_size;default:52428800"`            // bytes; 0 = unlimited; env var overrides take precedence
	MaxSyncBinarySize      int64      `json:"maxSyncBinarySize" gorm:"column:max_sync_binary_size;default:10485760"`          // bytes; 0 = unlimited; env var overrides take precedence
	PreDeployTimeoutSec    int        `json:"preDeployTimeoutSec" gorm:"column:pre_deploy_timeout_sec;default:60"`
	AutoSync               bool       `json:"autoSync" sortable:"true" search:"auto,automatic,sync,continuous,scheduled"`
	SyncDirectory          bool       `json:"syncDirectory" gorm:"column:sync_directory"` // Sync entire directory containing compose file
}

func (GitOpsSync) TableName() string {
	return "gitops_syncs"
}
