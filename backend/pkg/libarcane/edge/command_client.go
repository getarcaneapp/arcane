package edge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sync"
	"time"
	"uuid"

	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

const (
	tunnelCapabilityChunkedRequest = "chunked-request"
	// tunnelCapabilityProtoParity signals the peer decodes the full TunnelMessage vocabulary natively over gRPC.
	tunnelCapabilityProtoParity = "proto-parity-v1"
	// Command output credit keeps at most commandCreditWindow bytes unconsumed by the manager.
	// Agents offer it; only the manager sends the grant, which older echoing managers cannot produce.
	tunnelCapabilityCommandCredit      = "command-credit-v1"
	tunnelCapabilityCommandCreditGrant = "command-credit-grant-v1"
	commandCreditWindow                = 8 << 20
	bodyTransferMetadataKey            = "body_transfer_id"
)

// commandDuration is created once and shared by every CommandClient.
var commandDuration = sync.OnceValue(func() metric.Float64Histogram {
	histogram, err := otel.Meter(tracing.InstrumentationName).Float64Histogram("arcane.edge.command.duration",
		metric.WithDescription("Edge tunnel command round-trip duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300),
	)
	if err != nil {
		otel.Handle(err)
	}
	return histogram
})

func NewCommandClient() *CommandClient {
	return &CommandClient{}
}

func (c *CommandClient) Execute(ctx context.Context, tunnel *AgentTunnel, req *CommandRequest) (result *CommandResult, err error) {
	commandName, err := resolveCommand(ctx, tunnel, req, false)
	if err != nil {
		return nil, err
	}
	requestID := cmp.Or(req.ID, uuid.New().String())

	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "edge.command "+commandName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("arcane.edge.command", commandName), attribute.String("http.request.method", req.Method)),
	)
	started := time.Now()
	defer func() {
		tracing.End(span, err)
		var outcome string
		switch {
		case err == nil:
			outcome = "success"
		case errors.Is(err, context.Canceled):
			outcome = "canceled"
		case errors.Is(err, context.DeadlineExceeded):
			outcome = "timeout"
		default:
			outcome = "error"
		}
		attrs := metric.WithAttributes(attribute.String("arcane.edge.command", commandName), attribute.String("arcane.edge.outcome", outcome))
		commandDuration().Record(context.WithoutCancel(ctx), time.Since(started).Seconds(), attrs)
	}()

	// Replace any inbound trace headers so the agent's in-process request
	// continues this span.
	headers := req.Headers
	if span.SpanContext().IsValid() {
		carrier := make(propagation.MapCarrier, len(req.Headers)+2)
		for k, v := range req.Headers {
			if key := http.CanonicalHeaderKey(k); key != "Traceparent" && key != "Tracestate" {
				carrier[k] = v
			}
		}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		headers = carrier
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
		Headers:       headers,
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

	var transferID string
	if len(req.Body) > defaultCommandChunkSize && slices.Contains(tunnel.Capabilities, tunnelCapabilityChunkedRequest) {
		transferID = uuid.New().String()
		msg.Body = nil
		msg.Metadata = map[string]string{bodyTransferMetadataKey: transferID}
	}

	if sendErr := tunnel.Conn.Send(msg); sendErr != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", sendErr)
	}
	if transferID != "" {
		for sequence, offset := int64(0), 0; offset < len(req.Body); sequence++ {
			end := min(offset+defaultCommandChunkSize, len(req.Body))
			if sendErr := tunnel.Conn.Send(&TunnelMessage{
				ID:       transferID,
				Type:     MessageTypeFileChunk,
				Body:     req.Body[offset:end],
				Sequence: sequence,
				EOF:      end == len(req.Body),
			}); sendErr != nil {
				return nil, fmt.Errorf("tunnel request body transfer failed: %w", sendErr)
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
	commandName, err := resolveCommand(ctx, tunnel, req, true)
	if err != nil {
		return err
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
// queued credits, so a stalled connection never holds up the response reader.
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

// resolveCommand validates a command request and resolves its edge command name.
func resolveCommand(ctx context.Context, tunnel *AgentTunnel, req *CommandRequest, stream bool) (string, error) {
	switch {
	case ctx == nil:
		return "", errors.New("context is required")
	case !tunnel.connected():
		return "", errors.New("edge tunnel is not connected")
	case req == nil:
		return "", errors.New("command request is required")
	case stream && req.ID == "":
		return "", errors.New("stream ID is required")
	case req.Command != "":
		return req.Command, nil
	}
	if name, ok := ResolveEdgeCommandName(kit.Ternary(stream, http.MethodGet, req.Method), req.Path, stream).Get(); ok {
		return name, nil
	}
	if stream {
		return "", fmt.Errorf("unsupported edge stream target %q", req.Path)
	}
	return "", fmt.Errorf("unsupported edge command for %s %s", req.Method, req.Path)
}
