package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	appfs "github.com/getarcaneapp/arcane/backend/internal/utils/fs"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	libswarm "github.com/getarcaneapp/arcane/backend/pkg/libarcane/swarm"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"
)

var ErrSwarmNotEnabled = errors.New("swarm mode is not enabled")
var ErrSwarmManagerRequired = errors.New("swarm manager access required")

// SwarmService provides Docker Swarm related operations.
type SwarmService struct {
	dockerService   *DockerClientService
	settingsService *SettingsService
}

func NewSwarmService(dockerService *DockerClientService, settingsService *SettingsService) *SwarmService {
	return &SwarmService{
		dockerService:   dockerService,
		settingsService: settingsService,
	}
}

func (s *SwarmService) ListServicesPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.ServiceSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{Status: true})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}
	services := servicesResult.Items

	// Fetch nodes to resolve node IDs to hostnames
	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	nodes := nodesResult.Items
	nodeNameByID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	// Fetch networks to resolve network IDs to names
	networksResult, err := dockerClient.NetworkList(ctx, dockerclient.NetworkListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list networks: %w", err)
	}
	networks := networksResult.Items
	networkNameByID := make(map[string]string, len(networks))
	for _, n := range networks {
		networkNameByID[n.ID] = n.Name
	}

	// Fetch tasks and group running tasks by service ID
	tasksResult, err := dockerClient.TaskList(ctx, dockerclient.TaskListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm tasks: %w", err)
	}
	tasks := tasksResult.Items
	serviceNodes := make(map[string]map[string]struct{})
	for _, task := range tasks {
		if string(task.Status.State) != "running" {
			continue
		}
		if _, ok := serviceNodes[task.ServiceID]; !ok {
			serviceNodes[task.ServiceID] = make(map[string]struct{})
		}
		if nodeName, ok := nodeNameByID[task.NodeID]; ok {
			serviceNodes[task.ServiceID][nodeName] = struct{}{}
		}
	}

	items := make([]swarmtypes.ServiceSummary, 0, len(services))
	for _, service := range services {
		var nodeNames []string
		if nodeSet, ok := serviceNodes[service.ID]; ok {
			nodeNames = make([]string, 0, len(nodeSet))
			for name := range nodeSet {
				nodeNames = append(nodeNames, name)
			}
			sort.Strings(nodeNames)
		}
		items = append(items, swarmtypes.NewServiceSummary(service, nodeNames, networkNameByID))
	}

	config := s.buildServicePaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) GetService(ctx context.Context, serviceID string) (*swarmtypes.ServiceInspect, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, dockerclient.ServiceInspectOptions{})
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

func (s *SwarmService) resolveServiceNodeNamesInternal(ctx context.Context, dockerClient *dockerclient.Client, serviceID string) []string {
	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	tasksResult, err := dockerClient.TaskList(ctx, dockerclient.TaskListOptions{})
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

	nodeNames := make([]string, 0, len(nodeSet))
	for name := range nodeSet {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)
	return nodeNames
}

