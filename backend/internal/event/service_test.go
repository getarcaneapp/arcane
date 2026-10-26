package event

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/streams/bus"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event/children/correlation"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

func setupEventServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Event{}))
	return &database.DB{DB: db}
}

func TestCreateEventRequestJSONOmitempty(t *testing.T) {
	t.Run("includes optional fields when set", func(t *testing.T) {
		payload, err := json.Marshal(CreateEventRequest{
			Type:         EventTypeContainerStart,
			Title:        "Container started",
			Description:  "Container 'web' has been started",
			ResourceType: new("container"),
			ResourceID:   new("container-1"),
			ResourceName: new("web"),
			UserID:       new("user-1"),
			Username:     new("arcane"),
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(payload, &decoded))

		require.Equal(t, "container", decoded["resourceType"])
		require.Equal(t, "container-1", decoded["resourceId"])
		require.Equal(t, "web", decoded["resourceName"])
		require.Equal(t, "user-1", decoded["userId"])
		require.Equal(t, "arcane", decoded["username"])
	})

	t.Run("omits optional fields when nil", func(t *testing.T) {
		payload, err := json.Marshal(CreateEventRequest{
			Type:  EventTypeUserLogin,
			Title: "User logged in",
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(payload, &decoded))

		_, hasResourceType := decoded["resourceType"]
		_, hasResourceID := decoded["resourceId"]
		_, hasResourceName := decoded["resourceName"]
		_, hasUserID := decoded["userId"]
		_, hasUsername := decoded["username"]

		require.False(t, hasResourceType)
		require.False(t, hasResourceID)
		require.False(t, hasResourceName)
		require.False(t, hasUserID)
		require.False(t, hasUsername)
	})
}

func TestEventService_LogEventsPersistOptionalPointers(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)

	metadata := database.JSON{"source": "test"}
	err := svc.LogContainerEvent(ctx, EventTypeContainerStart, "container-1", "web", "user-1", "arcane", "0", metadata)
	require.NoError(t, err)

	var containerEvent Event
	err = db.WithContext(ctx).Where("type = ?", EventTypeContainerStart).First(&containerEvent).Error
	require.NoError(t, err)

	require.NotNil(t, containerEvent.ResourceType)
	require.Equal(t, "container", *containerEvent.ResourceType)
	require.NotNil(t, containerEvent.ResourceID)
	require.Equal(t, "container-1", *containerEvent.ResourceID)
	require.NotNil(t, containerEvent.ResourceName)
	require.Equal(t, "web", *containerEvent.ResourceName)
	require.NotNil(t, containerEvent.UserID)
	require.Equal(t, "user-1", *containerEvent.UserID)
	require.NotNil(t, containerEvent.Username)
	require.Equal(t, "arcane", *containerEvent.Username)
	require.NotNil(t, containerEvent.EnvironmentID)
	require.Equal(t, "0", *containerEvent.EnvironmentID)
	require.Equal(t, "test", containerEvent.Metadata["source"])

	err = svc.LogUserEvent(ctx, EventTypeUserLogin, "user-2", "arcane-user", nil)
	require.NoError(t, err)

	var userEvent Event
	err = db.WithContext(ctx).Where("type = ?", EventTypeUserLogin).First(&userEvent).Error
	require.NoError(t, err)

	require.Nil(t, userEvent.ResourceType)
	require.Nil(t, userEvent.ResourceID)
	require.Nil(t, userEvent.ResourceName)
	require.NotNil(t, userEvent.UserID)
	require.Equal(t, "user-2", *userEvent.UserID)
	require.NotNil(t, userEvent.Username)
	require.Equal(t, "arcane-user", *userEvent.Username)
}

func TestCloneEventMetadata(t *testing.T) {
	src := database.JSON{"a": "b"}
	cloned := cloneEventMetadata(src)

	require.Equal(t, "b", cloned["a"])
	cloned["a"] = "changed"
	require.Equal(t, "b", src["a"], "clone should not mutate source metadata")

	nilClone := cloneEventMetadata(nil)
	require.NotNil(t, nilClone)
	require.Empty(t, nilClone)

	nested := database.JSON{
		"outer": map[string]any{
			"slice": []any{
				map[string]any{"k": "v"},
			},
		},
	}
	nestedClone := cloneEventMetadata(nested)
	require.NotNil(t, nestedClone["outer"])

	outer := nested["outer"].(map[string]any)
	outerClone := nestedClone["outer"].(database.JSON)
	sliceOriginal := outer["slice"].([]any)
	sliceClone := outerClone["slice"].([]any)

	sliceClone[0].(database.JSON)["k"] = "changed"
	require.Equal(t, "v", sliceOriginal[0].(map[string]any)["k"], "nested map inside slice should be deep-cloned")
}

func TestEventService_LogErrorEvent_DoesNotMutateInputMetadata(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)

	metadata := database.JSON{"phase": "pull"}
	svc.LogErrorEvent(
		ctx,
		EventTypeImageScan,
		"image",
		"img-1",
		"nginx:latest",
		"user-1",
		"arcane",
		"0",
		errors.New("pull failed"),
		metadata,
	)

	_, mutated := metadata["error"]
	require.False(t, mutated, "input metadata should not be mutated by LogErrorEvent")

	var saved Event
	err := db.WithContext(ctx).Where("type = ?", EventTypeImageScan).First(&saved).Error
	require.NoError(t, err)
	require.Equal(t, EventSeverityError, saved.Severity)
	require.Equal(t, "pull", saved.Metadata["phase"])
	require.Equal(t, "pull failed", saved.Metadata["error"])
}

