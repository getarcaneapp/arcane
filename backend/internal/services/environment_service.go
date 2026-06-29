package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/google/uuid"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

type EnvironmentService struct {
	db              *database.DB
	httpClient      *http.Client
	dockerService   *DockerClientService
	eventService    *EventService
	settingsService *SettingsService
	apiKeyService   *ApiKeyService
	remoteClient    *remenv.Client
	tokenCacheMu    sync.RWMutex
	tokenCache      map[string]edgeTokenCacheEntry
	tokenByEnvID    map[string]string
	remoteEnvMu     sync.RWMutex
	remoteEnvs      map[string]models.Environment

	// scheduler and lifecycleCtx are injected post-construction via SetScheduler
	// (manager-only). Each enabled environment gets its own health-check job; this
	// replaces the single global environment-health job.
	scheduler    DynamicScheduler
	lifecycleCtx context.Context
	// runningHealthChecks guards against a per-environment health check overlapping
	// with itself (replaces the old single job's atomic "running" guard).
	runningHealthChecks sync.Map
}

const (
	defaultEnvironmentHealthInterval = "0 */2 * * * *"
	environmentHealthCheckTimeout    = 90 * time.Second
)

type edgeTokenCacheEntry struct {
	EnvironmentID string
	ExpiresAt     time.Time
}

const edgeTokenCacheTTL = time.Minute

var (
	ErrEnvironmentAccessTokenRequired = errors.New("environment access token required")
	ErrInvalidEnvironmentAccessToken  = errors.New("invalid environment access token")
)

func NewEnvironmentService(db *database.DB, httpClient *http.Client, dockerService *DockerClientService, eventService *EventService, settingsService *SettingsService, apiKeyService *ApiKeyService) *EnvironmentService {
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
		tokenCache:   make(map[string]edgeTokenCacheEntry),
		tokenByEnvID: make(map[string]string),
		remoteEnvs:   make(map[string]models.Environment),
	}
}

const environmentHealthJobPrefix = "environment-health:"

func environmentHealthJobNameInternal(envID string) string { return environmentHealthJobPrefix + envID }

// SetScheduler injects the job scheduler and app lifecycle context. Called during
// bootstrap on the manager only (agent mode leaves scheduler nil, so all health-job
// registration becomes a no-op).
func (s *EnvironmentService) SetScheduler(ctx context.Context, scheduler DynamicScheduler) { //nolint:contextcheck // health-check jobs must capture the app lifecycle context, not request contexts
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *EnvironmentService) schedulerCtxInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

// buildHealthJobInternal builds the dynamic job for one environment. The schedule is
// the global environmentHealthInterval (environment health has no per-entity
// interval); the run body re-reads nothing it can't and self-cancels cleanly when
// the environment is gone.
func (s *EnvironmentService) buildHealthJobInternal(envID string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: environmentHealthJobNameInternal(envID),
		ScheduleFn: func(ctx context.Context) string {
			sched := s.settingsService.GetStringSetting(ctx, "environmentHealthInterval", defaultEnvironmentHealthInterval)
			if sched == "" {
				return defaultEnvironmentHealthInterval
			}
			return sched
		},
		RunFn: func(ctx context.Context) {
			s.runHealthCheckInternal(ctx, envID)
		},
	}
}

func (s *EnvironmentService) registerHealthJobInternal(ctx context.Context, envID string) {
	if s.scheduler == nil {
		return
	}
	schedulerCtx := s.schedulerCtxInternal(ctx)
	if err := s.scheduler.AddJob(schedulerCtx, s.buildHealthJobInternal(envID)); err != nil {
		slog.ErrorContext(schedulerCtx, "Failed to register environment health job", "environment_id", envID, "error", err)
	}
}

func (s *EnvironmentService) removeHealthJobInternal(ctx context.Context, envID string) {
	if s.scheduler == nil {
		return
	}
	s.scheduler.RemoveJob(s.schedulerCtxInternal(ctx), environmentHealthJobNameInternal(envID))
}

func (s *EnvironmentService) listEnabledEnvironmentIDsInternal(ctx context.Context) ([]string, error) {
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
	ids, err := s.listEnabledEnvironmentIDsInternal(ctx)
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
	if s.scheduler == nil {
		return
	}
	n := s.registerAllEnabledHealthJobsInternal(ctx)
	slog.InfoContext(ctx, "Registered environment health jobs on startup", "count", n)
}

// RescheduleHealthJobs re-registers all enabled environments' health jobs, picking
// up a changed global interval. Wired from the Jobs UI via JobService.
func (s *EnvironmentService) RescheduleHealthJobs(ctx context.Context) {
	if s.scheduler == nil {
		return
	}
	s.registerAllEnabledHealthJobsInternal(ctx)
}

// RunHealthChecksNow runs every enabled environment's health check synchronously.
// Backs the "run now" button for the environment-health job in the Jobs UI.
func (s *EnvironmentService) RunHealthChecksNow(ctx context.Context) error {
	ids, err := s.listEnabledEnvironmentIDsInternal(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.runHealthCheckInternal(ctx, id)
	}
	return nil
}

func (s *EnvironmentService) acquireHealthLockInternal(envID string) (func(), bool) {
	if _, loaded := s.runningHealthChecks.LoadOrStore(envID, struct{}{}); loaded {
		return nil, false
	}
	return func() { s.runningHealthChecks.Delete(envID) }, true
}

// runHealthCheckInternal tests one environment's connection (updating its DB status)
// and, for online remotes, syncs registries and repositories to it.
func (s *EnvironmentService) runHealthCheckInternal(ctx context.Context, envID string) {
	release, ok := s.acquireHealthLockInternal(envID)
	if !ok {
		slog.WarnContext(ctx, "environment health check skipped; previous run still in progress", "environment_id", envID)
		return
	}
	defer release()

	status, err := s.TestConnection(ctx, envID, nil)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "environment health check failed", "environment_id", envID, "status", status, "error", err)
		return
	case status != "online":
		return
	}

	// Local environment (ID "0") has no registries/repositories to push.
	if envID == "0" {
		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, environmentHealthCheckTimeout)
	defer cancel()
	if err := s.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
		slog.WarnContext(syncCtx, "failed to sync registries during health check", "environment_id", envID, "error", err)
	}
	if err := s.SyncRepositoriesToEnvironment(syncCtx, envID); err != nil {
		slog.WarnContext(syncCtx, "failed to sync git repositories during health check", "environment_id", envID, "error", err)
	}
}

