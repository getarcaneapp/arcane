package transfer

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"
	"uuid"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/go-units"
	"github.com/italypaleale/francis/builtin/workflow"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker"

	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	volumeDomain "getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	Availability string

	Environment interface {
		Availability(ctx context.Context) Availability
		Project(ctx context.Context, projectID string) (types.Project, error)
		Projects(ctx context.Context, search string) ([]types.Project, error)
		Volume(ctx context.Context, name string) (types.Volume, bool, error)
		Network(ctx context.Context, name string) (types.NetworkInspect, bool, error)
		CreateVolume(ctx context.Context, volume types.Volume, user types.Actor) error
		CreateNetwork(ctx context.Context, network types.Network, user types.Actor) error
		ComposeProjectInUse(ctx context.Context, name string) (bool, error)
		ExportProject(ctx context.Context, projectID string, request types.ExportProjectArchive, user types.Actor) (types.UploadSession, error)
		ImportProject(ctx context.Context, request types.ImportProjectArchive, user types.Actor) (string, error)
		CreateUpload(ctx context.Context, kind string, request types.CreateUploadSessionRequest) (types.UploadSession, error)
		Upload(ctx context.Context, kind, uploadID string) (types.UploadSession, error)
		ReadChunk(ctx context.Context, kind, uploadID string, index int) ([]byte, error)
		WriteChunk(ctx context.Context, kind, uploadID string, index int, data []byte) error
		DeleteUpload(ctx context.Context, kind, uploadID string) error
		DeployProject(ctx context.Context, projectID string, user types.Actor) error
		DownProject(ctx context.Context, projectID string, user types.Actor) error
		DestroyProject(ctx context.Context, projectID string, user types.Actor) error
		DeleteVolume(ctx context.Context, name string, user types.Actor) error
	}

	Dependencies struct {
		Environment      func(environmentID string) Environment
		RetargetWebhooks func(ctx context.Context, retarget types.ProjectWebhookRetarget) (int64, error)
		CountWebhooks    func(ctx context.Context, environmentID, projectID string) (int64, error)
		VolumeSizes      func(ctx context.Context) (map[string]volumeDomain.VolumeSizeData, error)
		Activities       activity.MessageAppender
		Backups          *backup.Engine
	}

	Options struct {
		User                types.Actor
		SourceEnvironmentID string
		ProjectID           string
		Request             types.ProjectTransferRequest
	}

	ResourceOptions struct {
		User                types.Actor
		SourceEnvironmentID string
		ProjectID           string
		Request             types.ProjectTransferResource
	}

	Service struct {
		deps     Dependencies
		flow     *flow.Engine
		workflow *flow.Workflow
	}

	runInput struct {
		User                types.Actor                  `json:"user"`
		RunID               string                       `json:"runId"`
		TargetProjectID     string                       `json:"targetProjectId"`
		SourceEnvironmentID string                       `json:"sourceEnvironmentId"`
		ProjectID           string                       `json:"projectId"`
		Request             types.ProjectTransferRequest `json:"request"`
		Requesters          []types.BackupRequester      `json:"requesters"`
		RetargetWebhooks    bool                         `json:"retargetWebhooks"`
	}

	checkpoint struct {
		Relay           relay                         `json:"relay"`
		TargetProjectID string                        `json:"targetProjectId,omitempty"`
		TargetCompose   string                        `json:"targetCompose,omitempty"`
		Volumes         []types.ProjectTransferVolume `json:"volumes,omitempty"`
		CreatedVolumes  []string                      `json:"createdVolumes,omitempty"`
		CopiedVolumes   []string                      `json:"copiedVolumes,omitempty"`
		Leftovers       []string                      `json:"leftovers,omitempty"`
		WebhooksKept    int64                         `json:"webhooksKept,omitempty"`
		Webhooks        int64                         `json:"webhooks,omitempty"`
		Prepared        bool                          `json:"prepared"`
		Importing       bool                          `json:"importing"`
		SourceRunning   bool                          `json:"sourceRunning"`
		SourceStopped   bool                          `json:"sourceStopped"`
		Restarted       bool                          `json:"restarted"`
		Deployed        bool                          `json:"deployed"`
		Retargeting     bool                          `json:"retargeting"`
		Completed       bool                          `json:"completed"`
	}

	relay struct {
		Kind           string `json:"kind,omitempty"`
		Artifact       string `json:"artifact,omitempty"`
		SourceUploadID string `json:"sourceUploadId,omitempty"`
		TargetUploadID string `json:"targetUploadId,omitempty"`
	}

	run struct {
		source     Environment
		target     Environment
		activityID string
		input      runInput
		state      checkpoint
	}
)

const (
	AvailabilityReady    Availability = "ready"
	AvailabilityOffline  Availability = "offline"
	AvailabilityOutdated Availability = "outdated"

	workflowName     = "project-transfer"
	checkpointID     = "project-transfer"
	admissionScope   = "project-transfer"
	operationTimeout = 30 * time.Minute
	maxChunkRetries  = 3
	maxLockRetries   = 10
	artifactFiles    = "project files"
	artifactBundle   = "project and its volumes"
)

var (
	errAlreadyRunning = common.Classify(common.ErrConflict, errors.New("a transfer of this project is already running"))

	chunkBackoff = concurrency.Backoff{
		Base: 500 * time.Millisecond,
		Max:  5 * time.Second,
	}
)

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) RegisterWorkflows(engine *flow.Engine) error {
	var err error
	s.flow = engine
	s.workflow, err = engine.Define(flow.Definition{
		Name:        workflowName,
		Version:     1,
		Fingerprint: "d7dfa4bd6563c744751e3ac9c4b8fd7bc0523b91483386d74be505a676eb21e6",
		Concurrency: 4,
		Timeout:     24 * time.Hour,
		Activity:    activity.StartRequest{Type: types.ActivityTypeResourceAction},
		Steps: []workflow.StepSpec{
			workflow.Step("transfer", engine.Handler(s.execute), workflow.WithMaxAttempts(1)),
		},
	})
	return err
}

