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
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"kit.getarcane.app"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/common"
	dockerInternal "getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		docker             *dockerInternal.DockerClientService
		ensureManager      func(ctx context.Context) error
		listTasks          func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error)
		localJoinTokens    func(ctx context.Context) (*types.SwarmJoinTokensResponse, error)
		environmentService *environment.EnvironmentService
		tunnels            *agent.TunnelRegistry
		identityCache      *cache.Cache[string, SwarmNodeIdentity]
		enrollments        singleflight.Group
	}

	ServiceParams struct {
		DockerService      *dockerInternal.DockerClientService
		EnsureManager      func(ctx context.Context) error
		ListTasks          func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error)
		LocalJoinTokens    func(ctx context.Context) (*types.SwarmJoinTokensResponse, error)
		EnvironmentService *environment.EnvironmentService
		Tunnels            *agent.TunnelRegistry
	}

	EnrollNodeAgentParams struct {
		EnrollmentRequestHash *string
		ParentEnvironmentID   string
		NodeID                string
	}

	SwarmNodeIdentity struct {
		SwarmNodeID   string `json:"swarmNodeId"`
		Hostname      string `json:"hostname"`
		Role          string `json:"role"`
		EngineVersion string `json:"engineVersion"`
		SwarmActive   bool   `json:"swarmActive"`
	}

	swarmNodeAgentRuntime struct {
		lastHeartbeat *time.Time
		identity      *SwarmNodeIdentity
		connected     bool
	}

	swarmNodeAgentCoverage struct {
		runtimeByEnvID     map[string]swarmNodeAgentRuntime
		boundEnvsByNodeID  map[string][]environment.Environment
		candidatesByNodeID map[string][]environment.Environment
		localIdentity      *SwarmNodeIdentity
		// Resolved once per request rather than per node: the name is user-editable, and
		// applyNodeAgentCoverage runs in a loop with no context to look it up.
		localEnvironmentName string
	}
)

const (
	swarmNodeIdentityProbeConcurrency = 5
	swarmNodeIdentityCacheTTL         = 30 * time.Second
)

func NewService(p ServiceParams) *Service {
	return &Service{
		docker:             p.DockerService,
		ensureManager:      p.EnsureManager,
		listTasks:          p.ListTasks,
		localJoinTokens:    p.LocalJoinTokens,
		environmentService: p.EnvironmentService,
		tunnels:            p.Tunnels,
		identityCache: &cache.Cache[string, SwarmNodeIdentity]{
			Capacity:        512,
			TTL:             swarmNodeIdentityCacheTTL,
			JanitorInterval: swarmNodeIdentityCacheTTL,
		},
	}
}

func (s *Service) ListNodesPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]types.SwarmNode, pagination.Response, error) {
	if environmentID != "0" {
		var remote struct {
			Data    []types.SwarmNode `json:"data"`
			Success bool              `json:"success"`
		}
		if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/agents/0/swarm/nodes?limit=-1", nil, &remote); err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to list remote swarm nodes: %w", err)
		}
		if !remote.Success {
			return nil, pagination.Response{}, errors.New("remote swarm node listing failed")
		}

		s.enrichNodeAgentStatuses(ctx, environmentID, remote.Data)
		result := s.buildNodePaginationConfig().SearchOrderAndPaginate(remote.Data, params)
		return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
	}

	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	nodesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeListResult, error) {
		return dockerClient.NodeList(ctx, client.NodeListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm nodes: %w", err)
	}
	nodes := nodesResult.Items

	items := make([]types.SwarmNode, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, types.NewSwarmNode(node))
	}

	s.enrichNodeAgentStatuses(ctx, environmentID, items)

	config := s.buildNodePaginationConfig()
	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *Service) GetNode(ctx context.Context, environmentID, nodeID string) (*types.SwarmNode, error) {
	node, err := s.inspectNode(ctx, environmentID, nodeID)
	if err != nil {
		return nil, err
	}

	items := []types.SwarmNode{*node}
	s.enrichNodeAgentStatuses(ctx, environmentID, items)
	return &items[0], nil
}