func TestEventService_CreateEvent_ForwardsToManagerAPIOnAgent(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)

	requests := make(chan CreateEventRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/events", r.URL.Path) {
			return
		}
		if !assert.Equal(t, "test-agent-token", r.Header.Get(middleware.HeaderAgentToken)) {
			return
		}

		defer func() { _ = r.Body.Close() }()
		var payload CreateEventRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload)) {
			return
		}

		select {
		case requests <- payload:
		default:
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		AgentEnabled:  true,
		AgentToken:    "test-agent-token",
		ManagerApiUrl: server.URL,
	}
	svc := NewEventService(db, cfg, server.Client())

	_, err := svc.CreateEvent(ctx, CreateEventRequest{
		Type:          EventTypeContainerStart,
		Severity:      EventSeverityInfo,
		Title:         "Container started: web",
		Description:   "Container 'web' has been started",
		EnvironmentID: new("0"),
		Metadata:      database.JSON{"source": "test"},
	})
	require.NoError(t, err)

	select {
	case payload := <-requests:
		require.Equal(t, EventTypeContainerStart, payload.Type)
		require.Equal(t, EventSeverityInfo, payload.Severity)
		require.Equal(t, "Container started: web", payload.Title)
		require.NotNil(t, payload.EnvironmentID)
		require.Equal(t, "0", *payload.EnvironmentID)
		require.Equal(t, "test", payload.Metadata["source"])
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for manager event sync request")
	}
}

func TestEventService_CreateEvent_NormalizesActor(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)

	t.Run("falls back to system when actor is missing", func(t *testing.T) {
		evt, err := svc.CreateEvent(ctx, CreateEventRequest{
			Type:     EventTypeSystemAutoUpdate,
			Severity: EventSeverityInfo,
			Title:    "System task",
		})
		require.NoError(t, err)
		require.NotNil(t, evt.UserID)
		require.NotNil(t, evt.Username)
		require.Equal(t, "system", *evt.UserID)
		require.Equal(t, "System", *evt.Username)
	})

	t.Run("copies user id to username when username is missing", func(t *testing.T) {
		evt, err := svc.CreateEvent(ctx, CreateEventRequest{
			Type:     EventTypeProjectDeploy,
			Severity: EventSeverityInfo,
			Title:    "Deploy",
			UserID:   new("u-123"),
		})
		require.NoError(t, err)
		require.NotNil(t, evt.UserID)
		require.NotNil(t, evt.Username)
		require.Equal(t, "u-123", *evt.UserID)
		require.Equal(t, "u-123", *evt.Username)
	})

	t.Run("copies username to user id when user id is missing", func(t *testing.T) {
		evt, err := svc.CreateEvent(ctx, CreateEventRequest{
			Type:     EventTypeProjectDeploy,
			Severity: EventSeverityInfo,
			Title:    "Deploy",
			Username: new("kmendell"),
		})
		require.NoError(t, err)
		require.NotNil(t, evt.UserID)
		require.NotNil(t, evt.Username)
		require.Equal(t, "kmendell", *evt.UserID)
		require.Equal(t, "kmendell", *evt.Username)
	})
}

