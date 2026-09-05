package environment

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"go.getarcane.app/kit/normalization"

	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"uuid"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

type EnvironmentService struct {
	db              *database.DB
	httpClient      *http.Client
	dockerService   *docker.DockerClientService
	eventService    *event.EventService
	settingsService *settings.SettingsService
	apiKeyService   *apikey.ApiKeyService
	remoteClient    *remenv.Client
	edgeTokens      *edgeTokenCacheInternal
	remoteEnvs      *remoteEnvSnapshotCacheInternal

	// jobs carries the scheduler and app lifecycle context, injected
	// post-construction via SetScheduler (manager-only). Each enabled environment
	// gets its own health-check job; this replaces the single global
	// environment-health job.
	jobs *entityjobs.Registry

	// variableSyncer is injected post-construction via SetVariableSyncer
	// (manager-only) to avoid a wire cycle with variable.VariableService.
	variableSyncer VariableSyncer

	// runtimeWatchers receive a coalesced wake-up whenever an environment's
	// liveness changes. See environment_runtime_notify.go.
	runtimeWatchers runtimeWatchersInternal
}

// VariableSyncer pushes the effective global-variable set to one environment.
// Implemented by variable.VariableService.
type VariableSyncer interface {
	SyncEnvironment(ctx context.Context, envID string) error
}

const (
	ErrEnvironmentAccessTokenRequired = errors.Sentinel("environment access token required")
	ErrInvalidEnvironmentAccessToken  = errors.Sentinel("invalid environment access token")
)

func NewEnvironmentService(db *database.DB, httpClient *http.Client, dockerService *docker.DockerClientService, eventService *event.EventService, settingsService *settings.SettingsService, apiKeyService *apikey.ApiKeyService) *EnvironmentService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EnvironmentService{
		db:              db,
		httpClient:      httpClient,
		dockerService:   dockerService,
		eventService:    eventService,
		settingsService: settingsService,
		apiKeyService:   apiKeyService,
		remoteClient: remenv.NewClient(httpClient, remenv.TunnelTransportFuncs{
			EnsureAvailableFunc: ensureRemoteEnvironmentTunnelAvailableInternal,
			DoFunc:              doRemoteEnvironmentTunnelRequestInternal,
		}),
		edgeTokens: newEdgeTokenCacheInternal(),
		remoteEnvs: newRemoteEnvSnapshotCacheInternal(),
		jobs:       entityjobs.New(environmentHealthJobPrefix, environmentHealthAdmissionScopeInternal),
	}
}

// SetVariableSyncer injects the global-variable syncer. Called during
// bootstrap on the manager only; agents leave it nil.
func (s *EnvironmentService) SetVariableSyncer(syncer VariableSyncer) {
	s.variableSyncer = syncer
}

func (s *EnvironmentService) ResolveEdgeEnvironmentByToken(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("agent token required")
	}

	if envID, ok := s.edgeTokens.environmentID(token).Get(); ok {
		return envID, nil
	}

	var env Environment
	if err := s.db.WithContext(ctx).
		Select("id", "access_token").
		Where("is_edge = ?", true).
		Where("access_token = ?", token).
		First(&env).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.logEdgeTokenResolveMissInternal(ctx, token)
			return "", errors.New("invalid agent token")
		}
		return "", errors.WrapIf(err, "failed to resolve edge environment by token")
	}

	s.edgeTokens.put(env.ID, token)
	return env.ID, nil
}

// logEdgeTokenResolveMissInternal emits a debug log diagnosing why an agent
// token failed to resolve to an edge environment. Counts existing edge
// environments (by access_token presence) so operators can distinguish
// "no edge envs configured" from "token does not match any configured env".
// Token contents are never logged in full — only length and a short
// fingerprint that cannot be reversed.
func (s *EnvironmentService) logEdgeTokenResolveMissInternal(ctx context.Context, token string) {
	if s == nil || s.db == nil {
		return
	}
	if !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return
	}

	var totalEdgeEnvs int64
	var edgeEnvsWithToken int64
	totalEdgeEnvsErr := s.db.WithContext(ctx).Model(&Environment{}).Where("is_edge = ?", true).Count(&totalEdgeEnvs).Error
	edgeEnvsWithTokenErr := s.db.WithContext(ctx).Model(&Environment{}).
		Where("is_edge = ?", true).
		Where("access_token IS NOT NULL AND access_token != ?", "").
		Count(&edgeEnvsWithToken).Error

	args := []any{
		"token_length", len(token),
		"token_fingerprint", remenv.RedactedTokenFingerprint(token),
	}
	if totalEdgeEnvsErr == nil {
		args = append(args, "edge_envs_total", totalEdgeEnvs)
	}
	if edgeEnvsWithTokenErr == nil {
		args = append(args, "edge_envs_with_access_token", edgeEnvsWithToken)
	}

	slog.DebugContext(ctx, "Edge agent token did not match any environment", args...)
}