func (s *Service) Preflight(ctx context.Context, opts Options) (types.ProjectTransferPreflight, error) {
	webhooks, err := s.deps.CountWebhooks(ctx, opts.SourceEnvironmentID, opts.ProjectID)
	if err != nil {
		return types.ProjectTransferPreflight{}, err
	}
	if authErr := s.authorize(ctx, requesters(ctx, opts, webhooks > 0)); authErr != nil {
		return types.ProjectTransferPreflight{}, authErr
	}
	return s.plan(ctx, opts)
}

func (s *Service) Start(ctx context.Context, opts Options) (types.ProjectTransferStarted, error) {
	webhooks, err := s.deps.CountWebhooks(ctx, opts.SourceEnvironmentID, opts.ProjectID)
	if err != nil {
		return types.ProjectTransferStarted{}, err
	}
	required := requesters(ctx, opts, webhooks > 0)
	if authErr := s.authorize(ctx, required); authErr != nil {
		return types.ProjectTransferStarted{}, authErr
	}
	plan, err := s.plan(ctx, opts)
	if err != nil {
		return types.ProjectTransferStarted{}, err
	}
	if blockedErr := blocked(plan); blockedErr != nil {
		return types.ProjectTransferStarted{}, blockedErr
	}
	admissionID := opts.SourceEnvironmentID + ":" + opts.ProjectID
	lease, admitted, err := s.deps.Backups.TryAcquireRun(ctx, admissionScope, admissionID)
	if err != nil {
		return types.ProjectTransferStarted{}, err
	}
	if !admitted {
		return types.ProjectTransferStarted{}, errAlreadyRunning
	}
	runID := uuid.New().String()
	release := s.deps.Backups.Hold(admissionScope, admissionID, runID, lease)
	activityID, err := s.flow.Submit(ctx, s.workflow, runInput{
		User:                opts.User,
		Requesters:          required,
		RetargetWebhooks:    webhooks > 0,
		RunID:               runID,
		TargetProjectID:     uuid.New().String(),
		SourceEnvironmentID: opts.SourceEnvironmentID,
		ProjectID:           opts.ProjectID,
		Request:             opts.Request,
	}, activity.StartRequest{
		EnvironmentID: opts.SourceEnvironmentID,
		ResourceType:  new("project"),
		ResourceID:    new(opts.ProjectID),
		ResourceName:  new(plan.ProjectName),
		StartedBy:     &opts.User,
		Step:          "Transferring project",
		LatestMessage: fmt.Sprintf("Transfer of %s to environment %s requested", plan.ProjectName, opts.Request.TargetEnvironmentID),
		Metadata: database.JSON{
			"action":              "transfer_project",
			"mode":                string(opts.Request.Mode),
			"name":                opts.Request.Name,
			"targetEnvironmentId": opts.Request.TargetEnvironmentID,
			"includeVolumes":      opts.Request.IncludeVolumes,
			"start":               opts.Request.Start,
		},
	})
	if err != nil {
		release(ctx)
		return types.ProjectTransferStarted{}, err
	}
	go func() {
		background := context.WithoutCancel(ctx)
		if _, waitErr := s.flow.Wait(background, s.workflow, activityID); waitErr != nil {
			slog.WarnContext(background, "Stopped waiting for project transfer workflow", "activityId", activityID, "error", waitErr)
		}
		release(background)
	}()
	return types.ProjectTransferStarted{ActivityID: activityID}, nil
}

func (s *Service) CreateResource(ctx context.Context, opts ResourceOptions) error {
	request := opts.Request
	volume := request.Kind == types.ProjectTransferResourceVolume
	if authErr := s.authorize(ctx, []types.BackupRequester{
		backup.NewRequester(ctx, opts.User, opts.SourceEnvironmentID, authz.PermProjectsRead),
		backup.NewRequester(ctx, opts.User, opts.SourceEnvironmentID, kit.Ternary(volume, authz.PermVolumesRead, authz.PermNetworksRead)),
		backup.NewRequester(ctx, opts.User, request.TargetEnvironmentID, kit.Ternary(volume, authz.PermVolumesCreate, authz.PermNetworksCreate)),
	}); authErr != nil {
		return authErr
	}
	source := s.deps.Environment(opts.SourceEnvironmentID)
	target := s.deps.Environment(request.TargetEnvironmentID)
	project, err := source.Project(ctx, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("load source project: %w", err)
	}
	if volume {
		return createVolume(ctx, project, source, target, request.Name, opts.User)
	}
	return createNetwork(ctx, project, source, target, request.Name, opts.User)
}

func createVolume(ctx context.Context, project types.Project, source, target Environment, name string, user types.Actor) error {
	if !slices.ContainsFunc(slices.Collect(maps.Values(project.Volumes)), func(config composeTypes.VolumeConfig) bool {
		return bool(config.External) && config.Name == name
	}) {
		return common.Classify(common.ErrBadRequest, fmt.Errorf("project %s has no external volume %s", project.Name, name))
	}
	found, exists, err := source.Volume(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect source volume %s: %w", name, err)
	}
	if !exists {
		return common.Classify(common.ErrNotFound, fmt.Errorf("external volume %s does not exist on the source environment either", name))
	}
	_, exists, err = target.Volume(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect target volume %s: %w", name, err)
	}
	if exists {
		return common.Classify(common.ErrConflict, fmt.Errorf("volume %s already exists on the target environment", name))
	}
	return target.CreateVolume(ctx, types.Volume{
		Name:    found.Name,
		Driver:  found.Driver,
		Options: found.Options,
		Labels:  found.Labels,
	}, user)
}

func createNetwork(ctx context.Context, project types.Project, source, target Environment, name string, user types.Actor) error {
	if !slices.ContainsFunc(slices.Collect(maps.Values(project.Networks)), func(config composeTypes.NetworkConfig) bool {
		return bool(config.External) && config.Name == name
	}) {
		return common.Classify(common.ErrBadRequest, fmt.Errorf("project %s has no external network %s", project.Name, name))
	}
	found, exists, err := source.Network(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect source network %s: %w", name, err)
	}
	if !exists {
		return common.Classify(common.ErrNotFound, fmt.Errorf("external network %s does not exist on the source environment either", name))
	}
	if found.ConfigFrom.Network != "" {
		return common.Classify(common.ErrBadRequest, fmt.Errorf("network %s takes its configuration from network %s; create both on the target environment manually", name, found.ConfigFrom.Network))
	}
	return target.CreateNetwork(ctx, types.Network{
		Name: found.Name,
		Options: types.NetworkCreateOptions{
			IPAM:       &found.IPAM,
			Options:    found.Options,
			Labels:     found.Labels,
			Driver:     found.Driver,
			Internal:   found.Internal,
			Attachable: found.Attachable,
			Ingress:    found.Ingress,
			EnableIPv6: found.EnableIPv6,
			EnableIPv4: new(found.EnableIPv4 || slices.ContainsFunc(found.IPAM.Config, func(config types.NetworkIPAMConfig) bool {
				prefix, parseErr := netip.ParsePrefix(config.Subnet)
				return parseErr == nil && prefix.Addr().Is4()
			})),
		},
	}, user)
}

