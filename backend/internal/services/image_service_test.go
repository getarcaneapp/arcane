package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
)

func TestGetImageIDsFromSummariesInternal(t *testing.T) {
	items := []imagetypes.Summary{
		{ID: "img1"},
		{ID: "img2"},
		{ID: "img1"},
		{ID: ""},
	}

	got := getImageIDsFromSummariesInternal(items)
	assert.Equal(t, []string{"img1", "img2"}, got)
}

func TestApplyVulnerabilitySummariesToItemsInternal(t *testing.T) {
	items := []imagetypes.Summary{
		{ID: "img1"},
		{ID: "img2"},
	}

	summary := &vulnerability.ScanSummary{
		ImageID: "img1",
		Status:  vulnerability.ScanStatusCompleted,
	}
	vulnerabilityMap := map[string]*vulnerability.ScanSummary{
		"img1": summary,
	}

	applyVulnerabilitySummariesToItemsInternal(items, vulnerabilityMap)

	assert.Equal(t, summary, items[0].VulnerabilityScan)
	assert.Nil(t, items[1].VulnerabilityScan)
}

func TestImageService_GetUpdateInfoByImageRefs_MatchesCanonicalAndFamiliarRepos(t *testing.T) {
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}))

	svc := &ImageService{db: &database.DB{DB: db}}
	now := time.Now().UTC()

	records := []models.ImageUpdateRecord{
		{
			ID:             "sha256:nginx-latest",
			Repository:     "docker.io/library/nginx",
			Tag:            "latest",
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentVersion: "latest",
			CheckTime:      now,
		},
		{
			ID:             "sha256:redis-seven",
			Repository:     "library/redis",
			Tag:            "7",
			HasUpdate:      false,
			UpdateType:     "digest",
			CurrentVersion: "7",
			CheckTime:      now.Add(-time.Minute),
		},
	}

	for i := range records {
		require.NoError(t, db.Create(&records[i]).Error)
	}

	updates, err := svc.GetUpdateInfoByImageRefs(context.Background(), []string{
		"nginx:latest",
		"docker.io/library/nginx:latest",
		"redis:7",
	})
	require.NoError(t, err)

	require.Contains(t, updates, "nginx:latest")
	require.Contains(t, updates, "docker.io/library/nginx:latest")
	require.Contains(t, updates, "redis:7")
	assert.True(t, updates["nginx:latest"].HasUpdate)
	assert.True(t, updates["docker.io/library/nginx:latest"].HasUpdate)
	assert.False(t, updates["redis:7"].HasUpdate)
}

func setupImageServiceAuthTest(t *testing.T) (*ImageService, *database.DB) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ContainerRegistry{}, &models.KVEntry{}))

	crypto.InitEncryption(&crypto.Config{
		Environment:   string(config.AppEnvironmentTest),
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
	})

	dbWrap := &database.DB{DB: db}
	svc := &ImageService{
		registryService: NewContainerRegistryService(dbWrap, nil, NewKVService(dbWrap)),
	}

	return svc, dbWrap
}

func createTestPullRegistry(t *testing.T, db *database.DB, url, username, token string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt(token)
	require.NoError(t, err)

	reg := &models.ContainerRegistry{
		URL:          url,
		Username:     username,
		Token:        encryptedToken,
		Enabled:      true,
		RegistryType: registryTypeGeneric,
	}
	require.NoError(t, db.WithContext(context.Background()).Create(reg).Error)
}

