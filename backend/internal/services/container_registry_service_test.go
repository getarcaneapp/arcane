package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/containerregistry"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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

func newTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func newDockerHubRateLimitTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	targetURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	client := server.Client()
	baseTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "registry-1.docker.io" || req.URL.Host == "auth.docker.io" {
			rewritten := req.Clone(req.Context())
			rewritten.URL.Scheme = targetURL.Scheme
			rewritten.URL.Host = targetURL.Host
			return baseTransport.RoundTrip(rewritten)
		}

		return baseTransport.RoundTrip(req)
	})

	return client
}

func TestNewContainerRegistryService_InitializesDistributionHTTPClient(t *testing.T) {
	svc := NewContainerRegistryService(nil, nil, nil)
	require.NotNil(t, svc.distributionHTTPClient)
}

func TestContainerRegistryService_GetAllRegistryAuthConfigs_NormalizesHosts(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")
	createTestPullRegistry(t, db, "https://GHCR.IO/", "gh-user", "gh-token")

	svc := NewContainerRegistryService(db, nil, nil)
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

	svc := NewContainerRegistryService(db, nil, nil)
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

func TestContainerRegistryService_GetRegistryPullUsage_AnonymousDockerHubLimit(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	require.NoError(t, db.WithContext(context.Background()).Create(&models.ContainerRegistry{
		URL:          "docker.io",
		Enabled:      true,
		RegistryType: registryTypeGeneric,
	}).Error)

	var tokenCalls int

	svc := NewContainerRegistryService(db, nil, NewKVService(db))
	svc.distributionHTTPClient = newDockerHubRateLimitTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls++
			assert.Empty(t, r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"token":"anonymous-token"}`))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/ratelimitpreview/test/manifests/latest":
			if r.Header.Get("Authorization") != "Bearer anonymous-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("RateLimit-Limit", "100;w=21600")
			w.Header().Set("RateLimit-Remaining", "90;w=21600")
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	result, err := svc.GetRegistryPullUsage(context.Background())
	require.NoError(t, err)

	require.Len(t, result.Registries, 1)
	registry := result.Registries[0]
	assert.Equal(t, "dockerhub", registry.Provider)
	assert.Equal(t, "anonymous", registry.AuthMethod)
	assert.Empty(t, registry.Error)
	require.NotNil(t, registry.Limit)
	require.NotNil(t, registry.Remaining)
	require.NotNil(t, registry.Used)
	assert.Equal(t, 100, *registry.Limit)
	assert.Equal(t, 90, *registry.Remaining)
	assert.Equal(t, 10, *registry.Used)
	assert.Equal(t, 1, tokenCalls)

	cachedResult, err := svc.GetRegistryPullUsage(context.Background())
	require.NoError(t, err)
	require.Len(t, cachedResult.Registries, 1)
	require.NotNil(t, cachedResult.Registries[0].Remaining)
	assert.Equal(t, 90, *cachedResult.Registries[0].Remaining)
	assert.Equal(t, 1, tokenCalls)
}

func TestContainerRegistryService_GetRegistryPullUsage_UsesDockerHubCredential(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "gh-token")

	var tokenAuth string
	svc := NewContainerRegistryService(db, nil, NewKVService(db))
	svc.distributionHTTPClient = newDockerHubRateLimitTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"token":"credential-token"}`))
		case r.Method == http.MethodHead && r.URL.Path == "/v2/ratelimitpreview/test/manifests/latest":
			if r.Header.Get("Authorization") != "Bearer credential-token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("RateLimit-Limit", "200;w=21600")
			w.Header().Set("RateLimit-Remaining", "199;w=21600")
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	result, err := svc.GetRegistryPullUsage(context.Background())
	require.NoError(t, err)

	require.Len(t, result.Registries, 2)
	registry := result.Registries[0]
	assert.Equal(t, "credential", registry.AuthMethod)
	assert.Equal(t, "docker-user", registry.AuthUsername)
	assert.Empty(t, registry.Error)
	require.NotNil(t, registry.Limit)
	require.NotNil(t, registry.Remaining)
	assert.Equal(t, 200, *registry.Limit)
	assert.Equal(t, 199, *registry.Remaining)
	assert.Contains(t, tokenAuth, "Basic ")
}

