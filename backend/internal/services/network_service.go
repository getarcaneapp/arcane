package services

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"

	dockersdkclient "github.com/docker/go-sdk/client"
	dockersdknetwork "github.com/docker/go-sdk/network"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	networktypes "github.com/getarcaneapp/arcane/types/network"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	client "github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
)

var dockerSDKNetworkNewInternal = dockersdknetwork.New

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
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInspect, err := libarcane.NetworkInspectWithCompatibility(ctx, dockerClient, id, client.NetworkInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}

	return new(networkInspect.Network), nil
}

func (s *NetworkService) GetNetworkTopology(ctx context.Context) (*networktypes.Topology, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containerInfoByID := buildTopologyContainerInfoInternal(containerList.Items)

	networkList, err := libarcane.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Docker networks: %w", err)
	}

	topology := &networktypes.Topology{
		Nodes: make([]networktypes.TopologyNode, 0, len(networkList.Items)),
		Edges: make([]networktypes.TopologyEdge, 0),
	}
	containerNodeIDs := make(map[string]struct{})
	inspectedNetworks := make([]struct {
		summary network.Summary
		inspect network.Inspect
	}, len(networkList.Items))

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for i := range networkList.Items {
		rawNetwork := networkList.Items[i]
		g.Go(func() error {
			inspected, err := libarcane.NetworkInspectWithCompatibility(groupCtx, dockerClient, rawNetwork.ID, client.NetworkInspectOptions{})
			if err != nil {
				return fmt.Errorf("failed to inspect network %s: %w", rawNetwork.Name, err)
			}

			inspectedNetworks[i] = struct {
				summary network.Summary
				inspect network.Inspect
			}{
				summary: rawNetwork,
				inspect: inspected.Network,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	for _, inspectedNetwork := range inspectedNetworks {
		rawNetwork := inspectedNetwork.summary
		topology.Nodes = append(topology.Nodes, networktypes.TopologyNode{
			ID:   rawNetwork.ID,
			Name: rawNetwork.Name,
			Type: networktypes.TopologyNodeTypeNetwork,
			Metadata: networktypes.TopologyNodeMetadata{
				Driver:    rawNetwork.Driver,
				Scope:     rawNetwork.Scope,
				IsDefault: dockerutil.IsDefaultNetwork(rawNetwork.Name),
			},
		})

		for containerID, endpoint := range inspectedNetwork.inspect.Containers {
			info, ok := containerInfoByID[containerID]
			if !ok {
				info = topologyContainerInfo{Name: endpoint.Name}
			}
			if info.Name == "" {
				info.Name = endpoint.Name
			}

			if _, exists := containerNodeIDs[containerID]; !exists {
				topology.Nodes = append(topology.Nodes, networktypes.TopologyNode{
					ID:   containerID,
					Name: info.Name,
					Type: networktypes.TopologyNodeTypeContainer,
					Metadata: networktypes.TopologyNodeMetadata{
						Status: info.State,
						Image:  info.Image,
					},
				})
				containerNodeIDs[containerID] = struct{}{}
			}

			edge := networktypes.TopologyEdge{
				ID:     fmt.Sprintf("%s:%s", rawNetwork.ID, containerID),
				Source: rawNetwork.ID,
				Target: containerID,
			}
			if endpoint.IPv4Address.IsValid() {
				edge.IPv4Address = endpoint.IPv4Address.String()
			}
			if endpoint.IPv6Address.IsValid() {
				edge.IPv6Address = endpoint.IPv6Address.String()
			}

			topology.Edges = append(topology.Edges, edge)
		}
	}

	sort.Slice(topology.Nodes, func(i, j int) bool {
		left, right := topology.Nodes[i], topology.Nodes[j]
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	sort.Slice(topology.Edges, func(i, j int) bool {
		left, right := topology.Edges[i], topology.Edges[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Target < right.Target
	})

	return topology, nil
}

func (s *NetworkService) CreateNetwork(ctx context.Context, name string, createOptions networktypes.CreateOptions, user models.User) (*network.CreateResponse, error) {
	options := toDockerNetworkCreateOptionsInternal(createOptions)
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", err, models.JSON{"action": "create", "driver": options.Driver})
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	response := network.CreateResponse{}
	if sdkClient, sdkErr := s.dockerService.GetSDKClient(ctx); sdkErr == nil && canUseSDKNetworkCreateInternal(options) {
		createdNetwork, sdkCreateErr := dockerSDKNetworkNewInternal(ctx, buildSDKNetworkCreateOptionsInternal(sdkClient, name, options)...)
		if sdkCreateErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", sdkCreateErr, models.JSON{"action": "create", "driver": options.Driver})
			return nil, fmt.Errorf("failed to create network: %w", sdkCreateErr)
		}
		response.ID = createdNetwork.ID()
	} else {
		createResult, createErr := dockerClient.NetworkCreate(ctx, name, options)
		if createErr != nil {
			s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", "", name, user.ID, user.Username, "0", createErr, models.JSON{"action": "create", "driver": options.Driver})
			return nil, fmt.Errorf("failed to create network: %w", createErr)
		}
		response = network.CreateResponse{
			ID:      createResult.ID,
			Warning: strings.Join(createResult.Warning, "; "),
		}
	}

	metadata := models.JSON{
		"action": "create",
		"driver": options.Driver,
		"name":   name,
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkCreate, response.ID, name, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log network creation action", "error", logErr)
	}

	return &response, nil
}

func toDockerNetworkCreateOptionsInternal(src networktypes.CreateOptions) client.NetworkCreateOptions {
	opts := client.NetworkCreateOptions{
		Driver:     src.Driver,
		Internal:   src.Internal,
		Attachable: src.Attachable,
		Ingress:    src.Ingress,
		EnableIPv6: boolPtrIfTrueInternal(src.EnableIPv6),
		Options:    src.Options,
		Labels:     src.Labels,
	}

	if src.IPAM != nil {
		opts.IPAM = toDockerNetworkIPAMInternal(src.IPAM)
	}

	return opts
}

func boolPtrIfTrueInternal(v bool) *bool {
	if !v {
		return nil
	}

	return &v
}

func toDockerNetworkIPAMInternal(src *networktypes.IPAM) *network.IPAM {
	dockerIPAM := &network.IPAM{
		Driver:  src.Driver,
		Options: src.Options,
	}

	for _, cfg := range src.Config {
		if dockerCfg, ok := toDockerNetworkIPAMConfigInternal(cfg); ok {
			dockerIPAM.Config = append(dockerIPAM.Config, dockerCfg)
		}
	}

	return dockerIPAM
}

func toDockerNetworkIPAMConfigInternal(cfg networktypes.IPAMConfig) (network.IPAMConfig, bool) {
	dockerCfg := network.IPAMConfig{}
	hasAny := false

	if parsed, ok := parseNetworkPrefixInternal(cfg.Subnet); ok {
		dockerCfg.Subnet = parsed
		hasAny = true
	}
	if parsed, ok := parseNetworkAddrInternal(cfg.Gateway); ok {
		dockerCfg.Gateway = parsed
		hasAny = true
	}
	if parsed, ok := parseNetworkPrefixInternal(cfg.IPRange); ok {
		dockerCfg.IPRange = parsed
		hasAny = true
	}
	if aux := parseNetworkAuxAddressInternal(cfg.AuxAddress); len(aux) > 0 {
		dockerCfg.AuxAddress = aux
		hasAny = true
	}

	return dockerCfg, hasAny
}

func parseNetworkPrefixInternal(raw string) (netip.Prefix, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return netip.Prefix{}, false
	}

	return prefix, true
}

func parseNetworkAddrInternal(raw string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return netip.Addr{}, false
	}

	return addr, true
}

func parseNetworkAuxAddressInternal(auxAddress map[string]string) map[string]netip.Addr {
	if len(auxAddress) == 0 {
		return nil
	}

	aux := make(map[string]netip.Addr, len(auxAddress))
	for key, rawAddr := range auxAddress {
		if parsed, ok := parseNetworkAddrInternal(rawAddr); ok {
			aux[key] = parsed
		}
	}

	return aux
}

func canUseSDKNetworkCreateInternal(options client.NetworkCreateOptions) bool {
	if options.Ingress {
		return false
	}
	if len(options.Options) > 0 {
		return false
	}

	return true
}

func buildSDKNetworkCreateOptionsInternal(sdkClient dockersdkclient.SDKClient, name string, options client.NetworkCreateOptions) []dockersdknetwork.Option {
	createOptions := []dockersdknetwork.Option{
		dockersdknetwork.WithClient(sdkClient),
		dockersdknetwork.WithName(name),
	}

	if driver := strings.TrimSpace(options.Driver); driver != "" {
		createOptions = append(createOptions, dockersdknetwork.WithDriver(driver))
	}
	if options.Internal {
		createOptions = append(createOptions, dockersdknetwork.WithInternal())
	}
	if options.Attachable {
		createOptions = append(createOptions, dockersdknetwork.WithAttachable())
	}
	if options.EnableIPv6 != nil && *options.EnableIPv6 {
		createOptions = append(createOptions, dockersdknetwork.WithEnableIPv6())
	}
	if options.IPAM != nil {
		createOptions = append(createOptions, dockersdknetwork.WithIPAM(options.IPAM))
	}
	if len(options.Labels) > 0 {
		createOptions = append(createOptions, dockersdknetwork.WithLabels(options.Labels))
	}

	return createOptions
}

func (s *NetworkService) RemoveNetwork(ctx context.Context, id string, user models.User) error {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", id, "", user.ID, user.Username, "0", err, models.JSON{"action": "delete"})
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	networkInfo, err := libarcane.NetworkInspectWithCompatibility(ctx, dockerClient, id, client.NetworkInspectOptions{})
	var networkName string
	if err == nil {
		networkName = networkInfo.Network.Name
	} else {
		networkName = id
	}

	if _, err := dockerClient.NetworkRemove(ctx, id, client.NetworkRemoveOptions{}); err != nil {
		s.eventService.LogErrorEvent(ctx, models.EventTypeNetworkError, "network", id, networkName, user.ID, user.Username, "0", err, models.JSON{"action": "delete"})
		return fmt.Errorf("failed to remove network: %w", err)
	}

	metadata := models.JSON{
		"action":    "delete",
		"networkId": id,
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkDelete, id, networkName, user.ID, user.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log network delete action", "error", logErr)
	}

	return nil
}

func (s *NetworkService) PruneNetworks(ctx context.Context) (*network.PruneReport, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	filterArgs := make(client.Filters)

	report, err := dockerClient.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: filterArgs})
	if err != nil {
		return nil, fmt.Errorf("failed to prune networks: %w", err)
	}
	pruneReport := report.Report

	metadata := models.JSON{
		"action":          "prune",
		"networksDeleted": len(pruneReport.NetworksDeleted),
	}
	if logErr := s.eventService.LogNetworkEvent(ctx, models.EventTypeNetworkDelete, "", "bulk_prune", systemUser.ID, systemUser.Username, "0", metadata); logErr != nil {
		slog.WarnContext(ctx, "could not log network prune action", "error", logErr)
	}

	return &pruneReport, nil
}

func (s *NetworkService) ListNetworksPaginated(ctx context.Context, params pagination.QueryParams) ([]networktypes.Summary, pagination.Response, networktypes.UsageCounts, error) {
	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	containerList, err := dockerClient.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to list containers: %w", err)
	}
	containers := containerList.Items

	inUseByID, inUseByName := s.buildNetworkUsageMaps(containers)

	networkList, err := libarcane.NetworkListWithCompatibility(ctx, dockerClient, client.NetworkListOptions{})
	if err != nil {
		return nil, pagination.Response{}, networktypes.UsageCounts{}, fmt.Errorf("failed to list Docker networks: %w", err)
	}
	rawNets := networkList.Items

	items := s.convertToNetworkSummaries(rawNets, inUseByID, inUseByName)
	config := s.buildNetworkPaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	counts := s.calculateNetworkUsageCounts(items)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

	return result.Items, paginationResp, counts, nil
}

