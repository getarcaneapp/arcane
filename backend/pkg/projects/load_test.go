package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectComposeFile_SupportsPodmanComposeNames(t *testing.T) {
	t.Parallel()

	composeContent := "services:\n  app:\n    image: nginx:alpine\n"

	testCases := []struct {
		name     string
		fileName string
	}{
		{name: "podman-compose.yaml", fileName: "podman-compose.yaml"},
		{name: "podman-compose.yml", fileName: "podman-compose.yml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			expectedPath := filepath.Join(dir, tc.fileName)
			require.NoError(t, os.WriteFile(expectedPath, []byte(composeContent), 0o600))

			composePath, err := DetectComposeFile(dir)
			require.NoError(t, err)
			assert.Equal(t, expectedPath, composePath)
		})
	}
}

func TestLoadComposeProjectFromDir_SupportsPodmanComposeNames(t *testing.T) {
	composeContent := "services:\n  app:\n    image: nginx:alpine\n"

	testCases := []struct {
		name     string
		fileName string
	}{
		{name: "podman-compose.yaml", fileName: "podman-compose.yaml"},
		{name: "podman-compose.yml", fileName: "podman-compose.yml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			expectedPath := filepath.Join(dir, tc.fileName)
			require.NoError(t, os.WriteFile(expectedPath, []byte(composeContent), 0o600))

			project, composePath, err := LoadComposeProjectFromDir(
				context.Background(),
				dir,
				"podman-project",
				filepath.Dir(dir),
				false,
				nil,
			)
			require.NoError(t, err)
			require.NotNil(t, project)

			assert.Equal(t, expectedPath, composePath)
			assert.Equal(t, []string{expectedPath}, project.ComposeFiles)
			assert.NotEmpty(t, project.Services)
		})
	}
}
