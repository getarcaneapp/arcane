package webhook

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

const (
	webhookTokenPrefix    = "arc_wh_"
	webhookTokenLength    = 32 // raw bytes → 64 hex chars
	webhookTokenPrefixLen = 8  // chars of the hex portion used as lookup prefix
	// hex length of a generated token's ciphertext: GCM nonce (12) + encrypted secretHex + GCM tag (16)
	webhookTokenHexLen = 2 * (12 + webhookTokenLength*2 + 16)
)

var (
	ErrWebhookNotFound      = common.Classify(common.ErrNotFound, errors.New("webhook not found"))
	ErrWebhookInvalid       = errors.New("invalid webhook token")
	ErrWebhookDisabled      = errors.New("webhook is disabled")
	ErrWebhookInvalidType   = common.Classify(common.ErrValidation, errors.New("invalid target type, must be 'container', 'project', 'updater', or 'gitops'"))
	ErrWebhookInvalidAction = common.Classify(common.ErrValidation, errors.New("invalid action type for target type"))
	ErrWebhookMissingName   = common.Classify(common.ErrValidation, errors.New("webhook name is required"))
	ErrWebhookMissingTarget = common.Classify(common.ErrValidation, errors.New("target ID is required for container, project, and gitops webhook types"))
	ErrWebhookForbidden     = common.Classify(common.ErrForbidden, errors.New("you do not have permission to run this webhook action"))

	// remoteWebhookTargetListPaths lists agent endpoints for target types whose records live in the agent's database.
	remoteWebhookTargetListPaths = map[string]string{
		WebhookTargetTypeProject: "/api/environments/" + types.LocalDockerEnvironmentID + "/projects/references",
		WebhookTargetTypeGitOps:  "/api/environments/" + types.LocalDockerEnvironmentID + "/gitops/syncs?limit=-1",
	}
)

type remoteWebhookTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type WebhookService struct {
	tokenWriteMu sync.Mutex
	tokenMu      sync.RWMutex
	tokenHashes  map[string]struct{}

	// actions tracks in-flight background webhook actions accepted with a 202
	// so shutdown can drain them instead of dropping acknowledged work.
	actions            sync.WaitGroup
	db                 *database.DB
	containerService   *container.ContainerService
	updaterService     *updater.UpdaterService
	projectService     *project.ProjectService
	gitOpsSyncService  *gitops.GitOpsSyncService
	eventService       *event.EventService
	environmentService *environment.EnvironmentService
}

func NewWebhookService(
	db *database.DB,
	containerService *container.ContainerService,
	updaterService *updater.UpdaterService,
	projectService *project.ProjectService,
	gitOpsSyncService *gitops.GitOpsSyncService,
	eventService *event.EventService,
	environmentService *environment.EnvironmentService,
) *WebhookService {
	return &WebhookService{
		db:                 db,
		containerService:   containerService,
		updaterService:     updaterService,
		projectService:     projectService,
		gitOpsSyncService:  gitOpsSyncService,
		eventService:       eventService,
		environmentService: environmentService,
	}
}

// isRemoteWebhookEnvironment reports whether environmentID refers to a
// remote environment (anything but the local Docker environment).
func isRemoteWebhookEnvironment(environmentID string) bool {
	return environmentID != "" && environmentID != types.LocalDockerEnvironmentID
}

// generateWebhookToken creates a new random webhook token and returns the raw token
// (to be shown to the user once), its SHA-256 hash, and the lookup prefix.
func generateWebhookToken() (raw, hash, prefix string, err error) {
	b := make([]byte, webhookTokenLength)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate webhook token: %w", err)
	}
	secretHex := hex.EncodeToString(b)
	encrypted, err := crypto.Encrypt(secretHex)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encrypt webhook token: %w", err)
	}
	encryptedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decode encrypted webhook token: %w", err)
	}
	tokenHex := hex.EncodeToString(encryptedBytes)
	raw = webhookTokenPrefix + tokenHex
	hash = kit.SHA256Hex(raw)
	prefix = webhookTokenPrefix + tokenHex[:webhookTokenPrefixLen]
	return raw, hash, prefix, nil
}

