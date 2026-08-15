package registry

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLabelTestRegistryInternal starts an in-memory OCI registry and returns its
// host as "localhost:PORT" so go-containerregistry resolves it over plain HTTP.
func newLabelTestRegistryInternal(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(ggcrregistry.New(ggcrregistry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	return "localhost:" + serverURL.Port()
}

func pushLabeledImageInternal(t *testing.T, imageRef string, imageLabels map[string]string) v1.Image {
	t.Helper()
	img, err := random.Image(64, 1)
	require.NoError(t, err)

	if imageLabels != nil {
		cfg, err := img.ConfigFile()
		require.NoError(t, err)
		cfg = cfg.DeepCopy()
		cfg.Config.Labels = imageLabels
		img, err = mutate.ConfigFile(img, cfg)
		require.NoError(t, err)
	}

	ref, err := name.ParseReference(imageRef)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, img))
	return img
}

func TestContainerRegistryService_ImageVersionLabelInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	imageRef := host + "/getarcaneapp/arcane:next"
	img := pushLabeledImageInternal(t, imageRef, map[string]string{
		ociImageVersionLabel: "v2.8.0-next.66",
	})

	svc := NewContainerRegistryService(nil, nil, nil)

	label, err := svc.ImageVersionLabel(context.Background(), imageRef)
	require.NoError(t, err)
	assert.Equal(t, "v2.8.0-next.66", label)

	// Digest references resolve too — the version service prefers them.
	imgDigest, err := img.Digest()
	require.NoError(t, err)
	label, err = svc.ImageVersionLabel(context.Background(), host+"/getarcaneapp/arcane@"+imgDigest.String())
	require.NoError(t, err)
	assert.Equal(t, "v2.8.0-next.66", label)
}

func TestContainerRegistryService_ImageVersionLabelMissingLabelInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	imageRef := host + "/getarcaneapp/arcane:next"
	pushLabeledImageInternal(t, imageRef, nil)

	svc := NewContainerRegistryService(nil, nil, nil)
	_, err := svc.ImageVersionLabel(context.Background(), imageRef)
	assert.ErrorIs(t, err, ErrNoVersionLabel)
}

func TestContainerRegistryService_ImageVersionLabelErrorNotCachedInternal(t *testing.T) {
	host := newLabelTestRegistryInternal(t)
	imageRef := host + "/getarcaneapp/arcane:next"

	svc := NewContainerRegistryService(nil, nil, nil)

	// The image does not exist yet: the lookup must fail...
	_, err := svc.ImageVersionLabel(context.Background(), imageRef)
	require.Error(t, err)

	// ...and the failure must not be cached: after the push the same ref resolves.
	pushLabeledImageInternal(t, imageRef, map[string]string{
		ociImageVersionLabel: "v2.8.0-next.67",
	})
	label, err := svc.ImageVersionLabel(context.Background(), imageRef)
	require.NoError(t, err)
	assert.Equal(t, "v2.8.0-next.67", label)
}