func (s *Service) authorize(ctx context.Context, required []types.BackupRequester) error {
	for _, requester := range required {
		if err := s.deps.Backups.Authorize(ctx, requester); err != nil {
			return common.Classify(common.ErrForbidden, fmt.Errorf("permission denied: %s on environment %s: %w", requester.Permission, requester.EnvironmentID, err))
		}
	}
	return nil
}

func requesters(ctx context.Context, opts Options, webhooks bool) []types.BackupRequester {
	source, target, request := opts.SourceEnvironmentID, opts.Request.TargetEnvironmentID, opts.Request
	required := []types.BackupRequester{
		backup.NewRequester(ctx, opts.User, source, authz.PermProjectsRead),
		backup.NewRequester(ctx, opts.User, target, authz.PermProjectsCreate),
		backup.NewRequester(ctx, opts.User, target, authz.PermProjectsRead),
		backup.NewRequester(ctx, opts.User, target, authz.PermVolumesRead),
		backup.NewRequester(ctx, opts.User, target, authz.PermNetworksRead),
		backup.NewRequester(ctx, opts.User, target, authz.PermContainersList),
	}
	if request.IncludeVolumes {
		required = append(required,
			backup.NewRequester(ctx, opts.User, source, authz.PermVolumesBackup),
			backup.NewRequester(ctx, opts.User, target, authz.PermVolumesUpload),
			backup.NewRequester(ctx, opts.User, target, authz.PermVolumesCreate),
		)
	}
	if request.IncludeVolumes || request.Mode == types.ProjectTransferModeMove {
		required = append(required, backup.NewRequester(ctx, opts.User, source, authz.PermProjectsDown))
	}
	if webhooks && request.Mode == types.ProjectTransferModeMove {
		required = append(required,
			backup.NewRequester(ctx, opts.User, source, authz.PermWebhooksUpdate),
			backup.NewRequester(ctx, opts.User, target, authz.PermWebhooksCreate),
		)
	}
	if request.IncludeVolumes {
		required = append(required, backup.NewRequester(ctx, opts.User, source, authz.PermProjectsDeploy))
	}
	if request.Start {
		required = append(required, backup.NewRequester(ctx, opts.User, target, authz.PermProjectsDeploy))
	}
	return required
}