func TestContainerRegistryService_GetRegistryPullUsage_CredentialErrorIsNonFatal(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://docker.io", "docker-user", "docker-token")

	svc := NewContainerRegistryService(db, nil, NewKVService(db))
	svc.distributionHTTPClient = newDockerHubRateLimitTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodHead && r.URL.Path == "/v2/ratelimitpreview/test/manifests/latest":
			w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	result, err := svc.GetRegistryPullUsage(context.Background())
	require.NoError(t, err)

	require.Len(t, result.Registries, 1)
	registry := result.Registries[0]
	assert.Equal(t, "credential", registry.AuthMethod)
	assert.Equal(t, "docker-user", registry.AuthUsername)
	assert.Contains(t, registry.Error, "token request failed with status: 401")
}

func TestContainerRegistryService_RecordImagePull_IncrementsObservedRegistryCount(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	require.NoError(t, db.WithContext(context.Background()).Create(&models.ContainerRegistry{
		URL:          "ghcr.io",
		Enabled:      true,
		RegistryType: registryTypeGeneric,
	}).Error)

	svc := NewContainerRegistryService(db, nil, NewKVService(db))
	require.NoError(t, svc.RecordImagePull(context.Background(), "ghcr.io/example/app:latest"))
	require.NoError(t, svc.RecordImagePull(context.Background(), "ghcr.io/example/worker:latest"))

	result, err := svc.GetRegistryPullUsage(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Registries, 1)
	assert.Equal(t, "ghcr.io", result.Registries[0].Registry)
	assert.Equal(t, int64(2), result.Registries[0].ObservedPulls)
	assert.Nil(t, result.Registries[0].Limit)
}

func TestContainerRegistryService_CreateRegistry_RejectsUnsupportedRegistryType(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	_, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:          "registry.example.com",
		RegistryType: "ECR-ish",
	})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "registryType", validationErr.Field)
}

func TestContainerRegistryService_CreateRegistry_RejectsEmptyUsernameForGeneric(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	_, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "",
		Token:    "my-token",
	})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "username", validationErr.Field)
}

func TestContainerRegistryService_CreateRegistry_RejectsEmptyTokenForGeneric(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	_, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "",
	})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "token", validationErr.Field)
}

func TestContainerRegistryService_CreateRegistry_AcceptsValidGenericCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	reg, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "my-token",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-user", reg.Username)
	assert.NotEmpty(t, reg.Token)
}

func TestContainerRegistryService_UpdateRegistry_RejectsBlankingUsername(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	reg, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "my-token",
	})
	require.NoError(t, err)

	_, err = svc.UpdateRegistry(context.Background(), reg.ID, models.UpdateContainerRegistryRequest{
		Username: new(""),
	})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "username", validationErr.Field)
}

func TestContainerRegistryService_UpdateRegistry_KeepsExistingTokenWhenNotProvided(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	reg, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "my-token",
	})
	require.NoError(t, err)
	originalToken := reg.Token

	updated, err := svc.UpdateRegistry(context.Background(), reg.ID, models.UpdateContainerRegistryRequest{
		Username: new("updated-user"),
	})
	require.NoError(t, err)
	assert.Equal(t, "updated-user", updated.Username)
	assert.Equal(t, originalToken, updated.Token)
}

func TestContainerRegistryService_UpdateRegistry_RejectsChangingRegistryType(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	reg, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "my-token",
	})
	require.NoError(t, err)

	_, err = svc.UpdateRegistry(context.Background(), reg.ID, models.UpdateContainerRegistryRequest{
		RegistryType: new("ecr"),
	})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "registryType", validationErr.Field)
}

func TestContainerRegistryService_UpdateRegistry_AllowsSameRegistryType(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	svc := NewContainerRegistryService(db, nil, nil)

	reg, err := svc.CreateRegistry(context.Background(), models.CreateContainerRegistryRequest{
		URL:      "https://registry.example.com",
		Username: "my-user",
		Token:    "my-token",
	})
	require.NoError(t, err)

	updated, err := svc.UpdateRegistry(context.Background(), reg.ID, models.UpdateContainerRegistryRequest{
		RegistryType: new("generic"),
		Username:     new("updated-user"),
	})
	require.NoError(t, err)
	assert.Equal(t, "updated-user", updated.Username)
	assert.Equal(t, "generic", updated.RegistryType)
}

