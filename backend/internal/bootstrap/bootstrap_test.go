package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeTunnelGRPCRequestPathInternal(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("nil request", func(t *testing.T) {
		assert.Nil(t, normalizeTunnelGRPCRequestPathInternal(nil))
	})

	t.Run("path without prefix remains unchanged", func(t *testing.T) {
		req := httptest.NewRequest("POST", fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.Same(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
	})

	t.Run("api prefix is removed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api"+fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("legacy api tunnel path maps to grpc method", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/tunnel/connect", nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("prefixed legacy api tunnel path maps to grpc method", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api/tunnel/connect", nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("nested proxy prefix is removed up to method path", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api"+fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})
}

func TestIsTunnelGRPCRequestInternal(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("detects by grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", nil)
		req.Header.Set("Content-Type", "application/grpc")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by grpc-web content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", nil)
		req.Header.Set("Content-Type", "application/grpc-web+proto")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by method path without grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fullMethodPath, nil)
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by legacy tunnel path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tunnel/connect", nil)
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match regular api requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/environments/pair", nil)
		req.Header.Set("Content-Type", "application/json")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("requires post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fullMethodPath, nil)
		req.Header.Set("Content-Type", "application/grpc")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})
}
