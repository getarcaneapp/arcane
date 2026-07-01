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

func TestDetectComposeOverrideFile(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when no override present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
		assert.Empty(t, DetectComposeOverrideFile(dir))
	})

	t.Run("detects override file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		overridePath := filepath.Join(dir, "compose.override.yaml")
		require.NoError(t, os.WriteFile(overridePath, []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))
		assert.Equal(t, overridePath, DetectComposeOverrideFile(dir))
	})

	t.Run("prefers highest-preference override when multiple present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		preferred := filepath.Join(dir, "compose.override.yml")
		require.NoError(t, os.WriteFile(preferred, []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.override.yaml"), []byte("services:\n  app:\n    image: alpine:3\n"), 0o600))
		assert.Equal(t, preferred, DetectComposeOverrideFile(dir))
	})
}

func TestDetectComposeFile_ReturnsBaseNotOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))

	detected, err := DetectComposeFile(dir)
	require.NoError(t, err)
	assert.Equal(t, basePath, detected)
}

func TestDetectComposeFile_IgnoresOverrideOnlyDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))

	_, err := DetectComposeFile(dir)
	require.Error(t, err)
}

func TestLoadComposeProject_MergesComposeOverrideFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "compose.yaml")
	overridePath := filepath.Join(dir, "compose.override.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("services:\n  app:\n    image: nginx:alpine\n    environment:\n      FROM_BASE: \"1\"\n"), 0o600))
	require.NoError(t, os.WriteFile(overridePath, []byte("services:\n  app:\n    image: busybox:latest\n    environment:\n      FROM_OVERRIDE: \"1\"\n"), 0o600))

	project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)

	app := project.Services["app"]
	assert.Equal(t, "busybox:latest", app.Image)
	assert.Contains(t, app.Environment, "FROM_BASE")
	assert.Contains(t, app.Environment, "FROM_OVERRIDE")
	assert.Equal(t, []string{basePath, overridePath}, project.ComposeFiles)
}

func TestLoadComposeProject_DoesNotMergeOverrideForCustomBaseName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A custom base filename (not a standard compose candidate) is the explicit
	// `-f` case: `docker compose` does not auto-load an override for it, so neither
	// do we, even though compose.override.yaml sits right beside it.
	basePath := filepath.Join(dir, "mystack.yaml")
	overridePath := filepath.Join(dir, "compose.override.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("services:\n  app:\n    image: nginx:alpine\n    environment:\n      FROM_BASE: \"1\"\n"), 0o600))
	require.NoError(t, os.WriteFile(overridePath, []byte("services:\n  app:\n    image: busybox:latest\n    environment:\n      FROM_OVERRIDE: \"1\"\n"), 0o600))

	project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)

	app := project.Services["app"]
	assert.Equal(t, "nginx:alpine", app.Image)
	assert.Contains(t, app.Environment, "FROM_BASE")
	assert.NotContains(t, app.Environment, "FROM_OVERRIDE")
	assert.Equal(t, []string{basePath}, project.ComposeFiles)
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

func TestLoadComposeProjectLenient_ToleratesUndefinedVariables(t *testing.T) {
	t.Parallel()

	// Reproduces the GitSync chicken-and-egg problem: a compose file references
	// ${CONFIG_FILE} in a bind-mount source, but no .env exists yet.  The strict
	// loader would resolve ${CONFIG_FILE} to "" and produce ":/etc/app/app.conf"
	// (empty section between colons).  The lenient loader must succeed instead.
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  app:
    image: nginx:alpine
    volumes:
      - ${CONFIG_FILE}:/etc/app/app.conf
`), 0o600))

	project, err := LoadComposeProjectLenient(context.Background(), composePath, "demo", dir, false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Len(t, project.Services, 1)
}

func TestLoadComposeProjectLenient_ToleratesUndefinedTypedFieldVariables(t *testing.T) {
	t.Parallel()

	// Same chicken-and-egg problem for typed fields: deploy.resources.limits.cpus
	// is parsed as a float and memory as a size. With no .env the strict loader
	// fails with `strconv.ParseFloat: parsing "": invalid syntax` and
	// `invalid size: ''`. Lenient mode must succeed so the GitOps sync can
	// create the project and let the user supply real values afterward.
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  chrony:
    image: ${DOCKER_IMAGE}
    deploy:
      resources:
        limits:
          cpus: ${CPU}
          memory: ${MEMORY}
`), 0o600))

	project, err := LoadComposeProjectLenient(context.Background(), composePath, "demo", dir, false, nil)
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Len(t, project.Services, 1)
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

func TestLoadComposeProject_YamlNameOverridesDefaultName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		composeBody string
		wantName    string
	}{
		{
			name: "yaml name",
			composeBody: `name: ai_tools
services:
  app:
    image: nginx:alpine
`,
			wantName: "ai_tools",
		},
		{
			name: "default name",
			composeBody: `services:
  app:
    image: nginx:alpine
`,
			wantName: "aitools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			projectDir := t.TempDir()
			composePath := filepath.Join(projectDir, "compose.yaml")
			require.NoError(t, os.WriteFile(composePath, []byte(tt.composeBody), 0o600))

			project, err := LoadComposeProject(context.Background(), composePath, "aitools", projectDir, false, nil)
			require.NoError(t, err)
			require.NotNil(t, project)
			require.Equal(t, tt.wantName, project.Name)

			service := project.Services["app"]
			require.Equal(t, tt.wantName, service.CustomLabels[api.ProjectLabel])
		})
	}
}

func TestResolveRelativeProjectPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  app:
    image: nginx:alpine
    volumes:
      - ./config.conf:/etc/app/config.conf
`), 0o600))

	project, err := LoadComposeProject(context.Background(), composePath, "demo", dir, false, nil)
	require.NoError(t, err)

	ResolveRelativeProjectPaths(project, dir)

	service := project.Services["app"]
	require.Len(t, service.Volumes, 1)
	assert.Equal(t, filepath.Join(dir, "config.conf"), service.Volumes[0].Source)
}
