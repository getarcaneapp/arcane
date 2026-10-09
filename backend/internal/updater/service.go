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
	imageupdatetypes "github.com/getarcaneapp/arcane/types/v2/imageupdate"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	arcaneupdater "github.com/getarcaneapp/arcane/types/v2/updater"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/builtin/workflow"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	kit "go.getarcane.app/kit/pkg"
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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

// UpdaterService is Arcane's handler-facing service for the standalone updater engine.
type UpdaterService struct {
	admission  *runs.Admission
	deps       updaterDependencies
	engine     *updater.Service
	execution  *execution.Service
	recovery   *recovery.Service
	autoUpdate *flow.Workflow
	// updateMu serializes container updates: compose's recreate pipeline is not safe for
	// sibling containers sharing a namespace. Upgrade to per-project if throughput matters.
	updateMu sync.Mutex
}

type updaterDependencies struct {
	DB                     *database.DB
	Docker                 *dockerInternal.DockerClientService
	Settings               *settings.SettingsService
	Projects               *projectpkg.ProjectService
	ImagePuller            *image.ImageService
	ImageUpdates           *imageupdate.ImageUpdateService
	RegistryDigestResolver *registry.ContainerRegistryService
	Events                 *event.EventService
	Notifications          *notification.NotificationService
	SelfUpgrade            selfUpgradeService
	Activity               *activity.ActivityService
	SystemUser             usertypes.Actor
}

type selfUpgradeService interface {
	// TriggerUpgradeViaCLI returns the spawned upgrader container's ID, which this
	// service does not need — only update-all's manager step uses it.
	TriggerUpgradeViaCLI(ctx context.Context, user usertypes.Actor, target updater.SelfUpdateTarget) (string, error)
}

type (
	// containerUpdateBatch accumulates per-container update notifications
	// so a single update batch produces one batched notification.
	containerUpdateBatch struct {
		sync.Mutex

		entries []notifications.ContainerUpdateBatchEntry
	}

	containerUpdateBatchContextKey struct{}
	activityIDContextKey           struct{}
	updateAdmissionKey             struct{}
	updateProgressKey              struct{}

	updateProgress struct {
		mu            sync.Mutex
		err           error
		selfTriggered bool
		selfID        string
	}

	// autoUpdateApplied is the apply step's output, read back by finalize.
	autoUpdateApplied struct {
		Result         arcaneupdater.Result `json:"result"`
		Error          string               `json:"error,omitempty"`
		RecordingError string               `json:"recordingError,omitempty"`
		SelfTriggered  bool                 `json:"selfTriggered,omitempty"`
		SelfID         string               `json:"selfId,omitempty"`
		Unresolved     bool                 `json:"unresolved,omitempty"`
		Completed      bool                 `json:"completed"`
		// Busy means another update held the updater, so this run applied nothing; Started means an earlier
		// delivery had begun applying its plan.
		Busy    bool `json:"busy,omitempty"`
		Started bool `json:"started,omitempty"`
	}
)