func (s *SwarmService) enrichServiceNetworkDetailsInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
	networkConfigs []swarm.NetworkAttachmentConfig,
) map[string]swarmtypes.ServiceNetworkDetail {
	if len(networkConfigs) == 0 {
		return nil
	}

	details := make(map[string]swarmtypes.ServiceNetworkDetail, len(networkConfigs))
	for _, networkConfig := range networkConfigs {
		networkID := networkConfig.Target
		netInspectResult, err := dockerClient.NetworkInspect(ctx, networkID, dockerclient.NetworkInspectOptions{})
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
		if networkInfo.ConfigFrom.Network != "" {
			detail.ConfigFrom = networkInfo.ConfigFrom.Network
		}

		for _, ipamCfg := range networkInfo.IPAM.Config {
			detail.IPAMConfigs = append(detail.IPAMConfigs, toServiceNetworkIPAMConfigInternal(ipamCfg))
		}

		if detail.ConfigFrom != "" {
			configInspectResult, err := dockerClient.NetworkInspect(ctx, detail.ConfigFrom, dockerclient.NetworkInspectOptions{})
			if err == nil {
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

func (s *SwarmService) enrichServiceMountsInternal(
	ctx context.Context,
	dockerClient *dockerclient.Client,
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
			volInspectResult, err := dockerClient.VolumeInspect(ctx, serviceMount.Source, dockerclient.VolumeInspectOptions{})
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

func (s *SwarmService) CreateService(ctx context.Context, req swarmtypes.ServiceCreateRequest) (*swarmtypes.ServiceCreateResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Unmarshal spec from JSON
	var spec swarm.ServiceSpec
	if err := json.Unmarshal(req.Spec, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse service spec: %w", err)
	}

	optionsPayload := swarmtypes.ServiceCreateOptions{}
	if req.Options != nil {
		optionsPayload = *req.Options
	}

	resp, err := dockerClient.ServiceCreate(ctx, dockerclient.ServiceCreateOptions{
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

func (s *SwarmService) UpdateService(ctx context.Context, serviceID string, req swarmtypes.ServiceUpdateRequest) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	versionIndex := req.Version
	if versionIndex == 0 {
		serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, dockerclient.ServiceInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
		}
		versionIndex = serviceResult.Service.Version.Index
	}

	optionsPayload := swarmtypes.ServiceUpdateOptions{}
	if req.Options != nil {
		optionsPayload = *req.Options
	}

	resp, err := dockerClient.ServiceUpdate(ctx, serviceID, dockerclient.ServiceUpdateOptions{
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

func (s *SwarmService) RemoveService(ctx context.Context, serviceID string) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.ServiceRemove(ctx, serviceID, dockerclient.ServiceRemoveOptions{}); err != nil {
		return fmt.Errorf("failed to remove swarm service: %w", err)
	}

	return nil
}

func (s *SwarmService) StreamServiceLogs(ctx context.Context, serviceID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	options := dockerclient.ServiceLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Since:      since,
		Timestamps: timestamps,
	}

	logs, err := dockerClient.ServiceLogs(ctx, serviceID, options)
	if err != nil {
		return fmt.Errorf("failed to get service logs: %w", err)
	}
	defer func() { _ = logs.Close() }()

	if follow {
		return streamMultiplexedLogs(ctx, logs, logsChan)
	}

	return readAllLogs(logs, logsChan)
}

func (s *SwarmService) ListNodesPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.NodeSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	nodes := nodesResult.Items

	items := make([]swarmtypes.NodeSummary, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, swarmtypes.NewNodeSummary(node))
	}

	config := s.buildNodePaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) GetNode(ctx context.Context, nodeID string) (*swarmtypes.NodeSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodeResult, err := dockerClient.NodeInspect(ctx, nodeID, dockerclient.NodeInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	out := swarmtypes.NewNodeSummary(nodeResult.Node)
	return &out, nil
}

func (s *SwarmService) ListTasksPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}
	services := servicesResult.Items

	serviceNameByID := make(map[string]string, len(services))
	for _, service := range services {
		serviceNameByID[service.ID] = service.Spec.Name
	}

	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	nodes := nodesResult.Items

	nodeNameByID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	tasksResult, err := dockerClient.TaskList(ctx, dockerclient.TaskListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm tasks: %w", err)
	}
	tasks := tasksResult.Items

	items := make([]swarmtypes.TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, swarmtypes.NewTaskSummary(task, serviceNameByID[task.ServiceID], nodeNameByID[task.NodeID]))
	}

	config := s.buildTaskPaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) ListStacksPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.StackSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}
	services := servicesResult.Items

	stacks := make(map[string]*swarmtypes.StackSummary)
	for _, service := range services {
		stackName := service.Spec.Labels[swarmtypes.StackNamespaceLabel]
		if stackName == "" {
			continue
		}

		entry, exists := stacks[stackName]
		if !exists {
			stacks[stackName] = &swarmtypes.StackSummary{
				ID:        stackName,
				Name:      stackName,
				Namespace: stackName,
				Services:  1,
				CreatedAt: service.CreatedAt,
				UpdatedAt: service.UpdatedAt,
			}
			continue
		}

		entry.Services++
		if service.CreatedAt.Before(entry.CreatedAt) {
			entry.CreatedAt = service.CreatedAt
		}
		if service.UpdatedAt.After(entry.UpdatedAt) {
			entry.UpdatedAt = service.UpdatedAt
		}
	}

	items := make([]swarmtypes.StackSummary, 0, len(stacks))
	for _, stack := range stacks {
		items = append(items, *stack)
	}

	config := s.buildStackPaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) DeployStack(ctx context.Context, environmentID string, req swarmtypes.StackDeployRequest) (*swarmtypes.StackDeployResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	stackName := strings.TrimSpace(req.Name)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if err := libswarm.DeployStack(ctx, dockerClient, libswarm.StackDeployOptions{
		Name:             stackName,
		ComposeContent:   req.ComposeContent,
		EnvContent:       req.EnvContent,
		WithRegistryAuth: req.WithRegistryAuth,
		Prune:            req.Prune,
		ResolveImage:     req.ResolveImage,
	}); err != nil {
		return nil, err
	}

	if err := s.upsertStackSourceInternal(ctx, environmentID, stackName, req.ComposeContent, req.EnvContent); err != nil {
		slog.WarnContext(ctx, "failed to persist swarm stack source", "environmentID", normalizeSwarmEnvironmentIDInternal(environmentID), "stackName", stackName, "error", err)
	}

	return &swarmtypes.StackDeployResponse{Name: stackName}, nil
}

func (s *SwarmService) GetSwarmInfo(ctx context.Context) (*swarmtypes.SwarmInfo, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	infoResult, err := dockerClient.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	out := swarmtypes.NewSwarmInfo(infoResult.Swarm)
	return &out, nil
}

func (s *SwarmService) InitSwarm(ctx context.Context, req swarmtypes.SwarmInitRequest) (*swarmtypes.SwarmInitResponse, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	defaultAddrPool := make([]netip.Prefix, 0, len(req.DefaultAddrPool))
	for _, raw := range req.DefaultAddrPool {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse default address pool %q: %w", raw, err)
		}
		defaultAddrPool = append(defaultAddrPool, prefix.Masked())
	}

	initResult, err := dockerClient.SwarmInit(ctx, dockerclient.SwarmInitOptions{
		ListenAddr:       req.ListenAddr,
		AdvertiseAddr:    req.AdvertiseAddr,
		DataPathAddr:     req.DataPathAddr,
		DataPathPort:     req.DataPathPort,
		ForceNewCluster:  req.ForceNewCluster,
		Spec:             req.Spec,
		AutoLockManagers: req.AutoLockManagers,
		Availability:     req.Availability,
		DefaultAddrPool:  defaultAddrPool,
		SubnetSize:       req.SubnetSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize swarm: %w", err)
	}

	return &swarmtypes.SwarmInitResponse{NodeID: initResult.NodeID}, nil
}

func (s *SwarmService) JoinSwarm(ctx context.Context, req swarmtypes.SwarmJoinRequest) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.SwarmJoin(ctx, dockerclient.SwarmJoinOptions{
		ListenAddr:    req.ListenAddr,
		AdvertiseAddr: req.AdvertiseAddr,
		DataPathAddr:  req.DataPathAddr,
		RemoteAddrs:   req.RemoteAddrs,
		JoinToken:     req.JoinToken,
		Availability:  req.Availability,
	}); err != nil {
		return fmt.Errorf("failed to join swarm: %w", err)
	}

	return nil
}

