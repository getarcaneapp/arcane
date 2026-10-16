package network

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/docker/compat"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

const (
	networkTopologyCacheTTL     = 5 * time.Second
	networkTopologyBuildTimeout = 30 * time.Second
)

type NetworkService struct {
	db            *database.DB
	dockerService *dockerInternal.DockerClientService
	eventService  *event.EventService

	// Shared, immutable topology build; network mutations bump topologyGen,
	// which retires cached entries and in-flight builds started before them.
	topologyFlight singleflight.Group
	topologyCache  atomic.Pointer[networkTopologyCacheEntry]
	topologyGen    atomic.Uint64
}

type networkTopologyCacheEntry struct {
	topology *networktypes.Topology
	builtAt  time.Time
	gen      uint64
}

func NewNetworkService(db *database.DB, dockerService *dockerInternal.DockerClientService, eventService *event.EventService) *NetworkService {
	return &NetworkService{
		db:            db,
		dockerService: dockerService,
		eventService:  eventService,
	}
}

func (s *NetworkService) GetNetworkByID(ctx context.Context, id string) (*network.Inspect, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInspect, err := compat.NetworkInspectWithCompatibility(ctx, dockerClient, id, client.NetworkInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}

	return new(networkInspect.Network), nil
}

// GetNetworkTopology returns a short-TTL cached topology shared by all callers; it must not be mutated.
func (s *NetworkService) GetNetworkTopology(ctx context.Context) (*networktypes.Topology, error) {
	gen := s.topologyGen.Load()
	if entry := s.topologyCache.Load(); entry != nil && entry.gen == gen && time.Since(entry.builtAt) < networkTopologyCacheTTL {
		return entry.topology, nil
	}

	// Keyed by generation so callers after a network change never join an older build.
	ch := s.topologyFlight.DoChan(strconv.FormatUint(gen, 10), func() (any, error) {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), networkTopologyBuildTimeout)
		defer cancel()

		topology, err := s.buildTopology(buildCtx)
		if err != nil {
			return nil, err
		}
		s.topologyCache.Store(&networkTopologyCacheEntry{
			topology: topology,
			builtAt:  time.Now(),
			gen:      gen,
		})
		return topology, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		topology, ok := res.Val.(*networktypes.Topology)
		if !ok {
			return nil, errors.New("network topology cache returned unexpected type")
		}
		return topology, nil
	}
}

