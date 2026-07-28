package edge

import (
	"maps"
	"math"

	"emperror.dev/errors"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

// errUnknownTunnelPayload marks a proto payload this peer cannot decode, e.g.
// a oneof case added by a newer peer. Receivers skip such messages instead of
// killing the stream, mirroring how unknown JSON message types are ignored on
// the websocket transport.
const errUnknownTunnelPayload = errors.Sentinel("unknown tunnel payload type")

// tunnelMessageToManagerProto encodes a manager->agent message. parity is true
// once the agent advertised tunnelCapabilityProtoParity; without it, legacy
// agents receive stream_data re-encoded as ws_data and cannot receive stream_end.
func tunnelMessageToManagerProto(msg *TunnelMessage, parity bool) (*tunnelpb.ManagerMessage, error) {
	if msg == nil {
		return nil, errors.New("message is nil")
	}

	switch msg.Type {
	case MessageTypeRequest:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_HttpRequest{HttpRequest: &tunnelpb.HttpRequest{
			RequestId: msg.ID,
			Method:    msg.Method,
			Path:      msg.Path,
			Query:     msg.Query,
			Headers:   cloneHeaderMap(msg.Headers),
			Body:      msg.Body,
		}}}, nil
	case MessageTypeHeartbeatAck:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_HeartbeatPong{HeartbeatPong: &tunnelpb.HeartbeatPong{Id: msg.ID}}}, nil
	case MessageTypeWebSocketStart:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_WsStart{WsStart: &tunnelpb.WebSocketStart{
			StreamId: msg.ID,
			Path:     msg.Path,
			Query:    msg.Query,
			Headers:  cloneHeaderMap(msg.Headers),
		}}}, nil
	case MessageTypeWebSocketData:
		messageType, err := intToInt32(msg.WSMessageType, "ws_message_type")
		if err != nil {
			return nil, err
		}
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_WsData{WsData: &tunnelpb.WebSocketData{
			StreamId:    msg.ID,
			Data:        msg.Body,
			MessageType: messageType,
		}}}, nil
	case MessageTypeStreamData:
		messageType, err := intToInt32(msg.WSMessageType, "ws_message_type")
		if err != nil {
			return nil, err
		}
		if parity {
			return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_StreamData{StreamData: &tunnelpb.StreamData{
				RequestId:   msg.ID,
				Data:        msg.Body,
				MessageType: messageType,
			}}}, nil
		}
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_WsData{WsData: &tunnelpb.WebSocketData{
			StreamId:    msg.ID,
			Data:        msg.Body,
			MessageType: messageType,
		}}}, nil
	case MessageTypeStreamEnd:
		if parity {
			return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_StreamEnd{StreamEnd: &tunnelpb.StreamEnd{RequestId: msg.ID}}}, nil
		}
		return nil, errors.Errorf("unsupported manager message type: %s", msg.Type)
	case MessageTypeWebSocketClose:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_WsClose{WsClose: &tunnelpb.WebSocketClose{StreamId: msg.ID}}}, nil
	case MessageTypeRegisterResponse:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_RegisterResponse{RegisterResponse: &tunnelpb.RegisterResponse{
			Id:            msg.ID,
			Accepted:      msg.Accepted,
			EnvironmentId: msg.EnvironmentID,
			Error:         msg.Error,
			SessionId:     msg.SessionID,
			SecurityMode:  msg.SecurityMode,
			Capabilities:  append([]string(nil), msg.Capabilities...),
			DrainPrevious: msg.DrainPrevious,
		}}}, nil
	case MessageTypeCommandRequest:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_CommandRequest{CommandRequest: &tunnelpb.CommandRequest{
			CommandId:       msg.ID,
			CommandName:     msg.Command,
			Method:          msg.Method,
			Path:            msg.Path,
			Query:           msg.Query,
			Headers:         cloneHeaderMap(msg.Headers),
			Body:            msg.Body,
			TimeoutMillis:   msg.TimeoutMillis,
			SessionId:       msg.SessionID,
			AgentInstanceId: msg.AgentInstance,
			Metadata:        cloneHeaderMap(msg.Metadata),
		}}}, nil
	case MessageTypeStreamOpen:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_StreamOpen{StreamOpen: &tunnelpb.StreamOpen{
			StreamId:    msg.ID,
			CommandName: msg.Command,
			Path:        msg.Path,
			Query:       msg.Query,
			Headers:     cloneHeaderMap(msg.Headers),
			SessionId:   msg.SessionID,
		}}}, nil
	case MessageTypeStreamClose:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_StreamClose{StreamClose: &tunnelpb.StreamClose{
			StreamId: msg.ID,
			Error:    msg.Error,
		}}}, nil
	case MessageTypeCancelRequest:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_CancelRequest{CancelRequest: &tunnelpb.CancelRequest{
			CommandId: msg.ID,
		}}}, nil
	case MessageTypeFileChunk:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_FileChunk{FileChunk: &tunnelpb.FileChunk{
			TransferId: msg.ID,
			Data:       msg.Body,
			Sequence:   msg.Sequence,
			Eof:        msg.EOF,
			Metadata:   cloneHeaderMap(msg.Metadata),
		}}}, nil
	case MessageTypeResponse,
		MessageTypeHeartbeat,
		MessageTypeRegister,
		MessageTypeEvent,
		MessageTypeCommandAck,
		MessageTypeCommandOutput,
		MessageTypeCommandComplete:
		return nil, errors.Errorf("unsupported manager message type: %s", msg.Type)
	default:
		return nil, errors.Errorf("unsupported manager message type: %s", msg.Type)
	}
}

