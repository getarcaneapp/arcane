package services

import (
	"cmp"
	"context"
	stdjson "encoding/json"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"

	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

// Service manages swarm services.
type Service struct {
	docker        *dockerInternal.DockerClientService
	ensureManager func(ctx context.Context) error
	listTasks     func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error)
}

func NewService(
	dockerService *dockerInternal.DockerClientService,
	ensureManager func(ctx context.Context) error,
	listTasks func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error),
) *Service {
	return &Service{docker: dockerService, ensureManager: ensureManager, listTasks: listTasks}
}

func (s *Service) ListServicesPaginated(ctx context.Context, params pagination.QueryParams) ([]types.SwarmServiceSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	servicesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceListResult, error) {
		return dockerClient.ServiceList(ctx, client.ServiceListOptions{Status: true})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	return s.PaginateSummaries(ctx, servicesResult.Items, params)
}

// PaginateSummaries summarizes services with their running nodes and networks, then paginates them.
func (s *Service) PaginateSummaries(
	ctx context.Context,
	services []swarm.Service,
	params pagination.QueryParams,
) ([]types.SwarmServiceSummary, pagination.Response, error) {
	summaries, err := s.summarizeServices(ctx, services)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	config := pagination.Config[types.SwarmServiceSummary]{
		SearchAccessors: []pagination.SearchAccessor[types.SwarmServiceSummary]{
			func(svc types.SwarmServiceSummary) (string, error) { return svc.Name, nil },
			func(svc types.SwarmServiceSummary) (string, error) { return svc.Image, nil },
			func(svc types.SwarmServiceSummary) (string, error) { return svc.ID, nil },
			func(svc types.SwarmServiceSummary) (string, error) { return svc.StackName, nil },
			func(svc types.SwarmServiceSummary) (string, error) { return svc.Mode, nil },
			func(svc types.SwarmServiceSummary) (string, error) {
				return strings.Join(svc.Networks, " "), nil
			},
			func(svc types.SwarmServiceSummary) (string, error) {
				return strings.Join(svc.Nodes, " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[types.SwarmServiceSummary]{
			{Key: "name", Fn: func(a, b types.SwarmServiceSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "image", Fn: func(a, b types.SwarmServiceSummary) int { return strings.Compare(a.Image, b.Image) }},
			{Key: "mode", Fn: func(a, b types.SwarmServiceSummary) int { return strings.Compare(a.Mode, b.Mode) }},
			{Key: "replicas", Fn: func(a, b types.SwarmServiceSummary) int {
				if localCmp := cmp.Compare(a.Replicas, b.Replicas); localCmp != 0 {
					return localCmp
				}
				return cmp.Compare(a.RunningReplicas, b.RunningReplicas)
			}},
			{Key: "created", Fn: func(a, b types.SwarmServiceSummary) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b types.SwarmServiceSummary) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
	result := config.SearchOrderAndPaginate(summaries, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) GetService(ctx context.Context, serviceID string) (*types.SwarmServiceInspect, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	serviceResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceInspectResult, error) {
		return dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}
	service := serviceResult.Service

	inspect := types.NewSwarmServiceInspect(service)
	inspect.Nodes = s.resolveServiceNodeNames(ctx, serviceID)
	inspect.NetworkDetails = s.enrichServiceNetworkDetails(ctx, service.Spec.TaskTemplate.Networks)
	inspect.Mounts = s.enrichServiceMounts(ctx, service.Spec.TaskTemplate.ContainerSpec)

	return &inspect, nil
}

func (s *Service) resolveServiceNodeNames(ctx context.Context, serviceID string) []string {
	nodesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeListResult, error) {
		return dockerClient.NodeList(ctx, client.NodeListOptions{})
	})
	if err != nil {
		return nil
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	tasksResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.TaskListResult, error) {
		return dockerClient.TaskList(ctx, client.TaskListOptions{})
	})
	if err != nil {
		return nil
	}

	nodeSet := make(map[string]struct{})
	for _, task := range tasksResult.Items {
		if task.ServiceID != serviceID || task.Status.State != swarm.TaskStateRunning {
			continue
		}
		if name, ok := nodeNameByID[task.NodeID]; ok {
			nodeSet[name] = struct{}{}
		}
	}

	nodeNames := slices.Sorted(maps.Keys(nodeSet))
	return nodeNames
}

func (s *Service) enrichServiceNetworkDetails(
	ctx context.Context,
	networkConfigs []swarm.NetworkAttachmentConfig,
) map[string]types.SwarmServiceNetworkDetail {
	if len(networkConfigs) == 0 {
		return nil
	}

	details := make(map[string]types.SwarmServiceNetworkDetail, len(networkConfigs))
	for _, networkConfig := range networkConfigs {
		networkID := networkConfig.Target
		netInspectResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NetworkInspectResult, error) {
			return dockerClient.NetworkInspect(ctx, networkID, client.NetworkInspectOptions{})
		})
		if err != nil {
			continue
		}

		networkInfo := netInspectResult.Network
		detail := types.SwarmServiceNetworkDetail{
			ID:         networkInfo.ID,
			Name:       networkInfo.Name,
			Driver:     networkInfo.Driver,
			Scope:      networkInfo.Scope,
			Internal:   networkInfo.Internal,
			Attachable: networkInfo.Attachable,
			Ingress:    networkInfo.Ingress,
			EnableIPv4: networkInfo.EnableIPv4,
			EnableIPv6: networkInfo.EnableIPv6,
			ConfigOnly: networkInfo.ConfigOnly,
			Options:    networkInfo.Options,
			ConfigFrom: networkInfo.ConfigFrom.Network,
		}

		for _, ipamCfg := range networkInfo.IPAM.Config {
			detail.IPAMConfigs = append(detail.IPAMConfigs, toServiceNetworkIPAMConfig(ipamCfg))
		}

		if detail.ConfigFrom != "" {
			configInspectResult, networkInspectErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NetworkInspectResult, error) {
				return dockerClient.NetworkInspect(ctx, detail.ConfigFrom, client.NetworkInspectOptions{})
			})
			if networkInspectErr == nil {
				configNetwork := configInspectResult.Network
				configDetail := &types.SwarmServiceNetworkConfigDetail{
					Name:       configNetwork.Name,
					Driver:     configNetwork.Driver,
					Scope:      configNetwork.Scope,
					EnableIPv4: configNetwork.EnableIPv4,
					EnableIPv6: configNetwork.EnableIPv6,
					Options:    configNetwork.Options,
				}
				for _, ipamCfg := range configNetwork.IPAM.Config {
					converted := toServiceNetworkIPAMConfig(ipamCfg)
					if ipamCfg.Subnet.IsValid() && ipamCfg.Subnet.Addr().Is6() {
						configDetail.IPv6Configs = append(configDetail.IPv6Configs, converted)
						continue
					}
					configDetail.IPv4Configs = append(configDetail.IPv4Configs, converted)
				}
				detail.ConfigNetwork = configDetail
			}
		}

		details[networkID] = detail
	}

	return details
}