func (s *NetworkService) buildNetworkUsageMaps(containers []container.Summary) (map[string]bool, map[string]bool) {
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
	return inUseByID, inUseByName
}

type topologyContainerInfo struct {
	Name  string
	Image string
	State string
}

func buildTopologyContainerInfoInternal(containers []container.Summary) map[string]topologyContainerInfo {
	infoByID := make(map[string]topologyContainerInfo, len(containers))
	for _, rawContainer := range containers {
		name := rawContainer.ID
		if len(rawContainer.Names) > 0 {
			name = strings.TrimPrefix(rawContainer.Names[0], "/")
		}
		infoByID[rawContainer.ID] = topologyContainerInfo{
			Name:  name,
			Image: rawContainer.Image,
			State: string(rawContainer.State),
		}
	}
	return infoByID
}

func (s *NetworkService) convertToNetworkSummaries(rawNets []network.Summary, inUseByID, inUseByName map[string]bool) []networktypes.Summary {
	items := make([]networktypes.Summary, 0, len(rawNets))
	for _, n := range rawNets {
		netDto := networktypes.NewSummary(n)
		netDto.InUse = inUseByID[netDto.ID] || inUseByName[netDto.Name]
		netDto.IsDefault = dockerutil.IsDefaultNetwork(netDto.Name)
		items = append(items, netDto)
	}
	return items
}

