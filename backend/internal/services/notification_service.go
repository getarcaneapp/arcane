package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/resources"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/notification"
	"github.com/getarcaneapp/arcane/types/system"
)

const (
	logoURLPath = "/api/app-images/logo-email"

	notificationTestTypeSimple           = "simple"
	notificationTestTypeImageUpdate      = "image-update"
	notificationTestTypeBatchImageUpdate = "batch-image-update"
	notificationTestTypeVulnerability    = "vulnerability-found"
	notificationTestTypePruneReport      = "prune-report"
	notificationTestTypeAutoHeal         = "auto-heal"
)

var supportedNotificationTestTypes = map[string]struct{}{
	notificationTestTypeSimple:           {},
	notificationTestTypeImageUpdate:      {},
	notificationTestTypeBatchImageUpdate: {},
	notificationTestTypeVulnerability:    {},
	notificationTestTypePruneReport:      {},
	notificationTestTypeAutoHeal:         {},
}

var ErrUnauthorizedNotificationDispatch = errors.New("unauthorized notification dispatch")
var ErrUnsupportedDispatchKind = errors.New("unsupported notification dispatch kind")

// VulnerabilityNotificationPayload is the data sent to all providers for vulnerability_found events.
// Only vulnerabilities with a fixed version should trigger this notification.
type VulnerabilityNotificationPayload struct {
	CVEID            string // e.g. CVE-2024-1234
	CVELink          string // e.g. https://nvd.nist.gov/vuln/detail/CVE-2024-1234
	Severity         string // CRITICAL, HIGH, MEDIUM, LOW, UNKNOWN
	ImageName        string // e.g. nginx:latest
	FixedVersion     string
	PkgName          string // optional
	InstalledVersion string // optional
}

type NotificationService struct {
	db             *database.DB
	config         *config.Config
	environmentSvc *EnvironmentService
	appriseService *AppriseService
	httpClient     *http.Client
}

type NotificationTarget struct {
	EnvironmentID   string
	EnvironmentName string
}

func logManagerDispatchNotificationInternal(ctx context.Context, target NotificationTarget, kind notificationdto.DispatchKind) {
	slog.InfoContext(ctx,
		"Manager dispatching notification on behalf of agent",
		"environment_id", target.EnvironmentID,
		"environment_name", target.EnvironmentName,
		"kind", string(kind),
	)
}

func (s *NotificationService) ResolveNotificationTarget(ctx context.Context, environmentID string) (NotificationTarget, error) {
	return s.resolveNotificationTargetInternal(ctx, environmentID)
}

