package environment

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"uuid"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment/children/health"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment/children/proxy"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment/children/snippets"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment/children/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

type EnvironmentService struct {
	db               *database.DB
	httpClient       *http.Client
	dockerService    *docker.DockerClientService
	eventService     *event.EventService
	settingsService  *settings.SettingsService
	edgeTokens       *edgeTokenCacheInternal
	remoteEnvs       *remoteEnvSnapshotCacheInternal
	environmentCache *hot.HotCache[environmentCacheKeyInternal, Environment]
	environmentGens  [environmentCacheStripes]atomic.Uint64

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

	// syncGate skips the periodic registry/S3/repository pushes to an agent
	// while the payload it last accepted is unchanged.
	syncGate utils.SyncGate

	proxy    *proxy.Service
	health   *health.Service
	snippets *snippets.Service
	swarm    *swarm.Service
}

const (
	// LocalEnvironmentID is the reserved ID of the environment Arcane manages directly.
	LocalEnvironmentID                   = "0"
	localEnvironmentFallbackNameInternal = "Local"

	// SyncDeliveryExpiry bounds how long an accepted config push is trusted. An
	// agent rebuilt with a fresh data volume while its status stayed online gets
	// everything again within this window without operator action.
	SyncDeliveryExpiry = time.Hour
)

var (
	ErrEnvironmentAccessTokenRequired = errors.New("environment access token required")
	ErrInvalidEnvironmentAccessToken  = errors.New("invalid environment access token")
	ErrEnvironmentNotFound            = errors.New("environment not found")
)

// VariableSyncer pushes the effective global-variable set to one environment.
// Implemented by variable.VariableService.
type VariableSyncer interface {
	SyncEnvironment(ctx context.Context, envID string) error
	ForgetSyncState(envID string)
}

// ForgetSyncState makes the next sync of every resource group resend to the
// environment regardless of whether the payload changed.
func (s *EnvironmentService) ForgetSyncState(environmentID string) {
	s.syncGate.Forget(environmentID)
	if s.variableSyncer != nil {
		s.variableSyncer.ForgetSyncState(environmentID)
	}
}

func NewEnvironmentService(
	db *database.DB,
	httpClient *http.Client,
	dockerService *docker.DockerClientService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	apiKeyService *apikey.ApiKeyService,
) *EnvironmentService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	s := &EnvironmentService{
		syncGate:         utils.SyncGate{Expiry: SyncDeliveryExpiry},
		db:               db,
		httpClient:       httpClient,
		dockerService:    dockerService,
		eventService:     eventService,
		settingsService:  settingsService,
		edgeTokens:       newEdgeTokenCacheInternal(),
		remoteEnvs:       newRemoteEnvSnapshotCacheInternal(),
		environmentCache: newEnvironmentCacheInternal(),
		jobs:             entityjobs.New(environmentHealthJobPrefix, environmentHealthAdmissionScopeInternal),
		health:           health.New(httpClient, dockerService),
		snippets:         snippets.New(eventService),
		swarm:            swarm.New(apiKeyService),
	}
	s.proxy = proxy.New(db, httpClient, settingsService, &s.syncGate)
	return s
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
		return "", fmt.Errorf("failed to resolve edge environment by token: %w", err)
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
	env, err := s.GetEnvironmentByIDCached(ctx, environmentID)
	if err != nil || env == nil {
		if !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "failed to resolve environment name", "environmentID", environmentID, "error", err)
		}
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
			if updateLocalURLErr := s.db.WithContext(ctx).Model(&existingEnv).Update("api_url", appUrl).Error; updateLocalURLErr != nil {
				return fmt.Errorf("failed to update local environment api url: %w", updateLocalURLErr)
			}
			s.invalidateEnvironmentCacheInternal(LocalEnvironmentID)
			slog.InfoContext(ctx, "updated local environment api url", "id", LocalEnvironmentID, "url", appUrl)
		}
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check for local environment: %w", err)
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

	if createLocalEnvironmentErr := s.db.WithContext(ctx).Create(localEnv).Error; createLocalEnvironmentErr != nil {
		return fmt.Errorf("failed to create local environment: %w", createLocalEnvironmentErr)
	}
	s.invalidateEnvironmentCacheInternal(LocalEnvironmentID)

	slog.InfoContext(ctx, "created local environment record", "id", LocalEnvironmentID)
	return nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, env *Environment, userID, username *string) (*Environment, error) {
	if err := normalization.Normalize(env); err != nil {
		return nil, err
	}
	env.ID = uuid.New().String()

	// Only set status to offline if not already set (e.g., API key flow sets it to pending)
	if env.Status == "" {
		env.Status = string(EnvironmentStatusOffline)
	}

	now := time.Now()
	env.CreatedAt = now
	env.UpdatedAt = new(now)

	if err := s.db.WithContext(ctx).Create(env).Error; err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	// Create event in background
	go s.createEnvironmentEvent(
		context.WithoutCancel(
			ctx,
		),
		env.ID,
		env.Name,
		event.EventTypeEnvironmentCreate,
		"Environment Created",
		fmt.Sprintf(
			"Environment '%s' was created",
			env.Name,
		),
		event.EventSeveritySuccess,
		userID,
		username,
	)

	if env.Enabled {
		s.registerHealthJobInternal(ctx, env.ID)
	}
	s.remoteEnvs.put(*env)
	s.invalidateEnvironmentCacheInternal(env.ID)
	s.NotifyRuntimeStateChanged()

	return env, nil
}

