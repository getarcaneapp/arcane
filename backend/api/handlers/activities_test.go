package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/activity"
)

func setupActivityHandlerTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Activity{},
		&models.ActivityMessage{},
		&models.Environment{},
		&models.SettingVariable{},
	))
	return &database.DB{DB: db}
}

func TestActivitySnapshotFingerprintDetectsChangesInternal(t *testing.T) {
	progress := 10
	items := []activity.Activity{
		{ID: "a-1", Status: activity.StatusRunning, Progress: &progress, Step: "pull"},
		{ID: "a-2", Status: activity.StatusSuccess},
	}
	base := activitySnapshotFingerprintInternal(items)
	require.Equal(t, base, activitySnapshotFingerprintInternal(items))

	bumped := 20
	items[0].Progress = &bumped
	require.NotEqual(t, base, activitySnapshotFingerprintInternal(items))
}

func TestActivityHandlerClearHistoryDeletesSelectedEnvironmentOnlyInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityHandlerTestDBInternal(t)
	activityService := services.NewActivityService(db, nil)
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

// limitStreamTestDBToSingleConnInternal serializes DB access: the aggregated
// stream queries from concurrent goroutines, and every extra pooled
// connection to a :memory: SQLite database is a fresh empty database.
func limitStreamTestDBToSingleConnInternal(t *testing.T, db *database.DB) {
	t.Helper()
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
}

func createStreamTestRemoteEnvironmentInternal(t *testing.T, db *database.DB, environmentID, name, apiURL, token string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.Environment{
		BaseModel: models.BaseModel{
			ID:        environmentID,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        name,
		ApiUrl:      apiURL,
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)
}

// runStreamAllInternal drives streamAllActivitiesInternal through a pipe and
// returns each decoded event to onEvent until it reports done or the stream
// ends; remaining output is drained so a blocked writer can always finish.
func runStreamAllInternal(t *testing.T, ctx context.Context, cancel context.CancelFunc, handler *ActivityHandler, ps *authz.PermissionSet, onEvent func(activity.StreamEvent) bool) {
	t.Helper()

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = pw.Close() }()
		handler.streamAllActivitiesInternal(ctx, ps, 50, pw, func() {})
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		var event activity.StreamEvent
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &event))
		if onEvent(event) {
			cancel()
			break
		}
	}

	go func() {
		_, _ = io.Copy(io.Discard, pr)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after cancel")
	}
}

func TestActivityHandlerStreamAllEmitsEnvironmentScopedEventsInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	activityService := services.NewActivityService(db, settingsService)

	local, err := activityService.StartActivity(ctx, services.StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)

	token := "remote-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"remote-activity-1"}],"pagination":{"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":50}}`))
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &ActivityHandler{
		activityService:    activityService,
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	var localSnapshot, remoteSnapshot bool
	runStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event activity.StreamEvent) bool {
		if event.Type == "snapshot" && event.EnvironmentID == "0" && len(event.Activities) == 1 {
			require.Equal(t, local.ID, event.Activities[0].ID)
			require.Equal(t, "0", event.Activities[0].SourceEnvironmentID)
			localSnapshot = true
		}
		if event.Type == "snapshot" && event.EnvironmentID == "remote-1" && len(event.Activities) == 1 {
			require.Equal(t, "remote-activity-1", event.Activities[0].ID)
			require.Equal(t, "remote-1", event.Activities[0].SourceEnvironmentID)
			require.Equal(t, "Remote", event.Activities[0].SourceEnvironmentName)
			remoteSnapshot = true
		}
		return localSnapshot && remoteSnapshot
	})

	require.True(t, localSnapshot)
	require.True(t, remoteSnapshot)
}