func NewNotificationService(db *database.DB, cfg *config.Config, environmentSvc *EnvironmentService) *NotificationService {
	return &NotificationService{
		db:             db,
		config:         cfg,
		environmentSvc: environmentSvc,
		appriseService: NewAppriseService(db, cfg),
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *NotificationService) resolveNotificationTargetInternal(ctx context.Context, environmentID string) (NotificationTarget, error) {
	trimmedEnvironmentID := strings.TrimSpace(environmentID)
	if trimmedEnvironmentID == "" {
		trimmedEnvironmentID = "0"
	}

	if s.environmentSvc != nil {
		env, err := s.environmentSvc.GetEnvironmentByID(ctx, trimmedEnvironmentID)
		if err == nil && env != nil {
			environmentName := strings.TrimSpace(env.Name)
			if environmentName == "" && trimmedEnvironmentID == "0" {
				environmentName = "Local Docker"
			}
			return NotificationTarget{
				EnvironmentID:   env.ID,
				EnvironmentName: environmentName,
			}, nil
		}
		if trimmedEnvironmentID != "0" {
			return NotificationTarget{}, fmt.Errorf("failed to resolve notification environment: %w", err)
		}
		if err != nil {
			slog.WarnContext(ctx, "Failed to resolve local environment, falling back to 'Local Docker'", "error", err)
		}
	}

	return NotificationTarget{
		EnvironmentID:   "0",
		EnvironmentName: "Local Docker",
	}, nil
}

func (s *NotificationService) resolveNotificationTargetForAccessTokenInternal(ctx context.Context, accessToken string) (NotificationTarget, error) {
	if s.environmentSvc == nil {
		return NotificationTarget{}, fmt.Errorf("environment service not initialized")
	}

	env, err := s.environmentSvc.ResolveEnvironmentByAccessToken(ctx, accessToken)
	if err != nil {
		if errors.Is(err, ErrEnvironmentAccessTokenRequired) || errors.Is(err, ErrInvalidEnvironmentAccessToken) {
			return NotificationTarget{}, fmt.Errorf("%w", ErrUnauthorizedNotificationDispatch)
		}
		return NotificationTarget{}, err
	}

	environmentName := strings.TrimSpace(env.Name)
	if environmentName == "" && env.ID == "0" {
		environmentName = "Local Docker"
	}

	return NotificationTarget{
		EnvironmentID:   env.ID,
		EnvironmentName: environmentName,
	}, nil
}

func (s *NotificationService) dispatchNotificationToManagerInternal(ctx context.Context, payload notificationdto.DispatchRequest) error {
	if s.config == nil || strings.TrimSpace(s.config.GetManagerBaseURL()) == "" {
		return fmt.Errorf("manager API URL is required for notification dispatch")
	}
	if strings.TrimSpace(s.config.AgentToken) == "" {
		return fmt.Errorf("agent token is required for notification dispatch")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification dispatch payload: %w", err)
	}

	dispatchURL := strings.TrimRight(s.config.GetManagerBaseURL(), "/") + "/api/notifications/dispatch"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dispatchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create notification dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.config.AgentToken)

	resp, err := s.httpClient.Do(req) //nolint:gosec // dispatch URL is derived from validated manager configuration
	if err != nil {
		return fmt.Errorf("failed to dispatch notification to manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	responseBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("manager notification dispatch failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

func (s *NotificationService) DispatchNotification(ctx context.Context, accessToken string, payload notificationdto.DispatchRequest) error {
	if s.config != nil && s.config.AgentMode {
		return fmt.Errorf("notification dispatch is manager-only")
	}

	target, err := s.resolveNotificationTargetForAccessTokenInternal(ctx, accessToken)
	if err != nil {
		return err
	}

	switch payload.Kind {
	case notificationdto.DispatchKindImageUpdate:
		if payload.ImageUpdate == nil {
			return fmt.Errorf("image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendImageUpdateNotificationForTargetInternal(ctx, target, payload.ImageUpdate.ImageRef, &payload.ImageUpdate.UpdateInfo, models.NotificationEventImageUpdate)
	case notificationdto.DispatchKindBatchImageUpdate:
		if payload.BatchImageUpdate == nil {
			return fmt.Errorf("batch image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendBatchImageUpdateNotificationForTargetInternal(ctx, target, payload.BatchImageUpdate.Updates)
	case notificationdto.DispatchKindContainerUpdate:
		if payload.ContainerUpdate == nil {
			return fmt.Errorf("container update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendContainerUpdateNotificationForTargetInternal(ctx, target, payload.ContainerUpdate.ContainerName, payload.ContainerUpdate.ImageRef, payload.ContainerUpdate.OldDigest, payload.ContainerUpdate.NewDigest)
	case notificationdto.DispatchKindVulnerabilityFound:
		if payload.VulnerabilityFound == nil {
			return fmt.Errorf("vulnerability payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendVulnerabilityNotificationForTargetInternal(ctx, target, VulnerabilityNotificationPayload{
			CVEID:            payload.VulnerabilityFound.CVEID,
			CVELink:          payload.VulnerabilityFound.CVELink,
			Severity:         payload.VulnerabilityFound.Severity,
			ImageName:        payload.VulnerabilityFound.ImageName,
			FixedVersion:     payload.VulnerabilityFound.FixedVersion,
			PkgName:          payload.VulnerabilityFound.PkgName,
			InstalledVersion: payload.VulnerabilityFound.InstalledVersion,
		})
	case notificationdto.DispatchKindPruneReport:
		if payload.PruneReport == nil {
			return fmt.Errorf("prune report payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendPruneReportNotificationForTargetInternal(ctx, target, &payload.PruneReport.Result)
	case notificationdto.DispatchKindAutoHeal:
		if payload.AutoHeal == nil {
			return fmt.Errorf("auto-heal payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendAutoHealNotificationForTargetInternal(ctx, target, payload.AutoHeal.ContainerName, payload.AutoHeal.ContainerID)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedDispatchKind, payload.Kind)
	}
}

func (s *NotificationService) GetAllSettings(ctx context.Context) ([]models.NotificationSettings, error) {
	var settings []models.NotificationSettings
	if err := s.db.WithContext(ctx).Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("failed to get notification settings: %w", err)
	}
	return settings, nil
}

func (s *NotificationService) GetSettingsByProvider(ctx context.Context, provider models.NotificationProvider) (*models.NotificationSettings, error) {
	var setting models.NotificationSettings
	if err := s.db.WithContext(ctx).Where("provider = ?", provider).First(&setting).Error; err != nil {
		return nil, err
	}
	return &setting, nil
}

func (s *NotificationService) CreateOrUpdateSettings(ctx context.Context, provider models.NotificationProvider, enabled bool, config models.JSON) (*models.NotificationSettings, error) {
	var setting models.NotificationSettings

	// Clear config if provider is disabled
	if !enabled {
		config = models.JSON{}
	}

	err := s.db.WithContext(ctx).Where("provider = ?", provider).First(&setting).Error
	if err != nil {
		setting = models.NotificationSettings{
			Provider: provider,
			Enabled:  enabled,
			Config:   config,
		}
		if err := s.db.WithContext(ctx).Create(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to create notification settings: %w", err)
		}
	} else {
		setting.Enabled = enabled
		setting.Config = config
		if err := s.db.WithContext(ctx).Save(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to update notification settings: %w", err)
		}
	}

	return &setting, nil
}

func (s *NotificationService) DeleteSettings(ctx context.Context, provider models.NotificationProvider) error {
	if err := s.db.WithContext(ctx).Where("provider = ?", provider).Delete(&models.NotificationSettings{}).Error; err != nil {
		return fmt.Errorf("failed to delete notification settings: %w", err)
	}
	return nil
}

func (s *NotificationService) SendImageUpdateNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response, eventType models.NotificationEventType) error {
	if updateInfo == nil {
		return fmt.Errorf("updateInfo is required")
	}

	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindImageUpdate,
			ImageUpdate: &notificationdto.DispatchImageUpdate{
				ImageRef:   imageRef,
				UpdateInfo: *updateInfo,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendImageUpdateNotificationForTargetInternal(ctx, target, imageRef, updateInfo, eventType)
}

func (s *NotificationService) sendImageUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, imageRef string, updateInfo *imageupdate.Response, eventType models.NotificationEventType) error {
	// Send to Apprise if enabled (don't block on error)
	if appriseErr := s.appriseService.SendImageUpdateNotification(ctx, target.EnvironmentName, imageRef, updateInfo); appriseErr != nil {
		slog.WarnContext(ctx, "Failed to send Apprise notification", "error", appriseErr)
	}

	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errors []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}

		// Check if this event type is enabled for this provider
		if !s.isEventEnabled(setting.Config, eventType) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendDiscordNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendTelegramNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendSignalNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendSlackNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendNtfyNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendPushoverNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendGotifyNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendMatrixNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendGenericNotification(ctx, target.EnvironmentName, imageRef, updateInfo, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errors = append(errors, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		s.logNotification(ctx, setting.Provider, imageRef, status, errMsg, models.JSON{
			"hasUpdate":     updateInfo.HasUpdate,
			"currentDigest": updateInfo.CurrentDigest,
			"latestDigest":  updateInfo.LatestDigest,
			"updateType":    updateInfo.UpdateType,
			"eventType":     string(eventType),
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// isEventEnabled checks if a specific event type is enabled in the config
func (s *NotificationService) isEventEnabled(config models.JSON, eventType models.NotificationEventType) bool {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return true // Default to enabled if we can't parse
	}

	var configMap map[string]any
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		return true // Default to enabled if we can't parse
	}

	events, ok := configMap["events"].(map[string]any)
	if !ok {
		return true // If no events config, default to enabled
	}

	enabled, ok := events[string(eventType)].(bool)
	if !ok {
		return true // If event type not specified, default to enabled
	}

	return enabled
}

func (s *NotificationService) SendContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string) error {
	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindContainerUpdate,
			ContainerUpdate: &notificationdto.DispatchContainerUpdate{
				ContainerName: containerName,
				ImageRef:      imageRef,
				OldDigest:     oldDigest,
				NewDigest:     newDigest,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendContainerUpdateNotificationForTargetInternal(ctx, target, containerName, imageRef, oldDigest, newDigest)
}

func (s *NotificationService) sendContainerUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, containerName, imageRef, oldDigest, newDigest string) error {
	// Send to Apprise if enabled (don't block on error)
	if appriseErr := s.appriseService.SendContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest); appriseErr != nil {
		slog.WarnContext(ctx, "Failed to send Apprise notification", "error", appriseErr)
	}

	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errors []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}

		// Check if container update event is enabled for this provider
		if !s.isEventEnabled(setting.Config, models.NotificationEventContainerUpdate) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendDiscordContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendTelegramContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendSignalContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendSlackContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendNtfyContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendPushoverContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendGotifyContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendMatrixContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendGenericContainerUpdateNotification(ctx, target.EnvironmentName, containerName, imageRef, oldDigest, newDigest, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errors = append(errors, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		s.logNotification(ctx, setting.Provider, imageRef, status, errMsg, models.JSON{
			"containerName": containerName,
			"oldDigest":     oldDigest,
			"newDigest":     newDigest,
			"eventType":     string(models.NotificationEventContainerUpdate),
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

func isVulnerabilitySummaryPayload(payload VulnerabilityNotificationPayload) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(payload.CVEID)), "DAILY SUMMARY")
}

// SendVulnerabilityNotification notifies all enabled providers that have vulnerability_found event enabled.
// Only daily summary payloads are sent; legacy per-CVE payloads are ignored.
func (s *NotificationService) SendVulnerabilityNotification(ctx context.Context, payload VulnerabilityNotificationPayload) error {
	if !isVulnerabilitySummaryPayload(payload) {
		slog.InfoContext(ctx, "skipping legacy individual vulnerability notification payload", "cve", payload.CVEID)
		return nil
	}

	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindVulnerabilityFound,
			VulnerabilityFound: &notificationdto.DispatchVulnerabilityFound{
				CVEID:            payload.CVEID,
				CVELink:          payload.CVELink,
				Severity:         payload.Severity,
				ImageName:        payload.ImageName,
				FixedVersion:     payload.FixedVersion,
				PkgName:          payload.PkgName,
				InstalledVersion: payload.InstalledVersion,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendVulnerabilityNotificationForTargetInternal(ctx, target, payload)
}

func (s *NotificationService) sendVulnerabilityNotificationForTargetInternal(ctx context.Context, target NotificationTarget, payload VulnerabilityNotificationPayload) error {
	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errors []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}
		if !s.isEventEnabled(setting.Config, models.NotificationEventVulnerabilityFound) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendDiscordVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendTelegramVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendSignalVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendSlackVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendNtfyVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendPushoverVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendGotifyVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendMatrixVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendGenericVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errors = append(errors, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		s.logNotification(ctx, setting.Provider, payload.ImageName, status, errMsg, models.JSON{
			"cveId":        payload.CVEID,
			"severity":     payload.Severity,
			"fixedVersion": payload.FixedVersion,
			"eventType":    string(models.NotificationEventVulnerabilityFound),
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}
	return nil
}

func (s *NotificationService) sendDiscordNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Discord config: %w", err)
	}

	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return fmt.Errorf("discord webhook ID or token not configured")
	}

	// Decrypt token if encrypted
	if discordConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(discordConfig.Token); err == nil {
			discordConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Discord token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatMarkdown,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendDiscord(ctx, discordConfig, message); err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendTelegramNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Telegram config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &telegramConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Telegram config: %w", err)
	}

	if telegramConfig.BotToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return fmt.Errorf("no telegram chat IDs configured")
	}

	// Decrypt bot token if encrypted
	if telegramConfig.BotToken != "" {
		if decrypted, err := crypto.Decrypt(telegramConfig.BotToken); err == nil {
			telegramConfig.BotToken = decrypted
		} else {
			slog.Warn("Failed to decrypt Telegram bot token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatHTML,
		environmentName,
		imageRef,
		updateInfo,
	)

	// Set parse mode to HTML if not already set
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}

	if err := notifications.SendTelegram(ctx, telegramConfig, message); err != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendEmailNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var emailConfig models.EmailConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal email config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &emailConfig); err != nil {
		return fmt.Errorf("failed to unmarshal email config: %w", err)
	}

	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if emailConfig.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		} else {
			slog.Warn("Failed to decrypt email SMTP password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	htmlBody, _, err := s.renderEmailTemplate(environmentName, imageRef, updateInfo)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Container Update Available: %s", notifications.SanitizeForEmail(imageRef)))
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderEmailTemplate(environmentName, imageRef string, updateInfo *imageupdate.Response) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   environmentName,
		"ImageRef":      imageRef,
		"HasUpdate":     updateInfo.HasUpdate,
		"UpdateType":    updateInfo.UpdateType,
		"CurrentDigest": updateInfo.CurrentDigest,
		"LatestDigest":  updateInfo.LatestDigest,
		"CheckTime":     updateInfo.CheckTime.Format(time.RFC1123),
	}

	htmlContent, err := resources.FS.ReadFile("email-templates/image-update_html.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile("email-templates/image-update_text.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read text template: %w", err)
	}

	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

func (s *NotificationService) sendDiscordContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Discord config: %w", err)
	}

	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return fmt.Errorf("discord webhook ID or token not configured")
	}

	// Decrypt token if encrypted
	if discordConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(discordConfig.Token); err == nil {
			discordConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Discord token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatMarkdown,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendDiscord(ctx, discordConfig, message); err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendTelegramContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Telegram config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &telegramConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Telegram config: %w", err)
	}

	if telegramConfig.BotToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return fmt.Errorf("no telegram chat IDs configured")
	}

	// Decrypt bot token if encrypted
	if telegramConfig.BotToken != "" {
		if decrypted, err := crypto.Decrypt(telegramConfig.BotToken); err == nil {
			telegramConfig.BotToken = decrypted
		} else {
			slog.Warn("Failed to decrypt Telegram bot token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatHTML,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	// Set parse mode to HTML if not already set
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}

	if err := notifications.SendTelegram(ctx, telegramConfig, message); err != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendEmailContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var emailConfig models.EmailConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal email config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &emailConfig); err != nil {
		return fmt.Errorf("failed to unmarshal email config: %w", err)
	}

	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if emailConfig.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		} else {
			slog.Warn("Failed to decrypt email SMTP password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	htmlBody, _, err := s.renderContainerUpdateEmailTemplate(environmentName, containerName, imageRef, oldDigest, newDigest)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Container Updated: %s", notifications.SanitizeForEmail(containerName)))
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderContainerUpdateEmailTemplate(environmentName, containerName, imageRef, oldDigest, newDigest string) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   environmentName,
		"ContainerName": containerName,
		"ImageRef":      imageRef,
		"OldDigest":     oldDigest,
		"NewDigest":     newDigest,
		"UpdateTime":    time.Now().Format(time.RFC1123),
	}

	htmlContent, err := resources.FS.ReadFile("email-templates/container-update_html.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile("email-templates/container-update_text.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read text template: %w", err)
	}

	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

func (s *NotificationService) TestNotification(ctx context.Context, environmentID string, provider models.NotificationProvider, testType string) error {
	setting, err := s.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return fmt.Errorf("please save your %s settings before testing", provider)
	}
	testType = strings.TrimSpace(testType)
	if testType == "" {
		testType = notificationTestTypeSimple
	}
	if _, ok := supportedNotificationTestTypes[testType]; !ok {
		return fmt.Errorf("unsupported notification test type: %s", testType)
	}

	target, err := s.resolveNotificationTargetInternal(ctx, environmentID)
	if err != nil {
		return err
	}

	// Test vulnerability notification (all providers)
	if testType == notificationTestTypeVulnerability {
		payload := VulnerabilityNotificationPayload{
			CVEID:        fmt.Sprintf("Daily Summary - %s", time.Now().UTC().Format("2006-01-02")),
			Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
			ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
			FixedVersion: "7 fixable vulnerability record(s)",
			PkgName:      "CVE-2025-1234, CVE-2025-5678, CVE-2026-0001",
		}
		switch provider {
		case models.NotificationProviderDiscord:
			return s.sendDiscordVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderEmail:
			return s.sendEmailVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderTelegram:
			return s.sendTelegramVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderSignal:
			return s.sendSignalVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderSlack:
			return s.sendSlackVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderNtfy:
			return s.sendNtfyVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderPushover:
			return s.sendPushoverVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderGotify:
			return s.sendGotifyVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderMatrix:
			return s.sendMatrixVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		case models.NotificationProviderGeneric:
			return s.sendGenericVulnerabilityNotification(ctx, target.EnvironmentName, payload, setting.Config)
		default:
			return fmt.Errorf("unknown provider: %s", provider)
		}
	}

	if testType == notificationTestTypeAutoHeal {
		testContainerName := "test-container"
		switch provider {
		case models.NotificationProviderDiscord:
			return s.sendDiscordAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderEmail:
			return s.sendEmailAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderTelegram:
			return s.sendTelegramAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderSignal:
			return s.sendSignalAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderSlack:
			return s.sendSlackAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderNtfy:
			return s.sendNtfyAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderPushover:
			return s.sendPushoverAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderGotify:
			return s.sendGotifyAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderMatrix:
			return s.sendMatrixAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		case models.NotificationProviderGeneric:
			return s.sendGenericAutoHealNotification(ctx, target.EnvironmentName, testContainerName, setting.Config)
		default:
			return fmt.Errorf("unknown provider: %s", provider)
		}
	}

	if testType == notificationTestTypePruneReport {
		result := &system.PruneAllResult{
			Success:                  true,
			ContainersPruned:         []string{"a1b2c3d4e5f6", "f6e5d4c3b2a1"},
			ImagesDeleted:            []string{"sha256:1111111111111111111111111111111111111111111111111111111111111111"},
			VolumesDeleted:           []string{"arcane_test_volume"},
			NetworksDeleted:          []string{"arcane_test_network"},
			SpaceReclaimed:           3825205248,
			ContainerSpaceReclaimed:  503316480,
			ImageSpaceReclaimed:      2449473536,
			VolumeSpaceReclaimed:     641728512,
			BuildCacheSpaceReclaimed: 230162432,
			Errors:                   []string{},
		}

		switch provider {
		case models.NotificationProviderDiscord:
			return s.sendDiscordPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderEmail:
			return s.sendEmailPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderTelegram:
			return s.sendTelegramPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderSignal:
			return s.sendSignalPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderSlack:
			return s.sendSlackPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderNtfy:
			return s.sendNtfyPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderPushover:
			return s.sendPushoverPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderGotify:
			return s.sendGotifyPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderMatrix:
			return s.sendMatrixPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderGeneric:
			return s.sendGenericPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		default:
			return fmt.Errorf("unknown provider: %s", provider)
		}
	}

	testUpdate := &imageupdate.Response{
		HasUpdate:      true,
		UpdateType:     "digest",
		CurrentDigest:  "sha256:abc123def456789012345678901234567890",
		LatestDigest:   "sha256:xyz789ghi012345678901234567890123456",
		CheckTime:      time.Now(),
		ResponseTimeMs: 100,
	}

	if testType == notificationTestTypeBatchImageUpdate {
		// Create test batch updates with multiple images
		testUpdates := map[string]*imageupdate.Response{
			"nginx:latest": {
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentDigest:  "sha256:abc123def456789012345678901234567890",
				LatestDigest:   "sha256:xyz789ghi012345678901234567890123456",
				CheckTime:      time.Now(),
				ResponseTimeMs: 100,
			},
			"postgres:16-alpine": {
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentDigest:  "sha256:def456abc123789012345678901234567890",
				LatestDigest:   "sha256:ghi789xyz012345678901234567890123456",
				CheckTime:      time.Now(),
				ResponseTimeMs: 120,
			},
			"redis:7.2-alpine": {
				HasUpdate:      true,
				UpdateType:     "digest",
				CurrentDigest:  "sha256:123456789abc012345678901234567890def",
				LatestDigest:   "sha256:456789012def345678901234567890123abc",
				CheckTime:      time.Now(),
				ResponseTimeMs: 95,
			},
		}
		switch provider {
		case models.NotificationProviderDiscord:
			return s.sendBatchDiscordNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderEmail:
			return s.sendBatchEmailNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderTelegram:
			return s.sendBatchTelegramNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderSignal:
			return s.sendBatchSignalNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderSlack:
			return s.sendBatchSlackNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderNtfy:
			return s.sendBatchNtfyNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderPushover:
			return s.sendBatchPushoverNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderGotify:
			return s.sendBatchGotifyNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderMatrix:
			return s.sendBatchMatrixNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		case models.NotificationProviderGeneric:
			return s.sendBatchGenericNotification(ctx, target.EnvironmentName, testUpdates, setting.Config)
		default:
			return fmt.Errorf("unknown provider: %s", provider)
		}
	}

	imageRef := "nginx:latest"
	if testType == notificationTestTypeSimple {
		imageRef = "test/image:latest"
	}

	switch provider {
	case models.NotificationProviderDiscord:
		return s.sendDiscordNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderEmail:
		if testType == notificationTestTypeSimple {
			return s.sendTestEmail(ctx, target.EnvironmentName, setting.Config)
		}
		return s.sendEmailNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderTelegram:
		return s.sendTelegramNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderSignal:
		return s.sendSignalNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderSlack:
		return s.sendSlackNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderNtfy:
		return s.sendNtfyNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderPushover:
		return s.sendPushoverNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderGotify:
		return s.sendGotifyNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderMatrix:
		return s.sendMatrixNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	case models.NotificationProviderGeneric:
		return s.sendGenericNotification(ctx, target.EnvironmentName, imageRef, testUpdate, setting.Config)
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
}

func (s *NotificationService) sendTestEmail(ctx context.Context, environmentName string, config models.JSON) error {
	var emailConfig models.EmailConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal email config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &emailConfig); err != nil {
		return fmt.Errorf("failed to unmarshal email config: %w", err)
	}

	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if emailConfig.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		}
	}

	htmlBody, _, err := s.renderTestEmailTemplate(environmentName)
	if err != nil {
		return fmt.Errorf("failed to render test email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, "Test Email from Arcane")
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderTestEmailTemplate(environmentName string) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
		"Environment": environmentName,
	}

	htmlContent, err := resources.FS.ReadFile("email-templates/test_html.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile("email-templates/test_text.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read text template: %w", err)
	}

	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

func (s *NotificationService) logNotification(ctx context.Context, provider models.NotificationProvider, imageRef, status string, errMsg *string, metadata models.JSON) {
	log := &models.NotificationLog{
		Provider: provider,
		ImageRef: imageRef,
		Status:   status,
		Error:    errMsg,
		Metadata: metadata,
		SentAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(log).Error; err != nil {
		slog.WarnContext(ctx, "Failed to log notification", "provider", string(provider), "error", err.Error())
	}
}

func (s *NotificationService) SendBatchImageUpdateNotification(ctx context.Context, updates map[string]*imageupdate.Response) error {
	updatesWithChanges := filterUpdatesWithChangesInternal(updates)
	if len(updatesWithChanges) == 0 {
		return nil
	}

	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindBatchImageUpdate,
			BatchImageUpdate: &notificationdto.DispatchBatchImageUpdate{
				Updates: updatesWithChanges,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendBatchImageUpdateNotificationForTargetInternal(ctx, target, updatesWithChanges)
}

func filterUpdatesWithChangesInternal(updates map[string]*imageupdate.Response) map[string]*imageupdate.Response {
	updatesWithChanges := make(map[string]*imageupdate.Response, len(updates))
	for imageRef, update := range updates {
		if update != nil && update.HasUpdate {
			updatesWithChanges[imageRef] = update
		}
	}
	return updatesWithChanges
}

func (s *NotificationService) sendBatchImageUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, updates map[string]*imageupdate.Response) error {
	updatesWithChanges := filterUpdatesWithChangesInternal(updates)

	if len(updatesWithChanges) == 0 {
		return nil
	}

	// Send to Apprise if enabled
	if appriseErr := s.appriseService.SendBatchImageUpdateNotification(ctx, target.EnvironmentName, updatesWithChanges); appriseErr != nil {
		slog.WarnContext(ctx, "Failed to send Apprise notification", "error", appriseErr)
	}

	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errors []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}

		if !s.isEventEnabled(setting.Config, models.NotificationEventImageUpdate) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendBatchDiscordNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendBatchEmailNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendBatchTelegramNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendBatchSignalNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendBatchSlackNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendBatchNtfyNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendBatchPushoverNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendBatchGotifyNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendBatchMatrixNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendBatchGenericNotification(ctx, target.EnvironmentName, updatesWithChanges, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errors = append(errors, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		imageRefs := make([]string, 0, len(updatesWithChanges))
		for ref := range updatesWithChanges {
			imageRefs = append(imageRefs, ref)
		}

		s.logNotification(ctx, setting.Provider, strings.Join(imageRefs, ", "), status, errMsg, models.JSON{
			"updateCount": len(updatesWithChanges),
			"eventType":   string(models.NotificationEventImageUpdate),
			"batch":       true,
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

func (s *NotificationService) sendBatchDiscordNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal discord config: %w", err)
	}

	// Decrypt token if encrypted
	if decrypted, err := crypto.Decrypt(discordConfig.Token); err == nil {
		discordConfig.Token = decrypted
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatMarkdown,
		environmentName,
		updates,
	)

	if err := notifications.SendDiscord(ctx, discordConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Discord notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchTelegramNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &telegramConfig); err != nil {
		return fmt.Errorf("failed to unmarshal telegram config: %w", err)
	}

	// Decrypt bot token if encrypted
	if decrypted, err := crypto.Decrypt(telegramConfig.BotToken); err == nil {
		telegramConfig.BotToken = decrypted
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatHTML,
		environmentName,
		updates,
	)

	// Set parse mode to HTML if not already set
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}

	if err := notifications.SendTelegram(ctx, telegramConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Telegram notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchEmailNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var emailConfig models.EmailConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal email config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &emailConfig); err != nil {
		return fmt.Errorf("failed to unmarshal email config: %w", err)
	}

	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if emailConfig.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		} else {
			slog.Warn("Failed to decrypt email SMTP password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	htmlBody, _, err := s.renderBatchEmailTemplate(environmentName, updates)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	updateCount := len(updates)
	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("%d Image Update%s Available", updateCount, func() string {
		if updateCount > 1 {
			return "s"
		}
		return ""
	}()))
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderBatchEmailTemplate(environmentName string, updates map[string]*imageupdate.Response) (string, string, error) {
	// Build list of image names
	imageList := make([]string, 0, len(updates))
	for imageRef := range updates {
		imageList = append(imageList, imageRef)
	}

	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
		"Environment": environmentName,
		"UpdateCount": len(updates),
		"CheckTime":   time.Now().Format(time.RFC1123),
		"ImageList":   imageList,
	}

	htmlContent, err := resources.FS.ReadFile("email-templates/batch-image-updates_html.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile("email-templates/batch-image-updates_text.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read text template: %w", err)
	}

	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text template: %w", err)
	}

	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute text template: %w", err)
	}

	return htmlBuf.String(), textBuf.String(), nil
}

func (s *NotificationService) sendSignalNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var signalConfig models.SignalConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Signal config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &signalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Signal config: %w", err)
	}

	if signalConfig.Host == "" {
		return fmt.Errorf("signal host not configured")
	}
	if signalConfig.Port == 0 {
		return fmt.Errorf("signal port not configured")
	}
	if signalConfig.Source == "" {
		return fmt.Errorf("signal source phone number not configured")
	}
	if len(signalConfig.Recipients) == 0 {
		return fmt.Errorf("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return fmt.Errorf("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return fmt.Errorf("signal cannot use both basic auth and token authentication simultaneously")
	}

	// Decrypt sensitive fields if encrypted
	if signalConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Password); err == nil {
			signalConfig.Password = decrypted
		} else {
			slog.Warn("Failed to decrypt Signal password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	if signalConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Token); err == nil {
			signalConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Signal token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendSignal(ctx, signalConfig, message); err != nil {
		return fmt.Errorf("failed to send Signal notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendSignalContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var signalConfig models.SignalConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Signal config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &signalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Signal config: %w", err)
	}

	if signalConfig.Host == "" {
		return fmt.Errorf("signal host not configured")
	}
	if signalConfig.Port == 0 {
		return fmt.Errorf("signal port not configured")
	}
	if signalConfig.Source == "" {
		return fmt.Errorf("signal source phone number not configured")
	}
	if len(signalConfig.Recipients) == 0 {
		return fmt.Errorf("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return fmt.Errorf("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return fmt.Errorf("signal cannot use both basic auth and token authentication simultaneously")
	}

	// Decrypt sensitive fields if encrypted
	if signalConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Password); err == nil {
			signalConfig.Password = decrypted
		} else {
			slog.Warn("Failed to decrypt Signal password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	if signalConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Token); err == nil {
			signalConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Signal token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendSignal(ctx, signalConfig, message); err != nil {
		return fmt.Errorf("failed to send Signal notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchSignalNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var signalConfig models.SignalConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal signal config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &signalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal signal config: %w", err)
	}

	// Validate authentication
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return fmt.Errorf("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return fmt.Errorf("signal cannot use both basic auth and token authentication simultaneously")
	}

	// Decrypt sensitive fields if encrypted
	if signalConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Password); err == nil {
			signalConfig.Password = decrypted
		}
	}
	if signalConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Token); err == nil {
			signalConfig.Token = decrypted
		}
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendSignal(ctx, signalConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Signal notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendSlackNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var slackConfig models.SlackConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &slackConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Slack config: %w", err)
	}

	if slackConfig.Token == "" {
		return fmt.Errorf("slack token not configured")
	}

	// Decrypt token if encrypted
	if slackConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(slackConfig.Token); err == nil {
			slackConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Slack token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatSlack,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendSlack(ctx, slackConfig, message); err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendSlackContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var slackConfig models.SlackConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &slackConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Slack config: %w", err)
	}

	if slackConfig.Token == "" {
		return fmt.Errorf("slack token not configured")
	}

	// Decrypt token if encrypted
	if slackConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(slackConfig.Token); err == nil {
			slackConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Slack token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatSlack,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendSlack(ctx, slackConfig, message); err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchSlackNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var slackConfig models.SlackConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal slack config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &slackConfig); err != nil {
		return fmt.Errorf("failed to unmarshal slack config: %w", err)
	}

	// Decrypt token if encrypted
	if decrypted, err := crypto.Decrypt(slackConfig.Token); err == nil {
		slackConfig.Token = decrypted
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatSlack,
		environmentName,
		updates,
	)

	if err := notifications.SendSlack(ctx, slackConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Slack notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendNtfyNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Ntfy config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &ntfyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Ntfy config: %w", err)
	}

	if ntfyConfig.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}

	// Decrypt password if encrypted
	if ntfyConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(ntfyConfig.Password); err == nil {
			ntfyConfig.Password = decrypted
		} else {
			slog.Warn("Failed to decrypt Ntfy password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendNtfy(ctx, ntfyConfig, message); err != nil {
		return fmt.Errorf("failed to send Ntfy notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendNtfyContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Ntfy config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &ntfyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Ntfy config: %w", err)
	}

	if ntfyConfig.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}

	// Decrypt password if encrypted
	if ntfyConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(ntfyConfig.Password); err == nil {
			ntfyConfig.Password = decrypted
		} else {
			slog.Warn("Failed to decrypt Ntfy password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendNtfy(ctx, ntfyConfig, message); err != nil {
		return fmt.Errorf("failed to send Ntfy notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchNtfyNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal ntfy config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &ntfyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal ntfy config: %w", err)
	}

	// Decrypt password if encrypted
	if ntfyConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(ntfyConfig.Password); err == nil {
			ntfyConfig.Password = decrypted
		}
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendNtfy(ctx, ntfyConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Ntfy notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendPushoverNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Pushover config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &pushoverConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Pushover config: %w", err)
	}

	if pushoverConfig.Token == "" {
		return fmt.Errorf("pushover API token not configured")
	}
	if pushoverConfig.User == "" {
		return fmt.Errorf("pushover user key not configured")
	}

	if pushoverConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(pushoverConfig.Token); err == nil {
			pushoverConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Pushover token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendPushover(ctx, pushoverConfig, message); err != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendPushoverContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Pushover config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &pushoverConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Pushover config: %w", err)
	}

	if pushoverConfig.Token == "" {
		return fmt.Errorf("pushover API token not configured")
	}
	if pushoverConfig.User == "" {
		return fmt.Errorf("pushover user key not configured")
	}

	if pushoverConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(pushoverConfig.Token); err == nil {
			pushoverConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Pushover token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendPushover(ctx, pushoverConfig, message); err != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchPushoverNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal pushover config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &pushoverConfig); err != nil {
		return fmt.Errorf("failed to unmarshal pushover config: %w", err)
	}

	if pushoverConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(pushoverConfig.Token); err == nil {
			pushoverConfig.Token = decrypted
		}
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendPushover(ctx, pushoverConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Pushover notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendGenericNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var genericConfig models.GenericConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Generic config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &genericConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Generic config: %w", err)
	}

	if genericConfig.WebhookURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	title := "Container Image Update"
	if err := notifications.SendGenericWithTitle(ctx, genericConfig, title, message); err != nil {
		return fmt.Errorf("failed to send Generic webhook notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendGenericContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var genericConfig models.GenericConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Generic config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &genericConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Generic config: %w", err)
	}

	if genericConfig.WebhookURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	title := "Container Updated"
	if err := notifications.SendGenericWithTitle(ctx, genericConfig, title, message); err != nil {
		return fmt.Errorf("failed to send Generic webhook notification: %w", err)
	}

	return nil
}

func (s *NotificationService) renderVulnerabilitySummaryEmailTemplate(environmentName string, payload VulnerabilityNotificationPayload) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":           logoURL,
		"AppURL":            appURL,
		"Environment":       environmentName,
		"SummaryLabel":      payload.CVEID,
		"Overview":          payload.ImageName,
		"FixableCount":      payload.FixedVersion,
		"SeverityBreakdown": payload.Severity,
		"SampleCVEs":        payload.PkgName,
	}

	htmlContent, err := resources.FS.ReadFile("email-templates/vulnerability-summary_html.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}
	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile("email-templates/vulnerability-summary_text.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to read text template: %w", err)
	}
	textTmpl, err := template.New("text").Parse(string(textContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text template: %w", err)
	}
	var textBuf bytes.Buffer
	if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute text template: %w", err)
	}
	return htmlBuf.String(), textBuf.String(), nil
}

func (s *NotificationService) sendEmailVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var emailConfig models.EmailConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal email config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &emailConfig); err != nil {
		return fmt.Errorf("failed to unmarshal email config: %w", err)
	}
	if emailConfig.SMTPHost == "" || emailConfig.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}
	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}
	if emailConfig.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		} else {
			slog.Warn("Failed to decrypt email SMTP password, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	htmlBody, _, err := s.renderVulnerabilitySummaryEmailTemplate(environmentName, payload)
	if err != nil {
		return fmt.Errorf("failed to render summary email template: %w", err)
	}
	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Daily Vulnerability Summary: %s", notifications.SanitizeForEmail(payload.CVEID)))
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

func (s *NotificationService) sendDiscordVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Discord config: %w", err)
	}
	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return fmt.Errorf("discord webhook ID or token not configured")
	}
	if discordConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(discordConfig.Token); err == nil {
			discordConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Discord token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatMarkdown,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendDiscord(ctx, discordConfig, message); err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendTelegramVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Telegram config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &telegramConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Telegram config: %w", err)
	}
	if telegramConfig.BotToken == "" {
		return fmt.Errorf("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return fmt.Errorf("no telegram chat IDs configured")
	}
	if telegramConfig.BotToken != "" {
		if decrypted, err := crypto.Decrypt(telegramConfig.BotToken); err == nil {
			telegramConfig.BotToken = decrypted
		} else {
			slog.Warn("Failed to decrypt Telegram bot token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatHTML,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendTelegram(ctx, telegramConfig, message); err != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendSignalVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var signalConfig models.SignalConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Signal config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &signalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Signal config: %w", err)
	}
	if signalConfig.Host == "" || signalConfig.Port == 0 || signalConfig.Source == "" || len(signalConfig.Recipients) == 0 {
		return fmt.Errorf("signal not fully configured")
	}
	if signalConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Password); err == nil {
			signalConfig.Password = decrypted
		}
	}
	if signalConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(signalConfig.Token); err == nil {
			signalConfig.Token = decrypted
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendSignal(ctx, signalConfig, message); err != nil {
		return fmt.Errorf("failed to send Signal notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendSlackVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var slackConfig models.SlackConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &slackConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Slack config: %w", err)
	}
	if slackConfig.Token == "" {
		return fmt.Errorf("slack token not configured")
	}
	if slackConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(slackConfig.Token); err == nil {
			slackConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Slack token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatSlack,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendSlack(ctx, slackConfig, message); err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendNtfyVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Ntfy config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &ntfyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Ntfy config: %w", err)
	}
	if ntfyConfig.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}
	if ntfyConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(ntfyConfig.Password); err == nil {
			ntfyConfig.Password = decrypted
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendNtfy(ctx, ntfyConfig, message); err != nil {
		return fmt.Errorf("failed to send Ntfy notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendPushoverVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Pushover config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &pushoverConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Pushover config: %w", err)
	}
	if pushoverConfig.Token == "" || pushoverConfig.User == "" {
		return fmt.Errorf("pushover token or user not configured")
	}
	if pushoverConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(pushoverConfig.Token); err == nil {
			pushoverConfig.Token = decrypted
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if pushoverConfig.Title == "" {
		pushoverConfig.Title = notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	}
	if err := notifications.SendPushover(ctx, pushoverConfig, message); err != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendGotifyVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Gotify config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &gotifyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Gotify config: %w", err)
	}
	if gotifyConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(gotifyConfig.Token); err == nil {
			gotifyConfig.Token = decrypted
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if gotifyConfig.Title == "" {
		gotifyConfig.Title = notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	}
	if err := notifications.SendGotify(ctx, gotifyConfig, message); err != nil {
		return fmt.Errorf("failed to send Gotify notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendMatrixVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Matrix config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &matrixConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Matrix config: %w", err)
	}
	if matrixConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(matrixConfig.Password); err == nil {
			matrixConfig.Password = decrypted
		}
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	if err := notifications.SendMatrix(ctx, matrixConfig, message); err != nil {
		return fmt.Errorf("failed to send Matrix notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendGenericVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	var genericConfig models.GenericConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Generic config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &genericConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Generic config: %w", err)
	}
	if genericConfig.WebhookURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
	title := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	if err := notifications.SendGenericWithTitle(ctx, genericConfig, title, message); err != nil {
		return fmt.Errorf("failed to send Generic webhook notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendBatchGenericNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var genericConfig models.GenericConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal generic config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &genericConfig); err != nil {
		return fmt.Errorf("failed to unmarshal generic config: %w", err)
	}

	if genericConfig.WebhookURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	title := "Container Image Updates Available"
	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendGenericWithTitle(ctx, genericConfig, title, message); err != nil {
		return fmt.Errorf("failed to send batch Generic webhook notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendGotifyNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Gotify config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &gotifyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Gotify config: %w", err)
	}

	if gotifyConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(gotifyConfig.Token); err == nil {
			gotifyConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Gotify token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendGotify(ctx, gotifyConfig, message); err != nil {
		return fmt.Errorf("failed to send Gotify notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendGotifyContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Gotify config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &gotifyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Gotify config: %w", err)
	}

	if gotifyConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(gotifyConfig.Token); err == nil {
			gotifyConfig.Token = decrypted
		} else {
			slog.Warn("Failed to decrypt Gotify token, using raw value (may be unencrypted legacy value)", "error", err)
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendGotify(ctx, gotifyConfig, message); err != nil {
		return fmt.Errorf("failed to send Gotify notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchGotifyNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal gotify config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &gotifyConfig); err != nil {
		return fmt.Errorf("failed to unmarshal gotify config: %w", err)
	}

	if gotifyConfig.Token != "" {
		if decrypted, err := crypto.Decrypt(gotifyConfig.Token); err == nil {
			gotifyConfig.Token = decrypted
		}
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendGotify(ctx, gotifyConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Gotify notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendMatrixNotification(ctx context.Context, environmentName, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Matrix config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &matrixConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Matrix config: %w", err)
	}

	if matrixConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(matrixConfig.Password); err == nil {
			matrixConfig.Password = decrypted
		}
	}

	message := notifications.BuildImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		imageRef,
		updateInfo,
	)

	if err := notifications.SendMatrix(ctx, matrixConfig, message); err != nil {
		return fmt.Errorf("failed to send Matrix notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendMatrixContainerUpdateNotification(ctx context.Context, environmentName, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Matrix config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &matrixConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Matrix config: %w", err)
	}

	if matrixConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(matrixConfig.Password); err == nil {
			matrixConfig.Password = decrypted
		}
	}

	message := notifications.BuildContainerUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		containerName,
		imageRef,
		oldDigest,
		newDigest,
	)

	if err := notifications.SendMatrix(ctx, matrixConfig, message); err != nil {
		return fmt.Errorf("failed to send Matrix notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendBatchMatrixNotification(ctx context.Context, environmentName string, updates map[string]*imageupdate.Response, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Matrix config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &matrixConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Matrix config: %w", err)
	}

	if matrixConfig.Password != "" {
		if decrypted, err := crypto.Decrypt(matrixConfig.Password); err == nil {
			matrixConfig.Password = decrypted
		}
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(
		notifications.MessageFormatPlain,
		environmentName,
		updates,
	)

	if err := notifications.SendMatrix(ctx, matrixConfig, message); err != nil {
		return fmt.Errorf("failed to send batch Matrix notification: %w", err)
	}

	return nil
}

// MigrateDiscordWebhookUrlToFields migrates legacy Discord webhookUrl to separate webhookId and token fields.
// This should be called during bootstrap to ensure existing Discord configurations are preserved.
func (s *NotificationService) MigrateDiscordWebhookUrlToFields(ctx context.Context) error {
	var setting models.NotificationSettings
	err := s.db.WithContext(ctx).Where("provider = ?", models.NotificationProviderDiscord).First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// No Discord config exists, nothing to migrate
			return nil
		}
		return fmt.Errorf("failed to query Discord settings: %w", err)
	}

	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(setting.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		slog.WarnContext(ctx, "Failed to parse Discord config for migration", "error", err)
		return nil
	}

	// Check if already migrated (has webhookId and token)
	if discordConfig.WebhookID != "" && discordConfig.Token != "" {
		slog.DebugContext(ctx, "Discord config already migrated, skipping")
		return nil
	}

	// Check for legacy webhookUrl field
	var legacyConfig struct {
		WebhookUrl string                                `json:"webhookUrl"`
		Username   string                                `json:"username,omitempty"`
		AvatarURL  string                                `json:"avatarUrl,omitempty"`
		Events     map[models.NotificationEventType]bool `json:"events,omitempty"`
	}
	if err := json.Unmarshal(configBytes, &legacyConfig); err != nil {
		slog.WarnContext(ctx, "Failed to parse legacy Discord config structure", "error", err)
		return nil
	}

	if legacyConfig.WebhookUrl == "" {
		slog.DebugContext(ctx, "No legacy webhookUrl to migrate")
		return nil
	}

	// Parse webhook URL: https://discord.com/api/webhooks/{id}/{token}
	parts := strings.Split(legacyConfig.WebhookUrl, "/webhooks/")
	if len(parts) != 2 {
		slog.WarnContext(ctx, "Invalid Discord webhook URL format, skipping migration", "url", legacyConfig.WebhookUrl)
		return nil
	}

	webhookParts := strings.Split(parts[1], "/")
	if len(webhookParts) != 2 {
		slog.WarnContext(ctx, "Invalid Discord webhook URL format, skipping migration", "url", legacyConfig.WebhookUrl)
		return nil
	}

	webhookID := webhookParts[0]
	token := webhookParts[1]

	slog.InfoContext(ctx, "Migrating legacy Discord webhookUrl to webhookId and token")

	// Encrypt token before storing
	encryptedToken, err := crypto.Encrypt(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt Discord token: %w", err)
	}

	// Update with new structure
	newConfig := models.DiscordConfig{
		WebhookID: webhookID,
		Token:     encryptedToken,
		Username:  legacyConfig.Username,
		AvatarURL: legacyConfig.AvatarURL,
		Events:    legacyConfig.Events,
	}

	var configMap models.JSON
	newConfigBytes, err := json.Marshal(newConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal new Discord config: %w", err)
	}
	if err = json.Unmarshal(newConfigBytes, &configMap); err != nil {
		return fmt.Errorf("failed to unmarshal new Discord config to JSON: %w", err)
	}

	setting.Config = configMap
	if err = s.db.WithContext(ctx).Save(&setting).Error; err != nil {
		return fmt.Errorf("failed to save migrated Discord config: %w", err)
	}

	slog.InfoContext(ctx, "Successfully migrated Discord config")
	return nil
}

func (s *NotificationService) SendPruneReportNotification(ctx context.Context, result *system.PruneAllResult) error {
	hasChanges := pruneResultHasChangesInternal(result)
	hasErrors := result != nil && len(result.Errors) > 0
	if !hasChanges && !hasErrors {
		slog.InfoContext(ctx, "skipping prune report notification because no resources were pruned and no errors were reported")
		return nil
	}

	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindPruneReport,
			PruneReport: &notificationdto.DispatchPruneReport{
				Result: *result,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendPruneReportNotificationForTargetInternal(ctx, target, result)
}

func (s *NotificationService) sendPruneReportNotificationForTargetInternal(ctx context.Context, target NotificationTarget, result *system.PruneAllResult) error {
	hasChanges := pruneResultHasChangesInternal(result)
	hasErrors := result != nil && len(result.Errors) > 0

	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errors []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}

		if !s.isEventEnabled(setting.Config, models.NotificationEventPruneReport) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendDiscordPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendTelegramPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendSignalPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendSlackPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendNtfyPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendPushoverPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendGotifyPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendMatrixPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendGenericPruneNotification(ctx, target.EnvironmentName, result, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errors = append(errors, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		s.logNotification(ctx, setting.Provider, "System Prune Report", status, errMsg, models.JSON{
			"spaceReclaimed": result.SpaceReclaimed,
			"eventType":      string(models.NotificationEventPruneReport),
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}
	if hasErrors && !hasChanges {
		slog.WarnContext(ctx, "sending prune report notification with errors but no resources were pruned", "errorCount", len(result.Errors))
	}

	return nil
}

func pruneResultHasChangesInternal(result *system.PruneAllResult) bool {
	if result == nil {
		return false
	}

	if result.SpaceReclaimed > 0 {
		return true
	}

	return len(result.ContainersPruned) > 0 ||
		len(result.ImagesDeleted) > 0 ||
		len(result.VolumesDeleted) > 0 ||
		len(result.NetworksDeleted) > 0
}

func (s *NotificationService) formatBytesInternal(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (s *NotificationService) sendDiscordPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var discordConfig models.DiscordConfig
	if err := s.unmarshalConfigInternal(config, &discordConfig); err != nil {
		return err
	}

	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return fmt.Errorf("discord webhook ID or token not configured")
	}

	s.decryptDiscordTokenInternal(&discordConfig)

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatMarkdown, environmentName, result)

	if err := notifications.SendDiscord(ctx, discordConfig, message); err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendTelegramPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	if err := s.unmarshalConfigInternal(config, &telegramConfig); err != nil {
		return err
	}

	if telegramConfig.BotToken == "" || len(telegramConfig.ChatIDs) == 0 {
		return fmt.Errorf("telegram bot token or chat IDs not configured")
	}

	s.decryptTelegramTokenInternal(&telegramConfig)

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatHTML, environmentName, result)

	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}

	if err := notifications.SendTelegram(ctx, telegramConfig, message); err != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}

	return nil
}

