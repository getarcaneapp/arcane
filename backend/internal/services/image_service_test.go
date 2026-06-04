package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dockersdkimage "github.com/docker/go-sdk/image"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/getarcaneapp/arcane/types/vulnerability"
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

func TestResolveManagedPullCredentialsInternal_DBRegistrySkipsEmptyToken(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "docker-user", "   ")

	username, token, ok, err := resolveManagedPullCredentialsInternal(context.Background(), svc.registryService, "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, username)
	assert.Empty(t, token)
	assert.False(t, ok)
}

func TestResolveManagedPullCredentialsInternal_DBRegistrySkipsEmptyUsername(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "   ", "docker-token")

	username, token, ok, err := resolveManagedPullCredentialsInternal(context.Background(), svc.registryService, "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, username)
	assert.Empty(t, token)
	assert.False(t, ok)
}

func TestResolveManagedPullCredentialsInternal_DBRegistryUsesValidCredentials(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	username, token, ok, err := resolveManagedPullCredentialsInternal(context.Background(), svc.registryService, "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "docker-user", username)
	assert.Equal(t, "docker-token", token)
}

func TestResolveManagedPullCredentialsInternal_PublicImageUsesNoCredentials(t *testing.T) {
	svc, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://registry.example.com", "registry-user", "registry-token")

	username, token, ok, err := resolveManagedPullCredentialsInternal(context.Background(), svc.registryService, "docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Empty(t, username)
	assert.Empty(t, token)
	assert.False(t, ok)
}

func TestResolveExternalPullCredentialsInternal_MatchesRegistryHost(t *testing.T) {
	username, password, ok := resolveExternalPullCredentialsInternal("ghcr.io/getarcaneapp/arcane:latest", []containerregistry.Credential{
		{URL: "https://ghcr.io", Username: "gh-user", Token: "gh-token", Enabled: true},
		{URL: "https://docker.io", Username: "docker-user", Token: "docker-token", Enabled: true},
	})

	require.True(t, ok)
	assert.Equal(t, "gh-user", username)
	assert.Equal(t, "gh-token", password)
}

func TestImageService_PullImageInternal_RetriesAnonymouslyOnUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	svc := &ImageService{
		dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
	}

	originalPull := dockerSDKImagePullInternal
	t.Cleanup(func() {
		dockerSDKImagePullInternal = originalPull
	})

	calls := 0
	dockerSDKImagePullInternal = func(ctx context.Context, imageName string, opts ...dockersdkimage.PullOption) error {
		calls++
		if calls == 1 {
			return errors.New("unauthorized: authentication required")
		}
		return nil
	}

	err := pullImageWithRuntimeCredentialsInternal(context.Background(), svc.dockerService, svc.registryService, "ghcr.io/getarcaneapp/arcane:latest", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestImageService_PullImageInternal_ReconcilesDockerConfigBeforePull(t *testing.T) {
	dockerConfigDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerConfigDir)

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	svc, db := setupImageServiceAuthTest(t)
	svc.dockerService = &DockerClientService{client: newTestDockerClient(t, server)}
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "gh-token")

	originalPull := dockerSDKImagePullInternal
	t.Cleanup(func() {
		dockerSDKImagePullInternal = originalPull
	})

	dockerSDKImagePullInternal = func(ctx context.Context, imageName string, opts ...dockersdkimage.PullOption) error {
		doc := readDockerConfigTestDocument(t, dockerConfigDir)
		require.Contains(t, doc.Auths, "ghcr.io")
		username, password := decodeDockerConfigAuthEntryInternal(t, doc.Auths["ghcr.io"].Auth)
		assert.Equal(t, "gh-user", username)
		assert.Equal(t, "gh-token", password)
		return nil
	}

	require.NoError(t, pullImageWithRuntimeCredentialsInternal(context.Background(), svc.dockerService, svc.registryService, "ghcr.io/getarcaneapp/arcane:latest", nil, nil))
}

func TestImageService_PullImageInternal_DoesNotRetryOnNonUnauthorizedError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	svc := &ImageService{
		dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
	}

	originalPull := dockerSDKImagePullInternal
	t.Cleanup(func() {
		dockerSDKImagePullInternal = originalPull
	})

	calls := 0
	dockerSDKImagePullInternal = func(ctx context.Context, imageName string, opts ...dockersdkimage.PullOption) error {
		calls++
		return errors.New("i/o timeout")
	}

	err := pullImageWithRuntimeCredentialsInternal(context.Background(), svc.dockerService, svc.registryService, "ghcr.io/getarcaneapp/arcane:latest", nil, nil)
	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.Contains(t, err.Error(), "failed to initiate image pull")
}
