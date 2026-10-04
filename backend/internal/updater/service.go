package updater

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater"
	"go.getarcane.app/updater/labels"
	"go.getarcane.app/updater/pkg/utils/tagpolicy"
	"go.getarcane.app/updater/refs"
	updatertypes "go.getarcane.app/updater/types"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater/children/execution"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater/children/recovery"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

// UpdaterService is Arcane's handler-facing service for the standalone updater engine.
type UpdaterService struct {
	admission   *runs.Admission
	config      *config.Config
	coordinator *runs.Coordinator
	roles       *role.RoleService
	deps        updaterDependenciesInternal
	engine      *updater.Service
	execution   *execution.Service
	recovery    *recovery.Service
	// updateMu serializes per-container updates. docker compose's recreate
	// pipeline is not concurrency-safe for sibling containers sharing a
	// namespace. ponytail: global lock ceiling — all updates serialize; fine
	// for a UI, upgrade to per-project if batch throughput ever matters.
	updateMu sync.Mutex
}

type updaterDependenciesInternal struct {
	DB                     *database.DB
	Docker                 *dockerInternal.DockerClientService
	Settings               *settings.SettingsService
	Projects               *projectpkg.ProjectService
	ImagePuller            *image.ImageService
	ImageUpdates           *imageupdate.ImageUpdateService
	RegistryDigestResolver *registry.ContainerRegistryService
	Events                 *event.EventService
	Notifications          *notification.NotificationService
	SelfUpgrade            selfUpgradeServiceInternal
	Activity               *activity.ActivityService
	SystemUser             usertypes.Actor
	Logger                 *slog.Logger
}

type selfUpgradeServiceInternal interface {
	// TriggerUpgradeViaCLI returns the spawned upgrader container's ID, which this
	// service does not need — only update-all's manager step uses it.
	TriggerUpgradeViaCLI(ctx context.Context, user usertypes.Actor, target updater.SelfUpdateTarget) (string, error)
}

// NewUpdaterService constructs the Arcane updater facade.
func NewUpdaterService(
	db *database.DB,
	localSettings *settings.SettingsService,
	localDocker *dockerInternal.DockerClientService,
	projectService *projectpkg.ProjectService,
	imageUpdates *imageupdate.ImageUpdateService,
	registries *registry.ContainerRegistryService,
	events *event.EventService,
	imageSvc *image.ImageService,
	localNotifications *notification.NotificationService,
	upgrade selfUpgradeServiceInternal,
	activityService *activity.ActivityService,
	cfg *config.Config,
	coordinator *runs.Coordinator,
	admission *runs.Admission,
	roles *role.RoleService,
) (*UpdaterService, error) {
	service := &UpdaterService{
		config: cfg, coordinator: coordinator, admission: admission, roles: roles,
		deps: updaterDependenciesInternal{
			DB:                     db,
			Docker:                 localDocker,
			Settings:               localSettings,
			Projects:               projectService,
			ImagePuller:            imageSvc,
			ImageUpdates:           imageUpdates,
			RegistryDigestResolver: registries,
			Events:                 events,
			Notifications:          localNotifications,
			SelfUpgrade:            upgrade,
			Activity:               activityService,
			SystemUser:             usertypes.SystemUser,
		},
	}
	service.recovery = recovery.NewService(
		service.DockerClient,
		service.registryDigestResolverInternal,
		service.PendingImageUpdates,
		activityIDFromContextInternal,
		service.acquireUpdateInternal,
		service.ApplyPending,
	)
	service.execution = execution.NewService(execution.Dependencies{
		Coordinator:      coordinator,
		Config:           cfg,
		Roles:            roles,
		Activity:         activityService,
		Logger:           service.loggerInternal,
		AcquireUpdate:    service.acquireUpdateInternal,
		UpdateBusy:       errUpdateBusyInternal,
		TrackActivity:    service.trackActivityInternal,
		RunUpdate:        service.runSingleContainerUpdateInternal,
		FinishUpdate:     service.finishSingleContainerUpdateInternal,
		FreezeContainer:  service.recovery.FreezeContainer,
		ConfirmTarget:    service.recovery.ConfirmTarget,
		WithFrozenTarget: service.recovery.WithSingleTarget,
	})
	engine, err := updater.New(service.configInternal())
	if err != nil {
		return nil, fmt.Errorf("configure updater engine: %w", err)
	}
	service.engine = engine
	events.SetDockerUpdatingContainers(func() []string { return engine.Status().ContainerIDs })
	return service, nil
}

func (s *UpdaterService) configInternal() updater.Config {
	return updater.Config{
		DockerClientProvider:   s,
		ImagePuller:            s,
		PendingStore:           s,
		RunRecorder:            s,
		Settings:               s,
		RegistryDigestResolver: s.registryDigestResolverInternal(),
		RegistryTagLister:      s,
		ProjectUpdater:         s,
		SelfUpdater:            s,
		Notifier:               s,
		EventRecorder:          s,
		UsedImageCollector:     updater.UsedImageCollectorFunc(s.CollectUsedImages),
		LabelPolicy:            updater.DefaultLabelPolicy(),
		SelfContainerID:        selfContainerIDInternal(),
		Logger:                 s.loggerInternal(),
	}
}

// selfContainerIDInternal returns the ID of the container Arcane runs in, so
// the updater engine routes it through the CLI self-updater even when the
// container is missing the Arcane labels. Empty when not running in Docker.
func selfContainerIDInternal() string {
	id, err := cgroup.CurrentContainerID()
	return kit.Ternary(err != nil, "", id)
}

func (s *UpdaterService) engineInternal() *updater.Service {
	return s.engine
}

func (s *UpdaterService) loggerInternal() *slog.Logger {
	if s.deps.Logger != nil {
		return s.deps.Logger
	}
	return slog.Default()
}

func (s *UpdaterService) registryDigestResolverInternal() updater.RegistryDigestResolver {
	if s == nil || s.deps.RegistryDigestResolver == nil {
		return nil
	}
	return s.deps.RegistryDigestResolver
}

