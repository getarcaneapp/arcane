package event

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	eventtypes "github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/stretchr/testify/require"
)

func TestEventStreamPublishesCommittedMutations(t *testing.T) {
	db := setupEventServiceTestDB(t)
	service := NewEventService(db, nil, nil)
	changes := 0
	var persistedCounts []int64
	unsubscribe := service.changes.Subscribe(func(struct{}) {
		changes++
		var count int64
		require.NoError(t, db.Model(&Event{}).Count(&count).Error)
		persistedCounts = append(persistedCounts, count)
	})
	defer unsubscribe()
	created, err := service.CreateEvent(t.Context(), CreateEventRequest{Type: EventTypeImagePull, Title: "Pulled"})
	require.NoError(t, err)
	require.Equal(t, 1, changes)
	_, err = service.IngestAgentEvent(t.Context(), "remote", CreateEventRequest{Type: EventTypeImagePull, Title: "Remote pull"})
	require.NoError(t, err)
	require.Equal(t, 2, changes)
	require.NoError(t, service.DeleteOldEvents(t.Context(), time.Hour))
	require.Equal(t, 2, changes)
	require.Error(t, service.DeleteEvent(t.Context(), "missing"))
	require.Equal(t, 2, changes)
	require.NoError(t, service.DeleteEvent(t.Context(), created.ID))
	require.Equal(t, 3, changes)
	require.NoError(t, service.DeleteOldEvents(t.Context(), -time.Hour))
	require.Equal(t, 4, changes)
	require.Equal(t, []int64{1, 2, 1, 0}, persistedCounts)
	require.NoError(t, db.Migrator().DropTable(&Event{}))
	_, err = service.CreateEvent(t.Context(), CreateEventRequest{Title: "Cannot persist"})
	require.Error(t, err)
	require.Error(t, service.DeleteOldEvents(t.Context(), time.Hour))
	require.Error(t, service.DeleteEvent(t.Context(), created.ID))
	require.Equal(t, 4, changes)
}

func TestEventStreamProducerCoalescesAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		output := make(chan eventtypes.StreamEvent)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			service.RunStreamProducer(ctx, output)
		}()
		synctest.Wait()
		// Initial send is blocked, but subscriptions must already be ready.
		for range 100 {
			service.changes.Publish(struct{}{})
		}
		initial := <-output
		require.Equal(t, "changed", initial.Type)
		require.False(t, initial.Timestamp.IsZero())
		require.Equal(t, "changed", (<-output).Type)
		synctest.Wait()
		select {
		case <-output:
			t.Fatal("burst should coalesce to one pending invalidation")
		default:
		}
		// A stalled client must not hold up publishers or prevent shutdown.
		service.changes.Publish(struct{}{})
		synctest.Wait()
		cancel()
		<-stopped
		service.changes.Publish(struct{}{})
	})
}

func TestEventStreamProducerBroadcasts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		first := make(chan eventtypes.StreamEvent, 1)
		second := make(chan eventtypes.StreamEvent, 1)
		go service.RunStreamProducer(ctx, first)
		go service.RunStreamProducer(ctx, second)
		synctest.Wait()
		<-first
		<-second
		service.changes.Publish(struct{}{})
		synctest.Wait()
		require.Equal(t, "changed", (<-first).Type)
		require.Equal(t, "changed", (<-second).Type)
	})
}
