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

	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
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
	defer func() { _ = conn.CloseNow() }()

	// Allow large terminal pastes; coder/websocket's default limit is 32KB.
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancelCause(c.Request().Context())
	defer cancel(nil)

	// The pong is serviced by the concurrent stdin reader.
	go keepWSConnAliveInternal(ctx, func() { cancel(errors.New("websocket ping failed")) }, conn, 54*time.Second)

	h.runContainerExecInternal(ctx, cancel, conn, containerID, shell)
	return nil
}

func (h *WebSocketHandler) runContainerExecInternal(ctx context.Context, cancel context.CancelCauseFunc, conn *websocket.Conn, containerID, shell string) {
	started := time.Now()

	execID, err := h.containerService.CreateExec(ctx, containerID, []string{shell})
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error creating exec"))
		closeExecInternal(ctx, conn, containerID, "", started, errors.WithMessage(err, "create exec"))
		return
	}

	execSession, err := h.containerService.AttachExec(ctx, containerID, execID)
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error attaching to exec"))
		closeExecInternal(ctx, conn, containerID, execID, started, errors.WithMessage(err, "attach exec"))
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

	done := make(chan error, 1)
	go h.pipeExecOutputInternal(ctx, conn, execSession.Stdout(), done)
	go h.pipeExecInputInternal(ctx, cancel, conn, execSession.Stdin())

	cause := <-done
	if ctxCause := context.Cause(ctx); ctxCause != nil {
		cause = ctxCause
	}
	closeExecInternal(ctx, conn, containerID, execID, started, cause)
}

func (h *WebSocketHandler) writeExecErrorInternal(ctx context.Context, conn *websocket.Conn, err error) {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, []byte(err.Error()+"\r\n"))
}

// closeExecInternal completes the close handshake with a status and reason
// derived from the terminating cause, so the browser can show why the
// session ended, and logs the outcome with the session duration.
func closeExecInternal(ctx context.Context, conn *websocket.Conn, containerID, execID string, started time.Time, cause error) {
	status, reason, level := websocket.StatusNormalClosure, "shell exited", slog.LevelInfo
	switch {
	case cause == nil:
	case wshub.IsExpectedClose(cause):
		reason, level = "", slog.LevelDebug
	case errors.Is(cause, context.Canceled):
		status, reason = websocket.StatusGoingAway, "server shutting down"
	default:
		status, reason, level = websocket.StatusInternalError, cause.Error(), slog.LevelWarn
	}
	if len(reason) > 123 {
		reason = reason[:123]
	}
	closeErr := conn.Close(status, reason)
	slog.Log(ctx, level, "Container exec session ended",
		"containerID", containerID,
		"execID", execID,
		"status", int(status),
		"reason", reason,
		"cause", cause,
		"duration", time.Since(started),
		"closeError", closeErr,
	)
}

func (h *WebSocketHandler) pipeExecOutputInternal(ctx context.Context, conn *websocket.Conn, stdout io.Reader, done chan<- error) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			done <- context.Cause(ctx)
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				done <- nil
			} else {
				done <- errors.WithMessage(err, "exec output read")
			}
			return
		}
		if n > 0 {
			if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
				done <- errors.WithMessage(err, "websocket write")
				return
			}
		}
	}
}

func (h *WebSocketHandler) pipeExecInputInternal(ctx context.Context, cancel context.CancelCauseFunc, conn *websocket.Conn, stdin io.Writer) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			cancel(errors.WithMessage(err, "websocket read"))
			return
		}
		if _, err := stdin.Write(data); err != nil {
			cancel(errors.WithMessage(err, "exec input write"))
			return
		}
	}
}
