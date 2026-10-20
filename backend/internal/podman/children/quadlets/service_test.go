package quadlets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

type fakeSystemd struct {
	calls []string
}

func (f *fakeSystemd) Availability(context.Context) podmantypes.SystemdAvailability {
	return podmantypes.SystemdAvailability{Available: true}
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

func (f *fakeSystemd) UnitStatus(_ context.Context, unit string) (podmantypes.UnitStatus, error) {
	return podmantypes.UnitStatus{
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
		entry podmantypes.Quadlet
		want  podmantypes.QuadletDetail
	}{
		{
			name:  "container",
			entry: podmantypes.Quadlet{Name: "web.container", Status: "active"},
			want: podmantypes.QuadletDetail{
				Name:     "web.container",
				UnitName: "web.service",
				Status:   "active",
				Type:     podmantypes.QuadletTypeContainer,
			},
		},
		{
			name:  "kube",
			entry: podmantypes.Quadlet{Name: "shop.kube", Status: "inactive"},
			want: podmantypes.QuadletDetail{
				Name:     "shop.kube",
				UnitName: "shop.service",
				Status:   "inactive",
				Type:     podmantypes.QuadletTypeKube,
			},
		},
		{
			name:  "pod",
			entry: podmantypes.Quadlet{Name: "stack.pod", Status: "active"},
			want: podmantypes.QuadletDetail{
				Name:     "stack.pod",
				UnitName: "stack-pod.service",
				Status:   "active",
				Type:     podmantypes.QuadletTypePod,
			},
		},
		{
			name:  "network",
			entry: podmantypes.Quadlet{Name: "backend.network", Status: "Not loaded"},
			want: podmantypes.QuadletDetail{
				Name:     "backend.network",
				UnitName: "backend-network.service",
				Status:   "Not loaded",
				Type:     podmantypes.QuadletTypeNetwork,
			},
		},
		{
			name: "reported unit wins",
			entry: podmantypes.Quadlet{
				Name:     "data.volume",
				UnitName: "custom.service",
				Status:   "active",
			},
			want: podmantypes.QuadletDetail{
				Name:     "data.volume",
				UnitName: "custom.service",
				Status:   "active",
				Type:     podmantypes.QuadletTypeVolume,
			},
		},
		{
			name:  "template file name",
			entry: podmantypes.Quadlet{Name: "worker@.container", Status: "inactive"},
			want: podmantypes.QuadletDetail{
				Name:     "worker@.container",
				UnitName: "worker@.service",
				Status:   "inactive",
				Type:     podmantypes.QuadletTypeContainer,
				Template: true,
			},
		},
		{
			name:  "template status",
			entry: podmantypes.Quadlet{Name: "job.container", Status: "loaded template"},
			want: podmantypes.QuadletDetail{
				Name:     "job.container",
				UnitName: "job.service",
				Status:   "loaded template",
				Type:     podmantypes.QuadletTypeContainer,
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

			report, err := svc.Update(t.Context(), "web.container", podmantypes.QuadletUpdateRequest{Content: "[Container]\nImage=nginx\n"})
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)
			assert.Contains(t, report.InstalledQuadlets, "web.container")
		})
	}
}

func TestInstallValidatesTypeAndApplication(t *testing.T) {
	tests := []struct {
		name          string
		req           podmantypes.QuadletInstallRequest
		appsSupported bool
	}{
		{name: "unsupported extension", req: podmantypes.QuadletInstallRequest{Name: "web.service", Content: "[Unit]\n"}, appsSupported: true},
		{name: "missing stem", req: podmantypes.QuadletInstallRequest{Name: ".container", Content: "[Container]\n"}, appsSupported: true},
		{name: "application without support", req: podmantypes.QuadletInstallRequest{Name: "web.container", Content: "[Container]\n", App: "shop"}},
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
