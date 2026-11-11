package podman

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kit.getarcane.app/docker/compat"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/notifications"
	libpodman "getarcane.dev/arcane/v3/pkg/podman"
	"getarcane.dev/types/v3"
)

type (
	fakeSystemd struct {
		startErr     error
		availability types.PodmanSystemdAvailability
		statuses     []types.PodmanUnitStatus
		calls        []string
	}

	testHost struct {
		systemd  *fakeSystemd
		routes   map[string]http.HandlerFunc
		engine   string
		version  string
		notified [][]notifications.ContainerUpdateBatchEntry
		rootless bool
	}
)

func (f *fakeSystemd) Availability(context.Context) types.PodmanSystemdAvailability {
	return f.availability
}

func (f *fakeSystemd) Start(_ context.Context, unit string) error {
	f.calls = append(f.calls, "start "+unit)
	return f.startErr
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

func newTestServer(t *testing.T, host *testHost) *httptest.Server {
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
	return server
}

func newTestService(t *testing.T, host *testHost) *Service {
	t.Helper()

	server := newTestServer(t, host)
	return NewService(Config{
		Engine: func(context.Context) (compat.EngineCompatibilityInfo, error) {
			return compat.EngineCompatibilityInfo{Name: host.engine}, nil
		},
		Host: func() string {
			return server.URL
		},
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
		Host: func() string {
			return "unix:///run/podman/podman.sock"
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

	var host *testHost
	var hostURL string
	svc := NewService(Config{
		Engine: func(context.Context) (compat.EngineCompatibilityInfo, error) {
			return compat.EngineCompatibilityInfo{Name: "podman"}, nil
		},
		Host: func() string {
			return hostURL
		},
		NewSystemd: func(bool) libpodman.Systemd {
			return host.systemd
		},
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host = &testHost{
				engine:   "podman",
				version:  tt.version,
				rootless: true,
				systemd:  &fakeSystemd{availability: tt.systemd},
			}
			hostURL = newTestServer(t, host).URL
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
	tests := []struct {
		startErr error
		wantErr  error
		name     string
	}{
		{
			name:     "failed job reported as result",
			startErr: common.Classify(common.ErrConflict, errors.New("start systemd unit podman-auto-update.service: job failed")),
		},
		{
			name:     "bus error propagates",
			startErr: common.Classify(common.ErrUnavailable, errors.New("connect to systemd bus")),
			wantErr:  common.ErrUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, &testHost{
				engine:  "podman",
				version: "5.8.0",
				systemd: &fakeSystemd{
					startErr:     tt.startErr,
					availability: types.PodmanSystemdAvailability{Available: true},
					statuses: []types.PodmanUnitStatus{
						{ActiveState: "failed", SubState: "failed"},
					},
				},
			})

			result, err := svc.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, types.PodmanAutoUpdateModeSystemd, result.Mode)
			assert.Equal(t, []string{"podman-auto-update.service failed: failed"}, result.Errors)
			require.NotNil(t, result.Unit)
			assert.Equal(t, "failed", result.Unit.ActiveState)
		})
	}
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
	tests := []struct {
		name    string
		ref     string
		want    []string
		handled bool
	}{
		{name: "unique id prefix", ref: "abc123", want: []string{"stop web.service"}, handled: true},
		{name: "exact id", ref: "abc123def456", want: []string{"stop web.service"}, handled: true},
		{name: "name with slash", ref: "/web", want: []string{"stop web.service"}, handled: true},
		{name: "unlabelled name beats managed id prefix", ref: "abc"},
		{name: "prefix shared with unlabelled container", ref: "cd"},
		{name: "template instance", ref: "cd1", want: []string{"stop worker@1.service"}, handled: true},
		{name: "instance quadlet", ref: "solo", want: []string{"stop solo@1.service"}, handled: true},
		{name: "unmanaged unit label", ref: "spoof"},
		{name: "no match", ref: "zzz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			systemd := &fakeSystemd{availability: types.PodmanSystemdAvailability{Available: true}}
			svc := newTestService(t, &testHost{
				engine:  "podman",
				version: "6.1.0",
				systemd: systemd,
				routes: map[string]http.HandlerFunc{
					"/containers/json": func(w http.ResponseWriter, _ *http.Request) {
						_, _ = w.Write([]byte(`[
							{"Id":"abc123def456","Names":["web"],"Labels":{"PODMAN_SYSTEMD_UNIT":"web.service"}},
							{"Id":"fed654cba321","Names":["spoof"],"Labels":{"PODMAN_SYSTEMD_UNIT":"nginx.service"}},
							{"Id":"ab7777000222","Names":["abc"]},
							{"Id":"cd1111000333","Names":["worker"],"Labels":{"PODMAN_SYSTEMD_UNIT":"worker@1.service"}},
							{"Id":"cd2222000444","Names":["plain"]},
							{"Id":"ef5555000555","Names":["solo"],"Labels":{"PODMAN_SYSTEMD_UNIT":"solo@1.service"}}
						]`))
					},
					"/quadlets/json": func(w http.ResponseWriter, _ *http.Request) {
						_, _ = w.Write([]byte(`[
							{"Name":"web.container","UnitName":"web.service"},
							{"Name":"worker@.container","UnitName":"worker@.service"},
							{"Name":"solo@1.container","UnitName":"solo@1.service"}
						]`))
					},
				},
			})

			handled, err := svc.ContainerUnitAction(t.Context(), tt.ref, libpodman.Systemd.Stop)
			require.NoError(t, err)
			assert.Equal(t, tt.handled, handled)
			assert.Equal(t, tt.want, systemd.calls)
		})
	}
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
		{name: "template instance", engine: "podman", version: "6.1.0", unit: "worker@1.service", wantErr: true},
		{name: "instance quadlet", engine: "podman", version: "6.1.0", unit: "solo@1.service", wantErr: true},
		{name: "unknown template instance", engine: "podman", version: "6.1.0", unit: "other@1.service"},
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
						_, _ = w.Write([]byte(`[{"Name":"web.container","UnitName":"web.service"},{"Name":"worker@.container","UnitName":"worker@.service"},{"Name":"solo@1.container","UnitName":"solo@1.service"}]`))
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

func podmanScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
	t.Helper()
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok := bodies[strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")[0]]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.MarshalWrite(w, body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func TestAggregateStatusSumsPods(t *testing.T) {
	ctx := podmanScopeContext(t, "/api/agents/a,b,c/podman/status", map[string]any{
		"a": types.ApiResponse[types.PodmanStatus]{
			Success: true,
			Data: types.PodmanStatus{
				Detected:       true,
				AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable,
				PodCount:       2,
			},
		},
		"b": types.ApiResponse[types.PodmanStatus]{
			Success: true,
			Data: types.PodmanStatus{
				AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable,
			},
		},
	})

	body, err := (&Service{}).AggregateStatus(ctx)
	require.NoError(t, err)

	require.True(t, body.Data.Detected)
	require.Equal(t, 2, body.Data.PodCount)
	require.Len(t, body.Environments, 2)
	require.Equal(t, 2, body.Environments[0].Data.PodCount)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}
