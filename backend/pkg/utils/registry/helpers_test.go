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

func TestNormalizeRegistryForComparison(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://ghcr.io", want: "ghcr.io"},
		{in: "ghcr.io/", want: "ghcr.io"},
		{in: "GHCR.IO", want: "ghcr.io"},
		{in: "https://ghcr.io/path", want: "ghcr.io"},
		{in: "registry-1.docker.io", want: "docker.io"},
		{in: "index.docker.io", want: "docker.io"},
		{in: "docker.io", want: "docker.io"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, NormalizeRegistryForComparison(tt.in))
	}
}

func TestNormalizeRegistryURL(t *testing.T) {
	assert.Equal(t, "https://index.docker.io/v1/", NormalizeRegistryURL("docker.io"))
	assert.Equal(t, "ghcr.io", NormalizeRegistryURL("https://ghcr.io/"))
}

func TestIsRegistryMatch(t *testing.T) {
	assert.True(t, IsRegistryMatch("https://index.docker.io/v1/", "registry-1.docker.io"))
	assert.True(t, IsRegistryMatch("https://ghcr.io", "ghcr.io"))
	assert.False(t, IsRegistryMatch("ghcr.io", "registry.example.com"))
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

func TestRegistryAuthLookupKeys(t *testing.T) {
	assert.Equal(t, []string{"ghcr.io"}, RegistryAuthLookupKeys("https://GHCR.IO/"))
	assert.Equal(t, []string{"docker.io", "index.docker.io", "registry-1.docker.io"}, RegistryAuthLookupKeys("https://index.docker.io/v1/"))
	assert.Nil(t, RegistryAuthLookupKeys("   "))
}