func (s *SwarmService) LeaveSwarm(ctx context.Context, req swarmtypes.SwarmLeaveRequest) error {
	if err := s.ensureSwarmActiveInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.SwarmLeave(ctx, dockerclient.SwarmLeaveOptions{Force: req.Force}); err != nil {
		return fmt.Errorf("failed to leave swarm: %w", err)
	}

	return nil
}

func (s *SwarmService) UnlockSwarm(ctx context.Context, req swarmtypes.SwarmUnlockRequest) error {
	if err := s.ensureSwarmActiveInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.SwarmUnlock(ctx, dockerclient.SwarmUnlockOptions{Key: req.Key}); err != nil {
		return fmt.Errorf("failed to unlock swarm: %w", err)
	}

	return nil
}

func (s *SwarmService) GetSwarmUnlockKey(ctx context.Context) (*swarmtypes.SwarmUnlockKeyResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	unlockResult, err := dockerClient.SwarmGetUnlockKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get swarm unlock key: %w", err)
	}

	return &swarmtypes.SwarmUnlockKeyResponse{UnlockKey: unlockResult.Key}, nil
}

func (s *SwarmService) GetSwarmJoinTokens(ctx context.Context) (*swarmtypes.SwarmJoinTokensResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	infoResult, err := dockerClient.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	return &swarmtypes.SwarmJoinTokensResponse{
		Worker:  infoResult.Swarm.JoinTokens.Worker,
		Manager: infoResult.Swarm.JoinTokens.Manager,
	}, nil
}

func (s *SwarmService) RotateSwarmJoinTokens(ctx context.Context, req swarmtypes.SwarmRotateJoinTokensRequest) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	infoResult, err := dockerClient.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect swarm: %w", err)
	}

	rotateWorker := req.RotateWorkerToken
	rotateManager := req.RotateManagerToken
	if !rotateWorker && !rotateManager {
		rotateWorker = true
		rotateManager = true
	}

	if _, err := dockerClient.SwarmUpdate(ctx, dockerclient.SwarmUpdateOptions{
		Version:            infoResult.Swarm.Version,
		Spec:               infoResult.Swarm.Spec,
		RotateWorkerToken:  rotateWorker,
		RotateManagerToken: rotateManager,
	}); err != nil {
		return fmt.Errorf("failed to rotate swarm join tokens: %w", err)
	}

	return nil
}

func (s *SwarmService) UpdateSwarmSpec(ctx context.Context, req swarmtypes.SwarmUpdateRequest) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	version := req.Version
	if version == 0 {
		infoResult, err := dockerClient.SwarmInspect(ctx, dockerclient.SwarmInspectOptions{})
		if err != nil {
			return fmt.Errorf("failed to inspect swarm: %w", err)
		}
		version = infoResult.Swarm.Version.Index
	}

	if _, err := dockerClient.SwarmUpdate(ctx, dockerclient.SwarmUpdateOptions{
		Version:                swarm.Version{Index: version},
		Spec:                   req.Spec,
		RotateWorkerToken:      req.RotateWorkerToken,
		RotateManagerToken:     req.RotateManagerToken,
		RotateManagerUnlockKey: req.RotateManagerUnlockKey,
	}); err != nil {
		return fmt.Errorf("failed to update swarm spec: %w", err)
	}

	return nil
}

func (s *SwarmService) ListServiceTasksPaginated(ctx context.Context, serviceID string, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(dockerclient.Filters)
	filters.Add("service", serviceID)
	return s.listTasksPaginatedWithFiltersInternal(ctx, filters, params)
}