func (s *NotificationService) sendEmailPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var emailConfig models.EmailConfig
	if err := s.unmarshalConfigInternal(config, &emailConfig); err != nil {
		return err
	}

	if err := s.validateEmailConfigInternal(&emailConfig); err != nil {
		return err
	}

	s.decryptEmailPasswordInternal(&emailConfig)

	htmlBody, _, err := s.renderPruneReportEmailTemplate(environmentName, result)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("System Prune Report: %s Reclaimed", s.formatBytesInternal(result.SpaceReclaimed)))
	if err := notifications.SendEmail(ctx, emailConfig, subject, htmlBody); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderPruneReportEmailTemplate(environmentName string, result *system.PruneAllResult) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]any{
		"LogoURL":                  logoURL,
		"AppURL":                   appURL,
		"Environment":              environmentName,
		"TotalSpaceReclaimed":      s.formatBytesInternal(result.SpaceReclaimed),
		"ContainerSpaceReclaimed":  s.formatBytesInternal(result.ContainerSpaceReclaimed),
		"ImageSpaceReclaimed":      s.formatBytesInternal(result.ImageSpaceReclaimed),
		"VolumeSpaceReclaimed":     s.formatBytesInternal(result.VolumeSpaceReclaimed),
		"BuildCacheSpaceReclaimed": s.formatBytesInternal(result.BuildCacheSpaceReclaimed),
		"Time":                     time.Now().Format(time.RFC1123),
	}

	return s.renderTemplatesInternal("prune-report", data)
}