func TestContainerRegistryService_SyncRegistries_ClearsGenericTokenWhenManagerSendsEmptyValue(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://registry.example.com", "registry-user", "old-token")

	var existing models.ContainerRegistry
	require.NoError(t, db.WithContext(context.Background()).First(&existing).Error)

	svc := NewContainerRegistryService(db, nil, nil)
	err := svc.SyncRegistries(context.Background(), []containerregistry.Sync{
		{
			ID:           existing.ID,
			URL:          existing.URL,
			Username:     existing.Username,
			Token:        "",
			Enabled:      true,
			RegistryType: registryTypeGeneric,
			CreatedAt:    existing.CreatedAt,
			UpdatedAt:    existing.UpdatedAt,
		},
	})
	require.NoError(t, err)

	var updated models.ContainerRegistry
	require.NoError(t, db.WithContext(context.Background()).First(&updated, "id = ?", existing.ID).Error)

	decryptedToken, err := crypto.Decrypt(updated.Token)
	require.NoError(t, err)
	assert.Empty(t, decryptedToken)
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
	}, nil)

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
	}, nil)

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
	}, nil)

	err := svc.TestRegistry(context.Background(), "registry.example.com", "", "")
	require.NoError(t, err)

	err = svc.TestRegistry(context.Background(), "registry.example.com", "  ", "  ")
	require.NoError(t, err)
}

func TestContainerRegistryService_InspectImageDigest_AnonymousSuccess(t *testing.T) {
	wantDigest := digest.FromString("anonymous-success").String()

	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				assert.Equal(t, "registry.example.com:5443/team/app:1.2.3", imageRef)
				assert.Empty(t, options.EncodedRegistryAuth)
				return client.DistributionInspectResult{
					DistributionInspect: dockerregistry.DistributionInspect{
						Descriptor: ocispec.Descriptor{
							Digest: digest.Digest(wantDigest),
						},
					},
				}, nil
			},
		}, nil
	}, nil)

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry.example.com:5443/team/app:1.2.3", nil)
	require.NoError(t, err)
	assert.Equal(t, wantDigest, result.Digest)
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Equal(t, "registry.example.com:5443", result.AuthRegistry)
}

func TestContainerRegistryService_InspectImageDigest_RetriesWithStoredCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")
	wantDigest := digest.FromString("stored-credentials").String()

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
							Digest: digest.Digest(wantDigest),
						},
					},
				}, nil
			},
		}, nil
	}, nil)

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry-1.docker.io/library/nginx:latest", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, wantDigest, result.Digest)
	assert.Equal(t, "credential", result.AuthMethod)
	assert.Equal(t, "docker-user", result.AuthUsername)
	assert.True(t, result.UsedCredential)
}

func TestContainerRegistryService_InspectImageDigest_FallsBackWhenDistributionNotFound(t *testing.T) {
	wantDigest := digest.FromString("fallback-not-found").String()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/team/app/manifests/1.2.3" {
			w.Header().Set("Docker-Content-Digest", wantDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	var calls int
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				calls++
				assert.Equal(t, serverURL.Host+"/team/app:1.2.3", imageRef)
				assert.Empty(t, options.EncodedRegistryAuth)
				return client.DistributionInspectResult{}, errors.New("Error response from daemon: Not Found")
			},
		}, nil
	}, nil)
	svc.distributionHTTPClient = server.Client()

	result, err := svc.inspectImageDigestInternal(context.Background(), serverURL.Host+"/team/app:1.2.3", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, wantDigest, result.Digest)
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Equal(t, serverURL.Host, result.AuthRegistry)
}

func TestContainerRegistryService_InspectImageDigest_FallsBackWhenDistributionForbidden(t *testing.T) {
	wantDigest := digest.FromString("fallback-forbidden").String()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/team/app/manifests/1.2.3" {
			w.Header().Set("Docker-Content-Digest", wantDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	var calls int
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				calls++
				assert.Equal(t, serverURL.Host+"/team/app:1.2.3", imageRef)
				assert.Empty(t, options.EncodedRegistryAuth)
				return client.DistributionInspectResult{}, errors.New("Error response from daemon: <html><body><h1>403 Forbidden</h1> Request forbidden by administrative rules. </body></html>")
			},
		}, nil
	}, nil)
	svc.distributionHTTPClient = server.Client()

	result, err := svc.inspectImageDigestInternal(context.Background(), serverURL.Host+"/team/app:1.2.3", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, wantDigest, result.Digest)
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Equal(t, serverURL.Host, result.AuthRegistry)
}

