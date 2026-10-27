package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

// serveMuxSession serves manager requests over a multiplexed session with the agent's own router until the session ends.
func (c *TunnelClient) serveMuxSession(ctx context.Context, rw io.ReadWriteCloser) error {
	session, err := yamux.Client(rw, newMuxConfig(true))
	if err != nil {
		_ = rw.Close()
		return fmt.Errorf("start agent tunnel session: %w", err)
	}

	srv := &http.Server{
		// The agent command allowlist stays the security boundary for what the manager may call.
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stream := httpx.IsWebSocketUpgradeRequest(r)
			if ResolveCommandName(r.Method, r.URL.EscapedPath(), stream).IsAbsent() {
				slog.WarnContext(r.Context(), "Rejected agent tunnel request outside the command allowlist", "method", r.Method, "path", r.URL.Path, "websocket", stream)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"error":"unsupported agent command"}`))
				return
			}
			c.handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       180 * time.Second,
		// Manager requests are logged by the agent's request logger, like any other API call.
		BaseContext: func(net.Listener) context.Context { return ctx },
		ConnContext: httpx.WithConn,
		ErrorLog:    slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
	}
	// Closing the server closes the session it listens on and every stream.
	defer func() { _ = srv.Close() }()

	eventsClient := newMuxHTTPClient(func(dialCtx context.Context) (net.Conn, error) {
		return openMuxStream(dialCtx, session)
	})
	activeAgentEvents.Store(eventsClient)
	// A newer session may already have replaced this one.
	defer activeAgentEvents.CompareAndSwap(eventsClient, nil)

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(session) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-session.CloseChan():
		return ErrTunnelConnectionClosed
	case serveErr := <-serveDone:
		return serveErr
	}
}
