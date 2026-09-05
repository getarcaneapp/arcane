package projects

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBuildContext_AllowsRemoteGitContext(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://github.com/getarcaneapp/arcane.git#main:docker/app",
		},
	}

	contextDir, err := ResolveBuildContext("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/getarcaneapp/arcane.git#main:docker/app", contextDir)
}

func TestResolveBuildContext_AllowsRemoteGitContextWithoutGitSuffix(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app",
		},
	}

	contextDir, err := ResolveBuildContext("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app", contextDir)
}

func TestMergeBuildTags(t *testing.T) {
	tags := MergeBuildTags("example/app:latest", []string{"example/app:sha", "example/app:latest", " "})
	assert.Equal(t, []string{"example/app:latest", "example/app:sha"}, tags)
}

func TestBuildPlatformsFromCompose(t *testing.T) {
	t.Run("uses service platform when build platforms missing", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context: ".",
			},
		}

		platforms := BuildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/amd64"}, platforms)
	})

	t.Run("keeps explicit build platforms", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context:   ".",
				Platforms: []string{"linux/arm64"},
			},
		}

		platforms := BuildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/arm64"}, platforms)
	})
}

func TestPrepareServiceBuildRequest_MapsComposeFields(t *testing.T) {
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:     "web",
		Image:    "example/web:latest",
		Platform: "linux/amd64",
		Build: &composetypes.BuildConfig{
			Context:    ".",
			Dockerfile: "Dockerfile.custom",
			Target:     "prod",
			Args: composetypes.MappingWithEquals{
				"FOO": new("bar"),
			},
			Tags:      []string{"example/web:sha", "example/web:latest"},
			CacheFrom: []string{"example/cache:latest"},
			CacheTo:   []string{"type=local,dest=/tmp/cache"},
			NoCache:   true,
			Pull:      true,
			Network:   "host",
			Isolation: "default",
			ShmSize:   composetypes.UnitBytes(64 * 1024 * 1024),
			Ulimits: map[string]*composetypes.UlimitsConfig{
				"nofile": {Soft: 1024, Hard: 2048},
			},
			Entitlements: []string{"network.host"},
			Privileged:   true,
			ExtraHosts: composetypes.HostsList{
				"registry.local": {"10.0.0.5"},
			},
			Labels: composetypes.Labels{
				"com.example.team": "platform",
			},
		},
	}

	req, _, _, err := PrepareServiceBuildRequest(
		"project-id",
		proj,
		"web",
		serviceCfg,
		projecttypes.BuildOptions{}, "",
	)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/project", req.ContextDir)
	assert.Equal(t, "Dockerfile.custom", req.Dockerfile)
	assert.Equal(t, "prod", req.Target)
	assert.Equal(t, map[string]string{"FOO": "bar"}, req.BuildArgs)
	assert.Equal(t, []string{"example/web:latest", "example/web:sha"}, req.Tags)
	assert.Equal(t, []string{"linux/amd64"}, req.Platforms)
	assert.Equal(t, []string{"example/cache:latest"}, req.CacheFrom)
	assert.Equal(t, []string{"type=local,dest=/tmp/cache"}, req.CacheTo)
	assert.True(t, req.NoCache)
	assert.True(t, req.Pull)
	assert.Equal(t, "host", req.Network)
	assert.Equal(t, "default", req.Isolation)
	assert.Equal(t, int64(64*1024*1024), req.ShmSize)
	assert.Equal(t, map[string]string{"nofile": "1024:2048"}, req.Ulimits)
	assert.Equal(t, []string{"network.host"}, req.Entitlements)
	assert.True(t, req.Privileged)
	assert.Equal(t, map[string]string{"com.example.team": "platform"}, req.Labels)
	require.Len(t, req.ExtraHosts, 1)
	assert.Contains(t, req.ExtraHosts[0], "registry.local")
	assert.Contains(t, req.ExtraHosts[0], "10.0.0.5")
}

// TestPrepareServiceBuildRequest_KeepsContainerPaths is a
// regression test for #2314: Arcane's local build pipeline (the docker and
// buildkit providers both read the build context via the Arcane process's own
// filesystem) cannot use host paths, so prepareServiceBuildRequest must leave
// the build context and any absolute Dockerfile path as container paths even
// when the projects mount has a non-matching host prefix.
func TestPrepareServiceBuildRequest_KeepsContainerPaths(t *testing.T) {
	proj := &composetypes.Project{WorkingDir: "/app/data/projects/demo", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "web",
		Image: "example/web:latest",
		Build: &composetypes.BuildConfig{
			Context:    ".",
			Dockerfile: "/app/data/projects/demo/Dockerfile.custom",
		},
	}

	req, _, _, err := PrepareServiceBuildRequest(
		"project-id",
		proj,
		"web",
		serviceCfg,
		projecttypes.BuildOptions{}, "",
	)
	require.NoError(t, err)

	assert.Equal(t, "/app/data/projects/demo", req.ContextDir)
	assert.Equal(t, "/app/data/projects/demo/Dockerfile.custom", req.Dockerfile)
}

// TestPrepareServiceBuildRequest_BuildDotKeepsContainerPath
// reproduces the exact configuration from #2314: a compose file with
// `build: .` next to its Dockerfile, on an installation where the projects
// directory is bind-mounted from a different host path than the container
// path. The resulting BuildRequest must point at the container path so the
// local builder can stat / tar the directory.
func TestPrepareServiceBuildRequest_BuildDotKeepsContainerPath(t *testing.T) {
	proj := &composetypes.Project{WorkingDir: "/app/data/projects/caddy", Name: "caddy"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "caddy",
		Image: "caddy",
		Build: &composetypes.BuildConfig{
			Context: ".",
		},
	}

	req, _, _, err := PrepareServiceBuildRequest(
		"project-id",
		proj,
		"caddy",
		serviceCfg,
		projecttypes.BuildOptions{}, "",
	)
	require.NoError(t, err)

	assert.Equal(t, "/app/data/projects/caddy", req.ContextDir)
	assert.Equal(t, "Dockerfile", req.Dockerfile)
}

func TestPrepareServiceBuildRequest_UsesInlineDockerfile(t *testing.T) {
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "web",
		Image: "example/web:latest",
		Build: &composetypes.BuildConfig{
			Context:          ".",
			DockerfileInline: "FROM alpine:3.20\nRUN echo inline\n",
		},
	}

	req, _, _, err := PrepareServiceBuildRequest(
		"project-id",
		proj,
		"web",
		serviceCfg,
		projecttypes.BuildOptions{}, "",
	)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/project", req.ContextDir)
	assert.Empty(t, req.Dockerfile)
	assert.Equal(t, "FROM alpine:3.20\nRUN echo inline\n", req.DockerfileInline)
}

func TestPrepareServiceBuildRequest_GeneratedImageProviderGuardrails(t *testing.T) {
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name: "web",
		Build: &composetypes.BuildConfig{
			Context: ".",
		},
	}

	_, _, _, err := PrepareServiceBuildRequest(
		"project-id",
		proj,
		"web",
		serviceCfg,
		projecttypes.BuildOptions{Provider: "depot"}, "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define an image when using depot")

	_, _, _, err = PrepareServiceBuildRequest(
		"project-id",
		proj,
		"web",
		serviceCfg,
		projecttypes.BuildOptions{Provider: "local", Push: new(true)}, "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define an image when push is enabled")
}
