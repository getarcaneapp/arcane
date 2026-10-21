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

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
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

var (
	errUpdateBusy = common.Classify(common.ErrConflict, errors.New("another container update is running"))

	containerEventTypes = map[string]event.EventType{
		"container_stop":   event.EventTypeContainerStop,
		"container_delete": event.EventTypeContainerDelete,
		"container_create": event.EventTypeContainerCreate,
		"container_start":  event.EventTypeContainerStart,
		"container_update": event.EventTypeContainerUpdate,
	}

	updateLabelPolicy = updater.LabelPolicy{
		IsUpdateDisabledFunc: func(l map[string]string) bool {
			return labels.IsUpdateDisabled(l) || types.QuadletUnit(l) != ""
		},
	}
)

// UpdaterService is Arcane's handler-facing service for the standalone updater engine.
type UpdaterService struct {
	admission  *runs.Admission
	deps       updaterDependencies
	engine     *updater.Service
	execution  *execution.Service
	recovery   *recovery.Service
	autoUpdate *flow.Workflow
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
	SystemUser             types.Actor
}

type selfUpgradeService interface {
	// TriggerUpgradeViaCLI returns the spawned upgrader container's ID, which this
	// service does not need — only update-all's manager step uses it.
	TriggerUpgradeViaCLI(ctx context.Context, user types.Actor, target updater.SelfUpdateTarget) (string, error)
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
	scopedPendingKey               struct{}

	updateProgress struct {
		mu            sync.Mutex
		err           error
		selfTriggered bool
		selfID        string
	}

	// autoUpdateApplied is the apply step's output, read back by finalize.
	autoUpdateApplied struct {
		Result         types.UpdaterResult `json:"result"`
		Error          string              `json:"error,omitempty"`
		RecordingError string              `json:"recordingError,omitempty"`
		SelfTriggered  bool                `json:"selfTriggered,omitempty"`
		SelfID         string              `json:"selfId,omitempty"`
		Unresolved     bool                `json:"unresolved,omitempty"`
		Completed      bool                `json:"completed"`
		// Busy means another update held the updater, so this run applied nothing; Started means an earlier
		// delivery had begun applying its plan.
		Busy    bool `json:"busy,omitempty"`
		Started bool `json:"started,omitempty"`
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
			SystemUser:             types.SystemUser,
		},
	}
	// keep: recovery callbacks bind to the allocated service
	service.recovery = recovery.NewService(
		localDocker,
		imageSvc,
		service.PendingImageUpdates,
		activityIDFromContext,
		func(ctx context.Context, ids []string) (context.Context, func(), error) {
			return service.acquireUpdate(ctx, ids, false)
		},
		service.ApplyPending,
	)
	// keep: execution callbacks bind to the allocated service and its recovery
	service.execution = execution.NewService(execution.Dependencies{
		Config: cfg,
		Roles:  roles,
		AcquireUpdate: func(ctx context.Context, ids []string) (context.Context, func(), error) {
			return service.acquireUpdate(ctx, ids, false)
		},
		UpdateBusy:       errUpdateBusy,
		RunUpdate:        service.runSingleContainerUpdate,
		RecordUpdate:     service.recordSingleContainerUpdate,
		FreezeContainer:  service.recovery.FreezeContainer,
		ConfirmTarget:    service.recovery.ConfirmTarget,
		ResumeTarget:     service.recovery.ResumeTarget,
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
		RegistryDigestResolver: registries,
		RegistryTagLister:      service,
		ProjectUpdater:         service,
		SelfUpdater:            service,
		Notifier:               service,
		EventRecorder:          service,
		UsedImageCollector:     updater.UsedImageCollectorFunc(service.CollectUsedImages),
		LabelPolicy:            updateLabelPolicy,
		SelfContainerID:        kit.Ternary(selfErr != nil, "", selfContainerID),
		OperationTimeout:       timeouts.DefaultAutoUpdateOperation,
		Logger:                 slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("configure updater engine: %w", err)
	}
	// keep: the engine is configured with the allocated service
	service.engine = engine
	events.SetDockerUpdatingContainers(func() []string { return engine.Status().ContainerIDs })
	return service, nil
}

