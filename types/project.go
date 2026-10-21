package types

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/go-units"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	buildtypes "go.getarcane.app/builds/types"
)

// ProjectIncludeFile represents an included file within a project.
type ProjectIncludeFile struct {
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
	TagColors map[string]ProjectTagColor `json:"tagColors,omitempty"`
}

// ProjectTagSource identifies where a project tag is managed.
type ProjectTagSource string

// ProjectTagColor identifies a supported project tag display color.
type ProjectTagColor string

const (
	// ProjectTagSourceUI identifies a tag association managed through Arcane.
	ProjectTagSourceUI ProjectTagSource = "ui"
	// ProjectTagSourceCompose identifies a tag association managed through Compose metadata.
	ProjectTagSourceCompose ProjectTagSource = "compose"

	// ProjectTagColorGray is the neutral gray tag color.
	ProjectTagColorGray ProjectTagColor = "gray"
	// ProjectTagColorPurple is the purple tag color.
	ProjectTagColorPurple ProjectTagColor = "purple"
	// ProjectTagColorBlue is the blue tag color.
	ProjectTagColorBlue ProjectTagColor = "blue"
	// ProjectTagColorGreen is the green tag color.
	ProjectTagColorGreen ProjectTagColor = "green"
	// ProjectTagColorYellow is the yellow tag color.
	ProjectTagColorYellow ProjectTagColor = "yellow"
	// ProjectTagColorOrange is the orange tag color.
	ProjectTagColorOrange ProjectTagColor = "orange"
	// ProjectTagColorRed is the red tag color.
	ProjectTagColorRed ProjectTagColor = "red"
	// ProjectTagColorPink is the pink tag color.
	ProjectTagColorPink ProjectTagColor = "pink"
)

// ProjectTag is a normalized project tag and all of its effective sources.
type ProjectTag struct {
	Name    string             `json:"name"`
	Color   ProjectTagColor    `json:"color"`
	Sources []ProjectTagSource `json:"sources"`
}

// ProjectTagOption is a reusable tag name and color returned by the tag catalog.
type ProjectTagOption struct {
	Name  string          `json:"name"`
	Color ProjectTagColor `json:"color"`
}

// ProjectReference identifies a project by ID and name.
type ProjectReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UpdateProjectTag mutates a UI-managed tag association.
type UpdateProjectTag struct {
	Name     string          `json:"name"`
	Attached bool            `json:"attached"`
	Color    ProjectTagColor `json:"color,omitempty"`
}

// UpdateProjectTagResponse is returned after a project tag mutation.
type UpdateProjectTagResponse struct {
	Tags       []ProjectTag `json:"tags"`
	ActivityID *string      `json:"activityId,omitempty"`
}

// ProjectBuildRequest configures a project's image build.
type ProjectBuildRequest struct {
	Services []string `json:"services,omitempty" doc:"Service names to build"`
	Provider string   `json:"provider,omitempty" doc:"Build provider override"`
	Push     *bool    `json:"push,omitempty" doc:"Push images"`
	Load     *bool    `json:"load,omitempty" doc:"Load images into Docker"`
}

// ProjectUpdateServicesRequest selects project services to update.
type ProjectUpdateServicesRequest struct {
	Services []string `json:"services,omitempty" doc:"Service names to update; empty updates all services"`
}

