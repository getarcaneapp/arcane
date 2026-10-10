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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

// dialClient is shared by every upstream dial; its span covers the whole proxied session.
var dialClient = func() *http.Client {
	client := httpx.NewHTTPClient(httpxtypes.ClientOptions{TLSHandshakeTimeout: 10 * time.Second})
	client.Transport = otelhttp.NewTransport(client.Transport)
	return client
}()

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
		respStatus := 0
		if resp != nil {
			respStatus = resp.StatusCode
		}
		slog.ErrorContext(dialCtx, "failed to dial remote websocket", "remoteWs", remoteWS, "err", err, "respStatus", respStatus)
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
	pump := func(src, dst *websocket.Conn) {
		defer func() { errc <- struct{}{} }()
		for {
			mt, msg, readErr := src.Read(pumpCtx)
			if readErr != nil {
				// Forward the peer's close status so the terminating reason survives the proxy hop.
				if ce, ok := errors.AsType[websocket.CloseError](readErr); ok {
					code := ce.Code
					if code == websocket.StatusNoStatusRcvd {
						code = websocket.StatusNormalClosure
					}
					if closeErr := dst.Close(code, ce.Reason); closeErr != nil {
						slog.DebugContext(r.Context(), "failed to relay websocket close", "status", int(code), "err", closeErr)
					}
				}
				return
			}
			if writeErr := dst.Write(pumpCtx, mt, msg); writeErr != nil {
				return
			}
		}
	}
	go pump(clientConn, remoteConn)
	go pump(remoteConn, clientConn)

	<-errc
	return nil
}
