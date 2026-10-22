package swarm

import (
	"bytes"
	"cmp"
	"context"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/configs"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/nodes"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/secrets"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/services"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/stacks"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

const (
	KVKeySwarmEnabled      = "swarm.enabled"
	defaultSwarmListenAddr = "0.0.0.0:2377"
)

// SwarmService provides Docker Swarm cluster operations and coordinates its
// services, nodes, stacks, configs, and secrets features.
type SwarmService struct {
	dockerService *docker.DockerClientService
	kvService     *kv.KVService

	services *services.Service
	nodes    *nodes.Service
	stacks   *stacks.Service
	configs  *configs.Service
	secrets  *secrets.Service
}

func NewSwarmService(
	dockerService *docker.DockerClientService,
	settingsService *settings.SettingsService,
	kvService *kv.KVService,
	registryService *registry.ContainerRegistryService,
	environmentService *environment.EnvironmentService,
) *SwarmService {
	s := &SwarmService{dockerService: dockerService, kvService: kvService}

	var registryAuth func(context.Context, string) (string, error)
	if registryService != nil {
		registryAuth = registryService.GetRegistryAuthForImage
	}

	s.services = services.NewService(dockerService, s.ensureSwarmManager, s.listTasksPaginatedWithFilters)
	s.nodes = nodes.NewService(dockerService, s.ensureSwarmManager, s.listTasksPaginatedWithFilters, s.GetSwarmJoinTokens, environmentService)
	s.stacks = stacks.NewService(dockerService, s.ensureSwarmManager, s.listTasksPaginatedWithFilters, s.services.PaginateSummaries, registryAuth, settingsService)
	s.configs = configs.NewService(dockerService, s.ensureSwarmManager)
	s.secrets = secrets.NewService(dockerService, s.ensureSwarmManager)
	return s
}

// DeployStack deploys a swarm stack; GitOps sync uses it as the domain API.
func (s *SwarmService) DeployStack(ctx context.Context, environmentID string, req types.SwarmStackDeployRequest) (*types.SwarmStackDeployResponse, error) {
	return s.stacks.DeployStack(ctx, environmentID, req)
}

// StreamServiceLogs streams swarm service logs for the WebSocket log endpoint.
func (s *SwarmService) StreamServiceLogs(ctx context.Context, serviceID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	return s.services.StreamServiceLogs(ctx, serviceID, logsChan, follow, tail, since, timestamps)
}

func (s *SwarmService) IsEnabled(ctx context.Context) (bool, error) {
	if s.kvService == nil {
		return false, nil
	}

	enabled, err := s.kvService.GetBool(ctx, KVKeySwarmEnabled, false)
	if err != nil {
		return false, fmt.Errorf("failed to read swarm enabled state: %w", err)
	}

	return enabled, nil
}

func (s *SwarmService) ListTasksPaginated(ctx context.Context, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	return s.listTasksPaginatedWithFilters(ctx, nil, params)
}

func (s *SwarmService) GetSwarmInfo(ctx context.Context) (*types.SwarmInfo, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	infoResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmInspectResult, error) {
		return dockerClient.SwarmInspect(ctx, client.SwarmInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	return new(types.NewSwarmInfo(infoResult.Swarm)), nil
}

func (s *SwarmService) InitSwarm(ctx context.Context, req types.SwarmInitRequest) (*types.SwarmInitResponse, error) {
	if !req.ForceNewCluster {
		if err := s.ensureSwarmInactive(ctx); err != nil {
			return nil, err
		}
	}
	spec, err := decodeSwarmSpec(req.Spec)
	if err != nil {
		return nil, err
	}

	defaultAddrPool := make([]netip.Prefix, 0, len(req.DefaultAddrPool))
	for _, raw := range req.DefaultAddrPool {
		prefix, parsePrefixErr := netip.ParsePrefix(raw)
		if parsePrefixErr != nil {
			return nil, fmt.Errorf("failed to parse default address pool %q: %w", raw, parsePrefixErr)
		}
		defaultAddrPool = append(defaultAddrPool, prefix.Masked())
	}

	initResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmInitResult, error) {
		return dockerClient.SwarmInit(ctx, client.SwarmInitOptions{
			ListenAddr:       cmp.Or(strings.TrimSpace(req.ListenAddr), defaultSwarmListenAddr),
			AdvertiseAddr:    req.AdvertiseAddr,
			DataPathAddr:     req.DataPathAddr,
			DataPathPort:     req.DataPathPort,
			ForceNewCluster:  req.ForceNewCluster,
			Spec:             spec,
			AutoLockManagers: req.AutoLockManagers,
			Availability:     req.Availability,
			DefaultAddrPool:  defaultAddrPool,
			SubnetSize:       req.SubnetSize,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize swarm: %w", err)
	}

	s.persistSwarmEnabledState(ctx, true)

	return &types.SwarmInitResponse{NodeID: initResult.NodeID}, nil
}

func (s *SwarmService) JoinSwarm(ctx context.Context, req types.SwarmJoinRequest) error {
	if err := s.ensureSwarmInactive(ctx); err != nil {
		return err
	}
	if _, swarmJoinErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmJoinResult, error) {
		return dockerClient.SwarmJoin(ctx, client.SwarmJoinOptions{
			ListenAddr:    cmp.Or(strings.TrimSpace(req.ListenAddr), defaultSwarmListenAddr),
			AdvertiseAddr: req.AdvertiseAddr,
			DataPathAddr:  req.DataPathAddr,
			RemoteAddrs:   req.RemoteAddrs,
			JoinToken:     req.JoinToken,
			Availability:  req.Availability,
		})
	}); swarmJoinErr != nil {
		return fmt.Errorf("failed to join swarm: %w", swarmJoinErr)
	}

	s.persistSwarmEnabledState(ctx, true)

	return nil
}

func (s *SwarmService) LeaveSwarm(ctx context.Context, req types.SwarmLeaveRequest) error {
	if _, err := s.ensureSwarmActive(ctx); err != nil {
		return err
	}

	if _, swarmLeaveErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmLeaveResult, error) {
		return dockerClient.SwarmLeave(ctx, client.SwarmLeaveOptions{Force: req.Force})
	}); swarmLeaveErr != nil {
		return fmt.Errorf("failed to leave swarm: %w", swarmLeaveErr)
	}

	s.persistSwarmEnabledState(ctx, false)

	return nil
}

