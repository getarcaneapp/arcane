package nodes

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"go.getarcane.app/kit/pkg"
	"golang.org/x/sync/errgroup"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	swarmNodeIdentityProbeConcurrency = 5
	swarmNodeIdentityCacheTTL         = 30 * time.Second
)

// Service manages swarm nodes, their agent bindings, and Easy Join.
type Service struct {
	dockerClient       func(ctx context.Context) (*client.Client, error)
	ensureManager      func(ctx context.Context) error
	listTasks          func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error)
	localJoinTokens    func(ctx context.Context) (*swarmtypes.SwarmJoinTokensResponse, error)
	environmentService *environment.EnvironmentService
	identityCache      *hot.HotCache[string, SwarmNodeIdentity]
}

func NewService(
	dockerClient func(ctx context.Context) (*client.Client, error),
	ensureManager func(ctx context.Context) error,
	listTasks func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error),
	localJoinTokens func(ctx context.Context) (*swarmtypes.SwarmJoinTokensResponse, error),
	environmentService *environment.EnvironmentService,
) *Service {
	return &Service{
		dockerClient:       dockerClient,
		ensureManager:      ensureManager,
		listTasks:          listTasks,
		localJoinTokens:    localJoinTokens,
		environmentService: environmentService,
		identityCache: hot.NewHotCache[string, SwarmNodeIdentity](hot.LRU, 512).
			WithTTL(swarmNodeIdentityCacheTTL).
			WithJanitor().
			Build(),
	}
}

type SwarmNodeIdentity struct {
	SwarmNodeID   string `json:"swarmNodeId"`
	Hostname      string `json:"hostname"`
	Role          string `json:"role"`
	EngineVersion string `json:"engineVersion"`
	SwarmActive   bool   `json:"swarmActive"`
}

type swarmNodeAgentRuntime struct {
	connected     bool
	lastHeartbeat *time.Time
	lastPollAt    *time.Time
	identity      *SwarmNodeIdentity
}

type swarmNodeAgentCoverage struct {
	runtimeByEnvID     map[string]swarmNodeAgentRuntime
	boundEnvsByNodeID  map[string][]environment.Environment
	candidatesByNodeID map[string][]environment.Environment
	localIdentity      *SwarmNodeIdentity
	// Resolved once per request rather than per node: the name is user-editable, and
	// applyNodeAgentCoverageInternal runs in a loop with no context to look it up.
	localEnvironmentName string
}

func (s *Service) ListNodesPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]swarmtypes.NodeSummary, pagination.Response, error) {
	if environmentID != "0" && s.environmentService != nil {
		var remote struct {
			Success bool                     `json:"success"`
			Data    []swarmtypes.NodeSummary `json:"data"`
		}
		if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/environments/0/swarm/nodes?limit=-1", nil, &remote); err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to list remote swarm nodes: %w", err)
		}
		if !remote.Success {
			return nil, pagination.Response{}, errors.New("remote swarm node listing failed")
		}

		s.enrichNodeAgentStatusesInternal(ctx, environmentID, remote.Data)
		result := s.buildNodePaginationConfigInternal().SearchOrderAndPaginate(remote.Data, params)
		return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
	}

	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodesResult, err := dockerClient.NodeList(ctx, client.NodeListOptions{})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	nodes := nodesResult.Items

	items := make([]swarmtypes.NodeSummary, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, swarmtypes.NewNodeSummary(node))
	}

	s.enrichNodeAgentStatusesInternal(ctx, environmentID, items)

	config := s.buildNodePaginationConfigInternal()
	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *Service) GetNode(ctx context.Context, environmentID, nodeID string) (*swarmtypes.NodeSummary, error) {
	if environmentID != "0" && s.environmentService != nil {
		var remote struct {
			Success bool                   `json:"success"`
			Data    swarmtypes.NodeSummary `json:"data"`
		}
		remotePath := "/api/environments/0/swarm/nodes/" + nodeID
		if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, remotePath, nil, &remote); err != nil {
			return nil, fmt.Errorf("failed to inspect remote swarm node: %w", err)
		}
		if !remote.Success {
			return nil, errors.New("remote swarm node inspection failed")
		}
		items := []swarmtypes.NodeSummary{remote.Data}
		s.enrichNodeAgentStatusesInternal(ctx, environmentID, items)
		return &items[0], nil
	}

	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodeResult, err := dockerClient.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	items := []swarmtypes.NodeSummary{swarmtypes.NewNodeSummary(nodeResult.Node)}
	s.enrichNodeAgentStatusesInternal(ctx, environmentID, items)
	return new(items[0]), nil
}

