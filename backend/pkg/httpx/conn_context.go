package httpx

import (
	"context"
	"net"
	"sync"
	"time"
)

type (
	connContextKey struct{}

	connState struct {
		conn net.Conn

		mu      sync.Mutex
		holders int

		clearReadDeadlineOnce sync.Once
	}
)

func WithConn(ctx context.Context, conn net.Conn) context.Context {
	return context.WithValue(ctx, connContextKey{}, &connState{conn: conn})
}

func ClearReadDeadline(ctx context.Context) {
	state, _ := ctx.Value(connContextKey{}).(*connState)
	if state == nil {
		return
	}
	state.clearReadDeadlineOnce.Do(func() {
		_ = state.conn.SetReadDeadline(time.Time{})
	})
}

func AcquireDeadPeerTimeout(ctx context.Context, timeout time.Duration) (func(), error) {
	state, _ := ctx.Value(connContextKey{}).(*connState)
	if state == nil {
		return func() {}, nil
	}
	return state.acquire(timeout)
}

func (s *connState) acquire(timeout time.Duration) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Callers share a single timeout value, so only the first has to apply it.
	if s.holders == 0 {
		if err := setDeadPeerTimeoutOnConn(s.conn, timeout); err != nil {
			return func() {}, err
		}
	}
	s.holders++

	var once sync.Once
	return func() {
		once.Do(s.release)
	}, nil
}

func (s *connState) release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.holders--
	if s.holders > 0 {
		return
	}
	// HTTP keep-alive can hand this connection to ordinary requests once the
	// last stream ends, so give back the system default.
	_ = setDeadPeerTimeoutOnConn(s.conn, 0)
}

func setDeadPeerTimeoutOnConn(conn net.Conn, timeout time.Duration) error {
	tcpConn := tcpConnFrom(conn)
	if tcpConn == nil {
		return nil
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return err
	}
	return setDeadPeerTimeout(rawConn, timeout)
}

func tcpConnFrom(conn net.Conn) *net.TCPConn {
	for range 4 {
		switch typed := conn.(type) {
		case nil:
			return nil
		case *net.TCPConn:
			return typed
		case interface{ NetConn() net.Conn }:
			conn = typed.NetConn()
		case interface{ Raw() net.Conn }:
			conn = typed.Raw()
		default:
			return nil
		}
	}
	return nil
}
