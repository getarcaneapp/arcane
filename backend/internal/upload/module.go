package upload

import (
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Module struct {
		service *UploadService
	}
)

func New(service *UploadService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *UploadService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterUploads(api, nil)
		return
	}
	RegisterUploads(api, m.service)
}

func RegisterUploads(api huma.API, service *UploadService) {
	h := &UploadHandler{uploadService: service}

	huma.Register(api, huma.Operation{
		OperationID: "create-upload-session",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/uploads/{kind}",
		Summary:     "Create an upload session",
		Description: "Start a chunked upload session; the file arrives as independently retryable chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
	}, h.CreateSession)

	huma.Register(api, huma.Operation{
		OperationID:     "upload-chunk",
		Method:          http.MethodPut,
		Path:            "/agents/{id}/uploads/{kind}/{uploadId}/chunks/{index}",
		Summary:         "Upload a chunk",
		Description:     "Upload one chunk of an upload session; re-sending a chunk is idempotent",
		Tags:            []string{"Uploads"},
		Security:        handlerutil.DefaultOperationSecurity(),
		Metadata:        map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
		MaxBodyBytes:    types.UploadMaxChunkSize,
		BodyReadTimeout: 10 * time.Minute,
	}, h.UploadChunk)

	huma.Register(api, huma.Operation{
		OperationID: "read-upload-chunk",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/uploads/{kind}/{uploadId}/chunks/{index}",
		Summary:     "Read a chunk",
		Description: "Download the bytes of one received chunk of an upload session",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
	}, h.ReadChunk)

	huma.Register(api, huma.Operation{
		OperationID: "download-upload-session",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/uploads/{kind}/{uploadId}/download",
		Summary:     "Download an upload session",
		Description: "Stream the file of a complete upload session; the session is deleted once the whole file was sent",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
	}, h.DownloadSession)

	huma.Register(api, huma.Operation{
		OperationID: "get-upload-session",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/uploads/{kind}/{uploadId}",
		Summary:     "Get an upload session",
		Description: "Inspect an upload session to resume by re-sending only the missing chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
	}, h.GetSession)

	huma.Register(api, huma.Operation{
		OperationID: "delete-upload-session",
		Method:      http.MethodDelete,
		Path:        "/agents/{id}/uploads/{kind}/{uploadId}",
		Summary:     "Delete an upload session",
		Description: "Abort an upload session and discard its received chunks",
		Tags:        []string{"Uploads"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.UploadKindConditions},
	}, h.DeleteSession)
}
