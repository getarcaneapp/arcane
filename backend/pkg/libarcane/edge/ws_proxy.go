package edge

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

const (
	// tunnelWSPingWait bounds the ping round-trip on the browser-facing side of
	// a proxied WebSocket. Reverse proxies (Caddy, Nginx, ...) silently drop
	// idle TCP connections; without a heartbeat the master would hold a dead
	// stream open until something else terminates it.
	tunnelWSPingWait      = 10 * time.Second
	tunnelWSPingPeriod    = 54 * time.Second
	tunnelWSDataWriteWait = 10 * time.Second
)

// ProxyWebSocketRequest proxies a WebSocket upgrade through an edge tunnel.
// This handles logs, stats, and other streaming endpoints.
//
// checkOrigin must be the same Origin validator the local WebSocket endpoints
// use. It is required: the caller's session cookie has already been validated by
// the time this runs, so accepting any Origin would let an attacker-controlled
// page open a terminal or log stream in the tunnelled environment.
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
	// Client frames are forwarded into tunnel messages capped at
	// maxGRPCTunnelMessageSize; allow the same size here instead of
	// coder/websocket's 32KB default.
	clientWS.SetReadLimit(maxGRPCTunnelMessageSize)

	streamID := uuid.New().String()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Register the stream before sending the start message so agent replies
	// are never dropped when the routing goroutine is faster than this goroutine.
	pending := &PendingRequest{
		ResponseCh: make(chan *TunnelMessage, 512),
		failureCh:  make(chan error, 1),
	}
	clientDoneCh := make(chan struct{})

	tunnel.Pending.Store(streamID, pending)
	defer tunnel.Pending.Delete(streamID)

	headers := buildWebSocketHeaders(req)
	if err := DefaultCommandClient.OpenStream(streamCtx, tunnel, &CommandRequest{
		ID:      streamID,
		Method:  http.MethodGet,
		Path:    targetPath,
		Query:   req.URL.RawQuery,
		Headers: headers,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to send WebSocket start to agent", "error", err)
		return nil
	}

	slog.DebugContext(ctx, "Started WebSocket stream through edge tunnel",
		"stream_id", streamID,
		"environment_id", tunnel.EnvironmentID,
		"path", targetPath,
	)

	// Goroutine to read from client and send to agent
	go forwardClientToAgent(ctx, streamCtx, clientWS, tunnel, streamID, clientDoneCh)

	forwardAgentToClient(ctx, streamCtx, clientWS, tunnel, pending, streamID, clientDoneCh)
	return nil
}

func buildWebSocketHeaders(req *http.Request) map[string]string {
	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

func forwardClientToAgent(ctx context.Context, streamCtx context.Context, clientWS *websocket.Conn, tunnel *AgentTunnel, streamID string, doneCh chan<- struct{}) {
	defer close(doneCh)
	for {
		msgType, data, err := clientWS.Read(streamCtx)
		if err != nil {
			if !wshub.IsExpectedClose(err) {
				slog.DebugContext(ctx, "Client WebSocket read error", "error", err)
			}
			sendWebSocketClose(tunnel, streamID)
			return
		}

		if !isForwardableWSMessage(int(msgType)) {
			continue
		}

		if err := sendStreamDataInternal(tunnel, streamID, int(msgType), data); err != nil {
			slog.DebugContext(ctx, "Failed to send WebSocket data to agent", "error", err)
			return
		}
	}
}

func forwardAgentToClient(ctx context.Context, streamCtx context.Context, clientWS *websocket.Conn, tunnel *AgentTunnel, pending *PendingRequest, streamID string, clientDoneCh <-chan struct{}) {
	pingTicker := time.NewTicker(tunnelWSPingPeriod)
	defer pingTicker.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case <-tunnel.done:
			return
		case <-pending.failureCh:
			return
		case <-clientDoneCh:
			return
		case <-pingTicker.C:
			// Ping round-trips; the pong is serviced by forwardClientToAgent's read.
			pctx, pcancel := context.WithTimeout(streamCtx, tunnelWSPingWait)
			err := clientWS.Ping(pctx)
			pcancel()
			if err != nil {
				slog.DebugContext(ctx, "Failed to ping client WebSocket", "stream_id", streamID, "error", err)
				sendWebSocketClose(tunnel, streamID)
				return
			}
		case msg, ok := <-pending.ResponseCh:
			if !ok {
				return
			}
			if shouldStop, err := handleAgentMessage(ctx, streamCtx, clientWS, msg, streamID); err != nil {
				slog.DebugContext(ctx, "Failed to write to client WebSocket", "error", err)
				return
			} else if shouldStop {
				return
			}
		}
	}
}

func handleAgentMessage(ctx context.Context, streamCtx context.Context, clientWS *websocket.Conn, msg *TunnelMessage, streamID string) (bool, error) {
	switch msg.Type {
	case MessageTypeWebSocketData, MessageTypeStreamData:
		return false, writeWebSocketData(streamCtx, clientWS, msg)
	case MessageTypeWebSocketClose, MessageTypeStreamClose, MessageTypeStreamEnd:
		slog.DebugContext(ctx, "Agent closed WebSocket stream", "stream_id", streamID)
		return true, nil
	case MessageTypeRequest,
		MessageTypeResponse,
		MessageTypeHeartbeat,
		MessageTypeHeartbeatAck,
		MessageTypeWebSocketStart,
		MessageTypeRegister,
		MessageTypeRegisterResponse,
		MessageTypeEvent,
		MessageTypeCommandRequest,
		MessageTypeCommandAck,
		MessageTypeCommandOutput,
		MessageTypeCommandComplete,
		MessageTypeFileChunk,
		MessageTypeStreamOpen,
		MessageTypeCancelRequest:
		slog.DebugContext(ctx, "Ignoring tunnel message", "type", msg.Type, "stream_id", streamID)
		return false, nil
	default:
		slog.DebugContext(ctx, "Unknown tunnel message", "type", msg.Type, "stream_id", streamID)
		return false, nil
	}
}

func writeWebSocketData(streamCtx context.Context, clientWS *websocket.Conn, msg *TunnelMessage) error {
	msgType := websocket.MessageType(msg.WSMessageType)
	if msgType != websocket.MessageText && msgType != websocket.MessageBinary {
		slog.Warn("Dropping WebSocket message with unsupported type", "messageType", msg.WSMessageType)
		return nil
	}
	wctx, cancel := context.WithTimeout(streamCtx, tunnelWSDataWriteWait)
	defer cancel()
	return clientWS.Write(wctx, msgType, msg.Body)
}

func sendStreamDataInternal(tunnel *AgentTunnel, streamID string, msgType int, data []byte) error {
	wsDataMsg := &TunnelMessage{
		ID:            streamID,
		Type:          MessageTypeStreamData,
		Body:          data,
		WSMessageType: msgType,
	}
	return tunnel.Conn.Send(wsDataMsg)
}

func sendWebSocketClose(tunnel *AgentTunnel, streamID string) {
	closeMsg := &TunnelMessage{
		ID:   streamID,
		Type: MessageTypeStreamClose,
	}
	_ = tunnel.Conn.Send(closeMsg)
}

func isForwardableWSMessage(msgType int) bool {
	return msgType == int(websocket.MessageText) || msgType == int(websocket.MessageBinary)
}
