package event

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/streams/bus"
	"gorm.io/gorm"
)

func TestMapDaemonEventInternal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		kind   events.Type
		action events.Action
		want   EventType
	}{
		{"container create", events.ContainerEventType, events.ActionCreate, EventTypeContainerCreate},
		{"container start", events.ContainerEventType, events.ActionStart, EventTypeContainerStart},
		{"container stop", events.ContainerEventType, events.ActionStop, EventTypeContainerStop},
		{"container restart", events.ContainerEventType, events.ActionRestart, EventTypeContainerRestart},
		{"container die", events.ContainerEventType, events.ActionDie, EventTypeContainerDie},
		{"container oom", events.ContainerEventType, events.ActionOOM, EventTypeContainerOOM},
		{"container kill", events.ContainerEventType, events.ActionKill, EventTypeContainerKill},
		{"container destroy", events.ContainerEventType, events.ActionDestroy, EventTypeContainerDelete},
		{"container pause", events.ContainerEventType, events.ActionPause, EventTypeContainerPause},
		{"container unpause", events.ContainerEventType, events.ActionUnPause, EventTypeContainerUnpause},
		{"container rename", events.ContainerEventType, events.ActionRename, EventTypeContainerRename},
		{"container update", events.ContainerEventType, events.ActionUpdate, EventTypeContainerUpdate},
		{"container unhealthy", events.ContainerEventType, events.ActionHealthStatusUnhealthy, EventTypeContainerUnhealthy},
		{"image pull", events.ImageEventType, events.ActionPull, EventTypeImagePull},
		{"image delete", events.ImageEventType, events.ActionDelete, EventTypeImageDelete},
		{"image tag", events.ImageEventType, events.ActionTag, EventTypeImageTag},
		{"image untag", events.ImageEventType, events.ActionUnTag, EventTypeImageUntag},
		{"image load", events.ImageEventType, events.ActionLoad, EventTypeImageLoad},
		{"image import", events.ImageEventType, events.ActionImport, EventTypeImageImport},
		{"image prune", events.ImageEventType, events.ActionPrune, EventTypeImagePrune},
		{"volume create", events.VolumeEventType, events.ActionCreate, EventTypeVolumeCreate},
		{"volume destroy", events.VolumeEventType, events.ActionDestroy, EventTypeVolumeDelete},
		{"volume prune", events.VolumeEventType, events.ActionPrune, EventTypeVolumePrune},
		{"network create", events.NetworkEventType, events.ActionCreate, EventTypeNetworkCreate},
		{"network destroy", events.NetworkEventType, events.ActionDestroy, EventTypeNetworkDelete},
		{"network prune", events.NetworkEventType, events.ActionPrune, EventTypeNetworkPrune},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, ok := mapDaemonEventInternal(events.Message{Type: tc.kind, Action: tc.action, Scope: "local", Actor: events.Actor{ID: "resource-id", Attributes: map[string]string{"name": "web", "image": "alpine", "exitCode": "0", "signal": "15", "com.docker.compose.project": "demo", "com.docker.compose.service": "web", "secret": "never-copy"}}})
			require.True(t, ok)
			require.Equal(t, tc.want, req.Type)
			require.Equal(t, "web", *req.ResourceName)
			require.Equal(t, "0", *req.EnvironmentID)
			require.Nil(t, req.UserID)
			require.Nil(t, req.Username)
			require.Equal(t, database.JSON{"source": "docker", "action": string(tc.action), "scope": "local", "name": "web", "image": "alpine", "exitCode": "0", "signal": "15", "composeProject": "demo", "composeService": "web"}, req.Metadata)
			severity := EventSeverityInfo
			if tc.action == events.ActionOOM {
				severity = EventSeverityError
			}
			if tc.action == events.ActionHealthStatusUnhealthy {
				severity = EventSeverityWarning
			}
			require.Equal(t, severity, req.Severity)
		})
	}
	for _, exitCode := range []string{"1", "137", ""} {
		t.Run("die exit "+exitCode, func(t *testing.T) {
			req, ok := mapDaemonEventInternal(events.Message{Type: events.ContainerEventType, Action: events.ActionDie, Actor: events.Actor{ID: "id", Attributes: map[string]string{"exitCode": exitCode}}})
			require.True(t, ok)
			require.Equal(t, EventSeverityWarning, req.Severity)
			require.Equal(t, "id", *req.ResourceName)
		})
	}
	for _, kind := range []events.Type{events.ImageEventType, events.VolumeEventType, events.NetworkEventType} {
		t.Run(string(kind)+" empty prune", func(t *testing.T) {
			req, ok := mapDaemonEventInternal(events.Message{Type: kind, Action: events.ActionPrune})
			require.True(t, ok)
			require.Equal(t, "prune", *req.ResourceName)
		})
	}
	for _, tc := range []struct {
		name   string
		kind   events.Type
		action events.Action
		id     string
	}{
		{"exec", events.ContainerEventType, "exec_create", "id"}, {"attach", events.ContainerEventType, "attach", "id"}, {"resize", events.ContainerEventType, "resize", "id"}, {"healthy", events.ContainerEventType, "health_status: healthy", "id"}, {"mount", events.VolumeEventType, "mount", "id"}, {"connect", events.NetworkEventType, "connect", "id"}, {"disconnect", events.NetworkEventType, "disconnect", "id"}, {"unknown action", events.ImageEventType, "unknown-image-action", "image-id"}, {"empty create", events.ContainerEventType, events.ActionCreate, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := mapDaemonEventInternal(events.Message{Type: tc.kind, Action: tc.action, Actor: events.Actor{ID: tc.id}})
			require.False(t, ok)
		})
	}
}

