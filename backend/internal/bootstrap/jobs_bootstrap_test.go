package bootstrap

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
)

type settingsSubscriptionStubInternal struct {
	featureCallbacks []func([]libarcane.SettingUpdate)
	pollingCallback  func([]libarcane.SettingUpdate)
	timeoutCallback  func([]libarcane.SettingUpdate)
}

func (s *settingsSubscriptionStubInternal) SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func() {
	if slices.Contains(keys, types.FeatureVulnerabilityManagementSettingKey) {
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

func (s *settingsSubscriptionSchedulerStubInternal) RescheduleJob(_ context.Context, job types.SchedulerJob) error {
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

func (s *timeoutSyncEnvironmentStubInternal) ProxyRequest(ctx context.Context, _, _, _ string, _ []byte) ([]byte, int, error) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

type agentSettingsSyncStub struct {
	snapshots map[string][]byte
	patches   map[string]types.SettingsUpdate
}

func (s *agentSettingsSyncStub) ListRemoteEnvironments(context.Context) ([]environment.Environment, error) {
	return []environment.Environment{
		{ID: "current", Name: "current"},
		{ID: "legacy", Name: "legacy"},
	}, nil
}

func (s *agentSettingsSyncStub) ProxyRequest(_ context.Context, envID, method, _ string, body []byte) ([]byte, int, error) {
	if method == http.MethodGet {
		return s.snapshots[envID], http.StatusOK, nil
	}
	var update types.SettingsUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return nil, 0, err
	}
	s.patches[envID] = update
	return []byte(`{}`), http.StatusOK, nil
}

func TestSyncTimeoutSettingsToAgentsSkipsLockedKeysAndMismatchedAgents(t *testing.T) {
	currentSnapshot, err := json.Marshal(types.SettingsSnapshot{
		Settings: []types.SettingState{
			{Key: "dockerApiTimeout", Locked: true, LockReason: "env"},
			{Key: "buildTimeout"},
		},
	})
	require.NoError(t, err)
	stub := &agentSettingsSyncStub{
		snapshots: map[string][]byte{
			"current": currentSnapshot,
			"legacy":  []byte(`[{"key":"dockerApiTimeout","type":"string","value":"30"}]`),
		},
		patches: map[string]types.SettingsUpdate{},
	}

	syncTimeoutSettingsToAgents(t.Context(), stub, map[string]string{
		"dockerApiTimeout": "60",
		"buildTimeout":     "900",
	})

	require.Equal(t, map[string]types.SettingsUpdate{"current": {"buildTimeout": "900"}}, stub.patches)
}

func TestSettingsTimeoutSyncDoesNotBlockOtherEffects(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)

	settings := &settingsSubscriptionStubInternal{}
	localScheduler := &settingsSubscriptionSchedulerStubInternal{rescheduled: make(chan struct{})}
	localEnvironment := &timeoutSyncEnvironmentStubInternal{started: make(chan struct{})}
	require.NoError(t, setupSettingsSubscriptions(settingsSubscriptionsParams{
		Lifecycle:          lifecycle,
		LifecycleCtx:       t.Context(),
		Config:             &config.Config{},
		Scheduler:          localScheduler,
		Settings:           settings,
		Environment:        localEnvironment,
		AutoUpdate:         &flow.Job{JobName: "auto-update"},
		ImageUpdateWatcher: &scheduler.ImageUpdateWatcher{},
	}))

	timeoutCallbackDone := make(chan struct{})
	go func() {
		settings.timeoutCallback([]libarcane.SettingUpdate{{Key: "dockerApiTimeout", Value: "30"}})
		close(timeoutCallbackDone)
	}()

	select {
	case <-localEnvironment.started:
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
	case <-localScheduler.rescheduled:
	case <-time.After(time.Second):
		require.FailNow(t, "local settings effect was blocked by remote timeout sync")
	}

	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestFeatureChangeReschedulesScanAndPatchJobs(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	settings := &settingsSubscriptionStubInternal{}
	schedulerStub := &settingsSubscriptionSchedulerStubInternal{rescheduled: make(chan struct{})}
	require.NoError(t, setupSettingsSubscriptions(settingsSubscriptionsParams{
		Lifecycle:         lifecycle,
		LifecycleCtx:      t.Context(),
		Config:            &config.Config{},
		Scheduler:         schedulerStub,
		Settings:          settings,
		VulnerabilityScan: scheduler.NewVulnerabilityScanJob(nil, &vulnerability.VulnerabilityService{}, nil),
		VulnerabilityRisk: scheduler.NewVulnerabilityRiskJob(nil, nil),
		AutoPatch:         scheduler.NewAutoPatchJob(nil, &image.ImageService{}, nil),
	}))
	require.Len(t, settings.featureCallbacks, 3)
	for _, callback := range settings.featureCallbacks {
		callback([]libarcane.SettingUpdate{{Key: types.FeatureVulnerabilityManagementSettingKey, Value: "false"}})
	}
	require.ElementsMatch(t, []string{scheduler.VulnerabilityScanJobName, scheduler.VulnerabilityRiskJobName, scheduler.AutoPatchJobName}, schedulerStub.jobs)
	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}
