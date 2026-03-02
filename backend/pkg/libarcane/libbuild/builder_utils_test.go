package libbuild

import (
	"os"
	"path/filepath"
	"testing"

	imagetypes "github.com/getarcaneapp/arcane/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createBuildContextWithDockerfileInternal(t *testing.T) string {
	t.Helper()

	contextDir := t.TempDir()
	dockerfilePath := filepath.Join(contextDir, "Dockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte("FROM alpine:3.20\n"), 0o644))
	return contextDir
}

func TestValidateBuildRequestInternal_LocalProviderUnsupportedOptions(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir:   contextDir,
		Dockerfile:   "Dockerfile",
		Tags:         []string{"ghcr.io/getarcaneapp/arcane:test"},
		Load:         true,
		CacheTo:      []string{"type=registry,ref=ghcr.io/getarcaneapp/cache:latest"},
		Entitlements: []string{"network.host"},
		Privileged:   true,
		Platforms:    []string{"linux/amd64", "linux/arm64"},
	}

	err := validateBuildRequestInternal(req, "local")
	require.EqualError(t, err, "unsupported build options for provider local: cacheTo, entitlements, platforms, privileged")
}

func TestValidateBuildRequestInternal_DepotProviderUnsupportedOptions(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
		Push:       true,
		Network:    "host",
		Isolation:  "process",
		ShmSize:    128 * 1024 * 1024,
		Ulimits: map[string]string{
			"nofile": "1024:2048",
		},
		ExtraHosts: []string{"registry.local:10.0.0.5"},
	}

	err := validateBuildRequestInternal(req, "depot")
	require.EqualError(t, err, "unsupported build options for provider depot: extraHosts, isolation, network, shmSize, ulimits")
}

func TestValidateBuildRequestInternal_RespectsTrimmedValues(t *testing.T) {
	contextDir := createBuildContextWithDockerfileInternal(t)
	req := imagetypes.BuildRequest{
		ContextDir:   contextDir,
		Dockerfile:   "Dockerfile",
		Tags:         []string{"ghcr.io/getarcaneapp/arcane:test"},
		Load:         true,
		CacheTo:      []string{"   "},
		Entitlements: []string{"\n\t"},
		ExtraHosts:   []string{"  "},
		Platforms:    []string{"linux/amd64", "   "},
	}

	err := validateBuildRequestInternal(req, "local")
	assert.NoError(t, err)
}
