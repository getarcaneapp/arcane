package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildService_GetRegistryAuthForHost_UsesDatabaseCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db)}

	auth, err := svc.GetRegistryAuthForHost(context.Background(), "registry-1.docker.io")
	require.NoError(t, err)
	require.NotEmpty(t, auth)

	cfg := decodeRegistryAuth(t, auth)
	assert.Equal(t, "docker-user", cfg.Username)
	assert.Equal(t, "docker-token", cfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", cfg.ServerAddress)
}

func TestBuildService_GetRegistryAuthForImage_UsesHostLookup(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "gh-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db)}

	auth, err := svc.GetRegistryAuthForImage(context.Background(), "ghcr.io/getarcaneapp/arcane:latest")
	require.NoError(t, err)
	require.NotEmpty(t, auth)

	cfg := decodeRegistryAuth(t, auth)
	assert.Equal(t, "gh-user", cfg.Username)
	assert.Equal(t, "gh-token", cfg.Password)
	assert.Equal(t, "ghcr.io", cfg.ServerAddress)
}

func TestBuildService_GetAllRegistryAuthConfigs_UsesDatabaseCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db)}

	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, authConfigs)

	dockerCfg, ok := authConfigs["docker.io"]
	require.True(t, ok)
	assert.Equal(t, "docker-user", dockerCfg.Username)
	assert.Equal(t, "docker-token", dockerCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", dockerCfg.ServerAddress)
	assert.Equal(t, dockerCfg, authConfigs["registry-1.docker.io"])
	assert.Equal(t, dockerCfg, authConfigs["index.docker.io"])
}

func TestBuildService_GetAllRegistryAuthConfigs_NoRegistryService(t *testing.T) {
	svc := &BuildService{}

	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	assert.Nil(t, authConfigs)
}
