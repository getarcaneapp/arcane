package ws

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"emperror.dev/errors"
	"github.com/coder/websocket"
)

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

	clientConn, err := Accept(w, r, checkOrigin)
	if err != nil {
		slog.Error("failed to upgrade client connection", "remoteWS", remoteWS, "err", err)
		return err
	}
	defer func() { _ = clientConn.CloseNow() }()
	// This is a pure bridge; frame-size policing is the remote endpoint's job.
	clientConn.SetReadLimit(-1)

	slog.Debug("attempting websocket dial", "remoteWS", remoteWS, "headers", header)
	dialCtx, dialCancel := context.WithTimeout(r.Context(), 45*time.Second)
	remoteConn, resp, err := websocket.Dial(dialCtx, remoteWS, &websocket.DialOptions{
		HTTPHeader: header,
		HTTPClient: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
	})
	dialCancel()
	// Ensure the response body is drained & closed to avoid leaking resources.
	if resp != nil && resp.Body != nil {
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
		_ = clientConn.Close(websocket.StatusBadGateway, "")
		return err
	}
	defer func() { _ = remoteConn.CloseNow() }()
	remoteConn.SetReadLimit(-1)

	slog.Debug("websocket proxy established", "remoteWS", remoteWS)

	// When either pump ends, the handler returns and the deferred CloseNow
	// calls unblock the other pump.
	pumpCtx, pumpCancel := context.WithCancel(r.Context())
	defer pumpCancel()

	errc := make(chan struct{}, 2)

	// client -> remote
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, err := clientConn.Read(pumpCtx)
			if err != nil {
				return
			}
			if err := remoteConn.Write(pumpCtx, mt, msg); err != nil {
				return
			}
		}
	}()

	// remote -> client
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, err := remoteConn.Read(pumpCtx)
			if err != nil {
				return
			}
			if err := clientConn.Write(pumpCtx, mt, msg); err != nil {
				return
			}
		}
	}()

	<-errc
	return nil
}
