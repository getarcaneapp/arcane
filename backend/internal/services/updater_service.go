package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	projectspkg "github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"go.getarcane.app/sys/cgroup"
	moduleapi "go.getarcane.app/updater/api"
	updaterdigest "go.getarcane.app/updater/pkg/digest"
	"go.getarcane.app/updater/pkg/labels"
	"go.getarcane.app/updater/pkg/refs"
	moduletypes "go.getarcane.app/updater/types"
)

// UpdaterService is Arcane's handler-facing service for the standalone updater engine.
type UpdaterService struct {
	deps   updaterDependenciesInternal
	engine *moduleapi.Service
}

type updaterDependenciesInternal struct {
	DB                     *database.DB
	Docker                 *DockerClientService
	Settings               *SettingsService
	Projects               *ProjectService
	ImagePuller            *ImageService
	ImageUpdates           *ImageUpdateService
	RegistryDigestResolver *ContainerRegistryService
	Events                 *EventService
	Notifications          *NotificationService
	SelfUpgrade            selfUpgradeServiceInternal
	Activity               *ActivityService
	SystemUser             models.User
	Logger                 *slog.Logger
}

type selfUpgradeServiceInternal interface {
	TriggerUpgradeViaCLI(ctx context.Context, user models.User, target moduletypes.SelfUpdateTarget) error
}

// NewUpdaterService constructs the Arcane updater facade.
func NewUpdaterService(
	db *database.DB,
	settings *SettingsService,
	docker *DockerClientService,
	projects *ProjectService,
	imageUpdates *ImageUpdateService,
	registries *ContainerRegistryService,
	events *EventService,
	imageSvc *ImageService,
	notifications *NotificationService,
	upgrade selfUpgradeServiceInternal,
	activityService *ActivityService,
) *UpdaterService {
	service := &UpdaterService{
		deps: updaterDependenciesInternal{
			DB:                     db,
			Docker:                 docker,
			Settings:               settings,
			Projects:               projects,
			ImagePuller:            imageSvc,
			ImageUpdates:           imageUpdates,
			RegistryDigestResolver: registries,
			Events:                 events,
			Notifications:          notifications,
			SelfUpgrade:            upgrade,
			Activity:               activityService,
			SystemUser:             systemUser,
		},
	}
	service.engine = moduleapi.NewService(service.configInternal())
	return service
}

func (s *UpdaterService) configInternal() moduleapi.Config {
	return moduleapi.Config{
		DockerClientProvider:   s,
		ImagePuller:            s,
		PendingStore:           s,
		RunRecorder:            s,
		Settings:               s,
		RegistryDigestResolver: s.registryDigestResolverInternal(),
		ProjectUpdater:         s,
		SelfUpdater:            s,
		Notifier:               s,
		EventRecorder:          s,
		UsedImageCollector:     moduleapi.UsedImageCollectorFunc(s.CollectUsedImages),
		LabelPolicy:            labels.DefaultLabelPolicy(),
		SelfContainerID:        selfContainerIDInternal(),
		Logger:                 s.loggerInternal(),
	}
}

// selfContainerIDInternal returns the ID of the container Arcane runs in, so
// the updater engine routes it through the CLI self-updater even when the
// container is missing the Arcane labels. Empty when not running in Docker.
func selfContainerIDInternal() string {
	id, err := cgroup.CurrentContainerID()
	if err != nil {
		return ""
	}
	return id
}

func (s *UpdaterService) engineInternal() *moduleapi.Service {
	return s.engine
}

func (s *UpdaterService) loggerInternal() *slog.Logger {
	if s.deps.Logger != nil {
		return s.deps.Logger
	}
	return slog.Default()
}

func (s *UpdaterService) registryDigestResolverInternal() updaterdigest.RemoteResolver {
	if s == nil || s.deps.RegistryDigestResolver == nil {
		return nil
	}
	return s.deps.RegistryDigestResolver
}

