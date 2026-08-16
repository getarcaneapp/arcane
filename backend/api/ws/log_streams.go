package ws

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"emperror.dev/errors"
	"github.com/labstack/echo/v5"

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
)

// ============================================================================
// Shared Log Stream Helpers
// ============================================================================

type wsLogStream struct {
	hub             *wshub.Hub
	cancel          context.CancelFunc
	firstSubscriber chan struct{}
	format          string
	key             string
	refs            int
	done            bool
	seq             atomic.Uint64
}

func buildLogStreamKeyInternal(envID, kind, resourceID, format string, batched, follow bool, tail, since string, timestamps bool) string {
	return strings.Join([]string{
		envID,
		kind,
		resourceID,
		format,
		strconv.FormatBool(batched),
		strconv.FormatBool(follow),
		tail,
		since,
		strconv.FormatBool(timestamps),
	}, "|")
}

func (h *WebSocketHandler) getOrCreateLogStreamInternal(key string, create func(onEmpty func(*wsLogStream)) *wsLogStream) *wsLogStream {
	h.logStreamsMu.Lock()
	defer h.logStreamsMu.Unlock()

	if stream, ok := h.logStreams[key]; ok {
		if !stream.done {
			stream.refs++
			return stream
		}
	}

	stream := create(func(stream *wsLogStream) {
		h.markLogStreamDoneInternal(key, stream)
	})
	stream.key = key
	stream.refs = 1
	h.logStreams[key] = stream
	return stream
}

func takeLogStreamCancelInternal(stream *wsLogStream) context.CancelFunc {
	cancel := stream.cancel
	stream.cancel = nil
	return cancel
}