func (s *NetworkService) buildNetworkPaginationConfig() pagination.Config[networktypes.Summary] {
	return pagination.Config[networktypes.Summary]{
		SearchAccessors: []pagination.SearchAccessor[networktypes.Summary]{
			func(n networktypes.Summary) (string, error) { return n.Name, nil },
			func(n networktypes.Summary) (string, error) { return n.Driver, nil },
			func(n networktypes.Summary) (string, error) { return n.Scope, nil },
			func(n networktypes.Summary) (string, error) { return n.ID, nil },
		},
		SortBindings:    s.buildNetworkSortBindings(),
		FilterAccessors: s.buildNetworkFilterAccessors(),
	}
}

func (s *NetworkService) buildNetworkSortBindings() []pagination.SortBinding[networktypes.Summary] {
	return []pagination.SortBinding[networktypes.Summary]{
		{
			Key: "name",
			Fn:  func(a, b networktypes.Summary) int { return strings.Compare(a.Name, b.Name) },
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
			Fn:  s.compareNetworkCreated,
		},
		{
			Key: "inUse",
			Fn:  s.compareNetworkInUse,
		},
	}
}

func (s *NetworkService) compareNetworkCreated(a, b networktypes.Summary) int {
	if a.Created.Before(b.Created) {
		return -1
	}
	if a.Created.After(b.Created) {
		return 1
	}
	return 0
}

