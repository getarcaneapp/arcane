package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/robfig/cron/v3"
	"golang.org/x/sync/errgroup"
)

const AutoHealJobName = "auto-heal"
const autoHealInspectConcurrency = 4

// restartRecord tracks restart timestamps for a single container.
type restartRecord struct {
	timestamps []time.Time
}

type AutoHealJob struct {
	dockerClientService *services.DockerClientService
	settingsService     *services.SettingsService
	eventService        *services.EventService
	notificationService *services.NotificationService

	mu       sync.Mutex
	restarts map[string]*restartRecord

	getDockerClient  func() (*client.Client, error)
	listContainers   func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error)
	inspectContainer func(ctx context.Context, dockerClient *client.Client, containerID string) (container.InspectResponse, error)
	restartContainer func(ctx context.Context, dockerClient *client.Client, containerID string) error
}

func NewAutoHealJob(
	dockerClientService *services.DockerClientService,
	settingsService *services.SettingsService,
	eventService *services.EventService,
	notificationService *services.NotificationService,
) *AutoHealJob {
	return &AutoHealJob{
		dockerClientService: dockerClientService,
		settingsService:     settingsService,
		eventService:        eventService,
		notificationService: notificationService,
		restarts:            make(map[string]*restartRecord),
	}
}

func (j *AutoHealJob) Name() string {
	return AutoHealJobName
}

func (j *AutoHealJob) Schedule(ctx context.Context) string {
	schedule := j.settingsService.GetStringSetting(ctx, "autoHealInterval", "*/30 * * * * *")
	if schedule == "" {
		schedule = "*/30 * * * * *"
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for auto-heal, using default", "invalid_schedule", schedule, "error", err)
		return "*/30 * * * * *"
	}

	return schedule
}

func (j *AutoHealJob) Run(ctx context.Context) {
	enabled := j.settingsService.GetBoolSetting(ctx, "autoHealEnabled", false)
	if !enabled {
		slog.DebugContext(ctx, "auto-heal disabled; skipping run")
		return
	}

	dockerClient, err := j.getDockerClientInternal(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to get Docker client", "error", err)
		return
	}

	containerList, err := j.listContainersInternal(ctx, dockerClient)
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to list containers", "error", err)
		return
	}
	containers := containerList

	excludedContainers := j.parseExcludedContainers(ctx)
	maxRestarts := j.settingsService.GetIntSetting(ctx, "autoHealMaxRestarts", 5)
	restartWindowMinutes := j.settingsService.GetIntSetting(ctx, "autoHealRestartWindow", 30)
	restartWindow := time.Duration(restartWindowMinutes) * time.Minute

	candidates := j.filterCandidatesInternal(containers, excludedContainers)

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(autoHealInspectConcurrency)

	for _, candidate := range candidates {
		g.Go(func() error {
			j.processCandidateInternal(groupCtx, dockerClient, candidate, maxRestarts, restartWindow, restartWindowMinutes)
			return nil
		})
	}

	_ = g.Wait()
}

func (j *AutoHealJob) filterCandidatesInternal(containers []container.Summary, excludedContainers map[string]struct{}) []container.Summary {
	candidates := make([]container.Summary, 0, len(containers))
	for _, c := range containers {
		if libarcane.IsInternalContainer(c.Labels) {
			continue
		}

		containerName := j.getContainerName(c.Names)
		if j.isExcluded(containerName, excludedContainers) {
			continue
		}

		candidates = append(candidates, c)
	}

	return candidates
}

func (j *AutoHealJob) processCandidateInternal(
	ctx context.Context,
	dockerClient *client.Client,
	candidate container.Summary,
	maxRestarts int,
	restartWindow time.Duration,
	restartWindowMinutes int,
) {
	containerID := candidate.ID
	containerName := j.getContainerName(candidate.Names)

	inspect, err := j.inspectContainerInternal(ctx, dockerClient, containerID)
	if err != nil {
		slog.WarnContext(ctx, "auto-heal failed to inspect container", "container", containerName, "error", err)
		return
	}

	if inspect.State == nil || inspect.State.Health == nil {
		return
	}
	if inspect.State.Health.Status != container.Unhealthy {
		return
	}

	if !j.canRestart(containerID, maxRestarts, restartWindow) {
		slog.WarnContext(ctx, "auto-heal restart-loop protection: skipping container",
			"container", containerName,
			"max_restarts", maxRestarts,
			"window_minutes", restartWindowMinutes,
		)
		return
	}

	if err := j.restartContainerInternal(ctx, dockerClient, containerID); err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to restart container", "container", containerName, "error", err)
		return
	}

	j.recordRestart(containerID)
	j.postRestartActionsInternal(ctx, containerID, containerName)

	slog.InfoContext(ctx, "auto-heal restarted unhealthy container", "container", containerName, "container_id", containerID)
}

