package scheduler

import (
	"context"
	"log/slog"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
)

const (
	DockerClientRefreshJobName         = "docker-client-refresh"
	dockerClientRefreshDefaultSchedule = "0 */5 * * * *"
)

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
	return scheduleutil.Or(ctx, j.settingsService.GetStringSetting(ctx, "dockerClientRefreshInterval", dockerClientRefreshDefaultSchedule), dockerClientRefreshDefaultSchedule, "docker-client-refresh")
}

func (j *DockerClientRefreshJob) Run(ctx context.Context) (schedulertypes.Outcome, error) {
	if err := j.dockerClientService.RefreshClient(ctx); err != nil {
		slog.WarnContext(ctx, "Docker client refresh failed", "error", err)
		return schedulertypes.Outcome{}, err
	}

	slog.DebugContext(ctx, "Docker client refresh completed")
	return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
}
