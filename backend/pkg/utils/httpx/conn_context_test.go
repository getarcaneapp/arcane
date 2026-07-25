package httpx

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newConnContextForTest(t *testing.T) context.Context {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return WithConn(context.Background(), conn)
}

// HTTP/2 multiplexes the dashboard and activity streams onto one connection, so
// the first to finish must not restore the default timeout out from under the
// other.
func TestAcquireDeadPeerTimeoutHoldsUntilLastRelease(t *testing.T) {
	ctx := newConnContextForTest(t)
	state, _ := ctx.Value(connContextKeyInternal{}).(*connStateInternal)
	require.NotNil(t, state)

	releaseFirst, err := AcquireDeadPeerTimeout(ctx, 45*time.Second)
	require.NoError(t, err)
	releaseSecond, err := AcquireDeadPeerTimeout(ctx, 45*time.Second)
	require.NoError(t, err)
	require.Equal(t, 2, state.holders)

	releaseFirst()
	require.Equal(t, 1, state.holders, "connection must stay bounded while a stream is still running")

	releaseSecond()
	require.Equal(t, 0, state.holders)

	// Releasing twice must not drop the count below zero and re-arm the socket
	// for the next acquirer.
	releaseSecond()
	require.Equal(t, 0, state.holders)
}

// Handlers reached over a non-TCP transport (or a request whose base context
// never passed through WithConn) must degrade quietly rather than fail the
// stream.
func TestAcquireDeadPeerTimeoutIgnoresMissingConn(t *testing.T) {
	release, err := AcquireDeadPeerTimeout(context.Background(), 45*time.Second)
	require.NoError(t, err)
	require.NotPanics(t, release)
}
