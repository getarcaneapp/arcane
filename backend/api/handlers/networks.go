package handlers

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	dockernetwork "github.com/moby/moby/api/types/network"
)

type networkHandler struct {
	networkService  *services.NetworkService
	dockerService   *services.DockerClientService
	activityService *services.ActivityService
	appCtx          context.Context
}

type networkPaginatedResponse struct {
	Success    bool                     `json:"success"`
	Data       []networktypes.Summary   `json:"data"`
	Counts     networktypes.UsageCounts `json:"counts"`
	Pagination base.PaginationResponse  `json:"pagination"`
}

type listNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	InUse         string `query:"inUse" doc:"Filter by in-use status (true/false)"`
}

type listNetworksOutput struct {
	Body networkPaginatedResponse
}

type getNetworkCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type networkCountsApiResponse struct {
	Success bool                     `json:"success"`
	Data    networktypes.UsageCounts `json:"data"`
}

type getNetworkCountsOutput struct {
	Body networkCountsApiResponse
}

type networkCreatedApiResponse struct {
	Success bool                        `json:"success"`
	Data    networktypes.CreateResponse `json:"data"`
}

type createNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          networktypes.CreateRequest
}

type createNetworkOutput struct {
	Body networkCreatedApiResponse
}

// networkInspectApiResponse is a dedicated response type
type networkInspectApiResponse struct {
	Success bool                 `json:"success"`
	Data    networktypes.Inspect `json:"data"`
}

type getNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
	Sort          string `query:"sort" default:"name"`
	Order         string `query:"order" default:"asc"`
}

type getNetworkOutput struct {
	Body networkInspectApiResponse
}

type networkTopologyApiResponse struct {
	Success bool                  `json:"success"`
	Data    networktypes.Topology `json:"data"`
}

type getNetworkTopologyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getNetworkTopologyOutput struct {
	Body networkTopologyApiResponse
}

// networkMessageApiResponse is a dedicated response type
type networkMessageApiResponse struct {
	Success bool                 `json:"success"`
	Data    base.MessageResponse `json:"data"`
}

type deleteNetworkInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NetworkID     string `path:"networkId" doc:"Network ID"`
}

type deleteNetworkOutput struct {
	Body networkMessageApiResponse
}

type pruneNetworksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// networkPruneResponse is a dedicated response type
type networkPruneResponse struct {
	Success bool                     `json:"success"`
	Data    networktypes.PruneReport `json:"data"`
}

type pruneNetworksOutput struct {
	Body networkPruneResponse
}

// RegisterNetworks registers network endpoints.
func RegisterNetworks(api huma.API, networkSvc *services.NetworkService, dockerSvc *services.DockerClientService, activitySvc *services.ActivityService, appCtx ActivityAppContext) {
	h := &networkHandler{
		networkService:  networkSvc,
		dockerService:   dockerSvc,
		activityService: activitySvc,
		appCtx:          appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-networks",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks",
		Summary:     "List networks",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksList, h.listNetworksInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "network-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/counts",
		Summary:     "Network counts",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksList, h.getNetworkCountsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-network",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks",
		Summary:     "Create network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksCreate, h.createNetworkInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-network-topology",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/topology",
		Summary:     "Get network topology",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksRead, h.getNetworkTopologyInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-network",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Get network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksRead, h.getNetworkInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-network",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/networks/{networkId}",
		Summary:     "Delete network",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksDelete, h.deleteNetworkInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "prune-networks",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/networks/prune",
		Summary:     "Prune networks",
		Tags:        []string{"Networks"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNetworksPrune, h.pruneNetworksInternal)
}

func (h *networkHandler) listNetworksInternal(ctx context.Context, input *listNetworksInput) (*listNetworksOutput, error) {
	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.InUse != "" {
		params.Filters["inUse"] = input.InUse
	}

	networks, paginationResp, counts, err := h.networkService.ListNetworksPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkListError{Err: err}).Error())
	}

	return &listNetworksOutput{
		Body: networkPaginatedResponse{
			Success:    true,
			Data:       networks,
			Counts:     counts,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

func (h *networkHandler) getNetworkCountsInternal(ctx context.Context, _ *getNetworkCountsInput) (*getNetworkCountsOutput, error) {
	_, inuse, unused, total, err := h.dockerService.GetAllNetworks(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkUsageCountsError{Err: err}).Error())
	}

	return &getNetworkCountsOutput{
		Body: networkCountsApiResponse{
			Success: true,
			Data: networktypes.UsageCounts{
				Inuse:  inuse,
				Unused: unused,
				Total:  total,
			},
		},
	}, nil
}