func (s *EnvironmentService) GetEnvironmentByID(ctx context.Context, id string) (*Environment, error) {
	var envRecord Environment
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&envRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEnvironmentNotFound
		}
		return nil, fmt.Errorf("failed to get environment: %w", err)
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
	if validateCredentialTargetChangeErr := validation.ValidateCredentialTargetChange(
		"environment API URL",
		current.ApiUrl,
		nextAPIURL,
		func(value string) string {
			normalized, normalizeErr := httpx.NormalizeBaseURL(value)
			if normalizeErr != nil {
				return strings.TrimSpace(value)
			}
			return normalized
		},
		map[string]bool{"accessToken": current.AccessToken != nil && *current.AccessToken != ""},
		map[string]bool{"accessToken": accessTokenUpdated},
	); validateCredentialTargetChangeErr != nil {
		return nil, validateCredentialTargetChangeErr
	}

	updates["updated_at"] = new(time.Now())

	if updateEnvironmentErr := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", id).Updates(updates).Error; updateEnvironmentErr != nil {
		return nil, fmt.Errorf("failed to update environment: %w", updateEnvironmentErr)
	}
	s.invalidateEnvironmentCacheInternal(id)
	s.NotifyRuntimeStateChanged()

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
		go s.createEnvironmentEvent(
			context.WithoutCancel(
				ctx,
			),
			id,
			updated.Name,
			event.EventTypeEnvironmentUpdate,
			"Environment Updated",
			fmt.Sprintf(
				"Environment '%s' was updated",
				updated.Name,
			),
			event.EventSeverityInfo,
			userID,
			username,
		)
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
	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if loadSyncIDsErr := tx.Table("gitops_syncs").
			Where("environment_id = ?", id).
			Pluck("id", &syncIDs).Error; loadSyncIDsErr != nil {
			return fmt.Errorf("failed to list environment gitops syncs: %w", loadSyncIDsErr)
		}

		if len(syncIDs) > 0 {
			if clearProjectLinksErr := tx.Table("projects").
				Where("gitops_managed_by IN ?", syncIDs).
				Update("gitops_managed_by", nil).Error; clearProjectLinksErr != nil {
				return fmt.Errorf("failed to clear environment gitops project references: %w", clearProjectLinksErr)
			}
			if deleteSyncsErr := tx.Exec("DELETE FROM gitops_syncs WHERE environment_id = ?", id).Error; deleteSyncsErr != nil {
				return fmt.Errorf("failed to delete environment gitops syncs: %w", deleteSyncsErr)
			}
		}
		if deleteEnvironmentErr := tx.Delete(&Environment{}, "id = ?", id).Error; deleteEnvironmentErr != nil {
			return fmt.Errorf("failed to delete environment: %w", deleteEnvironmentErr)
		}
		return nil
	}); transactionErr != nil {
		if env.Enabled {
			s.registerHealthJobInternal(ctx, env.ID)
		}
		return transactionErr
	}

	// Deleting an environment orphans its GitOps syncs, whose jobs belong to
	// gitops.GitOpsSyncService's own registry — remove them by name here.
	if jobScheduler := s.jobs.Scheduler(); jobScheduler != nil {
		schedulerCtx := s.jobs.Context(ctx)
		for _, syncID := range syncIDs {
			jobScheduler.RemoveJob(schedulerCtx, entityjobs.GitOpsSyncJobPrefix+syncID)
		}
	}

	s.edgeTokens.invalidate(id)
	s.ForgetSyncState(id)
	s.remoteEnvs.remove(id)
	s.invalidateEnvironmentCacheInternal(id)
	s.NotifyRuntimeStateChanged()

	// Create event in background
	go s.createEnvironmentEvent(
		context.WithoutCancel(
			ctx,
		),
		id,
		env.Name,
		event.EventTypeEnvironmentDelete,
		"Environment Deleted",
		fmt.Sprintf(
			"Environment '%s' was deleted",
			env.Name,
		),
		event.EventSeverityWarning,
		userID,
		username,
	)

	return nil
}

func (
	s *EnvironmentService,
) createEnvironmentEvent(
	ctx context.Context,
	envID, envName string,
	eventType event.EventType,
	title, description string,
	severity event.EventSeverity,
	userID, username *string,
) {
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

// RegenerateEnvironmentApiKey rotates the key and updates its environment binding.
func (s *EnvironmentService) RegenerateEnvironmentApiKey(ctx context.Context, env *Environment, userID, username string) (string, error) {
	if env == nil {
		return "", errors.New("environment is required")
	}
	return s.swarm.EnsureApiKey(ctx, env.ID, env.AccessToken, env.ApiKeyID, true,
		func(linkCtx context.Context, apiKeyID, apiKey string) error {
			return s.linkEnvironmentApiKey(linkCtx, env.ID, apiKeyID, apiKey, userID, username, env.Name)
		})
}

func (s *EnvironmentService) linkEnvironmentApiKey(ctx context.Context, envID, newApiKeyID, apiKey, userID, username, envName string) error {
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
		return fmt.Errorf("failed to update environment with new API key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// A zero-row update would otherwise report a successful rotation while
		// the new key was never linked to anything.
		return ErrEnvironmentNotFound
	}
	s.invalidateEnvironmentCacheInternal(envID)
	s.NotifyRuntimeStateChanged()

	s.edgeTokens.sync(envID, apiKey)
	now := time.Now()
	s.remoteEnvs.update(envID, func(env *Environment) {
		env.ApiKeyID = &newApiKeyID
		env.AccessToken = &apiKey
		env.Status = string(EnvironmentStatusPending)
		env.LastSeen = nil
		env.UpdatedAt = &now
	})

	// Create event log in background
	go s.createEnvironmentEvent(
		context.WithoutCancel(
			ctx,
		),
		envID,
		envName,
		event.EventTypeEnvironmentApiKeyRegenerated,
		"API Key Regenerated",
		"Environment API key was regenerated and status set to pending",
		event.EventSeverityInfo,
		new(
			userID,
		),
		new(
			username,
		),
	)

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
		return nil, fmt.Errorf("failed to resolve environment by access token: %w", err)
	}

	return &env, nil
}