func (s *NetworkService) compareNetworkInUse(a, b networktypes.Summary) int {
	aInUse := a.InUse || a.IsDefault
	bInUse := b.InUse || b.IsDefault

	if aInUse == bInUse {
		// Use name as secondary sort key for consistent ordering
		return strings.Compare(a.Name, b.Name)
	}
	if aInUse {
		return -1
	}
	return 1
}

func (s *NetworkService) buildNetworkFilterAccessors() []pagination.FilterAccessor[networktypes.Summary] {
	return []pagination.FilterAccessor[networktypes.Summary]{
		{
			Key: "inUse",
			Fn: func(n networktypes.Summary, filterValue string) bool {
				if filterValue == "true" {
					return n.InUse || n.IsDefault
				}
				if filterValue == "false" {
					return !n.InUse && !n.IsDefault
				}
				return true
			},
		},
	}
}

func (s *NetworkService) calculateNetworkUsageCounts(items []networktypes.Summary) networktypes.UsageCounts {
	counts := networktypes.UsageCounts{
		Total: len(items),
	}
	for _, n := range items {
		if n.InUse || n.IsDefault {
			counts.Inuse++
		} else {
			// Only count non-default networks as unused
			// Default networks (bridge, host, none, ingress) are never "unused"
			counts.Unused++
		}
	}
	return counts
}
