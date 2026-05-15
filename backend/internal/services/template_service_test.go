package services

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	httputils "github.com/getarcaneapp/arcane/backend/pkg/utils/httpx"
	envtypes "github.com/getarcaneapp/arcane/types/env"
	tmpl "github.com/getarcaneapp/arcane/types/template"
)

func setupTemplateServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.TemplateRegistry{}, &models.ComposeTemplate{}))

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

func makePublicTestClient(t *testing.T, server *httptest.Server) (*http.Client, httputils.LookupIPFunc, string) {
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

	baseURL := fmt.Sprintf("http://templates.example.test:%s", parsedURL.Port())
	return client, lookupIP, baseURL
}

func TestResolveTemplateIconURL(t *testing.T) {
	service := &TemplateService{}

	tests := []struct {
		name       string
		compose    string
		envContent string
		want       string
	}{
		{
			name: "top level icon",
			compose: `x-arcane:
  icon: https://cdn.example/icon.png
services:
  app:
    image: nginx:alpine
`,
			want: "https://cdn.example/icon.png",
		},
		{
			name: "icons alias",
			compose: `x-arcane:
  icons: https://cdn.example/alias.png
services:
  app:
    image: nginx:alpine
`,
			want: "https://cdn.example/alias.png",
		},
		{
			name: "env interpolation",
			compose: `x-arcane:
  icon: ${TEMPLATE_ICON}
services:
  app:
    image: nginx:alpine
`,
			envContent: "TEMPLATE_ICON=https://cdn.example/from-env.png\n",
			want:       "https://cdn.example/from-env.png",
		},
		{
			name: "invalid x arcane block",
			compose: `x-arcane: plain-text
services:
  app:
    image: nginx:alpine
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iconURL := service.resolveTemplateIconURL(context.Background(), tt.compose, tt.envContent)
			if tt.want == "" {
				require.Nil(t, iconURL)
				return
			}

			require.NotNil(t, iconURL)
			require.Equal(t, tt.want, *iconURL)
		})
	}
}

func TestFetchRegistryTemplates_ReusesCachedIconsOnNotModified(t *testing.T) {
	var composeHits atomic.Int32
	var okComposeURL string
	var badComposeURL string

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
      "description": "Has a compose icon",
      "version": "1.0.0",
      "author": "Arcane",
      "compose_url": "` + okComposeURL + `",
      "env_url": "",
      "documentation_url": "",
      "tags": ["demo"]
    },
    {
      "id": "bad",
      "name": "Broken Template",
      "description": "Compose fetch fails",
      "version": "1.0.0",
      "author": "Arcane",
      "compose_url": "` + badComposeURL + `",
      "env_url": "",
      "documentation_url": "",
      "tags": ["demo"]
    }
  ]
}`))
		case "/ok.yml":
			composeHits.Add(1)
			_, _ = w.Write([]byte(`x-arcane:
  icon: https://cdn.example/good.png
services:
  app:
    image: nginx:alpine
`))
		case "/missing.yml":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, lookupIP, baseURL := makePublicTestClient(t, server)
	registryURL := baseURL + "/registry.json"
	okComposeURL = baseURL + "/ok.yml"
	badComposeURL = baseURL + "/missing.yml"

	service := &TemplateService{
		httpClient:        client,
		lookupIP:          lookupIP,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}
	registry := &models.TemplateRegistry{
		BaseModel: models.BaseModel{ID: "reg-1"},
		Name:      "Demo Registry",
		URL:       registryURL,
		Enabled:   true,
	}

	templates, err := service.fetchRegistryTemplates(context.Background(), registry)
	require.NoError(t, err)
	require.Len(t, templates, 2)
	require.NotNil(t, templates[0].Metadata)
	require.NotNil(t, templates[0].Metadata.IconURL)
	require.Equal(t, "https://cdn.example/good.png", *templates[0].Metadata.IconURL)
	require.Nil(t, templates[1].Metadata.IconURL)
	require.EqualValues(t, 1, composeHits.Load())

	cachedTemplates, err := service.fetchRegistryTemplates(context.Background(), registry)
	require.NoError(t, err)
	require.Len(t, cachedTemplates, 2)
	require.NotNil(t, cachedTemplates[0].Metadata)
	require.NotNil(t, cachedTemplates[0].Metadata.IconURL)
	require.Equal(t, "https://cdn.example/good.png", *cachedTemplates[0].Metadata.IconURL)
	require.EqualValues(t, 1, composeHits.Load())
}

