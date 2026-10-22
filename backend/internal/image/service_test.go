package image

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/imageref"
)

var imageDockerAPIVersionPrefix = regexp.MustCompile(`^/v\d+\.\d+`)

// testProjectRow is a minimal stand-in for project.Project: the project
// package imports this one, so the in-package test cannot import it back.
type testProjectRow struct {
	database.BaseModel
	Name               string
	Path               string
	BuildImageRefsJSON *string `gorm:"column:build_image_refs_json"`
}

func imageDockerTestPath(path string) string {
	return imageDockerAPIVersionPrefix.ReplaceAllString(path, "")
}

func setupImageProjectTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&testProjectRow{},
		&settings.SettingVariable{},
		&imageupdate.ImageUpdateRecord{},
		&event.Event{},
	))
	return &database.DB{DB: db}
}

func TestCollectPinnedReferencesByImageID(t *testing.T) {
	containers := []container.Summary{
		{
			ID:      "c1",
			ImageID: "sha256:image-pinned-1",
			Image:   "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0",
			State:   "running",
		},
		{
			ID:      "c2",
			ImageID: "sha256:image-pinned-1",
			Image:   "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0",
			State:   "exited",
		},
		{
			ID:      "c3",
			ImageID: "sha256:image-pinned-1",
			Image:   "ghcr.io/syncthing/syncthing:v2@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0",
			State:   "running",
		},
		{
			ID:      "c4",
			ImageID: "sha256:image-pinned-2",
			Image:   "docker.io/library/redis@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			State:   "running",
		},
		{
			ID:      "c5",
			ImageID: "sha256:image-regular",
			Image:   "nginx:latest",
			State:   "running",
		},
		{
			ID:      "c6",
			ImageID: "sha256:image-hash",
			Image:   "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			State:   "running",
		},
		{
			ID:      "c7",
			ImageID: "sha256:image-invalid",
			Image:   "not a valid docker reference ::: 123",
			State:   "running",
		},
	}

	result := collectPinnedReferencesByImageID(containers)
	require.NotNil(t, result)

	pins1 := result["sha256:image-pinned-1"]
	require.Len(t, pins1, 2)
	assert.Equal(t, []string{
		"ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0",
		"ghcr.io/syncthing/syncthing:v2@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0",
	}, pins1)

	pins2 := result["sha256:image-pinned-2"]
	require.Len(t, pins2, 1)
	assert.Equal(t, []string{"docker.io/library/redis@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}, pins2)

	assert.NotContains(t, result, "sha256:image-regular")
	assert.NotContains(t, result, "sha256:image-hash")
	assert.NotContains(t, result, "sha256:image-invalid")
}

func TestMapDockerImagesToDTOs_PopulatesPinnedReferences(t *testing.T) {
	pinnedRef := "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"
	dockerImages := []image.Summary{
		{
			ID:          "sha256:pinned-image",
			RepoTags:    []string{},
			RepoDigests: []string{"ghcr.io/syncthing/syncthing@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"},
			Size:        12345,
		},
		{
			ID:          "sha256:regular-image",
			RepoTags:    []string{"nginx:alpine"},
			RepoDigests: []string{"nginx@sha256:abcdef"},
			Size:        67890,
		},
	}
	containers := []container.Summary{
		{
			ID:      "c-pinned",
			ImageID: "sha256:pinned-image",
			Image:   pinnedRef,
			Names:   []string{"/syncthing"},
		},
		{
			ID:      "c-regular",
			ImageID: "sha256:regular-image",
			Image:   "nginx:alpine",
			Names:   []string{"/nginx"},
		},
	}

	usageMap := BuildVolumeUsageMap(containers, nil)
	items := MapDockerImagesToDTOs(dockerImages, containers, usageMap, nil, nil)

	require.Len(t, items, 2)

	assert.Equal(t, "sha256:pinned-image", items[0].ID)
	assert.Equal(t, "ghcr.io/syncthing/syncthing", items[0].Repo)
	assert.Equal(t, "<none>", items[0].Tag)
	assert.Empty(t, items[0].RepoTags)
	assert.Equal(t, []string{pinnedRef}, items[0].PinnedReferences)
	assert.True(t, items[0].InUse)

	assert.Equal(t, "sha256:regular-image", items[1].ID)
	assert.Equal(t, "nginx", items[1].Repo)
	assert.Equal(t, "alpine", items[1].Tag)
	assert.Equal(t, []string{"nginx:alpine"}, items[1].RepoTags)
	assert.Nil(t, items[1].PinnedReferences)
	assert.True(t, items[1].InUse)
}

func TestImageService_GetImageDetail_EnrichesPinnedReferences(t *testing.T) {
	db := setupImageProjectTestDB(t)
	const pinnedID = "sha256:test-pinned-detail"
	pinnedRef := "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_ = json.NewEncoder(w).Encode([]image.Summary{
				{ID: pinnedID, Size: 2048},
			})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(image.InspectResponse{
				ID:          pinnedID,
				RepoTags:    []string{},
				RepoDigests: []string{"ghcr.io/syncthing/syncthing@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"},
				Size:        2048,
			})
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_ = json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:      "c-pinned-detail",
					ImageID: pinnedID,
					Image:   pinnedRef,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, event.NewEventService(db, nil, nil, nil), nil, nil, nil)

	detail, err := imageSvc.GetImageDetail(t.Context(), pinnedID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, pinnedID, detail.ID)
	assert.Equal(t, []string{pinnedRef}, detail.PinnedReferences)
}

