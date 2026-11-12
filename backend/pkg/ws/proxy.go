package ws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	proxyClientReadLimit = 1 << 20
	proxyRemoteReadLimit = 16 << 20
)

func ProxyHTTP(w http.ResponseWriter, r *http.Request, remoteWS string, header http.Header, checkOrigin func(*http.Request) bool, client *http.Client) error {
	if checkOrigin == nil {
		return errors.New("websocket proxy requires an origin validator")
	}
	if client == nil {
		return errors.New("websocket proxy requires a dial client")
	}

	clientConn, err := Accept(w, r, checkOrigin)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to upgrade client connection", "remoteWs", remoteWS, "err", err)
		return err
	}
	defer func() { _ = clientConn.CloseNow() }()
	clientConn.SetReadLimit(proxyClientReadLimit)

	slog.DebugContext(r.Context(), "attempting websocket dial", "remoteWs", remoteWS)
	dialCtx, dialCancel := context.WithTimeout(r.Context(), 45*time.Second)
	remoteConn, resp, err := websocket.Dial(dialCtx, remoteWS, &websocket.DialOptions{
		HTTPHeader: header,
		HTTPClient: client,
	})
	dialCancel()
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
	remoteConn.SetReadLimit(proxyRemoteReadLimit)

	slog.DebugContext(dialCtx, "websocket proxy established", "remoteWs", remoteWS)

	// When either pump ends, the handler returns and the deferred CloseNow
	// calls unblock the other pump.
	pumpCtx, pumpCancel := context.WithCancel(r.Context())
	defer pumpCancel()

	errc := make(chan struct{}, 2)
	pump := func(src, dst *websocket.Conn) {
		defer func() { errc <- struct{}{} }()
		var msg bytes.Buffer
		for {
			mt, reader, readErr := src.Reader(pumpCtx)
			if readErr == nil {
				msg.Reset()
				_, readErr = msg.ReadFrom(reader)
			}
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
			if writeErr := dst.Write(pumpCtx, mt, msg.Bytes()); writeErr != nil {
				return
			}
		}
	}
	go pump(clientConn, remoteConn)
	go pump(remoteConn, clientConn)
	go KeepAlive(pumpCtx, pumpCancel, clientConn)
	go KeepAlive(pumpCtx, pumpCancel, remoteConn)

	<-errc
	return nil
}
