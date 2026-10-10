// Package swarm owns swarm-node agent provisioning, node binding policy, and
// agent API-key rotation; the parent persists environment rows.
package swarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/environment"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
)

type Service struct {
	apiKeys *apikey.ApiKeyService
}

func New(apiKeys *apikey.ApiKeyService) *Service {
	return &Service{apiKeys: apiKeys}
}

// AgentIdentity validates a node agent request and returns the name and API URL
// for a newly provisioned agent environment.
func (s *Service) AgentIdentity(parentEnvironmentID, nodeID, hostname string) (string, string, error) {
	if strings.TrimSpace(parentEnvironmentID) == "" {
		return "", "", errors.New("parent environment ID is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return "", "", errors.New("swarm node ID is required")
	}
	return buildAgentNameInternal(hostname, nodeID), buildAgentURLInternal(nodeID), nil
}

// AuthorizeBinding rejects bindings that would silently move an environment or
// replace a node's visible binding unless rebind is set. countVisibleBindings
// returns the other visible environments already bound to the node.
func (s *Service) AuthorizeBinding(
	candidate environment.SwarmBindingCandidate,
	parentEnvironmentID, nodeID string,
	rebind bool,
	countVisibleBindings func() (int64, error),
) error {
	if candidate.Hidden {
		return errors.New("dedicated agent environments cannot be attached as visible environments")
	}
	if !candidate.Enabled {
		return errors.New("disabled environments cannot be attached to swarm nodes")
	}

	boundElsewhere := candidate.ParentEnvironmentID != nil && candidate.SwarmNodeID != nil &&
		(strings.TrimSpace(*candidate.ParentEnvironmentID) != parentEnvironmentID || strings.TrimSpace(*candidate.SwarmNodeID) != nodeID)
	if boundElsewhere && !rebind {
		return errors.New("environment is already bound to another swarm node; explicit rebinding is required")
	}

	existingVisibleBindings, err := countVisibleBindings()
	if err != nil {
		return fmt.Errorf("failed to inspect existing swarm node binding: %w", err)
	}
	if existingVisibleBindings > 0 && !rebind {
		return errors.New("swarm node already has a visible environment binding; explicit rebinding is required")
	}
	return nil
}

// EnsureApiKey returns the agent's existing token unless rotate is set or none
// exists; otherwise it creates a key, links it via link, and deletes the previous key.
func (s *Service) EnsureApiKey(
	ctx context.Context,
	environmentID string,
	accessToken, previousApiKeyID *string,
	rotate bool,
	link func(ctx context.Context, apiKeyID, apiKey string) error,
) (string, error) {
	if !rotate && accessToken != nil && strings.TrimSpace(*accessToken) != "" {
		return strings.TrimSpace(*accessToken), nil
	}

	if s.apiKeys == nil {
		return "", errors.New("api key service not configured")
	}

	apiKeyDto, err := s.apiKeys.CreateEnvironmentApiKey(ctx, environmentID)
	if err != nil {
		return "", fmt.Errorf("failed to create environment API key: %w", err)
	}

	if linkErr := link(ctx, apiKeyDto.ID, apiKeyDto.Key); linkErr != nil {
		// The new key was never linked; remove it so a failed rotation does
		// not leave an orphaned valid credential behind.
		if delErr := s.apiKeys.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil && !errors.Is(delErr, apikey.ErrApiKeyNotFound) {
			slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentId", environmentID, "error", delErr.Error())
		}
		return "", linkErr
	}

	// Delete the previous key only after the environment points at the new
	// one — while still referenced it is protected and the delete would be
	// rejected. Failure is non-fatal for the rotation itself, but the old key
	// remains a valid credential until deleted, so log it as an error; the key
	// stays visible and deletable on the API Keys page.
	if previousApiKeyID != nil && *previousApiKeyID != apiKeyDto.ID {
		if deleteApiKeyErr := s.apiKeys.DeleteApiKey(ctx, *previousApiKeyID); deleteApiKeyErr != nil && !errors.Is(deleteApiKeyErr, apikey.ErrApiKeyNotFound) {
			slog.ErrorContext(ctx, "Failed to delete previous environment API key; the old key remains valid until deleted manually", "environmentId", environmentID, "error", deleteApiKeyErr.Error())
		}
	}

	return apiKeyDto.Key, nil
}