func TestDaemonEventsPersistEveryOccurrenceImmediately(t *testing.T) {
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)
	die := events.Message{Type: events.ContainerEventType, Action: events.ActionDie, Actor: events.Actor{ID: "web", Attributes: map[string]string{"exitCode": "0"}}}
	stop := die
	stop.Action = events.ActionStop
	for index, msg := range []events.Message{die, stop, die, stop} {
		msg.TimeNano = int64(index + 1)
		svc.RecordDockerEvent(t.Context(), msg)
		requireDaemonCountInternal(t, db, int64(index+1))
	}
	var rows []Event
	require.NoError(t, db.Find(&rows).Error)
	for _, row := range rows {
		require.Equal(t, "System", *row.Username)
		require.Equal(t, "0", *row.EnvironmentID)
		require.Equal(t, "docker", row.Metadata["source"])
	}
}

func TestDaemonSuppressionAndFailureAllowLaterObservations(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "suppression"
		if failure {
			name = "persistence failure"
		}
		t.Run(name, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			now := time.Now()
			svc.dockerCorrelation.now = func() time.Time { return now }
			msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, TimeNano: 123, Actor: events.Actor{ID: "web"}}
			if failure {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("daemon_failure", func(tx *gorm.DB) { tx.AddError(errors.New("unavailable")) }))
			}
			if !failure {
				svc.MarkDockerExpectation("container", "web", "")
			}
			svc.RecordDockerEvent(t.Context(), msg)
			requireDaemonCountInternal(t, db, 0)
			if failure {
				require.NoError(t, db.Callback().Create().Remove("daemon_failure"))
			}
			now = now.Add(time.Minute)
			svc.RecordDockerEvent(t.Context(), msg)
			requireDaemonCountInternal(t, db, 1)
		})
	}
}

func TestDockerEventSubscriptionsReadyAndStop(t *testing.T) {
	for _, closeBus := range []bool{false, true} {
		name := "cancellation"
		if closeBus {
			name = "closed bus"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := setupEventServiceTestDB(t)
				sqlDB, err := db.DB.DB()
				require.NoError(t, err)
				defer sqlDB.Close()
				sqlDB.SetMaxOpenConns(1)
				svc := NewEventService(db, nil, nil)
				var recovered, published atomic.Int64
				stopChanges := svc.changes.Subscribe(func(struct{}) { published.Add(1) })
				defer stopChanges()
				eventBus := bus.NewDockerEventBus(bus.WithDroppedEventCallback(func(message events.Message) {
					recovered.Add(1)
					svc.RecordDockerEvent(t.Context(), message)
				}))
				defer eventBus.Close()
				run, cleanup := svc.SubscribeDockerEvents(eventBus)
				defer cleanup()
				// A stalled unrelated subscriber also invokes the global overflow callback.
				_, stopStalled := eventBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(1))
				defer stopStalled()
				const occurrences = daemonEventChanBuffer + 64
				publishBurst := func(offset int64) {
					for occurrence := range occurrences {
						for _, kind := range []events.Type{events.ContainerEventType, events.ImageEventType, events.VolumeEventType, events.NetworkEventType} {
							action := events.ActionCreate
							if kind == events.ImageEventType {
								action = events.ActionPull
							}
							eventBus.Publish(events.Message{Type: kind, Action: action, TimeNano: offset + int64(occurrence+1), Actor: events.Actor{ID: "resource"}})
						}
					}
				}
				publishBurst(0)
				require.Positive(t, recovered.Load())

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- run(ctx) }()
				synctest.Wait()
				requireDaemonCountInternal(t, db, 4*occurrences)
				require.Equal(t, int64(4*occurrences), published.Load())
				publishBurst(occurrences)
				synctest.Wait()
				requireDaemonCountInternal(t, db, 8*occurrences)
				require.Equal(t, int64(8*occurrences), published.Load())
				if closeBus {
					eventBus.Close()
				} else {
					cancel()
				}
				synctest.Wait()
				require.NoError(t, <-done)
				cleanup()
				cleanup()
			})
		})
	}
}

func requireDaemonCountInternal(t *testing.T, db *database.DB, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&Event{}).Count(&count).Error)
	require.Equal(t, want, count)
}