func TestImageService_GetImageDetail_ContainerFailureDoesNotBreakInspection(t *testing.T) {
	db := setupImageProjectTestDB(t)
	const testID = "sha256:test-detail-resilient"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_ = json.NewEncoder(w).Encode([]image.Summary{
				{ID: testID, Size: 1024},
			})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(image.InspectResponse{
				ID:   testID,
				Size: 1024,
			})
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			http.Error(w, "docker daemon error listing containers", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, event.NewEventService(db, nil, nil, nil), nil, nil, nil)

	detail, err := imageSvc.GetImageDetail(t.Context(), testID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, testID, detail.ID)
	assert.Nil(t, detail.PinnedReferences)
}

func TestImagePaginationConfig_SearchesPinnedReferences(t *testing.T) {
	localConfig := NewImageService(nil, nil, nil, nil, nil, nil, nil, nil, nil).getImagePaginationConfig()

	item := types.ImageSummary{
		ID:               "sha256:syncthing",
		Repo:             "ghcr.io/syncthing/syncthing",
		Tag:              "<none>",
		PinnedReferences: []string{"ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"},
	}

	res := localConfig.SearchOrderAndPaginate([]types.ImageSummary{item}, pagination.QueryParams{
		Search: "8c8ff37",
		Limit:  10,
	})
	require.Len(t, res.Items, 1)

	resTag := localConfig.SearchOrderAndPaginate([]types.ImageSummary{item}, pagination.QueryParams{
		Search: "2.1.3",
		Limit:  10,
	})
	require.Len(t, resTag.Items, 1)
}

