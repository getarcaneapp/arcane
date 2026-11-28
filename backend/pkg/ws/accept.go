package ws

import (
	"context"
	"errors"
	"log/slog"
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

// WatchAccess calls revoke once check fails, so a long-lived socket does not outlive its caller's access.
func WatchAccess(ctx context.Context, revoke func(), check func(context.Context) error) {
	ticker := time.NewTicker(accessCheckPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A failed lookup also revokes: an unverifiable session must not keep a shell open.
			checkCtx, checkCancel := context.WithTimeout(ctx, writeWait)
			err := check(checkCtx)
			checkCancel()
			// A slow lookup times out and revokes too.
			if err != nil && ctx.Err() == nil {
				slog.InfoContext(ctx, "Closing websocket after access check failed", "error", err)
				revoke()
				return
			}
		}
	}
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
