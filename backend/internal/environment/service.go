package environment

import (
	"cmp"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"hash/maphash"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"uuid"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/backup"
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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

type EnvironmentService struct {
	db               *database.DB
	eventService     *event.EventService
	settingsService  *settings.SettingsService
	edgeTokens       *edgeTokenCache
	remoteEnvs       *remoteEnvSnapshotCache
	environmentCache *hot.HotCache[environmentCacheKey, Environment]
	environmentGens  [environmentCacheStripes]atomic.Uint64
	accessTokens     *hot.HotCache[string, string]
	jobs             *entityjobs.Registry

	variableSyncer  VariableSyncer
	runtimeWatchers runtimeWatchers
	syncGate        *utils.SyncGate
	proxy           *proxy.Service
	health          *health.Service
	snippets        *snippets.Service
	swarm           *swarm.Service
}

const (
	// LocalEnvironmentID is the reserved ID of the environment Arcane manages directly.
	LocalEnvironmentID           = "0"
	localEnvironmentFallbackName = "Local"

	// SyncDeliveryExpiry bounds how long an accepted config push is trusted, so a
	// rebuilt agent that stayed online is resynced without operator action.
	SyncDeliveryExpiry = time.Hour
)

var (
	ErrEnvironmentAccessTokenRequired = errors.New("environment access token required")
	ErrInvalidEnvironmentAccessToken  = errors.New("invalid environment access token")
	ErrEnvironmentNotFound            = errors.New("environment not found")
)

// VariableSyncer pushes the effective global-variable set to one environment.
type VariableSyncer interface {
	SyncEnvironment(ctx context.Context, envID string) error
	ForgetSyncState(envID string)
}

type resourceSync struct {
	kind string
	sync func(ctx context.Context, environmentID string) error
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

// ForgetSyncState makes the next sync of every resource group resend regardless of payload changes.
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
	syncGate := &utils.SyncGate{Expiry: SyncDeliveryExpiry}
	return &EnvironmentService{
		syncGate:        syncGate,
		db:              db,
		eventService:    eventService,
		settingsService: settingsService,
		edgeTokens: &edgeTokenCache{
			byToken: hot.NewHotCache[string, string](hot.LRU, 1024).WithTTL(edgeTokenCacheTTL).WithJanitor().Build(),
			byEnvID: make(map[string]string),
		},
		remoteEnvs: &remoteEnvSnapshotCache{envs: make(map[string]Environment)},
		// Caches records by ID, including misses, for hot paths that read only CRUD-managed fields.
		environmentCache: hot.NewHotCache[environmentCacheKey, Environment](hot.LRU, 256).
			WithTTL(environmentCacheTTL).
			WithMissingSharedCache().
			WithJanitor().
			Build(),
		accessTokens:    hot.NewHotCache[string, string](hot.LRU, 1024).WithTTL(environmentCacheTTL).WithJanitor().Build(),
		jobs:            entityjobs.New(environmentHealthJobPrefix, environmentHealthAdmissionScope),
		runtimeWatchers: runtimeWatchers{chans: make(map[int]chan struct{})},
		proxy:           proxy.New(db, httpClient, settingsService, syncGate),
		health:          health.New(httpClient, dockerService),
		snippets:        snippets.New(eventService),
		swarm:           swarm.New(apiKeyService),
	}
}

// RegisterMetrics reports the arcane.environments gauge; only the manager calls it.
func (s *EnvironmentService) RegisterMetrics() {
	meter := otel.Meter(tracing.InstrumentationName)
	statusGauge, err := meter.Int64ObservableGauge("arcane.environments",
		metric.WithDescription("Visible environments by persisted status"),
		metric.WithUnit("{environment}"),
	)
	if err == nil {
		_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
			var rows []struct {
				Status string
				Count  int64
			}
			if scanErr := s.db.WithContext(ctx).Model(&Environment{}).
				Select("status, COUNT(*) AS count").
				Where("hidden = ?", false).
				Group("status").
				Scan(&rows).Error; scanErr != nil {
				return fmt.Errorf("failed to count environments by status: %w", scanErr)
			}
			// Known statuses report zero so their series drop instead of going stale.
			counts := map[string]int64{
				string(EnvironmentStatusOnline):  0,
				string(EnvironmentStatusStandby): 0,
				string(EnvironmentStatusOffline): 0,
				string(EnvironmentStatusError):   0,
				string(EnvironmentStatusPending): 0,
			}
			for _, row := range rows {
				counts[row.Status] += row.Count
			}
			for status, count := range counts {
				o.ObserveInt64(statusGauge, count, metric.WithAttributes(attribute.String("status", status)))
			}
			return nil
		}, statusGauge)
	}
	if err != nil {
		otel.Handle(err)
	}
}

// SetVariableSyncer injects the global-variable syncer on the manager; agents leave it nil.
func (s *EnvironmentService) SetVariableSyncer(syncer VariableSyncer) {
	s.variableSyncer = syncer
}

