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
	"github.com/getarcaneapp/arcane/types/v2/base"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// UploadHandler provides the Huma-based chunked upload-session endpoints.
type UploadHandler struct {
	uploadService *UploadService
}

// --- Huma Input/Output Wrappers ---

type CreateUploadSessionInput struct {
	EnvironmentID string `path:"id"`
	Kind          string `path:"kind" enum:"image,volume-backup,build-workspace"`
	Body          uploadtypes.CreateSessionRequest
}

type UploadChunkInput struct {
	EnvironmentID string `path:"id"`
	Kind          string `path:"kind" enum:"image,volume-backup,build-workspace"`
	UploadID      string `path:"uploadId"`
	Index         int    `path:"index"`
	RawBody       []byte `contentType:"application/octet-stream"`
}

type GetUploadSessionInput struct {
	EnvironmentID string `path:"id"`
	Kind          string `path:"kind" enum:"image,volume-backup,build-workspace"`
	UploadID      string `path:"uploadId"`
}

type DeleteUploadSessionInput struct {
	EnvironmentID string `path:"id"`
	Kind          string `path:"kind" enum:"image,volume-backup,build-workspace"`
	UploadID      string `path:"uploadId"`
}

// SessionHTTPError maps upload-session errors to HTTP errors for this package
// and the domain endpoints that consume sessions. It returns nil for errors
// that are not upload-session errors.
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

// requireUploadPermissionInternal enforces the kind-derived permission for the
// target environment. Registration cannot use middleware.RegisterWithPermission
// because the required permission depends on the {kind} path parameter; the
// manager-side proxy applies the same mapping for remote environments.
func requireUploadPermissionInternal(ctx context.Context, kind, environmentID string) error {
	perm, known := authz.UploadKindPermission(kind)
	if !known {
		return huma.Error403Forbidden("permission denied: unknown upload kind " + kind)
	}
	ps, _ := middleware.PermissionsFromContext(ctx)
	if !ps.Allows(perm, environmentID) {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return nil
}

func sessionResponseInternal(session *uploadtypes.Session) *handlerutil.Out[uploadtypes.Session] {
	return &handlerutil.Out[uploadtypes.Session]{
		Body: base.ApiResponse[uploadtypes.Session]{
			Success: true,
			Data:    *session,
		},
	}
}

func (h *UploadHandler) CreateSession(ctx context.Context, input *CreateUploadSessionInput) (*handlerutil.Out[uploadtypes.Session], error) {
	if err := requireUploadPermissionInternal(ctx, input.Kind, input.EnvironmentID); err != nil {
		return nil, err
	}
	session, err := h.uploadService.CreateSession(ctx, input.Kind, input.Body)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponseInternal(session), nil
}

func (h *UploadHandler) UploadChunk(ctx context.Context, input *UploadChunkInput) (*handlerutil.Out[uploadtypes.Session], error) {
	if err := requireUploadPermissionInternal(ctx, input.Kind, input.EnvironmentID); err != nil {
		return nil, err
	}
	session, err := h.uploadService.WriteChunk(ctx, input.Kind, input.UploadID, input.Index, input.RawBody)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponseInternal(session), nil
}

func (h *UploadHandler) GetSession(ctx context.Context, input *GetUploadSessionInput) (*handlerutil.Out[uploadtypes.Session], error) {
	if err := requireUploadPermissionInternal(ctx, input.Kind, input.EnvironmentID); err != nil {
		return nil, err
	}
	session, err := h.uploadService.GetSession(ctx, input.Kind, input.UploadID)
	if err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return sessionResponseInternal(session), nil
}

func (h *UploadHandler) DeleteSession(ctx context.Context, input *DeleteUploadSessionInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := requireUploadPermissionInternal(ctx, input.Kind, input.EnvironmentID); err != nil {
		return nil, err
	}
	if err := h.uploadService.DeleteSession(ctx, input.Kind, input.UploadID); err != nil {
		if httpErr := SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Upload session deleted"},
		},
	}, nil
}

// LegacyMultipartMiddleware converts a deprecated multipart/form-data request
// into a complete upload session and rewrites the request into the JSON
// {"uploadId"} form, so clients predating chunked uploads keep working. It
// runs after authentication (a huma global middleware) but before the
// operation's permission middleware, so it enforces the kind permission
// itself to avoid staging files for unauthorized callers.
//
// This compatibility path is deprecated for API consumers; delete this
// middleware when multipart upload support is removed.
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
		if err := requireUploadPermissionInternal(ctx.Context(), kind, authz.EnvIDFromPath(ctx.URL().Path)); err != nil {
			writeErr(http.StatusForbidden, "permission denied")
			return
		}

		request := humaecho.Unwrap(ctx).Request()
		if err := request.ParseMultipartForm(32 << 10); err != nil {
			writeErr(http.StatusBadRequest, "Invalid multipart form: "+err.Error())
			return
		}
		defer func() { _ = request.MultipartForm.RemoveAll() }()
		file, header, err := request.FormFile("file")
		if err != nil {
			writeErr(http.StatusBadRequest, "No file uploaded")
			return
		}
		defer func() { _ = file.Close() }()

		slog.WarnContext(ctx.Context(), "Deprecated multipart upload used; migrate to chunked upload sessions",
			"kind", kind, "path", ctx.URL().Path, "filename", header.Filename, "size", header.Size)

		session, err := service.IngestSession(ctx.Context(), kind, header.Filename, header.Size, file)
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

		payload, err := json.Marshal(uploadtypes.ConsumeRequest{UploadID: session.ID})
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

// RegisterUploads registers the upload-session routes. The required permission
// depends on the {kind} path parameter, so the operations carry no static
// permission metadata; enforcement happens in-handler and, for remote
// environments, in the proxy's upload special case.
func RegisterUploads(api huma.API, service *UploadService) {
	h := &UploadHandler{uploadService: service}

	huma.Register(api, huma.Operation{
		OperationID: "create-upload-session",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/uploads/{kind}",
		Summary:     "Create an upload session",
		Description: "Start a chunked upload session; the file arrives as independently retryable chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.CreateSession)

	huma.Register(api, huma.Operation{
		OperationID:  "upload-chunk",
		Method:       http.MethodPut,
		Path:         "/environments/{id}/uploads/{kind}/{uploadId}/chunks/{index}",
		Summary:      "Upload a chunk",
		Description:  "Upload one chunk of an upload session; re-sending a chunk is idempotent",
		Tags:         []string{"Uploads"},
		Security:     handlerutil.DefaultOperationSecurity(),
		MaxBodyBytes: uploadtypes.MaxChunkSize,
	}, h.UploadChunk)

	huma.Register(api, huma.Operation{
		OperationID: "get-upload-session",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/uploads/{kind}/{uploadId}",
		Summary:     "Get an upload session",
		Description: "Inspect an upload session to resume by re-sending only the missing chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.GetSession)

	huma.Register(api, huma.Operation{
		OperationID: "delete-upload-session",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/uploads/{kind}/{uploadId}",
		Summary:     "Delete an upload session",
		Description: "Abort an upload session and discard its received chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, h.DeleteSession)
}
