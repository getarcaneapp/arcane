package ws

import (
	"net/http"

	"emperror.dev/errors"
	"github.com/coder/websocket"
)

// Accept validates the request Origin with checkOrigin and upgrades it to a
// WebSocket. The origin check is the CSRF barrier for cookie-authenticated
// upgrades; a nil checkOrigin skips it and must only be used for endpoints
// authenticated by tokens or mTLS rather than browser cookies.
func Accept(w http.ResponseWriter, r *http.Request, checkOrigin func(*http.Request) bool) (*websocket.Conn, error) {
	if checkOrigin != nil && !checkOrigin(r) {
		http.Error(w, "websocket origin not allowed", http.StatusForbidden)
		return nil, errors.New("websocket origin not allowed")
	}
	return websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin validated above
		CompressionMode:    websocket.CompressionContextTakeover,
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
