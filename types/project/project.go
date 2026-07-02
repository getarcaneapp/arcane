package project

import (
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
)

// IncludeFile represents an included file within a project.
type IncludeFile struct {
	// Path is the absolute path to the include file.
	//
	// Required: true
	Path string `json:"path"`

	// RelativePath is the path to the include file relative to the project.
	//
	// Required: true
	RelativePath string `json:"relativePath"`

	// Content is the file content.
	//
	// Required: false
	Content string `json:"content,omitempty"`
}

// ProjectFile represents a file or folder within a project directory.
type ProjectFile struct {
	// Path is the absolute path to the file or folder.
	//
	// Required: true
	Path string `json:"path"`

	// RelativePath is the path relative to the project directory.
	//
	// Required: true
	RelativePath string `json:"relativePath"`

	// Name is the base name of the file or folder.
	//
	// Required: true
	Name string `json:"name"`

	// IsDirectory indicates whether this entry is a folder.
	//
	// Required: true
	IsDirectory bool `json:"isDirectory"`

	// Size is the file size in bytes. Directories report zero.
	//
	// Required: true
	Size int64 `json:"size"`

	// ModTime is the last modification time.
	//
	// Required: true
	ModTime time.Time `json:"modTime"`

	// Protected indicates that Arcane owns this path and it cannot be renamed,
	// deleted, moved, or overwritten through project file management.
	//
	// Required: false
	Protected bool `json:"protected,omitempty"`

	// Content is the file content when explicitly requested.
	//
	// Required: false
	Content string `json:"content,omitempty"`
}

// ProjectFileDraft is used when creating a project with staged files.
type ProjectFileDraft struct {
	// RelativePath is the path relative to the project directory.
	//
	// Required: true
	RelativePath string `json:"relativePath" binding:"required"`

	// IsDirectory indicates whether the draft creates a folder.
	//
	// Required: true
	IsDirectory bool `json:"isDirectory"`

	// Content is the text file content. It is ignored for folders.
	//
	// Required: false
	Content string `json:"content,omitempty"`
}

// Project file operations accepted by ProjectFileChange.Operation.
const (
	FileOpCreateFile   = "create_file"
	FileOpCreateFolder = "create_folder"
	FileOpUpdateFile   = "update_file"
	FileOpRename       = "rename"
	FileOpMove         = "move"
	FileOpDelete       = "delete"
)

// ProjectFileChange describes one staged file-tree operation.
type ProjectFileChange struct {
	// Operation is one of create_file, create_folder, update_file, rename, move, or delete.
	//
	// Required: true
	Operation string `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete"`

	// RelativePath is the source or target path relative to the project directory.
	//
	// Required: true
	RelativePath string `json:"relativePath" binding:"required"`

	// NewName is used by rename operations. Rename is basename-only and never moves
	// a file or folder to another parent.
	//
	// Required: false
	NewName string `json:"newName,omitempty"`

	// NewParentPath is used by move operations. Empty means project root.
	//
	// Required: false
	NewParentPath string `json:"newParentPath,omitempty"`

	// Content is used by create_file and update_file operations.
	//
	// Required: false
	Content *string `json:"content,omitempty"`

	// Recursive allows deleting a non-empty folder. The UI must require a strong
	// confirmation before sending this flag.
	//
	// Required: false
	Recursive bool `json:"recursive,omitempty"`
}

// FileContentRequest requests the contents of a single project-related file.
type FileContentRequest struct {
	// RelativePath is the path to the file relative to the project.
	//
	// Required: true
	RelativePath string `json:"relativePath" query:"relativePath" binding:"required"`
}