func (s *SwarmService) UnlockSwarm(ctx context.Context, req types.SwarmUnlockRequest) error {
	info, err := s.getDockerInfo(ctx)
	if err != nil {
		return err
	}

	if info.Swarm.LocalNodeState != swarm.LocalNodeStateLocked && info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return common.Classify(common.ErrSwarmNotEnabled, errors.New("Swarm mode is not enabled")) //nolint:staticcheck // Preserve the existing error message.
	}

	if _, swarmUnlockErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmUnlockResult, error) {
		return dockerClient.SwarmUnlock(ctx, client.SwarmUnlockOptions{Key: req.Key})
	}); swarmUnlockErr != nil {
		return fmt.Errorf("failed to unlock swarm: %w", swarmUnlockErr)
	}

	return nil
}

func (s *SwarmService) GetSwarmUnlockKey(ctx context.Context) (*types.SwarmUnlockKeyResponse, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	unlockResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmGetUnlockKeyResult, error) {
		return dockerClient.SwarmGetUnlockKey(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get swarm unlock key: %w", err)
	}

	return &types.SwarmUnlockKeyResponse{UnlockKey: unlockResult.Key}, nil
}

func (s *SwarmService) GetSwarmJoinTokens(ctx context.Context) (*types.SwarmJoinTokensResponse, error) {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return nil, err
	}

	infoResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmInspectResult, error) {
		return dockerClient.SwarmInspect(ctx, client.SwarmInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm: %w", err)
	}

	return &types.SwarmJoinTokensResponse{
		Worker:  infoResult.Swarm.JoinTokens.Worker,
		Manager: infoResult.Swarm.JoinTokens.Manager,
	}, nil
}

