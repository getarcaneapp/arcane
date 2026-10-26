package agent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	// maxMuxEventBytes bounds one agent event posted over a multiplexed session.
	maxMuxEventBytes           = 1 << 20
	muxRegistrationPingTimeout = 3 * time.Second
)

// dial opens a stream for the session client. Callers that found the tunnel during the register handshake
// wait for the session to start.
func (m *muxSession) dial(ctx context.Context) (net.Conn, error) {
	select {
	case <-m.ready:
	case <-m.closed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if session == nil {
		return nil, ErrTunnelConnectionClosed
	}
	return openMuxStream(ctx, session)
}

// close tears the session down and fails waiting and future dials with ErrTunnelConnectionClosed.
func (m *muxSession) close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		close(m.closed)
		session := m.session
		m.session = nil
		m.mu.Unlock()
		if session != nil {
			_ = session.Close()
		}
	})
}

func (m *muxSession) alive(ctx context.Context, timeout time.Duration) bool {
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if session == nil {
		return true
	}
	pinged := make(chan error, 1)
	go func() {
		_, err := session.Ping()
		pinged <- err
	}()
	select {
	case err := <-pinged:
		return err == nil
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return true
	}
}

// start publishes session to waiting dials. It reports false when the tunnel already closed.
func (m *muxSession) start(session *yamux.Session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.closed:
		return false
	default:
		m.session = session
		close(m.ready)
		return true
	}
}

// serveMuxTunnel runs the manager end of a multiplexed session until the agent disconnects or the tunnel closes.
func (s *TunnelServer) serveMuxTunnel(ctx context.Context, tunnel *AgentTunnel, rw io.ReadWriteCloser) error {
	defer tunnel.mux.close()
	session, err := yamux.Server(rw, newMuxConfig(false))
	if err != nil {
		_ = rw.Close()
		return fmt.Errorf("start agent tunnel session: %w", err)
	}
	if !tunnel.mux.start(session) {
		return session.Close()
	}

	callbackCtx := context.WithoutCancel(ctx)
	s.updateConnectionStatus(callbackCtx, tunnel, true)

	// Agents open streams to post events; the manager serves nothing else on its end.
	events := &http.Server{
		Handler:           s.muxEventHandler(callbackCtx, tunnel.EnvironmentID),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
	}
	go func() {
		// Closing the server or the session ends Serve normally; anything else is a real failure.
		serveErr := events.Serve(session)
		if !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, yamux.ErrSessionShutdown) {
			slog.WarnContext(callbackCtx, "Agent tunnel event server stopped", "environmentId", tunnel.EnvironmentID, "error", serveErr)
		}
	}()
	defer func() { _ = events.Close() }()

	ticker := time.NewTicker(DefaultHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-session.CloseChan():
			return nil
		case <-tunnel.done:
			return nil
		case <-ticker.C:
			rtt, pingErr := session.Ping()
			if pingErr != nil {
				_ = tunnel.CloseWithReason("agent tunnel keepalive failed")
				return fmt.Errorf("agent tunnel keepalive failed: %w", pingErr)
			}
			tunnel.mux.rtt.Store(int64(rtt))
			tunnel.UpdateHeartbeat()
		}
	}
}

// muxEventHandler accepts the events an agent posts over its multiplexed session.
func (s *TunnelServer) muxEventHandler(ctx context.Context, envID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != muxEventsPath {
			http.NotFound(w, r)
			return
		}
		var event TunnelEvent
		if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxMuxEventBytes), &event); err != nil {
			http.Error(w, "invalid agent event: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateTunnelEvent(&event); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.EventCallback != nil {
			eventCtx, cancel := context.WithTimeout(ctx, agentEventTimeout)
			defer cancel()
			if err := s.EventCallback(eventCtx, envID, &event); err != nil {
				slog.WarnContext(eventCtx, "Failed to process agent event", "environmentId", envID, "type", event.Type, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