// ApplyPending executes pending image updates. When the options carry
// resource IDs the run is scoped: the engine's ApplyPending has no resource
// filtering (it would apply every pending update), so scoped requests resolve
// to concrete containers and go through the engine's single-container path
// instead — same activity, events, and cleanup either way.
func (s *UpdaterService) ApplyPending(ctx context.Context, options arcaneupdater.Options) (out *arcaneupdater.Result, err error) {
	var release func()
	ctx, release, err = s.acquireUpdateInternal(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	batchCompleted := false
	activityID := s.startAutoUpdateActivityInternal(ctx, options.DryRun)
	out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
	ctx = s.trackActivityInternal(ctx, activityID)
	ctx = contextWithActivityIDInternal(ctx, activityID)
	progress := &updateProgressInternal{}
	ctx = context.WithValue(ctx, updateProgressKeyInternal{}, progress)
	notifyBatch := &containerUpdateBatchInternal{}
	ctx = context.WithValue(ctx, containerUpdateBatchContextKeyInternal{}, notifyBatch)

	defer func() {
		s.flushBatchedContainerUpdatesInternal(ctx, notifyBatch)
		if out == nil {
			out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
		}
		if out.Duration == "" {
			out.Duration = time.Since(start).String()
		}
		out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
		err = progress.completeBatchInternal(ctx, options, out, batchCompleted, err)
		s.completeAutoUpdateActivityInternal(ctx, activityID, out, err)
	}()

	ctx, err = s.recovery.FreezePending(ctx)
	if err != nil {
		return out, err
	}

	if activityID != "" && s.deps.Activity != nil {
		// Bounded slot wait: an unbounded wait behind other long-running runs
		// would strand the queued activity row (the completion defer above
		// flips it to failed on timeout instead).
		if slotErr := s.deps.Activity.AwaitActivitySlotBounded(ctx, activityID, "0"); slotErr != nil {
			return out, slotErr
		}
	}

	// The engine's per-container docker operations carry no timeouts, so cap
	// the whole run; the Track ctx stays unbounded for the deferred completion
	// so user cancellation is still detected there.
	runCtx, cancelRun := context.WithTimeout(ctx, timeouts.DefaultAutoUpdateApply)
	defer cancelRun()

	s.recordAutoUpdateEventInternal(ctx, event.EventSeverityInfo, database.JSON{
		"phase":       "start",
		"dryRun":      options.DryRun,
		"forceUpdate": options.ForceUpdate,
		"scopedType":  options.Type,
		"scopedCount": len(options.ResourceIds),
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
	s.appendAutoUpdateActivityMessageInternal(ctx, activityID, "Planning pending updates", "Planning updates", 5)

	if len(options.ResourceIds) > 0 {
		if applyErr := s.applyScopedUpdatesInternal(runCtx, options, out); applyErr != nil {
			return out, applyErr
		}
	} else {
		moduleResult, engineErr := s.engineInternal().ApplyPending(runCtx, moduleOptionsFromUpdaterOptionsInternal(options))
		if moduleResult != nil {
			out = resultFromModuleInternal(moduleResult)
			out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
			s.logResultItemsInternal(ctx, out)
		}
		if moduleResult == nil && engineErr == nil {
			engineErr = errors.New("updater returned no batch result")
		}
		if engineErr != nil {
			err = engineErr
			return out, err
		}
	}

	if !options.DryRun && s.deps.ImageUpdates != nil {
		s.appendAutoUpdateActivityMessageInternal(ctx, activityID, "Cleaning up update records", "Cleaning up", 95)
		if cleanupErr := s.deps.ImageUpdates.CleanupOrphanedRecords(runCtx); cleanupErr != nil {
			s.loggerInternal().WarnContext(ctx, "cleanup orphaned update records failed", "error", cleanupErr)
		}
	}

	s.recordAutoUpdateEventInternal(ctx, event.EventSeverityInfo, database.JSON{
		"phase":     "complete",
		"checked":   out.Checked,
		"updated":   out.Updated,
		"restarted": out.Restarted,
		"skipped":   out.Skipped,
		"failed":    out.Failed,
		"duration":  out.Duration,
		"time":      time.Now().UTC().Format(time.RFC3339),
	})
	batchCompleted = true
	return out, nil
}

// applyScopedUpdatesInternal runs a scoped update into the caller's result:
// resolves the requested resources to container IDs and updates each through
// the engine's single-container path.
func (s *UpdaterService) applyScopedUpdatesInternal(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result) error {
	containerIDs, err := s.resolveScopedContainerIDsInternal(ctx, options)
	if err != nil {
		return err
	}
	if len(containerIDs) == 0 {
		return common.ErrUpdaterNoContainersMatched
	}

	engineOpts := moduleOptionsFromUpdaterOptionsInternal(options)
	var engineErrs []error
	for _, containerID := range containerIDs {
		target := scheduler.TargetOutcome{ID: containerID, ResourceType: "container", Status: scheduler.Running}
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return progressErr
		}
		moduleResult, engineErr := s.engineInternal().UpdateContainer(ctx, containerID, engineOpts)
		target.Status = scheduler.NeedsAttention
		if moduleResult != nil {
			partial := resultFromModuleInternal(moduleResult)
			out.Checked += partial.Checked
			out.Updated += partial.Updated
			out.Restarted += partial.Restarted
			out.Skipped += partial.Skipped
			out.Failed += partial.Failed
			out.Items = append(out.Items, partial.Items...)
			target.Status = scheduler.Succeeded
			if partial.Failed > 0 {
				target.Status = scheduler.Failed
			} else if partial.Skipped > 0 {
				target.Status = scheduler.Skipped
			}
		}
		if engineErr != nil {
			out.Failed++
			out.Items = append(out.Items, arcaneupdater.ResourceResult{
				ResourceID:   containerID,
				ResourceType: "container",
				Status:       arcaneupdater.StatusFailed,
				Error:        engineErr.Error(),
			})
			engineErrs = append(engineErrs, fmt.Errorf("%s: %w", containerID, engineErr))
			target.Status = scheduler.Failed
			target.Message = engineErr.Error()
		}
		if progressErr2 := jobcontext.Progress(ctx, target); progressErr2 != nil {
			return errors.Join(progressErr2, errors.Join(engineErrs...))
		}
	}
	s.logResultItemsInternal(ctx, out)
	out.Success = out.Failed == 0
	// Engine errors propagate like the unscoped path's engine error does —
	// the remaining containers were still attempted and recorded above.
	return errors.Join(engineErrs...)
}

// resolveScopedContainerIDsInternal maps a scoped options payload to the
// container IDs it covers.
func (s *UpdaterService) resolveScopedContainerIDsInternal(ctx context.Context, options arcaneupdater.Options) ([]string, error) {
	requested := kit.TrimNonEmpty(options.ResourceIds)
	if len(requested) == 0 {
		return nil, nil
	}

	switch strings.ToLower(strings.TrimSpace(options.Type)) {
	case "", "container":
		return requested, nil
	case "project":
		return s.containerIDsForProjectsInternal(ctx, requested)
	case "image":
		return s.containerIDsForImagesInternal(ctx, requested)
	default:
		return nil, fmt.Errorf("unsupported scoped update type %q", options.Type)
	}
}

// containerIDsForProjectsInternal resolves project IDs or compose names to
// the IDs of the containers that belong to those projects.
func (s *UpdaterService) containerIDsForProjectsInternal(ctx context.Context, projectRefs []string) ([]string, error) {
	if s.deps.Docker == nil {
		return nil, errors.New("docker service unavailable")
	}

	names := make(map[string]struct{}, len(projectRefs))
	for _, ref := range projectRefs {
		name := strings.TrimSpace(ref)
		if discoveredName, discovered := strings.CutPrefix(name, "compose:"); discovered {
			name = discoveredName
		} else if s.deps.Projects != nil {
			if project, lookupErr := s.deps.Projects.GetProjectFromDatabaseByID(ctx, ref); lookupErr == nil && project != nil {
				switch {
				case project.ComposeProjectName != nil && strings.TrimSpace(*project.ComposeProjectName) != "":
					name = *project.ComposeProjectName
				case strings.TrimSpace(project.Name) != "":
					name = project.Name
				}
			}
		}
		names[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	containers, _, _, _, err := s.deps.Docker.GetAllContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var ids []string
	for _, summary := range containers {
		if docker.ComposeServiceLabel(summary.Labels) == "" {
			continue
		}
		project := strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(summary.Labels)))
		if project == "" {
			continue
		}
		if _, ok := names[project]; ok {
			ids = append(ids, summary.ID)
		}
	}
	return ids, nil
}

// containerIDsForImagesInternal resolves image IDs or references to the IDs
// of the containers currently running those images.
func (s *UpdaterService) containerIDsForImagesInternal(ctx context.Context, imageRefs []string) ([]string, error) {
	if s.deps.Docker == nil {
		return nil, errors.New("docker service unavailable")
	}

	wanted := make(map[string]struct{}, len(imageRefs)*2)
	for _, ref := range imageRefs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}
		wanted[trimmed] = struct{}{}
		if normalized := refs.NormalizeImageUpdateRef(trimmed); normalized != "" {
			wanted[normalized] = struct{}{}
		}
	}

	containers, _, _, _, err := s.deps.Docker.GetAllContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var ids []string
	for _, summary := range containers {
		if _, ok := wanted[summary.ImageID]; ok {
			ids = append(ids, summary.ID)
			continue
		}
		if normalized := refs.NormalizeImageUpdateRef(summary.Image); normalized != "" {
			if _, ok := wanted[normalized]; ok {
				ids = append(ids, summary.ID)
			}
		}
	}
	return ids, nil
}

// UpdateSingleContainer updates a single container by ID to the latest available image.
func (s *UpdaterService) UpdateSingleContainer(ctx context.Context, containerID string) (out *arcaneupdater.Result, err error) {
	localActivity, workCtx, err := s.startSingleContainerUpdateActivityInternal(ctx, containerID, true)
	if err != nil {
		return nil, err
	}
	activityID := ""
	if localActivity != nil {
		activityID = localActivity.ID
	}
	defer func() {
		s.finishSingleContainerUpdateInternal(workCtx, activityID, out, err)
	}()
	defer utils.RecoverToError(&err, "single container update")
	return s.runSingleContainerUpdateInternal(workCtx, containerID, activityID)
}

