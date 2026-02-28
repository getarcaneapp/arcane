package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerRegistryService_GetAllRegistryAuthConfigs_NormalizesHosts(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")
	createTestPullRegistry(t, db, "https://GHCR.IO/", "gh-user", "gh-token")

	svc := NewContainerRegistryService(db)
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

	ghcrCfg, ok := authConfigs["ghcr.io"]
	require.True(t, ok)
	assert.Equal(t, "gh-user", ghcrCfg.Username)
	assert.Equal(t, "gh-token", ghcrCfg.Password)
	assert.Equal(t, "ghcr.io", ghcrCfg.ServerAddress)
}

func TestContainerRegistryService_GetAllRegistryAuthConfigs_SkipsInvalidEntries(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "  ", "docker-token")
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "   ")
	createTestPullRegistry(t, db, "https://registry.example.com", "example-user", "example-token")

	svc := NewContainerRegistryService(db)
	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, authConfigs)

	assert.NotContains(t, authConfigs, "docker.io")
	assert.NotContains(t, authConfigs, "ghcr.io")

	exampleCfg, ok := authConfigs["registry.example.com"]
	require.True(t, ok)
	assert.Equal(t, "example-user", exampleCfg.Username)
	assert.Equal(t, "example-token", exampleCfg.Password)
	assert.Equal(t, "registry.example.com", exampleCfg.ServerAddress)
}
