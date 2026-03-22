package edge

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type CommandRequest struct {
	ID            string
	Command       string
	Method        string
	Path          string
	Query         string
	Headers       map[string]string
	Body          []byte
	TimeoutMillis int64
}

type CommandResult struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

type CommandClient struct{}

func NewCommandClient() *CommandClient {
	return &CommandClient{}
}

func (c *CommandClient) Execute(ctx context.Context, tunnel *AgentTunnel, req *CommandRequest) (*CommandResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if err := validateConnectedTunnelInternal(tunnel); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("command request is required")
	}

	commandName := req.Command
	if commandName == "" {
		resolved, ok := ResolveEdgeCommandName(req.Method, req.Path, false)
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

	respCh, err := registerPendingRequestInternal(tunnel, requestID)
	if err != nil {
		return nil, err
	}
	defer tunnel.Pending.Delete(requestID)

	if err := tunnel.Conn.Send(msg); err != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", err)
	}

	status, headers, body, err := collectCommandResponseInternal(ctx, respCh)
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
		return fmt.Errorf("context is required")
	}
	if err := validateConnectedTunnelInternal(tunnel); err != nil {
		return err
	}
	if req == nil {
		return fmt.Errorf("command request is required")
	}
	if req.ID == "" {
		return fmt.Errorf("stream ID is required")
	}

	commandName := req.Command
	if commandName == "" {
		resolved, ok := ResolveEdgeCommandName(http.MethodGet, req.Path, true)
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
		return fmt.Errorf("edge tunnel is not connected")
	}
	return nil
}