// ApplyPending executes pending image updates.
func (s *UpdaterService) ApplyPending(ctx context.Context, options updater.Options) (out *updater.Result, err error) {
	start := time.Now()
	activityID := s.startAutoUpdateActivityInternal(ctx, options.DryRun)
	out = &updater.Result{Items: []updater.ResourceResult{}, ActivityID: utils.StringPtrFromTrimmed(activityID)}
	ctx = s.trackActivityInternal(ctx, activityID)
	ctx = contextWithActivityIDInternal(ctx, activityID)

	defer func() {
		if out == nil {
			out = &updater.Result{Items: []updater.ResourceResult{}}
		}
		if out.Duration == "" {
			out.Duration = time.Since(start).String()
		}
		out.ActivityID = utils.StringPtrFromTrimmed(activityID)
		s.completeAutoUpdateActivityInternal(ctx, activityID, out, err)
	}()

	s.recordAutoUpdateEventInternal(ctx, models.EventSeverityInfo, models.JSON{
		"phase":       "start",
		"dryRun":      options.DryRun,
		"forceUpdate": options.ForceUpdate,
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
	s.appendAutoUpdateActivityMessageInternal(ctx, activityID, "Planning pending updates", "Planning updates", 5)

	moduleResult, engineErr := s.engineInternal().ApplyPending(ctx, moduleOptionsFromUpdaterOptionsInternal(options))
	if moduleResult != nil {
		out = resultFromModuleInternal(moduleResult)
		out.ActivityID = utils.StringPtrFromTrimmed(activityID)
		s.logResultItemsInternal(ctx, out)
	}
	if engineErr != nil {
		err = engineErr
		return out, err
	}

	if !options.DryRun && s.deps.ImageUpdates != nil {
		s.appendAutoUpdateActivityMessageInternal(ctx, activityID, "Cleaning up update records", "Cleaning up", 95)
		if cleanupErr := s.deps.ImageUpdates.CleanupOrphanedRecords(ctx); cleanupErr != nil {
			s.loggerInternal().WarnContext(ctx, "cleanup orphaned update records failed", "error", cleanupErr)
		}
	}

	s.recordAutoUpdateEventInternal(ctx, models.EventSeverityInfo, models.JSON{
		"phase":     "complete",
		"checked":   out.Checked,
		"updated":   out.Updated,
		"restarted": out.Restarted,
		"skipped":   out.Skipped,
		"failed":    out.Failed,
		"duration":  out.Duration,
		"time":      time.Now().UTC().Format(time.RFC3339),
	})
	return out, nil
}

// UpdateSingleContainer updates a single container by ID to the latest available image.
func (s *UpdaterService) UpdateSingleContainer(ctx context.Context, containerID string) (out *updater.Result, err error) {
	start := time.Now()
	activityID := s.startSingleContainerUpdateActivityInternal(ctx, containerID)
	out = &updater.Result{Items: []updater.ResourceResult{}, ActivityID: utils.StringPtrFromTrimmed(activityID)}
	ctx = s.trackActivityInternal(ctx, activityID)
	ctx = contextWithActivityIDInternal(ctx, activityID)

	defer func() {
		if out == nil {
			out = &updater.Result{Items: []updater.ResourceResult{}}
		}
		if out.Duration == "" {
			out.Duration = time.Since(start).String()
		}
		out.ActivityID = utils.StringPtrFromTrimmed(activityID)
		s.completeAutoUpdateActivityInternal(ctx, activityID, out, err)
	}()

	moduleResult, engineErr := s.engineInternal().UpdateContainer(ctx, containerID, moduletypes.Options{})
	if moduleResult != nil {
		out = resultFromModuleInternal(moduleResult)
		out.ActivityID = utils.StringPtrFromTrimmed(activityID)
		s.logResultItemsInternal(ctx, out)
	}
	if engineErr != nil {
		err = engineErr
		return out, err
	}
	return out, nil
}

// GetStatus returns the current in-memory update activity snapshot.
func (s *UpdaterService) GetStatus() updater.Status {
	return statusFromModuleInternal(s.engineInternal().Status())
}

// GetHistory returns the most recent auto-update history records, newest first.
func (s *UpdaterService) GetHistory(ctx context.Context, limit int) ([]models.AutoUpdateRecord, error) {
	var records []models.AutoUpdateRecord
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
func (s *UpdaterService) RestartContainersUsingOldIDs(ctx context.Context, oldIDToNewRef map[string]string, oldRefToNewRef map[string]string) ([]updater.ResourceResult, error) {
	results, err := s.engineInternal().RestartContainersUsingOldImages(ctx, oldIDToNewRef, oldRefToNewRef)
	return resourceResultsFromModuleInternal(results), err
}

// TriggerSelfUpdateViaCLI triggers Arcane's detached CLI self-update path.
func (s *UpdaterService) TriggerSelfUpdateViaCLI(ctx context.Context, source, containerID, containerName string, labelMap map[string]string) error {
	if !labels.IsArcaneContainer(labelMap) {
		return fmt.Errorf("%s: container is not an Arcane self-update target", source)
	}
	return s.TriggerSelfUpdate(ctx, moduletypes.SelfUpdateTarget{
		ContainerID:   containerID,
		ContainerName: containerName,
		InstanceType:  instanceTypeFromLabelsInternal(labelMap),
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

func (s *UpdaterService) recordAutoUpdateEventInternal(ctx context.Context, severity models.EventSeverity, metadata models.JSON) {
	if s.deps.Events == nil {
		return
	}
	phase, _ := metadata["phase"].(string)
	_, err := s.deps.Events.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeSystemAutoUpdate,
		Severity:      severity,
		Title:         autoUpdateEventTitleInternal(phase, metadata),
		ResourceType:  utils.StringPtrFromTrimmed("system"),
		ResourceName:  utils.StringPtrFromTrimmed("auto_updater"),
		EnvironmentID: utils.StringPtrFromTrimmed("0"),
		Metadata:      metadata,
	})
	if err != nil {
		s.loggerInternal().DebugContext(ctx, "failed to record auto-update event", "error", err)
	}
}

func instanceTypeFromLabelsInternal(labelMap map[string]string) string {
	if labels.IsArcaneAgentContainer(labelMap) {
		return "agent"
	}
	return "server"
}

func autoUpdateEventTitleInternal(phase string, metadata models.JSON) string {
	switch phase {
	case "start":
		return "Auto-update run started"
	case "image_pull", "image":
		image := strings.TrimSpace(fmt.Sprint(metadata["imageNew"]))
		if image == "" {
			image = strings.TrimSpace(fmt.Sprint(metadata["imageOld"]))
		}
		if image != "" {
			return "Auto-update: image pull " + image
		}
		return "Auto-update: image pull"
	case "image_prune":
		imageID := strings.TrimSpace(fmt.Sprint(metadata["imageId"]))
		if imageID != "" {
			return "Auto-update: image prune " + imageID
		}
		return "Auto-update: image prune"
	case "container":
		name := strings.TrimSpace(fmt.Sprint(metadata["resourceName"]))
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(metadata["container"]))
		}
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(metadata["containerId"]))
		}
		if name != "" {
			return "Auto-update: container " + name
		}
		return "Auto-update: container"
	case "project":
		name := strings.TrimSpace(fmt.Sprint(metadata["projectName"]))
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(metadata["projectId"]))
		}
		if name != "" {
			return "Auto-update: project " + name
		}
		return "Auto-update: project"
	case "complete":
		return "Auto-update run completed"
	default:
		if phase != "" {
			return "Auto-update: " + phase
		}
		return "Auto-update"
	}
}

