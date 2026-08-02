package services

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestReadAttestationLayerBytesInternalDecompressesGzip(t *testing.T) {
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

	layer := static.NewLayer(buf.Bytes(), inTotoLayerMediaTypeInternal)
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected decompressed statement %q, got %q", statement, got)

}

func TestReadAttestationLayerBytesInternalPassesThroughRawJSON(t *testing.T) {
	statement := []byte(`{"predicateType":"https://spdx.dev/Document"}`)

	layer := static.NewLayer(statement, inTotoLayerMediaTypeInternal)
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected raw statement unchanged %q, got %q", statement, got)

}

func TestReadAttestationLayerBytesInternalDecompressesZstd(t *testing.T) {
	statement := []byte(`{"predicateType":"https://slsa.dev/provenance/v1"}`)

	encoder, err := zstd.NewWriter(nil)

	require.NoError(t, err,
		"zstd writer: %v", err)

	compressed := encoder.EncodeAll(statement, nil)
	{
		err := encoder.Close()
		require.NoError(t, err,
			"zstd close: %v", err)
	}

	layer := static.NewLayer(compressed, inTotoLayerMediaTypeInternal)
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")

	require.NoError(t, err,
		"unexpected error: %v", err)

	require.True(t, bytes.Equal(got, statement),
		"expected decompressed statement %q, got %q", statement, got)

}
