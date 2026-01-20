package edge

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var wsUpgraderForProxy = websocket.Upgrader{
	ReadBufferSize:    64 * 1024,
	WriteBufferSize:   64 * 1024,
	EnableCompression: true,
	CheckOrigin:       func(r *http.Request) bool { return true },
}

// ProxyWebSocketRequest proxies a WebSocket upgrade through an edge tunnel
// This handles logs, stats, and other streaming endpoints
func ProxyWebSocketRequest(c *gin.Context, tunnel *AgentTunnel, targetPath string) {
	ctx := c.Request.Context()

	// Upgrade the client connection to WebSocket
	clientWS, err := wsUpgraderForProxy.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to upgrade WebSocket for edge proxy", "error", err)
		return
	}
	defer clientWS.Close()

	streamID := uuid.New().String()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	headers := buildWebSocketHeaders(c.Request)
	if err := sendWebSocketStart(tunnel, streamID, targetPath, c.Request.URL.RawQuery, headers); err != nil {
		slog.ErrorContext(ctx, "Failed to send WebSocket start to agent", "error", err)
		return
	}

	slog.DebugContext(ctx, "Started WebSocket stream through edge tunnel",
		"stream_id", streamID,
		"environment_id", tunnel.EnvironmentID,
		"path", targetPath,
	)

	// Create channels for bidirectional data
	agentDataCh := make(chan *TunnelMessage, 100)
	clientDoneCh := make(chan struct{})

	// Register the stream to receive data from the agent
	tunnel.Pending.Store(streamID, &PendingRequest{
		ResponseCh: agentDataCh,
	})
	defer tunnel.Pending.Delete(streamID)

	// Goroutine to read from client and send to agent
	go forwardClientToAgent(ctx, streamCtx, clientWS, tunnel, streamID, clientDoneCh)

	forwardAgentToClient(ctx, streamCtx, clientWS, agentDataCh, streamID, clientDoneCh)
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

func sendWebSocketStart(tunnel *AgentTunnel, streamID, targetPath, query string, headers map[string]string) error {
	startMsg := &TunnelMessage{
		ID:      streamID,
		Type:    MessageTypeWebSocketStart,
		Path:    targetPath,
		Query:   query,
		Headers: headers,
	}
	return tunnel.Conn.Send(startMsg)
}

func forwardClientToAgent(ctx context.Context, streamCtx context.Context, clientWS *websocket.Conn, tunnel *AgentTunnel, streamID string, doneCh chan<- struct{}) {
	defer close(doneCh)
	for {
		if streamCtx.Err() != nil {
			return
		}

		msgType, data, err := clientWS.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived) {
				slog.DebugContext(ctx, "Client WebSocket read error", "error", err)
			}
			sendWebSocketClose(tunnel, streamID)
			return
		}

		if !isForwardableWSMessage(msgType) {
			continue
		}

		if err := sendWebSocketData(tunnel, streamID, msgType, data); err != nil {
			slog.DebugContext(ctx, "Failed to send WebSocket data to agent", "error", err)
			return
		}
	}
}

func forwardAgentToClient(ctx context.Context, streamCtx context.Context, clientWS *websocket.Conn, agentDataCh <-chan *TunnelMessage, streamID string, clientDoneCh <-chan struct{}) {
	for {
		select {
		case <-streamCtx.Done():
			return
		case <-clientDoneCh:
			return
		case msg, ok := <-agentDataCh:
			if !ok {
				return
			}
			if shouldStop, err := handleAgentMessage(ctx, clientWS, msg, streamID); err != nil {
				slog.DebugContext(ctx, "Failed to write to client WebSocket", "error", err)
				return
			} else if shouldStop {
				return
			}
		}
	}
}

func handleAgentMessage(ctx context.Context, clientWS *websocket.Conn, msg *TunnelMessage, streamID string) (bool, error) {
	switch msg.Type {
	case MessageTypeWebSocketData:
		return false, writeWebSocketData(clientWS, msg)
	case MessageTypeWebSocketClose, MessageTypeStreamEnd:
		slog.DebugContext(ctx, "Agent closed WebSocket stream", "stream_id", streamID)
		return true, nil
	case MessageTypeRequest, MessageTypeResponse, MessageTypeHeartbeat, MessageTypeHeartbeatAck, MessageTypeStreamData, MessageTypeWebSocketStart:
		slog.DebugContext(ctx, "Ignoring tunnel message", "type", msg.Type, "stream_id", streamID)
		return false, nil
	default:
		slog.DebugContext(ctx, "Unknown tunnel message", "type", msg.Type, "stream_id", streamID)
		return false, nil
	}
}

func writeWebSocketData(clientWS *websocket.Conn, msg *TunnelMessage) error {
	msgType := msg.WSMessageType
	if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
		slog.Warn("Dropping WebSocket message with unsupported type", "messageType", msgType)
		return nil
	}
	return clientWS.WriteMessage(msgType, msg.Body)
}

func sendWebSocketData(tunnel *AgentTunnel, streamID string, msgType int, data []byte) error {
	wsDataMsg := &TunnelMessage{
		ID:            streamID,
		Type:          MessageTypeWebSocketData,
		Body:          data,
		WSMessageType: msgType,
	}
	return tunnel.Conn.Send(wsDataMsg)
}

func sendWebSocketClose(tunnel *AgentTunnel, streamID string) {
	closeMsg := &TunnelMessage{
		ID:   streamID,
		Type: MessageTypeWebSocketClose,
	}
	_ = tunnel.Conn.Send(closeMsg)
}

func isForwardableWSMessage(msgType int) bool {
	return msgType == websocket.TextMessage || msgType == websocket.BinaryMessage
}
