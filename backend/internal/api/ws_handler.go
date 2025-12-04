package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	httputil "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	ws "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	projectService   *services.ProjectService
	containerService *services.ContainerService
	wsUpgrader       websocket.Upgrader
}

type wsLogStream struct {
	hub    *ws.Hub
	cancel context.CancelFunc
	format string
	seq    atomic.Uint64
}

// NewWebSocketHandler registers all WebSocket and streaming endpoints under /ws/*
func NewWebSocketHandler(
	group *gin.RouterGroup,
	projectService *services.ProjectService,
	containerService *services.ContainerService,
	authMiddleware *middleware.AuthMiddleware,
	cfg *config.Config,
) {
	handler := &WebSocketHandler{
		projectService:   projectService,
		containerService: containerService,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin:       httputil.ValidateWebSocketOrigin(cfg.AppUrl),
			ReadBufferSize:    32 * 1024,
			WriteBufferSize:   32 * 1024,
			EnableCompression: true,
		},
	}

	wsGroup := group.Group("/environments/:id/ws")
	wsGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		// Project endpoints
		wsGroup.GET("/projects/:projectId/logs", handler.ProjectLogs)
		wsGroup.POST("/projects/:projectId/pull", handler.ProjectPullImages)

		// Container endpoints
		wsGroup.GET("/containers/:containerId/logs", handler.ContainerLogs)
		wsGroup.GET("/containers/:containerId/stats", handler.ContainerStats)
		wsGroup.GET("/containers/:containerId/exec", handler.ContainerExec)
	}
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
func (h *WebSocketHandler) ProjectLogs(c *gin.Context) {
	projectID := c.Param("projectId")
	if strings.TrimSpace(projectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	follow := c.DefaultQuery("follow", "true") == "true"
	tail := c.DefaultQuery("tail", "100")
	since := c.Query("since")
	timestamps := c.DefaultQuery("timestamps", "false") == "true"
	format := c.DefaultQuery("format", "text")
	batched := c.DefaultQuery("batched", "false") == "true"

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startProjectLogHub(projectID, format, batched, follow, tail, since, timestamps)
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startProjectLogHub(projectID, format string, batched, follow bool, tail, since string, timestamps bool) *ws.Hub {
	ls := &wsLogStream{
		hub:    ws.NewHub(1024),
		format: format,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	ls.hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up project log hub", "projectID", projectID)
		cancel()
	})

	go ls.hub.Run(ctx)

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		_ = h.projectService.StreamProjectLogs(ctx, projectID, lines, follow, tail, since, timestamps)
	}()

	if format == "json" {
		msgs := make(chan ws.LogMessage, 256)
		go func() {
			defer close(msgs)
			for line := range lines {
				level, service, msg, ts := ws.NormalizeProjectLine(line)
				seq := ls.seq.Add(1)
				timestamp := ts
				if timestamp == "" {
					timestamp = ws.NowRFC3339()
				}
				msgs <- ws.LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Service:   service,
					Timestamp: timestamp,
				}
			}
		}()
		if batched {
			go ws.ForwardLogJSONBatched(ctx, ls.hub, msgs, 50, 400*time.Millisecond)
		} else {
			go ws.ForwardLogJSON(ctx, ls.hub, msgs)
		}
	} else {
		cleanChan := make(chan string, 256)
		go func() {
			defer close(cleanChan)
			for line := range lines {
				_, _, msg, _ := ws.NormalizeProjectLine(line)
				cleanChan <- msg
			}
		}()
		go ws.ForwardLines(ctx, ls.hub, cleanChan)
	}

	return ls.hub
}