func (s *NetworkService) buildTopology(ctx context.Context) (*networktypes.Topology, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containersByID := make(map[string]container.Summary, len(containers))
	for _, rawContainer := range containers {
		containersByID[rawContainer.ID] = rawContainer
	}

	networkList, err := compat.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker networks: %w", err)
	}

	inspectedNetworks := make([]network.Inspect, len(networkList.Items))

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for i, rawNetwork := range networkList.Items {
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "network worker")

			inspected, inspectErr := compat.NetworkInspectWithCompatibility(groupCtx, dockerClient, rawNetwork.ID, client.NetworkInspectOptions{})
			if inspectErr != nil {
				return fmt.Errorf("failed to inspect network %s: %w", rawNetwork.Name, inspectErr)
			}

			inspectedNetworks[i] = inspected.Network
			return nil
		})
	}

	if waitErr := g.Wait(); waitErr != nil {
		return nil, waitErr
	}

	nodes := make([]networktypes.TopologyNode, 0, len(networkList.Items))
	edges := make([]networktypes.TopologyEdge, 0)
	containerNodeIDs := make(map[string]struct{})
	for i, rawNetwork := range networkList.Items {
		nodes = append(nodes, networktypes.TopologyNode{
			ID:   rawNetwork.ID,
			Name: rawNetwork.Name,
			Type: networktypes.TopologyNodeTypeNetwork,
			Metadata: networktypes.TopologyNodeMetadata{
				Driver:    rawNetwork.Driver,
				Scope:     rawNetwork.Scope,
				IsDefault: docker.IsDefaultNetwork(rawNetwork.Name),
			},
		})

		for containerID, endpoint := range inspectedNetworks[i].Containers {
			if _, exists := containerNodeIDs[containerID]; !exists {
				ctr := containersByID[containerID]
				nodes = append(nodes, networktypes.TopologyNode{
					ID:   containerID,
					Name: cmp.Or(kit.Ternary(len(ctr.Names) > 0, docker.ContainerNameFromNames(ctr.Names), ctr.ID), endpoint.Name),
					Type: networktypes.TopologyNodeTypeContainer,
					Metadata: networktypes.TopologyNodeMetadata{
						Status: string(ctr.State),
						Image:  ctr.Image,
					},
				})
				containerNodeIDs[containerID] = struct{}{}
			}

			edges = append(edges, networktypes.TopologyEdge{
				ID:          fmt.Sprintf("%s:%s", rawNetwork.ID, containerID),
				Source:      rawNetwork.ID,
				Target:      containerID,
				IPv4Address: kit.Ternary(endpoint.IPv4Address.IsValid(), endpoint.IPv4Address.String(), ""),
				IPv6Address: kit.Ternary(endpoint.IPv6Address.IsValid(), endpoint.IPv6Address.String(), ""),
			})
		}
	}

	slices.SortFunc(nodes, func(a, b networktypes.TopologyNode) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	slices.SortFunc(edges, func(a, b networktypes.TopologyEdge) int {
		return cmp.Or(strings.Compare(a.Source, b.Source), strings.Compare(a.Target, b.Target))
	})

	return &networktypes.Topology{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

func (s *NetworkService) CreateNetwork(ctx context.Context, name string, options client.NetworkCreateOptions, user usertypes.Actor) (*network.CreateResponse, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	defer s.eventService.BeginDockerResourceSuppressionWindow("network", "", name)()
	response, err := dockerClient.NetworkCreate(ctx, name, options)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", err, database.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to create network: %w", err)
	}
	s.topologyGen.Add(1)

	if logErr := s.eventService.LogNetworkEvent(ctx, event.EventTypeNetworkCreate, response.ID, name, user.ID, user.Username, "0", database.JSON{
		"action": "create",
		"driver": options.Driver,
		"name":   name,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log network creation action", "error", logErr)
	}

	return &network.CreateResponse{
		ID:      response.ID,
		Warning: strings.Join(response.Warning, "; "),
	}, nil
}

func (s *NetworkService) RemoveNetwork(ctx context.Context, id string, user usertypes.Actor) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", id, "", user.ID, user.Username, "0", err, database.JSON{"action": "delete"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInfo, err := compat.NetworkInspectWithCompatibility(ctx, dockerClient, id, client.NetworkInspectOptions{})
	networkName := kit.Ternary(err == nil, networkInfo.Network.Name, id)

	defer s.eventService.BeginDockerResourceSuppressionWindow("network", id, networkName)()
	if _, networkRemoveErr := dockerClient.NetworkRemove(ctx, id, client.NetworkRemoveOptions{}); networkRemoveErr != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", id, networkName, user.ID, user.Username, "0", networkRemoveErr, database.JSON{"action": "delete"})
		return fmt.Errorf("failed to remove network: %w", networkRemoveErr)
	}
	s.topologyGen.Add(1)

	if logErr := s.eventService.LogNetworkEvent(ctx, event.EventTypeNetworkDelete, id, networkName, user.ID, user.Username, "0", database.JSON{
		"action":    "delete",
		"networkId": id,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log network delete action", "error", logErr)
	}

	return nil
}

// ConnectContainer connects a container to a network without recreating it.
func (s *NetworkService) ConnectContainer(ctx context.Context, networkID string, req networktypes.ConnectContainerRequest, user usertypes.Actor) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", networkID, "", user.ID, user.Username, "0", err, database.JSON{"action": "connect", "containerId": req.ContainerID})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	ipv4, ipv4Err := netip.ParseAddr(strings.TrimSpace(req.IPv4Address))
	if ipv4Err != nil && strings.TrimSpace(req.IPv4Address) != "" {
		return common.Classify(common.ErrValidation, fmt.Errorf("invalid IPv4 address %s: %w", strings.TrimSpace(req.IPv4Address), ipv4Err))
	}
	ipv6, ipv6Err := netip.ParseAddr(strings.TrimSpace(req.IPv6Address))
	if ipv6Err != nil && strings.TrimSpace(req.IPv6Address) != "" {
		return common.Classify(common.ErrValidation, fmt.Errorf("invalid IPv6 address %s: %w", strings.TrimSpace(req.IPv6Address), ipv6Err))
	}

	if _, networkConnectErr := dockerClient.NetworkConnect(ctx, networkID, client.NetworkConnectOptions{
		Container: req.ContainerID,
		EndpointConfig: &network.EndpointSettings{
			Aliases: req.Aliases,
			IPAMConfig: func() *network.EndpointIPAMConfig {
				if !ipv4.IsValid() && !ipv6.IsValid() {
					return nil
				}
				return &network.EndpointIPAMConfig{
					IPv4Address: ipv4,
					IPv6Address: ipv6,
				}
			}(),
		},
	}); networkConnectErr != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeNetworkError,
			"network",
			networkID,
			"",
			user.ID,
			user.Username,
			"0",
			networkConnectErr,
			database.JSON{
				"action":      "connect",
				"containerId": req.ContainerID,
			},
		)
		return fmt.Errorf("failed to connect container to network: %w", networkConnectErr)
	}
	s.topologyGen.Add(1)

	if logErr := s.eventService.LogNetworkEvent(ctx, event.EventTypeNetworkConnect, networkID, networkID, user.ID, user.Username, "0", database.JSON{
		"action":      "connect",
		"networkId":   networkID,
		"containerId": req.ContainerID,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log network connect action", "error", logErr)
	}

	return nil
}

