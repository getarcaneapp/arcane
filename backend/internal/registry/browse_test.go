package registry

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
)

func createBrowseTestRegistryInternal(t *testing.T, db *database.DB, registryURL, username, token string) string {
	t.Helper()

	record := &ContainerRegistry{URL: registryURL, Username: username, Enabled: true, RegistryType: RegistryTypeGeneric}
	if token != "" {
		encryptedToken, err := crypto.Encrypt(token)
		require.NoError(t, err)
		record.Token = encryptedToken
	}
	require.NoError(t, db.WithContext(context.Background()).Create(record).Error)
	return record.ID
}

func platformImageInternal(t *testing.T, platform v1.Platform, created time.Time) v1.Image {
	t.Helper()

	img, err := random.Image(128, 2)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.OS = platform.OS
	cfg.Architecture = platform.Architecture
	cfg.Variant = platform.Variant
	cfg.Created = v1.Time{Time: created}
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	return img
}

func mustParseReferenceInternal(t *testing.T, imageRef string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(imageRef)
	require.NoError(t, err)
	return ref
}

func writeImageInternal(t *testing.T, imageRef string, img v1.Image, options ...remote.Option) {
	t.Helper()
	require.NoError(t, remote.Write(mustParseReferenceInternal(t, imageRef), img, options...))
}

func browseParamsInternal(search string) pagination.QueryParams {
	return pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{Search: search},
		Params:      pagination.Params{Start: 0, Limit: 20},
	}
}

func TestContainerRegistryService_ListRepositoriesInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	img := platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now())
	writeImageInternal(t, host+"/team/api:1.0", img)
	writeImageInternal(t, host+"/team/web:1.0", img)
	writeImageInternal(t, host+"/other/tool:1.0", img)

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	ctx := context.Background()

	id := createBrowseTestRegistryInternal(t, db, host, "", "")
	repositories, page, err := svc.ListRepositories(ctx, id, browseParamsInternal(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"other/tool", "team/api", "team/web"}, repositoryNamesInternal(repositories))
	assert.EqualValues(t, 3, page.TotalItems)

	repositories, _, err = svc.ListRepositories(ctx, id, browseParamsInternal("web"))
	require.NoError(t, err)
	assert.Equal(t, []string{"team/web"}, repositoryNamesInternal(repositories))

	namespacedID := createBrowseTestRegistryInternal(t, db, "http://"+host+"/team/", "", "")
	repositories, _, err = svc.ListRepositories(ctx, namespacedID, browseParamsInternal(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"team/api", "team/web"}, repositoryNamesInternal(repositories))
}

func TestContainerRegistryService_ListRepositoryTagsSingleImageInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	img := platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}, created)
	writeImageInternal(t, host+"/team/api:1.0", img)
	writeImageInternal(t, host+"/team/api:2.0", img)

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	id := createBrowseTestRegistryInternal(t, db, host, "", "")

	tags, page, err := svc.ListRepositoryTags(context.Background(), id, "team/api", pagination.QueryParams{Params: pagination.Params{Start: 0, Limit: 1}})
	require.NoError(t, err)
	assert.EqualValues(t, 2, page.TotalItems)
	require.Len(t, tags, 1)

	digest, err := img.Digest()
	require.NoError(t, err)
	size, err := imageSizeInternal(img)
	require.NoError(t, err)

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

