package project

import (
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/containerregistry"
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
	// Required: true
	Content string `json:"content"`
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

	// ServiceConfig is the configuration of the service from the compose file.
	//
	// Required: false
	ServiceConfig *composetypes.ServiceConfig `json:"serviceConfig,omitempty"`
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

	// CreatedAt is the date and time when the project was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt"`

	// UpdatedAt is the date and time when the project was last updated.
	//
	// Required: true
	UpdatedAt string `json:"updatedAt"`
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

	// Path is the file path to the project.
	//
	// Required: true
	Path string `json:"path"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent string `json:"composeContent,omitempty"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// IncludeFiles is a list of included files in the project.
	//
	// Required: false
	IncludeFiles []IncludeFile `json:"includeFiles,omitempty"`

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
}

// Destroy is used to destroy a project.
type Destroy struct {
	// RemoveFiles indicates if project files should be removed.
	//
	// Required: false
	RemoveFiles bool `json:"removeFiles,omitempty"`

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
}

// ImagePullRequest is used to pull images for a project.
type ImagePullRequest struct {
	// Credentials is a list of container registry credentials for pulling images.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
}