func (h *WebSocketHandler) releaseLogStreamInternal(key string, stream *wsLogStream) {
	var cancel context.CancelFunc

	h.logStreamsMu.Lock()
	if stream.refs > 0 {
		stream.refs--
	}
	if stream.refs == 0 {
		if current, ok := h.logStreams[key]; ok && current == stream {
			delete(h.logStreams, key)
		}
		cancel = takeLogStreamCancelInternal(stream)
	}
	h.logStreamsMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (h *WebSocketHandler) markLogStreamDoneInternal(key string, stream *wsLogStream) {
	var cancel context.CancelFunc

	h.logStreamsMu.Lock()
	stream.done = true
	if stream.refs == 0 {
		if current, ok := h.logStreams[key]; ok && current == stream {
			delete(h.logStreams, key)
		}
		cancel = takeLogStreamCancelInternal(stream)
	}
	h.logStreamsMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// logStreamParams holds the standard query parameters shared by every WS log endpoint.
type logStreamParams struct {
	tail       string
	since      string
	format     string
	follow     bool
	timestamps bool
	batched    bool
}

func parseLogStreamParamsInternal(c *echo.Context) logStreamParams {
	return logStreamParams{
		follow:     cmp.Or(c.QueryParam("follow"), "true") == "true",
		tail:       cmp.Or(c.QueryParam("tail"), "100"),
		since:      c.QueryParam("since"),
		timestamps: c.QueryParam("timestamps") == "true",
		format:     cmp.Or(c.QueryParam("format"), "text"),
		batched:    c.QueryParam("batched") == "true",
	}
}

// serveLogStreamInternal is the shared scaffold for all three WS log endpoints (project, container, service).
// It performs upgrade, builds the stream key, gets-or-creates the multiplexing hub, registers metrics,
// and serves the client. The caller-supplied hubBuilder constructs the underlying *wsLogStream
// when no hub already exists for streamKey.
func (h *WebSocketHandler) serveLogStreamInternal(
	c *echo.Context,
	kind, resourceID string,
	params logStreamParams,
	hubBuilder func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream,
) {
	conn, unregister, ok := h.acceptWSInternal(c, kind, resourceID)
	if !ok {
		return
	}

	streamKey := buildLogStreamKeyInternal(c.Param("id"), kind, resourceID, params.format, params.batched, params.follow, params.tail, params.since, params.timestamps)
	stream := h.getOrCreateLogStreamInternal(streamKey, func(onEmpty func(*wsLogStream)) *wsLogStream {
		return hubBuilder(streamKey, onEmpty)
	})
	release := func() {
		unregister()
		h.releaseLogStreamInternal(streamKey, stream)
	}
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled via the hub's OnEmpty callback
	// which triggers when all clients disconnect.
	if !wshub.ServeClientWithOnRemove(context.Background(), stream.hub, conn, release) {
		// The stream refcount normally keeps this hub alive, so a stopped hub
		// here means it was torn down out from under us; drop our reference
		// rather than leaking the connection and the metrics entry.
		slog.Debug("log stream hub stopped before client registration", "streamKey", streamKey)
		_ = conn.CloseNow()
		release()
	}
}

// broadcastLogStreamErrorInternal emits an error message to every client of a log stream.
// resourceLabel is the human-readable noun used in slog/error text (e.g. "project log stream").
// errorPrefix is the user-facing message prefix (e.g. "Failed to stream project logs: ").
func broadcastLogStreamErrorInternal(resourceLabel, errorPrefix string, resourceID string, format string, err error, ls *wsLogStream) {
	slog.Warn(resourceLabel+" failed", "resourceID", resourceID, "error", err)

	if format == "json" {
		msg := wshub.LogMessage{
			Seq:       ls.seq.Add(1),
			Level:     "error",
			Message:   errorPrefix + err.Error(),
			Service:   "arcane",
			Timestamp: wshub.NowRFC3339(),
		}
		if b, marshalErr := json.Marshal(msg); marshalErr == nil {
			ls.hub.Broadcast(b)
		}
		return
	}

	ls.hub.Broadcast([]byte(errorPrefix + err.Error()))
}

// ============================================================================
// Project WebSocket/Streaming Endpoints
// ============================================================================

// ProjectLogs streams project logs over WebSocket.
//
//	@Summary		Get project logs via WebSocket
//	@Description	Stream project logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			projectId	path	string	true	"Project ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/projects/{projectId}/logs [get]
func (h *WebSocketHandler) ProjectLogs(c *echo.Context) error {
	projectID := c.Param("projectId")
	if strings.TrimSpace(projectID) == "" {
		return wsErrorJSONInternal(c, http.StatusBadRequest, "Project ID is required")
	}

	streamLogs := h.projectLogStreamer
	if streamLogs == nil {
		streamLogs = h.projectService.StreamProjectLogs
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindProjectLogs, projectID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			projectID,
			"project",
			params,
			streamLogs,
			normalizeProjectLogMessageInternal,
			normalizeProjectLogTextInternal,
			onEmpty,
		)
	})
	return nil
}

func newWSLogStreamInternal(key, format string) (*wsLogStream, context.Context) {
	ls := &wsLogStream{
		hub:             wshub.NewHub(1024),
		firstSubscriber: make(chan struct{}),
		format:          format,
		key:             key,
	}
	ls.hub.SetOnFirstClient(func() {
		close(ls.firstSubscriber)
	})

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is intentionally retained and invoked by the hub OnEmpty callback.
	ls.cancel = cancel

	go ls.hub.Run(ctx)

	return ls, ctx
}

func normalizeProjectLogMessageInternal(line string) wshub.LogMessage {
	level, service, message, timestamp := wshub.NormalizeProjectLine(line)
	return wshub.LogMessage{
		Level:     level,
		Message:   message,
		Service:   service,
		Timestamp: timestamp,
	}
}

func normalizeContainerLogMessageInternal(line string) wshub.LogMessage {
	level, message, timestamp := wshub.NormalizeContainerLine(line)
	return wshub.LogMessage{
		Level:     level,
		Message:   message,
		Timestamp: timestamp,
	}
}

func normalizeProjectLogTextInternal(line string) string {
	_, _, message, _ := wshub.NormalizeProjectLine(line)
	return message
}

func (h *WebSocketHandler) startLogHubInternal(
	key, resourceID, label string,
	params logStreamParams,
	stream func(context.Context, string, chan<- string, bool, string, string, bool) error,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
	onEmptyHook func(*wsLogStream),
) *wsLogStream {
	ls, ctx := newWSLogStreamInternal(key, params.format)

	ls.hub.SetOnEmpty(func() {
		if onEmptyHook != nil {
			onEmptyHook(ls)
		}
		slog.Debug("client disconnected, cleaning up "+label+" log hub", label+"ID", resourceID)
	})

	lines := h.startLogSourceInternal(ctx, key, resourceID, label, params, stream, ls)
	startLogForwardersInternal(ctx, ls, lines, params, normalizeJSON, normalizeText)

	return ls
}

