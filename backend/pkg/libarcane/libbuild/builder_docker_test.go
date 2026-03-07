package libbuild

import (
	"os"
	"path/filepath"
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
	assert.Empty(t, buildOpts.Version)
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

func TestPrepareDockerBuildContextInternal_StagesInlineDockerfile(t *testing.T) {
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "app.txt"), []byte("hello\n"), 0o644))

	req := imagetypes.BuildRequest{
		ContextDir:       contextDir,
		DockerfileInline: "FROM alpine:3.20\nCOPY app.txt /app.txt\n",
	}

	input, reportProgress, err := prepareDockerBuildInputInternal(req)
	require.NoError(t, err)
	assert.False(t, reportProgress)

	buildContextDir, dockerfileForBuild, cleanup, err := prepareDockerBuildContextInternal(input)
	require.NoError(t, err)
	defer cleanup()

	contents, err := os.ReadFile(filepath.Join(buildContextDir, filepath.FromSlash(dockerfileForBuild)))
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine:3.20\nCOPY app.txt /app.txt\n", string(contents))

	appContents, err := os.ReadFile(filepath.Join(buildContextDir, "app.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(appContents))
}

func TestPrepareDockerBuildContextInternal_StagesDockerfileExcludedByDockerignore(t *testing.T) {
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM alpine:3.20\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, ".dockerignore"), []byte("**/Dockerfile*\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "app.txt"), []byte("hello\n"), 0o644))

	req := imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
	}

	input, reportProgress, err := prepareDockerBuildInputInternal(req)
	require.NoError(t, err)
	assert.False(t, reportProgress)

	buildContextDir, dockerfileForBuild, cleanup, err := prepareDockerBuildContextInternal(input)
	require.NoError(t, err)
	defer cleanup()

	assert.NotEqual(t, contextDir, buildContextDir)
	assert.Equal(t, ".arcane.external.Dockerfile", dockerfileForBuild)

	contents, err := os.ReadFile(filepath.Join(buildContextDir, dockerfileForBuild))
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine:3.20\n", string(contents))

	appContents, err := os.ReadFile(filepath.Join(buildContextDir, "app.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(appContents))
}