// ReconcileNodeAgents verifies visible environment identities and persists only
// unique node matches. Ambiguous and mismatched identities remain unchanged.
func (s *Service) ReconcileNodeAgents(ctx context.Context, environmentID string) (*swarmtypes.NodeAgentReconcileResponse, error) {
	items, _, err := s.ListNodesPaginated(ctx, environmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, err
	}

	results := make([]swarmtypes.NodeAgentReconcileResult, 0, len(items))
	for _, node := range items {
		result := swarmtypes.NodeAgentReconcileResult{
			NodeID:        node.ID,
			State:         node.Agent.State,
			EnvironmentID: node.Agent.EnvironmentID,
			Candidates:    node.Agent.Candidates,
		}
		if node.Agent.State == swarmtypes.NodeAgentStateMismatched || len(node.Agent.Candidates) != 1 {
			results = append(results, result)
			continue
		}

		candidate := node.Agent.Candidates[0]
		bound, bindErr := s.environmentService.BindSwarmNodeEnvironment(ctx, environmentID, node.ID, candidate.EnvironmentID, false)
		if bindErr != nil {
			slog.WarnContext(ctx, "failed to persist reconciled swarm node binding", "environmentId", candidate.EnvironmentID, "nodeId", node.ID, "error", bindErr.Error())
			results = append(results, result)
			continue
		}

		result.State = swarmtypes.NodeAgentStateConnected
		result.EnvironmentID = &bound.ID
		result.Candidates = nil
		results = append(results, result)
	}

	return &swarmtypes.NodeAgentReconcileResponse{Results: results}, nil
}

// BindNodeAgent verifies that a visible environment currently reports the
// requested node identity before persisting the binding.
func (s *Service) BindNodeAgent(ctx context.Context, parentEnvironmentID, nodeID string, request swarmtypes.NodeAgentBindingRequest) (*environment.Environment, error) {
	localEnvironment, err := s.environmentService.GetEnvironmentByID(ctx, request.EnvironmentID)
	if err != nil {
		return nil, err
	}
	if localEnvironment.Hidden {
		return nil, errors.New("only visible environments can be attached")
	}

	runtime := s.resolveSwarmNodeAgentRuntimeInternal(ctx, localEnvironment)
	if runtime.identity == nil || !runtime.identity.SwarmActive {
		return nil, errors.New("environment did not report an active swarm identity")
	}
	if strings.TrimSpace(runtime.identity.SwarmNodeID) != strings.TrimSpace(nodeID) {
		return nil, fmt.Errorf("environment reports swarm node %s instead of %s", runtime.identity.SwarmNodeID, nodeID)
	}

	bound, err := s.environmentService.BindSwarmNodeEnvironment(ctx, parentEnvironmentID, nodeID, request.EnvironmentID, request.Rebind)
	if err != nil {
		return nil, err
	}
	return bound, nil
}

func (s *Service) GetLocalNodeIdentity(ctx context.Context) (*SwarmNodeIdentity, error) {
	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	infoResult, err := dockerClient.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect local Docker engine: %w", err)
	}

	swarmInfo := infoResult.Info
	swarmActive := swarmInfo.Swarm.LocalNodeState == swarm.LocalNodeStateActive && strings.TrimSpace(swarmInfo.Swarm.NodeID) != ""
	role := ""
	if swarmActive {
		role = kit.Ternary(swarmInfo.Swarm.ControlAvailable, "manager", "worker")
	}

	return &SwarmNodeIdentity{
		SwarmNodeID:   strings.TrimSpace(swarmInfo.Swarm.NodeID),
		Hostname:      strings.TrimSpace(swarmInfo.Name),
		Role:          role,
		EngineVersion: strings.TrimSpace(swarmInfo.ServerVersion),
		SwarmActive:   swarmActive,
	}, nil
}

