package services

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	libcrypto "github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/updater"
	webhooktypes "github.com/getarcaneapp/arcane/types/webhook"
	"gorm.io/gorm"
)

var (
	ErrWebhookNotFound      = errors.New("webhook not found")
	ErrWebhookInvalid       = errors.New("invalid webhook token")
	ErrWebhookDisabled      = errors.New("webhook is disabled")
	ErrWebhookInvalidType   = errors.New("invalid webhook target type")
	ErrWebhookInvalidAction = errors.New("invalid webhook action type")
	ErrWebhookMissingTarget = errors.New("target ID is required for container, project, and gitops webhook types")
)

const (
	webhookTokenPrefix    = "arc_wh_"
	webhookTokenLength    = 32 // raw bytes → 64 hex chars
	webhookTokenPrefixLen = 8  // chars of the hex portion used as lookup prefix
)

type WebhookService struct {
	db                *database.DB
	containerService  *ContainerService
	updaterService    *UpdaterService
	projectService    *ProjectService
	gitOpsSyncService *GitOpsSyncService
	eventService      *EventService
}

func NewWebhookService(db *database.DB, containerService *ContainerService, updaterService *UpdaterService, projectService *ProjectService, gitOpsSyncService *GitOpsSyncService, eventService *EventService) *WebhookService {
	return &WebhookService{
		db:                db,
		containerService:  containerService,
		updaterService:    updaterService,
		projectService:    projectService,
		gitOpsSyncService: gitOpsSyncService,
		eventService:      eventService,
	}
}

// generateWebhookTokenInternal creates a new random webhook token and returns the raw token
// (to be shown to the user once), its SHA-256 hash, and the lookup prefix.
func generateWebhookTokenInternal() (raw, hash, prefix string, err error) {
	b := make([]byte, webhookTokenLength)
	if _, err = crand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate webhook token: %w", err)
	}
	secretHex := hex.EncodeToString(b)
	encrypted, err := libcrypto.Encrypt(secretHex)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encrypt webhook token: %w", err)
	}
	encryptedBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decode encrypted webhook token: %w", err)
	}
	tokenHex := hex.EncodeToString(encryptedBytes)
	raw = webhookTokenPrefix + tokenHex
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	prefix = webhookTokenPrefix + tokenHex[:webhookTokenPrefixLen]
	return raw, hash, prefix, nil
}

