package ws

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"

	wshub "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ws"
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

func (h *WebSocketHandler) getOrCreateLogStream(key string, create func(onEmpty func(*wsLogStream)) *wsLogStream) *wsLogStream {
	h.logStreamsMu.Lock()
	defer h.logStreamsMu.Unlock()

	if stream, ok := h.logStreams[key]; ok {
		if !stream.done {
			stream.refs++
			return stream
		}
	}

	stream := create(func(stream *wsLogStream) {
		h.releaseLogStream(key, stream, true)
	})
	stream.key = key
	stream.refs = 1
	h.logStreams[key] = stream
	return stream
}

// releaseLogStream drops a client reference, or marks the source finished when done is set,
// and tears the stream down once no clients remain.
func (h *WebSocketHandler) releaseLogStream(key string, stream *wsLogStream, done bool) {
	var cancel context.CancelFunc

	h.logStreamsMu.Lock()
	if done {
		stream.done = true
	} else {
		stream.refs = max(stream.refs-1, 0)
	}
	if stream.refs == 0 {
		if current, ok := h.logStreams[key]; ok && current == stream {
			delete(h.logStreams, key)
		}
		cancel, stream.cancel = stream.cancel, nil
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

func parseLogStreamParams(c *echo.Context) logStreamParams {
	return logStreamParams{
		follow:     cmp.Or(c.QueryParam("follow"), "true") == "true",
		tail:       cmp.Or(c.QueryParam("tail"), "100"),
		since:      c.QueryParam("since"),
		timestamps: c.QueryParam("timestamps") == "true",
		format:     cmp.Or(c.QueryParam("format"), "text"),
		batched:    c.QueryParam("batched") == "true",
	}
}

// serveLogStream is the shared scaffold for all three WS log endpoints (project, container, service).
// It performs upgrade, builds the stream key, gets-or-creates the multiplexing hub, registers metrics,
// and serves the client. The caller-supplied hubBuilder constructs the underlying *wsLogStream
// when no hub already exists for streamKey.
func (h *WebSocketHandler) serveLogStream(
	c *echo.Context,
	kind, resourceID string,
	params logStreamParams,
	hubBuilder func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream,
) {
	conn, unregister, ok := h.acceptWS(c, kind, resourceID)
	if !ok {
		return
	}

	streamKey := strings.Join([]string{
		c.Param("id"),
		kind,
		resourceID,
		params.format,
		strconv.FormatBool(params.batched),
		strconv.FormatBool(params.follow),
		params.tail,
		params.since,
		strconv.FormatBool(params.timestamps),
	}, "|")
	stream := h.getOrCreateLogStream(streamKey, func(onEmpty func(*wsLogStream)) *wsLogStream {
		return hubBuilder(streamKey, onEmpty)
	})
	release := func() {
		unregister()
		h.releaseLogStream(streamKey, stream, false)
	}
	// The hub owns connection cleanup, so it outlives the request and ends with the app.
	if !wshub.ServeClientWithOnRemove(h.ctx, stream.hub, conn, release) {
		// The stream refcount normally keeps this hub alive, so a stopped hub
		// here means it was torn down out from under us; drop our reference
		// rather than leaking the connection and the metrics entry.
		slog.DebugContext(c.Request().Context(), "log stream hub stopped before client registration", "streamKey", streamKey)
		_ = conn.CloseNow()
		release()
	}
}

// broadcastLogStreamError emits an error message to every client of a log stream.
// label is the human-readable resource noun (e.g. "project").
func broadcastLogStreamError(ctx context.Context, label, resourceID, format string, err error, ls *wsLogStream) {
	slog.WarnContext(ctx, "log stream failed", "kind", label, "resourceId", resourceID, "error", err)
	errorPrefix := "Failed to stream " + label + " logs: "

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
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Project ID is required"})
	}

	streamLogs := h.projectLogStreamer
	if streamLogs == nil {
		streamLogs = h.projectService.StreamProjectLogs
	}

	params := parseLogStreamParams(c)
	h.serveLogStream(c, types.WSKindProjectLogs, projectID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHub(
			streamKey,
			projectID,
			"project",
			params,
			streamLogs,
			func(line string) wshub.LogMessage {
				level, service, message, timestamp := wshub.NormalizeProjectLine(line)
				return wshub.LogMessage{
					Level:     level,
					Message:   message,
					Service:   service,
					Timestamp: timestamp,
				}
			},
			func(line string) string {
				_, _, message, _ := wshub.NormalizeProjectLine(line)
				return message
			},
			onEmpty,
		)
	})
	return nil
}