func TestContainerRegistryService_ListRepositoryTagsIndexInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	amd64 := v1.Platform{OS: "linux", Architecture: "amd64"}
	arm64 := v1.Platform{OS: "linux", Architecture: "arm64"}
	amd64Image := platformImageInternal(t, amd64, time.Now())
	arm64Image := platformImageInternal(t, arm64, time.Now())
	attestation, err := random.Image(32, 1)
	require.NoError(t, err)

	index := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: arm64Image, Descriptor: v1.Descriptor{Platform: &arm64}},
		mutate.IndexAddendum{Add: amd64Image, Descriptor: v1.Descriptor{Platform: &amd64}},
		mutate.IndexAddendum{Add: attestation, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}}},
	)
	ref, err := name.ParseReference(host + "/team/api:multi")
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(ref, index))

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	id := createBrowseTestRegistryInternal(t, db, host, "", "")

	tags, _, err := svc.ListRepositoryTags(context.Background(), id, "team/api", browseParamsInternal(""))
	require.NoError(t, err)
	require.Len(t, tags, 1)

	amd64Size, err := imageSizeInternal(amd64Image)
	require.NoError(t, err)
	arm64Size, err := imageSizeInternal(arm64Image)
	require.NoError(t, err)

	tag := tags[0]
	assert.Empty(t, tag.Error)
	assert.Nil(t, tag.Created)
	assert.Equal(t, amd64Size+arm64Size, tag.Size)
	require.Len(t, tag.Platforms, 2)
	assert.Equal(t, "amd64", tag.Platforms[0].Architecture)
	assert.Equal(t, amd64Size, tag.Platforms[0].Size)
	assert.Equal(t, "arm64", tag.Platforms[1].Architecture)
}

func TestContainerRegistryService_DeleteRepositoryTagInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	deleted := platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now())
	writeImageInternal(t, host+"/team/api:1.0", deleted)
	writeImageInternal(t, host+"/team/api:2.0", platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()))

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	id := createBrowseTestRegistryInternal(t, db, host, "", "")
	ctx := context.Background()

	digest, err := svc.DeleteRepositoryTag(ctx, id, "team/api", "1.0")
	require.NoError(t, err)
	deletedDigest, err := deleted.Digest()
	require.NoError(t, err)
	assert.Equal(t, deletedDigest.String(), digest)

	_, err = remote.Head(mustParseReferenceInternal(t, host+"/team/api@"+digest))
	require.Error(t, err)
	_, err = remote.Head(mustParseReferenceInternal(t, host+"/team/api:2.0"))
	require.NoError(t, err)
}

func TestContainerRegistryService_BrowseUsesStoredCredentialsInternal(t *testing.T) {
	registryHandler := ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0)))
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

	writeImageInternal(t, host+"/private/app:1.0", platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()),
		remote.WithAuth(&authn.Basic{Username: "robot", Password: "secret"}))

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	ctx := context.Background()

	id := createBrowseTestRegistryInternal(t, db, host, "robot", "secret")
	repositories, _, err := svc.ListRepositories(ctx, id, browseParamsInternal(""))
	require.NoError(t, err)
	assert.Equal(t, []string{"private/app"}, repositoryNamesInternal(repositories))

	anonymousID := createBrowseTestRegistryInternal(t, db, host, "", "")
	_, _, err = svc.ListRepositories(ctx, anonymousID, browseParamsInternal(""))
	assert.ErrorIs(t, err, common.ErrBadRequest)
}

func TestContainerRegistryService_BrowseErrorsInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	writeImageInternal(t, host+"/team/api:1.0", platformImageInternal(t, v1.Platform{OS: "linux", Architecture: "amd64"}, time.Now()))

	db := setupContainerRegistryTestDBInternal(t)
	svc := NewContainerRegistryService(db, nil, nil)
	id := createBrowseTestRegistryInternal(t, db, host, "", "")
	namespacedID := createBrowseTestRegistryInternal(t, db, "http://"+host+"/team/", "", "")

	tests := []struct {
		name    string
		call    func(ctx context.Context) error
		wantErr error
	}{
		{
			name: "unknown registry",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositories(ctx, "missing", browseParamsInternal(""))
				return err
			},
			wantErr: common.ErrNotFound,
		},
		{
			name: "empty repository",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositoryTags(ctx, id, " / ", browseParamsInternal(""))
				return err
			},
			wantErr: common.ErrValidation,
		},
		{
			name: "tags outside registry namespace",
			call: func(ctx context.Context) error {
				_, _, err := svc.ListRepositoryTags(ctx, namespacedID, "other/tool", browseParamsInternal(""))
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

func repositoryNamesInternal(repositories []containerregistry.Repository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.Name)
	}
	return names
}
