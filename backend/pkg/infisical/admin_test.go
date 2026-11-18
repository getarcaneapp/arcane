package infisical

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adminFakeInternal is a minimal in-memory Infisical for the write calls.
type adminFakeInternal struct {
	folders map[string][]string // parent path -> child names
	bodies  map[string][]map[string]any
	mu      sync.Mutex
}

func newAdminServerInternal(t *testing.T, register func(mux *http.ServeMux, fake *adminFakeInternal)) *Client {
	t.Helper()
	fake := &adminFakeInternal{folders: map[string][]string{}, bodies: map[string][]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/universal-auth/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"tok","expiresIn":3600}`))
	})
	register(mux, fake)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), Config{SiteURL: server.URL, ClientID: "setup", ClientSecret: "secret"})
	require.NoError(t, err)
	return client
}

func (f *adminFakeInternal) record(t *testing.T, key string, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies[key] = append(f.bodies[key], body)
	return body
}

func TestCreateProjectReturnsEnvironments(t *testing.T) {
	var fake *adminFakeInternal
	client := newAdminServerInternal(t, func(mux *http.ServeMux, f *adminFakeInternal) {
		fake = f
		mux.HandleFunc("POST /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
			f.record(t, "project", r)
			_, _ = w.Write([]byte(`{"project":{"id":"p1","name":"immich","slug":"immich-x1","environments":[{"id":"e1","name":"Development","slug":"dev"},{"id":"e3","name":"Production","slug":"prod"}]}}`))
		})
	})

	project, err := client.CreateProject(t.Context(), " immich ")
	require.NoError(t, err)
	assert.Equal(t, "p1", project.ID)
	require.Len(t, project.Environments, 2)
	assert.Equal(t, "prod", project.Environments[1].Slug)
	assert.Equal(t, "immich", fake.bodies["project"][0]["projectName"])
	assert.Equal(t, "secret-manager", fake.bodies["project"][0]["type"])

	_, err = client.CreateProject(t.Context(), "  ")
	require.Error(t, err)
}

func TestEnsureFolderPathCreatesOnlyMissingSegments(t *testing.T) {
	var fake *adminFakeInternal
	client := newAdminServerInternal(t, func(mux *http.ServeMux, f *adminFakeInternal) {
		fake = f
		f.folders["/"] = []string{"apps"}
		mux.HandleFunc("GET /api/v2/folders", func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			names := f.folders[r.URL.Query().Get("path")]
			f.mu.Unlock()
			items := make([]map[string]string, 0, len(names))
			for _, name := range names {
				items = append(items, map[string]string{"name": name})
			}
			encoded, _ := json.Marshal(map[string]any{"folders": items})
			_, _ = w.Write(encoded)
		})
		mux.HandleFunc("POST /api/v2/folders", func(w http.ResponseWriter, r *http.Request) {
			body := f.record(t, "folder", r)
			f.mu.Lock()
			parent := body["path"].(string)
			f.folders[parent] = append(f.folders[parent], body["name"].(string))
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"folder":{"id":"f"}}`))
		})
	})

	created, err := client.EnsureFolderPath(t.Context(), "p1", "prod", "apps/immich/db/")
	require.NoError(t, err)
	assert.Equal(t, []string{"/apps/immich", "/apps/immich/db"}, created)
	require.Len(t, fake.bodies["folder"], 2)
	assert.Equal(t, "/apps", fake.bodies["folder"][0]["path"])

	created, err = client.EnsureFolderPath(t.Context(), "p1", "prod", "/apps/immich/db")
	require.NoError(t, err)
	assert.Empty(t, created)

	created, err = client.EnsureFolderPath(t.Context(), "p1", "prod", "/")
	require.NoError(t, err)
	assert.Empty(t, created)
}

