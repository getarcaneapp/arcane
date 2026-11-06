package ws

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func Accept(w http.ResponseWriter, r *http.Request, checkOrigin func(*http.Request) bool) (*websocket.Conn, error) {
	if checkOrigin != nil && !checkOrigin(r) {
		http.Error(w, "websocket origin not allowed", http.StatusForbidden)
		return nil, errors.New("websocket origin not allowed")
	}
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin validated above
	})
}

func IsExpectedClose(err error) bool {
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure ||
		status == websocket.StatusGoingAway ||
		status == websocket.StatusNoStatusRcvd
}

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