func (s *Service) enrichNodeAgentStatusesInternal(ctx context.Context, environmentID string, items []swarmtypes.NodeSummary) {
	if s.environmentService == nil || len(items) == 0 || strings.TrimSpace(environmentID) == "" {
		return
	}
	coverage, err := s.resolveNodeAgentCoverageInternal(ctx, environmentID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve swarm node agent coverage", "environmentId", environmentID, "error", err.Error())
		return
	}

	for index := range items {
		item := &items[index]
		s.applyNodeAgentCoverageInternal(environmentID, item, coverage)
	}
}

func (s *Service) resolveNodeAgentCoverageInternal(ctx context.Context, environmentID string) (*swarmNodeAgentCoverage, error) {
	agentEnvs, err := s.environmentService.ListSwarmNodeAgentEnvironments(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	candidateEnvs, err := s.environmentService.ListSwarmNodeCandidateEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load swarm node agent candidates", "environmentId", environmentID, "error", err.Error())
		candidateEnvs = nil
	}

	probeEnvsByID := make(map[string]environment.Environment, len(agentEnvs)+len(candidateEnvs))
	for _, env := range candidateEnvs {
		probeEnvsByID[env.ID] = env
	}
	for _, env := range agentEnvs {
		probeEnvsByID[env.ID] = env
	}

	runtimeByEnvID := make(map[string]swarmNodeAgentRuntime, len(probeEnvsByID))
	var runtimeMu sync.Mutex
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(swarmNodeIdentityProbeConcurrency)
	for _, candidate := range probeEnvsByID {
		env := candidate
		g.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "swarm worker")

			runtime := s.resolveSwarmNodeAgentRuntimeInternal(groupCtx, &env)
			runtimeMu.Lock()
			runtimeByEnvID[env.ID] = runtime
			runtimeMu.Unlock()
			return nil
		})
	}
	if waitErr := g.Wait(); waitErr != nil {
		return nil, waitErr
	}

	boundEnvsByNodeID := make(map[string][]environment.Environment, len(agentEnvs))
	for _, env := range agentEnvs {
		if env.SwarmNodeID != nil && strings.TrimSpace(*env.SwarmNodeID) != "" {
			nodeID := strings.TrimSpace(*env.SwarmNodeID)
			boundEnvsByNodeID[nodeID] = append(boundEnvsByNodeID[nodeID], env)
		}
	}

	candidatesByNodeID := make(map[string][]environment.Environment)
	for _, env := range candidateEnvs {
		runtime := runtimeByEnvID[env.ID]
		if runtime.identity == nil || !runtime.identity.SwarmActive {
			continue
		}
		nodeID := strings.TrimSpace(runtime.identity.SwarmNodeID)
		if nodeID != "" {
			candidatesByNodeID[nodeID] = append(candidatesByNodeID[nodeID], env)
		}
	}

	coverage := &swarmNodeAgentCoverage{
		runtimeByEnvID:     runtimeByEnvID,
		boundEnvsByNodeID:  boundEnvsByNodeID,
		candidatesByNodeID: candidatesByNodeID,
	}
	if environmentID == environment.LocalEnvironmentID {
		identity, identityErr := s.GetLocalNodeIdentity(ctx)
		if identityErr == nil {
			coverage.localIdentity = identity
		}
		coverage.localEnvironmentName = s.environmentService.ResolveEnvironmentName(ctx, environment.LocalEnvironmentID)
	}
	return coverage, nil
}