var (
	errUpdateBusy = errors.New("another container update is running")

	containerEventTypes = map[string]event.EventType{
		"container_stop":   event.EventTypeContainerStop,
		"container_delete": event.EventTypeContainerDelete,
		"container_create": event.EventTypeContainerCreate,
		"container_start":  event.EventTypeContainerStart,
		"container_update": event.EventTypeContainerUpdate,
	}
)

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
	upgrade selfUpgradeService,
	activityService *activity.ActivityService,
	cfg *config.Config,
	_ *runs.Coordinator,
	admission *runs.Admission,
	roles *role.RoleService,
) (*UpdaterService, error) {
	service := &UpdaterService{
		admission: admission,
		deps: updaterDependencies{
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
	// A nil registry service must reach the engine as a nil interface, not a typed nil.
	var digestResolver updater.RegistryDigestResolver
	if registries != nil {
		digestResolver = registries
	}
	service.recovery = recovery.NewService(
		service.DockerClient,
		func() updater.RegistryDigestResolver { return digestResolver },
		service.PendingImageUpdates,
		activityIDFromContext,
		service.acquireUpdate,
		service.ApplyPending,
	)
	service.execution = execution.NewService(execution.Dependencies{
		Config:           cfg,
		Roles:            roles,
		AcquireUpdate:    service.acquireUpdate,
		UpdateBusy:       errUpdateBusy,
		RunUpdate:        service.runSingleContainerUpdate,
		RecordUpdate:     service.recordSingleContainerUpdate,
		FreezeContainer:  service.recovery.FreezeContainer,
		ConfirmTarget:    service.recovery.ConfirmTarget,
		WithFrozenTarget: service.recovery.WithSingleTarget,
	})
	// The self container ID routes Arcane through the CLI self-updater even without its labels.
	selfContainerID, selfErr := cgroup.CurrentContainerID()
	engine, err := updater.New(updater.Config{
		DockerClientProvider:   service,
		ImagePuller:            service,
		PendingStore:           service,
		RunRecorder:            service,
		Settings:               service,
		RegistryDigestResolver: digestResolver,
		RegistryTagLister:      service,
		ProjectUpdater:         service,
		SelfUpdater:            service,
		Notifier:               service,
		EventRecorder:          service,
		UsedImageCollector:     updater.UsedImageCollectorFunc(service.CollectUsedImages),
		LabelPolicy:            updater.DefaultLabelPolicy(),
		SelfContainerID:        kit.Ternary(selfErr != nil, "", selfContainerID),
		Logger:                 slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("configure updater engine: %w", err)
	}
	service.engine = engine
	events.SetDockerUpdatingContainers(func() []string { return engine.Status().ContainerIDs })
	return service, nil
}

// ApplyPending executes pending image updates. The engine's ApplyPending applies every
// pending update, so a scoped request runs its containers through the single-container path.
func (s *UpdaterService) ApplyPending(ctx context.Context, options arcaneupdater.Options) (out *arcaneupdater.Result, err error) {
	var release func()
	ctx, release, err = s.acquireUpdate(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	batchCompleted := false
	activityID := ""
	if s.deps.Activity != nil {
		started, startErr := s.deps.Activity.StartActivity(ctx, activitylib.StartRequest{
			EnvironmentID: "0",
			Type:          activitytypes.TypeAutoUpdate,
			Queue:         true,
			ResourceType:  new("system"),
			ResourceName:  new("Auto update"),
			Step:          "Planning updates",
			LatestMessage: "Auto-update run started",
			Metadata:      database.JSON{"dryRun": options.DryRun},
		})
		if startErr != nil {
			slog.DebugContext(ctx, "failed to start auto-update activity", "error", startErr)
		} else {
			activityID = started.ID
			ctx = contextWithActivityID(s.deps.Activity.Track(ctx, activityID), activityID)
		}
	}
	out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
	progress := &updateProgress{}
	ctx = context.WithValue(ctx, updateProgressKey{}, progress)

	defer func() {
		out.Duration = cmp.Or(out.Duration, time.Since(start).String())
		out.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
		err = progress.completeBatch(ctx, options, out, batchCompleted, err)
		failure := ""
		if err != nil {
			failure = err.Error()
		} else if out.Failed > 0 {
			failure = fmt.Sprintf("%d update action(s) failed", out.Failed)
		}
		s.completeAutoUpdateActivity(ctx, activityID, "Auto-update run completed", failure)
	}()

	if activityID != "" {
		// Bounded so a long queue fails the activity instead of stranding it queued.
		if slotErr := s.deps.Activity.AwaitActivitySlotBounded(ctx, activityID, "0"); slotErr != nil {
			return out, slotErr
		}
	}

	if applyErr := s.applyBatch(ctx, options, out); applyErr != nil {
		return out, applyErr
	}
	batchCompleted = true
	return out, nil
}

// applyBatch runs one update batch into out, records its events, and sends its notifications
// as one batch. The engine's per-container docker operations carry no timeouts, so the run is capped.
func (s *UpdaterService) applyBatch(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result) error {
	batch := &containerUpdateBatch{}
	ctx = context.WithValue(ctx, containerUpdateBatchContextKey{}, batch)
	defer func() {
		batch.Lock()
		entries := batch.entries
		batch.Unlock()
		if len(entries) == 0 || s.deps.Notifications == nil {
			return
		}
		if err := s.deps.Notifications.SendBatchContainerUpdateNotification(ctx, entries); err != nil {
			slog.ErrorContext(ctx, "failed to send batched container update notification", "error", err, "count", len(entries))
		}
	}()
	runCtx, cancelRun := context.WithTimeout(ctx, timeouts.DefaultAutoUpdateApply)
	defer cancelRun()
	activityID := activityIDFromContext(ctx)

	s.recordAutoUpdateEvent(ctx, event.EventSeverityInfo, database.JSON{
		"phase":       "start",
		"dryRun":      options.DryRun,
		"forceUpdate": options.ForceUpdate,
		"scopedType":  options.Type,
		"scopedCount": len(options.ResourceIds),
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
	s.appendAutoUpdateActivityMessage(ctx, activityID, "Planning pending updates", "Planning updates", 5)

	if len(options.ResourceIds) > 0 {
		if applyErr := s.applyScopedUpdates(runCtx, options, out); applyErr != nil {
			return applyErr
		}
	} else {
		moduleResult, engineErr := s.engine.ApplyPending(runCtx, updater.Options{Force: options.ForceUpdate, DryRun: options.DryRun})
		if moduleResult != nil {
			*out = *resultFromModule(moduleResult)
			s.logResultItems(ctx, out)
		} else if engineErr == nil {
			engineErr = errors.New("updater returned no batch result")
		}
		if engineErr != nil {
			return engineErr
		}
	}

	if !options.DryRun && s.deps.ImageUpdates != nil {
		s.appendAutoUpdateActivityMessage(ctx, activityID, "Cleaning up update records", "Cleaning up", 95)
		if cleanupErr := s.deps.ImageUpdates.CleanupOrphanedRecords(runCtx); cleanupErr != nil {
			slog.WarnContext(ctx, "cleanup orphaned update records failed", "error", cleanupErr)
		}
	}

	s.recordAutoUpdateEvent(ctx, event.EventSeverityInfo, database.JSON{
		"phase":     "complete",
		"checked":   out.Checked,
		"updated":   out.Updated,
		"restarted": out.Restarted,
		"skipped":   out.Skipped,
		"failed":    out.Failed,
		"duration":  out.Duration,
		"time":      time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

// scopedContainerIDs resolves the requested containers, projects, or images to container IDs.
func (s *UpdaterService) scopedContainerIDs(ctx context.Context, options arcaneupdater.Options) ([]string, error) {
	requested := kit.TrimNonEmpty(options.ResourceIds)
	scope := strings.ToLower(strings.TrimSpace(options.Type))
	containerIDs := requested
	switch {
	case len(requested) == 0, scope == "", scope == "container":
	case scope == "project", scope == "image":
		// Projects match by lowercase Compose project name; images by ID or normalized reference.
		wanted := make(map[string]bool, len(requested)*2)
		for _, ref := range requested {
			if scope == "image" {
				wanted[ref], wanted[refs.NormalizeImageUpdateRef(ref)] = true, true
				continue
			}
			name, discovered := strings.CutPrefix(ref, "compose:")
			if !discovered && s.deps.Projects != nil {
				if project, lookupErr := s.deps.Projects.GetProjectFromDatabaseByID(ctx, ref); lookupErr == nil && project != nil {
					name = cmp.Or(strings.TrimSpace(kit.FromPtr(project.ComposeProjectName)), strings.TrimSpace(project.Name), name)
				}
			}
			wanted[strings.ToLower(strings.TrimSpace(name))] = true
		}
		delete(wanted, "")
		if s.deps.Docker == nil {
			return nil, errors.New("docker service unavailable")
		}
		containers, _, _, _, err := s.deps.Docker.GetAllContainers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list containers: %w", err)
		}
		containerIDs = nil
		for _, summary := range containers {
			matched := wanted[summary.ImageID] || wanted[refs.NormalizeImageUpdateRef(summary.Image)]
			if scope == "project" {
				project := strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(summary.Labels)))
				matched = docker.ComposeServiceLabel(summary.Labels) != "" && wanted[project]
			}
			if matched {
				containerIDs = append(containerIDs, summary.ID)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported scoped update type %q", options.Type)
	}
	return containerIDs, nil
}

// applyScopedUpdates updates each requested container through the engine's single-container path.
func (s *UpdaterService) applyScopedUpdates(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result) error {
	containerIDs, err := s.scopedContainerIDs(ctx, options)
	if err != nil {
		return err
	}
	if len(containerIDs) == 0 {
		return common.ErrUpdaterNoContainersMatched
	}

	engineOpts := updater.Options{Force: options.ForceUpdate, DryRun: options.DryRun}
	var engineErrs []error
	for _, containerID := range containerIDs {
		target := scheduler.TargetOutcome{ID: containerID, ResourceType: "container", Status: scheduler.Running}
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return progressErr
		}
		moduleResult, engineErr := s.engine.UpdateContainer(ctx, containerID, engineOpts)
		target.Status = scheduler.NeedsAttention
		if moduleResult != nil {
			partial := resultFromModule(moduleResult)
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
		if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
			return errors.Join(progressErr, errors.Join(engineErrs...))
		}
	}
	s.logResultItems(ctx, out)
	out.Success = out.Failed == 0
	// Like the unscoped engine error, these surface after every container was attempted.
	return errors.Join(engineErrs...)
}

// UpdateSingleContainer updates a single container by ID to the latest available image.
func (s *UpdaterService) UpdateSingleContainer(ctx context.Context, containerID string) (out *arcaneupdater.Result, err error) {
	activityID, workCtx := "", ctx
	if s.deps.Activity != nil {
		localActivity, trackedCtx, startErr := s.deps.Activity.StartTrackedActivity(ctx, s.singleContainerActivityRequest(ctx, containerID))
		if startErr != nil {
			return nil, fmt.Errorf("start container update activity: %w", startErr)
		}
		activityID, workCtx = localActivity.ID, trackedCtx
	}
	defer func() {
		s.finishSingleContainerUpdate(workCtx, activityID, out, err)
	}()
	defer utils.RecoverToError(&err, "single container update")
	return s.runSingleContainerUpdate(workCtx, containerID, activityID)
}

// AcceptSingleContainerUpdate submits a cancellable container-update workflow and returns its activity.
func (s *UpdaterService) AcceptSingleContainerUpdate(ctx context.Context, containerID string) (*activitytypes.Activity, error) {
	if s.deps.Activity == nil {
		return nil, errors.New("asynchronous container updates unavailable")
	}
	activityID, err := s.execution.Submit(ctx, containerID, s.singleContainerActivityRequest(ctx, containerID))
	if err != nil {
		return nil, err
	}
	detail, err := s.deps.Activity.GetActivityDetail(ctx, "0", activityID, 1)
	if err != nil {
		return nil, err
	}
	return &detail.Activity, nil
}

func (s *UpdaterService) runSingleContainerUpdate(ctx context.Context, containerID, activityID string) (out *arcaneupdater.Result, err error) {
	var release func()
	ctx, release, err = s.acquireUpdate(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	out = &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
	defer func() {
		out.Duration = cmp.Or(out.Duration, time.Since(start).String())
		out.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
	}()
	ctx = contextWithActivityID(ctx, activityID)
	if s.deps.Activity != nil && activityID != "" {
		if slotErr := s.deps.Activity.AwaitActivitySlotBounded(ctx, activityID, "0"); slotErr != nil {
			return out, slotErr
		}
	}

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, ctxErr
	}

	// The caller picked this container, so the autoUpdateExcludedContainers setting does not
	// apply here; labels and immutable references still do.
	moduleResult, err := s.engine.UpdateContainer(ctx, containerID, updater.Options{IgnoreSettingsExclusions: true})
	if moduleResult != nil {
		out = resultFromModule(moduleResult)
		s.logResultItems(ctx, out)
	}
	return out, err
}

// GetStatus returns the current in-memory update activity snapshot.
func (s *UpdaterService) GetStatus() arcaneupdater.Status {
	status := s.engine.Status()
	return arcaneupdater.Status{
		UpdatingContainers: status.UpdatingContainers,
		UpdatingProjects:   status.UpdatingProjects,
		ContainerIds:       status.ContainerIDs,
		ProjectIds:         status.ProjectIDs,
	}
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
	results, err := s.engine.RestartContainersUsingOldImages(ctx, oldIDToNewRef, oldRefToNewRef)
	return resultFromModule(&updater.Result{Items: results}).Items, err
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
	return s.engine.BeginContainerUpdate(containerID)
}

// BeginProjectUpdate marks a project as updating.
func (s *UpdaterService) BeginProjectUpdate(projectID string) func() {
	return s.engine.BeginProjectUpdate(projectID)
}

func (s *UpdaterService) recordAutoUpdateEvent(ctx context.Context, severity event.EventSeverity, metadata database.JSON) {
	if s.deps.Events == nil {
		return
	}
	phase, _ := metadata["phase"].(string)
	title, subject := kit.Ternary(phase != "", "Auto-update: "+phase, "Auto-update"), ""
	switch phase {
	case "start":
		title = "Auto-update run started"
	case "complete":
		title = "Auto-update run completed"
	case "image_pull", "image":
		title, subject = "Auto-update: image pull", cmp.Or(kit.ToString(metadata["imageNew"]), kit.ToString(metadata["imageOld"]))
	case "image_prune":
		title, subject = "Auto-update: image prune", kit.ToString(metadata["imageId"])
	case "container":
		title = "Auto-update: container"
		subject = cmp.Or(kit.ToString(metadata["resourceName"]), kit.ToString(metadata["container"]), kit.ToString(metadata["containerId"]))
	case "project":
		title, subject = "Auto-update: project", cmp.Or(kit.ToString(metadata["projectName"]), kit.ToString(metadata["projectId"]))
	}
	if subject != "" {
		title += " " + subject
	}
	_, err := s.deps.Events.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeSystemAutoUpdate,
		Severity:      severity,
		Title:         title,
		ResourceType:  new("system"),
		ResourceName:  new("auto_updater"),
		EnvironmentID: new("0"),
		Metadata:      metadata,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to record auto-update event", "error", err)
	}
}

// DockerClient returns Arcane's configured Docker client for the updater engine.
func (s *UpdaterService) DockerClient(ctx context.Context) (*client.Client, error) {
	if s == nil || s.deps.Docker == nil {
		return nil, common.Classify(common.ErrUnavailable, errors.New("docker service unavailable"))
	}
	return s.deps.Docker.GetClient(ctx)
}

// PullImage pulls an image through Arcane's image service, bounded by the
// dockerImagePullTimeout setting so a stuck pull cannot hold the auto-update run.
func (s *UpdaterService) PullImage(ctx context.Context, imageRef string, progress io.Writer) error {
	if s == nil || s.deps.ImagePuller == nil {
		return common.Classify(common.ErrUnavailable, errors.New("image service unavailable"))
	}
	pulledRef, err := s.recovery.PreparePull(ctx, imageRef)
	if err != nil {
		return err
	}
	writer := activitylib.NewWriter(ctx, s.deps.Activity, activityIDFromContext(ctx), progress, "Pulling updated images")
	defer activitylib.FlushWriter(writer)

	pullTimeoutSeconds := 0
	if s.deps.Settings != nil {
		pullTimeoutSeconds = s.deps.Settings.GetSettingsConfig().DockerImagePullTimeout.AsInt()
	}
	pullCtx, cancelPull := context.WithTimeout(ctx, timeouts.GetDuration(pullTimeoutSeconds, timeouts.DefaultDockerImagePull))
	defer cancelPull()

	var credentials []containerregistry.Credential
	if s.deps.Projects != nil {
		if credentials, err = s.deps.Projects.ResolveRegistryCredentials(pullCtx); err != nil {
			return fmt.Errorf("resolve registry credentials: %w", err)
		}
	}
	if pullErr := s.deps.ImagePuller.PullImage(pullCtx, pulledRef, writer, s.deps.SystemUser, credentials); pullErr != nil {
		return pullErr
	}
	// A frozen digest pull is tagged back to the reference the engine asked for.
	if pulledRef != imageRef {
		dockerClient, clientErr := s.DockerClient(pullCtx)
		if clientErr != nil {
			return clientErr
		}
		if _, tagErr := dockerClient.ImageTag(pullCtx, client.ImageTagOptions{Source: pulledRef, Target: imageRef}); tagErr != nil {
			return tagErr
		}
	}
	// Reconcile by tag on the run context so a late pull still clears its update records (#4306).
	if reconcileErr := s.deps.ImagePuller.ReconcilePulledImageUpdate(ctx, imageRef); reconcileErr != nil {
		slog.WarnContext(ctx, "failed to reconcile pulled image update state", "image", imageRef, "error", reconcileErr)
	}
	return nil
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

	// Flush "Updates Available" notifications before the engine clears these records (#3132);
	// a flush that cannot decide what to send aborts so the records survive for retry.
	if s.deps.ImageUpdates != nil {
		if err := s.deps.ImageUpdates.SendBatchUpdateNotifications(ctx); err != nil {
			return nil, err
		}
	}
	s.appendAutoUpdateActivityMessage(ctx, activityIDFromContext(ctx), fmt.Sprintf("Found %d pending image update records", len(records)), "Planning updates", 10)

	// Once container-scoped records exist, an unscoped record applies per running non-tag-policy container of its image.
	var scopedCount int64
	if err := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("container_id <> ?", "").Limit(1).Count(&scopedCount).Error; err != nil {
		return nil, err
	}
	var running []container.Summary
	if scopedCount > 0 {
		dockerClient, err := s.DockerClient(ctx)
		if err != nil {
			return nil, err
		}
		listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
		if err != nil {
			return nil, err
		}
		running = listed.Items
	}
	out := make([]updater.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		converted := updater.ImageUpdateRecord{
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
		if scopedCount == 0 || record.ContainerID != "" {
			out = append(out, converted)
			continue
		}
		for _, cnt := range running {
			resolved, policyErr := tagpolicy.Resolve(cnt.Image, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
			if policyErr != nil || resolved.Strategy == "tag" || refs.NormalizeImageUpdateRef(cnt.Image) != refs.NormalizeImageUpdateRef(converted.ImageRef()) {
				continue
			}
			converted.ContainerID = cnt.ID
			out = append(out, converted)
		}
	}
	return out, nil
}

// ClearImageUpdateRecord clears a pending image update record after it is handled.
func (s *UpdaterService) ClearImageUpdateRecord(ctx context.Context, record updater.ImageUpdateRecord) error {
	if s == nil {
		return common.Classify(common.ErrUnavailable, errors.New("updater service unavailable"))
	}
	if s.deps.DB == nil {
		return nil
	}
	query := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{})
	if record.ContainerID == "" || strings.HasPrefix(record.ID, "container::") {
		query = query.Where("container_id = ?", record.ContainerID)
		if record.ID != "" {
			return query.Where("id = ?", record.ID).Update("has_update", false).Error
		}
		return query.Where("repository = ? AND tag = ?", record.Repository, record.Tag).Update("has_update", false).Error
	}

	// A shared record stays pending while another non-tag-policy container still runs an older image.
	dockerClient, err := s.DockerClient(ctx)
	if err != nil {
		return err
	}
	target, err := dockerClient.ImageInspect(ctx, record.NewImageRef())
	if err != nil {
		return err
	}
	listed, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{})
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
	return query.Where("id = ? AND container_id = ?", record.ID, "").Update("has_update", false).Error
}

// ExcludedContainers returns auto-update exclusions from Arcane settings.
func (s *UpdaterService) ExcludedContainers(ctx context.Context) ([]string, error) {
	if s == nil || s.deps.Settings == nil {
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
	// The frozen digest is only what gets pulled; the recreated container keeps
	// the selected tag so later runs do not treat it as an immutable reference.
	if target.NewImageRef != "" {
		pinned, err := s.recovery.PreparePull(ctx, target.NewImageRef)
		if err != nil {
			return err
		}
		target.PullImageRef = pinned
	}
	if s == nil || s.deps.SelfUpgrade == nil {
		instanceType := cmp.Or(strings.TrimSpace(target.InstanceType), "server")
		return fmt.Errorf("%s self-update requires CLI upgrade service", instanceType)
	}

	// A server self-update stops this process before the run completes, so the
	// activity is flagged first and startup reconciliation finalizes it.
	if activityID := activityIDFromContext(ctx); target.InstanceType != "agent" && activityID != "" && s.deps.Activity != nil {
		message := "Self-update initiated — Arcane will restart"
		if target.NewImageRef != "" {
			message += " with " + target.NewImageRef
		}
		s.appendAutoUpdateActivityMessage(ctx, activityID, message, "Self-update", 90)
		if err := s.deps.Activity.PatchActivityMetadata(ctx, activityID, database.JSON{"selfUpdateTriggered": true}); err != nil {
			slog.DebugContext(ctx, "failed to mark self-update on activity", "activityId", activityID, "error", err)
		}
	}

	if _, err := s.deps.SelfUpgrade.TriggerUpgradeViaCLI(ctx, s.deps.SystemUser, target); err != nil {
		return fmt.Errorf("CLI upgrade failed: %w", err)
	}
	if progress, ok := ctx.Value(updateProgressKey{}).(*updateProgress); ok {
		progress.mu.Lock()
		progress.selfTriggered = true
		progress.selfID = target.ContainerID
		progress.mu.Unlock()
	}
	return nil
}

// Notify buffers the container update notification inside an update batch, which sends
// them as one notification when it ends; outside a batch it sends immediately.
func (s *UpdaterService) Notify(ctx context.Context, localNotification updater.Notification) error {
	if s == nil || s.deps.Notifications == nil {
		return nil
	}
	if buffer, ok := ctx.Value(containerUpdateBatchContextKey{}).(*containerUpdateBatch); ok {
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

// RecordEvent records updater lifecycle events in Arcane's event stream.
func (s *UpdaterService) RecordEvent(ctx context.Context, evt updater.Event) error {
	if s == nil {
		return nil
	}

	if eventType, ok := containerEventTypes[evt.Phase]; ok {
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
	s.recordAutoUpdateEvent(ctx, severity, database.JSON{
		"phase":        evt.Phase,
		"resourceId":   evt.ResourceID,
		"resourceName": evt.ResourceName,
		"resourceType": evt.ResourceType,
		"time":         time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func contextWithActivityID(ctx context.Context, activityID string) context.Context {
	if activityID == "" {
		return ctx
	}
	return context.WithValue(ctx, activityIDContextKey{}, activityID)
}

func activityIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	activityID, _ := ctx.Value(activityIDContextKey{}).(string)
	return activityID
}

// singleContainerActivityRequest describes a container update's activity, named after the container when it can be inspected.
func (s *UpdaterService) singleContainerActivityRequest(ctx context.Context, containerID string) activitylib.StartRequest {
	name := containerID
	lookupCtx, cancelLookup := context.WithTimeout(ctx, 2*time.Second)
	if dockerClient, dockerErr := s.DockerClient(lookupCtx); dockerErr == nil {
		if inspected, inspectErr := compat.ContainerInspectWithCompatibility(lookupCtx, dockerClient, containerID, client.ContainerInspectOptions{}); inspectErr == nil {
			name = cmp.Or(strings.TrimPrefix(inspected.Container.Name, "/"), containerID)
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
	return activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          activitytypes.TypeAutoUpdate,
		Queue:         true,
		DeferSlot:     true,
		ResourceType:  new("container"),
		ResourceID:    &containerID,
		ResourceName:  &name,
		StartedBy:     user,
		Step:          "Updating container",
		LatestMessage: "Container update started",
		Metadata:      database.JSON{"containerID": containerID},
	}
}

func (s *UpdaterService) finishSingleContainerUpdate(ctx context.Context, activityID string, result *arcaneupdater.Result, runErr error) {
	if s.deps.Activity == nil || activityID == "" {
		return
	}
	outcome := s.recordSingleContainerUpdate(ctx, activityID, result, runErr)
	s.completeAutoUpdateActivity(ctx, activityID, outcome.Message, kit.Ternary(outcome.Status != scheduler.Succeeded, outcome.Message, ""))
}

// recordSingleContainerUpdate saves a container update's result on its activity and summarizes it as an outcome.
func (s *UpdaterService) recordSingleContainerUpdate(ctx context.Context, activityID string, result *arcaneupdater.Result, runErr error) scheduler.Outcome {
	metadata, containerName := database.JSON{}, ""
	if result != nil {
		metadata = database.JSON{"updated": result.Updated, "restarted": result.Restarted, "skipped": result.Skipped, "failed": result.Failed}
		if len(result.Items) > 0 {
			item := result.Items[0]
			metadata["updateOutcome"] = item.Status
			if containerName = item.ResourceName; containerName != "" {
				metadata["containerName"] = containerName
			}
			if item.Error != "" {
				metadata["updateReason"] = item.Error
			}
		}
	}
	if s.deps.Activity != nil && activityID != "" {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if containerName != "" {
			if _, err := s.deps.Activity.UpdateActivity(writeCtx, activityID, activitylib.UpdateRequest{ResourceName: &containerName}); err != nil {
				slog.WarnContext(writeCtx, "failed to update container activity name", "activityId", activityID, "error", err)
			}
		}
		if err := s.deps.Activity.PatchActivityMetadata(writeCtx, activityID, metadata); err != nil {
			slog.WarnContext(writeCtx, "failed to persist container update result", "activityId", activityID, "error", err)
		}
	}
	if runErr == nil && result != nil && result.Failed == 0 {
		message := "Container already current"
		switch {
		case result.Updated > 0 || result.Restarted > 0:
			message = "Container updated"
		case len(result.Items) > 0 && result.Items[0].Status == string(updater.StatusSkipped) && result.Items[0].Error != "":
			message = "Container update skipped: " + result.Items[0].Error
		case result.Skipped > 0:
			message = "Container update skipped"
		}
		return scheduler.Outcome{Status: scheduler.Succeeded, Message: message}
	}
	switch {
	case runErr != nil:
	case result == nil:
		runErr = errors.New("container update produced no result")
	case len(result.Items) > 0 && result.Items[0].Error != "":
		runErr = errors.New(result.Items[0].Error)
	default:
		runErr = fmt.Errorf("%d update action(s) failed", result.Failed)
	}
	return scheduler.Outcome{Status: scheduler.Failed, Message: runErr.Error()}
}

func (s *UpdaterService) appendAutoUpdateActivityMessage(ctx context.Context, activityID, message, step string, progress int) {
	if s.deps.Activity == nil || activityID == "" {
		return
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

// completeAutoUpdateActivity ends the activity with message, or as failed when failure is set;
// a failure caused by cancelling the activity ends it as cancelled.
func (s *UpdaterService) completeAutoUpdateActivity(ctx context.Context, activityID, message, failure string) {
	if s.deps.Activity == nil || activityID == "" {
		return
	}
	status := activitytypes.StatusSuccess
	var errMessage *string
	switch {
	case failure == "":
	case activitylib.CancelledByContext(ctx):
		status, message = activitytypes.StatusCancelled, "Auto-update cancelled"
	default:
		status, message, errMessage = activitytypes.StatusFailed, failure, &failure
	}
	// A lost terminal write strands the activity as running, so it is written past cancellation and logged loudly.
	if _, err := s.deps.Activity.CompleteActivity(context.WithoutCancel(ctx), activityID, status, message, errMessage); err != nil {
		slog.ErrorContext(ctx, "failed to complete auto-update activity", "activityId", activityID, "error", err)
	}
}

// resultFromModule converts an engine result to Arcane's wire type, which carries times as
// strings and images as maps holding only the "main" entry. Callers set ActivityID.
func resultFromModule(result *updater.Result) *arcaneupdater.Result {
	if result == nil {
		return &arcaneupdater.Result{Items: []arcaneupdater.ResourceResult{}}
	}
	mainImage := func(ref string) map[string]string {
		if ref == "" {
			return nil
		}
		return map[string]string{"main": ref}
	}
	out := &arcaneupdater.Result{
		Success:   result.Success,
		Checked:   result.Checked,
		Updated:   result.Updated,
		Restarted: result.Restarted,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
		StartTime: kit.Ternary(result.StartTime.IsZero(), "", result.StartTime.UTC().Format(time.RFC3339)),
		EndTime:   kit.Ternary(result.EndTime.IsZero(), "", result.EndTime.UTC().Format(time.RFC3339)),
		Duration:  result.Duration().String(),
		Items:     make([]arcaneupdater.ResourceResult, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		out.Items = append(out.Items, arcaneupdater.ResourceResult{
			ResourceID:      item.ResourceID,
			ResourceName:    item.ResourceName,
			ResourceType:    string(item.ResourceType),
			Status:          string(item.Status),
			UpdateAvailable: item.UpdateAvailable,
			UpdateApplied:   item.UpdateApplied,
			OldImages:       mainImage(item.OldImage),
			NewImages:       mainImage(item.NewImage),
			Error:           item.Error,
			Details:         item.Details,
		})
	}
	return out
}

func (s *UpdaterService) logResultItems(ctx context.Context, result *arcaneupdater.Result) {
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
		s.recordAutoUpdateEvent(ctx, severity, database.JSON{
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

	if err := s.collectUsedImagesFromContainers(ctx, out); err != nil {
		errs = append(errs, err)
		slog.DebugContext(ctx, "collectUsedImages: failed collecting from containers", "error", err)
	} else {
		successfulSources++
	}

	if s.deps.Projects != nil {
		if err := s.collectUsedImagesFromProjects(ctx, out); err != nil {
			errs = append(errs, err)
			slog.DebugContext(ctx, "collectUsedImages: failed collecting from projects", "error", err)
		} else {
			successfulSources++
		}
	}

	if successfulSources == 0 {
		return nil, errors.Join(errs...)
	}

	slog.DebugContext(ctx, "collectUsedImages: collected used images", "count", len(out))
	return out, nil
}

func (s *UpdaterService) collectUsedImagesFromContainers(ctx context.Context, out map[string]struct{}) error {
	dcli, err := s.DockerClient(ctx)
	if err != nil {
		return err
	}
	var excludedContainers map[string]bool
	if s.deps.Settings != nil {
		excludedContainers = docker.ExcludedContainerNameSet(s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))
	}
	listResult, err := dcli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return err
	}

	for _, summary := range listResult.Items {
		if labels.IsUpdateDisabled(summary.Labels) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container opted out by labels", "containerId", summary.ID)
			continue
		}

		if docker.ContainerNameExcluded(summary.Names, excludedContainers) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: skipping excluded container", "containerId", summary.ID, "names", summary.Names)
			continue
		}

		imageRef := strings.TrimSpace(summary.Image)
		if imageRef != "" && !refs.IsImageIDLikeReference(imageRef) {
			addNormalizedImageUpdateRef(ctx, out, imageRef, "collectUsedImagesFromContainers", "containerId", summary.ID)
			continue
		}

		// The summary only names an image ID, so the image's tags and the configured reference stand in.
		inspectResult, inspectErr := compat.ContainerInspectWithCompatibility(ctx, dcli, summary.ID, client.ContainerInspectOptions{})
		if inspectErr != nil {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect failed", "containerId", summary.ID, "error", inspectErr)
			continue
		}
		inspect := inspectResult.Container
		if inspect.Config != nil && labels.IsUpdateDisabled(inspect.Config.Labels) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect labels opted out", "containerId", summary.ID)
			continue
		}
		if imageInspect, imageErr := dcli.ImageInspect(ctx, inspect.Image); imageErr == nil {
			for _, tag := range imageInspect.RepoTags {
				if strings.TrimSpace(tag) != "" && tag != "<none>:<none>" {
					addNormalizedImageUpdateRef(ctx, out, tag, "normalizedTagsForContainer repo tag", "imageId", inspect.Image)
				}
			}
		}
		if inspect.Config != nil && inspect.Config.Image != "" {
			addNormalizedImageUpdateRef(ctx, out, inspect.Config.Image, "normalizedTagsForContainer config image", "imageId", inspect.Image)
		}
	}
	return nil
}

func (s *UpdaterService) collectUsedImagesFromProjects(ctx context.Context, out map[string]struct{}) error {
	allProjects, err := s.deps.Projects.ListAllProjects(ctx)
	if err != nil {
		return err
	}

	activeProjectNames := map[string]struct{}{}
	for _, project := range allProjects {
		name := strings.TrimSpace(project.Name)
		running := project.Status == projectpkg.ProjectStatusRunning || project.Status == projectpkg.ProjectStatusPartiallyRunning
		if project.IsArchived || !running || name == "" {
			continue
		}
		activeProjectNames[name] = struct{}{}
		if normalized := projects.NormalizeProjectName(name); normalized != "" {
			activeProjectNames[normalized] = struct{}{}
		}
	}
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

	for _, summary := range composeContainers {
		if _, isActive := activeProjectNames[docker.ComposeProjectLabel(summary.Labels)]; !isActive || labels.IsUpdateDisabled(summary.Labels) {
			continue
		}
		imageRef := strings.TrimSpace(summary.Image)
		if imageRef == "" || refs.IsImageIDLikeReference(imageRef) {
			continue
		}
		addNormalizedImageUpdateRef(ctx, out, imageRef, "collectUsedImagesFromComposeContainers", "containerId", summary.ID)
	}
	return nil
}

func addNormalizedImageUpdateRef(ctx context.Context, out map[string]struct{}, imageRef, source string, attrs ...any) {
	if normalizedRef := refs.NormalizeImageUpdateRef(imageRef); normalizedRef != "" {
		out[normalizedRef] = struct{}{}
		return
	}
	slog.DebugContext(ctx, "skipping invalid image reference", append(attrs, "source", source, "imageRef", imageRef)...)
}

func (s *UpdaterService) acquireUpdate(ctx context.Context) (context.Context, func(), error) {
	if s.admission == nil || ctx.Value(updateAdmissionKey{}) == s {
		return ctx, func() {}, nil
	}
	lease, admitted, err := s.admission.TryAcquire(ctx, scheduler.AdmissionKey{Scope: "updater"})
	if err != nil {
		return ctx, nil, err
	}
	if !admitted {
		return ctx, nil, errUpdateBusy
	}
	return context.WithValue(ctx, updateAdmissionKey{}, s), func() { lease.Release(ctx) }, nil
}

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result updater.ResourceResult) error {
	var recordErr error
	if s != nil && s.deps.DB != nil {
		now := time.Now()
		record := &AutoUpdateRecord{
			ResourceID:      result.ResourceID,
			ResourceType:    string(result.ResourceType),
			ResourceName:    result.ResourceName,
			Status:          AutoUpdateStatus(result.Status),
			StartTime:       now,
			EndTime:         &now,
			UpdateAvailable: result.UpdateAvailable || result.Status == updater.StatusUpdated || result.Status == updater.StatusUpdateAvailable,
			UpdateApplied:   result.UpdateApplied,
		}
		if result.OldImage != "" {
			record.OldImageVersions = database.JSON{"main": result.OldImage}
		}
		if result.NewImage != "" {
			record.NewImageVersions = database.JSON{"main": result.NewImage}
		}
		if len(result.Details) > 0 {
			record.Details = maps.Clone(result.Details)
		}
		if result.Error != "" {
			record.Error = &result.Error
		}
		recordErr = s.deps.DB.WithContext(ctx).Create(record).Error
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
	activityID := activityIDFromContext(ctx)
	if activityID != "" && s != nil && s.deps.Activity != nil {
		if _, appendErr := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{Level: level, Message: message, Step: "Applying updates"}); appendErr != nil {
			slog.DebugContext(ctx, "failed to append update result activity message", "activityId", activityID, "resource", name, "error", appendErr)
		}
	}
	progressMessage := result.Error
	var evidenceErr error
	if status == scheduler.Succeeded {
		if evidenceErr = s.recovery.VerifyResult(ctx, result.ResourceID); evidenceErr != nil {
			status, progressMessage = scheduler.NeedsAttention, evidenceErr.Error()
		}
	}
	target := scheduler.TargetOutcome{ID: result.ResourceID, ResourceType: string(result.ResourceType), Status: status, Message: progressMessage, ActivityID: activityID}
	err := errors.Join(recordErr, jobcontext.Progress(ctx, target), evidenceErr)
	if progress, ok := ctx.Value(updateProgressKey{}).(*updateProgress); ok && err != nil {
		progress.mu.Lock()
		progress.err = errors.Join(progress.err, err)
		progress.mu.Unlock()
	}
	return err
}

func (p *updateProgress) completeBatch(ctx context.Context, options arcaneupdater.Options, out *arcaneupdater.Result, batchCompleted bool, err error) error {
	activityID := activityIDFromContext(ctx)
	p.mu.Lock()
	err = errors.Join(err, p.err)
	recordingFailed := p.err != nil
	previous, durableRun := jobcontext.Run(ctx)
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
		selfTarget := scheduler.TargetOutcome{
			ID:           selfID,
			ResourceType: "container",
			Status:       scheduler.NeedsAttention,
			ActivityID:   activityID,
			Message:      "Self-update completion requires review",
		}
		err = errors.Join(err, jobcontext.Progress(ctx, selfTarget), errors.New("self-update was triggered but completion requires review"))
		status = scheduler.NeedsAttention
	}
	// A scoped rerun of a durable run is a retry unless that run already recorded a finished batch.
	batchType := "update-batch"
	if durableRun && len(options.ResourceIds) > 0 && !slices.ContainsFunc(previous.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return target.ID == "auto-update" && target.ResourceType == "update-batch" && (target.Status == scheduler.Succeeded || target.Status == scheduler.Partial)
	}) {
		batchType = "update-retry"
	}
	if !batchCompleted {
		batchType = "update-interrupted"
	}
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
			return nil, fmt.Errorf("resolve registry credentials: %w", err)
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
	s.appendAutoUpdateActivityMessage(ctx, activityIDFromContext(ctx), "Updating Compose image references", "Persisting image updates", 50)
	return s.deps.Projects.UpdateProjectServiceImages(ctx, projectID, changes, s.deps.SystemUser)
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
		records = append(records, s.checkProjectService(ctx, projectID, service))
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

func (s *UpdaterService) checkProjectService(ctx context.Context, projectID string, service types.ServiceConfig) imageupdate.ImageUpdateRecord {
	record := imageupdate.ImageUpdateRecord{
		ID:          "project::" + projectID + "::" + service.Name,
		ProjectID:   projectID,
		ServiceName: service.Name,
		PolicyKey:   imageref.UpdatePolicyKey(service.Image, service.Labels),
		CheckTime:   time.Now().UTC(),
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

// RegisterWorkflows defines the auto-update workflow (fresh candidate check, frozen plan without
// failed checks, confirm-first apply) and the container-update workflow. Call it before the host starts.
func (s *UpdaterService) RegisterWorkflows(engine *flow.Engine) error {
	autoUpdate, err := engine.Define(flow.Definition{
		Name:        "auto-update",
		Version:     5,
		Fingerprint: "91e27dc48df6737110ec334dfa9c2e2e032a6cc4285e8f1dd08dc40741e6d5b9",
		Concurrency: 1,
		Timeout:     2 * time.Hour,
		Activity: activitylib.StartRequest{
			Type:          activitytypes.TypeAutoUpdate,
			Queue:         true,
			ResourceType:  new("system"),
			ResourceName:  new("Auto update"),
			Step:          "Planning updates",
			LatestMessage: "Auto-update run started",
			Metadata:      database.JSON{"dryRun": false},
		},
		Labels: map[string]string{
			"resume":     "Checking for a saved plan",
			"candidates": "Selecting update candidates",
			"check":      "Checking images",
			"plan":       "Planning updates",
			"apply":      "Applying updates",
			"finalize":   "Finishing auto-update",
		},
		Steps: []workflow.StepSpec{
			workflow.Step("resume", engine.Handler(s.autoUpdateResume)),
			// A retry keeps its frozen plan, so it skips selecting and checking images.
			workflow.Step("candidates", engine.Handler(s.autoUpdateCandidates), workflow.WithSkipIf("resume", true)),
			workflow.Child("check", workflow.WithDefinition(s.deps.ImageUpdates.CheckWorkflow().Francis()), workflow.WithSkipIf("resume", true)),
			workflow.Step("plan", engine.Handler(s.planAutoUpdate)),
			workflow.Step("apply", engine.Handler(s.applyAutoUpdate),
				workflow.WithMaxAttempts(5), workflow.WithRetryBackoff(30*time.Second, 5*time.Minute)),
			workflow.Step("finalize", engine.Handler(s.finalizeAutoUpdate)),
		},
	})
	if err != nil {
		return err
	}
	s.autoUpdate = autoUpdate
	return s.execution.RegisterWorkflows(engine)
}

// AutoUpdateWorkflow is the durable auto-update workflow.
func (s *UpdaterService) AutoUpdateWorkflow() *flow.Workflow { return s.autoUpdate }

// autoUpdateCandidates selects the monitored images of auto-update eligible containers.
func (s *UpdaterService) autoUpdateCandidates(ctx context.Context, t flow.Task) (any, error) {
	used, err := s.CollectUsedImages(ctx)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	monitored, err := s.deps.ImageUpdates.MonitoredImageRefs(ctx, slices.Sorted(maps.Keys(used)))
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	return t.ChildInput(imageupdatetypes.CheckRequest{ImageRefs: monitored})
}

// autoUpdateResume reports whether the run already holds a frozen plan.
func (s *UpdaterService) autoUpdateResume(ctx context.Context, _ flow.Task) (any, error) {
	run, ok := jobcontext.Run(ctx)
	return ok && s.recovery.Planned(run), nil
}

// planAutoUpdate freezes the update plan without the images and containers whose check failed.
func (s *UpdaterService) planAutoUpdate(ctx context.Context, t flow.Task) (any, error) {
	run, ok := jobcontext.Run(ctx)
	if !ok {
		return nil, errors.New("auto-update requires a job run")
	}
	var checked scheduler.Outcome
	if err := t.DecodeOutput("check", &checked); err != nil && !errors.Is(err, workflow.ErrStepSkipped) {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, target := range checked.Targets {
		// Failed checks are run evidence, so a retry or recovery that skips the check still reports them.
		if err := jobcontext.Progress(ctx, target); err != nil {
			return nil, common.Classify(common.ErrUnavailable, err)
		}
		switch target.ResourceType {
		case "image":
			excluded[refs.NormalizeImageUpdateRef(target.ID)] = true
		case "container":
			excluded[target.ID] = true
		}
	}
	if _, err := s.recovery.FreezePending(contextWithActivityID(ctx, t.ActivityID()), run, excluded); err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	return nil, nil
}

// applyAutoUpdate confirms every frozen target on each delivery before
// applying the ones still unchanged, so redelivery never repeats a Docker change.
func (s *UpdaterService) applyAutoUpdate(ctx context.Context, t flow.Task) (any, error) {
	run, ok := jobcontext.Run(ctx)
	if !ok {
		return nil, errors.New("auto-update requires a job run")
	}
	ctx, release, err := s.acquireUpdate(ctx)
	if errors.Is(err, errUpdateBusy) {
		// A plan whose every target already settled needs no updater, so it finishes normally.
		if unsettled, unsettledErr := s.recovery.Unsettled(run); unsettledErr == nil && unsettled == 0 {
			return autoUpdateApplied{Completed: true}, nil
		}
		// Waiting would hold this run's activity slot against the update holding the updater, so the run ends now:
		// a plan that already started applying stays retryable, and an untouched one skips as the job always has.
		return autoUpdateApplied{Busy: true, Started: s.recovery.Started(run)}, nil
	}
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	defer release()
	start := time.Now()
	ctx = contextWithActivityID(ctx, t.ActivityID())
	progress := &updateProgress{}
	ctx = context.WithValue(ctx, updateProgressKey{}, progress)

	ctx, remaining, unresolved, err := s.recovery.ResumePlan(ctx, run)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	applied := autoUpdateApplied{Unresolved: unresolved, Completed: true}
	if remaining > 0 {
		if applyErr := s.applyBatch(ctx, arcaneupdater.Options{}, &applied.Result); applyErr != nil {
			// A shutdown hands the task back so the redelivery finishes the remaining frozen targets.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			applied.Error, applied.Completed = applyErr.Error(), false
		}
	}
	applied.Result.Items = nil
	applied.Result.Duration = time.Since(start).String()
	progress.mu.Lock()
	if progress.err != nil {
		applied.RecordingError = progress.err.Error()
	}
	applied.SelfTriggered, applied.SelfID = progress.selfTriggered, progress.selfID
	progress.mu.Unlock()
	return applied, nil
}

// finalizeAutoUpdate records the batch checkpoint and returns the run outcome.
func (s *UpdaterService) finalizeAutoUpdate(ctx context.Context, t flow.Task) (any, error) {
	var applied autoUpdateApplied
	if err := t.DecodeOutput("apply", &applied); err != nil {
		return nil, err
	}
	activityID := t.ActivityID()
	ctx = contextWithActivityID(ctx, activityID)
	if applied.Busy {
		status, message := scheduler.Skipped, "Another update is active"
		if applied.Started {
			status, message = scheduler.Failed, "Another update is active; the remaining targets were not updated"
		}
		batch := scheduler.TargetOutcome{ID: "auto-update", ResourceType: "update-batch", Status: status, ActivityID: activityID, Message: message}
		if err := jobcontext.Progress(ctx, batch); err != nil {
			return nil, common.Classify(common.ErrUnavailable, err)
		}
		return scheduler.Outcome{Status: status, Message: message, ActivityID: activityID}, nil
	}
	progress := &updateProgress{selfTriggered: applied.SelfTriggered, selfID: applied.SelfID}
	if applied.RecordingError != "" {
		progress.err = errors.New(applied.RecordingError)
	}
	var applyErr error
	if applied.Error != "" {
		applyErr = errors.New(applied.Error)
	}
	result := &applied.Result
	// Containers that could not be planned never reached apply, so they count as failed here.
	run, _ := jobcontext.Run(ctx)
	unplanned := s.recovery.Unplanned(run)
	result.Failed += len(unplanned)
	err := progress.completeBatch(ctx, arcaneupdater.Options{}, result, applied.Completed, applyErr)
	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"restarted", result.Restarted,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
	outcome := scheduler.Outcome{Status: scheduler.Succeeded, Message: "Auto-update run completed", ActivityID: activityID}
	if result.Failed > 0 || err != nil {
		outcome.Status = scheduler.Partial
		outcome.Message = "Some updates failed"
		if err != nil {
			outcome.Message += ": " + err.Error()
		}
	}
	outcome.Targets = append(outcome.Targets, unplanned...)
	if applied.Unresolved {
		outcome.Status = scheduler.Failed
		outcome.Message = "Some target outcomes could not be confirmed"
	}
	if outcomeErr, ok := errors.AsType[*scheduler.OutcomeError](err); ok {
		outcome.Status = outcomeErr.Outcome.Status
		outcome.Message = outcomeErr.Outcome.Message
	}
	return outcome, nil
}

// ValidateAutoUpdateRetry allows a retry only while frozen targets remain unsettled.
func (s *UpdaterService) ValidateAutoUpdateRetry(_ context.Context, run scheduler.Run) error {
	unsettled, err := s.recovery.Unsettled(run)
	if err != nil {
		return fmt.Errorf("auto-update has no frozen plan to retry: %w", err)
	}
	if unsettled == 0 {
		return errors.New("auto-update has no unsettled targets to retry")
	}
	return nil
}

// ReconcilePending resumes only frozen targets whose original container is unchanged.
func (s *UpdaterService) ReconcilePending(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	return s.recovery.ReconcilePending(ctx, run)
}