func hashWebhookTokenInternal(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func parseWebhookPrefixInternal(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	hexPart, ok := strings.CutPrefix(raw, webhookTokenPrefix)
	if !ok || len(hexPart) < webhookTokenPrefixLen {
		return "", ErrWebhookInvalid
	}
	return webhookTokenPrefix + hexPart[:webhookTokenPrefixLen], nil
}

func defaultWebhookActionTypeInternal(targetType string) (string, error) {
	switch targetType {
	case models.WebhookTargetTypeContainer, models.WebhookTargetTypeProject:
		return models.WebhookActionTypeUpdate, nil
	case models.WebhookTargetTypeUpdater:
		return models.WebhookActionTypeRun, nil
	case models.WebhookTargetTypeGitOps:
		return models.WebhookActionTypeSync, nil
	default:
		return "", ErrWebhookInvalidType
	}
}

func resolveWebhookActionTypeInternal(targetType, actionType string) (string, error) {
	switch targetType {
	case models.WebhookTargetTypeContainer, models.WebhookTargetTypeProject, models.WebhookTargetTypeUpdater, models.WebhookTargetTypeGitOps:
	default:
		return "", ErrWebhookInvalidType
	}

	normalizedActionType := strings.TrimSpace(strings.ToLower(actionType))
	if normalizedActionType == "" {
		return defaultWebhookActionTypeInternal(targetType)
	}

	if targetType == models.WebhookTargetTypeProject && normalizedActionType == "deploy" {
		normalizedActionType = models.WebhookActionTypeUp
	}

	switch targetType {
	case models.WebhookTargetTypeContainer:
		switch normalizedActionType {
		case models.WebhookActionTypeUpdate, models.WebhookActionTypeStart, models.WebhookActionTypeStop, models.WebhookActionTypeRestart, models.WebhookActionTypeRedeploy:
			return normalizedActionType, nil
		}
	case models.WebhookTargetTypeProject:
		switch normalizedActionType {
		case models.WebhookActionTypeUpdate, models.WebhookActionTypeUp, models.WebhookActionTypeDown, models.WebhookActionTypeRestart, models.WebhookActionTypeRedeploy:
			return normalizedActionType, nil
		}
	case models.WebhookTargetTypeUpdater:
		if normalizedActionType == models.WebhookActionTypeRun {
			return normalizedActionType, nil
		}
	case models.WebhookTargetTypeGitOps:
		if normalizedActionType == models.WebhookActionTypeSync {
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
func (s *WebhookService) CreateWebhook(ctx context.Context, name, targetType, actionType, targetID, environmentID string, actor models.User) (*models.Webhook, string, error) {
	resolvedActionType, err := resolveWebhookActionTypeInternal(targetType, actionType)
	if err != nil {
		return nil, "", err
	}

	targetRef := ""

	// The updater target type operates environment-wide and has no specific target resource.
	if targetType == models.WebhookTargetTypeUpdater {
		targetID = ""
	} else if strings.TrimSpace(targetID) == "" {
		return nil, "", ErrWebhookMissingTarget
	}

	if targetType == models.WebhookTargetTypeContainer {
		targetRef, err = s.resolveContainerWebhookTargetRefInternal(ctx, targetID)
		if err != nil {
			return nil, "", err
		}
	}

	raw, hash, prefix, err := generateWebhookTokenInternal()
	if err != nil {
		return nil, "", err
	}

	wh := &models.Webhook{
		Name:          name,
		TokenHash:     hash,
		TokenPrefix:   prefix,
		TargetType:    targetType,
		ActionType:    resolvedActionType,
		TargetID:      targetID,
		TargetRef:     targetRef,
		EnvironmentID: environmentID,
		Enabled:       true,
	}

	if err := s.db.WithContext(ctx).Create(wh).Error; err != nil {
		return nil, "", fmt.Errorf("failed to create webhook: %w", err)
	}

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookCreate,
			Severity:      models.EventSeveritySuccess,
			Title:         fmt.Sprintf("Webhook created: %s", wh.Name),
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
func (s *WebhookService) ListWebhooks(ctx context.Context, environmentID string) ([]models.Webhook, error) {
	var webhooks []models.Webhook
	if err := s.db.WithContext(ctx).
		Where("environment_id = ?", environmentID).
		Order("created_at DESC").
		Find(&webhooks).Error; err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}
	return webhooks, nil
}

func (s *WebhookService) ListWebhookSummaries(ctx context.Context, environmentID string) ([]webhooktypes.Summary, error) {
	webhooks, err := s.ListWebhooks(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	summaries := make([]webhooktypes.Summary, len(webhooks))
	for i := range webhooks {
		wh := webhooks[i]
		summaries[i] = webhooktypes.Summary{
			ID:              wh.ID,
			Name:            wh.Name,
			TokenPrefix:     wh.TokenPrefix,
			TargetType:      wh.TargetType,
			ActionType:      resolvedWebhookActionTypeInternal(wh.TargetType, wh.ActionType),
			TargetID:        wh.TargetID,
			TargetName:      s.resolveWebhookTargetNameInternal(ctx, &wh),
			EnvironmentID:   wh.EnvironmentID,
			Enabled:         wh.Enabled,
			LastTriggeredAt: wh.LastTriggeredAt,
			CreatedAt:       wh.CreatedAt,
		}
	}

	return summaries, nil
}

func (s *WebhookService) resolveWebhookTargetNameInternal(ctx context.Context, wh *models.Webhook) string {
	switch wh.TargetType {
	case models.WebhookTargetTypeContainer:
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
		if err != nil {
			return ""
		}
		return name
	case models.WebhookTargetTypeProject:
		var project models.Project
		if err := s.db.WithContext(ctx).
			Select("name").
			Where("id = ?", wh.TargetID).
			First(&project).Error; err != nil {
			return ""
		}
		return project.Name
	case models.WebhookTargetTypeUpdater:
		return "Environment updater"
	case models.WebhookTargetTypeGitOps:
		var sync models.GitOpsSync
		if err := s.db.WithContext(ctx).
			Select("name").
			Where("id = ? AND environment_id = ?", wh.TargetID, wh.EnvironmentID).
			First(&sync).Error; err != nil {
			return ""
		}
		return sync.Name
	default:
		return ""
	}
}

// GetWebhookByID returns a single webhook by ID, scoped to an environment.
func (s *WebhookService) GetWebhookByID(ctx context.Context, id, environmentID string) (*models.Webhook, error) {
	var wh models.Webhook
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
func (s *WebhookService) DeleteWebhook(ctx context.Context, id, environmentID string, actor models.User) error {
	wh, err := s.GetWebhookByID(ctx, id, environmentID)
	if err != nil {
		return err
	}

	result := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", id, environmentID).
		Delete(&models.Webhook{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete webhook: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrWebhookNotFound
	}

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookDelete,
			Severity:      models.EventSeverityInfo,
			Title:         fmt.Sprintf("Webhook deleted: %s", wh.Name),
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
func (s *WebhookService) UpdateWebhook(ctx context.Context, id, environmentID string, enabled bool, actor models.User) (*models.Webhook, error) {
	var wh models.Webhook
	err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", id, environmentID).
		First(&wh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWebhookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&wh).Update("enabled", enabled).Error; err != nil {
		return nil, fmt.Errorf("failed to update webhook: %w", err)
	}

	if s.eventService != nil {
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookUpdate,
			Severity:      models.EventSeveritySuccess,
			Title:         fmt.Sprintf("Webhook updated: %s", wh.Name),
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

// TriggerByToken looks up a webhook by its raw token and executes the configured action.
// Returns an updater result for "updater" webhooks; nil for "project" and "gitops".
func (s *WebhookService) TriggerByToken(ctx context.Context, rawToken string) (*updater.Result, error) {
	prefix, err := parseWebhookPrefixInternal(rawToken)
	if err != nil {
		return nil, ErrWebhookInvalid
	}

	// Narrow by prefix first (indexed), then verify hash
	var candidates []models.Webhook
	if err := s.db.WithContext(ctx).
		Where("token_prefix = ?", prefix).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("failed to look up webhook: %w", err)
	}

	hash := hashWebhookTokenInternal(rawToken)
	var wh *models.Webhook
	for i := range candidates {
		if candidates[i].TokenHash == hash {
			wh = &candidates[i]
			break
		}
	}
	if wh == nil {
		return nil, ErrWebhookNotFound
	}
	if !wh.Enabled {
		return nil, ErrWebhookDisabled
	}

	var result *updater.Result
	actionType, err := resolveWebhookActionTypeInternal(wh.TargetType, wh.ActionType)
	if err != nil {
		return nil, err
	}

	result, err = s.executeWebhookActionInternal(ctx, wh, actionType)
	if err != nil {
		return nil, err
	}

	// Record trigger time — best-effort, do not fail the request if this update fails.
	now := time.Now()
	_ = s.db.WithContext(ctx).Model(wh).Update("last_triggered_at", now).Error //nolint:errcheck

	s.logWebhookEventInternal(ctx, wh, actionType, models.EventSeveritySuccess, "")

	return result, nil
}

func (s *WebhookService) executeWebhookActionInternal(ctx context.Context, wh *models.Webhook, actionType string) (*updater.Result, error) {
	switch wh.TargetType {
	case models.WebhookTargetTypeContainer:
		return s.executeContainerWebhookActionInternal(ctx, wh, actionType)
	case models.WebhookTargetTypeProject:
		return s.executeProjectWebhookActionInternal(ctx, wh, actionType)
	case models.WebhookTargetTypeUpdater:
		return s.executeUpdaterWebhookActionInternal(ctx, wh, actionType)
	case models.WebhookTargetTypeGitOps:
		return s.executeGitOpsWebhookActionInternal(ctx, wh, actionType)
	default:
		return nil, ErrWebhookInvalidType
	}
}

func (s *WebhookService) executeContainerWebhookActionInternal(ctx context.Context, wh *models.Webhook, actionType string) (*updater.Result, error) {
	containerID, err := s.resolveContainerWebhookTargetIDInternal(ctx, wh)
	if err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
	}

	switch actionType {
	case models.WebhookActionTypeUpdate:
		result, err := s.updaterService.UpdateSingleContainer(ctx, containerID)
		if err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
		}
		return result, nil
	case models.WebhookActionTypeStart:
		if err := s.containerService.StartContainer(ctx, containerID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeStop:
		if err := s.containerService.StopContainer(ctx, containerID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeRestart:
		if err := s.containerService.RestartContainer(ctx, containerID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeRedeploy:
		if _, err := s.containerService.RedeployContainer(ctx, containerID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "container", actionType, err)
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

func (s *WebhookService) resolveContainerWebhookTargetIDInternal(ctx context.Context, wh *models.Webhook) (string, error) {
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

	if lastErr != nil {
		return "", lastErr
	}

	return "", ErrWebhookMissingTarget
}

func (s *WebhookService) syncWebhookContainerTargetInternal(ctx context.Context, wh *models.Webhook, containerID, containerName string) {
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

	_ = s.db.WithContext(ctx).Model(wh).Updates(updates).Error //nolint:errcheck
}

func (s *WebhookService) executeProjectWebhookActionInternal(ctx context.Context, wh *models.Webhook, actionType string) (*updater.Result, error) {
	switch actionType {
	case models.WebhookActionTypeUpdate:
		if err := s.projectService.UpdateProjectServices(ctx, wh.TargetID, nil, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeUp:
		if err := s.projectService.DeployProject(ctx, wh.TargetID, systemUser, nil); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeDown:
		if err := s.projectService.DownProject(ctx, wh.TargetID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeRestart:
		if err := s.projectService.RestartProject(ctx, wh.TargetID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	case models.WebhookActionTypeRedeploy:
		if err := s.projectService.RedeployProject(ctx, wh.TargetID, systemUser); err != nil {
			return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "project", actionType, err)
		}
		return nil, nil
	default:
		return nil, ErrWebhookInvalidAction
	}
}

func (s *WebhookService) executeUpdaterWebhookActionInternal(ctx context.Context, wh *models.Webhook, actionType string) (*updater.Result, error) {
	if actionType != models.WebhookActionTypeRun {
		return nil, ErrWebhookInvalidAction
	}

	result, err := s.updaterService.ApplyPending(ctx, false)
	if err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "updater", actionType, err)
	}

	return result, nil
}

func (s *WebhookService) executeGitOpsWebhookActionInternal(ctx context.Context, wh *models.Webhook, actionType string) (*updater.Result, error) {
	if actionType != models.WebhookActionTypeSync {
		return nil, ErrWebhookInvalidAction
	}

	if _, err := s.gitOpsSyncService.PerformSync(ctx, wh.EnvironmentID, wh.TargetID, systemUser); err != nil {
		return nil, s.wrapWebhookActionErrorInternal(ctx, wh, "gitops", actionType, err)
	}

	return nil, nil
}

func (s *WebhookService) wrapWebhookActionErrorInternal(ctx context.Context, wh *models.Webhook, targetKind, actionType string, err error) error {
	msg := fmt.Sprintf("%s %s failed: %s", targetKind, actionType, err)
	s.logWebhookEventInternal(ctx, wh, actionType, models.EventSeverityError, msg)
	return fmt.Errorf("%s %s failed: %w", targetKind, actionType, err)
}

func (s *WebhookService) logWebhookEventInternal(ctx context.Context, wh *models.Webhook, actionType string, severity models.EventSeverity, errMsg string) {
	if s.eventService == nil {
		return
	}
	title := fmt.Sprintf("Webhook triggered: %s", wh.Name)
	if severity == models.EventSeverityError {
		title = fmt.Sprintf("Webhook trigger failed: %s", wh.Name)
	}
	description := fmt.Sprintf("Target type: %s, action: %s", wh.TargetType, actionType)
	if errMsg != "" {
		description = errMsg
	}
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeWebhookTrigger,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  new("webhook"),
		ResourceID:    &wh.ID,
		ResourceName:  &wh.Name,
		EnvironmentID: &wh.EnvironmentID,
		Metadata: models.JSON{
			"targetType":  wh.TargetType,
			"actionType":  actionType,
			"targetId":    wh.TargetID,
			"tokenPrefix": wh.TokenPrefix,
		},
	})
}