func TestEventService_GetEventSeverityCounts(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)

	seed := []EventSeverity{
		EventSeverityInfo,
		EventSeverityInfo,
		EventSeveritySuccess,
		EventSeverityError,
	}
	for i, severity := range seed {
		_, err := svc.CreateEvent(ctx, CreateEventRequest{
			Type:     EventTypeContainerStart,
			Severity: severity,
			Title:    "event",
		})
		require.NoError(t, err, "seed event %d", i)
	}

	counts, err := svc.GetEventSeverityCounts(ctx)
	require.NoError(t, err)
	require.Equal(t, types.EventSeverityCounts{Total: 4, Info: 2, Success: 1, Warning: 0, Error: 1}, counts)
}

func TestEventService_ListEventsPaginated_TypeCategoryFilter(t *testing.T) {
	ctx := t.Context()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)

	for _, eventType := range []EventType{
		EventTypeContainerStart,
		EventTypeContainerStop,
		EventTypeImagePull,
	} {
		_, err := svc.CreateEvent(ctx, CreateEventRequest{Type: eventType, Title: "event"})
		require.NoError(t, err)
	}

	listWithTypeFilter := func(value string) []string {
		events, _, err := svc.ListEventsPaginated(ctx, pagination.QueryParams{
			Limit:   10,
			Filters: map[string]string{"type": value},
		})
		require.NoError(t, err)
		types := make([]string, 0, len(events))
		for _, e := range events {
			types = append(types, e.Type)
		}
		return types
	}

	require.ElementsMatch(t, []string{"container.start", "container.stop"}, listWithTypeFilter("container"))
	require.ElementsMatch(t, []string{"image.pull"}, listWithTypeFilter("image.pull"))
	require.ElementsMatch(t, []string{"container.start", "container.stop", "image.pull"}, listWithTypeFilter("container,image.pull"))
}

func TestIngestAgentEventAuthenticatedEnvironment(t *testing.T) {
	for _, supplied := range []string{"0", "unrelated-environment", ""} {
		t.Run("payload environment "+supplied, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			req, ok := mapDaemonEvent(events.Message{
				Type:   events.ContainerEventType,
				Action: events.ActionStart,
				Actor: events.Actor{
					ID: "container-id",
					Attributes: map[string]string{
						"name":                       "web",
						"com.docker.compose.project": "demo",
					},
				},
			})
			require.True(t, ok)
			req.EnvironmentID = &supplied
			recorded, err := svc.IngestAgentEvent(t.Context(), " remote-environment ", req)
			require.NoError(t, err)
			require.Equal(t, "remote-environment", *recorded.EnvironmentID)
			require.Equal(t, "System", *recorded.Username)
			require.Equal(t, req.Metadata, recorded.Metadata)
			require.False(t, svc.ShouldSuppressDaemonEvent("container", "container-id", "web", "demo"))
			var persisted Event
			require.NoError(t, db.First(&persisted).Error)
			require.Equal(t, "remote-environment", *persisted.EnvironmentID)
			require.Equal(t, database.JSON{"source": "docker", "action": "start", "scope": "", "name": "web", "composeProject": "demo"}, persisted.Metadata)

			// Attributed Arcane records must not seed expectations for the manager's daemon either.
			req.Metadata = nil
			req.UserID = new("operator-id")
			req.Username = new("operator")
			recorded, err = svc.IngestAgentEvent(t.Context(), "remote-environment", req)
			require.NoError(t, err)
			require.Equal(t, "operator", *recorded.Username)
			require.False(t, svc.ShouldSuppressDaemonEvent("container", "container-id", "web", "demo"))
		})
	}
}

