package event

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
)

const daemonEventChanBuffer = 256

// SubscribeDockerEvents prepares subscriptions before the watcher starts. The caller
// runs the returned worker and calls cleanup after joining it, including on failed startup.
func (s *EventService) SubscribeDockerEvents(eventBus *bus.DockerEventBus) (run func(context.Context) error, cleanup func()) {
	containers, unsubscribeContainers := eventBus.Subscribe(events.ContainerEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	images, unsubscribeImages := eventBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	volumes, unsubscribeVolumes := eventBus.Subscribe(events.VolumeEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	networks, unsubscribeNetworks := eventBus.Subscribe(events.NetworkEventType, bus.WithSubscriberBuffer(daemonEventChanBuffer))
	cleanup = sync.OnceFunc(func() {
		unsubscribeContainers()
		unsubscribeImages()
		unsubscribeVolumes()
		unsubscribeNetworks()
	})
	return func(ctx context.Context) error {
		return s.runDockerEventsInternal(ctx, containers, images, volumes, networks)
	}, cleanup
}

func (s *EventService) runDockerEventsInternal(ctx context.Context, containers, images, volumes, networks <-chan events.Message) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for containers != nil || images != nil || volumes != nil || networks != nil {
		var msg events.Message
		var ok bool
		select {
		case <-ctx.Done():
			return nil
		case msg, ok = <-containers:
			if !ok {
				containers = nil
				continue
			}
		case msg, ok = <-images:
			if !ok {
				images = nil
				continue
			}
		case msg, ok = <-volumes:
			if !ok {
				volumes = nil
				continue
			}
		case msg, ok = <-networks:
			if !ok {
				networks = nil
				continue
			}
		case <-ticker.C:
			s.dockerCorrelation.mu.Lock()
			s.dockerCorrelation.pruneInternal(s.dockerCorrelation.timeInternal())
			s.dockerCorrelation.mu.Unlock()
			continue
		}
		s.RecordDockerEvent(ctx, msg)
	}
	return nil
}

// RecordDockerEvent persists a daemon occurrence once across normal and overflow delivery.
func (s *EventService) RecordDockerEvent(ctx context.Context, msg events.Message) {
	if ctx.Err() != nil {
		return
	}
	req, ok := mapDaemonEventInternal(msg)
	if !ok {
		return
	}
	if s.ShouldSuppressDaemonEvent(string(msg.Type), msg.Actor.ID, *req.ResourceName, msg.Actor.Attributes["com.docker.compose.project"]) {
		return
	}
	if msg.TimeNano != 0 {
		identity, err := json.Marshal(msg, json.Deterministic(true))
		if err != nil {
			slog.WarnContext(ctx, "Failed to identify Docker daemon event", "type", req.Type, "resourceId", msg.Actor.ID, "error", err)
			return
		}
		req.deduplicationID = uuid.NewSHA1(uuid.NameSpaceOID, identity).String()
	}
	if _, err := s.CreateEvent(ctx, req); err != nil {
		slog.WarnContext(ctx, "Failed to log Docker daemon event", "type", req.Type, "resourceId", msg.Actor.ID, "error", err)
	}
}

var daemonEventTypesInternal = map[events.Type]map[events.Action]EventType{
	events.ContainerEventType: {
		events.ActionCreate:                EventTypeContainerCreate,
		events.ActionStart:                 EventTypeContainerStart,
		events.ActionStop:                  EventTypeContainerStop,
		events.ActionRestart:               EventTypeContainerRestart,
		events.ActionDie:                   EventTypeContainerDie,
		events.ActionOOM:                   EventTypeContainerOOM,
		events.ActionKill:                  EventTypeContainerKill,
		events.ActionDestroy:               EventTypeContainerDelete,
		events.ActionPause:                 EventTypeContainerPause,
		events.ActionUnPause:               EventTypeContainerUnpause,
		events.ActionRename:                EventTypeContainerRename,
		events.ActionUpdate:                EventTypeContainerUpdate,
		events.ActionHealthStatusUnhealthy: EventTypeContainerUnhealthy,
	},
	events.ImageEventType: {
		events.ActionPull:   EventTypeImagePull,
		events.ActionDelete: EventTypeImageDelete,
		events.ActionTag:    EventTypeImageTag,
		events.ActionUnTag:  EventTypeImageUntag,
		events.ActionImport: EventTypeImageImport,
		events.ActionLoad:   EventTypeImageLoad,
		events.ActionPrune:  EventTypeImagePrune,
	},
	events.VolumeEventType: {
		events.ActionCreate:  EventTypeVolumeCreate,
		events.ActionDestroy: EventTypeVolumeDelete,
		events.ActionPrune:   EventTypeVolumePrune,
	},
	events.NetworkEventType: {
		events.ActionCreate:  EventTypeNetworkCreate,
		events.ActionDestroy: EventTypeNetworkDelete,
		events.ActionPrune:   EventTypeNetworkPrune,
	},
}

func mapDaemonEventInternal(msg events.Message) (CreateEventRequest, bool) {
	eventType := daemonEventTypesInternal[msg.Type][msg.Action]
	if eventType == "" || (msg.Actor.ID == "" && msg.Action != events.ActionPrune) {
		return CreateEventRequest{}, false
	}
	severity := EventSeverityInfo
	if eventType == EventTypeContainerDie && msg.Actor.Attributes["exitCode"] != "0" || eventType == EventTypeContainerUnhealthy {
		severity = EventSeverityWarning
	}
	if eventType == EventTypeContainerOOM {
		severity = EventSeverityError
	}

	name := msg.Actor.Attributes["name"]
	if name == "" {
		name = msg.Actor.ID
	}
	if name == "" {
		name = "prune"
	}
	metadata := database.JSON{"source": "docker", "action": string(msg.Action), "scope": msg.Scope}
	for _, key := range []string{"name", "image", "exitCode", "signal"} {
		if value, ok := msg.Actor.Attributes[key]; ok {
			metadata[key] = value
		}
	}
	for attr, key := range map[string]string{"com.docker.compose.project": "composeProject", "com.docker.compose.service": "composeService"} {
		if value, ok := msg.Actor.Attributes[attr]; ok {
			metadata[key] = value
		}
	}
	return CreateEventRequest{
		Type: eventType, Severity: severity,
		Title:        fmt.Sprintf("Docker %s %s: %s", msg.Type, msg.Action, name),
		Description:  fmt.Sprintf("%s '%s': %s observed from the Docker daemon", msg.Type, name, msg.Action),
		ResourceType: new(string(msg.Type)), ResourceID: new(msg.Actor.ID), ResourceName: new(name),
		EnvironmentID: new("0"), Metadata: metadata,
	}, true
}