func TestActivityHandlerStreamAllReusesRemoteEnvironmentAfterInitialPollInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	activityService := services.NewActivityService(db, settingsService)

	var countEnvironmentQueries atomic.Bool
	var environmentQueryCount atomic.Int64
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("arcane_test_count_environment_queries", func(tx *gorm.DB) {
		if countEnvironmentQueries.Load() && activityTestQueryLoadsEnvironmentIDInternal(tx, "remote-1") {
			environmentQueryCount.Add(1)
		}
	}))

	token := "remote-token"
	// Vary the payload per poll so snapshot fingerprint suppression does not
	// swallow the follow-up snapshots this test waits for.
	var pollCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := fmt.Sprintf(`{"success":true,"data":[{"id":"remote-activity-1","latestMessage":"poll-%d"}],"pagination":{"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":50}}`, pollCount.Add(1))
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &ActivityHandler{
		activityService:    activityService,
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	remoteSnapshotCount := 0
	runStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event activity.StreamEvent) bool {
		if event.Type != "snapshot" || event.EnvironmentID != "remote-1" {
			return false
		}

		remoteSnapshotCount++
		if remoteSnapshotCount == 1 {
			countEnvironmentQueries.Store(true)
			return false
		}

		require.Zero(t, environmentQueryCount.Load(), "steady-state remote activity poll should not reload environment rows")
		return true
	})

	require.GreaterOrEqual(t, remoteSnapshotCount, 2)
}

func activityTestQueryLoadsEnvironmentIDInternal(tx *gorm.DB, environmentID string) bool {
	if tx.Statement == nil || tx.Statement.Table != "environments" {
		return false
	}
	for _, arg := range tx.Statement.Vars {
		if arg == environmentID {
			return true
		}
	}
	return false
}

func TestActivityHandlerStreamAllRemoteFailureEmitsErrorAndKeepsStreamingInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	activityService := services.NewActivityService(db, settingsService)

	token := "remote-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &ActivityHandler{
		activityService:    activityService,
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	var localSnapshot, remoteError bool
	runStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event activity.StreamEvent) bool {
		if event.Type == "snapshot" && event.EnvironmentID == "0" {
			localSnapshot = true
		}
		if event.Type == "error" && event.EnvironmentID == "remote-1" && event.Error != "" {
			remoteError = true
		}
		return localSnapshot && remoteError
	})

	require.True(t, localSnapshot)
	require.True(t, remoteError)
}

func TestActivityHandlerStreamAllFiltersUnauthorizedEnvironmentsInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	activityService := services.NewActivityService(db, settingsService)
	_, err = activityService.StartActivity(ctx, services.StartActivityRequest{EnvironmentID: "0", Type: models.ActivityTypeResourceAction})
	require.NoError(t, err)

	allowedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"allowed-activity"}],"pagination":{"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":50}}`))
	}))
	defer allowedServer.Close()

	var deniedRequests atomic.Int64
	deniedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deniedRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[],"pagination":{"totalPages":1,"totalItems":0,"currentPage":1,"itemsPerPage":50}}`))
	}))
	defer deniedServer.Close()

	createStreamTestRemoteEnvironmentInternal(t, db, "remote-allowed", "Allowed", allowedServer.URL, "allowed-token")
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-denied", "Denied", deniedServer.URL, "denied-token")

	handler := &ActivityHandler{
		activityService:    activityService,
		environmentService: services.NewEnvironmentService(db, allowedServer.Client(), nil, nil, settingsService, nil),
	}
	ps := authz.NewPermissionSet()
	ps.AddEnv("remote-allowed", authz.PermActivitiesRead)

	seenEnvironments := make(map[string]struct{})
	runStreamAllInternal(t, ctx, cancel, handler, ps, func(event activity.StreamEvent) bool {
		if event.EnvironmentID != "" {
			seenEnvironments[event.EnvironmentID] = struct{}{}
		}
		return event.Type == "snapshot" && event.EnvironmentID == "remote-allowed"
	})

	require.Contains(t, seenEnvironments, "remote-allowed")
	require.NotContains(t, seenEnvironments, "0")
	require.NotContains(t, seenEnvironments, "remote-denied")
	require.Zero(t, deniedRequests.Load())
}