func parseWebhookPrefix(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	hexPart, ok := strings.CutPrefix(raw, webhookTokenPrefix)
	if !ok || len(hexPart) < webhookTokenPrefixLen {
		return "", ErrWebhookInvalid
	}
	return webhookTokenPrefix + hexPart[:webhookTokenPrefixLen], nil
}

// LoadTokenHashes loads the rate-limit index before the server accepts requests.
func (s *WebhookService) LoadTokenHashes(ctx context.Context) error {
	s.tokenWriteMu.Lock()
	defer s.tokenWriteMu.Unlock()

	var hashes []string
	if err := s.db.WithContext(ctx).Model(&Webhook{}).Pluck("token_hash", &hashes).Error; err != nil {
		return fmt.Errorf("failed to load webhook token hashes: %w", err)
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.tokenHashes = make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		s.tokenHashes[hash] = struct{}{}
	}
	return nil
}

// IsKnownToken checks stored token identity without decrypting or querying the database.
// TriggerByToken still checks existence and enabled state before accepting an action.
func (s *WebhookService) IsKnownToken(raw string) bool {
	if s == nil || len(raw) > len(webhookTokenPrefix)+webhookTokenHexLen || !strings.HasPrefix(raw, webhookTokenPrefix) {
		return false
	}
	hash := kit.SHA256Hex(raw)
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	_, ok := s.tokenHashes[hash]
	return ok
}

func defaultWebhookActionType(targetType string) (string, error) {
	switch targetType {
	case WebhookTargetTypeContainer, WebhookTargetTypeProject:
		return WebhookActionTypeUpdate, nil
	case WebhookTargetTypeUpdater:
		return WebhookActionTypeRun, nil
	case WebhookTargetTypeGitOps:
		return WebhookActionTypeSync, nil
	default:
		return "", ErrWebhookInvalidType
	}
}

func resolveWebhookActionType(targetType, actionType string) (string, error) {
	switch targetType {
	case WebhookTargetTypeContainer, WebhookTargetTypeProject, WebhookTargetTypeUpdater, WebhookTargetTypeGitOps:
	default:
		return "", ErrWebhookInvalidType
	}

	normalizedActionType := strings.TrimSpace(strings.ToLower(actionType))
	if normalizedActionType == "" {
		return defaultWebhookActionType(targetType)
	}

	if targetType == WebhookTargetTypeProject && normalizedActionType == "deploy" {
		normalizedActionType = WebhookActionTypeUp
	}

	switch targetType {
	case WebhookTargetTypeContainer:
		switch normalizedActionType {
		case WebhookActionTypeUpdate, WebhookActionTypeStart, WebhookActionTypeStop, WebhookActionTypeRestart, WebhookActionTypeRedeploy:
			return normalizedActionType, nil
		}
	case WebhookTargetTypeProject:
		switch normalizedActionType {
		case WebhookActionTypeUpdate, WebhookActionTypeUp, WebhookActionTypeDown, WebhookActionTypeRestart, WebhookActionTypeRedeploy:
			return normalizedActionType, nil
		}
	case WebhookTargetTypeUpdater:
		if normalizedActionType == WebhookActionTypeRun {
			return normalizedActionType, nil
		}
	case WebhookTargetTypeGitOps:
		if normalizedActionType == WebhookActionTypeSync {
			return normalizedActionType, nil
		}
	}

	return "", ErrWebhookInvalidAction
}

var webhookActionPermissions = map[string]map[string]string{
	WebhookTargetTypeContainer: {
		WebhookActionTypeUpdate:   authz.PermImageUpdatesCheck,
		WebhookActionTypeStart:    authz.PermContainersStart,
		WebhookActionTypeStop:     authz.PermContainersStop,
		WebhookActionTypeRestart:  authz.PermContainersRestart,
		WebhookActionTypeRedeploy: authz.PermContainersRedeploy,
	},
	WebhookTargetTypeProject: {
		WebhookActionTypeUpdate:   authz.PermProjectsUpdate,
		WebhookActionTypeUp:       authz.PermProjectsDeploy,
		WebhookActionTypeDown:     authz.PermProjectsDown,
		WebhookActionTypeRestart:  authz.PermProjectsRestart,
		WebhookActionTypeRedeploy: authz.PermProjectsDeploy,
	},
	WebhookTargetTypeUpdater: {
		WebhookActionTypeRun: authz.PermImageUpdatesCheck,
	},
	WebhookTargetTypeGitOps: {
		WebhookActionTypeSync: authz.PermGitOpsSync,
	},
}

