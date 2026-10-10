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

	"github.com/getarcaneapp/arcane/types/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	updatertypes "github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/webhook"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	webhookTokenPrefix    = "arc_wh_"
	webhookTokenLength    = 32 // raw bytes → 64 hex chars
	webhookTokenPrefixLen = 8  // chars of the hex portion used as lookup prefix
	// hex length of a generated token's ciphertext: GCM nonce (12) + encrypted secretHex + GCM tag (16)
	webhookTokenHexLen = 2 * (12 + webhookTokenLength*2 + 16)
)

var (
	ErrWebhookNotFound      = errors.New("webhook not found")
	ErrWebhookInvalid       = errors.New("invalid webhook token")
	ErrWebhookDisabled      = errors.New("webhook is disabled")
	ErrWebhookInvalidType   = errors.New("invalid webhook target type")
	ErrWebhookInvalidAction = errors.New("invalid webhook action type")
	ErrWebhookMissingTarget = errors.New("target ID is required for container, project, and gitops webhook types")

	// remoteWebhookTargetListPaths lists agent endpoints for target types whose records live in the agent's database.
	remoteWebhookTargetListPaths = map[string]string{
		WebhookTargetTypeProject: "/api/environments/" + types.LocalDockerEnvironmentID + "/projects/references",
		WebhookTargetTypeGitOps:  "/api/environments/" + types.LocalDockerEnvironmentID + "/gitops-syncs?limit=-1",
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

// isRemoteWebhookEnvironmentInternal reports whether environmentID refers to a
// remote environment (anything but the local Docker environment).
func isRemoteWebhookEnvironmentInternal(environmentID string) bool {
	return environmentID != "" && environmentID != types.LocalDockerEnvironmentID
}

// generateWebhookTokenInternal creates a new random webhook token and returns the raw token
// (to be shown to the user once), its SHA-256 hash, and the lookup prefix.
func generateWebhookTokenInternal() (raw, hash, prefix string, err error) {
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

func parseWebhookPrefixInternal(raw string) (string, error) {
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

func defaultWebhookActionTypeInternal(targetType string) (string, error) {
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

func resolveWebhookActionTypeInternal(targetType, actionType string) (string, error) {
	switch targetType {
	case WebhookTargetTypeContainer, WebhookTargetTypeProject, WebhookTargetTypeUpdater, WebhookTargetTypeGitOps:
	default:
		return "", ErrWebhookInvalidType
	}

	normalizedActionType := strings.TrimSpace(strings.ToLower(actionType))
	if normalizedActionType == "" {
		return defaultWebhookActionTypeInternal(targetType)
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

func resolvedWebhookActionTypeInternal(targetType, actionType string) string {
	resolvedActionType, err := resolveWebhookActionTypeInternal(targetType, actionType)
	if err != nil {
		return strings.TrimSpace(strings.ToLower(actionType))
	}
	return resolvedActionType
}

// CreateWebhook creates a new webhook targeting a stack, the environment-wide updater, or a gitops sync.
// It returns the webhook record with the raw token populated (only available at creation time).
func (s *WebhookService) CreateWebhook(ctx context.Context, name, targetType, actionType, targetID, environmentID string, actor user.Actor) (*Webhook, string, error) {
	input := webhook.CreateInput{Name: name, TargetType: targetType, ActionType: actionType, TargetID: targetID}
	if err := normalization.Normalize(&input); err != nil {
		return nil, "", err
	}
	resolvedActionType, err := resolveWebhookActionTypeInternal(targetType, actionType)
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

	// Container references can only be resolved against the local Docker daemon;
	// remote-environment webhooks keep the raw target and resolve on trigger.
	if targetType == WebhookTargetTypeContainer && !isRemoteWebhookEnvironmentInternal(environmentID) {
		targetRef, err = s.resolveContainerWebhookTargetRefInternal(ctx, targetID)
		if err != nil {
			return nil, "", err
		}
	}

	raw, hash, prefix, err := generateWebhookTokenInternal()
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

func (s *WebhookService) ListWebhookSummaries(ctx context.Context, environmentID string) ([]webhook.Summary, error) {
	webhooks, err := s.ListWebhooks(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	remoteNames := s.remoteWebhookTargetNamesInternal(ctx, environmentID, webhooks)
	summaries := make([]webhook.Summary, len(webhooks))
	for i := range webhooks {
		wh := webhooks[i]
		targetName, ok := remoteNames[wh.TargetType][wh.TargetID]
		if !ok {
			targetName = s.resolveWebhookTargetNameInternal(ctx, &wh)
		}
		summaries[i] = webhook.Summary{
			ID:              wh.ID,
			Name:            wh.Name,
			TokenPrefix:     wh.TokenPrefix,
			TargetType:      wh.TargetType,
			ActionType:      resolvedWebhookActionTypeInternal(wh.TargetType, wh.ActionType),
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

// remoteWebhookTargetNamesInternal fetches target names keyed by target type and ID from a remote environment.
func (s *WebhookService) remoteWebhookTargetNamesInternal(ctx context.Context, environmentID string, webhooks []Webhook) map[string]map[string]string {
	names := map[string]map[string]string{}
	if !isRemoteWebhookEnvironmentInternal(environmentID) {
		return names
	}

	for targetType, path := range remoteWebhookTargetListPaths {
		if !slices.ContainsFunc(webhooks, func(wh Webhook) bool { return wh.TargetType == targetType }) {
			continue
		}
		var out base.ApiResponse[[]remoteWebhookTarget]
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

func (s *WebhookService) resolveWebhookTargetNameInternal(ctx context.Context, wh *Webhook) string {
	switch wh.TargetType {
	case WebhookTargetTypeContainer:
		// Remote containers cannot be resolved against the local Docker daemon.
		if isRemoteWebhookEnvironmentInternal(wh.EnvironmentID) {
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
func (s *WebhookService) DeleteWebhook(ctx context.Context, id, environmentID string, actor user.Actor) error {
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
func (s *WebhookService) UpdateWebhook(ctx context.Context, id, environmentID string, enabled bool, actor user.Actor) (*Webhook, error) {
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
	prefix, err := parseWebhookPrefixInternal(rawToken)
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

	actionType, err := resolveWebhookActionTypeInternal(wh.TargetType, wh.ActionType)
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
		if _, executeWebhookActionErr := s.executeWebhookActionInternal(execCtx, wh, actionType); executeWebhookActionErr != nil {
			// Action failures are recorded as error events by wrapWebhookActionErrorInternal.
			slog.ErrorContext(execCtx, "webhook action failed", "webhookId", wh.ID, "webhookName", wh.Name, "actionType", actionType, "error", executeWebhookActionErr)
			return
		}
		s.logWebhookEventInternal(execCtx, wh, actionType, event.EventSeveritySuccess, "")
	})

	return nil
}

// DrainActions blocks until all accepted background webhook actions finish or
// ctx expires, so shutdown does not silently drop acknowledged work.
func (s *WebhookService) DrainActions(ctx context.Context) error {
	return utils.WaitGroup(ctx, &s.actions)
}

func (s *WebhookService) executeWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	// Webhooks are stored and triggered on the manager; actions against remote
	// environments are forwarded through the environment proxy.
	if isRemoteWebhookEnvironmentInternal(wh.EnvironmentID) {
		return s.executeRemoteWebhookActionInternal(ctx, wh, actionType)
	}

	switch wh.TargetType {
	case WebhookTargetTypeContainer:
		return s.executeContainerWebhookActionInternal(ctx, wh, actionType)
	case WebhookTargetTypeProject:
		return s.executeProjectWebhookActionInternal(ctx, wh, actionType)
	case WebhookTargetTypeUpdater:
		return s.executeUpdaterWebhookActionInternal(ctx, wh, actionType)
	case WebhookTargetTypeGitOps:
		return s.executeGitOpsWebhookActionInternal(ctx, wh, actionType)
	default:
		return nil, ErrWebhookInvalidType
	}
}

// remoteWebhookRequestInternal maps a webhook action to the env-scoped API
// request forwarded to the remote environment. Paths use the agent-local
// environment ID, matching the environment proxy's path-rewriting convention.
// wantResult reports whether the response carries an updatertypes.Result payload.
func remoteWebhookRequestInternal(wh *Webhook, actionType string) (method, path string, wantResult bool, err error) {
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
		case WebhookActionTypeStart, WebhookActionTypeStop, WebhookActionTypeRestart, WebhookActionTypeRedeploy:
			return http.MethodPost, containerPath + "/" + actionType, false, nil
		}
		return "", "", false, ErrWebhookInvalidAction
	case WebhookTargetTypeProject:
		projectPath := apiPrefix + "/projects/" + url.PathEscape(wh.TargetID)
		switch actionType {
		case WebhookActionTypeUpdate:
			return http.MethodPost, projectPath + "/update-services", false, nil
		case WebhookActionTypeUp, WebhookActionTypeDown, WebhookActionTypeRestart, WebhookActionTypeRedeploy:
			return http.MethodPost, projectPath + "/" + actionType, false, nil
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
		return http.MethodPost, apiPrefix + "/gitops-syncs/" + url.PathEscape(wh.TargetID) + "/sync", false, nil
	default:
		return "", "", false, ErrWebhookInvalidType
	}
}

func (s *WebhookService) executeRemoteWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	if s.environmentService == nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, wh.TargetType, actionType, errors.New("environment service not available"))
	}

	method, path, wantResult, err := remoteWebhookRequestInternal(wh, actionType)
	if err != nil {
		return nil, err
	}

	var result *updatertypes.Result
	if wantResult {
		var out base.ApiResponse[*updatertypes.Result]
		if proxyJSONRequestErr := s.environmentService.ProxyJSONRequest(ctx, wh.EnvironmentID, method, path, nil, &out); proxyJSONRequestErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, wh.TargetType, actionType, proxyJSONRequestErr)
		}
		result = out.Data
	} else {
		var out base.ApiResponse[any]
		if proxyServiceActionErr := s.environmentService.ProxyJSONRequest(ctx, wh.EnvironmentID, method, path, nil, &out); proxyServiceActionErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, wh.TargetType, actionType, proxyServiceActionErr)
		}
	}

	return result, nil
}

func (s *WebhookService) executeContainerWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	containerID, err := s.resolveContainerWebhookTargetIDInternal(ctx, wh)
	if err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
	}

	switch actionType {
	case WebhookActionTypeUpdate:
		result, updateSingleContainerErr := s.updaterService.UpdateSingleContainer(ctx, containerID)
		if updateSingleContainerErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, updateSingleContainerErr)
		}
		return result, nil
	case WebhookActionTypeStart:
		if startContainerErr := s.containerService.StartContainer(ctx, containerID, user.SystemUser); startContainerErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, startContainerErr)
		}
		return nil, nil
	case WebhookActionTypeStop:
		if stopContainerErr := s.containerService.StopContainer(ctx, containerID, user.SystemUser); stopContainerErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, stopContainerErr)
		}
		return nil, nil
	case WebhookActionTypeRestart:
		if restartContainerErr := s.containerService.RestartContainer(ctx, containerID, user.SystemUser); restartContainerErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, restartContainerErr)
		}
		return nil, nil
	case WebhookActionTypeRedeploy:
		if _, redeployContainerErr := s.containerService.RedeployContainer(ctx, containerID, user.SystemUser); redeployContainerErr != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, redeployContainerErr)
		}
		return nil, nil
	default:
		return nil, ErrWebhookInvalidAction
	}
}