func TestIngestAgentEventRejectsLocalEnvironment(t *testing.T) {
	for _, environmentID := range []string{"", " ", "0", " 0 "} {
		t.Run("environment "+environmentID, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			recorded, err := svc.IngestAgentEvent(t.Context(), environmentID, CreateEventRequest{Type: EventTypeContainerStart, Title: "start", EnvironmentID: new("remote-environment")})
			require.Error(t, err)
			require.Nil(t, recorded)
			requireDaemonCount(t, db, 0)
		})
	}
}

func TestDaemonEventForwardingPreservesMetadata(t *testing.T) {
	requests := make(chan CreateEventRequest, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CreateEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode forwarded event: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- req
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, &config.Config{AgentEnabled: true, AgentToken: "agent-token", ManagerApiUrl: server.URL}, server.Client())
	message := events.Message{
		Type:     events.ContainerEventType,
		Action:   events.ActionDie,
		TimeNano: 123,
		Actor: events.Actor{
			ID: "container-id",
			Attributes: map[string]string{
				"name":     "web",
				"exitCode": "137",
				"signal":   "9",
			},
		},
	}
	req, ok := mapDaemonEvent(message)
	require.True(t, ok)
	svc.RecordDockerEvent(t.Context(), message)
	svc.RecordDockerEvent(t.Context(), message)
	require.NoError(t, svc.Stop(t.Context()))
	select {
	case payload := <-requests:
		require.Equal(t, req.Metadata, payload.Metadata)
		require.Equal(t, EventSeverityWarning, payload.Severity)
		require.Equal(t, EventTypeContainerDie, payload.Type)
		require.Equal(t, "System", *payload.Username)
		require.Equal(t, "0", *payload.EnvironmentID)
		require.Equal(t, "container-id", *payload.ResourceID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for daemon event forwarding")
	}
	require.Never(t, func() bool { return len(requests) != 0 }, 50*time.Millisecond, time.Millisecond)
	requireDaemonCount(t, db, 1)
}

func TestEventStreamPublishesCommittedMutations(t *testing.T) {
	db := setupEventServiceTestDB(t)
	service := NewEventService(db, nil, nil)
	changes := 0
	var persistedCounts []int64
	unsubscribe := service.Changes.Subscribe(func(types.EnvironmentChanges) {
		changes++
		var count int64
		require.NoError(t, db.Model(&Event{}).Count(&count).Error)
		persistedCounts = append(persistedCounts, count)
	})
	defer unsubscribe()
	created, err := service.CreateEvent(t.Context(), CreateEventRequest{Type: EventTypeImagePull, Title: "Pulled"})
	require.NoError(t, err)
	require.Equal(t, 1, changes)
	_, err = service.IngestAgentEvent(t.Context(), "remote", CreateEventRequest{Type: EventTypeImagePull, Title: "Remote pull"})
	require.NoError(t, err)
	require.Equal(t, 2, changes)
	require.NoError(t, service.DeleteOldEvents(t.Context(), time.Hour))
	require.Equal(t, 2, changes)
	require.Error(t, service.DeleteEvent(t.Context(), "missing"))
	require.Equal(t, 2, changes)
	require.NoError(t, service.DeleteEvent(t.Context(), created.ID))
	require.Equal(t, 3, changes)
	require.NoError(t, service.DeleteOldEvents(t.Context(), -time.Hour))
	require.Equal(t, 4, changes)
	require.Equal(t, []int64{1, 2, 1, 0}, persistedCounts)
	require.NoError(t, db.Migrator().DropTable(&Event{}))
	_, err = service.CreateEvent(t.Context(), CreateEventRequest{Title: "Cannot persist"})
	require.Error(t, err)
	require.Error(t, service.DeleteOldEvents(t.Context(), time.Hour))
	require.Error(t, service.DeleteEvent(t.Context(), created.ID))
	require.Equal(t, 4, changes)
}

func TestEventStreamProducerCoalescesAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		output := make(chan types.EventStreamEvent)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			service.RunStreamProducer(ctx, authz.SudoPermissionSet(), output)
		}()
		synctest.Wait()
		// Initial send is blocked, but subscriptions must already be ready.
		for range 100 {
			service.PublishChange("0", "container")
		}
		initial := <-output
		require.Equal(t, "changed", initial.Type)
		require.False(t, initial.Timestamp.IsZero())
		require.Equal(t, "changed", (<-output).Type)
		synctest.Wait()
		select {
		case <-output:
			t.Fatal("burst should coalesce to one pending invalidation")
		default:
		}
		// A stalled client must not hold up publishers or prevent shutdown.
		service.PublishChange("0", "container")
		synctest.Wait()
		cancel()
		<-stopped
		service.PublishChange("0", "container")
	})
}

