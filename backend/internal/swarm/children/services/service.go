package services

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

// Service manages swarm services.
type Service struct {
	dockerClient  func(ctx context.Context) (*client.Client, error)
	ensureManager func(ctx context.Context) error
	listTasks     func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error)
}

func NewService(
	dockerClient func(ctx context.Context) (*client.Client, error),
	ensureManager func(ctx context.Context) error,
	listTasks func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error),
) *Service {
	return &Service{dockerClient: dockerClient, ensureManager: ensureManager, listTasks: listTasks}
}

func (s *Service) ListServicesPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.ServiceSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	servicesResult, err := dockerClient.ServiceList(ctx, client.ServiceListOptions{Status: true})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	return s.PaginateSummaries(ctx, dockerClient, servicesResult.Items, params)
}

// PaginateSummaries summarizes services with their running nodes and networks, then paginates them.
func (s *Service) PaginateSummaries(
	ctx context.Context,
	dockerClient *client.Client,
	services []swarm.Service,
	params pagination.QueryParams,
) ([]swarmtypes.ServiceSummary, pagination.Response, error) {
	summaries, err := s.summarizeServicesInternal(ctx, dockerClient, services)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	result := s.buildServicePaginationConfigInternal().SearchOrderAndPaginate(summaries, params)
	return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
}

func (s *Service) GetService(ctx context.Context, serviceID string) (*swarmtypes.ServiceInspect, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}
	service := serviceResult.Service

	inspect := swarmtypes.NewServiceInspect(service)
	inspect.Nodes = s.resolveServiceNodeNamesInternal(ctx, dockerClient, serviceID)
	inspect.NetworkDetails = s.enrichServiceNetworkDetailsInternal(ctx, dockerClient, service.Spec.TaskTemplate.Networks)
	inspect.Mounts = s.enrichServiceMountsInternal(ctx, dockerClient, service.Spec.TaskTemplate.ContainerSpec)

	return &inspect, nil
}

