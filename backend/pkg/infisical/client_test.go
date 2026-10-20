package infisical

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInfisical struct {
	logins       atomic.Int32
	expiresIn    int
	secrets      string
	lastQuery    atomic.Pointer[string]
	rejectSecret bool
}

func (f *fakeInfisical) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/universal-auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if f.rejectSecret || body["clientSecret"] != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"statusCode":401,"message":"Invalid credentials"}`))
			return
		}
		f.logins.Add(1)
		_, _ = w.Write([]byte(`{"accessToken":"tok","expiresIn":` + strconv.Itoa(f.expiresIn) + `,"accessTokenMaxTTL":7200,"tokenType":"Bearer"}`))
	})
	mux.HandleFunc("GET /api/v4/secrets", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw := r.URL.RawQuery
		f.lastQuery.Store(&raw)
		_, _ = w.Write([]byte(f.secrets))
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"Apps","slug":"apps","environments":[{"id":"e1","name":"Production","slug":"prod"}]}]}`))
	})
	mux.HandleFunc("GET /api/v2/folders", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/billing", r.URL.Query().Get("path"))
		_, _ = w.Write([]byte(`{"folders":[{"id":"f1","name":"db"},{"id":"f2","name":"stripe"}]}`))
	})
	return mux
}

func newTestClientInternal(t *testing.T, fake *fakeInfisical) *Client {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), Config{SiteURL: server.URL + "/", ClientID: "id", ClientSecret: "secret"})
	require.NoError(t, err)
	return client
}

func TestLoginReportsExpiry(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600}
	client := newTestClientInternal(t, fake)

	session, err := client.Login(t.Context())
	require.NoError(t, err)
	assert.Equal(t, time.Hour, session.ExpiresIn)
}

func TestLoginSurfacesInfisicalMessage(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600, rejectSecret: true}
	client := newTestClientInternal(t, fake)

	_, err := client.Login(t.Context())
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Equal(t, "Invalid credentials", apiErr.Message)
}

func TestListSecretsReusesTokenUntilExpiry(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600, secrets: `{"secrets":[]}`}
	client := newTestClientInternal(t, fake)
	current := time.Unix(1_700_000_000, 0)
	client.now = func() time.Time { return current }
	query := SecretsQuery{ProjectID: "p1", Environment: "prod"}

	_, err := client.ListSecrets(t.Context(), query)
	require.NoError(t, err)
	_, err = client.ListSecrets(t.Context(), query)
	require.NoError(t, err)
	assert.Equal(t, int32(1), fake.logins.Load())

	current = current.Add(time.Hour)
	_, err = client.ListSecrets(t.Context(), query)
	require.NoError(t, err)
	assert.Equal(t, int32(2), fake.logins.Load())
}

func TestListSecretsPrecedence(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600, secrets: `{
		"secrets":[{"secretKey":"DB_PASS","secretValue":"direct"}],
		"imports":[
			{"secretPath":"/shared","environment":"prod","secrets":[{"secretKey":"DB_PASS","secretValue":"first"},{"secretKey":"SMTP_HOST","secretValue":"first"}]},
			{"secretPath":"/smtp","environment":"prod","secrets":[{"secretKey":"SMTP_HOST","secretValue":"second"}]}
		]}`}
	client := newTestClientInternal(t, fake)

	values, err := client.ListSecrets(t.Context(), SecretsQuery{
		ProjectID: "p1", Environment: "prod", SecretPath: "billing/", IncludeImports: true, ExpandSecretReferences: true,
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASS": "direct", "SMTP_HOST": "second"}, values)

	query := *fake.lastQuery.Load()
	assert.Contains(t, query, "secretPath=%2Fbilling")
	assert.Contains(t, query, "includeImports=true")
	assert.Contains(t, query, "expandSecretReferences=true")
}

func TestListSecretsRejectsHiddenValues(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600, secrets: `{"secrets":[{"secretKey":"A","secretValue":"","secretValueHidden":true}]}`}
	client := newTestClientInternal(t, fake)

	_, err := client.ListSecrets(t.Context(), SecretsQuery{ProjectID: "p1", Environment: "prod"})
	require.ErrorIs(t, err, ErrSecretValueHidden)
}

func TestListProjectsAndFolders(t *testing.T) {
	fake := &fakeInfisical{expiresIn: 3600}
	client := newTestClientInternal(t, fake)

	projects, err := client.ListProjects(t.Context())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "prod", projects[0].Environments[0].Slug)

	folders, err := client.ListFolders(t.Context(), "p1", "prod", "billing")
	require.NoError(t, err)
	assert.Equal(t, []string{"db", "stripe"}, folders)
}

func TestParseSiteURL(t *testing.T) {
	parsed, err := ParseSiteURL("")
	require.NoError(t, err)
	assert.Equal(t, DefaultSiteURL, parsed.String())

	parsed, err = ParseSiteURL("https://infisical.example.ts.net/sub/?x=1")
	require.NoError(t, err)
	assert.Equal(t, "https://infisical.example.ts.net/sub", parsed.String())

	_, err = ParseSiteURL("ftp://infisical.example")
	require.Error(t, err)
	_, err = ParseSiteURL("https://user:pass@infisical.example")
	require.Error(t, err)
}

func TestNormalizeSecretPath(t *testing.T) {
	assert.Equal(t, "/", NormalizeSecretPath(""))
	assert.Equal(t, "/", NormalizeSecretPath(" / "))
	assert.Equal(t, "/api/db", NormalizeSecretPath("api/db/"))
}