func (s *Service) enrichServiceMounts(
	ctx context.Context,
	containerSpec *swarm.ContainerSpec,
) []types.SwarmServiceMount {
	if containerSpec == nil {
		return nil
	}

	mounts := make([]types.SwarmServiceMount, 0, len(containerSpec.Mounts))
	for _, serviceMount := range containerSpec.Mounts {
		volumeInfo := func() client.VolumeInspectResult {
			if serviceMount.Type != "volume" || serviceMount.Source == "" {
				return client.VolumeInspectResult{}
			}
			result, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.VolumeInspectResult, error) {
				return dockerClient.VolumeInspect(ctx, serviceMount.Source, client.VolumeInspectOptions{})
			})
			if err != nil {
				return client.VolumeInspectResult{}
			}
			return result
		}().Volume
		mounts = append(mounts, types.SwarmServiceMount{
			Type:          string(serviceMount.Type),
			Source:        serviceMount.Source,
			Target:        serviceMount.Target,
			ReadOnly:      serviceMount.ReadOnly,
			VolumeDriver:  volumeInfo.Driver,
			VolumeOptions: volumeInfo.Options,
			DevicePath:    volumeInfo.Mountpoint,
		})
	}

	return mounts
}

func (s *Service) CreateService(ctx context.Context, req types.SwarmServiceCreateRequest) (*types.SwarmServiceCreateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	// Unmarshal spec from JSON
	var spec swarm.ServiceSpec
	// Docker encodes durations as integer nanoseconds.
	if unmarshalErr := json.Unmarshal(req.Spec, &spec, stdjson.FormatDurationAsNano(true)); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse service spec: %w", unmarshalErr)
	}

	optionsPayload := kit.FromPtr(req.Options)

	// Sanitize spec to avoid empty UID/GID in secret/config refs
	sanitizeServiceSpec(&spec)

	resp, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceCreateResult, error) {
		return dockerClient.ServiceCreate(ctx, client.ServiceCreateOptions{
			Spec:                spec,
			EncodedRegistryAuth: optionsPayload.EncodedRegistryAuth,
			QueryRegistry:       optionsPayload.QueryRegistry,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm service: %w", err)
	}

	return &types.SwarmServiceCreateResponse{
		ID:       resp.ID,
		Warnings: resp.Warnings,
	}, nil
}

func (s *Service) UpdateService(ctx context.Context, serviceID string, req types.SwarmServiceUpdateRequest) (*types.SwarmServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	versionIndex := req.Version
	if versionIndex == 0 {
		serviceResult, serviceInspectErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceInspectResult, error) {
			return dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
		})
		if serviceInspectErr != nil {
			return nil, fmt.Errorf("failed to inspect swarm service: %w", serviceInspectErr)
		}
		versionIndex = serviceResult.Service.Version.Index
	}

	optionsPayload := kit.FromPtr(req.Options)

	// Sanitize spec to avoid empty UID/GID in secret/config refs
	sanitizeServiceSpec(&req.Spec)

	resp, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceUpdateResult, error) {
		return dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
			Version:             swarm.Version{Index: versionIndex},
			Spec:                req.Spec,
			EncodedRegistryAuth: optionsPayload.EncodedRegistryAuth,
			RegistryAuthFrom:    optionsPayload.RegistryAuthFrom,
			Rollback:            optionsPayload.Rollback,
			QueryRegistry:       optionsPayload.QueryRegistry,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update swarm service: %w", err)
	}

	return &types.SwarmServiceUpdateResponse{
		Warnings: resp.Warnings,
	}, nil
}

