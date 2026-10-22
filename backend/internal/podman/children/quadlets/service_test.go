package quadlets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
)

type fakeSystemd struct {
	calls []string
}

func (f *fakeSystemd) Availability(context.Context) types.PodmanSystemdAvailability {
	return types.PodmanSystemdAvailability{Available: true}
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
	return types.PodmanUnitStatus{
		Name:        unit,
		LoadState:   "loaded",
		ActiveState: "active",
		SubState:    "running",
	}, nil
}

func newTestService(t *testing.T, appsSupported bool, handler http.HandlerFunc) (*Service, *fakeSystemd) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := podman.NewClient(server.URL)
	require.NoError(t, err)
	systemd := &fakeSystemd{}
	return NewService(func(context.Context) (Runtime, error) {
		return Runtime{
			Client:        client,
			Systemd:       systemd,
			AppsSupported: appsSupported,
		}, nil
	}), systemd
}

func listHandler(body string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/quadlets/json" {
			_, _ = w.Write([]byte(body))
			return
		}
		if next == nil {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func TestDescribeDerivesUnitsAndTemplates(t *testing.T) {
	tests := []struct {
		name  string
		entry types.Quadlet
		want  types.QuadletDetail
	}{
		{
			name:  "container",
			entry: types.Quadlet{Name: "web.container", Status: "active"},
			want: types.QuadletDetail{
				Name:     "web.container",
				UnitName: "web.service",
				Status:   "active",
				Type:     types.QuadletTypeContainer,
			},
		},
		{
			name:  "kube",
			entry: types.Quadlet{Name: "shop.kube", Status: "inactive"},
			want: types.QuadletDetail{
				Name:     "shop.kube",
				UnitName: "shop.service",
				Status:   "inactive",
				Type:     types.QuadletTypeKube,
			},
		},
		{
			name:  "pod",
			entry: types.Quadlet{Name: "stack.pod", Status: "active"},
			want: types.QuadletDetail{
				Name:     "stack.pod",
				UnitName: "stack-pod.service",
				Status:   "active",
				Type:     types.QuadletTypePod,
			},
		},
		{
			name:  "network",
			entry: types.Quadlet{Name: "backend.network", Status: "Not loaded"},
			want: types.QuadletDetail{
				Name:     "backend.network",
				UnitName: "backend-network.service",
				Status:   "Not loaded",
				Type:     types.QuadletTypeNetwork,
			},
		},
		{
			name: "reported unit wins",
			entry: types.Quadlet{
				Name:     "data.volume",
				UnitName: "custom.service",
				Status:   "active",
			},
			want: types.QuadletDetail{
				Name:     "data.volume",
				UnitName: "custom.service",
				Status:   "active",
				Type:     types.QuadletTypeVolume,
			},
		},
		{
			name:  "template file name",
			entry: types.Quadlet{Name: "worker@.container", Status: "inactive"},
			want: types.QuadletDetail{
				Name:     "worker@.container",
				UnitName: "worker@.service",
				Status:   "inactive",
				Type:     types.QuadletTypeContainer,
				Template: true,
			},
		},
		{
			name:  "template status",
			entry: types.Quadlet{Name: "job.container", Status: "loaded template"},
			want: types.QuadletDetail{
				Name:     "job.container",
				UnitName: "job.service",
				Status:   "loaded template",
				Type:     types.QuadletTypeContainer,
				Template: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, describe(tt.entry))
		})
	}
}

func TestLifecycleRunsDerivedUnitAndReadsStatus(t *testing.T) {
	svc, systemd := newTestService(t, true, listHandler(`[{"Name":"web.container","Status":"Not loaded"}]`, nil))

	status, err := svc.Lifecycle(t.Context(), "web.container", podman.Systemd.Start)
	require.NoError(t, err)
	assert.Equal(t, []string{"start web.service"}, systemd.calls)
	assert.Equal(t, "web.service", status.Name)
	assert.Equal(t, "active", status.ActiveState)
}

func TestLifecycleRejectsTemplates(t *testing.T) {
	svc, systemd := newTestService(t, true, listHandler(`[{"Name":"worker@.container","Status":"loaded template"}]`, nil))

	_, err := svc.Lifecycle(t.Context(), "worker@.container", podman.Systemd.Restart)
	require.ErrorIs(t, err, common.ErrConflict)
	assert.Empty(t, systemd.calls)
}

func TestLifecycleUnknownQuadlet(t *testing.T) {
	svc, _ := newTestService(t, true, listHandler(`[{"Name":"web.container.bak"}]`, nil))

	_, err := svc.Lifecycle(t.Context(), "web.container", podman.Systemd.Stop)
	require.ErrorIs(t, err, common.ErrNotFound)
}

func TestUpdateReplacesAndKeepsApplication(t *testing.T) {
	tests := []struct {
		name          string
		listBody      string
		appsSupported bool
		wantQuery     string
	}{
		{
			name:          "app member keeps its application",
			listBody:      `[{"Name":"web.container","App":"shop"}]`,
			appsSupported: true,
			wantQuery:     "application=shop&replace=true",
		},
		{
			name:          "standalone quadlet",
			listBody:      `[{"Name":"web.container"}]`,
			appsSupported: true,
			wantQuery:     "replace=true",
		},
		{
			name:      "apps unsupported",
			listBody:  `[{"Name":"web.container","App":"shop"}]`,
			wantQuery: "replace=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery string
			svc, _ := newTestService(t, tt.appsSupported, listHandler(tt.listBody, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v4.0.0/libpod/quadlets", r.URL.Path)
				gotQuery = r.URL.RawQuery
				_, _ = w.Write([]byte(`{"InstalledQuadlets":{"web.container":"/etc/containers/systemd/web.container"},"QuadletErrors":{}}`))
			}))

			report, err := svc.Update(t.Context(), "web.container", types.QuadletUpdateRequest{Content: "[Container]\nImage=nginx\n"})
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)
			assert.Contains(t, report.InstalledQuadlets, "web.container")
		})
	}
}

func TestInstallValidatesTypeAndApplication(t *testing.T) {
	tests := []struct {
		name          string
		req           types.QuadletInstallRequest
		appsSupported bool
	}{
		{name: "unsupported extension", req: types.QuadletInstallRequest{Name: "web.service", Content: "[Unit]\n"}, appsSupported: true},
		{name: "missing stem", req: types.QuadletInstallRequest{Name: ".container", Content: "[Container]\n"}, appsSupported: true},
		{name: "application without support", req: types.QuadletInstallRequest{Name: "web.container", Content: "[Container]\n", App: "shop"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t, tt.appsSupported, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			})

			_, err := svc.Install(t.Context(), tt.req)
			require.ErrorIs(t, err, common.ErrBadRequest)
		})
	}
}