func (s *SwarmService) RollbackService(ctx context.Context, serviceID string) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, dockerclient.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}

	updateResult, err := dockerClient.ServiceUpdate(ctx, serviceID, dockerclient.ServiceUpdateOptions{
		Version:  serviceResult.Service.Version,
		Spec:     serviceResult.Service.Spec,
		Rollback: "previous",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rollback swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *SwarmService) ScaleService(ctx context.Context, serviceID string, replicas uint64) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	serviceResult, err := dockerClient.ServiceInspect(ctx, serviceID, dockerclient.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}
	service := serviceResult.Service

	if service.Spec.Mode.Global != nil {
		return nil, errors.New("cannot scale global service")
	}
	if service.Spec.Mode.Replicated == nil {
		service.Spec.Mode.Replicated = &swarm.ReplicatedService{}
	}
	service.Spec.Mode.Replicated.Replicas = &replicas

	updateResult, err := dockerClient.ServiceUpdate(ctx, serviceID, dockerclient.ServiceUpdateOptions{
		Version: service.Version,
		Spec:    service.Spec,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scale swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{Warnings: updateResult.Warnings}, nil
}

func (s *SwarmService) UpdateNode(ctx context.Context, nodeID string, req swarmtypes.NodeUpdateRequest) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodeResult, err := dockerClient.NodeInspect(ctx, nodeID, dockerclient.NodeInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	version := req.Version
	if version == 0 {
		version = nodeResult.Node.Version.Index
	}

	spec := nodeResult.Node.Spec
	if req.Name != nil {
		spec.Name = *req.Name
	}
	if req.Labels != nil {
		spec.Labels = req.Labels
	}
	if req.Role != nil {
		spec.Role = *req.Role
	}
	if req.Availability != nil {
		spec.Availability = *req.Availability
	}

	if _, err := dockerClient.NodeUpdate(ctx, nodeID, dockerclient.NodeUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	}); err != nil {
		return fmt.Errorf("failed to update swarm node: %w", err)
	}

	return nil
}

func (s *SwarmService) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.NodeRemove(ctx, nodeID, dockerclient.NodeRemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("failed to remove swarm node: %w", err)
	}

	return nil
}

func (s *SwarmService) PromoteNode(ctx context.Context, nodeID string) error {
	role := swarm.NodeRoleManager
	return s.UpdateNode(ctx, nodeID, swarmtypes.NodeUpdateRequest{Role: &role})
}

func (s *SwarmService) DemoteNode(ctx context.Context, nodeID string) error {
	role := swarm.NodeRoleWorker
	return s.UpdateNode(ctx, nodeID, swarmtypes.NodeUpdateRequest{Role: &role})
}

func (s *SwarmService) ListNodeTasksPaginated(ctx context.Context, nodeID string, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(dockerclient.Filters)
	filters.Add("node", nodeID)
	return s.listTasksPaginatedWithFiltersInternal(ctx, filters, params)
}

func (s *SwarmService) GetStack(ctx context.Context, environmentID, stackName string) (*swarmtypes.StackInspect, error) {
	_ = environmentID
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := s.listStackServicesRawInternal(ctx, dockerClient, stackName)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, cerrdefs.ErrNotFound
	}

	createdAt := services[0].CreatedAt
	updatedAt := services[0].UpdatedAt
	for _, service := range services[1:] {
		if service.CreatedAt.Before(createdAt) {
			createdAt = service.CreatedAt
		}
		if service.UpdatedAt.After(updatedAt) {
			updatedAt = service.UpdatedAt
		}
	}

	return &swarmtypes.StackInspect{
		Name:      stackName,
		Namespace: stackName,
		Services:  len(services),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *SwarmService) GetStackSource(ctx context.Context, environmentID, stackName string) (*swarmtypes.StackSource, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	_, stackSourceDir, err := s.resolveSwarmStackSourceDirInternal(ctx, environmentID, stackName)
	if err != nil {
		return nil, err
	}

	composeContent, err := os.ReadFile(filepath.Join(stackSourceDir, swarmStackComposeFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, cerrdefs.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read swarm stack compose source: %w", err)
	}

	envContent := ""
	envBytes, err := os.ReadFile(filepath.Join(stackSourceDir, swarmStackEnvFilename))
	if err == nil {
		envContent = string(envBytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read swarm stack env source: %w", err)
	}

	return &swarmtypes.StackSource{
		Name:           stackName,
		ComposeContent: string(composeContent),
		EnvContent:     envContent,
	}, nil
}

func (s *SwarmService) RemoveStack(ctx context.Context, environmentID, stackName string) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return errors.New("stack name is required")
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := s.listStackServicesRawInternal(ctx, dockerClient, stackName)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return cerrdefs.ErrNotFound
	}

	serviceIDs := make(map[string]struct{}, len(services))
	for _, service := range services {
		serviceIDs[service.ID] = struct{}{}
		if _, err := dockerClient.ServiceRemove(ctx, service.ID, dockerclient.ServiceRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove swarm service %s: %w", service.Spec.Name, err)
		}
	}

	if err := s.waitForRemovedServiceTasksInternal(ctx, dockerClient, serviceIDs, 30*time.Second); err != nil {
		return err
	}

	stackLabel := fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName)

	configFilter := make(dockerclient.Filters).Add("label", stackLabel)
	configsResult, err := dockerClient.ConfigList(ctx, dockerclient.ConfigListOptions{Filters: configFilter})
	if err != nil {
		return fmt.Errorf("failed to list stack configs: %w", err)
	}
	for _, cfg := range configsResult.Items {
		if _, err := dockerClient.ConfigRemove(ctx, cfg.ID, dockerclient.ConfigRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove stack config %s: %w", cfg.Spec.Name, err)
		}
	}

	secretFilter := make(dockerclient.Filters).Add("label", stackLabel)
	secretsResult, err := dockerClient.SecretList(ctx, dockerclient.SecretListOptions{Filters: secretFilter})
	if err != nil {
		return fmt.Errorf("failed to list stack secrets: %w", err)
	}
	for _, secret := range secretsResult.Items {
		if _, err := dockerClient.SecretRemove(ctx, secret.ID, dockerclient.SecretRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove stack secret %s: %w", secret.Spec.Name, err)
		}
	}

	networkFilter := make(dockerclient.Filters).Add("label", stackLabel)
	networksResult, err := dockerClient.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: networkFilter})
	if err != nil {
		return fmt.Errorf("failed to list stack networks: %w", err)
	}
	for _, network := range networksResult.Items {
		if network.Ingress {
			continue
		}
		if _, err := dockerClient.NetworkRemove(ctx, network.ID, dockerclient.NetworkRemoveOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove stack network %s: %w", network.Name, err)
		}
	}

	if err := s.deleteStackSourceInternal(ctx, environmentID, stackName); err != nil {
		slog.WarnContext(ctx, "failed to remove persisted swarm stack source", "environmentID", normalizeSwarmEnvironmentIDInternal(environmentID), "stackName", stackName, "error", err)
	}

	return nil
}

