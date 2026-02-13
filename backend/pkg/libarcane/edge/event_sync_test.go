package edge

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEventTunnelConn struct {
	mu      sync.Mutex
	msgs    []*TunnelMessage
	closed  bool
	sendErr error
}

func (f *fakeEventTunnelConn) Send(msg *TunnelMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	copyMsg := *msg
	copyMsg.Event = cloneTunnelEvent(msg.Event)
	f.msgs = append(f.msgs, &copyMsg)
	return nil
}

func (f *fakeEventTunnelConn) Receive() (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeEventTunnelConn) IsExpectedReceiveError(error) bool { return false }

func (f *fakeEventTunnelConn) Close() error { f.closed = true; return nil }

func (f *fakeEventTunnelConn) IsClosed() bool { return f.closed }

func (f *fakeEventTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func TestPublishEventToManager_NoActiveTunnel(t *testing.T) {
	clearActiveAgentTunnelConn(getActiveAgentTunnelConn())

	err := PublishEventToManager(&TunnelEvent{
		Type:  "container.start",
		Title: "Container started",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoActiveAgentTunnel)
}

func TestPublishEventToManager_SendsEventMessage(t *testing.T) {
	conn := &fakeEventTunnelConn{}
	setActiveAgentTunnelConn(conn)
	defer clearActiveAgentTunnelConn(conn)

	err := PublishEventToManager(&TunnelEvent{
		Type:         "container.start",
		Title:        "Container started",
		Severity:     "success",
		MetadataJSON: []byte(`{"source":"test"}`),
	})
	require.NoError(t, err)
	require.Len(t, conn.msgs, 1)
	assert.Equal(t, MessageTypeEvent, conn.msgs[0].Type)
	require.NotNil(t, conn.msgs[0].Event)
	assert.Equal(t, "container.start", conn.msgs[0].Event.Type)
	assert.Equal(t, "Container started", conn.msgs[0].Event.Title)
	assert.Equal(t, []byte(`{"source":"test"}`), conn.msgs[0].Event.MetadataJSON)
}
