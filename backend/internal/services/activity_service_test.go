package services

import (
	"context"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	activitylib "github.com/getarcaneapp/arcane/backend/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	activitytypes "github.com/getarcaneapp/arcane/types/activity"
)

func setupActivityServiceTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Activity{}, &models.ActivityMessage{}))
	return &database.DB{DB: db}
}

func TestActivityServiceLifecycleInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	resourceType := "image"
	resourceID := "img-123"
	resourceName := "nginx:latest"
	progress := 5
	displayName := "Arcane Admin"
	startedBy := &models.User{
		BaseModel:   models.BaseModel{ID: "user-1"},
		Username:    "arcane",
		DisplayName: &displayName,
	}
	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeImagePull,
		ResourceType:  &resourceType,
		ResourceID:    &resourceID,
		ResourceName:  &resourceName,
		StartedBy:     startedBy,
		Progress:      &progress,
		Step:          "queued",
		LatestMessage: "Pull queued",
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "0", created.EnvironmentID)
	require.Equal(t, "running", string(created.Status))
	require.Equal(t, 5, *created.Progress)
	require.NotNil(t, created.StartedBy)
	require.Equal(t, "user-1", created.StartedBy.UserID)
	require.Equal(t, "arcane", created.StartedBy.Username)
	require.Equal(t, "Arcane Admin", created.StartedBy.DisplayName)

	progress = 42
	message, err := service.AppendMessage(ctx, created.ID, AppendActivityMessageRequest{
		Level:    models.ActivityMessageLevelInfo,
		Message:  "Downloading layers",
		Progress: &progress,
		Step:     "download",
	})
	require.NoError(t, err)
	require.NotNil(t, message)
	require.Equal(t, created.ID, message.ActivityID)

	completed, err := service.CompleteActivity(ctx, created.ID, models.ActivityStatusSuccess, "Pull complete", nil)
	require.NoError(t, err)
	require.Equal(t, "success", string(completed.Status))
	require.NotNil(t, completed.EndedAt)
	require.NotNil(t, completed.DurationMs)
	require.Equal(t, 100, *completed.Progress)

	list, paginationResp, err := service.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
		PaginationParams: pagination.PaginationParams{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(1), paginationResp.TotalItems)
	require.Equal(t, created.ID, list[0].ID)

	detail, err := service.GetActivityDetail(ctx, "0", created.ID, 10)
	require.NoError(t, err)
	require.Equal(t, created.ID, detail.Activity.ID)
	require.Len(t, detail.Messages, 2)
	require.Equal(t, "Downloading layers", detail.Messages[0].Message)
	require.Equal(t, "Pull complete", detail.Messages[1].Message)
}

func TestActivityServiceStreamFanoutInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	events, _, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeProjectDeploy,
		LatestMessage: "Deploy queued",
	})
	require.NoError(t, err)

	first := receiveActivityEventInternal(t, events)
	require.Equal(t, "activity", first.Type)
	require.Equal(t, created.ID, first.ActivityID)
	require.NotNil(t, first.Activity)

	_, err = service.AppendMessage(ctx, created.ID, AppendActivityMessageRequest{
		Level:   models.ActivityMessageLevelInfo,
		Message: "Deploying services",
		Step:    "deploy",
	})
	require.NoError(t, err)

	messageEvent := receiveActivityEventInternal(t, events)
	require.Equal(t, "message", messageEvent.Type)
	require.Equal(t, created.ID, messageEvent.ActivityID)
	require.NotNil(t, messageEvent.Message)
	require.Equal(t, "Deploying services", messageEvent.Message.Message)
}

func TestActivityServiceRetentionCleanupInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeSystemPrune,
		LatestMessage: "Prune started",
	})
	require.NoError(t, err)
	_, err = service.AppendMessage(ctx, created.ID, AppendActivityMessageRequest{
		Message: "Removing unused resources",
	})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, created.ID, models.ActivityStatusSuccess, "Prune complete", nil)
	require.NoError(t, err)

	oldEndedAt := time.Now().Add(-((time.Duration(defaultActivityRetentionDays) * 24 * time.Hour) + time.Hour))
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", created.ID).Update("ended_at", oldEndedAt).Error)

	deleted, err := service.PruneHistory(ctx, defaultActivityRetentionDays, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var activityCount int64
	require.NoError(t, db.Model(&models.Activity{}).Count(&activityCount).Error)
	require.Zero(t, activityCount)

	var messageCount int64
	require.NoError(t, db.Model(&models.ActivityMessage{}).Count(&messageCount).Error)
	require.Zero(t, messageCount)
}

func TestActivityServicePruneHistoryZeroRetentionDisablesAgeCleanupInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeSystemPrune,
		LatestMessage: "Prune started",
	})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, created.ID, models.ActivityStatusSuccess, "Prune complete", nil)
	require.NoError(t, err)

	oldEndedAt := time.Now().Add(-((time.Duration(defaultActivityRetentionDays) * 24 * time.Hour) + time.Hour))
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", created.ID).Update("ended_at", oldEndedAt).Error)

	deleted, err := service.PruneHistory(ctx, 0, 0)
	require.NoError(t, err)
	require.Zero(t, deleted)

	var activityCount int64
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", created.ID).Count(&activityCount).Error)
	require.EqualValues(t, 1, activityCount)
}

