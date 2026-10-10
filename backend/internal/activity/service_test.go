package activity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

func setupActivityServiceTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Activity{}, &ActivityMessage{}))
	return &database.DB{DB: db}
}

func TestActivityServiceLifecycleInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	progress := 5
	startedBy := &user.Actor{
		ID:          "user-1",
		Username:    "arcane",
		DisplayName: new("Arcane Admin"),
	}
	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
		ResourceType:  new("image"),
		ResourceID:    new("img-123"),
		ResourceName:  new("nginx:latest"),
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
		Level:    activity.MessageLevelInfo,
		Message:  "Downloading layers",
		Progress: &progress,
		Step:     "download",
	})
	require.NoError(t, err)
	require.NotNil(t, message)
	require.Equal(t, created.ID, message.ActivityID)

	completed, err := service.CompleteActivity(ctx, created.ID, activity.StatusSuccess, "Pull complete", nil)
	require.NoError(t, err)
	require.Equal(t, "success", string(completed.Status))
	require.NotNil(t, completed.EndedAt)
	require.NotNil(t, completed.DurationMs)
	require.Equal(t, 100, *completed.Progress)

	list, paginationResp, err := service.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
		Limit: 10,
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
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	events, _, unsubscribe := service.Subscribe("0")
	defer unsubscribe()
	otherEvents, _, unsubscribeOther := service.Subscribe("0")
	defer unsubscribeOther()

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeProjectDeploy,
		LatestMessage: "Deploy queued",
	})
	require.NoError(t, err)

	first := receiveActivityEventInternal(t, events)
	require.Equal(t, "activity", first.Type)
	require.Equal(t, created.ID, first.ActivityID)
	require.NotNil(t, first.Activity)
	other := receiveActivityEventInternal(t, otherEvents)
	require.Equal(t, first, other)
	require.NotSame(t, first.Activity, other.Activity)

	_, err = service.AppendMessage(ctx, created.ID, AppendActivityMessageRequest{
		Level:   activity.MessageLevelInfo,
		Message: "Deploying services",
		Step:    "deploy",
	})
	require.NoError(t, err)

	messageEvent := receiveActivityEventInternal(t, events)
	require.Equal(t, "message", messageEvent.Type)
	require.Equal(t, created.ID, messageEvent.ActivityID)
	require.Equal(t, activity.TypeProjectDeploy, messageEvent.ActivityType)
	require.NotNil(t, messageEvent.Message)
	require.Equal(t, "Deploying services", messageEvent.Message.Message)
	require.Nil(t, messageEvent.Activity)
	require.Nil(t, messageEvent.Activities)
	require.Equal(t, messageEvent, receiveActivityEventInternal(t, otherEvents))
}

func TestActivityServiceRetentionCleanupInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeSystemPrune,
		LatestMessage: "Prune started",
	})
	require.NoError(t, err)
	_, err = service.AppendMessage(ctx, created.ID, AppendActivityMessageRequest{
		Message: "Removing unused resources",
	})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, created.ID, activity.StatusSuccess, "Prune complete", nil)
	require.NoError(t, err)

	oldEndedAt := time.Now().Add(-((time.Duration(defaultActivityRetentionDays) * 24 * time.Hour) + time.Hour))
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", created.ID).Update("ended_at", oldEndedAt).Error)

	deleted, err := service.PruneHistory(ctx, defaultActivityRetentionDays, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var activityCount int64
	require.NoError(t, db.Model(&Activity{}).Count(&activityCount).Error)
	require.Zero(t, activityCount)

	var messageCount int64
	require.NoError(t, db.Model(&ActivityMessage{}).Count(&messageCount).Error)
	require.Zero(t, messageCount)
}

