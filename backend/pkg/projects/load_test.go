package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
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

func TestDetectComposeFile_SupportsSingleCustomComposeName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	expectedPath := filepath.Join(dir, "radarr.yaml")
	require.NoError(t, os.WriteFile(expectedPath, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	composePath, err := DetectComposeFile(dir)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, composePath)
}

func TestDetectComposeFile_PrefersDirectoryMatchedCustomComposeName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "Radarr-3")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	expectedPath := filepath.Join(dir, "radarr.yaml")
	require.NoError(t, os.WriteFile(expectedPath, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x-extra: true\n"), 0o600))

	composePath, err := DetectComposeFile(dir)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, composePath)
}

func TestDetectComposeFile_ReturnsErrorForAmbiguousCustomComposeNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.yaml"), []byte("services:\n  a:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beta.yml"), []byte("services:\n  b:\n    image: busybox:latest\n"), 0o600))

	_, err := DetectComposeFile(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple custom compose files")
}

func TestDetectComposeFile_IgnoresSingleNonComposeYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("replicaCount: 2\nimage:\n  tag: latest\n"), 0o600))

	_, err := DetectComposeFile(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no compose file found")
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

func TestLoadComposeProjectFromDir_SupportsCustomComposeNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	expectedPath := filepath.Join(dir, "radarr.yaml")
	require.NoError(t, os.WriteFile(expectedPath, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	project, composePath, err := LoadComposeProjectFromDir(
		context.Background(),
		dir,
		"radarr",
		filepath.Dir(dir),
		false,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Equal(t, expectedPath, composePath)
	assert.Equal(t, []string{expectedPath}, project.ComposeFiles)
}

func TestLoadComposeProjectFromDir_EmptyProjectsDirectoryDoesNotCreateParentGlobalEnv(t *testing.T) {
	t.Parallel()

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "nested", "services")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	project, composePath, err := LoadComposeProjectFromDir(context.Background(), projectDir, "nested-services", "", false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)

	assert.Equal(t, filepath.Join(projectDir, "compose.yaml"), composePath)

	_, statErr := os.Stat(filepath.Join(projectsRoot, "nested", GlobalEnvFileName))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestLoadComposeProject_UsesProjectLevelComposeLabelsForIncludedServices(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	includePath := filepath.Join(projectDir, "included.compose.yaml")
	composePath := filepath.Join(projectDir, "compose.yaml")

	require.NoError(t, os.WriteFile(includePath, []byte(`services:
  included:
    image: nginx:alpine
`), 0o600))
	require.NoError(t, os.WriteFile(composePath, []byte(`include:
  - included.compose.yaml
services:
  root:
    image: busybox:latest
`), 0o600))

	project, err := LoadComposeProject(context.Background(), composePath, "demo", projectDir, false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)

	rootService := project.Services["root"]
	includedService := project.Services["included"]
	expectedConfigFiles := strings.Join(project.ComposeFiles, ",")

	require.Equal(t, []string{composePath}, project.ComposeFiles)
	require.Equal(t, project.WorkingDir, rootService.CustomLabels[api.WorkingDirLabel])
	require.Equal(t, expectedConfigFiles, rootService.CustomLabels[api.ConfigFilesLabel])
	require.Equal(t, project.WorkingDir, includedService.CustomLabels[api.WorkingDirLabel])
	require.Equal(t, expectedConfigFiles, includedService.CustomLabels[api.ConfigFilesLabel])
}
