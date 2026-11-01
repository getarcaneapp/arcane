package scheduler

import (
	"context"
	"log/slog"

	"getarcane.dev/arcane/v3/internal/docker"
	scheduleutil "getarcane.dev/arcane/v3/internal/scheduler/schedule"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/types/v3"
)

type (
	// DockerClientRefreshJob keeps the cached Docker client aligned with the daemon
	// API version after daemon restarts or upgrades.
	DockerClientRefreshJob struct {
		dockerClientService *docker.DockerClientService
		settingsService     *settings.SettingsService
	}
)

const (
	DockerClientRefreshJobName         = "docker-client-refresh"
	dockerClientRefreshDefaultSchedule = "*/5 * * * *"
)

// NewDockerClientRefreshJob creates the scheduled Docker client refresh job.
func NewDockerClientRefreshJob(dockerClientService *docker.DockerClientService, settingsService *settings.SettingsService) *DockerClientRefreshJob {
	return &DockerClientRefreshJob{
		dockerClientService: dockerClientService,
		settingsService:     settingsService,
	}
}

func (j *DockerClientRefreshJob) Name() string {
	return DockerClientRefreshJobName
}

func (j *DockerClientRefreshJob) Schedule(ctx context.Context) string {
	return scheduleutil.Or(ctx, j.settingsService.GetStringSetting(ctx, "dockerClientRefreshInterval"), dockerClientRefreshDefaultSchedule, "docker-client-refresh")
}

func (j *DockerClientRefreshJob) Run(ctx context.Context) (types.SchedulerOutcome, error) {
	if err := j.dockerClientService.RefreshClient(ctx); err != nil {
		slog.WarnContext(ctx, "Docker client refresh failed", "error", err)
		return types.SchedulerOutcome{}, err
	}

	slog.DebugContext(ctx, "Docker client refresh completed")
	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
}