func (s *EnvironmentService) ResolveEdgeEnvironmentByToken(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("agent token required")
	}

	if envID, ok := s.getCachedEnvironmentIDForTokenInternal(token, time.Now()); ok {
		return envID, nil
	}

	var env models.Environment
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

	s.cacheEnvironmentTokenInternal(env.ID, token, time.Now())
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
	totalEdgeEnvsErr := s.db.WithContext(ctx).Model(&models.Environment{}).Where("is_edge = ?", true).Count(&totalEdgeEnvs).Error
	edgeEnvsWithTokenErr := s.db.WithContext(ctx).Model(&models.Environment{}).
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

func (s *EnvironmentService) getCachedEnvironmentIDForTokenInternal(token string, now time.Time) (string, bool) {
	if s == nil || token == "" {
		return "", false
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.tokenCacheMu.RLock()
	entry, ok := s.tokenCache[token]
	s.tokenCacheMu.RUnlock()
	if !ok {
		return "", false
	}
	if entry.ExpiresAt.After(now) {
		return entry.EnvironmentID, true
	}

	s.tokenCacheMu.Lock()
	defer s.tokenCacheMu.Unlock()

	entry, ok = s.tokenCache[token]
	if !ok {
		return "", false
	}
	if !entry.ExpiresAt.After(now) {
		delete(s.tokenCache, token)
		if currentToken, ok := s.tokenByEnvID[entry.EnvironmentID]; ok && currentToken == token {
			delete(s.tokenByEnvID, entry.EnvironmentID)
		}
		return "", false
	}

	return entry.EnvironmentID, true
}

func (s *EnvironmentService) cacheEnvironmentTokenInternal(envID string, token string, now time.Time) {
	if s == nil || envID == "" || token == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.tokenCacheMu.Lock()
	defer s.tokenCacheMu.Unlock()

	if previousToken, ok := s.tokenByEnvID[envID]; ok && previousToken != token {
		delete(s.tokenCache, previousToken)
	}

	s.tokenByEnvID[envID] = token
	s.tokenCache[token] = edgeTokenCacheEntry{
		EnvironmentID: envID,
		ExpiresAt:     now.Add(edgeTokenCacheTTL),
	}
}

func (s *EnvironmentService) invalidateEnvironmentTokenInternal(envID string) {
	if s == nil || envID == "" {
		return
	}

	s.tokenCacheMu.Lock()
	defer s.tokenCacheMu.Unlock()

	if token, ok := s.tokenByEnvID[envID]; ok {
		delete(s.tokenByEnvID, envID)
		delete(s.tokenCache, token)
	}
}

func (s *EnvironmentService) syncEnvironmentTokenCacheInternal(envID string, token string) {
	if s == nil || envID == "" {
		return
	}

	s.invalidateEnvironmentTokenInternal(envID)

	resolvedToken := strings.TrimSpace(token)

	if resolvedToken != "" {
		s.cacheEnvironmentTokenInternal(envID, resolvedToken, time.Now())
	}
}

// GetActiveRemoteEnvironmentSnapshot returns the latest in-process snapshot for
// an enabled, visible, non-local remote environment.
func (s *EnvironmentService) GetActiveRemoteEnvironmentSnapshot(environmentID string) (models.Environment, bool) {
	if s == nil || environmentID == "" {
		return models.Environment{}, false
	}

	s.remoteEnvMu.RLock()
	environment, ok := s.remoteEnvs[environmentID]
	s.remoteEnvMu.RUnlock()
	if !ok || !isActiveRemoteEnvironmentInternal(environment) {
		return models.Environment{}, false
	}
	return environment, true
}

func isActiveRemoteEnvironmentInternal(environment models.Environment) bool {
	return environment.ID != "" && environment.ID != "0" && environment.Enabled && !environment.Hidden
}

func (s *EnvironmentService) syncRemoteEnvironmentSnapshotsInternal(environments []models.Environment) {
	if s == nil {
		return
	}

	next := make(map[string]models.Environment, len(environments))
	for _, environment := range environments {
		if isActiveRemoteEnvironmentInternal(environment) {
			next[environment.ID] = environment
		}
	}

	s.remoteEnvMu.Lock()
	s.remoteEnvs = next
	s.remoteEnvMu.Unlock()
}

func (s *EnvironmentService) cacheRemoteEnvironmentSnapshotInternal(environment models.Environment) {
	if s == nil {
		return
	}

	if !isActiveRemoteEnvironmentInternal(environment) {
		s.removeRemoteEnvironmentSnapshotInternal(environment.ID)
		return
	}

	s.remoteEnvMu.Lock()
	s.remoteEnvs[environment.ID] = environment
	s.remoteEnvMu.Unlock()
}

func (s *EnvironmentService) removeRemoteEnvironmentSnapshotInternal(environmentID string) {
	if s == nil || environmentID == "" {
		return
	}

	s.remoteEnvMu.Lock()
	delete(s.remoteEnvs, environmentID)
	s.remoteEnvMu.Unlock()
}

func (s *EnvironmentService) updateRemoteEnvironmentSnapshotInternal(environmentID string, update func(*models.Environment)) {
	if s == nil || environmentID == "" || update == nil {
		return
	}

	s.remoteEnvMu.Lock()
	defer s.remoteEnvMu.Unlock()

	environment, ok := s.remoteEnvs[environmentID]
	if !ok {
		return
	}
	update(&environment)
	if isActiveRemoteEnvironmentInternal(environment) {
		s.remoteEnvs[environmentID] = environment
	} else {
		delete(s.remoteEnvs, environmentID)
	}
}

func (s *EnvironmentService) EnsureLocalEnvironment(ctx context.Context, appUrl string) error {
	const localEnvID = "0"

	var existingEnv models.Environment
	err := s.db.WithContext(ctx).Where("id = ?", localEnvID).First(&existingEnv).Error

	if err == nil {
		// Local environment already exists, ensure ApiUrl matches current appUrl
		if existingEnv.ApiUrl != appUrl {
			if err := s.db.WithContext(ctx).Model(&existingEnv).Update("api_url", appUrl).Error; err != nil {
				return fmt.Errorf("failed to update local environment api url: %w", err)
			}
			slog.InfoContext(ctx, "updated local environment api url", "id", localEnvID, "url", appUrl)
		}
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check for local environment: %w", err)
	}

	// Create the local environment
	now := time.Now()
	localEnv := &models.Environment{
		BaseModel: models.BaseModel{
			ID:        localEnvID,
			CreatedAt: now,
			UpdatedAt: new(now),
		},
		Name:    "Local Docker",
		ApiUrl:  appUrl,
		Status:  string(models.EnvironmentStatusOnline),
		Enabled: true,
	}

	if err := s.db.WithContext(ctx).Create(localEnv).Error; err != nil {
		return fmt.Errorf("failed to create local environment: %w", err)
	}

	slog.InfoContext(ctx, "created local environment record", "id", localEnvID)
	return nil
}

func (s *EnvironmentService) CreateEnvironment(ctx context.Context, environment *models.Environment, userID, username *string) (*models.Environment, error) {
	environment.ID = uuid.New().String()

	// Only set status to offline if not already set (e.g., API key flow sets it to pending)
	if environment.Status == "" {
		environment.Status = string(models.EnvironmentStatusOffline)
	}

	now := time.Now()
	environment.CreatedAt = now
	environment.UpdatedAt = new(now)

	if err := s.db.WithContext(ctx).Create(environment).Error; err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), environment.ID, environment.Name, models.EventTypeEnvironmentCreate, "Environment Created", fmt.Sprintf("Environment '%s' was created", environment.Name), models.EventSeveritySuccess, userID, username)

	if environment.Enabled {
		s.registerHealthJobInternal(ctx, environment.ID)
	}
	s.cacheRemoteEnvironmentSnapshotInternal(*environment)

	return environment, nil
}