// AcceptSingleContainerUpdate persists and submits a cancellable update activity.
func (s *UpdaterService) AcceptSingleContainerUpdate(ctx context.Context, containerID string) (*activitytypes.Activity, error) {
	if s.deps.Activity == nil || !s.execution.Ready() {
		return nil, errors.New("asynchronous container updates unavailable")
	}
	localActivity, workCtx, err := s.startSingleContainerUpdateActivityInternal(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	if submitErr := s.execution.Submit(ctx, workCtx, containerID, localActivity.ID); submitErr != nil {
		return nil, submitErr
	}
	return localActivity, nil
}

func (s *UpdaterService) runSingleContainerUpdateInternal(ctx context.Context, containerID, activityID string) (out *arcaneupdater.Result, err error) {
	var release func()
	ctx, release, err = s.acquireUpdateInternal(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}, ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()}
	ctx = contextWithActivityIDInternal(ctx, activityID)
	if s.deps.Activity != nil && activityID != "" {
		if awaitActivitySlotBoundedErr := s.deps.Activity.AwaitActivitySlotBounded(ctx, activityID, "0"); awaitActivitySlotBoundedErr != nil {
			return out, awaitActivitySlotBoundedErr
		}
	}

	defer func() {
		if out == nil {
			out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
		}
		if out.Duration == "" {
			out.Duration = time.Since(start).String()
		}
		out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	}()

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if errErr := ctx.Err(); errErr != nil {
		return out, errErr
	}

	// The caller picked this container, so the autoUpdateExcludedContainers
	// setting does not apply: it only governs automatic and pending runs, which
	// keep skipping the container. Labels and immutable references still do.
	moduleResult, engineErr := s.engineInternal().UpdateContainer(ctx, containerID, updater.Options{IgnoreSettingsExclusions: true})
	if moduleResult != nil {
		out = resultFromModuleInternal(moduleResult)
		out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
		s.logResultItemsInternal(ctx, out)
	}
	if engineErr != nil {
		err = engineErr
		return out, err
	}
	return out, nil
}

// GetStatus returns the current in-memory update activity snapshot.
func (s *UpdaterService) GetStatus() arcaneupdater.Status {
	return statusFromModuleInternal(s.engineInternal().Status())
}

// GetHistory returns the most recent auto-update history records, newest first.
func (s *UpdaterService) GetHistory(ctx context.Context, limit int) ([]AutoUpdateRecord, error) {
	var records []AutoUpdateRecord
	query := s.deps.DB.WithContext(ctx).Order("start_time DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	return records, nil
}

// RestartContainersUsingOldIDs restarts containers matching old image IDs or refs.
func (s *UpdaterService) RestartContainersUsingOldIDs(ctx context.Context, oldIDToNewRef, oldRefToNewRef map[string]string) ([]arcaneupdater.ResourceResult, error) {
	results, err := s.engineInternal().RestartContainersUsingOldImages(ctx, oldIDToNewRef, oldRefToNewRef)
	return resourceResultsFromModuleInternal(results), err
}

// TriggerSelfUpdateViaCLI triggers Arcane's detached CLI self-update path.
func (s *UpdaterService) TriggerSelfUpdateViaCLI(ctx context.Context, source, containerID, containerName string, labelMap map[string]string) error {
	if !labels.IsArcaneContainer(labelMap) {
		return fmt.Errorf("%s: container is not an Arcane self-update target", source)
	}
	return s.TriggerSelfUpdate(ctx, updater.SelfUpdateTarget{
		ContainerID:   containerID,
		ContainerName: containerName,
		InstanceType:  kit.Ternary(labels.IsArcaneAgentContainer(labelMap), "agent", "server"),
		Labels:        labelMap,
	})
}

// BeginContainerUpdate marks a container as updating.
func (s *UpdaterService) BeginContainerUpdate(containerID string) func() {
	return s.engineInternal().BeginContainerUpdate(containerID)
}

// BeginProjectUpdate marks a project as updating.
func (s *UpdaterService) BeginProjectUpdate(projectID string) func() {
	return s.engineInternal().BeginProjectUpdate(projectID)
}

func (s *UpdaterService) recordAutoUpdateEventInternal(ctx context.Context, severity event.EventSeverity, metadata database.JSON) {
	if s.deps.Events == nil {
		return
	}
	phase, _ := metadata["phase"].(string)
	_, err := s.deps.Events.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeSystemAutoUpdate,
		Severity:      severity,
		Title:         autoUpdateEventTitleInternal(phase, metadata),
		ResourceType:  mo.EmptyableToOption(strings.TrimSpace("system")).ToPointer(),
		ResourceName:  mo.EmptyableToOption(strings.TrimSpace("auto_updater")).ToPointer(),
		EnvironmentID: mo.EmptyableToOption(strings.TrimSpace("0")).ToPointer(),
		Metadata:      metadata,
	})
	if err != nil {
		s.loggerInternal().DebugContext(ctx, "failed to record auto-update event", "error", err)
	}
}

func autoUpdateEventTitleInternal(phase string, metadata database.JSON) string {
	switch phase {
	case "start":
		return "Auto-update run started"
	case "image_pull", "image":
		localImage := cmp.Or(kit.ToString(metadata["imageNew"]), kit.ToString(metadata["imageOld"]))
		return kit.Ternary(localImage != "", "Auto-update: image pull "+localImage, "Auto-update: image pull")
	case "image_prune":
		imageID := kit.ToString(metadata["imageId"])
		return kit.Ternary(imageID != "", "Auto-update: image prune "+imageID, "Auto-update: image prune")
	case "container":
		name := cmp.Or(kit.ToString(metadata["resourceName"]), kit.ToString(metadata["container"]), kit.ToString(metadata["containerId"]))
		return kit.Ternary(name != "", "Auto-update: container "+name, "Auto-update: container")
	case "project":
		name := cmp.Or(kit.ToString(metadata["projectName"]), kit.ToString(metadata["projectId"]))
		return kit.Ternary(name != "", "Auto-update: project "+name, "Auto-update: project")
	case "complete":
		return "Auto-update run completed"
	default:
		return kit.Ternary(phase != "", "Auto-update: "+phase, "Auto-update")
	}
}

// DockerClient returns Arcane's configured Docker client for the updater engine.
func (s *UpdaterService) DockerClient(ctx context.Context) (*client.Client, error) {
	if s == nil || s.deps.Docker == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("docker service unavailable"))
	}
	return s.deps.Docker.GetClient(ctx)
}

// PullImage pulls an image through Arcane's image service. The pull is
// bounded by the dockerImagePullTimeout setting — image.ImageService.PullImage does
// not bound itself, and an unbounded engine pull would hold the auto-update
// run (and its activity slot) indefinitely.
func (s *UpdaterService) PullImage(ctx context.Context, imageRef string, progress io.Writer) error {
	if s == nil || s.deps.ImagePuller == nil {
		return common.Classify(common.ErrUnavailable, errors.New("image service unavailable"))
	}
	pulledRef, err := s.recovery.PreparePull(ctx, imageRef)
	if err != nil {
		return err
	}
	activityID := activityIDFromContextInternal(ctx)
	writer := activitylib.NewWriter(ctx, s.deps.Activity, activityID, progress, "Pulling updated images")
	defer activitylib.FlushWriter(writer)

	pullTimeoutSeconds := 0
	if s.deps.Settings != nil {
		pullTimeoutSeconds = s.deps.Settings.GetSettingsConfig().DockerImagePullTimeout.AsInt()
	}
	pullCtx, cancelPull := context.WithTimeout(ctx, timeouts.GetDuration(pullTimeoutSeconds, timeouts.DefaultDockerImagePull))
	defer cancelPull()

	if s.deps.Projects != nil {
		resolved, resolveRegistryCredentialsErr := s.deps.Projects.ResolveRegistryCredentials(pullCtx)
		if resolveRegistryCredentialsErr != nil {
			return fmt.Errorf("resolve registry credentials: %w", resolveRegistryCredentialsErr)
		}
		if pullImageErr := s.deps.ImagePuller.PullImage(pullCtx, pulledRef, writer, s.deps.SystemUser, resolved); pullImageErr != nil {
			return pullImageErr
		}
		return s.tagFrozenPullInternal(pullCtx, pulledRef, imageRef)
	}

	if pullImageErr2 := s.deps.ImagePuller.PullImage(pullCtx, pulledRef, writer, s.deps.SystemUser, nil); pullImageErr2 != nil {
		return pullImageErr2
	}
	return s.tagFrozenPullInternal(pullCtx, pulledRef, imageRef)
}

// PendingImageUpdates returns pending image update records from Arcane's database.
func (s *UpdaterService) PendingImageUpdates(ctx context.Context) ([]updater.ImageUpdateRecord, error) {
	if records, ok := s.recovery.FrozenRecords(ctx); ok {
		return records, nil
	}
	if s == nil || s.deps.DB == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("database unavailable"))
	}

	var records []imageupdate.ImageUpdateRecord
	if err := s.deps.DB.WithContext(ctx).Where("has_update = ? AND project_id = ?", true, "").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query pending image updates: %w", err)
	}

	// Flush pending "Updates Available" notifications before the engine
	// consumes and clears these records, otherwise the notification for an
	// update applied here is silently lost (#3132). A flush that cannot
	// determine what to send aborts the run so the records survive for retry.
	if s.deps.ImageUpdates != nil {
		if err := s.deps.ImageUpdates.SendBatchUpdateNotifications(ctx); err != nil {
			return nil, err
		}
	}
	s.appendAutoUpdateActivityMessageInternal(
		ctx,
		activityIDFromContextInternal(ctx),
		fmt.Sprintf("Found %d pending image update records", len(records)),
		"Planning updates",
		10,
	)

	return s.scopedPendingRecordsInternal(ctx, records)
}