func (s *WebhookService) authorizeWebhookAction(ctx context.Context, callerPerms *authz.PermissionSet, targetType, actionType, targetID, environmentID string) error {
	permission, ok := webhookActionPermissions[targetType][actionType]
	if !ok || !callerPerms.Allows(permission, environmentID) {
		return ErrWebhookForbidden
	}
	if callerPerms.Scope(permission, environmentID).IsUnrestricted() {
		return nil
	}
	if !isRemoteWebhookEnvironment(environmentID) {
		scopedCtx := authz.WithRequestEnvironment(authz.WithPermissions(ctx, callerPerms), environmentID)
		switch targetType {
		case WebhookTargetTypeContainer:
			_, err := s.containerService.AuthorizeContainer(scopedCtx, targetID, permission)
			return err
		case WebhookTargetTypeProject:
			_, err := s.projectService.GetAuthorizedProject(scopedCtx, targetID, permission)
			return err
		}
	}
	middleware.RecordDenial(ctx, types.AuthzDenial{
		Permission:    permission,
		EnvironmentID: environmentID,
		RemoteAddr:    middleware.GetRemoteAddrFromContext(ctx),
	})
	return ErrWebhookForbidden
}

func resolvedWebhookActionType(targetType, actionType string) string {
	resolvedActionType, err := resolveWebhookActionType(targetType, actionType)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(actionType))
	}
	return resolvedActionType
}

// CreateWebhook creates a new webhook targeting a stack, the environment-wide updater, or a gitops sync.
// It returns the webhook record with the raw token populated (only available at creation time).
func (s *WebhookService) CreateWebhook(
	ctx context.Context,
	name, targetType, actionType, targetID, environmentID string,
	actor types.Actor,
	callerPerms *authz.PermissionSet,
) (*Webhook, string, error) {
	input := types.Webhook{Name: name, TargetType: targetType, ActionType: actionType, TargetID: targetID}
	if err := normalization.Normalize(&input); err != nil {
		return nil, "", err
	}
	if input.Name == "" {
		return nil, "", ErrWebhookMissingName
	}
	resolvedActionType, err := resolveWebhookActionType(targetType, actionType)
	if err != nil {
		return nil, "", err
	}

	targetRef := ""

	// The updater target type operates environment-wide and has no specific target resource.
	if targetType == WebhookTargetTypeUpdater {
		targetID = ""
	} else if strings.TrimSpace(targetID) == "" {
		return nil, "", ErrWebhookMissingTarget
	}
	if authorizeErr := s.authorizeWebhookAction(ctx, callerPerms, targetType, resolvedActionType, targetID, environmentID); authorizeErr != nil {
		return nil, "", authorizeErr
	}

	// Container references can only be resolved against the local Docker daemon;
	// remote-environment webhooks keep the raw target and resolve on trigger.
	if targetType == WebhookTargetTypeContainer && !isRemoteWebhookEnvironment(environmentID) {
		targetRef, err = s.resolveContainerWebhookTargetRef(ctx, targetID)
		if err != nil {
			return nil, "", err
		}
	}

	raw, hash, prefix, err := generateWebhookToken()
	if err != nil {
		return nil, "", err
	}

	wh := &Webhook{
		Name:          input.Name,
		TokenHash:     hash,
		TokenPrefix:   prefix,
		TargetType:    targetType,
		ActionType:    resolvedActionType,
		TargetID:      targetID,
		TargetRef:     targetRef,
		EnvironmentID: environmentID,
		Enabled:       true,
	}

	s.tokenWriteMu.Lock()
	defer s.tokenWriteMu.Unlock()
	if createWebhookErr := s.db.WithContext(ctx).Create(wh).Error; createWebhookErr != nil {
		return nil, "", fmt.Errorf("failed to create webhook: %w", createWebhookErr)
	}

	s.tokenMu.Lock()
	if s.tokenHashes == nil {
		s.tokenHashes = make(map[string]struct{})
	}
	s.tokenHashes[hash] = struct{}{}
	s.tokenMu.Unlock()

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeWebhookCreate,
			Severity:      event.EventSeveritySuccess,
			Title:         "Webhook created: " + wh.Name,
			Description:   fmt.Sprintf("Created webhook '%s' targeting %s (%s)", wh.Name, wh.TargetType, wh.ActionType),
			ResourceType:  new("webhook"),
			ResourceID:    &wh.ID,
			ResourceName:  &wh.Name,
			UserID:        &actor.ID,
			Username:      &actor.Username,
			EnvironmentID: &wh.EnvironmentID,
		})
	}

	return wh, raw, nil
}

