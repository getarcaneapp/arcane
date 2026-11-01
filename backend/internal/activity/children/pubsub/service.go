// Package pubsub fans activity stream events out to per-environment subscribers.
package pubsub

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/mo"

	"kit.getarcane.app/concurrency"

	"getarcane.dev/types/v3"
)

type (
	// Service fans activity stream events out to per-environment subscribers.
	Service struct {
		changes           *concurrency.Signal[string]
		subscribers       map[int]*subscriber
		terminalPublished map[string]time.Time
		nextSubID         int

		subscribersMu sync.RWMutex

		// terminalPublished records when each activity's terminal snapshot was
		// published, so later non-terminal snapshots from slower goroutines can
		// be dropped (see admit). Entries are pruned on terminal publishes once
		// older than terminalPublishRetention.
		terminalPublishedMu sync.Mutex
	}

	// subscriber buffers stream events between publishers and one stream
	// consumer. "activity" events are coalesced in place per activity ID (only the
	// latest pending state matters to the UI), so bulk operations emitting rapid
	// progress updates cannot overflow the subscriber and force full-snapshot
	// resends. Other events keep arrival order in a FIFO bounded by
	// subscriberMessageQueueLimit with drop-oldest on overflow.
	subscriber struct {
		ch   chan types.ActivityStreamEvent
		done chan struct{}
		wake chan struct{}

		pendingActivity map[string]*pendingStreamEvent
		environmentID   string
		queue           []*pendingStreamEvent
		messageCount    int

		mu     sync.Mutex
		missed bool
	}

	pendingStreamEvent struct {
		event types.ActivityStreamEvent
	}
)

const (
	// terminalPublishRetention bounds how long a terminal publish suppresses
	// stale non-terminal snapshots for the same activity. Stale publishers are
	// goroutines already past their commit, so they publish within moments; the
	// retention only has to outlive that gap while keeping the latch map small.
	terminalPublishRetention = 10 * time.Minute
	// subscriberMessageQueueLimit bounds the per-subscriber backlog of "message"
	// events; the oldest message is dropped (and flagged as missed) on overflow.
	subscriberMessageQueueLimit = 256
)

// NewService returns an empty subscriber registry.
func NewService(changes *concurrency.Signal[string]) *Service {
	return &Service{
		changes:           changes,
		subscribers:       map[int]*subscriber{},
		terminalPublished: map[string]time.Time{},
	}
}

func (sub *subscriber) enqueue(event types.ActivityStreamEvent) {
	sub.mu.Lock()
	if event.Type == "activity" && event.ActivityID != "" {
		if pending, ok := sub.pendingActivity[event.ActivityID]; ok {
			pending.event = event
			sub.mu.Unlock()
			return
		}
	} else {
		if sub.messageCount >= subscriberMessageQueueLimit {
			sub.dropOldestMessageLocked()
		}
		sub.messageCount++
	}
	entry := &pendingStreamEvent{event: event}
	sub.queue = append(sub.queue, entry)
	if event.Type == "activity" && event.ActivityID != "" {
		sub.pendingActivity[event.ActivityID] = entry
	}
	sub.mu.Unlock()

	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

func (sub *subscriber) dropOldestMessageLocked() {
	for i, entry := range sub.queue {
		if entry.event.Type == "activity" && entry.event.ActivityID != "" {
			continue
		}
		sub.queue = append(sub.queue[:i], sub.queue[i+1:]...)
		sub.messageCount--
		sub.missed = true
		ctx := context.Background() //nolint:forbidigo // Subscriber buffer trimming runs outside any request context.
		slog.WarnContext(ctx, "activity subscriber message buffer full; snapshot will be sent on next heartbeat",
			"environmentId", sub.environmentID)
		return
	}
}

func (sub *subscriber) next() mo.Option[types.ActivityStreamEvent] {
	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.queue) == 0 {
		return mo.None[types.ActivityStreamEvent]()
	}
	entry := sub.queue[0]
	sub.queue = sub.queue[1:]
	event := entry.event
	if event.Type == "activity" && event.ActivityID != "" {
		if sub.pendingActivity[event.ActivityID] == entry {
			delete(sub.pendingActivity, event.ActivityID)
		}
	} else {
		sub.messageCount--
	}
	return mo.Some(event)
}

func (sub *subscriber) pump() {
	defer close(sub.ch)
	for {
		event, ok := sub.next().Get()
		if !ok {
			select {
			case <-sub.wake:
				continue
			case <-sub.done:
				return
			}
		}
		select {
		case sub.ch <- event:
		case <-sub.done:
			return
		}
	}
}