func (s *Service) applyNodeAgentCoverageInternal(environmentID string, item *swarmtypes.NodeSummary, coverage *swarmNodeAgentCoverage) {
	if item == nil || coverage == nil {
		return
	}
	nodeID := strings.TrimSpace(item.ID)
	if environmentID == environment.LocalEnvironmentID && coverage.localIdentity != nil && coverage.localIdentity.SwarmActive && strings.TrimSpace(coverage.localIdentity.SwarmNodeID) == nodeID {
		connected := true
		bindingKind := swarmtypes.NodeAgentBindingKindLocal
		localID, localType := environment.LocalEnvironmentID, "local"
		localName := environment.DisplayName(environment.LocalEnvironmentID, coverage.localEnvironmentName)
		item.Agent = swarmtypes.NodeAgentStatus{
			State:           swarmtypes.NodeAgentStateConnected,
			BindingKind:     &bindingKind,
			EnvironmentID:   &localID,
			EnvironmentName: &localName,
			EnvironmentType: &localType,
			Connected:       &connected,
		}
		return
	}

	boundEnvs := coverage.boundEnvsByNodeID[nodeID]
	if len(boundEnvs) > 0 {
		localEnvironment := preferredNodeAgentEnvironmentInternal(boundEnvs)
		item.Agent = s.buildNodeAgentStatusInternal(nodeID, &localEnvironment, coverage.runtimeByEnvID[localEnvironment.ID])
		return
	}

	candidates := coverage.candidatesByNodeID[nodeID]
	if len(candidates) > 1 {
		item.Agent = swarmtypes.NodeAgentStatus{State: swarmtypes.NodeAgentStateAmbiguous, Candidates: buildNodeAgentCandidatesInternal(candidates)}
		return
	}
	if len(candidates) == 1 {
		item.Agent.Candidates = buildNodeAgentCandidatesInternal(candidates)
	}
}

func (s *Service) resolveSwarmNodeAgentRuntimeInternal(ctx context.Context, env *environment.Environment) swarmNodeAgentRuntime {
	if env == nil {
		return swarmNodeAgentRuntime{}
	}

	runtime := swarmNodeAgentRuntime{
		connected: edge.HasActiveTunnel(env.ID),
	}

	if tunnelState, ok := edge.GetTunnelRuntimeState(env.ID).Get(); ok {
		runtime.lastHeartbeat = tunnelState.LastHeartbeat
	}

	if pollState, ok := edge.GetPollRuntimeRegistry().Get(env.ID, time.Now()).Get(); ok {
		runtime.lastPollAt = pollState.LastPollAt
	}

	if identity := s.cachedSwarmNodeIdentityInternal(env.ID); identity != nil {
		runtime.connected = true
		runtime.identity = identity
		return runtime
	}

	identity, err := s.fetchSwarmNodeIdentityViaEdgeInternal(ctx, env.ID)
	if err != nil {
		slog.DebugContext(ctx, "failed to probe swarm node identity", "environmentId", env.ID, "error", err.Error())
		return runtime
	}

	runtime.connected = true
	runtime.identity = identity
	s.cacheSwarmNodeIdentityInternal(env.ID, *identity)
	return runtime
}

func (s *Service) cachedSwarmNodeIdentityInternal(environmentID string) *SwarmNodeIdentity {
	if s.identityCache == nil {
		return nil
	}
	identity, ok, _ := s.identityCache.Get(environmentID)
	if !ok {
		return nil
	}
	return &identity
}

func (s *Service) cacheSwarmNodeIdentityInternal(environmentID string, identity SwarmNodeIdentity) {
	if s.identityCache != nil {
		s.identityCache.Set(environmentID, identity)
	}
}

func (s *Service) invalidateSwarmNodeIdentityInternal(environmentID string) {
	if s.identityCache != nil {
		s.identityCache.Delete(environmentID)
	}
}