// ResolveEnvironmentName looks up an environment and returns the label to show for it.
// Use this instead of hardcoding a name for a known ID: names are user-editable, so
// even the local environment's is not fixed.
func (s *EnvironmentService) ResolveEnvironmentName(ctx context.Context, environmentID string) string {
	if strings.TrimSpace(environmentID) == "" {
		environmentID = LocalEnvironmentID
	}
	env, err := s.GetEnvironmentByID(ctx, environmentID)
	if err != nil || env == nil {
		slog.WarnContext(ctx, "failed to resolve environment name", "environmentID", environmentID, "error", err)
		return DisplayName(environmentID, "")
	}
	return DisplayName(env.ID, env.Name)
}

func (s *EnvironmentService) EnsureLocalEnvironment(ctx context.Context, appUrl string) error {
	var existingEnv Environment
	err := s.db.WithContext(ctx).Where("id = ?", LocalEnvironmentID).First(&existingEnv).Error

	if err == nil {
		// Local environment already exists, ensure ApiUrl matches current appUrl
		if existingEnv.ApiUrl != appUrl {
			if err := s.db.WithContext(ctx).Model(&existingEnv).Update("api_url", appUrl).Error; err != nil {
				return errors.WrapIf(err, "failed to update local environment api url")
			}
			slog.InfoContext(ctx, "updated local environment api url", "id", LocalEnvironmentID, "url", appUrl)
		}
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.WrapIf(err, "failed to check for local environment")
	}

	// Create the local environment
	now := time.Now()
	localEnv := &Environment{
		ID:        LocalEnvironmentID,
		CreatedAt: now,
		UpdatedAt: new(now),
		Name:      "Local Docker",
		ApiUrl:    appUrl,
		Status:    string(EnvironmentStatusOnline),
		Enabled:   true,
	}

	if err := s.db.WithContext(ctx).Create(localEnv).Error; err != nil {
		return errors.WrapIf(err, "failed to create local environment")
	}

	slog.InfoContext(ctx, "created local environment record", "id", LocalEnvironmentID)
	return nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, environment *Environment, userID, username *string) (*Environment, error) {
	if err := normalization.Normalize(environment); err != nil {
		return nil, err
	}
	environment.ID = uuid.New().String()

	// Only set status to offline if not already set (e.g., API key flow sets it to pending)
	if environment.Status == "" {
		environment.Status = string(EnvironmentStatusOffline)
	}

	now := time.Now()
	environment.CreatedAt = now
	environment.UpdatedAt = new(now)

	if err := s.db.WithContext(ctx).Create(environment).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create environment")
	}

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), environment.ID, environment.Name, event.EventTypeEnvironmentCreate, "Environment Created", fmt.Sprintf("Environment '%s' was created", environment.Name), event.EventSeveritySuccess, userID, username)

	if environment.Enabled {
		s.registerHealthJobInternal(ctx, environment.ID)
	}
	s.remoteEnvs.put(*environment)

	return environment, nil
}

func (s *EnvironmentService) GetEnvironmentByID(ctx context.Context, id string) (*Environment, error) {
	var envRecord Environment
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&envRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("environment not found")
		}
		return nil, errors.WrapIf(err, "failed to get environment")
	}
	return &envRecord, nil
}

