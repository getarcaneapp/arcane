package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/mail"
	"strings"
	"text/template"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
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

var notificationCredentialFieldsByProviderInternal = map[models.NotificationProvider][]string{
	models.NotificationProviderDiscord:  {"token"},
	models.NotificationProviderEmail:    {"smtpPassword"},
	models.NotificationProviderTelegram: {"botToken"},
	models.NotificationProviderSignal:   {"password", "token"},
	models.NotificationProviderSlack:    {"token"},
	models.NotificationProviderNtfy:     {"password"},
	models.NotificationProviderPushover: {"token"},
	models.NotificationProviderGotify:   {"token"},
	models.NotificationProviderMatrix:   {"password"},
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
		return NotificationTarget{}, errors.New("environment service not initialized")
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
		return errors.New("manager API URL is required for notification dispatch")
	}
	if strings.TrimSpace(s.config.AgentToken) == "" {
		return errors.New("agent token is required for notification dispatch")
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
	req.Header.Set("X-Api-Key", s.config.AgentToken)

	resp, err := s.httpClient.Do(req)
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
		return errors.New("notification dispatch is manager-only")
	}

	target, err := s.resolveNotificationTargetForAccessTokenInternal(ctx, accessToken)
	if err != nil {
		return err
	}

	switch payload.Kind {
	case notificationdto.DispatchKindImageUpdate:
		if payload.ImageUpdate == nil {
			return errors.New("image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendImageUpdateNotificationForTargetInternal(ctx, target, payload.ImageUpdate.ImageRef, &payload.ImageUpdate.UpdateInfo, models.NotificationEventImageUpdate)
	case notificationdto.DispatchKindBatchImageUpdate:
		if payload.BatchImageUpdate == nil {
			return errors.New("batch image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendBatchImageUpdateNotificationForTargetInternal(ctx, target, payload.BatchImageUpdate.Updates)
	case notificationdto.DispatchKindContainerUpdate:
		if payload.ContainerUpdate == nil {
			return errors.New("container update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendContainerUpdateNotificationForTargetInternal(ctx, target, payload.ContainerUpdate.ContainerName, payload.ContainerUpdate.ImageRef, payload.ContainerUpdate.OldDigest, payload.ContainerUpdate.NewDigest)
	case notificationdto.DispatchKindVulnerabilityFound:
		if payload.VulnerabilityFound == nil {
			return errors.New("vulnerability payload is required")
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
			return errors.New("prune report payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return s.sendPruneReportNotificationForTargetInternal(ctx, target, &payload.PruneReport.Result)
	case notificationdto.DispatchKindAutoHeal:
		if payload.AutoHeal == nil {
			return errors.New("auto-heal payload is required")
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

	err := s.db.WithContext(ctx).Where("provider = ?", provider).First(&setting).Error
	existingConfig := models.JSON(nil)
	if err == nil {
		existingConfig = setting.Config
	}

	// Clear config if provider is disabled
	if !enabled {
		config = models.JSON{}
	}

	encryptedConfig, encryptErr := encryptNotificationConfigCredentialsInternal(provider, config, existingConfig)
	if encryptErr != nil {
		return nil, encryptErr
	}
	config = encryptedConfig

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

// RedactNotificationConfigCredentials returns a copy of config with provider credential fields blanked for API responses.
func RedactNotificationConfigCredentials(provider models.NotificationProvider, config models.JSON) models.JSON {
	redacted := cloneNotificationConfigInternal(config)
	for _, field := range notificationCredentialFieldsByProviderInternal[provider] {
		value, ok := redacted[field]
		if !ok {
			continue
		}
		if value == "" {
			delete(redacted, field)
			continue
		}
		redacted[field] = ""
	}
	return redacted
}

func encryptNotificationConfigCredentialsInternal(provider models.NotificationProvider, config models.JSON, existingConfig models.JSON) (models.JSON, error) {
	encryptedConfig := cloneNotificationConfigInternal(config)
	preserveConfig := existingConfig
	if provider == models.NotificationProviderSignal {
		preserveConfig = signalCredentialPreservationConfigInternal(config, existingConfig)
	}
	for _, field := range notificationCredentialFieldsByProviderInternal[provider] {
		value, _ := encryptedConfig[field].(string)
		if value == "" {
			if existingValue, ok := preserveConfig[field].(string); ok && existingValue != "" {
				encryptedConfig[field] = existingValue
			}
			continue
		}

		encrypted, err := encryptNotificationCredentialInternal(value)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt notification credential %q: %w", field, err)
		}
		encryptedConfig[field] = encrypted
	}
	return encryptedConfig, nil
}

func signalCredentialPreservationConfigInternal(config models.JSON, existingConfig models.JSON) models.JSON {
	preserveConfig := cloneNotificationConfigInternal(existingConfig)
	user, _ := config["user"].(string)
	password, _ := config["password"].(string)
	token, _ := config["token"].(string)

	if strings.TrimSpace(token) != "" {
		delete(preserveConfig, "password")
	}
	if strings.TrimSpace(user) != "" || strings.TrimSpace(password) != "" {
		delete(preserveConfig, "token")
	}

	return preserveConfig
}

func encryptNotificationCredentialInternal(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if _, err := crypto.Decrypt(value); err == nil {
		return value, nil
	}
	return crypto.Encrypt(value)
}

func cloneNotificationConfigInternal(config models.JSON) models.JSON {
	if config == nil {
		return models.JSON{}
	}
	cloned := make(models.JSON, len(config))
	maps.Copy(cloned, config)
	return cloned
}

func (s *NotificationService) DeleteSettings(ctx context.Context, provider models.NotificationProvider) error {
	if err := s.db.WithContext(ctx).Where("provider = ?", provider).Delete(&models.NotificationSettings{}).Error; err != nil {
		return fmt.Errorf("failed to delete notification settings: %w", err)
	}
	return nil
}

func (s *NotificationService) SendImageUpdateNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response, eventType models.NotificationEventType) error {
	if updateInfo == nil {
		return errors.New("updateInfo is required")
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
	events, ok := config["events"].(map[string]any)
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

		handled, sendErr := sendProviderNotificationInternal(ctx, setting.Provider, target.EnvironmentName, payload, setting.Config, s.vulnerabilityNotificationSendersInternal())
		if !handled {
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status, errMsg := collectNotificationSendResultInternal(&errors, setting.Provider, sendErr)

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
		return errors.New("discord webhook ID or token not configured")
	}

	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
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
		return errors.New("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return errors.New("no telegram chat IDs configured")
	}

	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
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
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}

	htmlBody, _, err := s.renderEmailTemplate(environmentName, imageRef, updateInfo)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, "Container Update Available: "+notifications.SanitizeForEmail(imageRef))
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
		return errors.New("discord webhook ID or token not configured")
	}

	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
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
		return errors.New("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return errors.New("no telegram chat IDs configured")
	}

	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
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
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}

	htmlBody, _, err := s.renderContainerUpdateEmailTemplate(environmentName, containerName, imageRef, oldDigest, newDigest)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, "Container Updated: "+notifications.SanitizeForEmail(containerName))
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
			CVEID:        "Daily Summary - " + time.Now().UTC().Format("2006-01-02"),
			Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
			ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
			FixedVersion: "7 fixable vulnerability record(s)",
			PkgName:      "CVE-2025-1234, CVE-2025-5678, CVE-2026-0001",
		}
		handled, sendErr := sendProviderNotificationInternal(ctx, provider, target.EnvironmentName, payload, setting.Config, s.vulnerabilityNotificationSendersInternal())
		if !handled {
			return unknownNotificationProviderErrorInternal(provider)
		}
		return sendErr
	}

	if testType == notificationTestTypeAutoHeal {
		testContainerName := "test-container"
		handled, sendErr := sendProviderNotificationInternal(ctx, provider, target.EnvironmentName, testContainerName, setting.Config, s.autoHealNotificationSendersInternal())
		if !handled {
			return unknownNotificationProviderErrorInternal(provider)
		}
		return sendErr
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

		handled, sendErr := sendProviderNotificationInternal(ctx, provider, target.EnvironmentName, result, setting.Config, s.pruneReportNotificationSendersInternal())
		if !handled {
			return unknownNotificationProviderErrorInternal(provider)
		}
		return sendErr
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
		handled, sendErr := sendProviderNotificationInternal(ctx, provider, target.EnvironmentName, testUpdates, setting.Config, s.batchImageUpdateNotificationSendersInternal())
		if !handled {
			return unknownNotificationProviderErrorInternal(provider)
		}
		return sendErr
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

type notificationProviderSenderInternal[T any] func(context.Context, string, T, models.JSON) error

type notificationProviderSendersInternal[T any] struct {
	Discord  notificationProviderSenderInternal[T]
	Email    notificationProviderSenderInternal[T]
	Telegram notificationProviderSenderInternal[T]
	Signal   notificationProviderSenderInternal[T]
	Slack    notificationProviderSenderInternal[T]
	Ntfy     notificationProviderSenderInternal[T]
	Pushover notificationProviderSenderInternal[T]
	Gotify   notificationProviderSenderInternal[T]
	Matrix   notificationProviderSenderInternal[T]
	Generic  notificationProviderSenderInternal[T]
}

func (s *NotificationService) vulnerabilityNotificationSendersInternal() notificationProviderSendersInternal[VulnerabilityNotificationPayload] {
	return notificationProviderSendersInternal[VulnerabilityNotificationPayload]{
		Discord:  s.sendDiscordVulnerabilityNotification,
		Email:    s.sendEmailVulnerabilityNotification,
		Telegram: s.sendTelegramVulnerabilityNotification,
		Signal:   s.sendSignalVulnerabilityNotification,
		Slack:    s.sendSlackVulnerabilityNotification,
		Ntfy:     s.sendNtfyVulnerabilityNotification,
		Pushover: s.sendPushoverVulnerabilityNotification,
		Gotify:   s.sendGotifyVulnerabilityNotification,
		Matrix:   s.sendMatrixVulnerabilityNotification,
		Generic:  s.sendGenericVulnerabilityNotification,
	}
}

func (s *NotificationService) batchImageUpdateNotificationSendersInternal() notificationProviderSendersInternal[map[string]*imageupdate.Response] {
	return notificationProviderSendersInternal[map[string]*imageupdate.Response]{
		Discord:  s.sendBatchDiscordNotification,
		Email:    s.sendBatchEmailNotification,
		Telegram: s.sendBatchTelegramNotification,
		Signal:   s.sendBatchSignalNotification,
		Slack:    s.sendBatchSlackNotification,
		Ntfy:     s.sendBatchNtfyNotification,
		Pushover: s.sendBatchPushoverNotification,
		Gotify:   s.sendBatchGotifyNotification,
		Matrix:   s.sendBatchMatrixNotification,
		Generic:  s.sendBatchGenericNotification,
	}
}

func (s *NotificationService) pruneReportNotificationSendersInternal() notificationProviderSendersInternal[*system.PruneAllResult] {
	return notificationProviderSendersInternal[*system.PruneAllResult]{
		Discord:  s.sendDiscordPruneNotification,
		Email:    s.sendEmailPruneNotification,
		Telegram: s.sendTelegramPruneNotification,
		Signal:   s.sendSignalPruneNotification,
		Slack:    s.sendSlackPruneNotification,
		Ntfy:     s.sendNtfyPruneNotification,
		Pushover: s.sendPushoverPruneNotification,
		Gotify:   s.sendGotifyPruneNotification,
		Matrix:   s.sendMatrixPruneNotification,
		Generic:  s.sendGenericPruneNotification,
	}
}

func (s *NotificationService) autoHealNotificationSendersInternal() notificationProviderSendersInternal[string] {
	return notificationProviderSendersInternal[string]{
		Discord:  s.sendDiscordAutoHealNotification,
		Email:    s.sendEmailAutoHealNotification,
		Telegram: s.sendTelegramAutoHealNotification,
		Signal:   s.sendSignalAutoHealNotification,
		Slack:    s.sendSlackAutoHealNotification,
		Ntfy:     s.sendNtfyAutoHealNotification,
		Pushover: s.sendPushoverAutoHealNotification,
		Gotify:   s.sendGotifyAutoHealNotification,
		Matrix:   s.sendMatrixAutoHealNotification,
		Generic:  s.sendGenericAutoHealNotification,
	}
}

func sendProviderNotificationInternal[T any](
	ctx context.Context,
	provider models.NotificationProvider,
	environmentName string,
	payload T,
	config models.JSON,
	senders notificationProviderSendersInternal[T],
) (bool, error) {
	switch provider {
	case models.NotificationProviderDiscord:
		return true, senders.Discord(ctx, environmentName, payload, config)
	case models.NotificationProviderEmail:
		return true, senders.Email(ctx, environmentName, payload, config)
	case models.NotificationProviderTelegram:
		return true, senders.Telegram(ctx, environmentName, payload, config)
	case models.NotificationProviderSignal:
		return true, senders.Signal(ctx, environmentName, payload, config)
	case models.NotificationProviderSlack:
		return true, senders.Slack(ctx, environmentName, payload, config)
	case models.NotificationProviderNtfy:
		return true, senders.Ntfy(ctx, environmentName, payload, config)
	case models.NotificationProviderPushover:
		return true, senders.Pushover(ctx, environmentName, payload, config)
	case models.NotificationProviderGotify:
		return true, senders.Gotify(ctx, environmentName, payload, config)
	case models.NotificationProviderMatrix:
		return true, senders.Matrix(ctx, environmentName, payload, config)
	case models.NotificationProviderGeneric:
		return true, senders.Generic(ctx, environmentName, payload, config)
	default:
		return false, nil
	}
}

func collectNotificationSendResultInternal(errors *[]string, provider models.NotificationProvider, sendErr error) (string, *string) {
	if sendErr == nil {
		return "success", nil
	}

	msg := sendErr.Error()
	*errors = append(*errors, fmt.Sprintf("%s: %s", provider, msg))
	return "failed", &msg
}

func unknownNotificationProviderErrorInternal(provider models.NotificationProvider) error {
	return fmt.Errorf("unknown provider: %s", provider)
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
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
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

		handled, sendErr := sendProviderNotificationInternal(ctx, setting.Provider, target.EnvironmentName, updatesWithChanges, setting.Config, s.batchImageUpdateNotificationSendersInternal())
		if !handled {
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status, errMsg := collectNotificationSendResultInternal(&errors, setting.Provider, sendErr)

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
	discordConfig, err := notifications.DecodeConfig[models.DiscordConfig](config, "discord")
	if err != nil {
		return err
	}
	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
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

	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
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
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}

	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}

	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
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
		return errors.New("signal host not configured")
	}
	if signalConfig.Port == 0 {
		return errors.New("signal port not configured")
	}
	if signalConfig.Source == "" {
		return errors.New("signal source phone number not configured")
	}
	if len(signalConfig.Recipients) == 0 {
		return errors.New("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return errors.New("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return errors.New("signal cannot use both basic auth and token authentication simultaneously")
	}

	if err := notifications.DecryptStringCredential(&signalConfig.Password); err != nil {
		return err
	}
	if err := notifications.DecryptStringCredential(&signalConfig.Token); err != nil {
		return err
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
		return errors.New("signal host not configured")
	}
	if signalConfig.Port == 0 {
		return errors.New("signal port not configured")
	}
	if signalConfig.Source == "" {
		return errors.New("signal source phone number not configured")
	}
	if len(signalConfig.Recipients) == 0 {
		return errors.New("no signal recipients configured")
	}

	// Validate authentication
	hasBasicAuth := signalConfig.User != "" && signalConfig.Password != ""
	hasTokenAuth := signalConfig.Token != ""
	if !hasBasicAuth && !hasTokenAuth {
		return errors.New("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return errors.New("signal cannot use both basic auth and token authentication simultaneously")
	}

	if err := notifications.DecryptStringCredential(&signalConfig.Password); err != nil {
		return err
	}
	if err := notifications.DecryptStringCredential(&signalConfig.Token); err != nil {
		return err
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
		return errors.New("signal requires either basic auth (user/password) or token authentication")
	}
	if hasBasicAuth && hasTokenAuth {
		return errors.New("signal cannot use both basic auth and token authentication simultaneously")
	}

	if err := notifications.DecryptStringCredential(&signalConfig.Password); err != nil {
		return err
	}
	if err := notifications.DecryptStringCredential(&signalConfig.Token); err != nil {
		return err
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
	slackConfig, err := notifications.PrepareSlackConfig(config, "Slack", true)
	if err != nil {
		return err
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
	slackConfig, err := notifications.PrepareSlackConfig(config, "Slack", true)
	if err != nil {
		return err
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
	slackConfig, err := notifications.PrepareSlackConfig(config, "slack", false)
	if err != nil {
		return err
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
	ntfyConfig, err := notifications.PrepareNtfyConfig(config, "Ntfy", true)
	if err != nil {
		return err
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
	ntfyConfig, err := notifications.PrepareNtfyConfig(config, "Ntfy", true)
	if err != nil {
		return err
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
	ntfyConfig, err := notifications.PrepareNtfyConfig(config, "ntfy", false)
	if err != nil {
		return err
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
	pushoverConfig, err := notifications.PreparePushoverConfig(config, "Pushover")
	if err != nil {
		return err
	}

	if pushoverConfig.Token == "" {
		return errors.New("pushover API token not configured")
	}
	if pushoverConfig.User == "" {
		return errors.New("pushover user key not configured")
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
	pushoverConfig, err := notifications.PreparePushoverConfig(config, "Pushover")
	if err != nil {
		return err
	}

	if pushoverConfig.Token == "" {
		return errors.New("pushover API token not configured")
	}
	if pushoverConfig.User == "" {
		return errors.New("pushover user key not configured")
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
	pushoverConfig, err := notifications.PreparePushoverConfig(config, "pushover")
	if err != nil {
		return err
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
		return errors.New("webhook URL not configured")
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
		return errors.New("webhook URL not configured")
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
		return errors.New("SMTP host or port not configured")
	}
	if len(emailConfig.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}
	if _, err := mail.ParseAddress(emailConfig.FromAddress); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	for _, addr := range emailConfig.ToAddresses {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("invalid to address %s: %w", addr, err)
		}
	}
	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}
	htmlBody, _, err := s.renderVulnerabilitySummaryEmailTemplate(environmentName, payload)
	if err != nil {
		return fmt.Errorf("failed to render summary email template: %w", err)
	}
	subject := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary: "+notifications.SanitizeForEmail(payload.CVEID))
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
		return errors.New("discord webhook ID or token not configured")
	}
	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
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
		return errors.New("telegram bot token not configured")
	}
	if len(telegramConfig.ChatIDs) == 0 {
		return errors.New("no telegram chat IDs configured")
	}
	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
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
		return errors.New("signal not fully configured")
	}
	if err := notifications.DecryptStringCredential(&signalConfig.Password); err != nil {
		return err
	}
	if err := notifications.DecryptStringCredential(&signalConfig.Token); err != nil {
		return err
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
	slackConfig, err := notifications.PrepareSlackConfig(config, "Slack", true)
	if err != nil {
		return err
	}
	message := buildVulnerabilitySummaryMessageInternal(notifications.MessageFormatSlack, environmentName, payload)
	if err := notifications.SendSlack(ctx, slackConfig, message); err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendNtfyVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	ntfyConfig, err := notifications.PrepareNtfyConfig(config, "Ntfy", true)
	if err != nil {
		return err
	}
	message := buildVulnerabilitySummaryMessageInternal(notifications.MessageFormatPlain, environmentName, payload)
	if err := notifications.SendNtfy(ctx, ntfyConfig, message); err != nil {
		return fmt.Errorf("failed to send Ntfy notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendPushoverVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	pushoverConfig, err := notifications.PreparePushoverConfig(config, "Pushover")
	if err != nil {
		return err
	}
	if pushoverConfig.Token == "" || pushoverConfig.User == "" {
		return errors.New("pushover token or user not configured")
	}
	message := buildVulnerabilitySummaryMessageInternal(notifications.MessageFormatPlain, environmentName, payload)
	if pushoverConfig.Title == "" {
		pushoverConfig.Title = notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	}
	if err := notifications.SendPushover(ctx, pushoverConfig, message); err != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendGotifyVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	gotifyConfig, err := notifications.PrepareGotifyConfig(config, "Gotify")
	if err != nil {
		return err
	}
	message := buildVulnerabilitySummaryMessageInternal(notifications.MessageFormatPlain, environmentName, payload)
	if gotifyConfig.Title == "" {
		gotifyConfig.Title = notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	}
	if err := notifications.SendGotify(ctx, gotifyConfig, message); err != nil {
		return fmt.Errorf("failed to send Gotify notification: %w", err)
	}
	return nil
}

func (s *NotificationService) sendMatrixVulnerabilityNotification(ctx context.Context, environmentName string, payload VulnerabilityNotificationPayload, config models.JSON) error {
	matrixConfig, err := notifications.PrepareMatrixConfig(config)
	if err != nil {
		return err
	}
	message := buildVulnerabilitySummaryMessageInternal(notifications.MessageFormatPlain, environmentName, payload)
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
		return errors.New("webhook URL not configured")
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
		return errors.New("webhook URL not configured")
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
	gotifyConfig, err := notifications.PrepareGotifyConfig(config, "Gotify")
	if err != nil {
		return err
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
	gotifyConfig, err := notifications.PrepareGotifyConfig(config, "Gotify")
	if err != nil {
		return err
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
	gotifyConfig, err := notifications.PrepareGotifyConfig(config, "gotify")
	if err != nil {
		return err
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
	matrixConfig, err := notifications.PrepareMatrixConfig(config)
	if err != nil {
		return err
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
	matrixConfig, err := notifications.PrepareMatrixConfig(config)
	if err != nil {
		return err
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
	matrixConfig, err := notifications.PrepareMatrixConfig(config)
	if err != nil {
		return err
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

		handled, sendErr := sendProviderNotificationInternal(ctx, setting.Provider, target.EnvironmentName, result, setting.Config, s.pruneReportNotificationSendersInternal())
		if !handled {
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status, errMsg := collectNotificationSendResultInternal(&errors, setting.Provider, sendErr)

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

func (s *NotificationService) sendDiscordPruneNotification(ctx context.Context, environmentName string, result *system.PruneAllResult, config models.JSON) error {
	var discordConfig models.DiscordConfig
	if err := s.unmarshalConfigInternal(config, &discordConfig); err != nil {
		return err
	}

	if discordConfig.WebhookID == "" || discordConfig.Token == "" {
		return errors.New("discord webhook ID or token not configured")
	}

	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
	}

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
		return errors.New("telegram bot token or chat IDs not configured")
	}

	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
	}

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

	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}

	htmlBody, _, err := s.renderPruneReportEmailTemplate(environmentName, result)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("System Prune Report: %s Reclaimed", notifications.FormatBytes(result.SpaceReclaimed)))
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
		"TotalSpaceReclaimed":      notifications.FormatBytes(result.SpaceReclaimed),
		"ContainerSpaceReclaimed":  notifications.FormatBytes(result.ContainerSpaceReclaimed),
		"ImageSpaceReclaimed":      notifications.FormatBytes(result.ImageSpaceReclaimed),
		"VolumeSpaceReclaimed":     notifications.FormatBytes(result.VolumeSpaceReclaimed),
		"BuildCacheSpaceReclaimed": notifications.FormatBytes(result.BuildCacheSpaceReclaimed),
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
	if err := notifications.DecryptStringCredential(&gotifyConfig.Token); err != nil {
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

		handled, sendErr := sendProviderNotificationInternal(ctx, setting.Provider, target.EnvironmentName, containerName, setting.Config, s.autoHealNotificationSendersInternal())
		if !handled {
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status, errMsg := collectNotificationSendResultInternal(&errs, setting.Provider, sendErr)

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
		return errors.New("discord webhook ID or token not configured")
	}
	if err := notifications.DecryptStringCredential(&discordConfig.Token); err != nil {
		return err
	}
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
	if err := notifications.DecryptStringCredential(&emailConfig.SMTPPassword); err != nil {
		return err
	}
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
		return errors.New("telegram bot token or chat IDs not configured")
	}
	if err := notifications.DecryptStringCredential(&telegramConfig.BotToken); err != nil {
		return err
	}
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
	if err := notifications.DecryptStringCredential(&gotifyConfig.Token); err != nil {
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
		return errors.New("SMTP host or port not configured")
	}
	if len(config.ToAddresses) == 0 {
		return errors.New("no recipient email addresses configured")
	}
	return nil
}

func buildVulnerabilitySummaryMessageInternal(format notifications.MessageFormat, environmentName string, payload VulnerabilityNotificationPayload) string {
	return notifications.BuildVulnerabilitySummaryNotificationMessage(
		format,
		environmentName,
		payload.CVEID,
		payload.ImageName,
		payload.FixedVersion,
		payload.Severity,
		payload.PkgName,
	)
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
