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

func TestTunnelMessageToAgentProto_RoundTripEvent(t *testing.T) {
	original := &TunnelMessage{
		Type: MessageTypeEvent,
		Event: &TunnelEvent{
			Type:         "container.start",
			Severity:     "success",
			Title:        "Container started",
			Description:  "Container started: web",
			ResourceType: "container",
			ResourceID:   "abc123",
			ResourceName: "web",
			UserID:       "user-1",
			Username:     "arcane",
			MetadataJSON: []byte(`{"source":"agent"}`),
		},
	}

	protoMsg, err := tunnelMessageToAgentProto(original)
	require.NoError(t, err)

	decoded, err := agentProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)

	require.NotNil(t, decoded.Event)
	assert.Equal(t, MessageTypeEvent, decoded.Type)
	assert.Equal(t, original.Event.Type, decoded.Event.Type)
	assert.Equal(t, original.Event.Severity, decoded.Event.Severity)
	assert.Equal(t, original.Event.Title, decoded.Event.Title)
	assert.Equal(t, original.Event.Description, decoded.Event.Description)
	assert.Equal(t, original.Event.ResourceType, decoded.Event.ResourceType)
	assert.Equal(t, original.Event.ResourceID, decoded.Event.ResourceID)
	assert.Equal(t, original.Event.ResourceName, decoded.Event.ResourceName)
	assert.Equal(t, original.Event.UserID, decoded.Event.UserID)
	assert.Equal(t, original.Event.Username, decoded.Event.Username)
	assert.Equal(t, original.Event.MetadataJSON, decoded.Event.MetadataJSON)
}

func TestTunnelMessageToManagerProto_UnsupportedType(t *testing.T) {
	_, err := tunnelMessageToManagerProto(&TunnelMessage{Type: MessageTypeResponse})
	require.Error(t, err)
}
