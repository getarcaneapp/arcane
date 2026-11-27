package configs

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	mobySwarm "github.com/moby/moby/api/types/swarm"
	mobyClient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

func TestService_UpdateConfig_KeepsDataAndReplacesLabels(t *testing.T) {
	current := mobySwarm.Config{
		ID:      "config-1",
		Version: mobySwarm.Version{Index: 4},
		Spec: mobySwarm.ConfigSpec{
			Annotations: mobySwarm.Annotations{
				Name:   "nginx-conf",
				Labels: map[string]string{"old": "label"},
			},
			Data: []byte("listen 80;"),
		},
	}
	var updatedSpec map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/configs/config-1":
			assert.NoError(t, json.MarshalWrite(w, current))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/configs/config-1/update":
			assert.Equal(t, "4", r.URL.Query().Get("version"))
			assert.NoError(t, json.UnmarshalRead(r.Body, &updatedSpec))
			current.Version.Index = 5
			current.Spec.Labels = map[string]string{"tier": "db"}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cli, err := mobyClient.New(
		mobyClient.WithHost(server.URL),
		mobyClient.WithAPIVersion("1.41"),
		mobyClient.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	svc := NewService(newSettingsDockerService(t, cli), func(context.Context) error {
		return nil
	})
	updated, err := svc.UpdateConfig(t.Context(), "config-1", types.SwarmConfigUpdateRequest{
		Version: 4,
		Labels:  map[string]string{"tier": "db"},
	})
	require.NoError(t, err)

	assert.Equal(t, "nginx-conf", updatedSpec["Name"])
	assert.Equal(t, map[string]any{"tier": "db"}, updatedSpec["Labels"])
	assert.Equal(t, "bGlzdGVuIDgwOw==", updatedSpec["Data"])
	assert.Equal(t, uint64(5), updated.Version.Index)
	assert.Equal(t, map[string]string{"tier": "db"}, updated.Spec.Labels)
}

func configsScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
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

func TestAggregateMergesEnvironments(t *testing.T) {
	ctx := configsScopeContext(t, "/api/agents/a,b,c/swarm/configs", map[string]any{
		"a": types.Paginated[types.SwarmConfig]{
			Success: true,
			Data:    []types.SwarmConfig{{ID: "c1"}},
		},
		"b": types.Paginated[types.SwarmConfig]{
			Success: true,
			Data:    []types.SwarmConfig{{ID: "c2"}},
		},
	})

	body, err := handlerutil.AggregatePaginated(ctx, configPagination, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
	require.NoError(t, err)

	require.Len(t, body.Data, 2)
	require.ElementsMatch(t, []string{"Env a", "Env b"}, []string{body.Data[0].Environment().Name, body.Data[1].Environment().Name})
	require.EqualValues(t, 2, body.Pagination.TotalItems)
	require.Len(t, body.FailedEnvironments, 1)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}

func newSettingsDockerService(t *testing.T, dockerClient *mobyClient.Client) *dockerInternal.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return dockerInternal.NewDockerClientService(t.Context(), db, settingsService, dockerInternal.ClientServiceOptions{}).WithClient(dockerClient)
}
