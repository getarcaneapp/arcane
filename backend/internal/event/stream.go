package event

import (
	"context"
	"time"

	eventtypes "github.com/getarcaneapp/arcane/types/v2/event"
	"go.getarcane.app/streams/agg"
)

// RunStreamProducer signals committed event changes so clients can refresh their current query.
func (s *EventService) RunStreamProducer(ctx context.Context, events chan<- eventtypes.StreamEvent) {
	changed := make(chan struct{}, 1)
	unsubscribe := s.changes.Subscribe(func(struct{}) {
		// One pending invalidation covers all changes, without blocking event persistence.
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	defer unsubscribe()

	// Subscribe first so connecting and reconnecting clients cannot miss a change.
	if !agg.Send(ctx, events, eventtypes.StreamEvent{Type: "changed", Timestamp: time.Now()}) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			if !agg.Send(ctx, events, eventtypes.StreamEvent{Type: "changed", Timestamp: time.Now()}) {
				return
			}
		}
	}
}
