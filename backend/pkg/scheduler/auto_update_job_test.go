package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
)

func TestAutoUpdateJob_ShouldSchedule_RequiresAutoUpdateAndPolling(t *testing.T) {
	ctx := t.Context()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, &updater.UpdaterService{}, settingsSvc)

	require.False(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", true))
	require.True(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", false))
	require.False(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", false))
	require.False(t, job.ShouldSchedule(ctx))
}