func TestEventStreamProducerBroadcasts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		first := make(chan types.EventStreamEvent, 1)
		second := make(chan types.EventStreamEvent, 1)
		go service.RunStreamProducer(ctx, authz.SudoPermissionSet(), first)
		go service.RunStreamProducer(ctx, authz.SudoPermissionSet(), second)
		synctest.Wait()
		<-first
		<-second
		service.PublishChange("0", "container")
		synctest.Wait()
		require.Equal(t, "changed", (<-first).Type)
		require.Equal(t, "changed", (<-second).Type)
	})
}

func TestMapDaemonEvent(t *testing.T) {
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
			req, ok := mapDaemonEvent(events.Message{
				Type:   tc.kind,
				Action: tc.action,
				Scope:  "local",
				Actor: events.Actor{
					ID: "resource-id",
					Attributes: map[string]string{
						"name":                       "web",
						"image":                      "alpine",
						"exitCode":                   "0",
						"signal":                     "15",
						"com.docker.compose.project": "demo",
						"com.docker.compose.service": "web",
						"secret":                     "never-copy",
					},
				},
			})
			require.True(t, ok)
			require.Equal(t, tc.want, req.Type)
			require.Equal(t, "web", *req.ResourceName)
			require.Equal(t, "0", *req.EnvironmentID)
			require.Nil(t, req.UserID)
			require.Nil(t, req.Username)
			require.Equal(
				t,
				database.JSON{
					"source":         "docker",
					"action":         string(tc.action),
					"scope":          "local",
					"name":           "web",
					"image":          "alpine",
					"exitCode":       "0",
					"signal":         "15",
					"composeProject": "demo",
					"composeService": "web",
				},
				req.Metadata,
			)
			severity := kit.Ternary(tc.action == events.ActionOOM, EventSeverityError, EventSeverityInfo)
			if tc.action == events.ActionHealthStatusUnhealthy {
				severity = EventSeverityWarning
			}
			require.Equal(t, severity, req.Severity)
		})
	}
	for _, exitCode := range []string{"1", "137", ""} {
		t.Run("die exit "+exitCode, func(t *testing.T) {
			req, ok := mapDaemonEvent(events.Message{
				Type:   events.ContainerEventType,
				Action: events.ActionDie,
				Actor: events.Actor{
					ID:         "id",
					Attributes: map[string]string{"exitCode": exitCode},
				},
			})
			require.True(t, ok)
			require.Equal(t, EventSeverityWarning, req.Severity)
			require.Equal(t, "id", *req.ResourceName)
		})
	}
	for _, kind := range []events.Type{events.ImageEventType, events.VolumeEventType, events.NetworkEventType} {
		t.Run(string(kind)+" empty prune", func(t *testing.T) {
			req, ok := mapDaemonEvent(events.Message{Type: kind, Action: events.ActionPrune})
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
		{
			"exec",
			events.ContainerEventType,
			"exec_create",
			"id",
		}, {
			"attach",
			events.ContainerEventType,
			"attach",
			"id",
		}, {
			"resize",
			events.ContainerEventType,
			"resize",
			"id",
		}, {
			"healthy",
			events.ContainerEventType,
			"health_status: healthy",
			"id",
		}, {
			"mount",
			events.VolumeEventType,
			"mount",
			"id",
		}, {
			"connect",
			events.NetworkEventType,
			"connect",
			"id",
		}, {
			"disconnect",
			events.NetworkEventType,
			"disconnect",
			"id",
		}, {
			"unknown action",
			events.ImageEventType,
			"unknown-image-action",
			"image-id",
		}, {
			"empty create",
			events.ContainerEventType,
			events.ActionCreate,
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := mapDaemonEvent(events.Message{Type: tc.kind, Action: tc.action, Actor: events.Actor{ID: tc.id}})
			require.False(t, ok)
		})
	}
}