func decodeRegistryAuth(t *testing.T, encoded string) dockerregistry.AuthConfig {
	t.Helper()

	cfg, err := dockerauthconfig.Decode(encoded)
	require.NoError(t, err)
	return *cfg
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyToken(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "docker-user", "   ")

	pullOptions, err := svc.getPullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyUsername(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "   ", "docker-token")

	pullOptions, err := svc.getPullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistryUsesValidCredentials(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	pullOptions, err := svc.getPullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuth(t, pullOptions.RegistryAuth)
	assert.Equal(t, "docker-user", authCfg.Username)
	assert.Equal(t, "docker-token", authCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", authCfg.ServerAddress)
}

func TestGetPullOptionsWithAuth_ExternalCredentialsOverrideDBRegistryInternal(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://registry.example.com", "db-user", "db-token")

	pullOptions, err := svc.getPullOptionsWithAuth(context.Background(), "registry.example.com/team/app:latest", []containerregistry.Credential{
		{URL: "https://registry.example.com", Username: "external-user", Token: "external-token", Enabled: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuth(t, pullOptions.RegistryAuth)
	assert.Equal(t, "external-user", authCfg.Username)
	assert.Equal(t, "external-token", authCfg.Password)
	assert.Equal(t, "registry.example.com", authCfg.ServerAddress)
}

func TestImageServicePullImageRetriesAnonymouslyAfterAuthRejectedInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	authHeaders := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/images/create") {
			http.NotFound(w, r)
			return
		}
		authHeaders = append(authHeaders, r.Header.Get(dockerregistry.AuthHeader))
		if len(authHeaders) == 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"Pulled anonymously"}` + "\n"))
	}))
	t.Cleanup(server.Close)

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	imageSvc := NewImageService(db, dockerService, nil, nil, nil, NewEventService(db, nil, nil))

	err := imageSvc.PullImage(context.Background(), "registry.example.com/team/app:latest", io.Discard, systemUser, []containerregistry.Credential{
		{URL: "https://registry.example.com", Username: "external-user", Token: "external-token", Enabled: true},
	})
	require.NoError(t, err)
	require.Len(t, authHeaders, 2)
	assert.NotEmpty(t, authHeaders[0])
	assert.Empty(t, authHeaders[1])
}

func TestImageServiceTagImageCallsDockerAPIInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	var gotRepo, gotTag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || dockerTestPathInternal(r.URL.Path) != "/images/source:latest/tag" {
			http.NotFound(w, r)
			return
		}
		gotRepo = r.URL.Query().Get("repo")
		gotTag = r.URL.Query().Get("tag")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, NewEventService(db, nil, nil))

	err := imageSvc.TagImage(context.Background(), "source:latest", imagetypes.TagRequest{Repository: "registry.example.com/team/app", Tag: "v2"}, systemUser)
	require.NoError(t, err)
	assert.Equal(t, "registry.example.com/team/app", gotRepo)
	assert.Equal(t, "v2", gotTag)
}

func TestImageServiceGetImageHistoryCallsDockerAPIInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || dockerTestPathInternal(r.URL.Path) != "/images/source:latest/history" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "layer-1", "Created": 1710000000, "CreatedBy": "/bin/sh", "Size": 123},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, NewEventService(db, nil, nil))

	history, err := imageSvc.GetImageHistory(context.Background(), "source:latest")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "layer-1", history[0].ID)
	assert.Equal(t, int64(123), history[0].Size)
}

func TestImageServiceSearchImagesRequiresTermInternal(t *testing.T) {
	imageSvc := NewImageService(nil, &DockerClientService{}, nil, nil, nil, nil)

	_, err := imageSvc.SearchImages(context.Background(), " ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search term is required")
}

func TestImageServiceExportImageReturnsTarStreamInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || dockerTestPathInternal(r.URL.Path) != "/images/get" {
			http.NotFound(w, r)
			return
		}
		names := r.URL.Query()["names"]
		require.Equal(t, []string{"source:latest"}, names)
		_, _ = w.Write([]byte("tar-bytes"))
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, NewEventService(db, nil, nil))

	reader, err := imageSvc.ExportImage(context.Background(), "source:latest")
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "tar-bytes", string(body))
}

func TestImageServiceSearchImagesCallsDockerAPIInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	var gotTerm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || dockerTestPathInternal(r.URL.Path) != "/images/search" {
			http.NotFound(w, r)
			return
		}
		gotTerm, _ = url.QueryUnescape(r.URL.Query().Get("term"))
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "library/nginx", "description": "web server", "star_count": 1, "is_official": true},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, NewEventService(db, nil, nil))

	results, err := imageSvc.SearchImages(context.Background(), "nginx")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "nginx", gotTerm)
	assert.Equal(t, "library/nginx", results[0].Name)
	assert.True(t, results[0].Official)
}

func TestShouldRetryAnonymousPullInternal_UnauthorizedWithAuth(t *testing.T) {
	err := errors.New(`Error response from daemon: Head "registry-1.docker.io/v2/library/nginx/manifests/latest": unauthorized: incorrect username or password`)

	assert.True(t, shouldRetryAnonymousPullInternal(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, err))
}

func TestShouldRetryAnonymousPullInternal_SkipsRetryWithoutUnauthorizedOrAuth(t *testing.T) {
	nonAuthErr := errors.New("Error response from daemon: i/o timeout")
	unauthorizedErr := errors.New("unauthorized: authentication required")

	assert.False(t, shouldRetryAnonymousPullInternal(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, nonAuthErr))
	assert.False(t, shouldRetryAnonymousPullInternal(client.ImagePullOptions{}, unauthorizedErr))
}
