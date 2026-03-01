package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
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

	registryURL := server.URL + "/registry.json"
	okComposeURL = server.URL + "/ok.yml"
	badComposeURL = server.URL + "/missing.yml"

	service := &TemplateService{
		httpClient:        server.Client(),
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

	service := &TemplateService{
		db:                setupTemplateServiceTestDB(t),
		httpClient:        server.Client(),
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
			RemoteURL: stringPtr(server.URL + "/compose.yaml"),
			EnvURL:    stringPtr(server.URL + "/template.env"),
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