// ListWebhooks returns all webhooks for an environment.
func (s *WebhookService) ListWebhooks(ctx context.Context, environmentID string) ([]Webhook, error) {
	var webhooks []Webhook
	if err := s.db.WithContext(ctx).
		Where("environment_id = ?", environmentID).
		Order("created_at DESC").
		Find(&webhooks).Error; err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}
	return webhooks, nil
}

func (s *WebhookService) ListWebhookSummaries(ctx context.Context, environmentID string) ([]types.Webhook, error) {
	webhooks, err := s.ListWebhooks(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	remoteNames := s.remoteWebhookTargetNames(ctx, environmentID, webhooks)
	summaries := make([]types.Webhook, len(webhooks))
	for i := range webhooks {
		wh := webhooks[i]
		targetName, ok := remoteNames[wh.TargetType][wh.TargetID]
		if !ok {
			targetName = s.resolveWebhookTargetName(ctx, &wh)
		}
		summaries[i] = types.Webhook{
			ID:              wh.ID,
			Name:            wh.Name,
			TokenPrefix:     wh.TokenPrefix,
			TargetType:      wh.TargetType,
			ActionType:      resolvedWebhookActionType(wh.TargetType, wh.ActionType),
			TargetID:        wh.TargetID,
			TargetName:      targetName,
			EnvironmentID:   wh.EnvironmentID,
			Enabled:         wh.Enabled,
			LastTriggeredAt: wh.LastTriggeredAt,
			CreatedAt:       wh.CreatedAt,
		}
	}

	return summaries, nil
}

// remoteWebhookTargetNames fetches target names keyed by target type and ID from a remote environment.
func (s *WebhookService) remoteWebhookTargetNames(ctx context.Context, environmentID string, webhooks []Webhook) map[string]map[string]string {
	names := map[string]map[string]string{}
	if !isRemoteWebhookEnvironment(environmentID) {
		return names
	}

	for targetType, path := range remoteWebhookTargetListPaths {
		if !slices.ContainsFunc(webhooks, func(wh Webhook) bool { return wh.TargetType == targetType }) {
			continue
		}
		var out types.ApiResponse[[]remoteWebhookTarget]
		if err := s.environmentService.ProxyJSONRequest(ctx, environmentID, http.MethodGet, path, nil, &out); err != nil {
			slog.WarnContext(ctx, "failed to resolve remote webhook target names", "environmentId", environmentID, "targetType", targetType, "error", err)
			continue
		}
		names[targetType] = make(map[string]string, len(out.Data))
		for _, target := range out.Data {
			names[targetType][target.ID] = target.Name
		}
	}
	return names
}

func (s *WebhookService) resolveWebhookTargetName(ctx context.Context, wh *Webhook) string {
	switch wh.TargetType {
	case WebhookTargetTypeContainer:
		// Remote containers cannot be resolved against the local Docker daemon.
		if isRemoteWebhookEnvironment(wh.EnvironmentID) {
			if strings.TrimSpace(wh.TargetRef) != "" {
				return wh.TargetRef
			}
			return wh.TargetID
		}
		if strings.TrimSpace(wh.TargetRef) != "" {
			if s.containerService == nil {
				return wh.TargetRef
			}
			name, err := s.containerService.GetContainerNameByReference(ctx, wh.TargetRef)
			if err == nil {
				return name
			}
			return wh.TargetRef
		}
		if s.containerService == nil {
			return ""
		}
		name, err := s.containerService.GetContainerNameByID(ctx, wh.TargetID)
		return kit.Ternary(err != nil, "", name)
	case WebhookTargetTypeProject:
		var localProject project.Project
		if err := s.db.WithContext(ctx).
			Select("name").
			Where("id = ?", wh.TargetID).
			First(&localProject).Error; err != nil {
			return ""
		}
		return localProject.Name
	case WebhookTargetTypeUpdater:
		return "Environment updater"
	case WebhookTargetTypeGitOps:
		var localSync project.GitOpsSync
		if err := s.db.WithContext(ctx).
			Select("name").
			Where("id = ? AND environment_id = ?", wh.TargetID, wh.EnvironmentID).
			First(&localSync).Error; err != nil {
			return ""
		}
		return localSync.Name
	default:
		return ""
	}
}

// GetWebhookByID returns a single webhook by ID, scoped to an environment.
func (s *WebhookService) GetWebhookByID(ctx context.Context, id, environmentID string) (*Webhook, error) {
	var wh Webhook
	err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", id, environmentID).
		First(&wh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWebhookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}
	return &wh, nil
}

// DeleteWebhook removes a webhook by ID, scoped to an environment.
func (s *WebhookService) DeleteWebhook(ctx context.Context, id, environmentID string, actor types.Actor) error {
	wh, err := s.GetWebhookByID(ctx, id, environmentID)
	if err != nil {
		return err
	}

	s.tokenWriteMu.Lock()
	defer s.tokenWriteMu.Unlock()
	result := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", id, environmentID).
		Delete(&Webhook{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete webhook: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrWebhookNotFound
	}

	s.tokenMu.Lock()
	delete(s.tokenHashes, wh.TokenHash)
	s.tokenMu.Unlock()

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeWebhookDelete,
			Severity:      event.EventSeverityInfo,
			Title:         "Webhook deleted: " + wh.Name,
			Description:   fmt.Sprintf("Deleted webhook '%s'", wh.Name),
			ResourceType:  new("webhook"),
			ResourceID:    &wh.ID,
			ResourceName:  &wh.Name,
			UserID:        &actor.ID,
			Username:      &actor.Username,
			EnvironmentID: &wh.EnvironmentID,
		})
	}

	return nil
}