func (s *NotificationService) sendSignalPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var signalConfig models.SignalConfig
	if err := s.unmarshalConfigInternal(config, &signalConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)

	return notifications.SendSignal(ctx, signalConfig, message)
}

func (s *NotificationService) sendSlackPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var slackConfig models.SlackConfig
	if err := s.unmarshalConfigInternal(config, &slackConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatSlack, environmentName, result)

	return notifications.SendSlack(ctx, slackConfig, message)
}

func (s *NotificationService) sendNtfyPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	if err := s.unmarshalConfigInternal(config, &ntfyConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)

	return notifications.SendNtfy(ctx, ntfyConfig, message)
}

func (s *NotificationService) sendPushoverPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	if err := s.unmarshalConfigInternal(config, &pushoverConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)

	if pushoverConfig.Title == "" {
		pushoverConfig.Title = notifications.BuildEmailSubject(environmentName, "System Prune Report")
	}

	return notifications.SendPushover(ctx, pushoverConfig, message)
}

func (s *NotificationService) sendGotifyPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	if err := s.unmarshalConfigInternal(config, &gotifyConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)

	if gotifyConfig.Title == "" {
		gotifyConfig.Title = notifications.BuildEmailSubject(environmentName, "System Prune Report")
	}

	return notifications.SendGotify(ctx, gotifyConfig, message)
}

