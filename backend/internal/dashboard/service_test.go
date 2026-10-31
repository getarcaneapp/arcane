package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/docker/compat"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	libpodman "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
)

func setupDashboardServiceTestSettings(t *testing.T) *settings.SettingsService {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))

	settingsEnv, err := settings.LoadEnv()
	require.NoError(t, err)
	settingsSvc, err := settings.NewSettingsService(t.Context(), &database.DB{DB: db}, &config.Config{}, settingsEnv)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context()))) })

	return settingsSvc
}

func newDashboardTestDockerService(t *testing.T, settingsSvc *settings.SettingsService) *docker.DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"), strings.HasSuffix(r.URL.Path, "/images/json"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/volumes"):
			_, _ = w.Write([]byte(`{"Volumes":[],"Warnings":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerClient.Close()
	})

	return docker.NewDockerClientService(t.Context(), nil, settingsSvc).WithClient(dockerClient)
}

func TestDashboardService_ReportsPodmanPods(t *testing.T) {
	dockerSvc := newDashboardTestDockerService(t, setupDashboardServiceTestSettings(t))

	libpodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v4.0.0/libpod/version":
			_, _ = w.Write([]byte(`{"Version":"6.1.0","ApiVersion":"6.1.0"}`))
		case "/v4.0.0/libpod/info":
			_, _ = w.Write([]byte(`{"host":{"security":{"rootless":false}}}`))
		case "/v4.0.0/libpod/pods/json":
			_, _ = w.Write([]byte(`[{"Id":"pod-a","Name":"web"},{"Id":"pod-b","Name":"db"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(libpodServer.Close)

	svc := NewDashboardService(dockerSvc, nil, nil, podman.NewService(podman.Config{
		Engine: func(context.Context) (compat.EngineCompatibilityInfo, error) {
			return compat.EngineCompatibilityInfo{Name: "podman"}, nil
		},
		Host: libpodServer.URL,
		NewSystemd: func(rootless bool) libpodman.Systemd {
			return libpodman.NewSystemd("", rootless, func(string) string {
				return ""
			})
		},
	}))

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context()))) })
	require.NoError(t, svc.ObserveInventory(provider.Meter("dashboard-test")))

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	var pods []metricdata.DataPoint[int64]
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "arcane.podman.pods" {
				pods = gauge.DataPoints
			}
		}
	}
	require.Len(t, pods, 1)
	require.Equal(t, int64(2), pods[0].Value)
}
