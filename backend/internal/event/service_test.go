package event

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	ctx := context.Background()
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

func TestCloneEventMetadataInternal(t *testing.T) {
	src := database.JSON{"a": "b"}
	cloned := cloneEventMetadataInternal(src)

	require.Equal(t, "b", cloned["a"])
	cloned["a"] = "changed"
	require.Equal(t, "b", src["a"], "clone should not mutate source metadata")

	nilClone := cloneEventMetadataInternal(nil)
	require.NotNil(t, nilClone)
	require.Empty(t, nilClone)

	nested := database.JSON{
		"outer": map[string]any{
			"slice": []any{
				map[string]any{"k": "v"},
			},
		},
	}
	nestedClone := cloneEventMetadataInternal(nested)
	require.NotNil(t, nestedClone["outer"])

	outer := nested["outer"].(map[string]any)
	outerClone := nestedClone["outer"].(database.JSON)
	sliceOriginal := outer["slice"].([]any)
	sliceClone := outerClone["slice"].([]any)

	sliceClone[0].(database.JSON)["k"] = "changed"
	require.Equal(t, "v", sliceOriginal[0].(map[string]any)["k"], "nested map inside slice should be deep-cloned")
}

func TestEventService_LogErrorEvent_DoesNotMutateInputMetadata(t *testing.T) {
	ctx := context.Background()
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

func TestEventService_CreateEvent_ForwardsToManagerAPIInAgentMode(t *testing.T) {
	ctx := context.Background()
	db := setupEventServiceTestDB(t)

	requests := make(chan CreateEventRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/events", r.URL.Path) {
			return
		}
		if !assert.Equal(t, "test-agent-token", r.Header.Get(utils.HeaderAgentToken)) {
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
		AgentMode:     true,
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
	ctx := context.Background()
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
	ctx := context.Background()
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
	require.Equal(t, EventSeverityCounts{Total: 4, Info: 2, Success: 1, Warning: 0, Error: 1}, counts)
}

func TestEventService_ListEventsPaginated_TypeCategoryFilter(t *testing.T) {
	ctx := context.Background()
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
			req, ok := mapDaemonEventInternal(events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "container-id", Attributes: map[string]string{"name": "web", "com.docker.compose.project": "demo"}}})
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
			requireDaemonCountInternal(t, db, 0)
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
	svc := NewEventService(db, &config.Config{AgentMode: true, AgentToken: "agent-token", ManagerApiUrl: server.URL}, server.Client())
	message := events.Message{Type: events.ContainerEventType, Action: events.ActionDie, TimeNano: 123, Actor: events.Actor{ID: "container-id", Attributes: map[string]string{"name": "web", "exitCode": "137", "signal": "9"}}}
	req, ok := mapDaemonEventInternal(message)
	require.True(t, ok)
	svc.RecordDockerEvent(t.Context(), message)
	svc.RecordDockerEvent(t.Context(), message)
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
	requireDaemonCountInternal(t, db, 1)

}