// ClearImageUpdateRecord clears a pending image update record after it is handled.
func (s *UpdaterService) ClearImageUpdateRecord(ctx context.Context, record updater.ImageUpdateRecord) error {
	if s == nil {
		return common.Classify(common.ErrUnavailable, errors.New("updater service unavailable"))
	}
	return s.clearImageUpdateRecordForModuleInternal(ctx, record)
}

// ExcludedContainers returns auto-update exclusions from Arcane settings.
func (s *UpdaterService) ExcludedContainers(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	if s.deps.Settings == nil {
		return nil, nil
	}
	return kit.Unique(kit.TrimNonEmpty(strings.Split(s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""), ","))), nil
}

// ProjectByComposeName resolves an Arcane project from a Docker Compose project name.
func (s *UpdaterService) ProjectByComposeName(ctx context.Context, composeName string) (updater.ComposeProject, error) {
	if s == nil || s.deps.Projects == nil {
		return updater.ComposeProject{}, common.Classify(common.ErrUnavailable, errors.New("project service unavailable"))
	}
	project, err := s.deps.Projects.GetProjectByComposeName(ctx, composeName)
	if err != nil {
		return updater.ComposeProject{}, err
	}
	if project == nil {
		return updater.ComposeProject{}, fmt.Errorf("compose project not found: %s", composeName)
	}
	return updater.ComposeProject{ID: project.ID, Name: project.Name}, nil
}

// UpdateServices redeploys selected services through Arcane's project service.
func (s *UpdaterService) UpdateServices(ctx context.Context, projectID string, services []string) error {
	if s == nil || s.deps.Projects == nil {
		return common.Classify(common.ErrUnavailable, errors.New("project service unavailable"))
	}
	return s.deps.Projects.UpdateProjectServices(ctx, projectID, services, s.deps.SystemUser, false)
}

// TriggerSelfUpdate runs Arcane's CLI-backed self-update hook.
func (s *UpdaterService) TriggerSelfUpdate(ctx context.Context, target updater.SelfUpdateTarget) error {
	if target.NewImageRef != "" {
		pinned, err := s.recovery.PreparePull(ctx, target.NewImageRef)
		if err != nil {
			return err
		}
		target.NewImageRef = pinned
	}
	if s == nil || s.deps.SelfUpgrade == nil {
		instanceType := cmp.Or(strings.TrimSpace(target.InstanceType), "server")
		return fmt.Errorf("%s self-update requires CLI upgrade service", instanceType)
	}

	// A server self-update stops this process before the run can complete its
	// activity, so annotate the activity first; startup reconciliation uses
	// the metadata flag to finalize it after the restart.
	if target.InstanceType != "agent" {
		s.markSelfUpdateTriggeredInternal(ctx, target)
	}

	if _, err := s.deps.SelfUpgrade.TriggerUpgradeViaCLI(ctx, s.deps.SystemUser, target); err != nil {
		return fmt.Errorf("CLI upgrade failed: %w", err)
	}
	if progress, ok := ctx.Value(updateProgressKeyInternal{}).(*updateProgressInternal); ok {
		progress.mu.Lock()
		progress.selfTriggered = true
		progress.selfID = target.ContainerID
		progress.mu.Unlock()
	}
	return nil
}

func (s *UpdaterService) markSelfUpdateTriggeredInternal(ctx context.Context, target updater.SelfUpdateTarget) {
	activityID := activityIDFromContextInternal(ctx)
	if s.deps.Activity == nil || activityID == "" {
		return
	}
	message := "Self-update initiated — Arcane will restart"
	if ref := strings.TrimSpace(target.NewImageRef); ref != "" {
		message = "Self-update initiated — Arcane will restart with " + ref
	}
	s.appendAutoUpdateActivityMessageInternal(ctx, activityID, message, "Self-update", 90)
	if err := s.deps.Activity.PatchActivityMetadata(ctx, activityID, database.JSON{"selfUpdateTriggered": true}); err != nil {
		slog.DebugContext(ctx, "failed to mark self-update on activity", "activityId", activityID, "error", err)
	}
}

// Notify buffers Arcane's container update notification when called within an
// auto-update run (see withBatchedNotificationsInternal); buffered entries are
// flushed as one batched notification when the run completes. Outside a run it
// sends the legacy per-container notification immediately.
func (s *UpdaterService) Notify(ctx context.Context, localNotification updater.Notification) error {
	if s == nil || s.deps.Notifications == nil {
		return nil
	}
	if buffer := batchedContainerUpdatesFromContextInternal(ctx); buffer != nil {
		buffer.Lock()
		buffer.entries = append(buffer.entries, notifications.ContainerUpdateBatchEntry{
			ContainerName: localNotification.ContainerName,
			ImageRef:      localNotification.ImageRef,
			OldDigest:     localNotification.OldImage,
			NewDigest:     localNotification.NewImage,
		})
		buffer.Unlock()
		return nil
	}
	return s.deps.Notifications.SendContainerUpdateNotification(
		ctx,
		localNotification.ContainerName,
		localNotification.ImageRef,
		localNotification.OldImage,
		localNotification.NewImage,
	)
}

// containerUpdateBatchInternal accumulates per-container update notifications
// so a single auto-update run produces one batched notification.
type containerUpdateBatchInternal struct {
	sync.Mutex

	entries []notifications.ContainerUpdateBatchEntry
}

type containerUpdateBatchContextKeyInternal struct{}

func batchedContainerUpdatesFromContextInternal(ctx context.Context) *containerUpdateBatchInternal {
	batch, _ := ctx.Value(containerUpdateBatchContextKeyInternal{}).(*containerUpdateBatchInternal)
	return batch
}

// flushBatchedContainerUpdatesInternal delivers the accumulated container
// update notifications as one batched notification.
func (s *UpdaterService) flushBatchedContainerUpdatesInternal(ctx context.Context, batch *containerUpdateBatchInternal) {
	if s == nil || s.deps.Notifications == nil || batch == nil {
		return
	}
	batch.Lock()
	entries := batch.entries
	batch.entries = nil
	batch.Unlock()
	if len(entries) == 0 {
		return
	}
	if err := s.deps.Notifications.SendBatchContainerUpdateNotification(ctx, entries); err != nil {
		s.loggerInternal().ErrorContext(ctx, "failed to send batched container update notification", "error", err, "count", len(entries))
	}
}