func (s *EnvironmentService) GetEnabledRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error) {
	var registries []registry.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled container registries: %w", err)
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
func (s *EnvironmentService) SyncResourcesToEnvironment(ctx context.Context, environmentID string, user *usertypes.Actor, activityService activity.Service) (string, error) {
	return activity.RunHandlerActivity(ctx, activityService, activity.HandlerOptions{
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

		s.ForgetSyncState(environmentID)
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

// DisplayName returns the stored environment name or its readable fallback.
func DisplayName(environmentID, storedName string) string {
	if name := strings.TrimSpace(storedName); name != "" {
		return name
	}
	id := strings.TrimSpace(environmentID)
	return kit.Ternary(id == "" || id == LocalEnvironmentID, localEnvironmentFallbackNameInternal, id)
}

const (
	edgeTokenCacheTTL   = time.Minute
	environmentCacheTTL = 30 * time.Second
)

// GetActiveRemoteEnvironmentSnapshot returns the latest in-process snapshot for
// an enabled, visible, non-local remote environment.
func (s *EnvironmentService) GetActiveRemoteEnvironmentSnapshot(environmentID string) mo.Option[Environment] {
	if s == nil {
		return mo.None[Environment]()
	}
	return s.remoteEnvs.get(environmentID)
}

// ListActiveRemoteEnvironments returns every enabled, visible, non-local
// environment from memory, sorted by ID. The first call loads them from the
// database.
func (s *EnvironmentService) ListActiveRemoteEnvironments(ctx context.Context) ([]Environment, error) {
	s.remoteEnvs.mu.RLock()
	seeded := s.remoteEnvs.seeded
	environments := make([]Environment, 0, len(s.remoteEnvs.envs))
	for _, envRecord := range s.remoteEnvs.envs {
		environments = append(environments, envRecord)
	}
	s.remoteEnvs.mu.RUnlock()

	if !seeded {
		var err error
		if environments, err = s.ListRemoteEnvironments(ctx); err != nil {
			return nil, err
		}
	}

	slices.SortFunc(environments, func(a, b Environment) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return environments, nil
}

// GetEnvironmentByIDCached is GetEnvironmentByID behind a short TTL cache that
// also remembers unknown IDs. Status and heartbeat fields may be stale.
func (s *EnvironmentService) GetEnvironmentByIDCached(ctx context.Context, id string) (*Environment, error) {
	key := environmentCacheKeyInternal{gen: s.environmentGens[environmentCacheStripeInternal(id)].Load(), id: id}
	envRecord, found, err := s.environmentCache.GetWithLoaders(key, func(keys []environmentCacheKeyInternal) (map[environmentCacheKeyInternal]Environment, error) {
		found := make(map[environmentCacheKeyInternal]Environment, len(keys))
		for _, k := range keys {
			loaded, loadErr := s.GetEnvironmentByID(ctx, k.id)
			if errors.Is(loadErr, ErrEnvironmentNotFound) {
				continue
			}
			if loadErr != nil {
				return nil, loadErr
			}
			found[k] = *loaded
		}
		return found, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrEnvironmentNotFound
	}
	return &envRecord, nil
}

// invalidateEnvironmentCacheInternal starts a new generation for id's stripe
// after a committed write, orphaning its cached record and in-flight loads.
func (s *EnvironmentService) invalidateEnvironmentCacheInternal(id string) {
	if s == nil || s.environmentCache == nil {
		return
	}
	previous := s.environmentGens[environmentCacheStripeInternal(id)].Add(1) - 1
	s.environmentCache.Delete(environmentCacheKeyInternal{gen: previous, id: id})
}

// invalidateAllEnvironmentCacheInternal covers writes whose affected IDs are unknown.
func (s *EnvironmentService) invalidateAllEnvironmentCacheInternal() {
	if s == nil || s.environmentCache == nil {
		return
	}
	for i := range s.environmentGens {
		s.environmentGens[i].Add(1)
	}
	s.environmentCache.Purge()
}

func (s *EnvironmentService) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	if strings.TrimSpace(params.Filters["type"]) != "" {
		return s.listEnvironmentsPaginatedWithRuntimeFiltersInternal(ctx, params, accessibleEnvIDs)
	}

	var envs []Environment
	q := s.db.WithContext(ctx).Model(&Environment{}).Where("hidden = ?", false)
	// accessibleEnvIDs == nil means "no restriction". A non-nil slice limits the
	// result to those environment IDs; an empty slice therefore matches nothing.
	switch {
	case accessibleEnvIDs == nil:
		// no restriction
	case len(accessibleEnvIDs) == 0:
		q = q.Where("1 = 0")
	default:
		q = q.Where("id IN ?", accessibleEnvIDs)
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR api_url LIKE ?",
			searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &envs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate environments: %w", err)
	}

	out, mapErr := mapping.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (
	s *EnvironmentService,
) listEnvironmentsPaginatedWithRuntimeFiltersInternal(
	ctx context.Context,
	params pagination.QueryParams,
	accessibleEnvIDs []string,
) (
	[]environment.Environment,
	pagination.Response,
	error,
) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Find(&envs).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list environments: %w", err)
	}

	items, mapErr := mapping.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	// nil = no restriction; non-nil restricts to the caller's accessible envs.
	if accessibleEnvIDs != nil {
		items = filterEnvironmentsByIDInternal(items, accessibleEnvIDs)
	}

	for i := range items {
		ApplyEnvironmentRuntimeState(&items[i])
	}

	config := pagination.Config[environment.Environment]{
		SearchAccessors: []pagination.SearchAccessor[environment.Environment]{
			func(env environment.Environment) (string, error) { return env.Name, nil },
			func(env environment.Environment) (string, error) { return env.ApiUrl, nil },
		},
		SortBindings: []pagination.SortBinding[environment.Environment]{
			{
				Key: "id",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
				},
			},
			{
				Key: "name",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
				},
			},
			{
				Key: "status",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.Status), strings.ToLower(b.Status))
				},
			},
			{
				Key: "enabled",
				Fn: func(a, b environment.Environment) int {
					if a.Enabled == b.Enabled {
						return 0
					}
					return kit.Ternary(a.Enabled, 1, -1)
				},
			},
			{
				Key: "apiUrl",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.ApiUrl), strings.ToLower(b.ApiUrl))
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[environment.Environment]{
			{
				Key: "status",
				Fn: func(item environment.Environment, filterValue string) bool {
					return strings.EqualFold(item.Status, strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "enabled",
				Fn: func(item environment.Environment, filterValue string) bool {
					value, valid := kit.ParseBool(filterValue)
					return !valid || item.Enabled == value
				},
			},
			{
				Key: "type",
				Fn:  environmentTypeMatchesInternal,
			},
		},
	}

	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

// ListVisibleEnvironments returns persisted rows; callers apply ApplyEnvironmentRuntimeState.
func (s *EnvironmentService) ListVisibleEnvironments(ctx context.Context) ([]environment.Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Order("created_at asc, id asc").
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list visible environments: %w", err)
	}

	out, mapErr := mapping.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	return out, nil
}

// ListRemoteEnvironmentIDs returns the IDs of enabled remote environments; it
// satisfies agg.RemoteEnvironmentLister for aggregated stream handlers.
func (s *EnvironmentService) ListRemoteEnvironmentIDs(ctx context.Context) ([]string, error) {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(envs))
	for _, env := range envs {
		ids = append(ids, env.ID)
	}
	return ids, nil
}

