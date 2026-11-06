package health

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type (
	Module struct {
		service *Service
	}
)

func New(service *Service) *Module {
	return &Module{service: service}
}

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
	}, handler.ready)
}
