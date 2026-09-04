package image

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"

	"github.com/libtnb/sqlite"
	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/vulnerability"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"go.getarcane.app/sys/crypto"
)

var imageDockerAPIVersionPrefixInternal = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

func imageDockerTestPathInternal(path string) string {
	return imageDockerAPIVersionPrefixInternal.ReplaceAllString(path, "")
}

func setupImageProjectTestDBInternal(t *testing.T) *database.DB {
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

func TestCollectPinnedReferencesByImageIDInternal(t *testing.T) {
	containers := []dockercontainer.Summary{
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

	result := collectPinnedReferencesByImageIDInternal(containers)
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

func TestMapDockerImagesToDTOs_PopulatesPinnedReferencesInternal(t *testing.T) {
	pinnedRef := "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"
	dockerImages := []dockertypesimage.Summary{
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
	containers := []dockercontainer.Summary{
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

func TestImageService_GetImageDetail_EnrichesPinnedReferencesInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	const pinnedID = "sha256:test-pinned-detail"
	pinnedRef := "ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_ = json.NewEncoder(w).Encode([]dockertypesimage.Summary{
				{ID: pinnedID, Size: 2048},
			})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(dockertypesimage.InspectResponse{
				ID:          pinnedID,
				RepoTags:    []string{},
				RepoDigests: []string{"ghcr.io/syncthing/syncthing@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"},
				Size:        2048,
			})
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_ = json.NewEncoder(w).Encode([]dockercontainer.Summary{
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

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	detail, err := imageSvc.GetImageDetail(context.Background(), pinnedID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, pinnedID, detail.ID)
	assert.Equal(t, []string{pinnedRef}, detail.PinnedReferences)
}

func TestImageService_GetImageDetail_ContainerFailureDoesNotBreakInspectionInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	const testID = "sha256:test-detail-resilient"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			_ = json.NewEncoder(w).Encode([]dockertypesimage.Summary{
				{ID: testID, Size: 1024},
			})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(dockertypesimage.InspectResponse{
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

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	detail, err := imageSvc.GetImageDetail(context.Background(), testID)
	require.NoError(t, err)
	require.NotNil(t, detail)
	assert.Equal(t, testID, detail.ID)
	assert.Nil(t, detail.PinnedReferences)
}

func TestImagePaginationConfig_SearchesPinnedReferencesInternal(t *testing.T) {
	svc := &ImageService{}
	config := svc.getImagePaginationConfig()

	item := imagetypes.Summary{
		ID:               "sha256:syncthing",
		Repo:             "ghcr.io/syncthing/syncthing",
		Tag:              "<none>",
		PinnedReferences: []string{"ghcr.io/syncthing/syncthing:2.1.3@sha256:8c8ff37ab6aa8be23b700648a90fa9412e214852e9fd6ea8477c8334792daec0"},
	}

	res := config.SearchOrderAndPaginate([]imagetypes.Summary{item}, pagination.QueryParams{
		Search: "8c8ff37",
		Limit:  10,
	})
	require.Len(t, res.Items, 1)

	resTag := config.SearchOrderAndPaginate([]imagetypes.Summary{item}, pagination.QueryParams{
		Search: "2.1.3",
		Limit:  10,
	})
	require.Len(t, resTag.Items, 1)
}

func TestImageService_GetUpdateInfoByImageRefs_MatchesCanonicalAndFamiliarRepos(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&imageupdate.ImageUpdateRecord{}))

	svc := &ImageService{db: &database.DB{DB: db}}
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

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&registry.ContainerRegistry{}, &kv.KVEntry{}, &testProjectRow{}))

	crypto.InitEncryption(&crypto.Config{
		Environment:   string(config.AppEnvironmentTest),
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
	})

	dbWrap := &database.DB{DB: db}
	svc := &ImageService{
		registryService: registry.NewContainerRegistryService(dbWrap, nil, kv.NewKVService(dbWrap)),
	}

	return svc, dbWrap
}

func createTestPullRegistry(t *testing.T, db *database.DB, url, username, token string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt(token)
	require.NoError(t, err)

	reg := &registry.ContainerRegistry{
		URL:          url,
		Username:     username,
		Token:        encryptedToken,
		Enabled:      true,
		RegistryType: registry.RegistryTypeGeneric,
	}
	require.NoError(t, db.WithContext(context.Background()).Create(reg).Error)
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyToken(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "docker-user", "   ")

	pullOptions, err := svc.PullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistrySkipsEmptyUsername(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "   ", "docker-token")

	pullOptions, err := svc.PullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, pullOptions.RegistryAuth)
}

func TestGetPullOptionsWithAuth_DBRegistryUsesValidCredentials(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	pullOptions, err := svc.PullOptionsWithAuth(context.Background(), "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuthInternal(t, pullOptions.RegistryAuth)
	assert.Equal(t, "docker-user", authCfg.Username)
	assert.Equal(t, "docker-token", authCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", authCfg.ServerAddress)
}

func TestGetPullOptionsWithAuth_ExternalCredentialsOverrideDBRegistryInternal(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://registry.example.com", "db-user", "db-token")

	pullOptions, err := svc.PullOptionsWithAuth(context.Background(), "registry.example.com/team/app:latest", []containerregistry.Credential{
		{URL: "https://registry.example.com", Username: "external-user", Token: "external-token", Enabled: true},
	})
	require.NoError(t, err)
	require.NotEmpty(t, pullOptions.RegistryAuth)

	authCfg := decodeRegistryAuthInternal(t, pullOptions.RegistryAuth)
	assert.Equal(t, "external-user", authCfg.Username)
	assert.Equal(t, "external-token", authCfg.Password)
	assert.Equal(t, "registry.example.com", authCfg.ServerAddress)
}

func TestImageServicePullImageRetriesAnonymouslyAfterAuthRejectedInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	var authHeaders []string
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

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	imageSvc := NewImageService(db, dockerService, nil, nil, nil, event.NewEventService(db, nil, nil))

	err := imageSvc.PullImage(context.Background(), "registry.example.com/team/app:latest", io.Discard, common.SystemUser, []containerregistry.Credential{
		{URL: "https://registry.example.com", Username: "external-user", Token: "external-token", Enabled: true},
	})
	require.NoError(t, err)
	require.Len(t, authHeaders, 2)
	assert.NotEmpty(t, authHeaders[0])
	assert.Empty(t, authHeaders[1])
}

func TestImageServiceTagImageCallsDockerAPIInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	var gotRepo, gotTag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || imageDockerTestPathInternal(r.URL.Path) != "/images/source:latest/tag" {
			http.NotFound(w, r)
			return
		}
		gotRepo = r.URL.Query().Get("repo")
		gotTag = r.URL.Query().Get("tag")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	err := imageSvc.TagImage(context.Background(), "source:latest", imagetypes.TagRequest{Repository: "registry.example.com/team/app", Tag: "v2"}, common.SystemUser)
	require.NoError(t, err)
	assert.Equal(t, "registry.example.com/team/app", gotRepo)
	assert.Equal(t, "v2", gotTag)
}

func TestImageServiceGetImageHistoryCallsDockerAPIInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPathInternal(r.URL.Path) != "/images/source:latest/history" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Id": "layer-1", "Created": 1710000000, "CreatedBy": "/bin/sh", "Size": 123},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	history, err := imageSvc.GetImageHistory(context.Background(), "source:latest")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "layer-1", history[0].ID)
	assert.Equal(t, int64(123), history[0].Size)
}