// UpdateWebhook updates the enabled state of a webhook, scoped to an environment.
func (s *WebhookService) UpdateWebhook(
	ctx context.Context,
	id, environmentID string,
	update types.Webhook,
	submitted map[string]bool,
	actor types.Actor,
	callerPerms *authz.PermissionSet,
) (*Webhook, error) {
	var wh Webhook
	err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", id, environmentID).
		First(&wh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWebhookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}
	if authorizeErr := s.authorizeWebhookAction(
		ctx,
		callerPerms,
		wh.TargetType,
		resolvedWebhookActionType(wh.TargetType, wh.ActionType),
		cmp.Or(wh.TargetRef, wh.TargetID),
		wh.EnvironmentID,
	); authorizeErr != nil {
		return nil, authorizeErr
	}

	enabled := kit.Ternary(submitted["enabled"], update.Enabled, wh.Enabled)
	if updateWebhookErr := s.db.WithContext(ctx).Model(&wh).Update("enabled", enabled).Error; updateWebhookErr != nil {
		return nil, fmt.Errorf("failed to update webhook: %w", updateWebhookErr)
	}

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeWebhookUpdate,
			Severity:      event.EventSeveritySuccess,
			Title:         "Webhook updated: " + wh.Name,
			Description:   fmt.Sprintf("Updated webhook '%s' enabled=%v", wh.Name, enabled),
			ResourceType:  new("webhook"),
			ResourceID:    &wh.ID,
			ResourceName:  &wh.Name,
			UserID:        &actor.ID,
			Username:      &actor.Username,
			EnvironmentID: &wh.EnvironmentID,
		})
	}

	return &wh, nil
}

