package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	httpxtypes "github.com/getarcaneapp/arcane/types/v2/httpx"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// dialClient is shared by every upstream dial instead of building a transport per connection.
var dialClient = httpx.NewHTTPClient(httpxtypes.ClientOptions{TLSHandshakeTimeout: 10 * time.Second})

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
		slog.ErrorContext(r.Context(), "failed to upgrade client connection", "remoteWs", remoteWS, "err", err)
		return err
	}
	defer func() { _ = clientConn.CloseNow() }()
	// This is a pure bridge; frame-size policing is the remote endpoint's job.
	clientConn.SetReadLimit(-1)

	slog.DebugContext(r.Context(), "attempting websocket dial", "remoteWs", remoteWS, "headers", header)
	dialCtx, dialCancel := context.WithTimeout(r.Context(), 45*time.Second)
	remoteConn, resp, err := websocket.Dial(dialCtx, remoteWS, &websocket.DialOptions{
		HTTPHeader: header,
		HTTPClient: dialClient,
	})
	dialCancel()
	// Ensure the response body is drained & closed to avoid leaking resources.
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if err != nil {
		slog.ErrorContext(dialCtx, "failed to dial remote websocket", "remoteWs", remoteWS, "err", err, "respStatus", func() int {
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

	slog.DebugContext(dialCtx, "websocket proxy established", "remoteWs", remoteWS)

	// When either pump ends, the handler returns and the deferred CloseNow
	// calls unblock the other pump.
	pumpCtx, pumpCancel := context.WithCancel(r.Context())
	defer pumpCancel()

	errc := make(chan struct{}, 2)

	// client -> remote
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, readErr := clientConn.Read(pumpCtx)
			if readErr != nil {
				closeRelay(r.Context(), remoteConn, readErr)
				return
			}
			if writeErr := remoteConn.Write(pumpCtx, mt, msg); writeErr != nil {
				return
			}
		}
	}()

	// remote -> client
	go func() {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, readErr := remoteConn.Read(pumpCtx)
			if readErr != nil {
				closeRelay(r.Context(), clientConn, readErr)
				return
			}
			if writeErr := clientConn.Write(pumpCtx, mt, msg); writeErr != nil {
				return
			}
		}
	}()

	<-errc
	return nil
}

// closeRelay forwards a peer's close status to the other side of the
// bridge so the terminating reason survives the proxy hop.
func closeRelay(ctx context.Context, conn *websocket.Conn, err error) {
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		return
	}
	code := ce.Code
	if code == websocket.StatusNoStatusRcvd {
		code = websocket.StatusNormalClosure
	}
	if closeErr := conn.Close(code, ce.Reason); closeErr != nil {
		slog.DebugContext(ctx, "failed to relay websocket close", "status", int(code), "err", closeErr)
	}
}