func blocked(plan types.ProjectTransferPreflight) error {
	var messages []string
	for _, check := range plan.Checks {
		if check.Severity == types.ProjectTransferSeverityError {
			messages = append(messages, check.Message)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	return common.Classify(common.ErrConflict, fmt.Errorf("transfer blocked: %s", strings.Join(messages, " ")))
}

func (s *Service) plan(ctx context.Context, opts Options) (types.ProjectTransferPreflight, error) {
	var plan types.ProjectTransferPreflight
	if err := s.check(ctx, &plan, opts); err != nil {
		return types.ProjectTransferPreflight{}, err
	}
	plan.Allowed = blocked(plan) == nil
	return plan, nil
}

func (s *Service) check(ctx context.Context, plan *types.ProjectTransferPreflight, opts Options) error {
	request := opts.Request
	source := s.deps.Environment(opts.SourceEnvironmentID)
	target := s.deps.Environment(request.TargetEnvironmentID)
	sourceReady := availabilityCheck(plan, source.Availability(ctx), types.ProjectTransferSourceOffline, types.ProjectTransferSourceOutdated, "source")
	targetReady := availabilityCheck(plan, target.Availability(ctx), types.ProjectTransferTargetOffline, types.ProjectTransferTargetOutdated, "target")
	if !sourceReady {
		return nil
	}
	project, err := source.Project(ctx, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("load source project: %w", err)
	}
	plan.ProjectName = project.Name
	plan.SourceRunning = project.RunningCount > 0
	if project.ComposeName == "" {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferComposeInvalid,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("The compose files of project %s cannot be loaded on the source environment; fix them before transferring the project.", project.Name),
		})
		return nil
	}
	if opts.SourceEnvironmentID == request.TargetEnvironmentID && projects.NormalizeProjectName(project.Name) == projects.NormalizeProjectName(request.Name) {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferSameProject,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("The target environment and name %s point at project %s itself; choose another environment or name.", request.Name, project.Name),
		})
	}
	gitOps := strings.TrimSpace(kit.FromPtr(project.GitOpsManagedBy)) != ""
	if gitOps && request.Mode == types.ProjectTransferModeMove {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferGitOpsMove,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("Project %s is managed by a GitOps sync; unlink the sync before moving it, because the sync would redeploy the source files that a move keeps.", project.Name),
		})
	}
	if gitOps && request.Mode == types.ProjectTransferModeCopy {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferGitOpsCopy,
			Severity: types.ProjectTransferSeverityWarning,
			Message:  fmt.Sprintf("Project %s is managed by a GitOps sync, but its copy %s on the target environment is not.", project.Name, request.Name),
		})
	}
	resolved, inspected := projects.ClassifyProjectVolumes(project), map[string]types.Volume{}
	if request.IncludeVolumes {
		resolved, inspected, err = projects.ResolveProjectVolumes(ctx, projects.ResolveProjectVolumesOptions{
			Inspect: source.Volume,
			Project: project,
		})
		if err != nil {
			return fmt.Errorf("inspect source volumes: %w", err)
		}
	}
	var externalVolumes, hostVolumes, sourceVolumes, missingVolumes, targetVolumes, built []string
	for _, volume := range resolved {
		switch volume.Kind {
		case types.ProjectVolumeExternal:
			externalVolumes = append(externalVolumes, volume.Name)
		case types.ProjectVolumeDriverOpts:
			hostVolumes = append(hostVolumes, volume.Name)
		case types.ProjectVolumeNamed:
			targetName := targetVolumeName(project.Name, volume.Key, volume.Name, request.Name)
			if request.IncludeVolumes {
				targetVolumes = append(targetVolumes, targetName)
				if volume.Missing {
					missingVolumes = append(missingVolumes, volume.Name)
					continue
				}
			}
			sourceVolumes = append(sourceVolumes, volume.Name)
			plan.Volumes = append(plan.Volumes, types.ProjectTransferVolume{
				Key:        volume.Key,
				SourceName: volume.Name,
				TargetName: targetName,
			})
		}
	}
	for _, volume := range plan.Volumes {
		if request.IncludeVolumes && slices.ContainsFunc(plan.Volumes, func(other types.ProjectTransferVolume) bool {
			return other.SourceName == volume.SourceName && other.TargetName != volume.TargetName
		}) {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferVolumeShared,
				Severity: types.ProjectTransferSeverityError,
				Message: fmt.Sprintf(
					"Volume %s of project %s is used under several Compose keys that resolve to different volume names on the target environment; "+
						"give those keys the same explicit name before transferring the project.",
					volume.SourceName, project.Name,
				),
			})
			break
		}
	}
	for _, volume := range plan.Volumes {
		if index := slices.IndexFunc(plan.Volumes, func(other types.ProjectTransferVolume) bool {
			return other.SourceName != volume.SourceName && other.TargetName == volume.TargetName
		}); request.IncludeVolumes && index >= 0 {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferVolumeShared,
				Severity: types.ProjectTransferSeverityError,
				Message: fmt.Sprintf(
					"Volumes %s and %s of project %s would both be named %s on the target environment; give them distinct explicit names before transferring the project.",
					volume.SourceName, plan.Volumes[index].SourceName, project.Name, volume.TargetName,
				),
			})
			break
		}
	}
	if !request.IncludeVolumes && len(sourceVolumes) > 0 {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferVolumesNotCopied,
			Severity: types.ProjectTransferSeverityWarning,
			Message:  fmt.Sprintf("Volume data is not included, so these volumes of project %s start empty on the target environment: %s.", project.Name, strings.Join(sourceVolumes, ", ")),
		})
	}
	if len(missingVolumes) > 0 {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferVolumeMissing,
			Severity: types.ProjectTransferSeverityWarning,
			Message: fmt.Sprintf(
				"These volumes of project %s do not exist on the source environment, so they are skipped and start empty on the target environment: %s.",
				project.Name, strings.Join(missingVolumes, ", "),
			),
		})
	}
	if len(hostVolumes) > 0 {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferVolumeDataExternal,
			Severity: types.ProjectTransferSeverityWarning,
			Message: fmt.Sprintf(
				"These volumes of project %s keep their data outside Docker through another driver or driver options, "+
					"so their data is not copied and they are created from the same configuration on the target environment: %s.",
				project.Name, strings.Join(hostVolumes, ", "),
			),
		})
	}
	for _, service := range project.Services {
		if service.Build != nil {
			built = append(built, service.Name)
		}
	}
	if len(built) > 0 {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferBuildRequired,
			Severity: types.ProjectTransferSeverityWarning,
			Message: fmt.Sprintf(
				"These services of project %s are built from source, so their images must be rebuilt on the target environment: %s.",
				project.Name, strings.Join(slices.Sorted(slices.Values(built)), ", "),
			),
		})
	}
	if !targetReady {
		return nil
	}
	required, err := s.requiredSpace(ctx, opts.SourceEnvironmentID, kit.Ternary(request.IncludeVolumes, sourceVolumes, nil), inspected)
	if err != nil {
		return err
	}
	return targetChecks(ctx, plan, project, target, request, targetVolumes, externalVolumes, required)
}

func (s *Service) requiredSpace(ctx context.Context, sourceEnvironmentID string, names []string, inspected map[string]types.Volume) (int64, error) {
	var sizes map[string]volumeDomain.VolumeSizeData
	if sourceEnvironmentID == environment.LocalEnvironmentID && slices.ContainsFunc(names, func(name string) bool {
		return inspected[name].Size <= 0
	}) {
		var err error
		if sizes, err = s.deps.VolumeSizes(ctx); err != nil {
			return 0, fmt.Errorf("measure source volumes: %w", err)
		}
	}
	var bundleSize, largest int64
	for _, name := range slices.Compact(slices.Sorted(slices.Values(names))) {
		size := max(inspected[name].Size, sizes[name].Size, 0)
		bundleSize += size
		largest = max(largest, size)
	}
	return bundleSize + largest, nil
}

