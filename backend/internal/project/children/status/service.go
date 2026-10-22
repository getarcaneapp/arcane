package status

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/docker"
	"go.getarcane.app/streams/bus"

	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
)

const (
	reconcileDelay      = time.Second
	reconcileRetryDelay = 15 * time.Second
	eventBufferSize     = 256
)

type Service struct {
	dockerService *dockerInternal.DockerClientService
	reconcile     func(ctx context.Context) error
}

func New(dockerService *dockerInternal.DockerClientService, reconcile func(ctx context.Context) error) *Service {
	return &Service{
		dockerService: dockerService,
		reconcile:     reconcile,
	}
}

func (s *Service) Run(ctx context.Context) error {
	messages, unsubscribe := s.dockerService.EventBus().Subscribe(events.ContainerEventType, bus.WithSubscriberBuffer(eventBufferSize))
	defer unsubscribe()
	connected := make(chan struct{}, 1)
	defer s.dockerService.EventStreamConnected().Subscribe(func(struct{}) {
		select {
		case connected <- struct{}{}:
		default:
		}
	})()

	timer := time.NewTimer(reconcileDelay)
	defer timer.Stop()
	armed := true
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-messages:
			if !ok {
				return errors.New("docker container event subscription closed")
			}
			if armed || docker.ComposeProjectLabel(msg.Actor.Attributes) == "" {
				continue
			}
			switch msg.Action {
			case events.ActionCreate, events.ActionStart, events.ActionDie, events.ActionStop, events.ActionDestroy, events.ActionPause, events.ActionUnPause:
				timer.Reset(reconcileDelay)
				armed = true
			}
		case <-connected:
			if !armed {
				timer.Reset(reconcileDelay)
				armed = true
			}
		case <-timer.C:
			armed = false
			if err := s.reconcile(ctx); err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "Failed to reconcile project statuses", "error", err)
				timer.Reset(reconcileRetryDelay)
				armed = true
			}
		}
	}
}