// DockerClient returns Arcane's configured Docker client for the updater engine.
func (s *UpdaterService) DockerClient(ctx context.Context) (*client.Client, error) {
	if s == nil || s.deps.Docker == nil {
		return nil, &common.UpdaterDockerServiceUnavailableError{}
	}
	return s.deps.Docker.GetClient(ctx)
}

// PullImage pulls an image through Arcane's image service.
func (s *UpdaterService) PullImage(ctx context.Context, imageRef string, progress io.Writer) error {
	if s == nil || s.deps.ImagePuller == nil {
		return &common.UpdaterImageServiceUnavailableError{}
	}
	activityID := activityIDFromContextInternal(ctx)
	writer := activitylib.NewWriter(ctx, s.deps.Activity, activityID, progress, "Pulling updated images")
	defer activitylib.FlushWriter(writer)

	if s.deps.Projects != nil {
		resolved, err := s.deps.Projects.resolveRegistryCredentialsInternal(ctx)
		if err != nil {
			return fmt.Errorf("resolve registry credentials: %w", err)
		}
		return s.deps.ImagePuller.PullImage(ctx, imageRef, writer, s.deps.SystemUser, resolved)
	}

	return s.deps.ImagePuller.PullImage(ctx, imageRef, writer, s.deps.SystemUser, nil)
}

