package ws

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"emperror.dev/errors"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:    32 * 1024,
	WriteBufferSize:   32 * 1024,
	EnableCompression: true,
}

// ProxyHTTP upgrades the incoming client connection and bridges it to remoteWS.
//
// checkOrigin must be the same Origin validator the local WebSocket endpoints
// use. It is required: this upgrade is reached with the caller's session cookie
// already validated, so accepting any Origin would let an attacker-controlled
// page open a terminal or log stream in a remote environment.
func ProxyHTTP(w http.ResponseWriter, r *http.Request, remoteWS string, header http.Header, checkOrigin func(*http.Request) bool) error {
	if checkOrigin == nil {
		return errors.New("websocket proxy requires an origin validator")
	}

	proxyUpgrader := upgrader
	proxyUpgrader.CheckOrigin = checkOrigin

	clientConn, err := proxyUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade client connection", "remoteWS", remoteWS, "err", err)
		return err
	}
	defer func() { _ = clientConn.Close() }()

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	slog.Debug("attempting websocket dial", "remoteWS", remoteWS, "headers", header)
	remoteConn, resp, err := dialer.Dial(remoteWS, header)
	// Ensure the response body is drained & closed to avoid leaking resources.
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if err != nil {
		slog.Error("failed to dial remote websocket", "remoteWS", remoteWS, "err", err, "resp_status", func() int {
			if resp != nil {
				return resp.StatusCode
			}
			return 0
		}())
		_ = clientConn.WriteMessage(websocket.CloseMessage, []byte{})
		return err
	}
	defer func() { _ = remoteConn.Close() }()

	slog.Debug("websocket proxy established", "remoteWS", remoteWS)

	errc := make(chan struct{}, 2)

	// client -> remote
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := remoteConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	// remote -> client
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, err := remoteConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()

	<-errc
	return nil
}
