package edge

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// TunnelMessageType represents the type of message sent over the tunnel
type TunnelMessageType string

const (
	// MessageTypeRequest is sent from manager to agent to initiate a request
	MessageTypeRequest TunnelMessageType = "request"
	// MessageTypeResponse is sent from agent to manager with the response
	MessageTypeResponse TunnelMessageType = "response"
	// MessageTypeHeartbeat is sent by agents to keep the connection alive
	MessageTypeHeartbeat TunnelMessageType = "heartbeat"
	// MessageTypeHeartbeatAck is sent by manager to acknowledge a heartbeat
	MessageTypeHeartbeatAck TunnelMessageType = "heartbeat_ack"
	// MessageTypeStreamData is sent for streaming responses (logs, stats)
	MessageTypeStreamData TunnelMessageType = "stream_data"
	// MessageTypeStreamEnd indicates end of a stream
	MessageTypeStreamEnd TunnelMessageType = "stream_end"
	// MessageTypeWebSocketStart starts a WebSocket stream for logs/stats
	MessageTypeWebSocketStart TunnelMessageType = "ws_start"
	// MessageTypeWebSocketData is a WebSocket message in either direction
	MessageTypeWebSocketData TunnelMessageType = "ws_data"
	// MessageTypeWebSocketClose closes a WebSocket stream
	MessageTypeWebSocketClose TunnelMessageType = "ws_close"
)

// TunnelMessage represents a message sent over the edge tunnel
type TunnelMessage struct {
	ID            string            `json:"id"`                        // Unique request/stream ID
	Type          TunnelMessageType `json:"type"`                      // Message type
	Method        string            `json:"method,omitempty"`          // HTTP method for requests
	Path          string            `json:"path,omitempty"`            // Request path
	Query         string            `json:"query,omitempty"`           // Query string
	Headers       map[string]string `json:"headers,omitempty"`         // HTTP headers
	Body          []byte            `json:"body,omitempty"`            // Request/response body
	WSMessageType int               `json:"ws_message_type,omitempty"` // WebSocket message type
	Status        int               `json:"status,omitempty"`          // HTTP status for responses
}

// MarshalJSON custom marshaler to handle nil body as empty
func (m *TunnelMessage) MarshalJSON() ([]byte, error) {
	type Alias TunnelMessage
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// PendingRequest tracks an in-flight request waiting for response
type PendingRequest struct {
	ResponseCh chan *TunnelMessage
	CreatedAt  time.Time
}

// TunnelConn wraps a WebSocket connection with send/receive helpers
type TunnelConn struct {
	conn     *websocket.Conn
	mu       sync.Mutex
	closed   bool
	closedMu sync.RWMutex
}

// NewTunnelConn creates a new tunnel connection wrapper
func NewTunnelConn(conn *websocket.Conn) *TunnelConn {
	return &TunnelConn{
		conn: conn,
	}
}

// Send sends a tunnel message over the connection
func (t *TunnelConn) Send(msg *TunnelMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closedMu.RLock()
	if t.closed {
		t.closedMu.RUnlock()
		return websocket.ErrCloseSent
	}
	t.closedMu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return t.conn.WriteMessage(websocket.TextMessage, data)
}

// Receive receives a tunnel message from the connection
func (t *TunnelConn) Receive() (*TunnelMessage, error) {
	_, data, err := t.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	var msg TunnelMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// Close closes the tunnel connection
func (t *TunnelConn) Close() error {
	t.closedMu.Lock()
	t.closed = true
	t.closedMu.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.Close()
}

// IsClosed returns whether the connection is closed
func (t *TunnelConn) IsClosed() bool {
	t.closedMu.RLock()
	defer t.closedMu.RUnlock()
	return t.closed
}

// SendRequest is a helper for the manager side to send a request and wait for response
func (t *TunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	respCh := make(chan *TunnelMessage, 1)
	pending.Store(msg.ID, &PendingRequest{
		ResponseCh: respCh,
		CreatedAt:  time.Now(),
	})
	defer pending.Delete(msg.ID)

	if err := t.Send(msg); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}