func (h *networkHandler) createNetworkInternal(ctx context.Context, input *createNetworkInput) (*createNetworkOutput, error) {
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to Docker SDK options
	dockerOptions := input.Body.Options.ToDockerCreateOptions()

	var response *dockernetwork.CreateResponse
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.Body.Name,
		ResourceName:   input.Body.Name,
		User:           user,
		Step:           "Creating network",
		Message:        "Creating network",
		SuccessMessage: "Network created successfully",
		Metadata: models.JSON{
			"action": "create_network",
			"driver": input.Body.Options.Driver,
		},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		response, createErr = h.networkService.CreateNetwork(runtimeCtx, input.Body.Name, dockerOptions, *user)
		return createErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkCreationError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.CreateResponse, networktypes.CreateResponse](*response)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}
	out.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &createNetworkOutput{
		Body: networkCreatedApiResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *networkHandler) getNetworkInternal(ctx context.Context, input *getNetworkInput) (*getNetworkOutput, error) {
	networkInspect, err := h.networkService.GetNetworkByID(ctx, input.NetworkID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.NetworkNotFoundError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.Inspect, networktypes.Inspect](*networkInspect)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}

	// Ensure ID is mapped correctly
	if out.ID == "" {
		out.ID = networkInspect.ID
	}

	// Populate ContainersList
	out.ContainersList = make([]networktypes.ContainerEndpoint, 0, len(out.Containers))
	for id, container := range out.Containers {
		ipv4Address := ""
		if container.IPv4Address.IsValid() {
			ipv4Address = container.IPv4Address.String()
		}
		ipv6Address := ""
		if container.IPv6Address.IsValid() {
			ipv6Address = container.IPv6Address.String()
		}
		out.ContainersList = append(out.ContainersList, networktypes.ContainerEndpoint{
			ID:          id,
			Name:        container.Name,
			EndpointID:  container.EndpointID,
			IPv4Address: ipv4Address,
			IPv6Address: ipv6Address,
			MacAddress:  container.MacAddress.String(),
		})
	}

	// Sort ContainersList
	sort.Slice(out.ContainersList, func(i, j int) bool {
		a, b := out.ContainersList[i], out.ContainersList[j]

		if input.Sort == "ip" {
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
				if input.Order == "desc" {
					return valA > valB
				}
				return valA < valB
			}

			cmp := bytes.Compare(parsedA, parsedB)
			if input.Order == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}

		// Default to Name
		if input.Order == "desc" {
			return strings.ToLower(a.Name) > strings.ToLower(b.Name)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return &getNetworkOutput{
		Body: networkInspectApiResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *networkHandler) getNetworkTopologyInternal(ctx context.Context, _ *getNetworkTopologyInput) (*getNetworkTopologyOutput, error) {
	topology, err := h.networkService.GetNetworkTopology(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to build network topology")
	}

	return &getNetworkTopologyOutput{
		Body: networkTopologyApiResponse{
			Success: true,
			Data:    *topology,
		},
	}, nil
}

func (h *networkHandler) deleteNetworkInternal(ctx context.Context, input *deleteNetworkInput) (*deleteNetworkOutput, error) {
	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "network",
		ResourceID:     input.NetworkID,
		ResourceName:   input.NetworkID,
		User:           user,
		Step:           "Removing network",
		Message:        "Removing network",
		SuccessMessage: "Network removed successfully",
		Metadata: models.JSON{
			"action": "remove_network",
		},
	}, func(runtimeCtx context.Context) error {
		return h.networkService.RemoveNetwork(runtimeCtx, input.NetworkID, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkRemovalError{Err: err}).Error())
	}

	return &deleteNetworkOutput{
		Body: networkMessageApiResponse{
			Success: true,
			Data:    base.MessageResponse{Message: "Network removed successfully", ActivityID: utils.StringPtrFromTrimmed(activityID)},
		},
	}, nil
}

func (h *networkHandler) pruneNetworksInternal(ctx context.Context, input *pruneNetworksInput) (*pruneNetworksOutput, error) {
	var report *dockernetwork.PruneReport
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "network",
		Step:           "Pruning unused networks",
		Message:        "Pruning unused networks",
		SuccessMessage: "Networks pruned successfully",
		Metadata:       models.JSON{"action": "prune_networks"},
	}, func(runtimeCtx context.Context) error {
		var pruneErr error
		report, pruneErr = h.networkService.PruneNetworks(runtimeCtx)
		return pruneErr
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkPruneError{Err: err}).Error())
	}

	out, err := mapper.MapOne[dockernetwork.PruneReport, networktypes.PruneReport](*report)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NetworkMappingError{Err: err}).Error())
	}
	out.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &pruneNetworksOutput{
		Body: networkPruneResponse{
			Success: true,
			Data:    out,
		},
	}, nil
}
