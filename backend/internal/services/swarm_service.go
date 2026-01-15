package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	libswarm "github.com/getarcaneapp/arcane/backend/pkg/libarcane/swarm"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
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

	return &swarmtypes.StackDeployResponse{Name: stackName}, nil
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