func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, id string, updates map[string]any, userID, username *string) (*Environment, error) {
	if name, ok := updates["name"].(string); ok {
		updates["name"] = normalization.Text(name, true, true)
	}
	current, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var nextAPIURL *string
	if rawAPIURL, ok := updates["api_url"]; ok {
		if apiURL, isString := rawAPIURL.(string); isString {
			nextAPIURL = new(apiURL)
		}
	}
	_, accessTokenUpdated := updates["access_token"]
	if err := validation.ValidateCredentialTargetChange(
		"environment API URL",
		current.ApiUrl,
		nextAPIURL,
		func(value string) string {
			normalized, normalizeErr := httputils.NormalizeBaseURL(value)
			if normalizeErr != nil {
				return strings.TrimSpace(value)
			}
			return normalized
		},
		map[string]bool{"accessToken": current.AccessToken != nil && *current.AccessToken != ""},
		map[string]bool{"accessToken": accessTokenUpdated},
	); err != nil {
		return nil, err
	}

	updates["updated_at"] = new(time.Now())

	if err := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to update environment")
	}

	updated, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.remoteEnvs.put(*updated)

	if rawAccessToken, ok := updates["access_token"]; ok {
		accessToken, _ := rawAccessToken.(string)
		s.edgeTokens.sync(id, accessToken)
	}

	// Reconcile the per-environment health job when the enabled flag is toggled.
	if rawEnabled, ok := updates["enabled"]; ok {
		if enabled, isBool := rawEnabled.(bool); isBool {
			if enabled {
				s.registerHealthJobInternal(ctx, id)
			} else {
				s.removeHealthJobInternal(ctx, id)
			}
		}
	}

	// Create event in background (skip for local environment)
	if id != "0" {
		go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, updated.Name, event.EventTypeEnvironmentUpdate, "Environment Updated", fmt.Sprintf("Environment '%s' was updated", updated.Name), event.EventSeverityInfo, userID, username)
	}

	return updated, nil
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string, userID, username *string) error {
	// Get environment details before deletion
	env, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return err
	}

	// Stop the per-environment health job before the row is removed.
	s.removeHealthJobInternal(ctx, id)

	var syncIDs []string
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("gitops_syncs").
			Where("environment_id = ?", id).
			Pluck("id", &syncIDs).Error; err != nil {
			return errors.WrapIf(err, "failed to list environment gitops syncs")
		}

		if len(syncIDs) > 0 {
			if err := tx.Table("projects").
				Where("gitops_managed_by IN ?", syncIDs).
				Update("gitops_managed_by", nil).Error; err != nil {
				return errors.WrapIf(err, "failed to clear environment gitops project references")
			}
			if err := tx.Exec("DELETE FROM gitops_syncs WHERE environment_id = ?", id).Error; err != nil {
				return errors.WrapIf(err, "failed to delete environment gitops syncs")
			}
		}

		if err := tx.Delete(&Environment{}, "id = ?", id).Error; err != nil {
			return errors.WrapIf(err, "failed to delete environment")
		}

		return nil
	}); err != nil {
		if env.Enabled {
			s.registerHealthJobInternal(ctx, env.ID)
		}
		return err
	}

	// Deleting an environment orphans its GitOps syncs, whose jobs belong to
	// gitops.GitOpsSyncService's own registry — remove them by name here.
	if scheduler := s.jobs.Scheduler(); scheduler != nil {
		schedulerCtx := s.jobs.Context(ctx)
		for _, syncID := range syncIDs {
			scheduler.RemoveJob(schedulerCtx, entityjobs.GitOpsSyncJobPrefix+syncID)
		}
	}

	s.edgeTokens.invalidate(id)
	s.remoteEnvs.remove(id)

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, env.Name, event.EventTypeEnvironmentDelete, "Environment Deleted", fmt.Sprintf("Environment '%s' was deleted", env.Name), event.EventSeverityWarning, userID, username)

	return nil
}

func (s *EnvironmentService) createEnvironmentEvent(ctx context.Context, envID, envName string, eventType event.EventType, title, description string, severity event.EventSeverity, userID, username *string) {
	if s == nil || s.eventService == nil {
		return
	}

	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          eventType,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("environment"),
		ResourceID:    new(envID),
		ResourceName:  new(envName),
		UserID:        userID,
		Username:      username,
		EnvironmentID: new(envID),
	})
}