// JoinEnvironments joins visible remote environments to the selected swarm
// manager and returns an independent result for every target.
func (s *Service) JoinEnvironments(ctx context.Context, managerEnvironmentID string, request swarmtypes.SwarmJoinEnvironmentsRequest) (*swarmtypes.SwarmJoinEnvironmentsResponse, error) {
	if len(request.Targets) == 0 {
		return &swarmtypes.SwarmJoinEnvironmentsResponse{Results: []swarmtypes.SwarmJoinEnvironmentResult{}}, nil
	}

	nodes, _, err := s.ListNodesPaginated(ctx, managerEnvironmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, err
	}
	remoteAddrs, err := selectSwarmManagerAddressesInternal(request.RemoteAddrs, nodes)
	if err != nil {
		return nil, err
	}
	tokens, err := s.getSwarmJoinTokensForEnvironmentInternal(ctx, managerEnvironmentID)
	if err != nil {
		return nil, err
	}
	memberNodeIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		memberNodeIDs[node.ID] = struct{}{}
	}

	results := make([]swarmtypes.SwarmJoinEnvironmentResult, len(request.Targets))
	processTargetInternal := func(index int) {
		target := request.Targets[index]
		results[index] = s.joinEnvironmentInternal(ctx, managerEnvironmentID, target, remoteAddrs, tokens, memberNodeIDs)
	}

	for index, target := range request.Targets {
		if target.Role == swarmtypes.SwarmJoinEnvironmentRoleManager {
			processTargetInternal(index)
		}
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(swarmNodeIdentityProbeConcurrency)
	for index, target := range request.Targets {
		if target.Role != swarmtypes.SwarmJoinEnvironmentRoleWorker {
			continue
		}
		group.Go(func() (workerErr error) {
			defer utils.RecoverToError(&workerErr, "swarm worker")

			if groupCtx.Err() == nil {
				processTargetInternal(index)
			}
			return nil
		})
	}
	_ = group.Wait()

	return &swarmtypes.SwarmJoinEnvironmentsResponse{Results: results}, nil
}

func (s *Service) getSwarmJoinTokensForEnvironmentInternal(ctx context.Context, environmentID string) (*swarmtypes.SwarmJoinTokensResponse, error) {
	if environmentID == "0" {
		return s.localJoinTokens(ctx)
	}

	var response struct {
		Success bool                               `json:"success"`
		Data    swarmtypes.SwarmJoinTokensResponse `json:"data"`
	}
	if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/environments/0/swarm/join-tokens", nil, &response); err != nil {
		return nil, fmt.Errorf("failed to get remote swarm join tokens: %w", err)
	}
	if !response.Success {
		return nil, errors.New("remote swarm join-token request failed")
	}
	return &response.Data, nil
}