func (s *NotificationService) sendMatrixPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	if err := s.unmarshalConfigInternal(config, &matrixConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)

	return notifications.SendMatrix(ctx, matrixConfig, message)
}

func (s *NotificationService) sendGenericPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var genericConfig models.GenericConfig
	if err := s.unmarshalConfigInternal(config, &genericConfig); err != nil {
		return err
	}

	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatPlain, environmentName, result)
	title := notifications.BuildEmailSubject(environmentName, "System Prune Report")
	return notifications.SendGenericWithTitle(ctx, genericConfig, title, message)
}

// SendAutoHealNotification sends a notification when a container is auto-healed.
func (s *NotificationService) SendAutoHealNotification(ctx context.Context, containerName, containerID string) error {
	if s.config != nil && s.config.AgentMode {
		return s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindAutoHeal,
			AutoHeal: &notificationdto.DispatchAutoHeal{
				ContainerName: containerName,
				ContainerID:   containerID,
			},
		})
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendAutoHealNotificationForTargetInternal(ctx, target, containerName, containerID)
}

func (s *NotificationService) sendAutoHealNotificationForTargetInternal(ctx context.Context, target NotificationTarget, containerName, containerID string) error {
	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notification settings: %w", err)
	}

	var errs []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}

		if !s.isEventEnabled(setting.Config, models.NotificationEventAutoHeal) {
			continue
		}

		var sendErr error
		switch setting.Provider {
		case models.NotificationProviderDiscord:
			sendErr = s.sendDiscordAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderTelegram:
			sendErr = s.sendTelegramAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderSignal:
			sendErr = s.sendSignalAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderSlack:
			sendErr = s.sendSlackAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderNtfy:
			sendErr = s.sendNtfyAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderPushover:
			sendErr = s.sendPushoverAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderGotify:
			sendErr = s.sendGotifyAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderMatrix:
			sendErr = s.sendMatrixAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		case models.NotificationProviderGeneric:
			sendErr = s.sendGenericAutoHealNotification(ctx, target.EnvironmentName, containerName, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = new(msg)
			errs = append(errs, fmt.Sprintf("%s: %s", setting.Provider, msg))
		}

		s.logNotification(ctx, setting.Provider, containerName, status, errMsg, models.JSON{
			"containerID": containerID,
			"eventType":   string(models.NotificationEventAutoHeal),
		})
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (s *NotificationService) sendDiscordAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var discordConfig models.DiscordConfig
	if err := s.unmarshalConfigInternal(config, &discordConfig); err != nil {
		return err
	}
	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return fmt.Errorf("discord webhook ID or token not configured")
	}
	s.decryptDiscordTokenInternal(&discordConfig)
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatMarkdown, environmentName, containerName)
	return notifications.SendDiscord(ctx, discordConfig, message)
}