// ApplyPending executes pending image updates. A project or image scope limits the engine's
// pending records to its containers; a container scope or a forced run updates each container.
func (s *UpdaterService) ApplyPending(ctx context.Context, options types.UpdaterOptions) (out *types.UpdaterResult, err error) {
	containerIDs, err := s.pendingContainerIDs(ctx, options)
	if err != nil {
		return nil, err
	}
	var release func()
	ctx, release, err = s.acquireUpdate(ctx, containerIDs, true)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	batchCompleted := false
	started, startErr := s.deps.Activity.StartActivity(ctx, activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          types.ActivityTypeAutoUpdate,
		Queue:         true,
		ResourceType:  new("system"),
		ResourceName:  new("Auto update"),
		Step:          "Planning updates",
		LatestMessage: "Auto-update run started",
		Metadata:      database.JSON{"dryRun": options.DryRun},
	})
	if startErr != nil {
		slog.DebugContext(ctx, "failed to start auto-update activity", "error", startErr)
	}
	activityID := kit.FromPtr(started).ID
	if activityID != "" {
		ctx = contextWithActivityID(s.deps.Activity.Track(ctx, activityID), activityID)
	}
	out = &types.UpdaterResult{Items: []types.UpdaterResourceResult{}}
	progress := &updateProgress{}
	ctx = context.WithValue(ctx, updateProgressKey{}, progress)

	defer func() {
		// keep: stamps the named result on every return path
		out.Duration = cmp.Or(out.Duration, time.Since(start).String())
		// keep: stamps the named result on every return path
		out.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
		err = progress.completeBatch(ctx, options, out, batchCompleted, err)
		s.completeAutoUpdateActivity(ctx, activityID, "Auto-update run completed", func() string {
			switch {
			case err != nil:
				return err.Error()
			case out.Failed > 0:
				return fmt.Sprintf("%d update action(s) failed", out.Failed)
			}
			return ""
		}())
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
// as one batch, capped by a whole-run timeout.
func (s *UpdaterService) applyBatch(ctx context.Context, options types.UpdaterOptions, out *types.UpdaterResult) error {
	batch := &containerUpdateBatch{}
	ctx = context.WithValue(ctx, containerUpdateBatchContextKey{}, batch)
	defer func() {
		batch.Lock()
		entries := batch.entries
		batch.Unlock()
		if len(entries) == 0 {
			return
		}
		notifyCtx, cancelNotify := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultHTTPClient)
		defer cancelNotify()
		if err := s.deps.Notifications.SendBatchContainerUpdateNotification(notifyCtx, entries); err != nil {
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
		if moduleResult == nil {
			return cmp.Or(engineErr, errors.New("updater returned no batch result"))
		}
		*out = *resultFromModule(moduleResult)
		s.logResultItems(ctx, out)
		if engineErr != nil {
			return engineErr
		}
	}

	if !options.DryRun {
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

func (s *UpdaterService) pendingContainerIDs(ctx context.Context, options types.UpdaterOptions) ([]string, error) {
	if len(options.ResourceIds) > 0 {
		containerIDs, _, _, _, err := s.scopedContainerIDs(ctx, options)
		return containerIDs, err
	}
	if frozen, ok := s.recovery.FrozenRecords(ctx); ok {
		return s.recordContainerIDs(ctx, frozen)
	}
	var records []imageupdate.ImageUpdateRecord
	if err := s.deps.DB.WithContext(ctx).Select("container_id", "repository", "tag").Where("has_update = ? AND project_id = ?", true, "").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query pending image updates: %w", err)
	}
	pending := make([]updater.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		pending = append(pending, updater.ImageUpdateRecord{
			ContainerID: record.ContainerID,
			Repository:  record.Repository,
			Tag:         record.Tag,
		})
	}
	return s.recordContainerIDs(ctx, pending)
}

func (s *UpdaterService) recordContainerIDs(ctx context.Context, records []updater.ImageUpdateRecord) ([]string, error) {
	containerIDs := make([]string, 0, len(records))
	imageRefs := make([]string, 0, len(records))
	for _, record := range records {
		if record.ContainerID != "" {
			containerIDs = append(containerIDs, record.ContainerID)
			continue
		}
		imageRefs = append(imageRefs, record.ImageRef())
	}
	imageContainerIDs, _, _, _, err := s.scopedContainerIDs(ctx, types.UpdaterOptions{Type: "image", ResourceIds: imageRefs})
	if err != nil {
		return nil, err
	}
	return kit.Unique(kit.TrimNonEmpty(slices.Concat(containerIDs, imageContainerIDs))), nil
}

// scopedContainerIDs resolves the requested containers, projects, or images to container IDs.
func (s *UpdaterService) scopedContainerIDs(ctx context.Context, options types.UpdaterOptions) ([]string, map[string]string, map[string]string, map[string]bool, error) {
	requested := kit.TrimNonEmpty(options.ResourceIds)
	scope := strings.ToLower(strings.TrimSpace(options.Type))
	switch {
	case len(requested) == 0, scope == "", scope == "container":
		return requested, nil, nil, nil, nil
	case scope != "project" && scope != "image":
		return nil, nil, nil, nil, fmt.Errorf("unsupported scoped update type %q", options.Type)
	}
	// Projects match by lowercase Compose project name; images by ID or normalized reference.
	wanted := make(map[string]bool, len(requested)*2)
	localServices := map[string]string{}
	projectIDs := map[string]string{}
	for _, ref := range requested {
		if scope == "image" {
			wanted[ref], wanted[refs.NormalizeImageUpdateRef(ref)] = true, true
			continue
		}
		name, discovered := strings.CutPrefix(ref, "compose:")
		if !discovered {
			if project, lookupErr := s.deps.Projects.GetProjectFromDatabaseByID(ctx, ref); lookupErr == nil && project != nil {
				name = cmp.Or(strings.TrimSpace(kit.FromPtr(project.ComposeProjectName)), strings.TrimSpace(project.Name), name)
			}
		}
		projectKey := strings.ToLower(strings.TrimSpace(name))
		wanted[projectKey] = true
		if discovered {
			continue
		}
		details, detailsErr := s.deps.Projects.GetProjectDetails(ctx, ref, types.ProjectDetailsOptions{IncludeServiceConfigs: true})
		if detailsErr != nil {
			continue
		}
		projectIDs[projectKey] = ref
		for _, svc := range details.Services {
			localServices[projectKey+"/"+svc.Name] = cmp.Or(
				kit.Ternary(svc.Build != nil, "locally built image", ""),
				kit.Ternary(projects.ResolveServiceImagePullMode(svc).Mode == projects.ImagePullModeNever, "pull_policy: never", ""),
			)
		}
	}
	delete(wanted, "")
	containers, _, _, _, listErr := s.deps.Docker.GetAllContainers(ctx)
	if listErr != nil {
		return nil, nil, nil, nil, fmt.Errorf("list containers: %w", listErr)
	}
	matches := func(summary container.Summary) bool {
		return docker.ComposeServiceLabel(summary.Labels) != "" && wanted[strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(summary.Labels)))]
	}
	if scope == "image" {
		matches = func(summary container.Summary) bool {
			configured, _ := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
				return imageref.ConfiguredImage(ctx, dockerClient, summary), nil
			})
			return wanted[summary.ImageID] || wanted[refs.NormalizeImageUpdateRef(configured)]
		}
	}
	var matched []string
	skips, checkRecords, running := map[string]string{}, map[string]string{}, map[string]bool{}
	for _, summary := range containers {
		if !matches(summary) {
			continue
		}
		projectKey := strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(summary.Labels)))
		matched = append(matched, summary.ID)
		skips[summary.ID] = localServices[projectKey+"/"+docker.ComposeServiceLabel(summary.Labels)]
		checkRecords[summary.ID] = kit.Ternary(projectIDs[projectKey] != "", "project::"+projectIDs[projectKey]+"::"+docker.ComposeServiceLabel(summary.Labels), "")
		running[summary.ID] = summary.State == container.StateRunning
	}
	return matched, skips, checkRecords, running, nil
}

// applyScopedUpdates applies running containers of a project or image scope through the engine's grouped
// pending path, and stopped ones, a container scope, or a forced run through the single-container path.
func (s *UpdaterService) applyScopedUpdates(ctx context.Context, options types.UpdaterOptions, out *types.UpdaterResult) error {
	containerIDs, skipReasons, checkRecords, running, err := s.scopedContainerIDs(ctx, options)
	if err != nil {
		return err
	}
	if len(containerIDs) == 0 {
		return common.ErrUpdaterNoContainersMatched
	}

	var (
		items                                        = out.Items
		checked, updated, restarted, skipped, failed int
		engineErrs                                   []error
		completed                                    bool
		pending                                      []string
	)
	defer func() {
		*out = types.UpdaterResult{ //exhaustruct:enforce
			Success:    kit.Ternary(completed, out.Failed+failed == 0, out.Success),
			Checked:    out.Checked + checked,
			Updated:    out.Updated + updated,
			Restarted:  out.Restarted + restarted,
			Skipped:    out.Skipped + skipped,
			Failed:     out.Failed + failed,
			StartTime:  out.StartTime,
			EndTime:    out.EndTime,
			Duration:   out.Duration,
			Items:      items,
			ActivityID: out.ActivityID,
		}
	}()
	projectOrImage := slices.Contains([]string{"project", "image"}, strings.ToLower(strings.TrimSpace(options.Type)))
	scoped := make(map[string]string, len(containerIDs))
	for _, containerID := range containerIDs {
		reason := skipReasons[containerID]
		switch {
		case reason == "" && projectOrImage && !options.ForceUpdate && running[containerID]:
			scoped[containerID] = checkRecords[containerID]
			continue
		case reason == "":
			pending = append(pending, containerID)
			continue
		}
		checked++
		skipped++
		items = append(items, types.UpdaterResourceResult{
			ResourceID:   containerID,
			ResourceType: "container",
			Status:       types.UpdaterStatusSkipped,
			Error:        reason,
		})
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           containerID,
			ResourceType: "container",
			Status:       types.RunStatusSkipped,
			Message:      reason,
		}); progressErr != nil {
			return progressErr
		}
	}

	if len(scoped) > 0 {
		moduleResult, engineErr := s.engine.ApplyPending(context.WithValue(ctx, scopedPendingKey{}, scoped), updater.Options{DryRun: options.DryRun})
		if moduleResult == nil {
			return cmp.Or(engineErr, errors.New("updater returned no batch result"))
		}
		partial := resultFromModule(moduleResult)
		checked += partial.Checked
		updated += partial.Updated
		restarted += partial.Restarted
		skipped += partial.Skipped
		failed += partial.Failed
		items = append(items, partial.Items...)
		if engineErr != nil {
			engineErrs = append(engineErrs, engineErr)
		}
	}

	for index, containerID := range pending {
		if ctx.Err() != nil {
			for _, remainingID := range pending[index:] {
				failed++
				items = append(items, types.UpdaterResourceResult{
					ResourceID:   remainingID,
					ResourceType: "container",
					Status:       types.UpdaterStatusFailed,
					Error:        fmt.Sprintf("update deadline exceeded before update: %v", ctx.Err()),
				})
				if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
					ID:           remainingID,
					ResourceType: "container",
					Status:       types.RunStatusFailed,
					Message:      fmt.Sprintf("update deadline exceeded before update: %v", ctx.Err()),
				}); progressErr != nil {
					return errors.Join(progressErr, errors.Join(engineErrs...))
				}
			}
			return errors.Join(append(engineErrs, ctx.Err())...)
		}
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           containerID,
			ResourceType: "container",
			Status:       types.RunStatusRunning,
		}); progressErr != nil {
			return progressErr
		}
		moduleResult, engineErr := s.engine.UpdateContainer(ctx, containerID, updater.Options{Force: options.ForceUpdate, DryRun: options.DryRun})
		partial := resultFromModule(moduleResult)
		checked += partial.Checked
		updated += partial.Updated
		restarted += partial.Restarted
		skipped += partial.Skipped
		failed += partial.Failed
		items = append(items, partial.Items...)
		if projectOrImage && errors.Is(engineErr, updater.ErrContainerNotFound) {
			skipped++
			items = append(items, types.UpdaterResourceResult{
				ResourceID:   containerID,
				ResourceType: "container",
				Status:       types.UpdaterStatusSkipped,
				Error:        "recreated by an earlier service update",
			})
			if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
				ID:           containerID,
				ResourceType: "container",
				Status:       types.RunStatusSkipped,
				Message:      "recreated by an earlier service update",
			}); progressErr != nil {
				return errors.Join(progressErr, errors.Join(engineErrs...))
			}
			continue
		}
		if engineErr != nil {
			failed++
			items = append(items, types.UpdaterResourceResult{
				ResourceID:   containerID,
				ResourceType: "container",
				Status:       types.UpdaterStatusFailed,
				Error:        engineErr.Error(),
			})
			engineErrs = append(engineErrs, fmt.Errorf("%s: %w", containerID, engineErr))
		}
		if progressErr := jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           containerID,
			ResourceType: "container",
			Status: func() types.RunStatus {
				switch {
				case engineErr != nil, partial.Failed > 0:
					return types.RunStatusFailed
				case moduleResult == nil:
					return types.RunStatusNeedsAttention
				case partial.Skipped > 0:
					return types.RunStatusSkipped
				}
				return types.RunStatusSucceeded
			}(),
			Message: func() string {
				if engineErr == nil {
					return ""
				}
				return engineErr.Error()
			}(),
		}); progressErr != nil {
			return errors.Join(progressErr, errors.Join(engineErrs...))
		}
	}
	s.logResultItems(ctx, &types.UpdaterResult{Items: items})
	completed = true
	// Like the unscoped engine error, these surface after every container was attempted.
	return errors.Join(engineErrs...)
}

