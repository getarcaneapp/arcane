package bootstrap

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"context"
	"slices"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/features"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type settingsSubscriptionStubInternal struct {
	featureCallbacks []func([]libarcane.SettingUpdate)
	pollingCallback  func([]libarcane.SettingUpdate)
	timeoutCallback  func([]libarcane.SettingUpdate)
}

func (s *settingsSubscriptionStubInternal) SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func() {
	if slices.Contains(keys, features.VulnerabilityManagementSettingKey) {
		s.featureCallbacks = append(s.featureCallbacks, callback)
	}
	if slices.Contains(keys, "pollingEnabled") {
		s.pollingCallback = callback
	}
	if slices.Contains(keys, "dockerApiTimeout") {
		s.timeoutCallback = callback
	}
	return func() {}
}

type settingsSubscriptionSchedulerStubInternal struct {
	rescheduled chan struct{}
	jobs        []string
}

func (s *settingsSubscriptionSchedulerStubInternal) RescheduleJob(_ context.Context, job schedulertypes.Job) error {
	if job != nil {
		s.jobs = append(s.jobs, job.Name())
	}
	select {
	case <-s.rescheduled:
	default:
		close(s.rescheduled)
	}
	return nil
}

type timeoutSyncEnvironmentStubInternal struct {
	started chan struct{}
}

func (s *timeoutSyncEnvironmentStubInternal) ListRemoteEnvironments(context.Context) ([]environment.Environment, error) {
	return []environment.Environment{{
		ID:   "remote",
		Name: "remote",
	}}, nil
}

func (s *timeoutSyncEnvironmentStubInternal) ProxyRequest(ctx context.Context, _ string, _ string, _ string, _ []byte) ([]byte, int, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

func TestSettingsTimeoutSyncDoesNotBlockOtherEffectsInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(context.Background(), lifecycle)
	require.NoError(t, err)

	settings := &settingsSubscriptionStubInternal{}
	scheduler := &settingsSubscriptionSchedulerStubInternal{rescheduled: make(chan struct{})}
	environment := &timeoutSyncEnvironmentStubInternal{started: make(chan struct{})}
	require.NoError(t, setupSettingsSubscriptionsInternal(settingsSubscriptionsParams{
		Lifecycle:    lifecycle,
		LifecycleCtx: context.Background(),
		Config:       &config.Config{},
		Scheduler:    scheduler,
		ActorRuntime: runtime,
		Settings:     settings,
		Environment:  environment,
	}))

	timeoutCallbackDone := make(chan struct{})
	go func() {
		settings.timeoutCallback([]libarcane.SettingUpdate{{Key: "dockerApiTimeout", Value: "30"}})
		close(timeoutCallbackDone)
	}()

	select {
	case <-environment.started:
	case <-time.After(time.Second):
		require.FailNow(t, "timeout sync did not start")
	}
	select {
	case <-timeoutCallbackDone:
	case <-time.After(time.Second):
		require.FailNow(t, "timeout settings callback remained blocked by remote sync")
	}

	settings.pollingCallback([]libarcane.SettingUpdate{{Key: "pollingEnabled", Value: "true"}})
	select {
	case <-scheduler.rescheduled:
	case <-time.After(time.Second):
		require.FailNow(t, "local settings effect was blocked by remote timeout sync")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestFeatureChangeReschedulesScanAndPatchJobsInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	settings := &settingsSubscriptionStubInternal{}
	schedulerStub := &settingsSubscriptionSchedulerStubInternal{rescheduled: make(chan struct{})}
	require.NoError(t, setupSettingsSubscriptionsInternal(settingsSubscriptionsParams{
		Lifecycle:         lifecycle,
		LifecycleCtx:      t.Context(),
		Config:            &config.Config{},
		Scheduler:         schedulerStub,
		ActorRuntime:      runtime,
		Settings:          settings,
		VulnerabilityScan: scheduler.NewVulnerabilityScanJob(nil, nil),
		AutoPatch:         scheduler.NewAutoPatchJob(nil, nil),
	}))
	require.Len(t, settings.featureCallbacks, 2)
	for _, callback := range settings.featureCallbacks {
		callback([]libarcane.SettingUpdate{{Key: features.VulnerabilityManagementSettingKey, Value: "false"}})
	}
	require.ElementsMatch(t, []string{scheduler.VulnerabilityScanJobName, scheduler.AutoPatchJobName}, schedulerStub.jobs)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}
