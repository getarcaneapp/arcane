package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/stretchr/testify/require"
)

func TestAutoUpdateJob_ShouldSchedule_RequiresAutoUpdateAndPolling(t *testing.T) {
	ctx := context.Background()
	_, settingsSvc, _ := setupAnalyticsStateServicesInternal(t)
	job, err := NewAutoUpdateJob(nil, settingsSvc, newTestAdmissionGateInternal(t))
	require.NoError(t, err)

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
	job := &AutoUpdateJob{updaterService: applier, settingsService: settingsSvc, admissionGate: newTestAdmissionGateInternal(t)}

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
		require.FailNow(t, "first auto-update run did not finish")
	}

	// The guard resets once the run finishes.
	job.Run(ctx)
	require.Equal(t, int32(2), applier.calls.Load())
	for _, tc := range []struct {
		name       string
		targets    []schedulertypes.TargetOutcome
		retryIDs   []string
		status     schedulertypes.RunStatus
		unresolved bool
	}{
		{name: "missing progress", unresolved: true, status: schedulertypes.NeedsAttention},
		{name: "image pull alone", unresolved: true, targets: []schedulertypes.TargetOutcome{{ID: "image", ResourceType: "image", Status: schedulertypes.Succeeded}}, status: schedulertypes.NeedsAttention},
		{name: "failed containers only", unresolved: true, targets: []schedulertypes.TargetOutcome{{ID: "done", ResourceType: "container", Status: schedulertypes.Succeeded}, {ID: "failed", ResourceType: "container", Status: schedulertypes.Failed}, {ID: "image", ResourceType: "image", Status: schedulertypes.Failed}}, retryIDs: []string{"failed"}},
		{name: "completed failed batch retry", targets: []schedulertypes.TargetOutcome{{ID: "auto-update", ResourceType: "update-batch", Status: schedulertypes.Partial}, {ID: "failed", ResourceType: "container", Status: schedulertypes.Failed}}, retryIDs: []string{"failed"}},
		{name: "scoped retry cannot confirm original batch", targets: []schedulertypes.TargetOutcome{{ID: "auto-update", ResourceType: "update-retry", Status: schedulertypes.Succeeded}, {ID: "done", ResourceType: "container", Status: schedulertypes.Succeeded}}, status: schedulertypes.NeedsAttention, unresolved: true},
		{name: "completed batch", targets: []schedulertypes.TargetOutcome{{ID: "auto-update", ResourceType: "update-batch", Status: schedulertypes.Succeeded}}, status: schedulertypes.Succeeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := schedulertypes.Run{AttemptCount: 2, Outcome: schedulertypes.Outcome{Targets: tc.targets}}
			options, outcome, unresolved := autoUpdateRetryOptionsInternal(previous)
			require.Equal(t, tc.unresolved, unresolved)
			require.Equal(t, tc.retryIDs, options.ResourceIds)
			require.Equal(t, tc.status, outcome.Status)
			if len(tc.retryIDs) == 0 {
				require.Error(t, job.ValidateRetry(ctx, previous))
			} else {
				require.NoError(t, job.ValidateRetry(ctx, previous))
			}
			reconciled, err := job.Reconcile(ctx, previous)
			require.NoError(t, err)
			if tc.status == schedulertypes.Succeeded {
				require.Equal(t, schedulertypes.Succeeded, reconciled.Status)
			} else {
				require.Equal(t, schedulertypes.NeedsAttention, reconciled.Status)
			}
		})
	}

	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "autoUpdate", false))
	retryCtx := jobcontext.WithExecution(ctx, schedulertypes.Run{AttemptCount: 2, Outcome: schedulertypes.Outcome{Targets: []schedulertypes.TargetOutcome{{ID: "failed", ResourceType: "container", Status: schedulertypes.Failed}}}}, nil)
	outcome, err := job.Run(retryCtx)
	require.NoError(t, err)
	require.Equal(t, schedulertypes.NeedsAttention, outcome.Status)
	require.Equal(t, int32(2), applier.calls.Load())

}
