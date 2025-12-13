package handlers

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	networktypes "github.com/getarcaneapp/arcane/types/network"
)

type NetworkHandler struct {
	networkService *services.NetworkService
	dockerService  *services.DockerClientService
}

type NetworkPaginatedResponse struct {
	Success    bool                     `json:"success"`
	Data       []networktypes.Summary   `json:"data"`
	Counts     networktypes.UsageCounts `json:"counts"`
	Pagination base.PaginationResponse  `json:"pagination"`
}

type ListNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Page          int    `query:"pagination[page]" default:"1"`
	Limit         int    `query:"pagination[limit]" default:"20"`
	SortCol       string `query:"sort[column]"`
	SortDir       string `query:"sort[direction]" default:"asc"`
	InUse         string `query:"inUse" doc:"Filter by in-use status (true/false)"`
}

type ListNetworksOutput struct {
	Body NetworkPaginatedResponse
}

type GetNetworkCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type NetworkCountsApiResponse struct {
	Success bool                     `json:"success"`
	Data    networktypes.UsageCounts `json:"data"`
}

type GetNetworkCountsOutput struct {
	Body NetworkCountsApiResponse
}

type NetworkCreatedApiResponse struct {
	Success bool                        `json:"success"`
	Data    networktypes.CreateResponse `json:"data"`
}

type CreateNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          networktypes.CreateRequest
}

type CreateNetworkOutput struct {
	Body NetworkCreatedApiResponse
}

// NetworkInspectApiResponse is a dedicated response type
type NetworkInspectApiResponse struct {
	Success bool                 `json:"success"`
	Data    networktypes.Inspect `json:"data"`
}

type GetNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
	SortCol       string `query:"sort[column]" default:"name"`
	SortDir       string `query:"sort[direction]" default:"asc"`
}

type GetNetworkOutput struct {
	Body NetworkInspectApiResponse
}

// NetworkMessageApiResponse is a dedicated response type
type NetworkMessageApiResponse struct {
	Success bool                 `json:"success"`
	Data    base.MessageResponse `json:"data"`
}

type DeleteNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
}

type DeleteNetworkOutput struct {
	Body NetworkMessageApiResponse
}

type PruneNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// NetworkPruneResponse is a dedicated response type
type NetworkPruneResponse struct {
	Success bool                     `json:"success"`
	Data    networktypes.PruneReport `json:"data"`
}

type PruneNetworksOutput struct {
	Body NetworkPruneResponse
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
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.ListNetworks)

	huma.Register(api, huma.Operation{
		OperationID: "network-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/counts",
		Summary:     "Network counts",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNetworkCounts)

	huma.Register(api, huma.Operation{
		OperationID: "create-network",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks",
		Summary:     "Create network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "get-network",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Get network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "delete-network",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Delete network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteNetwork)

	huma.Register(api, huma.Operation{
		OperationID: "prune-networks",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks/prune",
		Summary:     "Prune networks",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.PruneNetworks)
}

func (h *NetworkHandler) ListNetworks(ctx context.Context, input *ListNetworksInput) (*ListNetworksOutput, error) {
	filters := make(map[string]string)
	if input.InUse != "" {
		filters["inUse"] = input.InUse
	}

	params := pagination.QueryParams{
		SortParams: pagination.SortParams{
			Sort:  input.SortCol,
			Order: pagination.SortOrder(input.SortDir),
		},
		PaginationParams: pagination.PaginationParams{
			Start: (input.Page - 1) * input.Limit,
			Limit: input.Limit,
		},
		Filters: filters,
	}

	networks, paginationResp, counts, err := h.networkService.ListNetworksPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkListError{Err: err}).Error())
	}

	return &ListNetworksOutput{
		Body: NetworkPaginatedResponse{
			Success: true,
			Data:    networks,
			Counts:  counts,
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
		return nil, huma.Error500InternalServerError((&common.NetworkUsageCountsError{Err: err}).Error())
	}

	return &GetNetworkCountsOutput{
		Body: NetworkCountsApiResponse{
			Success: true,
			Data: networktypes.UsageCounts{
				Inuse:  inuse,
				Unused: unused,
				Total:  total,
			},
		},
	}, nil
}

func (h *NetworkHandler) CreateNetwork(ctx context.Context, input *CreateNetworkInput) (*CreateNetworkOutput, error) {
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	// Convert to Docker SDK options
	dockerOptions := input.Body.Options.ToDockerCreateOptions()

	response, err := h.networkService.CreateNetwork(ctx, input.Body.Name, dockerOptions, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkCreationError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.CreateResponse, networktypes.CreateResponse](*response)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}

	return &CreateNetworkOutput{
		Body: NetworkCreatedApiResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *NetworkHandler) GetNetwork(ctx context.Context, input *GetNetworkInput) (*GetNetworkOutput, error) {
	networkInspect, err := h.networkService.GetNetworkByID(ctx, input.NetworkID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.NetworkNotFoundError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.Inspect, networktypes.Inspect](*networkInspect)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}

	// Populate ContainersList
	out.ContainersList = make([]networktypes.ContainerEndpoint, 0, len(out.Containers))
	for id, container := range out.Containers {
		out.ContainersList = append(out.ContainersList, networktypes.ContainerEndpoint{
			ID:          id,
			Name:        container.Name,
			EndpointID:  container.EndpointID,
			IPv4Address: container.IPv4Address,
			IPv6Address: container.IPv6Address,
			MacAddress:  container.MacAddress,
		})
	}

	// Sort ContainersList
	sort.Slice(out.ContainersList, func(i, j int) bool {
		a, b := out.ContainersList[i], out.ContainersList[j]

		if input.SortCol == "ip" {
			valA := a.IPv4Address
			if valA == "" {
				valA = a.IPv6Address
			}
			valB := b.IPv4Address
			if valB == "" {
				valB = b.IPv6Address
			}

			// Parse IPs for proper numeric comparison
			ipA, _, _ := strings.Cut(valA, "/")
			ipB, _, _ := strings.Cut(valB, "/")

			parsedA := net.ParseIP(ipA)
			parsedB := net.ParseIP(ipB)

			if parsedA == nil || parsedB == nil {
				// Fallback to string comparison if parsing fails
				if input.SortDir == "desc" {
					return valA > valB
				}
				return valA < valB
			}

			cmp := bytes.Compare(parsedA, parsedB)
			if input.SortDir == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}

		// Default to Name
		if input.SortDir == "desc" {
			return strings.ToLower(a.Name) > strings.ToLower(b.Name)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return &GetNetworkOutput{
		Body: NetworkInspectApiResponse{
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
		return nil, huma.Error500InternalServerError((&common.NetworkRemovalError{Err: err}).Error())
	}

	return &DeleteNetworkOutput{
		Body: NetworkMessageApiResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Network removed successfully"},
		},
	}, nil
}

func (h *NetworkHandler) PruneNetworks(ctx context.Context, input *PruneNetworksInput) (*PruneNetworksOutput, error) {
	report, err := h.networkService.PruneNetworks(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkPruneError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.PruneReport, networktypes.PruneReport](*report)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}

	return &PruneNetworksOutput{
		Body: NetworkPruneResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}
