package job

import (
	"context"
	"testing"
	"time"
	"uuid"

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
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	activities := activity.NewActivityService(db, nil)
	svc := NewJobService(db, nil, &config.Config{}, newJobCoordinatorForTestInternal(t, db), nil, nil, activities)
	return svc, activities, db
}

func TestJobActivityLifecycle(t *testing.T) {
	svc, activities, db := newJobActivityTestServiceInternal(t)
	ctx := t.Context()
	run, err := svc.runs.Submit(ctx, st.Request{JobID: "auto-update", RunID: "b6d679c2-bdf5-4af1-985f-49c468983ba0", Trigger: "scheduled"})
	require.NoError(t, err)
	activityID, err := uuid.Parse(run.ActivityID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil(), activityID)
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
	duplicate, err := svc.runs.Submit(ctx, st.Request{JobID: run.JobID, RunID: run.ID, Trigger: "scheduled"})
	require.NoError(t, err)
	require.Equal(t, run.ActivityID, duplicate.ActivityID)
	for _, status := range []st.RunStatus{st.Running, st.Waiting, st.Failed} {
		require.NoError(t, svc.runs.UpdateRun(ctx, run, func(current *st.Run) error {
			current.Status = status
			current.UpdatedAt = time.Now().UTC()
			current.Outcome = st.Outcome{Status: status, Message: "operation detail"}
			return nil
		}))
	}
	check(activitytypes.StatusFailed, st.Failed)
	failedDetail, err := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.NotNil(t, failedDetail.Activity.Error)
	require.Equal(t, "operation detail", *failedDetail.Activity.Error)
	later, err := svc.runs.Submit(ctx, st.Request{JobID: run.JobID, Trigger: "scheduled"})
	require.NoError(t, err)
	require.NoError(t, svc.runs.UpdateRun(ctx, later, func(current *st.Run) error {
		current.Status = st.Succeeded
		current.UpdatedAt = time.Now().UTC()
		return nil
	}))
	failedDetail, err = activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, activitytypes.StatusFailed, failedDetail.Activity.Status)
	require.Equal(t, "operation detail", *failedDetail.Activity.Error)
	_, err = svc.runs.Retry(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	check(activitytypes.StatusQueued, st.Queued)
	_, err = activities.CancelActivity(ctx, "0", run.ActivityID, "operator")
	require.ErrorIs(t, err, activity.ErrActivityNotCancelable)
	check(activitytypes.StatusQueued, st.Queued)
	require.NoError(t, svc.runs.UpdateRun(ctx, run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.UpdatedAt = time.Now().UTC()
		return nil
	}))
	_, err = svc.runs.Resolve(ctx, "0", run.JobID, run.ID, "operator")
	require.NoError(t, err)
	check(activitytypes.StatusCancelled, st.Canceled)
	var count int64
	require.NoError(t, db.Model(&activity.Activity{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
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
			run, err := svc.runs.Submit(t.Context(), st.Request{JobID: test.job, Trigger: test.trigger, EnvironmentID: test.environment})
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
	run, err := svc.runs.Submit(ctx, st.Request{JobID: "auto-update", Trigger: "scheduled"})
	require.NoError(t, err, "activity failure must not reject accepted work")
	require.NotEmpty(t, run.ActivityID)
	require.NoError(t, svc.runs.UpdateRun(ctx, run, func(current *st.Run) error {
		current.Status = st.NeedsAttention
		current.Outcome = st.Outcome{Message: "interrupted before applying updates"}
		current.UpdatedAt = time.Now().UTC()
		return nil
	}))
	require.NoError(t, db.AutoMigrate(&activity.Activity{}, &activity.ActivityMessage{}))
	restarted := NewJobService(db, nil, &config.Config{}, svc.runs, nil, nil, activities)
	require.NoError(t, restarted.runs.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, restarted.runs.Stop(stopCtx))
	})
	detail, err := activities.GetActivityDetail(ctx, "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, activitytypes.StatusFailed, detail.Activity.Status)
	require.Contains(t, detail.Activity.LatestMessage, "interrupted")
	persisted, err := restarted.runs.Get(ctx, "0", run.JobID, run.ID)
	require.NoError(t, err)
	require.Zero(t, persisted.AttemptCount, "repair must not execute interrupted work")
	require.Equal(t, st.Failed, persisted.Status)
	require.Equal(t, "interrupted before applying updates", persisted.Outcome.Message)
	require.NotNil(t, persisted.FinishedAt)
	require.Equal(t, run.ActivityID, persisted.ActivityID)
}
