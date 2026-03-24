package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoUpdateJob_ShouldSchedule_RequiresAutoUpdateAndPolling(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.False(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", true))
	require.True(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", false))
	require.False(t, job.ShouldSchedule(ctx))

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", false))
	require.False(t, job.ShouldSchedule(ctx))
}
