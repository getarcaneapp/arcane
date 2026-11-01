package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestNewTransport_Defaults(t *testing.T) {
	transport := NewTransport(TransportConfig{})

	assert.NotNil(t, transport.Proxy)
	assert.NotNil(t, transport.DialContext)
	assert.Nil(t, transport.TLSClientConfig)
	assert.True(t, transport.ForceAttemptHTTP2)
	assert.Equal(t, 100, transport.MaxIdleConns)
	assert.Zero(t, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
	assert.Equal(t, time.Second, transport.ExpectContinueTimeout)
	assert.Zero(t, transport.ResponseHeaderTimeout)
	assert.NotSame(t, http.DefaultTransport, transport)
	assert.NotSame(t, NewTransport(TransportConfig{}), transport)
}

func TestNewTransport_Overrides(t *testing.T) {
	errDial := errors.New("custom dial")
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	transport := NewTransport(TransportConfig{
		TLSClientConfig: tlsConfig,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errDial
		},
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxIdleConnsPerHost:   8,
		ReadBufferSize:        64 << 10,
		WriteBufferSize:       32 << 10,
		DisableProxy:          true,
	})

	assert.Nil(t, transport.Proxy)
	assert.Same(t, tlsConfig, transport.TLSClientConfig)
	_, err := transport.DialContext(t.Context(), "tcp", "example.com:80")
	require.ErrorIs(t, err, errDial)
	assert.Equal(t, 30*time.Second, transport.IdleConnTimeout)
	assert.Equal(t, 5*time.Second, transport.ResponseHeaderTimeout)
	assert.Equal(t, 8, transport.MaxIdleConnsPerHost)
	assert.Equal(t, 64<<10, transport.ReadBufferSize)
	assert.Equal(t, 32<<10, transport.WriteBufferSize)
	assert.Equal(t, 100, transport.MaxIdleConns)
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("plain transport", func(t *testing.T) {
		client := NewHTTPClient(ClientConfig{
			Timeout:             15 * time.Second,
			MaxIdleConnsPerHost: 32,
		})

		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok)
		assert.Equal(t, 32, transport.MaxIdleConnsPerHost)
		assert.Equal(t, 15*time.Second, client.Timeout)
	})

	t.Run("instrumented transport", func(t *testing.T) {
		client := NewHTTPClient(ClientConfig{Instrument: true})

		_, ok := client.Transport.(*otelhttp.Transport)
		assert.True(t, ok)
		assert.Zero(t, client.Timeout)
	})

	t.Run("negative timeout disables the request timeout", func(t *testing.T) {
		assert.Zero(t, NewHTTPClient(ClientConfig{Timeout: -time.Second}).Timeout)
	})
}