func TestImageServiceSearchImagesRequiresTermInternal(t *testing.T) {
	imageSvc := NewImageService(nil, &docker.DockerClientService{}, nil, nil, nil, nil)

	_, err := imageSvc.SearchImages(context.Background(), " ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search term is required")
}

func TestImageServiceExportImageReturnsTarStreamInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPathInternal(r.URL.Path) != "/images/get" {
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

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	reader, err := imageSvc.ExportImage(context.Background(), "source:latest")
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "tar-bytes", string(body))
}

func TestImageServiceSearchImagesCallsDockerAPIInternal(t *testing.T) {
	db := setupImageProjectTestDBInternal(t)
	var gotTerm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || imageDockerTestPathInternal(r.URL.Path) != "/images/search" {
			http.NotFound(w, r)
			return
		}
		gotTerm, _ = url.QueryUnescape(r.URL.Query().Get("term"))
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "library/nginx", "description": "web server", "star_count": 1, "is_official": true},
		})
	}))
	t.Cleanup(server.Close)

	imageSvc := NewImageService(db, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, event.NewEventService(db, nil, nil))

	results, err := imageSvc.SearchImages(context.Background(), "nginx")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "nginx", gotTerm)
	assert.Equal(t, "library/nginx", results[0].Name)
	assert.True(t, results[0].Official)
}