// ProjectDeployOptions configures project deploy behavior.
type ProjectDeployOptions struct {
	// PullPolicy overrides the image pull policy used during deploy.
	//
	// Required: false
	PullPolicy string `json:"pullPolicy,omitempty" binding:"omitempty,oneof=missing always never"`

	// ForceRecreate forces compose to recreate containers even when unchanged.
	//
	// Required: false
	ForceRecreate bool `json:"forceRecreate,omitempty"`

	// Build rebuilds services that define a build section before deploying.
	//
	// Required: false
	Build bool `json:"build,omitempty"`

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

// ProjectRuntimeService contains live container status information for a service.
type ProjectRuntimeService struct {
	// ContainerLabels carries runtime policy inputs for server-side update aggregation.
	ContainerLabels map[string]string `json:"-"`

	// ImageID is the runtime image ID used to match image-level update checks.
	ImageID string `json:"-"`

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

// ProjectServiceUpdateInfo keeps version candidates separate for services sharing an image.
type ProjectServiceUpdateInfo struct {
	ImageRef   string           `json:"imageRef"`
	UpdateInfo *ImageUpdateInfo `json:"updateInfo,omitempty"`
}

// ProjectUpdateInfo contains aggregated image update status for a project.
type ProjectUpdateInfo struct {
	ServiceUpdates map[string]ProjectServiceUpdateInfo `json:"serviceUpdates,omitempty"`
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
	UpdateInfoByRef map[string]ImageUpdateInfo `json:"updateInfoByRef,omitempty"`

	// LastCheckedAt is the latest successful or failed image update check time for this project.
	//
	// Required: false
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
}

type ProjectDetailsOptions struct {
	IncludeComposeContent  bool
	IncludeEnvState        bool
	IncludeIncludeFiles    bool
	IncludeServiceConfigs  bool
	IncludeRuntimeServices bool
	IncludeUpdateInfo      bool
}

func AllProjectDetails() ProjectDetailsOptions {
	return ProjectDetailsOptions{
		IncludeComposeContent:  true,
		IncludeEnvState:        true,
		IncludeIncludeFiles:    true,
		IncludeServiceConfigs:  true,
		IncludeRuntimeServices: true,
		IncludeUpdateInfo:      true,
	}
}

// CreateProjectResponse is the response when a project is created.
type CreateProjectResponse struct {
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
	Tags []ProjectTag `json:"tags"`
}

// ProjectConfigurationError describes a filesystem-level configuration problem that
// prevents Arcane from fully using a project.
type ProjectConfigurationError struct {
	// Code identifies the configuration problem.
	//
	// Required: true
	Code string `json:"code"`

	// Path is the offending file path.
	//
	// Required: true
	Path string `json:"path"`

	// UID is the effective user id of the Arcane runtime.
	//
	// Required: true
	UID int `json:"uid"`

	// GID is the effective group id of the Arcane runtime.
	//
	// Required: true
	GID int `json:"gid"`

	// BlocksOperations indicates whether the problem prevents project operations.
	//
	// Required: true
	BlocksOperations bool `json:"blocksOperations"`
}

// Project contains detailed information about a project.
type Project struct {
	EnvironmentTag

	// Tags are the effective UI and Compose tags for the project.
	Tags []ProjectTag `json:"tags" readOnly:"true"`

	// StatusReason provides additional information about the status.
	//
	// Required: false
	StatusReason *string `json:"statusReason,omitempty" readOnly:"true"`

	// ActivityID is the activity created by the project action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty" readOnly:"true"`

	// LastSyncCommit is the last commit synced from Git (if GitOps managed).
	//
	// Required: false
	LastSyncCommit *string `json:"lastSyncCommit,omitempty" readOnly:"true"`

	// GitOpsManagedBy is the ID of the GitOps sync managing this project (if any).
	//
	// Required: false
	GitOpsManagedBy *string `json:"gitOpsManagedBy,omitempty" readOnly:"true"`

	// UpdateInfo contains aggregated image update status for the project.
	//
	// Required: false
	UpdateInfo *ProjectUpdateInfo `json:"updateInfo,omitempty" readOnly:"true"`

	// ArchivedAt is the date and time when the project was archived.
	//
	// Required: false
	ArchivedAt *time.Time `json:"archivedAt,omitempty" readOnly:"true"`

	// Status is the current status of the project.
	//
	// Required: true
	Status string `json:"status" readOnly:"true"`

	// DirName is the directory name where the project is stored.
	//
	// Required: false
	DirName string `json:"dirName,omitempty" readOnly:"true"`

	// ComposeContent is the Docker Compose file content.
	//
	// Required: false
	ComposeContent string `json:"composeContent,omitempty"`

	// ComposeFileName is the detected compose file name for the project.
	//
	// Required: false
	ComposeFileName string `json:"composeFileName,omitempty" readOnly:"true"`

	// ComposeFiles is the ordered project-relative list of compose files selected
	// by a COMPOSE_FILE entry in the project's .env. It is populated only when
	// COMPOSE_FILE selects more than one file; otherwise it is empty and
	// ComposeFileName is authoritative.
	//
	// Required: false
	ComposeFiles []string `json:"composeFiles,omitempty" readOnly:"true"`

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
	OverrideFileName string `json:"overrideFileName,omitempty" readOnly:"true"`

	// Name of the project.
	//
	// Required: false
	Name string `json:"name" required:"false"`

	// GitRepositoryURL is the URL of the Git repository (if GitOps managed).
	//
	// Required: false
	GitRepositoryURL string `json:"gitRepositoryURL,omitempty" readOnly:"true"`

	// IconLightURL is the optional light stack icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty" readOnly:"true"`

	// RelativePath is the path to the project directory relative to the configured projects root.
	//
	// Required: false
	RelativePath string `json:"relativePath,omitempty" readOnly:"true"`
	// ID is the unique identifier of the project.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// IconDarkURL is the optional dark stack icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty" readOnly:"true"`

	// Path is the file path to the project.
	//
	// Required: true
	Path string `json:"path" readOnly:"true"`

	// UpdatedAt is the date and time when the project was last updated.
	//
	// Required: true
	UpdatedAt string `json:"updatedAt" readOnly:"true"`

	// CreatedAt is the date and time when the project was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt" readOnly:"true"`

	// RuntimeServices contains live container status information for each service.
	//
	// Required: false
	RuntimeServices []ProjectRuntimeService `json:"runtimeServices,omitempty" readOnly:"true"`

	// Services is a list of services defined in the Docker Compose file.
	//
	// Required: false
	Services []composetypes.ServiceConfig `json:"services,omitempty" readOnly:"true"`

	// URLs are optional custom stack URLs from compose metadata.
	//
	// Required: false
	URLs []string `json:"urls,omitempty" readOnly:"true"`

	// IncludeFiles is a list of included files in the project.
	//
	// Required: false
	IncludeFiles []ProjectIncludeFile `json:"includeFiles,omitempty" readOnly:"true"`

	// RunningCount is the number of running services in the project.
	//
	// Required: true
	RunningCount int `json:"runningCount" readOnly:"true"`

	// ServiceCount is the total number of services in the project.
	//
	// Required: true
	ServiceCount int `json:"serviceCount" readOnly:"true"`

	// IsDiscovered indicates whether this row was derived from runtime Compose labels instead of an Arcane project record.
	//
	// Required: false
	IsDiscovered bool `json:"isDiscovered,omitempty" readOnly:"true"`

	// IsArchived indicates whether the project is hidden from the default project list.
	//
	// Required: true
	IsArchived bool `json:"isArchived" readOnly:"true"`

	// HasBuildDirective indicates whether any Compose service defines a build directive.
	//
	// Required: false
	HasBuildDirective bool `json:"hasBuildDirective,omitempty" readOnly:"true"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this project.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty" readOnly:"true"`

	// ConfigurationError reports a filesystem-level configuration problem detected for this project.
	//
	// Required: false
	ConfigurationError *ProjectConfigurationError `json:"configurationError,omitempty" readOnly:"true"`
}

// ProjectDestroy is used to destroy a project.
type ProjectDestroy struct {
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

// ProjectStatusCounts contains counts of projects by status.
type ProjectStatusCounts struct {
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

// ComposeCache stores values until their Compose configuration inputs change.
// Fingerprints identify the settings and project identity used when loading.
type ComposeCache[T any] interface {
	Get(projectID, fingerprint string) (T, bool)
	Set(projectID, fingerprint, projectPath, projectsDirectory, composePath string, composeFiles, envFiles []string, loadedAt time.Time, value T) error
	Invalidate(projectID string)
}

// ComposeDependencies records files consumed before executable models discard env_file.
type ComposeDependencies struct {
	EnvFiles []string
}

// VolumeSourcePathMapper translates Compose volume sources for the Docker host.
type VolumeSourcePathMapper interface {
	TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
	// ContainerToHost translates a single container-side path to its host-side
	// equivalent. mapped is false when the path is outside every mounted
	// directory, in which case the path is returned unchanged; a path inside an
	// identity mount is also unchanged but reports mapped as true. Needed to
	// re-resolve relative Compose paths that escape the projects mount, where
	// prefix translation has nothing to match.
	ContainerToHost(containerPath string) (hostPath string, mapped bool, err error)
}

// ComposeContentOptions configures loading a Compose project from in-memory content.
type ComposeContentOptions struct {
	ProjectName     string
	ComposeContent  string
	OverrideContent string
	EnvContent      string
	WorkingDir      string
	PathMapper      VolumeSourcePathMapper
}

// ProjectBuildOptions selects Compose services and overrides the configured build provider.
type ProjectBuildOptions struct {
	Services []string
	Provider string
	Push     *bool
	Load     *bool
}

// ComposeImageOperations connects Compose preparation to the application's image and build services.
type ComposeImageOperations struct {
	Exists        func(context.Context, string) (bool, error)
	LastTagged    func(context.Context, string) (time.Time, error)
	Pull          func(context.Context, string, io.Writer) error
	Build         func(context.Context, buildtypes.BuildRequest, io.Writer, string) error
	BuildProvider string
}

// ComposeDeployment supplies application actions in deployment order.
type ComposeDeployment struct {
	ProjectID         string
	ProjectPath       string
	Options           *ProjectDeployOptions
	DefaultPullPolicy string
	GitOpsManaged     bool
	WaitTimeout       time.Duration
	AuthConfigs       map[string]registry.AuthConfig
	Progress          io.Writer
	PreDeploy         func(context.Context) error
	Load              func(context.Context) (*composetypes.Project, error)
	ResolveImages     func(context.Context) (ComposeImageOperations, error)
	Recover           func(context.Context)
}

// ComposeServiceUpdate supplies an already authorized service update.
type ComposeServiceUpdate struct {
	Project  *composetypes.Project
	Services []string
	// Dependents are recreated alongside Services because they share a member's
	// namespace or volumes; they are never pulled. StoppedDependents are
	// recreated without being started so an operator's stop is preserved.
	Dependents            []string
	StoppedDependents     []string
	Images                ComposeImageOperations
	Progress              io.Writer
	AuthConfigs           map[string]registry.AuthConfig
	WaitTimeout           time.Duration
	RestoreBeforeMutation func(context.Context)
	Recover               func(context.Context)
}

// ComposeCommands supplies the SDK operations used by the coordinator.
type ComposeCommands struct {
	Stop   func(context.Context, *composetypes.Project, []string) error
	Up     func(context.Context, *composetypes.Project, []string, bool, bool, bool, map[string]registry.AuthConfig, time.Duration) error
	Create func(context.Context, *composetypes.Project, []string, map[string]registry.AuthConfig) error
}

// ComposeCoordinator executes Compose workflows through application collaborators.
type ComposeCoordinator interface {
	Deploy(ctx context.Context, request ComposeDeployment) (*composetypes.Project, error)
	UpdateServices(ctx context.Context, request ComposeServiceUpdate) error
	PullServices(ctx context.Context, model *composetypes.Project, services []string, operations ComposeImageOperations, progress io.Writer) error
	EnsureImagesPresent(ctx context.Context, model *composetypes.Project, progress io.Writer, operations ComposeImageOperations) error
	BuildServices(ctx context.Context, projectID string, model *composetypes.Project, options ProjectBuildOptions, progress io.Writer, operations ComposeImageOperations) error
}

var unitBytesUnmarshalers = json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, value *composetypes.UnitBytes) error {
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}

	switch token.Kind() {
	case jsontext.KindNull:
		return nil
	case jsontext.KindString:
		parsed, parseMemoryErr := units.RAMInBytes(strings.TrimSpace(token.String()))
		if parseMemoryErr != nil {
			return parseMemoryErr
		}
		*value = composetypes.UnitBytes(parsed)
		return nil
	case jsontext.KindNumber:
		raw := token.String()
		parsed, parseIntErr := strconv.ParseInt(raw, 10, 64)
		if parseIntErr != nil {
			floatValue, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr != nil {
				return parseErr
			}
			parsed = int64(floatValue)
			if floatValue != float64(parsed) {
				return fmt.Errorf("invalid UnitBytes value %q", raw)
			}
		}
		*value = composetypes.UnitBytes(parsed)
		return nil
	case jsontext.KindInvalid, jsontext.KindFalse, jsontext.KindTrue,
		jsontext.KindBeginObject, jsontext.KindEndObject,
		jsontext.KindBeginArray, jsontext.KindEndArray:
		return fmt.Errorf("unsupported JSON kind %s for UnitBytes", token.Kind())
	}

	return fmt.Errorf("unsupported JSON kind %s for UnitBytes", token.Kind())
})

type runtimeServiceAlias ProjectRuntimeService

func (r *ProjectRuntimeService) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*runtimeServiceAlias)(r), json.WithUnmarshalers(unitBytesUnmarshalers))
}