// UpdateSingleContainer updates a single container by ID to the latest available image.
func (s *UpdaterService) UpdateSingleContainer(ctx context.Context, containerID string) (out *types.UpdaterResult, err error) {
	started, workCtx, err := s.deps.Activity.StartTrackedActivity(ctx, s.singleContainerActivityRequest(ctx, containerID))
	if err != nil {
		return nil, fmt.Errorf("start container update activity: %w", err)
	}
	defer func() {
		s.finishSingleContainerUpdate(workCtx, started.ID, out, err)
	}()
	defer utils.RecoverToError(&err, "single container update")
	return s.runSingleContainerUpdate(workCtx, containerID, started.ID)
}

// AcceptSingleContainerUpdate submits a cancellable container-update workflow and returns its activity.
func (s *UpdaterService) AcceptSingleContainerUpdate(ctx context.Context, containerID string) (*types.Activity, error) {
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

func (s *UpdaterService) runSingleContainerUpdate(ctx context.Context, containerID, activityID string) (out *types.UpdaterResult, err error) {
	var release func()
	ctx, release, err = s.acquireUpdate(ctx, []string{containerID}, true)
	if err != nil {
		return nil, err
	}
	defer release()
	start := time.Now()
	out = &types.UpdaterResult{Items: []types.UpdaterResourceResult{}}
	defer func() {
		// keep: stamps the named result on every return path
		out.Duration = cmp.Or(out.Duration, time.Since(start).String())
		// keep: stamps the named result on every return path
		out.ActivityID = mo.EmptyableToOption(activityID).ToPointer()
	}()
	ctx = contextWithActivityID(ctx, activityID)
	if activityID != "" {
		if slotErr := s.deps.Activity.AwaitActivitySlotBounded(ctx, activityID, "0"); slotErr != nil {
			return out, slotErr
		}
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, ctxErr
	}

	// The caller picked this container, so the autoUpdateExcludedContainers setting and the
	// updater opt-out label do not apply here; immutable references still do.
	moduleResult, err := s.engine.UpdateContainer(ctx, containerID, updater.Options{IgnoreSettingsExclusions: true})
	if moduleResult != nil {
		out = resultFromModule(moduleResult)
		s.logResultItems(ctx, out)
	}
	return out, err
}

// GetStatus returns the current in-memory update activity snapshot.
func (s *UpdaterService) GetStatus() types.UpdaterStatus {
	status := s.engine.Status()
	return types.UpdaterStatus{
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
func (s *UpdaterService) RestartContainersUsingOldIDs(ctx context.Context, oldIDToNewRef, oldRefToNewRef map[string]string) ([]types.UpdaterResourceResult, error) {
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
	title, subject := func() (string, string) {
		phase := kit.As(metadata["phase"], "")
		switch phase {
		case "start":
			return "Auto-update run started", ""
		case "complete":
			return "Auto-update run completed", ""
		case "image_pull", "image":
			return "Auto-update: image pull", cmp.Or(kit.ToString(metadata["imageNew"]), kit.ToString(metadata["imageOld"]))
		case "image_prune":
			return "Auto-update: image prune", kit.ToString(metadata["imageId"])
		case "container":
			return "Auto-update: container", cmp.Or(kit.ToString(metadata["resourceName"]), kit.ToString(metadata["container"]), kit.ToString(metadata["containerId"]))
		case "project":
			return "Auto-update: project", cmp.Or(kit.ToString(metadata["projectName"]), kit.ToString(metadata["projectId"]))
		}
		return kit.Ternary(phase != "", "Auto-update: "+phase, "Auto-update"), ""
	}()
	_, err := s.deps.Events.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeSystemAutoUpdate,
		Severity:      severity,
		Title:         title + kit.Ternary(subject != "", " "+subject, ""),
		ResourceType:  new("system"),
		ResourceName:  new("auto_updater"),
		EnvironmentID: new("0"),
		Metadata:      metadata,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to record auto-update event", "error", err)
	}
}

// DockerClient implements updater.DockerClientProvider for the updater engine.
func (s *UpdaterService) DockerClient(ctx context.Context) (*client.Client, error) {
	return s.deps.Docker.GetClient(ctx)
}

// PullImage pulls an image through Arcane's image service, which bounds it by the
// dockerImagePullTimeout setting so a stuck pull cannot hold the auto-update run.
func (s *UpdaterService) PullImage(ctx context.Context, imageRef string, progress io.Writer) error {
	pulledRef, err := s.recovery.PreparePull(ctx, imageRef)
	if err != nil {
		return err
	}
	writer := activitylib.NewWriter(ctx, s.deps.Activity, activityIDFromContext(ctx), progress, "Pulling updated images")
	defer activitylib.FlushWriter(writer)

	credentials, err := s.deps.Projects.ResolveRegistryCredentials(ctx)
	if err != nil {
		return fmt.Errorf("resolve registry credentials: %w", err)
	}
	return s.deps.ImagePuller.Pull(ctx, image.PullRequest{
		Ref:         pulledRef,
		Tag:         imageRef,
		Progress:    writer,
		User:        s.deps.SystemUser,
		Credentials: credentials,
	})
}

// PendingImageUpdates returns pending image update records from Arcane's database.
func (s *UpdaterService) PendingImageUpdates(ctx context.Context) ([]updater.ImageUpdateRecord, error) {
	records, err := s.loadPendingImageUpdates(ctx)
	if err != nil {
		return nil, err
	}
	if _, batch := ctx.Value(updateAdmissionKey{}).(*updateLeases); !batch || len(records) == 0 {
		return records, nil
	}
	containers, _, _, _, err := s.deps.Docker.GetAllContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	dockerClient, err := s.deps.Docker.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	keysFor := updateAdmissionKeys(containers)
	configured := map[string]string{}
	if slices.ContainsFunc(records, func(record updater.ImageUpdateRecord) bool { return record.ContainerID == "" }) {
		for _, summary := range containers {
			configured[summary.ID] = refs.NormalizeImageUpdateRef(imageref.ConfiguredImage(ctx, dockerClient, summary))
		}
	}
	admitted := make([]updater.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		containerIDs, matchErr := func() ([]string, error) {
			if record.ContainerID != "" {
				return []string{record.ContainerID}, nil
			}
			if record.ImageRef() == "" {
				return nil, nil
			}
			inspected, inspectErr := dockerClient.ImageInspect(ctx, record.ImageRef())
			if inspectErr != nil && !errdefs.IsNotFound(inspectErr) {
				return nil, fmt.Errorf("inspect pending image %s: %w", record.ImageRef(), inspectErr)
			}
			oldIDs := kit.TrimNonEmpty([]string{inspected.ID, record.ID})
			normalized := refs.NormalizeImageUpdateRef(record.ImageRef())
			var matched []string
			for _, summary := range containers {
				if slices.Contains(oldIDs, summary.ImageID) || (normalized != "" && configured[summary.ID] == normalized) {
					matched = append(matched, summary.ID)
				}
			}
			return matched, nil
		}()
		if matchErr != nil {
			return nil, matchErr
		}
		_, _, acquireErr := s.admitUpdate(ctx, keysFor(containerIDs), false)
		if errors.Is(acquireErr, errUpdateBusy) {
			slog.InfoContext(ctx, "skipping pending update for a container another update holds", "imageRef", record.ImageRef(), "containerId", record.ContainerID)
			continue
		}
		if acquireErr != nil {
			return nil, acquireErr
		}
		admitted = append(admitted, record)
	}
	return admitted, nil
}

func (s *UpdaterService) loadPendingImageUpdates(ctx context.Context) ([]updater.ImageUpdateRecord, error) {
	scoped, isScoped := ctx.Value(scopedPendingKey{}).(map[string]string)
	outOfScope := func(record updater.ImageUpdateRecord) bool {
		_, inScope := scoped[record.ContainerID]
		return isScoped && !inScope
	}
	if records, ok := s.recovery.FrozenRecords(ctx); ok {
		return slices.DeleteFunc(records, outOfScope), nil
	}
	var records []imageupdate.ImageUpdateRecord
	if err := s.deps.DB.WithContext(ctx).Where("has_update = ? AND project_id = ?", true, "").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query pending image updates: %w", err)
	}

	// Flush "Updates Available" notifications before the engine clears these records (#3132);
	// a flush that cannot decide what to send aborts so the records survive for retry.
	if err := s.deps.ImageUpdates.SendBatchUpdateNotifications(ctx); err != nil {
		return nil, err
	}
	s.appendAutoUpdateActivityMessage(ctx, activityIDFromContext(ctx), fmt.Sprintf("Found %d pending image update records", len(records)), "Planning updates", 10)

	// Once container-scoped records exist, an unscoped record applies per running non-tag-policy container of its image.
	var scopedCount int64
	if err := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("container_id <> ?", "").Limit(1).Count(&scopedCount).Error; err != nil {
		return nil, err
	}
	// Resolve each container's policy and normalized configured ref once, not per record.
	type eligibleContainer struct {
		id  string
		ref string
	}
	eligible, err := func() ([]eligibleContainer, error) {
		if scopedCount == 0 && !isScoped {
			return nil, nil
		}
		listed, listErr := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
			return dockerClient.ContainerList(ctx, client.ContainerListOptions{})
		})
		if listErr != nil {
			return nil, listErr
		}
		matched := make([]eligibleContainer, 0, len(listed.Items))
		for _, cnt := range listed.Items {
			configured, _ := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
				return imageref.ConfiguredImage(ctx, dockerClient, cnt), nil
			})
			resolved, policyErr := tagpolicy.Resolve(configured, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
			if policyErr != nil || resolved.Strategy == "tag" {
				continue
			}
			matched = append(matched, eligibleContainer{id: cnt.ID, ref: refs.NormalizeImageUpdateRef(configured)})
		}
		return matched, nil
	}()
	if err != nil {
		return nil, err
	}
	converted := func(record imageupdate.ImageUpdateRecord, containerID string) updater.ImageUpdateRecord {
		return updater.ImageUpdateRecord{
			ContainerID:    containerID,
			ID:             record.ID,
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
	projectChecks, err := func() ([]imageupdate.ImageUpdateRecord, error) {
		if !isScoped {
			return nil, nil
		}
		var found []imageupdate.ImageUpdateRecord
		if findErr := s.deps.DB.WithContext(ctx).Where("has_update = ? AND id IN ?", true, kit.Unique(kit.TrimNonEmpty(slices.Collect(maps.Values(scoped))))).Find(&found).Error; findErr != nil {
			return nil, fmt.Errorf("query project update checks: %w", findErr)
		}
		return found, nil
	}()
	if err != nil {
		return nil, err
	}
	out := make([]updater.ImageUpdateRecord, 0, len(records)+len(scoped))
	covered := map[string]bool{}
	for _, record := range projectChecks {
		for _, containerID := range slices.Sorted(maps.Keys(scoped)) {
			if scoped[containerID] == record.ID {
				covered[containerID] = true
				out = append(out, converted(record, containerID))
			}
		}
	}
	for _, record := range records {
		if (scopedCount == 0 && !isScoped) || record.ContainerID != "" {
			out = append(out, converted(record, record.ContainerID))
			continue
		}
		recordRef := refs.NormalizeImageUpdateRef(converted(record, "").ImageRef())
		for _, cnt := range eligible {
			if cnt.ref == recordRef {
				out = append(out, converted(record, cnt.id))
			}
		}
	}
	return slices.DeleteFunc(out, func(record updater.ImageUpdateRecord) bool {
		return outOfScope(record) || (covered[record.ContainerID] && record.ID != scoped[record.ContainerID])
	}), nil
}

// ClearImageUpdateRecord clears a pending image update record after it is handled.
func (s *UpdaterService) ClearImageUpdateRecord(ctx context.Context, record updater.ImageUpdateRecord) error {
	query := s.deps.DB.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{})
	if record.ContainerID == "" || strings.HasPrefix(record.ID, "container::") {
		query = query.Where("container_id = ?", record.ContainerID)
		if record.ID != "" {
			return query.Where("id = ?", record.ID).Updates(map[string]any{"has_update": false, "latest_seen_at": nil}).Error
		}
		return query.Where("repository = ? AND tag = ?", record.Repository, record.Tag).Updates(map[string]any{"has_update": false, "latest_seen_at": nil}).Error
	}

	// A record stays pending while another container it covers still runs an older image.
	rest, isProject := strings.CutPrefix(record.ID, "project::")
	projectID, service, _ := strings.Cut(rest, "::")
	composeName, err := func() (string, error) {
		if !isProject {
			return "", nil
		}
		project, lookupErr := s.deps.Projects.GetProjectFromDatabaseByID(ctx, projectID)
		if lookupErr != nil {
			return "", fmt.Errorf("resolve project %s: %w", projectID, lookupErr)
		}
		return strings.ToLower(cmp.Or(strings.TrimSpace(kit.FromPtr(kit.FromPtr(project).ComposeProjectName)), strings.TrimSpace(kit.FromPtr(project).Name))), nil
	}()
	if err != nil {
		return err
	}
	target, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
		return dockerClient.ImageInspect(ctx, record.NewImageRef())
	})
	if err != nil {
		return err
	}
	listed, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	})
	if err != nil {
		return err
	}
	for _, cnt := range listed.Items {
		if isProject && (docker.ComposeServiceLabel(cnt.Labels) != service || strings.ToLower(strings.TrimSpace(docker.ComposeProjectLabel(cnt.Labels))) != composeName) {
			continue
		}
		// Untagged images list by ID, so an orphaned container matches by its configured image.
		configured, _ := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (string, error) {
			return imageref.ConfiguredImage(ctx, dockerClient, cnt), nil
		})
		resolved, policyErr := tagpolicy.Resolve(configured, updater.DefaultLabelPolicy().TagPolicy(cnt.Labels))
		if !isProject && (policyErr != nil || resolved.Strategy == "tag") {
			continue
		}
		if refs.NormalizeImageUpdateRef(configured) == refs.NormalizeImageUpdateRef(record.ImageRef()) && cnt.ImageID != target.ID {
			return nil
		}
	}
	return query.Where("id = ? AND container_id = ?", record.ID, "").Updates(map[string]any{"has_update": false, "latest_seen_at": nil}).Error
}