// ListRemoteEnvironments returns all non-local, enabled environments for syncing purposes.
func (s *EnvironmentService) ListRemoteEnvironments(ctx context.Context) ([]Environment, error) {
	for {
		s.remoteEnvs.mu.RLock()
		revision := s.remoteEnvs.revision
		s.remoteEnvs.mu.RUnlock()

		var envs []Environment
		err := s.db.WithContext(ctx).
			Model(&Environment{}).
			Where("id != ?", "0").
			Where("enabled = ?", true).
			Where("hidden = ?", false).
			Find(&envs).Error
		if err != nil {
			return nil, fmt.Errorf("failed to list remote environments: %w", err)
		}

		s.remoteEnvs.mu.Lock()
		// Retry if a cache mutation or another refresh overtook the database read.
		if s.remoteEnvs.revision != revision {
			s.remoteEnvs.mu.Unlock()
			continue
		}
		s.remoteEnvs.envs = make(map[string]Environment, len(envs))
		for _, envRecord := range envs {
			s.remoteEnvs.envs[envRecord.ID] = envRecord
		}
		s.remoteEnvs.seeded = true
		s.remoteEnvs.revision++
		s.remoteEnvs.mu.Unlock()
		return envs, nil
	}
}

// SubscribeRuntimeChanges returns a channel that receives a coalesced wake-up
// whenever environment liveness may have changed, plus a function to release it.
func (s *EnvironmentService) SubscribeRuntimeChanges() (<-chan struct{}, func()) {
	return s.runtimeWatchers.subscribe()
}

// NotifyRuntimeStateChanged wakes every runtime watcher.
func (s *EnvironmentService) NotifyRuntimeStateChanged() {
	if s == nil {
		return
	}
	s.runtimeWatchers.notify()
}

const (
	defaultEnvironmentHealthInterval        = "0 */2 * * * *"
	environmentHealthCheckTimeout           = 90 * time.Second
	environmentHealthJobPrefix              = "environment-health:"
	environmentHealthAdmissionScopeInternal = "environment-health"
)

// SetScheduler injects the job scheduler and app lifecycle context. Called during
// bootstrap on the manager only (agent mode leaves scheduler nil, so all health-job
// registration becomes a no-op).
func (s *EnvironmentService) SetScheduler(ctx context.Context, jobScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, jobScheduler, admissionGate)
}

// registerHealthJobInternal schedules one environment's health check on the
// global environmentHealthInterval (environment health has no per-entity
// interval); the run body self-cancels cleanly when the environment is gone.
func (s *EnvironmentService) registerHealthJobInternal(ctx context.Context, envID string) {
	s.jobs.Register(ctx, envID,
		func(ctx context.Context) string {
			sched := s.settingsService.GetStringSetting(ctx, "environmentHealthInterval", defaultEnvironmentHealthInterval)
			return kit.Ternary(sched == "", defaultEnvironmentHealthInterval, sched)
		},
		func(ctx context.Context) (scheduler.Outcome, error) { return s.runHealthCheckInternal(ctx, envID) },
		func(context.Context, scheduler.Run) (scheduler.Outcome, error) {
			return scheduler.Outcome{Status: scheduler.Retrying, Message: "Connection checks can safely resume"}, nil
		},
	)
}

func (s *EnvironmentService) removeHealthJobInternal(ctx context.Context, envID string) {
	s.jobs.Unregister(ctx, envID)
}

func (s *EnvironmentService) ListEnabledEnvironmentIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).
		Table("environments").
		Where("enabled = ?", true).
		Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list enabled environments: %w", err)
	}
	return ids, nil
}

func (s *EnvironmentService) registerAllEnabledHealthJobsInternal(ctx context.Context) int {
	ids, err := s.ListEnabledEnvironmentIDs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list environments for health jobs", "error", err)
		return 0
	}
	for _, id := range ids {
		s.registerHealthJobInternal(ctx, id)
	}
	return len(ids)
}

// RegisterHealthJobsOnStartup registers a health-check job for every enabled
// environment. Replaces the old global environment-health job.
func (s *EnvironmentService) RegisterHealthJobsOnStartup(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	n := s.registerAllEnabledHealthJobsInternal(ctx)
	slog.InfoContext(ctx, "Registered environment health jobs on startup", "count", n)
}

// RescheduleHealthJobs re-registers all enabled environments' health jobs, picking
// up a changed global interval. Wired from the Jobs UI via job.JobService.
func (s *EnvironmentService) RescheduleHealthJobs(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	s.registerAllEnabledHealthJobsInternal(ctx)
}

// RunHealthChecksNow queues each enabled environment's health check.
// Child runs retain the parent identity so authorization is checked at execution.
func (s *EnvironmentService) RunHealthChecksNow(ctx context.Context) error {
	if !s.jobs.Enabled() {
		return errors.New("environment health scheduler unavailable")
	}
	ids, err := s.ListEnabledEnvironmentIDs(ctx)
	if err != nil {
		return err
	}
	parent, hasParent := jobcontext.Run(ctx)
	request := scheduler.Request{EnvironmentID: "0", Trigger: "internal"}
	if hasParent {
		request.Trigger = parent.Trigger
		request.RequestedBy = parent.RequestedBy
		request.RequestedWithKey = parent.RequestedWithKey
	}
	for _, id := range ids {
		request.JobID = s.jobs.JobName(id)
		if _, submitErr := s.jobs.Scheduler().Submit(ctx, request); submitErr != nil {
			return submitErr
		}
	}
	return nil
}