func (s *WebhookService) resolveContainerWebhookTargetRefInternal(ctx context.Context, targetID string) (string, error) {
	if s.containerService == nil {
		return "", nil
	}

	containerName, err := s.containerService.GetContainerNameByReference(ctx, targetID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve container target reference: %w", err)
	}

	return containerName, nil
}

func (s *WebhookService) resolveContainerWebhookTargetIDInternal(ctx context.Context, wh *Webhook) (string, error) {
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
		s.syncWebhookContainerTargetInternal(ctx, wh, containerInfo.ID, containerName)
		return containerInfo.ID, nil
	}

	return "", kit.Ternary[error](lastErr != nil, lastErr, ErrWebhookMissingTarget)
}

func (s *WebhookService) syncWebhookContainerTargetInternal(ctx context.Context, wh *Webhook, containerID, containerName string) {
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

func (s *WebhookService) executeProjectWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	switch actionType {
	case WebhookActionTypeUpdate:
		if err := s.projectService.UpdateProjectServices(ctx, wh.TargetID, nil, user.SystemUser, true); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeUp:
		if err := s.projectService.DeployProject(ctx, wh.TargetID, user.SystemUser, nil); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeDown:
		if err := s.projectService.DownProject(ctx, wh.TargetID, user.SystemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeRestart:
		if err := s.projectService.RestartProject(ctx, wh.TargetID, nil, user.SystemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case WebhookActionTypeRedeploy:
		if err := s.projectService.RedeployProject(ctx, wh.TargetID, user.SystemUser, nil); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	default:
		return nil, ErrWebhookInvalidAction
	}
}

func (s *WebhookService) executeUpdaterWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	if actionType != WebhookActionTypeRun {
		return nil, ErrWebhookInvalidAction
	}

	result, err := s.updaterService.ApplyPending(ctx, updatertypes.Options{})
	if err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "updater", actionType, err)
	}

	return result, nil
}

func (s *WebhookService) executeGitOpsWebhookActionInternal(ctx context.Context, wh *Webhook, actionType string) (*updatertypes.Result, error) {
	if actionType != WebhookActionTypeSync {
		return nil, ErrWebhookInvalidAction
	}

	if _, err := s.gitOpsSyncService.PerformSync(ctx, wh.EnvironmentID, wh.TargetID, user.SystemUser); err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "gitops", actionType, err)
	}

	return nil, nil
}

func (s *WebhookService) wrapWebhookActionErrorInternal(ctx context.Context, wh *Webhook, targetKind, actionType string, err error) error {
	msg := fmt.Sprintf("%s %s failed: %s", targetKind, actionType, err)
	s.logWebhookEventInternal(ctx, wh, actionType, event.EventSeverityError, msg)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", targetKind, actionType, err)
	}
	return nil
}

func (s *WebhookService) logWebhookEventInternal(ctx context.Context, wh *Webhook, actionType string, severity event.EventSeverity, errMsg string) {
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
