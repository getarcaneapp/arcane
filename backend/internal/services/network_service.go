package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	networktypes "go.getarcane.app/types/network"
)

type NetworkService struct {
	db            *database.DB
	dockerService *DockerClientService
	eventService  *EventService
}

func NewNetworkService(db *database.DB, dockerService *DockerClientService, eventService *EventService) *NetworkService {
	return &NetworkService{
		db:            db,
		dockerService: dockerService,
		eventService:  eventService,
	}
}

func (s *NetworkService) GetNetworkByID(ctx context.Context, id string) (*network.Inspect, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInspect, err := dockerClient.NetworkInspect(ctx, id, network.InspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}

	return &networkInspect, nil
}

func (s *NetworkService) CreateNetwork(ctx context.Context, name string, options network.CreateOptions, user models.User) (*network.CreateResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	response, err := dockerClient.NetworkCreate(ctx, name, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	metadata := models.JSON{
		"action": "create",
		"driver": options.Driver,
		"name":   name,
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkCreate, response.ID, name, user.ID, user.Username, "0", metadata); logErr != nil {
		fmt.Printf("Could not log network creation action: %s\n", logErr)
	}

	return &response, nil
}

func (s *NetworkService) RemoveNetwork(ctx context.Context, id string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", id, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInfo, err := dockerClient.NetworkInspect(ctx, id, network.InspectOptions{})
	var networkName string
	if err == nil {
		networkName = networkInfo.Name
	} else {
		networkName = id
	}

	if err := dockerClient.NetworkRemove(ctx, id); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", id, networkName, user.ID, user.Username, "0", err, models.JSON{"action": "delete"})
		return fmt.Errorf("failed to remove network: %w", err)
	}

	metadata := models.JSON{
		"action":    "delete",
		"networkId": id,
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkDelete, id, networkName, user.ID, user.Username, "0", metadata); logErr != nil {
		fmt.Printf("Could not log network delete action: %s\n", logErr)
	}

	return nil
}

func (s *NetworkService) PruneNetworks(ctx context.Context) (*network.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := filters.NewArgs()

	report, err := dockerClient.NetworksPrune(ctx, filterArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to prune networks: %w", err)
	}

	metadata := models.JSON{
		"action":          "prune",
		"networksDeleted": len(report.NetworksDeleted),
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		fmt.Printf("Could not log network prune action: %s\n", logErr)
	}

	return &report, nil
}

func (s *NetworkService) ListNetworksPaginated(ctx context.Context, params pagination.QueryParams) ([]networktypes.Summary, pagination.Response, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list containers: %w", err)
	}

	inUseByID := make(map[string]bool)
	inUseByName := make(map[string]bool)
	for _, c := range containers {
		if c.NetworkSettings == nil || c.NetworkSettings.Networks == nil {
			continue
		}
		for netName, es := range c.NetworkSettings.Networks {
			if es.NetworkID != "" {
				inUseByID[es.NetworkID] = true
			}
			inUseByName[netName] = true
		}
	}

	rawNets, err := dockerClient.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list Docker networks: %w", err)
	}

	items := make([]networktypes.Summary, 0, len(rawNets))
	for _, n := range rawNets {
		netDto := networktypes.NewSummary(n)
		netDto.InUse = inUseByID[netDto.ID] || inUseByName[netDto.Name]
		netDto.IsDefault = dockerutil.IsDefaultNetwork(netDto.Name)
		items = append(items, netDto)
	}

	config := pagination.Config[networktypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[networktypes.Summary]{
			func(n networktypes.Summary) (string, error) { return n.Name, nil },
			func(n networktypes.Summary) (string, error) { return n.Driver, nil },
			func(n networktypes.Summary) (string, error) { return n.Scope, nil },
			func(n networktypes.Summary) (string, error) { return n.ID, nil },
		},
		SortBindings: []pagination.SortBinding[networktypes.Summary]{
			{
				Key: "name",
				Fn: func(a, b networktypes.Summary) int {
					return strings.Compare(a.Name, b.Name)
				},
			},
			{
				Key: "driver",
				Fn: func(a, b networktypes.Summary) int {
					return strings.Compare(a.Driver, b.Driver)
				},
			},
			{
				Key: "scope",
				Fn: func(a, b networktypes.Summary) int {
					return strings.Compare(a.Scope, b.Scope)
				},
			},
			{
				Key: "created",
				Fn: func(a, b networktypes.Summary) int {
					if a.Created.Before(b.Created) {
						return -1
					}
					if a.Created.After(b.Created) {
						return 1
					}
					return 0
				},
			},
			{
				Key: "inUse",
				Fn: func(a, b networktypes.Summary) int {
					if a.InUse == b.InUse {
						return 0
					}
					if a.InUse {
						return -1
					}
					return 1
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[networktypes.Summary]{
			{
				Key: "inUse",
				Fn: func(n networktypes.Summary, filterValue string) bool {
					if filterValue == "true" {
						return n.InUse
					}
					if filterValue == "false" {
						return !n.InUse
					}
					return true
				},
			},
		},
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)

	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	paginationResp := pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}

	return result.Items, paginationResp, nil
}
