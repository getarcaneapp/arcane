package httpx

import (
	"context"
	"net"
	"sync"
	"time"
)

type connContextKeyInternal struct{}

// connStateInternal tracks per-connection socket tuning. HTTP/2 multiplexes
// every stream a client opens onto one TCP connection, so the dead-peer
// timeout has to outlive whichever stream happens to finish first.
type connStateInternal struct {
	conn net.Conn

	mu      sync.Mutex
	holders int
}

// WithConn stores an accepted connection on its base context. Wire it up as
// http.Server.ConnContext so long-lived handlers can tune socket options for
// their own connection without affecting every other client.
func WithConn(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, connContextKeyInternal{}, &connStateInternal{conn: conn})
}

// AcquireDeadPeerTimeout bounds how long data written to the request's
// connection may stay unacknowledged before the kernel tears it down, and
// returns a release function restoring the system default once every caller
// sharing the connection has released it.
//
// Long-lived streams need this because they write on their own schedule into a
// connection the client may have silently abandoned: left to the defaults the
// kernel retransmits until tcp_retries2 is exhausted, roughly eleven minutes.
// TCP keepalive does not cover the case, since probes only fire while a
// connection is idle and a stream with an unacknowledged write in flight never
// is.
//
// It is deliberately scoped to one connection rather than the listener: the
// same listener carries edge tunnel gRPC streams, which tolerate up to
// edge.TunnelStaleTimeout of silence and would be reset by a shared timeout.
//
// The release function is always safe to call, including when acquisition
// failed or the request never passed through WithConn.
func AcquireDeadPeerTimeout(ctx context.Context, timeout time.Duration) (func(), error) {
	state, _ := ctx.Value(connContextKeyInternal{}).(*connStateInternal)
	if state == nil {
		return func() {}, nil
	}
	return state.acquire(timeout)
}

func (s *connStateInternal) acquire(timeout time.Duration) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Callers share a single timeout value, so only the first has to apply it.
	if s.holders == 0 {
		if err := setDeadPeerTimeoutOnConnInternal(s.conn, timeout); err != nil {
			return func() {}, err
		}
	}
	s.holders++

	var once sync.Once
	return func() {
		once.Do(s.release)
	}, nil
}

func (s *connStateInternal) release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.holders--
	if s.holders > 0 {
		return
	}
	// HTTP keep-alive can hand this connection to ordinary requests once the
	// last stream ends, so give back the system default.
	_ = setDeadPeerTimeoutOnConnInternal(s.conn, 0)
}

func setDeadPeerTimeoutOnConnInternal(conn net.Conn, timeout time.Duration) error {
	tcpConn := tcpConnFromInternal(conn)
	if tcpConn == nil {
		return nil
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return err
	}
	return setDeadPeerTimeoutInternal(rawConn, timeout)
}

// tcpConnFromInternal unwraps transport wrappers (TLS, most notably) to reach
// the underlying TCP connection. Returns nil when there isn't one.
func tcpConnFromInternal(conn net.Conn) *net.TCPConn {
	for range 4 {
		switch typed := conn.(type) {
		case nil:
			return nil
		case *net.TCPConn:
			return typed
		case interface{ NetConn() net.Conn }:
			conn = typed.NetConn()
		default:
			return nil
		}
	}
	return nil
}
