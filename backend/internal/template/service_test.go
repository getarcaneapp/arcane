package template

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tmpl "github.com/getarcaneapp/arcane/types/v2/template"
	"github.com/libtnb/sqlite"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

func setupTemplateServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TemplateRegistry{}, &ComposeTemplate{}))

	return &database.DB{DB: db}
}

func setTestWorkingDir(t *testing.T, dir string) {
	t.Helper()

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))

	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalDir))
	})
}

func makePublicTestClient(t *testing.T, server *httptest.Server) (*http.Client, httpx.LookupIPFunc, string) {
	t.Helper()

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	listenerAddr := server.Listener.Addr().String()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, network, listenerAddr)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	lookupIP := func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}

	baseURL := "http://templates.example.test:" + parsedURL.Port()
	return client, lookupIP, baseURL
}

func TestFetchRegistryTemplates_ReusesCachedIconsOnNotModified(t *testing.T) {
	var composeHits atomic.Int32
	var composeURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			if r.Header.Get("If-Modified-Since") != "" {
				w.WriteHeader(http.StatusNotModified)
				return
			}

			w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
			_, _ = w.Write([]byte(`{
  "name": "Demo Registry",
  "description": "Registry used in tests",
  "version": "1.0.0",
  "author": "Arcane",
  "templates": [
    {
      "id": "good",
      "name": "Good Template",
      "description": "Has a registry icon",
      "version": "1.0.0",
      "author": "Arcane",
      "compose_url": "` + composeURL + `",
      "env_url": "",
      "documentation_url": "",
      "icon_url": "https://cdn.example/good.png",
      "tags": ["demo"]
    },
    {
      "id": "plain",
      "name": "Plain Template",
      "description": "Only has a compose icon",
      "version": "1.0.0",
      "author": "Arcane",
      "compose_url": "` + composeURL + `",
      "env_url": "",
      "documentation_url": "",
      "tags": ["demo"]
    }
  ]
}`))
		case "/compose.yml":
			composeHits.Add(1)
			_, _ = w.Write([]byte(`x-arcane:
  icon: https://cdn.example/compose.png
services:
  app:
    image: nginx:alpine
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, lookupIP, baseURL := makePublicTestClient(t, server)
	registryURL := baseURL + "/registry.json"
	composeURL = baseURL + "/compose.yml"

	service := &TemplateService{
		httpClient:        client,
		lookupIP:          lookupIP,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}
	registry := &TemplateRegistry{
		ID:      "reg-1",
		Name:    "Demo Registry",
		URL:     registryURL,
		Enabled: true,
	}

	templates, err := service.fetchRegistryTemplatesInternal(t.Context(), registry, service.remoteGeneration.Load())
	require.NoError(t, err)
	require.Len(t, templates, 2)
	require.NotNil(t, templates[0].Metadata)
	require.NotNil(t, templates[0].Metadata.IconURL)
	require.Equal(t, "https://cdn.example/good.png", *templates[0].Metadata.IconURL)
	require.Nil(t, templates[1].Metadata.IconURL)

	cachedTemplates, err := service.fetchRegistryTemplatesInternal(t.Context(), registry, service.remoteGeneration.Load())
	require.NoError(t, err)
	require.Len(t, cachedTemplates, 2)
	require.NotNil(t, cachedTemplates[0].Metadata)
	require.NotNil(t, cachedTemplates[0].Metadata.IconURL)
	require.Equal(t, "https://cdn.example/good.png", *cachedTemplates[0].Metadata.IconURL)
	require.Zero(t, composeHits.Load())
}

func TestDownloadTemplate_PreservesIconURL(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/compose.yaml":
			_, _ = w.Write([]byte(`services:
  app:
    image: nginx:alpine
`))
		case "/template.env":
			_, _ = w.Write([]byte("APP_PORT=8080\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, lookupIP, baseURL := makePublicTestClient(t, server)

	settingsSvc := minimalSettingsServiceForTest(t)
	require.NoError(t, settingsSvc.UpdateSetting(t.Context(), "templatesDirectory", filepath.Join(tempDir, "templates")))

	service := &TemplateService{
		db:                setupTemplateServiceTestDB(t),
		httpClient:        client,
		lookupIP:          lookupIP,
		settingsService:   settingsSvc,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	remoteTemplate := &ComposeTemplate{
		ID:          "remote:reg-1:demo",
		Name:        "Demo Template",
		Description: "Remote template",
		IsRemote:    true,
		IsCustom:    false,
		RegistryID:  mo.EmptyableToOption(strings.TrimSpace("reg-1")).ToPointer(),
		Metadata: &ComposeTemplateMetadata{
			RemoteURL:       mo.EmptyableToOption(strings.TrimSpace(baseURL + "/compose.yaml")).ToPointer(),
			EnvURL:          mo.EmptyableToOption(strings.TrimSpace(baseURL + "/template.env")).ToPointer(),
			IconURL:         mo.EmptyableToOption(strings.TrimSpace("https://cdn.example/download.png")).ToPointer(),
			RegistryIconURL: mo.EmptyableToOption(strings.TrimSpace("https://cdn.example/download.png")).ToPointer(),
		},
	}

	downloaded, err := service.DownloadTemplate(t.Context(), remoteTemplate)
	require.NoError(t, err)
	require.NotNil(t, downloaded)
	require.False(t, downloaded.IsRemote)
	require.NotNil(t, downloaded.Metadata)
	require.NotNil(t, downloaded.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/download.png", *downloaded.Metadata.IconURL)

	var stored ComposeTemplate
	require.NoError(t, service.db.WithContext(t.Context()).First(&stored, "id = ?", downloaded.ID).Error)
	require.NotNil(t, stored.Metadata)
	require.NotNil(t, stored.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/download.png", *stored.Metadata.IconURL)

	// Filesystem sync and parsed content must not clear the registry icon.
	content, err := service.GetTemplateContentWithParsedData(t.Context(), downloaded.ID)
	require.NoError(t, err)
	require.NotNil(t, content.Template.Metadata)
	require.NotNil(t, content.Template.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/download.png", *content.Template.Metadata.IconURL)

	// A compose icon wins, and removing it restores the registry icon.
	for _, tc := range []struct{ compose, icon string }{
		{"x-arcane:\n  icon: https://cdn.example/compose.png\nservices:\n  app:\n    image: nginx:alpine\n", "https://cdn.example/compose.png"},
		{"services:\n  app:\n    image: nginx:alpine\n", "https://cdn.example/download.png"},
	} {
		require.NoError(t, service.UpdateTemplate(t.Context(), downloaded.ID, &ComposeTemplate{Name: stored.Name, Description: stored.Description, Content: tc.compose}))
		require.NoError(t, service.db.WithContext(t.Context()).First(&stored, "id = ?", downloaded.ID).Error)
		require.NotNil(t, stored.Metadata)
		require.NotNil(t, stored.Metadata.IconURL)
		require.Equal(t, tc.icon, *stored.Metadata.IconURL)
	}
}

func TestGetAllTemplatesPaginated_FiltersByType(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	now := time.Now()
	db := setupTemplateServiceTestDB(t)
	localTemplates := []ComposeTemplate{
		{
			ID: "local-one", CreatedAt: now, UpdatedAt: &now,
			Name:        "Local One",
			Description: "Local template",
			Content:     "services: {}",
			IsCustom:    true,
			IsRemote:    false,
		},
		{
			ID: "local-two", CreatedAt: now, UpdatedAt: &now,
			Name:        "Local Two",
			Description: "Local template",
			Content:     "services: {}",
			IsCustom:    true,
			IsRemote:    false,
		},
	}
	require.NoError(t, db.WithContext(t.Context()).Create(&localTemplates).Error)

	service := NewTemplateService(t.Context(), db, http.DefaultClient, nil)
	service.remoteCache.Set(service.remoteGeneration.Load(), []ComposeTemplate{
		{
			ID: "remote-one", CreatedAt: now, UpdatedAt: &now,
			Name:        "Remote One",
			Description: "Remote template",
			Content:     "services: {}",
			IsRemote:    true,
			RegistryID:  mo.EmptyableToOption(strings.TrimSpace("registry-one")).ToPointer(),
		},
	})

	tests := []struct {
		name       string
		typeFilter string
		wantIDs    []string
	}{
		{name: "local", typeFilter: "false", wantIDs: []string{"local-one", "local-two"}},
		{name: "remote", typeFilter: "true", wantIDs: []string{"remote-one"}},
		{name: "both", typeFilter: "false,true", wantIDs: []string{"local-one", "local-two", "remote-one"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates, _, err := service.GetAllTemplatesPaginated(t.Context(), pagination.QueryParams{
				Start: 0, Limit: 20,
				Filters: map[string]string{"type": tt.typeFilter},
			})
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantIDs, templateIDsInternal(templates))
		})
	}
}

func templateIDsInternal(templates []tmpl.Template) []string {
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		ids = append(ids, template.ID)
	}
	return ids
}

func TestFetchRaw_BlocksUnsafeRemoteURL(t *testing.T) {
	service := &TemplateService{
		httpClient:        http.DefaultClient,
		lookupIP:          httpx.DefaultLookupIP,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	_, err := service.FetchRaw(t.Context(), "http://127.0.0.1:8080/registry.json")
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrUnsafeRemoteURL)
}

func TestSyncFilesystemTemplatesInternal_PopulatesIconURL(t *testing.T) {
	tempDir := t.TempDir()

	templatesRoot := filepath.Join(tempDir, "templates")
	templateDir := filepath.Join(templatesRoot, "example")
	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "compose.yaml"), []byte(`x-arcane:
  icon: https://cdn.example/local.png
services:
  app:
    image: nginx:alpine
`), 0o644))

	settingsSvc := minimalSettingsServiceForTest(t)
	require.NoError(t, settingsSvc.UpdateSetting(t.Context(), "templatesDirectory", templatesRoot))

	service := &TemplateService{
		db:                setupTemplateServiceTestDB(t),
		httpClient:        http.DefaultClient,
		settingsService:   settingsSvc,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	require.NoError(t, service.syncFilesystemTemplatesInternal(t.Context()))

	var stored ComposeTemplate
	require.NoError(t, service.db.WithContext(t.Context()).First(&stored, "name = ?", "example").Error)
	require.NotNil(t, stored.Metadata)
	require.NotNil(t, stored.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/local.png", *stored.Metadata.IconURL)
}

func TestGetTemplate_ForceRefreshesRemoteCacheOnMiss(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	var registryHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/registry.json" {
			http.NotFound(w, r)
			return
		}
		registryHits.Add(1)
		_, _ = w.Write([]byte(`{
  "name": "Demo Registry",
  "description": "Test",
  "version": "1.0.0",
  "author": "Arcane",
  "templates": [
    {
      "id": "affine",
      "name": "AFFiNE",
      "description": "",
      "version": "1.0.0",
      "author": "Arcane",
      "compose_url": "",
      "env_url": "",
      "documentation_url": "",
      "tags": []
    }
  ]
}`))
	}))
	defer server.Close()

	client, lookupIP, baseURL := makePublicTestClient(t, server)
	db := setupTemplateServiceTestDB(t)

	registry := &TemplateRegistry{
		ID:      "reg-1",
		Name:    "Demo",
		URL:     baseURL + "/registry.json",
		Enabled: true,
	}
	require.NoError(t, db.WithContext(t.Context()).Create(registry).Error)

	settingsSvc := minimalSettingsServiceForTest(t)
	// Point templates+projects directories at tempDir so sync calls don't try to
	// touch the real /app filesystem during tests.
	require.NoError(t, settingsSvc.UpdateSetting(t.Context(), "templatesDirectory", filepath.Join(tempDir, "templates")))
	require.NoError(t, settingsSvc.UpdateSetting(t.Context(), "projectsDirectory", filepath.Join(tempDir, "projects")))

	service := NewTemplateService(t.Context(), db, client, settingsSvc)
	service.lookupIP = lookupIP
	service.safeHTTPClient = service.newSafeHTTPClientInternal(t.Context())

	// Cache starts empty; GetTemplate for a remote ID should force a refresh and find the template.
	got, err := service.GetTemplate(t.Context(), "remote:reg-1:affine")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.IsRemote)
	require.Equal(t, "AFFiNE", got.Name)
	require.GreaterOrEqual(t, registryHits.Load(), int32(1), "registry should be fetched at least once")

	// Unknown remote ID still surfaces a clear error after the forced refresh.
	_, err = service.GetTemplate(t.Context(), "remote:reg-1:does-not-exist")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func minimalSettingsServiceForTest(t *testing.T) *settings.SettingsService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	svc, err := newSettingsServiceForTestInternal(t, t.Context(), &database.DB{DB: db})
	require.NoError(t, err)
	return svc
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}
