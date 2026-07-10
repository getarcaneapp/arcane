package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	sqlite "github.com/libtnb/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardHandlerTestDB(t *testing.T) (*database.DB, *services.SettingsService) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}, &models.Environment{}, &models.ImageUpdateRecord{}, &models.Project{}, &models.SettingVariable{}))

	databaseDB := &database.DB{DB: db}
	settingsSvc, err := services.NewSettingsService(context.Background(), databaseDB)
	require.NoError(t, err)

	return databaseDB, settingsSvc
}

func newDashboardHandlerTestDockerService(
	t *testing.T,
	settingsSvc *services.SettingsService,
	containers []dockercontainer.Summary,
	images []dockerimage.Summary,
) *services.DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.41")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode(containers))
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			require.NoError(t, json.NewEncoder(w).Encode(images))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return services.NewDockerClientService(
		context.Background(),
		nil,
		&config.Config{DockerHost: server.URL},
		settingsSvc,
	)
}

func TestDashboardHandlerGetDashboardReturnsSnapshot(t *testing.T) {
	db, settingsSvc := setupDashboardHandlerTestDB(t)

	containers := []dockercontainer.Summary{
		{
			ID:      "container-running",
			Names:   []string{"/running-app"},
			Image:   "repo/app:stable",
			ImageID: "sha256:image-a",
			Created: 1700000000,
			State:   "running",
			Status:  "Up 2 hours",
			Labels:  map[string]string{},
		},
		{
			ID:      "container-stopped",
			Names:   []string{"/stopped-app"},
			Image:   "repo/worker:latest",
			ImageID: "sha256:image-b",
			Created: 1800000000,
			State:   "exited",
			Status:  "Exited (0) 1 hour ago",
			Labels:  map[string]string{},
		},
	}
	images := []dockerimage.Summary{
		{ID: "sha256:image-a", RepoTags: []string{"repo/app:stable"}, Created: 1710000000, Size: 100},
		{ID: "sha256:image-b", RepoTags: []string{"repo/worker:latest"}, Created: 1720000000, Size: 250},
	}

	require.NoError(t, db.WithContext(context.Background()).Create(&models.ImageUpdateRecord{
		ID:        "sha256:image-b",
		HasUpdate: true,
	}).Error)
	require.NoError(t, db.WithContext(context.Background()).Create(&models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_handler",
		UserID:    new("user-1"),
		ExpiresAt: new(time.Now().Add(12 * time.Hour)),
	}).Error)

	dockerSvc := newDashboardHandlerTestDockerService(t, settingsSvc, containers, images)
	handler := &DashboardHandler{
		dashboardService: services.NewDashboardService(db, dockerSvc, nil, nil, nil, settingsSvc, nil, nil, nil),
	}

	output, err := handler.GetDashboard(context.Background(), &GetDashboardInput{EnvironmentID: "0"})
	require.NoError(t, err)
	require.NotNil(t, output)
	require.True(t, output.Body.Success)

	snapshot := output.Body.Data
	require.Len(t, snapshot.Containers.Data, 2)
	require.Len(t, snapshot.Images.Data, 2)
	require.Equal(t, 1, snapshot.Containers.Counts.RunningContainers)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, dashboardtypes.SnapshotSettings{}, snapshot.Settings)
	require.ElementsMatch(t, []dashboardtypes.ActionItem{
		{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindImageUpdates, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindExpiringKeys, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
	}, snapshot.ActionItems.Items)
}

// runDashboardStreamAllInternal drives streamAllDashboardsInternal through a
// pipe and returns each decoded event to onEvent until it reports done or the
// stream ends; remaining output is drained so a blocked encoder can finish.
func runDashboardStreamAllInternal(t *testing.T, ctx context.Context, cancel context.CancelFunc, handler *DashboardHandler, ps *authz.PermissionSet, onEvent func(dashboardtypes.StreamEvent) bool) {
	t.Helper()

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = pw.Close() }()
		handler.streamAllDashboardsInternal(ctx, ps, false, json.NewEncoder(pw), func() {})
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		var event dashboardtypes.StreamEvent
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

