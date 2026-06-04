package services

import (
	"testing"

	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/stretchr/testify/require"
)

func decodeRegistryAuth(t *testing.T, encoded string) dockerregistry.AuthConfig {
	t.Helper()

	cfg, err := dockerauthconfig.Decode(encoded)
	require.NoError(t, err)
	return *cfg
}
