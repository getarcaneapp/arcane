package secrets

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/streams/bus"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

func TestService_UpdateSecret_ReplacesLabelsOnly(t *testing.T) {
	current := swarm.Secret{
		ID:      "secret-1",
		Version: swarm.Version{Index: 4},
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "db-pass",
				Labels: map[string]string{"old": "label"},
			},
			Driver: &swarm.Driver{Name: "vault"},
		},
	}
	var updatedSpec map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/secrets/secret-1":
			assert.NoError(t, json.MarshalWrite(w, current))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/secrets/secret-1/update":
			assert.Equal(t, "4", r.URL.Query().Get("version"))
			assert.NoError(t, json.UnmarshalRead(r.Body, &updatedSpec))
			current.Version.Index = 5
			current.Spec.Labels = map[string]string{"tier": "db"}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	svc := NewService(newSettingsDockerService(t, cli), func(context.Context) error {
		return nil
	})
	updated, err := svc.UpdateSecret(t.Context(), "secret-1", types.SwarmSecretUpdateRequest{
		Version: 4,
		Labels:  map[string]string{"tier": "db"},
	})
	require.NoError(t, err)

	assert.Equal(t, "db-pass", updatedSpec["Name"])
	assert.Equal(t, map[string]any{"tier": "db"}, updatedSpec["Labels"])
	assert.Equal(t, map[string]any{"Name": "vault"}, updatedSpec["Driver"])
	assert.NotContains(t, updatedSpec, "Data")
	assert.Equal(t, uint64(5), updated.Version.Index)
	assert.Equal(t, map[string]string{"tier": "db"}, updated.Spec.Labels)
}

func secretsScopeContext(t *testing.T, target string, bodies map[string]any) context.Context {
	t.Helper()
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok := bodies[strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]]
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
	ctx := secretsScopeContext(t, "/api/environments/a,b,c/swarm/secrets", map[string]any{
		"a": types.Paginated[types.SwarmSecret]{
			Success: true,
			Data:    []types.SwarmSecret{{ID: "s1"}},
		},
		"b": types.Paginated[types.SwarmSecret]{
			Success: true,
			Data:    []types.SwarmSecret{{ID: "s2"}},
		},
	})

	body, err := (&Service{}).Aggregate(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
	require.NoError(t, err)

	require.Len(t, body.Data, 2)
	require.ElementsMatch(t, []string{"Env a", "Env b"}, []string{body.Data[0].Environment().Name, body.Data[1].Environment().Name})
	require.EqualValues(t, 2, body.Pagination.TotalItems)
	require.Len(t, body.FailedEnvironments, 1)
	require.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}

func newSettingsDockerService(t *testing.T, dockerClient *client.Client) *docker.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(t.Context(), db, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
}
