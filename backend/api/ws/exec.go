package ws

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"

	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
)

// ============================================================================
// Container Exec WebSocket Endpoint
// ============================================================================

// ContainerExec provides interactive terminal access to a container.
//
//	@Summary		Execute command in container via WebSocket
//	@Description	Interactive terminal access to a container over WebSocket
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			shell		query	string	false	"Shell to execute"	default(/bin/sh)
//	@Router			/api/environments/{id}/ws/containers/{containerId}/terminal [get]
func (h *WebSocketHandler) ContainerExec(c *echo.Context) error {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		return wsErrorJSONInternal(c, http.StatusBadRequest, "Container ID is required")
	}

	shell := cmp.Or(c.QueryParam("shell"), "/bin/sh")

	conn, unregister, ok := h.acceptWSInternal(c, systemtypes.WSKindContainerExec, containerID)
	if !ok {
		return nil
	}
	defer unregister()
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close container exec websocket connection", "containerID", containerID, "error", err)
		}
	}()

	// Allow large terminal pastes; coder/websocket's default limit is 32KB.
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// The pong is serviced by the concurrent stdin reader.
	go keepWSConnAliveInternal(ctx, cancel, conn, 54*time.Second)

	h.runContainerExecInternal(ctx, cancel, conn, containerID, shell)
	return nil
}

func (h *WebSocketHandler) runContainerExecInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, containerID, shell string) {
	// Create exec instance
	execID, err := h.containerService.CreateExec(ctx, containerID, []string{shell})
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error creating exec"))
		return
	}

	// Attach to exec
	execSession, err := h.containerService.AttachExec(ctx, containerID, execID)
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error attaching to exec"))
		return
	}
	// Cleanup must proceed even if the parent ctx is canceled, and must also
	// run on cancellation while the stdout pipe is blocked in a read — closing
	// the session is what unblocks it.
	cleanup := sync.OnceFunc(func() {
		slog.Debug("Cleaning up exec session", "execID", execID, "containerID", containerID, "contextErr", ctx.Err())
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := execSession.Close(cleanupCtx); err != nil { //nolint:contextcheck
			slog.Warn("Failed to clean up exec session", "execID", execID, "error", err)
		}
	})
	defer cleanup()
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	done := make(chan struct{})
	go h.pipeExecOutputInternal(ctx, conn, execSession.Stdout(), execID, containerID, done)
	go h.pipeExecInputInternal(ctx, cancel, conn, execSession.Stdin(), execID, containerID)

	<-done
}

func (h *WebSocketHandler) writeExecErrorInternal(ctx context.Context, conn *websocket.Conn, err error) {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, []byte(err.Error()+"\r\n"))
}

func (h *WebSocketHandler) pipeExecOutputInternal(ctx context.Context, conn *websocket.Conn, stdout io.Reader, execID, containerID string, done chan<- struct{}) {
	defer close(done)
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil {
			slog.Debug("Exec stdout read error", "execID", execID, "containerID", containerID, "error", err)
			return
		}
		if n > 0 {
			if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
				slog.Debug("Exec websocket write error", "execID", execID, "containerID", containerID, "error", err)
				return
			}
		}
	}
}

func (h *WebSocketHandler) pipeExecInputInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stdin io.Writer, execID, containerID string) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			slog.Debug("Exec websocket read error", "execID", execID, "containerID", containerID, "error", err)
			cancel()
			return
		}
		if _, err := stdin.Write(data); err != nil {
			slog.Debug("Exec stdin write error", "execID", execID, "containerID", containerID, "error", err)
			return
		}
	}
}