func TestActivityServicePruneHistoryZeroRetentionDisablesAgeCleanupInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeSystemPrune,
		LatestMessage: "Prune started",
	})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, created.ID, activity.StatusSuccess, "Prune complete", nil)
	require.NoError(t, err)

	oldEndedAt := time.Now().Add(-((time.Duration(defaultActivityRetentionDays) * 24 * time.Hour) + time.Hour))
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", created.ID).Update("ended_at", oldEndedAt).Error)

	deleted, err := service.PruneHistory(ctx, 0, 0)
	require.NoError(t, err)
	require.Zero(t, deleted)

	var activityCount int64
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", created.ID).Count(&activityCount).Error)
	require.EqualValues(t, 1, activityCount)
}

func TestActivityServiceSubscribeMarksMissedEventsWhenBufferFullInternal(t *testing.T) {
	service := NewActivityService(nil, nil)

	events, missedEvents, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	// Message-type events are not coalesced; overflowing the delivery channel
	// plus the bounded pending FIFO must flag missed events so the stream
	// handler resends a snapshot.
	total := cap(events) + subscriberMessageQueueLimit + 50
	for range total {
		service.publishInternal("0", activity.StreamEvent{Type: "message"})
	}
	require.True(t, missedEvents())
	require.False(t, missedEvents())
}

func TestActivityServiceDeleteHistoryPreservesActiveActivitiesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	completed, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeResourceAction})
	require.NoError(t, err)
	_, err = service.AppendMessage(ctx, completed.ID, AppendActivityMessageRequest{Message: "done"})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, completed.ID, activity.StatusSuccess, "complete", nil)
	require.NoError(t, err)

	running, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeResourceAction})
	require.NoError(t, err)

	remoteCompleted, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: activity.TypeResourceAction})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, remoteCompleted.ID, activity.StatusFailed, "failed", nil)
	require.NoError(t, err)

	deleted, err := service.DeleteHistory(ctx, "0")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining []Activity
	require.NoError(t, db.Order("id").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.ElementsMatch(t, []string{running.ID, remoteCompleted.ID}, []string{remaining[0].ID, remaining[1].ID})
}

func TestActivityServicePruneHistoryByAgeAndCountInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	oldActivity, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeResourceAction})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, oldActivity.ID, activity.StatusSuccess, "old", nil)
	require.NoError(t, err)
	oldTime := time.Now().Add(-48 * time.Hour)
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", oldActivity.ID).Updates(map[string]any{
		"ended_at":   oldTime,
		"updated_at": oldTime,
	}).Error)

	for i := range 3 {
		item, startErr := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: activity.TypeResourceAction})
		require.NoError(t, startErr)
		_, completeErr := service.CompleteActivity(ctx, item.ID, activity.StatusSuccess, "done", nil)
		require.NoError(t, completeErr)
		stamp := time.Now().Add(time.Duration(i) * time.Minute)
		require.NoError(t, db.Model(&Activity{}).Where("id = ?", item.ID).Updates(map[string]any{
			"ended_at":   stamp,
			"updated_at": stamp,
		}).Error)
	}

	running, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "remote-1", Type: activity.TypeResourceAction})
	require.NoError(t, err)

	deleted, err := service.PruneHistory(ctx, 1, 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	var terminalRemoteCount int64
	require.NoError(t, db.Model(&Activity{}).
		Where("environment_id = ? AND status IN ?", "remote-1", terminalActivityStatusesInternal()).
		Count(&terminalRemoteCount).Error)
	require.EqualValues(t, 2, terminalRemoteCount)

	var runningCount int64
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", running.ID).Count(&runningCount).Error)
	require.EqualValues(t, 1, runningCount)

	var oldCount int64
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", oldActivity.ID).Count(&oldCount).Error)
	require.Zero(t, oldCount)
}

func TestActivitySubscriberCoalescesProgressEventsInternal(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	events, missedEvents, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	const updates = 500
	for i := 1; i <= updates; i++ {
		progress := i * 100 / updates
		service.publishActivityInternal(activity.Activity{
			ID:            "act-1",
			EnvironmentID: "0",
			Status:        activity.StatusRunning,
			Progress:      &progress,
		})
	}

	received := 0
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			received++
			require.Equal(t, "activity", event.Type)
			if event.Activity != nil && event.Activity.Progress != nil && *event.Activity.Progress == 100 {
				// The consumer was idle during publishing, so the backlog must
				// have been coalesced instead of delivered event-for-event.
				require.Less(t, received, updates)
				require.False(t, missedEvents())
				return
			}
		case <-deadline:
			require.FailNow(t, "did not receive final coalesced progress event")
		}
	}
}