type projectAlias Project

func (d *Project) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*projectAlias)(d), json.WithUnmarshalers(unitBytesUnmarshalers))
}

// Project status values shared by stored projects and runtime projections.
const (
	ProjectStatusRunning          = "running"
	ProjectStatusStopped          = "stopped"
	ProjectStatusPartiallyRunning = "partially running"
	ProjectStatusUnknown          = "unknown"
	ProjectStatusDeploying        = "deploying"
	ProjectStatusStopping         = "stopping"
	ProjectStatusRestarting       = "restarting"
)

// ProjectRecord is the stored state of a project that list projections read.
type ProjectRecord struct {
	ID                 string
	Name               string
	DirName            *string
	Path               string
	Status             string
	StatusReason       *string
	ServiceCount       int
	RunningCount       int
	GitOpsManagedBy    *string
	ComposeProjectName *string
	BuildImageRefsJSON *string
	IsArchived         bool
	ArchivedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          *time.Time
}

// ProjectTagAssignment is one stored project tag association.
type ProjectTagAssignment struct {
	ProjectID string
	Name      string
	Source    ProjectTagSource
	Color     ProjectTagColor
}

// ComposeIdentity is the compose project identity of a discovered directory.
type ComposeIdentity struct {
	ServiceCount        int
	ResolvedProjectName string
	ComposeProjectName  *string
	ExplicitProjectName bool
}