// RecordEvent records updater lifecycle events in Arcane's event stream.
func (s *UpdaterService) RecordEvent(ctx context.Context, evt updater.Event) error {
	if s == nil {
		return nil
	}

	eventType, ok := containerEventTypeInternal(evt.Phase).Get()
	if ok {
		if s.deps.Events == nil {
			return nil
		}
		return s.deps.Events.LogContainerEvent(
			ctx,
			eventType,
			evt.ResourceID,
			evt.ResourceName,
			s.deps.SystemUser.ID,
			s.deps.SystemUser.Username,
			"0",
			evt.Metadata,
		)
	}

	severity := kit.Ternary(strings.EqualFold(evt.Severity, "error"), event.EventSeverityError, event.EventSeverityInfo)
	s.recordAutoUpdateEventInternal(ctx, severity, database.JSON{
		"phase":        evt.Phase,
		"resourceId":   evt.ResourceID,
		"resourceName": evt.ResourceName,
		"resourceType": evt.ResourceType,
		"time":         time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func containerEventTypeInternal(phase string) mo.Option[event.EventType] {
	switch phase {
	case "container_stop":
		return mo.Some(event.EventTypeContainerStop)
	case "container_delete":
		return mo.Some(event.EventTypeContainerDelete)
	case "container_create":
		return mo.Some(event.EventTypeContainerCreate)
	case "container_start":
		return mo.Some(event.EventTypeContainerStart)
	case "container_update":
		return mo.Some(event.EventTypeContainerUpdate)
	default:
		return mo.None[event.EventType]()
	}
}

type activityIDContextKeyInternal struct{}

func contextWithActivityIDInternal(ctx context.Context, activityID string) context.Context {
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return ctx
	}
	return context.WithValue(ctx, activityIDContextKeyInternal{}, activityID)
}

func activityIDFromContextInternal(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	activityID, _ := ctx.Value(activityIDContextKeyInternal{}).(string)
	return strings.TrimSpace(activityID)
}

func (s *UpdaterService) startAutoUpdateActivityInternal(ctx context.Context, dryRun bool) string {
	if s.deps.Activity == nil {
		return ""
	}
	localActivity, err := s.deps.Activity.StartActivity(ctx, activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          activitytypes.TypeAutoUpdate,
		Queue:         true,
		ResourceType:  mo.EmptyableToOption(strings.TrimSpace("system")).ToPointer(),
		ResourceName:  mo.EmptyableToOption(strings.TrimSpace("Auto update")).ToPointer(),
		Step:          "Planning updates",
		LatestMessage: "Auto-update run started",
		Metadata:      database.JSON{"dryRun": dryRun},
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start auto-update activity", "error", err)
		return ""
	}
	return localActivity.ID
}

func (s *UpdaterService) startSingleContainerUpdateActivityInternal(ctx context.Context, containerID string, tracked bool) (*activitytypes.Activity, context.Context, error) {
	if s.deps.Activity == nil {
		return nil, ctx, nil
	}
	name := containerID
	lookupCtx, cancelLookup := context.WithTimeout(ctx, 2*time.Second)
	if dockerClient, dockerErr := s.DockerClient(lookupCtx); dockerErr == nil && dockerClient != nil {
		if inspected, inspectErr := compat.ContainerInspectWithCompatibility(lookupCtx, dockerClient, containerID, client.ContainerInspectOptions{}); inspectErr == nil {
			if actualName := strings.TrimPrefix(strings.TrimSpace(inspected.Container.Name), "/"); actualName != "" {
				name = actualName
			}
		}
	}
	cancelLookup()
	user, _ := userctx.CurrentUserFromContext(ctx)
	if initiator, ok := utils.UpdateInitiatorFromContext(ctx); ok {
		user = &usertypes.Actor{Username: initiator.Username}
		user.ID = initiator.UserID
		if initiator.DisplayName != "" {
			user.DisplayName = &initiator.DisplayName
		}
	}
	request := activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          activitytypes.TypeAutoUpdate,
		Queue:         true,
		DeferSlot:     true,
		ResourceType:  mo.EmptyableToOption(strings.TrimSpace("container")).ToPointer(),
		ResourceID:    &containerID,
		ResourceName:  &name,
		StartedBy:     user,
		Step:          "Updating container",
		LatestMessage: "Container update started",
		Metadata:      database.JSON{"containerID": containerID},
	}
	var item *activitytypes.Activity
	workCtx := ctx
	var err error
	if tracked {
		item, workCtx, err = s.deps.Activity.StartTrackedActivity(ctx, request)
	} else {
		item, err = s.deps.Activity.StartActivity(ctx, request)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("start container update activity: %w", err)
	}
	return item, workCtx, nil
}

func (s *UpdaterService) finishSingleContainerUpdateInternal(ctx context.Context, activityID string, result *arcaneupdater.Result, runErr error) {
	if s.deps.Activity == nil || activityID == "" {
		return
	}
	metadata, message := singleContainerActivitySummaryInternal(result)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if containerName, ok := metadata["containerName"].(string); ok && containerName != "" {
		if _, err := s.deps.Activity.UpdateActivity(writeCtx, activityID, activitylib.UpdateRequest{ResourceName: &containerName}); err != nil {
			slog.WarnContext(writeCtx, "failed to update container activity name", "activityId", activityID, "error", err)
		}
	}
	if err := s.deps.Activity.PatchActivityMetadata(writeCtx, activityID, metadata); err != nil {
		slog.WarnContext(writeCtx, "failed to persist container update result", "activityId", activityID, "error", err)
	}
	if runErr == nil && result != nil && result.Failed == 0 {
		if result.Updated > 0 || result.Restarted > 0 {
			message = "Container updated"
		}
		if _, err := s.deps.Activity.CompleteActivity(writeCtx, activityID, activitytypes.StatusSuccess, message, nil); err != nil {
			slog.ErrorContext(writeCtx, "failed to complete container update activity", "activityId", activityID, "error", err)
		}
		return
	}
	if runErr == nil && result == nil {
		runErr = errors.New("container update produced no result")
	}
	if runErr == nil && result != nil && len(result.Items) > 0 && result.Items[0].Error != "" {
		runErr = errors.New(result.Items[0].Error)
	}
	s.completeAutoUpdateActivityInternal(ctx, activityID, result, runErr)
}

func singleContainerActivitySummaryInternal(result *arcaneupdater.Result) (database.JSON, string) {
	metadata := database.JSON{}
	message := "Container update completed"
	if result == nil {
		return metadata, message
	}
	metadata["updated"] = result.Updated
	metadata["restarted"] = result.Restarted
	metadata["skipped"] = result.Skipped
	metadata["failed"] = result.Failed
	if len(result.Items) > 0 {
		item := result.Items[0]
		metadata["updateOutcome"] = item.Status
		if item.ResourceName != "" {
			metadata["containerName"] = item.ResourceName
		}
		if item.Error != "" {
			metadata["updateReason"] = item.Error
			if item.Status == string(updater.StatusSkipped) {
				return metadata, "Container update skipped: " + item.Error
			}
		}
	}
	if result.Skipped > 0 {
		return metadata, "Container update skipped"
	}
	if result.Updated == 0 && result.Restarted == 0 && result.Failed == 0 {
		message = "Container already current"
	}
	return metadata, message
}

func (s *UpdaterService) appendAutoUpdateActivityMessageInternal(ctx context.Context, activityID, message, step string, progress int) {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return
	}
	if strings.TrimSpace(step) == "" {
		step = message
	}
	if _, err := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{
		Level:    activitytypes.MessageLevelInfo,
		Message:  message,
		Progress: &progress,
		Step:     step,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append auto-update activity message", "activityId", activityID, "error", err)
	}
}

func (s *UpdaterService) completeAutoUpdateActivityInternal(ctx context.Context, activityID string, result *arcaneupdater.Result, applyErr error) {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	status := activitytypes.StatusSuccess
	message := "Auto-update run completed"
	var errMessage *string
	if applyErr != nil {
		status = activitytypes.StatusFailed
		errText := applyErr.Error()
		errMessage = &errText
		message = errText
	} else if result != nil && result.Failed > 0 {
		status = activitytypes.StatusFailed
		errText := fmt.Sprintf("%d update action(s) failed", result.Failed)
		errMessage = &errText
		message = errText
	}
	if status == activitytypes.StatusFailed && activitylib.CancelledByContext(ctx) {
		status = activitytypes.StatusCancelled
		message = "Auto-update cancelled"
		errMessage = nil
	}

	if _, err := s.deps.Activity.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errMessage); err != nil {
		// A lost terminal write strands the activity in running forever, so it
		// must be loud enough to correlate with a stuck activity panel entry.
		slog.ErrorContext(ctx, "failed to complete auto-update activity", "activityId", activityID, "error", err)
	}
}

func (s *UpdaterService) trackActivityInternal(ctx context.Context, activityID string) context.Context {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return ctx
	}
	return s.deps.Activity.Track(ctx, activityID)
}

func imageUpdateRecordToModuleInternal(record imageupdate.ImageUpdateRecord) updater.ImageUpdateRecord {
	return updater.ImageUpdateRecord{
		ID:             record.ID,
		ContainerID:    record.ContainerID,
		Repository:     record.Repository,
		Tag:            record.Tag,
		HasUpdate:      record.HasUpdate,
		UpdateType:     updater.UpdateType(record.UpdateType),
		CurrentVersion: record.CurrentVersion,
		LatestVersion:  record.LatestVersion,
		CurrentDigest:  record.CurrentDigest,
		LatestDigest:   record.LatestDigest,
		CheckTime:      record.CheckTime,
		LastError:      record.LastError,
	}
}

// moduleOptionsFromUpdaterOptionsInternal narrows Arcane's request options to
// what the engine acts on. Options.Type and Options.ResourceIds stay behind:
// the engine never read them, and ApplyPending already routes a scoped request
// through applyScopedUpdatesInternal before reaching the engine. Settings
// exclusions always apply to these runs; only UpdateSingleContainer overrides
// them for its explicitly requested container.
func moduleOptionsFromUpdaterOptionsInternal(options arcaneupdater.Options) updater.Options {
	return updater.Options{
		Force:  options.ForceUpdate,
		DryRun: options.DryRun,
	}
}

// resultFromModuleInternal converts an engine result to Arcane's wire type. The
// engine reports times as time.Time and a Duration method; Arcane's API has
// always carried them as strings, so they are formatted here. ActivityID is not
// an engine concept; every caller sets it on the returned value.
func resultFromModuleInternal(result *updater.Result) *arcaneupdater.Result {
	if result == nil {
		return &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
	}
	return &arcaneupdater.Result{
		Success:   result.Success,
		Checked:   result.Checked,
		Updated:   result.Updated,
		Restarted: result.Restarted,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
		StartTime: formatModuleTimeInternal(result.StartTime),
		EndTime:   formatModuleTimeInternal(result.EndTime),
		Duration:  result.Duration().String(),
		Items:     resourceResultsFromModuleInternal(result.Items),
	}
}