func (s *EnvironmentService) RegenerateEnvironmentApiKey(ctx context.Context, envID string, newApiKeyID string, apiKey string, userID, username string, envName string) error {
	// Trim once at the boundary so the value persisted, the value cached,
	// and the value returned by callers (which already TrimSpace before
	// returning) all stay byte-identical. Any divergence here would surface
	// as a 401 "invalid agent token" because lookup is direct equality.
	apiKey = strings.TrimSpace(apiKey)

	updates := map[string]any{
		"api_key_id":   newApiKeyID,
		"access_token": apiKey,
		"status":       string(EnvironmentStatusPending),
		"last_seen":    nil, // Clear last seen time
	}

	result := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", envID).Updates(updates)
	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to update environment with new API key")
	}
	if result.RowsAffected == 0 {
		// A zero-row update would otherwise report a successful rotation while
		// the new key was never linked to anything.
		return errors.New("environment not found")
	}

	s.edgeTokens.sync(envID, apiKey)
	now := time.Now()
	s.remoteEnvs.update(envID, func(environment *Environment) {
		environment.ApiKeyID = &newApiKeyID
		environment.AccessToken = &apiKey
		environment.Status = string(EnvironmentStatusPending)
		environment.LastSeen = nil
		environment.UpdatedAt = &now
	})

	// Create event log in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), envID, envName, event.EventTypeEnvironmentApiKeyRegenerated, "API Key Regenerated", "Environment API key was regenerated and status set to pending", event.EventSeverityInfo, new(userID), new(username))

	return nil
}

func (s *EnvironmentService) GetDB() *database.DB {
	return s.db
}

func (s *EnvironmentService) ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*Environment, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEnvironmentAccessTokenRequired
	}

	var env Environment
	if err := s.db.WithContext(ctx).
		Where("access_token = ?", token).
		First(&env).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidEnvironmentAccessToken
		}
		return nil, errors.WrapIf(err, "failed to resolve environment by access token")
	}

	return &env, nil
}

func (s *EnvironmentService) GetEnabledRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	var registries []registry.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to get enabled container registries")
	}

	var creds []containerregistry.Credential
	for _, reg := range registries {
		if !reg.Enabled || reg.Username == "" || reg.Token == "" {
			continue
		}

		decryptedToken, err := crypto.Decrypt(reg.Token)
		if err != nil {
			slog.WarnContext(ctx, "Failed to decrypt registry token", "registryURL", reg.URL, "error", err.Error())
			continue
		}

		creds = append(creds, containerregistry.Credential{
			URL:      reg.URL,
			Username: reg.Username,
			Token:    decryptedToken,
			Enabled:  reg.Enabled,
		})
	}

	return creds, nil
}

// SyncResourcesToEnvironment tracks a manual sync and attempts every resource group.
func (s *EnvironmentService) SyncResourcesToEnvironment(ctx context.Context, environmentID string, user *common.User, activityService activitylib.Service) (string, error) {
	return activitylib.RunHandlerActivity(ctx, activityService, activitylib.HandlerOptions{
		EnvironmentID:  environmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "environment",
		ResourceID:     environmentID,
		ResourceName:   environmentID,
		User:           user,
		Step:           "Syncing environment",
		Message:        "Syncing container registries, S3 destinations, and git repositories",
		SuccessMessage: "Environment synced successfully",
		Metadata:       database.JSON{"action": "sync_environment"},
	}, func(ctx context.Context) error {
		var failedGroups []string

		if err := s.SyncRegistriesToEnvironment(ctx, environmentID); err != nil {
			slog.WarnContext(ctx, "Failed to sync registries", "environmentID", environmentID, "error", err.Error())
			failedGroups = append(failedGroups, "container registries")
		}

		if err := s.SyncS3DestinationsToEnvironment(ctx, environmentID); err != nil {
			slog.WarnContext(ctx, "Failed to sync S3 destinations", "environmentID", environmentID, "error", err.Error())
			failedGroups = append(failedGroups, "S3 destinations")
		}

		if err := s.SyncRepositoriesToEnvironment(ctx, environmentID); err != nil {
			slog.WarnContext(ctx, "Failed to sync git repositories", "environmentID", environmentID, "error", err.Error())
			failedGroups = append(failedGroups, "git repositories")
		}

		if len(failedGroups) > 0 {
			return errors.New("Failed to sync " + strings.Join(failedGroups, ", ") + ". Other resource groups may have synced successfully. Check the manager logs, correct the failed sync, and retry.")
		}

		return nil
	})
}
