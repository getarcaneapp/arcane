package upload

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"

	kit "kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	UploadHandler struct {
		uploadService *UploadService
	}

	CreateUploadSessionInput struct {
		EnvironmentID string `path:"id"`
		Kind          string `path:"kind" enum:"image,volume-backup,build-workspace,project-archive"`
		Body          types.CreateUploadSessionRequest
	}

	UploadChunkInput struct {
		EnvironmentID string `path:"id"`
		Kind          string `path:"kind" enum:"image,volume-backup,build-workspace,project-archive"`
		UploadID      string `path:"uploadId"`
		RawBody       []byte `contentType:"application/octet-stream"`
		Index         int    `path:"index"`
	}

	ReadChunkInput struct {
		EnvironmentID string `path:"id"`
		Kind          string `path:"kind" enum:"image,volume-backup,build-workspace,project-archive"`
		UploadID      string `path:"uploadId"`
		Index         int    `path:"index"`
	}

	ReadChunkOutput struct {
		ContentType string `header:"Content-Type"`
		Body        []byte
	}

	GetUploadSessionInput struct {
		EnvironmentID string `path:"id"`
		Kind          string `path:"kind" enum:"image,volume-backup,build-workspace,project-archive"`
		UploadID      string `path:"uploadId"`
	}

	DeleteUploadSessionInput struct {
		EnvironmentID string `path:"id"`
		Kind          string `path:"kind" enum:"image,volume-backup,build-workspace,project-archive"`
		UploadID      string `path:"uploadId"`
	}
)

func SessionHTTPError(err error) error {
	switch {
	case errors.Is(err, common.ErrUploadSessionNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrUploadSessionIncomplete):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrUploadKindMismatch):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, common.ErrUploadChunkInvalid), errors.Is(err, common.ErrUploadSessionInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return nil
}

func requireUploadPermission(ctx context.Context, kind, environmentID, target string) error {
	perms := authz.UploadKindConditions.Required("kind", kind)
	if len(perms) == 0 {
		return huma.Error403Forbidden("permission denied: unknown upload kind " + kind)
	}
	if kind == types.UploadKindVolumeBackup && target != "" {
		return middleware.RequireResourcePermissions(ctx, environmentID, authz.ResourceTypeVolume, target, perms)
	}
	return middleware.RequirePermissions(ctx, environmentID, perms)
}

func (h *UploadHandler) requireSessionPermission(ctx context.Context, kind, environmentID, uploadID string) error {
	session, err := h.uploadService.GetSession(ctx, kind, uploadID)
	if err != nil {
		return requireUploadPermission(ctx, kind, environmentID, "")
	}
	return requireUploadPermission(ctx, kind, environmentID, session.Target)
}

func sessionResponse(session *types.UploadSession) *handlerutil.Out[types.UploadSession] {
	return &handlerutil.Out[types.UploadSession]{
		Body: types.ApiResponse[types.UploadSession]{
			Success: true,
			Data:    *session,
		},
	}
}

func (h *UploadHandler) CreateSession(ctx context.Context, input *CreateUploadSessionInput) (*handlerutil.Out[types.UploadSession], error) {
	if err := requireUploadPermission(ctx, input.Kind, input.EnvironmentID, strings.TrimSpace(input.Body.Target)); err != nil {
		return nil, err
	}
	session, err := h.uploadService.CreateSession(ctx, input.Kind, input.Body)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponse(session), nil
}

func (h *UploadHandler) UploadChunk(ctx context.Context, input *UploadChunkInput) (*handlerutil.Out[types.UploadSession], error) {
	if err := h.requireSessionPermission(ctx, input.Kind, input.EnvironmentID, input.UploadID); err != nil {
		return nil, err
	}
	session, err := h.uploadService.WriteChunk(ctx, input.Kind, input.UploadID, input.Index, input.RawBody)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponse(session), nil
}

func (h *UploadHandler) ReadChunk(ctx context.Context, input *ReadChunkInput) (*ReadChunkOutput, error) {
	if err := h.requireSessionPermission(ctx, input.Kind, input.EnvironmentID, input.UploadID); err != nil {
		return nil, err
	}
	data, err := h.uploadService.ReadChunk(ctx, input.Kind, input.UploadID, input.Index)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &ReadChunkOutput{
		ContentType: "application/octet-stream",
		Body:        data,
	}, nil
}

