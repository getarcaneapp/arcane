// Package aggstream provides the plumbing for aggregated JSON-lines streams:
// endpoints that multiplex per-environment events from the local environment
// and every remote environment over a single HTTP response, so the browser
// needs one connection regardless of environment count.
package aggstream

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// RemoteEnvironmentLister lists the remote environment IDs an aggregated
// stream should cover; it decouples this package from the environment service.
type RemoteEnvironmentLister interface {
	ListRemoteEnvironmentIDs(ctx context.Context) ([]string, error)
}

// Run drives a JSON-lines aggregated stream: it fans in events from the given
// producers over a single buffered channel and multiplexes them onto the
// response together with periodic heartbeats. It returns when the request
// context is canceled or the response writer fails.
func Run[T any](
	ctx context.Context,
	encoder *json.Encoder,
	flush func(),
	buffer int,
	heartbeatInterval time.Duration,
	makeHeartbeat func() T,
	producers ...func(ctx context.Context, events chan<- T),
) {
	streamCtx, cancel := context.WithCancel(ctx)

	events := make(chan T, buffer)
	var wg sync.WaitGroup
	wg.Add(len(producers))
	for _, producer := range producers {
		go func(producer func(context.Context, chan<- T)) {
			defer wg.Done()
			producer(streamCtx, events)
		}(producer)
	}
	defer wg.Wait()
	defer cancel()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case event := <-events:
			if err := encoder.Encode(event); err != nil {
				return
			}
			flush()
		case <-heartbeat.C:
			if err := encoder.Encode(makeHeartbeat()); err != nil {
				return
			}
			flush()
		}
	}
}

// Send forwards an event to the stream's event channel, giving up when the
// stream is shutting down so producers can never block.
func Send[T any](ctx context.Context, events chan<- T, event T) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

// ReconcileEnvironmentPollers keeps one poller goroutine per enabled remote
// environment, re-listing periodically so environments added or removed while
// the stream is open are picked up without a reconnect. It returns when the
// stream context is canceled, after every poller has exited.
func ReconcileEnvironmentPollers(
	ctx context.Context,
	lister RemoteEnvironmentLister,
	reconcileInterval time.Duration,
	streamLabel string,
	runPoller func(ctx context.Context, environmentID string),
) {
	ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]string, error) {
			return lister.ListRemoteEnvironmentIDs(ctx)
		},
		func(environmentID string) string {
			return environmentID
		},
		nil,
		reconcileInterval,
		streamLabel,
		runPoller,
	)
}

// ReconcilePollersByKey keeps one poller goroutine per listed item, keyed by a
// stable item ID. If the same key is later returned with a different version,
// the old poller is replaced so callers can pass refreshed item metadata.
func ReconcilePollersByKey[T any](
	ctx context.Context,
	listItems func(context.Context) ([]T, error),
	keyFunc func(T) string,
	versionFunc func(T) string,
	reconcileInterval time.Duration,
	streamLabel string,
	runPoller func(ctx context.Context, item T),
) {
	type poller struct {
		cancel  context.CancelFunc
		version string
	}

	pollers := make(map[string]poller)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer func() {
		for _, activePoller := range pollers {
			activePoller.cancel()
		}
	}()

	reconcile := func() {
		items, err := listItems(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.WarnContext(ctx, "failed to list environments for "+streamLabel, "error", err)
			}
			return
		}

		current := make(map[string]struct{}, len(items))
		for _, item := range items {
			key := keyFunc(item)
			if key == "" {
				continue
			}
			version := ""
			if versionFunc != nil {
				version = versionFunc(item)
			}
			current[key] = struct{}{}

			if existingPoller, ok := pollers[key]; ok && existingPoller.version == version {
				continue
			}

			if existingPoller, ok := pollers[key]; ok {
				existingPoller.cancel()
				delete(pollers, key)
			}

			pollCtx, cancelPoll := context.WithCancel(ctx)
			pollers[key] = poller{cancel: cancelPoll, version: version}
			wg.Add(1)
			go func(pollCtx context.Context, item T) {
				defer wg.Done()
				runPoller(pollCtx, item)
			}(pollCtx, item)
		}

		for id, activePoller := range pollers {
			if _, ok := current[id]; !ok {
				activePoller.cancel()
				delete(pollers, id)
			}
		}
	}

	reconcile()

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}
