package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
)

const DockerClientRefreshJobName = "docker-client-refresh"
const dockerClientRefreshDefaultSchedule = "*/30 * * * * *"

// DockerClientRefreshJob keeps the cached Docker client aligned with the daemon
// API version after daemon restarts or upgrades.
type DockerClientRefreshJob struct {
	dockerClientService *docker.DockerClientService
	settingsService     *settings.SettingsService
}

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
	schedule := j.settingsService.GetStringSetting(ctx, "dockerClientRefreshInterval", dockerClientRefreshDefaultSchedule)
	if schedule == "" {
		schedule = dockerClientRefreshDefaultSchedule
	}

	parser := scheduleutil.Parser()
	if _, err := parser.Parse(schedule); err != nil {
		slog.WarnContext(ctx, "Invalid cron expression for Docker client refresh, using default", "invalid_schedule", schedule, "error", err)
		return dockerClientRefreshDefaultSchedule
	}

	return schedule
}

func (j *DockerClientRefreshJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	if err := j.dockerClientService.RefreshClient(ctx); err != nil {
		slog.WarnContext(ctx, "Docker client refresh failed", "error", err)
		return schedulertypes.Outcome{}, err
	}

	slog.DebugContext(ctx, "Docker client refresh completed")
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}