func TestContainerRegistryService_InspectImageDigest_RetriesStoredCredentialsAfterRegistryAuth403(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	wantDigest := digest.FromString("stored-credential-fallback").String()

	var authHeaders []string
	var tokenURL string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/team/app/manifests/1.2.3":
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			switch len(authHeaders) {
			case 1:
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenURL+`",service="registry.example.com"`)
				w.WriteHeader(http.StatusUnauthorized)
			case 2:
				w.WriteHeader(http.StatusForbidden)
			case 3:
				w.Header().Set("Docker-Content-Digest", wantDigest)
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected manifest call %d", len(authHeaders))
			}
		case "/token":
			username, password, ok := r.BasicAuth()
			if !ok {
				require.Equal(t, "", r.Header.Get("Authorization"))
				require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
					"token": "anonymous-token",
				}))
				return
			}

			require.Equal(t, "stored-user", username)
			require.Equal(t, "stored-token", password)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"token": "credential-token",
			}))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	tokenURL = server.URL + "/token"

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	createTestPullRegistry(t, db, server.URL, "stored-user", "stored-token")

	svc := NewContainerRegistryService(db, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				return client.DistributionInspectResult{}, errors.New("Error response from daemon: Not Found")
			},
		}, nil
	}, nil)
	svc.distributionHTTPClient = server.Client()

	result, err := svc.inspectImageDigestInternal(context.Background(), serverURL.Host+"/team/app:1.2.3", nil)
	require.NoError(t, err)
	assert.Equal(t, wantDigest, result.Digest)
	assert.Equal(t, "credential", result.AuthMethod)
	assert.Equal(t, "stored-user", result.AuthUsername)
	assert.True(t, result.UsedCredential)
	require.Len(t, authHeaders, 3)
	assert.Equal(t, "", authHeaders[0])
	assert.Equal(t, "Bearer anonymous-token", authHeaders[1])
	assert.Equal(t, "Basic c3RvcmVkLXVzZXI6c3RvcmVkLXRva2Vu", authHeaders[2])
}

func TestContainerRegistryService_InspectImageDigest_DoesNotFallbackOnTLSFailure(t *testing.T) {
	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				assert.Equal(t, "registry.example.com/team/app:1.2.3", imageRef)
				assert.Empty(t, options.EncodedRegistryAuth)
				return client.DistributionInspectResult{}, errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")
			},
		}, nil
	}, nil)

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry.example.com/team/app:1.2.3", nil)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, strings.ToLower(err.Error()), "x509")
	assert.NotContains(t, err.Error(), "registry fallback failed")
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Equal(t, "registry.example.com", result.AuthRegistry)
}

func TestContainerRegistryService_InspectImageDigest_PreservesDaemonAndFallbackErrors(t *testing.T) {
	daemonErr := errors.New("Error response from daemon: Not Found")
	fallbackErr := errors.New("dial tcp: i/o timeout")

	svc := NewContainerRegistryService(nil, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				return client.DistributionInspectResult{}, daemonErr
			},
		}, nil
	}, nil)
	svc.distributionHTTPClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fallbackErr
		}),
	}

	result, err := svc.inspectImageDigestInternal(context.Background(), "registry.example.com/team/app:1.2.3", nil)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.ErrorIs(t, err, daemonErr)
	assert.ErrorIs(t, err, fallbackErr)
}

func TestContainerRegistryService_InspectImageDigest_PreservesAnonymousUnauthorizedWhenCredentialLookupFails(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	var tokenURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/team/app/manifests/1.2.3":
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenURL+`",service="registry.example.com"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"token": "anonymous-token",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tokenURL = server.URL + "/token"

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	svc := NewContainerRegistryService(db, func(context.Context) (RegistryDaemonClient, error) {
		return &fakeRegistryDaemonClient{
			distributionInspectFn: func(ctx context.Context, imageRef string, options client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
				return client.DistributionInspectResult{}, errors.New("Error response from daemon: Not Found")
			},
		}, nil
	}, nil)
	svc.distributionHTTPClient = server.Client()

	result, err := svc.inspectImageDigestInternal(context.Background(), serverURL.Host+"/team/app:1.2.3", nil)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "anonymous", result.AuthMethod)
	assert.Contains(t, err.Error(), "anonymous access unauthorized")
	assert.Contains(t, err.Error(), "status: 401")
	assert.Contains(t, err.Error(), "failed to load enabled registries")
}
