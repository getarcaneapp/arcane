package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	composegoloader "github.com/compose-spec/compose-go/v2/loader"
	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/stack/options"
	stackswarm "github.com/docker/cli/cli/command/stack/swarm"
	composetypes "github.com/docker/cli/cli/compose/types"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/docker/api/types/swarm"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
	"github.com/spf13/pflag"
)

var ErrSwarmNotEnabled = errors.New("swarm mode is not enabled")
var ErrSwarmManagerRequired = errors.New("swarm manager access required")

// SwarmService provides Docker Swarm related operations.
type SwarmService struct {
	dockerService *DockerClientService
}

func NewSwarmService(dockerService *DockerClientService) *SwarmService {
	return &SwarmService{
		dockerService: dockerService,
	}
}

func (s *SwarmService) ListServicesPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.ServiceSummary, pagination.Response, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := dockerClient.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	items := make([]swarmtypes.ServiceSummary, 0, len(services))
	for _, service := range services {
		items = append(items, swarmtypes.NewServiceSummary(service))
	}

	config := s.buildServicePaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponse(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) GetService(ctx context.Context, serviceID string) (*swarmtypes.ServiceInspect, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	service, _, err := dockerClient.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
	}

	inspect := swarmtypes.NewServiceInspect(service)
	return &inspect, nil
}

func (s *SwarmService) CreateService(ctx context.Context, req swarmtypes.ServiceCreateRequest) (*swarmtypes.ServiceCreateResponse, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
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

	// Unmarshal options if provided
	var options swarm.ServiceCreateOptions
	if len(req.Options) > 0 {
		if err := json.Unmarshal(req.Options, &options); err != nil {
			return nil, fmt.Errorf("failed to parse service options: %w", err)
		}
	}

	resp, err := dockerClient.ServiceCreate(ctx, spec, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create swarm service: %w", err)
	}

	return &swarmtypes.ServiceCreateResponse{
		ID:       resp.ID,
		Warnings: resp.Warnings,
	}, nil
}

func (s *SwarmService) UpdateService(ctx context.Context, serviceID string, req swarmtypes.ServiceUpdateRequest) (*swarmtypes.ServiceUpdateResponse, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	versionIndex := req.Version
	if versionIndex == 0 {
		service, _, err := dockerClient.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to inspect swarm service: %w", err)
		}
		versionIndex = service.Version.Index
	}

	resp, err := dockerClient.ServiceUpdate(ctx, serviceID, swarm.Version{Index: versionIndex}, req.Spec, req.Options)
	if err != nil {
		return nil, fmt.Errorf("failed to update swarm service: %w", err)
	}

	return &swarmtypes.ServiceUpdateResponse{
		Warnings: resp.Warnings,
	}, nil
}

func (s *SwarmService) RemoveService(ctx context.Context, serviceID string) error {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if err := dockerClient.ServiceRemove(ctx, serviceID); err != nil {
		return fmt.Errorf("failed to remove swarm service: %w", err)
	}

	return nil
}

func (s *SwarmService) ListNodesPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.NodeSummary, pagination.Response, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodes, err := dockerClient.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	items := make([]swarmtypes.NodeSummary, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, swarmtypes.NewNodeSummary(node))
	}

	config := s.buildNodePaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponse(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) GetNode(ctx context.Context, nodeID string) (*swarmtypes.NodeSummary, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	node, _, err := dockerClient.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	out := swarmtypes.NewNodeSummary(node)
	return &out, nil
}

func (s *SwarmService) ListTasksPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := dockerClient.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	serviceNameByID := make(map[string]string, len(services))
	for _, service := range services {
		serviceNameByID[service.ID] = service.Spec.Name
	}

	nodes, err := dockerClient.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	tasks, err := dockerClient.TaskList(ctx, swarm.TaskListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm tasks: %w", err)
	}

	items := make([]swarmtypes.TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, swarmtypes.NewTaskSummary(task, serviceNameByID[task.ServiceID], nodeNameByID[task.NodeID]))
	}

	config := s.buildTaskPaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponse(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) ListStacksPaginated(ctx context.Context, params pagination.QueryParams) ([]swarmtypes.StackSummary, pagination.Response, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	services, err := dockerClient.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

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

	config := s.buildStackPaginationConfig()
	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := buildPaginationResponse(result, params)

	return result.Items, paginationResp, nil
}