func TestActivityServiceListOrderStableUnderProgressUpdatesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	older, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull})
	require.NoError(t, err)
	newer, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull})
	require.NoError(t, err)
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", older.ID).
		Update("created_at", time.Now().Add(-time.Minute)).Error)

	terminal, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, terminal.ID, activity.StatusSuccess, "done", nil)
	require.NoError(t, err)

	listIDs := func() []string {
		list, _, listErr := service.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{
			Limit: 10,
		})
		require.NoError(t, listErr)
		ids := make([]string, 0, len(list))
		for _, item := range list {
			ids = append(ids, item.ID)
		}
		return ids
	}

	expected := []string{newer.ID, older.ID, terminal.ID}
	require.Equal(t, expected, listIDs())

	progress := 50
	_, err = service.UpdateActivity(ctx, older.ID, UpdateActivityRequest{Progress: &progress, LatestMessage: new("halfway")})
	require.NoError(t, err)
	require.Equal(t, expected, listIDs())
}

func setupQueuedActivityServiceInternal(t *testing.T) (*ActivityService, context.Context) {
	t.Helper()
	ctx := t.Context()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Activity{}, &ActivityMessage{}, &settings.SettingVariable{}))
	wrapped := &database.DB{DB: db}

	// Every extra pooled connection to a :memory: SQLite database is a fresh
	// empty database; the await goroutine must see the same data.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	localSettings, err := newSettingsServiceForTestInternal(t, ctx, wrapped)
	require.NoError(t, err)
	require.NoError(t, localSettings.SetIntSetting(ctx, maxConcurrentActivitiesSettingKey, 1))
	return NewActivityService(wrapped, localSettings), ctx
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func TestActivityServiceQueuedActivityFlipsToRunningWhenSlotFreesInternal(t *testing.T) {
	service, ctx := setupQueuedActivityServiceInternal(t)

	first, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "running", string(first.Status))

	second, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "queued", string(second.Status))

	awaitDone := make(chan error, 1)
	go func() { awaitDone <- service.AwaitActivitySlot(ctx, second.ID, "0") }()

	select {
	case awaitErr := <-awaitDone:
		require.FailNowf(t, "unexpected failure", "await returned before the slot freed: %v", awaitErr)
	case <-time.After(100 * time.Millisecond):
	}

	_, err = service.CompleteActivity(ctx, first.ID, activity.StatusSuccess, "done", nil)
	require.NoError(t, err)

	select {
	case awaitErr := <-awaitDone:
		require.NoError(t, awaitErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "await did not acquire the freed slot")
	}

	var model Activity
	require.NoError(t, service.db.First(&model, "id = ?", second.ID).Error)
	require.Equal(t, activity.StatusRunning, model.Status)

	_, err = service.CompleteActivity(ctx, second.ID, activity.StatusSuccess, "done", nil)
	require.NoError(t, err)
	firstDeferred, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true, DeferSlot: true})
	require.NoError(t, err)
	secondDeferred, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true, DeferSlot: true})
	require.NoError(t, err)
	require.Equal(t, activity.StatusQueued, firstDeferred.Status)
	require.Equal(t, activity.StatusQueued, secondDeferred.Status)
	require.NoError(t, service.AwaitActivitySlot(ctx, firstDeferred.ID, "0"))
	_, err = service.CompleteActivity(ctx, firstDeferred.ID, activity.StatusSuccess, "done", nil)
	require.NoError(t, err)
	require.NoError(t, service.AwaitActivitySlot(ctx, secondDeferred.ID, "0"))
}

