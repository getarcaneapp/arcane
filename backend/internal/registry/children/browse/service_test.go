package browse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

// newTestRegistry starts an in-memory OCI registry and returns its
// host as "localhost:PORT" so go-containerregistry resolves it over plain HTTP.
func newTestRegistry(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	return "localhost:" + serverURL.Port()
}

// testRegistries is an in-memory registry store keyed by ID.
type testRegistries map[string]types.ContainerRegistryCredential

func (r testRegistries) add(registryURL, username, token string) string {
	id := fmt.Sprintf("registry-%d", len(r)+1)
	r[id] = types.ContainerRegistryCredential{URL: registryURL, Username: username, Token: token, Enabled: true}
	return id
}

func newTestService(registries testRegistries) *Service {
	return NewService(
		func(_ context.Context, id string) (types.ContainerRegistry, *types.ContainerRegistryCredential, error) {
			credential, ok := registries[id]
			if !ok {
				return types.ContainerRegistry{}, nil, common.Classify(common.ErrNotFound, errors.New("registry not found"))
			}
			registryRecord := types.ContainerRegistry{ID: id, URL: credential.URL}
			if credential.Username == "" {
				return registryRecord, nil, nil
			}
			return registryRecord, &credential, nil
		},
		func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, time.Minute)
		},
		nil,
	)
}

func platformImage(t *testing.T, platform v1.Platform, created time.Time) v1.Image {
	t.Helper()

	img, err := random.Image(128, 2)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	img, err = mutate.ConfigFile(img, &v1.ConfigFile{
		Architecture: platform.Architecture,
		Created:      v1.Time{Time: created},
		History:      cfg.History,
		OS:           platform.OS,
		RootFS:       cfg.RootFS,
		Config:       cfg.Config,
		Variant:      platform.Variant,
	})
	require.NoError(t, err)
	return img
}

func mustParseReference(t *testing.T, imageRef string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(imageRef)
	require.NoError(t, err)
	return ref
}

func writeImage(t *testing.T, imageRef string, img v1.Image, options ...remote.Option) {
	t.Helper()
	require.NoError(t, remote.Write(mustParseReference(t, imageRef), img, options...))
}

func imageSize(t *testing.T, img v1.Image) int64 {
	t.Helper()
	digest, err := img.Digest()
	require.NoError(t, err)
	platform, err := tagPlatform(img, nil, digest)
	require.NoError(t, err)
	return platform.Size
}

func browseParams(search string) pagination.QueryParams {
	return pagination.QueryParams{
		Search: search,
		Start:  0, Limit: 20,
	}
}

func TestService_ListRepositories(t *testing.T) {
	host := newTestRegistry(t)
	img := platformImage(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now())
	writeImage(t, host+"/team/api:1.0", img)
	writeImage(t, host+"/team/web:1.0", img)
	writeImage(t, host+"/other/tool:1.0", img)

	registries := testRegistries{}
	svc := newTestService(registries)
	ctx := t.Context()

	id := registries.add(host, "", "")
	repositories, page, err := svc.ListRepositories(ctx, id, browseParams(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"other/tool", "team/api", "team/web"}, repositoryNames(repositories))
	assert.EqualValues(t, 3, page.TotalItems)

	repositories, _, err = svc.ListRepositories(ctx, id, browseParams("web"))
	require.NoError(t, err)
	assert.Equal(t, []string{"team/web"}, repositoryNames(repositories))

	namespacedID := registries.add("http://"+host+"/team/", "", "")
	repositories, _, err = svc.ListRepositories(ctx, namespacedID, browseParams(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"team/api", "team/web"}, repositoryNames(repositories))
}

func TestService_ListRepositoryTagsSingleImage(t *testing.T) {
	host := newTestRegistry(t)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	img := platformImage(t, v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, created)
	writeImage(t, host+"/team/api:1.0", img)
	writeImage(t, host+"/team/api:2.0", img)

	registries := testRegistries{}
	svc := newTestService(registries)
	id := registries.add(host, "", "")

	tags, page, err := svc.ListRepositoryTags(t.Context(), id, "team/api", pagination.QueryParams{Start: 0, Limit: 1})
	require.NoError(t, err)
	assert.EqualValues(t, 2, page.TotalItems)
	require.Len(t, tags, 1)

	digest, err := img.Digest()
	require.NoError(t, err)
	size := imageSize(t, img)

	tag := tags[0]
	assert.Equal(t, "1.0", tag.Name)
	assert.Empty(t, tag.Error)
	assert.Equal(t, digest.String(), tag.Digest)
	assert.Equal(t, size, tag.Size)
	require.NotNil(t, tag.Created)
	assert.True(t, created.Equal(*tag.Created))
	require.Len(t, tag.Platforms, 1)
	assert.Equal(t, "linux", tag.Platforms[0].OS)
	assert.Equal(t, "arm64", tag.Platforms[0].Architecture)
	assert.Equal(t, "v8", tag.Platforms[0].Variant)
}

