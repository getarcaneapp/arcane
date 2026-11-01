package attestations

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

// RegisterRoutes registers the image attestation endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-attestations",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{name}/attestations",
		Summary:     "Get image attestations",
		Description: "Get in-toto attestation statements attached to a Docker image",
		Tags:        []string{"Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImagesRead, h.GetImageAttestations)
}