func formatModuleTimeInternal(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func resourceResultsFromModuleInternal(results []updater.ResourceResult) []arcaneupdater.ResourceResult {
	out := make([]arcaneupdater.ResourceResult, 0, len(results))
	for _, result := range results {
		out = append(out, resourceResultFromModuleInternal(result))
	}
	return out
}

// resourceResultFromModuleInternal converts one engine result to Arcane's wire
// type. The engine now reports a single old/new image; Arcane's API carries
// maps, which only ever held the "main" entry, so that shape is rebuilt here.
func resourceResultFromModuleInternal(result updater.ResourceResult) arcaneupdater.ResourceResult {
	mainImage := func(ref string) map[string]string {
		if ref == "" {
			return nil
		}
		return map[string]string{"main": ref}
	}
	return arcaneupdater.ResourceResult{
		ResourceID:      result.ResourceID,
		ResourceName:    result.ResourceName,
		ResourceType:    string(result.ResourceType),
		Status:          string(result.Status),
		UpdateAvailable: result.UpdateAvailable,
		UpdateApplied:   result.UpdateApplied,
		OldImages:       mainImage(result.OldImage),
		NewImages:       mainImage(result.NewImage),
		Error:           result.Error,
		Details:         result.Details,
	}
}

func statusFromModuleInternal(status updater.Status) arcaneupdater.Status {
	return arcaneupdater.Status{
		UpdatingContainers: status.UpdatingContainers,
		UpdatingProjects:   status.UpdatingProjects,
		ContainerIds:       status.ContainerIDs,
		ProjectIds:         status.ProjectIDs,
	}
}

func (s *UpdaterService) recordRunInternal(ctx context.Context, item arcaneupdater.ResourceResult) error {
	now := time.Now()
	record := &AutoUpdateRecord{
		ResourceID:       item.ResourceID,
		ResourceType:     item.ResourceType,
		ResourceName:     item.ResourceName,
		Status:           AutoUpdateStatus(item.Status),
		StartTime:        now,
		EndTime:          &now,
		UpdateAvailable:  item.UpdateAvailable || item.Status == string(updater.StatusUpdated) || item.Status == string(updater.StatusUpdateAvailable),
		UpdateApplied:    item.UpdateApplied,
		OldImageVersions: mapToJSONInternal(item.OldImages),
		NewImageVersions: mapToJSONInternal(item.NewImages),
		Details:          detailsToJSONInternal(item.Details),
	}
	if item.Error != "" {
		record.Error = &item.Error
	}
	return s.deps.DB.WithContext(ctx).Create(record).Error
}

func (s *UpdaterService) clearImageUpdateRecordForModuleInternal(ctx context.Context, record updater.ImageUpdateRecord) error {
	if s.deps.DB == nil {
		return nil
	}

	if record.ContainerID != "" && !strings.HasPrefix(record.ID, "container::") {
		return s.clearUnscopedRecordInternal(ctx, record)
	}
	query := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{})
	if record.ContainerID != "" {
		query = query.Where("container_id = ?", record.ContainerID)
	} else {
		query = query.Where("container_id = ?", "")
	}
	if strings.TrimSpace(record.ID) != "" {
		return query.Where("id = ?", record.ID).Update("has_update", false).Error
	}
	return query.Where("repository = ? AND tag = ?", record.Repository, record.Tag).Update("has_update", false).Error
}

func mapToJSONInternal(values map[string]string) database.JSON {
	if len(values) == 0 {
		return nil
	}
	out := make(database.JSON, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func detailsToJSONInternal(values map[string]any) database.JSON {
	if len(values) == 0 {
		return nil
	}
	out := make(database.JSON, len(values))
	maps.Copy(out, values)
	return out
}

func (s *UpdaterService) logResultItemsInternal(ctx context.Context, result *arcaneupdater.Result) {
	if result == nil {
		return
	}
	for _, item := range result.Items {
		severity := event.EventSeverityInfo
		switch item.Status {
		case string(updater.StatusFailed):
			severity = event.EventSeverityError
		case string(updater.StatusUpdated):
			severity = event.EventSeveritySuccess
		}
		s.recordAutoUpdateEventInternal(ctx, severity, database.JSON{
			"phase":        item.ResourceType,
			"resourceId":   item.ResourceID,
			"resourceName": item.ResourceName,
			"status":       item.Status,
			"error":        item.Error,
			"oldImages":    item.OldImages,
			"newImages":    item.NewImages,
		})
	}
}

// CollectUsedImages returns normalized image references used by running Arcane resources.
func (s *UpdaterService) CollectUsedImages(ctx context.Context) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	var errs []error
	successfulSources := 0

	if s.deps.Docker == nil {
		errs = append(errs, common.Classify(common.ErrUnavailable, errors.New("docker service unavailable")))
	} else {
		dcli, err := s.deps.Docker.GetClient(ctx)
		if err != nil || dcli == nil {
			if err == nil {
				err = common.Classify(common.ErrUnavailable, errors.New("docker client unavailable"))
			}
			errs = append(errs, err)
			s.loggerInternal().DebugContext(ctx, "collectUsedImages: docker connection unavailable", "error", err)
		} else if collectUsedImagesFromContainersErr := s.collectUsedImagesFromContainersInternal(ctx, dcli, out); collectUsedImagesFromContainersErr != nil {
			errs = append(errs, collectUsedImagesFromContainersErr)
			s.loggerInternal().DebugContext(ctx, "collectUsedImages: failed collecting from containers", "error", collectUsedImagesFromContainersErr)
		} else {
			successfulSources++
		}
	}

	if s.deps.Projects != nil {
		if err := s.collectUsedImagesFromProjectsInternal(ctx, out); err != nil {
			errs = append(errs, err)
			s.loggerInternal().DebugContext(ctx, "collectUsedImages: failed collecting from projects", "error", err)
		} else {
			successfulSources++
		}
	}

	if successfulSources == 0 {
		return nil, errors.Join(errs...)
	}

	s.loggerInternal().DebugContext(ctx, "collectUsedImages: collected used images", "count", len(out))
	return out, nil
}

func (s *UpdaterService) collectUsedImagesFromContainersInternal(ctx context.Context, dcli *client.Client, out map[string]struct{}) error {
	if dcli == nil {
		return nil
	}

	excludedContainers := s.buildExcludedContainerSetInternal(ctx)
	listResult, err := dcli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return err
	}

	for _, summary := range listResult.Items {
		if labels.IsUpdateDisabled(summary.Labels) {
			s.loggerInternal().DebugContext(ctx, "collectUsedImagesFromContainers: container opted out by labels", "containerId", summary.ID)
			continue
		}

		if docker.ContainerNameExcluded(summary.Names, excludedContainers) {
			s.loggerInternal().DebugContext(ctx, "collectUsedImagesFromContainers: skipping excluded container", "containerId", summary.ID, "names", summary.Names)
			continue
		}

		imageRef := strings.TrimSpace(summary.Image)
		if imageRef != "" && !refs.IsImageIDLikeReference(imageRef) {
			addNormalizedImageUpdateRefInternal(ctx, out, imageRef, "collectUsedImagesFromContainers", "containerId", summary.ID)
			continue
		}

		inspectResult, inspectErr := compat.ContainerInspectWithCompatibility(ctx, dcli, summary.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			s.loggerInternal().DebugContext(ctx, "collectUsedImagesFromContainers: container inspect failed", "containerId", summary.ID, "error", inspectErr)
			continue
		}
		inspect := inspectResult.Container
		if inspect.Config != nil && labels.IsUpdateDisabled(inspect.Config.Labels) {
			s.loggerInternal().DebugContext(ctx, "collectUsedImagesFromContainers: container inspect labels opted out", "containerId", summary.ID)
			continue
		}
		for _, tag := range s.normalizedTagsForContainerInternal(ctx, dcli, inspect) {
			out[tag] = struct{}{}
		}
	}
	return nil
}

func (s *UpdaterService) collectUsedImagesFromComposeContainersInternal(ctx context.Context, composeContainers []container.Summary, activeProjectNames, out map[string]struct{}) {
	for _, summary := range composeContainers {
		projectName := docker.ComposeProjectLabel(summary.Labels)
		if projectName == "" {
			continue
		}
		if _, isActive := activeProjectNames[projectName]; !isActive {
			continue
		}
		if labels.IsUpdateDisabled(summary.Labels) {
			continue
		}

		imageRef := strings.TrimSpace(summary.Image)
		if imageRef == "" || refs.IsImageIDLikeReference(imageRef) {
			continue
		}
		addNormalizedImageUpdateRefInternal(ctx, out, imageRef, "collectUsedImagesFromComposeContainers", "containerId", summary.ID)
	}
}

