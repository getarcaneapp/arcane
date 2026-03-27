package imagedigest

import (
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	want := digest.FromString("arcane").String()

	got, err := Normalize("  " + want + "  ")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNormalize_InvalidDigest(t *testing.T) {
	_, err := Normalize("sha256:not-a-valid-digest")
	require.Error(t, err)
}

func TestFromReferenceSuffix(t *testing.T) {
	want := digest.FromString("arcane-reference").String()

	got, ok := FromReferenceSuffix("docker.io/library/nginx@" + want)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestFromReferenceSuffix_InvalidDigest(t *testing.T) {
	_, ok := FromReferenceSuffix("docker.io/library/nginx@sha256:bad")
	assert.False(t, ok)
}