func TestDaemonEventsPersistEveryOccurrence(t *testing.T) {
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)
	die := events.Message{Type: events.ContainerEventType, Action: events.ActionDie, Actor: events.Actor{ID: "web", Attributes: map[string]string{"exitCode": "0"}}}
	stop := die
	stop.Action = events.ActionStop
	for index, msg := range []events.Message{die, stop, die, stop} {
		msg.TimeNano = int64(index + 1)
		svc.RecordDockerEvent(t.Context(), msg)
	}
	require.NoError(t, svc.Stop(t.Context()))
	requireDaemonCount(t, db, 4)
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
		name := kit.Ternary(failure, "persistence failure", "suppression")
		t.Run(name, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			now := time.Now()
			svc.correlation = correlation.NewService(func() time.Time { return now })
			msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, TimeNano: 123, Actor: events.Actor{ID: "web"}}
			if failure {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("daemon_failure", func(tx *gorm.DB) {
					failureErr := errors.New("unavailable")
					require.ErrorIs(t, tx.AddError(failureErr), failureErr)
				}))
			}
			if !failure {
				svc.MarkDockerExpectation("container", "web", "")
			}
			svc.RecordDockerEvent(t.Context(), msg)
			require.NoError(t, svc.Stop(t.Context()))
			requireDaemonCount(t, db, 0)
			if failure {
				require.NoError(t, db.Callback().Create().Remove("daemon_failure"))
			}
			now = now.Add(time.Minute)
			svc.RecordDockerEvent(t.Context(), msg)
			requireDaemonCount(t, db, 1)
		})
	}
}

func TestDockerEventSubscriptionsReadyAndStop(t *testing.T) {
	for _, closeBus := range []bool{false, true} {
		name := kit.Ternary(closeBus, "closed bus", "cancellation")
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := setupEventServiceTestDB(t)
				sqlDB, err := db.DB.DB()
				require.NoError(t, err)
				defer func() { assert.NoError(t, sqlDB.Close()) }()
				sqlDB.SetMaxOpenConns(1)
				svc := NewEventService(db, nil, nil)
				var recovered, published atomic.Int64
				stopChanges := svc.Changes.Subscribe(func(types.EnvironmentChanges) { published.Add(1) })
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
							action := kit.Ternary(kind == events.ImageEventType, events.ActionPull, events.ActionCreate)
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
				time.Sleep(daemonEventFlushInterval)
				synctest.Wait()
				requireDaemonCount(t, db, 4*occurrences)
				require.Equal(t, int64(4*occurrences), published.Load())
				publishBurst(occurrences)
				synctest.Wait()
				time.Sleep(daemonEventFlushInterval)
				synctest.Wait()
				requireDaemonCount(t, db, 8*occurrences)
				require.Equal(t, int64(8*occurrences), published.Load())
				if closeBus {
					eventBus.Close()
				} else {
					cancel()
				}
				synctest.Wait()
				require.NoError(t, <-done)
				require.NoError(t, svc.Stop(t.Context()))
				cleanup()
				cleanup()
			})
		})
	}
}