// PendingImageUpdates returns pending image update records from Arcane's database.
func (s *UpdaterService) PendingImageUpdates(ctx context.Context) ([]moduletypes.ImageUpdateRecord, error) {
	if s == nil || s.deps.DB == nil {
		return nil, &common.UpdaterDatabaseUnavailableError{}
	}

	var records []models.ImageUpdateRecord
	if err := s.deps.DB.WithContext(ctx).Where("has_update = ?", true).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("query pending image updates: %w", err)
	}

	// Flush pending "Updates Available" notifications before the engine
	// consumes and clears these records, otherwise the notification for an
	// update applied here is silently lost (#3132).
	if s.deps.ImageUpdates != nil {
		s.deps.ImageUpdates.sendBatchImageUpdateNotificationsInternal(ctx)
	}
	s.appendAutoUpdateActivityMessageInternal(
		ctx,
		activityIDFromContextInternal(ctx),
		fmt.Sprintf("Found %d pending image update records", len(records)),
		"Planning updates",
		10,
	)

	out := make([]moduletypes.ImageUpdateRecord, 0, len(records))
	for _, record := range records {
		out = append(out, imageUpdateRecordToModuleInternal(record))
	}
	return out, nil
}

// ClearImageUpdateRecord clears a pending image update record after it is handled.
func (s *UpdaterService) ClearImageUpdateRecord(ctx context.Context, record moduletypes.ImageUpdateRecord) error {
	if s == nil {
		return &common.UpdaterServiceUnavailableError{}
	}
	return s.clearImageUpdateRecordForModuleInternal(ctx, record)
}

// RecordUpdateRun persists one updater resource result into Arcane history.
func (s *UpdaterService) RecordUpdateRun(ctx context.Context, result moduletypes.ResourceResult) error {
	if s == nil || s.deps.DB == nil {
		return nil
	}
	return s.recordRunInternal(ctx, resourceResultFromModuleInternal(result))
}

// ExcludedContainers returns auto-update exclusions from Arcane settings.
func (s *UpdaterService) ExcludedContainers(ctx context.Context) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	excluded := s.buildExcludedContainerSetInternal(ctx)
	if len(excluded) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(excluded))
	for name := range excluded {
		out = append(out, name)
	}
	return out, nil
}

// ProjectByComposeName resolves an Arcane project from a Docker Compose project name.
func (s *UpdaterService) ProjectByComposeName(ctx context.Context, composeName string) (moduletypes.ComposeProject, error) {
	if s == nil || s.deps.Projects == nil {
		return moduletypes.ComposeProject{}, &common.UpdaterProjectServiceUnavailableError{}
	}
	project, err := s.deps.Projects.GetProjectByComposeName(ctx, composeName)
	if err != nil {
		return moduletypes.ComposeProject{}, err
	}
	if project == nil {
		return moduletypes.ComposeProject{}, fmt.Errorf("compose project not found: %s", composeName)
	}
	return moduletypes.ComposeProject{ID: project.ID, Name: project.Name}, nil
}

// UpdateServices redeploys selected services through Arcane's project service.
func (s *UpdaterService) UpdateServices(ctx context.Context, projectID string, services []string) error {
	if s == nil || s.deps.Projects == nil {
		return &common.UpdaterProjectServiceUnavailableError{}
	}
	return s.deps.Projects.UpdateProjectServices(ctx, projectID, services, s.deps.SystemUser)
}