func (s *SwarmService) ListStackServicesPaginated(ctx context.Context, stackName string, params pagination.QueryParams) ([]swarmtypes.ServiceSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := s.listStackServicesRawInternal(ctx, dockerClient, stackName)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if len(services) == 0 {
		return nil, pagination.Response{}, cerrdefs.ErrNotFound
	}

	summaries, err := s.summarizeServicesInternal(ctx, dockerClient, services)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	config := s.buildServicePaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(summaries, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)
	return result.Items, paginationResp, nil
}

func (s *SwarmService) ListStackTasksPaginated(ctx context.Context, stackName string, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := s.listStackServicesRawInternal(ctx, dockerClient, stackName)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if len(services) == 0 {
		return nil, pagination.Response{}, cerrdefs.ErrNotFound
	}

	filters := make(dockerclient.Filters)
	for _, service := range services {
		filters.Add("service", service.ID)
	}

	return s.listTasksPaginatedWithFiltersInternal(ctx, filters, params)
}

func (s *SwarmService) RenderStackConfig(ctx context.Context, req swarmtypes.StackRenderConfigRequest) (*swarmtypes.StackRenderConfigResponse, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	result, err := libswarm.RenderStackConfig(ctx, libswarm.StackRenderOptions{
		Name:           req.Name,
		ComposeContent: req.ComposeContent,
		EnvContent:     req.EnvContent,
	})
	if err != nil {
		return nil, err
	}

	return &swarmtypes.StackRenderConfigResponse{
		Name:            result.Name,
		RenderedCompose: result.RenderedCompose,
		Services:        result.Services,
		Networks:        result.Networks,
		Volumes:         result.Volumes,
		Configs:         result.Configs,
		Secrets:         result.Secrets,
	}, nil
}

func (s *SwarmService) ListConfigs(ctx context.Context) ([]swarmtypes.ConfigSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	configsResult, err := dockerClient.ConfigList(ctx, dockerclient.ConfigListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm configs: %w", err)
	}

	items := make([]swarmtypes.ConfigSummary, 0, len(configsResult.Items))
	for _, cfg := range configsResult.Items {
		items = append(items, swarmtypes.NewConfigSummary(cfg))
	}
	return items, nil
}

func (s *SwarmService) GetConfig(ctx context.Context, configID string) (*swarmtypes.ConfigSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	cfgResult, err := dockerClient.ConfigInspect(ctx, configID, dockerclient.ConfigInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm config: %w", err)
	}

	item := swarmtypes.NewConfigSummary(cfgResult.Config)
	return &item, nil
}

func (s *SwarmService) CreateConfig(ctx context.Context, req swarmtypes.ConfigCreateRequest) (*swarmtypes.ConfigSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	spec, err := decodeConfigSpecInternal(req.Spec)
	if err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	createResult, err := dockerClient.ConfigCreate(ctx, dockerclient.ConfigCreateOptions{Spec: spec})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm config: %w", err)
	}

	return s.GetConfig(ctx, createResult.ID)
}

func (s *SwarmService) UpdateConfig(ctx context.Context, configID string, req swarmtypes.ConfigUpdateRequest) (*swarmtypes.ConfigSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	spec, err := decodeConfigSpecInternal(req.Spec)
	if err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	version := req.Version
	if version == 0 {
		cfgResult, err := dockerClient.ConfigInspect(ctx, configID, dockerclient.ConfigInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect swarm config: %w", err)
		}
		version = cfgResult.Config.Version.Index
	}

	if _, err := dockerClient.ConfigUpdate(ctx, configID, dockerclient.ConfigUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	}); err != nil {
		return nil, fmt.Errorf("failed to update swarm config: %w", err)
	}

	return s.GetConfig(ctx, configID)
}

func (s *SwarmService) RemoveConfig(ctx context.Context, configID string) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.ConfigRemove(ctx, configID, dockerclient.ConfigRemoveOptions{}); err != nil {
		return fmt.Errorf("failed to remove swarm config: %w", err)
	}

	return nil
}

func (s *SwarmService) ListSecrets(ctx context.Context) ([]swarmtypes.SecretSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	secretsResult, err := dockerClient.SecretList(ctx, dockerclient.SecretListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm secrets: %w", err)
	}

	items := make([]swarmtypes.SecretSummary, 0, len(secretsResult.Items))
	for _, secret := range secretsResult.Items {
		items = append(items, swarmtypes.NewSecretSummary(secret))
	}
	return items, nil
}