func TestDaemonEventWriterBatchesAndFlushesOnStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := setupEventServiceTestDB(t)
		sqlDB, err := db.DB.DB()
		require.NoError(t, err)
		defer func() { assert.NoError(t, sqlDB.Close()) }()
		sqlDB.SetMaxOpenConns(1)
		svc := NewEventService(db, nil, nil)
		defer func() { assert.NoError(t, svc.Stop(t.Context())) }()
		var published atomic.Int64
		stopChanges := svc.Changes.Subscribe(func(types.EnvironmentChanges) { published.Add(1) })
		defer stopChanges()
		record := func(timeNano int64) {
			svc.RecordDockerEvent(t.Context(), events.Message{Type: events.ContainerEventType, Action: events.ActionStart, TimeNano: timeNano, Actor: events.Actor{ID: "web"}})
		}

		const occurrences = DaemonEventBatchSize*3 + 5
		var wg sync.WaitGroup
		for occurrence := range int64(occurrences) {
			for range 2 {
				wg.Go(func() { record(occurrence + 1) })
			}
		}
		wg.Wait()
		synctest.Wait()
		time.Sleep(daemonEventFlushInterval)
		synctest.Wait()
		requireDaemonCount(t, db, occurrences)
		require.Equal(t, int64(occurrences), published.Load())

		// Fewer than a batch stays queued until the flush timer, which Stop preempts.
		for occurrence := range int64(5) {
			record(occurrences + occurrence + 1)
		}
		require.NoError(t, svc.Stop(t.Context()))
		requireDaemonCount(t, db, occurrences+5)

		record(occurrences + 6)
		requireDaemonCount(t, db, occurrences+6)
		require.Equal(t, int64(occurrences+6), published.Load())
	})
}

func requireDaemonCount(t *testing.T, db *database.DB, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&Event{}).Count(&count).Error)
	require.Equal(t, want, count)
}

func TestCreateEventCorrelationEligibility(t *testing.T) {
	for _, tc := range []struct {
		name        string
		environment *string
		source      string
		eventType   EventType
		want        bool
	}{
		{"local", new("0"), "", EventTypeContainerStart, true},
		{"implicit local", nil, "", EventTypeContainerStart, true},
		{"remote", new("remote"), "", EventTypeContainerStart, false},
		{"daemon", new("0"), "docker", EventTypeContainerStart, false},
		{"failure", new("0"), "", EventTypeContainerError, false},
		{"container scan", new("0"), "", EventTypeContainerScan, false},
		{"image scan", new("0"), "", EventTypeImageScan, false},
		{"volume backup", new("0"), "", EventTypeVolumeBackupCreate, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupEventServiceTestDB(t)
			svc := NewEventService(db, nil, nil)
			_, err := svc.CreateEvent(
				t.Context(),
				CreateEventRequest{
					Type:  tc.eventType,
					Title: "action",
					ResourceType: new(strings.SplitN(
						string(tc.eventType),
						".",
						2,
					)[0]),
					ResourceID:    new("id"),
					EnvironmentID: tc.environment,
					Metadata:      database.JSON{"source": tc.source},
				},
			)
			require.NoError(t, err)
			require.Equal(t, tc.want, svc.ShouldSuppressDaemonEvent(strings.SplitN(string(tc.eventType), ".", 2)[0], "id", "", ""))
		})
	}
}

func TestRecordDockerEventPublishesSuppressedChange(t *testing.T) {
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db, nil, nil)
	svc.MarkDockerExpectation("container", "web", "")
	svc.RecordDockerEvent(t.Context(), events.Message{
		Type:     events.ContainerEventType,
		Action:   events.ActionStart,
		TimeNano: 123,
		Actor:    events.Actor{ID: "web"},
	})
	require.NoError(t, svc.Stop(t.Context()))
	requireDaemonCount(t, db, 0)
	require.Contains(t, svc.EnvironmentChanges("0").Kinds, "container")
}

