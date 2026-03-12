package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
)

func setupAnalyticsStateServicesInternal(t *testing.T) (*database.DB, *services.SettingsService, *services.KVService) {
	t.Helper()
	ctx := context.Background()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))

	wrappedDB := &database.DB{DB: db}
	settingsService, err := services.NewSettingsService(ctx, wrappedDB)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "instanceId", "test-instance"))

	return wrappedDB, settingsService, services.NewKVService(wrappedDB)
}

func newHeartbeatServer(t *testing.T) (*httptest.Server, <-chan []byte, *atomic.Int32) {
	t.Helper()
	bodyCh := make(chan []byte, 10)
	requestCount := &atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read heartbeat body: %v", err)
		}
		bodyCh <- body
		w.WriteHeader(http.StatusOK)
	}))

	return server, bodyCh, requestCount
}

func TestAnalyticsJob_Run_ManagerPayload(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, bodyCh, _ := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{Environment: config.AppEnvironmentProduction}
	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL

	job.Run(ctx)

	var body []byte
	select {
	case body = <-bodyCh:
	default:
		t.Fatal("expected heartbeat request")
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, getAnalyticsVersion(), payload["version"])
	require.Equal(t, "test-instance", payload["instance_id"])
	require.Equal(t, "manager", payload["server_type"])
}

func TestAnalyticsJob_Run_AgentPayload(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, bodyCh, _ := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{AgentMode: true, Environment: config.AppEnvironmentProduction}
	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL

	job.Run(ctx)

	var body []byte
	select {
	case body = <-bodyCh:
	default:
		t.Fatal("expected heartbeat request")
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "agent", payload["server_type"])
}

func TestAnalyticsJob_Run_SkipsWhenDisabled(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, bodyCh, _ := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{AnalyticsDisabled: true, Environment: config.AppEnvironmentProduction}
	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL

	job.Run(ctx)

	select {
	case <-bodyCh:
		t.Fatal("unexpected heartbeat request")
	default:
	}
}

func TestAnalyticsJob_Run_SkipsWhenTestEnv(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, bodyCh, _ := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{Environment: config.AppEnvironmentTest}
	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL

	job.Run(ctx)

	select {
	case <-bodyCh:
		t.Fatal("unexpected heartbeat request")
	default:
	}
}

func TestAnalyticsJob_Run_SkipsWithinHeartbeatWindowAfterRestart(t *testing.T) {
	ctx := context.Background()
	wrappedDB, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, _, requestCount := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{Environment: config.AppEnvironmentProduction}
	firstAttemptAt := time.Date(2026, time.March, 10, 7, 9, 46, 0, time.UTC)

	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL
	job.now = func() time.Time { return firstAttemptAt }
	job.Run(ctx)

	reloadedSettingsService, err := services.NewSettingsService(ctx, wrappedDB)
	require.NoError(t, err)
	restartedJob := NewAnalyticsJob(reloadedSettingsService, services.NewKVService(wrappedDB), server.Client(), cfg)
	restartedJob.heartbeatURL = server.URL
	restartedJob.now = func() time.Time { return firstAttemptAt.Add(19 * time.Minute) }
	restartedJob.Run(ctx)

	require.Equal(t, int32(1), requestCount.Load())
}

func TestAnalyticsJob_Run_AllowsSendAfterHeartbeatWindow(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, _, requestCount := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{Environment: config.AppEnvironmentProduction}
	firstAttemptAt := time.Date(2026, time.March, 10, 7, 9, 46, 0, time.UTC)

	firstJob := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	firstJob.heartbeatURL = server.URL
	firstJob.now = func() time.Time { return firstAttemptAt }
	firstJob.Run(ctx)

	secondJob := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	secondJob.heartbeatURL = server.URL
	secondJob.now = func() time.Time { return firstAttemptAt.Add(25 * time.Hour) }
	secondJob.Run(ctx)

	require.Equal(t, int32(2), requestCount.Load())
}

func TestAnalyticsJob_Run_ConcurrentRunsSendOnce(t *testing.T) {
	ctx := context.Background()
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	server, _, requestCount := newHeartbeatServer(t)
	defer server.Close()

	cfg := &config.Config{Environment: config.AppEnvironmentProduction}
	job := NewAnalyticsJob(settingsService, kvService, server.Client(), cfg)
	job.heartbeatURL = server.URL
	job.now = func() time.Time {
		return time.Date(2026, time.March, 10, 7, 9, 46, 0, time.UTC)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			job.Run(ctx)
		})
	}

	close(start)
	wg.Wait()

	require.Equal(t, int32(1), requestCount.Load())
}

func TestAnalyticsJob_Schedule_UsesFixedHourlyCheck(t *testing.T) {
	_, settingsService, kvService := setupAnalyticsStateServicesInternal(t)
	job := NewAnalyticsJob(settingsService, kvService, nil, &config.Config{Environment: config.AppEnvironmentProduction})

	require.Equal(t, analyticsHeartbeatCheckSchedule, job.Schedule(context.Background()))
}
