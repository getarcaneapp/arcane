package libbuild

import (
	"context"
	"errors"
	"testing"

	configtypes "github.com/docker/cli/cli/config/types"
	utilsregistry "github.com/getarcaneapp/arcane/backend/internal/utils/registry"
	"github.com/moby/buildkit/session/auth/authprovider"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRegistryAuthProvider struct {
	hostAuth map[string]string
	hostErr  error
}

func (f fakeRegistryAuthProvider) GetRegistryAuthForImage(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f fakeRegistryAuthProvider) GetRegistryAuthForHost(_ context.Context, host string) (string, error) {
	if f.hostErr != nil {
		return "", f.hostErr
	}
	return f.hostAuth[host], nil
}

func (f fakeRegistryAuthProvider) GetAllRegistryAuthConfigs(_ context.Context) (map[string]dockerregistry.AuthConfig, error) {
	return nil, nil
}

func encodeAuthConfigInternal(t *testing.T, cfg configtypes.AuthConfig) string {
	t.Helper()

	raw, err := utilsregistry.EncodeAuthHeader(cfg.Username, cfg.Password, cfg.ServerAddress)
	require.NoError(t, err)
	return raw
}

func TestBuildkitAuthConfigProvider_UsesRegistryAuthProviderForHost(t *testing.T) {
	registryCfg := configtypes.AuthConfig{
		Username:      "db-user",
		Password:      "db-token",
		ServerAddress: "ghcr.io",
	}

	provider := buildkitAuthConfigProviderInternal(
		func(_ context.Context, _ string, _ []string, _ authprovider.ExpireCachedAuthCheck) (configtypes.AuthConfig, error) {
			return configtypes.AuthConfig{Username: "default"}, nil
		},
		fakeRegistryAuthProvider{hostAuth: map[string]string{"ghcr.io": encodeAuthConfigInternal(t, registryCfg)}},
	)

	cfg, err := provider(context.Background(), "ghcr.io", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "db-user", cfg.Username)
	assert.Equal(t, "db-token", cfg.Password)
	assert.Equal(t, "ghcr.io", cfg.ServerAddress)
}

func TestBuildkitAuthConfigProvider_FallsBackToDefaultProvider(t *testing.T) {
	defaultCalled := false
	defaultCfg := configtypes.AuthConfig{Username: "default-user", Password: "default-token", ServerAddress: "docker.io"}

	t.Run("registry provider error", func(t *testing.T) {
		defaultCalled = false
		provider := buildkitAuthConfigProviderInternal(
			func(_ context.Context, _ string, _ []string, _ authprovider.ExpireCachedAuthCheck) (configtypes.AuthConfig, error) {
				defaultCalled = true
				return defaultCfg, nil
			},
			fakeRegistryAuthProvider{hostErr: errors.New("db unavailable")},
		)

		cfg, err := provider(context.Background(), "docker.io", nil, nil)
		require.NoError(t, err)
		assert.True(t, defaultCalled)
		assert.Equal(t, defaultCfg, cfg)
	})

	t.Run("registry provider invalid auth header", func(t *testing.T) {
		defaultCalled = false
		provider := buildkitAuthConfigProviderInternal(
			func(_ context.Context, _ string, _ []string, _ authprovider.ExpireCachedAuthCheck) (configtypes.AuthConfig, error) {
				defaultCalled = true
				return defaultCfg, nil
			},
			fakeRegistryAuthProvider{hostAuth: map[string]string{"docker.io": "not-base64"}},
		)

		cfg, err := provider(context.Background(), "docker.io", nil, nil)
		require.NoError(t, err)
		assert.True(t, defaultCalled)
		assert.Equal(t, defaultCfg, cfg)
	})
}
