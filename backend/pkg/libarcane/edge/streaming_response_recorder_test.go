package edge

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTunnelConn struct {
	mu      sync.Mutex
	msgs    []*TunnelMessage
	closed  bool
	sendErr error
}

func (f *fakeTunnelConn) Send(msg *TunnelMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	copyMsg := *msg
	if msg.Headers != nil {
		copyMsg.Headers = cloneHeaderMap(msg.Headers)
	}
	if msg.Body != nil {
		copyMsg.Body = append([]byte(nil), msg.Body...)
	}
	f.msgs = append(f.msgs, &copyMsg)
	return nil
}

func (f *fakeTunnelConn) Receive() (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTunnelConn) IsExpectedReceiveError(error) bool {
	return false
}

func (f *fakeTunnelConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeTunnelConn) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func TestStreamingResponseRecorder_Sequence(t *testing.T) {
	conn := &fakeTunnelConn{}
	r := newStreamingResponseRecorder("req-1", conn)

	r.Header().Set("Content-Type", "text/plain")

	n, err := r.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	n, err = r.Write([]byte(" world"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)

	require.NoError(t, r.Close())

	require.Len(t, conn.msgs, 4)
	assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
	assert.Equal(t, "req-1", conn.msgs[0].ID)
	assert.Equal(t, "text/plain", conn.msgs[0].Headers["Content-Type"])
	assert.Equal(t, "1", conn.msgs[0].Headers["X-Arcane-Tunnel-Stream"])

	assert.Equal(t, MessageTypeStreamData, conn.msgs[1].Type)
	assert.Equal(t, "hello", string(conn.msgs[1].Body))

	assert.Equal(t, MessageTypeStreamData, conn.msgs[2].Type)
	assert.Equal(t, " world", string(conn.msgs[2].Body))

	assert.Equal(t, MessageTypeStreamEnd, conn.msgs[3].Type)
}

func TestStreamingResponseRecorder_WriteHeaderAndClose(t *testing.T) {
	conn := &fakeTunnelConn{}
	r := newStreamingResponseRecorder("req-2", conn)

	r.WriteHeader(http.StatusCreated)
	require.NoError(t, r.Close())

	require.Len(t, conn.msgs, 2)
	assert.Equal(t, MessageTypeResponse, conn.msgs[0].Type)
	assert.Equal(t, http.StatusCreated, conn.msgs[0].Status)
	assert.Equal(t, MessageTypeStreamEnd, conn.msgs[1].Type)
}