// runHealthCheckInternal tests one environment's connection (updating its DB status)
// and, for online remotes, syncs registries and repositories to it.
func (s *EnvironmentService) runHealthCheckInternal(ctx context.Context, envID string) (scheduler.Outcome, error) {
	lease, admitted, err := s.jobs.TryAcquire(ctx, envID)
	if err != nil {
		slog.ErrorContext(ctx, "environment health check admission failed", "environment_id", envID, "error", err)
		return scheduler.Outcome{}, err
	}
	if !admitted {
		slog.WarnContext(ctx, "environment health check skipped; previous run still in progress", "environment_id", envID)
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	defer lease.Release(ctx)

	localEnvironment, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return scheduler.Outcome{}, err
	}
	if !localEnvironment.Enabled {
		return scheduler.Outcome{Status: scheduler.Canceled, Message: "Environment disabled"}, nil
	}
	wasOnline := localEnvironment.Status == string(EnvironmentStatusOnline)
	status, err := s.TestConnection(ctx, envID, nil)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "environment health check failed", "environment_id", envID, "status", status, "error", err)
		return scheduler.Outcome{Status: scheduler.Retrying}, err
	case status != "online":
		return scheduler.Outcome{Status: scheduler.Retrying, Message: "Environment is offline"}, errors.New("environment is offline")
	}

	// Local environment (ID "0") has no registries/repositories to push.
	if envID == "0" {
		return scheduler.Outcome{Status: scheduler.Succeeded}, nil
	}

	// An agent that just came back may have restarted with different state;
	// resend everything once instead of trusting the last delivered payload.
	if !wasOnline {
		s.ForgetSyncState(envID)
	}

	var syncErrors []error
	syncCtx, cancel := context.WithTimeout(ctx, environmentHealthCheckTimeout)
	defer cancel()
	if syncRegistriesToEnvironmentErr := s.SyncRegistriesToEnvironment(syncCtx, envID); syncRegistriesToEnvironmentErr != nil {
		slog.WarnContext(syncCtx, "failed to sync registries during health check", "environment_id", envID, "error", syncRegistriesToEnvironmentErr)
		syncErrors = append(syncErrors, syncRegistriesToEnvironmentErr)
	}
	if syncS3DestinationsToEnvironmentErr := s.SyncS3DestinationsToEnvironment(syncCtx, envID); syncS3DestinationsToEnvironmentErr != nil {
		slog.WarnContext(syncCtx, "failed to sync S3 destinations during health check", "environment_id", envID, "error", syncS3DestinationsToEnvironmentErr)
		syncErrors = append(syncErrors, syncS3DestinationsToEnvironmentErr)
	}
	if syncRepositoriesToEnvironmentErr := s.SyncRepositoriesToEnvironment(syncCtx, envID); syncRepositoriesToEnvironmentErr != nil {
		slog.WarnContext(syncCtx, "failed to sync git repositories during health check", "environment_id", envID, "error", syncRepositoriesToEnvironmentErr)
		syncErrors = append(syncErrors, syncRepositoriesToEnvironmentErr)
	}
	if s.variableSyncer != nil {
		if syncEnvironmentErr := s.variableSyncer.SyncEnvironment(syncCtx, envID); syncEnvironmentErr != nil {
			slog.WarnContext(syncCtx, "failed to sync global variables during health check", "environment_id", envID, "error", syncEnvironmentErr)
			syncErrors = append(syncErrors, syncEnvironmentErr)
		}
	}
	if joinErr := errors.Join(syncErrors...); joinErr != nil {
		return scheduler.Outcome{Status: scheduler.Partial}, joinErr
	}
	return scheduler.Outcome{Status: scheduler.Succeeded}, nil
}

// SyncRegistriesToEnvironment syncs all registries from this manager to a remote environment.
func (s *EnvironmentService) SyncRegistriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.proxy.SyncRegistries(ctx, target)
}

// SyncS3DestinationsToEnvironment sends manager-owned destinations to one remote environment.
func (s *EnvironmentService) SyncS3DestinationsToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.proxy.SyncS3Destinations(ctx, target)
}

// SyncRepositoriesToEnvironment syncs all git repositories from this manager to a remote environment.
func (s *EnvironmentService) SyncRepositoriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.proxy.SyncRepositories(ctx, target)
}

func (s *EnvironmentService) TestConnection(ctx context.Context, id string, customApiUrl *string) (string, error) {
	envRecord, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return "error", err
	}

	apiURL := envRecord.ApiUrl
	if customApiUrl != nil {
		apiURL = *customApiUrl
	}
	status, err := s.health.Probe(ctx, id, apiURL, envRecord.IsEdge, customApiUrl != nil)
	if customApiUrl == nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, status)
		return status, err
	}
	if err != nil {
		slog.WarnContext(ctx, "Environment custom URL connection test failed", "environment_id", id, "error", err)
		return "error", common.ErrEnvironmentConnectionTestFailed
	}
	return status, nil
}

func (s *EnvironmentService) updateEnvironmentStatusInternal(ctx context.Context, id, status string) error {
	var currentEnv Environment
	if err := s.db.WithContext(ctx).Select("status", "is_edge").Where("id = ?", id).First(&currentEnv).Error; err != nil {
		return fmt.Errorf("failed to check environment status: %w", err)
	}

	if currentEnv.Status == string(EnvironmentStatusPending) {
		// Edge envs must complete pairing via the agent's outbound tunnel — manager
		// can't dial them directly, so a manager-side reachability check means nothing.
		// Direct envs are reachable from the manager, so a successful health check IS
		// the pairing signal. Don't promote on offline/error ticks though, or a transient
		// blip during initial setup would flip the env out of pending.
		if currentEnv.IsEdge || status != string(EnvironmentStatusOnline) {
			slog.DebugContext(ctx, "skipping status update for pending environment", "environment_id", id)
			return nil
		}
		slog.InfoContext(ctx, "promoted pending direct environment to online via reachability check", "environment_id", id)
	}

	now := time.Now()
	updates := map[string]any{
		"status":     status,
		"last_seen":  &now,
		"updated_at": &now,
	}
	if err := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment status: %w", err)
	}
	// Direct environments have no tunnel callback, so this health-check write is
	// the only moment their liveness changes.
	s.NotifyRuntimeStateChanged()
	return nil
}

func (s *EnvironmentService) UpdateEnvironmentHeartbeat(ctx context.Context, id string) error {
	now := time.Now()

	// Use Exec with raw SQL for better performance
	// Only update if last_seen is NULL or older than 30 seconds to reduce write frequency
	result := s.db.WithContext(ctx).Exec(`
		UPDATE environments
		SET last_seen = ?, status = ?, updated_at = ?
		WHERE id = ?
		AND (last_seen IS NULL OR last_seen < ?)
	`, new(now), string(EnvironmentStatusOnline), new(now), id, now.Add(-30*time.Second))

	if result.Error != nil {
		// The 30s throttle above doubles as the notify throttle: a no-op heartbeat
		// changed nothing worth waking a stream for.
		return fmt.Errorf("failed to update environment heartbeat: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		s.NotifyRuntimeStateChanged()
	}

	return nil
}

// UpdateEnvironmentConnectionState updates runtime connectivity status without creating
// a generic "environment updated" event. This is used for edge tunnel connect/disconnect.
func (s *EnvironmentService) UpdateEnvironmentConnectionState(ctx context.Context, id string, connected bool) error {
	now := time.Now()

	updates := map[string]any{
		"updated_at": &now,
	}
	if connected {
		updates["status"] = string(EnvironmentStatusOnline)
		updates["last_seen"] = &now
		// Remember the tunnel transport so the UI can keep showing it after
		// the tunnel drops or while the agent is poll-only.
		if state, ok := edge.GetTunnelRuntimeState(id).Get(); ok && state.Transport != "" {
			updates["last_edge_transport"] = state.Transport
		}
	} else {
		updates["status"] = string(EnvironmentStatusOffline)
	}

	if err := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment connection state: %w", err)
	}

	s.NotifyRuntimeStateChanged()

	return nil
}