// TriggerSelfUpdate runs Arcane's CLI-backed self-update hook.
func (s *UpdaterService) TriggerSelfUpdate(ctx context.Context, target moduletypes.SelfUpdateTarget) error {
	if s == nil || s.deps.SelfUpgrade == nil {
		instanceType := strings.TrimSpace(target.InstanceType)
		if instanceType == "" {
			instanceType = "server"
		}
		return fmt.Errorf("%s self-update requires CLI upgrade service", instanceType)
	}

	// A server self-update stops this process before the run can complete its
	// activity, so annotate the activity first; startup reconciliation uses
	// the metadata flag to finalize it after the restart.
	if target.InstanceType != "agent" {
		s.markSelfUpdateTriggeredInternal(ctx, target)
	}

	if err := s.deps.SelfUpgrade.TriggerUpgradeViaCLI(ctx, s.deps.SystemUser, target); err != nil {
		return fmt.Errorf("CLI upgrade failed: %w", err)
	}
	return nil
}

func (s *UpdaterService) markSelfUpdateTriggeredInternal(ctx context.Context, target moduletypes.SelfUpdateTarget) {
	activityID := activityIDFromContextInternal(ctx)
	if s.deps.Activity == nil || activityID == "" {
		return
	}
	message := "Self-update initiated — Arcane will restart"
	if ref := strings.TrimSpace(target.NewImageRef); ref != "" {
		message = "Self-update initiated — Arcane will restart with " + ref
	}
	s.appendAutoUpdateActivityMessageInternal(ctx, activityID, message, "Self-update", 90)
	if err := s.deps.Activity.PatchActivityMetadata(ctx, activityID, models.JSON{"selfUpdateTriggered": true}); err != nil {
		slog.DebugContext(ctx, "failed to mark self-update on activity", "activityId", activityID, "error", err)
	}
}

// Notify sends Arcane's container update notification.
func (s *UpdaterService) Notify(ctx context.Context, notification moduletypes.Notification) error {
	if s == nil || s.deps.Notifications == nil {
		return nil
	}
	return s.deps.Notifications.SendContainerUpdateNotification(
		ctx,
		notification.ContainerName,
		notification.ImageRef,
		notification.OldImage,
		notification.NewImage,
	)
}

