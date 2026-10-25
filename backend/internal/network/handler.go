package network

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

type NetworkHandler struct {
	networkService  *NetworkService
	dockerService   *docker.DockerClientService
	activityService *activity.ActivityService
	appCtx          context.Context
}

type ListNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse         string `query:"inUse" doc:"Filter by in-use status (true/false)"`
}

type ListNetworksOutput struct {
	Body base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]
}

type GetNetworkCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type CreateNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          networktypes.CreateRequest
}

type GetNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
	Sort          string `query:"sort" default:"name"`
	Order         string `query:"order" default:"asc"`
}

type GetNetworkTopologyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type DeleteNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
}

type PruneNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ConnectContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
	Body          networktypes.ConnectContainerRequest
}

type DisconnectContainerInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
	Body          networktypes.DisconnectContainerRequest
}

func (h *NetworkHandler) ListNetworks(ctx context.Context, input *ListNetworksInput) (*ListNetworksOutput, error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}

	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.networkService.AggregateNetworks(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list networks: " + err.Error())
		}
		return &ListNetworksOutput{Body: body}, nil
	}

	networks, paginationResp, counts, err := h.networkService.ListNetworksPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list networks: " + err.Error())
	}

	return &ListNetworksOutput{
		Body: base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]{
			Success:    true,
			Data:       networks,
			Counts:     counts,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

func (h *NetworkHandler) GetNetworkCounts(ctx context.Context, input *GetNetworkCountsInput) (*handlerutil.Out[networktypes.UsageCounts], error) {
	_, inuse, unused, total, err := h.dockerService.GetAllNetworks(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get network counts: " + err.Error())
	}

	return &handlerutil.Out[networktypes.UsageCounts]{
		Body: base.ApiResponse[networktypes.UsageCounts]{
			Success: true,
			Data: networktypes.UsageCounts{
				Inuse:  inuse,
				Unused: unused,
				Total:  total,
			},
		},
	}, nil
}

func (h *NetworkHandler) CreateNetwork(ctx context.Context, input *CreateNetworkInput) (*handlerutil.Out[networktypes.CreateResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	dockerOptions, err := mapping.MapOne[networktypes.CreateOptions, client.NetworkCreateOptions](input.Body.Options)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid network options: " + err.Error())
	}
	if !input.Body.Options.EnableIPv6 {
		dockerOptions.EnableIPv6 = nil // keep: mapped options need nil to omit IPv6
	}

	var response *network.CreateResponse
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.Body.Name,
		ResourceName:   input.Body.Name,
		User:           user,
		Step:           "Creating network",
		Message:        "Creating network",
		SuccessMessage: "Network created successfully",
		Metadata: database.JSON{
			"action": "create_network",
			"driver": input.Body.Options.Driver,
		},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		response, createErr = h.networkService.CreateNetwork(runtimeCtx, input.Body.Name, dockerOptions, *user)
		return createErr
	})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(common.ClassifyDockerError(err)).HTTPStatus(), "Failed to create network: "+err.Error())
	}

	out, err := mapping.MapOne[network.CreateResponse, networktypes.CreateResponse](*response)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map network: " + err.Error())
	}
	out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer() // keep: enriches the mapped DTO

	return &handlerutil.Out[networktypes.CreateResponse]{
		Body: base.ApiResponse[networktypes.CreateResponse]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *NetworkHandler) GetNetwork(ctx context.Context, input *GetNetworkInput) (*handlerutil.Out[networktypes.Inspect], error) {
	networkInspect, err := h.networkService.GetNetworkByID(ctx, input.NetworkID)
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve network: "+apiErr.Message)
	}

	out, err := mapping.MapOne[network.Inspect, networktypes.Inspect](*networkInspect)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map network: " + err.Error())
	}

	// Ensure ID is mapped correctly
	out.ID = cmp.Or(out.ID, networkInspect.ID) // keep: enriches the mapped DTO

	// Populate ContainersList
	containers := make([]networktypes.ContainerEndpoint, 0, len(out.Containers))
	for id, container := range out.Containers {
		containers = append(containers, networktypes.ContainerEndpoint{
			ID:          id,
			Name:        container.Name,
			EndpointID:  container.EndpointID,
			IPv4Address: kit.Ternary(container.IPv4Address.IsValid(), container.IPv4Address.String(), ""),
			IPv6Address: kit.Ternary(container.IPv6Address.IsValid(), container.IPv6Address.String(), ""),
			MacAddress:  container.MacAddress.String(),
		})
	}

	// Sort ContainersList
	slices.SortFunc(containers, func(a, b networktypes.ContainerEndpoint) int {
		if input.Sort == "ip" {
			return kit.Ternary(input.Order == "desc", -1, 1) * kit.CompareAddresses(cmp.Or(a.IPv4Address, a.IPv6Address), cmp.Or(b.IPv4Address, b.IPv6Address))
		}

		// Default to Name
		return kit.Ternary(input.Order == "desc", -1, 1) * strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	out.ContainersList = containers // keep: enriches the mapped DTO

	return &handlerutil.Out[networktypes.Inspect]{
		Body: base.ApiResponse[networktypes.Inspect]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *NetworkHandler) GetNetworkTopology(ctx context.Context, input *GetNetworkTopologyInput) (*handlerutil.Out[networktypes.Topology], error) {
	topology, err := h.networkService.GetNetworkTopology(ctx)
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to build network topology: "+apiErr.Message)
	}

	return &handlerutil.Out[networktypes.Topology]{
		Body: base.ApiResponse[networktypes.Topology]{
			Success: true,
			Data:    *topology,
		},
	}, nil
}

func (h *NetworkHandler) DeleteNetwork(ctx context.Context, input *DeleteNetworkInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.NetworkID,
		ResourceName:   input.NetworkID,
		User:           user,
		Step:           "Removing network",
		Message:        "Removing network",
		SuccessMessage: "Network removed successfully",
		Metadata: database.JSON{
			"action": "remove_network",
		},
	}, func(runtimeCtx context.Context) error {
		return h.networkService.RemoveNetwork(runtimeCtx, input.NetworkID, *user)
	})
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to remove network: "+apiErr.Message)
	}

	return handlerutil.MessageOutput("Network removed successfully", activityID), nil
}

