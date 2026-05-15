package eventbus

import (
	"sync"

	"github.com/moby/moby/api/types/events"
)

// DockerEventBus is an in-process fan-out point for Docker daemon events.
//
// Publishers send the raw Docker event message once, and subscribers register by
// Docker event type. Consumers should treat messages as invalidation signals
// rather than authoritative state deltas; after receiving an event they should
// refetch the resource state they own.
type DockerEventBus struct {
	mu   sync.RWMutex
	subs map[events.Type]map[chan<- events.Message]struct{}
}

// NewDockerEventBus creates an empty Docker event bus.
func NewDockerEventBus() *DockerEventBus {
	return &DockerEventBus{
		subs: make(map[events.Type]map[chan<- events.Message]struct{}),
	}
}

// Subscribe registers ch for messages of eventType and returns a cancellation
// function that removes the subscription. Delivery is non-blocking, so callers
// should use a buffered channel sized for their invalidation workload.
func (b *DockerEventBus) Subscribe(eventType events.Type, ch chan<- events.Message) func() {
	if b == nil || ch == nil {
		return func() {}
	}

	b.mu.Lock()
	if b.subs[eventType] == nil {
		b.subs[eventType] = make(map[chan<- events.Message]struct{})
	}
	b.subs[eventType][ch] = struct{}{}
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		if subs := b.subs[eventType]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subs, eventType)
			}
		}
		b.mu.Unlock()
	}
}

// Publish fans out msg to subscribers of msg.Type. Slow or full subscriber
// channels are skipped so Docker event consumption cannot be blocked by one
// listener.
func (b *DockerEventBus) Publish(msg events.Message) {
	if b == nil {
		return
	}

	b.mu.RLock()
	subs := make([]chan<- events.Message, 0, len(b.subs[msg.Type]))
	for ch := range b.subs[msg.Type] {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
		}
	}
}
