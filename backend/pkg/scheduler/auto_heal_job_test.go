package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestAutoHealJob() *AutoHealJob {
	return &AutoHealJob{
		restarts: make(map[string]*restartRecord),
	}
}

func TestAutoHeal_CanRestart_UnderLimit(t *testing.T) {
	job := newTestAutoHealJob()

	// No restarts recorded yet — should be allowed
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))

	// Record 4 restarts (under limit of 5)
	for range 4 {
		job.RecordRestartExported("container-1")
	}

	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_AtLimit(t *testing.T) {
	job := newTestAutoHealJob()

	// Record exactly 5 restarts (at limit)
	for range 5 {
		job.RecordRestartExported("container-1")
	}

	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_WindowExpiry(t *testing.T) {
	job := newTestAutoHealJob()

	// Record 5 restarts 31 minutes ago (outside window)
	oldTime := time.Now().Add(-31 * time.Minute)
	for range 5 {
		job.RecordRestartAtExported("container-1", oldTime)
	}

	// Should be allowed because all timestamps are outside the 30-minute window
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_MixedTimestamps(t *testing.T) {
	job := newTestAutoHealJob()

	// Record 3 old restarts (outside window)
	oldTime := time.Now().Add(-31 * time.Minute)
	for range 3 {
		job.RecordRestartAtExported("container-1", oldTime)
	}

	// Record 4 recent restarts (inside window)
	for range 4 {
		job.RecordRestartExported("container-1")
	}

	// Should still be allowed (only 4 recent, limit is 5)
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))

	// Add one more recent restart
	job.RecordRestartExported("container-1")

	// Now should be blocked (5 recent)
	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_DifferentContainers(t *testing.T) {
	job := newTestAutoHealJob()

	// Fill up container-1
	for range 5 {
		job.RecordRestartExported("container-1")
	}

	// container-1 should be blocked
	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))

	// container-2 should still be allowed
	require.True(t, job.CanRestartExported("container-2", 5, 30*time.Minute))
}

func TestAutoHeal_Schedule_Default(t *testing.T) {
	job := newTestAutoHealJob()
	// Without a settings service, Schedule would panic.
	// We test the Name() method directly.
	require.Equal(t, "auto-heal", job.Name())
}

func TestAutoHeal_ResetRestartTracking(t *testing.T) {
	job := newTestAutoHealJob()

	// Fill up container-1
	for range 5 {
		job.RecordRestartExported("container-1")
	}
	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))

	// Reset tracking
	job.ResetRestartTracking()

	// Should be allowed again
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_Run_UsesBoundedConcurrency(t *testing.T) {
	ctx := context.Background()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))

	settingsSvc, err := services.NewSettingsService(ctx, &database.DB{DB: db})
	require.NoError(t, err)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoHealEnabled", true))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoHealExcludedContainers", "skip-me"))

	job := NewAutoHealJob(nil, settingsSvc, nil, nil)
	job.getDockerClient = func() (*client.Client, error) { return nil, nil }
	job.listContainers = func(ctx context.Context, dockerClient *client.Client) ([]container.Summary, error) {
		return []container.Summary{
			{ID: "c1", Names: []string{"/one"}},
			{ID: "c2", Names: []string{"/two"}},
			{ID: "c3", Names: []string{"/three"}},
			{ID: "c4", Names: []string{"/four"}},
			{ID: "c5", Names: []string{"/five"}},
			{ID: "c6", Names: []string{"/six"}},
			{ID: "skip", Names: []string{"/skip-me"}},
		}, nil
	}

	var current int32
	var maxConcurrent int32
	var restarts int32

	job.inspectContainer = func(ctx context.Context, dockerClient *client.Client, containerID string) (container.InspectResponse, error) {
		active := atomic.AddInt32(&current, 1)
		for {
			maxSeen := atomic.LoadInt32(&maxConcurrent)
			if active <= maxSeen || atomic.CompareAndSwapInt32(&maxConcurrent, maxSeen, active) {
				break
			}
		}
		defer atomic.AddInt32(&current, -1)

		time.Sleep(40 * time.Millisecond)

		switch containerID {
		case "c5":
			return container.InspectResponse{State: &container.State{}}, nil
		case "c6":
			return container.InspectResponse{State: &container.State{Health: &container.Health{Status: container.Healthy}}}, nil
		default:
			return container.InspectResponse{State: &container.State{Health: &container.Health{Status: container.Unhealthy}}}, nil
		}
	}
	job.restartContainer = func(ctx context.Context, dockerClient *client.Client, containerID string) error {
		atomic.AddInt32(&restarts, 1)
		return nil
	}

	job.Run(ctx)

	require.Greater(t, atomic.LoadInt32(&maxConcurrent), int32(1))
	require.LessOrEqual(t, atomic.LoadInt32(&maxConcurrent), int32(autoHealInspectConcurrency))
	require.Equal(t, int32(4), atomic.LoadInt32(&restarts))
}