// DisconnectContainer disconnects a container from a network.
func (s *NetworkService) DisconnectContainer(ctx context.Context, networkID string, req networktypes.DisconnectContainerRequest, user usertypes.Actor) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, event.EventTypeNetworkError, "network", networkID, "", user.ID, user.Username, "0", err, database.JSON{"action": "disconnect", "containerId": req.ContainerID})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, networkDisconnectErr := dockerClient.NetworkDisconnect(ctx, networkID, client.NetworkDisconnectOptions{
		Container: req.ContainerID,
		Force:     req.Force,
	}); networkDisconnectErr != nil {
		s.eventService.LogErrorEvent(
			ctx,
			event.EventTypeNetworkError,
			"network",
			networkID,
			"",
			user.ID,
			user.Username,
			"0",
			networkDisconnectErr,
			database.JSON{
				"action":      "disconnect",
				"containerId": req.ContainerID,
			},
		)
		return fmt.Errorf("failed to disconnect container from network: %w", networkDisconnectErr)
	}
	s.topologyGen.Add(1)

	if logErr := s.eventService.LogNetworkEvent(ctx, event.EventTypeNetworkDisconnect, networkID, networkID, user.ID, user.Username, "0", database.JSON{
		"action":      "disconnect",
		"networkId":   networkID,
		"containerId": req.ContainerID,
		"force":       req.Force,
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log network disconnect action", "error", logErr)
	}

	return nil
}

func (s *NetworkService) PruneNetworks(ctx context.Context) (*network.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	report, err := dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: make(client.Filters)})
	if err != nil {
		return nil, fmt.Errorf("failed to prune networks: %w", err)
	}
	s.topologyGen.Add(1)

	if logErr := s.eventService.LogNetworkEvent(ctx, event.EventTypeNetworkDelete, "", "bulk_prune", usertypes.SystemUser.ID, usertypes.SystemUser.Username, "0", database.JSON{
		"action":          "prune",
		"networksDeleted": len(report.Report.NetworksDeleted),
	}); logErr != nil {
		slog.WarnContext(ctx, "could not log network prune action", "error", logErr)
	}

	return &report.Report, nil
}

func (s *NetworkService) ListNetworksPaginated(ctx context.Context, params pagination.QueryParams) ([]networktypes.Summary, pagination.Response, networktypes.UsageCounts, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to list containers: %w", err)
	}

	inUseByID, inUseByName := docker.BuildNetworkUsageMaps(containers)

	networkList, err := compat.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to list Docker networks: %w", err)
	}

	items := make([]networktypes.Summary, 0, len(networkList.Items))
	inUse := 0
	for _, n := range networkList.Items {
		netDto, mapErr := mapping.MapOne[network.Summary, networktypes.Summary](n)
		if mapErr != nil {
			return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to map network: %w", mapErr)
		}
		netDto.InUse = inUseByID[netDto.ID] || inUseByName[netDto.Name] // keep: enriches the mapped DTO
		netDto.IsDefault = docker.IsDefaultNetwork(netDto.Name)         // keep: enriches the mapped DTO
		items = append(items, netDto)
		if netDto.InUse || netDto.IsDefault {
			inUse++
		}
	}
	result := s.buildNetworkPaginationConfig().SearchOrderAndPaginate(items, params)

	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), networktypes.UsageCounts{
		Inuse: inUse,
		// Only count non-default networks as unused
		// Default networks (bridge, host, none, ingress) are never "unused"
		Unused: len(items) - inUse,
		Total:  len(items),
	}, nil
}