func (s *EnvironmentService) ResolveEdgeEnvironmentByToken(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("agent token required")
	}

	staleEnvironmentID, wasCached := s.edgeTokens.byToken.Peek(token)
	if envID, ok, _ := s.edgeTokens.byToken.Get(token); ok {
		return envID, nil
	}
	if wasCached {
		s.edgeTokens.mu.Lock()
		if currentToken, indexed := s.edgeTokens.byEnvID[staleEnvironmentID]; indexed && currentToken == token {
			delete(s.edgeTokens.byEnvID, staleEnvironmentID)
		}
		s.edgeTokens.mu.Unlock()
	}

	var env Environment
	if err := s.db.WithContext(ctx).
		Select("id", "access_token").
		Where("is_edge = ?", true).
		Where("access_token = ?", token).
		First(&env).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("failed to resolve edge environment by token: %w", err)
		}
		// Log edge environment counts so a miss can be diagnosed; only the token's
		// length and irreversible fingerprint are logged.
		if slog.Default().Enabled(ctx, slog.LevelDebug) {
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
		return "", errors.New("invalid agent token")
	}

	s.edgeTokens.sync(env.ID, token)
	return env.ID, nil
}

// ResolveEnvironmentName returns an environment's display label; names are
// user-editable, so never hardcode one for a known ID.
func (s *EnvironmentService) ResolveEnvironmentName(ctx context.Context, environmentID string) string {
	if strings.TrimSpace(environmentID) == "" {
		environmentID = LocalEnvironmentID
	}
	env, err := s.GetEnvironmentByIDCached(ctx, environmentID)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.WarnContext(ctx, "failed to resolve environment name", "environmentId", environmentID, "error", err)
		}
		return DisplayName(environmentID, "")
	}
	return DisplayName(env.ID, env.Name)
}

func (s *EnvironmentService) EnsureLocalEnvironment(ctx context.Context, appUrl string) error {
	var existingEnv Environment
	err := s.db.WithContext(ctx).Where("id = ?", LocalEnvironmentID).First(&existingEnv).Error

	if err == nil {
		if existingEnv.ApiUrl != appUrl {
			if updateLocalURLErr := s.db.WithContext(ctx).Model(&existingEnv).Update("api_url", appUrl).Error; updateLocalURLErr != nil {
				return fmt.Errorf("failed to update local environment api url: %w", updateLocalURLErr)
			}
			s.invalidateEnvironmentCache(LocalEnvironmentID)
			slog.InfoContext(ctx, "updated local environment api url", "id", LocalEnvironmentID, "url", appUrl)
		}
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check for local environment: %w", err)
	}

	now := time.Now()
	if createLocalEnvironmentErr := s.db.WithContext(ctx).Create(&Environment{
		ID:        LocalEnvironmentID,
		CreatedAt: now,
		UpdatedAt: new(now),
		Name:      "Local Docker",
		ApiUrl:    appUrl,
		Status:    string(EnvironmentStatusOnline),
		Enabled:   true,
	}).Error; createLocalEnvironmentErr != nil {
		return fmt.Errorf("failed to create local environment: %w", createLocalEnvironmentErr)
	}
	s.invalidateEnvironmentCache(LocalEnvironmentID)

	slog.InfoContext(ctx, "created local environment record", "id", LocalEnvironmentID)
	return nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, env *Environment, userID, username *string) (*Environment, error) {
	if err := normalization.Normalize(env); err != nil {
		return nil, err
	}
	now := time.Now()
	*env = Environment{ //exhaustruct:enforce
		BaseModel: database.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: now,
			UpdatedAt: new(now),
		},
		Name:   env.Name,
		ApiUrl: env.ApiUrl,
		// Only set status to offline if not already set (e.g., API key flow sets it to pending)
		Status:              cmp.Or(env.Status, string(EnvironmentStatusOffline)),
		Enabled:             env.Enabled,
		IsEdge:              env.IsEdge,
		Hidden:              env.Hidden,
		LastSeen:            env.LastSeen,
		LastEdgeTransport:   env.LastEdgeTransport,
		AccessToken:         env.AccessToken,
		ApiKeyID:            env.ApiKeyID,
		ParentEnvironmentID: env.ParentEnvironmentID,
		SwarmNodeID:         env.SwarmNodeID,
	}

	if err := s.db.WithContext(ctx).Create(env).Error; err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	go s.createEnvironmentEvent(context.WithoutCancel(ctx), env.ID, env.Name, event.EventTypeEnvironmentCreate,
		"Environment Created", fmt.Sprintf("Environment '%s' was created", env.Name), event.EventSeveritySuccess, userID, username)

	if env.Enabled {
		s.registerHealthJob(ctx, env.ID)
	}
	s.remoteEnvs.put(*env)
	s.invalidateEnvironmentCache(env.ID)
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

	if validateCredentialTargetChangeErr := validation.ValidateCredentialTargetChange(
		"environment API URL",
		current.ApiUrl,
		func() *string {
			if apiURL, isString := updates["api_url"].(string); isString {
				return new(apiURL)
			}
			return nil
		}(),
		func(value string) string {
			normalized, normalizeErr := httpx.NormalizeBaseURL(value)
			if normalizeErr != nil {
				return strings.TrimSpace(value)
			}
			return normalized
		},
		map[string]bool{"accessToken": kit.FromPtr(current.AccessToken) != ""},
		map[string]bool{"accessToken": func() bool {
			_, accessTokenUpdated := updates["access_token"]
			return accessTokenUpdated
		}()},
	); validateCredentialTargetChangeErr != nil {
		return nil, validateCredentialTargetChangeErr
	}

	updates["updated_at"] = new(time.Now())

	if updateEnvironmentErr := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", id).Updates(updates).Error; updateEnvironmentErr != nil {
		return nil, fmt.Errorf("failed to update environment: %w", updateEnvironmentErr)
	}
	s.invalidateEnvironmentCache(id)
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
				s.registerHealthJob(ctx, id)
			} else {
				s.jobs.Unregister(ctx, id)
			}
		}
	}

	if id != LocalEnvironmentID {
		go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, updated.Name, event.EventTypeEnvironmentUpdate,
			"Environment Updated", fmt.Sprintf("Environment '%s' was updated", updated.Name), event.EventSeverityInfo, userID, username)
	}

	return updated, nil
}