// ApplyEnvironmentRuntimeState normalizes edge environment runtime status using
// in-memory tunnel and poll registries without mutating persisted state.
func ApplyEnvironmentRuntimeState(env *environment.Environment) {
	if env == nil || !env.IsEdge {
		return
	}

	connected := false
	env.Connected = &connected
	env.ConnectedAt = nil
	env.LastHeartbeat = nil
	env.LastPollAt = nil
	env.EdgeTransport = nil
	env.EdgeSecurityMode = nil
	env.EdgeSessionID = nil
	env.EdgeAgentInstance = nil
	env.EdgeCapabilities = nil

	if pollState, ok := edge.GetPollRuntimeRegistry().Get(env.ID, time.Now()).Get(); ok {
		env.LastPollAt = pollState.LastPollAt
	}

	if runtimeState, ok := edge.GetTunnelRuntimeState(env.ID).Get(); ok {
		connected = true
		env.Connected = &connected
		env.Status = string(EnvironmentStatusOnline)
		env.ConnectedAt = runtimeState.ConnectedAt
		env.LastHeartbeat = runtimeState.LastHeartbeat
		if runtimeState.SecurityMode != "" {
			env.EdgeSecurityMode = &runtimeState.SecurityMode
		}
		if runtimeState.SessionID != "" {
			env.EdgeSessionID = &runtimeState.SessionID
		}
		if runtimeState.AgentInstance != "" {
			env.EdgeAgentInstance = &runtimeState.AgentInstance
		}
		if len(runtimeState.Capabilities) > 0 {
			env.EdgeCapabilities = append([]string(nil), runtimeState.Capabilities...)
		}
		if transport, localOk := edge.GetActiveTunnelTransport(env.ID).Get(); localOk {
			env.EdgeTransport = &transport
		} else if runtimeState.Transport != "" {
			env.EdgeTransport = &runtimeState.Transport
		}
		return
	}

	if env.LastPollAt != nil {
		env.Status = string(EnvironmentStatusStandby)
		return
	}

	if env.Status != string(EnvironmentStatusPending) {
		env.Status = string(EnvironmentStatusOffline)
	}
}

// ReconcileEdgeStatusesOnStartup resets edge environments to offline when the manager starts.
// Live edge tunnels are process-local runtime state, so persisted "online" flags can be stale
// after a restart until agents reconnect. Pending environments are left untouched.
func (s *EnvironmentService) ReconcileEdgeStatusesOnStartup(ctx context.Context) error {
	result := s.db.WithContext(ctx).Model(&Environment{}).
		Where("is_edge = ?", true).
		Where("status <> ?", string(EnvironmentStatusPending)).
		Where("status <> ?", string(EnvironmentStatusOffline)).
		Updates(map[string]any{
			"status":     string(EnvironmentStatusOffline),
			"updated_at": new(time.Now()),
		})
	if result.Error != nil {
		return fmt.Errorf("failed to reconcile edge environment statuses: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		slog.InfoContext(ctx, "Reconciled stale edge environment statuses on startup", "count", result.RowsAffected)
	}

	return nil
}

// SyncRegistriesToRemoteEnvironments syncs container registries to all eligible remote environments.
// Eligibility requires a non-local, enabled environment with a configured access token.
func (s *EnvironmentService) SyncRegistriesToRemoteEnvironments(ctx context.Context) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list remote environments for registry sync: %w", err)
	}

	if len(envs) == 0 {
		return nil
	}

	var failedCount int
	for _, env := range envs {
		if env.AccessToken == nil || *env.AccessToken == "" {
			slog.DebugContext(ctx, "Skipping registry sync for environment without access token",
				"environmentID", env.ID,
				"environmentName", env.Name)
			continue
		}

		if syncRegistriesToEnvironmentErr := s.SyncRegistriesToEnvironment(ctx, env.ID); syncRegistriesToEnvironmentErr != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync registries to remote environment",
				"environmentID", env.ID,
				"environmentName", env.Name,
				"error", syncRegistriesToEnvironmentErr.Error())
		}
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to sync registries to %d remote environment(s)", failedCount)
	}

	return nil
}

// SyncS3DestinationsToRemoteEnvironments refreshes the destination cache on every enabled remote environment.
func (s *EnvironmentService) SyncS3DestinationsToRemoteEnvironments(ctx context.Context) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list remote environments for S3 destination sync: %w", err)
	}

	var failedCount int
	for _, env := range envs {
		if env.AccessToken == nil || strings.TrimSpace(*env.AccessToken) == "" {
			slog.DebugContext(ctx, "Skipping S3 destination sync for environment without access token", "environmentID", env.ID, "environmentName", env.Name)
			continue
		}
		if syncS3DestinationsToEnvironmentErr := s.SyncS3DestinationsToEnvironment(ctx, env.ID); syncS3DestinationsToEnvironmentErr != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync S3 destinations to remote environment", "environmentID", env.ID, "environmentName", env.Name, "error", syncS3DestinationsToEnvironmentErr)
		}
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to sync S3 destinations to %d remote environment(s)", failedCount)
	}
	return nil
}

// CheckS3DestinationReferences returns an error while any managed environment
// still references the destination, or when a synced environment cannot be
// checked conclusively. Deleting credentials that a remote environment still
// needs would strand its policies and retained backups.
func (s *EnvironmentService) CheckS3DestinationReferences(ctx context.Context, destinationID string) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list remote environments for S3 destination reference check: %w", err)
	}
	for _, env := range envs {
		if env.AccessToken == nil || strings.TrimSpace(*env.AccessToken) == "" {
			// Environments without an access token never receive destination
			// syncs, so they cannot hold a reference.
			continue
		}
		var result struct {
			InUse bool `json:"inUse"`
		}
		if proxyJSONRequestForEnvironmentErr := s.ProxyJSONRequestForEnvironment(
			ctx,
			env,
			http.MethodGet,
			"/api/backups/s3/"+url.PathEscape(
				destinationID,
			)+"/in-use",
			nil,
			&result,
		); proxyJSONRequestForEnvironmentErr != nil {
			return fmt.Errorf("cannot verify S3 destination references on environment %s; restore connectivity before deleting: %w", env.Name, proxyJSONRequestForEnvironmentErr)
		}
		if result.InUse {
			return fmt.Errorf("still referenced by environment %s", env.Name)
		}
	}
	return nil
}

