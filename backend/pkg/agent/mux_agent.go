package agent

import (
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/klauspost/compress/gzhttp"

	"getarcane.dev/arcane/v3/pkg/httpx"
)

func (c *TunnelClient) serveMuxSession(ctx context.Context, rw *muxConn, streamWindowMiB int) error {
	compress, err := gzhttp.NewWrapper(
		gzhttp.MinSize(muxCompressMinSize),
		gzhttp.CompressionLevel(gzip.BestSpeed),
		gzhttp.ContentTypes([]string{"application/json", "application/problem+json", "text/plain"}),
	)
	if err != nil {
		_ = rw.Close()
		return fmt.Errorf("configure agent tunnel compression: %w", err)
	}
	compressed := compress(c.handler)

	session, err := yamux.Client(rw, newMuxConfig(true, streamWindowMiB))
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
			if stream || rw.pendingLimit.Load() >= muxMaxPendingWrite {
				c.handler.ServeHTTP(w, r)
				return
			}
			compressed.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       180 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ConnContext:       httpx.WithConn,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
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