func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string, userID, username *string) error {
	env, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return err
	}

	// Stop the per-environment health job before the row is removed.
	s.jobs.Unregister(ctx, id)

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
			s.registerHealthJob(ctx, env.ID)
		}
		return transactionErr
	}

	// The deleted GitOps syncs' jobs live in the gitops registry, so remove them by name.
	if jobScheduler := s.jobs.Scheduler(); jobScheduler != nil {
		schedulerCtx := s.jobs.Context(ctx)
		for _, syncID := range syncIDs {
			jobScheduler.RemoveJob(schedulerCtx, entityjobs.GitOpsSyncJobPrefix+syncID)
		}
	}

	s.edgeTokens.sync(id, "")
	s.ForgetSyncState(id)
	s.remoteEnvs.mu.Lock()
	delete(s.remoteEnvs.envs, id)
	s.remoteEnvs.revision++
	s.remoteEnvs.mu.Unlock()
	s.invalidateEnvironmentCache(id)
	s.NotifyRuntimeStateChanged()

	go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, env.Name, event.EventTypeEnvironmentDelete,
		"Environment Deleted", fmt.Sprintf("Environment '%s' was deleted", env.Name), event.EventSeverityWarning, userID, username)

	return nil
}

func (s *EnvironmentService) createEnvironmentEvent(
	ctx context.Context,
	envID, envName string,
	eventType event.EventType,
	title, description string,
	severity event.EventSeverity,
	userID, username *string,
) {
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
	return s.swarm.EnsureApiKey(ctx, env.ID, env.AccessToken, env.ApiKeyID, true,
		func(linkCtx context.Context, apiKeyID, apiKey string) error {
			return s.linkEnvironmentApiKey(linkCtx, env.ID, apiKeyID, apiKey, userID, username, env.Name)
		})
}

func (s *EnvironmentService) linkEnvironmentApiKey(ctx context.Context, envID, newApiKeyID, apiKey, userID, username, envName string) error {
	// Persisted, cached, and returned keys must stay byte-identical: token lookup is direct equality.
	apiKey = strings.TrimSpace(apiKey)

	result := s.db.WithContext(ctx).Model(&Environment{}).Where("id = ?", envID).Updates(map[string]any{
		"api_key_id":   newApiKeyID,
		"access_token": apiKey,
		"status":       string(EnvironmentStatusPending),
		"last_seen":    nil,
	})
	if result.Error != nil {
		return fmt.Errorf("failed to update environment with new API key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// Otherwise a rotation would succeed without linking the new key.
		return ErrEnvironmentNotFound
	}
	s.invalidateEnvironmentCache(envID)
	s.NotifyRuntimeStateChanged()

	s.edgeTokens.sync(envID, apiKey)
	s.remoteEnvs.mu.Lock()
	s.remoteEnvs.revision++
	if envRecord, ok := s.remoteEnvs.envs[envID]; ok {
		s.remoteEnvs.envs[envID] = Environment{ //exhaustruct:enforce
			BaseModel: database.BaseModel{
				ID:        envRecord.ID,
				CreatedAt: envRecord.CreatedAt,
				UpdatedAt: new(time.Now()),
			},
			Name:                envRecord.Name,
			ApiUrl:              envRecord.ApiUrl,
			Status:              string(EnvironmentStatusPending),
			Enabled:             envRecord.Enabled,
			IsEdge:              envRecord.IsEdge,
			Hidden:              envRecord.Hidden,
			LastSeen:            nil,
			LastEdgeTransport:   envRecord.LastEdgeTransport,
			AccessToken:         &apiKey,
			ApiKeyID:            &newApiKeyID,
			ParentEnvironmentID: envRecord.ParentEnvironmentID,
			SwarmNodeID:         envRecord.SwarmNodeID,
		}
	}
	s.remoteEnvs.mu.Unlock()

	go s.createEnvironmentEvent(context.WithoutCancel(ctx), envID, envName, event.EventTypeEnvironmentApiKeyRegenerated,
		"API Key Regenerated", "Environment API key was regenerated and status set to pending", event.EventSeverityInfo, new(userID), new(username))

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

	if envID, ok, _ := s.accessTokens.Get(token); ok {
		cached, err := s.GetEnvironmentByIDCached(ctx, envID)
		if err == nil && subtle.ConstantTimeCompare([]byte(kit.FromPtr(cached.AccessToken)), []byte(token)) == 1 {
			return cached, nil
		}
		s.accessTokens.Delete(token)
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

	s.accessTokens.Set(token, env.ID)
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
			slog.WarnContext(ctx, "Failed to decrypt registry token", "registryUrl", reg.URL, "error", err.Error())
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
		for _, resource := range s.resourceSyncs() {
			if err := resource.sync(ctx, environmentID); err != nil {
				slog.WarnContext(ctx, "Failed to sync resources", "kind", resource.kind, "environmentId", environmentID, "error", err.Error())
				failedGroups = append(failedGroups, resource.kind)
			}
		}

		if len(failedGroups) > 0 {
			return errors.New("Failed to sync " + strings.Join(failedGroups, ", ") + ". Other resource groups may have synced successfully. Check the manager logs, correct the failed sync, and retry.")
		}

		return nil
	})
}

