package project

import (
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
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

	// Tags are UI-managed tags to attach to the new project.
	//
	// Required: false
	Tags []string `json:"tags,omitempty"`

	// TagColors maps normalized UI-managed tag names to their display colors.
	//
	// Required: false
	TagColors map[string]TagColor `json:"tagColors,omitempty"`
}

// TagSource identifies where a project tag is managed.
type TagSource string

// TagColor identifies a supported project tag display color.
type TagColor string

const (
	// TagSourceUI identifies a tag association managed through Arcane.
	TagSourceUI TagSource = "ui"
	// TagSourceCompose identifies a tag association managed through Compose metadata.
	TagSourceCompose TagSource = "compose"

	// TagColorGray is the neutral gray tag color.
	TagColorGray TagColor = "gray"
	// TagColorPurple is the purple tag color.
	TagColorPurple TagColor = "purple"
	// TagColorBlue is the blue tag color.
	TagColorBlue TagColor = "blue"
	// TagColorGreen is the green tag color.
	TagColorGreen TagColor = "green"
	// TagColorYellow is the yellow tag color.
	TagColorYellow TagColor = "yellow"
	// TagColorOrange is the orange tag color.
	TagColorOrange TagColor = "orange"
	// TagColorRed is the red tag color.
	TagColorRed TagColor = "red"
	// TagColorPink is the pink tag color.
	TagColorPink TagColor = "pink"
)

// Tag is a normalized project tag and all of its effective sources.
type Tag struct {
	Name    string      `json:"name"`
	Color   TagColor    `json:"color"`
	Sources []TagSource `json:"sources"`
}

// TagOption is a reusable tag name and color returned by the tag catalog.
type TagOption struct {
	Name  string   `json:"name"`
	Color TagColor `json:"color"`
}

// UpdateTag mutates a UI-managed tag association.
type UpdateTag struct {
	Name     string   `json:"name"`
	Attached bool     `json:"attached"`
	Color    TagColor `json:"color,omitempty"`
}

// UpdateTagResponse is returned after a project tag mutation.
type UpdateTagResponse struct {
	Tags       []Tag   `json:"tags"`
	ActivityID *string `json:"activityId,omitempty"`
}

// UpdateProject is used to update a project.
type UpdateProject struct {
	// Name of the project.
	//
	// Required: false
	Name *string `json:"name,omitzero"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent *string `json:"composeContent,omitzero"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent *string `json:"envContent,omitzero"`

	// OverrideContent is the Docker Compose override file content merged on top of
	// the base compose file at deploy. Its tri-state controls the on-disk override:
	// nil leaves any existing override untouched (it still participates in
	// validation of a base-compose edit); a non-nil blank string deletes the
	// override file; a non-nil non-empty string writes it.
	//
	// Required: false
	OverrideContent *string `json:"overrideContent,omitzero"`
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

	// RecreateVolumes allows compose to recreate volumes whose configuration
	// diverged from the compose file. The volume's data is lost.
	//
	// Required: false
	RecreateVolumes bool `json:"recreateVolumes,omitempty"`
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
	// Values: has_update | up_to_date | not_pulled | unknown | error
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

	// ImagesNotPulled is the number of project image references not present locally.
	//
	// Required: true
	ImagesNotPulled int `json:"imagesNotPulled"`

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

	// NotPulledImageRefs is the subset of project image references not present locally.
	//
	// Required: false
	NotPulledImageRefs []string `json:"notPulledImageRefs,omitempty"`

	// UpdateInfoByRef contains the latest persisted per-image update result keyed by image reference.
	//
	// Required: false
	UpdateInfoByRef map[string]imagetypes.UpdateInfo `json:"updateInfoByRef,omitempty"`

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
	IncludeRuntimeServices bool
	IncludeUpdateInfo      bool
}