func (s *EnvironmentService) ListSwarmNodeAgentEnvironments(ctx context.Context, parentEnvironmentID string) ([]models.Environment, error) {
	var envs []models.Environment
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("hidden = ?", true).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list swarm node agent environments: %w", err)
	}

	return envs, nil
}

func buildSwarmNodeAgentNameInternal(hostname, nodeID string) string {
	trimmedHostname := strings.TrimSpace(hostname)
	if trimmedHostname != "" {
		return "Swarm Node Agent - " + trimmedHostname
	}
	if len(nodeID) > 12 {
		nodeID = nodeID[:12]
	}
	return "Swarm Node Agent - " + nodeID
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

	apiKeyDto, err := s.apiKeyService.CreateEnvironmentApiKey(ctx, env.ID, userID)
	if err != nil {
		return "", fmt.Errorf("failed to create environment API key: %w", err)
	}

	if err := s.RegenerateEnvironmentApiKey(ctx, env.ID, apiKeyDto.ID, apiKeyDto.Key, userID, username, env.Name); err != nil {
		return "", err
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
	err := s.db.WithContext(ctx).
		Where("hidden = ?", true).
		Where("parent_environment_id = ?", parentEnvironmentID).
		Where("swarm_node_id = ?", nodeID).
		First(&env).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", fmt.Errorf("failed to load swarm node agent environment: %w", err)
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		createdEnv := &models.Environment{
			Name:                buildSwarmNodeAgentNameInternal(hostname, nodeID),
			ApiUrl:              buildSwarmNodeAgentURLInternal(nodeID),
			Status:              string(models.EnvironmentStatusPending),
			Enabled:             true,
			IsEdge:              true,
			Hidden:              true,
			ParentEnvironmentID: new(parentEnvironmentID),
			SwarmNodeID:         new(nodeID),
		}

		if _, createErr := s.CreateEnvironment(ctx, createdEnv, new(userID), new(username)); createErr != nil {
			return nil, "", fmt.Errorf("failed to create swarm node agent environment: %w", createErr)
		}
		env = *createdEnv
	} else {
		updates := map[string]any{}
		expectedName := buildSwarmNodeAgentNameInternal(hostname, nodeID)
		if env.Name != expectedName {
			updates["name"] = expectedName
		}
		if !env.Hidden {
			updates["hidden"] = true
		}
		if !env.IsEdge {
			updates["is_edge"] = true
		}
		if !env.Enabled {
			updates["enabled"] = true
		}
		if env.ParentEnvironmentID == nil || *env.ParentEnvironmentID != parentEnvironmentID {
			updates["parent_environment_id"] = parentEnvironmentID
		}
		if env.SwarmNodeID == nil || *env.SwarmNodeID != nodeID {
			updates["swarm_node_id"] = nodeID
		}
		if len(updates) > 0 {
			updatedEnv, updateErr := s.UpdateEnvironment(ctx, env.ID, updates, new(userID), new(username))
			if updateErr != nil {
				return nil, "", fmt.Errorf("failed to update swarm node agent environment: %w", updateErr)
			}
			env = *updatedEnv
		}
	}

	apiKey, err := s.applySwarmNodeAgentApiKeyInternal(ctx, &env, userID, username, rotate)
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

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", envID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update swarm node identity: %w", err)
	}

	return nil
}

func (s *EnvironmentService) GetEnvironmentByID(ctx context.Context, id string) (*models.Environment, error) {
	var environment models.Environment
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&environment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("environment not found")
		}
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}
	return &environment, nil
}

func (s *EnvironmentService) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	if strings.TrimSpace(params.Filters["type"]) != "" {
		return s.listEnvironmentsPaginatedWithRuntimeFiltersInternal(ctx, params, accessibleEnvIDs)
	}

	var envs []models.Environment
	q := s.db.WithContext(ctx).Model(&models.Environment{}).Where("hidden = ?", false)
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

	out, mapErr := mapper.MapSlice[models.Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	return out, paginationResp, nil
}

