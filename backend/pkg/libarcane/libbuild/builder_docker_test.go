package libbuild

import (
	"testing"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareDockerBuildInputInternal_RejectsMultiPlatform(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
		Platforms:  []string{"linux/amd64", "linux/arm64"},
	}

	_, reportProgress, err := prepareDockerBuildInputInternal(req)
	require.Error(t, err)
	assert.True(t, reportProgress)
	assert.Contains(t, err.Error(), "does not support multi-platform builds")
}

func TestBuildDockerImageOptionsInternal_IncludesAuthConfigs(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
		Platforms:  []string{"linux/amd64"},
	}
	input, _, err := prepareDockerBuildInputInternal(req)
	require.NoError(t, err)

	authConfigs := map[string]dockerregistry.AuthConfig{
		"ghcr.io": {
			Username:      "db-user",
			Password:      "db-token",
			ServerAddress: "ghcr.io",
		},
	}

	buildOpts, err := buildDockerImageOptionsInternal(req, input, "Dockerfile", authConfigs)
	require.NoError(t, err)
	require.NotNil(t, buildOpts.AuthConfigs)
	assert.Equal(t, authConfigs, buildOpts.AuthConfigs)
	require.Len(t, buildOpts.Platforms, 1)
	assert.Equal(t, "linux", buildOpts.Platforms[0].OS)
	assert.Equal(t, "amd64", buildOpts.Platforms[0].Architecture)
}

func TestBuildDockerImageOptionsInternal_EmptyAuthConfigsBecomesNil(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
	}
	input, _, err := prepareDockerBuildInputInternal(req)
	require.NoError(t, err)

	buildOpts, err := buildDockerImageOptionsInternal(req, input, "Dockerfile", map[string]dockerregistry.AuthConfig{})
	require.NoError(t, err)
	assert.Nil(t, buildOpts.AuthConfigs)
}