// CreateProject is used to create a new project.
type CreateProject struct {
	// Name of the project.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: true
	ComposeContent string `json:"composeContent" binding:"required"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent *string `json:"envContent,omitempty"`

	// ProjectFiles are optional text files and folders staged during project creation.
	//
	// Required: false
	ProjectFiles []ProjectFileDraft `json:"projectFiles,omitempty" maxItems:"500"`
}

// UpdateProject is used to update a project.
type UpdateProject struct {
	// Name of the project.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent *string `json:"composeContent,omitempty"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent *string `json:"envContent,omitempty"`

	// FileTreeRevision is the revision observed by the client before staging
	// FileChanges. The server rejects stale revisions to avoid clobbering
	// concurrent filesystem changes.
	//
	// Required: false
	FileTreeRevision *string `json:"fileTreeRevision,omitempty"`

	// FileChanges are staged project file-tree operations applied with Save.
	//
	// Required: false
	FileChanges []ProjectFileChange `json:"fileChanges,omitempty" maxItems:"500"`
}

// DeployOptions configures project deploy behavior.
type DeployOptions struct {
	// PullPolicy overrides the image pull policy used during deploy.
	//
	// Required: false
	PullPolicy string `json:"pullPolicy,omitempty" binding:"omitempty,oneof=missing always never"`

	// ForceRecreate forces compose to recreate containers even when unchanged.
	//
	// Required: false
	ForceRecreate bool `json:"forceRecreate,omitempty"`

	// RemoveOrphans removes containers for services not defined in the compose file.
	//
	// Required: false
	RemoveOrphans bool `json:"removeOrphans,omitempty"`
}

// UpdateIncludeFile is used to update an include file within a project.
type UpdateIncludeFile struct {
	// RelativePath is the path to the include file relative to the project.
	//
	// Required: true
	RelativePath string `json:"relativePath" binding:"required"`

	// Content is the file content.
	//
	// Required: true
	Content string `json:"content" binding:"required"`
}

// RuntimeService contains live container status information for a service.
type RuntimeService struct {
	// Name is the service name from the compose file.
	//
	// Required: true
	Name string `json:"name"`

	// Image is the Docker image used by the service.
	//
	// Required: true
	Image string `json:"image"`

	// Status is the current status of the container (running, stopped, etc.).
	//
	// Required: true
	Status string `json:"status"`

	// ContainerID is the Docker container ID.
	//
	// Required: false
	ContainerID string `json:"containerId,omitempty"`

	// ContainerName is the Docker container name.
	//
	// Required: false
	ContainerName string `json:"containerName,omitempty"`

	// Ports is a list of port mappings for the container.
	//
	// Required: false
	Ports []string `json:"ports,omitempty"`

	// Health is the health status of the container.
	//
	// Required: false
	Health *string `json:"health,omitempty"`

	// IconLightURL is an optional light icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty"`

	// IconDarkURL is an optional dark icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty"`

	// ServiceConfig is the configuration of the service from the compose file.
	//
	// Required: false
	ServiceConfig *composetypes.ServiceConfig `json:"serviceConfig,omitempty"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this runtime service.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty"`
}

// UpdateInfo contains aggregated image update status for a project.
type UpdateInfo struct {
	// Status is the aggregate update status for the project.
	//
	// Values: has_update | up_to_date | unknown | error
	// Required: true
	Status string `json:"status"`

	// HasUpdate indicates whether any project image has an available update.
	//
	// Required: true
	HasUpdate bool `json:"hasUpdate"`

	// ImageCount is the total number of unique checkable image references in the project.
	//
	// Required: true
	ImageCount int `json:"imageCount"`

	// CheckedImageCount is the number of project image references with persisted update-check results.
	//
	// Required: true
	CheckedImageCount int `json:"checkedImageCount"`

	// ImagesWithUpdates is the number of project image references with available updates.
	//
	// Required: true
	ImagesWithUpdates int `json:"imagesWithUpdates"`

	// ErrorCount is the number of project image references whose latest check failed.
	//
	// Required: true
	ErrorCount int `json:"errorCount"`

	// ErrorMessage is the first available error message from the latest project image checks.
	//
	// Required: false
	ErrorMessage *string `json:"errorMessage,omitempty"`

	// ImageRefs is the list of unique image references detected for the project.
	//
	// Required: false
	ImageRefs []string `json:"imageRefs,omitempty"`

	// UpdatedImageRefs is the subset of project image references with available updates.
	//
	// Required: false
	UpdatedImageRefs []string `json:"updatedImageRefs,omitempty"`

	// LastCheckedAt is the latest successful or failed image update check time for this project.
	//
	// Required: false
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
}

