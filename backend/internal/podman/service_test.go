package podman

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/docker/compat"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	libpodman "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

type fakeSystemd struct {
	availability types.PodmanSystemdAvailability
	statuses     []types.PodmanUnitStatus
	calls        []string
}

func (f *fakeSystemd) Availability(context.Context) types.PodmanSystemdAvailability {
	return f.availability
}

func (f *fakeSystemd) Start(_ context.Context, unit string) error {
	f.calls = append(f.calls, "start "+unit)
	return nil
}

func (f *fakeSystemd) Stop(_ context.Context, unit string) error {
	f.calls = append(f.calls, "stop "+unit)
	return nil
}

func (f *fakeSystemd) Restart(_ context.Context, unit string) error {
	f.calls = append(f.calls, "restart "+unit)
	return nil
}

func (f *fakeSystemd) UnitStatus(_ context.Context, unit string) (types.PodmanUnitStatus, error) {
	status := f.statuses[0]
	if len(f.statuses) > 1 {
		f.statuses = f.statuses[1:]
	}
	return types.PodmanUnitStatus{
		Name:        unit,
		LoadState:   status.LoadState,
		ActiveState: status.ActiveState,
		SubState:    status.SubState,
	}, nil
}

type testHost struct {
	engine   string
	version  string
	rootless bool
	systemd  *fakeSystemd
	routes   map[string]http.HandlerFunc
	notified [][]notifications.ContainerUpdateBatchEntry
}

func newTestService(t *testing.T, host *testHost) *Service {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch path := strings.TrimPrefix(r.URL.Path, "/v4.0.0/libpod"); path {
		case "/version":
			_, _ = w.Write([]byte(`{"Version":"` + host.version + `","ApiVersion":"` + host.version + `"}`))
		case "/info":
			_, _ = w.Write([]byte(`{"host":{"security":{"rootless":` + strconv.FormatBool(host.rootless) + `}}}`))
		default:
			route, ok := host.routes[path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			route(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return NewService(Config{
		Engine: func(context.Context) (compat.EngineCompatibilityInfo, error) {
			return compat.EngineCompatibilityInfo{Name: host.engine}, nil
		},
		Host: server.URL,
		NewSystemd: func(rootless bool) libpodman.Systemd {
			assert.Equal(t, host.rootless, rootless)
			return host.systemd
		},
		NotifyUpdates: func(_ context.Context, entries []notifications.ContainerUpdateBatchEntry) error {
			host.notified = append(host.notified, entries)
			return nil
		},
	})
}

func TestServiceStatusOnDocker(t *testing.T) {
	svc := newTestService(t, &testHost{engine: "docker"})

	assert.Equal(t, types.PodmanStatus{AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable}, svc.Status(t.Context()))

	_, err := svc.Info(t.Context())
	require.ErrorIs(t, err, common.ErrPodmanNotDetected)
	_, err = svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
	require.ErrorIs(t, err, common.ErrPodmanNotDetected)
	_, err = svc.pods.List(t.Context())
	require.ErrorIs(t, err, common.ErrPodmanNotDetected)
	assert.True(t, svc.PodCount(t.Context()).IsAbsent())
}

func TestServiceStatusEngineErrorReportsNotDetected(t *testing.T) {
	svc := NewService(Config{
		Engine: func(context.Context) (compat.EngineCompatibilityInfo, error) {
			return compat.EngineCompatibilityInfo{}, errors.New("daemon unreachable")
		},
	})

	assert.False(t, svc.Status(t.Context()).Detected)
	_, err := svc.Info(t.Context())
	require.ErrorContains(t, err, "daemon unreachable")
}

func TestServiceStatusOnPodman(t *testing.T) {
	tests := []struct {
		name    string
		version string
		systemd types.PodmanSystemdAvailability
		want    types.PodmanStatus
	}{
		{
			name:    "podman 6.1 with user bus",
			version: "6.1.0",
			systemd: types.PodmanSystemdAvailability{Available: true, Scope: types.PodmanSystemdScopeUser},
			want: types.PodmanStatus{
				Detected:             true,
				Version:              "6.1.0",
				APIVersion:           "6.1.0",
				Rootless:             true,
				QuadletsSupported:    true,
				QuadletAppsSupported: true,
				AutoUpdateMode:       types.PodmanAutoUpdateModeSystemd,
				SystemdAvailable:     true,
				SystemdScope:         types.PodmanSystemdScopeUser,
			},
		},
		{
			name:    "podman 5.4 without bus",
			version: "5.4.2",
			systemd: types.PodmanSystemdAvailability{Scope: types.PodmanSystemdScopeUser, Reason: "DBUS_SESSION_BUS_ADDRESS is not set"},
			want: types.PodmanStatus{
				Detected:       true,
				Version:        "5.4.2",
				APIVersion:     "5.4.2",
				Rootless:       true,
				AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable,
				SystemdScope:   types.PodmanSystemdScopeUser,
				SystemdReason:  "DBUS_SESSION_BUS_ADDRESS is not set",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, &testHost{
				engine:   "podman",
				version:  tt.version,
				rootless: true,
				systemd:  &fakeSystemd{availability: tt.systemd},
			})
			assert.Equal(t, tt.want, svc.Status(t.Context()))
		})
	}
}

func TestServiceQuadletsRequirePodman58(t *testing.T) {
	svc := newTestService(t, &testHost{
		engine:  "podman",
		version: "5.7.1",
		systemd: &fakeSystemd{},
	})

	_, err := svc.quadlets.List(t.Context(), nil)
	require.ErrorIs(t, err, common.ErrBadRequest)
	require.ErrorContains(t, err, "5.8")
}

func TestServiceAutoUpdateSystemdMode(t *testing.T) {
	systemd := &fakeSystemd{
		availability: types.PodmanSystemdAvailability{Available: true, Scope: types.PodmanSystemdScopeSystem},
		statuses: []types.PodmanUnitStatus{
			{
				LoadState:   "loaded",
				ActiveState: "inactive",
				SubState:    "dead",
			},
		},
	}
	host := &testHost{
		engine:  "podman",
		version: "6.1.0",
		systemd: systemd,
	}
	svc := newTestService(t, host)

	_, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{DryRun: true})
	require.ErrorIs(t, err, common.ErrBadRequest)
	assert.Empty(t, systemd.calls)

	result, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"start podman-auto-update.service"}, systemd.calls)
	assert.Equal(t, types.PodmanAutoUpdateModeSystemd, result.Mode)
	assert.Empty(t, result.Errors)
	require.NotNil(t, result.Unit)
	assert.Equal(t, "inactive", result.Unit.ActiveState)
	assert.Empty(t, host.notified)
}