// ProjectPullImages pulls project images with streaming output.
//
//	@Summary		Pull project images
//	@Description	Pull all images for a Docker Compose project with streaming output
//	@Tags			WebSocket
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Accept			json
//	@Produce		application/x-json-stream
//	@Param			id			path		string	true	"Environment ID"
//	@Param			projectId	path		string	true	"Project ID"
//	@Success		200			{string}	string	"Streaming JSON response"
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/ws/projects/{projectId}/pull [post]
func (h *WebSocketHandler) ProjectPullImages(c *gin.Context) {
	projectID := c.Param("projectId")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	c.Writer.Header().Set("Content-Type", "application/x-json-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	_, _ = c.Writer.WriteString(`{"status":"starting project image pull"}` + "\n")

	if err := h.projectService.PullProjectImages(c.Request.Context(), projectID, c.Writer, nil); err != nil {
		_, _ = c.Writer.WriteString(fmt.Sprintf(`{"error":%q}`+"\n", err.Error()))
		return
	}

	_, _ = c.Writer.WriteString(`{"status":"complete"}` + "\n")
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
func (h *WebSocketHandler) ContainerLogs(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Container ID is required"})
		return
	}

	follow := c.DefaultQuery("follow", "true") == "true"
	tail := c.DefaultQuery("tail", "100")
	since := c.Query("since")
	timestamps := c.DefaultQuery("timestamps", "false") == "true"
	format := c.DefaultQuery("format", "text")
	batched := c.DefaultQuery("batched", "false") == "true"

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startContainerLogHub(containerID, format, batched, follow, tail, since, timestamps)
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startContainerLogHub(containerID, format string, batched, follow bool, tail, since string, timestamps bool) *ws.Hub {
	ls := &wsLogStream{
		hub:    ws.NewHub(1024),
		format: format,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	ls.hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up container log hub", "containerID", containerID)
		cancel()
	})

	go ls.hub.Run(ctx)

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		_ = h.containerService.StreamLogs(ctx, containerID, lines, follow, tail, since, timestamps)
	}()

	if format == "json" {
		msgs := make(chan ws.LogMessage, 256)
		go func() {
			defer close(msgs)
			for line := range lines {
				level, msg, ts := ws.NormalizeContainerLine(line)
				seq := ls.seq.Add(1)
				timestamp := ts
				if timestamp == "" {
					timestamp = ws.NowRFC3339()
				}
				msgs <- ws.LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Timestamp: timestamp,
				}
			}
		}()
		if batched {
			go ws.ForwardLogJSONBatched(ctx, ls.hub, msgs, 50, 400*time.Millisecond)
		} else {
			go ws.ForwardLogJSON(ctx, ls.hub, msgs)
		}
	} else {
		go ws.ForwardLines(ctx, ls.hub, lines)
	}

	return ls.hub
}

// ContainerStats streams container stats over WebSocket.
//
//	@Summary		Get container stats via WebSocket
//	@Description	Stream container resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Router			/api/environments/{id}/ws/containers/{containerId}/stats [get]
func (h *WebSocketHandler) ContainerStats(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Container ID is required"})
		return
	}

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startContainerStatsHub(containerID)
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startContainerStatsHub(containerID string) *ws.Hub {
	hub := ws.NewHub(64)

	ctx, cancel := context.WithCancel(context.Background())

	hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up container stats hub", "containerID", containerID)
		cancel()
	})

	go hub.Run(ctx)

	statsChan := make(chan interface{}, 64)
	go func() {
		defer close(statsChan)
		_ = h.containerService.StreamStats(ctx, containerID, statsChan)
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case stats, ok := <-statsChan:
				if !ok {
					return
				}
				if b, err := json.Marshal(stats); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}()

	return hub
}

// ContainerExec provides interactive terminal access to a container.
//
//	@Summary		Execute command in container via WebSocket
//	@Description	Interactive terminal access to a container over WebSocket
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			cmd			query	string	false	"Command to execute"	default(/bin/sh)
//	@Router			/api/environments/{id}/ws/containers/{containerId}/exec [get]
func (h *WebSocketHandler) ContainerExec(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Container ID is required"})
		return
	}

	cmd := c.DefaultQuery("cmd", "/bin/sh")

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Create exec instance
	execID, err := h.containerService.CreateExec(ctx, containerID, []string{cmd})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to create exec: %v\r\n", err)))
		return
	}

	// Attach to exec
	stdin, stdout, err := h.containerService.AttachExec(ctx, execID)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to attach exec: %v\r\n", err)))
		return
	}
	defer stdin.Close()

	done := make(chan struct{})

	// Read from container, write to websocket
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
		}
	}()

	// Read from websocket, write to container
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			if _, err := stdin.Write(data); err != nil {
				return
			}
		}
	}()

	<-done
}