// filterEnvironmentsByIDInternal returns only the environments whose ID is in
// allowedIDs. A non-nil but empty allowedIDs yields an empty slice. Used to
// restrict the runtime-filtered list path to a caller's accessible environments.
func filterEnvironmentsByIDInternal(items []environment.Environment, allowedIDs []string) []environment.Environment {
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	out := make([]environment.Environment, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func (s *EnvironmentService) listEnvironmentsPaginatedWithRuntimeFiltersInternal(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	var envs []models.Environment
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("hidden = ?", false).
		Find(&envs).Error; err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list environments: %w", err)
	}

	items, mapErr := mapper.MapSlice[models.Environment, environment.Environment](envs)
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
					if a.Enabled {
						return 1
					}
					return -1
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
					switch strings.ToLower(strings.TrimSpace(filterValue)) {
					case "true", "1":
						return item.Enabled
					case "false", "0":
						return !item.Enabled
					default:
						return true
					}
				},
			},
			{
				Key: "type",
				Fn:  environmentTypeMatchesInternal,
			},
		},
	}

	result := pagination.SearchOrderAndPaginate(items, params, config)
	paginationResp := pagination.BuildResponseFromFilterResult(result, params)

	return result.Items, paginationResp, nil
}

func environmentTypeMatchesInternal(env environment.Environment, filterValue string) bool {
	return environmentTypeKeyInternal(env) == strings.ToLower(strings.TrimSpace(filterValue))
}

func environmentTypeKeyInternal(env environment.Environment) string {
	if !env.IsEdge {
		return "http"
	}
	transport := ""
	if env.Connected != nil && *env.Connected && env.EdgeTransport != nil {
		transport = *env.EdgeTransport
	} else if env.LastEdgeTransport != nil {
		// Disconnected or poll-only agents classify by the transport they
		// last used rather than collapsing into the generic edge bucket.
		transport = *env.LastEdgeTransport
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case edge.EdgeTransportWebSocket:
		return "websocket"
	case edge.EdgeTransportGRPC:
		return "grpc"
	default:
		return "edge"
	}
}

func (s *EnvironmentService) ListVisibleEnvironments(ctx context.Context) ([]environment.Environment, error) {
	var envs []models.Environment
	if err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("hidden = ?", false).
		Order("created_at asc, id asc").
		Find(&envs).Error; err != nil {
		return nil, fmt.Errorf("failed to list visible environments: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	for i := range out {
		ApplyEnvironmentRuntimeState(&out[i])
	}

	return out, nil
}

// ListRemoteEnvironmentIDs returns the IDs of enabled remote environments; it
// satisfies aggstream.RemoteEnvironmentLister for aggregated stream handlers.
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
func (s *EnvironmentService) ListRemoteEnvironments(ctx context.Context) ([]models.Environment, error) {
	var envs []models.Environment
	err := s.db.WithContext(ctx).
		Model(&models.Environment{}).
		Where("id != ?", "0").
		Where("enabled = ?", true).
		Where("hidden = ?", false).
		Find(&envs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list remote environments: %w", err)
	}
	s.syncRemoteEnvironmentSnapshotsInternal(envs)
	return envs, nil
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

		if err := s.SyncRegistriesToEnvironment(ctx, env.ID); err != nil {
			failedCount++
			slog.WarnContext(ctx, "Failed to sync registries to remote environment",
				"environmentID", env.ID,
				"environmentName", env.Name,
				"error", err.Error())
		}
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to sync registries to %d remote environment(s)", failedCount)
	}

	return nil
}

func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, id string, updates map[string]any, userID, username *string) (*models.Environment, error) {
	updates["updated_at"] = new(time.Now())

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update environment: %w", err)
	}

	updated, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.cacheRemoteEnvironmentSnapshotInternal(*updated)

	if rawAccessToken, ok := updates["access_token"]; ok {
		accessToken, _ := rawAccessToken.(string)
		s.syncEnvironmentTokenCacheInternal(id, accessToken)
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
		go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, updated.Name, models.EventTypeEnvironmentUpdate, "Environment Updated", fmt.Sprintf("Environment '%s' was updated", updated.Name), models.EventSeverityInfo, userID, username)
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

	if err := s.db.WithContext(ctx).Delete(&models.Environment{}, "id = ?", id).Error; err != nil {
		if env.Enabled {
			s.registerHealthJobInternal(ctx, env.ID)
		}
		return fmt.Errorf("failed to delete environment: %w", err)
	}

	s.invalidateEnvironmentTokenInternal(id)
	s.removeRemoteEnvironmentSnapshotInternal(id)

	// Create event in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), id, env.Name, models.EventTypeEnvironmentDelete, "Environment Deleted", fmt.Sprintf("Environment '%s' was deleted", env.Name), models.EventSeverityWarning, userID, username)

	return nil
}

func (s *EnvironmentService) TestConnection(ctx context.Context, id string, customApiUrl *string) (string, error) {
	environment, err := s.GetEnvironmentByID(ctx, id)
	if err != nil {
		return "error", err
	}

	// Special handling for local Docker environment (ID "0")
	if id == "0" && customApiUrl == nil {
		return s.testLocalDockerConnection(ctx, id)
	}

	// For edge environments, check if there's an active tunnel and route through it
	if environment.IsEdge && customApiUrl == nil {
		return s.testEdgeConnection(ctx, id)
	}

	apiUrl := environment.ApiUrl
	if customApiUrl != nil && *customApiUrl != "" {
		apiUrl = *customApiUrl
	}

	healthURL, err := buildEnvironmentEndpointURLInternal(apiUrl, "/api/health")
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return "offline", fmt.Errorf("invalid environment API URL: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, healthURL, nil)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return "offline", fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		}
		return "offline", fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		if customApiUrl == nil {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
		}
		return "online", nil
	}

	if customApiUrl == nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusError))
	}
	return "error", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

// testEdgeConnection tests connection to an edge agent via its tunnel
func (s *EnvironmentService) testEdgeConnection(ctx context.Context, id string) (string, error) {
	if !edge.HasActiveTunnel(id) {
		if _, ok := edge.RequestTunnelAndWait(ctx, id, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()); !ok {
			_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
			return "offline", errors.New("edge agent is not connected")
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	statusCode, _, err := edge.DoRequest(reqCtx, id, http.MethodGet, "/api/health", nil)
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", fmt.Errorf("health check via tunnel failed: %w", err)
	}

	if statusCode == http.StatusOK {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
		return "online", nil
	}

	_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusError))
	return "error", fmt.Errorf("unexpected status code: %d", statusCode)
}

