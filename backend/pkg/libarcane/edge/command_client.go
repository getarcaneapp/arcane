package edge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	tunnelCapabilityChunkedRequest = "chunked-request"
	bodyTransferMetadataKey        = "body_transfer_id"
)

func NewCommandClient() *CommandClient {
	return &CommandClient{}
}

func (c *CommandClient) Execute(ctx context.Context, tunnel *AgentTunnel, req *CommandRequest) (*CommandResult, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if err := validateConnectedTunnelInternal(tunnel); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("command request is required")
	}

	commandName := req.Command
	if commandName == "" {
		resolved, ok := ResolveEdgeCommandName(req.Method, req.Path, false).Get()
		if !ok {
			return nil, fmt.Errorf("unsupported edge command for %s %s", req.Method, req.Path)
		}
		commandName = resolved
	}

	requestID := req.ID
	if requestID == "" {
		requestID = uuid.New().String()
	}

	timeoutMillis := req.TimeoutMillis
	if timeoutMillis <= 0 {
		timeoutMillis = int64(DefaultProxyTimeout / time.Millisecond)
	}

	msg := &TunnelMessage{
		ID:            requestID,
		Type:          MessageTypeCommandRequest,
		Command:       commandName,
		Method:        req.Method,
		Path:          req.Path,
		Query:         req.Query,
		Headers:       req.Headers,
		Body:          req.Body,
		TimeoutMillis: timeoutMillis,
		SessionID:     tunnel.SessionID,
		AgentInstance: tunnel.AgentInstance,
	}

	pending, err := registerPendingRequestInternal(tunnel, requestID)
	if err != nil {
		return nil, err
	}
	defer tunnel.Pending.Delete(requestID)

	chunkRequestBody := len(req.Body) > defaultCommandChunkSize && slices.Contains(tunnel.Capabilities, tunnelCapabilityChunkedRequest)
	if chunkRequestBody {
		transferID := uuid.NewString()
		msg.Body = nil
		msg.Metadata = map[string]string{bodyTransferMetadataKey: transferID}
	}

	if err := tunnel.Conn.Send(msg); err != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", err)
	}
	if chunkRequestBody {
		transferID := msg.Metadata[bodyTransferMetadataKey]
		for sequence, offset := int64(0), 0; offset < len(req.Body); sequence++ {
			end := min(offset+defaultCommandChunkSize, len(req.Body))
			if err := tunnel.Conn.Send(&TunnelMessage{
				ID:       transferID,
				Type:     MessageTypeFileChunk,
				Body:     req.Body[offset:end],
				Sequence: sequence,
				EOF:      end == len(req.Body),
			}); err != nil {
				return nil, fmt.Errorf("tunnel request body transfer failed: %w", err)
			}
			offset = end
		}
	}

	status, headers, body, err := collectCommandResponseInternal(ctx, tunnel, pending, req.Method)
	if err != nil {
		return nil, err
	}

	return &CommandResult{
		Status:  status,
		Headers: headers,
		Body:    body,
	}, nil
}

func (c *CommandClient) OpenStream(ctx context.Context, tunnel *AgentTunnel, req *CommandRequest) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := validateConnectedTunnelInternal(tunnel); err != nil {
		return err
	}
	if req == nil {
		return errors.New("command request is required")
	}
	if req.ID == "" {
		return errors.New("stream ID is required")
	}

	commandName := req.Command
	if commandName == "" {
		resolved, ok := ResolveEdgeCommandName(http.MethodGet, req.Path, true).Get()
		if !ok {
			return fmt.Errorf("unsupported edge stream target %q", req.Path)
		}
		commandName = resolved
	}

	msg := &TunnelMessage{
		ID:        req.ID,
		Type:      MessageTypeStreamOpen,
		Command:   commandName,
		Path:      req.Path,
		Query:     req.Query,
		Headers:   req.Headers,
		SessionID: tunnel.SessionID,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return tunnel.Conn.Send(msg)
	}
}

var DefaultCommandClient = NewCommandClient()

func validateConnectedTunnelInternal(tunnel *AgentTunnel) error {
	if tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return errors.New("edge tunnel is not connected")
	}
	return nil
}