func (s *SwarmService) RotateSwarmJoinTokens(ctx context.Context, req types.SwarmRotateJoinTokensRequest) error {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return err
	}

	infoResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmInspectResult, error) {
		return dockerClient.SwarmInspect(ctx, client.SwarmInspectOptions{})
	})
	if err != nil {
		return fmt.Errorf("failed to inspect swarm: %w", err)
	}

	rotateWorker := req.RotateWorkerToken
	rotateManager := req.RotateManagerToken
	if !rotateWorker && !rotateManager {
		rotateWorker = true
		rotateManager = true
	}

	if _, swarmUpdateErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmUpdateResult, error) {
		return dockerClient.SwarmUpdate(ctx, client.SwarmUpdateOptions{
			Version:            infoResult.Swarm.Version,
			Spec:               infoResult.Swarm.Spec,
			RotateWorkerToken:  rotateWorker,
			RotateManagerToken: rotateManager,
		})
	}); swarmUpdateErr != nil {
		return fmt.Errorf("failed to rotate swarm join tokens: %w", swarmUpdateErr)
	}

	return nil
}

func (s *SwarmService) UpdateSwarmSpec(ctx context.Context, req types.SwarmUpdateRequest) error {
	if err := s.ensureSwarmManager(ctx); err != nil {
		return err
	}

	version := req.Version
	if version == 0 {
		infoResult, swarmInspectErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmInspectResult, error) {
			return dockerClient.SwarmInspect(ctx, client.SwarmInspectOptions{})
		})
		if swarmInspectErr != nil {
			return fmt.Errorf("failed to inspect swarm: %w", swarmInspectErr)
		}
		version = infoResult.Swarm.Version.Index
	}

	spec, err := decodeSwarmSpec(req.Spec)
	if err != nil {
		return err
	}

	if _, swarmUpdateErr := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SwarmUpdateResult, error) {
		return dockerClient.SwarmUpdate(ctx, client.SwarmUpdateOptions{
			Version:                swarm.Version{Index: version},
			Spec:                   spec,
			RotateWorkerToken:      req.RotateWorkerToken,
			RotateManagerToken:     req.RotateManagerToken,
			RotateManagerUnlockKey: req.RotateManagerUnlockKey,
		})
	}); swarmUpdateErr != nil {
		return fmt.Errorf("failed to update swarm spec: %w", swarmUpdateErr)
	}

	return nil
}

func (s *SwarmService) listTasksPaginatedWithFilters(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error) {
	servicesResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceListResult, error) {
		return dockerClient.ServiceList(ctx, client.ServiceListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}

	serviceNameByID := make(map[string]string, len(servicesResult.Items))
	for _, service := range servicesResult.Items {
		serviceNameByID[service.ID] = service.Spec.Name
	}

	nodesResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeListResult, error) {
		return dockerClient.NodeList(ctx, client.NodeListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	nodeNameByID := make(map[string]string, len(nodesResult.Items))
	for _, node := range nodesResult.Items {
		nodeNameByID[node.ID] = node.Description.Hostname
	}

	if filters == nil {
		filters = make(client.Filters)
	}
	tasksResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.TaskListResult, error) {
		return dockerClient.TaskList(ctx, client.TaskListOptions{Filters: filters})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm tasks: %w", err)
	}

	items := make([]types.SwarmTaskSummary, 0, len(tasksResult.Items))
	for _, task := range tasksResult.Items {
		items = append(items, types.NewSwarmTaskSummary(task, serviceNameByID[task.ServiceID], nodeNameByID[task.NodeID]))
	}

	config := pagination.Config[types.SwarmTaskSummary]{
		SearchAccessors: []pagination.SearchAccessor[types.SwarmTaskSummary]{
			func(task types.SwarmTaskSummary) (string, error) { return task.Name, nil },
			func(task types.SwarmTaskSummary) (string, error) { return task.ServiceName, nil },
			func(task types.SwarmTaskSummary) (string, error) { return task.NodeName, nil },
			func(task types.SwarmTaskSummary) (string, error) { return task.ID, nil },
			func(task types.SwarmTaskSummary) (string, error) { return task.CurrentState, nil },
		},
		SortBindings: []pagination.SortBinding[types.SwarmTaskSummary]{
			{Key: "service", Fn: func(a, b types.SwarmTaskSummary) int { return strings.Compare(a.ServiceName, b.ServiceName) }},
			{Key: "node", Fn: func(a, b types.SwarmTaskSummary) int { return strings.Compare(a.NodeName, b.NodeName) }},
			{Key: "state", Fn: func(a, b types.SwarmTaskSummary) int { return strings.Compare(a.CurrentState, b.CurrentState) }},
			{Key: "created", Fn: func(a, b types.SwarmTaskSummary) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b types.SwarmTaskSummary) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)
	return result.Items, paginationResp, nil
}

func decodeSwarmSpec(raw stdjson.RawMessage) (swarm.Spec, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return swarm.Spec{}, errors.New("swarm spec is required")
	}

	var spec swarm.Spec
	// Docker encodes durations as integer nanoseconds.
	if err := json.Unmarshal(trimmed, &spec, stdjson.FormatDurationAsNano(true)); err != nil {
		return swarm.Spec{}, fmt.Errorf("failed to parse swarm spec: %w", err)
	}

	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}

	return spec, nil
}

