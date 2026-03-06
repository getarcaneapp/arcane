package libbuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSolveOptInternal_StagesInlineDockerfile(t *testing.T) {
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "app.txt"), []byte("hello\n"), 0o644))

	b := &builder{}
	req := imagetypes.BuildRequest{
		ContextDir:       contextDir,
		DockerfileInline: "FROM alpine:3.20\nCOPY app.txt /app.txt\n",
		BuildArgs: map[string]string{
			"FOO": "bar",
		},
	}

	solveOpt, loadErrCh, cleanup, err := b.buildSolveOptInternal(context.Background(), req)
	require.NoError(t, err)
	defer cleanup()
	assert.Nil(t, loadErrCh)
	assert.Equal(t, ".arcane.inline.Dockerfile", solveOpt.FrontendAttrs["filename"])

	contextPath := solveOpt.LocalDirs["context"]
	dockerfileDir := solveOpt.LocalDirs["dockerfile"]
	assert.NotEmpty(t, contextPath)
	assert.Equal(t, contextPath, dockerfileDir)

	contents, err := os.ReadFile(filepath.Join(dockerfileDir, solveOpt.FrontendAttrs["filename"]))
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine:3.20\nCOPY app.txt /app.txt\n", string(contents))

	appContents, err := os.ReadFile(filepath.Join(contextPath, "app.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(appContents))
}
