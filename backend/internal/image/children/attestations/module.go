package attestations

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
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
