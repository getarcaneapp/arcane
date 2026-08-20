package port

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	porttypes "github.com/getarcaneapp/arcane/types/v2/port"
)

type PortHandler struct {
	portService *PortService
}

type ListPortsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

func RegisterPorts(api huma.API, portSvc *PortService) {
	h := &PortHandler{portService: portSvc}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-ports",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/ports",
		Summary:     "List port mappings",
		Tags:        []string{"Ports"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermContainersList, h.ListPorts)
}

func (h *PortHandler) ListPorts(ctx context.Context, input *ListPortsInput) (*handlerutil.Page[porttypes.PortMapping], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	items, paginationResp, err := h.portService.ListPortsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list ports")
	}

	return &handlerutil.Page[porttypes.PortMapping]{
		Body: base.Paginated[porttypes.PortMapping]{
			Success:    true,
			Data:       items,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}
