package edge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelMessageToManagerProto_RoundTripRequest(t *testing.T) {
	original := &TunnelMessage{
		ID:      "req-1",
		Type:    MessageTypeRequest,
		Method:  "POST",
		Path:    "/api/test",
		Query:   "a=1",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"ok":true}`),
	}

	protoMsg, err := tunnelMessageToManagerProto(original)
	require.NoError(t, err)

	decoded, err := managerProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Method, decoded.Method)
	assert.Equal(t, original.Path, decoded.Path)
	assert.Equal(t, original.Query, decoded.Query)
	assert.Equal(t, original.Headers, decoded.Headers)
	assert.Equal(t, original.Body, decoded.Body)
}

func TestTunnelMessageToAgentProto_RoundTripResponse(t *testing.T) {
	original := &TunnelMessage{
		ID:      "req-1",
		Type:    MessageTypeResponse,
		Status:  201,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"created":true}`),
	}

	protoMsg, err := tunnelMessageToAgentProto(original)
	require.NoError(t, err)

	decoded, err := agentProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Status, decoded.Status)
	assert.Equal(t, original.Headers, decoded.Headers)
	assert.Equal(t, original.Body, decoded.Body)
}

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
	_, err := tunnelMessageToManagerProto(&TunnelMessage{Type: MessageTypeResponse})
	require.Error(t, err)
}
