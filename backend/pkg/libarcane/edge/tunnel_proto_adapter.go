package edge

import (
	"errors"
	"fmt"
	"slices"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

// errUnknownTunnelPayload marks a proto payload this peer cannot decode, such as a oneof case
// added by a newer peer. Receivers skip it, as unknown websocket message types are skipped.
var errUnknownTunnelPayload = errors.New("unknown tunnel payload type")

func tunnelMessageToManagerProto(msg *TunnelMessage) (*tunnelpb.ManagerMessage, error) {
	if msg == nil {
		return nil, errors.New("message is nil")
	}
	if msg.Type != MessageTypeRegisterResponse {
		return nil, fmt.Errorf("unsupported manager message type: %s", msg.Type)
	}
	return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_RegisterResponse{RegisterResponse: &tunnelpb.RegisterResponse{
		Id:            msg.ID,
		Accepted:      msg.Accepted,
		EnvironmentId: msg.EnvironmentID,
		Error:         msg.Error,
		SessionId:     msg.SessionID,
		SecurityMode:  msg.SecurityMode,
		Capabilities:  slices.Clone(msg.Capabilities),
		DrainPrevious: msg.DrainPrevious,
	}}}, nil
}

func managerProtoToTunnelMessage(msg *tunnelpb.ManagerMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("manager message is nil")
	}

	switch payload := msg.GetPayload().(type) {
	case *tunnelpb.ManagerMessage_RegisterResponse:
		return &TunnelMessage{
			ID:            payload.RegisterResponse.GetId(),
			Type:          MessageTypeRegisterResponse,
			Accepted:      payload.RegisterResponse.GetAccepted(),
			EnvironmentID: payload.RegisterResponse.GetEnvironmentId(),
			Error:         payload.RegisterResponse.GetError(),
			SessionID:     payload.RegisterResponse.GetSessionId(),
			SecurityMode:  payload.RegisterResponse.GetSecurityMode(),
			Capabilities:  slices.Clone(payload.RegisterResponse.GetCapabilities()),
			DrainPrevious: payload.RegisterResponse.GetDrainPrevious(),
		}, nil
	default:
		return nil, fmt.Errorf("manager payload %T: %w", payload, errUnknownTunnelPayload)
	}
}

func tunnelMessageToAgentProto(msg *TunnelMessage) (*tunnelpb.AgentMessage, error) {
	if msg == nil {
		return nil, errors.New("message is nil")
	}
	if msg.Type != MessageTypeRegister {
		return nil, fmt.Errorf("unsupported agent message type: %s", msg.Type)
	}
	return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{
		AgentToken:      msg.AgentToken,
		AgentInstanceId: msg.AgentInstance,
		Capabilities:    slices.Clone(msg.Capabilities),
		ResumeSessionId: msg.ResumeSession,
	}}}, nil
}

func agentProtoToTunnelMessage(msg *tunnelpb.AgentMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("agent message is nil")
	}

	switch payload := msg.GetPayload().(type) {
	case *tunnelpb.AgentMessage_Register:
		return &TunnelMessage{
			Type:          MessageTypeRegister,
			AgentToken:    payload.Register.GetAgentToken(),
			AgentInstance: payload.Register.GetAgentInstanceId(),
			Capabilities:  slices.Clone(payload.Register.GetCapabilities()),
			ResumeSession: payload.Register.GetResumeSessionId(),
		}, nil
	default:
		return nil, fmt.Errorf("agent payload %T: %w", payload, errUnknownTunnelPayload)
	}
}