func (s *SwarmService) DeployStack(ctx context.Context, req swarmtypes.StackDeployRequest) (*swarmtypes.StackDeployResponse, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	dockerCli, err := command.NewDockerCli(command.WithAPIClient(dockerClient))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker cli: %w", err)
	}

	cliOpts := flags.NewClientOptions()
	cliOpts.Hosts = []string{dockerClient.DaemonHost()}
	if err := dockerCli.Initialize(cliOpts); err != nil {
		return nil, fmt.Errorf("failed to initialize docker cli: %w", err)
	}

	composeConfig, err := s.loadStackComposeConfig(ctx, req.ComposeContent, req.EnvContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose content: %w", err)
	}

	resolveImage := req.ResolveImage
	if resolveImage == "" {
		resolveImage = stackswarm.ResolveImageAlways
	}

	//nolint:staticcheck // docker/cli stack deploy helpers are deprecated but required for Swarm stack deploy behavior.
	deployOpts := options.Deploy{
		Namespace:        req.Name,
		ResolveImage:     resolveImage,
		SendRegistryAuth: req.WithRegistryAuth,
		Prune:            req.Prune,
		Detach:           true,
	}

	flagSet := pflag.NewFlagSet("swarm-stack-deploy", pflag.ContinueOnError)
	flagSet.Bool("detach", deployOpts.Detach, "")
	_ = flagSet.Set("detach", strconv.FormatBool(deployOpts.Detach))

	//nolint:staticcheck // docker/cli stack deploy helpers are deprecated but required for Swarm stack deploy behavior.
	if err := stackswarm.RunDeploy(ctx, dockerCli, flagSet, &deployOpts, composeConfig); err != nil {
		return nil, fmt.Errorf("failed to deploy swarm stack: %w", err)
	}

	return &swarmtypes.StackDeployResponse{Name: req.Name}, nil
}

func (s *SwarmService) GetSwarmInfo(ctx context.Context) (*swarmtypes.SwarmInfo, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	info, err := dockerClient.SwarmInspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	out := swarmtypes.NewSwarmInfo(info)
	return &out, nil
}

func (s *SwarmService) ensureSwarmManager(ctx context.Context) error {
	dockerClient, err := s.dockerService.GetClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	info, err := dockerClient.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Docker info: %w", err)
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return ErrSwarmNotEnabled
	}
	if !info.Swarm.ControlAvailable {
		return ErrSwarmManagerRequired
	}

	return nil
}