func (s *Service) inspectNode(ctx context.Context, environmentID, nodeID string) (*types.SwarmNode, error) {
	if environmentID != "0" {
		var remote struct {
			Data    types.SwarmNode `json:"data"`
			Success bool            `json:"success"`
		}
		remotePath := "/api/agents/0/swarm/nodes/" + nodeID
		if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, remotePath, nil, &remote); err != nil {
			return nil, fmt.Errorf("failed to inspect remote swarm node: %w", err)
		}
		if !remote.Success {
			return nil, errors.New("remote swarm node inspection failed")
		}
		return &remote.Data, nil
	}

	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	nodeResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeInspectResult, error) {
		return dockerClient.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm node: %w", err)
	}
	return new(types.NewSwarmNode(nodeResult.Node)), nil
}

func (s *Service) ReconcileNodeAgents(ctx context.Context, environmentID string) (*types.NodeAgentReconcileResponse, error) {
	items, _, err := s.ListNodesPaginated(ctx, environmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, err
	}

	results := make([]types.NodeAgentReconcileResult, 0, len(items))
	for _, node := range items {
		result := types.NodeAgentReconcileResult{
			NodeID:        node.ID,
			State:         node.Agent.State,
			EnvironmentID: node.Agent.EnvironmentID,
			Candidates:    node.Agent.Candidates,
		}
		if node.Agent.State == types.NodeAgentStateMismatched || len(node.Agent.Candidates) != 1 {
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

		results = append(results, types.NodeAgentReconcileResult{
			NodeID:        node.ID,
			State:         types.NodeAgentStateConnected,
			EnvironmentID: &bound.ID,
		})
	}

	return &types.NodeAgentReconcileResponse{Results: results}, nil
}

func (s *Service) BindNodeAgent(ctx context.Context, parentEnvironmentID, nodeID string, request types.NodeAgentBindingRequest) (*environment.Environment, error) {
	localEnvironment, err := s.environmentService.GetEnvironmentByID(ctx, request.EnvironmentID)
	if err != nil {
		return nil, err
	}
	if localEnvironment.Hidden {
		return nil, errors.New("only visible environments can be attached")
	}

	runtime := s.resolveSwarmNodeAgentRuntime(ctx, localEnvironment)
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

func (s *Service) EnrollNodeAgent(ctx context.Context, p EnrollNodeAgentParams) (*types.AgentEnrollResponse, error) {
	if p.NodeID == "" || len(p.NodeID) > 64 || strings.Trim(p.NodeID, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
		return nil, common.Classify(common.ErrBadRequest, errors.New("a valid swarm node ID is required"))
	}

	enrolled, err, _ := s.enrollments.Do(p.ParentEnvironmentID+"/"+p.NodeID, func() (any, error) {
		return s.enrollNodeAgent(ctx, p)
	})
	if err != nil {
		return nil, err
	}
	return enrolled.(*types.AgentEnrollResponse), nil
}

func (s *Service) enrollNodeAgent(ctx context.Context, p EnrollNodeAgentParams) (*types.AgentEnrollResponse, error) {
	node, err := s.inspectNode(ctx, p.ParentEnvironmentID, p.NodeID)
	if err != nil {
		return nil, err
	}
	if node.ID != p.NodeID {
		return nil, common.Classify(common.ErrBadRequest, errors.New("the full swarm node ID is required"))
	}

	agentEnvs, err := s.environmentService.ListSwarmNodeAgentEnvironments(ctx, p.ParentEnvironmentID)
	if err != nil {
		return nil, err
	}
	candidateEnvs, err := s.environmentService.ListSwarmNodeCandidateEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	liveCandidateEnvs := slices.DeleteFunc(slices.Clone(candidateEnvs), func(candidate environment.Environment) bool {
		return !s.tunnels.Active(candidate.ID).IsPresent()
	})
	agentEnvs = slices.DeleteFunc(agentEnvs, func(bound environment.Environment) bool {
		return strings.TrimSpace(kit.FromPtr(bound.SwarmNodeID)) != node.ID
	})
	coverage, err := s.buildNodeAgentCoverage(ctx, p.ParentEnvironmentID, agentEnvs, liveCandidateEnvs)
	if err != nil {
		return nil, err
	}
	nodeEnvironments, err := s.environmentService.CountSwarmNodeEnvironments(ctx, node.ID)
	if err != nil {
		return nil, err
	}
	s.applyNodeAgentCoverage(p.ParentEnvironmentID, node, coverage)
	switch {
	case node.Agent.State == types.NodeAgentStateConnected, node.Agent.State == types.NodeAgentStateMismatched:
		return nil, common.Classify(common.ErrConflict, errors.New("this swarm node is already covered by a connected Arcane agent"))
	case node.Agent.State == types.NodeAgentStateAmbiguous, len(node.Agent.Candidates) > 0:
		return nil, common.Classify(common.ErrConflict, errors.New("this swarm node is already covered by an unbound Arcane agent; reconcile node agents to bind it"))
	case nodeEnvironments > 0:
		return nil, common.Classify(common.ErrConflict, errors.New("this swarm node already has an Arcane environment; enroll it with that environment's enrollment token"))
	}

	env, err := s.environmentService.EnsureSwarmNodeAgentEnvironment(ctx, environment.SwarmNodeAgentEnvironmentParams{
		ParentEnvironmentID:   p.ParentEnvironmentID,
		NodeID:                node.ID,
		Hostname:              node.Hostname,
		UserID:                types.SystemUser.ID,
		Username:              types.SystemUser.Username,
		EnrollmentRequestHash: p.EnrollmentRequestHash,
	})
	if err != nil {
		return nil, err
	}
	token, err := s.environmentService.RegenerateEnvironmentApiKey(ctx, env, types.SystemUser.ID, types.SystemUser.Username)
	if err != nil {
		if deleteErr := s.environmentService.DeleteEnvironment(ctx, env.ID, new(types.SystemUser.ID), new(types.SystemUser.Username)); deleteErr != nil {
			slog.ErrorContext(ctx, "Failed to remove swarm node agent environment without an API key", "environmentId", env.ID, "error", deleteErr)
		}
		return nil, err
	}
	return &types.AgentEnrollResponse{
		EnvironmentID: env.ID,
		Token:         token,
	}, nil
}

func (s *Service) GetLocalNodeIdentity(ctx context.Context) (*SwarmNodeIdentity, error) {
	infoResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.SystemInfoResult, error) {
		return dockerClient.Info(ctx, client.InfoOptions{})
	})
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

func (s *Service) enrichNodeAgentStatuses(ctx context.Context, environmentID string, items []types.SwarmNode) {
	if len(items) == 0 || strings.TrimSpace(environmentID) == "" {
		return
	}
	coverage, err := s.resolveNodeAgentCoverage(ctx, environmentID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve swarm node agent coverage", "environmentId", environmentID, "error", err.Error())
		return
	}

	for index := range items {
		item := &items[index]
		s.applyNodeAgentCoverage(environmentID, item, coverage)
	}
}

func (s *Service) resolveNodeAgentCoverage(ctx context.Context, environmentID string) (*swarmNodeAgentCoverage, error) {
	agentEnvs, err := s.environmentService.ListSwarmNodeAgentEnvironments(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	candidateEnvs, err := s.environmentService.ListSwarmNodeCandidateEnvironments(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load swarm node agent candidates", "environmentId", environmentID, "error", err.Error())
		candidateEnvs = nil
	}
	return s.buildNodeAgentCoverage(ctx, environmentID, agentEnvs, candidateEnvs)
}

func (s *Service) buildNodeAgentCoverage(ctx context.Context, environmentID string, agentEnvs, candidateEnvs []environment.Environment) (*swarmNodeAgentCoverage, error) {
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
			defer concurrency.RecoverToError(&workerErr, "swarm worker")

			runtime := s.resolveSwarmNodeAgentRuntime(groupCtx, &env)
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

func (s *Service) applyNodeAgentCoverage(environmentID string, item *types.SwarmNode, coverage *swarmNodeAgentCoverage) {
	if item == nil || coverage == nil {
		return
	}
	nodeID := strings.TrimSpace(item.ID)
	if environmentID == environment.LocalEnvironmentID && coverage.localIdentity != nil && coverage.localIdentity.SwarmActive && strings.TrimSpace(coverage.localIdentity.SwarmNodeID) == nodeID {
		item.Agent = types.NodeAgentStatus{
			State:           types.NodeAgentStateConnected,
			BindingKind:     new(types.NodeAgentBindingKindLocal),
			EnvironmentID:   new(environment.LocalEnvironmentID),
			EnvironmentName: new(environment.DisplayName(environment.LocalEnvironmentID, coverage.localEnvironmentName)),
			EnvironmentType: new("local"),
			Connected:       new(true),
		}
		return
	}

	boundEnvs := coverage.boundEnvsByNodeID[nodeID]
	if len(boundEnvs) > 0 {
		localEnvironment := boundEnvs[max(0, slices.IndexFunc(boundEnvs, func(env environment.Environment) bool { return !env.Hidden }))]
		item.Agent = s.buildNodeAgentStatus(nodeID, &localEnvironment, coverage.runtimeByEnvID[localEnvironment.ID])
		return
	}

	candidates := coverage.candidatesByNodeID[nodeID]
	if len(candidates) > 1 {
		item.Agent = types.NodeAgentStatus{State: types.NodeAgentStateAmbiguous, Candidates: buildNodeAgentCandidates(candidates)}
		return
	}
	if len(candidates) == 1 {
		item.Agent.Candidates = buildNodeAgentCandidates(candidates)
	}
}

func (s *Service) resolveSwarmNodeAgentRuntime(ctx context.Context, env *environment.Environment) swarmNodeAgentRuntime {
	if env == nil {
		return swarmNodeAgentRuntime{}
	}

	identity, found, _ := s.identityCache.Get(env.ID)
	if !found {
		if fetched, err := s.fetchSwarmNodeIdentityViaAgent(ctx, env.ID); err != nil {
			slog.DebugContext(ctx, "failed to probe swarm node identity", "environmentId", env.ID, "error", err.Error())
		} else {
			identity, found = *fetched, true
			s.identityCache.Set(cache.Entry[string, SwarmNodeIdentity]{
				Key:   env.ID,
				Value: identity,
			})
		}
	}
	return swarmNodeAgentRuntime{
		connected:     found || s.tunnels.Active(env.ID).IsPresent(),
		lastHeartbeat: kit.FromPtr(s.tunnels.RuntimeState(env.ID).OrEmpty()).LastHeartbeat,
		identity:      kit.Ternary(found, &identity, nil),
	}
}

func (s *Service) JoinEnvironments(ctx context.Context, managerEnvironmentID string, request types.SwarmJoinEnvironmentsRequest) (*types.SwarmJoinEnvironmentsResponse, error) {
	if len(request.Targets) == 0 {
		return &types.SwarmJoinEnvironmentsResponse{Results: []types.SwarmJoinEnvironmentResult{}}, nil
	}

	nodes, _, err := s.ListNodesPaginated(ctx, managerEnvironmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, err
	}
	remoteAddrs, err := selectSwarmManagerAddresses(request.RemoteAddrs, nodes)
	if err != nil {
		return nil, err
	}
	tokens, err := s.getSwarmJoinTokensForEnvironment(ctx, managerEnvironmentID)
	if err != nil {
		return nil, err
	}
	memberNodeIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		memberNodeIDs[node.ID] = struct{}{}
	}

	results := make([]types.SwarmJoinEnvironmentResult, len(request.Targets))
	processTarget := func(index int) {
		target := request.Targets[index]
		results[index] = s.joinEnvironment(ctx, managerEnvironmentID, target, remoteAddrs, tokens, memberNodeIDs)
	}

	for index, target := range request.Targets {
		if target.Role == types.SwarmJoinEnvironmentRoleManager {
			processTarget(index)
		}
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(swarmNodeIdentityProbeConcurrency)
	for index, target := range request.Targets {
		if target.Role != types.SwarmJoinEnvironmentRoleWorker {
			continue
		}
		group.Go(func() (workerErr error) {
			defer concurrency.RecoverToError(&workerErr, "swarm worker")

			if groupCtx.Err() == nil {
				processTarget(index)
			}
			return nil
		})
	}
	_ = group.Wait()

	return &types.SwarmJoinEnvironmentsResponse{Results: results}, nil
}

func (s *Service) getSwarmJoinTokensForEnvironment(ctx context.Context, environmentID string) (*types.SwarmJoinTokensResponse, error) {
	if environmentID == "0" {
		return s.localJoinTokens(ctx)
	}

	var response struct {
		Data    types.SwarmJoinTokensResponse `json:"data"`
		Success bool                          `json:"success"`
	}
	if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, "/api/agents/0/swarm/join/tokens", nil, &response); err != nil {
		return nil, fmt.Errorf("failed to get remote swarm join tokens: %w", err)
	}
	if !response.Success {
		return nil, errors.New("remote swarm join-token request failed")
	}
	return &response.Data, nil
}

func (s *Service) joinEnvironment(
	ctx context.Context,
	managerEnvironmentID string,
	target types.SwarmJoinEnvironmentTarget,
	remoteAddrs []string,
	tokens *types.SwarmJoinTokensResponse,
	memberNodeIDs map[string]struct{},
) types.SwarmJoinEnvironmentResult {
	result := func(state types.SwarmJoinEnvironmentResultState, nodeID, message string) types.SwarmJoinEnvironmentResult {
		return types.SwarmJoinEnvironmentResult{
			EnvironmentID: target.EnvironmentID,
			State:         state,
			NodeID:        kit.Ternary(nodeID == "", nil, &nodeID),
			Error:         kit.Ternary(message == "", nil, &message),
		}
	}
	failed := func(message string) types.SwarmJoinEnvironmentResult {
		return result(types.SwarmJoinEnvironmentResultFailed, "", message)
	}

	localEnvironment, err := s.environmentService.GetEnvironmentByID(ctx, target.EnvironmentID)
	if err != nil {
		return failed(err.Error())
	}
	if localEnvironment.Hidden || !localEnvironment.Enabled {
		return failed("target environment is unavailable")
	}

	runtime := s.resolveSwarmNodeAgentRuntime(ctx, localEnvironment)
	if runtime.identity != nil && runtime.identity.SwarmActive {
		nodeID := strings.TrimSpace(runtime.identity.SwarmNodeID)
		if _, member := memberNodeIDs[nodeID]; !member {
			return failed("environment is active in another swarm cluster")
		}
		if _, bindErr := s.environmentService.BindSwarmNodeEnvironment(ctx, managerEnvironmentID, nodeID, target.EnvironmentID, true); bindErr != nil {
			return failed(bindErr.Error())
		}
		return result(types.SwarmJoinEnvironmentResultAlreadyMember, nodeID, "")
	}

	joinToken := kit.Ternary(target.Role == types.SwarmJoinEnvironmentRoleManager, tokens.Manager, tokens.Worker)
	body, err := json.Marshal(types.SwarmJoinRequest{
		ListenAddr:    target.ListenAddr,
		AdvertiseAddr: target.AdvertiseAddr,
		DataPathAddr:  target.DataPathAddr,
		RemoteAddrs:   remoteAddrs,
		JoinToken:     joinToken,
		Availability:  target.Availability,
	})
	if err != nil {
		return failed(err.Error())
	}
	var joinResponse struct {
		Success bool `json:"success"`
	}
	if proxyErr := s.environmentService.ProxyJSONRequest(ctx, target.EnvironmentID, http.MethodPost, "/api/agents/0/swarm/join", body, &joinResponse); proxyErr != nil {
		return failed(describeSwarmJoinFailure(proxyErr, joinToken))
	}
	if !joinResponse.Success {
		return failed("swarm join failed")
	}

	for range 6 {
		s.identityCache.Delete(target.EnvironmentID)
		verifiedRuntime := s.resolveSwarmNodeAgentRuntime(ctx, localEnvironment)
		if verifiedRuntime.identity != nil && verifiedRuntime.identity.SwarmActive {
			nodeID := strings.TrimSpace(verifiedRuntime.identity.SwarmNodeID)
			if _, getNodeErr := s.GetNode(ctx, managerEnvironmentID, nodeID); getNodeErr == nil {
				if _, bindErr := s.environmentService.BindSwarmNodeEnvironment(ctx, managerEnvironmentID, nodeID, target.EnvironmentID, true); bindErr == nil {
					return result(types.SwarmJoinEnvironmentResultJoined, nodeID, "")
				}
			}
		}

		select {
		case <-ctx.Done():
			return failed(ctx.Err().Error())
		case <-time.After(time.Second):
		}
	}

	return result(types.SwarmJoinEnvironmentResultJoinedUnverified, "", "")
}

func (s *Service) fetchSwarmNodeIdentityViaAgent(ctx context.Context, environmentID string) (*SwarmNodeIdentity, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var parsed struct {
		Data    *SwarmNodeIdentity `json:"data"`
		Success bool               `json:"success"`
	}
	if err := s.environmentService.ProxyJSONRequest(reqCtx, environmentID, http.MethodGet, "/api/swarm/identity", nil, &parsed); err != nil {
		return nil, err
	}
	if !parsed.Success || parsed.Data == nil {
		return nil, errors.New("swarm node identity probe failed")
	}

	return parsed.Data, nil
}

func (s *Service) buildNodeAgentStatus(nodeID string, env *environment.Environment, runtime swarmNodeAgentRuntime) types.NodeAgentStatus {
	if env == nil {
		return types.NodeAgentStatus{State: types.NodeAgentStateNone}
	}

	// Live means an open tunnel; the runtime registry only holds fresh activity, unlike env.Status.
	live := runtime.connected || runtime.lastHeartbeat != nil
	identity := kit.FromPtr(runtime.identity)
	reportedNodeID := strings.TrimSpace(identity.SwarmNodeID)
	reportedHostname := strings.TrimSpace(identity.Hostname)
	state := func() types.NodeAgentState {
		switch {
		// Only a live tunnel with a completed identity probe can authoritatively detect a node-ID mismatch.
		case runtime.connected && runtime.identity != nil:
			return kit.Ternary(identity.SwarmActive && reportedNodeID == strings.TrimSpace(nodeID), types.NodeAgentStateConnected, types.NodeAgentStateMismatched)
		// A fresh check-in proves the agent Arcane mapped to this node at deploy time is reachable.
		case live:
			return types.NodeAgentStateConnected
		// No live evidence: an unpaired agent stays pending; one seen before is offline.
		case env.Status == string(environment.EnvironmentStatusPending) || env.LastSeen == nil:
			return types.NodeAgentStatePending
		default:
			return types.NodeAgentStateOffline
		}
	}()

	return types.NodeAgentStatus{
		State:            state,
		BindingKind:      new(kit.Ternary(env.Hidden, types.NodeAgentBindingKindDedicated, types.NodeAgentBindingKindEnvironment)),
		EnvironmentID:    &env.ID,
		EnvironmentName:  &env.Name,
		EnvironmentType:  new("agent"),
		Connected:        &live,
		LastHeartbeat:    runtime.lastHeartbeat,
		ReportedNodeID:   kit.Ternary(reportedNodeID == "", nil, &reportedNodeID),
		ReportedHostname: kit.Ternary(reportedHostname == "", nil, &reportedHostname),
	}
}

func (s *Service) UpdateNode(ctx context.Context, nodeID string, update types.SwarmNode, submitted map[string]bool) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	nodeResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeInspectResult, error) {
		return dockerClient.NodeInspect(ctx, nodeID, client.NodeInspectOptions{})
	})
	if err != nil {
		return fmt.Errorf("failed to inspect swarm node: %w", err)
	}

	current := nodeResult.Node.Spec
	if _, nodeUpdateErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeUpdateResult, error) {
		return dockerClient.NodeUpdate(ctx, nodeID, client.NodeUpdateOptions{
			Version: swarm.Version{Index: cmp.Or(update.Version, nodeResult.Node.Version.Index)},
			Spec: swarm.NodeSpec{
				Name:         kit.Ternary(submitted["name"], update.Name, current.Name),
				Labels:       kit.Ternary(submitted["labels"], update.Labels, current.Labels),
				Role:         kit.Ternary(submitted["role"], swarm.NodeRole(update.Role), current.Role),
				Availability: kit.Ternary(submitted["availability"], swarm.NodeAvailability(update.Availability), current.Availability),
			},
		})
	}); nodeUpdateErr != nil {
		return fmt.Errorf("failed to update swarm node: %w", nodeUpdateErr)
	}

	return nil
}

