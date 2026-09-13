package swarm

import (
	"context"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
)

// GetJoinCandidates lists environments available to join the selected manager.
func (s *SwarmService) GetJoinCandidates(ctx context.Context, environmentID string) ([]swarmtypes.SwarmJoinCandidate, error) {
	var identity *SwarmNodeIdentity
	var err error
	if environmentID == environment.LocalEnvironmentID {
		identity, err = s.GetLocalNodeIdentity(ctx)
	} else {
		identity, err = s.fetchSwarmNodeIdentityViaEdgeInternal(ctx, environmentID)
	}
	if err != nil {
		return nil, errors.WrapIf(err, "failed to inspect Easy Join manager")
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
		environmentType := "direct"
		if candidate.IsEdge {
			environmentType = "edge"
		}
		candidates = append(candidates, swarmtypes.SwarmJoinCandidate{
			EnvironmentID:   candidate.ID,
			EnvironmentName: candidate.Name,
			EnvironmentType: environmentType,
			Status:          candidate.Status,
		})
	}
	return candidates, nil
}
