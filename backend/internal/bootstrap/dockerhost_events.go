package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/moby/moby/client"
	"go.uber.org/fx"

	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/pkg/envcontext"
)

type (
	// dockerHostEventWatcher keeps one event stream per docker-host environment. Those daemons are
	// reached in-process rather than through an agent, so the manager's own event stream never sees
	// them; without this their events and activity would be missing from the log.
	dockerHostEventWatcher struct {
		docker       *docker.DockerClientService
		environments *environment.EnvironmentService
		events       *event.EventService
		streams      map[string]*dockerHostStream
		cancel       context.CancelFunc
		mu           sync.Mutex
	}

	dockerHostStream struct {
		cancel context.CancelFunc
		host   string
	}

	dockerHostEventWatcherParams struct {
		fx.In

		AppCtx       context.Context
		Docker       *docker.DockerClientService
		Environments *environment.EnvironmentService
		Events       *event.EventService
	}
)

// dockerHostEventReconcileInterval bounds how long a newly created docker-host environment waits
// for its daemon's events to show up in the log.
const dockerHostEventReconcileInterval = 30 * time.Second

func startDockerHostEventWatcher(lc fx.Lifecycle, p dockerHostEventWatcherParams) {
	ctx, cancel := context.WithCancel(p.AppCtx)
	watcher := &dockerHostEventWatcher{
		docker:       p.Docker,
		environments: p.Environments,
		events:       p.Events,
		cancel:       cancel,
		streams:      make(map[string]*dockerHostStream),
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go watcher.run(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			watcher.stop()
			return nil
		},
	})
}

// run reconciles the per-environment streams until the process stops.
func (w *dockerHostEventWatcher) run(ctx context.Context) {
	defer concurrency.RecoverToError(nil, "docker host event watcher")
	ticker := time.NewTicker(dockerHostEventReconcileInterval)
	defer ticker.Stop()
	for {
		w.reconcile(ctx)
		select {
		case <-ctx.Done():
			w.stop()
			return
		case <-ticker.C:
		}
	}
}

// reconcile starts a stream for every enabled docker-host environment and stops the ones whose
// environment is gone, disabled or now points at a different daemon.
func (w *dockerHostEventWatcher) reconcile(ctx context.Context) {
	hosts, err := w.environments.ListDockerHostEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to list docker host environments for the event log", "error", err)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for environmentID, stream := range w.streams {
		if host, ok := hosts[environmentID]; ok && host == stream.host {
			continue
		}
		stream.cancel()
		delete(w.streams, environmentID)
	}
	for environmentID, host := range hosts {
		if _, ok := w.streams[environmentID]; ok {
			continue
		}
		streamCtx, cancel := context.WithCancel(ctx)
		w.streams[environmentID] = &dockerHostStream{cancel: cancel, host: host}
		go w.watch(streamCtx, environmentID, host)
	}
}

func (w *dockerHostEventWatcher) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for environmentID, stream := range w.streams {
		stream.cancel()
		delete(w.streams, environmentID)
	}
	w.cancel()
}

// watch records one environment's daemon events, reconnecting while the environment is served.
// The daemon client is resolved through the environment target so the stream follows the
// environment rather than the manager's own daemon.
func (w *dockerHostEventWatcher) watch(ctx context.Context, environmentID, host string) {
	defer concurrency.RecoverToError(nil, "docker host event stream", "environmentId", environmentID)
	// Reconnect backoff: a daemon that drops connections immediately must not spin.
	backoff := time.Second
	for ctx.Err() == nil {
		scopedCtx := envcontext.WithTarget(ctx, envcontext.Target{EnvironmentID: environmentID, DockerHost: host})
		startedAt := time.Now()
		err := w.streamOnce(scopedCtx, environmentID)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			slog.WarnContext(ctx, "Docker host event stream stopped", "environmentId", environmentID, "error", err)
		}
		if time.Since(startedAt) > dockerHostEventReconcileInterval {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < dockerHostEventReconcileInterval {
			backoff *= 2
		}
	}
}

func (w *dockerHostEventWatcher) streamOnce(ctx context.Context, environmentID string) error {
	dockerClient, err := w.docker.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	result := dockerClient.Events(ctx, client.EventsListOptions{})
	var streamErr error
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-result.Messages:
			if !ok {
				return streamErr
			}
			if msg.TimeNano == 0 {
				msg.TimeNano = time.Now().UnixNano()
			}
			w.events.RecordDockerHostEvent(ctx, environmentID, msg)
		case recvErr, ok := <-result.Err:
			if ok {
				streamErr = recvErr
			}
			return streamErr
		}
	}
}
