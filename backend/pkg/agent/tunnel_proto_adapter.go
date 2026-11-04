package agent

import (
	"errors"
	"fmt"
	"slices"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
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
	return tunnelpb.ManagerMessage_builder{
		RegisterResponse: tunnelpb.RegisterResponse_builder{
			Accepted:      msg.Accepted,
			EnvironmentId: msg.EnvironmentID,
			Error:         msg.Error,
			SessionId:     msg.SessionID,
			SecurityMode:  msg.SecurityMode,
			Capabilities:  slices.Clone(msg.Capabilities),
			DrainPrevious: msg.DrainPrevious,
			Id:            msg.ID,
		}.Build(),
	}.Build(), nil
}

func managerProtoToTunnelMessage(msg *tunnelpb.ManagerMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("manager message is nil")
	}

	switch msg.WhichPayload() {
	case tunnelpb.ManagerMessage_RegisterResponse_case:
		resp := msg.GetRegisterResponse()
		return &TunnelMessage{
			ID:            resp.GetId(),
			Type:          MessageTypeRegisterResponse,
			Accepted:      resp.GetAccepted(),
			EnvironmentID: resp.GetEnvironmentId(),
			Error:         resp.GetError(),
			SessionID:     resp.GetSessionId(),
			SecurityMode:  resp.GetSecurityMode(),
			Capabilities:  slices.Clone(resp.GetCapabilities()),
			DrainPrevious: resp.GetDrainPrevious(),
		}, nil
	default:
		return nil, fmt.Errorf("manager payload %v: %w", msg.WhichPayload(), errUnknownTunnelPayload)
	}
}

func tunnelMessageToAgentProto(msg *TunnelMessage) (*tunnelpb.AgentMessage, error) {
	if msg == nil {
		return nil, errors.New("message is nil")
	}
	if msg.Type != MessageTypeRegister {
		return nil, fmt.Errorf("unsupported agent message type: %s", msg.Type)
	}
	return tunnelpb.AgentMessage_builder{
		Register: tunnelpb.RegisterRequest_builder{
			AgentToken:      msg.AgentToken,
			AgentInstanceId: msg.AgentInstance,
			Capabilities:    slices.Clone(msg.Capabilities),
			ResumeSessionId: msg.ResumeSession,
			Host:            agentHostInfoToProto(msg.Host),
		}.Build(),
	}.Build(), nil
}

func agentProtoToTunnelMessage(msg *tunnelpb.AgentMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("agent message is nil")
	}

	switch msg.WhichPayload() {
	case tunnelpb.AgentMessage_Register_case:
		req := msg.GetRegister()
		return &TunnelMessage{
			Type:          MessageTypeRegister,
			AgentToken:    req.GetAgentToken(),
			AgentInstance: req.GetAgentInstanceId(),
			Capabilities:  slices.Clone(req.GetCapabilities()),
			ResumeSession: req.GetResumeSessionId(),
			Host:          protoToAgentHostInfo(req.GetHost()),
		}, nil
	default:
		return nil, fmt.Errorf("agent payload %v: %w", msg.WhichPayload(), errUnknownTunnelPayload)
	}
}

func agentHostInfoToProto(host *AgentHostInfo) *tunnelpb.AgentHostInfo {
	if host == nil {
		return nil
	}
	return tunnelpb.AgentHostInfo_builder{
		Hostname:      host.Hostname,
		Os:            host.OS,
		KernelVersion: host.KernelVersion,
		Architecture:  host.Architecture,
		DockerVersion: host.DockerVersion,
		AgentVersion:  host.AgentVersion,
		Cpus:          host.CPUs,
		MemoryBytes:   host.MemoryBytes,
	}.Build()
}

func protoToAgentHostInfo(host *tunnelpb.AgentHostInfo) *AgentHostInfo {
	if host == nil {
		return nil
	}
	return &AgentHostInfo{
		Hostname:      host.GetHostname(),
		OS:            host.GetOs(),
		KernelVersion: host.GetKernelVersion(),
		Architecture:  host.GetArchitecture(),
		DockerVersion: host.GetDockerVersion(),
		AgentVersion:  host.GetAgentVersion(),
		CPUs:          host.GetCpus(),
		MemoryBytes:   host.GetMemoryBytes(),
	}
}