func TestImageService_GetUpdateInfoByImageRefs_MatchesCanonicalAndFamiliarRepos(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&imageupdate.ImageUpdateRecord{}))

	svc := NewImageService(&database.DB{DB: db}, nil, nil, nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()

	records := []imageupdate.ImageUpdateRecord{
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

	updates, err := svc.GetUpdateInfoByImageRefs(t.Context(), []string{
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

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&registry.ContainerRegistry{}, &kv.KVEntry{}, &testProjectRow{}))

	crypto.InitEncryption(&crypto.Config{
		Environment:   string(config.AppEnvironmentTest),
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
	})

	dbWrap := &database.DB{DB: db}
	return NewImageService(nil, nil, registry.NewContainerRegistryService(dbWrap, nil, kv.NewKVService(dbWrap), nil), nil, nil, nil, nil, nil, nil), dbWrap
}

func createTestPullRegistry(t *testing.T, db *database.DB, localUrl, username, token string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt(token)
	require.NoError(t, err)

	reg := &registry.ContainerRegistry{
		URL:          localUrl,
		Username:     username,
		Token:        encryptedToken,
		Enabled:      true,
		RegistryType: registry.RegistryTypeGeneric,
	}
	require.NoError(t, db.WithContext(t.Context()).Create(reg).Error)
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyToken(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "docker-user", "   ")

	pullOptions, err := svc.PullOptionsWithAuth(t.Context(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyUsername(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "   ", "docker-token")

	pullOptions, err := svc.PullOptionsWithAuth(t.Context(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistryUsesValidCredentials(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	pullOptions, err := svc.PullOptionsWithAuth(t.Context(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuth(t, pullOptions.RegistryAuth)
	assert.Equal(t, "docker-user", authCfg.Username)
	assert.Equal(t, "docker-token", authCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", authCfg.ServerAddress)
}

func TestGetPullOptionsWithAuth_ExternalCredentialsOverrideDBRegistry(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://registry.example.com", "db-user", "db-token")

	pullOptions, err := svc.PullOptionsWithAuth(t.Context(), "registry.example.com/team/app:latest", []types.ContainerRegistryCredential{
		{
			URL:      "https://registry.example.com",
			Username: "external-user",
			Token:    "external-token",
			Enabled:  true,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuth(t, pullOptions.RegistryAuth)
	assert.Equal(t, "external-user", authCfg.Username)
	assert.Equal(t, "external-token", authCfg.Password)
	assert.Equal(t, "registry.example.com", authCfg.ServerAddress)
}

func TestImageServicePullImageRetriesAnonymouslyAfterAuthRejected(t *testing.T) {
	db := setupImageProjectTestDB(t)
	eventService := event.NewEventService(db, nil, nil, nil)
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/images/create") {
			http.NotFound(w, r)
			return
		}
		assert.True(t, eventService.ShouldSuppressDaemonEvent("image", "", "registry.example.com/team/app:latest", ""))
		assert.False(t, eventService.ShouldSuppressDaemonEvent("image", "", "registry.example.com/team/other:latest", ""))
		authHeaders = append(authHeaders, r.Header.Get(dockerregistry.AuthHeader))
		if len(authHeaders) == 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("{\"status\":\"Pulled anonymously\"}\n"))
	}))
	t.Cleanup(server.Close)

	dockerService := (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server))
	imageSvc := NewImageService(db, dockerService, nil, nil, nil, eventService, nil, nil, nil)

	err := imageSvc.PullImage(t.Context(), "registry.example.com/team/app:latest", io.Discard, types.SystemUser, []types.ContainerRegistryCredential{
		{
			URL:      "https://registry.example.com",
			Username: "external-user",
			Token:    "external-token",
			Enabled:  true,
		},
	})
	require.NoError(t, err)
	require.Len(t, authHeaders, 2)
	assert.NotEmpty(t, authHeaders[0])
	assert.Empty(t, authHeaders[1])
}

func TestImageServiceTagImageCallsDockerAPI(t *testing.T) {
	db := setupImageProjectTestDB(t)
	eventService := event.NewEventService(db, nil, nil, nil)
	var gotRepo, gotTag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || imageDockerTestPath(r.URL.Path) != "/images/source:latest/tag" {
			http.NotFound(w, r)
			return
		}
		assert.True(t, eventService.ShouldSuppressDaemonEvent("image", "new-image-id", "registry.example.com/team/app:v2", ""))
		gotRepo = r.URL.Query().Get("repo")
		gotTag = r.URL.Query().Get("tag")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, eventService, nil, nil, nil)

	err := imageSvc.TagImage(t.Context(), "source:latest", types.ImageTagRequest{Repository: "registry.example.com/team/app", Tag: "v2"}, types.SystemUser)
	require.NoError(t, err)
	assert.Equal(t, "registry.example.com/team/app", gotRepo)
	assert.Equal(t, "v2", gotTag)
}

func TestImageServiceGetImageHistoryCallsDockerAPI(t *testing.T) {
	db := setupImageProjectTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPath(r.URL.Path) != "/images/source:latest/history" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"Id":        "layer-1",
				"Created":   1710000000,
				"CreatedBy": "/bin/sh",
				"Size":      123,
			},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, event.NewEventService(db, nil, nil, nil), nil, nil, nil)

	history, err := imageSvc.GetImageHistory(t.Context(), "source:latest")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "layer-1", history[0].ID)
	assert.Equal(t, int64(123), history[0].Size)
}

func TestImageServiceSearchImagesRequiresTerm(t *testing.T) {
	imageSvc := NewImageService(nil, &docker.DockerClientService{}, nil, nil, nil, nil, nil, nil, nil)

	_, err := imageSvc.SearchImages(t.Context(), " ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search term is required")
}

func TestImageServiceExportImageReturnsTarStream(t *testing.T) {
	db := setupImageProjectTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPath(r.URL.Path) != "/images/get" {
			http.NotFound(w, r)
			return
		}
		names := r.URL.Query()["names"]
		if !assert.Equal(t, []string{"source:latest"}, names) {
			return
		}
		_, _ = w.Write([]byte("tar-bytes"))
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, event.NewEventService(db, nil, nil, nil), nil, nil, nil)

	reader, err := imageSvc.ExportImage(t.Context(), "source:latest")
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "tar-bytes", string(body))
}

