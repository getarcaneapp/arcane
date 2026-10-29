package releasenotes

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-image-release-notes",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/updates/releasenotes",
		Summary:     "Get image release notes",
		Description: "Get release notes for the target version of an image update",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetReleaseNotes)
}
