package upload

import "github.com/danielgtaylor/huma/v2"

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
