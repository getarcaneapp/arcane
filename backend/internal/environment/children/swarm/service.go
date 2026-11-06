package swarm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/samber/mo"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		apiKeys *apikey.ApiKeyService
	}
)

func New(apiKeys *apikey.ApiKeyService) *Service {
	return &Service{apiKeys: apiKeys}
}

func (s *Service) AgentIdentity(parentEnvironmentID, nodeID, hostname string) (string, string, error) {
	if strings.TrimSpace(parentEnvironmentID) == "" {
		return "", "", errors.New("parent environment ID is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return "", "", errors.New("swarm node ID is required")
	}
	return cmp.Or(strings.TrimSpace(hostname), "Swarm Node "+nodeID[:min(len(nodeID), 12)]), "agent://swarm-node-" + nodeID[:min(len(nodeID), 12)], nil
}

func (s *Service) AuthorizeBinding(
	candidate types.SwarmBindingCandidate,
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

	if candidate.ParentEnvironmentID != nil && candidate.SwarmNodeID != nil && !rebind &&
		(strings.TrimSpace(*candidate.ParentEnvironmentID) != parentEnvironmentID || strings.TrimSpace(*candidate.SwarmNodeID) != nodeID) {
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

func (s *Service) RotateApiKey(
	ctx context.Context,
	environmentID string,
	previousApiKeyID *string,
	link func(ctx context.Context, apiKeyID, apiKey string) error,
) (string, error) {
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

	if mo.PointerToOption(previousApiKeyID).OrElse(apiKeyDto.ID) != apiKeyDto.ID {
		if deleteApiKeyErr := s.apiKeys.DeleteApiKey(ctx, *previousApiKeyID); deleteApiKeyErr != nil && !errors.Is(deleteApiKeyErr, apikey.ErrApiKeyNotFound) {
			slog.ErrorContext(ctx, "Failed to delete previous environment API key; the old key remains valid until deleted manually", "environmentId", environmentID, "error", deleteApiKeyErr.Error())
		}
	}

	return apiKeyDto.Key, nil
}
