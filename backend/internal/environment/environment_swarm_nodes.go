package environment

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"gorm.io/gorm"
)

func (s *EnvironmentService) ListSwarmNodeAgentEnvironments(ctx context.Context, parentEnvironmentID string) ([]models.Environment, error) {
	var envs []models.Environment
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Find(&envs).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list swarm node agent environments")
	}

	return envs, nil
}

// ListSwarmNodeCandidateEnvironments returns enabled visible environments that
// can provide swarm-node coverage for a manager environment.
func (s *EnvironmentService) ListSwarmNodeCandidateEnvironments(ctx context.Context) ([]models.Environment, error) {
	var envs []models.Environment
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("hidden = ?", false).
		Where("enabled = ?", true).
		Where("id <> ?", "0").
		Order("name ASC").
		Find(&envs).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list swarm node candidate environments")
	}

	return envs, nil
}

// BindSwarmNodeEnvironment binds an existing visible environment to a swarm
// node without modifying its connection details or agent token.
func (s *EnvironmentService) BindSwarmNodeEnvironment(
	ctx context.Context,
	parentEnvironmentID, nodeID, environmentID string,
	rebind bool,
) (*models.Environment, error) {
	var envRecord models.Environment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", environmentID).First(&envRecord).Error; err != nil {
			return errors.WrapIf(err, "failed to load environment for swarm node binding")
		}
		if envRecord.Hidden {
			return errors.New("dedicated agent environments cannot be attached as visible environments")
		}
		if !envRecord.Enabled {
			return errors.New("disabled environments cannot be attached to swarm nodes")
		}

		boundElsewhere := envRecord.ParentEnvironmentID != nil && envRecord.SwarmNodeID != nil &&
			(strings.TrimSpace(*envRecord.ParentEnvironmentID) != parentEnvironmentID || strings.TrimSpace(*envRecord.SwarmNodeID) != nodeID)
		if boundElsewhere && !rebind {
			return errors.New("environment is already bound to another swarm node; explicit rebinding is required")
		}

		var existingVisibleBindings int64
		if err := tx.Model(&models.Environment{}).
			Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ? AND id <> ?", false, parentEnvironmentID, nodeID, environmentID).
			Count(&existingVisibleBindings).Error; err != nil {
			return errors.WrapIf(err, "failed to inspect existing swarm node binding")
		}
		if existingVisibleBindings > 0 && !rebind {
			return errors.New("swarm node already has a visible environment binding; explicit rebinding is required")
		}
		if rebind {
			if err := tx.Model(&models.Environment{}).
				Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ? AND id <> ?", false, parentEnvironmentID, nodeID, environmentID).
				Updates(map[string]any{"parent_environment_id": nil, "swarm_node_id": nil, "updated_at": new(time.Now())}).Error; err != nil {
				return errors.WrapIf(err, "failed to clear previous swarm node binding")
			}
		}

		if err := tx.Model(&models.Environment{}).Where("id = ?", environmentID).Updates(map[string]any{
			"parent_environment_id": parentEnvironmentID,
			"swarm_node_id":         nodeID,
			"updated_at":            new(time.Now()),
		}).Error; err != nil {
			return errors.WrapIf(err, "failed to bind environment to swarm node")
		}

		return tx.Where("id = ?", environmentID).First(&envRecord).Error
	})
	if err != nil {
		return nil, err
	}

	s.remoteEnvs.put(envRecord)
	return &envRecord, nil
}

// DetachSwarmNodeEnvironment clears a visible environment binding from a node.
func (s *EnvironmentService) DetachSwarmNodeEnvironment(ctx context.Context, parentEnvironmentID, nodeID string) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).
		Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ?", false, parentEnvironmentID, nodeID).
		Updates(map[string]any{"parent_environment_id": nil, "swarm_node_id": nil, "updated_at": &now}).Error; err != nil {
		return errors.WrapIf(err, "failed to detach swarm node environment")
	}

	return nil
}

// DeleteSwarmNodeAgentDeployment removes a dedicated hidden agent registration
// while leaving visible remote environments untouched.
func (s *EnvironmentService) DeleteSwarmNodeAgentDeployment(ctx context.Context, parentEnvironmentID, nodeID string, userID, username *string) error {
	var envRecord models.Environment
	if err := s.db.WithContext(ctx).
		Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ?", true, parentEnvironmentID, nodeID).
		First(&envRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return errors.WrapIf(err, "failed to load swarm node agent deployment")
	}

	return s.DeleteEnvironment(ctx, envRecord.ID, userID, username)
}

func buildSwarmNodeAgentNameInternal(hostname, nodeID string) string {
	trimmedHostname := strings.TrimSpace(hostname)
	if trimmedHostname != "" {
		return trimmedHostname
	}
	if len(nodeID) > 12 {
		nodeID = nodeID[:12]
	}
	return "Swarm Node " + nodeID
}