func TestPublishChangeNormalizesKinds(t *testing.T) {
	svc := NewEventService(nil, nil, nil)
	for _, kind := range []string{"images", "podman.pod.start", " Container.Start ", "git.sync.run", "image_update", ""} {
		svc.PublishChange("", kind)
	}
	require.ElementsMatch(t, []string{"image", "podman", "container", "gitops", "image_update"}, slices.Collect(maps.Keys(svc.EnvironmentChanges("0").Kinds)))
}

func TestEventStreamProducerFlushesOnceForAccessibleEnvironments(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ps := authz.NewPermissionSet()
		ps.AddEnv("1", authz.PermContainersRead)
		output := make(chan types.EventStreamEvent, 4)
		go service.RunStreamProducer(ctx, ps, output)
		synctest.Wait()
		require.Empty(t, (<-output).Changes)

		service.PublishChange("1", "container")
		service.PublishChange("1", "image")
		service.PublishChange("2", "container")
		synctest.Wait()
		require.Empty(t, output)
		time.Sleep(changeStreamFlushInterval)
		synctest.Wait()
		require.Len(t, output, 1)
		flushed := <-output
		require.Len(t, flushed.Changes, 1)
		require.Equal(t, "1", flushed.Changes[0].EnvironmentID)
		require.ElementsMatch(t, []string{"container", "image"}, slices.Collect(maps.Keys(flushed.Changes[0].Kinds)))

		service.PublishChange("2", "volume")
		time.Sleep(2 * changeStreamFlushInterval)
		synctest.Wait()
		require.Empty(t, output)
	})
}

func TestWaitForChangesSinceAndEpoch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		service.PublishChange("0", "container")
		current := service.EnvironmentChanges("0")
		started := time.Now()
		require.Equal(t, current, service.WaitForChanges(t.Context(), "0", "previous-process", current.Kinds["container"], 0))
		require.Equal(t, current, service.WaitForChanges(t.Context(), "0", current.Epoch, 0, 0))
		require.Zero(t, time.Since(started))
		require.Equal(t, current, service.WaitForChanges(t.Context(), "0", current.Epoch, current.Kinds["container"], 0))
		require.Equal(t, changeWaitLimit, time.Since(started))
	})
}

func TestRelayChangesPublishesAllOnBaselineAndEpochChange(t *testing.T) {
	service := NewEventService(nil, nil, nil)
	cursor := types.EnvironmentChanges{
		EnvironmentID: "0",
		Epoch:         "agent-start",
		Kinds:         map[string]uint64{"container": 3, "image": 5},
	}
	service.RelayChanges("7", types.EnvironmentChanges{}, cursor)
	require.ElementsMatch(t, []string{"all"}, slices.Collect(maps.Keys(service.EnvironmentChanges("7").Kinds)))

	service.RelayChanges("7", cursor, types.EnvironmentChanges{
		EnvironmentID: "0",
		Epoch:         "agent-start",
		Kinds:         map[string]uint64{"container": 3, "image": 6, "volume": 7},
	})
	require.ElementsMatch(t, []string{"all", "image", "volume"}, slices.Collect(maps.Keys(service.EnvironmentChanges("7").Kinds)))

	service.RelayChanges("7", cursor, types.EnvironmentChanges{
		EnvironmentID: "0",
		Epoch:         "agent-restart",
		Kinds:         map[string]uint64{"container": 1},
	})
	require.Contains(t, service.EnvironmentChanges("7").Kinds, "all")
}

func TestWaitForChangesReturnsOnPublish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewEventService(nil, nil, nil)
		epoch := service.EnvironmentChanges("0").Epoch
		result := make(chan types.EnvironmentChanges, 1)
		started := time.Now()
		go func() {
			result <- service.WaitForChanges(t.Context(), "0", epoch, 0, 0)
		}()
		synctest.Wait()
		service.PublishChange("1", "container")
		synctest.Wait()
		require.Empty(t, result)

		service.PublishChange("0", "container")
		service.PublishChange("0", "image")
		changes := <-result
		require.Equal(t, changeWaitCoalesce, time.Since(started))
		require.ElementsMatch(t, []string{"container", "image"}, slices.Collect(maps.Keys(changes.Kinds)))
	})
}