func TestActivityServiceSubscribeMarksMissedEventsWhenBufferFullInternal(t *testing.T) {
	service := NewActivityService(nil)

	events, missedEvents, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	for i := 0; i < cap(events); i++ {
		service.publishInternal("0", activitytypes.StreamEvent{Type: "activity"})
	}
	require.False(t, missedEvents())

	service.publishInternal("0", activitytypes.StreamEvent{Type: "activity"})
	require.True(t, missedEvents())
	require.False(t, missedEvents())
}

func TestActivityServiceDeleteHistoryPreservesActiveActivitiesInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	completed, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	_, err = service.AppendMessage(ctx, completed.ID, AppendActivityMessageRequest{Message: "done"})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, completed.ID, models.ActivityStatusSuccess, "complete", nil)
	require.NoError(t, err)

	running, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)

	remoteCompleted, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, remoteCompleted.ID, models.ActivityStatusFailed, "failed", nil)
	require.NoError(t, err)

	deleted, err := service.DeleteHistory(ctx, "0")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining []models.Activity
	require.NoError(t, db.Order("id").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.ElementsMatch(t, []string{running.ID, remoteCompleted.ID}, []string{remaining[0].ID, remaining[1].ID})
}

func TestActivityServicePruneHistoryByAgeAndCountInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	oldActivity, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, oldActivity.ID, models.ActivityStatusSuccess, "old", nil)
	require.NoError(t, err)
	oldTime := time.Now().Add(-48 * time.Hour)
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", oldActivity.ID).Updates(map[string]any{
		"ended_at":   oldTime,
		"updated_at": oldTime,
	}).Error)

	for i := 0; i < 3; i++ {
		item, startErr := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: models.ActivityTypeResourceAction})
		require.NoError(t, startErr)
		_, completeErr := service.CompleteActivity(ctx, item.ID, models.ActivityStatusSuccess, "done", nil)
		require.NoError(t, completeErr)
		stamp := time.Now().Add(time.Duration(i) * time.Minute)
		require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", item.ID).Updates(map[string]any{
			"ended_at":   stamp,
			"updated_at": stamp,
		}).Error)
	}

	running, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)

	deleted, err := service.PruneHistory(ctx, 1, 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	var terminalRemoteCount int64
	require.NoError(t, db.Model(&models.Activity{}).
		Where("environment_id = ? AND status IN ?", "remote-1", terminalActivityStatusesInternal()).
		Count(&terminalRemoteCount).Error)
	require.EqualValues(t, 2, terminalRemoteCount)

	var runningCount int64
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", running.ID).Count(&runningCount).Error)
	require.EqualValues(t, 1, runningCount)

	var oldCount int64
	require.NoError(t, db.Model(&models.Activity{}).Where("id = ?", oldActivity.ID).Count(&oldCount).Error)
	require.Zero(t, oldCount)
}

func TestActivityServiceCompleteActivityRejectsUninitializedServiceInternal(t *testing.T) {
	service := NewActivityService(nil)
	_, err := service.CompleteActivity(context.Background(), "any-id", models.ActivityStatusSuccess, "done", nil)
	require.Error(t, err)
}

func TestActivityServiceTrackAndRequestCancelInternal(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	// Mirror the handler flow: work runs under an app-lifecycle runtime context.
	appCtx := utils.WithAppLifecycleContext(context.Background())
	runtimeCtx := utils.ActivityRuntimeContext(context.Background(), appCtx)

	created, err := service.StartActivity(runtimeCtx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeImagePull,
		LatestMessage: "running",
	})
	require.NoError(t, err)

	workCtx := service.Track(runtimeCtx, created.ID)
	require.NoError(t, workCtx.Err())

	// A tracked activity is found and cancelled with the ErrCanceled cause.
	require.True(t, service.RequestCancel(created.ID))
	require.ErrorIs(t, workCtx.Err(), context.Canceled)
	require.ErrorIs(t, context.Cause(workCtx), activitylib.ErrCanceled)
	require.True(t, activitylib.CancelledByContext(workCtx))

	// Completion must land even though the work context is cancelled (this is the
	// path CompleteHandlerActivity takes after re-wrapping the work context).
	completed, err := service.CompleteActivity(utils.ActivityRuntimeContext(workCtx, nil), created.ID, models.ActivityStatusCancelled, "Cancelled by user", nil)
	require.NoError(t, err)
	require.Equal(t, "cancelled", string(completed.Status))
	require.NotNil(t, completed.EndedAt)

	// Completing the activity releases the registration.
	require.False(t, service.RequestCancel(created.ID))
}

func TestActivityServiceCancelActivityInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db)

	// An untracked running activity (e.g. after a restart) is finalized directly.
	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          models.ActivityTypeSystemPrune,
		LatestMessage: "running",
	})
	require.NoError(t, err)

	cancelled, err := service.CancelActivity(ctx, "0", created.ID, "Tester")
	require.NoError(t, err)
	require.Equal(t, "cancelled", string(cancelled.Status))
	require.NotNil(t, cancelled.EndedAt)

	// Cancelling an already-terminal activity is rejected.
	_, err = service.CancelActivity(ctx, "0", created.ID, "Tester")
	require.ErrorIs(t, err, ErrActivityNotCancelable)

	// Unknown activity reports not found.
	_, err = service.CancelActivity(ctx, "0", "missing", "Tester")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func receiveActivityEventInternal(t *testing.T, events <-chan activitytypes.StreamEvent) activitytypes.StreamEvent {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for activity event")
		return activitytypes.StreamEvent{}
	}
}