type DetailsOptions struct {
	IncludeComposeContent  bool
	IncludeEnvState        bool
	IncludeIncludeFiles    bool
	IncludeServiceConfigs  bool
	IncludeDirectoryFiles  bool
	IncludeProjectFiles    bool
	IncludeRuntimeServices bool
	IncludeUpdateInfo      bool
}

func AllDetails() DetailsOptions {
	return DetailsOptions{
		IncludeComposeContent:  true,
		IncludeEnvState:        true,
		IncludeIncludeFiles:    true,
		IncludeServiceConfigs:  true,
		IncludeDirectoryFiles:  true,
		IncludeProjectFiles:    true,
		IncludeRuntimeServices: true,
		IncludeUpdateInfo:      true,
	}
}

// CreateReponse is the response when a project is created.
type CreateReponse struct {
	// ID is the unique identifier of the project.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the project.
	//
	// Required: true
	Name string `json:"name"`

	// DirName is the directory name where the project is stored.
	//
	// Required: false
	DirName string `json:"dirName,omitempty"`

	// RelativePath is the path to the project directory relative to the configured projects root.
	//
	// Required: false
	RelativePath string `json:"relativePath,omitempty"`

	// Path is the file path to the project.
	//
	// Required: true
	Path string `json:"path"`

	// Status is the current status of the project.
	//
	// Required: true
	Status string `json:"status"`

	// StatusReason provides additional information about the status.
	//
	// Required: false
	StatusReason *string `json:"statusReason,omitempty"`

	// ServiceCount is the total number of services in the project.
	//
	// Required: true
	ServiceCount int `json:"serviceCount"`

	// RunningCount is the number of running services in the project.
	//
	// Required: true
	RunningCount int `json:"runningCount"`

	// GitOpsManagedBy is the ID of the GitOps sync managing this project (if any).
	//
	// Required: false
	GitOpsManagedBy *string `json:"gitOpsManagedBy,omitempty"`

	// IsArchived indicates whether the project is hidden from the default project list.
	//
	// Required: true
	IsArchived bool `json:"isArchived"`

	// ArchivedAt is the date and time when the project was archived.
	//
	// Required: false
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`

	// CreatedAt is the date and time when the project was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt"`

	// UpdatedAt is the date and time when the project was last updated.
	//
	// Required: true
	UpdatedAt string `json:"updatedAt"`

	// ActivityID is the activity created by the project action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// Details contains detailed information about a project.
type Details struct {
	// ID is the unique identifier of the project.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the project.
	//
	// Required: true
	Name string `json:"name"`

	// DirName is the directory name where the project is stored.
	//
	// Required: false
	DirName string `json:"dirName,omitempty"`

	// RelativePath is the path to the project directory relative to the configured projects root.
	//
	// Required: false
	RelativePath string `json:"relativePath,omitempty"`

	// Path is the file path to the project.
	//
	// Required: true
	Path string `json:"path"`

	// IconLightURL is the optional light stack icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty"`

	// IconDarkURL is the optional dark stack icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty"`

	// URLs are optional custom stack URLs from compose metadata.
	//
	// Required: false
	URLs []string `json:"urls,omitempty"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent string `json:"composeContent,omitempty"`

	// ComposeFileName is the detected compose file name for the project.
	//
	// Required: false
	ComposeFileName string `json:"composeFileName,omitempty"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// IncludeFiles is a list of included files in the project.
	//
	// Required: false
	IncludeFiles []IncludeFile `json:"includeFiles,omitempty"`

	// DirectoryFiles contains all other files in the project directory
	// (excluding compose files, .env, and include files which are shown separately).
	//
	// Required: false
	DirectoryFiles []IncludeFile `json:"directoryFiles,omitempty"`

	// ProjectFiles contains the editable file tree for project file management.
	//
	// Required: false
	ProjectFiles []ProjectFile `json:"projectFiles,omitempty"`

	// FileTreeRevision identifies the project file tree state returned to the client.
	// Mutations using staged file changes must include this value.
	//
	// Required: false
	FileTreeRevision string `json:"fileTreeRevision,omitempty"`

	// Status is the current status of the project.
	//
	// Required: true
	Status string `json:"status"`

	// StatusReason provides additional information about the status.
	//
	// Required: false
	StatusReason *string `json:"statusReason,omitempty"`

	// ServiceCount is the total number of services in the project.
	//
	// Required: true
	ServiceCount int `json:"serviceCount"`

	// RunningCount is the number of running services in the project.
	//
	// Required: true
	RunningCount int `json:"runningCount"`

	// IsArchived indicates whether the project is hidden from the default project list.
	//
	// Required: true
	IsArchived bool `json:"isArchived"`

	// IsDiscovered indicates whether this row was derived from runtime Compose labels instead of an Arcane project record.
	//
	// Required: false
	IsDiscovered bool `json:"isDiscovered,omitempty"`

	// ArchivedAt is the date and time when the project was archived.
	//
	// Required: false
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`

	// CreatedAt is the date and time when the project was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt"`

	// UpdatedAt is the date and time when the project was last updated.
	//
	// Required: true
	UpdatedAt string `json:"updatedAt"`

	// Services is a list of services defined in the Docker Compose file.
	//
	// Required: false
	Services []composetypes.ServiceConfig `json:"services,omitempty"`

	// RuntimeServices contains live container status information for each service.
	//
	// Required: false
	RuntimeServices []RuntimeService `json:"runtimeServices,omitempty"`

	// UpdateInfo contains aggregated image update status for the project.
	//
	// Required: false
	UpdateInfo *UpdateInfo `json:"updateInfo,omitempty"`

	// HasBuildDirective indicates whether any Compose service defines a build directive.
	//
	// Required: false
	HasBuildDirective bool `json:"hasBuildDirective,omitempty"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this project.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty"`

	// GitOpsManagedBy is the ID of the GitOps sync managing this project (if any).
	//
	// Required: false
	GitOpsManagedBy *string `json:"gitOpsManagedBy,omitempty"`

	// LastSyncCommit is the last commit synced from Git (if GitOps managed).
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty"`

	// GitRepositoryURL is the URL of the Git repository (if GitOps managed).
	//
	// Required: false
	GitRepositoryURL string `json:"gitRepositoryURL,omitempty"`

	// ActivityID is the activity created by the project action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// Destroy is used to destroy a project.
type Destroy struct {
	// RemoveFiles indicates if project files should be removed. Defaults to true when omitted.
	// When false and the project is stored under the projects directory, files are renamed
	// to a hidden .arcane-trash-* directory so filesystem discovery does not re-import them.
	//
	// Required: false
	RemoveFiles *bool `json:"removeFiles,omitempty"`

	// RemoveVolumes indicates if project volumes should be removed.
	//
	// Required: false
	RemoveVolumes bool `json:"removeVolumes,omitempty"`
}

// StatusCounts contains counts of projects by status.
type StatusCounts struct {
	// RunningProjects is the number of running projects.
	//
	// Required: true
	RunningProjects int `json:"runningProjects"`

	// StoppedProjects is the number of stopped projects.
	//
	// Required: true
	StoppedProjects int `json:"stoppedProjects"`

	// TotalProjects is the total number of projects.
	//
	// Required: true
	TotalProjects int `json:"totalProjects"`

	// ArchivedProjects is the number of archived projects.
	//
	// Required: true
	ArchivedProjects int `json:"archivedProjects"`
}

// ImagePullRequest is used to pull images for a project.
type ImagePullRequest struct {
	// Credentials is a list of container registry credentials for pulling images.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
}