func (s *EnvironmentService) testLocalDockerConnection(ctx context.Context, id string) (string, error) {
	// Test local Docker socket by pinging Docker
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, err = dockerClient.Ping(reqCtx, client.PingOptions{})
	if err != nil {
		_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOffline))
		return "offline", fmt.Errorf("docker ping failed: %w", err)
	}

	_ = s.updateEnvironmentStatusInternal(ctx, id, string(models.EnvironmentStatusOnline))
	return "online", nil
}

func (s *EnvironmentService) updateEnvironmentStatusInternal(ctx context.Context, id, status string) error {
	var currentEnv models.Environment
	if err := s.db.WithContext(ctx).Select("status", "is_edge").Where("id = ?", id).First(&currentEnv).Error; err != nil {
		return fmt.Errorf("failed to check environment status: %w", err)
	}

	if currentEnv.Status == string(models.EnvironmentStatusPending) {
		// Edge envs must complete pairing via the agent's outbound tunnel — manager
		// can't dial them directly, so a manager-side reachability check means nothing.
		// Direct envs are reachable from the manager, so a successful health check IS
		// the pairing signal. Don't promote on offline/error ticks though, or a transient
		// blip during initial setup would flip the env out of pending.
		if currentEnv.IsEdge || status != string(models.EnvironmentStatusOnline) {
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
	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment status: %w", err)
	}
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
	`, new(now), string(models.EnvironmentStatusOnline), new(now), id, now.Add(-30*time.Second))

	if result.Error != nil {
		return fmt.Errorf("failed to update environment heartbeat: %w", result.Error)
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
		updates["status"] = string(models.EnvironmentStatusOnline)
		updates["last_seen"] = &now
		// Remember the tunnel transport so the UI can keep showing it after
		// the tunnel drops or while the agent is poll-only.
		if state, ok := edge.GetTunnelRuntimeState(id); ok && state.Transport != "" {
			updates["last_edge_transport"] = state.Transport
		}
	} else {
		updates["status"] = string(models.EnvironmentStatusOffline)
	}

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment connection state: %w", err)
	}

	return nil
}

// ReconcileEdgeStatusesOnStartup resets edge environments to offline when the manager starts.
// Live edge tunnels are process-local runtime state, so persisted "online" flags can be stale
// after a restart until agents reconnect. Pending environments are left untouched.
func (s *EnvironmentService) ReconcileEdgeStatusesOnStartup(ctx context.Context) error {
	result := s.db.WithContext(ctx).Model(&models.Environment{}).
		Where("is_edge = ?", true).
		Where("status <> ?", string(models.EnvironmentStatusPending)).
		Where("status <> ?", string(models.EnvironmentStatusOffline)).
		Updates(map[string]any{
			"status":     string(models.EnvironmentStatusOffline),
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

func (s *EnvironmentService) createEnvironmentEvent(ctx context.Context, envID, envName string, eventType models.EventType, title, description string, severity models.EventSeverity, userID, username *string) {
	if s == nil || s.eventService == nil {
		return
	}

	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
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

func (s *EnvironmentService) RegenerateEnvironmentApiKey(ctx context.Context, envID string, newApiKeyID string, encryptedKey string, userID, username string, envName string) error {
	// Trim once at the boundary so the value persisted, the value cached,
	// and the value returned by callers (which already TrimSpace before
	// returning) all stay byte-identical. Any divergence here would surface
	// as a 401 "invalid agent token" because lookup is direct equality.
	encryptedKey = strings.TrimSpace(encryptedKey)

	updates := map[string]any{
		"api_key_id":   newApiKeyID,
		"access_token": encryptedKey,
		"status":       string(models.EnvironmentStatusPending),
		"last_seen":    nil, // Clear last seen time
	}

	if err := s.db.WithContext(ctx).Model(&models.Environment{}).Where("id = ?", envID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update environment with new API key: %w", err)
	}

	s.syncEnvironmentTokenCacheInternal(envID, encryptedKey)
	now := time.Now()
	s.updateRemoteEnvironmentSnapshotInternal(envID, func(environment *models.Environment) {
		environment.ApiKeyID = &newApiKeyID
		environment.AccessToken = &encryptedKey
		environment.Status = string(models.EnvironmentStatusPending)
		environment.LastSeen = nil
		environment.UpdatedAt = &now
	})

	// Create event log in background
	go s.createEnvironmentEvent(context.WithoutCancel(ctx), envID, envName, models.EventTypeEnvironmentApiKeyRegenerated, "API Key Regenerated", "Environment API key was regenerated and status set to pending", models.EventSeverityInfo, new(userID), new(username))

	return nil
}

func (s *EnvironmentService) GetDB() *database.DB {
	return s.db
}

func (s *EnvironmentService) ResolveEnvironmentByAccessToken(ctx context.Context, token string) (*models.Environment, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrEnvironmentAccessTokenRequired
	}

	var env models.Environment
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
	var registries []models.ContainerRegistry
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

// DeploymentSnippets contains deployment configuration snippets for an environment.
type DeploymentSnippets struct {
	DockerRun     string
	DockerCompose string
	MTLS          *DeploymentSnippetMTLS
}

type DeploymentSnippetFile struct {
	Name          string
	Content       string
	ContainerPath string
	Permissions   string
}

type DeploymentSnippetMTLS struct {
	DockerRun     string
	DockerCompose string
	Files         []DeploymentSnippetFile
	HostDirHint   string
}

const (
	deploymentSnippetsDataPath = "/app/data"
	deploymentSnippetsMTLSPath = "/app/data/edge-mtls-agent"
)

// GenerateDeploymentSnippets generates Docker deployment snippets for an environment.
func (s *EnvironmentService) GenerateDeploymentSnippets(ctx context.Context, envID string, envAddress string, apiKey string) (*DeploymentSnippets, error) {
	managerURL := strings.TrimRight(envAddress, "/")

	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-agent \\",
		"  --restart unless-stopped \\",
		"  -e AGENT_MODE=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -p 3553:3553 \\",
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/arcane-headless:latest",
	}, "\n")

	dockerCompose := strings.Join([]string{
		"services:",
		"  arcane-agent:",
		"    image: ghcr.io/getarcaneapp/arcane-headless:latest",
		"    container_name: arcane-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - AGENT_MODE=true",
		"      - EDGE_TRANSPORT=poll",
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    ports:",
		"      - \"3553:3553\"",
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	return &DeploymentSnippets{
		DockerRun:     dockerRun,
		DockerCompose: dockerCompose,
	}, nil
}

// GenerateEdgeDeploymentSnippets generates Docker deployment snippets for an edge agent.
// Edge agents connect outbound to the manager and don't require exposed ports.
func (s *EnvironmentService) GenerateEdgeDeploymentSnippets(ctx context.Context, envID string, managerURL string, apiKey string, edgeCfg *edge.Config) (*DeploymentSnippets, error) {
	managerURL = strings.TrimRight(managerURL, "/")

	dockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/arcane-headless:latest",
	}, "\n")

	dockerCompose := strings.Join([]string{
		"# Edge agent - connects outbound, no exposed ports required",
		"services:",
		"  arcane-edge-agent:",
		"    image: ghcr.io/getarcaneapp/arcane-headless:latest",
		"    container_name: arcane-edge-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - EDGE_AGENT=true",
		"      - EDGE_TRANSPORT=poll",
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	snippets := &DeploymentSnippets{
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

	generatedAssets, err := edge.GenerateManagerClientMTLSAssetsWithContext(ctx, edgeCfg, envID, envName)
	if err != nil {
		slog.WarnContext(ctx, "Failed to generate edge mTLS assets; returning basic snippets only", "environment_id", envID, "error", err)
		return snippets, nil
	}
	if generatedAssets == nil {
		return snippets, nil
	}
	s.logGeneratedMTLSEventsInternal(ctx, envID, envName, generatedAssets)

	snippets.MTLS = buildMTLSDeploymentSnippetInternal(managerURL, apiKey, generatedAssets)
	return snippets, nil
}

func (s *EnvironmentService) logGeneratedMTLSEventsInternal(ctx context.Context, envID string, envName string, assets *edge.GeneratedMTLSAssets) {
	if s == nil || s.eventService == nil || assets == nil {
		return
	}
	if assets.CAGenerated {
		if _, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:        models.EventTypeEnvironmentMTLSCAGenerated,
			Severity:    models.EventSeverityInfo,
			Title:       "Edge mTLS CA generated",
			Description: "Arcane generated a new edge mTLS certificate authority",
			Metadata:    models.JSON{"kind": "ca"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS CA generation event", "error", err)
		}
	}
	if assets.CertIssued {
		envIDCopy := envID
		if _, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeEnvironmentMTLSCertIssued,
			Severity:      models.EventSeverityInfo,
			Title:         "Edge mTLS certificate issued",
			Description:   fmt.Sprintf("Arcane issued an edge mTLS client certificate for environment '%s'", envName),
			ResourceType:  new("environment"),
			ResourceID:    &envIDCopy,
			ResourceName:  new(envName),
			EnvironmentID: &envIDCopy,
			Metadata:      models.JSON{"kind": "client"},
		}); err != nil {
			slog.WarnContext(ctx, "Failed to create edge mTLS certificate issuance event", "environment_id", envID, "error", err)
		}
	}
}

func buildMTLSDeploymentSnippetInternal(managerURL string, apiKey string, generatedAssets *edge.GeneratedMTLSAssets) *DeploymentSnippetMTLS {
	if generatedAssets == nil {
		return nil
	}

	mtlsDockerRun := strings.Join([]string{
		"docker run -d \\",
		"  --name arcane-edge-agent \\",
		"  --restart unless-stopped \\",
		"  -e EDGE_AGENT=true \\",
		"  -e EDGE_TRANSPORT=poll \\",
		"  -e EDGE_MTLS_MODE=required \\",
		fmt.Sprintf("  -e EDGE_MTLS_ASSETS_DIR=%s \\", deploymentSnippetsMTLSPath),
		fmt.Sprintf("  -e AGENT_TOKEN=%s \\", apiKey),
		fmt.Sprintf("  -e MANAGER_API_URL=%s \\", managerURL),
		"  -v /var/run/docker.sock:/var/run/docker.sock \\",
		fmt.Sprintf("  -v arcane-data:%s \\", deploymentSnippetsDataPath),
		"  ghcr.io/getarcaneapp/arcane-headless:latest",
	}, "\n")

	mtlsDockerCompose := strings.Join([]string{
		"# Edge agent with automatic mTLS enrollment",
		"services:",
		"  arcane-edge-agent:",
		"    image: ghcr.io/getarcaneapp/arcane-headless:latest",
		"    container_name: arcane-edge-agent",
		"    restart: unless-stopped",
		"    environment:",
		"      - EDGE_AGENT=true",
		"      - EDGE_TRANSPORT=poll",
		"      - EDGE_MTLS_MODE=required",
		"      - EDGE_MTLS_ASSETS_DIR=" + deploymentSnippetsMTLSPath,
		"      - AGENT_TOKEN=" + apiKey,
		"      - MANAGER_API_URL=" + managerURL,
		"    volumes:",
		"      - /var/run/docker.sock:/var/run/docker.sock",
		"      - arcane-data:" + deploymentSnippetsDataPath,
		"",
		"volumes:",
		"  arcane-data:",
	}, "\n")

	files := make([]DeploymentSnippetFile, 0, len(generatedAssets.Files))
	for _, file := range generatedAssets.Files {
		files = append(files, DeploymentSnippetFile{
			Name:          file.Name,
			Content:       file.Content,
			ContainerPath: file.ContainerPath,
			Permissions:   file.Permissions,
		})
	}

	return &DeploymentSnippetMTLS{
		DockerRun:     mtlsDockerRun,
		DockerCompose: mtlsDockerCompose,
		Files:         files,
		HostDirHint:   strings.TrimSpace(generatedAssets.HostDirHint),
	}
}

type remoteEnvironmentTargetInternal struct {
	ID          string
	Name        string
	IsEdge      bool
	AccessToken *string
	TargetURL   string
}

func (s *EnvironmentService) resolveRemoteEnvironmentTargetInternal(ctx context.Context, envID string) (*remoteEnvironmentTargetInternal, error) {
	environment, err := s.GetEnvironmentByID(ctx, envID)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return s.remoteEnvironmentTargetFromModelInternal(*environment)
}

func (s *EnvironmentService) remoteEnvironmentTargetFromModelInternal(environment models.Environment) (*remoteEnvironmentTargetInternal, error) {
	if environment.ID == "0" {
		return nil, errors.New("cannot proxy request to local environment")
	}

	targetURL := strings.TrimRight(environment.ApiUrl, "/")
	if !environment.IsEdge {
		validatedTargetURL, err := normalizeEnvironmentBaseURLInternal(environment.ApiUrl)
		if err != nil {
			return nil, fmt.Errorf("invalid environment API URL: %w", err)
		}
		targetURL = validatedTargetURL
	}

	return &remoteEnvironmentTargetInternal{
		ID:          environment.ID,
		Name:        environment.Name,
		IsEdge:      environment.IsEdge,
		AccessToken: environment.AccessToken,
		TargetURL:   targetURL,
	}, nil
}

func normalizeEnvironmentBaseURLInternal(apiURL string) (string, error) {
	parsed, err := httputils.ValidateOutboundHTTPURL(apiURL)
	if err != nil {
		return "", err
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func buildEnvironmentEndpointURLInternal(apiURL, endpointPath string) (string, error) {
	baseURL, err := normalizeEnvironmentBaseURLInternal(apiURL)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(baseURL, "/") + endpointPath, nil
}

func (s *EnvironmentService) getProxyRequestContextInternal(ctx context.Context) (context.Context, context.CancelFunc) {
	if s != nil && s.settingsService != nil {
		settings := s.settingsService.GetSettingsConfig()
		return timeouts.WithTimeout(ctx, settings.ProxyRequestTimeout.AsInt(), timeouts.DefaultProxyRequest)
	}

	return context.WithTimeout(ctx, timeouts.DefaultProxyRequest)
}

func (s *EnvironmentService) buildRemoteRequestInternal(
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
	headers map[string]string,
) (remenv.Request, error) {
	if target == nil {
		return remenv.Request{}, errors.New("remote environment target is required")
	}

	requestHeaders := make(map[string]string, len(headers)+2)
	maps.Copy(requestHeaders, headers)
	if len(body) > 0 && method != http.MethodGet && requestHeaders["Content-Type"] == "" {
		requestHeaders["Content-Type"] = "application/json"
	}
	remenv.ApplyAgentTokenHeaderMap(requestHeaders, target.AccessToken)

	return remenv.Request{
		EnvironmentID: target.ID,
		IsEdge:        target.IsEdge,
		Method:        method,
		URL:           target.TargetURL + path,
		Path:          path,
		Headers:       requestHeaders,
		Body:          body,
	}, nil
}

func (s *EnvironmentService) ExecuteRemoteRequest(ctx context.Context, envID string, method string, path string, body []byte) (*remenv.Response, error) {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, envID)
	if err != nil {
		return nil, err
	}

	return s.executeRemoteRequestForTargetInternal(ctx, target, method, path, body)
}

func (s *EnvironmentService) executeRemoteRequestForTargetInternal(
	ctx context.Context,
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
) (*remenv.Response, error) {
	request, err := s.buildRemoteRequestInternal(target, method, path, body, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.remoteClient.Do(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to environment %s: %w", target.Name, err)
	}

	return resp, nil
}

func (s *EnvironmentService) ProxyJSONRequest(ctx context.Context, envID string, method string, path string, body []byte, out any) error {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	target, err := s.resolveRemoteEnvironmentTargetInternal(proxyCtx, envID)
	if err != nil {
		return err
	}

	return s.proxyJSONRequestForTargetInternal(proxyCtx, target, method, path, body, out)
}

// ProxyJSONRequestForEnvironment sends a JSON request using an already-loaded
// environment row, avoiding an extra environment lookup on hot stream paths.
func (s *EnvironmentService) ProxyJSONRequestForEnvironment(ctx context.Context, environment models.Environment, method string, path string, body []byte, out any) error {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	target, err := s.remoteEnvironmentTargetFromModelInternal(environment)
	if err != nil {
		return err
	}

	return s.proxyJSONRequestForTargetInternal(proxyCtx, target, method, path, body, out)
}

func (s *EnvironmentService) proxyJSONRequestForTargetInternal(
	ctx context.Context,
	target *remoteEnvironmentTargetInternal,
	method string,
	path string,
	body []byte,
	out any,
) error {
	resp, err := s.executeRemoteRequestForTargetInternal(ctx, target, method, path, body)
	if err != nil {
		return err
	}
	if err := resp.RequireSuccess(); err != nil {
		return err
	}
	if err := resp.DecodeJSON(out); err != nil {
		return err
	}

	return nil
}

func ensureRemoteEnvironmentTunnelAvailableInternal(ctx context.Context, envID string) error {
	if edge.HasActiveTunnel(envID) {
		return nil
	}

	if _, ok := edge.RequestTunnelAndWait(ctx, envID, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()); ok {
		return nil
	}

	return errors.New("edge agent is not connected")
}

func doRemoteEnvironmentTunnelRequestInternal(
	ctx context.Context,
	envID string,
	method string,
	path string,
	headers map[string]string,
	body []byte,
) (*remenv.Response, error) {
	tunnel, ok := edge.GetRegistry().Get(envID)
	if !ok {
		return nil, fmt.Errorf("no active tunnel for environment %s", envID)
	}
	if tunnel.Conn.IsClosed() {
		return nil, fmt.Errorf("tunnel for environment %s is closed", envID)
	}

	statusCode, respHeaders, respBody, err := edge.ProxyRequest(ctx, tunnel, method, path, "", headers, body)
	if err != nil {
		return nil, fmt.Errorf("tunnel request failed: %w", err)
	}

	return &remenv.Response{
		StatusCode: statusCode,
		Body:       respBody,
		Headers:    respHeaders,
	}, nil
}

// SyncRegistriesToEnvironment syncs all registries from this manager to a remote environment
func (s *EnvironmentService) SyncRegistriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Starting registry sync to environment", "environmentID", environmentID, "environmentName", target.Name, "apiUrl", target.TargetURL)

	// Get all registries from this manager
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return fmt.Errorf("failed to get registries: %w", err)
	}

	slog.InfoContext(ctx, "Found registries to sync", "count", len(registries))

	// Prepare sync items with decrypted credentials
	syncItems := make([]containerregistry.Sync, 0, len(registries))
	for _, reg := range registries {
		registryType, typeErr := normalizeRegistryTypeInternal(reg.RegistryType)
		if typeErr != nil {
			return fmt.Errorf("normalize registry type for sync %s: %w", reg.ID, typeErr)
		}

		syncItem := containerregistry.Sync{
			ID:           reg.ID,
			URL:          reg.URL,
			Description:  reg.Description,
			Insecure:     reg.Insecure,
			Enabled:      reg.Enabled,
			RegistryType: registryType,
			CreatedAt:    reg.CreatedAt,
			UpdatedAt:    reg.UpdatedAt,
		}

		if registryType == registryTypeECR {
			decryptedSecret, err := crypto.Decrypt(reg.AWSSecretAccessKey)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt ECR secret for sync", "registryID", reg.ID, "registryURL", reg.URL, "error", err.Error())
				continue
			}

			syncItem.AWSAccessKeyID = reg.AWSAccessKeyID
			syncItem.AWSSecretAccessKey = decryptedSecret
			syncItem.AWSRegion = reg.AWSRegion
		} else {
			decryptedToken, err := crypto.Decrypt(reg.Token)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt registry token for sync", "registryID", reg.ID, "registryURL", reg.URL, "error", err.Error())
				continue
			}

			syncItem.Username = reg.Username
			syncItem.Token = decryptedToken
		}

		syncItems = append(syncItems, syncItem)
	}

	// Prepare the sync request
	syncReq := containerregistry.SyncRequest{
		Registries: syncItems,
	}

	// Marshal the request
	reqBody, err := json.Marshal(syncReq)
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	slog.InfoContext(ctx, "Sending sync request to agent", "url", target.TargetURL+"/api/container-registries/sync", "registryCount", len(syncItems), "isEdge", target.IsEdge)

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := s.proxyJSONRequestForTargetInternal(reqCtx, target, http.MethodPost, "/api/container-registries/sync", reqBody, &result); err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("sync failed: %s", result.Data.Message)
	}

	slog.InfoContext(ctx, "Successfully synced registries to environment", "environmentID", environmentID, "environmentName", target.Name)

	return nil
}

// SyncRepositoriesToEnvironment syncs all git repositories from this manager to a remote environment
func (s *EnvironmentService) SyncRepositoriesToEnvironment(ctx context.Context, environmentID string) error {
	target, err := s.resolveRemoteEnvironmentTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "Starting git repository sync to environment", "environmentID", environmentID, "environmentName", target.Name, "apiUrl", target.TargetURL)

	// Get all git repositories from this manager
	var repositories []models.GitRepository
	if err := s.db.WithContext(ctx).Find(&repositories).Error; err != nil {
		return fmt.Errorf("failed to get git repositories: %w", err)
	}

	slog.InfoContext(ctx, "Found git repositories to sync", "count", len(repositories))

	// Prepare sync items with decrypted credentials
	syncItems := make([]gitops.RepositorySync, 0, len(repositories))
	for _, repo := range repositories {
		item := gitops.RepositorySync{
			ID:          repo.ID,
			Name:        repo.Name,
			URL:         repo.URL,
			AuthType:    repo.AuthType,
			Username:    repo.Username,
			Description: repo.Description,
			Enabled:     repo.Enabled,
			CreatedAt:   repo.CreatedAt,
		}
		if repo.UpdatedAt != nil {
			item.UpdatedAt = *repo.UpdatedAt
		}

		// Decrypt token if present
		if repo.Token != "" {
			decryptedToken, err := crypto.Decrypt(repo.Token)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt repository token for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
				continue
			}
			item.Token = decryptedToken
		}

		// Decrypt SSH key if present
		if repo.SSHKey != "" {
			decryptedSSHKey, err := crypto.Decrypt(repo.SSHKey)
			if err != nil {
				slog.WarnContext(ctx, "Failed to decrypt repository SSH key for sync", "repositoryID", repo.ID, "repositoryName", repo.Name, "error", err.Error())
				continue
			}
			item.SSHKey = decryptedSSHKey
		}

		syncItems = append(syncItems, item)
	}

	// Prepare the sync request
	syncReq := gitops.RepositorySyncRequest{
		Repositories: syncItems,
	}

	// Marshal the request
	reqBody, err := json.Marshal(syncReq)
	if err != nil {
		return fmt.Errorf("failed to marshal sync request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	slog.InfoContext(ctx, "Sending git repository sync request to agent", "url", target.TargetURL+"/api/git-repositories/sync", "repositoryCount", len(syncItems), "isEdge", target.IsEdge)

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := s.proxyJSONRequestForTargetInternal(reqCtx, target, http.MethodPost, "/api/git-repositories/sync", reqBody, &result); err != nil {
		return fmt.Errorf("failed to send sync request: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("sync failed: %s", result.Data.Message)
	}

	slog.InfoContext(ctx, "Successfully synced git repositories to environment", "environmentID", environmentID, "environmentName", target.Name)

	return nil
}

// ProxyRequest sends a request to a remote environment's API.
func (s *EnvironmentService) ProxyRequest(ctx context.Context, envID string, method string, path string, body []byte) ([]byte, int, error) {
	proxyCtx, cancel := s.getProxyRequestContextInternal(ctx)
	defer cancel()

	resp, err := s.ExecuteRemoteRequest(proxyCtx, envID, method, path, body)
	if err != nil {
		return nil, 0, err
	}

	return resp.Body, resp.StatusCode, nil
}