func normalizeContainerLogMessage(line string) wshub.LogMessage {
	level, message, timestamp := wshub.NormalizeContainerLine(line)
	return wshub.LogMessage{
		Level:     level,
		Message:   message,
		Timestamp: timestamp,
	}
}

func (h *WebSocketHandler) startLogHub(
	key, resourceID, label string,
	params logStreamParams,
	stream func(context.Context, string, chan<- string, bool, string, string, bool) error,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
	onEmptyHook func(*wsLogStream),
) *wsLogStream {
	ctx, cancel := context.WithCancel(h.ctx) //nolint:gosec // Cancel is stored on the stream and runs on idle teardown.
	ls := &wsLogStream{
		hub:             wshub.NewHub(1024),
		firstSubscriber: make(chan struct{}),
		format:          params.format,
		key:             key,
		cancel:          cancel,
	}
	ls.hub.SetOnFirstClient(func() {
		close(ls.firstSubscriber)
	})
	go ls.hub.Run(ctx)

	ls.hub.SetOnEmpty(func() {
		if onEmptyHook != nil {
			onEmptyHook(ls)
		}
		slog.DebugContext(ctx, "client disconnected, cleaning up log hub", "kind", label, "resourceId", resourceID)
	})

	lines := h.startLogSource(ctx, cancel, key, resourceID, label, params, stream, ls)
	startLogForwarders(ctx, ls, lines, params, normalizeJSON, normalizeText)

	return ls
}

func (h *WebSocketHandler) startLogSource(
	ctx context.Context,
	cancel context.CancelFunc,
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

			h.releaseLogStream(key, ls, true)
			broadcastLogStreamError(ctx, label, resourceID, params.format, err, ls)
			time.AfterFunc(streamEndDrainGrace, cancel)
			return
		}

		if ctx.Err() == nil {
			h.releaseLogStream(key, ls, true)
			time.AfterFunc(streamEndDrainGrace, cancel)
		}
	}()

	return lines
}

func startLogForwarders(
	ctx context.Context,
	ls *wsLogStream,
	lines <-chan string,
	params logStreamParams,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
) {
	if params.format == "json" {
		messages := mapLogLines(ctx, lines, func(line string) wshub.LogMessage {
			message := normalizeJSON(line)
			return wshub.LogMessage{
				Seq:         ls.seq.Add(1),
				Level:       message.Level,
				Message:     message.Message,
				Timestamp:   cmp.Or(message.Timestamp, wshub.NowRFC3339()),
				Service:     message.Service,
				ContainerID: message.ContainerID,
			}
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
		textLines = mapLogLines(ctx, lines, normalizeText)
	}
	go wshub.ForwardLines(ctx, ls.hub, textLines)
}

func mapLogLines[T any](ctx context.Context, lines <-chan string, transform func(string) T) <-chan T {
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
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Container ID is required"})
	}

	streamLogs := h.containerLogStreamer
	if streamLogs == nil {
		streamLogs = h.containerService.StreamLogs
	}

	params := parseLogStreamParams(c)
	h.serveLogStream(c, types.WSKindContainerLogs, containerID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHub(
			streamKey,
			containerID,
			"container",
			params,
			streamLogs,
			normalizeContainerLogMessage,
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
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Service ID is required"})
	}

	params := parseLogStreamParams(c)
	h.serveLogStream(c, types.WSKindServiceLogs, serviceID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHub(
			streamKey,
			serviceID,
			"service",
			params,
			h.swarmService.StreamServiceLogs,
			normalizeContainerLogMessage,
			nil,
			onEmpty,
		)
	})
	return nil
}
