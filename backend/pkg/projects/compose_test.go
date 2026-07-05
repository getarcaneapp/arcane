package projects

import (
	"reflect"
	"testing"

	"github.com/docker/cli/cli/command"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/flags"
	dockerregistry "github.com/moby/moby/api/types/registry"
	mobyclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestWrapDockerCLIWithInspectCompatibility(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://example.com:2375")

	apiClient, err := mobyclient.New(mobyclient.WithHost("tcp://example.com:2375"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = apiClient.Close()
	})

	baseCLI, err := command.NewDockerCli(command.WithAPIClient(apiClient))
	require.NoError(t, err)
	require.NoError(t, baseCLI.Initialize(flags.NewClientOptions()))

	wrapped := wrapDockerCLIWithInspectCompatibilityInternal(baseCLI)
	require.NotNil(t, wrapped)
	require.NotSame(t, baseCLI, wrapped)
	require.NotEqual(t, reflect.TypeFor[*mobyclient.Client](), reflect.TypeOf(wrapped.Client()))
}

func TestBuildComposeAuthConfigs(t *testing.T) {
	authConfigs := buildComposeAuthConfigsInternal(map[string]dockerregistry.AuthConfig{
		"docker.io": {
			Username:      "docker-user",
			Password:      "docker-token",
			ServerAddress: "https://index.docker.io/v1/",
		},
		"registry.gitlab.com": {
			Username:      "gitlab-user",
			Password:      "gitlab-token",
			ServerAddress: "registry.gitlab.com",
		},
	})

	require.Equal(t, map[string]clitypes.AuthConfig{
		"https://index.docker.io/v1/": {
			Username:      "docker-user",
			Password:      "docker-token",
			ServerAddress: "https://index.docker.io/v1/",
		},
		"registry.gitlab.com": {
			Username:      "gitlab-user",
			Password:      "gitlab-token",
			ServerAddress: "registry.gitlab.com",
		},
	}, authConfigs)
}