func (s *Service) joinEnvironmentInternal(
	ctx context.Context,
	managerEnvironmentID string,
	target swarmtypes.SwarmJoinEnvironmentTarget,
	remoteAddrs []string,
	tokens *swarmtypes.SwarmJoinTokensResponse,
	memberNodeIDs map[string]struct{},
) swarmtypes.SwarmJoinEnvironmentResult {
	result := swarmtypes.SwarmJoinEnvironmentResult{EnvironmentID: target.EnvironmentID, State: swarmtypes.SwarmJoinEnvironmentResultFailed}
	localEnvironment, err := s.environmentService.GetEnvironmentByID(ctx, target.EnvironmentID)
	if err != nil || localEnvironment.Hidden || !localEnvironment.Enabled {
		message := "target environment is unavailable"
		if err != nil {
			message = err.Error()
		}
		result.Error = &message
		return result
	}

	runtime := s.resolveSwarmNodeAgentRuntimeInternal(ctx, localEnvironment)
	if runtime.identity != nil && runtime.identity.SwarmActive {
		nodeID := strings.TrimSpace(runtime.identity.SwarmNodeID)
		if _, member := memberNodeIDs[nodeID]; !member {
			message := "environment is active in another swarm cluster"
			result.Error = &message
			return result
		}
		if _, bindSwarmNodeEnvironmentErr := s.environmentService.BindSwarmNodeEnvironment(ctx, managerEnvironmentID, nodeID, target.EnvironmentID, true); bindSwarmNodeEnvironmentErr != nil {
			message := bindSwarmNodeEnvironmentErr.Error()
			result.Error = &message
			return result
		}
		result.State = swarmtypes.SwarmJoinEnvironmentResultAlreadyMember
		result.NodeID = &nodeID
		return result
	}

	joinToken := tokens.Worker
	if target.Role == swarmtypes.SwarmJoinEnvironmentRoleManager {
		joinToken = tokens.Manager
	}
	joinRequest := swarmtypes.SwarmJoinRequest{
		ListenAddr:    target.ListenAddr,
		AdvertiseAddr: target.AdvertiseAddr,
		DataPathAddr:  target.DataPathAddr,
		RemoteAddrs:   remoteAddrs,
		JoinToken:     joinToken,
		Availability:  target.Availability,
	}
	body, err := json.Marshal(joinRequest)
	if err != nil {
		message := err.Error()
		result.Error = &message
		return result
	}
	var joinResponse struct {
		Success bool `json:"success"`
	}
	if proxyJSONRequestErr := s.environmentService.ProxyJSONRequest(ctx, target.EnvironmentID, http.MethodPost, "/api/environments/0/swarm/join", body, &joinResponse); proxyJSONRequestErr != nil {
		message := describeSwarmJoinFailureInternal(proxyJSONRequestErr, joinToken)
		result.Error = &message
		return result
	}
	if !joinResponse.Success {
		message := "swarm join failed"
		result.Error = &message
		return result
	}

	for range 6 {
		s.invalidateSwarmNodeIdentityInternal(target.EnvironmentID)
		verifiedRuntime := s.resolveSwarmNodeAgentRuntimeInternal(ctx, localEnvironment)
		if verifiedRuntime.identity != nil && verifiedRuntime.identity.SwarmActive {
			nodeID := strings.TrimSpace(verifiedRuntime.identity.SwarmNodeID)
			if _, getNodeErr := s.GetNode(ctx, managerEnvironmentID, nodeID); getNodeErr == nil {
				if _, bindErr := s.environmentService.BindSwarmNodeEnvironment(ctx, managerEnvironmentID, nodeID, target.EnvironmentID, true); bindErr == nil {
					result.State = swarmtypes.SwarmJoinEnvironmentResultJoined
					result.NodeID = &nodeID
					result.Error = nil
					return result
				}
			}
		}

		select {
		case <-ctx.Done():
			message := ctx.Err().Error()
			result.Error = &message
			return result
		case <-time.After(time.Second):
		}
	}

	result.State = swarmtypes.SwarmJoinEnvironmentResultJoinedUnverified
	result.Error = nil
	return result
}

func (s *Service) fetchSwarmNodeIdentityViaEdgeInternal(ctx context.Context, environmentID string) (*SwarmNodeIdentity, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var parsed struct {
		Success bool               `json:"success"`
		Data    *SwarmNodeIdentity `json:"data"`
	}
	if err := s.environmentService.ProxyJSONRequest(reqCtx, environmentID, http.MethodGet, "/api/swarm/node-identity", nil, &parsed); err != nil {
		return nil, err
	}
	if !parsed.Success || parsed.Data == nil {
		return nil, errors.New("swarm node identity probe failed")
	}

	return parsed.Data, nil
}

