package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/system"
)

// HealthOutput is the response for health check
type HealthOutput struct {
	Body system.HealthResponse
}

// RegisterHealth registers health check routes using Huma.
func RegisterHealth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "health-check",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Check if the API is healthy",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, input *struct{}) (*HealthOutput, error) {
		return &HealthOutput{
			Body: system.HealthResponse{
				Status: "UP",
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "health-check-head",
		Method:      http.MethodHead,
		Path:        "/health",
		Summary:     "Health check (HEAD)",
		Description: "Check if the API is healthy (HEAD request)",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, input *struct{}) (*struct{}, error) {
		return nil, nil
	})
}
