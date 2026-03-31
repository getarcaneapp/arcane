package swarm

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePathWithinWorkingDirInternal_AllowsPathsWithinWorkingDir(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "tmp", "stack")

	path, err := resolvePathWithinWorkingDirInternal(workingDir, filepath.Join("configs", "app.env"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(workingDir, "configs", "app.env"), path)
}

func TestResolvePathWithinWorkingDirInternal_RejectsEscapingPaths(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "tmp", "stack")

	_, err := resolvePathWithinWorkingDirInternal(workingDir, filepath.Join("..", "..", "etc", "shadow"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes the working directory")
}
