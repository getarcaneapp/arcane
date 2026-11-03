package agent

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	scriptedAgentStream struct {
		ctx  context.Context
		msgs []*tunnelpb.ManagerMessage
	}
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

func TestTunnelMessageToAgentProto_RegisterHost(t *testing.T) {
	host := &AgentHostInfo{
		Hostname:      "edge-1",
		OS:            "Debian GNU/Linux 13",
		KernelVersion: "6.12.0",
		Architecture:  "x86_64",
		DockerVersion: "28.5.0",
		AgentVersion:  "3.0.0",
		MemoryBytes:   8 << 30,
		CPUs:          4,
	}
	protoMsg, err := tunnelMessageToAgentProto(&TunnelMessage{
		Type:       MessageTypeRegister,
		AgentToken: "arc_123",
		Host:       host,
	})
	require.NoError(t, err)

	decoded, err := agentProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)
	assert.Equal(t, host, decoded.Host)

	withoutHost, err := tunnelMessageToAgentProto(&TunnelMessage{Type: MessageTypeRegister})
	require.NoError(t, err)
	decoded, err = agentProtoToTunnelMessage(withoutHost)
	require.NoError(t, err)
	assert.Nil(t, decoded.Host)
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