func targetChecks(
	ctx context.Context, plan *types.ProjectTransferPreflight, project types.Project, target Environment,
	request types.ProjectTransferRequest, targetVolumes, externalVolumes []string, required int64,
) error {
	existing, err := target.Projects(ctx, "")
	if err != nil {
		return fmt.Errorf("list target projects: %w", err)
	}
	composeName := projects.NormalizeProjectName(request.Name)
	conflict, err := target.ComposeProjectInUse(ctx, composeName)
	if err != nil {
		return fmt.Errorf("list target containers of Compose project %s: %w", composeName, err)
	}
	for _, candidate := range existing {
		if strings.EqualFold(candidate.Name, request.Name) || strings.EqualFold(candidate.DirName, projects.SanitizeProjectName(request.Name)) ||
			projects.NormalizeProjectName(candidate.Name) == composeName || projects.NormalizeProjectName(candidate.ComposeName) == composeName {
			conflict = true
		}
	}
	if conflict {
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     types.ProjectTransferNameConflict,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("A project or Compose stack named %s already exists on the target environment.", request.Name),
		})
	}
	for _, name := range targetVolumes {
		_, exists, existsErr := target.Volume(ctx, name)
		if existsErr != nil {
			return fmt.Errorf("inspect target volume %s: %w", name, existsErr)
		}
		if exists {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferVolumeExists,
				Severity: types.ProjectTransferSeverityError,
				Message:  fmt.Sprintf("Volume %s already exists on the target environment.", name),
			})
		}
	}
	for _, name := range externalVolumes {
		_, exists, existsErr := target.Volume(ctx, name)
		if existsErr != nil {
			return fmt.Errorf("inspect target volume %s: %w", name, existsErr)
		}
		if !exists {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferExternalMissing,
				Severity: types.ProjectTransferSeverityError,
				Message:  fmt.Sprintf("External volume %s does not exist on the target environment.", name),
				Resource: name,
			})
		}
	}
	if required > 0 {
		probe, probeErr := target.CreateUpload(ctx, types.UploadKindProjectArchive, types.CreateUploadSessionRequest{
			Filename:  projects.SanitizeProjectName(request.Name) + types.ProjectBundleExtension,
			Size:      required,
			ChunkSize: types.UploadDefaultChunkSize,
		})
		if probeErr != nil {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferTargetSpace,
				Severity: types.ProjectTransferSeverityError,
				Message: fmt.Sprintf(
					"The target environment cannot provide the %s of free space that importing the volume data of project %s needs: %v",
					units.BytesSize(float64(required)), project.Name, probeErr,
				),
			})
		} else if deleteErr := target.DeleteUpload(ctx, types.UploadKindProjectArchive, probe.ID); deleteErr != nil {
			return fmt.Errorf("remove the free space check upload on the target environment: %w", deleteErr)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(project.Networks)) {
		config := project.Networks[key]
		if !config.External {
			continue
		}
		_, exists, existsErr := target.Network(ctx, config.Name)
		if existsErr != nil {
			return fmt.Errorf("inspect target network %s: %w", config.Name, existsErr)
		}
		if !exists {
			plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
				Code:     types.ProjectTransferNetworkMissing,
				Severity: types.ProjectTransferSeverityError,
				Message:  fmt.Sprintf("External network %s does not exist on the target environment.", config.Name),
				Resource: config.Name,
			})
		}
	}
	return nil
}

func availabilityCheck(plan *types.ProjectTransferPreflight, availability Availability, offlineCode, outdatedCode, side string) bool {
	switch availability {
	case AvailabilityOffline:
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     offlineCode,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("The %s environment is offline.", side),
		})
		return false
	case AvailabilityOutdated:
		plan.Checks = append(plan.Checks, types.ProjectTransferCheck{
			Code:     outdatedCode,
			Severity: types.ProjectTransferSeverityError,
			Message:  fmt.Sprintf("The agent of the %s environment does not support project transfers; upgrade it first.", side),
		})
		return false
	}
	return true
}

func targetVolumeName(sourceProject, key, sourceName, name string) string {
	if sourceName == projects.NormalizeProjectName(sourceProject)+"_"+key {
		return projects.NormalizeProjectName(name) + "_" + key
	}
	return sourceName
}

func (s *Service) execute(ctx context.Context, t flow.Task) (any, error) {
	var input runInput
	if err := t.Payload(&input); err != nil {
		return nil, err
	}
	lease, admitted, err := s.deps.Backups.AcquireRun(ctx, admissionScope, input.SourceEnvironmentID+":"+input.ProjectID, input.RunID)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, errAlreadyRunning
	}
	defer lease.Release(context.WithoutCancel(ctx))
	r := &run{
		source:     s.deps.Environment(input.SourceEnvironmentID),
		target:     s.deps.Environment(input.Request.TargetEnvironmentID),
		activityID: t.ActivityID(),
		input:      input,
	}
	previous, _ := jobcontext.Run(ctx)
	for _, target := range previous.Outcome.Targets {
		if target.ID != checkpointID || len(target.RecoveryData) == 0 {
			continue
		}
		if decodeErr := json.Unmarshal(target.RecoveryData, &r.state); decodeErr != nil {
			return nil, fmt.Errorf("decode transfer checkpoint: %w", decodeErr)
		}
	}
	if !r.state.Completed {
		if err = s.transfer(ctx, r); err != nil {
			return nil, s.settle(ctx, r, err)
		}
	}
	message := summary(r)
	s.report(ctx, r, types.ActivityMessageLevelSuccess, message)
	return types.SchedulerOutcome{
		Status:     types.RunStatusSucceeded,
		Message:    message,
		ActivityID: r.activityID,
	}, nil
}

func (s *Service) transfer(ctx context.Context, r *run) error {
	err := s.authorize(ctx, r.input.Requesters)
	for _, phase := range []func(context.Context, *run) error{s.prepare, s.copyProject, s.restartSource, s.deployTarget, s.completeMove} {
		if err = cmp.Or(err, ctx.Err()); err == nil {
			err = phase(ctx, r)
		}
	}
	if err == nil && r.state.Relay != (relay{}) {
		err = s.finishRelay(ctx, r)
	}
	if err != nil {
		return err
	}
	r.state.Completed = true
	return s.save(ctx, r)
}

func (s *Service) save(ctx context.Context, r *run) error {
	data, err := json.Marshal(r.state)
	if err != nil {
		return err
	}
	return jobcontext.Progress(ctx, types.TargetOutcome{
		ResourceType: "project_transfer",
		ID:           checkpointID,
		Status:       types.RunStatusRunning,
		ActivityID:   r.activityID,
		RecoveryData: data,
	})
}

