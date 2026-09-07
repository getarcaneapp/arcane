package job

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

func newJobActivityTestServiceInternal(t *testing.T) (*JobService, *activity.ActivityService, *database.DB) {
	t.Helper()
	db := setupSettingsTestDBInternal(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	activities := activity.NewActivityService(db, nil)
	svc := New(Dependencies{DB: db, Config: &config.Config{}, Activity: activities}).Service()
	return svc, activities, db
}

func TestJobActivityLifecycle(t *testing.T) {
	svc, activities, db := newJobActivityTestServiceInternal(t)
	ctx := t.Context()
	run, err := svc.Queue.Submit(ctx, st.Request{JobID: "auto-update", Trigger: "scheduled"})
	require.NoError(t, err)
	require.NotEmpty(t, run.ActivityID)
	require.Equal(t, "0", run.ActivityEnvironmentID)
	check := func(status activitytypes.Status, precise st.RunStatus) {
		t.Helper()
		detail, detailErr := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
		require.NoError(t, detailErr)
		require.Equal(t, status, detail.Activity.Status)
		require.Equal(t, string(precise), detail.Activity.Metadata["jobStatus"])
		require.Equal(t, run.ID, *detail.Activity.BatchID)
	}
	check(activitytypes.StatusQueued, st.Queued)
	duplicate, err := svc.Queue.Submit(ctx, st.Request{JobID: run.JobID, RunID: run.ID, Trigger: "scheduled"})
	require.NoError(t, err)
	require.Equal(t, run.ActivityID, duplicate.ActivityID)
	for _, status := range []st.RunStatus{st.Running, st.Waiting, st.NeedsAttention} {
		require.NoError(t, svc.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
			current.Status = status
			current.UpdatedAt = time.Now().UTC()
			current.Outcome = st.Outcome{Status: status, Message: "operation detail"}
			return nil
		}))
	}
	check(activitytypes.StatusFailed, st.NeedsAttention)
	_, err = svc.Queue.Retry(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	check(activitytypes.StatusQueued, st.Queued)
	_, err = activities.CancelActivity(ctx, "0", run.ActivityID, "operator")
	require.ErrorIs(t, err, activity.ErrActivityNotCancelable)
	check(activitytypes.StatusQueued, st.Queued)
	require.NoError(t, svc.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.UpdatedAt = time.Now().UTC()
		return nil
	}))
	_, err = svc.Queue.Resolve(ctx, "0", run.JobID, run.ID, "operator")
	require.NoError(t, err)
	check(activitytypes.StatusCancelled, st.Canceled)
	var count int64
	require.NoError(t, db.Model(&activity.Activity{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestJobActivityVisibilityAndGrouping(t *testing.T) {
	svc, activities, _ := newJobActivityTestServiceInternal(t)
	for _, test := range []struct {
		job, trigger, environment string
		visible                   bool
	}{
		{"auto-update", "scheduled", "0", true},
		{"gitops-sync:project", "scheduled", "0", true},
		{"volume-backup:volume", "recovery", "0", true},
		{"system-backup:system", "scheduled", "0", true},
		{"image-polling", "manual", "0", true},
		{"auto-update", "manual", "remote", true},
		{"auto-update", "remote", "0", false},
		{"image-polling", "scheduled", "0", false},
		{"environment-health:0", "scheduled", "0", false},
		{"activity-sweep", "scheduled", "0", false},
	} {
		t.Run(test.job+"/"+test.trigger+"/"+test.environment, func(t *testing.T) {
			run, err := svc.Queue.Submit(t.Context(), st.Request{JobID: test.job, Trigger: test.trigger, EnvironmentID: test.environment})
			require.NoError(t, err)
			require.Equal(t, test.visible, run.ActivityID != "")
			if test.visible {
				require.Equal(t, test.environment, run.ActivityEnvironmentID)
				detail, err := activities.GetActivityDetail(t.Context(), test.environment, run.ActivityID, 10)
				require.NoError(t, err)
				require.Equal(t, test.environment, detail.Activity.Metadata["environmentId"])
			}
			child, err := activities.StartActivity(svc.runContextInternal(t.Context(), run), activity.StartActivityRequest{Type: activitytypes.TypeImagePull})
			require.NoError(t, err)
			require.Equal(t, run.ID, *child.BatchID)
		})
	}
}

func TestJobActivityRestartRepairsFailedProjection(t *testing.T) {
	svc, activities, db := newJobActivityTestServiceInternal(t)
	ctx := t.Context()
	require.NoError(t, db.Migrator().DropTable(&activity.ActivityMessage{}, &activity.Activity{}))
	run, err := svc.Queue.Submit(ctx, st.Request{JobID: "auto-update", Trigger: "scheduled"})
	require.NoError(t, err, "activity failure must not reject accepted work")
	require.NotEmpty(t, run.ActivityID)
	require.NoError(t, svc.Queue.UpdateRun(ctx, run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.Outcome = st.Outcome{Message: "interrupted before applying updates"}
		current.UpdatedAt = time.Now().UTC()
		return nil
	}))
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	restarted := New(Dependencies{DB: db, Config: &config.Config{}, Activity: activities}).Service()
	require.NoError(t, restarted.Queue.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, restarted.Queue.Stop(stopCtx))
	})
	detail, err := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, activitytypes.StatusFailed, detail.Activity.Status)
	require.Contains(t, detail.Activity.LatestMessage, "interrupted")
	persisted, err := restarted.Queue.Get(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Zero(t, persisted.AttemptCount, "repair must not execute blocked work")
	require.Equal(t, run.ActivityID, persisted.ActivityID)
}