func TestActivityServiceLimitIncreaseKeepsCountingActiveSlotsInternal(t *testing.T) {
	service, ctx := setupQueuedActivityServiceInternal(t)

	running, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "running", string(running.Status))
	waiting, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "queued", string(waiting.Status))

	awaitDone := make(chan error, 1)
	go func() { awaitDone <- service.AwaitActivitySlot(ctx, waiting.ID, "0") }()

	// Raising the limit to 2 must admit the waiter while still counting the
	// original holder, leaving no capacity for a third activity.
	require.NoError(t, service.limiter.settings.SetIntSetting(ctx, maxConcurrentActivitiesSettingKey, 2))

	select {
	case awaitErr := <-awaitDone:
		require.NoError(t, awaitErr)
	case <-time.After(2 * slotWaitRecheckInterval):
		require.FailNow(t, "waiter was not admitted after the limit increase")
	}

	third, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "queued", string(third.Status))
}

func TestActivityServiceCancelWhileQueuedUnblocksAwaitInternal(t *testing.T) {
	service, ctx := setupQueuedActivityServiceInternal(t)

	_, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	queued, err := service.StartActivity(ctx, StartActivityRequest{EnvironmentID: "0", Type: activity.TypeImagePull, Queue: true})
	require.NoError(t, err)
	require.Equal(t, "queued", string(queued.Status))

	workCtx := service.Track(ctx, queued.ID)
	awaitDone := make(chan error, 1)
	go func() { awaitDone <- service.AwaitActivitySlot(workCtx, queued.ID, "0") }()

	require.True(t, service.RequestCancel(queued.ID))

	select {
	case awaitErr := <-awaitDone:
		require.ErrorIs(t, awaitErr, activitylib.ErrCanceled)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "cancel did not unblock the queued slot wait")
	}
}

func TestActivityServiceCompleteActivityRejectsUninitializedServiceInternal(t *testing.T) {
	service := NewActivityService(nil, nil)
	_, err := service.CompleteActivity(t.Context(), "any-id", activity.StatusSuccess, "done", nil)
	require.Error(t, err)
}

func TestActivityServiceTrackAndRequestCancelInternal(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	// Mirror the handler flow: work runs under an app-lifecycle runtime context.
	appCtx := utils.WithAppLifecycleContext(t.Context())
	runtimeCtx := utils.ActivityRuntimeContext(t.Context(), appCtx)

	created, err := service.StartActivity(runtimeCtx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
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
	completed, err := service.CompleteActivity(utils.ActivityRuntimeContext(workCtx, nil), created.ID, activity.StatusCancelled, "Cancelled by user", nil)
	require.NoError(t, err)
	require.Equal(t, "cancelled", string(completed.Status))
	require.NotNil(t, completed.EndedAt)

	// Completing the activity releases the registration.
	require.False(t, service.RequestCancel(created.ID))
}

func TestActivityServiceCancelActivityInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	// An untracked running activity (e.g. after a restart) is finalized directly.
	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeSystemPrune,
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

func TestActivityServiceFailStaleImageUpdateChecksInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	staleCheck, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImageUpdateCheck,
		LatestMessage: "checking",
	})
	require.NoError(t, err)
	freshCheck, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImageUpdateCheck,
		LatestMessage: "checking",
	})
	require.NoError(t, err)
	staleOtherType, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
		LatestMessage: "pulling",
	})
	require.NoError(t, err)
	completedCheck, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImageUpdateCheck,
		LatestMessage: "checking",
	})
	require.NoError(t, err)
	_, err = service.CompleteActivity(ctx, completedCheck.ID, activity.StatusSuccess, "complete", nil)
	require.NoError(t, err)

	oldStartedAt := time.Now().Add(-7 * time.Hour)
	for _, id := range []string{staleCheck.ID, staleOtherType.ID, completedCheck.ID} {
		require.NoError(t, db.Model(&Activity{}).Where("id = ?", id).Updates(map[string]any{
			"started_at": oldStartedAt,
			"updated_at": oldStartedAt,
		}).Error)
	}

	failed, err := service.FailStaleImageUpdateChecks(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, failed)

	var stale Activity
	require.NoError(t, db.First(&stale, "id = ?", staleCheck.ID).Error)
	require.Equal(t, activity.StatusFailed, stale.Status)
	require.NotNil(t, stale.EndedAt)
	require.NotNil(t, stale.DurationMs)
	require.Contains(t, stale.LatestMessage, "stale")
	require.NotNil(t, stale.Error)
	require.Contains(t, *stale.Error, "stale")

	var fresh Activity
	require.NoError(t, db.First(&fresh, "id = ?", freshCheck.ID).Error)
	require.Equal(t, activity.StatusRunning, fresh.Status)
	require.Nil(t, fresh.EndedAt)

	var other Activity
	require.NoError(t, db.First(&other, "id = ?", staleOtherType.ID).Error)
	require.Equal(t, activity.StatusRunning, other.Status)
	require.Nil(t, other.EndedAt)

	var completed Activity
	require.NoError(t, db.First(&completed, "id = ?", completedCheck.ID).Error)
	require.Equal(t, activity.StatusSuccess, completed.Status)
}

func TestActivityServiceFailAbandonedActivitiesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	abandoned, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImageUpdateCheck,
		LatestMessage: "checking",
	})
	require.NoError(t, err)
	tracked, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeAutoUpdate,
		LatestMessage: "updating",
	})
	require.NoError(t, err)
	fresh, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImageUpdateCheck,
		LatestMessage: "checking",
	})
	require.NoError(t, err)

	backdated := time.Now().Add(-10 * time.Minute)
	require.NoError(t, db.Model(&Activity{}).
		Where("id IN ?", []string{abandoned.ID, tracked.ID}).
		Update("started_at", backdated).Error)
	_ = service.Track(ctx, tracked.ID)

	swept, err := service.FailAbandonedActivities(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, swept)

	var abandonedRow Activity
	require.NoError(t, db.First(&abandonedRow, "id = ?", abandoned.ID).Error)
	require.Equal(t, activity.StatusFailed, abandonedRow.Status)
	require.NotNil(t, abandonedRow.EndedAt)
	require.Contains(t, abandonedRow.LatestMessage, "worker is no longer running")

	var trackedRow Activity
	require.NoError(t, db.First(&trackedRow, "id = ?", tracked.ID).Error)
	require.Equal(t, activity.StatusRunning, trackedRow.Status)

	var freshRow Activity
	require.NoError(t, db.First(&freshRow, "id = ?", fresh.ID).Error)
	require.Equal(t, activity.StatusRunning, freshRow.Status)
}

func TestActivityServiceResolveStaleAutoUpdateActivitiesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	selfUpdateRun, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeAutoUpdate,
		LatestMessage: "updating",
		Metadata:      database.JSON{"dryRun": false},
	})
	require.NoError(t, err)
	require.NoError(t, service.PatchActivityMetadata(ctx, selfUpdateRun.ID, database.JSON{"selfUpdateTriggered": true}))

	interruptedRun, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeAutoUpdate,
		LatestMessage: "updating",
	})
	require.NoError(t, err)
	otherType, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
		LatestMessage: "pulling",
	})
	require.NoError(t, err)

	resolved, err := service.ResolveStaleAutoUpdateActivities(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, resolved)

	var selfUpdated Activity
	require.NoError(t, db.First(&selfUpdated, "id = ?", selfUpdateRun.ID).Error)
	require.Equal(t, activity.StatusSuccess, selfUpdated.Status)
	require.NotNil(t, selfUpdated.EndedAt)
	require.Contains(t, selfUpdated.LatestMessage, "restarted with the updated image")
	require.Equal(t, false, selfUpdated.Metadata["dryRun"])

	var interrupted Activity
	require.NoError(t, db.First(&interrupted, "id = ?", interruptedRun.ID).Error)
	require.Equal(t, activity.StatusFailed, interrupted.Status)
	require.Contains(t, interrupted.LatestMessage, "interrupted")

	var other Activity
	require.NoError(t, db.First(&other, "id = ?", otherType.ID).Error)
	require.Equal(t, activity.StatusRunning, other.Status)
}