func managerProtoToTunnelMessage(msg *tunnelpb.ManagerMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("manager message is nil")
	}

	switch payload := msg.GetPayload().(type) {
	case *tunnelpb.ManagerMessage_HttpRequest:
		return &TunnelMessage{
			ID:      payload.HttpRequest.GetRequestId(),
			Type:    MessageTypeRequest,
			Method:  payload.HttpRequest.GetMethod(),
			Path:    payload.HttpRequest.GetPath(),
			Query:   payload.HttpRequest.GetQuery(),
			Headers: cloneHeaderMap(payload.HttpRequest.GetHeaders()),
			Body:    payload.HttpRequest.GetBody(),
		}, nil
	case *tunnelpb.ManagerMessage_HeartbeatPong:
		return &TunnelMessage{ID: payload.HeartbeatPong.GetId(), Type: MessageTypeHeartbeatAck}, nil
	case *tunnelpb.ManagerMessage_WsStart:
		return &TunnelMessage{
			ID:      payload.WsStart.GetStreamId(),
			Type:    MessageTypeWebSocketStart,
			Path:    payload.WsStart.GetPath(),
			Query:   payload.WsStart.GetQuery(),
			Headers: cloneHeaderMap(payload.WsStart.GetHeaders()),
		}, nil
	case *tunnelpb.ManagerMessage_WsData:
		return &TunnelMessage{
			ID:            payload.WsData.GetStreamId(),
			Type:          MessageTypeWebSocketData,
			Body:          payload.WsData.GetData(),
			WSMessageType: int(payload.WsData.GetMessageType()),
		}, nil
	case *tunnelpb.ManagerMessage_WsClose:
		return &TunnelMessage{ID: payload.WsClose.GetStreamId(), Type: MessageTypeWebSocketClose}, nil
	case *tunnelpb.ManagerMessage_RegisterResponse:
		return &TunnelMessage{
			ID:            payload.RegisterResponse.GetId(),
			Type:          MessageTypeRegisterResponse,
			Accepted:      payload.RegisterResponse.GetAccepted(),
			EnvironmentID: payload.RegisterResponse.GetEnvironmentId(),
			Error:         payload.RegisterResponse.GetError(),
			SessionID:     payload.RegisterResponse.GetSessionId(),
			SecurityMode:  payload.RegisterResponse.GetSecurityMode(),
			Capabilities:  append([]string(nil), payload.RegisterResponse.GetCapabilities()...),
			DrainPrevious: payload.RegisterResponse.GetDrainPrevious(),
		}, nil
	case *tunnelpb.ManagerMessage_CommandRequest:
		return &TunnelMessage{
			ID:            payload.CommandRequest.GetCommandId(),
			Type:          MessageTypeCommandRequest,
			Command:       payload.CommandRequest.GetCommandName(),
			Method:        payload.CommandRequest.GetMethod(),
			Path:          payload.CommandRequest.GetPath(),
			Query:         payload.CommandRequest.GetQuery(),
			Headers:       cloneHeaderMap(payload.CommandRequest.GetHeaders()),
			Body:          payload.CommandRequest.GetBody(),
			TimeoutMillis: payload.CommandRequest.GetTimeoutMillis(),
			SessionID:     payload.CommandRequest.GetSessionId(),
			AgentInstance: payload.CommandRequest.GetAgentInstanceId(),
			Metadata:      cloneHeaderMap(payload.CommandRequest.GetMetadata()),
		}, nil
	case *tunnelpb.ManagerMessage_StreamOpen:
		return &TunnelMessage{
			ID:        payload.StreamOpen.GetStreamId(),
			Type:      MessageTypeStreamOpen,
			Command:   payload.StreamOpen.GetCommandName(),
			Path:      payload.StreamOpen.GetPath(),
			Query:     payload.StreamOpen.GetQuery(),
			Headers:   cloneHeaderMap(payload.StreamOpen.GetHeaders()),
			SessionID: payload.StreamOpen.GetSessionId(),
		}, nil
	case *tunnelpb.ManagerMessage_StreamClose:
		return &TunnelMessage{
			ID:    payload.StreamClose.GetStreamId(),
			Type:  MessageTypeStreamClose,
			Error: payload.StreamClose.GetError(),
		}, nil
	case *tunnelpb.ManagerMessage_CancelRequest:
		return &TunnelMessage{
			ID:   payload.CancelRequest.GetCommandId(),
			Type: MessageTypeCancelRequest,
		}, nil
	case *tunnelpb.ManagerMessage_FileChunk:
		return &TunnelMessage{
			ID:       payload.FileChunk.GetTransferId(),
			Type:     MessageTypeFileChunk,
			Body:     payload.FileChunk.GetData(),
			Sequence: payload.FileChunk.GetSequence(),
			EOF:      payload.FileChunk.GetEof(),
			Metadata: cloneHeaderMap(payload.FileChunk.GetMetadata()),
		}, nil
	case *tunnelpb.ManagerMessage_StreamData:
		return &TunnelMessage{
			ID:            payload.StreamData.GetRequestId(),
			Type:          MessageTypeStreamData,
			Body:          payload.StreamData.GetData(),
			WSMessageType: int(payload.StreamData.GetMessageType()),
		}, nil
	case *tunnelpb.ManagerMessage_StreamEnd:
		return &TunnelMessage{ID: payload.StreamEnd.GetRequestId(), Type: MessageTypeStreamEnd}, nil
	default:
		return nil, errors.WrapIff(errUnknownTunnelPayload, "manager payload %T", payload)
	}
}