// resourceSyncs lists the manager-owned resource groups pushed to remote environments.
func (s *EnvironmentService) resourceSyncs() []resourceSync {
	return []resourceSync{
		{"container registries", s.SyncRegistriesToEnvironment},
		{"S3 destinations", s.SyncS3DestinationsToEnvironment},
		{"git repositories", s.SyncRepositoriesToEnvironment},
	}
}

// DisplayName returns the stored environment name or its readable fallback.
func DisplayName(environmentID, storedName string) string {
	if name := strings.TrimSpace(storedName); name != "" {
		return name
	}
	id := strings.TrimSpace(environmentID)
	return kit.Ternary(id == "" || id == LocalEnvironmentID, localEnvironmentFallbackName, id)
}

const (
	edgeTokenCacheTTL   = time.Minute
	environmentCacheTTL = 30 * time.Second
)

// GetActiveRemoteEnvironmentSnapshot returns the latest in-process snapshot for
// an enabled, visible, non-local remote environment.
func (s *EnvironmentService) GetActiveRemoteEnvironmentSnapshot(environmentID string) mo.Option[Environment] {
	s.remoteEnvs.mu.RLock()
	envRecord, ok := s.remoteEnvs.envs[environmentID]
	s.remoteEnvs.mu.RUnlock()
	if !ok || !isActiveRemoteEnvironment(envRecord) {
		return mo.None[Environment]()
	}
	return mo.Some(envRecord)
}

// ListActiveRemoteEnvironments returns every enabled, visible, non-local
// environment from memory, sorted by ID; the first call loads them from the database.
func (s *EnvironmentService) ListActiveRemoteEnvironments(ctx context.Context) ([]Environment, error) {
	s.remoteEnvs.mu.RLock()
	seeded := s.remoteEnvs.seeded
	environments := slices.AppendSeq(make([]Environment, 0, len(s.remoteEnvs.envs)), maps.Values(s.remoteEnvs.envs))
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
// remembers unknown IDs; status and heartbeat fields may be stale.
func (s *EnvironmentService) GetEnvironmentByIDCached(ctx context.Context, id string) (*Environment, error) {
	key := environmentCacheKey{gen: s.environmentGens[maphash.String(environmentCacheSeed, id)%environmentCacheStripes].Load(), id: id}
	envRecord, found, err := s.environmentCache.GetWithLoaders(key, func(keys []environmentCacheKey) (map[environmentCacheKey]Environment, error) {
		found := make(map[environmentCacheKey]Environment, len(keys))
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

// invalidateEnvironmentCache starts a new generation for id's stripe after a
// committed write, orphaning its cached record and in-flight loads.
func (s *EnvironmentService) invalidateEnvironmentCache(id string) {
	previous := s.environmentGens[maphash.String(environmentCacheSeed, id)%environmentCacheStripes].Add(1) - 1
	s.environmentCache.Delete(environmentCacheKey{gen: previous, id: id})
}

func (s *EnvironmentService) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	if strings.TrimSpace(params.Filters["type"]) != "" {
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

		// nil means no restriction; non-nil restricts to the caller's accessible environments.
		if accessibleEnvIDs != nil {
			items = slices.DeleteFunc(items, func(item environment.Environment) bool { return !slices.Contains(accessibleEnvIDs, item.ID) })
		}

		rows := make([]environment.Environment, 0, len(items))
		for _, item := range items {
			rows = append(rows, ApplyEnvironmentRuntimeState(item))
		}

		result := pagination.Config[environment.Environment]{
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
					Fn: func(item environment.Environment, filterValue string) bool {
						if !item.IsEdge {
							return strings.ToLower(strings.TrimSpace(filterValue)) == "http"
						}
						// Disconnected or poll-only agents classify by the transport they last used.
						switch strings.ToLower(strings.TrimSpace(kit.Ternary(kit.FromPtr(item.Connected) && item.EdgeTransport != nil,
							kit.FromPtr(item.EdgeTransport), kit.FromPtr(item.LastEdgeTransport)))) {
						case edge.EdgeTransportWebSocket:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "websocket"
						case edge.EdgeTransportGRPC:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "grpc"
						default:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "edge"
						}
					},
				},
			},
		}.SearchOrderAndPaginate(rows, params)

		return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
	}

	var envs []Environment
	q := s.db.WithContext(ctx).Model(&Environment{}).Where("hidden = ?", false)
	// nil means no restriction; a non-nil slice limits results, so an empty one matches nothing.
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
			Where("id != ?", LocalEnvironmentID).
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
	w := &s.runtimeWatchers
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++
	id := w.seq
	// Capacity 1: watchers read live state, so a second pending signal is redundant.
	ch := make(chan struct{}, 1)
	w.chans[id] = ch

	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.chans, id)
	}
}

