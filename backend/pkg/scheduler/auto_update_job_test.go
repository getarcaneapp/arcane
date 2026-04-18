package scheduler

import (
	"context"
	"testing"
	"time"

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

func TestAutoUpdateJob_Schedule_WindowEnabled(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	// Default (window disabled): returns autoUpdateInterval cron
	require.Equal(t, "0 0 0 * * *", job.Schedule(ctx))

	// Enable window: must return every-5-min cron
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdateWindowEnabled", true))
	require.Equal(t, "*/5 * * * * *", job.Schedule(ctx))
}

func TestAutoUpdateJob_isWithinWindowInternal_NormalRange(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	loc := time.UTC

	require.True(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 3, 0, 0, 0, loc)))
	require.True(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 2, 0, 0, 0, loc)))
	require.False(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 1, 59, 0, 0, loc)))
	require.False(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 4, 0, 0, 0, loc)))
}

func TestAutoUpdateJob_isWithinWindowInternal_OvernightRange(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "23:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "01:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	loc := time.UTC

	require.True(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 23, 30, 0, 0, loc)))
	require.True(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 2, 0, 30, 0, 0, loc)))
	require.False(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 1, 12, 0, 0, 0, loc)))
	require.False(t, job.isWithinWindowInternal(ctx, time.Date(2026, 1, 2, 1, 0, 0, 0, loc)))
}

func TestAutoUpdateJob_isWithinWindowInternal_DayFilter(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "1,2,3,4,5"))

	loc := time.UTC

	// 2026-01-05 is Monday (Weekday=1)
	monday := time.Date(2026, 1, 5, 3, 0, 0, 0, loc)
	require.True(t, job.isWithinWindowInternal(ctx, monday))

	// 2026-01-04 is Sunday (Weekday=0)
	sunday := time.Date(2026, 1, 4, 3, 0, 0, 0, loc)
	require.False(t, job.isWithinWindowInternal(ctx, sunday))
}

func TestAutoUpdateJob_Run_SkipsOutsideWindow(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job := NewAutoUpdateJob(nil, settingsSvc)

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", true))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "pollingEnabled", true))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdateWindowEnabled", true))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowStart", "02:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowEnd", "04:00"))
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "autoUpdateWindowDays", "0,1,2,3,4,5,6"))

	// updaterService is nil — if Run tries to call ApplyPending it will panic.
	// The test passes only if runAt returns early when outside the window.
	loc := time.UTC
	outsideWindow := time.Date(2026, 1, 1, 10, 0, 0, 0, loc)
	require.NotPanics(t, func() {
		job.runAtInternal(ctx, outsideWindow)
	})
}