// TriggerByToken looks up a webhook by its raw token, validates it, and starts
// the configured action in the background so the HTTP caller gets an immediate
// acknowledgement instead of holding the connection for the full action
// duration (#3469). The action outcome is recorded in the event log.
func (s *WebhookService) TriggerByToken(ctx context.Context, rawToken string) error {
	prefix, err := parseWebhookPrefix(rawToken)
	if err != nil {
		return ErrWebhookInvalid
	}

	// Narrow by prefix first (indexed), then verify hash
	var candidates []Webhook
	if findWebhookErr := s.db.WithContext(ctx).
		Where("token_prefix = ?", prefix).
		Find(&candidates).Error; findWebhookErr != nil {
		return fmt.Errorf("failed to look up webhook: %w", findWebhookErr)
	}

	hash := kit.SHA256Hex(rawToken)
	var wh *Webhook
	for i := range candidates {
		if candidates[i].TokenHash == hash {
			wh = &candidates[i]
			break
		}
	}
	if wh == nil {
		return ErrWebhookNotFound
	}
	if !wh.Enabled {
		return ErrWebhookDisabled
	}

	actionType, err := resolveWebhookActionType(wh.TargetType, wh.ActionType)
	if err != nil {
		return err
	}

	// Record trigger time on accept — best-effort, do not fail the request if this update fails.
	now := time.Now()
	_ = s.db.WithContext(ctx).Model(wh).Update("last_triggered_at", now).Error

	execCtx := context.WithoutCancel(ctx)
	s.actions.Go(func() {
		defer func() {
			if panicErr := utils.PanicToError(recover()); panicErr != nil {
				slog.ErrorContext(execCtx, "webhook action panicked", "webhookId", wh.ID, "webhookName", wh.Name, "actionType", actionType, "error", panicErr)
			}
		}()
		if _, executeWebhookActionErr := s.executeWebhookAction(execCtx, wh, actionType); executeWebhookActionErr != nil {
			// Action failures are recorded as error events by wrapWebhookActionError.
			slog.ErrorContext(execCtx, "webhook action failed", "webhookId", wh.ID, "webhookName", wh.Name, "actionType", actionType, "error", executeWebhookActionErr)
			return
		}
		s.logWebhookEvent(execCtx, wh, actionType, event.EventSeveritySuccess, "")
	})

	return nil
}

// DrainActions blocks until all accepted background webhook actions finish or
// ctx expires, so shutdown does not silently drop acknowledged work.
func (s *WebhookService) DrainActions(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return utils.WaitGroup(ctx, &s.actions)
}

func (s *WebhookService) executeWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	// Webhooks are stored and triggered on the manager; actions against remote
	// environments are forwarded through the environment proxy.
	if isRemoteWebhookEnvironment(wh.EnvironmentID) {
		return s.executeRemoteWebhookAction(ctx, wh, actionType)
	}

	switch wh.TargetType {
	case WebhookTargetTypeContainer:
		return s.executeContainerWebhookAction(ctx, wh, actionType)
	case WebhookTargetTypeProject:
		return s.executeProjectWebhookAction(ctx, wh, actionType)
	case WebhookTargetTypeUpdater:
		return s.executeUpdaterWebhookAction(ctx, wh, actionType)
	case WebhookTargetTypeGitOps:
		return s.executeGitOpsWebhookAction(ctx, wh, actionType)
	default:
		return nil, ErrWebhookInvalidType
	}
}

// remoteWebhookRequest maps a webhook action to the env-scoped API
// request forwarded to the remote environment. Paths use the agent-local
// environment ID, matching the environment proxy's path-rewriting convention.
// wantResult reports whether the response carries an updatertypes.Result payload.
func remoteWebhookRequest(wh *Webhook, actionType string) (method, path string, wantResult bool, err error) {
	apiPrefix := "/api/environments/" + types.LocalDockerEnvironmentID

	switch wh.TargetType {
	case WebhookTargetTypeContainer:
		ref := cmp.Or(strings.TrimSpace(wh.TargetRef), strings.TrimSpace(wh.TargetID))
		if ref == "" {
			return "", "", false, ErrWebhookMissingTarget
		}
		containerPath := apiPrefix + "/containers/" + url.PathEscape(ref)
		switch actionType {
		case WebhookActionTypeUpdate:
			return http.MethodPost, containerPath + "/update", true, nil
		case WebhookActionTypeStart, WebhookActionTypeStop, WebhookActionTypeRestart:
			return http.MethodPost, containerPath + "?action=" + actionType, false, nil
		case WebhookActionTypeRedeploy:
			return http.MethodPost, containerPath + "/redeploy", false, nil
		}
		return "", "", false, ErrWebhookInvalidAction
	case WebhookTargetTypeProject:
		projectPath := apiPrefix + "/projects/" + url.PathEscape(wh.TargetID)
		switch actionType {
		case WebhookActionTypeUpdate:
			return http.MethodPost, projectPath + "?action=updateservices", false, nil
		case WebhookActionTypeDown, WebhookActionTypeRestart:
			return http.MethodPost, projectPath + "?action=" + actionType, false, nil
		case WebhookActionTypeUp, WebhookActionTypeRedeploy:
			return http.MethodPost, projectPath + "/deploy?action=" + actionType, false, nil
		}
		return "", "", false, ErrWebhookInvalidAction
	case WebhookTargetTypeUpdater:
		if actionType != WebhookActionTypeRun {
			return "", "", false, ErrWebhookInvalidAction
		}
		return http.MethodPost, apiPrefix + "/updater/run", true, nil
	case WebhookTargetTypeGitOps:
		if actionType != WebhookActionTypeSync {
			return "", "", false, ErrWebhookInvalidAction
		}
		return http.MethodPost, apiPrefix + "/gitops/syncs/" + url.PathEscape(wh.TargetID) + "?action=sync", false, nil
	default:
		return "", "", false, ErrWebhookInvalidType
	}
}

