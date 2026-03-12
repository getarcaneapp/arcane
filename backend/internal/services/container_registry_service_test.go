package services

import (
	"context"
	"errors"
	"testing"

	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRegistryDaemonClient struct {
	registryLoginFn       func(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error)
	distributionInspectFn func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

func (f *fakeRegistryDaemonClient) RegistryLogin(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
	if f.registryLoginFn == nil {
		return client.RegistryLoginResult{}, nil
	}
	return f.registryLoginFn(ctx, options)
}

func (f *fakeRegistryDaemonClient) DistributionInspect(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
	if f.distributionInspectFn == nil {
		return client.DistributionInspectResult{}, nil
	}
	return f.distributionInspectFn(ctx, imageRef, options)
}

func TestContainerRegistryService_GetAllRegistryAuthConfigs_NormalizesHosts(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")
	createTestPullRegistry(t, db, "https://GHCR.IO/", "gh-user", "gh-token")

	svc := NewContainerRegistryService(db, nil)
	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, authConfigs)

	dockerCfg, ok := authConfigs["docker.io"]
	require.True(t, ok)
	assert.Equal(t, "docker-user", dockerCfg.Username)
	assert.Equal(t, "docker-token", dockerCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", dockerCfg.ServerAddress)

	assert.Equal(t, dockerCfg, authConfigs["registry-1.docker.io"])
	assert.Equal(t, dockerCfg, authConfigs["index.docker.io"])

	ghcrCfg, ok := authConfigs["ghcr.io"]
	require.True(t, ok)
	assert.Equal(t, "gh-user", ghcrCfg.Username)
	assert.Equal(t, "gh-token", ghcrCfg.Password)
	assert.Equal(t, "ghcr.io", ghcrCfg.ServerAddress)
}

func TestContainerRegistryService_GetAllRegistryAuthConfigs_SkipsInvalidEntries(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "  ", "docker-token")
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "   ")
	createTestPullRegistry(t, db, "https://registry.example.com", "example-user", "example-token")

	svc := NewContainerRegistryService(db, nil)
	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, authConfigs)

	assert.NotContains(t, authConfigs, "docker.io")
	assert.NotContains(t, authConfigs, "ghcr.io")

	exampleCfg, ok := authConfigs["registry.example.com"]
	require.True(t, ok)
	assert.Equal(t, "example-user", exampleCfg.Username)
	assert.Equal(t, "example-token", exampleCfg.Password)
	assert.Equal(t, "registry.example.com", exampleCfg.ServerAddress)
}

func TestContainerRegistryService_TestRegistry_UsesDockerDaemon(t *testing.T) {
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			registryLoginFn: func(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
				assert.Equal(t, "user", options.Username)
				assert.Equal(t, "token", options.Password)
				assert.Equal(t, "registry.example.com:5443", options.ServerAddress)
				return client.RegistryLoginResult{}, nil
			},
		}, nil
	})

	err := svc.TestRegistry(context.Background(), "https://registry.example.com:5443", "user", "token")
	require.NoError(t, err)
}

func TestContainerRegistryService_TestRegistry_PropagatesDaemonError(t *testing.T) {
	expectedErr := errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			registryLoginFn: func(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
				return client.RegistryLoginResult{}, expectedErr
			},
		}, nil
	})

	err := svc.TestRegistry(context.Background(), "registry.example.com", "user", "token")
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestContainerRegistryService_TestRegistry_SkipsLoginForEmptyCredentials(t *testing.T) {
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			registryLoginFn: func(ctx context.Context, options client.RegistryLoginOptions) (client.RegistryLoginResult, error) {
				t.Fatal("RegistryLogin should not be called with empty credentials")
				return client.RegistryLoginResult{}, nil
			},
		}, nil
	})

	err := svc.TestRegistry(context.Background(), "registry.example.com", "", "")
	require.NoError(t, err)

	err = svc.TestRegistry(context.Background(), "registry.example.com", "  ", "  ")
	require.NoError(t, err)
}

func TestContainerRegistryService_InspectImageDigest_AnonymousSuccess(t *testing.T) {
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				assert.Equal(t, "registry.example.com:5443/team/app:1.2.3", imageRef)
				assert.Empty(t, options.EncodedRegistryAuth)
				return client.DistributionInspectResult{
					DistributionInspect: dockerregistry.DistributionInspect{
						Descriptor: ocispec.Descriptor{
							Digest: digest.Digest("sha256:feedface"),
						},
					},
				}, nil
			},
		}, nil
	})

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry.example.com:5443/team/app:1.2.3", nil)
	require.NoError(t, err)
	assert.Equal(t, "sha256:feedface", result.Digest)
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Equal(t, "registry.example.com:5443", result.AuthRegistry)
}

func TestContainerRegistryService_InspectImageDigest_RetriesWithStoredCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	var calls int
	svc := NewContainerRegistryService(db, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				calls++
				assert.Equal(t, "docker.io/library/nginx:latest", imageRef)
				if calls == 1 {
					assert.Empty(t, options.EncodedRegistryAuth)
					return client.DistributionInspectResult{}, errors.New("unauthorized: authentication required")
				}

				authCfg := decodeRegistryAuth(t, options.EncodedRegistryAuth)
				assert.Equal(t, "docker-user", authCfg.Username)
				assert.Equal(t, "docker-token", authCfg.Password)
				assert.Equal(t, "https://index.docker.io/v1/", authCfg.ServerAddress)

				return client.DistributionInspectResult{
					DistributionInspect: dockerregistry.DistributionInspect{
						Descriptor: ocispec.Descriptor{
							Digest: digest.Digest("sha256:cafebabe"),
						},
					},
				}, nil
			},
		}, nil
	})

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry-1.docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "sha256:cafebabe", result.Digest)
	assert.Equal(t, "credential", result.AuthMethod)
	assert.Equal(t, "docker-user", result.AuthUsername)
	assert.True(t, result.UsedCredential)
}