func (s *SwarmService) GetSecret(ctx context.Context, secretID string) (*swarmtypes.SecretSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	secretResult, err := dockerClient.SecretInspect(ctx, secretID, dockerclient.SecretInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm secret: %w", err)
	}

	item := swarmtypes.NewSecretSummary(secretResult.Secret)
	return &item, nil
}

func (s *SwarmService) CreateSecret(ctx context.Context, req swarmtypes.SecretCreateRequest) (*swarmtypes.SecretSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	spec, err := decodeSecretSpecInternal(req.Spec)
	if err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	createResult, err := dockerClient.SecretCreate(ctx, dockerclient.SecretCreateOptions{Spec: spec})
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm secret: %w", err)
	}

	return s.GetSecret(ctx, createResult.ID)
}

func (s *SwarmService) UpdateSecret(ctx context.Context, secretID string, req swarmtypes.SecretUpdateRequest) (*swarmtypes.SecretSummary, error) {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return nil, err
	}

	spec, err := decodeSecretSpecInternal(req.Spec)
	if err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	version := req.Version
	if version == 0 {
		secretResult, err := dockerClient.SecretInspect(ctx, secretID, dockerclient.SecretInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect swarm secret: %w", err)
		}
		version = secretResult.Secret.Version.Index
	}

	if _, err := dockerClient.SecretUpdate(ctx, secretID, dockerclient.SecretUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	}); err != nil {
		return nil, fmt.Errorf("failed to update swarm secret: %w", err)
	}

	return s.GetSecret(ctx, secretID)
}

func (s *SwarmService) RemoveSecret(ctx context.Context, secretID string) error {
	if err := s.ensureSwarmManagerInternal(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, err := dockerClient.SecretRemove(ctx, secretID, dockerclient.SecretRemoveOptions{}); err != nil {
		return fmt.Errorf("failed to remove swarm secret: %w", err)
	}

	return nil
}

func (s *SwarmService) listTasksPaginatedWithFiltersInternal(ctx context.Context, filters dockerclient.Filters, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	serviceNameByID := make(map[string]string, len(servicesResult.Items))
	for _, service := range servicesResult.Items {
		serviceNameByID[service.ID] = service.Spec.Name
	}

	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	if filters == nil {
		filters = make(dockerclient.Filters)
	}
	tasksResult, err := dockerClient.TaskList(ctx, dockerclient.TaskListOptions{Filters: filters})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm tasks: %w", err)
	}

	items := make([]swarmtypes.TaskSummary, 0, len(tasksResult.Items))
	for _, task := range tasksResult.Items {
		items = append(items, swarmtypes.NewTaskSummary(task, serviceNameByID[task.ServiceID], nodeNameByID[task.NodeID]))
	}

	config := s.buildTaskPaginationConfigInternal()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponseInternal(result, params)
	return result.Items, paginationResp, nil
}

func (s *SwarmService) summarizeServicesInternal(ctx context.Context, dockerClient *dockerclient.Client, services []swarm.Service) ([]swarmtypes.ServiceSummary, error) {
	nodesResult, err := dockerClient.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	networksResult, err := dockerClient.NetworkList(ctx, dockerclient.NetworkListOptions{})
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

	tasksResult, err := dockerClient.TaskList(ctx, dockerclient.TaskListOptions{})
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

func (s *SwarmService) listStackServicesRawInternal(ctx context.Context, dockerClient *dockerclient.Client, stackName string) ([]swarm.Service, error) {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	stackFilter := make(dockerclient.Filters).Add("label", fmt.Sprintf("%s=%s", swarmtypes.StackNamespaceLabel, stackName))
	servicesResult, err := dockerClient.ServiceList(ctx, dockerclient.ServiceListOptions{
		Filters: stackFilter,
		Status:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list stack services: %w", err)
	}

	return servicesResult.Items, nil
}

func (s *SwarmService) waitForRemovedServiceTasksInternal(ctx context.Context, dockerClient *dockerclient.Client, serviceIDs map[string]struct{}, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		tasksResult, err := dockerClient.TaskList(waitCtx, dockerclient.TaskListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list tasks while waiting for stack removal: %w", err)
		}

		hasActiveTasks := false
		for _, task := range tasksResult.Items {
			if _, ok := serviceIDs[task.ServiceID]; !ok {
				continue
			}
			if !isTaskTerminalInternal(task.Status.State) {
				hasActiveTasks = true
				break
			}
		}
		if !hasActiveTasks {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for stack task convergence: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func isTaskTerminalInternal(state swarm.TaskState) bool {
	switch state {
	case swarm.TaskStateComplete,
		swarm.TaskStateShutdown,
		swarm.TaskStateFailed,
		swarm.TaskStateRejected,
		swarm.TaskStateRemove,
		swarm.TaskStateOrphaned:
		return true
	case swarm.TaskStateNew,
		swarm.TaskStateAllocated,
		swarm.TaskStatePending,
		swarm.TaskStateAssigned,
		swarm.TaskStateAccepted,
		swarm.TaskStatePreparing,
		swarm.TaskStateReady,
		swarm.TaskStateStarting,
		swarm.TaskStateRunning:
		return false
	}

	return false
}

func decodeConfigSpecInternal(raw json.RawMessage) (swarm.ConfigSpec, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return swarm.ConfigSpec{}, errors.New("config spec is required")
	}

	var spec swarm.ConfigSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return swarm.ConfigSpec{}, fmt.Errorf("failed to parse config spec: %w", err)
	}

	if strings.TrimSpace(spec.Name) == "" {
		return swarm.ConfigSpec{}, errors.New("config spec name is required")
	}

	return spec, nil
}

func decodeSecretSpecInternal(raw json.RawMessage) (swarm.SecretSpec, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return swarm.SecretSpec{}, errors.New("secret spec is required")
	}

	var spec swarm.SecretSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return swarm.SecretSpec{}, fmt.Errorf("failed to parse secret spec: %w", err)
	}

	if strings.TrimSpace(spec.Name) == "" {
		return swarm.SecretSpec{}, errors.New("secret spec name is required")
	}

	return spec, nil
}

