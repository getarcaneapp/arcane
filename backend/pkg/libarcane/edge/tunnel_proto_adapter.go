package edge

import (
	"fmt"
	"maps"
	"math"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
)

func tunnelMessageToManagerProto(msg *TunnelMessage) (*tunnelpb.ManagerMessage, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
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
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_HeartbeatPong{HeartbeatPong: &tunnelpb.HeartbeatPong{}}}, nil
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
	case MessageTypeWebSocketClose:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_WsClose{WsClose: &tunnelpb.WebSocketClose{StreamId: msg.ID}}}, nil
	case MessageTypeRegisterResponse:
		return &tunnelpb.ManagerMessage{Payload: &tunnelpb.ManagerMessage_RegisterResponse{RegisterResponse: &tunnelpb.RegisterResponse{
			Accepted:      msg.Accepted,
			EnvironmentId: msg.EnvironmentID,
			Error:         msg.Error,
		}}}, nil
	case MessageTypeResponse, MessageTypeHeartbeat, MessageTypeStreamData, MessageTypeStreamEnd, MessageTypeRegister, MessageTypeEvent:
		return nil, fmt.Errorf("unsupported manager message type: %s", msg.Type)
	default:
		return nil, fmt.Errorf("unsupported manager message type: %s", msg.Type)
	}
}

func managerProtoToTunnelMessage(msg *tunnelpb.ManagerMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, fmt.Errorf("manager message is nil")
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
		return &TunnelMessage{Type: MessageTypeHeartbeatAck}, nil
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
			Type:          MessageTypeRegisterResponse,
			Accepted:      payload.RegisterResponse.GetAccepted(),
			EnvironmentID: payload.RegisterResponse.GetEnvironmentId(),
			Error:         payload.RegisterResponse.GetError(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported manager payload type %T", payload)
	}
}

func tunnelMessageToAgentProto(msg *TunnelMessage) (*tunnelpb.AgentMessage, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
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
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HeartbeatPing{HeartbeatPing: &tunnelpb.HeartbeatPing{}}}, nil
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
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamData{StreamData: &tunnelpb.StreamData{RequestId: msg.ID, Data: msg.Body}}}, nil
	case MessageTypeStreamEnd:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_StreamEnd{StreamEnd: &tunnelpb.StreamEnd{RequestId: msg.ID}}}, nil
	case MessageTypeRegister:
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: msg.AgentToken}}}, nil
	case MessageTypeEvent:
		if msg.Event == nil {
			return nil, fmt.Errorf("event payload is required for message type: %s", msg.Type)
		}
		return &tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Event{Event: &tunnelpb.EventLog{
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
	case MessageTypeRequest, MessageTypeHeartbeatAck, MessageTypeWebSocketStart, MessageTypeRegisterResponse:
		return nil, fmt.Errorf("unsupported agent message type: %s", msg.Type)
	default:
		return nil, fmt.Errorf("unsupported agent message type: %s", msg.Type)
	}
}

func agentProtoToTunnelMessage(msg *tunnelpb.AgentMessage) (*TunnelMessage, error) {
	if msg == nil {
		return nil, fmt.Errorf("agent message is nil")
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
		return &TunnelMessage{Type: MessageTypeHeartbeat}, nil
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
		return &TunnelMessage{ID: payload.StreamData.GetRequestId(), Type: MessageTypeStreamData, Body: payload.StreamData.GetData()}, nil
	case *tunnelpb.AgentMessage_StreamEnd:
		return &TunnelMessage{ID: payload.StreamEnd.GetRequestId(), Type: MessageTypeStreamEnd}, nil
	case *tunnelpb.AgentMessage_Register:
		return &TunnelMessage{Type: MessageTypeRegister, AgentToken: payload.Register.GetAgentToken()}, nil
	case *tunnelpb.AgentMessage_Event:
		return &TunnelMessage{
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
	default:
		return nil, fmt.Errorf("unsupported agent payload type %T", payload)
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
		return 0, fmt.Errorf("%s value %d is out of int32 range", field, value)
	}
	return int32(value), nil
}
