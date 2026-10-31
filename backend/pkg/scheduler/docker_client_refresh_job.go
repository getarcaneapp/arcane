package scheduler

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/schedule"
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
