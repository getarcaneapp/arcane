package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	dockernetwork "github.com/docker/docker/api/types/network"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"go.getarcane.app/types/base"
	networktypes "go.getarcane.app/types/network"
)

type NetworkHandler struct {
	networkService *services.NetworkService
	dockerService  *services.DockerClientService
}

type NetworkPaginatedResponse struct {
	Success    bool                        `json:"success"`
	Data       []networktypes.Summary      `json:"data"`
	Pagination base.PaginationResponse    `json:"pagination"`
}

type ListNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Page          int    `query:"pagination[page]" default:"1"`
	Limit         int    `query:"pagination[limit]" default:"20"`
	SortCol       string `query:"sort[column]"`
	SortDir       string `query:"sort[direction]" default:"asc"`
}

type ListNetworksOutput struct {
	Body NetworkPaginatedResponse
}

type GetNetworkCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetNetworkCountsOutput struct {
	Body base.ApiResponse[networktypes.UsageCounts]
}

type CreateNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          struct {
		Name    string                        `json:"name"`
		Options dockernetwork.CreateOptions `json:"options"`
	} `json:"body"`
}

type CreateNetworkOutput struct {
	Body base.ApiResponse[networktypes.CreateResponse]
}

type GetNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
}

type GetNetworkOutput struct {
	Body base.ApiResponse[networktypes.Inspect]
}

type DeleteNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
}

type DeleteNetworkOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PruneNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type PruneNetworksOutput struct {
	Body base.ApiResponse[networktypes.PruneReport]
}

// RegisterNetworks registers network endpoints.
func RegisterNetworks(api huma.API, networkSvc *services.NetworkService, dockerSvc *services.DockerClientService) {
	h := &NetworkHandler{
		networkService: networkSvc,
		dockerService:  dockerSvc,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-networks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks",
		Summary:     "List networks",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListNetworks)

	huma.Register(api, huma.Operation{
		OperationID: "network-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/counts",
		Summary:     "Network counts",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNetworkCounts)

	huma.Register(api, huma.Operation{
		OperationID: "create-network",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks",
		Summary:     "Create network",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "get-network",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Get network",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "delete-network",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Delete network",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "prune-networks",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks/prune",
		Summary:     "Prune networks",
		Tags:        []string{"Networks"},
		Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.PruneNetworks)
}

func (h *NetworkHandler) ListNetworks(ctx context.Context, input *ListNetworksInput) (*ListNetworksOutput, error) {
	params := pagination.QueryParams{
		PaginationParams: pagination.PaginationParams{
			Start: (input.Page - 1) * input.Limit,
			Limit: input.Limit,
		},
	}

	networks, paginationResp, err := h.networkService.ListNetworksPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &ListNetworksOutput{
		Body: NetworkPaginatedResponse{
			Success: true,
			Data:    networks,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

func (h *NetworkHandler) GetNetworkCounts(ctx context.Context, input *GetNetworkCountsInput) (*GetNetworkCountsOutput, error) {
	_, inuse, unused, total, err := h.dockerService.GetAllNetworks(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetNetworkCountsOutput{
		Body: base.ApiResponse[networktypes.UsageCounts]{
			Success: true,
			Data: networktypes.UsageCounts{
				Inuse:  int(inuse),
				Unused: int(unused),
				Total:  int(total),
			},
		},
	}, nil
}

func (h *NetworkHandler) CreateNetwork(ctx context.Context, input *CreateNetworkInput) (*CreateNetworkOutput, error) {
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	response, err := h.networkService.CreateNetwork(ctx, input.Body.Name, input.Body.Options, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	out, err := mapper.MapOne[dockernetwork.CreateResponse, networktypes.CreateResponse](*response)
	if err != nil {
		return nil, huma.Error500InternalServerError("mapping error")
	}

	return &CreateNetworkOutput{
		Body: base.ApiResponse[networktypes.CreateResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *NetworkHandler) GetNetwork(ctx context.Context, input *GetNetworkInput) (*GetNetworkOutput, error) {
	networkInspect, err := h.networkService.GetNetworkByID(ctx, input.NetworkID)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	out, err := mapper.MapOne[dockernetwork.Inspect, networktypes.Inspect](*networkInspect)
	if err != nil {
		return nil, huma.Error500InternalServerError("mapping error")
	}

	return &GetNetworkOutput{
		Body: base.ApiResponse[networktypes.Inspect]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *NetworkHandler) DeleteNetwork(ctx context.Context, input *DeleteNetworkInput) (*DeleteNetworkOutput, error) {
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	if err := h.networkService.RemoveNetwork(ctx, input.NetworkID, *user); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &DeleteNetworkOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{Message: "Network removed successfully"},
		},
	}, nil
}

func (h *NetworkHandler) PruneNetworks(ctx context.Context, input *PruneNetworksInput) (*PruneNetworksOutput, error) {
	report, err := h.networkService.PruneNetworks(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	out, err := mapper.MapOne[dockernetwork.PruneReport, networktypes.PruneReport](*report)
	if err != nil {
		return nil, huma.Error500InternalServerError("mapping error")
	}

	return &PruneNetworksOutput{
		Body: base.ApiResponse[networktypes.PruneReport]{
			Success: true,
			Data:    out,
		},
	}, nil
}