func (s *Service) resolveServiceNodeNamesInternal(ctx context.Context, dockerClient *client.Client, serviceID string) []string {
	nodesResult, err := dockerClient.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	tasksResult, err := dockerClient.TaskList(ctx, client.TaskListOptions{})
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

func (s *Service) enrichServiceNetworkDetailsInternal(
	ctx context.Context,
	dockerClient *client.Client,
	networkConfigs []swarm.NetworkAttachmentConfig,
) map[string]swarmtypes.ServiceNetworkDetail {
	if len(networkConfigs) == 0 {
		return nil
	}

	details := make(map[string]swarmtypes.ServiceNetworkDetail, len(networkConfigs))
	for _, networkConfig := range networkConfigs {
		networkID := networkConfig.Target
		netInspectResult, err := dockerClient.NetworkInspect(ctx, networkID, client.NetworkInspectOptions{})
		if err != nil {
			continue
		}

		networkInfo := netInspectResult.Network
		detail := swarmtypes.ServiceNetworkDetail{
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
		}
		detail.ConfigFrom = cmp.Or(networkInfo.ConfigFrom.Network, detail.ConfigFrom)

		for _, ipamCfg := range networkInfo.IPAM.Config {
			detail.IPAMConfigs = append(detail.IPAMConfigs, toServiceNetworkIPAMConfigInternal(ipamCfg))
		}

		if detail.ConfigFrom != "" {
			configInspectResult, networkInspectErr := dockerClient.NetworkInspect(ctx, detail.ConfigFrom, client.NetworkInspectOptions{})
			if networkInspectErr == nil {
				configNetwork := configInspectResult.Network
				configDetail := &swarmtypes.ServiceNetworkConfigDetail{
					Name:       configNetwork.Name,
					Driver:     configNetwork.Driver,
					Scope:      configNetwork.Scope,
					EnableIPv4: configNetwork.EnableIPv4,
					EnableIPv6: configNetwork.EnableIPv6,
					Options:    configNetwork.Options,
				}
				for _, ipamCfg := range configNetwork.IPAM.Config {
					converted := toServiceNetworkIPAMConfigInternal(ipamCfg)
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

func (s *Service) enrichServiceMountsInternal(
	ctx context.Context,
	dockerClient *client.Client,
	containerSpec *swarm.ContainerSpec,
) []swarmtypes.ServiceMount {
	if containerSpec == nil {
		return nil
	}

	mounts := make([]swarmtypes.ServiceMount, 0, len(containerSpec.Mounts))
	for _, serviceMount := range containerSpec.Mounts {
		mount := swarmtypes.ServiceMount{
			Type:     string(serviceMount.Type),
			Source:   serviceMount.Source,
			Target:   serviceMount.Target,
			ReadOnly: serviceMount.ReadOnly,
		}
		if serviceMount.Type == "volume" && serviceMount.Source != "" {
			volInspectResult, err := dockerClient.VolumeInspect(ctx, serviceMount.Source, client.VolumeInspectOptions{})
			if err == nil {
				volume := volInspectResult.Volume
				mount.VolumeDriver = volume.Driver
				mount.VolumeOptions = volume.Options
				mount.DevicePath = volume.Mountpoint
			}
		}
		mounts = append(mounts, mount)
	}

	return mounts
}

func (s *Service) CreateService(ctx context.Context, req swarmtypes.ServiceCreateRequest) (*swarmtypes.ServiceCreateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Unmarshal spec from JSON
	var spec swarm.ServiceSpec
	if unmarshalErr := json.Unmarshal(req.Spec, &spec); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse service spec: %w", unmarshalErr)
	}

	optionsPayload := swarmtypes.ServiceCreateOptions{}
	if req.Options != nil {
		optionsPayload = *req.Options
	}

	// Sanitize spec to avoid empty UID/GID in secret/config refs
	sanitizeServiceSpecInternal(&spec)

	resp, err := dockerClient.ServiceCreate(ctx, client.ServiceCreateOptions{
		Spec:                spec,
		EncodedRegistryAuth: optionsPayload.EncodedRegistryAuth,
		QueryRegistry:       optionsPayload.QueryRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm service: %w", err)
	}

	return &swarmtypes.ServiceCreateResponse{
		ID:       resp.ID,
		Warnings: resp.Warnings,
	}, nil
}

func (s *Service) UpdateService(ctx context.Context, serviceID string, req swarmtypes.ServiceUpdateRequest) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	versionIndex := req.Version
	if versionIndex == 0 {
		serviceResult, serviceInspectErr := dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
		if serviceInspectErr != nil {
			return nil, fmt.Errorf("failed to inspect swarm service: %w", serviceInspectErr)
		}
		versionIndex = serviceResult.Service.Version.Index
	}

	optionsPayload := swarmtypes.ServiceUpdateOptions{}
	if req.Options != nil {
		optionsPayload = *req.Options
	}

	// Sanitize spec to avoid empty UID/GID in secret/config refs
	sanitizeServiceSpecInternal(&req.Spec)

	resp, err := dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
		Version:             swarm.Version{Index: versionIndex},
		Spec:                req.Spec,
		EncodedRegistryAuth: optionsPayload.EncodedRegistryAuth,
		RegistryAuthFrom:    optionsPayload.RegistryAuthFrom,
		Rollback:            optionsPayload.Rollback,
		QueryRegistry:       optionsPayload.QueryRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{
		Warnings: resp.Warnings,
	}, nil
}

func (s *Service) RemoveService(ctx context.Context, serviceID string) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, serviceRemoveErr := dockerClient.ServiceRemove(ctx, serviceID, client.ServiceRemoveOptions{}); serviceRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm service: %w", serviceRemoveErr)
	}

	return nil
}

func (s *Service) StreamServiceLogs(ctx context.Context, serviceID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerClient(ctx)
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

func (s *Service) ListServiceTasksPaginated(ctx context.Context, serviceID string, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(client.Filters)
	filters.Add("service", serviceID)
	return s.listTasks(ctx, filters, params)
}

func (s *Service) RollbackService(ctx context.Context, serviceID string) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}

	updateResult, err := dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
		Version:  serviceResult.Service.Version,
		Spec:     serviceResult.Service.Spec,
		Rollback: "previous",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rollback swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *Service) ScaleService(ctx context.Context, serviceID string, replicas uint64) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, client.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}
	service := serviceResult.Service

	if applySwarmServiceScaleErr := applySwarmServiceScaleInternal(&service.Spec.Mode, replicas); applySwarmServiceScaleErr != nil {
		return nil, applySwarmServiceScaleErr
	}

	updateResult, err := dockerClient.ServiceUpdate(ctx, serviceID, client.ServiceUpdateOptions{
		Version: service.Version,
		Spec:    service.Spec,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scale swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *Service) summarizeServicesInternal(ctx context.Context, dockerClient *client.Client, services []swarm.Service) ([]swarmtypes.ServiceSummary, error) {
	nodesResult, err := dockerClient.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	networksResult, err := dockerClient.NetworkList(ctx, client.NetworkListOptions{})
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

	tasksResult, err := dockerClient.TaskList(ctx, client.TaskListOptions{})
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

	summaries := make([]swarmtypes.ServiceSummary, 0, len(services))
	for _, service := range services {
		var nodeNames []string
		if nodeSet, ok := serviceNodes[service.ID]; ok {
			nodeNames = make([]string, 0, len(nodeSet))
			for nodeName := range nodeSet {
				nodeNames = append(nodeNames, nodeName)
			}
			sort.Strings(nodeNames)
		}
		summaries = append(summaries, swarmtypes.NewServiceSummary(service, nodeNames, networkNameByID))
	}

	return summaries, nil
}

func (s *Service) buildServicePaginationConfigInternal() pagination.Config[swarmtypes.ServiceSummary] {
	return pagination.Config[swarmtypes.ServiceSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.ServiceSummary]{
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Name, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Image, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.ID, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.StackName, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Mode, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) {
				return strings.Join(svc.Networks, " "), nil
			},
			func(svc swarmtypes.ServiceSummary) (string, error) {
				return strings.Join(svc.Nodes, " "), nil
			},
		},
		SortBindings: []pagination.SortBinding[swarmtypes.ServiceSummary]{
			{Key: "name", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "image", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Image, b.Image) }},
			{Key: "mode", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Mode, b.Mode) }},
			{Key: "replicas", Fn: func(a, b swarmtypes.ServiceSummary) int {
				if localCmp := cmp.Compare(a.Replicas, b.Replicas); localCmp != 0 {
					return localCmp
				}
				return cmp.Compare(a.RunningReplicas, b.RunningReplicas)
			}},
			{Key: "created", Fn: func(a, b swarmtypes.ServiceSummary) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.ServiceSummary) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
}