func tunnelMessageToAgentProto(msg *TunnelMessage) (*tunnelpb.AgentMessage, error) {
	if msg == nil {
		return nil, errors.New("message is nil")
	}

	switch msg.Type {
	case MessageTypeResponse:
		status, err := intToInt32(msg.Status, "status")
		if err != nil {
			return nil, err
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HttpResponse{HttpResponse: &tunnelpb.HttpResponse{
			RequestId: msg.ID,
			Status:    status,
			Headers:   cloneHeaderMap(msg.Headers),
			Body:      msg.Body,
		}}}, nil
	case MessageTypeHeartbeat:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HeartbeatPing{HeartbeatPing: &tunnelpb.HeartbeatPing{Id: msg.ID}}}, nil
	case MessageTypeWebSocketData:
		messageType, err := intToInt32(msg.WSMessageType, "ws_message_type")
		if err != nil {
			return nil, err
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_WsData{WsData: &tunnelpb.WebSocketData{
			StreamId:    msg.ID,
			Data:        msg.Body,
			MessageType: messageType,
		}}}, nil
	case MessageTypeWebSocketClose:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_WsClose{WsClose: &tunnelpb.WebSocketClose{StreamId: msg.ID}}}, nil
	case MessageTypeStreamData:
		messageType, err := intToInt32(msg.WSMessageType, "ws_message_type")
		if err != nil {
			return nil, err
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamData{StreamData: &tunnelpb.StreamData{
			RequestId:   msg.ID,
			Data:        msg.Body,
			MessageType: messageType,
		}}}, nil
	case MessageTypeStreamEnd:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamEnd{StreamEnd: &tunnelpb.StreamEnd{RequestId: msg.ID}}}, nil
	case MessageTypeRegister:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{
			AgentToken:      msg.AgentToken,
			AgentInstanceId: msg.AgentInstance,
			Capabilities:    append([]string(nil), msg.Capabilities...),
			ResumeSessionId: msg.ResumeSession,
		}}}, nil
	case MessageTypeEvent:
		if msg.Event == nil {
			return nil, errors.Errorf("event payload is required for message type: %s", msg.Type)
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Event{Event: &tunnelpb.EventLog{
			Id:           msg.ID,
			Type:         msg.Event.Type,
			Severity:     msg.Event.Severity,
			Title:        msg.Event.Title,
			Description:  msg.Event.Description,
			ResourceType: msg.Event.ResourceType,
			ResourceId:   msg.Event.ResourceID,
			ResourceName: msg.Event.ResourceName,
			UserId:       msg.Event.UserID,
			Username:     msg.Event.Username,
			MetadataJson: append([]byte(nil), msg.Event.MetadataJSON...),
		}}}, nil
	case MessageTypeCommandAck:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: msg.ID,
		}}}, nil
	case MessageTypeCommandOutput:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandOutput{CommandOutput: &tunnelpb.CommandOutput{
			CommandId: msg.ID,
			Data:      msg.Body,
			Sequence:  msg.Sequence,
		}}}, nil
	case MessageTypeCommandComplete:
		status, err := intToInt32(msg.Status, "status")
		if err != nil {
			return nil, err
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: msg.ID,
			Status:    status,
			Headers:   cloneHeaderMap(msg.Headers),
			Body:      msg.Body,
			Error:     msg.Error,
			Streaming: msg.Streaming,
		}}}, nil
	case MessageTypeFileChunk:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_FileChunk{FileChunk: &tunnelpb.FileChunk{
			TransferId: msg.ID,
			Data:       msg.Body,
			Sequence:   msg.Sequence,
			Eof:        msg.EOF,
			Metadata:   cloneHeaderMap(msg.Metadata),
		}}}, nil
	case MessageTypeStreamClose:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamClose{StreamClose: &tunnelpb.StreamClose{
			StreamId: msg.ID,
			Error:    msg.Error,
		}}}, nil
	case MessageTypeCancelRequest:
		// Senders must check the manager advertised tunnelCapabilityProtoParity;
		// older managers decode this oneof case as an unknown payload.
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CancelRequest{CancelRequest: &tunnelpb.CancelRequest{
			CommandId: msg.ID,
		}}}, nil
	case MessageTypeRequest,
		MessageTypeHeartbeatAck,
		MessageTypeWebSocketStart,
		MessageTypeRegisterResponse,
		MessageTypeCommandRequest,
		MessageTypeStreamOpen:
		return nil, errors.Errorf("unsupported agent message type: %s", msg.Type)
	default:
		return nil, errors.Errorf("unsupported agent message type: %s", msg.Type)
	}
}