func (s *Service) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	if _, nodeRemoveErr := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.NodeRemoveResult, error) {
		return dockerClient.NodeRemove(ctx, nodeID, client.NodeRemoveOptions{Force: force})
	}); nodeRemoveErr != nil {
		return fmt.Errorf("failed to remove swarm node: %w", nodeRemoveErr)
	}

	return nil
}

func (s *Service) ListNodeTasksPaginated(ctx context.Context, nodeID string, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	filters := make(client.Filters)
	filters.Add("node", nodeID)
	return s.listTasks(ctx, filters, params)
}

func (s *Service) Aggregate(ctx context.Context, params pagination.QueryParams) (types.Paginated[types.SwarmNode], error) {
	collection, err := httpx.CollectEnvironments[types.Paginated[types.SwarmNode]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.Paginated[types.SwarmNode]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.SwarmNode], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.SwarmNode]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(s.buildNodePaginationConfig(), pages, params)

	return types.Paginated[types.SwarmNode]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func (s *Service) buildNodePaginationConfig() pagination.Config[types.SwarmNode] {
	return pagination.Config[types.SwarmNode]{
		SearchAccessors: []pagination.SearchAccessor[types.SwarmNode]{
			func(node types.SwarmNode) (string, error) { return node.Hostname, nil },
			func(node types.SwarmNode) (string, error) { return node.ID, nil },
			func(node types.SwarmNode) (string, error) { return node.Role, nil },
			func(node types.SwarmNode) (string, error) { return node.Status, nil },
			func(node types.SwarmNode) (string, error) { return node.Availability, nil },
		},
		SortBindings: []pagination.SortBinding[types.SwarmNode]{
			{Key: "hostname", Fn: func(a, b types.SwarmNode) int { return strings.Compare(a.Hostname, b.Hostname) }},
			{Key: "role", Fn: func(a, b types.SwarmNode) int { return strings.Compare(a.Role, b.Role) }},
			{Key: "status", Fn: func(a, b types.SwarmNode) int { return strings.Compare(a.Status, b.Status) }},
			{Key: "availability", Fn: func(a, b types.SwarmNode) int { return strings.Compare(a.Availability, b.Availability) }},
			{Key: "created", Fn: func(a, b types.SwarmNode) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b types.SwarmNode) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
}

func (s *Service) GetJoinCandidates(ctx context.Context, environmentID string) ([]types.SwarmJoinCandidate, error) {
	var identity *SwarmNodeIdentity
	var err error
	if environmentID == environment.LocalEnvironmentID {
		identity, err = s.GetLocalNodeIdentity(ctx)
	} else {
		identity, err = s.fetchSwarmNodeIdentityViaAgent(ctx, environmentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to inspect Easy Join manager: %w", err)
	}
	if !identity.SwarmActive || identity.Role == "worker" {
		return []types.SwarmJoinCandidate{}, nil
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
	candidates := make([]types.SwarmJoinCandidate, 0, len(environments))
	for _, candidate := range environments {
		if candidate.ID == environmentID {
			continue
		}
		if _, bound := boundEnvironmentIDs[candidate.ID]; bound {
			continue
		}
		candidates = append(candidates, types.SwarmJoinCandidate{
			EnvironmentID:   candidate.ID,
			EnvironmentName: candidate.Name,
			EnvironmentType: "agent",
			Status:          candidate.Status,
		})
	}
	return candidates, nil
}

func describeSwarmJoinFailure(err error, joinToken string) string {
	var status *agent.StatusError
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

func RedactSwarmJoinToken(message, joinToken string) string {
	if joinToken == "" {
		return message
	}
	return strings.ReplaceAll(message, joinToken, "[redacted]")
}

func selectSwarmManagerAddresses(explicit []string, nodes []types.SwarmNode) ([]string, error) {
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

func buildNodeAgentCandidates(environments []environment.Environment) []types.NodeAgentCandidate {
	candidates := make([]types.NodeAgentCandidate, 0, len(environments))
	for _, environment := range environments {
		candidates = append(candidates, types.NodeAgentCandidate{
			EnvironmentID:   environment.ID,
			EnvironmentName: environment.Name,
			EnvironmentType: "agent",
		})
	}
	return candidates
}