func sanitizeServiceSpecInternal(spec *swarm.ServiceSpec) {
	if spec == nil || spec.TaskTemplate.ContainerSpec == nil {
		return
	}

	for _, ref := range spec.TaskTemplate.ContainerSpec.Secrets {
		if ref != nil && ref.File != nil {
			if strings.TrimSpace(ref.File.UID) == "" {
				ref.File.UID = "0"
			}
			if strings.TrimSpace(ref.File.GID) == "" {
				ref.File.GID = "0"
			}
		}
	}

	for _, ref := range spec.TaskTemplate.ContainerSpec.Configs {
		if ref != nil && ref.File != nil {
			if strings.TrimSpace(ref.File.UID) == "" {
				ref.File.UID = "0"
			}
			if strings.TrimSpace(ref.File.GID) == "" {
				ref.File.GID = "0"
			}
		}
	}
}

func toServiceNetworkIPAMConfigInternal(cfg network.IPAMConfig) swarmtypes.ServiceNetworkIPAMConfig {
	out := swarmtypes.ServiceNetworkIPAMConfig{}
	if cfg.Subnet.IsValid() {
		out.Subnet = cfg.Subnet.String()
	}
	if cfg.Gateway.IsValid() {
		out.Gateway = cfg.Gateway.String()
	}
	if cfg.IPRange.IsValid() {
		out.IPRange = cfg.IPRange.String()
	}
	return out
}

func applySwarmServiceScaleInternal(mode *swarm.ServiceMode, replicas uint64) error {
	switch {
	case mode.Replicated != nil:
		mode.Replicated.Replicas = &replicas
	case mode.ReplicatedJob != nil:
		mode.ReplicatedJob.TotalCompletions = &replicas
	default:
		return fmt.Errorf("scale can only be used with replicated or replicated-job mode: %w", errdefs.ErrInvalidArgument)
	}

	return nil
}
