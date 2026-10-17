package edge

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
)

const tunnelWSDataWriteWait = 10 * time.Second

// ProxyWebSocketRequest proxies a WebSocket upgrade through a legacy edge tunnel. checkOrigin must be the local
// WebSocket Origin validator: the session cookie is already validated, so any Origin would allow cross-site terminals.
//
// Deprecated: legacy edge message protocol; remove in v3.
func ProxyWebSocketRequest(c *echo.Context, tunnel *AgentTunnel, targetPath string, checkOrigin func(*http.Request) bool) error {
	req := c.Request()
	ctx := req.Context()

	if checkOrigin == nil {
		slog.ErrorContext(ctx, "Refusing edge WebSocket proxy without an origin validator", "path", targetPath)
		return echo.NewHTTPError(http.StatusForbidden, "websocket origin validation unavailable")
	}
	clientWS, err := wshub.Accept(c.Response(), req, checkOrigin)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade WebSocket for edge proxy", "error", err)
		return nil
	}
	defer func() { _ = clientWS.CloseNow() }()
	// Client frames become tunnel messages capped at maxGRPCTunnelMessageSize, not coder/websocket's 32KB default.
	clientWS.SetReadLimit(maxGRPCTunnelMessageSize)

	streamID := uuid.New().String()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Register the stream before the start message so a fast agent reply is never dropped.
	pending := &PendingRequest{
		ResponseCh: make(chan *TunnelMessage, 512),
		failureCh:  make(chan error, 1),
	}
	tunnel.Pending.Store(streamID, pending)
	defer tunnel.Pending.Delete(streamID)

	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	if openStreamErr := DefaultCommandClient.OpenStream(streamCtx, tunnel, &CommandRequest{
		ID:      streamID,
		Method:  http.MethodGet,
		Path:    targetPath,
		Query:   req.URL.RawQuery,
		Headers: headers,
	}); openStreamErr != nil {
		slog.ErrorContext(ctx, "Failed to send WebSocket start to agent", "error", openStreamErr)
		return nil
	}
	slog.DebugContext(ctx, "Started WebSocket stream through edge tunnel", "streamId", streamID, "environmentId", tunnel.EnvironmentID, "path", targetPath)

	// A failed browser ping cancels the stream; the client reader then tells the agent to close it.
	go wshub.KeepAlive(streamCtx, cancel, clientWS)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			msgType, data, readErr := clientWS.Read(streamCtx)
			if readErr != nil {
				if !wshub.IsExpectedClose(readErr) {
					slog.DebugContext(ctx, "Client WebSocket read error", "error", readErr)
				}
				_ = tunnel.Conn.Send(&TunnelMessage{ID: streamID, Type: MessageTypeStreamClose})
				return
			}
			if !isForwardableWSMessage(int(msgType)) {
				continue
			}
			if sendErr := tunnel.Conn.Send(&TunnelMessage{ID: streamID, Type: MessageTypeStreamData, Body: data, WSMessageType: int(msgType)}); sendErr != nil {
				slog.DebugContext(ctx, "Failed to send WebSocket data to agent", "error", sendErr)
				return
			}
		}
	}()

	for {
		var msg *TunnelMessage
		select {
		case <-streamCtx.Done():
			return nil
		case <-tunnel.done:
			return nil
		case failErr := <-pending.failureCh:
			slog.DebugContext(ctx, "Edge WebSocket stream failed", "streamId", streamID, "error", failErr)
			return nil
		case <-clientDone:
			return nil
		case msg = <-pending.ResponseCh:
		}

		switch msg.Type {
		case MessageTypeWebSocketData, MessageTypeStreamData:
			if !isForwardableWSMessage(msg.WSMessageType) {
				slog.WarnContext(ctx, "Dropping WebSocket message with unsupported type", "messageType", msg.WSMessageType)
				continue
			}
			wctx, cancelWrite := context.WithTimeout(streamCtx, tunnelWSDataWriteWait)
			writeErr := clientWS.Write(wctx, websocket.MessageType(msg.WSMessageType), msg.Body)
			cancelWrite()
			if writeErr != nil {
				slog.DebugContext(ctx, "Failed to write to client WebSocket", "error", writeErr)
				return nil
			}
		case MessageTypeWebSocketClose, MessageTypeStreamClose, MessageTypeStreamEnd:
			slog.DebugContext(ctx, "Agent closed WebSocket stream", "streamId", streamID, "error", msg.Error)
			status, reason := websocket.StatusNormalClosure, msg.Error
			if reason != "" {
				// Close reasons are limited to 123 bytes.
				status, reason = websocket.StatusInternalError, reason[:min(len(reason), 123)]
			}
			if closeErr := clientWS.Close(status, reason); closeErr != nil {
				slog.DebugContext(ctx, "Failed to close client WebSocket", "streamId", streamID, "status", int(status), "error", closeErr)
			}
			return nil
		default:
			slog.DebugContext(ctx, "Ignoring tunnel message", "type", msg.Type, "streamId", streamID)
		}
	}
}

// isForwardableWSMessage reports whether msgType is a text or binary data frame.
func isForwardableWSMessage(msgType int) bool {
	return msgType == int(websocket.MessageText) || msgType == int(websocket.MessageBinary)
}
