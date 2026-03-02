package services

import (
	"context"
	"errors"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/getarcaneapp/arcane/types/vulnerability"
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

func setupImageServiceAuthTest(t *testing.T) (*ImageService, *database.DB) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ContainerRegistry{}))

	crypto.InitEncryption(&config.Config{
		Environment:   config.AppEnvironmentTest,
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
	})

	dbWrap := &database.DB{DB: db}
	svc := &ImageService{
		registryService: NewContainerRegistryService(dbWrap),
	}

	return svc, dbWrap
}

func createTestPullRegistry(t *testing.T, db *database.DB, url, username, token string) {
	t.Helper()

	encryptedToken, err := crypto.Encrypt(token)
	require.NoError(t, err)

	reg := &models.ContainerRegistry{
		URL:      url,
		Username: username,
		Token:    encryptedToken,
		Enabled:  true,
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