func (s *WebhookService) executeRemoteWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	if s.environmentService == nil {
		return nil, s.wrapWebhookActionError(ctx, wh, wh.TargetType, actionType, errors.New("environment service not available"))
	}

	method, path, wantResult, err := remoteWebhookRequest(wh, actionType)
	if err != nil {
		return nil, err
	}

	var result *types.UpdaterResult
	if wantResult {
		var out types.ApiResponse[*types.UpdaterResult]
		if proxyJSONRequestErr := s.environmentService.ProxyJSONRequest(ctx, wh.EnvironmentID, method, path, nil, &out); proxyJSONRequestErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, wh.TargetType, actionType, proxyJSONRequestErr)
		}
		result = out.Data
	} else {
		var out types.ApiResponse[any]
		if proxyServiceActionErr := s.environmentService.ProxyJSONRequest(ctx, wh.EnvironmentID, method, path, nil, &out); proxyServiceActionErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, wh.TargetType, actionType, proxyServiceActionErr)
		}
	}

	return result, nil
}

func (s *WebhookService) executeContainerWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	containerID, err := s.resolveContainerWebhookTargetID(ctx, wh)
	if err != nil {
		return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, err)
	}

	switch actionType {
	case WebhookActionTypeUpdate:
		result, updateSingleContainerErr := s.updaterService.UpdateSingleContainer(ctx, containerID)
		if updateSingleContainerErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, updateSingleContainerErr)
		}
		return result, nil
	case WebhookActionTypeStart:
		if startContainerErr := s.containerService.StartContainer(ctx, containerID, types.SystemUser); startContainerErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, startContainerErr)
		}
		return nil, nil
	case WebhookActionTypeStop:
		if stopContainerErr := s.containerService.StopContainer(ctx, containerID, types.SystemUser); stopContainerErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, stopContainerErr)
		}
		return nil, nil
	case WebhookActionTypeRestart:
		if restartContainerErr := s.containerService.RestartContainer(ctx, containerID, types.SystemUser); restartContainerErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, restartContainerErr)
		}
		return nil, nil
	case WebhookActionTypeRedeploy:
		if _, redeployContainerErr := s.containerService.RedeployContainer(ctx, containerID, types.SystemUser); redeployContainerErr != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "container", actionType, redeployContainerErr)
		}
		return nil, nil
	default:
		return nil, ErrWebhookInvalidAction
	}
}

func (s *WebhookService) resolveContainerWebhookTargetRef(ctx context.Context, targetID string) (string, error) {
	if s.containerService == nil {
		return "", nil
	}

	containerName, err := s.containerService.GetContainerNameByReference(ctx, targetID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve container target reference: %w", err)
	}

	return containerName, nil
}