func (h *UploadHandler) GetSession(ctx context.Context, input *GetUploadSessionInput) (*handlerutil.Out[types.UploadSession], error) {
	if err := h.requireSessionPermission(ctx, input.Kind, input.EnvironmentID, input.UploadID); err != nil {
		return nil, err
	}
	session, err := h.uploadService.GetSession(ctx, input.Kind, input.UploadID)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponse(session), nil
}

func (h *UploadHandler) DownloadSession(ctx context.Context, input *GetUploadSessionInput) (*huma.StreamResponse, error) {
	if input.Kind != types.UploadKindProjectArchive {
		return nil, huma.Error403Forbidden("permission denied: " + input.Kind + " uploads cannot be downloaded")
	}
	source, err := h.uploadService.GetSession(ctx, input.Kind, input.UploadID)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if permErr := middleware.RequireResourcePermissions(ctx, input.EnvironmentID, authz.ResourceTypeProject, source.Target, []string{authz.PermProjectsExport}); permErr != nil {
		return nil, permErr
	}
	reader, session, err := h.uploadService.DownloadSession(ctx, input.Kind, input.UploadID)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.DownloadResponse(reader, session.Size, session.Filename), nil
}

func (h *UploadHandler) DeleteSession(ctx context.Context, input *DeleteUploadSessionInput) (*handlerutil.Out[types.MessageResponse], error) {
	session, _ := h.uploadService.GetSession(ctx, input.Kind, input.UploadID)
	if input.Kind != types.UploadKindProjectArchive || middleware.RequireResourcePermissions(
		ctx, input.EnvironmentID, authz.ResourceTypeProject, kit.FromPtr(session).Target, []string{authz.PermProjectsExport},
	) != nil {
		if err := h.requireSessionPermission(ctx, input.Kind, input.EnvironmentID, input.UploadID); err != nil {
			return nil, err
		}
	}
	if err := h.uploadService.DeleteSession(ctx, input.Kind, input.UploadID); err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.MessageOutput("Upload session deleted", ""), nil
}

func LegacyMultipartMiddleware(api huma.API, service *UploadService, kind string) huma.Middlewares {
	return huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		if !strings.HasPrefix(ctx.Header("Content-Type"), "multipart/form-data") {
			next(ctx)
			return
		}
		writeErr := func(status int, message string) {
			if err := huma.WriteErr(api, ctx, status, message); err != nil {
				slog.WarnContext(ctx.Context(), "failed to write legacy upload error response", "error", err)
			}
		}
		if err := requireUploadPermission(ctx.Context(), kind, authz.EnvIDFromPath(ctx.URL().Path), ctx.Param("volumeName")); err != nil {
			writeErr(http.StatusForbidden, "permission denied")
			return
		}

		request := humaecho.Unwrap(ctx).Request()
		// Streamed, never spooled to disk: the size limit applies while the file part is written.
		parts, err := request.MultipartReader()
		if err != nil {
			writeErr(http.StatusBadRequest, "Invalid multipart form: "+err.Error())
			return
		}
		file, err := parts.NextPart()
		for err == nil && file.FormName() != "file" {
			file, err = parts.NextPart()
		}
		if err != nil {
			writeErr(http.StatusBadRequest, "No file uploaded")
			return
		}

		slog.WarnContext(ctx.Context(), "Deprecated multipart upload used; migrate to chunked upload sessions",
			"kind", kind, "path", ctx.URL().Path, "filename", file.FileName())

		session, err := service.IngestStream(ctx.Context(), kind, file.FileName(), file)
		if err != nil {
			if httpErr := SessionHTTPError(err); httpErr != nil {
				if statusErr, ok := errors.AsType[huma.StatusError](httpErr); ok {
					writeErr(statusErr.GetStatus(), statusErr.Error())
					return
				}
			}
			writeErr(http.StatusInternalServerError, err.Error())
			return
		}

		payload, err := json.Marshal(types.UploadConsumeRequest{UploadID: session.ID})
		if err != nil {
			writeErr(http.StatusInternalServerError, err.Error())
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.ContentLength = int64(len(payload))
		request.Header.Set("Content-Type", "application/json")
		next(ctx)
	}}
}
