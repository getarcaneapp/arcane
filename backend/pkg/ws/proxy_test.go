package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowAnyOriginForTest stands in for the caller-supplied Origin validator;
// these tests exercise the proxy bridge, not the Origin policy.
func allowAnyOriginForTest(*http.Request) bool { return true }

func TestProxyHTTP_RequiresOriginValidator(t *testing.T) {
	err := ProxyHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", http.NoBody), "ws://127.0.0.1:1", nil, nil, http.DefaultClient)
	require.Error(t, err, "proxy must refuse to upgrade without an origin validator")
}

func TestProxyHTTP_BidirectionalMessages(t *testing.T) {
	// 1. Create a "remote" WebSocket server that echoes messages
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			mt, msg, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
			// Echo with prefix
			if writeErr := conn.Write(r.Context(), mt, append([]byte("echo:"), msg...)); writeErr != nil {
				return
			}
		}
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	// 2. Create a "proxy" server that uses ProxyHTTP
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, nil, allowAnyOriginForTest, http.DefaultClient)
	}))
	defer proxyServer.Close()

	// 3. Connect a client to the proxy
	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	// 4. Send messages and verify they get proxied and echoed
	testMessages := []string{"hello", "world", "test123"}
	for _, msg := range testMessages {
		writeErr := clientConn.Write(t.Context(), websocket.MessageText, []byte(msg))
		require.NoError(t, writeErr)

		readCtx, readCancel := context.WithTimeout(t.Context(), 2*time.Second)
		_, received, writeErr := clientConn.Read(readCtx)
		readCancel()
		require.NoError(t, writeErr)
		assert.Equal(t, "echo:"+msg, string(received))
	}
}

func TestProxyHTTP_RemoteClose(t *testing.T) {
	// Remote server that closes immediately after upgrade
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately
		if closeErr := conn.Close(websocket.StatusNormalClosure, "bye"); closeErr != nil {
			t.Logf("close websocket connection: %v", closeErr)
		}
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	proxyDone := make(chan error, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyDone <- ProxyHTTP(w, r, remoteWS, nil, allowAnyOriginForTest, http.DefaultClient)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()
	clientConn.CloseRead(t.Context())

	// The proxy should complete (not hang)
	select {
	case proxyErr := <-proxyDone:
		require.NoError(t, proxyErr)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "ProxyHTTP did not return after remote closed")
	}
}

func TestProxyHTTP_InvalidRemoteURL(t *testing.T) {
	proxyDone := make(chan error, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyDone <- ProxyHTTP(w, r, "ws://127.0.0.1:1", nil, allowAnyOriginForTest, http.DefaultClient)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	select {
	case operationErr := <-proxyDone:
		require.Error(t, operationErr, "ProxyHTTP should return error when remote is unreachable")
	case <-time.After(50 * time.Second):
		require.FailNow(t, "ProxyHTTP did not return after failed dial")
	}
}

func TestProxyHTTP_BinaryMessages(t *testing.T) {
	// Remote server that echoes binary messages
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			mt, msg, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
			if writeErr := conn.Write(r.Context(), mt, msg); writeErr != nil {
				return
			}
		}
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, nil, allowAnyOriginForTest, http.DefaultClient)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	err = clientConn.Write(t.Context(), websocket.MessageBinary, binaryData)
	require.NoError(t, err)

	readCtx, readCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer readCancel()
	mt, received, err := clientConn.Read(readCtx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageBinary, mt)
	assert.Equal(t, binaryData, received)
}

func TestProxyHTTP_HeadersForwarded(t *testing.T) {
	headersCh := make(chan http.Header, 1)
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headersCh <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.CloseNow()
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	customHeaders := http.Header{
		"X-Custom-Header": {"test-value"},
		"Authorization":   {"Bearer token123"},
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, customHeaders, allowAnyOriginForTest, http.DefaultClient)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), proxyURL, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	select {
	case receivedHeaders := <-headersCh:
		assert.Equal(t, "test-value", receivedHeaders.Get("X-Custom-Header"))
		assert.Equal(t, "Bearer token123", receivedHeaders.Get("Authorization"))
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for headers")
	}
}