func (h *NetworkHandler) ConnectContainer(ctx context.Context, input *ConnectContainerInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.NetworkID,
		ResourceName:   input.NetworkID,
		User:           user,
		Step:           "Connecting container to network",
		Message:        "Connecting container to network",
		SuccessMessage: "Container connected successfully",
		Metadata: database.JSON{
			"action":      "connect_network",
			"containerId": input.Body.ContainerID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.networkService.ConnectContainer(runtimeCtx, input.NetworkID, input.Body, *user)
	})
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	return handlerutil.MessageOutput("Container connected successfully", activityID), nil
}

func (h *NetworkHandler) DisconnectContainer(ctx context.Context, input *DisconnectContainerInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.NetworkID,
		ResourceName:   input.NetworkID,
		User:           user,
		Step:           "Disconnecting container from network",
		Message:        "Disconnecting container from network",
		SuccessMessage: "Container disconnected successfully",
		Metadata: database.JSON{
			"action":      "disconnect_network",
			"containerId": input.Body.ContainerID,
			"force":       input.Body.Force,
		},
	}, func(runtimeCtx context.Context) error {
		return h.networkService.DisconnectContainer(runtimeCtx, input.NetworkID, input.Body, *user)
	})
	if err != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(err))
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to disconnect container from network: "+apiErr.Message)
	}

	return handlerutil.MessageOutput("Container disconnected successfully", activityID), nil
}

func (h *NetworkHandler) PruneNetworks(ctx context.Context, input *PruneNetworksInput) (*handlerutil.Out[networktypes.PruneReport], error) {
	var report *network.PruneReport
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "network",
		Step:           "Pruning unused networks",
		Message:        "Pruning unused networks",
		SuccessMessage: "Networks pruned successfully",
		Metadata:       database.JSON{"action": "prune_networks"},
	}, func(runtimeCtx context.Context) error {
		var pruneErr error
		report, pruneErr = h.networkService.PruneNetworks(runtimeCtx)
		return pruneErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to prune networks: " + err.Error())
	}

	out, err := mapping.MapOne[network.PruneReport, networktypes.PruneReport](*report)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map network: " + err.Error())
	}
	out.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer() // keep: enriches the mapped DTO

	return &handlerutil.Out[networktypes.PruneReport]{
		Body: base.ApiResponse[networktypes.PruneReport]{
			Success: true,
			Data:    out,
		},
	}, nil
}