func (s *SwarmService) upsertStackSourceInternal(ctx context.Context, environmentID, stackName, composeContent, envContent string) error {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return errors.New("stack name is required")
	}

	_, stackSourceDir, err := s.resolveSwarmStackSourceDirInternal(ctx, environmentID, stackName)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(stackSourceDir, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create swarm stack source directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(stackSourceDir, swarmStackComposeFilename), []byte(composeContent), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write swarm stack compose source: %w", err)
	}

	envPath := filepath.Join(stackSourceDir, swarmStackEnvFilename)
	if envContent == "" {
		if err := os.Remove(envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to clear swarm stack env source: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(envPath, []byte(envContent), common.FilePerm); err != nil {
		return fmt.Errorf("failed to write swarm stack env source: %w", err)
	}

	return nil
}

func (s *SwarmService) deleteStackSourceInternal(ctx context.Context, environmentID, stackName string) error {
	if strings.TrimSpace(stackName) == "" {
		return errors.New("stack name is required")
	}

	rootDir, stackSourceDir, err := s.resolveSwarmStackSourceDirInternal(ctx, environmentID, stackName)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(stackSourceDir); err != nil {
		return fmt.Errorf("failed to remove swarm stack source directory: %w", err)
	}

	// Best-effort cleanup of now-empty environment directory.
	environmentDir := filepath.Dir(stackSourceDir)
	if environmentDir != rootDir {
		if err := os.Remove(environmentDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if !errors.Is(err, os.ErrPermission) {
				// Directory is typically non-empty; ignore that case.
				if !strings.Contains(strings.ToLower(err.Error()), "directory not empty") {
					slog.DebugContext(ctx, "swarm stack source environment directory cleanup skipped", "dir", environmentDir, "error", err)
				}
			}
		}
	}

	return nil
}

func normalizeSwarmEnvironmentIDInternal(environmentID string) string {
	envID := strings.TrimSpace(environmentID)
	if envID == "" {
		return "0"
	}
	return envID
}

const (
	defaultSwarmStackSourceRootDir = "/app/data/swarm/sources"
	swarmStackComposeFilename      = "compose.yaml"
	swarmStackEnvFilename          = ".env"
)

func (s *SwarmService) resolveSwarmStackSourceDirInternal(ctx context.Context, environmentID, stackName string) (string, string, error) {
	normalizedStackName := appfs.SanitizeProjectName(strings.TrimSpace(stackName))
	if normalizedStackName == "" || strings.Trim(normalizedStackName, "_") == "" {
		return "", "", errors.New("invalid stack name")
	}

	normalizedEnvironmentID := appfs.SanitizeProjectName(normalizeSwarmEnvironmentIDInternal(environmentID))
	if normalizedEnvironmentID == "" || strings.Trim(normalizedEnvironmentID, "_") == "" {
		normalizedEnvironmentID = "0"
	}

	configuredRootDir := defaultSwarmStackSourceRootDir
	if s.settingsService != nil {
		configuredRootDir = strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "swarmStackSourcesDirectory", defaultSwarmStackSourceRootDir))
		if configuredRootDir == "" {
			configuredRootDir = defaultSwarmStackSourceRootDir
		}
	}

	rootDir, err := filepath.Abs(configuredRootDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve swarm source root: %w", err)
	}
	rootDir = filepath.Clean(rootDir)

	stackSourceDir := filepath.Clean(filepath.Join(rootDir, normalizedEnvironmentID, normalizedStackName))
	if !appfs.IsSafeSubdirectory(rootDir, stackSourceDir) {
		return "", "", errors.New("swarm stack source path escapes storage root")
	}

	return rootDir, stackSourceDir, nil
}

func (s *SwarmService) ensureSwarmManagerInternal(ctx context.Context) error {
	info, err := s.getDockerInfoInternal(ctx)
	if err != nil {
		return err
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return ErrSwarmNotEnabled
	}
	if !info.Swarm.ControlAvailable {
		return ErrSwarmManagerRequired
	}

	return nil
}

func (s *SwarmService) ensureSwarmActiveInternal(ctx context.Context) error {
	info, err := s.getDockerInfoInternal(ctx)
	if err != nil {
		return err
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return ErrSwarmNotEnabled
	}

	return nil
}