func (s *UpdaterService) normalizedTagsForContainerInternal(ctx context.Context, dcli *client.Client, inspect container.InspectResponse) []string {
	seen := map[string]struct{}{}

	if dcli != nil {
		if imageInspect, err := dcli.ImageInspect(ctx, inspect.Image); err == nil {
			for _, tag := range imageInspect.RepoTags {
				if strings.TrimSpace(tag) == "" || tag == "<none>:<none>" {
					continue
				}
				addNormalizedImageUpdateRefInternal(ctx, seen, tag, "normalizedTagsForContainer repo tag", "imageId", inspect.Image)
			}
		}
	}

	if inspect.Config != nil && inspect.Config.Image != "" {
		addNormalizedImageUpdateRefInternal(ctx, seen, inspect.Config.Image, "normalizedTagsForContainer config image", "imageId", inspect.Image)
	}

	out := slices.Collect(maps.Keys(seen))
	return out
}

func (s *UpdaterService) buildExcludedContainerSetInternal(ctx context.Context) map[string]bool {
	if s.deps.Settings == nil {
		return nil
	}
	return docker.ExcludedContainerNameSet(s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))
}

func (s *UpdaterService) collectUsedImagesFromProjectsInternal(ctx context.Context, out map[string]struct{}) error {
	if s.deps.Projects == nil {
		return nil
	}

	allProjects, err := s.deps.Projects.ListAllProjects(ctx)
	if err != nil {
		return err
	}

	activeProjectNames := activeComposeProjectNameSetInternal(allProjects)
	if len(activeProjectNames) == 0 {
		return nil
	}

	var dockerClient client.APIClient
	if s.deps.Docker != nil {
		cli, cliErr := s.deps.Docker.GetClient(ctx)
		if cliErr != nil {
			return cliErr
		}
		dockerClient = cli
	}
	composeContainers, err := projects.ListGlobalComposeContainers(ctx, dockerClient, s.deps.Docker.DockerHost())
	if err != nil {
		return err
	}

	s.collectUsedImagesFromComposeContainersInternal(ctx, composeContainers, activeProjectNames, out)
	return nil
}

func activeComposeProjectNameSetInternal(items []projectpkg.Project) map[string]struct{} {
	active := make(map[string]struct{})
	for _, project := range items {
		if project.IsArchived {
			continue
		}
		if project.Status != projectpkg.ProjectStatusRunning && project.Status != projectpkg.ProjectStatusPartiallyRunning {
			continue
		}

		name := strings.TrimSpace(project.Name)
		if name == "" {
			continue
		}
		active[name] = struct{}{}
		if normalized := projects.NormalizeProjectName(name); normalized != "" {
			active[normalized] = struct{}{}
		}
	}
	return active
}

func addNormalizedImageUpdateRefInternal(ctx context.Context, out map[string]struct{}, imageRef, source string, attrs ...any) {
	normalizedRef := refs.NormalizeImageUpdateRef(imageRef)
	if normalizedRef != "" {
		out[normalizedRef] = struct{}{}
		return
	}

	args := slices.Clone(attrs)
	args = append(args, "source", source, "imageRef", imageRef)
	slog.DebugContext(ctx, "skipping invalid image reference", args...)
}

func (s *UpdaterService) tagFrozenPullInternal(ctx context.Context, pulledRef, imageRef string) error {
	if pulledRef == imageRef {
		return nil
	}
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return err
	}
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: pulledRef, Target: imageRef})
	return err
}

type updateAdmissionKeyInternal struct{}

var errUpdateBusyInternal = errors.New("another container update is running")

func (s *UpdaterService) acquireUpdateInternal(ctx context.Context) (context.Context, func(), error) {
	if s.admission == nil || ctx.Value(updateAdmissionKeyInternal{}) == s {
		return ctx, func() {}, nil
	}
	lease, admitted, err := s.admission.TryAcquire(ctx, scheduler.AdmissionKey{Scope: "updater"})
	if err != nil {
		return ctx, nil, err
	}
	if !admitted {
		return ctx, nil, errUpdateBusyInternal
	}
	return context.WithValue(ctx, updateAdmissionKeyInternal{}, s), func() { lease.Release(ctx) }, nil
}

type (
	updateProgressKeyInternal struct{}
	updateProgressInternal    struct {
		mu            sync.Mutex
		err           error
		selfTriggered bool
		selfID        string
	}
)

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result updater.ResourceResult) error {
	var recordErr error
	if s != nil && s.deps.DB != nil {
		recordErr = s.recordRunInternal(ctx, resourceResultFromModuleInternal(result))
	}
	name := cmp.Or(strings.TrimSpace(result.ResourceName), result.ResourceID)
	message := name + ": " + string(result.Status)
	level := activitytypes.MessageLevelInfo
	status := scheduler.NeedsAttention
	switch result.Status {
	case updater.StatusUpdated, updater.StatusRestarted, updater.StatusUpToDate:
		status = scheduler.Succeeded
		if result.Status != updater.StatusUpToDate && result.OldImage != "" && result.NewImage != "" && result.OldImage != result.NewImage {
			message += " (" + result.OldImage + " -> " + result.NewImage + ")"
		}
	case updater.StatusSkipped:
		status = scheduler.Skipped
	case updater.StatusChecked, updater.StatusUpdateAvailable:
		status = scheduler.NeedsAttention
	case updater.StatusFailed:
		status = scheduler.Failed
		level = activitytypes.MessageLevelError
	}
	if reason := strings.TrimSpace(result.Error); reason != "" {
		if level == activitytypes.MessageLevelError {
			message = name + ": " + reason
		} else {
			message += " (" + reason + ")"
		}
	}
	if activityID := activityIDFromContextInternal(ctx); activityID != "" && s != nil && s.deps.Activity != nil {
		if _, appendErr := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{Level: level, Message: message, Step: "Applying updates"}); appendErr != nil {
			slog.DebugContext(ctx, "failed to append update result activity message", "activityId", activityID, "resource", name, "error", appendErr)
		}
	}
	var evidenceErr error
	if status == scheduler.Succeeded {
		evidenceErr = s.recovery.VerifyResult(ctx, result.ResourceID)
	}
	if evidenceErr != nil {
		status = scheduler.NeedsAttention
	}
	progressMessage := result.Error
	if evidenceErr != nil {
		progressMessage = evidenceErr.Error()
	}
	progressErr := jobcontext.Progress(
		ctx,
		scheduler.TargetOutcome{
			ID: result.ResourceID,
			ResourceType: string(
				result.ResourceType,
			),
			Status:  status,
			Message: progressMessage,
			ActivityID: activityIDFromContextInternal(
				ctx,
			),
		},
	)
	err := errors.Join(recordErr, progressErr, evidenceErr)
	if progress, ok := ctx.Value(updateProgressKeyInternal{}).(*updateProgressInternal); ok && err != nil {
		progress.mu.Lock()
		progress.err = errors.Join(progress.err, err)
		progress.mu.Unlock()
	}
	return err
}

func (p *updateProgressInternal) completeBatchInternal(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result, batchCompleted bool, err error) error {
	activityID := activityIDFromContextInternal(ctx)
	p.mu.Lock()
	err = errors.Join(err, p.err)
	recordingFailed := p.err != nil
	_, durableRun := jobcontext.Run(ctx)
	selfTriggered := p.selfTriggered && durableRun
	selfID := p.selfID
	p.mu.Unlock()
	status := scheduler.Succeeded
	if !batchCompleted && err == nil {
		err = errors.New("update batch ended without a confirmed result")
		recordingFailed = true
	}
	if err != nil || out.Failed > 0 {
		status = scheduler.Partial
	}
	if recordingFailed {
		status = scheduler.NeedsAttention
	}
	if selfTriggered {
		err = errors.Join(
			err,
			jobcontext.Progress(
				ctx,
				scheduler.TargetOutcome{
					ID:           selfID,
					ResourceType: "container",
					Status:       scheduler.NeedsAttention,
					ActivityID:   activityID,
					Message:      "Self-update completion requires review",
				},
			),
		)
		status = scheduler.NeedsAttention
		err = errors.Join(err, errors.New("self-update was triggered but completion requires review"))
	}
	batchType := updateBatchTypeInternal(ctx, options, batchCompleted)
	checkpoint := scheduler.TargetOutcome{ID: "auto-update", ResourceType: batchType, Status: status, ActivityID: activityID}
	if err != nil {
		checkpoint.Message = err.Error()
	}
	checkpointErr := jobcontext.Progress(ctx, checkpoint)
	err = errors.Join(err, checkpointErr)
	if err != nil {
		out.Success = false
	}
	if recordingFailed || selfTriggered || checkpointErr != nil {
		err = &scheduler.OutcomeError{Outcome: scheduler.Outcome{Status: scheduler.NeedsAttention, Message: err.Error(), ActivityID: activityID}, Cause: err}
	}
	return err
}