func AllDetails() DetailsOptions {
	return DetailsOptions{
		IncludeComposeContent:  true,
		IncludeEnvState:        true,
		IncludeIncludeFiles:    true,
		IncludeServiceConfigs:  true,
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

	// Tags are the effective UI and Compose tags for the project.
	Tags []Tag `json:"tags"`
}

// Details contains detailed information about a project.
type Details struct {
	// Tags are the effective UI and Compose tags for the project.
	Tags []Tag `json:"tags"`

	// StatusReason provides additional information about the status.
	//
	// Required: false
	StatusReason *string `json:"statusReason,omitempty"`

	// ActivityID is the activity created by the project action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`

	// LastSyncCommit is the last commit synced from Git (if GitOps managed).
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty"`

	// GitOpsManagedBy is the ID of the GitOps sync managing this project (if any).
	//
	// Required: false
	GitOpsManagedBy *string `json:"gitOpsManagedBy,omitempty"`

	// UpdateInfo contains aggregated image update status for the project.
	//
	// Required: false
	UpdateInfo *UpdateInfo `json:"updateInfo,omitempty"`

	// ArchivedAt is the date and time when the project was archived.
	//
	// Required: false
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`

	// Status is the current status of the project.
	//
	// Required: true
	Status string `json:"status"`

	// DirName is the directory name where the project is stored.
	//
	// Required: false
	DirName string `json:"dirName,omitempty"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent string `json:"composeContent,omitempty"`

	// ComposeFileName is the detected compose file name for the project.
	//
	// Required: false
	ComposeFileName string `json:"composeFileName,omitempty"`

	// ComposeFiles is the ordered project-relative list of compose files selected
	// by a COMPOSE_FILE entry in the project's .env. It is populated only when
	// COMPOSE_FILE selects more than one file; otherwise it is empty and
	// ComposeFileName is authoritative.
	//
	// Required: false
	ComposeFiles []string `json:"composeFiles,omitempty"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// OverrideContent is the Docker Compose override file content, when present.
	//
	// Required: false
	OverrideContent string `json:"overrideContent,omitempty"`

	// OverrideFileName is the detected compose override file name, when present.
	//
	// Required: false
	OverrideFileName string `json:"overrideFileName,omitempty"`

	// Name of the project.
	//
	// Required: true
	Name string `json:"name"`

	// GitRepositoryURL is the URL of the Git repository (if GitOps managed).
	//
	// Required: false
	GitRepositoryURL string `json:"gitRepositoryURL,omitempty"`

	// IconLightURL is the optional light stack icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty"`

	// RelativePath is the path to the project directory relative to the configured projects root.
	//
	// Required: false
	RelativePath string `json:"relativePath,omitempty"`
	// ID is the unique identifier of the project.
	//
	// Required: true
	ID string `json:"id"`

	// IconDarkURL is the optional dark stack icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty"`

	// Path is the file path to the project.
	//
	// Required: true
	Path string `json:"path"`

	// UpdatedAt is the date and time when the project was last updated.
	//
	// Required: true
	UpdatedAt string `json:"updatedAt"`

	// CreatedAt is the date and time when the project was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt"`

	// RuntimeServices contains live container status information for each service.
	//
	// Required: false
	RuntimeServices []RuntimeService `json:"runtimeServices,omitempty"`

	// Services is a list of services defined in the Docker Compose file.
	//
	// Required: false
	Services []composetypes.ServiceConfig `json:"services,omitempty"`

	// URLs are optional custom stack URLs from compose metadata.
	//
	// Required: false
	URLs []string `json:"urls,omitempty"`

	// IncludeFiles is a list of included files in the project.
	//
	// Required: false
	IncludeFiles []IncludeFile `json:"includeFiles,omitempty"`

	// RunningCount is the number of running services in the project.
	//
	// Required: true
	RunningCount int `json:"runningCount"`

	// ServiceCount is the total number of services in the project.
	//
	// Required: true
	ServiceCount int `json:"serviceCount"`

	// IsDiscovered indicates whether this row was derived from runtime Compose labels instead of an Arcane project record.
	//
	// Required: false
	IsDiscovered bool `json:"isDiscovered,omitempty"`

	// IsArchived indicates whether the project is hidden from the default project list.
	//
	// Required: true
	IsArchived bool `json:"isArchived"`

	// HasBuildDirective indicates whether any Compose service defines a build directive.
	//
	// Required: false
	HasBuildDirective bool `json:"hasBuildDirective,omitempty"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this project.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty"`
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