func (s *Service) buildNodeAgentStatusInternal(nodeID string, env *environment.Environment, runtime swarmNodeAgentRuntime) swarmtypes.NodeAgentStatus {
	if env == nil {
		return swarmtypes.NodeAgentStatus{State: swarmtypes.NodeAgentStateNone}
	}

	// An agent is live when it holds an open tunnel or has checked in via the poll
	// control plane recently. lastHeartbeat/lastPollAt are only populated from the
	// in-memory runtime registries when activity is fresh, so their presence is
	// current evidence — unlike env.Status, which poll-mode agents never advance
	// out of "pending" (HandlePoll updates the registry, not the persisted record).
	live := runtime.connected || runtime.lastHeartbeat != nil || runtime.lastPollAt != nil

	status := swarmtypes.NodeAgentStatus{
		State:         swarmtypes.NodeAgentStateOffline,
		EnvironmentID: &env.ID,
		Connected:     &live,
		LastHeartbeat: runtime.lastHeartbeat,
		LastPollAt:    runtime.lastPollAt,

		EnvironmentName: &env.Name,
	}
	environmentType := kit.Ternary(env.IsEdge, "edge", "direct")
	status.EnvironmentType = &environmentType
	bindingKind := kit.Ternary(env.Hidden, swarmtypes.NodeAgentBindingKindDedicated, swarmtypes.NodeAgentBindingKindEnvironment)
	status.BindingKind = &bindingKind

	if runtime.identity != nil {
		if reportedNodeID := strings.TrimSpace(runtime.identity.SwarmNodeID); reportedNodeID != "" {
			status.ReportedNodeID = &reportedNodeID
		}
		if reportedHostname := strings.TrimSpace(runtime.identity.Hostname); reportedHostname != "" {
			status.ReportedHostname = &reportedHostname
		}
	}

	// A live tunnel with a completed identity probe lets us authoritatively detect
	// a node-ID mismatch (only tunnel mode can probe identity).
	if runtime.connected && runtime.identity != nil {
		if !runtime.identity.SwarmActive || strings.TrimSpace(runtime.identity.SwarmNodeID) != strings.TrimSpace(nodeID) {
			status.State = swarmtypes.NodeAgentStateMismatched
			return status
		}
		status.State = swarmtypes.NodeAgentStateConnected
		return status
	}

	// Poll-mode agents (and tunnels without a completed probe) can't be identity-
	// verified live, but a fresh check-in proves the agent mapped to this node is
	// reachable. The node-to-env mapping was established by Arcane at deploy time,
	// so report it as connected instead of masking it behind a stale env.Status.
	if live {
		status.State = swarmtypes.NodeAgentStateConnected
		return status
	}

	// No live evidence: an unpaired agent stays pending; one seen before is offline.
	if env.Status == string(environment.EnvironmentStatusPending) || env.LastSeen == nil {
		status.State = swarmtypes.NodeAgentStatePending
		return status
	}

	status.State = swarmtypes.NodeAgentStateOffline
	return status
}

func (s *Service) UpdateNode(ctx context.Context, nodeID string, req swarmtypes.NodeUpdateRequest) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	nodeResult, err := dockerClient.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	version := cmp.Or(req.Version, nodeResult.Node.Version.Index)

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

	if _, nodeUpdateErr := dockerClient.NodeUpdate(ctx, nodeID, client.NodeUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	}); nodeUpdateErr != nil {
		return fmt.Errorf("failed to update swarm node: %w", nodeUpdateErr)
	}

	return nil
}

func (s *Service) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	dockerClient, err := s.dockerClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if _, nodeRemoveErr := dockerClient.NodeRemove(ctx, nodeID, client.NodeRemoveOptions{Force: force}); nodeRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm node: %w", nodeRemoveErr)
	}

	return nil
}

func (s *Service) PromoteNode(ctx context.Context, nodeID string) error {
	return s.UpdateNode(ctx, nodeID, swarmtypes.NodeUpdateRequest{Role: new(swarm.NodeRoleManager)})
}

func (s *Service) DemoteNode(ctx context.Context, nodeID string) error {
	return s.UpdateNode(ctx, nodeID, swarmtypes.NodeUpdateRequest{Role: new(swarm.NodeRoleWorker)})
}

func (s *Service) ListNodeTasksPaginated(ctx context.Context, nodeID string, params pagination.QueryParams) ([]swarmtypes.TaskSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(client.Filters)
	filters.Add("node", nodeID)
	return s.listTasks(ctx, filters, params)
}

func (s *Service) buildNodePaginationConfigInternal() pagination.Config[swarmtypes.NodeSummary] {
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
			{Key: "created", Fn: func(a, b swarmtypes.NodeSummary) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b swarmtypes.NodeSummary) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
}

