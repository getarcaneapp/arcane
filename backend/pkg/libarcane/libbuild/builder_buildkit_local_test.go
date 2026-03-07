package libbuild

import (
	"os"
	"path/filepath"
	"testing"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerfileRequiresBuildkitInternal(t *testing.T) {
	t.Run("syntax directive enables buildkit", func(t *testing.T) {
		assert.True(t, dockerfileRequiresBuildkitInternal("# syntax=docker/dockerfile:1.7\nFROM alpine:3.20\n"))
	})

	t.Run("run mount enables buildkit", func(t *testing.T) {
		assert.True(t, dockerfileRequiresBuildkitInternal("FROM oven/bun:alpine\nRUN --mount=type=cache,target=/root/.bun bun install\n"))
	})

	t.Run("plain dockerfile stays on classic docker", func(t *testing.T) {
		assert.False(t, dockerfileRequiresBuildkitInternal("FROM alpine:3.20\nRUN echo hello\n"))
	})
}

func TestRequiresLocalBuildkitInternal_ReadsRequestedDockerfile(t *testing.T) {
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "Dockerfile.custom"), []byte("FROM alpine:3.20\nRUN --mount=type=cache,target=/tmp/cache echo hi\n"), 0o644))

	required, err := requiresLocalBuildkitInternal(imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile.custom",
	})
	require.NoError(t, err)
	assert.True(t, required)
}

func TestRequiresLocalBuildkitInternal_UsesInlineDockerfile(t *testing.T) {
	required, err := requiresLocalBuildkitInternal(imagetypes.BuildRequest{
		ContextDir:       t.TempDir(),
		DockerfileInline: "FROM alpine:3.20\nRUN --mount=type=cache,target=/tmp/cache echo hi\n",
	})
	require.NoError(t, err)
	assert.True(t, required)
}
