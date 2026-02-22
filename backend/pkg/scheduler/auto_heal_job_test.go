package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
	for i := 0; i < 4; i++ {
		job.RecordRestartExported("container-1")
	}

	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_AtLimit(t *testing.T) {
	job := newTestAutoHealJob()

	// Record exactly 5 restarts (at limit)
	for i := 0; i < 5; i++ {
		job.RecordRestartExported("container-1")
	}

	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_WindowExpiry(t *testing.T) {
	job := newTestAutoHealJob()

	// Record 5 restarts 31 minutes ago (outside window)
	oldTime := time.Now().Add(-31 * time.Minute)
	for i := 0; i < 5; i++ {
		job.RecordRestartAtExported("container-1", oldTime)
	}

	// Should be allowed because all timestamps are outside the 30-minute window
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}

func TestAutoHeal_CanRestart_MixedTimestamps(t *testing.T) {
	job := newTestAutoHealJob()

	// Record 3 old restarts (outside window)
	oldTime := time.Now().Add(-31 * time.Minute)
	for i := 0; i < 3; i++ {
		job.RecordRestartAtExported("container-1", oldTime)
	}

	// Record 4 recent restarts (inside window)
	for i := 0; i < 4; i++ {
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
	for i := 0; i < 5; i++ {
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
	for i := 0; i < 5; i++ {
		job.RecordRestartExported("container-1")
	}
	require.False(t, job.CanRestartExported("container-1", 5, 30*time.Minute))

	// Reset tracking
	job.ResetRestartTracking()

	// Should be allowed again
	require.True(t, job.CanRestartExported("container-1", 5, 30*time.Minute))
}