func (s *Service) report(ctx context.Context, r *run, level types.ActivityMessageLevel, message string) {
	if _, err := s.deps.Activities.AppendMessage(context.WithoutCancel(ctx), r.activityID, activity.AppendMessageRequest{
		Level:   level,
		Message: message,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to append project transfer activity message", "activityId", r.activityID, "level", level, "error", err)
	}
}

func (s *Service) prepare(ctx context.Context, r *run) error {
	if r.state.Prepared {
		return nil
	}
	plan, err := s.plan(ctx, Options{
		User:                r.input.User,
		SourceEnvironmentID: r.input.SourceEnvironmentID,
		ProjectID:           r.input.ProjectID,
		Request:             r.input.Request,
	})
	if err != nil {
		return err
	}
	if blockedErr := blocked(plan); blockedErr != nil {
		return blockedErr
	}
	r.state = checkpoint{
		Volumes:       plan.Volumes,
		Prepared:      true,
		SourceRunning: plan.SourceRunning,
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, "Checks passed; the source project is "+kit.Ternary(plan.SourceRunning, "running", "stopped"))
	return s.save(ctx, r)
}

func (s *Service) copyProject(ctx context.Context, r *run) error {
	if r.state.TargetProjectID != "" {
		if r.input.Request.IncludeVolumes && len(r.state.CopiedVolumes) < len(r.state.Volumes) {
			return errors.New("the transfer was interrupted while it copied the volumes; start it again")
		}
		return nil
	}
	if r.state.Importing {
		return errors.New("the transfer was interrupted while the target environment imported the project; start it again")
	}
	withVolumes := r.input.Request.IncludeVolumes && len(r.state.Volumes) > 0
	if withVolumes && (r.source.Availability(ctx) != AvailabilityReady || r.target.Availability(ctx) != AvailabilityReady) {
		return errors.New("the source or target environment no longer supports copying volume data; start the transfer again")
	}
	if withVolumes && r.state.SourceRunning && !r.state.SourceStopped {
		r.state.SourceStopped = true
		if err := s.save(ctx, r); err != nil {
			return err
		}
		s.report(ctx, r, types.ActivityMessageLevelInfo, "Stopping the source project so its volume data is consistent")
		downCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		err := r.source.DownProject(downCtx, r.input.ProjectID, r.input.User)
		cancel()
		if err != nil {
			return fmt.Errorf("stop the source project: %w", err)
		}
	}
	request := types.ExportProjectArchive{Volumes: nil, StopProject: withVolumes}
	if withVolumes {
		current, err := r.source.Project(ctx, r.input.ProjectID)
		if err != nil {
			return fmt.Errorf("check the source project: %w", err)
		}
		if current.RunningCount > 0 {
			return fmt.Errorf("%d source container(s) are still running; volume data was not copied", current.RunningCount)
		}
		classified := projects.ClassifyProjectVolumes(current)
		for _, volume := range r.state.Volumes {
			if !slices.ContainsFunc(classified, func(planned types.ProjectVolume) bool {
				return planned.Key == volume.Key && planned.Name == volume.SourceName && planned.Kind == types.ProjectVolumeNamed
			}) {
				return fmt.Errorf("volume %s of the source project changed after the checks; volume data was not copied", volume.Key)
			}
			request.Volumes = append(request.Volumes, volume.Key)
		}
	}
	artifact := kit.Ternary(withVolumes, artifactBundle, artifactFiles)
	budget := operationTimeout * time.Duration(1+len(request.Volumes))
	source, err := s.resumedSource(ctx, r, types.UploadKindProjectArchive, artifact)
	if err != nil {
		return err
	}
	if source.ID == "" {
		s.report(ctx, r, types.ActivityMessageLevelInfo, "Exporting the "+artifact+" on the source environment")
		exportCtx, cancel := context.WithTimeout(ctx, budget)
		source, err = r.source.ExportProject(exportCtx, r.input.ProjectID, request, r.input.User)
		cancel()
		if err != nil {
			return fmt.Errorf("export the %s: %w", artifact, err)
		}
	}
	if restartErr := s.restartSource(ctx, r); restartErr != nil {
		return restartErr
	}
	uploadID, err := s.relay(ctx, r, types.UploadKindProjectArchive, artifact, source)
	if err != nil {
		return err
	}
	r.state.Importing = true
	r.state.TargetCompose = projects.NormalizeProjectName(r.input.Request.Name)
	if saveErr := s.save(ctx, r); saveErr != nil {
		return saveErr
	}
	importCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	r.state.TargetProjectID, err = r.target.ImportProject(importCtx, types.ImportProjectArchive{
		UploadID:  uploadID,
		Name:      r.input.Request.Name,
		ProjectID: r.input.TargetProjectID,
	}, r.input.User)
	if err != nil {
		r.state.TargetProjectID = ""
		return fmt.Errorf("import project on the target environment: %w", err)
	}
	if withVolumes {
		targetProject, projectErr := r.targetProject(ctx)
		if projectErr != nil {
			return projectErr
		}
		for _, volume := range r.state.Volumes {
			r.state.CreatedVolumes = append(r.state.CreatedVolumes, targetProject.Volumes[volume.Key].Name)
		}
		r.state.CopiedVolumes = request.Volumes
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, fmt.Sprintf("Project %s created on the target environment (stopped)", r.input.Request.Name))
	return s.finishRelay(ctx, r)
}

func (s *Service) restartSource(ctx context.Context, r *run) error {
	if r.input.Request.Mode != types.ProjectTransferModeCopy || !r.state.SourceStopped || !r.state.SourceRunning {
		return nil
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, "Starting the source project again")
	startCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := r.source.DeployProject(startCtx, r.input.ProjectID, r.input.User); err != nil {
		return fmt.Errorf("start the source project again: %w", err)
	}
	r.state.SourceStopped = false
	r.state.Restarted = true
	return s.save(ctx, r)
}

func (s *Service) deployTarget(ctx context.Context, r *run) error {
	if !r.input.Request.Start || r.state.Deployed {
		return nil
	}
	targetProject, err := r.targetProject(ctx)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(targetProject.Volumes)) {
		config := targetProject.Volumes[key]
		if bool(config.External) || slices.Contains(r.state.CreatedVolumes, config.Name) {
			continue
		}
		_, exists, existsErr := r.target.Volume(ctx, config.Name)
		if existsErr != nil {
			return fmt.Errorf("inspect target volume %s: %w", config.Name, existsErr)
		}
		if !exists {
			r.state.CreatedVolumes = append(r.state.CreatedVolumes, config.Name)
		}
	}
	if saveErr := s.save(ctx, r); saveErr != nil {
		return saveErr
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, "Deploying the project on the target environment")
	deployCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if deployErr := r.target.DeployProject(deployCtx, r.state.TargetProjectID, r.input.User); deployErr != nil {
		return fmt.Errorf("deploy the project on the target environment: %w", deployErr)
	}
	r.state.Deployed = true
	return s.save(ctx, r)
}

