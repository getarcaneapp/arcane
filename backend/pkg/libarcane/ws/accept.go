package ws

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Accept checks the Origin with checkOrigin (the CSRF barrier for cookie-authenticated upgrades) and upgrades
// to a WebSocket. A nil checkOrigin is only for endpoints authenticated by tokens or mTLS.
func Accept(w http.ResponseWriter, r *http.Request, checkOrigin func(*http.Request) bool) (*websocket.Conn, error) {
	if checkOrigin != nil && !checkOrigin(r) {
		http.Error(w, "websocket origin not allowed", http.StatusForbidden)
		return nil, errors.New("websocket origin not allowed")
	}
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin validated above
	})
}

// IsExpectedClose reports whether err is a WebSocket close error carrying one
// of the benign statuses peers send on ordinary disconnect.
func IsExpectedClose(err error) bool {
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure ||
		status == websocket.StatusGoingAway ||
		status == websocket.StatusNoStatusRcvd
}

// KeepAlive pings conn every pingPeriod and calls cancel when a ping fails, so idle connections survive
// proxies that drop silent TCP connections. The pong must be serviced by the connection's concurrent reader.
func KeepAlive(ctx context.Context, cancel func(), conn *websocket.Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pctx, pcancel := context.WithTimeout(ctx, writeWait)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}