func (s *Service) RemoveService(ctx context.Context, serviceID string) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	if _, serviceRemoveErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceRemoveResult, error) {
		return dockerClient.ServiceRemove(ctx, serviceID, client.ServiceRemoveOptions{})
	}); serviceRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm service: %w", serviceRemoveErr)
	}

	return nil
}

func (s *Service) StreamServiceLogs(ctx context.Context, serviceID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.docker.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	options := client.ServiceLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
		Details:    true,
	}

	logs, err := dockerClient.ServiceLogs(ctx, serviceID, options)
	if err != nil {
		return fmt.Errorf("failed to get service logs: %w", err)
	}
	defer func() { _ = logs.Close() }()

	if follow {
		return docker.StreamMultiplexedLogs(ctx, logs, logsChan)
	}

	return docker.ReadAllLogs(ctx, logs, logsChan)
}

func (s *Service) ListServiceTasksPaginated(ctx context.Context, serviceID string, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(client.Filters)
	filters.Add("service", serviceID)
	return s.listTasks(ctx, filters, params)
}

func (s *Service) RollbackService(ctx context.Context, serviceID string) (*types.SwarmServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	serviceResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceInspectResult, error) {
		return dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}

	updateResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceUpdateResult, error) {
		return dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
			Version:  serviceResult.Service.Version,
			Spec:     serviceResult.Service.Spec,
			Rollback: "previous",
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rollback swarm service: %w", err)
	}

	return &types.SwarmServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *Service) ScaleService(ctx context.Context, serviceID string, replicas uint64) (*types.SwarmServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	serviceResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceInspectResult, error) {
		return dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}
	service := serviceResult.Service

	switch {
	case service.Spec.Mode.Replicated != nil:
		service.Spec.Mode.Replicated.Replicas = &replicas
	case service.Spec.Mode.ReplicatedJob != nil:
		service.Spec.Mode.ReplicatedJob.TotalCompletions = &replicas
	default:
		return nil, fmt.Errorf("scale can only be used with replicated or replicated-job mode: %w", errdefs.ErrInvalidArgument)
	}

	updateResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceUpdateResult, error) {
		return dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
			Version: service.Version,
			Spec:    service.Spec,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scale swarm service: %w", err)
	}

	return &types.SwarmServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *Service) summarizeServices(ctx context.Context, services []swarm.Service) ([]types.SwarmServiceSummary, error) {
	nodesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeListResult, error) {
		return dockerClient.NodeList(ctx, client.NodeListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	networksResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NetworkListResult, error) {
		return dockerClient.NetworkList(ctx, client.NetworkListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	networkNameByID := make(map[string]string, len(networksResult.Items))
	for _, network := range networksResult.Items {
		networkNameByID[network.ID] = network.Name
	}

	serviceIDs := make(map[string]struct{}, len(services))
	for _, service := range services {
		serviceIDs[service.ID] = struct{}{}
	}

	tasksResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.TaskListResult, error) {
		return dockerClient.TaskList(ctx, client.TaskListOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm tasks: %w", err)
	}

	serviceNodes := make(map[string]map[string]struct{})
	for _, task := range tasksResult.Items {
		if string(task.Status.State) != "running" {
			continue
		}
		if _, ok := serviceIDs[task.ServiceID]; !ok {
			continue
		}
		if _, ok := serviceNodes[task.ServiceID]; !ok {
			serviceNodes[task.ServiceID] = make(map[string]struct{})
		}
		if nodeName, ok := nodeNameByID[task.NodeID]; ok {
			serviceNodes[task.ServiceID][nodeName] = struct{}{}
		}
	}

	summaries := make([]types.SwarmServiceSummary, 0, len(services))
	for _, service := range services {
		var nodeNames []string
		if nodeSet, ok := serviceNodes[service.ID]; ok {
			nodeNames = make([]string, 0, len(nodeSet))
			for nodeName := range nodeSet {
				nodeNames = append(nodeNames, nodeName)
			}
			sort.Strings(nodeNames)
		}
		summaries = append(summaries, types.NewSwarmServiceSummary(service, nodeNames, networkNameByID))
	}

	return summaries, nil
}

func sanitizeServiceSpec(spec *swarm.ServiceSpec) {
	if spec == nil || spec.TaskTemplate.ContainerSpec == nil {
		return
	}
	// Secret and config file targets default to root ownership.
	defaultOwner := func(ids ...*string) {
		for _, id := range ids {
			if strings.TrimSpace(*id) == "" {
				*id = "0"
			}
		}
	}
	for _, ref := range spec.TaskTemplate.ContainerSpec.Secrets {
		if ref != nil && ref.File != nil {
			defaultOwner(&ref.File.UID, &ref.File.GID)
		}
	}
	for _, ref := range spec.TaskTemplate.ContainerSpec.Configs {
		if ref != nil && ref.File != nil {
			defaultOwner(&ref.File.UID, &ref.File.GID)
		}
	}
}

func toServiceNetworkIPAMConfig(cfg network.IPAMConfig) types.SwarmServiceNetworkIPAMConfig {
	return types.SwarmServiceNetworkIPAMConfig{
		Subnet:  kit.Ternary(cfg.Subnet.IsValid(), cfg.Subnet.String(), ""),
		Gateway: kit.Ternary(cfg.Gateway.IsValid(), cfg.Gateway.String(), ""),
		IPRange: kit.Ternary(cfg.IPRange.IsValid(), cfg.IPRange.String(), ""),
	}
}
