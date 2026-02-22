package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/robfig/cron/v3"
)

const AutoHealJobName = "auto-heal"

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

	dockerClient, err := j.dockerClientService.GetClient()
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to get Docker client", "error", err)
		return
	}

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		slog.ErrorContext(ctx, "auto-heal failed to list containers", "error", err)
		return
	}

	excludedContainers := j.parseExcludedContainers(ctx)
	maxRestarts := j.settingsService.GetIntSetting(ctx, "autoHealMaxRestarts", 5)
	restartWindowMinutes := j.settingsService.GetIntSetting(ctx, "autoHealRestartWindow", 30)
	restartWindow := time.Duration(restartWindowMinutes) * time.Minute

	for _, c := range containers {
		// Skip Arcane internal containers
		if libarcane.IsInternalContainer(c.Labels) {
			continue
		}

		containerName := j.getContainerName(c.Names)

		// Skip excluded containers
		if j.isExcluded(containerName, excludedContainers) {
			continue
		}

		// Inspect to get health status
		inspect, err := dockerClient.ContainerInspect(ctx, c.ID)
		if err != nil {
			slog.WarnContext(ctx, "auto-heal failed to inspect container", "container", containerName, "error", err)
			continue
		}

		// Skip if no healthcheck configured or not unhealthy
		if inspect.State == nil || inspect.State.Health == nil {
			continue
		}
		if inspect.State.Health.Status != "unhealthy" {
			continue
		}

		// Check restart-loop protection
		if !j.canRestart(c.ID, maxRestarts, restartWindow) {
			slog.WarnContext(ctx, "auto-heal restart-loop protection: skipping container",
				"container", containerName,
				"max_restarts", maxRestarts,
				"window_minutes", restartWindowMinutes,
			)
			continue
		}

		// Restart the container
		slog.InfoContext(ctx, "auto-heal restarting unhealthy container", "container", containerName, "container_id", c.ID)
		if err := dockerClient.ContainerRestart(ctx, c.ID, container.StopOptions{}); err != nil {
			slog.ErrorContext(ctx, "auto-heal failed to restart container", "container", containerName, "error", err)
			continue
		}

		// Record the restart
		j.recordRestart(c.ID)

		// Log event
		if err := j.eventService.LogContainerEvent(
			ctx,
			models.EventTypeContainerRestart,
			c.ID,
			containerName,
			"", // no user - system action
			"system",
			"",
			models.JSON{"action": "auto-heal", "reason": "unhealthy"},
		); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to log event", "container", containerName, "error", err)
		}

		// Send notification
		if err := j.notificationService.SendAutoHealNotification(ctx, containerName, c.ID); err != nil {
			slog.WarnContext(ctx, "auto-heal failed to send notification", "container", containerName, "error", err)
		}

		slog.InfoContext(ctx, "auto-heal successfully restarted container", "container", containerName)
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
	for _, name := range strings.Split(raw, ",") {
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
