package handlers

import (
	"context"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

func setupEventsHandlerTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Event{}))
	return &database.DB{DB: db}
}

func strPtr(s string) *string { return &s }

func seedEvent(t *testing.T, svc *services.EventService, envID string, severity models.EventSeverity, title string) {
	t.Helper()
	_, err := svc.CreateEvent(context.Background(), services.CreateEventRequest{
		Type:          models.EventTypeSystemPrune,
		Severity:      severity,
		Title:         title,
		Description:   "test",
		EnvironmentID: strPtr(envID),
		Username:      strPtr("tester"),
		Metadata:      models.JSON{"test": true},
	})
	require.NoError(t, err)
}

func TestEventHandler_ListEvents_MapsEnvironmentFilter(t *testing.T) {
	ctx := context.Background()
	db := setupEventsHandlerTestDB(t)
	eventSvc := services.NewEventService(db, nil)
	h := &EventHandler{eventService: eventSvc}

	seedEvent(t, eventSvc, "0", models.EventSeverityInfo, "env0")
	seedEvent(t, eventSvc, "123", models.EventSeverityInfo, "env123")

	out, err := h.ListEvents(ctx, &ListEventsInput{
		Environment: "123",
		Start:       0,
		Limit:       50,
		Order:       "desc",
	})
	require.NoError(t, err)
	require.True(t, out.Body.Success)
	require.Len(t, out.Body.Data, 1)
	require.NotNil(t, out.Body.Data[0].EnvironmentID)
	require.Equal(t, "123", *out.Body.Data[0].EnvironmentID)
}

func TestEventHandler_GetEventsByEnvironment_MapsSeverityFilter(t *testing.T) {
	ctx := context.Background()
	db := setupEventsHandlerTestDB(t)
	eventSvc := services.NewEventService(db, nil)
	h := &EventHandler{eventService: eventSvc}

	seedEvent(t, eventSvc, "0", models.EventSeverityInfo, "info")
	seedEvent(t, eventSvc, "0", models.EventSeverityError, "error")
	seedEvent(t, eventSvc, "999", models.EventSeverityError, "other-env")

	out, err := h.GetEventsByEnvironment(ctx, &GetEventsByEnvironmentInput{
		EnvironmentID: "0",
		Severity:      string(models.EventSeverityError),
		Start:         0,
		Limit:         50,
		Order:         "desc",
	})
	require.NoError(t, err)
	require.True(t, out.Body.Success)
	require.Len(t, out.Body.Data, 1)
	require.Equal(t, string(models.EventSeverityError), out.Body.Data[0].Severity)
	require.NotNil(t, out.Body.Data[0].EnvironmentID)
	require.Equal(t, "0", *out.Body.Data[0].EnvironmentID)
}
