package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
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

			composePath, err := DetectComposeFile(t.Context(), "", dir)
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

	composePath, err := DetectComposeFile(t.Context(), "", dir)
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

	composePath, err := DetectComposeFile(t.Context(), "", dir)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, composePath)
}

func TestDetectComposeFile_ReturnsErrorForAmbiguousCustomComposeNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.yaml"), []byte("services:\n  a:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beta.yml"), []byte("services:\n  b:\n    image: busybox:latest\n"), 0o600))

	_, err := DetectComposeFile(t.Context(), "", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple custom compose files")
}

func TestDetectComposeFile_IgnoresSingleNonComposeYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("replicaCount: 2\nimage:\n  tag: latest\n"), 0o600))

	_, err := DetectComposeFile(t.Context(), "", dir)
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

	detected, err := DetectComposeFile(t.Context(), "", dir)
	require.NoError(t, err)
	assert.Equal(t, basePath, detected)
}

func TestDetectComposeFile_IgnoresOverrideOnlyDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))

	_, err := DetectComposeFile(t.Context(), "", dir)
	require.Error(t, err)
}

func TestLoadComposeProject_MergesComposeOverrideFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "compose.yaml")
	overridePath := filepath.Join(dir, "compose.override.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("services:\n  app:\n    image: nginx:alpine\n    environment:\n      FROM_BASE: \"1\"\n"), 0o600))
	require.NoError(t, os.WriteFile(overridePath, []byte("services:\n  app:\n    image: busybox:latest\n    environment:\n      FROM_OVERRIDE: \"1\"\n"), 0o600))

	project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil, nil, nil, false)
	require.NoError(t, err)
	require.NotNil(t, project)

	app := project.Services["app"]
	assert.Equal(t, "busybox:latest", app.Image)
	assert.Contains(t, app.Environment, "FROM_BASE")
	assert.Contains(t, app.Environment, "FROM_OVERRIDE")
	assert.Equal(t, []string{basePath, overridePath}, project.ComposeFiles)
}

func TestLoadComposeProject_ComposeFileEnvSelectsFilesAndSkipsOverride(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yml")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	extraPath := filepath.Join(dir, "sub", "extra.yml")
	require.NoError(t, os.WriteFile(basePath, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(extraPath, []byte("services:\n  worker:\n    image: busybox:latest\n"), 0o600))
	// A standard override on disk must NOT be auto-merged when COMPOSE_FILE is set.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.override.yaml"), []byte("services:\n  app:\n    image: alpine:3\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("COMPOSE_FILE=base.yml:sub/extra.yml\n"), 0o600))

	project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil, nil, nil, false)
	require.NoError(t, err)
	require.NotNil(t, project)

	assert.Contains(t, project.Services, "app")
	assert.Contains(t, project.Services, "worker")
	assert.Equal(t, "nginx:alpine", project.Services["app"].Image, "override must not be merged when COMPOSE_FILE is set")
	assert.Equal(t, []string{basePath, extraPath}, project.ComposeFiles)
}

func TestLoadComposeProject_ArcaneProcessEnvInterpolation(t *testing.T) {
	// Arcane's own env (e.g. PORT) must not leak into compose interpolation of
	// managed projects: ${PORT:-8191} falls back to its default (issue #3499).
	// Allowlisted vars like TZ still flow through.
	t.Setenv("PORT", "3552")
	t.Setenv("TZ", "America/Chicago")

	testCases := []struct {
		name    string
		compose string
		check   func(t *testing.T, app composetypes.ServiceConfig)
	}{
		{
			name:    "blocked var falls back to compose default",
			compose: "services:\n  app:\n    image: nginx:alpine\n    ports:\n      - \"${PORT:-8191}:8191\"\n",
			check: func(t *testing.T, app composetypes.ServiceConfig) {
				t.Helper()
				require.Len(t, app.Ports, 1)
				assert.Equal(t, "8191", app.Ports[0].Published)
			},
		},
		{
			name:    "allowlisted var flows through",
			compose: "services:\n  app:\n    image: nginx:alpine\n    environment:\n      TZ: ${TZ:-UTC}\n",
			check: func(t *testing.T, app composetypes.ServiceConfig) {
				t.Helper()
				require.NotNil(t, app.Environment["TZ"])
				assert.Equal(t, "America/Chicago", *app.Environment["TZ"])
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			basePath := filepath.Join(dir, "compose.yaml")
			require.NoError(t, os.WriteFile(basePath, []byte(tc.compose), 0o600))

			project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil, nil, nil, false)
			require.NoError(t, err)
			require.NotNil(t, project)

			tc.check(t, project.Services["app"])
		})
	}
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

	project, err := LoadComposeProject(context.Background(), basePath, "demo", dir, false, nil, nil, nil, false)
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

	project, err := LoadComposeProject(context.Background(), composePath, "demo", dir, false, nil, nil, nil, true)
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

	project, err := LoadComposeProject(context.Background(), composePath, "demo", dir, false, nil, nil, nil, true)
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Len(t, project.Services, 1)
}