func (s *Service) completeMove(ctx context.Context, r *run) error {
	if r.input.Request.Mode != types.ProjectTransferModeMove {
		return nil
	}
	r.state.SourceStopped = true
	if err := s.save(ctx, r); err != nil {
		return err
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, "Stopping the source project; its files and volumes are kept")
	downCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	err := r.source.DownProject(downCtx, r.input.ProjectID, r.input.User)
	cancel()
	if err != nil {
		return fmt.Errorf("stop the source project: %w", err)
	}
	if !r.input.RetargetWebhooks {
		r.state.WebhooksKept, err = s.deps.CountWebhooks(ctx, r.input.SourceEnvironmentID, r.input.ProjectID)
		if err != nil {
			return fmt.Errorf("check webhooks of the source project: %w", err)
		}
		return nil
	}
	r.state.Retargeting = true
	if saveErr := s.save(ctx, r); saveErr != nil {
		return saveErr
	}
	moved, err := s.deps.RetargetWebhooks(ctx, types.ProjectWebhookRetarget{
		SourceEnvironmentID: r.input.SourceEnvironmentID,
		SourceProjectID:     r.input.ProjectID,
		TargetEnvironmentID: r.input.Request.TargetEnvironmentID,
		TargetProjectID:     r.state.TargetProjectID,
	})
	if err != nil {
		return fmt.Errorf("re-point webhooks: %w", err)
	}
	r.state.Webhooks += moved
	return s.save(ctx, r)
}

func (s *Service) resumedSource(ctx context.Context, r *run, kind, artifact string) (types.UploadSession, error) {
	if r.state.Relay == (relay{}) {
		return types.UploadSession{}, nil
	}
	if r.state.Relay.Artifact != artifact || r.state.Relay.SourceUploadID == "" {
		return types.UploadSession{}, s.finishRelay(ctx, r)
	}
	session, err := r.source.Upload(ctx, kind, r.state.Relay.SourceUploadID)
	if err != nil || !session.Complete {
		return types.UploadSession{}, s.finishRelay(ctx, r)
	}
	return session, nil
}

func (s *Service) relay(ctx context.Context, r *run, kind, artifact string, source types.UploadSession) (string, error) {
	var received types.UploadSession
	if previous := r.state.Relay; previous.SourceUploadID == source.ID && previous.TargetUploadID != "" {
		session, lookupErr := r.target.Upload(ctx, kind, previous.TargetUploadID)
		if lookupErr == nil {
			received = session
		} else {
			s.removeUpload(context.WithoutCancel(ctx), r, r.target, "target", kind, previous.TargetUploadID)
		}
	}
	if received.ID == "" {
		target, err := r.target.CreateUpload(ctx, kind, types.CreateUploadSessionRequest{
			Filename:  source.Filename,
			Size:      source.Size,
			ChunkSize: source.ChunkSize,
		})
		if err != nil {
			r.state.Relay = relay{
				Kind:           kind,
				Artifact:       artifact,
				SourceUploadID: source.ID,
			}
			return "", errors.Join(fmt.Errorf("create the %s upload on the target environment: %w", artifact, err), s.save(ctx, r))
		}
		r.state.Relay = relay{
			Kind:           kind,
			Artifact:       artifact,
			SourceUploadID: source.ID,
			TargetUploadID: target.ID,
		}
		if saveErr := s.save(ctx, r); saveErr != nil {
			return "", saveErr
		}
		received = target
	}
	size := units.BytesSize(float64(source.Size))
	s.report(ctx, r, types.ActivityMessageLevelInfo, fmt.Sprintf("Copying the %s (%s)", artifact, size))
	failures := 0
	for index := 0; index < source.TotalChunks; {
		if slices.Contains(received.ReceivedChunks, index) {
			index++
			continue
		}
		data, chunkErr := r.source.ReadChunk(ctx, kind, source.ID, index)
		if chunkErr == nil {
			chunkErr = r.target.WriteChunk(ctx, kind, received.ID, index, data)
		}
		if chunkErr == nil {
			index++
			failures = 0
			continue
		}
		failures++
		if failures > maxChunkRetries || ctx.Err() != nil {
			return "", fmt.Errorf("copy chunk %d of the %s: %w", index, artifact, chunkErr)
		}
		if sleepErr := chunkBackoff.Sleep(ctx, failures-1); sleepErr != nil {
			return "", sleepErr
		}
	}
	s.report(ctx, r, types.ActivityMessageLevelInfo, fmt.Sprintf("Copied the %s (%s)", artifact, size))
	return received.ID, nil
}

func (s *Service) finishRelay(ctx context.Context, r *run) error {
	current := r.state.Relay
	r.state.Relay = relay{}
	s.removeUpload(context.WithoutCancel(ctx), r, r.source, "source", current.Kind, current.SourceUploadID)
	s.removeUpload(context.WithoutCancel(ctx), r, r.target, "target", current.Kind, current.TargetUploadID)
	return s.save(ctx, r)
}

// removeUpload retries like a chunk copy; an upload it cannot remove is kept in the checkpoint and reported.
func (s *Service) removeUpload(ctx context.Context, r *run, env Environment, side, kind, uploadID string) {
	if uploadID == "" {
		return
	}
	err := env.DeleteUpload(ctx, kind, uploadID)
	for attempt := 0; err != nil && attempt < maxChunkRetries; attempt++ {
		if sleepErr := chunkBackoff.Sleep(ctx, attempt); sleepErr != nil {
			break
		}
		err = env.DeleteUpload(ctx, kind, uploadID)
	}
	if err == nil {
		return
	}
	leftover := fmt.Sprintf("%s upload %s on the %s environment (%v)", kind, uploadID, side, err)
	r.state.Leftovers = append(r.state.Leftovers, leftover)
	s.report(ctx, r, types.ActivityMessageLevelWarning, "Could not remove the temporary "+leftover)
}

func (s *Service) settle(ctx context.Context, r *run, err error) error {
	if ctx.Err() != nil && !errors.Is(context.Cause(ctx), activity.ErrCanceled) && !errors.Is(context.Cause(ctx), flow.ErrInstanceFinished) {
		return err
	}
	cleanupCtx := context.WithoutCancel(ctx)
	cause := err
	if errors.Is(context.Cause(ctx), activity.ErrCanceled) {
		cause = activity.ErrCanceled
	}
	message := fmt.Sprintf("Transfer failed: %v. %s", cause, strings.Join(s.cleanup(cleanupCtx, r), " "))
	s.report(cleanupCtx, r, types.ActivityMessageLevelError, message)
	return errors.New(message)
}