func (s *NotificationService) sendEmailAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var emailConfig models.EmailConfig
	if err := s.unmarshalConfigInternal(config, &emailConfig); err != nil {
		return err
	}
	if err := s.validateEmailConfigInternal(&emailConfig); err != nil {
		return err
	}
	s.decryptEmailPasswordInternal(&emailConfig)
	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Auto Heal: Container '%s' Restarted", containerName))
	body := fmt.Sprintf(
		"<p><strong>Environment:</strong> %s</p><p><strong>Container:</strong> %s</p><p>Automatically restarted because it was unhealthy.</p>",
		environmentName,
		containerName,
	)
	return notifications.SendEmail(ctx, emailConfig, subject, body)
}

func (s *NotificationService) sendTelegramAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var telegramConfig models.TelegramConfig
	if err := s.unmarshalConfigInternal(config, &telegramConfig); err != nil {
		return err
	}
	if telegramConfig.BotToken == "" || len(telegramConfig.ChatIDs) == 0 {
		return fmt.Errorf("telegram bot token or chat IDs not configured")
	}
	s.decryptTelegramTokenInternal(&telegramConfig)
	if telegramConfig.ParseMode == "" {
		telegramConfig.ParseMode = "HTML"
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatHTML, environmentName, containerName)
	return notifications.SendTelegram(ctx, telegramConfig, message)
}

