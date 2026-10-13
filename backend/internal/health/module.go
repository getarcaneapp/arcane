package health

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
)

// RegisterRoutes registers the health check routes.
func RegisterRoutes(api huma.API, actors *francis.Runtime) {
	handler := &healthHandler{actors: actors}

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
}