func (s *SwarmService) buildServicePaginationConfig() pagination.Config[swarmtypes.ServiceSummary] {
	return pagination.Config[swarmtypes.ServiceSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.ServiceSummary]{
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Name, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Image, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.ID, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.StackName, nil },
			func(svc swarmtypes.ServiceSummary) (string, error) { return svc.Mode, nil },
		},
		SortBindings: []pagination.SortBinding[swarmtypes.ServiceSummary]{
			{Key: "name", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "image", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Image, b.Image) }},
			{Key: "mode", Fn: func(a, b swarmtypes.ServiceSummary) int { return strings.Compare(a.Mode, b.Mode) }},
			{Key: "replicas", Fn: func(a, b swarmtypes.ServiceSummary) int { return compareUint64(a.Replicas, b.Replicas) }},
			{Key: "created", Fn: func(a, b swarmtypes.ServiceSummary) int { return compareTime(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.ServiceSummary) int { return compareTime(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildNodePaginationConfig() pagination.Config[swarmtypes.NodeSummary] {
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
			{Key: "created", Fn: func(a, b swarmtypes.NodeSummary) int { return compareTime(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.NodeSummary) int { return compareTime(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildTaskPaginationConfig() pagination.Config[swarmtypes.TaskSummary] {
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
			{Key: "created", Fn: func(a, b swarmtypes.TaskSummary) int { return compareTime(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.TaskSummary) int { return compareTime(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func (s *SwarmService) buildStackPaginationConfig() pagination.Config[swarmtypes.StackSummary] {
	return pagination.Config[swarmtypes.StackSummary]{
		SearchAccessors: []pagination.SearchAccessor[swarmtypes.StackSummary]{
			func(stack swarmtypes.StackSummary) (string, error) { return stack.Name, nil },
			func(stack swarmtypes.StackSummary) (string, error) { return stack.Namespace, nil },
		},
		SortBindings: []pagination.SortBinding[swarmtypes.StackSummary]{
			{Key: "name", Fn: func(a, b swarmtypes.StackSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "services", Fn: func(a, b swarmtypes.StackSummary) int { return compareInt(a.Services, b.Services) }},
			{Key: "created", Fn: func(a, b swarmtypes.StackSummary) int { return compareTime(a.CreatedAt, b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.StackSummary) int { return compareTime(a.UpdatedAt, b.UpdatedAt) }},
		},
	}
}

func buildPaginationResponse[T any](result pagination.FilterResult[T], params pagination.QueryParams) pagination.Response {
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

func compareTime(a, b time.Time) int {
	if a.Before(b) {
		return -1
	}
	if a.After(b) {
		return 1
	}
	return 0
}

func compareUint64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func compareInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (s *SwarmService) loadStackComposeConfig(ctx context.Context, composeContent, envContent string) (*composetypes.Config, error) {
	composeContent = strings.TrimSpace(composeContent)
	if composeContent == "" {
		return nil, errors.New("compose content is required")
	}

	// Parse environment variables
	envMap, err := parseEnvContent(envContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse env content: %w", err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "/tmp"
	}

	// Use compose-go v2 for parsing (same as projects utils)
	configDetails := composegotypes.ConfigDetails{
		Version:    api.ComposeVersion,
		WorkingDir: workingDir,
		ConfigFiles: []composegotypes.ConfigFile{
			{Content: []byte(composeContent)},
		},
		Environment: composegotypes.Mapping(envMap),
	}

	project, err := composegoloader.LoadWithContext(ctx, configDetails)
	if err != nil {
		return nil, fmt.Errorf("failed to load compose project: %w", err)
	}

	// Convert compose-go types to docker/cli types for stack deploy
	config := convertComposeGoToDockerCLI(project)
	return config, nil
}

// convertComposeGoToDockerCLI converts compose-go v2 Project to docker/cli Config
func convertComposeGoToDockerCLI(project *composegotypes.Project) *composetypes.Config {
	config := &composetypes.Config{
		Filename: project.ComposeFiles[0],
		Version:  "3",
		Services: make([]composetypes.ServiceConfig, 0, len(project.Services)),
		Networks: make(map[string]composetypes.NetworkConfig),
		Volumes:  make(map[string]composetypes.VolumeConfig),
		Secrets:  make(map[string]composetypes.SecretConfig),
		Configs:  make(map[string]composetypes.ConfigObjConfig),
	}

	// Convert services
	for _, svc := range project.Services {
		serviceConfig := composetypes.ServiceConfig{
			Name:            svc.Name,
			Image:           svc.Image,
			Command:         composetypes.ShellCommand(svc.Command),
			Entrypoint:      composetypes.ShellCommand(svc.Entrypoint),
			Environment:     convertEnvironment(svc.Environment),
			Labels:          composetypes.Labels(svc.Labels),
			Networks:        convertServiceNetworks(svc.Networks),
			Volumes:         convertVolumes(svc.Volumes),
			Secrets:         convertServiceSecrets(svc.Secrets),
			Configs:         convertServiceConfigs(svc.Configs),
			Deploy:          convertDeploy(svc.Deploy),
			HealthCheck:     convertHealthCheck(svc.HealthCheck),
			WorkingDir:      svc.WorkingDir,
			User:            svc.User,
			Hostname:        svc.Hostname,
			StopSignal:      svc.StopSignal,
			StopGracePeriod: convertDuration(svc.StopGracePeriod),
		}
		config.Services = append(config.Services, serviceConfig)
	}

	// Convert networks
	for name, nw := range project.Networks {
		config.Networks[name] = composetypes.NetworkConfig{
			Name:       nw.Name,
			Driver:     nw.Driver,
			DriverOpts: nw.DriverOpts,
			Labels:     composetypes.Labels(nw.Labels),
			External:   composetypes.External{External: bool(nw.External)},
			Internal:   nw.Internal,
			Attachable: nw.Attachable,
		}
	}

	// Convert volumes
	for name, vol := range project.Volumes {
		config.Volumes[name] = composetypes.VolumeConfig{
			Name:       vol.Name,
			Driver:     vol.Driver,
			DriverOpts: vol.DriverOpts,
			Labels:     composetypes.Labels(vol.Labels),
			External:   composetypes.External{External: bool(vol.External)},
		}
	}

	// Convert secrets
	for name, secret := range project.Secrets {
		config.Secrets[name] = composetypes.SecretConfig{
			Name:     secret.Name,
			File:     secret.File,
			External: composetypes.External{External: bool(secret.External)},
			Labels:   composetypes.Labels(secret.Labels),
		}
	}

	// Convert configs
	for name, cfg := range project.Configs {
		config.Configs[name] = composetypes.ConfigObjConfig{
			Name:     cfg.Name,
			File:     cfg.File,
			External: composetypes.External{External: bool(cfg.External)},
			Labels:   composetypes.Labels(cfg.Labels),
		}
	}

	return config
}

func convertEnvironment(env composegotypes.MappingWithEquals) composetypes.MappingWithEquals {
	if env == nil {
		return nil
	}
	result := make(composetypes.MappingWithEquals)
	for k, v := range env {
		result[k] = v
	}
	return result
}

func convertServiceNetworks(networks map[string]*composegotypes.ServiceNetworkConfig) map[string]*composetypes.ServiceNetworkConfig {
	if networks == nil {
		return nil
	}
	result := make(map[string]*composetypes.ServiceNetworkConfig)
	for name, nw := range networks {
		if nw == nil {
			result[name] = nil
			continue
		}
		result[name] = &composetypes.ServiceNetworkConfig{
			Aliases:     nw.Aliases,
			Ipv4Address: nw.Ipv4Address,
			Ipv6Address: nw.Ipv6Address,
		}
	}
	return result
}

func convertVolumes(volumes []composegotypes.ServiceVolumeConfig) []composetypes.ServiceVolumeConfig {
	if volumes == nil {
		return nil
	}
	result := make([]composetypes.ServiceVolumeConfig, len(volumes))
	for i, vol := range volumes {
		result[i] = composetypes.ServiceVolumeConfig{
			Type:     vol.Type,
			Source:   vol.Source,
			Target:   vol.Target,
			ReadOnly: vol.ReadOnly,
		}
	}
	return result
}

func convertServiceSecrets(secrets []composegotypes.ServiceSecretConfig) []composetypes.ServiceSecretConfig {
	if secrets == nil {
		return nil
	}
	result := make([]composetypes.ServiceSecretConfig, len(secrets))
	for i, secret := range secrets {
		result[i] = composetypes.ServiceSecretConfig{
			Source: secret.Source,
			Target: secret.Target,
			UID:    secret.UID,
			GID:    secret.GID,
			Mode:   convertFileMode(secret.Mode),
		}
	}
	return result
}

func convertServiceConfigs(configs []composegotypes.ServiceConfigObjConfig) []composetypes.ServiceConfigObjConfig {
	if configs == nil {
		return nil
	}
	result := make([]composetypes.ServiceConfigObjConfig, len(configs))
	for i, cfg := range configs {
		result[i] = composetypes.ServiceConfigObjConfig{
			Source: cfg.Source,
			Target: cfg.Target,
			UID:    cfg.UID,
			GID:    cfg.GID,
			Mode:   convertFileMode(cfg.Mode),
		}
	}
	return result
}

func convertDeploy(deploy *composegotypes.DeployConfig) composetypes.DeployConfig {
	if deploy == nil {
		return composetypes.DeployConfig{}
	}
	var replicas *uint64
	if deploy.Replicas != nil {
		if r, ok := toUint64FromInt(*deploy.Replicas); ok {
			replicas = &r
		}
	}
	result := composetypes.DeployConfig{
		Mode:     deploy.Mode,
		Replicas: replicas,
		Labels:   composetypes.Labels(deploy.Labels),
	}
	if deploy.Placement.Constraints != nil {
		result.Placement.Constraints = append([]string{}, deploy.Placement.Constraints...)
	}
	return result
}

func convertHealthCheck(hc *composegotypes.HealthCheckConfig) *composetypes.HealthCheckConfig {
	if hc == nil {
		return nil
	}
	return &composetypes.HealthCheckConfig{
		Test:        composetypes.HealthCheckTest(hc.Test),
		Timeout:     convertDuration(hc.Timeout),
		Interval:    convertDuration(hc.Interval),
		Retries:     hc.Retries,
		StartPeriod: convertDuration(hc.StartPeriod),
		Disable:     hc.Disable,
	}
}

func convertDuration(d *composegotypes.Duration) *composetypes.Duration {
	if d == nil {
		return nil
	}
	result := composetypes.Duration(*d)
	return &result
}

func convertFileMode(mode *composegotypes.FileMode) *uint32 {
	if mode == nil {
		return nil
	}
	if result, ok := toUint32FromInt64(int64(*mode)); ok {
		return &result
	}
	return nil
}

func toUint64FromInt(value int) (uint64, bool) {
	if value < 0 {
		return 0, false
	}
	return uint64(value), true
}

func toUint32FromInt64(value int64) (uint32, bool) {
	if value < 0 || value > int64(^uint32(0)) {
		return 0, false
	}
	return uint32(value), true
}

func parseEnvContent(envContent string) (map[string]string, error) {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}

	if strings.TrimSpace(envContent) == "" {
		return env, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(envContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return env, nil
}