func (h *WebSocketHandler) startLogSourceInternal(
	ctx context.Context,
	key, resourceID, label string,
	params logStreamParams,
	stream func(context.Context, string, chan<- string, bool, string, string, bool) error,
	ls *wsLogStream,
) <-chan string {
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		// The Docker log stream is only started once someone is listening.
		select {
		case <-ctx.Done():
			return
		case <-ls.firstSubscriber:
		}

		if err := stream(ctx, resourceID, lines, params.follow, params.tail, params.since, params.timestamps); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}

			h.markLogStreamDoneInternal(key, ls)
			broadcastLogStreamErrorInternal(label+" log stream", "Failed to stream "+label+" logs: ", resourceID, params.format, err, ls)
			return
		}

		if ctx.Err() == nil {
			h.markLogStreamDoneInternal(key, ls)
		}
	}()

	return lines
}

func startLogForwardersInternal(
	ctx context.Context,
	ls *wsLogStream,
	lines <-chan string,
	params logStreamParams,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
) {
	if params.format == "json" {
		messages := mapLogLinesInternal(ctx, lines, func(line string) wshub.LogMessage {
			message := normalizeJSON(line)
			message.Seq = ls.seq.Add(1)
			if message.Timestamp == "" {
				message.Timestamp = wshub.NowRFC3339()
			}
			return message
		})

		if params.batched {
			go wshub.ForwardLogJSONBatched(ctx, ls.hub, messages, 50, 400*time.Millisecond)
		} else {
			go wshub.ForwardLogJSON(ctx, ls.hub, messages)
		}

		return
	}

	textLines := lines
	if normalizeText != nil {
		textLines = mapLogLinesInternal(ctx, lines, normalizeText)
	}
	go wshub.ForwardLines(ctx, ls.hub, textLines)
}

func mapLogLinesInternal[T any](ctx context.Context, lines <-chan string, transform func(string) T) <-chan T {
	mapped := make(chan T, 256)
	go func() {
		defer close(mapped)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-lines:
				if !ok {
					return
				}

				select {
				case <-ctx.Done():
					return
				case mapped <- transform(line):
				}
			}
		}
	}()

	return mapped
}

// ============================================================================
// Container WebSocket Endpoints
// ============================================================================

// ContainerLogs streams container logs over WebSocket.
//
//	@Summary		Get container logs via WebSocket
//	@Description	Stream container logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/containers/{containerId}/logs [get]
func (h *WebSocketHandler) ContainerLogs(c *echo.Context) error {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		return wsErrorJSONInternal(c, http.StatusBadRequest, "Container ID is required")
	}

	streamLogs := h.containerLogStreamer
	if streamLogs == nil {
		streamLogs = h.containerService.StreamLogs
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindContainerLogs, containerID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			containerID,
			"container",
			params,
			streamLogs,
			normalizeContainerLogMessageInternal,
			nil,
			onEmpty,
		)
	})
	return nil
}

// ============================================================================
// Swarm Service WebSocket/Streaming Endpoints
// ============================================================================

// ServiceLogs streams swarm service logs over WebSocket.
//
//	@Summary		Get swarm service logs via WebSocket
//	@Description	Stream swarm service logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			serviceId	path	string	true	"Service ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/swarm/services/{serviceId}/logs [get]
func (h *WebSocketHandler) ServiceLogs(c *echo.Context) error {
	serviceID := c.Param("serviceId")
	if strings.TrimSpace(serviceID) == "" {
		return wsErrorJSONInternal(c, http.StatusBadRequest, "Service ID is required")
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindServiceLogs, serviceID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			serviceID,
			"service",
			params,
			h.swarmService.StreamServiceLogs,
			normalizeContainerLogMessageInternal,
			nil,
			onEmpty,
		)
	})
	return nil
}
