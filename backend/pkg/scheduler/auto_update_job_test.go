package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/updater"
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

type blockingApplierFakeInternal struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (f *blockingApplierFakeInternal) ApplyPending(context.Context, updater.Options) (*updater.Result, error) {
	f.calls.Add(1)
	select {
	case f.started <- struct{}{}:
	default:
	}
	<-f.release
	return &updater.Result{}, nil
}

func TestAutoUpdateJob_OverlappingRunIsSkippedInternal(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", true))

	applier := &blockingApplierFakeInternal{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	job := &AutoUpdateJob{updaterService: applier, settingsService: settingsSvc}

	firstDone := make(chan struct{})
	go func() {
		job.Run(ctx)
		close(firstDone)
	}()
	<-applier.started

	// Overlapping tick returns immediately without a second ApplyPending.
	job.Run(ctx)
	require.Equal(t, int32(1), applier.calls.Load())

	close(applier.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first auto-update run did not finish")
	}

	// The guard resets once the run finishes.
	job.Run(ctx)
	require.Equal(t, int32(2), applier.calls.Load())
}