func (s *SwarmService) ensureSwarmManager(ctx context.Context) error {
	info, err := s.ensureSwarmActive(ctx)
	if err != nil {
		return err
	}
	if !info.Swarm.ControlAvailable {
		return common.Classify(common.ErrSwarmManagerRequired, errors.New("Swarm manager access required")) //nolint:staticcheck // Preserve the existing error message.
	}

	return nil
}

func (s *SwarmService) ensureSwarmActive(ctx context.Context) (system.Info, error) {
	info, err := s.getDockerInfo(ctx)
	if err != nil {
		return system.Info{}, err
	}
	if info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return system.Info{}, common.Classify(common.ErrSwarmNotEnabled, errors.New("Swarm mode is not enabled")) //nolint:staticcheck // Preserve the existing error message.
	}
	return info, nil
}

func (s *SwarmService) ensureSwarmInactive(ctx context.Context) error {
	info, err := s.getDockerInfo(ctx)
	if err != nil {
		return err
	}
	switch info.Swarm.LocalNodeState {
	case swarm.LocalNodeStateActive, swarm.LocalNodeStatePending, swarm.LocalNodeStateLocked:
		return common.Classify(common.ErrConflict, errors.New("this node is already part of a swarm"))
	default:
		return nil
	}
}

func (s *SwarmService) getDockerInfo(ctx context.Context) (system.Info, error) {
	infoResult, err := s.dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SystemInfoResult, error) {
		return dockerClient.Info(ctx, client.InfoOptions{})
	})
	if err != nil {
		return system.Info{}, fmt.Errorf("failed to get Docker info: %w", err)
	}

	return infoResult.Info, nil
}

func (s *SwarmService) SyncSwarmEnabledState(ctx context.Context) error {
	info, err := s.getDockerInfo(ctx)
	if err != nil {
		return err
	}

	enabled := info.Swarm.LocalNodeState == swarm.LocalNodeStateActive && strings.TrimSpace(info.Swarm.NodeID) != ""
	if s.kvService == nil {
		return nil
	}

	if setBoolErr := s.kvService.SetBool(ctx, KVKeySwarmEnabled, enabled); setBoolErr != nil {
		return fmt.Errorf("persist swarm enabled state: %w", setBoolErr)
	}

	return nil
}

func (s *SwarmService) persistSwarmEnabledState(ctx context.Context, enabled bool) {
	if s.kvService == nil {
		return
	}

	if err := s.kvService.SetBool(ctx, KVKeySwarmEnabled, enabled); err != nil {
		slog.WarnContext(ctx, "Failed to persist swarm enabled state", "enabled", enabled, "error", err)
	}
}