// ExcludedContainers returns auto-update exclusions from Arcane settings.
func (s *UpdaterService) ExcludedContainers(ctx context.Context) ([]string, error) {
	return kit.Unique(kit.TrimNonEmpty(strings.Split(s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""), ","))), nil
}

// ProjectByComposeName resolves an Arcane project from a Docker Compose project name.
func (s *UpdaterService) ProjectByComposeName(ctx context.Context, composeName string) (updater.ComposeProject, error) {
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
	return s.deps.Projects.UpdateProjectServices(ctx, projectID, services, s.deps.SystemUser, false)
}

// TriggerSelfUpdate runs Arcane's CLI-backed self-update hook.
func (s *UpdaterService) TriggerSelfUpdate(ctx context.Context, target updater.SelfUpdateTarget) error {
	// The frozen digest is only what gets pulled; the recreated container keeps
	// the selected tag so later runs do not treat it as an immutable reference.
	pullImageRef, pullErr := func() (string, error) {
		if target.NewImageRef == "" {
			return target.PullImageRef, nil
		}
		return s.recovery.PreparePull(ctx, target.NewImageRef)
	}()
	if pullErr != nil {
		return pullErr
	}
	containers, _, _, _, listErr := s.deps.Docker.GetAllContainers(ctx)
	if listErr != nil {
		return fmt.Errorf("list containers: %w", listErr)
	}
	stopsProcess := target.InstanceType != "agent"
	keys := updateAdmissionKeys(containers)(func() []string {
		if !stopsProcess {
			return []string{target.ContainerID}
		}
		allIDs := make([]string, 0, len(containers))
		for _, summary := range containers {
			allIDs = append(allIDs, summary.ID)
		}
		return allIDs
	}())
	ctx, release, acquireErr := s.admitUpdate(ctx, keys, false)
	if errors.Is(acquireErr, errUpdateBusy) {
		return common.Classify(common.ErrConflict, errors.New("another container update is in progress; the self-update stays pending for the next run"))
	}
	if acquireErr != nil {
		return acquireErr
	}
	defer release()

	// A server self-update stops this process before the run completes, so the
	// activity is flagged first and startup reconciliation finalizes it.
	if activityID := activityIDFromContext(ctx); stopsProcess && activityID != "" {
		s.appendAutoUpdateActivityMessage(
			ctx,
			activityID,
			"Self-update initiated — Arcane will restart"+kit.Ternary(target.NewImageRef != "", " with "+target.NewImageRef, ""),
			"Self-update",
			90,
		)
		if err := s.deps.Activity.PatchActivityMetadata(ctx, activityID, database.JSON{"selfUpdateTriggered": true}); err != nil {
			slog.DebugContext(ctx, "failed to mark self-update on activity", "activityId", activityID, "error", err)
		}
	}

	upgraderID, err := s.deps.SelfUpgrade.TriggerUpgradeViaCLI(ctx, s.deps.SystemUser, updater.SelfUpdateTarget{ //exhaustruct:enforce
		ContainerID:   target.ContainerID,
		ContainerName: target.ContainerName,
		InstanceType:  target.InstanceType,
		Labels:        target.Labels,
		NewImageRef:   target.NewImageRef,
		PullImageRef:  pullImageRef,
	})
	if err != nil {
		return fmt.Errorf("CLI upgrade failed: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeouts.DefaultDeployWait)
		defer cancel()
		if stopsProcess || upgraderID == "" {
			<-waitCtx.Done()
			return
		}
		dockerClient, clientErr := s.deps.Docker.GetClient(waitCtx)
		if clientErr != nil {
			slog.WarnContext(waitCtx, "cannot watch agent upgrader; holding its update keys until the deploy wait ends", "upgraderId", upgraderID, "error", clientErr)
			<-waitCtx.Done()
			return
		}
		wait := dockerClient.ContainerWait(waitCtx, upgraderID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
		select {
		case <-wait.Result:
		case waitErr := <-wait.Error:
			if !errdefs.IsNotFound(waitErr) {
				slog.WarnContext(waitCtx, "agent upgrader wait failed; holding its update keys until the deploy wait ends", "upgraderId", upgraderID, "error", waitErr)
				<-waitCtx.Done()
			}
		}
	}()
	if leases, ok := ctx.Value(updateAdmissionKey{}).(*updateLeases); ok {
		leases.mu.Lock()
		for _, key := range kit.Ternary(stopsProcess, leases.keys, keys) {
			leases.retain[key] = done
		}
		leases.mu.Unlock()
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
	if eventType, ok := containerEventTypes[evt.Phase]; ok {
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
	return kit.As(ctx.Value(activityIDContextKey{}), "")
}

// singleContainerActivityRequest describes a container update's activity, named after the container when it can be inspected.
func (s *UpdaterService) singleContainerActivityRequest(ctx context.Context, containerID string) activitylib.StartRequest {
	name := func() string {
		lookupCtx, cancelLookup := context.WithTimeout(ctx, 2*time.Second)
		defer cancelLookup()
		inspected, inspectErr := s.deps.Docker.Call(lookupCtx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
		})
		if inspectErr != nil {
			return containerID
		}
		return cmp.Or(strings.TrimPrefix(inspected.Container.Name, "/"), containerID)
	}()
	return activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          types.ActivityTypeAutoUpdate,
		Queue:         true,
		DeferSlot:     true,
		ResourceType:  new("container"),
		ResourceID:    &containerID,
		ResourceName:  &name,
		StartedBy: func() *types.Actor {
			initiator, ok := utils.UpdateInitiatorFromContext(ctx)
			if !ok {
				user, _ := userctx.CurrentUserFromContext(ctx)
				return user
			}
			return &types.Actor{
				ID:          initiator.UserID,
				Username:    initiator.Username,
				DisplayName: mo.EmptyableToOption(initiator.DisplayName).ToPointer(),
			}
		}(),
		Step:          "Updating container",
		LatestMessage: "Container update started",
		Metadata:      database.JSON{"containerID": containerID},
	}
}

func (s *UpdaterService) finishSingleContainerUpdate(ctx context.Context, activityID string, result *types.UpdaterResult, runErr error) {
	if activityID == "" {
		return
	}
	outcome := s.recordSingleContainerUpdate(ctx, activityID, result, runErr)
	s.completeAutoUpdateActivity(ctx, activityID, outcome.Message, kit.Ternary(outcome.Status != types.RunStatusSucceeded, outcome.Message, ""))
}

// recordSingleContainerUpdate saves a container update's result on its activity and summarizes it as an outcome.
func (s *UpdaterService) recordSingleContainerUpdate(ctx context.Context, activityID string, result *types.UpdaterResult, runErr error) types.SchedulerOutcome {
	item := func() types.UpdaterResourceResult {
		if items := kit.FromPtr(result).Items; len(items) > 0 {
			return items[0]
		}
		return types.UpdaterResourceResult{}
	}()
	metadata := func() database.JSON {
		if result == nil {
			return database.JSON{}
		}
		return database.JSON{
			"updated":   result.Updated,
			"restarted": result.Restarted,
			"skipped":   result.Skipped,
			"failed":    result.Failed,
		}
	}()
	if len(kit.FromPtr(result).Items) > 0 {
		metadata["updateOutcome"] = item.Status
	}
	if item.ResourceName != "" {
		metadata["containerName"] = item.ResourceName
	}
	if item.Error != "" {
		metadata["updateReason"] = item.Error
	}
	if activityID != "" {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if item.ResourceName != "" {
			if _, err := s.deps.Activity.UpdateActivity(writeCtx, activityID, activitylib.UpdateRequest{ResourceName: &item.ResourceName}); err != nil {
				slog.WarnContext(writeCtx, "failed to update container activity name", "activityId", activityID, "error", err)
			}
		}
		if err := s.deps.Activity.PatchActivityMetadata(writeCtx, activityID, metadata); err != nil {
			slog.WarnContext(writeCtx, "failed to persist container update result", "activityId", activityID, "error", err)
		}
	}
	if runErr == nil && result != nil && result.Failed == 0 {
		return types.SchedulerOutcome{
			Status: types.RunStatusSucceeded,
			Message: func() string {
				switch {
				case result.Updated > 0 || result.Restarted > 0:
					return "Container updated"
				case item.Status == string(updater.StatusSkipped) && item.Error != "":
					return "Container update skipped: " + item.Error
				case result.Skipped > 0:
					return "Container update skipped"
				}
				return "Container already current"
			}(),
		}
	}
	return types.SchedulerOutcome{
		Status: types.RunStatusFailed,
		Message: func() string {
			switch {
			case runErr != nil:
				return runErr.Error()
			case result == nil:
				return "container update produced no result"
			case item.Error != "":
				return item.Error
			}
			return fmt.Sprintf("%d update action(s) failed", result.Failed)
		}(),
	}
}

func (s *UpdaterService) appendAutoUpdateActivityMessage(ctx context.Context, activityID, message, step string, progress int) {
	if activityID == "" {
		return
	}
	if _, err := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{
		Level:    types.ActivityMessageLevelInfo,
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
	if activityID == "" {
		return
	}
	status, completion, errMessage := func() (types.ActivityStatus, string, *string) {
		switch {
		case failure == "":
			return types.ActivityStatusSuccess, message, nil
		case activitylib.CancelledByContext(ctx):
			return types.ActivityStatusCancelled, "Auto-update cancelled", nil
		}
		return types.ActivityStatusFailed, failure, &failure
	}()
	// A lost terminal write strands the activity as running, so it is written past cancellation and logged loudly.
	if _, err := s.deps.Activity.CompleteActivity(context.WithoutCancel(ctx), activityID, status, completion, errMessage); err != nil {
		slog.ErrorContext(ctx, "failed to complete auto-update activity", "activityId", activityID, "error", err)
	}
}

// resultFromModule converts an engine result to Arcane's wire type, which carries times as
// strings and images as maps holding only the "main" entry. Callers set ActivityID.
func resultFromModule(result *updater.Result) *types.UpdaterResult {
	if result == nil {
		return &types.UpdaterResult{Items: []types.UpdaterResourceResult{}}
	}
	mainImage := func(ref string) map[string]string {
		if ref == "" {
			return nil
		}
		return map[string]string{"main": ref}
	}
	items := make([]types.UpdaterResourceResult, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, types.UpdaterResourceResult{
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
	return &types.UpdaterResult{
		Success:   result.Success,
		Checked:   result.Checked,
		Updated:   result.Updated,
		Restarted: result.Restarted,
		Skipped:   result.Skipped,
		Failed:    result.Failed,
		StartTime: kit.Ternary(result.StartTime.IsZero(), "", result.StartTime.UTC().Format(time.RFC3339)),
		EndTime:   kit.Ternary(result.EndTime.IsZero(), "", result.EndTime.UTC().Format(time.RFC3339)),
		Duration:  result.Duration().String(),
		Items:     items,
	}
}

func (s *UpdaterService) logResultItems(ctx context.Context, result *types.UpdaterResult) {
	for _, item := range result.Items {
		s.recordAutoUpdateEvent(ctx, cmp.Or(map[string]event.EventSeverity{
			string(updater.StatusFailed):  event.EventSeverityError,
			string(updater.StatusUpdated): event.EventSeveritySuccess,
		}[item.Status], event.EventSeverityInfo), database.JSON{
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

	if err := s.collectUsedImagesFromProjects(ctx, out); err != nil {
		errs = append(errs, err)
		slog.DebugContext(ctx, "collectUsedImages: failed collecting from projects", "error", err)
	} else {
		successfulSources++
	}

	if successfulSources == 0 {
		return nil, errors.Join(errs...)
	}

	slog.DebugContext(ctx, "collectUsedImages: collected used images", "count", len(out))
	return out, nil
}

func (s *UpdaterService) collectUsedImagesFromContainers(ctx context.Context, out map[string]struct{}) error {
	excludedContainers := docker.ExcludedContainerNameSet(s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""))
	listResult, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerListResult, error) {
		return dockerClient.ContainerList(ctx, client.ContainerListOptions{})
	})
	if err != nil {
		return err
	}

	// Containers sharing an image add the same tags, so each image is inspected once.
	inspectedImages := map[string]struct{}{}
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
		inspectResult, inspectErr := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ContainerInspectResult, error) {
			return compat.ContainerInspectWithCompatibility(ctx, dockerClient, summary.ID, client.ContainerInspectOptions{})
		})
		if inspectErr != nil {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect failed", "containerId", summary.ID, "error", inspectErr)
			continue
		}
		if labels.IsUpdateDisabled(kit.FromPtr(inspectResult.Container.Config).Labels) {
			slog.DebugContext(ctx, "collectUsedImagesFromContainers: container inspect labels opted out", "containerId", summary.ID)
			continue
		}
		if _, inspected := inspectedImages[inspectResult.Container.Image]; !inspected {
			inspectedImages[inspectResult.Container.Image] = struct{}{}
			if imageInspect, imageErr := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ImageInspectResult, error) {
				return dockerClient.ImageInspect(ctx, inspectResult.Container.Image)
			}); imageErr == nil {
				for _, tag := range imageInspect.RepoTags {
					if strings.TrimSpace(tag) != "" && tag != "<none>:<none>" {
						addNormalizedImageUpdateRef(ctx, out, tag, "normalizedTagsForContainer repo tag", "imageId", inspectResult.Container.Image)
					}
				}
			}
		}
		if configImage := kit.FromPtr(inspectResult.Container.Config).Image; configImage != "" {
			addNormalizedImageUpdateRef(ctx, out, configImage, "normalizedTagsForContainer config image", "imageId", inspectResult.Container.Image)
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
		if project.IsArchived ||
			(project.Status != projectpkg.ProjectStatusRunning && project.Status != projectpkg.ProjectStatusPartiallyRunning) ||
			name == "" {
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

	composeContainers, err := s.deps.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
		return projects.ListGlobalComposeContainers(ctx, dockerClient, s.deps.Docker.DockerHost())
	})
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

func (s *UpdaterService) PruneHistory(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	result := s.deps.DB.WithContext(ctx).
		Where("start_time < ?", time.Now().Add(-time.Duration(retentionDays)*24*time.Hour)).
		Delete(&AutoUpdateRecord{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to prune auto-update history: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result updater.ResourceResult) error {
	now := time.Now()
	recordErr := s.deps.DB.WithContext(ctx).Create(&AutoUpdateRecord{
		ResourceID:       result.ResourceID,
		ResourceType:     string(result.ResourceType),
		ResourceName:     result.ResourceName,
		Status:           AutoUpdateStatus(result.Status),
		StartTime:        now,
		EndTime:          &now,
		UpdateAvailable:  result.UpdateAvailable || result.Status == updater.StatusUpdated || result.Status == updater.StatusUpdateAvailable,
		UpdateApplied:    result.UpdateApplied,
		OldImageVersions: kit.Ternary(result.OldImage != "", database.JSON{"main": result.OldImage}, nil),
		NewImageVersions: kit.Ternary(result.NewImage != "", database.JSON{"main": result.NewImage}, nil),
		Error:            mo.EmptyableToOption(result.Error).ToPointer(),
		Details:          kit.Ternary(len(result.Details) > 0, maps.Clone(result.Details), nil),
	}).Error
	name := cmp.Or(strings.TrimSpace(result.ResourceName), result.ResourceID)
	status := cmp.Or(map[updater.ResourceStatus]types.RunStatus{
		updater.StatusUpdated:   types.RunStatusSucceeded,
		updater.StatusRestarted: types.RunStatusSucceeded,
		updater.StatusUpToDate:  types.RunStatusSucceeded,
		updater.StatusSkipped:   types.RunStatusSkipped,
		updater.StatusFailed:    types.RunStatusFailed,
	}[result.Status], types.RunStatusNeedsAttention)
	activityID := activityIDFromContext(ctx)
	if activityID != "" {
		if _, appendErr := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{
			Level: kit.Ternary(result.Status == updater.StatusFailed, types.ActivityMessageLevelError, types.ActivityMessageLevelInfo),
			Message: func() string {
				reason := strings.TrimSpace(result.Error)
				if result.Status == updater.StatusFailed && reason != "" {
					return name + ": " + reason
				}
				return name + ": " + string(result.Status) +
					kit.Ternary(
						(result.Status == updater.StatusUpdated || result.Status == updater.StatusRestarted) &&
							result.OldImage != "" && result.NewImage != "" && result.OldImage != result.NewImage,
						" ("+result.OldImage+" -> "+result.NewImage+")",
						"",
					) +
					kit.Ternary(reason != "", " ("+reason+")", "")
			}(),
			Step: "Applying updates",
		}); appendErr != nil {
			slog.DebugContext(ctx, "failed to append update result activity message", "activityId", activityID, "resource", name, "error", appendErr)
		}
	}
	evidenceErr := func() error {
		if status != types.RunStatusSucceeded {
			return nil
		}
		return s.recovery.VerifyResult(ctx, result.ResourceID)
	}()
	err := errors.Join(recordErr, jobcontext.Progress(ctx, types.TargetOutcome{
		ID:           result.ResourceID,
		ResourceType: string(result.ResourceType),
		Status:       kit.Ternary(evidenceErr != nil, types.RunStatusNeedsAttention, status),
		Message: func() string {
			if evidenceErr != nil {
				return evidenceErr.Error()
			}
			return result.Error
		}(),
		ActivityID: activityID,
	}), evidenceErr)
	if progress, ok := ctx.Value(updateProgressKey{}).(*updateProgress); ok && err != nil {
		progress.mu.Lock()
		progress.err = errors.Join(progress.err, err)
		progress.mu.Unlock()
	}
	return err
}

func (p *updateProgress) completeBatch(ctx context.Context, options types.UpdaterOptions, out *types.UpdaterResult, batchCompleted bool, err error) error {
	activityID := activityIDFromContext(ctx)
	p.mu.Lock()
	err = errors.Join(err, p.err)
	recordingFailed := p.err != nil || (!batchCompleted && err == nil)
	previous, durableRun := jobcontext.Run(ctx)
	selfTriggered := p.selfTriggered && durableRun
	selfID := p.selfID
	p.mu.Unlock()
	if !batchCompleted && err == nil {
		err = errors.New("update batch ended without a confirmed result")
	}
	status := func() types.RunStatus {
		switch {
		case recordingFailed || selfTriggered:
			return types.RunStatusNeedsAttention
		case err != nil || out.Failed > 0:
			return types.RunStatusPartial
		}
		return types.RunStatusSucceeded
	}()
	if selfTriggered {
		err = errors.Join(err, jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           selfID,
			ResourceType: "container",
			Status:       types.RunStatusNeedsAttention,
			ActivityID:   activityID,
			Message:      "Self-update completion requires review",
		}), errors.New("self-update was triggered but completion requires review"))
	}
	checkpointErr := jobcontext.Progress(ctx, types.TargetOutcome{
		ID: "auto-update",
		ResourceType: func() string {
			switch {
			case !batchCompleted:
				return "update-interrupted"
			// A scoped rerun of a durable run is a retry unless that run already recorded a finished batch.
			case durableRun && len(options.ResourceIds) > 0 && !slices.ContainsFunc(previous.Outcome.Targets, func(target types.TargetOutcome) bool {
				return target.ID == "auto-update" && target.ResourceType == "update-batch" && (target.Status == types.RunStatusSucceeded || target.Status == types.RunStatusPartial)
			}):
				return "update-retry"
			}
			return "update-batch"
		}(),
		Status: status,
		Message: func() string {
			if err == nil {
				return ""
			}
			return err.Error()
		}(),
		ActivityID: activityID,
	})
	err = errors.Join(err, checkpointErr)
	if err != nil {
		// keep: the caller's result reports the failed batch
		out.Success = false
	}
	if recordingFailed || selfTriggered || checkpointErr != nil {
		return &types.SchedulerOutcomeError{
			Outcome: types.SchedulerOutcome{
				Status:     types.RunStatusNeedsAttention,
				Message:    err.Error(),
				ActivityID: activityID,
			},
			Cause: err,
		}
	}
	return err
}

// ListTags supplies Arcane's registry credentials to the updater's tag checker.
func (s *UpdaterService) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	credentials, err := s.deps.Projects.ResolveRegistryCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve registry credentials: %w", err)
	}
	return s.deps.RegistryDigestResolver.ListImageTags(ctx, imageRef, credentials)
}

