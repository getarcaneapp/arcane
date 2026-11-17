package transfer

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "preflight-project-transfer",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/projects/{projectId}/transfer/preflight",
		Summary:     "Check a project copy or move",
		Description: "Report what copying or moving the project to another environment would do, without changing either environment",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.Preflight)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "start-project-transfer",
		DefaultStatus: http.StatusAccepted,
		Method:        http.MethodPost,
		Path:          "/agents/{id}/projects/{projectId}/transfer",
		Summary:       "Copy or move a project to another environment",
		Description:   "Re-run the preflight checks and start the transfer; progress is reported through the returned activity",
		Tags:          []string{"Projects"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.Start)
}