func agentProtoToTunnelMessage(msg *tunnelpb.AgentMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, errors.New("agent message is nil")
	}

	switch payload := msg.GetPayload().(type) {
	case *tunnelpb.AgentMessage_HttpResponse:
		return &TunnelMessage{
			ID:      payload.HttpResponse.GetRequestId(),
			Type:    MessageTypeResponse,
			Status:  int(payload.HttpResponse.GetStatus()),
			Headers: cloneHeaderMap(payload.HttpResponse.GetHeaders()),
			Body:    payload.HttpResponse.GetBody(),
		}, nil
	case *tunnelpb.AgentMessage_HeartbeatPing:
		return &TunnelMessage{ID: payload.HeartbeatPing.GetId(), Type: MessageTypeHeartbeat}, nil
	case *tunnelpb.AgentMessage_WsData:
		return &TunnelMessage{
			ID:            payload.WsData.GetStreamId(),
			Type:          MessageTypeWebSocketData,
			Body:          payload.WsData.GetData(),
			WSMessageType: int(payload.WsData.GetMessageType()),
		}, nil
	case *tunnelpb.AgentMessage_WsClose:
		return &TunnelMessage{ID: payload.WsClose.GetStreamId(), Type: MessageTypeWebSocketClose}, nil
	case *tunnelpb.AgentMessage_StreamData:
		return &TunnelMessage{
			ID:            payload.StreamData.GetRequestId(),
			Type:          MessageTypeStreamData,
			Body:          payload.StreamData.GetData(),
			WSMessageType: int(payload.StreamData.GetMessageType()),
		}, nil
	case *tunnelpb.AgentMessage_StreamEnd:
		return &TunnelMessage{ID: payload.StreamEnd.GetRequestId(), Type: MessageTypeStreamEnd}, nil
	case *tunnelpb.AgentMessage_Register:
		return &TunnelMessage{
			Type:          MessageTypeRegister,
			AgentToken:    payload.Register.GetAgentToken(),
			AgentInstance: payload.Register.GetAgentInstanceId(),
			Capabilities:  append([]string(nil), payload.Register.GetCapabilities()...),
			ResumeSession: payload.Register.GetResumeSessionId(),
		}, nil
	case *tunnelpb.AgentMessage_Event:
		return &TunnelMessage{
			ID:   payload.Event.GetId(),
			Type: MessageTypeEvent,
			Event: &TunnelEvent{
				Type:         payload.Event.GetType(),
				Severity:     payload.Event.GetSeverity(),
				Title:        payload.Event.GetTitle(),
				Description:  payload.Event.GetDescription(),
				ResourceType: payload.Event.GetResourceType(),
				ResourceID:   payload.Event.GetResourceId(),
				ResourceName: payload.Event.GetResourceName(),
				UserID:       payload.Event.GetUserId(),
				Username:     payload.Event.GetUsername(),
				MetadataJSON: append([]byte(nil), payload.Event.GetMetadataJson()...),
			},
		}, nil
	case *tunnelpb.AgentMessage_CommandAck:
		return &TunnelMessage{ID: payload.CommandAck.GetCommandId(), Type: MessageTypeCommandAck}, nil
	case *tunnelpb.AgentMessage_CommandOutput:
		return &TunnelMessage{
			ID:       payload.CommandOutput.GetCommandId(),
			Type:     MessageTypeCommandOutput,
			Body:     payload.CommandOutput.GetData(),
			Sequence: payload.CommandOutput.GetSequence(),
		}, nil
	case *tunnelpb.AgentMessage_CommandComplete:
		return &TunnelMessage{
			ID:        payload.CommandComplete.GetCommandId(),
			Type:      MessageTypeCommandComplete,
			Status:    int(payload.CommandComplete.GetStatus()),
			Headers:   cloneHeaderMap(payload.CommandComplete.GetHeaders()),
			Body:      payload.CommandComplete.GetBody(),
			Error:     payload.CommandComplete.GetError(),
			Streaming: payload.CommandComplete.GetStreaming(),
		}, nil
	case *tunnelpb.AgentMessage_FileChunk:
		return &TunnelMessage{
			ID:       payload.FileChunk.GetTransferId(),
			Type:     MessageTypeFileChunk,
			Body:     payload.FileChunk.GetData(),
			Sequence: payload.FileChunk.GetSequence(),
			EOF:      payload.FileChunk.GetEof(),
			Metadata: cloneHeaderMap(payload.FileChunk.GetMetadata()),
		}, nil
	case *tunnelpb.AgentMessage_StreamClose:
		return &TunnelMessage{
			ID:    payload.StreamClose.GetStreamId(),
			Type:  MessageTypeStreamClose,
			Error: payload.StreamClose.GetError(),
		}, nil
	case *tunnelpb.AgentMessage_CancelRequest:
		return &TunnelMessage{
			ID:   payload.CancelRequest.GetCommandId(),
			Type: MessageTypeCancelRequest,
		}, nil
	default:
		return nil, errors.WrapIff(errUnknownTunnelPayload, "agent payload %T", payload)
	}
}

func cloneHeaderMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func intToInt32(value int, field string) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, errors.Errorf("%s value %d is out of int32 range", field, value)
	}
	return int32(value), nil
}