func (j *AutoHealJob) postRestartActionsInternal(ctx context.Context, containerID, containerName string) {
	if j.eventService != nil {
		if err := j.eventService.LogContainerEvent(
			ctx,
			models.EventTypeContainerRestart,
			containerID,
			containerName,
			"", // no user - system action
			"system",
			"",
			models.JSON{"action": "auto-heal", "reason": "unhealthy"},
		); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to log event", "container", containerName, "error", err)
		}
	}

	if j.notificationService != nil {
		if err := j.notificationService.SendAutoHealNotification(ctx, containerName, containerID); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to send notification", "container", containerName, "error", err)
		}
	}
}

func (j *AutoHealJob) Reschedule(ctx context.Context) error {
	slog.InfoContext(ctx, "rescheduling auto-heal job in new scheduler; currently requires restart")
	return nil
}

// canRestart checks if a container can be restarted within the rate limit.
func (j *AutoHealJob) canRestart(containerID string, maxRestarts int, window time.Duration) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		return true
	}

	cutoff := time.Now().Add(-window)
	recent := j.pruneTimestamps(record.timestamps, cutoff)
	record.timestamps = recent

	return len(recent) < maxRestarts
}

// recordRestart records a restart timestamp for a container.
func (j *AutoHealJob) recordRestart(containerID string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		record = &restartRecord{}
		j.restarts[containerID] = record
	}

	record.timestamps = append(record.timestamps, time.Now())
}

// pruneTimestamps removes timestamps older than the cutoff.
func (j *AutoHealJob) pruneTimestamps(timestamps []time.Time, cutoff time.Time) []time.Time {
	result := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			result = append(result, ts)
		}
	}
	return result
}

func (j *AutoHealJob) parseExcludedContainers(ctx context.Context) map[string]struct{} {
	raw := j.settingsService.GetStringSetting(ctx, "autoHealExcludedContainers", "")
	excluded := make(map[string]struct{})
	if raw == "" {
		return excluded
	}
	for name := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			excluded[trimmed] = struct{}{}
		}
	}
	return excluded
}

func (j *AutoHealJob) getContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	// Docker container names are prefixed with "/"
	return strings.TrimPrefix(names[0], "/")
}

func (j *AutoHealJob) isExcluded(name string, excluded map[string]struct{}) bool {
	_, ok := excluded[name]
	return ok
}

func (j *AutoHealJob) inspectContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) (container.InspectResponse, error) {
	if j.inspectContainer != nil {
		return j.inspectContainer(ctx, dockerClient, containerID)
	}

	inspect, err := libarcane.ContainerInspectWithCompatibility(ctx, dockerClient, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}

	return inspect.Container, nil
}

func (j *AutoHealJob) restartContainerInternal(ctx context.Context, dockerClient *client.Client, containerID string) error {
	if j.restartContainer != nil {
		return j.restartContainer(ctx, dockerClient, containerID)
	}

	_, err := dockerClient.ContainerRestart(ctx, containerID, client.ContainerRestartOptions{})
	return err
}

func (j *AutoHealJob) getDockerClientInternal(ctx context.Context) (*client.Client, error) {
	if j.getDockerClient != nil {
		return j.getDockerClient()
	}

	return j.dockerClientService.GetClient(ctx)
}

func (j *AutoHealJob) listContainersInternal(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
	if j.listContainers != nil {
		return j.listContainers(ctx, dockerClient)
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return nil, err
	}

	return containerList.Items, nil
}

// ResetRestartTracking clears all restart records (exported for testing).
func (j *AutoHealJob) ResetRestartTracking() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.restarts = make(map[string]*restartRecord)
}

// CanRestartExported exposes canRestart for testing.
func (j *AutoHealJob) CanRestartExported(containerID string, maxRestarts int, window time.Duration) bool {
	return j.canRestart(containerID, maxRestarts, window)
}

// RecordRestartExported exposes recordRestart for testing.
func (j *AutoHealJob) RecordRestartExported(containerID string) {
	j.recordRestart(containerID)
}

// RecordRestartAtExported records a restart at a specific time for testing.
func (j *AutoHealJob) RecordRestartAtExported(containerID string, t time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()

	record, exists := j.restarts[containerID]
	if !exists {
		record = &restartRecord{}
		j.restarts[containerID] = record
	}

	record.timestamps = append(record.timestamps, t)
}
