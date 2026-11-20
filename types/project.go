package types

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/go-units"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	buildtypes "kit.getarcane.app/builds/types"
)

type (
	ProjectIncludeFile struct {
		Path         string `json:"path"`
		RelativePath string `json:"relativePath"`
		Content      string `json:"content,omitempty"`
	}

	CreateProject struct {
		EnvContent     *string                    `json:"envContent,omitempty"`
		TagColors      map[string]ProjectTagColor `json:"tagColors,omitempty"`
		Name           string                     `json:"name" binding:"required"`
		ComposeContent string                     `json:"composeContent" binding:"required"`
		Directory      string                     `json:"directory,omitempty"`
		Tags           []string                   `json:"tags,omitempty"`
	}

	ImportProjectArchive struct {
		UploadID  string   `json:"uploadId" required:"true" doc:"ID of a complete project-archive upload session"`
		Name      string   `json:"name" required:"true" minLength:"1" doc:"Name of the project to create"`
		ProjectID string   `json:"projectId,omitempty" format:"uuid" doc:"ID for the new project, chosen by the caller so it can recognize the project after an interrupted import"`
		Tags      []string `json:"tags,omitempty" doc:"Project tags"`
		Start     bool     `json:"start,omitempty" doc:"Deploy the project after the import"`
	}

	ExportProjectArchive struct {
		Volumes     []string `json:"volumes,omitempty" doc:"Compose keys of the named and local external volumes whose data the export includes; empty exports only the project files"`
		StopProject bool     `json:"stopProject,omitempty" doc:"Stop a running project while its volumes are exported and start it again afterwards"`
	}

	ProjectExportOptions struct {
		Volumes []ProjectVolume `json:"volumes" doc:"The project's volumes; kind explains why external and driverOpts volumes cannot be included"`
		Running bool            `json:"running" doc:"True when the project has running or restarting containers"`
	}

	ProjectVolumeKind string

	ProjectVolume struct {
		Labels       map[string]string `json:"labels,omitempty" doc:"Compose volume labels"`
		DockerLabels map[string]string `json:"dockerLabels,omitempty" doc:"Docker labels of an included external volume, which an import recreates it with"`
		DriverOpts   map[string]string `json:"driverOpts,omitempty" doc:"Compose volume driver options"`
		Key          string            `json:"key" doc:"Volume key in the compose file"`
		Name         string            `json:"name" doc:"Docker volume name"`
		Kind         ProjectVolumeKind `json:"kind" enum:"named,external,driverOpts" doc:"named and local external volumes hold Docker-managed data; driverOpts and other external volumes do not"`
		Driver       string            `json:"driver,omitempty" doc:"Compose volume driver"`
		Archive      string            `json:"archive,omitempty" doc:"Path of the volume's data archive inside a project bundle; volumes naming one Docker volume share it"`
		Included     bool              `json:"included" doc:"Whether the volume's data is included; by default named and local external volumes that exist are"`
		Missing      bool              `json:"missing,omitempty" doc:"True when the volume has not been created in Docker yet, so it has no data to export"`
		InUse        bool              `json:"inUse,omitempty" doc:"True when a running container that is not stopped mounts the volume, so its data may change during an export"`
	}

	ProjectManifestProject struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		ComposeName string `json:"composeName"`
	}

	ProjectManifest struct {
		CreatedAt     time.Time              `json:"createdAt"`
		Kind          string                 `json:"kind"`
		ArcaneVersion string                 `json:"arcaneVersion"`
		Project       ProjectManifestProject `json:"project"`
		ComposeFiles  []string               `json:"composeFiles,omitempty"`
		IncludeFiles  []string               `json:"includeFiles,omitempty"`
		Volumes       []ProjectVolume        `json:"volumes"`
		FormatVersion int                    `json:"formatVersion"`
		Consistent    bool                   `json:"consistent" doc:"True when no project container ran, and no other running container mounted an included volume, while the volumes were exported"`
	}

	ProjectTransferMode string

	ProjectTransferSeverity string

	ProjectTransferResourceKind string

	ProjectTransferRequest struct {
		TargetEnvironmentID string              `json:"targetEnvironmentId" required:"true" minLength:"1" doc:"Environment that receives the project"`
		Mode                ProjectTransferMode `json:"mode" required:"true" enum:"copy,move" doc:"copy keeps the source running as before; move stops the source after the target succeeds"`
		Name                string              `json:"name" required:"true" minLength:"1" doc:"Project name on the target environment"`
		IncludeVolumes      bool                `json:"includeVolumes,omitempty" doc:"Copy the data of the project's named volumes; the source is stopped while it is copied"`
		Start               bool                `json:"start,omitempty" doc:"Deploy the project on the target after the copy"`
	}

	ProjectTransferCheck struct {
		Code     string                  `json:"code" doc:"Stable check code"`
		Severity ProjectTransferSeverity `json:"severity" enum:"error,warning" doc:"error blocks the transfer; warning does not"`
		Message  string                  `json:"message" doc:"Human-readable description"`
		Resource string                  `json:"resource,omitempty" doc:"Name of the missing external volume or network the check is about"`
	}

	ProjectTransferResource struct {
		TargetEnvironmentID string                      `json:"targetEnvironmentId" required:"true" minLength:"1" doc:"Environment to create the resource on"`
		Kind                ProjectTransferResourceKind `json:"kind" required:"true" enum:"volume,network" doc:"Kind of external resource to create"`
		Name                string                      `json:"name" required:"true" minLength:"1" doc:"Name of the project's external volume or network"`
	}

	ProjectTransferVolume struct {
		Key        string `json:"key" doc:"Volume key in the compose file"`
		SourceName string `json:"sourceName" doc:"Docker volume name on the source environment"`
		TargetName string `json:"targetName" doc:"Expected Docker volume name on the target environment"`
	}

	ProjectTransferPreflight struct {
		ProjectName   string                  `json:"projectName"`
		Volumes       []ProjectTransferVolume `json:"volumes" doc:"Named, non-external volumes whose data is copied when includeVolumes is set"`
		Checks        []ProjectTransferCheck  `json:"checks"`
		SourceRunning bool                    `json:"sourceRunning"`
		Allowed       bool                    `json:"allowed" doc:"True when no check has severity error"`
	}

	ProjectTransferStarted struct {
		ActivityID string `json:"activityId" doc:"Activity that reports the transfer's progress on the source environment"`
	}

	ProjectWebhookRetarget struct {
		SourceEnvironmentID string
		SourceProjectID     string
		TargetEnvironmentID string
		TargetProjectID     string
	}

	ProjectsDirectoryStatus struct {
		Path          string `json:"path"`
		ContainerPath string `json:"containerPath"`
		Error         string `json:"error,omitempty"`
		Primary       bool   `json:"primary"`
		Enabled       bool   `json:"enabled"`
		Exists        bool   `json:"exists"`
	}

	ProjectTagSource string

	ProjectTagColor string

	ProjectTag struct {
		Name    string             `json:"name"`
		Color   ProjectTagColor    `json:"color"`
		Sources []ProjectTagSource `json:"sources"`
	}

	ProjectTagOption struct {
		Name  string          `json:"name"`
		Color ProjectTagColor `json:"color"`
	}

	ProjectReference struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	UpdateProjectTag struct {
		Name     string          `json:"name"`
		Color    ProjectTagColor `json:"color,omitempty"`
		Attached bool            `json:"attached"`
	}

	UpdateProjectTagResponse struct {
		ActivityID *string      `json:"activityId,omitempty"`
		Tags       []ProjectTag `json:"tags"`
	}

	ProjectBuildRequest struct {
		Push     *bool    `json:"push,omitempty" doc:"Push images"`
		Load     *bool    `json:"load,omitempty" doc:"Load images into Docker"`
		Provider string   `json:"provider,omitempty" doc:"Build provider override"`
		Services []string `json:"services,omitempty" doc:"Service names to build"`
	}

	ProjectDeployOptions struct {
		PullPolicy    string `json:"pullPolicy,omitempty" binding:"omitempty,oneof=missing always never"`
		ForceRecreate bool   `json:"forceRecreate,omitempty"`
		Build         bool   `json:"build,omitempty"`
		RemoveOrphans bool   `json:"removeOrphans,omitempty"`

		RecreateVolumes bool `json:"recreateVolumes,omitempty"`
	}

	ProjectRuntimeService struct {
		ContainerLabels  map[string]string           `json:"-"`
		Health           *string                     `json:"health,omitempty"`
		ServiceConfig    *composetypes.ServiceConfig `json:"serviceConfig,omitempty"`
		ImageID          string                      `json:"-"`
		Name             string                      `json:"name"`
		Image            string                      `json:"image"`
		Status           string                      `json:"status"`
		ContainerID      string                      `json:"containerId,omitempty"`
		ContainerName    string                      `json:"containerName,omitempty"`
		IconLightURL     string                      `json:"iconLightUrl,omitempty"`
		IconDarkURL      string                      `json:"iconDarkUrl,omitempty"`
		Ports            []string                    `json:"ports,omitempty"`
		RedeployDisabled bool                        `json:"redeployDisabled,omitempty"`
	}

	ProjectJobSchedule struct {
		Cron     string `json:"cron" doc:"Cron expression"`
		Timezone string `json:"timezone,omitempty" doc:"IANA timezone the cron expression is evaluated in"`
	}

	ProjectJob struct {
		Name          string               `json:"name" doc:"Job name"`
		Image         string               `json:"image,omitempty" doc:"Job image"`
		Profiles      []string             `json:"profiles,omitempty" doc:"Compose profiles the job belongs to"`
		Schedules     []ProjectJobSchedule `json:"schedules" doc:"Schedule triggers registered with the engine on deploy"`
		ManualAllowed bool                 `json:"manualAllowed" doc:"Whether the job can be run on demand"`
		Disabled      bool                 `json:"disabled" doc:"Whether the job is disabled by inactive profiles"`
		HasBuild      bool                 `json:"hasBuild" doc:"Whether the job declares a build"`
	}

	ProjectServiceUpdateInfo struct {
		UpdateInfo *ImageUpdateInfo `json:"updateInfo,omitempty"`
		ImageRef   string           `json:"imageRef"`
	}

	ProjectUpdateInfo struct {
		ServiceUpdates     map[string]ProjectServiceUpdateInfo `json:"serviceUpdates,omitempty"`
		ErrorMessage       *string                             `json:"errorMessage,omitempty"`
		UpdateInfoByRef    map[string]ImageUpdateInfo          `json:"updateInfoByRef,omitempty"`
		LastCheckedAt      *time.Time                          `json:"lastCheckedAt,omitempty"`
		Status             string                              `json:"status"`
		ImageRefs          []string                            `json:"imageRefs,omitempty"`
		UpdatedImageRefs   []string                            `json:"updatedImageRefs,omitempty"`
		NotPulledImageRefs []string                            `json:"notPulledImageRefs,omitempty"`
		ImageCount         int                                 `json:"imageCount"`
		CheckedImageCount  int                                 `json:"checkedImageCount"`
		ImagesWithUpdates  int                                 `json:"imagesWithUpdates"`
		ImagesNotPulled    int                                 `json:"imagesNotPulled"`
		ErrorCount         int                                 `json:"errorCount"`
		HasUpdate          bool                                `json:"hasUpdate"`
	}

	ProjectDestroyOptions struct {
		RemoveFiles   bool
		RemoveVolumes bool
		SkipDown      bool
	}

	ProjectDetailsOptions struct {
		IncludeComposeContent  bool
		IncludeEnvState        bool
		IncludeIncludeFiles    bool
		IncludeServiceConfigs  bool
		IncludeRuntimeServices bool
		IncludeUpdateInfo      bool
	}

	CreateProjectResponse struct {
		StatusReason    *string      `json:"statusReason,omitempty"`
		GitOpsManagedBy *string      `json:"gitOpsManagedBy,omitempty"`
		ArchivedAt      *time.Time   `json:"archivedAt,omitempty"`
		ActivityID      *string      `json:"activityId,omitempty"`
		ID              string       `json:"id"`
		Name            string       `json:"name"`
		DirName         string       `json:"dirName,omitempty"`
		RelativePath    string       `json:"relativePath,omitempty"`
		Path            string       `json:"path"`
		Status          string       `json:"status"`
		CreatedAt       string       `json:"createdAt"`
		UpdatedAt       string       `json:"updatedAt"`
		Tags            []ProjectTag `json:"tags"`
		ServiceCount    int          `json:"serviceCount"`
		RunningCount    int          `json:"runningCount"`
		IsArchived      bool         `json:"isArchived"`
	}

	ProjectConfigurationError struct {
		Code             string `json:"code"`
		Path             string `json:"path"`
		UID              int    `json:"uid"`
		GID              int    `json:"gid"`
		BlocksOperations bool   `json:"blocksOperations"`
	}

	Project struct {
		StatusReason       *string                    `json:"statusReason,omitempty" readOnly:"true"`
		ActivityID         *string                    `json:"activityId,omitempty" readOnly:"true"`
		LastSyncCommit     *string                    `json:"lastSyncCommit,omitempty" readOnly:"true"`
		GitOpsManagedBy    *string                    `json:"gitOpsManagedBy,omitempty" readOnly:"true"`
		UpdateInfo         *ProjectUpdateInfo         `json:"updateInfo,omitempty" readOnly:"true"`
		ArchivedAt         *time.Time                 `json:"archivedAt,omitempty" readOnly:"true"`
		ConfigurationError *ProjectConfigurationError `json:"configurationError,omitempty" readOnly:"true"`
		EnvironmentTag
		Status                string       `json:"status" readOnly:"true"`
		DirName               string       `json:"dirName,omitempty" readOnly:"true"`
		ComposeContent        string       `json:"composeContent,omitempty"`
		ComposeFileName       string       `json:"composeFileName,omitempty" readOnly:"true"`
		EnvContent            string       `json:"envContent,omitempty"`
		OverrideContent       string       `json:"overrideContent,omitempty"`
		OverrideFileName      string       `json:"overrideFileName,omitempty" readOnly:"true"`
		Name                  string       `json:"name" required:"false"`
		GitRepositoryURL      string       `json:"gitRepositoryURL,omitempty" readOnly:"true"`
		GitRepositoryProvider string       `json:"gitRepositoryProvider,omitempty" readOnly:"true"`
		GitOpsSyncMode        string       `json:"gitOpsSyncMode,omitempty" readOnly:"true"`
		IconLightURL          string       `json:"iconLightUrl,omitempty" readOnly:"true"`
		RelativePath          string       `json:"relativePath,omitempty" readOnly:"true"`
		ProjectsRoot          string       `json:"projectsRoot,omitempty" readOnly:"true"`
		ID                    string       `json:"id" readOnly:"true"`
		IconDarkURL           string       `json:"iconDarkUrl,omitempty" readOnly:"true"`
		Path                  string       `json:"path" readOnly:"true"`
		UpdatedAt             string       `json:"updatedAt" readOnly:"true"`
		CreatedAt             string       `json:"createdAt" readOnly:"true"`
		Tags                  []ProjectTag `json:"tags" readOnly:"true"`

		ComposeFiles    []string                     `json:"composeFiles,omitempty" readOnly:"true"`
		RuntimeServices []ProjectRuntimeService      `json:"runtimeServices,omitempty" readOnly:"true"`
		Services        []composetypes.ServiceConfig `json:"services,omitempty" readOnly:"true"`
		Jobs            []ProjectJob                 `json:"jobs,omitempty" readOnly:"true"`
		ComposeName     string                       `json:"composeName,omitempty" readOnly:"true"`
		Volumes         composetypes.Volumes         `json:"volumes,omitempty" readOnly:"true"`
		Networks        composetypes.Networks        `json:"networks,omitempty" readOnly:"true"`
		URLs            []string                     `json:"urls,omitempty" readOnly:"true"`
		IncludeFiles    []ProjectIncludeFile         `json:"includeFiles,omitempty" readOnly:"true"`
		RunningCount    int                          `json:"runningCount" readOnly:"true"`
		ServiceCount    int                          `json:"serviceCount" readOnly:"true"`

		IsDiscovered      bool `json:"isDiscovered,omitzero" readOnly:"true"`
		IsArchived        bool `json:"isArchived" readOnly:"true"`
		HasBuildDirective bool `json:"hasBuildDirective,omitzero" readOnly:"true"`
		RedeployDisabled  bool `json:"redeployDisabled,omitzero" readOnly:"true"`
		EnvFileExists     bool `json:"envFileExists,omitzero" readOnly:"true"`
	}

	ProjectDestroy struct {
		RemoveFiles   *bool `json:"removeFiles,omitempty"`
		RemoveVolumes bool  `json:"removeVolumes,omitempty"`
	}

	ProjectStatusCounts struct {
		RunningProjects  int `json:"runningProjects"`
		StoppedProjects  int `json:"stoppedProjects"`
		TotalProjects    int `json:"totalProjects"`
		ArchivedProjects int `json:"archivedProjects"`

		UpdatesAvailable int `json:"updatesAvailable" doc:"Active projects with a pending image update"`
	}

	ComposeCache[T any] interface {
		Get(ctx context.Context, projectID, fingerprint string) (T, bool)
		Set(ctx context.Context, projectID, fingerprint, projectPath, projectsDirectory, composePath string, composeFiles, envFiles []string, loadedAt time.Time, value T) error
		Invalidate(projectID string)
	}

	ComposeDependencies struct {
		EnvFiles []string
	}

	VolumeSourcePathMapper interface {
		TranslateVolumeSources(project *composetypes.Project, translateFileResources bool) error
		ContainerToHost(containerPath string) (hostPath string, mapped bool, err error)
	}

	ComposeContentOptions struct {
		PathMapper      VolumeSourcePathMapper
		ProjectName     string
		ComposeContent  string
		OverrideContent string
		EnvContent      string
		WorkingDir      string
	}

	ProjectBuildOptions struct {
		Push     *bool
		Load     *bool
		Provider string
		Services []string
	}

	ComposeImageOperations struct {
		Exists        func(context.Context, string) (bool, error)
		LastTagged    func(context.Context, string) (time.Time, error)
		Pull          func(context.Context, string, io.Writer) error
		Build         func(context.Context, buildtypes.BuildRequest, io.Writer, string) error
		BuildProvider string
	}

	ComposeDeployment struct {
		Progress          io.Writer
		Options           *ProjectDeployOptions
		AuthConfigs       map[string]registry.AuthConfig
		PreDeploy         func(context.Context) error
		Load              func(context.Context) (*composetypes.Project, error)
		ResolveImages     func(context.Context) (ComposeImageOperations, error)
		Recover           func(context.Context)
		ProjectID         string
		ProjectPath       string
		DockerHost        string
		DefaultPullPolicy string
		WaitTimeout       time.Duration
		GitOpsManaged     bool
	}

	ComposeServiceUpdate struct {
		Progress              io.Writer
		Project               *composetypes.Project
		AuthConfigs           map[string]registry.AuthConfig
		RestoreBeforeMutation func(context.Context)
		Recover               func(context.Context)
		Images                ComposeImageOperations
		DockerHost            string
		Services              []string
		// Dependents share a member's namespace or volumes and are never pulled; StoppedDependents stay stopped.
		Dependents        []string
		StoppedDependents []string
		WaitTimeout       time.Duration
	}

	ComposeTarget struct {
		Project    *composetypes.Project
		DockerHost string
		Services   []string
	}

	ComposeUpOptions struct {
		AuthConfigs map[string]registry.AuthConfig
		ComposeTarget
		WaitTimeout     time.Duration
		RemoveOrphans   bool
		ForceRecreate   bool
		RecreateVolumes bool
	}

	ComposeCreateOptions struct {
		AuthConfigs map[string]registry.AuthConfig
		ComposeTarget
	}

	ComposeDownOptions struct {
		ComposeTarget
		RemoveVolumes bool
	}

	ComposePsOptions struct {
		ComposeTarget
		All bool
	}

	ComposeRunJobOptions struct {
		AuthConfigs map[string]registry.AuthConfig
		Project     *composetypes.Project
		DockerHost  string
		Job         string
		Timeout     time.Duration
	}

	ComposeCommands struct {
		Stop   func(context.Context, ComposeTarget) error
		Up     func(context.Context, ComposeUpOptions) error
		Create func(context.Context, ComposeCreateOptions) error
	}

	ComposeCoordinator interface {
		Deploy(ctx context.Context, request ComposeDeployment) (*composetypes.Project, error)
		UpdateServices(ctx context.Context, request ComposeServiceUpdate) error
		PullServices(ctx context.Context, model *composetypes.Project, services []string, operations ComposeImageOperations, progress io.Writer) error
		EnsureImagesPresent(ctx context.Context, model *composetypes.Project, progress io.Writer, operations ComposeImageOperations) error
		BuildServices(ctx context.Context, projectID string, model *composetypes.Project, options ProjectBuildOptions, progress io.Writer, operations ComposeImageOperations) error
	}

	runtimeServiceAlias ProjectRuntimeService

	projectAlias Project

	ProjectRecord struct {
		CreatedAt          time.Time
		DirName            *string
		StatusReason       *string
		GitOpsManagedBy    *string
		ComposeProjectName *string
		BuildImageRefsJSON *string
		ArchivedAt         *time.Time
		UpdatedAt          *time.Time
		ID                 string
		Name               string
		Path               string
		Status             string
		ServiceCount       int
		RunningCount       int
		IsArchived         bool
	}

	ProjectTagAssignment struct {
		ProjectID string
		Name      string
		Source    ProjectTagSource
		Color     ProjectTagColor
	}

	ComposeIdentity struct {
		ComposeProjectName  *string
		ResolvedProjectName string
		ServiceCount        int
		ExplicitProjectName bool
	}

	ProjectRenameJournal struct {
		UpdatedAt  time.Time             `json:"updatedAt"`
		OldDirName *string               `json:"oldDirName,omitempty"`
		ProjectID  string                `json:"projectId"`
		OldName    string                `json:"oldName"`
		NewName    string                `json:"newName"`
		OldPath    string                `json:"oldPath"`
		NewPath    string                `json:"newPath"`
		NewDirName string                `json:"newDirName"`
		Phase      string                `json:"phase"`
		Volumes    []VolumeJournalVolume `json:"volumes,omitempty"`
	}

	ProjectRenameRollbackCleanup struct {
		UpdatedAt time.Time             `json:"updatedAt"`
		ProjectID string                `json:"projectId"`
		OldName   string                `json:"oldName"`
		OldPath   string                `json:"oldPath"`
		NewName   string                `json:"newName"`
		NewPath   string                `json:"newPath"`
		Volumes   []VolumeJournalVolume `json:"volumes,omitempty"`
	}

	ProjectRenameRecoveryOperations struct {
		Docker               func(context.Context, bool) (*client.Client, error)
		WriteJournal         func(context.Context, *ProjectRenameJournal, string) error
		ClearJournal         func(context.Context, string) error
		ClearRollbackCleanup func(context.Context, string) error
		RestoreState         func(context.Context, *ProjectRenameJournal) error
		WriteRollbackCleanup func(context.Context, *ProjectRenameJournal) error
	}

	ProjectWorkspaceUpdateManifest struct {
		FileChanges []WorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
	}

	CreateProjectWorkspaceManifest struct {
		FileChanges []WorkspaceFileChange `json:"fileChanges" binding:"required" maxItems:"500"`
	}
)