func buildSwarmNodeAgentURLInternal(nodeID string) string {
	shortNodeID := nodeID
	if len(shortNodeID) > 12 {
		shortNodeID = shortNodeID[:12]
	}
	return "edge://swarm-node-" + shortNodeID
}

func (s *EnvironmentService) applySwarmNodeAgentApiKeyInternal(
	ctx context.Context,
	env *models.Environment,
	userID, username string,
	rotate bool,
) (string, error) {
	if env == nil {
		return "", errors.New("environment is required")
	}

	if !rotate && env.AccessToken != nil && strings.TrimSpace(*env.AccessToken) != "" {
		return strings.TrimSpace(*env.AccessToken), nil
	}

	if s.apiKeyService == nil {
		return "", errors.New("api key service not configured")
	}

	oldApiKeyID := env.ApiKeyID

	apiKeyDto, err := s.apiKeyService.CreateEnvironmentApiKey(ctx, env.ID)
	if err != nil {
		return "", errors.WrapIf(err, "failed to create environment API key")
	}

	if err := s.RegenerateEnvironmentApiKey(ctx, env.ID, apiKeyDto.ID, apiKeyDto.Key, userID, username, env.Name); err != nil {
		// The new key was never linked; remove it so a failed rotation does
		// not leave an orphaned valid credential behind.
		if delErr := s.apiKeyService.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil && !errors.Is(delErr, apikey.ErrApiKeyNotFound) {
			slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentID", env.ID, "error", delErr.Error())
		}
		return "", err
	}

	// Delete the previous key only after the environment points at the new
	// one — while still referenced it is protected and the delete would be
	// rejected. Failure is non-fatal for the rotation itself, but the old key
	// remains a valid credential until deleted, so log it as an error; the key
	// stays visible and deletable on the API Keys page.
	if oldApiKeyID != nil && *oldApiKeyID != apiKeyDto.ID {
		if err := s.apiKeyService.DeleteApiKey(ctx, *oldApiKeyID); err != nil && !errors.Is(err, apikey.ErrApiKeyNotFound) {
			slog.ErrorContext(ctx, "Failed to delete previous environment API key; the old key remains valid until deleted manually", "environmentID", env.ID, "error", err.Error())
		}
	}

	return apiKeyDto.Key, nil
}

func (s *EnvironmentService) EnsureSwarmNodeAgentEnvironment(
	ctx context.Context,
	parentEnvironmentID, nodeID, hostname, userID, username string,
	rotate bool,
) (*models.Environment, string, error) {
	if strings.TrimSpace(parentEnvironmentID) == "" {
		return nil, "", errors.New("parent environment ID is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return nil, "", errors.New("swarm node ID is required")
	}

	var env models.Environment
	// Prefer an existing visible binding. Legacy hidden registrations remain
	// reusable, but all newly provisioned node agents are normal Remote
	// Environments so one token and one agent can serve both use cases.
	err := s.db.WithContext(ctx).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Where("swarm_node_id = ?", nodeID).
		Order("hidden ASC").
		First(&env).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", errors.WrapIf(err, "failed to load swarm node agent environment")
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		createdEnv := &models.Environment{
			Name:                buildSwarmNodeAgentNameInternal(hostname, nodeID),
			ApiUrl:              buildSwarmNodeAgentURLInternal(nodeID),
			Status:              string(models.EnvironmentStatusPending),
			Enabled:             true,
			IsEdge:              true,
			Hidden:              false,
			ParentEnvironmentID: new(parentEnvironmentID),
			SwarmNodeID:         new(nodeID),
		}

		if _, createErr := s.CreateEnvironment(ctx, createdEnv, new(userID), new(username)); createErr != nil {
			return nil, "", errors.WrapIf(createErr, "failed to create swarm node agent environment")
		}
		env = *createdEnv
	}

	apiKey, err := s.applySwarmNodeAgentApiKeyInternal(ctx, &env, userID, username, rotate)
	if err != nil {
		return nil, "", err
	}

	refreshedEnv, err := s.GetEnvironmentByID(ctx, env.ID)
	if err != nil {
		return nil, "", errors.WrapIf(err, "failed to refresh swarm node agent environment")
	}

	return refreshedEnv, apiKey, nil
}

func (s *EnvironmentService) UpdateSwarmNodeIdentity(ctx context.Context, envID, swarmNodeID string) error {
	updates := map[string]any{
		"swarm_node_id": swarmNodeID,
		"updated_at":    new(time.Now()),
	}

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", envID).Updates(updates).Error; err != nil {
		return errors.WrapIf(err, "failed to update swarm node identity")
	}

	return nil
}