func receiveActivityEventInternal(t *testing.T, events <-chan activity.StreamEvent) activity.StreamEvent {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for activity event")
		return activity.StreamEvent{}
	}
}

// TestActivityServiceAppendMessagesBatchInternal verifies a batch lands all
// messages in order and coalesces the activity update to the batch's last
// message, last progress, and last step.
func TestActivityServiceAppendMessagesBatchInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
		Step:          "queued",
		LatestMessage: "Pull queued",
	})
	require.NoError(t, err)

	progress := 60
	messages, err := service.AppendMessages(ctx, created.ID, []AppendActivityMessageRequest{
		{Message: "layer 1/3", Step: "download"},
		{Message: "   "}, // blank lines are dropped, not persisted
		{Message: "layer 2/3", Progress: &progress},
		{Message: "layer 3/3"},
	})
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Equal(t, "layer 1/3", messages[0].Message)
	require.Equal(t, "layer 3/3", messages[2].Message)

	detail, err := service.GetActivityDetail(ctx, "0", created.ID, 10)
	require.NoError(t, err)
	require.Len(t, detail.Messages, 3)
	require.Equal(t, "layer 1/3", detail.Messages[0].Message)
	require.Equal(t, "layer 2/3", detail.Messages[1].Message)
	require.Equal(t, "layer 3/3", detail.Messages[2].Message)

	// Coalesced activity update: last message wins, last non-nil progress
	// and last non-empty step stick.
	require.Equal(t, "layer 3/3", detail.Activity.LatestMessage)
	require.NotNil(t, detail.Activity.Progress)
	require.Equal(t, 60, *detail.Activity.Progress)
	require.Equal(t, "download", detail.Activity.Step)

	// Unknown activity IDs are still rejected.
	_, err = service.AppendMessages(ctx, "missing", []AppendActivityMessageRequest{{Message: "x"}})
	require.ErrorContains(t, err, "activity not found")
}

