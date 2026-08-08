package health

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/system"
)

type healthHandler struct{}

type healthOutput struct {
	Body system.HealthResponse
}

// RegisterRoutes registers the health check routes.
func RegisterRoutes(api huma.API) {
	handler := &healthHandler{}

	huma.Register(api, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Check if the API is healthy",
		Tags:        []string{"Health"},
		Security:    []map[string][]string{},
	}, handler.getHealthInternal)

	huma.Register(api, huma.Operation{
		OperationID: "health-check-head",
		Method:      http.MethodHead,
		Path:        "/health",
		Summary:     "Health check (HEAD)",
		Description: "Check if the API is healthy (HEAD request)",
		Tags:        []string{"Health"},
		Security:    []map[string][]string{},
	}, handler.headHealthInternal)
}

func (h *healthHandler) getHealthInternal(context.Context, *struct{}) (*healthOutput, error) {
	return &healthOutput{
		Body: system.HealthResponse{
			Status: "UP",
		},
	}, nil
}

func (h *healthHandler) headHealthInternal(context.Context, *struct{}) (*struct{}, error) {
	return nil, nil
}