func TestServiceAutoUpdateSystemdFailureReported(t *testing.T) {
	systemd := &fakeSystemd{
		availability: types.PodmanSystemdAvailability{Available: true},
		statuses: []types.PodmanUnitStatus{
			{ActiveState: "failed", SubState: "failed"},
		},
	}
	svc := newTestService(t, &testHost{
		engine:  "podman",
		version: "5.8.0",
		systemd: systemd,
	})

	result, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"podman-auto-update.service failed: failed"}, result.Errors)
}

func TestServiceAutoUpdateRESTModeNotifiesUpdatedContainers(t *testing.T) {
	var gotQuery string
	host := &testHost{
		engine:  "podman",
		version: "6.2.0",
		systemd: &fakeSystemd{},
		routes: map[string]http.HandlerFunc{
			"/autoupdate": func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				_, _ = w.Write([]byte(`{"Reports":[` +
					`{"ContainerName":"web","ImageName":"docker.io/library/nginx:latest","Updated":"true"},` +
					`{"ContainerName":"db","ImageName":"docker.io/library/postgres:16","Updated":"false"}` +
					`],"Errors":[]}`))
			},
		},
	}
	svc := newTestService(t, host)

	result, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dryRun=false", gotQuery)
	assert.Equal(t, types.PodmanAutoUpdateModeREST, result.Mode)
	require.Len(t, result.Reports, 2)
	assert.Equal(t, [][]notifications.ContainerUpdateBatchEntry{{
		{ContainerName: "web", ImageRef: "docker.io/library/nginx:latest"},
	}}, host.notified)

	_, err = svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, "dryRun=true", gotQuery)
	assert.Len(t, host.notified, 1)
}

func TestServiceAutoUpdateUnavailable(t *testing.T) {
	svc := newTestService(t, &testHost{
		engine:  "podman",
		version: "5.8.0",
		systemd: &fakeSystemd{availability: types.PodmanSystemdAvailability{Reason: "bus not mounted"}},
	})

	_, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
	require.ErrorIs(t, err, common.ErrConflict)
	require.ErrorContains(t, err, "bus not mounted")
}

