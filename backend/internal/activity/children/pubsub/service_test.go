package pubsub

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
)

func TestSubscribeMarksMissedEventsWhenBufferFull(t *testing.T) {
	service := NewService(concurrency.NewSignal[string]())

	events, missedEvents, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	// Message-type events are not coalesced; overflowing the delivery channel
	// plus the bounded pending FIFO must flag missed events so the stream
	// handler resends a snapshot.
	total := cap(events) + subscriberMessageQueueLimit + 50
	for range total {
		service.publish("0", types.ActivityStreamEvent{Type: "message"})
	}
	require.True(t, missedEvents())
	require.False(t, missedEvents())
}

func TestSubscriberCoalescesProgressEvents(t *testing.T) {
	service := NewService(concurrency.NewSignal[string]())

	events, missedEvents, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	const updates = 500
	for i := 1; i <= updates; i++ {
		progress := i * 100 / updates
		service.PublishActivity(types.Activity{
			ID:            "act-1",
			EnvironmentID: "0",
			Status:        types.ActivityStatusRunning,
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

// A stale non-terminal snapshot published after the activity's terminal event
// — a goroutine that committed before the terminal write but publishes after
// it — is dropped instead of reverting subscribers to a running state no later
// event would correct.
func TestDropsStaleSnapshotAfterTerminalPublish(t *testing.T) {
	service := NewService(concurrency.NewSignal[string]())

	stale := types.Activity{
		ID:            "act-1",
		EnvironmentID: "0",
		Type:          types.ActivityTypeImagePull,
		Status:        types.ActivityStatusRunning,
	}
	completed := stale
	completed.Status = types.ActivityStatusSuccess

	events, _, unsubscribe := service.Subscribe("0")
	defer unsubscribe()

	service.PublishActivity(completed)
	select {
	case event := <-events:
		require.Equal(t, "activity", event.Type)
		require.Equal(t, types.ActivityStatusSuccess, event.Activity.Status)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "terminal snapshot was not published")
	}

	require.False(t, service.admit(stale))
	require.True(t, service.admit(completed))

	service.PublishActivity(stale)
	select {
	case localEvent := <-events:
		t.Fatalf("stale non-terminal snapshot reached subscriber: %+v", localEvent)
	case <-time.After(100 * time.Millisecond):
	}
}