// UpdateServiceImages persists and redeploys the selected Compose service images.
func (s *UpdaterService) UpdateServiceImages(ctx context.Context, projectID string, changes map[string]updatertypes.ServiceImageChange) error {
	s.appendAutoUpdateActivityMessage(ctx, activityIDFromContext(ctx), "Updating Compose image references", "Persisting image updates", 50)
	return s.deps.Projects.UpdateProjectServiceImages(ctx, projectID, changes, s.deps.SystemUser)
}

// CheckProjectUpdates checks effective Compose service policies without deploying containers.
func (s *UpdaterService) CheckProjectUpdates(ctx context.Context, projectID string) (*types.ProjectUpdateInfo, error) {
	details, err := s.deps.Projects.GetProjectDetails(ctx, projectID, types.ProjectDetailsOptions{IncludeServiceConfigs: true})
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

func (s *UpdaterService) checkProjectService(ctx context.Context, projectID string, service composetypes.ServiceConfig) imageupdate.ImageUpdateRecord {
	checkTime := time.Now().UTC()
	parsed, err := refs.NormalizeReference(service.Image)
	if err != nil {
		return imageupdate.ImageUpdateRecord{
			ProjectID:   projectID,
			ServiceName: service.Name,
			PolicyKey:   imageref.UpdatePolicyKey(service.Image, service.Labels),
			CheckTime:   checkTime,
			LastError:   new(err.Error()),
			ID:          "project::" + projectID + "::" + service.Name,
		}
	}
	check, checkErr := func() (updatertypes.CheckResult, error) {
		// Disabling automatic installation keeps the preview check; only the
		// update-check label opts a service out of monitoring.
		if service.Build != nil || imageref.IsUpdateCheckDisabled(service.Labels) {
			return updatertypes.CheckResult{}, nil
		}
		result, resultErr := s.engine.CheckImageUpdate(ctx, updatertypes.CheckRequest{
			ImageRef: service.Image,
			Policy:   updater.DefaultLabelPolicy().TagPolicy(service.Labels),
		})
		if resultErr != nil {
			return updatertypes.CheckResult{}, resultErr
		}
		return result, nil
	}()
	latestVersion, targetErr := func() (*string, error) {
		if check.TargetRef == "" {
			return nil, nil
		}
		target, parseErr := refs.NormalizeReference(check.TargetRef)
		if parseErr != nil {
			return nil, parseErr
		}
		return new(target.Tag), nil
	}()
	return imageupdate.ImageUpdateRecord{
		ProjectID:     projectID,
		ServiceName:   service.Name,
		PolicyKey:     imageref.UpdatePolicyKey(service.Image, service.Labels),
		CheckTime:     checkTime,
		LatestVersion: latestVersion,
		CurrentDigest: mo.EmptyableToOption(check.CurrentDigest).ToPointer(),
		LatestDigest:  mo.EmptyableToOption(check.TargetDigest).ToPointer(),
		LastError: func() *string {
			switch {
			case checkErr != nil && !errdefs.IsNotFound(checkErr):
				return new(checkErr.Error())
			case targetErr != nil:
				return new(targetErr.Error())
			}
			return nil
		}(),
		ID:         "project::" + projectID + "::" + service.Name,
		Repository: parsed.RegistryHost + "/" + parsed.Repository,
		Tag:        parsed.Tag,
		UpdateType: func() string {
			switch {
			case service.Build != nil:
				return imageupdate.UpdateTypeLocal
			case errdefs.IsNotFound(checkErr):
				return imageupdate.UpdateTypeNotPulled
			}
			return check.UpdateType
		}(),
		CurrentVersion: cmp.Or(check.CurrentVersion, parsed.Tag),
		HasUpdate:      check.UpdateAvailable && targetErr == nil,
	}
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
			Type:          types.ActivityTypeAutoUpdate,
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
	// keep: the workflow handle exists only once the shared service registers it
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
	return t.ChildInput(types.ImageUpdateCheckRequest{ImageRefs: monitored})
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
	var checked types.SchedulerOutcome
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
	deferred, err := s.autoUpdateDeferral(ctx, time.Now())
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	if _, freezeErr := s.recovery.FreezePending(contextWithActivityID(ctx, t.ActivityID()), run, excluded, deferred); freezeErr != nil {
		return nil, common.Classify(common.ErrUnavailable, freezeErr)
	}
	return nil, nil
}

// autoUpdateDeferral reports which pending updates are younger than their
// auto-update delay. The delay label wins over the environment setting, including 0,
// and age counts from when Arcane first saw the update.
func (s *UpdaterService) autoUpdateDeferral(ctx context.Context, now time.Time) (func(updater.ImageUpdateRecord, container.Summary) bool, error) {
	var seen []imageupdate.ImageUpdateRecord
	if err := s.deps.DB.WithContext(ctx).Select("id", "latest_seen_at").Where("has_update = ?", true).Find(&seen).Error; err != nil {
		return nil, err
	}
	seenAt := make(map[string]*time.Time, len(seen))
	for _, record := range seen {
		seenAt[record.ID] = record.LatestSeenAt
	}
	envDelay := s.deps.Settings.GetIntSetting(ctx, "autoUpdateDelay", 0)
	return func(record updater.ImageUpdateRecord, candidate container.Summary) bool {
		override, ok := imageref.UpdateDelay(candidate.Labels)
		delay := time.Duration(min(kit.Ternary(ok, override, envDelay), imageref.MaxUpdateDelayHours)) * time.Hour
		if delay <= 0 {
			return false
		}
		// A missing first-seen time counts as seen now, so the delay is never skipped.
		remaining := kit.FromPtr(cmp.Or(seenAt[record.ID], new(now))).Add(delay).Sub(now)
		if remaining <= 0 {
			return false
		}
		slog.InfoContext(ctx, "Deferring auto-update until the new image has been available for the configured delay",
			"containerId", candidate.ID, "imageRef", record.ImageRef(), "remaining", remaining.Round(time.Minute).String())
		return true
	}, nil
}

// applyAutoUpdate confirms every frozen target on each delivery before
// applying the ones still unchanged, so redelivery never repeats a Docker change.
func (s *UpdaterService) applyAutoUpdate(ctx context.Context, t flow.Task) (any, error) {
	run, ok := jobcontext.Run(ctx)
	if !ok {
		return nil, errors.New("auto-update requires a job run")
	}
	targetIDs, err := s.recovery.TargetRefs(run)
	if err != nil {
		return nil, common.Classify(common.ErrUnavailable, err)
	}
	ctx, release, err := s.acquireUpdate(ctx, targetIDs, false)
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
	var result types.UpdaterResult
	applyErr := func() error {
		if remaining == 0 {
			return nil
		}
		return s.applyBatch(ctx, types.UpdaterOptions{}, &result)
	}()
	// A shutdown hands the task back so the redelivery finishes the remaining frozen targets.
	if applyErr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	return autoUpdateApplied{
		Result: types.UpdaterResult{ //exhaustruct:enforce
			Success:    result.Success,
			Checked:    result.Checked,
			Updated:    result.Updated,
			Restarted:  result.Restarted,
			Skipped:    result.Skipped,
			Failed:     result.Failed,
			StartTime:  result.StartTime,
			EndTime:    result.EndTime,
			Duration:   time.Since(start).String(),
			Items:      nil,
			ActivityID: result.ActivityID,
		},
		Error: func() string {
			if applyErr == nil {
				return ""
			}
			return applyErr.Error()
		}(),
		RecordingError: func() string {
			if progress.err == nil {
				return ""
			}
			return progress.err.Error()
		}(),
		SelfTriggered: progress.selfTriggered,
		SelfID:        progress.selfID,
		Unresolved:    unresolved,
		Completed:     applyErr == nil,
	}, nil
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
		status := kit.Ternary(applied.Started, types.RunStatusFailed, types.RunStatusSkipped)
		message := kit.Ternary(applied.Started, "Another update is active; the remaining targets were not updated", "Another update is active")
		if err := jobcontext.Progress(ctx, types.TargetOutcome{
			ID:           "auto-update",
			ResourceType: "update-batch",
			Status:       status,
			ActivityID:   activityID,
			Message:      message,
		}); err != nil {
			return nil, common.Classify(common.ErrUnavailable, err)
		}
		return types.SchedulerOutcome{
			Status:     status,
			Message:    message,
			ActivityID: activityID,
		}, nil
	}
	// Containers that could not be planned never reached apply, so they count as failed here.
	run, _ := jobcontext.Run(ctx)
	unplanned := s.recovery.Unplanned(run)
	result := &types.UpdaterResult{ //exhaustruct:enforce
		Success:    applied.Result.Success,
		Checked:    applied.Result.Checked,
		Updated:    applied.Result.Updated,
		Restarted:  applied.Result.Restarted,
		Skipped:    applied.Result.Skipped,
		Failed:     applied.Result.Failed + len(unplanned),
		StartTime:  applied.Result.StartTime,
		EndTime:    applied.Result.EndTime,
		Duration:   applied.Result.Duration,
		Items:      applied.Result.Items,
		ActivityID: applied.Result.ActivityID,
	}
	err := (&updateProgress{
		err:           kit.Ternary(applied.RecordingError != "", errors.New(applied.RecordingError), nil),
		selfTriggered: applied.SelfTriggered,
		selfID:        applied.SelfID,
	}).completeBatch(ctx, types.UpdaterOptions{}, result, applied.Completed, kit.Ternary(applied.Error != "", errors.New(applied.Error), nil))
	slog.InfoContext(ctx, "auto-update run completed",
		"checked", result.Checked,
		"updated", result.Updated,
		"restarted", result.Restarted,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)
	status, message := func() (types.RunStatus, string) {
		if outcomeErr, ok := errors.AsType[*types.SchedulerOutcomeError](err); ok {
			return outcomeErr.Outcome.Status, outcomeErr.Outcome.Message
		}
		switch {
		case applied.Unresolved:
			return types.RunStatusFailed, "Some target outcomes could not be confirmed"
		case err != nil:
			return types.RunStatusPartial, "Some updates failed: " + err.Error()
		case result.Failed > 0:
			return types.RunStatusPartial, "Some updates failed"
		}
		return types.RunStatusSucceeded, "Auto-update run completed"
	}()
	return types.SchedulerOutcome{
		Status:     status,
		Message:    message,
		ActivityID: activityID,
		Targets:    unplanned,
	}, nil
}

// ValidateAutoUpdateRetry allows a retry only while frozen targets remain unsettled.
func (s *UpdaterService) ValidateAutoUpdateRetry(_ context.Context, run types.SchedulerRun) error {
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
func (s *UpdaterService) ReconcilePending(ctx context.Context, run types.SchedulerRun) (types.SchedulerOutcome, error) {
	return s.recovery.ReconcilePending(ctx, run)
}