// TestActivityServiceDropsStaleSnapshotAfterTerminalPublishInternal verifies
// that a non-terminal activity snapshot published after the activity's
// terminal event — a goroutine that committed before CompleteActivity but
// publishes after it — is dropped instead of reverting subscribers to a
// running state no later event would correct.
func TestActivityServiceDropsStaleSnapshotAfterTerminalPublishInternal(t *testing.T) {
	ctx := t.Context()
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	created, err := service.StartActivity(ctx, StartActivityRequest{
		EnvironmentID: "0",
		Type:          activity.TypeImagePull,
		LatestMessage: "Pull queued",
	})
	require.NoError(t, err)
	stale := *created // snapshot taken while the activity is still active

	events, _, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	completed, err := service.CompleteActivity(ctx, created.ID, activity.StatusSuccess, "", nil)
	require.NoError(t, err)
	require.Equal(t, activity.StatusSuccess, completed.Status)

	event := receiveActivityEventInternal(t, events)
	require.Equal(t, "activity", event.Type)
	require.Equal(t, activity.StatusSuccess, event.Activity.Status)

	require.False(t, service.admitActivityPublishInternal(stale))
	require.True(t, service.admitActivityPublishInternal(*completed))

	service.publishActivityInternal(stale)
	select {
	case localEvent := <-events:
		t.Fatalf("stale non-terminal snapshot reached subscriber: %+v", localEvent)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestJobSummaryReopensAndRejectsStaleSnapshots(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)
	now := time.Now().UTC()
	run := scheduler.Run{ID: "run", ActivityID: "summary", JobID: "auto-update", EnvironmentID: "0", Status: scheduler.NeedsAttention, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	require.NoError(t, service.SyncJobRun(t.Context(), run, "Auto update"))
	stale := run
	run.Status = scheduler.Queued
	run.UpdatedAt = now.Add(time.Second)
	require.NoError(t, service.SyncJobRun(t.Context(), run, "Auto update"))
	require.NoError(t, service.SyncJobRun(t.Context(), stale, "Auto update"))
	detail, err := service.GetActivityDetail(t.Context(), "0", run.ActivityID, 10)
	require.NoError(t, err)
	require.Equal(t, activity.StatusQueued, detail.Activity.Status)
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
	remoteItems := []activity.Activity{{ID: "agent-work", EnvironmentID: "0", Type: activity.TypeImagePull, Status: activity.StatusRunning, CreatedAt: now}}
	rows, page, err = service.ListRemoteActivities(ctx, "remote", remoteItems, int64(len(remoteItems)), pagination.QueryParams{Start: 1, Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, rows, 1)
	require.Equal(t, run.ActivityID, rows[0].ID)
	rows, _, err = service.ListRemoteActivities(ctx, "remote", remoteItems, int64(len(remoteItems)), pagination.QueryParams{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, "agent-work", rows[0].ID)
	require.Equal(t, "remote", rows[0].EnvironmentID)
	run.Status = scheduler.Succeeded
	run.UpdatedAt = run.UpdatedAt.Add(time.Second)
	require.NoError(t, service.SyncJobRun(ctx, run, "Auto update"))
	deleted, err := service.DeleteHistory(ctx, "remote")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	count, err = service.FailAbandonedActivities(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestJobActivityVisibilityFiltersBeforePagination(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)
	for _, environmentID := range []string{"0", "private"} {
		require.NoError(
			t,
			db.Create(&Activity{
				ID:            environmentID + "-job",
				EnvironmentID: environmentID,
				Type:          activity.TypeJobRun,
				Status:        activity.StatusSuccess,
				StartedAt:     time.Now(),
				Metadata:      database.JSON{"environmentId": environmentID},
			}).Error,
		)
		require.NoError(
			t,
			db.Create(&Activity{
				ID:            environmentID + "-pull",
				EnvironmentID: environmentID,
				Type:          activity.TypeImagePull,
				Status:        activity.StatusSuccess,
				StartedAt:     time.Now(),
			}).Error,
		)
	}
	permissions := authz.NewPermissionSet()
	permissions.PerEnv["0"] = map[string]struct{}{authz.PermActivitiesRead: {}}
	permissions.PerEnv["allowed"] = map[string]struct{}{authz.PermActivitiesRead: {}}
	ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, permissions)
	seen := make(map[string]bool)
	for page := range 2 {
		activities, response, err := service.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{Start: page, Limit: 1})
		require.NoError(t, err)
		require.EqualValues(t, 2, response.TotalItems)
		require.Len(t, activities, 1)
		require.True(t, canReadJobActivityInternal(ctx, activities[0]))
		seen[activities[0].ID] = true
	}
	require.Len(t, seen, 2)
	activities, response, err := service.ListActivitiesPaginated(ctx, "private", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, response.TotalItems)
	require.Len(t, activities, 1)
	require.Equal(t, "private-pull", activities[0].ID)
	require.False(
		t,
		canReadJobActivityInternal(
			ctx,
			activity.Activity{
				Type:          activity.TypeJobRun,
				EnvironmentID: "private",
				Metadata:      map[string]any{"environmentId": "allowed"},
			},
		),
	)
	require.True(t, canReadJobActivityInternal(ctx, activity.Activity{Type: activity.TypeJobRun, Metadata: map[string]any{"environmentId": "allowed"}}))
	privileged := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
	activities, response, err = service.ListActivitiesPaginated(privileged, "private", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, activities, 2)
	require.EqualValues(t, 2, response.TotalItems)
	require.False(t, canReadJobActivityInternal(privileged, activity.Activity{Type: activity.TypeJobRun}))
	permissions.PerEnv["0"][authz.PermActivitiesDelete] = struct{}{}
	deleted, err := service.DeleteHistory(ctx, "private")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.NoError(t, db.First(&Activity{}, "id = ?", "private-job").Error)
	deleted, err = service.DeleteHistory(ctx, "0")
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
}

func TestSyncResourcesToEnvironmentOutcomes(t *testing.T) {
	paths := []string{"/api/container-registries/sync", "/api/backups/s3/sync", "/api/git-repositories/sync"}
	cases := []struct {
		name     string
		failures map[string]string
		groups   string
	}{
		{name: "success"},
		{name: "registry failure", failures: map[string]string{paths[0]: "status"}, groups: "container registries"},
		{name: "S3 failure", failures: map[string]string{paths[1]: "status"}, groups: "S3 destinations"},
		{name: "repository failure", failures: map[string]string{paths[2]: "status"}, groups: "git repositories"},
		{name: "multiple failures", failures: map[string]string{paths[0]: "status", paths[2]: "status"}, groups: "container registries, git repositories"},
		{name: "all fail", failures: map[string]string{paths[0]: "status", paths[1]: "status", paths[2]: "status"}, groups: "container registries, S3 destinations, git repositories"},
		{name: "malformed response", failures: map[string]string{paths[0]: "malformed"}, groups: "container registries"},
		{name: "agent reports failure", failures: map[string]string{paths[0]: "false"}, groups: "container registries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupActivityServiceTestDBInternal(t)
			require.NoError(t, db.AutoMigrate(&environment.Environment{}, &registry.ContainerRegistry{}, &gitrepo.GitRepository{}, &s3.S3Destination{}))
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			crypto.InitEncryption(&crypto.Config{EncryptionKey: "test-encryption-key-for-testing-32bytes-min", Environment: "test"})
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.URL.Path)
				mu.Unlock()
				switch tc.failures[r.URL.Path] {
				case "status":
					http.Error(w, "private-agent-response", http.StatusUnauthorized)
				case "malformed":
					_, _ = w.Write([]byte("private-agent-response"))
				case "false":
					_, _ = w.Write([]byte(`{"success":false,"data":{"message":"private-agent-response"}}`))
				default:
					_, _ = w.Write([]byte(`{"success":true}`))
				}
			}))
			defer server.Close()
			require.NoError(t, db.Create(&environment.Environment{ID: "remote", Name: "Remote", ApiUrl: server.URL, AccessToken: new("agent-token"), Enabled: true}).Error)
			service := environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)
			activityService := NewActivityService(db, nil)
			id, err := service.SyncResourcesToEnvironment(t.Context(), "remote", &user.Actor{ID: "operator", Username: "operator"}, activityService)
			require.NotEmpty(t, id)
			var recorded Activity
			require.NoError(t, db.First(&recorded, "id = ?", id).Error)
			require.NotNil(t, recorded.EndedAt)
			require.NotNil(t, recorded.DurationMs)
			require.Equal(t, "remote", recorded.EnvironmentID)
			if tc.groups == "" {
				require.NoError(t, err)
				require.Equal(t, activity.StatusSuccess, recorded.Status)
				require.Equal(t, "Environment synced successfully", recorded.LatestMessage)
				require.Nil(t, recorded.Error)
			} else {
				require.EqualError(t, err, "Failed to sync "+tc.groups+". Other resource groups may have synced successfully. Check the manager logs, correct the failed sync, and retry.")
				require.Equal(t, activity.StatusFailed, recorded.Status)
				require.NotNil(t, recorded.Error)
				require.Equal(t, err.Error(), *recorded.Error)
				require.Equal(t, err.Error(), recorded.LatestMessage)
				require.NotContains(t, err.Error(), "private-agent-response")
				require.NotContains(t, err.Error(), "agent-token")
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, paths, calls)
		})
	}
}

func TestActivityService_StartActivityWithIDIsIdempotent(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)

	first, err := service.StartActivity(t.Context(), StartActivityRequest{ID: "workflow-activity", Type: activity.TypeAutoUpdate, LatestMessage: "first"})
	require.NoError(t, err)
	require.Equal(t, "workflow-activity", first.ID)

	again, err := service.StartActivity(t.Context(), StartActivityRequest{ID: "workflow-activity", Type: activity.TypeAutoUpdate, LatestMessage: "redelivered"})
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Equal(t, "first", again.LatestMessage)

	var count int64
	require.NoError(t, db.Model(&Activity{}).Where("id = ?", "workflow-activity").Count(&count).Error)
	require.Equal(t, int64(1), count)
}