func TestDownloadTemplate_PreservesIconURL(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/compose.yaml":
			_, _ = w.Write([]byte(`x-arcane:
  icon: https://cdn.example/download.png
services:
  app:
    image: nginx:alpine
`))
		case "/template.env":
			_, _ = w.Write([]byte("DOWNLOAD_ICON=https://cdn.example/download.png\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, lookupIP, baseURL := makePublicTestClient(t, server)

	service := &TemplateService{
		db:                setupTemplateServiceTestDB(t),
		httpClient:        client,
		lookupIP:          lookupIP,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	remoteTemplate := &models.ComposeTemplate{
		BaseModel:   models.BaseModel{ID: "remote:reg-1:demo"},
		Name:        "Demo Template",
		Description: "Remote template",
		IsRemote:    true,
		IsCustom:    false,
		RegistryID:  stringPtr("reg-1"),
		Metadata: &models.ComposeTemplateMetadata{
			RemoteURL: stringPtr(baseURL + "/compose.yaml"),
			EnvURL:    stringPtr(baseURL + "/template.env"),
			IconURL:   stringPtr("https://cdn.example/download.png"),
		},
	}

	downloaded, err := service.DownloadTemplate(context.Background(), remoteTemplate)
	require.NoError(t, err)
	require.NotNil(t, downloaded)
	require.False(t, downloaded.IsRemote)
	require.NotNil(t, downloaded.Metadata)
	require.NotNil(t, downloaded.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/download.png", *downloaded.Metadata.IconURL)

	var stored models.ComposeTemplate
	require.NoError(t, service.db.WithContext(context.Background()).First(&stored, "id = ?", downloaded.ID).Error)
	require.NotNil(t, stored.Metadata)
	require.NotNil(t, stored.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/download.png", *stored.Metadata.IconURL)
}

func TestGetAllTemplatesPaginated_FiltersByType(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	now := time.Now()
	db := setupTemplateServiceTestDB(t)
	localTemplates := []models.ComposeTemplate{
		{
			BaseModel:   models.BaseModel{ID: "local-one", CreatedAt: now, UpdatedAt: &now},
			Name:        "Local One",
			Description: "Local template",
			Content:     "services: {}",
			IsCustom:    true,
			IsRemote:    false,
		},
		{
			BaseModel:   models.BaseModel{ID: "local-two", CreatedAt: now, UpdatedAt: &now},
			Name:        "Local Two",
			Description: "Local template",
			Content:     "services: {}",
			IsCustom:    true,
			IsRemote:    false,
		},
	}
	require.NoError(t, db.WithContext(context.Background()).Create(&localTemplates).Error)

	service := &TemplateService{
		db:                db,
		httpClient:        http.DefaultClient,
		registryFetchMeta: make(map[string]*registryFetchMeta),
		remoteCache: remoteCache{
			templates: []models.ComposeTemplate{
				{
					BaseModel:   models.BaseModel{ID: "remote-one", CreatedAt: now, UpdatedAt: &now},
					Name:        "Remote One",
					Description: "Remote template",
					Content:     "services: {}",
					IsRemote:    true,
					RegistryID:  stringPtr("registry-one"),
				},
			},
			lastFetch: now,
		},
	}

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
			templates, _, err := service.GetAllTemplatesPaginated(context.Background(), pagination.QueryParams{
				PaginationParams: pagination.PaginationParams{Start: 0, Limit: 20},
				Filters:          map[string]string{"type": tt.typeFilter},
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
		lookupIP:          httputils.DefaultLookupIP,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	_, err := service.FetchRaw(context.Background(), "http://127.0.0.1:8080/registry.json")
	require.Error(t, err)
	var unsafeErr *common.UnsafeRemoteURLError
	require.ErrorAs(t, err, &unsafeErr)
}

func TestSyncFilesystemTemplatesInternal_PopulatesIconURL(t *testing.T) {
	tempDir := t.TempDir()
	setTestWorkingDir(t, tempDir)

	templateDir := filepath.Join(tempDir, "data", "templates", "example")
	require.NoError(t, os.MkdirAll(templateDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "compose.yaml"), []byte(`x-arcane:
  icon: https://cdn.example/local.png
services:
  app:
    image: nginx:alpine
`), 0o644))

	service := &TemplateService{
		db:                setupTemplateServiceTestDB(t),
		httpClient:        http.DefaultClient,
		registryFetchMeta: make(map[string]*registryFetchMeta),
	}

	require.NoError(t, service.syncFilesystemTemplatesInternal(context.Background()))

	var stored models.ComposeTemplate
	require.NoError(t, service.db.WithContext(context.Background()).First(&stored, "name = ?", "example").Error)
	require.NotNil(t, stored.Metadata)
	require.NotNil(t, stored.Metadata.IconURL)
	require.Equal(t, "https://cdn.example/local.png", *stored.Metadata.IconURL)
}

func TestUpdateGlobalVariables_RejectsNewlineInjectionKey(t *testing.T) {
	projectsDir := t.TempDir()

	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))
	dbWrap := &database.DB{DB: db}
	settingsSvc, err := NewSettingsService(context.Background(), dbWrap)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.UpdateSetting(context.Background(), "projectsDirectory", projectsDir))

	service := &TemplateService{db: dbWrap, settingsService: settingsSvc}

	err = service.UpdateGlobalVariables(context.Background(), []envtypes.Variable{
		{Key: "BENIGN\nINJECTED", Value: "x"},
	})
	require.True(t, common.IsInvalidEnvKeyError(err), "expected InvalidEnvKeyError, got %v", err)

	_, statErr := os.Stat(filepath.Join(projectsDir, ".env.global"))
	require.True(t, os.IsNotExist(statErr), ".env.global must not be written on validation failure")
}