func TestService_ListRepositoryTagsIndex(t *testing.T) {
	host := newTestRegistry(t)
	amd64 := v1.Platform{OS: "linux", Architecture: "amd64"}
	arm64 := v1.Platform{OS: "linux", Architecture: "arm64"}
	amd64Image := platformImage(t, amd64, time.Now())
	arm64Image := platformImage(t, arm64, time.Now())
	attestation, err := random.Image(32, 1)
	require.NoError(t, err)

	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: arm64Image, Platform: &arm64},
		mutate.IndexAddendum{Add: amd64Image, Platform: &amd64},
		mutate.IndexAddendum{Add: attestation, Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}},
	)
	ref, err := name.ParseReference(host + "/team/api:multi")
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(ref, index))

	registries := testRegistries{}
	svc := newTestService(registries)
	id := registries.add(host, "", "")

	tags, _, err := svc.ListRepositoryTags(t.Context(), id, "team/api", browseParams(""))
	require.NoError(t, err)
	require.Len(t, tags, 1)

	amd64Size := imageSize(t, amd64Image)
	arm64Size := imageSize(t, arm64Image)

	tag := tags[0]
	assert.Empty(t, tag.Error)
	assert.Nil(t, tag.Created)
	assert.Equal(t, amd64Size+arm64Size, tag.Size)
	require.Len(t, tag.Platforms, 2)
	assert.Equal(t, "amd64", tag.Platforms[0].Architecture)
	assert.Equal(t, amd64Size, tag.Platforms[0].Size)
	assert.Equal(t, "arm64", tag.Platforms[1].Architecture)
}

func TestService_DeleteRepositoryTag(t *testing.T) {
	host := newTestRegistry(t)
	deleted := platformImage(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now())
	writeImage(t, host+"/team/api:1.0", deleted)
	writeImage(t, host+"/team/api:2.0", platformImage(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()))

	registries := testRegistries{}
	svc := newTestService(registries)
	id := registries.add(host, "", "")
	ctx := t.Context()

	digest, err := svc.DeleteRepositoryTag(ctx, id, "team/api", "1.0")
	require.NoError(t, err)
	deletedDigest, err := deleted.Digest()
	require.NoError(t, err)
	assert.Equal(t, deletedDigest.String(), digest)

	_, err = remote.Head(mustParseReference(t, host+"/team/api@"+digest))
	require.Error(t, err)
	_, err = remote.Head(mustParseReference(t, host+"/team/api:2.0"))
	require.NoError(t, err)
}

func TestService_BrowseUsesStoredCredentials(t *testing.T) {
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "robot" || password != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		registryHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	host := "localhost:" + serverURL.Port()

	writeImage(t, host+"/private/app:1.0", platformImage(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()),
		remote.WithAuth(&authn.Basic{Username: "robot", Password: "secret"}))

	registries := testRegistries{}
	svc := newTestService(registries)
	ctx := t.Context()

	id := registries.add(host, "robot", "secret")
	repositories, _, err := svc.ListRepositories(ctx, id, browseParams(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"private/app"}, repositoryNames(repositories))

	anonymousID := registries.add(host, "", "")
	_, _, err = svc.ListRepositories(ctx, anonymousID, browseParams(""))
	assert.ErrorIs(t, err, common.ErrBadRequest)
}

func TestService_BrowseErrors(t *testing.T) {
	host := newTestRegistry(t)
	writeImage(t, host+"/team/api:1.0", platformImage(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()))

	registries := testRegistries{}
	svc := newTestService(registries)
	id := registries.add(host, "", "")
	namespacedID := registries.add("http://"+host+"/team/", "", "")

	tests := []struct {
		name    string
		call    func(ctx context.Context) error
		wantErr error
	}{
		{
			name: "unknown registry",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositories(ctx, "missing", browseParams(""))
				return err
			},
			wantErr: common.ErrNotFound,
		},
		{
			name: "empty repository",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositoryTags(ctx, id, " / ", browseParams(""))
				return err
			},
			wantErr: common.ErrValidation,
		},
		{
			name: "tags outside registry namespace",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositoryTags(ctx, namespacedID, "other/tool", browseParams(""))
				return err
			},
			wantErr: common.ErrValidation,
		},
		{
			name: "delete outside registry namespace",
			call: func(ctx context.Context) error {
				_, err := svc.DeleteRepositoryTag(ctx, namespacedID, "other/tool", "1.0")
				return err
			},
			wantErr: common.ErrValidation,
		},
		{
			name: "delete missing tag",
			call: func(ctx context.Context) error {
				_, err := svc.DeleteRepositoryTag(ctx, id, "team/api", "missing")
				return err
			},
			wantErr: common.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, tt.call(t.Context()), tt.wantErr)
		})
	}
}

func repositoryNames(repositories []types.ContainerRegistryRepository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.Name)
	}
	return names
}
