package bootstrap

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

type settingsSubscriptionStubInternal struct {
	pollingCallback func([]libarcane.SettingUpdate)
	timeoutCallback func([]libarcane.SettingUpdate)
}

func (s *settingsSubscriptionStubInternal) SubscribeSettingsChanges(keys []string, callback func([]libarcane.SettingUpdate)) func() {
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
}

func (s *settingsSubscriptionSchedulerStubInternal) RescheduleJob(context.Context, schedulertypes.Job) error {
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

func (s *timeoutSyncEnvironmentStubInternal) ListRemoteEnvironments(context.Context) ([]models.Environment, error) {
	return []models.Environment{{
		BaseModel: models.BaseModel{ID: "remote"},
		Name:      "remote",
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
		t.Fatal("timeout sync did not start")
	}
	select {
	case <-timeoutCallbackDone:
	case <-time.After(time.Second):
		t.Fatal("timeout settings callback remained blocked by remote sync")
	}

	settings.pollingCallback([]libarcane.SettingUpdate{{Key: "pollingEnabled", Value: "true"}})
	select {
	case <-scheduler.rescheduled:
	case <-time.After(time.Second):
		t.Fatal("local settings effect was blocked by remote timeout sync")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}