func TestShouldRetryAnonymousPullInternal_UnauthorizedWithAuth(t *testing.T) {
	err := errors.New(`error response from daemon: Head "registry-1.docker.io/v2/library/nginx/manifests/latest": unauthorized: incorrect username or password`)

	assert.True(t, ShouldRetryAnonymousPull(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, err))
}

func TestShouldRetryAnonymousPullInternal_SkipsRetryWithoutUnauthorizedOrAuth(t *testing.T) {
	nonAuthErr := errors.New("error response from daemon: i/o timeout")
	unauthorizedErr := errors.New("unauthorized: authentication required")

	assert.False(t, ShouldRetryAnonymousPull(client.ImagePullOptions{RegistryAuth: "encoded-auth"}, nonAuthErr))
	assert.False(t, ShouldRetryAnonymousPull(client.ImagePullOptions{}, unauthorizedErr))
}

// Test fixtures shared by this package's tests.

// decodeRegistryAuthInternal decodes a base64 X-Registry-Auth header.
func decodeRegistryAuthInternal(t *testing.T, encoded string) dockerregistry.AuthConfig {
	t.Helper()

	cfg, err := dockerauthconfig.Decode(encoded)
	require.NoError(t, err)
	return *cfg
}

// newTestDockerClientInternal builds a Docker client pointed at a fake Docker HTTP server.
func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
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

// newImagePullServerWithObserverInternal is NewImagePullServer with a pull callback.
func newImagePullServerWithObserverInternal(t *testing.T, inspectByRef map[string]dockertypesimage.InspectResponse, onPull func(fullRef string, authHeader string)) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/create"):
			fullRef := strings.TrimSpace(r.URL.Query().Get("fromImage"))
			tag := strings.TrimSpace(r.URL.Query().Get("tag"))
			if fullRef != "" && tag != "" {
				lastSlash := strings.LastIndex(fullRef, "/")
				lastColon := strings.LastIndex(fullRef, ":")
				if lastColon <= lastSlash {
					fullRef += ":" + tag
				}
			}
			if onPull != nil {
				onPull(fullRef, strings.TrimSpace(r.Header.Get("X-Registry-Auth")))
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "Pulled", "id": fullRef})
			return
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			path := r.URL.Path
			imagePathIndex := strings.Index(path, "/images/")
			if !assert.NotEqual(t, -1, imagePathIndex) {
				return
			}
			encodedRef := strings.TrimSuffix(path[imagePathIndex+len("/images/"):], "/json")
			imageRef, err := url.PathUnescape(encodedRef)
			if !assert.NoError(t, err) {
				return
			}

			inspect, ok := inspectByRef[imageRef]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(inspect)) {
				return
			}
			return
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)

	return server
}

// testProjectRow is a minimal stand-in for project.Project: the project
// package imports this one, so the in-package test cannot import it back.
type testProjectRow struct {
	database.BaseModel
	Name               string
	Path               string
	BuildImageRefsJSON *string `gorm:"column:build_image_refs_json"`
}

func (testProjectRow) TableName() string { return "projects" }
