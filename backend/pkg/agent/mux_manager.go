package agent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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

func (m *muxSession) dial(ctx context.Context) (net.Conn, error) {
	select {
	case <-m.ready:
	case <-m.closed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	session := m.session.Load()
	if session == nil {
		return nil, ErrTunnelConnectionClosed
	}
	conn, err := openMuxStream(ctx, session)
	if err != nil {
		return nil, err
	}
	m.opened.Add(1)
	return conn, nil
}

// close tears the session down and fails waiting and future dials with ErrTunnelConnectionClosed.
func (m *muxSession) close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		close(m.closed)
		session := m.session.Swap(nil)
		m.mu.Unlock()
		if session != nil {
			_ = session.Close()
		}
	})
}

func (m *muxSession) alive(ctx context.Context, timeout time.Duration) bool {
	session := m.session.Load()
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

func (m *muxSession) recordRTT(rtt time.Duration) {
	ns := int64(rtt)
	m.rtt.Store(ns)
	m.rttSum.Add(ns)
	first := m.rttCount.Add(1) == 1
	for {
		current := m.rttMin.Load()
		if (!first && current <= ns) || m.rttMin.CompareAndSwap(current, ns) {
			break
		}
	}
	for {
		current := m.rttMax.Load()
		if current >= ns || m.rttMax.CompareAndSwap(current, ns) {
			break
		}
	}
}

func (m *muxSession) traffic() (int64, int64) {
	conn := m.conn.Load()
	if conn == nil {
		return 0, 0
	}
	return conn.rx.Load(), conn.tx.Load()
}

// start publishes session to waiting dials. It reports false when the tunnel already closed.
func (m *muxSession) start(session *yamux.Session, conn *muxConn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.closed:
		return false
	default:
		m.conn.Store(conn)
		m.session.Store(session)
		close(m.ready)
		return true
	}
}

// serveMuxTunnel runs the manager end of a multiplexed session until the agent disconnects or the tunnel closes.
func (s *TunnelServer) serveMuxTunnel(ctx context.Context, tunnel *AgentTunnel, rw *muxConn) error {
	defer tunnel.mux.close()
	session, err := yamux.Server(rw, newMuxConfig(false, muxStreamWindowMiB(tunnel.Capabilities, s.streamWindowMiB())))
	if err != nil {
		_ = rw.Close()
		return fmt.Errorf("start agent tunnel session: %w", err)
	}
	if !tunnel.mux.start(session, rw) {
		return session.Close()
	}

	callbackCtx := context.WithoutCancel(ctx)
	s.updateConnectionStatus(callbackCtx, tunnel, true)

	events := &http.Server{
		Handler:           s.muxEventHandler(callbackCtx, tunnel.EnvironmentID),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
	}
	go func() {
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
			tunnel.mux.recordRTT(rtt)
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
		eventCtx, cancel := context.WithTimeout(ctx, agentEventTimeout)
		defer cancel()
		if err := s.eventCallback(eventCtx, envID, &event); err != nil {
			slog.WarnContext(eventCtx, "Failed to process agent event", "environmentId", envID, "type", event.Type, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