// Subscribe returns the event channel for environmentID, a drop-check, and an unsubscribe func.
func (s *Service) Subscribe(environmentID string) (<-chan types.ActivityStreamEvent, func() bool, func()) {
	ch := make(chan types.ActivityStreamEvent, 64)
	sub := &subscriber{
		environmentID:   cmp.Or(strings.TrimSpace(environmentID), "0"),
		ch:              ch,
		done:            make(chan struct{}),
		wake:            make(chan struct{}, 1),
		pendingActivity: map[string]*pendingStreamEvent{},
	}
	s.subscribersMu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = sub
	s.subscribersMu.Unlock()
	go sub.pump()

	missedEvents := func() bool {
		s.subscribersMu.RLock()
		localSub, ok := s.subscribers[id]
		s.subscribersMu.RUnlock()
		if !ok {
			return false
		}

		localSub.mu.Lock()
		defer localSub.mu.Unlock()
		if !localSub.missed {
			return false
		}
		localSub.missed = false
		return true
	}

	unsubscribe := func() {
		s.subscribersMu.Lock()
		localSub, ok := s.subscribers[id]
		if ok {
			delete(s.subscribers, id)
		}
		s.subscribersMu.Unlock()
		if ok {
			// The pump goroutine owns ch and closes it on shutdown.
			close(localSub.done)
		}
	}

	return ch, missedEvents, unsubscribe
}

// PublishActivity streams an activity snapshot unless a terminal snapshot for
// the same activity was already published.
func (s *Service) PublishActivity(item types.Activity) {
	if !s.admit(item) {
		return
	}
	s.publish(item.EnvironmentID, types.ActivityStreamEvent{
		Type:       "activity",
		ActivityID: item.ID,
		Activity:   &item,
		Timestamp:  time.Now(),
	})
	s.changes.Publish(item.EnvironmentID)
}

// PublishMessage delivers one activity message to every subscriber of environmentID.
func (s *Service) PublishMessage(environmentID string, activityType types.ActivityType, message types.ActivityMessage) {
	s.publish(environmentID, types.ActivityStreamEvent{
		Type:         "message",
		ActivityID:   message.ActivityID,
		ActivityType: activityType,
		Message:      &message,
		Timestamp:    time.Now(),
	})
}

// Reopen clears the terminal latch for an activity whose lifecycle can
// legitimately restart, such as a retried job summary.
func (s *Service) Reopen(activityID string) {
	s.terminalPublishedMu.Lock()
	delete(s.terminalPublished, activityID)
	s.terminalPublishedMu.Unlock()
}

// admit orders activity events across the commit → publish gap. Writes
// publish their snapshot after committing, so a goroutine that committed
// before a terminal write can publish after it; since a terminal activity
// emits no further events to correct the stream (and subscriber coalescing
// would even replace an undelivered terminal event in place), such a stale
// non-terminal snapshot must be dropped, not delivered. Activities never leave
// a terminal status, so once a terminal snapshot is published, any later
// non-terminal snapshot for that ID is stale by construction.
func (s *Service) admit(item types.Activity) bool {
	s.terminalPublishedMu.Lock()
	defer s.terminalPublishedMu.Unlock()
	if !slices.Contains([]types.ActivityStatus{types.ActivityStatusSuccess, types.ActivityStatusFailed, types.ActivityStatusCancelled}, item.Status) {
		_, sealed := s.terminalPublished[item.ID]
		return !sealed
	}
	now := time.Now()
	for id, publishedAt := range s.terminalPublished {
		if now.Sub(publishedAt) > terminalPublishRetention {
			delete(s.terminalPublished, id)
		}
	}
	s.terminalPublished[item.ID] = now
	return true
}

func (s *Service) publish(environmentID string, event types.ActivityStreamEvent) {
	s.subscribersMu.RLock()
	subs := make([]*subscriber, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		if sub.environmentID == environmentID {
			subs = append(subs, sub)
		}
	}
	s.subscribersMu.RUnlock()

	for _, sub := range subs {
		// Source labeling mutates activity fields independently for each subscriber.
		subscriberEvent := event
		if event.Activity != nil {
			subscriberEvent.Activity = new(*event.Activity)
		}
		subscriberEvent.Activities = slices.Clone(event.Activities)
		sub.enqueue(subscriberEvent)
	}
}
