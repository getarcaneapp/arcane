package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	writeWait  = 10 * time.Second
	pingPeriod = 54 * time.Second

	maxMessageSize   = 64 * 1024
	clientSendBuffer = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	once     sync.Once
	onRemove func()
}

func NewClient(conn *websocket.Conn, sendBuffer int) *Client {
	conn.SetReadLimit(maxMessageSize)
	return &Client{
		conn: conn,
		send: make(chan []byte, sendBuffer),
	}
}

// ServeClientWithOnRemove registers the client with the hub and starts read/write pumps.
// Caller is responsible for creating/closing the websocket.Conn.
// If onRemove is non-nil, it is invoked when the client is removed from the hub.
//
// It reports whether registration succeeded. Registration fails when the hub's
// Run has already exited (or ctx is done) — landing on that boundary used to
// block forever on the unbuffered register channel, leaking the caller's
// goroutine and the socket. On failure nothing is registered and onRemove is
// never invoked, so the caller can retry against a fresh hub with the same
// connection, or close it and release its own bookkeeping.
func ServeClientWithOnRemove(ctx context.Context, hub *Hub, conn *websocket.Conn, onRemove func()) bool {
	c := NewClient(conn, clientSendBuffer)
	c.onRemove = onRemove

	select {
	case hub.register <- c:
	case <-hub.stopped:
		return false
	case <-ctx.Done():
		return false
	}

	go c.writePump(ctx, hub)
	go c.readPump(ctx, hub)
	return true
}

func (c *Client) safeRemove(hub *Hub) {
	c.once.Do(func() {
		if c.onRemove != nil {
			c.onRemove()
		}
		hub.remove(c)
	})
}

func (c *Client) readPump(ctx context.Context, hub *Hub) {
	defer trackWorkerGoroutine()()
	// Ensure client is removed from hub without sending on a potentially
	// unserviced channel. Use hub.remove which is safe when the hub has exited.
	defer c.safeRemove(hub)

	for {
		// We ignore application messages; this read loop exists to notice the
		// peer going away and to service control frames (close/pong).
		if _, _, err := c.conn.Read(ctx); err != nil {
			if !isExpectedCloseError(err) {
				slog.Debug("websocket readPump end", "err", err)
			}
			return
		}
	}
}

func (c *Client) writePump(ctx context.Context, hub *Hub) {
	defer trackWorkerGoroutine()()

	// Timer instead of Ticker: no drain-after-Stop, and we can reset after each ping
	// so shutdown sees ctx.Done() without waiting for an extra tick.
	pingTimer := time.NewTimer(pingPeriod)
	defer func() {
		pingTimer.Stop()
		c.safeRemove(hub)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				if !isExpectedCloseError(err) {
					slog.Debug("websocket write error", "err", err)
				}
				return
			}
		case <-pingTimer.C:
			select {
			case <-ctx.Done():
				return
			default:
			}
			// Ping round-trips: it fails within writeWait unless the peer pongs,
			// which readPump services. A failed ping tears the client down.
			pctx, cancel := context.WithTimeout(ctx, writeWait)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
			pingTimer.Reset(pingPeriod)
		}
	}
}

func isExpectedCloseError(err error) bool {
	if err == nil {
		return true
	}

	if IsExpectedClose(err) {
		return true
	}

	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}

	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe")
}
