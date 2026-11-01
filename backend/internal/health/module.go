package health

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type (
	// Module owns the liveness and readiness routes.
	Module struct {
		service *Service
	}
)

// New builds the health module that registers the liveness and readiness routes.
func New(service *Service) *Module {
	return &Module{service: service}
}

// RegisterRoutes registers the liveness and readiness routes; readiness is hidden from the OpenAPI spec.
func (m *Module) RegisterRoutes(api huma.API) {
	handler := &healthHandler{service: m.service}

	huma.Register(api, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Check if the API is healthy",
		Tags:        []string{"Health"},
		Security:    []map[string][]string{},
	}, health[healthOutput](handler))

	huma.Register(api, huma.Operation{
		OperationID: "health-check-head",
		Method:      http.MethodHead,
		Path:        "/health",
		Summary:     "Health check (HEAD)",
		Description: "Check if the API is healthy (HEAD request)",
		Tags:        []string{"Health"},
		Security:    []map[string][]string{},
	}, health[headHealthOutput](handler))

	huma.Register(api, huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/health/ready",
		Summary:     "Readiness probe",
		Description: "Report whether the database and Docker daemon are reachable",
		Tags:        []string{"Health"},
		Security:    []map[string][]string{},
		Hidden:      true,
	}, handler.ready)
}
