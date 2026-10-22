package attestations

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func TestReadAttestationLayerBytesDecompressesGzip(t *testing.T) {
	statement := []byte(`{"predicateType":"https://slsa.dev/provenance/v0.2"}`)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	{
		_, err := gz.Write(statement)
		require.NoError(t, err,
			"gzip write: %v", err)
	}
	{

		err := gz.Close()
		require.NoError(t, err,
			"gzip close: %v", err)
	}

	layer := static.NewLayer(buf.Bytes(), inTotoLayerMediaType)
	got, err := readAttestationLayerBytes(t.Context(), layer, "sha256:test", int64(buf.Len()))

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected decompressed statement %q, got %q", statement, got)
}

func TestReadAttestationLayerBytesPassesThroughRawJSON(t *testing.T) {
	statement := []byte(`{"predicateType":"https://spdx.dev/Document"}`)

	layer := static.NewLayer(statement, inTotoLayerMediaType)
	got, err := readAttestationLayerBytes(t.Context(), layer, "sha256:test", int64(len(statement)))

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected raw statement unchanged %q, got %q", statement, got)
}

func TestReadAttestationLayerBytesDecompressesZstd(t *testing.T) {
	statement := []byte(`{"predicateType":"https://slsa.dev/provenance/v1"}`)

	encoder, err := zstd.NewWriter(nil)

	require.NoError(t, err,
		"zstd writer: %v", err)

	compressed := encoder.EncodeAll(statement, nil)
	{
		closeErr := encoder.Close()
		require.NoError(t, closeErr,
			"zstd close: %v", closeErr)
	}

	layer := static.NewLayer(compressed, inTotoLayerMediaType)
	got, err := readAttestationLayerBytes(t.Context(), layer, "sha256:test", int64(len(compressed)))

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected decompressed statement %q, got %q", statement, got)
}

func TestReadAttestationLayerBytesRejectsOversizedDecompression(t *testing.T) {
	oversized := make([]byte, maxAttestationLayerBytes+1)

	var gzipped bytes.Buffer
	gz := gzip.NewWriter(&gzipped)
	_, err := gz.Write(oversized)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	zstdCompressed := encoder.EncodeAll(oversized, nil)
	require.NoError(t, encoder.Close())

	for _, compressed := range [][]byte{gzipped.Bytes(), zstdCompressed} {
		layer := static.NewLayer(compressed, inTotoLayerMediaType)
		_, readErr := readAttestationLayerBytes(t.Context(), layer, "sha256:test", int64(len(compressed)))
		require.ErrorIs(t, readErr, errAttestationLayerTooLarge)
	}
}

func TestReadAttestationLayerBytesRejectsSizeMismatch(t *testing.T) {
	statement := []byte(`{"predicateType":"https://spdx.dev/Document"}`)

	_, err := readAttestationLayerBytes(t.Context(), static.NewLayer(statement, inTotoLayerMediaType), "sha256:test", int64(len(statement)+1))
	require.ErrorIs(t, err, common.ErrBadRequest)

	_, err = readAttestationLayerBytes(t.Context(), static.NewLayer(statement, inTotoLayerMediaType), "sha256:test", maxAttestationLayerBytes+1)
	require.ErrorIs(t, err, errAttestationLayerTooLarge)
}
