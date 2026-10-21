package upload

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Module wires the upload domain and mounts its routes.
type Module struct {
	service *UploadService
}

// New builds the upload module around its session service.
func New(service *UploadService) *Module {
	return &Module{service: service}
}

// Service exposes the upload session service to the domain endpoints that
// consume completed sessions.
func (m *Module) Service() *UploadService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the upload-session endpoints. A nil module still
// registers, so OpenAPI spec generation can discover the routes without a
// service graph.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterUploads(api, nil)
		return
	}
	RegisterUploads(api, m.service)
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
		MaxBodyBytes: types.UploadMaxChunkSize,
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