func TestLoadComposeProjectLenient_AppliesVariableDefaults(t *testing.T) {
	t.Parallel()

	// The lenient loader used to report the placeholder as every undefined
	// variable's value, which suppressed
	// ${VAR:-default} resolution and fed "/placeholder-undefined" into the
	// ports host entry ("invalid hostPort"). Defaults must resolve normally;
	// only variables with no default fall back to the placeholder.
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  app:
    image: nginx:alpine
    ports:
      - "${ARCANE_TEST_UNSET_PORT:-3169}:3000"
    environment:
      - MAX_FILE_SIZE=${ARCANE_TEST_UNSET_SIZE:-104857600}
`), 0o600))

	project, err := LoadComposeProject(context.Background(), composePath, "demo", dir, false, nil, nil, nil, true)
	require.NoError(t, err)
	require.NotNil(t, project)
	require.Len(t, project.Services, 1)
	svc := project.Services["app"]
	require.Len(t, svc.Ports, 1)
	assert.Equal(t, "3169", svc.Ports[0].Published)
	require.NotNil(t, svc.Environment["MAX_FILE_SIZE"])
	assert.Equal(t, "104857600", *svc.Environment["MAX_FILE_SIZE"])
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

	project, err := LoadComposeProject(context.Background(), composePath, "demo", projectDir, false, nil, nil, nil, false)
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

			project, err := LoadComposeProject(context.Background(), composePath, "aitools", projectDir, false, nil, nil, nil, false)
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

	project, err := LoadComposeProject(context.Background(), composePath, "demo", dir, false, nil, nil, nil, false)
	require.NoError(t, err)

	ResolveRelativeProjectPaths(project, dir)

	service := project.Services["app"]
	require.Len(t, service.Volumes, 1)
	assert.Equal(t, filepath.Join(dir, "config.conf"), service.Volumes[0].Source)
}

// Reproduces the reported case where a relative bind path escaping the projects
// mount resolved against Arcane's container path instead of the host path
// `docker compose up` would use, while checking that in-mount relative paths and
// intentionally absolute paths keep their current behavior.
func TestLoadComposeProject_RemapsRelativeBindEscapingProjectsMount(t *testing.T) {
	t.Parallel()

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "goclaw")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	composePath := filepath.Join(projectDir, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte(`services:
  goclaw:
    image: nginx:alpine
    volumes:
      - ../../../../goclaw/data:/app/data
      - ./data:/app/cache
      - /mnt/nas/media:/media
`), 0o600))

	pathMapper := NewPathMapper(projectsRoot, "/docker/112/arcane/arcane-data/projects")
	require.True(t, pathMapper.IsNonMatchingMount())

	project, err := LoadComposeProject(context.Background(), composePath, "goclaw", projectsRoot, false, pathMapper, nil, nil, false)
	require.NoError(t, err)

	sources := make(map[string]string)
	for _, volume := range project.Services["goclaw"].Volumes {
		sources[volume.Target] = volume.Source
	}

	// Escapes the mount: must land where `docker compose up` in the host project
	// directory would put it, not at the container-resolved "/goclaw/data".
	assert.Equal(t, "/docker/112/goclaw/data", sources["/app/data"])
	// Stays inside the mount: already correct today, must not change.
	assert.Equal(t, "/docker/112/arcane/arcane-data/projects/goclaw/data", sources["/app/cache"])
	// Absolute host path: must be passed through untouched.
	assert.Equal(t, "/mnt/nas/media", sources["/media"])
}
