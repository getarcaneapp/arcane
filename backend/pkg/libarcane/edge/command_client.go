package edge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"time"
	"uuid"
)

const (
	tunnelCapabilityChunkedRequest = "chunked-request"
	// tunnelCapabilityProtoParity signals the peer can decode the full
	// TunnelMessage vocabulary natively over gRPC (stream_data/stream_end
	// manager->agent, cancel_request agent->manager) instead of the legacy
	// re-encoded forms.
	tunnelCapabilityProtoParity = "proto-parity-v1"
	// Credit-based flow control for command output keeps at most commandCreditWindow bytes
	// unconsumed by the manager. Agents offer it; only the manager sends the grant, which older
	// managers that echo agent capabilities cannot produce.
	tunnelCapabilityCommandCredit      = "command-credit-v1"
	tunnelCapabilityCommandCreditGrant = "command-credit-grant-v1"
	commandCreditWindow                = 8 << 20
	bodyTransferMetadataKey            = "body_transfer_id"
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
		transferID := uuid.New().String()
		msg.Body = nil
		msg.Metadata = map[string]string{bodyTransferMetadataKey: transferID}
	}

	if sendErr := tunnel.Conn.Send(msg); sendErr != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", sendErr)
	}
	if chunkRequestBody {
		transferID := msg.Metadata[bodyTransferMetadataKey]
		for sequence, offset := int64(0), 0; offset < len(req.Body); sequence++ {
			end := min(offset+defaultCommandChunkSize, len(req.Body))
			if sendErr2 := tunnel.Conn.Send(&TunnelMessage{
				ID:       transferID,
				Type:     MessageTypeFileChunk,
				Body:     req.Body[offset:end],
				Sequence: sequence,
				EOF:      end == len(req.Body),
			}); sendErr2 != nil {
				return nil, fmt.Errorf("tunnel request body transfer failed: %w", sendErr2)
			}
			offset = end
		}
	}

	// Output the collector has consumed is credited back, so the agent cannot outrun a slow reader.
	var credit func(int)
	if slices.Contains(tunnel.Capabilities, tunnelCapabilityCommandCredit) {
		credit = func(consumed int) {
			tunnel.grantCommandCredit(ctx, requestID, int64(consumed))
		}
	}

	status, headers, body, err := collectCommandResponseInternal(ctx, tunnel, pending, req.Method, req.Output, credit)
	if err != nil {
		if credit != nil {
			// Release an agent still waiting to send output that nobody will read.
			credit(math.MaxInt32)
		}
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

// grantCommandCredit queues credit for a command without blocking. A per-tunnel sender coalesces
// queued credits and sends them, so a stalled connection never holds up the response reader.
func (t *AgentTunnel) grantCommandCredit(ctx context.Context, commandID string, consumed int64) {
	t.creditOnce.Do(func() {
		t.credits = make(map[string]int64)
		t.creditSignal = make(chan struct{}, 1)
		logCtx := context.WithoutCancel(ctx)
		go func() {
			for {
				select {
				case <-t.done:
					return
				case <-t.creditSignal:
				}
				t.creditMu.Lock()
				queued := t.credits
				t.credits = make(map[string]int64)
				t.creditMu.Unlock()
				for id, credit := range queued {
					if err := t.Conn.Send(&TunnelMessage{ID: id, Type: MessageTypeCommandCredit, Credit: credit}); err != nil {
						slog.DebugContext(logCtx, "Failed to send edge command credit", "id", id, "error", err)
					}
				}
			}
		}()
	})

	t.creditMu.Lock()
	t.credits[commandID] += consumed
	t.creditMu.Unlock()
	select {
	case t.creditSignal <- struct{}{}:
	default:
	}
}

func validateConnectedTunnelInternal(tunnel *AgentTunnel) error {
	if tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return errors.New("edge tunnel is not connected")
	}
	return nil
}