func TestImageServiceSearchImagesCallsDockerAPI(t *testing.T) {
	db := setupImageProjectTestDB(t)
	var gotTerm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPath(r.URL.Path) != "/images/search" {
			http.NotFound(w, r)
			return
		}
		gotTerm, _ = url.QueryUnescape(r.URL.Query().Get("term"))
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":        "library/nginx",
				"description": "web server",
				"star_count":  1,
				"is_official": true,
			},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, (&docker.DockerClientService{}).WithClient(newTestDockerClient(t, server)), nil, nil, nil, event.NewEventService(db, nil, nil, nil), nil, nil, nil)

	results, err := imageSvc.SearchImages(t.Context(), "nginx")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "nginx", gotTerm)
	assert.Equal(t, "library/nginx", results[0].Name)
	assert.True(t, results[0].Official)
}

func TestShouldRetryAnonymousPull_UnauthorizedWithAuth(t *testing.T) {
	err := errors.New(`error response from daemon: Head "registry-1.docker.io/v2/library/nginx/manifests/latest": unauthorized: incorrect username or password`)

	assert.True(t, ShouldRetryAnonymousPull(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, err))
}

func TestShouldRetryAnonymousPull_SkipsRetryWithoutUnauthorizedOrAuth(t *testing.T) {
	nonAuthErr := errors.New("error response from daemon: i/o timeout")
	unauthorizedErr := errors.New("unauthorized: authentication required")

	assert.False(t, ShouldRetryAnonymousPull(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, nonAuthErr))
	assert.False(t, ShouldRetryAnonymousPull(client.ImagePullOptions{}, unauthorizedErr))
}

// decodeRegistryAuth decodes a base64 X-Registry-Auth header.
func decodeRegistryAuth(t *testing.T, encoded string) dockerregistry.AuthConfig {
	t.Helper()

	cfg, err := authconfig.Decode(encoded)
	require.NoError(t, err)
	return *cfg
}

// newTestDockerClient builds a Docker client pointed at a fake Docker HTTP server.
func newTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func (testProjectRow) TableName() string { return "projects" }

func TestImageServiceContainerTagUpdatesStayScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&imageupdate.ImageUpdateRecord{}))
	svc := NewImageService(&database.DB{DB: db}, nil, nil, nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	firstLabels := map[string]string{
		"com.getarcaneapp.arcane.updater.strategy":   "tag",
		"com.getarcaneapp.arcane.updater.constraint": "3.x",
	}
	secondLabels := map[string]string{
		"com.getarcaneapp.arcane.updater.strategy":   "tag",
		"com.getarcaneapp.arcane.updater.constraint": "4.x",
	}
	firstTarget, secondTarget := "3.2.0", "4.0.0"
	records := []imageupdate.ImageUpdateRecord{
		{
			ID:             "shared-image",
			Repository:     "docker.io/library/example",
			Tag:            "3.1.0",
			CurrentVersion: "3.1.0",
			UpdateType:     "digest",
			CheckTime:      now,
		},
		{
			ID: "container::first",
			PolicyKey: imageref.UpdatePolicyKey(
				"example:3.1.0",
				firstLabels,
			),
			ContainerID:    "first",
			ImageID:        "shared-image",
			Repository:     "docker.io/library/example",
			Tag:            "3.1.0",
			CurrentVersion: "3.1.0",
			LatestVersion:  &firstTarget,
			HasUpdate:      true,
			UpdateType:     "tag",
			CheckTime:      now.Add(time.Second),
		},
		{
			ID: "container::second",
			PolicyKey: imageref.UpdatePolicyKey(
				"example:3.1.0",
				secondLabels,
			),
			ContainerID:    "second",
			ImageID:        "shared-image",
			Repository:     "docker.io/library/example",
			Tag:            "3.1.0",
			CurrentVersion: "3.1.0",
			LatestVersion:  &secondTarget,
			HasUpdate:      true,
			UpdateType:     "tag",
			CheckTime:      now.Add(2 * time.Second),
		},
	}
	require.NoError(t, db.Create(&records).Error)
	scoped, err := svc.GetUpdateInfoByContainers(
		t.Context(),
		[]container.Summary{
			{
				ID:     "first",
				Image:  "example:3.1.0",
				Labels: firstLabels,
			},
			{
				ID:     "second",
				Image:  "example:3.1.0",
				Labels: secondLabels,
			},
			{ID: "unseen"},
		},
	)
	require.NoError(t, err)
	require.Len(t, scoped, 2)
	require.Equal(t, firstTarget, scoped["first"].LatestVersion)
	require.Equal(t, secondTarget, scoped["second"].LatestVersion)
	byImage, err := svc.GetUpdateInfoByImageIDs(t.Context(), []string{"shared-image"})
	require.NoError(t, err)
	require.False(t, byImage["shared-image"].HasUpdate)
	require.Equal(t, "digest", byImage["shared-image"].UpdateType)
	byRef, err := svc.GetUpdateInfoByImageRefs(t.Context(), []string{"example:3.1.0"})
	require.NoError(t, err)
	require.False(t, byRef["example:3.1.0"].HasUpdate)
	require.Empty(t, byRef["example:3.1.0"].LatestVersion)
	for _, ordered := range [][]imageupdate.ImageUpdateRecord{records, {records[2], records[1], records[0]}} {
		aggregate := buildUpdateMap(ordered)["shared-image"]
		require.True(t, aggregate.HasUpdate)
		require.Empty(t, aggregate.CurrentVersion)
		require.Nil(t, aggregate.LatestVersion)
		require.Nil(t, aggregate.LatestDigest)
		require.Empty(t, aggregate.UpdateType)
		require.Equal(t, records[2].CheckTime, aggregate.CheckTime)
	}
	for _, currentLabels := range []map[string]string{
		{},
		{"com.getarcaneapp.arcane.updater.strategy": "digest"},
		{
			"com.getarcaneapp.arcane.updater.strategy":   "tag",
			"com.getarcaneapp.arcane.updater.constraint": "3.1.x",
		},
		{
			"com.getarcaneapp.arcane.updater.strategy":    "tag",
			"com.getarcaneapp.arcane.updater.constraint":  "3.x",
			"com.getarcaneapp.arcane.updater.tag-pattern": ".*",
		},
		{
			"com.getarcaneapp.arcane.updater.strategy":   "tag",
			"com.getarcaneapp.arcane.updater.constraint": "3.x",
			imageref.UpdateCheckLabel:                    "false",
		},
	} {
		checks, getUpdateInfoByContainersErr := svc.GetUpdateInfoByContainers(
			t.Context(),
			[]container.Summary{
				{
					ID:     "first",
					Image:  "example:3.1.0",
					Labels: currentLabels,
				},
				{
					ID:     "second",
					Image:  "example:3.1.0",
					Labels: secondLabels,
				},
			},
		)
		require.NoError(t, getUpdateInfoByContainersErr)
		require.NotContains(t, checks, "first")
		require.Contains(t, checks, "second")
	}
	// Disabling automatic installation does not change the monitoring policy,
	// so the stored check stays visible (#3532).
	installExcluded := map[string]string{
		"com.getarcaneapp.arcane.updater.strategy":   "tag",
		"com.getarcaneapp.arcane.updater.constraint": "3.x",
		"com.getarcaneapp.arcane.updater":            "false",
	}
	checks, err := svc.GetUpdateInfoByContainers(t.Context(), []container.Summary{
		{
			ID:     "first",
			Image:  "example:3.1.0",
			Labels: installExcluded,
		},
	})
	require.NoError(t, err)
	require.Equal(t, firstTarget, checks["first"].LatestVersion)

	// A digest check recorded under another local tag of the running image
	// applies to every digest-policy container using that image ID.
	require.NoError(
		t,
		db.Create(&imageupdate.ImageUpdateRecord{
			ID:             "moving-image",
			Repository:     "docker.io/library/app",
			Tag:            "stable",
			CurrentVersion: "stable",
			HasUpdate:      true,
			UpdateType:     "digest",
			CheckTime:      now,
		}).Error,
	)
	digestContainers := []container.Summary{
		{
			ID:      "moving",
			Image:   "app:latest",
			ImageID: "moving-image",
		},
		{
			ID:      "moving-twin",
			Image:   "app:latest",
			ImageID: "moving-image",
			Labels:  map[string]string{"com.getarcaneapp.arcane.updater": "false"},
		},
		{
			ID:      "moving-unmonitored",
			Image:   "app:latest",
			ImageID: "moving-image",
			Labels:  map[string]string{imageref.UpdateCheckLabel: "false"},
		},
		{
			ID:      "moving-tag",
			Image:   "app:latest",
			ImageID: "moving-image",
			Labels:  map[string]string{"com.getarcaneapp.arcane.updater.strategy": "tag"},
		},
		{ID: "moving-no-id", Image: "app:latest"},
	}
	checks, err = svc.GetUpdateInfoByContainers(t.Context(), digestContainers)
	require.NoError(t, err)
	require.True(t, checks["moving"].HasUpdate)
	require.True(t, checks["moving-twin"].HasUpdate, "disabling automatic installation keeps the check visible")
	require.NotContains(t, checks, "moving-unmonitored", "disabling update checks suppresses the result")
	require.NotContains(t, checks, "moving-tag", "a tag policy never falls back to the shared digest check")
	require.NotContains(t, checks, "moving-no-id", "a missing runtime image ID matches nothing")
	require.NotSame(t, checks["moving"], checks["moving-twin"], "each container receives its own copy")
}

