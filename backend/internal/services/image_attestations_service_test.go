package services

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/klauspost/compress/zstd"
)

func TestReadAttestationLayerBytesInternalDecompressesGzip(t *testing.T) {
	statement := []byte(`{"predicateType":"https://slsa.dev/provenance/v0.2"}`)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(statement); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	layer := static.NewLayer(buf.Bytes(), types.MediaType(inTotoLayerMediaTypeInternal))
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, statement) {
		t.Fatalf("expected decompressed statement %q, got %q", statement, got)
	}
}

func TestReadAttestationLayerBytesInternalPassesThroughRawJSON(t *testing.T) {
	statement := []byte(`{"predicateType":"https://spdx.dev/Document"}`)

	layer := static.NewLayer(statement, types.MediaType(inTotoLayerMediaTypeInternal))
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, statement) {
		t.Fatalf("expected raw statement unchanged %q, got %q", statement, got)
	}
}

func TestReadAttestationLayerBytesInternalDecompressesZstd(t *testing.T) {
	statement := []byte(`{"predicateType":"https://slsa.dev/provenance/v1"}`)

	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	compressed := encoder.EncodeAll(statement, nil)
	if err := encoder.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}

	layer := static.NewLayer(compressed, types.MediaType(inTotoLayerMediaTypeInternal))
	got, err := readAttestationLayerBytesInternal(layer, "sha256:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, statement) {
		t.Fatalf("expected decompressed statement %q, got %q", statement, got)
	}
}