func TestSecretWritesAndApproval(t *testing.T) {
	var fake *adminFakeInternal
	client := newAdminServerInternal(t, func(mux *http.ServeMux, f *adminFakeInternal) {
		fake = f
		mux.HandleFunc("POST /api/v4/secrets/batch", func(w http.ResponseWriter, r *http.Request) {
			f.record(t, "create", r)
			_, _ = w.Write([]byte(`{"secrets":[{"secretKey":"A"}]}`))
		})
		mux.HandleFunc("PATCH /api/v4/secrets/batch", func(w http.ResponseWriter, r *http.Request) {
			f.record(t, "update", r)
			_, _ = w.Write([]byte(`{"approval":{"id":"ap1","policyId":"pol"}}`))
		})
	})

	outcome, err := client.CreateSecrets(t.Context(), "p1", "prod", "apps", []SecretInput{{Key: "A", Value: "1", Comment: "from Arcane"}})
	require.NoError(t, err)
	assert.False(t, outcome.PendingApproval)
	body := fake.bodies["create"][0]
	assert.Equal(t, "/apps", body["secretPath"])
	secrets := body["secrets"].([]any)
	assert.Equal(t, map[string]any{"secretKey": "A", "secretValue": "1", "secretComment": "from Arcane"}, secrets[0])

	outcome, err = client.UpdateSecrets(t.Context(), "p1", "prod", "/", []SecretInput{{Key: "B", Value: "2"}})
	require.NoError(t, err)
	assert.True(t, outcome.PendingApproval)
	assert.Equal(t, "failOnNotFound", fake.bodies["update"][0]["mode"])

	outcome, err = client.CreateSecrets(t.Context(), "p1", "prod", "/", nil)
	require.NoError(t, err)
	assert.False(t, outcome.PendingApproval)
	assert.Len(t, fake.bodies["create"], 1, "an empty batch makes no request")
}

func TestFindIdentityByClientIDAndGrant(t *testing.T) {
	var fake *adminFakeInternal
	client := newAdminServerInternal(t, func(mux *http.ServeMux, f *adminFakeInternal) {
		fake = f
		mux.HandleFunc("GET /api/v1/identities/details", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"identityDetails":{"organization":{"id":"org1","name":"Home","slug":"home"}}}`))
		})
		mux.HandleFunc("GET /api/v1/identities", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "org1", r.URL.Query().Get("orgId"))
			_, _ = w.Write([]byte(`{"identities":[
				{"identityId":"i-oidc","identity":{"id":"i-oidc","name":"forgejo-ci"}},
				{"identityId":"i-setup","identity":{"id":"i-setup","name":"arcane-setup"}},
				{"identityId":"i-deploy","identity":{"id":"i-deploy","name":"arcane-dev"}}
			],"totalCount":3}`))
		})
		mux.HandleFunc("GET /api/v1/auth/universal-auth/identities/{id}", func(w http.ResponseWriter, r *http.Request) {
			switch r.PathValue("id") {
			case "i-oidc":
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not configured"}`))
			case "i-setup":
				_, _ = w.Write([]byte(`{"identityUniversalAuth":{"clientId":"setup-client"}}`))
			default:
				_, _ = w.Write([]byte(`{"identityUniversalAuth":{"clientId":"deploy-client"}}`))
			}
		})
		mux.HandleFunc("POST /api/v1/projects/{project}/memberships/identities/{identity}", func(w http.ResponseWriter, r *http.Request) {
			f.record(t, "grant", r)
			if r.PathValue("identity") == "i-member" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"Identity already exists in project"}`))
				return
			}
			_, _ = w.Write([]byte(`{"identityMembership":{"id":"m1"}}`))
		})
	})

	orgID, err := client.OrganizationID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "org1", orgID)

	identity, err := client.FindIdentityByClientID(t.Context(), orgID, " deploy-client ")
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, Identity{ID: "i-deploy", Name: "arcane-dev"}, *identity)

	identity, err = client.FindIdentityByClientID(t.Context(), orgID, "nobody")
	require.NoError(t, err)
	assert.Nil(t, identity)

	already, err := client.AddProjectIdentity(t.Context(), "p1", "i-deploy", "viewer")
	require.NoError(t, err)
	assert.False(t, already)
	roles := fake.bodies["grant"][0]["roles"].([]any)
	assert.Equal(t, "viewer", roles[0].(map[string]any)["role"])

	already, err = client.AddProjectIdentity(t.Context(), "p1", "i-member", "viewer")
	require.NoError(t, err)
	assert.True(t, already)
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, IsNotFound(&APIError{StatusCode: http.StatusNotFound}))
	assert.False(t, IsNotFound(&APIError{StatusCode: http.StatusForbidden}))
	assert.False(t, IsNotFound(nil))
}
