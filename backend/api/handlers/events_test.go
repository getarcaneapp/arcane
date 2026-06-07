package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEventDeleteRequiresEventsDeletePermission(t *testing.T) {
	testCases := []struct {
		name       string
		permission string
		wantStatus int
	}{
		{
			name:       "events read cannot delete",
			permission: authz.PermEventsRead,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "events delete reaches handler",
			permission: authz.PermEventsDelete,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ps := authz.NewPermissionSet()
			ps.AddGlobal(testCase.permission)
			router, api := newPermissionGatingRouterInternal(t, ps)
			RegisterEvents(api, nil)

			req := httptest.NewRequest(http.MethodDelete, "/api/events/event-1", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, testCase.wantStatus, rec.Code)
			if testCase.wantStatus == http.StatusForbidden {
				require.Contains(t, rec.Body.String(), authz.PermEventsDelete)
			}
		})
	}
}

func TestAgentEventIngestionRequiresAgentTokenAndPersists(t *testing.T) {
	ctx := context.Background()
	db := setupEventIngestionTestDBInternal(t)
	eventSvc := services.NewEventService(db, &config.Config{}, nil)
	router := echo.New()
	RegisterAgentEventIngestion(router.Group("/api"), eventSvc, &config.Config{AgentToken: "agent-token"})

	payload := services.CreateEventRequest{
		Type:          models.EventTypeContainerStart,
		Severity:      models.EventSeverityInfo,
		Title:         "Container started: web",
		Description:   "Container 'web' has been started",
		EnvironmentID: ptrStringInternal("0"),
		Metadata:      models.JSON{"source": "agent"},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	t.Run("unconfigured manager token is service unavailable", func(t *testing.T) {
		unconfiguredRouter := echo.New()
		RegisterAgentEventIngestion(unconfiguredRouter.Group("/api"), eventSvc, &config.Config{})

		req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(pkgutils.HeaderAgentToken, "agent-token")
		rec := httptest.NewRecorder()
		unconfiguredRouter.ServeHTTP(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "agent event ingestion is not configured")
		require.Equal(t, int64(0), countPersistedEventsInternal(t, ctx, db))
	})

	t.Run("missing token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, int64(0), countPersistedEventsInternal(t, ctx, db))
	})

	t.Run("api key header is not accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(pkgutils.HeaderApiKey, "agent-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Equal(t, int64(0), countPersistedEventsInternal(t, ctx, db))
	})

	t.Run("agent token persists event", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(pkgutils.HeaderAgentToken, "agent-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusAccepted, rec.Code)

		var saved models.Event
		require.NoError(t, db.WithContext(ctx).First(&saved).Error)
		require.Equal(t, models.EventTypeContainerStart, saved.Type)
		require.Equal(t, "Container started: web", saved.Title)
		require.NotNil(t, saved.EnvironmentID)
		require.Equal(t, "0", *saved.EnvironmentID)
		require.Equal(t, "agent", saved.Metadata["source"])
	})
}

func setupEventIngestionTestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Event{}))
	return &database.DB{DB: db}
}

func countPersistedEventsInternal(t *testing.T, ctx context.Context, db *database.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.Event{}).Count(&count).Error)
	return count
}

func ptrStringInternal(value string) *string {
	return &value
}
