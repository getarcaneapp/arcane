package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/updater"
	"gorm.io/gorm"
)

var (
	ErrWebhookNotFound      = errors.New("webhook not found")
	ErrWebhookInvalid       = errors.New("invalid webhook token")
	ErrWebhookDisabled      = errors.New("webhook is disabled")
	ErrWebhookInvalidType   = errors.New("invalid webhook target type")
	ErrWebhookMissingTarget = errors.New("target ID is required for container, project, and gitops webhook types")
)

const (
	webhookTokenPrefix    = "arc_wh_"
	webhookTokenLength    = 32 // raw bytes → 64 hex chars
	webhookTokenPrefixLen = 8  // chars of the hex portion used as lookup prefix
)

type WebhookService struct {
	db                *database.DB
	updaterService    *UpdaterService
	projectService    *ProjectService
	gitOpsSyncService *GitOpsSyncService
	eventService      *EventService
}

func NewWebhookService(db *database.DB, updaterService *UpdaterService, projectService *ProjectService, gitOpsSyncService *GitOpsSyncService, eventService *EventService) *WebhookService {
	return &WebhookService{
		db:                db,
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
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate webhook token: %w", err)
	}
	hexPart := hex.EncodeToString(b)
	raw = webhookTokenPrefix + hexPart
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	prefix = webhookTokenPrefix + hexPart[:webhookTokenPrefixLen]
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

// CreateWebhook creates a new webhook targeting a stack, the environment-wide updater, or a gitops sync.
// It returns the webhook record with the raw token populated (only available at creation time).
func (s *WebhookService) CreateWebhook(ctx context.Context, name, targetType, targetID, environmentID string, actor models.User) (*models.Webhook, string, error) {
	switch targetType {
	case models.WebhookTargetTypeContainer, models.WebhookTargetTypeProject, models.WebhookTargetTypeUpdater, models.WebhookTargetTypeGitOps:
	default:
		return nil, "", ErrWebhookInvalidType
	}

	// The updater target type operates environment-wide and has no specific target resource.
	if targetType == models.WebhookTargetTypeUpdater {
		targetID = ""
	} else if strings.TrimSpace(targetID) == "" {
		return nil, "", ErrWebhookMissingTarget
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
		TargetID:      targetID,
		EnvironmentID: environmentID,
		Enabled:       true,
	}

	if err := s.db.WithContext(ctx).Create(wh).Error; err != nil {
		return nil, "", fmt.Errorf("failed to create webhook: %w", err)
	}

	if s.eventService != nil {
		resourceType := "webhook"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookCreate,
			Severity:      models.EventSeveritySuccess,
			Title:         fmt.Sprintf("Webhook created: %s", wh.Name),
			Description:   fmt.Sprintf("Created webhook '%s' targeting %s", wh.Name, wh.TargetType),
			ResourceType:  &resourceType,
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
		resourceType := "webhook"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookDelete,
			Severity:      models.EventSeverityInfo,
			Title:         fmt.Sprintf("Webhook deleted: %s", wh.Name),
			Description:   fmt.Sprintf("Deleted webhook '%s'", wh.Name),
			ResourceType:  &resourceType,
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
		resourceType := "webhook"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeWebhookUpdate,
			Severity:      models.EventSeveritySuccess,
			Title:         fmt.Sprintf("Webhook updated: %s", wh.Name),
			Description:   fmt.Sprintf("Updated webhook '%s' enabled=%v", wh.Name, enabled),
			ResourceType:  &resourceType,
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

	switch wh.TargetType {
	case models.WebhookTargetTypeContainer:
		result, err = s.updaterService.UpdateSingleContainer(ctx, wh.TargetID)
		if err != nil {
			s.logWebhookEvent(ctx, wh, models.EventSeverityError, fmt.Sprintf("container update failed: %s", err))
			return nil, fmt.Errorf("container update failed: %w", err)
		}
	case models.WebhookTargetTypeProject:
		if err := s.projectService.UpdateProjectServices(ctx, wh.TargetID, nil, systemUser); err != nil {
			s.logWebhookEvent(ctx, wh, models.EventSeverityError, fmt.Sprintf("project update failed: %s", err))
			return nil, fmt.Errorf("project update failed: %w", err)
		}
	case models.WebhookTargetTypeUpdater:
		result, err = s.updaterService.ApplyPending(ctx, false)
		if err != nil {
			s.logWebhookEvent(ctx, wh, models.EventSeverityError, fmt.Sprintf("updater run failed: %s", err))
			return nil, fmt.Errorf("updater run failed: %w", err)
		}
	case models.WebhookTargetTypeGitOps:
		if _, err := s.gitOpsSyncService.PerformSync(ctx, wh.EnvironmentID, wh.TargetID, systemUser); err != nil {
			s.logWebhookEvent(ctx, wh, models.EventSeverityError, fmt.Sprintf("gitops sync failed: %s", err))
			return nil, fmt.Errorf("gitops sync failed: %w", err)
		}
	default:
		return nil, ErrWebhookInvalidType
	}

	// Record trigger time — best-effort, do not fail the request if this update fails.
	now := time.Now()
	_ = s.db.WithContext(ctx).Model(wh).Update("last_triggered_at", now).Error //nolint:errcheck

	s.logWebhookEvent(ctx, wh, models.EventSeveritySuccess, "")

	return result, nil
}

func (s *WebhookService) logWebhookEvent(ctx context.Context, wh *models.Webhook, severity models.EventSeverity, errMsg string) {
	if s.eventService == nil {
		return
	}
	title := fmt.Sprintf("Webhook triggered: %s", wh.Name)
	if severity == models.EventSeverityError {
		title = fmt.Sprintf("Webhook trigger failed: %s", wh.Name)
	}
	description := fmt.Sprintf("Target type: %s", wh.TargetType)
	if errMsg != "" {
		description = errMsg
	}
	resourceType := "webhook"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeWebhookTrigger,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceID:    &wh.ID,
		ResourceName:  &wh.Name,
		EnvironmentID: &wh.EnvironmentID,
		Metadata: models.JSON{
			"targetType":  wh.TargetType,
			"targetId":    wh.TargetID,
			"tokenPrefix": wh.TokenPrefix,
		},
	})
}