func TestServiceContainerUnitAction(t *testing.T) {
	systemd := &fakeSystemd{availability: types.PodmanSystemdAvailability{Available: true}}
	svc := newTestService(t, &testHost{
		engine:  "podman",
		version: "6.1.0",
		systemd: systemd,
		routes: map[string]http.HandlerFunc{
			"/containers/json": func(w http.ResponseWriter, r *http.Request) {
				assert.JSONEq(t, `{"label":["PODMAN_SYSTEMD_UNIT"]}`, r.URL.Query().Get("filters"))
				_, _ = w.Write([]byte(`[
					{"Id":"abc123def456","Names":["web"],"Labels":{"PODMAN_SYSTEMD_UNIT":"web.service"}},
					{"Id":"fed654cba321","Names":["spoof"],"Labels":{"PODMAN_SYSTEMD_UNIT":"nginx.service"}}
				]`))
			},
			"/quadlets/json": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`[{"Name":"web.container","UnitName":"web.service"}]`))
			},
		},
	})

	handled, err := svc.ContainerUnitAction(t.Context(), "abc123", libpodman.Systemd.Restart)
	require.NoError(t, err)
	assert.True(t, handled)

	handled, err = svc.ContainerUnitAction(t.Context(), "/web", libpodman.Systemd.Stop)
	require.NoError(t, err)
	assert.True(t, handled)

	handled, err = svc.ContainerUnitAction(t.Context(), "other", libpodman.Systemd.Start)
	require.NoError(t, err)
	assert.False(t, handled)

	handled, err = svc.ContainerUnitAction(t.Context(), "spoof", libpodman.Systemd.Stop)
	require.NoError(t, err)
	assert.False(t, handled)

	assert.Equal(t, []string{"restart web.service", "stop web.service"}, systemd.calls)
}

func TestServiceContainerUnitActionFallsBackWithoutQuadletSupport(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		available bool
	}{
		{name: "podman before 5.8", version: "5.4.0", available: true},
		{name: "systemd unreachable", version: "6.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			systemd := &fakeSystemd{availability: types.PodmanSystemdAvailability{Available: tt.available}}
			svc := newTestService(t, &testHost{
				engine:  "podman",
				version: tt.version,
				systemd: systemd,
				routes: map[string]http.HandlerFunc{
					"/containers/json": func(w http.ResponseWriter, _ *http.Request) {
						_, _ = w.Write([]byte(`[{"Id":"abc123def456","Names":["web"],"Labels":{"PODMAN_SYSTEMD_UNIT":"web.service"}}]`))
					},
				},
			})

			handled, err := svc.ContainerUnitAction(t.Context(), "web", libpodman.Systemd.Stop)
			require.NoError(t, err)
			assert.False(t, handled)
			assert.Empty(t, systemd.calls)
		})
	}
}

func TestServiceGuardQuadletManaged(t *testing.T) {
	tests := []struct {
		name    string
		engine  string
		version string
		unit    string
		wantErr bool
	}{
		{name: "unlabelled", engine: "podman", version: "6.1.0"},
		{name: "installed quadlet", engine: "podman", version: "6.1.0", unit: "web.service", wantErr: true},
		{name: "stale label", engine: "podman", version: "6.1.0", unit: "gone.service"},
		{name: "unverifiable before 5.8", engine: "podman", version: "5.4.0", unit: "gone.service", wantErr: true},
		{name: "docker host", engine: "docker", unit: "web.service"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, &testHost{
				engine:  tt.engine,
				version: tt.version,
				systemd: &fakeSystemd{},
				routes: map[string]http.HandlerFunc{
					"/quadlets/json": func(w http.ResponseWriter, _ *http.Request) {
						_, _ = w.Write([]byte(`[{"Name":"web.container","UnitName":"web.service"}]`))
					},
				},
			})

			err := svc.GuardQuadletManaged(t.Context(), map[string]string{types.PodmanSystemdUnitLabel: tt.unit})
			if tt.wantErr {
				require.ErrorIs(t, err, common.ErrContainerQuadletManaged)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestServiceContainerUnitActionOnDocker(t *testing.T) {
	svc := newTestService(t, &testHost{engine: "docker"})

	handled, err := svc.ContainerUnitAction(t.Context(), "abc123", libpodman.Systemd.Start)
	require.NoError(t, err)
	assert.False(t, handled)
}
