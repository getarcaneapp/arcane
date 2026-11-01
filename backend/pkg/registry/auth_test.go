package registry

import (
	"testing"

	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRegistryHost(t *testing.T) {
	t.Run("explicit registry", func(t *testing.T) {
		assert.Equal(t, "ghcr.io", ExtractRegistryHost("ghcr.io/getarcaneapp/arcane:latest"))
	})

	t.Run("implicit docker hub", func(t *testing.T) {
		assert.Equal(t, "docker.io", ExtractRegistryHost("redis:latest"))
	})

	t.Run("digest reference", func(t *testing.T) {
		assert.Equal(t, "registry.example.com", ExtractRegistryHost("registry.example.com/team/app@sha256:abcdef"))
	})
}

func TestSplitRegistryURL(t *testing.T) {
	tests := []struct{ in, host, namespace string }{
		{"ghcr.io", "ghcr.io", ""},
		{"https://ghcr.io/acme/", "ghcr.io", "acme"},
		{"http://localhost:5000/team/sub", "localhost:5000", "team/sub"},
		{"registry-1.docker.io", "docker.io", ""},
		{"https://index.docker.io/v1/", "docker.io", ""},
		{"https://index.docker.io/v1/acme", "docker.io", "acme"},
		{"docker.io/acme", "docker.io", "acme"},
		{"docker.io/v1team", "docker.io", "v1team"},
	}
	for _, tt := range tests {
		host, namespace := SplitRegistryURL(tt.in)
		assert.Equal(t, tt.host, host, tt.in)
		assert.Equal(t, tt.namespace, namespace, tt.in)
	}
}

func TestNormalizeRegistryURL(t *testing.T) {
	assert.Equal(t, "https://index.docker.io/v1/", NormalizeRegistryURL("docker.io"))
	assert.Equal(t, "ghcr.io", NormalizeRegistryURL("https://ghcr.io/"))
}

func TestEncodeAuthHeader(t *testing.T) {
	encoded, err := EncodeAuthHeader("user", "token", "ghcr.io")
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	cfg, err := dockerauthconfig.Decode(encoded)
	require.NoError(t, err)
	assert.Equal(t, "user", cfg.Username)
	assert.Equal(t, "token", cfg.Password)
	assert.Equal(t, "ghcr.io", cfg.ServerAddress)
}

func TestDecodeAuthHeader(t *testing.T) {
	encoded, err := EncodeAuthHeader("decode-user", "decode-token", "registry.example.com")
	require.NoError(t, err)

	cfg, err := DecodeAuthHeader(encoded)
	require.NoError(t, err)
	assert.Equal(t, "decode-user", cfg.Username)
	assert.Equal(t, "decode-token", cfg.Password)
	assert.Equal(t, "registry.example.com", cfg.ServerAddress)
}

func TestDecodeAuthHeader_InvalidInput(t *testing.T) {
	_, err := DecodeAuthHeader("not-base64")
	require.Error(t, err)
}