func TestDashboardHandlerStreamAllEmitsRemoteSnapshotInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	token := "remote-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/environments/0/dashboard", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{
			"containers":{"data":[{"id":"c1"}],"counts":{"runningContainers":2,"stoppedContainers":1,"totalContainers":3},"pagination":{"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}},
			"images":{"data":[],"pagination":{"totalPages":1,"totalItems":0,"currentPage":1,"itemsPerPage":20}},
			"imageUsageCounts":{"imagesInuse":4,"imagesUnused":1,"totalImages":5,"totalImageSize":0},
			"actionItems":{"items":[]},
			"settings":{}
		}}`))
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &DashboardHandler{
		dashboardService:   services.NewDashboardService(db, nil, nil, nil, nil, nil, nil, nil, nil),
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	var remoteSnapshot bool
	runDashboardStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event dashboardtypes.StreamEvent) bool {
		if event.Type == "snapshot" && event.EnvironmentID == "remote-1" && event.Snapshot != nil {
			require.Equal(t, 2, event.Snapshot.Containers.Counts.RunningContainers)
			require.Equal(t, 3, event.Snapshot.Containers.Counts.TotalContainers)
			require.Equal(t, 5, event.Snapshot.ImageUsageCounts.Total)
			require.Empty(t, event.Snapshot.Containers.Data, "first-page table rows must be trimmed from stream events")
			remoteSnapshot = true
		}
		return remoteSnapshot
	})

	require.True(t, remoteSnapshot)
}

func TestDashboardHandlerStreamAllLegacyAgentComposesSnapshotFromGranularEndpointsInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	token := "remote-token"
	// Older agents have no /dashboard route but do expose the granular
	// counts/version endpoints the fallback composes a snapshot from.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/environments/0/containers/counts":
			_, _ = w.Write([]byte(`{"success":true,"data":{"runningContainers":4,"stoppedContainers":2,"totalContainers":6}}`))
		case "/api/environments/0/images/counts":
			_, _ = w.Write([]byte(`{"success":true,"data":{"imagesInuse":3,"imagesUnused":1,"totalImages":4,"totalImageSize":1024}}`))
		case "/api/app-version":
			_, _ = w.Write([]byte(`{"currentVersion":"1.9.0","displayVersion":"1.9.0","updateAvailable":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"API endpoint not found: ` + r.URL.Path + `"}`))
		}
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &DashboardHandler{
		dashboardService:   services.NewDashboardService(db, nil, nil, nil, nil, nil, nil, nil, nil),
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	var composedSnapshot bool
	runDashboardStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event dashboardtypes.StreamEvent) bool {
		if event.Type == "snapshot" && event.EnvironmentID == "remote-1" && event.Snapshot != nil {
			require.Equal(t, 4, event.Snapshot.Containers.Counts.RunningContainers)
			require.Equal(t, 2, event.Snapshot.Containers.Counts.StoppedContainers)
			require.Equal(t, 4, event.Snapshot.ImageUsageCounts.Total)
			require.Len(t, event.Snapshot.ActionItems.Items, 1)
			require.Equal(t, dashboardtypes.ActionItemKindStoppedContainers, event.Snapshot.ActionItems.Items[0].Kind)
			require.NotNil(t, event.Snapshot.VersionInfo)
			require.Equal(t, "1.9.0", event.Snapshot.VersionInfo.CurrentVersion)
			composedSnapshot = true
		}
		return composedSnapshot
	})

	require.True(t, composedSnapshot)
}

func TestDashboardHandlerStreamAllLegacyAgent404EmitsIncompatibleErrorInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	token := "remote-token"
	// An agent so incompatible that even the granular fallback endpoints 404.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":"API endpoint not found: ` + r.URL.Path + `"}`))
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, token)

	handler := &DashboardHandler{
		dashboardService:   services.NewDashboardService(db, nil, nil, nil, nil, nil, nil, nil, nil),
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil),
	}

	var incompatibleError, localEvent bool
	runDashboardStreamAllInternal(t, ctx, cancel, handler, authz.SudoPermissionSet(), func(event dashboardtypes.StreamEvent) bool {
		if event.Type == "error" && event.EnvironmentID == "remote-1" {
			require.Equal(t, dashboardtypes.StreamErrorCodeAgentIncompatible, event.ErrorCode)
			require.NotEmpty(t, event.Error)
			incompatibleError = true
		}
		// The local producer still emits (an error here — no docker in tests),
		// proving one failing environment doesn't end the stream.
		if event.EnvironmentID == "0" {
			localEvent = true
		}
		return incompatibleError && localEvent
	})

	require.True(t, incompatibleError)
	require.True(t, localEvent)
}

func TestDashboardHandlerStreamAllFiltersUnauthorizedEnvironmentsInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := setupActivityHandlerTestDBInternal(t)
	limitStreamTestDBToSingleConnInternal(t, db)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	allowedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"containers":{"counts":{"runningContainers":1,"totalContainers":1}},"images":{},"imageUsageCounts":{"totalImages":1},"actionItems":{"items":[]},"settings":{}}}`))
	}))
	defer allowedServer.Close()

	var deniedRequests atomic.Int64
	deniedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deniedRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer deniedServer.Close()

	createStreamTestRemoteEnvironmentInternal(t, db, "remote-allowed", "Allowed", allowedServer.URL, "allowed-token")
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-denied", "Denied", deniedServer.URL, "denied-token")

	handler := &DashboardHandler{
		dashboardService:   services.NewDashboardService(db, nil, nil, nil, nil, nil, nil, nil, nil),
		environmentService: services.NewEnvironmentService(db, allowedServer.Client(), nil, nil, settingsService, nil),
	}
	ps := authz.NewPermissionSet()
	ps.AddEnv("remote-allowed", authz.PermDashboardRead)

	seenEnvironments := make(map[string]struct{})
	runDashboardStreamAllInternal(t, ctx, cancel, handler, ps, func(event dashboardtypes.StreamEvent) bool {
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

func TestDashboardHandlerRemoteFetchReusesLoadedEnvironmentInternal(t *testing.T) {
	ctx := context.Background()
	db := setupActivityHandlerTestDBInternal(t)
	settingsService, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"containers":{},"images":{},"imageUsageCounts":{},"actionItems":{"items":[]},"settings":{}}}`))
	}))
	defer server.Close()
	createStreamTestRemoteEnvironmentInternal(t, db, "remote-1", "Remote", server.URL, "remote-token")

	environmentService := services.NewEnvironmentService(db, server.Client(), nil, nil, settingsService, nil)
	environments, err := environmentService.ListRemoteEnvironments(ctx)
	require.NoError(t, err)
	require.Len(t, environments, 1)

	var environmentQueryCount atomic.Int64
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("arcane_test_count_dashboard_environment_queries", func(tx *gorm.DB) {
		if activityTestQueryLoadsEnvironmentIDInternal(tx, "remote-1") {
			environmentQueryCount.Add(1)
		}
	}))

	handler := &DashboardHandler{environmentService: environmentService}
	for range 2 {
		_, err := handler.fetchRemoteDashboardSnapshotInternal(ctx, environments[0], false)
		require.NoError(t, err)
	}
	require.Zero(t, environmentQueryCount.Load())
}
