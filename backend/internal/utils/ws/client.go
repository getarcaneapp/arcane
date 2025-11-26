package ws

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10

	maxMessageSize   = 64 * 1024
	clientSendBuffer = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	conn *websocket.Conn
	send chan []byte
	once sync.Once
}

func NewClient(conn *websocket.Conn, sendBuffer int) *Client {
	// Enable per-message deflate if negotiated (safe to ignore error)
	_ = conn.SetCompressionLevel(1)
	return &Client{
		conn: conn,
		send: make(chan []byte, sendBuffer),
	}
}

// ServeClient registers the client with the hub and starts read/write pumps.
// Caller is responsible for creating/closing the websocket.Conn.
func ServeClient(ctx context.Context, hub *Hub, conn *websocket.Conn) {
	c := NewClient(conn, clientSendBuffer)
	hub.register <- c

	go c.writePump(ctx, hub)
	go c.readPump(ctx, hub)
}

func (c *Client) safeRemove(hub *Hub) {
	c.once.Do(func() {
		hub.remove(c)
	})
}

func (c *Client) readPump(ctx context.Context, hub *Hub) {
	// Ensure client is removed from hub without sending on a potentially
	// unserviced channel. Use hub.remove which is safe when the hub has exited.
	defer c.safeRemove(hub)

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// We ignore application messages; reads are only for control frames (close/pong).
			if _, _, err := c.conn.ReadMessage(); err != nil {
				if !isExpectedCloseError(err) {
					slog.Debug("websocket readPump end", "err", err)
				}
				return
			}
		}
	}
}

func (c *Client) writePump(ctx context.Context, hub *Hub) {
	ticker := time.NewTicker(pingPeriod)
	// Stop ticker and ensure client is removed from hub without writing to hub.unregister.
	defer func() {
		ticker.Stop()
		c.safeRemove(hub)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				if !isExpectedCloseError(err) {
					slog.Debug("websocket write error", "err", err)
				}
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func isExpectedCloseError(err error) bool {
	if err == nil {
		return true
	}

	if websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived) {
		return true
	}

	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe")
}
