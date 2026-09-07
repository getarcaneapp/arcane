package activity

import (
	"context"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"os"
	"strings"
	"testing"
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	st "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/stretchr/testify/require"
)

func TestJobSummaryReopensAndRejectsStaleSnapshots(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)
	now := time.Now().UTC()
	run := st.Run{ID: "run", ActivityID: "summary", JobID: "auto-update", EnvironmentID: "0", Status: st.NeedsAttention, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	require.NoError(t, service.SyncJobRun(t.Context(), run, "Auto update"))
	stale := run
	run.Status = st.Queued
	run.UpdatedAt = now.Add(time.Second)
	require.NoError(t, service.SyncJobRun(t.Context(), run, "Auto update"))
	require.NoError(t, service.SyncJobRun(t.Context(), stale, "Auto update"))
	detail, err := service.GetActivityDetail(t.Context(), "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, activitytypes.StatusQueued, detail.Activity.Status)
	require.Nil(t, detail.Activity.EndedAt)
	require.Nil(t, detail.Activity.Error)
	require.Empty(t, service.slotReleases)
	require.Empty(t, service.running)
	require.NotContains(t, service.terminalPublished, run.ActivityID)
	count, err := service.ResolveOrphanedQueuedActivities(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	// Repair a legacy summary even when its durable timestamp has not changed.
	run.EnvironmentID = "remote"
	var legacy Activity
	require.NoError(t, db.First(&legacy, "id = ?", run.ActivityID).Error)
	legacy.Metadata["environmentId"] = "remote"
	require.NoError(t, db.Model(&legacy).Update("metadata", legacy.Metadata).Error)
	migration, err := os.ReadFile("../../resources/migrations/sqlite/084_route_job_activities_to_environment.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(strings.Split(string(migration), "-- +goose Down")[0]).Error)
	require.NoError(t, db.First(&legacy, "id = ?", run.ActivityID).Error)
	require.Equal(t, "remote", legacy.EnvironmentID)
	// Startup reconciliation also repairs legacy rows without replaying the job.
	require.NoError(t, db.Model(&legacy).Update("environment_id", "0").Error)
	events, _, unsubscribe := service.Subscribe("remote")
	defer unsubscribe()
	require.NoError(t, service.SyncJobRun(t.Context(), run, "Auto update"))
	select {
	case event := <-events:
		require.NotNil(t, event.Activity)
		require.Equal(t, "remote", event.Activity.EnvironmentID)
	case <-time.After(time.Second):
		t.Fatal("summary update was not published to the remote environment")
	}
	_, err = service.GetActivityDetail(t.Context(), "0", run.ActivityID, 10)
	require.Error(t, err)
	permissions := authz.NewPermissionSet()
	permissions.AddEnv("remote", authz.PermActivitiesRead, authz.PermActivitiesDelete)
	ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, permissions)
	rows, page, err := service.ListActivitiesPaginated(ctx, "remote", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Equal(t, "remote", rows[0].EnvironmentID)
	remoteItems := []activitytypes.Activity{{ID: "agent-work", EnvironmentID: "0", Type: activitytypes.TypeImagePull, Status: activitytypes.StatusRunning, CreatedAt: now}}
	rows, page, err = service.ListRemoteActivities(ctx, "remote", remoteItems, pagination.QueryParams{Start: 1, Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, rows, 1)
	require.Equal(t, run.ActivityID, rows[0].ID)
	rows, _, err = service.ListRemoteActivities(ctx, "remote", remoteItems, pagination.QueryParams{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, "agent-work", rows[0].ID)
	require.Equal(t, "remote", rows[0].EnvironmentID)
	run.Status = st.Succeeded
	run.UpdatedAt = run.UpdatedAt.Add(time.Second)
	require.NoError(t, service.SyncJobRun(ctx, run, "Auto update"))
	deleted, err := service.DeleteHistory(ctx, "remote")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	count, err = service.FailAbandonedActivities(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}
