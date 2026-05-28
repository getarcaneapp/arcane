package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

func setupActivityHandlerTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Activity{},
		&models.ActivityMessage{},
		&models.Environment{},
		&models.SettingVariable{},
	))
	return &database.DB{DB: db}
}

func TestActivityHandlerClearHistoryDeletesSelectedEnvironmentOnlyInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityHandlerTestDBInternal(t)
	activityService := services.NewActivityService(db)
	handler := &ActivityHandler{activityService: activityService}

	completed, err := activityService.StartActivity(ctx, services.StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	_, err = activityService.CompleteActivity(ctx, completed.ID, models.ActivityStatusSuccess, "done", nil)
	require.NoError(t, err)

	running, err := activityService.StartActivity(ctx, services.StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	remoteCompleted, err := activityService.StartActivity(ctx, services.StartActivityRequest{EnvironmentID: "remote-1", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)
	_, err = activityService.CompleteActivity(ctx, remoteCompleted.ID, models.ActivityStatusSuccess, "done", nil)
	require.NoError(t, err)

	out, err := handler.ClearHistory(ctx, &ClearActivityHistoryInput{EnvironmentID: "0"})
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Body.Data.Deleted)

	var remaining []models.Activity
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.ElementsMatch(t, []string{running.ID, remoteCompleted.ID}, []string{remaining[0].ID, remaining[1].ID})
}

func TestActivityHandlerClearHistoryProxiesRemoteEnvironmentInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityHandlerTestDBInternal(t)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	token := "remote-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/api/environments/0/activities/history", r.URL.Path)
		require.Equal(t, token, r.Header.Get("X-API-Key"))
		require.Equal(t, token, r.Header.Get("X-Arcane-Agent-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"deleted":7}}`))
	}))
	defer server.Close()

	now := time.Now()
	require.NoError(t, db.Create(&models.Environment{
		BaseModel: models.BaseModel{
			ID:        "remote-1",
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        "Remote",
		ApiUrl:      server.URL,
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)

	handler := &ActivityHandler{
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	out, err := handler.ClearHistory(ctx, &ClearActivityHistoryInput{EnvironmentID: "remote-1"})
	require.NoError(t, err)
	require.EqualValues(t, 7, out.Body.Data.Deleted)
}