// GetJoinCandidates lists environments available to join the selected manager.
func (s *Service) GetJoinCandidates(ctx context.Context, environmentID string) ([]swarmtypes.SwarmJoinCandidate, error) {
	var identity *SwarmNodeIdentity
	var err error
	if environmentID == environment.LocalEnvironmentID {
		identity, err = s.GetLocalNodeIdentity(ctx)
	} else {
		identity, err = s.fetchSwarmNodeIdentityViaEdgeInternal(ctx, environmentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to inspect Easy Join manager: %w", err)
	}
	if !identity.SwarmActive || identity.Role == "worker" {
		return []swarmtypes.SwarmJoinCandidate{}, nil
	}
	if identity.Role != "manager" {
		return nil, errors.New("unexpected swarm node role")
	}

	nodes, _, err := s.ListNodesPaginated(ctx, environmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, err
	}
	boundEnvironmentIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.Agent.EnvironmentID != nil {
			boundEnvironmentIDs[*node.Agent.EnvironmentID] = struct{}{}
		}
	}

	environments, err := s.environmentService.ListSwarmNodeCandidateEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]swarmtypes.SwarmJoinCandidate, 0, len(environments))
	for _, candidate := range environments {
		if candidate.ID == environmentID {
			continue
		}
		if _, bound := boundEnvironmentIDs[candidate.ID]; bound {
			continue
		}
		environmentType := kit.Ternary(candidate.IsEdge, "edge", "direct")
		candidates = append(candidates, swarmtypes.SwarmJoinCandidate{
			EnvironmentID:   candidate.ID,
			EnvironmentName: candidate.Name,
			EnvironmentType: environmentType,
			Status:          candidate.Status,
		})
	}
	return candidates, nil
}

// describeSwarmJoinFailureInternal turns a proxied join failure into a concise,
// token-free message. Agent HTTP errors contribute their detail; transport
// errors keep their own text.
func describeSwarmJoinFailureInternal(err error, joinToken string) string {
	var status *remenv.StatusError
	if !errors.As(err, &status) {
		return RedactSwarmJoinToken(err.Error(), joinToken)
	}
	var body struct {
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	if len(bytes.TrimSpace(status.Body)) > 0 && json.Unmarshal(status.Body, &body) == nil {
		if detail := strings.TrimSpace(body.Detail); detail != "" {
			return RedactSwarmJoinToken(detail, joinToken)
		}
		if legacy := strings.TrimSpace(body.Error); legacy != "" {
			return RedactSwarmJoinToken(legacy, joinToken)
		}
	}
	return fmt.Sprintf("swarm join failed with HTTP %d from the target agent", status.StatusCode)
}

// redactSwarmJoinTokenInternal masks the join token wherever it appears in message.
func RedactSwarmJoinToken(message, joinToken string) string {
	if joinToken == "" {
		return message
	}
	return strings.ReplaceAll(message, joinToken, "[redacted]")
}

// selectSwarmManagerAddressesInternal prefers explicit manager addresses and
// otherwise uses the addresses advertised by the cluster's manager nodes.
func selectSwarmManagerAddressesInternal(explicit []string, nodes []swarmtypes.NodeSummary) ([]string, error) {
	addrs := kit.TrimNonEmpty(explicit)
	if len(addrs) > 0 {
		return addrs, nil
	}
	for _, node := range nodes {
		if node.ManagerAddress != "" {
			addrs = append(addrs, node.ManagerAddress)
		}
	}
	if len(addrs) == 0 {
		return nil, common.Classify(common.ErrBadRequest, errors.New("no swarm manager addresses were discovered; enter manager addresses reachable from the target hosts"))
	}
	return addrs, nil
}

func buildNodeAgentCandidatesInternal(environments []environment.Environment) []swarmtypes.NodeAgentCandidate {
	candidates := make([]swarmtypes.NodeAgentCandidate, 0, len(environments))
	for _, environment := range environments {
		environmentType := kit.Ternary(environment.IsEdge, "edge", "direct")
		candidates = append(candidates, swarmtypes.NodeAgentCandidate{
			EnvironmentID:   environment.ID,
			EnvironmentName: environment.Name,
			EnvironmentType: environmentType,
		})
	}
	return candidates
}

func preferredNodeAgentEnvironmentInternal(environments []environment.Environment) environment.Environment {
	var preferred environment.Environment
	for index, environment := range environments {
		if index == 0 {
			preferred = environment
		}
		if !environment.Hidden {
			return environment
		}
	}
	return preferred
}