func (s *EnvironmentService) resolveRemoteEnvironmentTargetInternal(ctx context.Context, envID string) (*proxy.Target, error) {
	envRecord, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return proxy.NewTarget(envRecord.ID, envRecord.Name, envRecord.ApiUrl, envRecord.IsEdge, envRecord.AccessToken)
}

func (s *EnvironmentService) ExecuteRemoteRequest(ctx context.Context, envID, method, path string, body []byte) (*remenv.Response, error) {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, envID)
	if err != nil {
		return nil, err
	}

	return s.proxy.Execute(ctx, target, method, path, body)
}

func (s *EnvironmentService) ProxyJSONRequest(ctx context.Context, envID, method, path string, body []byte, out any) error {
	proxyCtx, cancel := s.proxy.Context(ctx)
	defer cancel()

	target, err := s.resolveRemoteEnvironmentTargetInternal(proxyCtx, envID)
	if err != nil {
		return err
	}

	return s.proxy.JSON(proxyCtx, target, method, path, body, out)
}

// ProxyJSONRequestForEnvironment sends a JSON request using an already-loaded
// environment row, avoiding an extra environment lookup on hot stream paths.
func (s *EnvironmentService) ProxyJSONRequestForEnvironment(ctx context.Context, env Environment, method, path string, body []byte, out any) error {
	proxyCtx, cancel := s.proxy.Context(ctx)
	defer cancel()

	target, err := proxy.NewTarget(env.ID, env.Name, env.ApiUrl, env.IsEdge, env.AccessToken)
	if err != nil {
		return err
	}

	return s.proxy.JSON(proxyCtx, target, method, path, body, out)
}

// ProxyRequest sends a request to a remote environment's API.
func (s *EnvironmentService) ProxyRequest(ctx context.Context, envID, method, path string, body []byte) ([]byte, int, error) {
	proxyCtx, cancel := s.proxy.Context(ctx)
	defer cancel()

	resp, err := s.ExecuteRemoteRequest(proxyCtx, envID, method, path, body)
	if err != nil {
		return nil, 0, err
	}

	return resp.Body, resp.StatusCode, nil
}

// DeploymentSnippets contains deployment configuration snippets for an environment.
type DeploymentSnippets struct {
	DockerRun     string
	DockerCompose string
	MTLS          *DeploymentSnippetMTLS
}

type DeploymentSnippetFile struct {
	Name          string `json:"name" doc:"Suggested filename"`
	Content       string `json:"content,omitempty" doc:"PEM file contents. Omitted for sensitive files such as private keys; use downloadUrl instead."`
	DownloadURL   string `json:"downloadUrl,omitempty" doc:"Pairing-permission endpoint to download this file when content is withheld"`
	Sensitive     bool   `json:"sensitive,omitempty" doc:"True when this file is sensitive and must be fetched via downloadUrl"`
	ContainerPath string `json:"containerPath" doc:"Container mount path expected by the mTLS snippet"`
	Permissions   string `json:"permissions" doc:"Suggested file mode"`
}

type DeploymentSnippetMTLS struct {
	DockerRun     string                  `json:"dockerRun" doc:"Docker run snippet using Arcane-generated mTLS assets"`
	DockerCompose string                  `json:"dockerCompose" doc:"Docker compose snippet using Arcane-generated mTLS assets"`
	Files         []DeploymentSnippetFile `json:"files" doc:"Generated PEM files to place on the edge host"`
	HostDirHint   string                  `json:"hostDirHint" doc:"Suggested host directory containing the generated PEM files"`
}

// GenerateDeploymentSnippets generates Docker deployment snippets for an environment.
func (s *EnvironmentService) GenerateDeploymentSnippets(ctx context.Context, envID, managerURL, agentURL, apiKey string) (*DeploymentSnippets, error) {
	dockerRun, dockerCompose := snippets.Direct(strings.TrimRight(managerURL, "/"), agentURL, apiKey)
	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}, nil
}

// GenerateEdgeDeploymentSnippets generates Docker deployment snippets for an edge agent.
// Edge agents connect outbound to the manager and don't require exposed ports.
func (s *EnvironmentService) GenerateEdgeDeploymentSnippets(ctx context.Context, envID, managerURL, apiKey string, edgeCfg *edge.Config) (*DeploymentSnippets, error) {
	managerURL = strings.TrimRight(managerURL, "/")
	dockerRun, dockerCompose := snippets.Edge(managerURL, apiKey)
	result := &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}

	envName := ""
	if s != nil && s.db != nil {
		if env, getErr := s.GetEnvironmentByID(ctx, envID); getErr == nil && env != nil {
			envName = env.Name
		}
	}
	if edgeCfg != nil && strings.TrimSpace(edgeCfg.AppURL) == "" {
		edgeCfg.AppURL = managerURL
	}

	generatedAssets, mtlsDockerRun, mtlsDockerCompose := s.snippets.GenerateMTLS(ctx, edgeCfg, envID, envName, managerURL, apiKey)
	if generatedAssets == nil {
		return result, nil
	}

	files := make([]DeploymentSnippetFile, 0, len(generatedAssets.Files))
	for _, file := range generatedAssets.Files {
		files = append(files, DeploymentSnippetFile{
			Name:          file.Name,
			Content:       file.Content,
			ContainerPath: file.ContainerPath,
			Permissions:   file.Permissions,
		})
	}
	result.MTLS = &DeploymentSnippetMTLS{
		DockerRun:     mtlsDockerRun,
		DockerCompose: mtlsDockerCompose,
		Files:         files,
		HostDirHint:   strings.TrimSpace(generatedAssets.HostDirHint),
	}
	return result, nil
}

func (s *EnvironmentService) ListSwarmNodeAgentEnvironments(ctx context.Context, parentEnvironmentID string) ([]Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list swarm node agent environments: %w", err)
	}
	return envs, nil
}

