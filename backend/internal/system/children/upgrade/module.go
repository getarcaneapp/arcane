package upgrade

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

// RegisterRoutes registers the self-upgrade and upgrade job endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-upgrade",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/check",
		Summary:     "Check for system upgrade",
		Description: "Check if a system upgrade is available",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.CheckUpgradeAvailable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "trigger-upgrade",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade",
		Summary:       "Trigger system upgrade",
		Description:   "Trigger a system upgrade",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemUpgrade, h.TriggerUpgrade)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:   "start-upgrade-job",
		Method:        http.MethodPost,
		Path:          "/environments/{id}/system/upgrade/jobs",
		Summary:       "Start upgrade job",
		Description:   "Upgrade the requested environments, or every enabled environment when none are given, with the manager last",
		DefaultStatus: http.StatusAccepted,
		Tags:          []string{"System"},
		Security:      handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemUpgrade, h.StartUpgradeJob)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-current-upgrade-job",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/jobs/current",
		Summary:     "Get current upgrade job",
		Description: "Get the latest upgrade job",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSystemRead, h.GetCurrentUpgradeJob)

	sse.Register(api, huma.Operation{
		OperationID: "stream-upgrade-progress",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/system/upgrade/progress",
		Summary:     "Stream upgrade progress",
		Description: "Stream this instance's self-upgrade progress, replaying the latest upgrade log once it has restarted",
		Tags:        []string{"System"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaRequiredPermission: authz.PermSystemRead},
		Middlewares: append(huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
			httpx.SetStreamHeaders(ctx)
			next(ctx)
		}}, middleware.RequirePermission(api, authz.PermSystemRead)...),
	}, map[string]any{"message": types.UpgradeProgressEvent{}}, h.StreamUpgradeProgress)
}