// RecordEvent records updater lifecycle events in Arcane's event stream.
func (s *UpdaterService) RecordEvent(ctx context.Context, event moduletypes.Event) error {
	if s == nil {
		return nil
	}

	eventType, ok := containerEventTypeInternal(event.Phase)
	if ok {
		if s.deps.Events == nil {
			return nil
		}
		return s.deps.Events.LogContainerEvent(
			ctx,
			eventType,
			event.ResourceID,
			event.ResourceName,
			s.deps.SystemUser.ID,
			s.deps.SystemUser.Username,
			"0",
			models.JSON(event.Metadata),
		)
	}

	severity := models.EventSeverityInfo
	if strings.EqualFold(event.Severity, "error") {
		severity = models.EventSeverityError
	}
	s.recordAutoUpdateEventInternal(ctx, severity, models.JSON{
		"phase":        event.Phase,
		"resourceId":   event.ResourceID,
		"resourceName": event.ResourceName,
		"resourceType": event.ResourceType,
		"time":         time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func containerEventTypeInternal(phase string) (models.EventType, bool) {
	switch phase {
	case "container_stop":
		return models.EventTypeContainerStop, true
	case "container_delete":
		return models.EventTypeContainerDelete, true
	case "container_create":
		return models.EventTypeContainerCreate, true
	case "container_start":
		return models.EventTypeContainerStart, true
	case "container_update":
		return models.EventTypeContainerUpdate, true
	default:
		return "", false
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
	activity, err := s.deps.Activity.StartActivity(ctx, activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeAutoUpdate,
		ResourceType:  utils.StringPtrFromTrimmed("system"),
		ResourceName:  utils.StringPtrFromTrimmed("Auto update"),
		Step:          "Planning updates",
		LatestMessage: "Auto-update run started",
		Metadata:      models.JSON{"dryRun": dryRun},
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start auto-update activity", "error", err)
		return ""
	}
	return activity.ID
}

func (s *UpdaterService) startSingleContainerUpdateActivityInternal(ctx context.Context, containerID string) string {
	if s.deps.Activity == nil {
		return ""
	}
	activity, err := s.deps.Activity.StartActivity(ctx, activitylib.StartRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeAutoUpdate,
		ResourceType:  utils.StringPtrFromTrimmed("container"),
		ResourceID:    &containerID,
		ResourceName:  utils.StringPtrFromTrimmed(containerID),
		Step:          "Updating container",
		LatestMessage: "Container update started",
		Metadata:      models.JSON{"containerID": containerID},
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start container update activity", "containerID", containerID, "error", err)
		return ""
	}
	return activity.ID
}

func (s *UpdaterService) appendAutoUpdateActivityMessageInternal(ctx context.Context, activityID, message, step string, progress int) {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return
	}
	if strings.TrimSpace(step) == "" {
		step = message
	}
	if _, err := s.deps.Activity.AppendMessage(ctx, activityID, activitylib.AppendMessageRequest{
		Level:    models.ActivityMessageLevelInfo,
		Message:  message,
		Progress: &progress,
		Step:     step,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append auto-update activity message", "activityId", activityID, "error", err)
	}
}

func (s *UpdaterService) completeAutoUpdateActivityInternal(ctx context.Context, activityID string, result *updater.Result, applyErr error) {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	status := models.ActivityStatusSuccess
	message := "Auto-update run completed"
	var errMessage *string
	if applyErr != nil {
		status = models.ActivityStatusFailed
		errText := applyErr.Error()
		errMessage = &errText
		message = errText
	} else if result != nil && result.Failed > 0 {
		status = models.ActivityStatusFailed
		errText := fmt.Sprintf("%d update action(s) failed", result.Failed)
		errMessage = &errText
		message = errText
	}
	if status == models.ActivityStatusFailed && activitylib.CancelledByContext(ctx) {
		status = models.ActivityStatusCancelled
		message = "Auto-update cancelled"
		errMessage = nil
	}

	if _, err := s.deps.Activity.CompleteActivity(utils.ActivityRuntimeContext(ctx, nil), activityID, status, message, errMessage); err != nil {
		slog.DebugContext(ctx, "failed to complete auto-update activity", "activityId", activityID, "error", err)
	}
}

func (s *UpdaterService) trackActivityInternal(ctx context.Context, activityID string) context.Context {
	if s.deps.Activity == nil || strings.TrimSpace(activityID) == "" {
		return ctx
	}
	return s.deps.Activity.Track(ctx, activityID)
}

func imageUpdateRecordToModuleInternal(record models.ImageUpdateRecord) moduletypes.ImageUpdateRecord {
	return moduletypes.ImageUpdateRecord{
		ID:             record.ID,
		Repository:     record.Repository,
		Tag:            record.Tag,
		HasUpdate:      record.HasUpdate,
		UpdateType:     record.UpdateType,
		CurrentVersion: record.CurrentVersion,
		LatestVersion:  record.LatestVersion,
		CurrentDigest:  record.CurrentDigest,
		LatestDigest:   record.LatestDigest,
		CheckTime:      record.CheckTime,
		LastError:      record.LastError,
	}
}

func moduleOptionsFromUpdaterOptionsInternal(options updater.Options) moduletypes.Options {
	return moduletypes.Options{
		Type:        options.Type,
		ResourceIDs: slices.Clone(options.ResourceIds),
		Force:       options.ForceUpdate,
		DryRun:      options.DryRun,
	}
}

func resultFromModuleInternal(result *moduletypes.Result) *updater.Result {
	if result == nil {
		return &updater.Result{Items: []updater.ResourceResult{}}
	}
	return &updater.Result{
		Success:    result.Success,
		Checked:    result.Checked,
		Updated:    result.Updated,
		Restarted:  result.Restarted,
		Skipped:    result.Skipped,
		Failed:     result.Failed,
		StartTime:  result.StartTime,
		EndTime:    result.EndTime,
		Duration:   result.Duration,
		Items:      resourceResultsFromModuleInternal(result.Items),
		ActivityID: result.ActivityID,
	}
}

func resourceResultsFromModuleInternal(results []moduletypes.ResourceResult) []updater.ResourceResult {
	out := make([]updater.ResourceResult, 0, len(results))
	for _, result := range results {
		out = append(out, resourceResultFromModuleInternal(result))
	}
	return out
}

func resourceResultFromModuleInternal(result moduletypes.ResourceResult) updater.ResourceResult {
	return updater.ResourceResult{
		ResourceID:      result.ResourceID,
		ResourceName:    result.ResourceName,
		ResourceType:    result.ResourceType,
		Status:          result.Status,
		UpdateAvailable: result.UpdateAvailable,
		UpdateApplied:   result.UpdateApplied,
		OldImages:       result.OldImages,
		NewImages:       result.NewImages,
		Error:           result.Error,
		Details:         result.Details,
	}
}

func statusFromModuleInternal(status moduletypes.Status) updater.Status {
	return updater.Status{
		UpdatingContainers: status.UpdatingContainers,
		UpdatingProjects:   status.UpdatingProjects,
		ContainerIds:       status.ContainerIDs,
		ProjectIds:         status.ProjectIDs,
	}
}

func (s *UpdaterService) recordRunInternal(ctx context.Context, item updater.ResourceResult) error {
	now := time.Now()
	record := &models.AutoUpdateRecord{
		ResourceID:       item.ResourceID,
		ResourceType:     item.ResourceType,
		ResourceName:     item.ResourceName,
		Status:           models.AutoUpdateStatus(item.Status),
		StartTime:        now,
		EndTime:          &now,
		UpdateAvailable:  item.UpdateAvailable || item.Status == moduletypes.StatusUpdated || item.Status == moduletypes.StatusUpdateAvailable,
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

func (s *UpdaterService) clearImageUpdateRecordForModuleInternal(ctx context.Context, record moduletypes.ImageUpdateRecord) error {
	if s.deps.DB == nil {
		return nil
	}

	query := s.deps.DB.WithContext(ctx).Model(&models.ImageUpdateRecord{})
	if strings.TrimSpace(record.ID) != "" {
		return query.Where("id = ?", record.ID).Update("has_update", false).Error
	}
	return query.Where("repository = ? AND tag = ?", record.Repository, record.Tag).Update("has_update", false).Error
}

func mapToJSONInternal(values map[string]string) models.JSON {
	if len(values) == 0 {
		return nil
	}
	out := make(models.JSON, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func detailsToJSONInternal(values map[string]any) models.JSON {
	if len(values) == 0 {
		return nil
	}
	out := make(models.JSON, len(values))
	maps.Copy(out, values)
	return out
}

func (s *UpdaterService) logResultItemsInternal(ctx context.Context, result *updater.Result) {
	if result == nil {
		return
	}
	for _, item := range result.Items {
		severity := models.EventSeverityInfo
		switch item.Status {
		case moduletypes.StatusFailed:
			severity = models.EventSeverityError
		case moduletypes.StatusUpdated:
			severity = models.EventSeveritySuccess
		}
		s.recordAutoUpdateEventInternal(ctx, severity, models.JSON{
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
		errs = append(errs, &common.UpdaterDockerServiceUnavailableError{})
	} else {
		dcli, err := s.deps.Docker.GetClient(ctx)
		if err != nil || dcli == nil {
			if err == nil {
				err = &common.UpdaterDockerClientUnavailableError{}
			}
			errs = append(errs, err)
			s.loggerInternal().DebugContext(ctx, "collectUsedImages: docker connection unavailable", "error", err)
		} else if err := s.collectUsedImagesFromContainersInternal(ctx, dcli, out); err != nil {
			errs = append(errs, err)
			s.loggerInternal().DebugContext(ctx, "collectUsedImages: failed collecting from containers", "error", err)
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

		if containerSummaryExcludedInternal(summary, excludedContainers) {
			s.loggerInternal().DebugContext(ctx, "collectUsedImagesFromContainers: skipping excluded container", "containerId", summary.ID, "names", summary.Names)
			continue
		}

		imageRef := strings.TrimSpace(summary.Image)
		if imageRef != "" && !refs.IsImageIDLikeReference(imageRef) {
			addNormalizedImageUpdateRefInternal(ctx, out, imageRef, "collectUsedImagesFromContainers: skipping invalid image reference", "containerId", summary.ID)
			continue
		}

		inspectResult, inspectErr := libarcane.ContainerInspectWithCompatibility(ctx, dcli, summary.ID, client.ContainerInspectOptions{})
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

func (s *UpdaterService) collectUsedImagesFromComposeContainersInternal(ctx context.Context, composeContainers []container.Summary, activeProjectNames map[string]struct{}, out map[string]struct{}) {
	for _, summary := range composeContainers {
		projectName := dockerutil.ComposeProjectLabel(summary.Labels)
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
		addNormalizedImageUpdateRefInternal(ctx, out, imageRef, "collectUsedImagesFromComposeContainers: skipping invalid image reference", "containerId", summary.ID)
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
				addNormalizedImageUpdateRefInternal(ctx, seen, tag, "normalizedTagsForContainer: skipping invalid repo tag", "imageId", inspect.Image)
			}
		}
	}

	if inspect.Config != nil && inspect.Config.Image != "" {
		addNormalizedImageUpdateRefInternal(ctx, seen, inspect.Config.Image, "normalizedTagsForContainer: skipping invalid config image reference", "imageId", inspect.Image)
	}

	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	return out
}

func (s *UpdaterService) buildExcludedContainerSetInternal(ctx context.Context) map[string]bool {
	if s.deps.Settings == nil {
		return nil
	}
	raw := s.deps.Settings.GetStringSetting(ctx, "autoUpdateExcludedContainers", "")
	if raw == "" {
		return nil
	}
	excluded := make(map[string]bool)
	for part := range strings.SplitSeq(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			excluded[name] = true
		}
	}
	return excluded
}

func (s *UpdaterService) collectUsedImagesFromProjectsInternal(ctx context.Context, out map[string]struct{}) error {
	if s.deps.Projects == nil {
		return nil
	}

	projects, err := s.deps.Projects.ListAllProjects(ctx)
	if err != nil {
		return err
	}

	activeProjectNames := activeComposeProjectNameSetInternal(projects)
	if len(activeProjectNames) == 0 {
		return nil
	}

	composeContainers, err := projectspkg.ListGlobalComposeContainers(ctx)
	if err != nil {
		return err
	}

	s.collectUsedImagesFromComposeContainersInternal(ctx, composeContainers, activeProjectNames, out)
	return nil
}

func activeComposeProjectNameSetInternal(projects []models.Project) map[string]struct{} {
	active := make(map[string]struct{})
	for _, project := range projects {
		if project.IsArchived {
			continue
		}
		if project.Status != models.ProjectStatusRunning && project.Status != models.ProjectStatusPartiallyRunning {
			continue
		}

		name := strings.TrimSpace(project.Name)
		if name == "" {
			continue
		}
		active[name] = struct{}{}
		if normalized := loader.NormalizeProjectName(name); normalized != "" {
			active[normalized] = struct{}{}
		}
	}
	return active
}

func addNormalizedImageUpdateRefInternal(ctx context.Context, out map[string]struct{}, imageRef, logMessage string, attrs ...any) {
	normalizedRef := refs.NormalizeImageUpdateRef(imageRef)
	if normalizedRef != "" {
		out[normalizedRef] = struct{}{}
		return
	}

	args := slices.Clone(attrs)
	args = append(args, "imageRef", imageRef)
	if ctx != nil {
		slog.DebugContext(ctx, logMessage, args...)
		return
	}
	slog.Debug(logMessage, args...)
}

func containerSummaryExcludedInternal(summary container.Summary, excludedContainers map[string]bool) bool {
	if len(excludedContainers) == 0 {
		return false
	}
	for _, name := range summary.Names {
		if excludedContainers[strings.TrimPrefix(name, "/")] {
			return true
		}
	}
	return false
}
