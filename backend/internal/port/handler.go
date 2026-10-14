package port

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/port"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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

func (h *PortHandler) ListPorts(ctx context.Context, input *ListPortsInput) (*handlerutil.Page[port.PortMapping], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.portService.AggregatePorts(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list ports")
		}
		return &handlerutil.Page[port.PortMapping]{Body: body}, nil
	}

	items, paginationResp, err := h.portService.ListPortsPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list ports")
	}

	return &handlerutil.Page[port.PortMapping]{
		Body: base.Paginated[port.PortMapping]{
			Success:    true,
			Data:       items,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}
