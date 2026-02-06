package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyHTTP_BidirectionalMessages(t *testing.T) {
	// 1. Create a "remote" WebSocket server that echoes messages
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo with prefix
			if err := conn.WriteMessage(mt, append([]byte("echo:"), msg...)); err != nil {
				return
			}
		}
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	// 2. Create a "proxy" server that uses ProxyHTTP
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, nil)
	}))
	defer proxyServer.Close()

	// 3. Connect a client to the proxy
	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	// 4. Send messages and verify they get proxied and echoed
	testMessages := []string{"hello", "world", "test123"}
	for _, msg := range testMessages {
		err := clientConn.WriteMessage(websocket.TextMessage, []byte(msg))
		require.NoError(t, err)

		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, received, err := clientConn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, "echo:"+msg, string(received))
	}
}

func TestProxyHTTP_RemoteClose(t *testing.T) {
	// Remote server that closes immediately after upgrade
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close immediately
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
		conn.Close()
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	proxyDone := make(chan error, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyDone <- ProxyHTTP(w, r, remoteWS, nil)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	// The proxy should complete (not hang)
	select {
	case <-proxyDone:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("ProxyHTTP did not return after remote closed")
	}
}

func TestProxyHTTP_InvalidRemoteURL(t *testing.T) {
	proxyDone := make(chan error, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyDone <- ProxyHTTP(w, r, "ws://127.0.0.1:1", nil)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	select {
	case err := <-proxyDone:
		require.Error(t, err, "ProxyHTTP should return error when remote is unreachable")
	case <-time.After(50 * time.Second):
		t.Fatal("ProxyHTTP did not return after failed dial")
	}
}

func TestProxyHTTP_BinaryMessages(t *testing.T) {
	// Remote server that echoes binary messages
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, nil)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	err = clientConn.WriteMessage(websocket.BinaryMessage, binaryData)
	require.NoError(t, err)

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, received, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, mt)
	assert.Equal(t, binaryData, received)
}

func TestProxyHTTP_HeadersForwarded(t *testing.T) {
	headersCh := make(chan http.Header, 1)
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headersCh <- r.Header.Clone()
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer remoteServer.Close()

	remoteWS := "ws" + strings.TrimPrefix(remoteServer.URL, "http")

	customHeaders := http.Header{
		"X-Custom-Header": {"test-value"},
		"Authorization":   {"Bearer token123"},
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = ProxyHTTP(w, r, remoteWS, customHeaders)
	}))
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	select {
	case receivedHeaders := <-headersCh:
		assert.Equal(t, "test-value", receivedHeaders.Get("X-Custom-Header"))
		assert.Equal(t, "Bearer token123", receivedHeaders.Get("Authorization"))
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for headers")
	}
}