// ListTags supplies Arcane's registry credentials to the updater's tag checker.
func (s *UpdaterService) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	if s.deps.RegistryDigestResolver == nil {
		return nil, errors.New("registry service unavailable")
	}
	var credentials []containerregistry.Credential
	if s.deps.Projects != nil {
		resolved, err := s.deps.Projects.ResolveRegistryCredentials(ctx)
		if err != nil {
			return nil, err
		}
		credentials = resolved
	}
	return s.deps.RegistryDigestResolver.ListImageTags(ctx, imageRef, credentials)
}

// UpdateServiceImages persists and redeploys the selected Compose service images.
func (s *UpdaterService) UpdateServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange) error {
	if s.deps.Projects == nil {
		return errors.New("project service unavailable")
	}
	s.appendAutoUpdateActivityMessageInternal(ctx, activityIDFromContextInternal(ctx), "Updating Compose image references", "Persisting image updates", 50)
	return s.deps.Projects.UpdateProjectServiceImages(ctx, projectID, changes, s.deps.SystemUser)
}

func (s *UpdaterService) scopedPendingRecordsInternal(ctx context.Context, records []imageupdate.ImageUpdateRecord) ([]updater.ImageUpdateRecord, error) {
	var scopedCount int64
	if err := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("container_id <> ?", "").Limit(1).Count(&scopedCount).Error; err != nil {
		return nil, err
	}
	hasScoped := scopedCount > 0
	if !hasScoped {
		out := make([]updater.ImageUpdateRecord, 0, len(records))
		for _, record := range records {
			out = append(out, imageUpdateRecordToModuleInternal(record))
		}
		return out, nil
	}
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return nil, err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return nil, err
	}
	out := make([]updater.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		converted := imageUpdateRecordToModuleInternal(record)
		if record.ContainerID != "" {
			out = append(out, converted)
			continue
		}
		for _, cnt := range listed.Items {
			resolved, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
			if policyErr != nil || resolved.Strategy == "tag" {
				continue
			}
			if refs.NormalizeImageUpdateRef(cnt.Image) != refs.NormalizeImageUpdateRef(converted.ImageRef()) {
				continue
			}
			converted.ContainerID = cnt.ID
			out = append(out, converted)
		}
	}
	return out, nil
}

func (s *UpdaterService) clearUnscopedRecordInternal(ctx context.Context, record updater.ImageUpdateRecord) error {
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return err
	}
	target, err := dockerClient.ImageInspect(ctx, record.NewImageRef())
	if err != nil {
		return err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return err
	}
	for _, cnt := range listed.Items {
		resolved, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if policyErr != nil || resolved.Strategy == "tag" {
			continue
		}
		if refs.NormalizeImageUpdateRef(cnt.Image) == refs.NormalizeImageUpdateRef(record.ImageRef()) && cnt.ImageID != target.ID {
			return nil
		}
	}
	return s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("id = ? AND container_id = ?", record.ID, "").Update("has_update", false).Error
}

// CheckProjectUpdates checks effective Compose service policies without deploying containers.
func (s *UpdaterService) CheckProjectUpdates(ctx context.Context, projectID string) (*projecttypes.UpdateInfo, error) {
	if s.deps.Projects == nil || s.deps.DB == nil || s.engine == nil {
		return nil, errors.New("project update checker unavailable")
	}
	details, err := s.deps.Projects.GetProjectDetails(ctx, projectID, projecttypes.DetailsOptions{IncludeServiceConfigs: true})
	if err != nil {
		return nil, err
	}
	if len(details.Services) == 0 {
		return nil, errors.New("project has no resolved Compose services to check")
	}
	records := make([]imageupdate.ImageUpdateRecord, 0, len(details.Services))
	for _, service := range details.Services {
		if strings.TrimSpace(service.Image) == "" {
			continue
		}
		records = append(records, s.checkProjectServiceInternal(ctx, projectID, service))
	}
	if transactionErr := s.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if deleteProjectUpdatesErr := tx.Where("project_id = ?", projectID).Delete(&imageupdate.ImageUpdateRecord{}).Error; deleteProjectUpdatesErr != nil {
			return deleteProjectUpdatesErr
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Create(&records).Error
	}); transactionErr != nil {
		return nil, fmt.Errorf("save project update checks: %w", transactionErr)
	}
	return projectpkg.BuildConfiguredUpdateInfo(projectID, details.Services, nil, records), nil
}

func (s *UpdaterService) checkProjectServiceInternal(ctx context.Context, projectID string, service types.ServiceConfig) imageupdate.ImageUpdateRecord {
	record := imageupdate.ImageUpdateRecord{
		ID:          "project::" + projectID + "::" + service.Name,
		ProjectID:   projectID,
		ServiceName: service.Name,
		PolicyKey: imageref.UpdatePolicyKey(
			service.Image,
			service.Labels,
		),
		CheckTime: time.Now().UTC(),
	}
	parsed, err := refs.NormalizeReference(service.Image)
	if err != nil {
		record.LastError = new(err.Error())
		return record
	}
	record.Repository = parsed.RegistryHost + "/" + parsed.Repository
	record.Tag = parsed.Tag
	record.CurrentVersion = parsed.Tag
	if service.Build != nil {
		record.UpdateType = imageupdate.UpdateTypeLocal
		return record
	}
	// Disabling automatic installation keeps the preview check; only the
	// update-check label opts a service out of monitoring.
	if imageref.IsUpdateCheckDisabled(service.Labels) {
		return record
	}
	check, err := s.engine.CheckImageUpdate(ctx, updatertypes.CheckRequest{ImageRef: service.Image, Policy: updater.DefaultLabelPolicy().TagPolicy(service.Labels)})
	if err != nil {
		if errdefs.IsNotFound(err) {
			record.UpdateType = imageupdate.UpdateTypeNotPulled
		} else {
			record.LastError = new(err.Error())
		}
		return record
	}
	record.HasUpdate = check.UpdateAvailable
	record.UpdateType = check.UpdateType
	record.CurrentVersion = cmp.Or(check.CurrentVersion, record.CurrentVersion)
	if check.CurrentDigest != "" {
		record.CurrentDigest = new(check.CurrentDigest)
	}
	if check.TargetDigest != "" {
		record.LatestDigest = new(check.TargetDigest)
	}
	if check.TargetRef != "" {
		target, parseErr := refs.NormalizeReference(check.TargetRef)
		if parseErr != nil {
			record.HasUpdate = false
			record.LastError = new(parseErr.Error())
		} else {
			record.LatestVersion = new(target.Tag)
		}
	}
	return record
}

func updateBatchTypeInternal(ctx context.Context, options arcaneupdater.Options, batchCompleted bool) string {
	batchType := "update-batch"
	if previous, ok := jobcontext.Run(ctx); ok && len(options.ResourceIds) > 0 {
		batchType = "update-retry"
		for _, target := range previous.Outcome.Targets {
			if target.ID == "auto-update" && target.ResourceType == "update-batch" && (target.Status == scheduler.Succeeded || target.Status == scheduler.Partial) {
				batchType = "update-batch"
			}
		}
	}
	if !batchCompleted {
		batchType = "update-interrupted"
	}
	return batchType
}

// RegisterActors registers durable single-container update delivery.
func (s *UpdaterService) RegisterActors(runtime *francis.Runtime) error {
	return s.execution.RegisterActors(runtime)
}

// Start repairs persisted single-container delivery intents until shutdown.
func (s *UpdaterService) Start(ctx context.Context) error {
	return s.execution.Start(ctx)
}

// Stop cancels business execution before the actor host drains jobs.
func (s *UpdaterService) Stop(ctx context.Context) error {
	return s.execution.Stop(ctx)
}

// ActiveUpdateActivityIDs lists activities owned by persisted single-container updates.
func (s *UpdaterService) ActiveUpdateActivityIDs(ctx context.Context) ([]string, error) {
	return s.execution.ActiveUpdateActivityIDs(ctx)
}

// ReconcilePending resumes only frozen targets whose original container is unchanged.
func (s *UpdaterService) ReconcilePending(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	return s.recovery.ReconcilePending(ctx, run)
}
