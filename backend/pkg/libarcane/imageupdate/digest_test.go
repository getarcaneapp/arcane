package imageupdate

import (
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDigest(t *testing.T) {
	want := digest.FromString("arcane").String()

	got, err := NormalizeDigest("  " + want + "  ")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNormalizeDigest_InvalidDigest(t *testing.T) {
	_, err := NormalizeDigest("sha256:not-a-valid-digest")
	require.Error(t, err)
}

func TestDigestFromReferenceSuffix(t *testing.T) {
	want := digest.FromString("arcane-reference").String()

	got, ok := DigestFromReferenceSuffix("docker.io/library/nginx@" + want)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestDigestFromReferenceSuffix_InvalidDigest(t *testing.T) {
	_, ok := DigestFromReferenceSuffix("docker.io/library/nginx@sha256:bad")
	assert.False(t, ok)
}

func TestParseImageRef(t *testing.T) {
	redisDigest := digest.FromString("redis").String()

	tests := []struct {
		name     string
		imageRef string
		wantHost string
		wantRepo string
		wantTag  string
	}{
		{
			name:     "docker hub official image",
			imageRef: "nginx:latest",
			wantHost: "docker.io",
			wantRepo: "library/nginx",
			wantTag:  "latest",
		},
		{
			name:     "custom registry image",
			imageRef: "ghcr.io/getarcaneapp/arcane:v1.2.3",
			wantHost: "ghcr.io",
			wantRepo: "getarcaneapp/arcane",
			wantTag:  "v1.2.3",
		},
		{
			name:     "digest reference defaults to latest tag",
			imageRef: "docker.io/library/redis@" + redisDigest,
			wantHost: "docker.io",
			wantRepo: "library/redis",
			wantTag:  "latest",
		},
		{
			name:     "docker registry variant is normalized",
			imageRef: "registry-1.docker.io/library/busybox:1.36",
			wantHost: "docker.io",
			wantRepo: "library/busybox",
			wantTag:  "1.36",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := NormalizeReference(tt.imageRef)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantHost, parts.RegistryHost)
			assert.Equal(t, tt.wantRepo, parts.Repository)
			assert.Equal(t, tt.wantTag, parts.Tag)
		})
	}
}

func TestNormalizeRef(t *testing.T) {
	parts, err := NormalizeReference("nginx")
	assert.NoError(t, err)
	assert.Equal(t, "docker.io/library/nginx:latest", parts.NormalizedRef)

	parts, err = NormalizeReference("index.docker.io/library/nginx:latest")
	assert.NoError(t, err)
	assert.Equal(t, "docker.io/library/nginx:latest", parts.NormalizedRef)

	parts, err = NormalizeReference("ghcr.io/getarcaneapp/arcane:v1")
	assert.NoError(t, err)
	assert.Equal(t, "ghcr.io/getarcaneapp/arcane:v1", parts.NormalizedRef)
}