func (s *Service) cleanup(ctx context.Context, r *run) []string {
	var notes []string
	switch {
	case r.state.SourceStopped && r.state.SourceRunning:
		restartCtx, cancelRestart := context.WithTimeout(ctx, operationTimeout)
		err := untilUnlocked(restartCtx, func(ctx context.Context) error {
			return r.source.DeployProject(ctx, r.input.ProjectID, r.input.User)
		})
		cancelRestart()
		if err != nil {
			notes = append(notes, fmt.Sprintf("The source project could not be restarted: %v.", err))
		} else {
			notes = append(notes, "The source project was restarted.")
		}
	case r.state.Restarted:
		notes = append(notes, "The source project was stopped to copy its volume data and is running again.")
	case r.state.SourceStopped:
		notes = append(notes, "The source project was stopped before the transfer and stays stopped.")
	default:
		notes = append(notes, "The source project was not changed.")
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	current := r.state.Relay
	r.state.Relay = relay{}
	s.removeUpload(ctx, r, r.source, "source", current.Kind, current.SourceUploadID)
	s.removeUpload(ctx, r, r.target, "target", current.Kind, current.TargetUploadID)
	leftover := slices.Clone(r.state.Leftovers)
	name := r.input.Request.Name
	targetProjectID := r.state.TargetProjectID
	if targetProjectID == "" && r.state.Importing {
		targetProjectID = r.input.TargetProjectID
	}
	keep := false
	if r.state.Retargeting {
		if _, err := s.deps.RetargetWebhooks(ctx, types.ProjectWebhookRetarget{
			SourceEnvironmentID: r.input.Request.TargetEnvironmentID,
			SourceProjectID:     r.state.TargetProjectID,
			TargetEnvironmentID: r.input.SourceEnvironmentID,
			TargetProjectID:     r.input.ProjectID,
		}); err != nil {
			keep = true
			notes = append(notes, fmt.Sprintf(
				"Webhooks could not be pointed back at the source project (%v), "+
					"so project %s and its volumes were kept on the target environment, where those webhooks point.",
				err, name,
			))
		} else {
			notes = append(notes, "Webhooks point at the source project again.")
		}
	}
	removedProject := false
	if targetProjectID != "" && !keep {
		err := untilUnlocked(ctx, func(ctx context.Context) error {
			return r.target.DestroyProject(ctx, targetProjectID, r.input.User)
		})
		switch {
		case err == nil:
			removedProject = true
		case !errors.Is(err, common.ErrNotFound):
			leftover = append(leftover, fmt.Sprintf("project %s (%v)", name, err))
		}
	}
	removable := slices.Clone(r.state.CreatedVolumes)
	if r.state.Importing {
		for _, volume := range r.state.Volumes {
			removable = append(removable, volume.TargetName)
		}
	}
	removedVolume := false
	for _, volume := range kit.Ternary(keep, nil, slices.Compact(slices.Sorted(slices.Values(removable)))) {
		existing, exists, err := r.target.Volume(ctx, volume)
		if err == nil && exists && r.state.TargetCompose != "" && docker.ComposeProjectLabel(existing.Labels) == r.state.TargetCompose {
			removedVolume = true
			err = r.target.DeleteVolume(ctx, volume, r.input.User)
		}
		if err != nil {
			leftover = append(leftover, fmt.Sprintf("volume %s (%v)", volume, err))
		}
	}
	created := removedProject || removedVolume
	switch {
	case len(leftover) > 0:
		notes = append(notes, "Could not clean up: "+strings.Join(leftover, ", ")+".")
	case keep:
	case created:
		notes = append(notes, "Everything this transfer created on the target environment was removed.")
	case r.state.Importing:
		notes = append(notes, fmt.Sprintf("Project %s was not found on the target environment; if its interrupted import still finishes there, remove it.", name))
	default:
		notes = append(notes, "Nothing was created on the target environment.")
	}
	return notes
}

func untilUnlocked(ctx context.Context, op func(ctx context.Context) error) error {
	err := op(ctx)
	for attempt := 0; errors.Is(err, common.ErrProjectLocked) && attempt < maxLockRetries; attempt++ {
		if sleepErr := chunkBackoff.Sleep(ctx, attempt); sleepErr != nil {
			return errors.Join(err, sleepErr)
		}
		err = op(ctx)
	}
	return err
}

func (r *run) targetProject(ctx context.Context) (types.Project, error) {
	project, err := r.target.Project(ctx, r.state.TargetProjectID)
	if err != nil {
		return types.Project{}, fmt.Errorf("load the imported project on the target environment: %w", err)
	}
	if project.ComposeName == "" {
		return types.Project{}, fmt.Errorf("the compose files of project %s cannot be loaded on the target environment", r.input.Request.Name)
	}
	r.state.TargetCompose = project.ComposeName
	return project, nil
}

func summary(r *run) string {
	verb := "copied"
	if r.input.Request.Mode == types.ProjectTransferModeMove {
		verb = "moved"
	}
	parts := []string{fmt.Sprintf("Project %s to environment %s as %s", verb, r.input.Request.TargetEnvironmentID, r.input.Request.Name)}
	if r.input.Request.IncludeVolumes {
		parts = append(parts, fmt.Sprintf("%d volume(s) copied", len(r.state.CopiedVolumes)))
	}
	parts = append(parts, "target "+kit.Ternary(r.state.Deployed, "running", "stopped"))
	if len(r.state.Leftovers) > 0 {
		parts = append(parts, "could not remove these temporary uploads: "+strings.Join(r.state.Leftovers, ", "))
	}
	if r.input.Request.Mode == types.ProjectTransferModeMove {
		parts = append(parts, fmt.Sprintf("source stopped with files and volumes kept, %d webhook(s) re-pointed", r.state.Webhooks))
		if r.state.WebhooksKept > 0 {
			parts = append(parts, fmt.Sprintf("%d webhook(s) added during the move still point at the source project because the move started without webhook permissions", r.state.WebhooksKept))
		}
	} else {
		parts = append(parts, "source "+kit.Ternary(r.state.SourceRunning && !r.state.SourceStopped, "running", "stopped"))
	}
	return strings.Join(parts, "; ")
}