// NotifyRuntimeStateChanged wakes every runtime watcher without blocking, so
// tunnel callbacks may call it.
func (s *EnvironmentService) NotifyRuntimeStateChanged() {
	s.runtimeWatchers.mu.Lock()
	defer s.runtimeWatchers.mu.Unlock()

	for _, ch := range s.runtimeWatchers.chans {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

const (
	defaultEnvironmentHealthInterval = "0 */2 * * * *"
	environmentHealthCheckTimeout    = 90 * time.Second
	environmentHealthJobPrefix       = "environment-health:"
	environmentHealthAdmissionScope  = "environment-health"
)

// SetScheduler injects the job scheduler and app lifecycle context on the manager;
// agents leave it nil, making health-job registration a no-op.
func (s *EnvironmentService) SetScheduler(ctx context.Context, jobScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, jobScheduler, admissionGate)
}

// registerHealthJob schedules one environment's health check on the global
// environmentHealthInterval; the run self-cancels once the environment is gone.
func (s *EnvironmentService) registerHealthJob(ctx context.Context, envID string) {
	s.jobs.Register(ctx, envID,
		func(ctx context.Context) string {
			return schedule.Or(ctx, s.settingsService.GetStringSetting(ctx, "environmentHealthInterval", defaultEnvironmentHealthInterval), defaultEnvironmentHealthInterval, environmentHealthJobPrefix+envID)
		},
		func(ctx context.Context) (scheduler.Outcome, error) { return s.runHealthCheck(ctx, envID) },
		func(context.Context, scheduler.Run) (scheduler.Outcome, error) {
			return scheduler.Outcome{Status: scheduler.Retrying, Message: "Connection checks can safely resume"}, nil
		},
	)
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

// RescheduleHealthJobs re-registers every enabled environment's health job,
// picking up a changed global interval.
func (s *EnvironmentService) RescheduleHealthJobs(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	ids, err := s.ListEnabledEnvironmentIDs(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list environments for health jobs", "error", err)
		return
	}
	for _, id := range ids {
		s.registerHealthJob(ctx, id)
	}
	slog.InfoContext(ctx, "Registered environment health jobs", "count", len(ids))
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
	for _, id := range ids {
		if _, submitErr := s.jobs.Scheduler().Submit(ctx, scheduler.Request{
			JobID:            s.jobs.JobName(id),
			EnvironmentID:    LocalEnvironmentID,
			Trigger:          kit.Ternary(hasParent, parent.Trigger, "internal"),
			RequestedBy:      parent.RequestedBy,
			RequestedWithKey: parent.RequestedWithKey,
		}); submitErr != nil {
			return submitErr
		}
	}
	return nil
}

// runHealthCheck tests one environment's connection, persisting its status, and
// syncs manager-owned resources to online remotes.
func (s *EnvironmentService) runHealthCheck(ctx context.Context, envID string) (scheduler.Outcome, error) {
	lease, admitted, err := s.jobs.TryAcquire(ctx, envID)
	if err != nil {
		slog.ErrorContext(ctx, "environment health check admission failed", "environmentId", envID, "error", err)
		return scheduler.Outcome{}, err
	}
	if !admitted {
		slog.WarnContext(ctx, "environment health check skipped; previous run still in progress", "environmentId", envID)
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
		slog.WarnContext(ctx, "environment health check failed", "environmentId", envID, "status", status, "error", err)
		return scheduler.Outcome{Status: scheduler.Retrying}, err
	case status != "online":
		return scheduler.Outcome{Status: scheduler.Retrying, Message: "Environment is offline"}, errors.New("environment is offline")
	}

	// The local environment has no resources to push.
	if envID == LocalEnvironmentID {
		return scheduler.Outcome{Status: scheduler.Succeeded}, nil
	}

	// An agent that just came back may have restarted with different state, so resend everything.
	if !wasOnline {
		s.ForgetSyncState(envID)
	}

	var syncErrors []error
	syncCtx, cancel := context.WithTimeout(ctx, environmentHealthCheckTimeout)
	defer cancel()
	for _, resource := range s.resourceSyncs() {
		if syncErr := resource.sync(syncCtx, envID); syncErr != nil {
			slog.WarnContext(syncCtx, "failed to sync resources during health check", "kind", resource.kind, "environmentId", envID, "error", syncErr)
			syncErrors = append(syncErrors, syncErr)
		}
	}
	if s.variableSyncer != nil {
		if syncErr := s.variableSyncer.SyncEnvironment(syncCtx, envID); syncErr != nil {
			slog.WarnContext(syncCtx, "failed to sync global variables during health check", "environmentId", envID, "error", syncErr)
			syncErrors = append(syncErrors, syncErr)
		}
	}
	if joinErr := errors.Join(syncErrors...); joinErr != nil {
		return scheduler.Outcome{Status: scheduler.Partial}, joinErr
	}
	return scheduler.Outcome{Status: scheduler.Succeeded}, nil
}

// SyncRegistriesToEnvironment syncs all registries from this manager to a remote environment.
func (s *EnvironmentService) SyncRegistriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTarget(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.proxy.SyncRegistries(ctx, target)
}

// SyncS3DestinationsToEnvironment sends manager-owned destinations to one remote environment.
func (s *EnvironmentService) SyncS3DestinationsToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTarget(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.proxy.SyncS3Destinations(ctx, target)
}

// SyncRepositoriesToEnvironment syncs all git repositories from this manager to a remote environment.
func (s *EnvironmentService) SyncRepositoriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTarget(ctx, environmentID)
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

	status, err := s.health.Probe(ctx, id, mo.PointerToOption(customApiUrl).OrElse(envRecord.ApiUrl), envRecord.IsEdge, customApiUrl != nil)
	if customApiUrl == nil {
		if updateErr := s.updateEnvironmentStatus(ctx, id, status); updateErr != nil {
			slog.WarnContext(ctx, "Failed to persist environment status", "environmentId", id, "status", status, "error", updateErr)
		}
		return status, err
	}
	if err != nil {
		slog.WarnContext(ctx, "Environment custom URL connection test failed", "environmentId", id, "error", err)
		return "error", common.ErrEnvironmentConnectionTestFailed
	}
	return status, nil
}

func (s *EnvironmentService) updateEnvironmentStatus(ctx context.Context, id, status string) error {
	var currentEnv Environment
	if err := s.db.WithContext(ctx).Select("status", "is_edge").Where("id = ?", id).First(&currentEnv).Error; err != nil {
		return fmt.Errorf("failed to check environment status: %w", err)
	}

	if currentEnv.Status == string(EnvironmentStatusPending) {
		// Only a successful check pairs a direct environment; edge environments pair
		// through the agent's tunnel, so manager reachability means nothing for them.
		if currentEnv.IsEdge || status != string(EnvironmentStatusOnline) {
			slog.DebugContext(ctx, "skipping status update for pending environment", "environmentId", id)
			return nil
		}
		slog.InfoContext(ctx, "promoted pending direct environment to online via reachability check", "environmentId", id)
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
	// Direct environments have no tunnel callback, so this is their only liveness change.
	s.NotifyRuntimeStateChanged()
	return nil
}

func (s *EnvironmentService) UpdateEnvironmentHeartbeat(ctx context.Context, id string) error {
	now := time.Now()

	// Throttle unchanged online status to one write every 30 seconds; this doubles as the notify throttle.
	result := s.db.WithContext(ctx).Exec(`
		UPDATE environments
		SET last_seen = ?, status = ?, updated_at = ?
		WHERE id = ?
		AND (status != ? OR last_seen IS NULL OR last_seen < ?)
	`, new(now), string(EnvironmentStatusOnline), new(now), id, string(EnvironmentStatusOnline), now.Add(-30*time.Second))

	if result.Error != nil {
		return fmt.Errorf("failed to update environment heartbeat: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		s.NotifyRuntimeStateChanged()
	}

	return nil
}

// UpdateEnvironmentConnectionState records an edge tunnel connect or disconnect
// without emitting an "environment updated" event.
func (s *EnvironmentService) UpdateEnvironmentConnectionState(ctx context.Context, id string, connected bool) error {
	now := time.Now()

	updates := map[string]any{
		"updated_at": &now,
	}
	if connected {
		updates["status"] = string(EnvironmentStatusOnline)
		updates["last_seen"] = &now
		// Remember the transport so the UI can show it after the tunnel drops or while poll-only.
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

// ApplyEnvironmentRuntimeState overlays in-memory tunnel and poll state onto an
// edge environment without mutating persisted state.
func ApplyEnvironmentRuntimeState(env environment.Environment) environment.Environment {
	if !env.IsEdge {
		return env
	}

	pollState, _ := edge.GetPollRuntimeRegistry().Get(env.ID, time.Now()).Get()
	runtimeState, connected := edge.GetTunnelRuntimeState(env.ID).Get()
	return environment.Environment{ //exhaustruct:enforce
		ID:     env.ID,
		Name:   env.Name,
		ApiUrl: env.ApiUrl,
		Status: func() string {
			switch {
			case connected:
				return string(EnvironmentStatusOnline)
			case pollState.LastPollAt != nil:
				return string(EnvironmentStatusStandby)
			case env.Status != string(EnvironmentStatusPending):
				return string(EnvironmentStatusOffline)
			default:
				return env.Status
			}
		}(),
		Enabled:           env.Enabled,
		IsEdge:            env.IsEdge,
		LastSeen:          env.LastSeen,
		EdgeTransport:     kit.Ternary(kit.FromPtr(runtimeState).Transport != "", new(kit.FromPtr(runtimeState).Transport), nil),
		LastEdgeTransport: env.LastEdgeTransport,
		EdgeSecurityMode:  kit.Ternary(kit.FromPtr(runtimeState).SecurityMode != "", new(kit.FromPtr(runtimeState).SecurityMode), nil),
		EdgeSessionID:     kit.Ternary(kit.FromPtr(runtimeState).SessionID != "", new(kit.FromPtr(runtimeState).SessionID), nil),
		EdgeAgentInstance: kit.Ternary(kit.FromPtr(runtimeState).AgentInstance != "", new(kit.FromPtr(runtimeState).AgentInstance), nil),
		EdgeCapabilities:  kit.Ternary(len(kit.FromPtr(runtimeState).Capabilities) > 0, slices.Clone(kit.FromPtr(runtimeState).Capabilities), nil),
		EdgeDataPlane:     kit.Ternary(connected, new(kit.FromPtr(runtimeState).DataPlane), nil),
		EdgeRoundTripMs: kit.Ternary(kit.FromPtr(runtimeState).RoundTripTime != nil,
			new(float64(kit.FromPtr(kit.FromPtr(runtimeState).RoundTripTime).Microseconds())/1000), nil),
		EdgeOpenStreams:     kit.Ternary(connected, new(kit.FromPtr(runtimeState).OpenStreams), nil),
		Connected:           new(connected),
		ConnectedAt:         kit.FromPtr(runtimeState).ConnectedAt,
		LastHeartbeat:       kit.FromPtr(runtimeState).LastHeartbeat,
		LastPollAt:          pollState.LastPollAt,
		EdgeMTLSCertificate: env.EdgeMTLSCertificate,
		ApiKey:              env.ApiKey,
	}
}

// ReconcileEdgeStatusesOnStartup marks non-pending edge environments offline at
// startup, since tunnels are process-local and persisted "online" flags are stale.
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

// SyncRegistriesToRemoteEnvironments syncs container registries to every remote environment with an access token.
func (s *EnvironmentService) SyncRegistriesToRemoteEnvironments(ctx context.Context) error {
	return s.syncToRemoteEnvironments(ctx, "registries", s.SyncRegistriesToEnvironment)
}

// SyncS3DestinationsToRemoteEnvironments refreshes the destination cache on every remote environment with an access token.
func (s *EnvironmentService) SyncS3DestinationsToRemoteEnvironments(ctx context.Context) error {
	return s.syncToRemoteEnvironments(ctx, "S3 destinations", s.SyncS3DestinationsToEnvironment)
}

func (s *EnvironmentService) syncToRemoteEnvironments(ctx context.Context, kind string, sync func(context.Context, string) error) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list remote environments for %s sync: %w", kind, err)
	}

	var failedCount int
	for _, env := range envs {
		if strings.TrimSpace(kit.FromPtr(env.AccessToken)) == "" {
			slog.DebugContext(ctx, "Skipping sync for environment without access token", "kind", kind, "environmentId", env.ID, "environmentName", env.Name)
			continue
		}
		if syncErr := sync(ctx, env.ID); syncErr != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync to remote environment", "kind", kind, "environmentId", env.ID, "environmentName", env.Name, "error", syncErr)
		}
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to sync %s to %d remote environment(s)", kind, failedCount)
	}
	return nil
}

// CheckS3DestinationReferences fails while any synced environment references the
// destination or cannot be checked, since deleting it would strand remote backups.
func (s *EnvironmentService) CheckS3DestinationReferences(ctx context.Context, destinationID string) error {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list remote environments for S3 destination reference check: %w", err)
	}
	for _, env := range envs {
		// Environments without an access token never receive destination syncs.
		if strings.TrimSpace(kit.FromPtr(env.AccessToken)) == "" {
			continue
		}
		var result backup.S3DestinationUsage
		if proxyErr := s.ProxyJSONRequestForEnvironment(ctx, env, http.MethodGet, "/api/backups/s3/"+url.PathEscape(destinationID)+"/in-use", nil, &result); proxyErr != nil {
			return fmt.Errorf("cannot verify S3 destination references on environment %s; restore connectivity before deleting: %w", env.Name, proxyErr)
		}
		if result.InUse {
			return fmt.Errorf("still referenced by environment %s", env.Name)
		}
	}
	return nil
}

func (s *EnvironmentService) resolveRemoteEnvironmentTarget(ctx context.Context, envID string) (*proxy.Target, error) {
	envRecord, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return proxy.NewTarget(envRecord.ID, envRecord.Name, envRecord.ApiUrl, envRecord.IsEdge, envRecord.AccessToken)
}

func (s *EnvironmentService) ExecuteRemoteRequest(ctx context.Context, envID, method, path string, body []byte) (*remenv.Response, error) {
	target, err := s.resolveRemoteEnvironmentTarget(ctx, envID)
	if err != nil {
		return nil, err
	}

	return s.proxy.Execute(ctx, target, method, path, body)
}

func (s *EnvironmentService) ProxyJSONRequest(ctx context.Context, envID, method, path string, body []byte, out any) error {
	proxyCtx, cancel := s.proxy.Context(ctx)
	defer cancel()

	target, err := s.resolveRemoteEnvironmentTarget(proxyCtx, envID)
	if err != nil {
		return err
	}

	return s.proxy.JSON(proxyCtx, target, method, path, body, out)
}

// ProxyJSONRequestForEnvironment sends a JSON request for an already-loaded environment row.
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

// GenerateDeploymentSnippets generates Docker deployment snippets for an environment.
func (s *EnvironmentService) GenerateDeploymentSnippets(ctx context.Context, envID, managerURL, agentURL, apiKey string) (*DeploymentSnippets, error) {
	dockerRun, dockerCompose := snippets.Direct(strings.TrimRight(managerURL, "/"), agentURL, apiKey)
	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}, nil
}

