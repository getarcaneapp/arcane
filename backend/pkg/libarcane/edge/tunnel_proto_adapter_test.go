package edge

import (
	"context"
	"io"
	"testing"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
	"github.com/gorilla/websocket"
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

	protoMsg, err := tunnelMessageToManagerProto(original, false)
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

func TestTunnelMessageToManagerProto_StreamDataUsesWebSocketPayload(t *testing.T) {
	original := &TunnelMessage{
		ID:            "stream-1",
		Type:          MessageTypeStreamData,
		Body:          []byte("hello"),
		WSMessageType: websocket.TextMessage,
	}

	protoMsg, err := tunnelMessageToManagerProto(original, false)
	require.NoError(t, err)

	wsData := protoMsg.GetWsData()
	require.NotNil(t, wsData)
	assert.Equal(t, original.ID, wsData.GetStreamId())
	assert.Equal(t, original.Body, wsData.GetData())
	assert.Equal(t, int32(original.WSMessageType), wsData.GetMessageType())
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
	_, err := tunnelMessageToManagerProto(&TunnelMessage{Type: MessageTypeResponse}, false)
	require.Error(t, err)
}

func TestTunnelMessageToAgentProto_RoundTripStreamDataPreservesWebSocketType(t *testing.T) {
	original := &TunnelMessage{
		ID:            "stream-1",
		Type:          MessageTypeStreamData,
		Body:          []byte(`{"cpu":10}`),
		WSMessageType: websocket.TextMessage,
	}

	protoMsg, err := tunnelMessageToAgentProto(original)
	require.NoError(t, err)

	decoded, err := agentProtoToTunnelMessage(protoMsg)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Body, decoded.Body)
	assert.Equal(t, original.WSMessageType, decoded.WSMessageType)
}

func TestTunnelProtoAdapter_HeartbeatIDRoundTrip(t *testing.T) {
	ping, err := tunnelMessageToAgentProto(&TunnelMessage{ID: "hb-1", Type: MessageTypeHeartbeat})
	require.NoError(t, err)
	decodedPing, err := agentProtoToTunnelMessage(ping)
	require.NoError(t, err)
	assert.Equal(t, "hb-1", decodedPing.ID)

	pong, err := tunnelMessageToManagerProto(&TunnelMessage{ID: "hb-1", Type: MessageTypeHeartbeatAck}, false)
	require.NoError(t, err)
	decodedPong, err := managerProtoToTunnelMessage(pong)
	require.NoError(t, err)
	assert.Equal(t, "hb-1", decodedPong.ID)
}

func TestTunnelProtoAdapter_FileChunkMetadataRoundTrip(t *testing.T) {
	original := &TunnelMessage{
		ID:       "transfer-1",
		Type:     MessageTypeFileChunk,
		Body:     []byte("chunk"),
		Sequence: 2,
		Metadata: map[string]string{bodyTransferMetadataKey: "transfer-1"},
	}

	agentMsg, err := tunnelMessageToAgentProto(original)
	require.NoError(t, err)
	decoded, err := agentProtoToTunnelMessage(agentMsg)
	require.NoError(t, err)
	assert.Equal(t, original.Metadata, decoded.Metadata)

	managerMsg, err := tunnelMessageToManagerProto(original, false)
	require.NoError(t, err)
	decodedManager, err := managerProtoToTunnelMessage(managerMsg)
	require.NoError(t, err)
	assert.Equal(t, original.Metadata, decodedManager.Metadata)
}

func TestTunnelMessageToManagerProto_StreamDataParityEncoding(t *testing.T) {
	original := &TunnelMessage{
		ID:            "stream-1",
		Type:          MessageTypeStreamData,
		Body:          []byte("hello"),
		WSMessageType: websocket.TextMessage,
	}

	// Legacy agents receive the ws_data re-encode and see ws_data.
	legacy, err := tunnelMessageToManagerProto(original, false)
	require.NoError(t, err)
	decodedLegacy, err := managerProtoToTunnelMessage(legacy)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeWebSocketData, decodedLegacy.Type)

	// Parity-capable agents receive native stream_data with the type preserved.
	parity, err := tunnelMessageToManagerProto(original, true)
	require.NoError(t, err)
	require.NotNil(t, parity.GetStreamData())
	decodedParity, err := managerProtoToTunnelMessage(parity)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeStreamData, decodedParity.Type)
	assert.Equal(t, original.ID, decodedParity.ID)
	assert.Equal(t, original.Body, decodedParity.Body)
}

func TestTunnelMessageToManagerProto_StreamEndRequiresParity(t *testing.T) {
	original := &TunnelMessage{ID: "stream-1", Type: MessageTypeStreamEnd}

	_, err := tunnelMessageToManagerProto(original, false)
	require.Error(t, err)

	parity, err := tunnelMessageToManagerProto(original, true)
	require.NoError(t, err)
	decoded, err := managerProtoToTunnelMessage(parity)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeStreamEnd, decoded.Type)
	assert.Equal(t, original.ID, decoded.ID)
}

func TestGRPCAgentTunnelConn_ReceiveSkipsUnknownPayload(t *testing.T) {
	stream := &scriptedAgentStream{msgs: []*tunnelpb.ManagerMessage{
		{}, // nil payload, decodes as unknown
		{Payload: &tunnelpb.ManagerMessage_HeartbeatPong{HeartbeatPong: &tunnelpb.HeartbeatPong{Id: "hb-2"}}},
	}}
	conn := NewGRPCAgentTunnelConn(stream)

	msg, err := conn.Receive()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeHeartbeatAck, msg.Type)
	assert.Equal(t, "hb-2", msg.ID)
}

type scriptedAgentStream struct {
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

func (s *scriptedAgentStream) Context() context.Context { return context.Background() }

func (s *scriptedAgentStream) CloseSend() error { return nil }