func TestPatchWorkflowDefinitions(t *testing.T) {
	harness := flowtest.New(t, nil)
	require.NoError(t, NewImageService(nil, nil, nil, nil, nil, nil, nil, nil, nil).RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)
}

func imageScopeContext(t *testing.T, target string, bodies map[string]any) (context.Context, *sync.Map) {
	t.Helper()
	var queries sync.Map
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]
			queries.Store(id, r.URL.Query())
			body, ok := bodies[id]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	}), &queries
}

func TestAggregateImagesMergesEnvironments(t *testing.T) {
	bodies := map[string]any{
		"a": types.ImageListResponse{
			Success: true,
			Data: []types.ImageSummary{
				{
					ID:   "a1",
					Repo: "nginx",
					Tag:  "latest",
				},
				{
					ID:   "a2",
					Repo: "redis",
					Tag:  "7",
				},
			},
			Pagination:         types.PaginationResponse{TotalItems: 2, GrandTotalItems: 4},
			MaxImageUploadSize: 500,
		},
		"b": types.ImageListResponse{
			Success: true,
			Data: []types.ImageSummary{
				{
					ID:   "b1",
					Repo: "postgres",
					Tag:  "16",
				},
			},
			Pagination:         types.PaginationResponse{TotalItems: 1},
			MaxImageUploadSize: 100,
		},
	}
	rowLabels := func(items []types.ImageSummary) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.Repo)
		}
		return out
	}
	svc := NewImageService(nil, nil, nil, nil, nil, nil, nil, nil, nil)

	t.Run("sorts by repo across environments and pages", func(t *testing.T) {
		ctx, queries := imageScopeContext(t, "/api/environments/a,b,c/images?start=0&limit=2&sort=repo&order=asc", bodies)
		body, err := svc.AggregateImages(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "repo", Order: pagination.SortAsc}, Params: pagination.Params{Limit: 2}})
		require.NoError(t, err)

		assert.True(t, body.Success)
		assert.Equal(t, []string{"a/nginx", "b/postgres"}, rowLabels(body.Data))
		assert.Equal(t, "Env b", body.Data[1].EnvironmentName)
		assert.Equal(t, int64(3), body.Pagination.TotalItems)
		assert.Equal(t, int64(5), body.Pagination.GrandTotalItems)
		assert.Equal(t, int64(2), body.Pagination.TotalPages)
		assert.Equal(t, 500, body.MaxImageUploadSize)
		require.Len(t, body.FailedEnvironments, 1)
		assert.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
		assert.Equal(t, "environment offline", body.FailedEnvironments[0].Message)
		for _, id := range []string{"a", "b"} {
			query, ok := queries.Load(id)
			require.True(t, ok)
			assert.Equal(t, "0", query.(url.Values).Get("start"))
			assert.Equal(t, "-1", query.(url.Values).Get("limit"))
		}
	})

	t.Run("sorts by environment", func(t *testing.T) {
		ctx, _ := imageScopeContext(t, "/api/environments/a,b,c/images?sort=environment&order=desc", bodies)
		body, err := svc.AggregateImages(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "environment", Order: pagination.SortDesc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		assert.Equal(t, []string{"b/postgres", "a/nginx", "a/redis"}, rowLabels(body.Data))
	})

	t.Run("upload limit comes from the first answering environment", func(t *testing.T) {
		ctx, _ := imageScopeContext(t, "/api/environments/a,b,c/images", map[string]any{"b": bodies["b"]})
		body, err := svc.AggregateImages(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		assert.Equal(t, 100, body.MaxImageUploadSize)
		assert.Len(t, body.FailedEnvironments, 2)
	})

	t.Run("all environments failing returns empty data", func(t *testing.T) {
		ctx, _ := imageScopeContext(t, "/api/environments/a,b,c/images", map[string]any{})
		body, err := svc.AggregateImages(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		assert.Empty(t, body.Data)
		assert.Len(t, body.FailedEnvironments, 3)
	})
}

func TestAggregateImageCountsSumsEnvironments(t *testing.T) {
	ctx, _ := imageScopeContext(t, "/api/environments/a,b,c/images/counts", map[string]any{
		"a": types.ApiResponse[types.ImageUsageCounts]{
			Success: true,
			Data: types.ImageUsageCounts{
				Inuse:     1,
				Unused:    2,
				Total:     3,
				TotalSize: 100,
			},
		},
		"b": types.ApiResponse[types.ImageUsageCounts]{
			Success: true,
			Data: types.ImageUsageCounts{
				Inuse:     2,
				Total:     2,
				TotalSize: 50,
			},
		},
	})

	body, err := NewImageService(nil, nil, nil, nil, nil, nil, nil, nil, nil).AggregateImageCounts(ctx)
	require.NoError(t, err)

	assert.True(t, body.Success)
	assert.Equal(t, types.ImageUsageCounts{
		Inuse:     3,
		Unused:    2,
		Total:     5,
		TotalSize: 150,
	}, body.Data)
	require.Len(t, body.FailedEnvironments, 1)
	assert.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
}