// ListSwarmNodeCandidateEnvironments returns enabled visible environments that
// can provide swarm-node coverage for a manager environment.
func (s *EnvironmentService) ListSwarmNodeCandidateEnvironments(ctx context.Context) ([]Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Where("enabled = ?", true).
		Where("id <> ?", "0").
		Order("name ASC").
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list swarm node candidate environments: %w", err)
	}
	return envs, nil
}

// BindSwarmNodeEnvironment binds an existing visible environment to a swarm
// node without modifying its connection details or agent token.
func (s *EnvironmentService) BindSwarmNodeEnvironment(
	ctx context.Context,
	parentEnvironmentID, nodeID, environmentID string,
	rebind bool,
) (*Environment, error) {
	var envRecord Environment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", environmentID).First(&envRecord).Error; err != nil {
			return fmt.Errorf("failed to load environment for swarm node binding: %w", err)
		}
		if err := s.swarm.AuthorizeBinding(environment.SwarmBindingCandidate{
			Hidden:              envRecord.Hidden,
			Enabled:             envRecord.Enabled,
			ParentEnvironmentID: envRecord.ParentEnvironmentID,
			SwarmNodeID:         envRecord.SwarmNodeID,
		}, parentEnvironmentID, nodeID, rebind, func() (int64, error) {
			var existingVisibleBindings int64
			err := tx.Model(&Environment{}).
				Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ? AND id <> ?", false, parentEnvironmentID, nodeID, environmentID).
				Count(&existingVisibleBindings).Error
			return existingVisibleBindings, err
		}); err != nil {
			return err
		}
		if rebind {
			if err := tx.Model(&Environment{}).
				Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ? AND id <> ?", false, parentEnvironmentID, nodeID, environmentID).
				Updates(map[string]any{"parent_environment_id": nil, "swarm_node_id": nil, "updated_at": new(time.Now())}).Error; err != nil {
				return fmt.Errorf("failed to clear previous swarm node binding: %w", err)
			}
		}

		if err := tx.Model(&Environment{}).Where("id = ?", environmentID).Updates(map[string]any{
			"parent_environment_id": parentEnvironmentID,
			"swarm_node_id":         nodeID,
			"updated_at":            new(time.Now()),
		}).Error; err != nil {
			return fmt.Errorf("failed to bind environment to swarm node: %w", err)
		}

		return tx.Where("id = ?", environmentID).First(&envRecord).Error
	})
	if err != nil {
		return nil, err
	}

	s.remoteEnvs.put(envRecord)
	// A rebind also clears the binding on other environments.
	if rebind {
		s.invalidateAllEnvironmentCacheInternal()
	} else {
		s.invalidateEnvironmentCacheInternal(envRecord.ID)
	}
	s.NotifyRuntimeStateChanged()
	return &envRecord, nil
}

// DetachSwarmNodeEnvironment clears a visible environment binding from a node.
func (s *EnvironmentService) DetachSwarmNodeEnvironment(ctx context.Context, parentEnvironmentID, nodeID string) error {
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&Environment{}).
		Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ?", false, parentEnvironmentID, nodeID).
		Updates(map[string]any{"parent_environment_id": nil, "swarm_node_id": nil, "updated_at": &now}).Error; err != nil {
		return fmt.Errorf("failed to detach swarm node environment: %w", err)
	}
	s.invalidateAllEnvironmentCacheInternal()
	s.NotifyRuntimeStateChanged()

	return nil
}

// DeleteSwarmNodeAgentDeployment removes a dedicated hidden agent registration
// while leaving visible remote environments untouched.
func (s *EnvironmentService) DeleteSwarmNodeAgentDeployment(ctx context.Context, parentEnvironmentID, nodeID string, userID, username *string) error {
	var envRecord Environment
	if err := s.db.WithContext(ctx).
		Where("hidden = ? AND parent_environment_id = ? AND swarm_node_id = ?", true, parentEnvironmentID, nodeID).
		First(&envRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("failed to load swarm node agent deployment: %w", err)
	}

	return s.DeleteEnvironment(ctx, envRecord.ID, userID, username)
}

func (s *EnvironmentService) EnsureSwarmNodeAgentEnvironment(
	ctx context.Context,
	parentEnvironmentID, nodeID, hostname, userID, username string,
	rotate bool,
) (*Environment, string, error) {
	agentName, agentURL, err := s.swarm.AgentIdentity(parentEnvironmentID, nodeID, hostname)
	if err != nil {
		return nil, "", err
	}

	var env Environment
	// Prefer an existing visible binding. Legacy hidden registrations remain
	// reusable, but all newly provisioned node agents are normal Remote
	// Environments so one token and one agent can serve both use cases.
	err = s.db.WithContext(ctx).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Where("swarm_node_id = ?", nodeID).
		Order("hidden ASC").
		First(&env).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", fmt.Errorf("failed to load swarm node agent environment: %w", err)
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		createdEnv := &Environment{
			Name:                agentName,
			ApiUrl:              agentURL,
			Status:              string(EnvironmentStatusPending),
			Enabled:             true,
			IsEdge:              true,
			Hidden:              false,
			ParentEnvironmentID: new(parentEnvironmentID),
			SwarmNodeID:         new(nodeID),
		}

		if _, createErr := s.CreateEnvironment(ctx, createdEnv, new(userID), new(username)); createErr != nil {
			return nil, "", fmt.Errorf("failed to create swarm node agent environment: %w", createErr)
		}
		env = *createdEnv
	}

	apiKey, err := s.swarm.EnsureApiKey(ctx, env.ID, env.AccessToken, env.ApiKeyID, rotate, func(ctx context.Context, apiKeyID, apiKey string) error {
		return s.linkEnvironmentApiKey(ctx, env.ID, apiKeyID, apiKey, userID, username, env.Name)
	})
	if err != nil {
		return nil, "", err
	}

	refreshedEnv, err := s.GetEnvironmentByID(ctx, env.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to refresh swarm node agent environment: %w", err)
	}

	return refreshedEnv, apiKey, nil
}

func (s *EnvironmentService) UpdateSwarmNodeIdentity(ctx context.Context, envID, swarmNodeID string) error {
	updates := map[string]any{
		"swarm_node_id": swarmNodeID,
		"updated_at":    new(time.Now()),
	}

	if err := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", envID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update swarm node identity: %w", err)
	}
	s.invalidateEnvironmentCacheInternal(envID)
	s.NotifyRuntimeStateChanged()

	return nil
}
