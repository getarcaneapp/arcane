package status

import (
	"context"
	"errors"
	"log/slog"
	"time"

	mobyEvents "github.com/moby/moby/api/types/events"

	"kit.getarcane.app/docker"
	"kit.getarcane.app/docker/streams/bus"

	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
)

type (
	Service struct {
		dockerService *dockerInternal.DockerClientService
		reconcile     func(ctx context.Context) error
	}
)

const (
	reconcileDelay      = time.Second
	reconcileRetryDelay = 15 * time.Second
	eventBufferSize     = 256
)

func New(dockerService *dockerInternal.DockerClientService, reconcile func(ctx context.Context) error) *Service {
	return &Service{
		dockerService: dockerService,
		reconcile:     reconcile,
	}
}

func (s *Service) Run(ctx context.Context) error {
	messages, unsubscribe := s.dockerService.EventBus().Subscribe(mobyEvents.ContainerEventType, bus.SubscribeOptions{Buffer: new(eventBufferSize)})
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
			case mobyEvents.ActionCreate, mobyEvents.ActionStart, mobyEvents.ActionDie, mobyEvents.ActionStop, mobyEvents.ActionDestroy, mobyEvents.ActionPause, mobyEvents.ActionUnPause:
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
