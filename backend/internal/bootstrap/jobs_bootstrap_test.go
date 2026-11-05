package bootstrap

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/scheduler"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/types/v3"
)

type (
	settingsSubscriptionStub struct {
		featureCallbacks []func([]types.SettingUpdate)
	}

	settingsSubscriptionSchedulerStub struct {
		rescheduled chan struct{}
		jobs        []string
	}
)

func (s *settingsSubscriptionStub) SubscribeSettingsChanges(keys []string, callback func([]types.SettingUpdate)) func() {
	if slices.Contains(keys, types.FeatureVulnerabilityManagementSettingKey) {
		s.featureCallbacks = append(s.featureCallbacks, callback)
	}
	return func() {}
}

func (s *settingsSubscriptionSchedulerStub) AddJob(_ context.Context, job types.SchedulerJob) error {
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

func TestFeatureChangeReschedulesScanAndPatchJobs(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	settings := &settingsSubscriptionStub{}
	schedulerStub := &settingsSubscriptionSchedulerStub{rescheduled: make(chan struct{})}
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
		callback([]types.SettingUpdate{{Key: types.FeatureVulnerabilityManagementSettingKey, Value: "false"}})
	}
	require.ElementsMatch(t, []string{scheduler.VulnerabilityScanJobName, scheduler.VulnerabilityRiskJobName, scheduler.AutoPatchJobName}, schedulerStub.jobs)
	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, lifecycle.Stop(stopCtx))
}