func (s *SwarmService) getDockerInfoInternal(ctx context.Context) (system.Info, error) {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return system.Info{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	infoResult, err := dockerClient.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return system.Info{}, fmt.Errorf("failed to get Docker info: %w", err)
	}

	return infoResult.Info, nil
}

func (s *SwarmService) buildServicePaginationConfigInternal() pagination.Config[swarmtypes.ServiceSummary] {
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
				if cmp := compareUint64Internal(a.Replicas, b.Replicas); cmp != 0 {
					return cmp
				}
				return compareUint64Internal(a.RunningReplicas, b.RunningReplicas)
			}},
			{Key: "created", Fn: func(a, b swarmtypes.ServiceSummary) int { return compareTimeInternal(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.ServiceSummary) int { return compareTimeInternal(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildNodePaginationConfigInternal() pagination.Config[swarmtypes.NodeSummary] {
	return pagination.Config[swarmtypes.NodeSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.NodeSummary]{
			func(node swarmtypes.NodeSummary) (string, error) { return node.Hostname, nil },
			func(node swarmtypes.NodeSummary) (string, error) { return node.ID, nil },
			func(node swarmtypes.NodeSummary) (string, error) { return node.Role, nil },
			func(node swarmtypes.NodeSummary) (string, error) { return node.Status, nil },
			func(node swarmtypes.NodeSummary) (string, error) { return node.Availability, nil },
		},
		SortBindings: []pagination.SortBinding[swarmtypes.NodeSummary]{
			{Key: "hostname", Fn: func(a, b swarmtypes.NodeSummary) int { return strings.Compare(a.Hostname, b.Hostname) }},
			{Key: "role", Fn: func(a, b swarmtypes.NodeSummary) int { return strings.Compare(a.Role, b.Role) }},
			{Key: "status", Fn: func(a, b swarmtypes.NodeSummary) int { return strings.Compare(a.Status, b.Status) }},
			{Key: "availability", Fn: func(a, b swarmtypes.NodeSummary) int { return strings.Compare(a.Availability, b.Availability) }},
			{Key: "created", Fn: func(a, b swarmtypes.NodeSummary) int { return compareTimeInternal(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.NodeSummary) int { return compareTimeInternal(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildTaskPaginationConfigInternal() pagination.Config[swarmtypes.TaskSummary] {
	return pagination.Config[swarmtypes.TaskSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.TaskSummary]{
			func(task swarmtypes.TaskSummary) (string, error) { return task.Name, nil },
			func(task swarmtypes.TaskSummary) (string, error) { return task.ServiceName, nil },
			func(task swarmtypes.TaskSummary) (string, error) { return task.NodeName, nil },
			func(task swarmtypes.TaskSummary) (string, error) { return task.ID, nil },
			func(task swarmtypes.TaskSummary) (string, error) { return task.CurrentState, nil },
		},
		SortBindings: []pagination.SortBinding[swarmtypes.TaskSummary]{
			{Key: "service", Fn: func(a, b swarmtypes.TaskSummary) int { return strings.Compare(a.ServiceName, b.ServiceName) }},
			{Key: "node", Fn: func(a, b swarmtypes.TaskSummary) int { return strings.Compare(a.NodeName, b.NodeName) }},
			{Key: "state", Fn: func(a, b swarmtypes.TaskSummary) int { return strings.Compare(a.CurrentState, b.CurrentState) }},
			{Key: "created", Fn: func(a, b swarmtypes.TaskSummary) int { return compareTimeInternal(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.TaskSummary) int { return compareTimeInternal(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildStackPaginationConfigInternal() pagination.Config[swarmtypes.StackSummary] {
	return pagination.Config[swarmtypes.StackSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.StackSummary]{
			func(stack swarmtypes.StackSummary) (string, error) { return stack.Name, nil },
			func(stack swarmtypes.StackSummary) (string, error) { return stack.Namespace, nil },
		},
		SortBindings: []pagination.SortBinding[swarmtypes.StackSummary]{
			{Key: "name", Fn: func(a, b swarmtypes.StackSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "services", Fn: func(a, b swarmtypes.StackSummary) int { return compareIntInternal(a.Services, b.Services) }},
			{Key: "created", Fn: func(a, b swarmtypes.StackSummary) int { return compareTimeInternal(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.StackSummary) int { return compareTimeInternal(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func buildPaginationResponseInternal[T any](result pagination.FilterResult[T], params pagination.QueryParams) pagination.Response {
	totalPages := int64(0)
	if params.Limit > 0 {
		totalPages = (int64(result.TotalCount) + int64(params.Limit) - 1) / int64(params.Limit)
	}

	page := 1
	if params.Limit > 0 {
		page = (params.Start / params.Limit) + 1
	}

	return pagination.Response{
		TotalPages:      totalPages,
		TotalItems:      int64(result.TotalCount),
		CurrentPage:     page,
		ItemsPerPage:    params.Limit,
		GrandTotalItems: int64(result.TotalAvailable),
	}
}

func toServiceNetworkIPAMConfigInternal(cfg networktypes.IPAMConfig) swarmtypes.ServiceNetworkIPAMConfig {
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

func compareTimeInternal(a, b time.Time) int {
	if a.Before(b) {
		return -1
	}
	if a.After(b) {
		return 1
	}
	return 0
}

func compareUint64Internal(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareIntInternal(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