func (s *NetworkService) AggregateNetworks(ctx context.Context, params pagination.QueryParams) (base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts], error) {
	collection, err := httpx.CollectEnvironments[base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]{}, err
	}

	pages := make([]pagination.EnvironmentPage[networktypes.Summary], 0, len(collection.Results))
	inUse, unused, total := 0, 0, 0
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[networktypes.Summary]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
		inUse += result.Value.Counts.Inuse
		unused += result.Value.Counts.Unused
		total += result.Value.Counts.Total
	}
	data, resp := pagination.MergeEnvironmentPages(s.buildNetworkPaginationConfig(), pages, params)

	return base.PaginatedWithCounts[networktypes.Summary, networktypes.UsageCounts]{
		Success: true,
		Data:    data,
		Counts: networktypes.UsageCounts{
			Inuse:  inUse,
			Unused: unused,
			Total:  total,
		},
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *NetworkService) buildNetworkPaginationConfig() pagination.Config[networktypes.Summary] {
	return pagination.Config[networktypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[networktypes.Summary]{
			func(n networktypes.Summary) (string, error) { return n.Name, nil },
			func(n networktypes.Summary) (string, error) { return n.Driver, nil },
			func(n networktypes.Summary) (string, error) { return n.Scope, nil },
			func(n networktypes.Summary) (string, error) { return n.ID, nil },
			func(n networktypes.Summary) (string, error) {
				subnets, gateways := ipamValues(n)
				return strings.Join(append(subnets, gateways...), " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[networktypes.Summary]{
			{
				Key: "name",
				// Docker allows duplicate network names, so tie-break on ID to keep
				// pagination deterministic.
				Fn: func(a, b networktypes.Summary) int {
					return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
				},
			},
			{
				Key: "driver",
				Fn:  func(a, b networktypes.Summary) int { return strings.Compare(a.Driver, b.Driver) },
			},
			{
				Key: "scope",
				Fn:  func(a, b networktypes.Summary) int { return strings.Compare(a.Scope, b.Scope) },
			},
			{
				Key: "created",
				Fn:  func(a, b networktypes.Summary) int { return a.Created.Compare(b.Created) },
			},
			{
				Key: "inUse",
				Fn: func(a, b networktypes.Summary) int {
					if (a.InUse || a.IsDefault) == (b.InUse || b.IsDefault) {
						// Use name as secondary sort key for consistent ordering
						return strings.Compare(a.Name, b.Name)
					}
					return kit.Ternary(a.InUse || a.IsDefault, -1, 1)
				},
			},
			// Subnet and gateway compare sorted copies so IPAM entry order does
			// not affect row order; the displayed entries are untouched.
			{
				Key: "subnet",
				Fn: func(a, b networktypes.Summary) int {
					subnetsA, _ := ipamValues(a)
					subnetsB, _ := ipamValues(b)
					return slices.CompareFunc(
						slices.SortedFunc(slices.Values(subnetsA), kit.CompareSubnets),
						slices.SortedFunc(slices.Values(subnetsB), kit.CompareSubnets),
						kit.CompareSubnets,
					)
				},
			},
			{
				Key: "gateway",
				Fn: func(a, b networktypes.Summary) int {
					_, gatewaysA := ipamValues(a)
					_, gatewaysB := ipamValues(b)
					return slices.CompareFunc(
						slices.SortedFunc(slices.Values(gatewaysA), kit.CompareAddresses),
						slices.SortedFunc(slices.Values(gatewaysB), kit.CompareAddresses),
						kit.CompareAddresses,
					)
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[networktypes.Summary]{
			{
				Key: "inUse",
				Fn: func(n networktypes.Summary, filterValue string) bool {
					return kit.Ternary(filterValue == "true", n.InUse || n.IsDefault, filterValue != "false" || (!n.InUse && !n.IsDefault))
				},
			},
		},
	}
}

func ipamValues(n networktypes.Summary) (subnets, gateways []string) {
	for _, cfg := range n.IPAM.Config {
		if cfg.Subnet != "" {
			subnets = append(subnets, cfg.Subnet)
		}
		if cfg.Gateway != "" {
			gateways = append(gateways, cfg.Gateway)
		}
	}
	return subnets, gateways
}
