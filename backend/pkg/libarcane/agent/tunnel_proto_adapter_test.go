package agent

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v3/proto/tunnel/v1"
)

func TestTunnelMessageToAgentProto_Register(t *testing.T) {
	protoMsg, err := tunnelMessageToAgentProto(&TunnelMessage{
		Type:       MessageTypeRegister,
		AgentToken: "arc_123",
	})
	require.NoError(t, err)

	decoded, err := agentProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeRegister, decoded.Type)
	assert.Equal(t, "arc_123", decoded.AgentToken)
}

func TestTunnelMessageToManagerProto_UnsupportedType(t *testing.T) {
	_, err := tunnelMessageToManagerProto(&TunnelMessage{Type: MessageTypeRegister})
	require.Error(t, err)
}

func TestGRPCAgentTunnelConn_ReceiveSkipsUnknownPayload(t *testing.T) {
	stream := &scriptedAgentStream{ctx: t.Context(), msgs: []*tunnelpb.ManagerMessage{
		{}, // nil payload, decodes as unknown
		{Payload: &tunnelpb.ManagerMessage_RegisterResponse{RegisterResponse: &tunnelpb.RegisterResponse{Id: "reg-2"}}},
	}}
	conn := NewGRPCAgentTunnelConn(stream)

	msg, err := conn.Receive()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRegisterResponse, msg.Type)
	assert.Equal(t, "reg-2", msg.ID)
}

type scriptedAgentStream struct {
	ctx  context.Context
	msgs []*tunnelpb.ManagerMessage
}

func (s *scriptedAgentStream) Send(*tunnelpb.AgentMessage) error { return nil }

func (s *scriptedAgentStream) Recv() (*tunnelpb.ManagerMessage, error) {
	if len(s.msgs) == 0 {
		return nil, io.EOF
	}
	msg := s.msgs[0]
	s.msgs = s.msgs[1:]
	return msg, nil
}

func (s *scriptedAgentStream) Context() context.Context { return s.ctx }

func (s *scriptedAgentStream) CloseSend() error { return nil }