func (s *WebhookService) resolveContainerWebhookTargetID(ctx context.Context, wh *Webhook) (string, error) {
	if s.containerService == nil {
		return wh.TargetID, nil
	}

	references := make([]string, 0, 2)
	if strings.TrimSpace(wh.TargetRef) != "" {
		references = append(references, wh.TargetRef)
	}
	if strings.TrimSpace(wh.TargetID) != "" {
		references = append(references, wh.TargetID)
	}

	var lastErr error
	for _, ref := range references {
		containerInfo, err := s.containerService.GetContainerByReference(ctx, ref)
		if err != nil {
			lastErr = err
			continue
		}

		containerName := strings.TrimPrefix(containerInfo.Name, "/")
		s.syncWebhookContainerTarget(ctx, wh, containerInfo.ID, containerName)
		return containerInfo.ID, nil
	}

	return "", kit.Ternary[error](lastErr != nil, lastErr, ErrWebhookMissingTarget)
}

func (s *WebhookService) syncWebhookContainerTarget(ctx context.Context, wh *Webhook, containerID, containerName string) {
	updates := map[string]any{}
	if containerID != "" && containerID != wh.TargetID {
		updates["target_id"] = containerID
		wh.TargetID = containerID
	}
	if containerName != "" && containerName != wh.TargetRef {
		updates["target_ref"] = containerName
		wh.TargetRef = containerName
	}
	if len(updates) == 0 {
		return
	}

	_ = s.db.WithContext(ctx).Model(wh).Updates(updates).Error
}

func (s *WebhookService) executeProjectWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	switch actionType {
	case WebhookActionTypeUpdate:
		if err := s.projectService.UpdateProjectServices(ctx, wh.TargetID, nil, types.SystemUser, true); err != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeUp:
		if err := s.projectService.DeployProject(ctx, wh.TargetID, types.SystemUser, nil); err != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeDown:
		if err := s.projectService.DownProject(ctx, wh.TargetID, types.SystemUser); err != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeRestart:
		if err := s.projectService.RestartProject(ctx, wh.TargetID, nil, types.SystemUser); err != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeRedeploy:
		if err := s.projectService.RedeployProject(ctx, wh.TargetID, types.SystemUser, nil); err != nil {
			return nil, s.wrapWebhookActionError(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	default:
		return nil, ErrWebhookInvalidAction
	}
}

func (s *WebhookService) executeUpdaterWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	if actionType != WebhookActionTypeRun {
		return nil, ErrWebhookInvalidAction
	}

	result, err := s.updaterService.ApplyPending(ctx, types.UpdaterOptions{})
	if err != nil {
		return nil, s.wrapWebhookActionError(ctx, wh, "updater", actionType, err)
	}

	return result, nil
}

func (s *WebhookService) executeGitOpsWebhookAction(ctx context.Context, wh *Webhook, actionType string) (*types.UpdaterResult, error) {
	if actionType != WebhookActionTypeSync {
		return nil, ErrWebhookInvalidAction
	}

	if _, err := s.gitOpsSyncService.PerformSync(ctx, wh.EnvironmentID, wh.TargetID, types.SystemUser); err != nil {
		return nil, s.wrapWebhookActionError(ctx, wh, "gitops", actionType, err)
	}

	return nil, nil
}

func (s *WebhookService) wrapWebhookActionError(ctx context.Context, wh *Webhook, targetKind, actionType string, err error) error {
	msg := fmt.Sprintf("%s %s failed: %s", targetKind, actionType, err)
	s.logWebhookEvent(ctx, wh, actionType, event.EventSeverityError, msg)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", targetKind, actionType, err)
	}
	return nil
}

func (s *WebhookService) logWebhookEvent(ctx context.Context, wh *Webhook, actionType string, severity event.EventSeverity, errMsg string) {
	if s.eventService == nil {
		return
	}
	title := "Webhook triggered: " + wh.Name
	if severity == event.EventSeverityError {
		title = "Webhook trigger failed: " + wh.Name
	}
	description := fmt.Sprintf("Target type: %s, action: %s", wh.TargetType, actionType)
	description = cmp.Or(errMsg, description)
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeWebhookTrigger,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("webhook"),
		ResourceID:    &wh.ID,
		ResourceName:  &wh.Name,
		EnvironmentID: &wh.EnvironmentID,
		Metadata: database.JSON{
			"targetType":  wh.TargetType,
			"actionType":  actionType,
			"targetId":    wh.TargetID,
			"tokenPrefix": wh.TokenPrefix,
		},
	})
}