func (s *NotificationService) sendSignalAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var signalConfig models.SignalConfig
	if err := s.unmarshalConfigInternal(config, &signalConfig); err != nil {
		return err
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	return notifications.SendSignal(ctx, signalConfig, message)
}

func (s *NotificationService) sendSlackAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var slackConfig models.SlackConfig
	if err := s.unmarshalConfigInternal(config, &slackConfig); err != nil {
		return err
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatSlack, environmentName, containerName)
	return notifications.SendSlack(ctx, slackConfig, message)
}

func (s *NotificationService) sendNtfyAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var ntfyConfig models.NtfyConfig
	if err := s.unmarshalConfigInternal(config, &ntfyConfig); err != nil {
		return err
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	return notifications.SendNtfy(ctx, ntfyConfig, message)
}

func (s *NotificationService) sendPushoverAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var pushoverConfig models.PushoverConfig
	if err := s.unmarshalConfigInternal(config, &pushoverConfig); err != nil {
		return err
	}
	if pushoverConfig.Title == "" {
		pushoverConfig.Title = notifications.BuildEmailSubject(environmentName, "Auto Heal")
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	return notifications.SendPushover(ctx, pushoverConfig, message)
}

func (s *NotificationService) sendGotifyAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var gotifyConfig models.GotifyConfig
	if err := s.unmarshalConfigInternal(config, &gotifyConfig); err != nil {
		return err
	}
	if gotifyConfig.Title == "" {
		gotifyConfig.Title = notifications.BuildEmailSubject(environmentName, "Auto Heal")
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	return notifications.SendGotify(ctx, gotifyConfig, message)
}

