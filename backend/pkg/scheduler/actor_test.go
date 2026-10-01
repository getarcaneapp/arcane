package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var schedulerTestRuntimesInternal sync.Map

func newTestAdmissionGateInternal(t *testing.T) *runs.Admission {
	t.Helper()
	runtime := francistest.New(t)
	admission := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, admission.Register(runtime))
	francistest.Start(t, runtime)
	return admission
}

func newTestCoordinatorInternal(t testing.TB, ctx context.Context, location *time.Location, execute func(context.Context, schedulertypes.Run) (schedulertypes.Outcome, error)) (*runs.Coordinator, *francis.Runtime) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runtime := francistest.New(t)
	coordinator := runs.New(kv.NewKVService(&database.DB{DB: db}), runtime.Service(), location)
	coordinator.SetExecutor(execute, nil)
	require.NoError(t, coordinator.Register(runtime))
	require.NoError(t, runtime.Start(t.Context(), ctx, nil))
	t.Cleanup(func() { require.NoError(t, runtime.Stop(context.Background())) })
	require.NoError(t, coordinator.Start(ctx))
	coordinator.Activate()
	t.Cleanup(func() { require.NoError(t, coordinator.Stop(context.Background())) })
	return coordinator, runtime
}

func newJobSchedulerForTestInternal(t testing.TB, ctx context.Context, location *time.Location) *jobSchedulerInternal {
	t.Helper()
	var scheduler *jobSchedulerInternal
	coordinator, runtime := newTestCoordinatorInternal(t, ctx, location, func(ctx context.Context, run schedulertypes.Run) (schedulertypes.Outcome, error) {
		job, ok := scheduler.GetJob(run.JobID)
		if !ok {
			return schedulertypes.Outcome{Status: schedulertypes.Skipped}, nil
		}
		return job.Run(jobcontext.WithExecution(ctx, run, nil))
	})
	created, err := NewJobScheduler(ctx, coordinator, location)
	require.NoError(t, err)
	scheduler = created.(*jobSchedulerInternal)
	schedulerTestRuntimesInternal.Store(scheduler, runtime)
	t.Cleanup(func() {
		require.NoError(t, scheduler.Stop(context.Background()))
		schedulerTestRuntimesInternal.Delete(scheduler)
	})
	return scheduler
}

func stopJobSchedulerForTestInternal(ctx context.Context, scheduler *jobSchedulerInternal) error {
	if err := scheduler.Stop(ctx); err != nil {
		return err
	}
	if err := scheduler.coordinator.Stop(ctx); err != nil {
		return err
	}
	runtime, _ := schedulerTestRuntimesInternal.Load(scheduler)
	return runtime.(*francis.Runtime).Stop(ctx)
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.Background())) })
	}
	return svc, err
}