// GenerateEdgeDeploymentSnippets generates Docker deployment snippets for an
// edge agent, which dials the manager and exposes no ports.
func (s *EnvironmentService) GenerateEdgeDeploymentSnippets(ctx context.Context, envID, managerURL, apiKey string, edgeCfg *edge.Config) (*DeploymentSnippets, error) {
	managerURL = strings.TrimRight(managerURL, "/")
	dockerRun, dockerCompose := snippets.Edge(managerURL, apiKey)
	generatedAssets, mtlsDockerRun, mtlsDockerCompose := s.snippets.GenerateMTLS(ctx,
		func() *edge.Config {
			if edgeCfg == nil {
				return nil
			}
			return &edge.Config{ //exhaustruct:enforce
				EdgeAgent:             edgeCfg.EdgeAgent,
				EdgeTransport:         edgeCfg.EdgeTransport,
				EdgeReconnectInterval: edgeCfg.EdgeReconnectInterval,
				EdgeMTLSMode:          edgeCfg.EdgeMTLSMode,
				EdgeMTLSCAFile:        edgeCfg.EdgeMTLSCAFile,
				EdgeMTLSCertFile:      edgeCfg.EdgeMTLSCertFile,
				EdgeMTLSKeyFile:       edgeCfg.EdgeMTLSKeyFile,
				EdgeMTLSServerName:    edgeCfg.EdgeMTLSServerName,
				EdgeMTLSAssetsDir:     edgeCfg.EdgeMTLSAssetsDir,
				AppURL:                kit.Ternary(strings.TrimSpace(edgeCfg.AppURL) == "", managerURL, edgeCfg.AppURL),
				ManagerApiUrl:         edgeCfg.ManagerApiUrl,
				AgentToken:            edgeCfg.AgentToken,
				Port:                  edgeCfg.Port,
				Listen:                edgeCfg.Listen,
				EdgeMuxDisabled:       edgeCfg.EdgeMuxDisabled,
			}
		}(),
		envID,
		func() string {
			env, getErr := s.GetEnvironmentByID(ctx, envID)
			if getErr != nil {
				return ""
			}
			return env.Name
		}(),
		managerURL,
		apiKey,
	)
	if generatedAssets == nil {
		return &DeploymentSnippets{
			DockerRun:     dockerRun,
			DockerCompose: dockerCompose,
		}, nil
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
	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
		MTLS: &DeploymentSnippetMTLS{
			DockerRun:     mtlsDockerRun,
			DockerCompose: mtlsDockerCompose,
			Files:         files,
			HostDirHint:   strings.TrimSpace(generatedAssets.HostDirHint),
		},
	}, nil
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

// ListSwarmNodeCandidateEnvironments returns enabled visible environments that can cover a swarm node.
func (s *EnvironmentService) ListSwarmNodeCandidateEnvironments(ctx context.Context) ([]Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Where("enabled = ?", true).
		Where("id <> ?", LocalEnvironmentID).
		Order("name ASC").
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list swarm node candidate environments: %w", err)
	}
	return envs, nil
}

// BindSwarmNodeEnvironment binds a visible environment to a swarm node, keeping its connection and token.
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
	// A rebind also clears the binding on other environments, whose IDs are unknown.
	if rebind {
		for i := range s.environmentGens {
			s.environmentGens[i].Add(1)
		}
		s.environmentCache.Purge()
	} else {
		s.invalidateEnvironmentCache(envRecord.ID)
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
	// The detached environment's ID is unknown, so invalidate every stripe.
	for i := range s.environmentGens {
		s.environmentGens[i].Add(1)
	}
	s.environmentCache.Purge()
	s.NotifyRuntimeStateChanged()

	return nil
}

// DeleteSwarmNodeAgentDeployment removes a dedicated hidden agent registration, leaving visible ones untouched.
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
	// Prefer a visible binding; legacy hidden registrations stay reusable, but new
	// node agents are normal remote environments so one token serves both uses.
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
	s.invalidateEnvironmentCache(envID)
	s.NotifyRuntimeStateChanged()

	return nil
}