func (s *NotificationService) sendMatrixAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var matrixConfig models.MatrixConfig
	if err := s.unmarshalConfigInternal(config, &matrixConfig); err != nil {
		return err
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	return notifications.SendMatrix(ctx, matrixConfig, message)
}

func (s *NotificationService) sendGenericAutoHealNotification(ctx context.Context, environmentName, containerName string, config models.JSON) error {
	var genericConfig models.GenericConfig
	if err := s.unmarshalConfigInternal(config, &genericConfig); err != nil {
		return err
	}
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatPlain, environmentName, containerName)
	title := notifications.BuildEmailSubject(environmentName, "Auto Heal")
	return notifications.SendGenericWithTitle(ctx, genericConfig, title, message)
}

// Helper methods to reduce code duplication
func (s *NotificationService) unmarshalConfigInternal(config models.JSON, dest any) error {
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := json.Unmarshal(configBytes, dest); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return nil
}

func (s *NotificationService) validateEmailConfigInternal(config *models.EmailConfig) error {
	if config.SMTPHost == "" || config.SMTPPort == 0 {
		return fmt.Errorf("SMTP host or port not configured")
	}
	if len(config.ToAddresses) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}
	return nil
}

func (s *NotificationService) decryptDiscordTokenInternal(config *models.DiscordConfig) {
	if config.Token != "" {
		if decrypted, err := crypto.Decrypt(config.Token); err == nil {
			config.Token = decrypted
		}
	}
}

func (s *NotificationService) decryptTelegramTokenInternal(config *models.TelegramConfig) {
	if config.BotToken != "" {
		if decrypted, err := crypto.Decrypt(config.BotToken); err == nil {
			config.BotToken = decrypted
		}
	}
}

func (s *NotificationService) decryptEmailPasswordInternal(config *models.EmailConfig) {
	if config.SMTPPassword != "" {
		if decrypted, err := crypto.Decrypt(config.SMTPPassword); err == nil {
			config.SMTPPassword = decrypted
		}
	}
}

func (s *NotificationService) renderTemplatesInternal(name string, data any) (string, string, error) {
	htmlContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_html.tmpl", name))
	if err != nil {
		return "", "", fmt.Errorf("failed to read HTML template: %w", err)
	}

	htmlTmpl, err := template.New("html").Parse(string(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := htmlTmpl.ExecuteTemplate(&htmlBuf, "root", data); err != nil {
		return "", "", fmt.Errorf("failed to execute HTML template: %w", err)
	}

	textContent, err := resources.FS.ReadFile(fmt.Sprintf("email-templates/%s_text.tmpl", name))
	if err == nil {
		textTmpl, err := template.New("text").Parse(string(textContent))
		if err == nil {
			var textBuf bytes.Buffer
			if err := textTmpl.ExecuteTemplate(&textBuf, "root", data); err == nil {
				return htmlBuf.String(), textBuf.String(), nil
			}
		}
	}

	return htmlBuf.String(), "", nil
}