const (
	ProjectRenameJournalKeyPrefix         = "project_rename_journal:"
	ProjectRenameRollbackCleanupKeyPrefix = "project_rename_rollback_cleanup:"

	ProjectRenameJournalPhaseStarted                = "started"
	ProjectRenameJournalPhaseTargetsCopied          = "targets_copied"
	ProjectRenameJournalPhaseOldVolumesRemoved      = "old_volumes_removed"
	ProjectRenameJournalPhaseProjectStateCommitted  = "project_state_committed"
	ProjectRenameJournalPhaseSourceCleanupPending   = "source_cleanup_pending"
	ProjectRenameJournalPhaseProjectStateRolledBack = "project_state_rolled_back"
)

type ProjectRenameJournal struct {
	ProjectID  string                `json:"projectId"`
	OldName    string                `json:"oldName"`
	NewName    string                `json:"newName"`
	OldPath    string                `json:"oldPath"`
	NewPath    string                `json:"newPath"`
	OldDirName *string               `json:"oldDirName,omitempty"`
	NewDirName string                `json:"newDirName"`
	Phase      string                `json:"phase"`
	Volumes    []VolumeJournalVolume `json:"volumes,omitempty"`
	UpdatedAt  time.Time             `json:"updatedAt"`
}

type ProjectRenameRollbackCleanup struct {
	ProjectID string                `json:"projectId"`
	OldName   string                `json:"oldName"`
	OldPath   string                `json:"oldPath"`
	NewName   string                `json:"newName"`
	NewPath   string                `json:"newPath"`
	Volumes   []VolumeJournalVolume `json:"volumes,omitempty"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

// ProjectRenameRecoveryOperations supplies persistence and Docker access to rename recovery.
type ProjectRenameRecoveryOperations struct {
	Docker               func(context.Context, bool) (*client.Client, error)
	WriteJournal         func(context.Context, *ProjectRenameJournal, string) error
	ClearJournal         func(context.Context, string) error
	ClearRollbackCleanup func(context.Context, string) error
	RestoreState         func(context.Context, *ProjectRenameJournal) error
	WriteRollbackCleanup func(context.Context, *ProjectRenameJournal) error
}

type ProjectWorkspaceUpdateManifest struct {
	FileTreeRevision string                `json:"fileTreeRevision" binding:"required"`
	FileChanges      []WorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
}

type CreateProjectWorkspaceManifest struct {
	FileChanges []WorkspaceFileChange `json:"fileChanges" binding:"required" maxItems:"500"`
}
