package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/robfig/cron/v3"
)

const DockerClientRefreshJobName = "docker-client-refresh"
const dockerClientRefreshDefaultSchedule = "*/30 * * * * *"

// DockerClientRefreshJob keeps the cached Docker client aligned with the daemon
// API version after daemon restarts or upgrades.
type DockerClientRefreshJob struct {
	dockerClientService *services.DockerClientService
	settingsService     *services.SettingsService
}

// NewDockerClientRefreshJob creates the scheduled Docker client refresh job.
func NewDockerClientRefreshJob(dockerClientService *services.DockerClientService, settingsService *services.SettingsService) *DockerClientRefreshJob {
	return &DockerClientRefreshJob{
		dockerClientService: dockerClientService,
		settingsService:     settingsService,
	}
}

func (j *DockerClientRefreshJob) Name() string {
	return DockerClientRefreshJobName
}

func (j *DockerClientRefreshJob) Schedule(ctx context.Context) string {
	schedule := j.settingsService.GetStringSetting(ctx, "dockerClientRefreshInterval", dockerClientRefreshDefaultSchedule)
	if schedule == "" {
		schedule = dockerClientRefreshDefaultSchedule
	}

	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for Docker client refresh, using default", "invalid_schedule", schedule, "error", err)
		return dockerClientRefreshDefaultSchedule
	}

	return schedule
}

func (j *DockerClientRefreshJob) Run(ctx context.Context) {
	if err := j.dockerClientService.RefreshClient(ctx); err != nil {
		slog.WarnContext(ctx, "Docker client refresh failed", "error", err)
		return
	}

	slog.DebugContext(ctx, "Docker client refresh completed")
}