const (
	ProjectTagSourceUI      ProjectTagSource = "ui"
	ProjectTagSourceCompose ProjectTagSource = "compose"
	ProjectTagColorGray     ProjectTagColor  = "gray"
	ProjectTagColorPurple   ProjectTagColor  = "purple"
	ProjectTagColorBlue     ProjectTagColor  = "blue"
	ProjectTagColorGreen    ProjectTagColor  = "green"
	ProjectTagColorYellow   ProjectTagColor  = "yellow"
	ProjectTagColorOrange   ProjectTagColor  = "orange"
	ProjectTagColorRed      ProjectTagColor  = "red"
	ProjectTagColorPink     ProjectTagColor  = "pink"

	ProjectStatusRunning          = "running"
	ProjectStatusStopped          = "stopped"
	ProjectStatusPartiallyRunning = "partially running"
	ProjectStatusUnknown          = "unknown"
	ProjectStatusDeploying        = "deploying"
	ProjectStatusStopping         = "stopping"
	ProjectStatusRestarting       = "restarting"

	ProjectRenameJournalKeyPrefix                   = "project_rename_journal:"
	ProjectRenameRollbackCleanupKeyPrefix           = "project_rename_rollback_cleanup:"
	ProjectRenameJournalPhaseStarted                = "started"
	ProjectRenameJournalPhaseTargetsCopied          = "targets_copied"
	ProjectRenameJournalPhaseOldVolumesRemoved      = "old_volumes_removed"
	ProjectRenameJournalPhaseProjectStateCommitted  = "project_state_committed"
	ProjectRenameJournalPhaseSourceCleanupPending   = "source_cleanup_pending"
	ProjectRenameJournalPhaseProjectStateRolledBack = "project_state_rolled_back"

	ProjectTransferModeCopy           ProjectTransferMode         = "copy"
	ProjectTransferModeMove           ProjectTransferMode         = "move"
	ProjectTransferSeverityError      ProjectTransferSeverity     = "error"
	ProjectTransferSeverityWarning    ProjectTransferSeverity     = "warning"
	ProjectTransferResourceVolume     ProjectTransferResourceKind = "volume"
	ProjectTransferResourceNetwork    ProjectTransferResourceKind = "network"
	ProjectTransferSourceOffline                                  = "source_offline"
	ProjectTransferTargetOffline                                  = "target_offline"
	ProjectTransferSourceOutdated                                 = "source_unsupported"
	ProjectTransferTargetOutdated                                 = "target_unsupported"
	ProjectTransferSameProject                                    = "same_project"
	ProjectTransferNameConflict                                   = "name_conflict"
	ProjectTransferVolumeExists                                   = "volume_exists"
	ProjectTransferNetworkMissing                                 = "external_network_missing"
	ProjectTransferExternalMissing                                = "external_volume_missing"
	ProjectTransferGitOpsMove                                     = "gitops_move"
	ProjectTransferBuildRequired                                  = "build_required"
	ProjectTransferGitOpsCopy                                     = "gitops_copy"
	ProjectTransferVolumesNotCopied                               = "volume_data_not_copied"
	ProjectTransferVolumeDataExternal                             = "volume_data_external"
	ProjectTransferComposeInvalid                                 = "compose_invalid"
	ProjectTransferVolumeMissing                                  = "volume_missing"

	ProjectVolumeNamed      ProjectVolumeKind = "named"
	ProjectVolumeExternal   ProjectVolumeKind = "external"
	ProjectVolumeDriverOpts ProjectVolumeKind = "driverOpts"

	ProjectManifestFormatVersion = 1
	ProjectManifestKindExport    = "export"
	ProjectBundleExtension       = ".arcane.tar.gz"
)

var composeUnmarshalers = json.JoinUnmarshalers(
	json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, value *composetypes.UnitBytes) error {
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
	}),
	json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, value *composetypes.UlimitsConfig) error {
		if decoder.PeekKind() != jsontext.KindNumber {
			return errors.ErrUnsupported
		}
		return json.UnmarshalDecode(decoder, &value.Single)
	}),
)

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

func (r *ProjectRuntimeService) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*runtimeServiceAlias)(r), json.WithUnmarshalers(composeUnmarshalers))
}

func (d *Project) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*projectAlias)(d), json.WithUnmarshalers(composeUnmarshalers))
}
