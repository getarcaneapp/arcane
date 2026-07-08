package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/crypto"
)

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

type NotificationService struct {
	db             *database.DB
	config         *config.Config
	environmentSvc *EnvironmentService
	eventSvc       *EventService
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

func NewNotificationService(db *database.DB, cfg *config.Config, environmentSvc *EnvironmentService, eventSvc *EventService) *NotificationService {
	return &NotificationService{
		db:             db,
		config:         cfg,
		environmentSvc: environmentSvc,
		eventSvc:       eventSvc,
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

func (s *NotificationService) dispatchNotificationToManagerInternal(ctx context.Context, payload notificationdto.DispatchRequest) (notificationdto.DispatchResponse, error) {
	if s.config == nil || strings.TrimSpace(s.config.GetManagerBaseURL()) == "" {
		return notificationdto.DispatchResponse{}, errors.New("manager API URL is required for notification dispatch")
	}
	if strings.TrimSpace(s.config.AgentToken) == "" {
		return notificationdto.DispatchResponse{}, errors.New("agent token is required for notification dispatch")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return notificationdto.DispatchResponse{}, fmt.Errorf("failed to marshal notification dispatch payload: %w", err)
	}

	dispatchURL := strings.TrimRight(s.config.GetManagerBaseURL(), "/") + "/api/notifications/dispatch"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dispatchURL, bytes.NewReader(body))
	if err != nil {
		return notificationdto.DispatchResponse{}, fmt.Errorf("failed to create notification dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", s.config.AgentToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return notificationdto.DispatchResponse{}, fmt.Errorf("failed to dispatch notification to manager: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		var apiResponse struct {
			Data notificationdto.DispatchResponse `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
			return notificationdto.DispatchResponse{}, fmt.Errorf("failed to decode manager notification dispatch response: %w", err)
		}
		return apiResponse.Data, nil
	}

	responseBody, _ := io.ReadAll(resp.Body)
	return notificationdto.DispatchResponse{}, fmt.Errorf("manager notification dispatch failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

func (s *NotificationService) DispatchNotification(ctx context.Context, accessToken string, payload notificationdto.DispatchRequest) (notificationdto.DispatchResponse, error) {
	if s.config != nil && s.config.AgentMode {
		return notificationdto.DispatchResponse{}, errors.New("notification dispatch is manager-only")
	}

	target, err := s.resolveNotificationTargetForAccessTokenInternal(ctx, accessToken)
	if err != nil {
		return notificationdto.DispatchResponse{}, err
	}

	dispatchResponse := notificationdto.DispatchResponse{Message: "Notification dispatched successfully"}
	switch payload.Kind {
	case notificationdto.DispatchKindImageUpdate:
		if payload.ImageUpdate == nil {
			return notificationdto.DispatchResponse{}, errors.New("image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		dispatchResponse.Delivered, err = s.sendImageUpdateNotificationForTargetInternal(ctx, target, payload.ImageUpdate.ImageRef, &payload.ImageUpdate.UpdateInfo, models.NotificationEventImageUpdate)
		return dispatchResponse, err
	case notificationdto.DispatchKindBatchImageUpdate:
		if payload.BatchImageUpdate == nil {
			return notificationdto.DispatchResponse{}, errors.New("batch image update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		dispatchResponse.Delivered, err = s.sendBatchImageUpdateNotificationForTargetInternal(ctx, target, payload.BatchImageUpdate.Updates)
		return dispatchResponse, err
	case notificationdto.DispatchKindContainerUpdate:
		if payload.ContainerUpdate == nil {
			return notificationdto.DispatchResponse{}, errors.New("container update payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return dispatchResponse, s.sendContainerUpdateNotificationForTargetInternal(ctx, target, payload.ContainerUpdate.ContainerName, payload.ContainerUpdate.ImageRef, payload.ContainerUpdate.OldDigest, payload.ContainerUpdate.NewDigest)
	case notificationdto.DispatchKindVulnerabilityFound:
		if payload.VulnerabilityFound == nil {
			return notificationdto.DispatchResponse{}, errors.New("vulnerability payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return dispatchResponse, s.sendVulnerabilityNotificationForTargetInternal(ctx, target, VulnerabilityNotificationPayload{
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
			return notificationdto.DispatchResponse{}, errors.New("prune report payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return dispatchResponse, s.sendPruneReportNotificationForTargetInternal(ctx, target, &payload.PruneReport.Result)
	case notificationdto.DispatchKindAutoHeal:
		if payload.AutoHeal == nil {
			return notificationdto.DispatchResponse{}, errors.New("auto-heal payload is required")
		}
		logManagerDispatchNotificationInternal(ctx, target, payload.Kind)
		return dispatchResponse, s.sendAutoHealNotificationForTargetInternal(ctx, target, payload.AutoHeal.ContainerName, payload.AutoHeal.ContainerID)
	default:
		return notificationdto.DispatchResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedDispatchKind, payload.Kind)
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
	if provider == models.NotificationProviderEmail {
		preserveConfig = emailCredentialPreservationConfigInternal(config, existingConfig)
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

func emailCredentialPreservationConfigInternal(config models.JSON, existingConfig models.JSON) models.JSON {
	preserveConfig := cloneNotificationConfigInternal(existingConfig)
	if authMode, _ := config["authMode"].(string); authMode == string(models.EmailAuthModeNone) {
		delete(preserveConfig, "smtpPassword")
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

// SendImageUpdateNotification dispatches a single-image update notification and

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

// logNotification records a delivery attempt in the event log so sends and
// failures are visible alongside every other Arcane event.
func (s *NotificationService) logNotification(ctx context.Context, environmentID string, provider models.NotificationProvider, subject, status string, errMsg *string, metadata models.JSON) {
	if s.eventSvc == nil {
		return
	}

	severity := models.EventSeveritySuccess
	title := fmt.Sprintf("Notification sent via %s", provider)
	description := subject
	if errMsg != nil {
		severity = models.EventSeverityError
		title = fmt.Sprintf("Notification failed via %s", provider)
		description = fmt.Sprintf("%s: %s", subject, *errMsg)
	}

	eventMetadata := cloneNotificationConfigInternal(metadata)
	eventMetadata["provider"] = string(provider)
	eventMetadata["status"] = status

	resourceType := "notification"
	providerName := string(provider)
	if _, err := s.eventSvc.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeNotificationSend,
		Severity:      severity,
		Title:         title,
		Description:   description,
		ResourceType:  &resourceType,
		ResourceName:  &providerName,
		EnvironmentID: &environmentID,
		Metadata:      eventMetadata,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to log notification event", "provider", providerName, "error", err.Error())
	}
}

// SendBatchImageUpdateNotification dispatches a batched image-update notification
// and returns the number of eligible providers it was delivered to (0 means no

// notifyEnabledProvidersInternal is the single fan-out loop behind every
// notification event: it walks all provider settings, skips disabled providers
// and providers with the event unsubscribed, dispatches to the rest, logs each
// attempt, and aggregates send errors. It returns how many providers the
// notification was actually delivered to.
func (s *NotificationService) notifyEnabledProvidersInternal(
	ctx context.Context,
	target NotificationTarget,
	eventType models.NotificationEventType,
	logRef string,
	metadata models.JSON,
	dispatch func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (handled bool, err error),
) (int, error) {
	settings, err := s.GetAllSettings(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get notification settings: %w", err)
	}

	delivered := 0
	var errs []string
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}
		if !s.isEventEnabled(setting.Config, eventType) {
			continue
		}

		handled, sendErr := dispatch(ctx, setting.Provider, setting.Config)
		if !handled {
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		if sendErr == nil {
			delivered++
		}

		status, errMsg := collectNotificationSendResultInternal(&errs, setting.Provider, sendErr)
		s.logNotification(ctx, target.EnvironmentID, setting.Provider, logRef, status, errMsg, metadata)
	}

	if len(errs) > 0 {
		return delivered, fmt.Errorf("notification errors: %s", strings.Join(errs, "; "))
	}
	return delivered, nil
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

const (
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

// --- Per-event notification content ---

func (s *NotificationService) imageUpdateNotificationContentInternal(environmentName, imageRef string, updateInfo *imageupdate.Response) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildImageUpdateNotificationMessage(format, environmentName, imageRef, updateInfo)
		}),
		Title: "Container Image Update",
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderEmailTemplate(environmentName, imageRef, updateInfo)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Container Update Available: "+notifications.SanitizeForEmail(imageRef))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *NotificationService) containerUpdateNotificationContentInternal(environmentName, containerName, imageRef, oldDigest, newDigest string) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildContainerUpdateNotificationMessage(format, environmentName, containerName, imageRef, oldDigest, newDigest)
		}),
		Title: "Container Updated",
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderContainerUpdateEmailTemplate(environmentName, containerName, imageRef, oldDigest, newDigest)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Container Updated: "+notifications.SanitizeForEmail(containerName))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *NotificationService) vulnerabilityNotificationContentInternal(environmentName string, payload VulnerabilityNotificationPayload) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildVulnerabilitySummaryNotificationMessage(
				format,
				environmentName,
				payload.CVEID,
				payload.ImageName,
				payload.FixedVersion,
				payload.Severity,
				payload.PkgName,
			)
		}),
		Title:        defaultTitle,
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderVulnerabilitySummaryEmailTemplate(environmentName, payload)
			if err != nil {
				return "", "", fmt.Errorf("failed to render summary email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, "Daily Vulnerability Summary: "+notifications.SanitizeForEmail(payload.CVEID))
			return subject, htmlBody, nil
		},
		RequireNtfyTopic:     true,
		ValidatePushoverUser: true,
	}
}

func (s *NotificationService) batchImageUpdateNotificationContentInternal(environmentName string, updates map[string]*imageupdate.Response) notifications.Content {
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildBatchImageUpdateNotificationMessage(format, environmentName, updates)
		}),
		Title: "Container Image Updates Available",
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderBatchEmailTemplate(environmentName, updates)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			updateCount := len(updates)
			plural := ""
			if updateCount > 1 {
				plural = "s"
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("%d Image Update%s Available", updateCount, plural))
			return subject, htmlBody, nil
		},
	}
}

func (s *NotificationService) pruneReportNotificationContentInternal(environmentName string, result *system.PruneAllResult) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "System Prune Report")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildPruneReportNotificationMessage(format, environmentName, result)
		}),
		Title:        defaultTitle,
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			htmlBody, _, err := s.renderPruneReportEmailTemplate(environmentName, result)
			if err != nil {
				return "", "", fmt.Errorf("failed to render email template: %w", err)
			}
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("System Prune Report: %s Reclaimed", notifications.FormatBytes(result.SpaceReclaimed)))
			return subject, htmlBody, nil
		},
	}
}

func (s *NotificationService) autoHealNotificationContentInternal(environmentName, containerName string) notifications.Content {
	defaultTitle := notifications.BuildEmailSubject(environmentName, "Auto Heal")
	return notifications.Content{
		Text: notifications.TextByFormat(func(format notifications.MessageFormat) string {
			return notifications.BuildAutoHealNotificationMessage(format, environmentName, containerName)
		}),
		Title:        defaultTitle,
		DefaultTitle: defaultTitle,
		RenderEmail: func() (string, string, error) {
			subject := notifications.BuildEmailSubject(environmentName, fmt.Sprintf("Auto Heal: Container '%s' Restarted", containerName))
			body := fmt.Sprintf(
				"<p><strong>Environment:</strong> %s</p><p><strong>Container:</strong> %s</p><p>Automatically restarted because it was unhealthy.</p>",
				html.EscapeString(environmentName),
				html.EscapeString(containerName),
			)
			return subject, body, nil
		},
	}
}

// --- Event entry points ---

// SendImageUpdateNotification dispatches a single-image update notification and
// returns the number of eligible providers it was delivered to (0 means no
// provider has this event enabled, so callers must not mark the update notified).
func (s *NotificationService) SendImageUpdateNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response, eventType models.NotificationEventType) (int, error) {
	if updateInfo == nil {
		return 0, errors.New("updateInfo is required")
	}

	if s.config != nil && s.config.AgentMode {
		dispatchResponse, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindImageUpdate,
			ImageUpdate: &notificationdto.DispatchImageUpdate{
				ImageRef:   imageRef,
				UpdateInfo: *updateInfo,
			},
		})
		if err != nil {
			return 0, err
		}
		return dispatchResponse.Delivered, nil
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return 0, err
	}

	return s.sendImageUpdateNotificationForTargetInternal(ctx, target, imageRef, updateInfo, eventType)
}

func (s *NotificationService) sendImageUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, imageRef string, updateInfo *imageupdate.Response, eventType models.NotificationEventType) (int, error) {
	metadata := models.JSON{
		"hasUpdate":     updateInfo.HasUpdate,
		"currentDigest": updateInfo.CurrentDigest,
		"latestDigest":  updateInfo.LatestDigest,
		"updateType":    updateInfo.UpdateType,
		"eventType":     string(eventType),
	}
	content := s.imageUpdateNotificationContentInternal(target.EnvironmentName, imageRef, updateInfo)
	return s.notifyEnabledProvidersInternal(ctx, target, eventType, imageRef, metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
}

func (s *NotificationService) SendContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string) error {
	if s.config != nil && s.config.AgentMode {
		_, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindContainerUpdate,
			ContainerUpdate: &notificationdto.DispatchContainerUpdate{
				ContainerName: containerName,
				ImageRef:      imageRef,
				OldDigest:     oldDigest,
				NewDigest:     newDigest,
			},
		})
		return err
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendContainerUpdateNotificationForTargetInternal(ctx, target, containerName, imageRef, oldDigest, newDigest)
}

func (s *NotificationService) sendContainerUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, containerName, imageRef, oldDigest, newDigest string) error {
	metadata := models.JSON{
		"containerName": containerName,
		"oldDigest":     oldDigest,
		"newDigest":     newDigest,
		"eventType":     string(models.NotificationEventContainerUpdate),
	}
	content := s.containerUpdateNotificationContentInternal(target.EnvironmentName, containerName, imageRef, oldDigest, newDigest)
	_, err := s.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventContainerUpdate, imageRef, metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
	return err
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
		_, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
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
		return err
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendVulnerabilityNotificationForTargetInternal(ctx, target, payload)
}

func (s *NotificationService) sendVulnerabilityNotificationForTargetInternal(ctx context.Context, target NotificationTarget, payload VulnerabilityNotificationPayload) error {
	metadata := models.JSON{
		"cveId":        payload.CVEID,
		"severity":     payload.Severity,
		"fixedVersion": payload.FixedVersion,
		"eventType":    string(models.NotificationEventVulnerabilityFound),
	}
	content := s.vulnerabilityNotificationContentInternal(target.EnvironmentName, payload)
	_, err := s.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventVulnerabilityFound, payload.ImageName, metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
	return err
}

// SendBatchImageUpdateNotification dispatches a batched image-update notification
// and returns the number of eligible providers it was delivered to (0 means no
// provider has this event enabled, so callers must not mark the updates notified).
func (s *NotificationService) SendBatchImageUpdateNotification(ctx context.Context, updates map[string]*imageupdate.Response) (int, error) {
	updatesWithChanges := filterUpdatesWithChangesInternal(updates)
	if len(updatesWithChanges) == 0 {
		return 0, nil
	}

	if s.config != nil && s.config.AgentMode {
		dispatchResponse, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindBatchImageUpdate,
			BatchImageUpdate: &notificationdto.DispatchBatchImageUpdate{
				Updates: updatesWithChanges,
			},
		})
		if err != nil {
			return 0, err
		}
		return dispatchResponse.Delivered, nil
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return 0, err
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

func (s *NotificationService) sendBatchImageUpdateNotificationForTargetInternal(ctx context.Context, target NotificationTarget, updates map[string]*imageupdate.Response) (int, error) {
	updatesWithChanges := filterUpdatesWithChangesInternal(updates)

	if len(updatesWithChanges) == 0 {
		return 0, nil
	}

	imageRefs := make([]string, 0, len(updatesWithChanges))
	for ref := range updatesWithChanges {
		imageRefs = append(imageRefs, ref)
	}

	metadata := models.JSON{
		"updateCount": len(updatesWithChanges),
		"eventType":   string(models.NotificationEventImageUpdate),
		"batch":       true,
	}
	content := s.batchImageUpdateNotificationContentInternal(target.EnvironmentName, updatesWithChanges)
	return s.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventImageUpdate, strings.Join(imageRefs, ", "), metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
}

func (s *NotificationService) SendPruneReportNotification(ctx context.Context, result *system.PruneAllResult) error {
	hasChanges := pruneResultHasChangesInternal(result)
	hasErrors := result != nil && len(result.Errors) > 0
	if !hasChanges && !hasErrors {
		slog.InfoContext(ctx, "skipping prune report notification because no resources were pruned and no errors were reported")
		return nil
	}

	if s.config != nil && s.config.AgentMode {
		_, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindPruneReport,
			PruneReport: &notificationdto.DispatchPruneReport{
				Result: *result,
			},
		})
		return err
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

	metadata := models.JSON{
		"spaceReclaimed": result.SpaceReclaimed,
		"eventType":      string(models.NotificationEventPruneReport),
	}
	content := s.pruneReportNotificationContentInternal(target.EnvironmentName, result)
	_, err := s.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventPruneReport, "System Prune Report", metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
	if err != nil {
		return err
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

// SendAutoHealNotification sends a notification when a container is auto-healed.
func (s *NotificationService) SendAutoHealNotification(ctx context.Context, containerName, containerID string) error {
	if s.config != nil && s.config.AgentMode {
		_, err := s.dispatchNotificationToManagerInternal(ctx, notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKindAutoHeal,
			AutoHeal: &notificationdto.DispatchAutoHeal{
				ContainerName: containerName,
				ContainerID:   containerID,
			},
		})
		return err
	}

	target, err := s.resolveNotificationTargetInternal(ctx, "")
	if err != nil {
		return err
	}

	return s.sendAutoHealNotificationForTargetInternal(ctx, target, containerName, containerID)
}

func (s *NotificationService) sendAutoHealNotificationForTargetInternal(ctx context.Context, target NotificationTarget, containerName, containerID string) error {
	metadata := models.JSON{
		"containerID": containerID,
		"eventType":   string(models.NotificationEventAutoHeal),
	}
	content := s.autoHealNotificationContentInternal(target.EnvironmentName, containerName)
	_, err := s.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventAutoHeal, containerName, metadata, func(ctx context.Context, provider models.NotificationProvider, config models.JSON) (bool, error) {
		return notifications.Deliver(ctx, provider, config, content)
	})
	return err
}

// --- Test notifications ---

// notificationEventTypeForTestTypeInternal maps a test type to the event type a
// real notification of that kind would be gated on ("" = no event gate).
func notificationEventTypeForTestTypeInternal(testType string) models.NotificationEventType {
	switch testType {
	case notificationTestTypeImageUpdate, notificationTestTypeBatchImageUpdate:
		return models.NotificationEventImageUpdate
	case notificationTestTypeVulnerability:
		return models.NotificationEventVulnerabilityFound
	case notificationTestTypePruneReport:
		return models.NotificationEventPruneReport
	case notificationTestTypeAutoHeal:
		return models.NotificationEventAutoHeal
	default:
		return ""
	}
}

// testNotificationWarningInternal reports why a real notification would not send
// even though the test did: the provider is disabled, or the tested event type is
// unsubscribed. Empty means real notifications would send.
func (s *NotificationService) testNotificationWarningInternal(setting *models.NotificationSettings, testType string) string {
	if !setting.Enabled {
		return fmt.Sprintf("%s is disabled, so real notifications will not send", setting.Provider)
	}
	if eventType := notificationEventTypeForTestTypeInternal(testType); eventType != "" && !s.isEventEnabled(setting.Config, eventType) {
		return fmt.Sprintf("%s events are disabled for %s, so real notifications will not send", eventType, setting.Provider)
	}
	return ""
}

func (s *NotificationService) testNotificationContentInternal(environmentName, testType string) notifications.Content {
	switch testType {
	case notificationTestTypeVulnerability:
		return s.vulnerabilityNotificationContentInternal(environmentName, VulnerabilityNotificationPayload{
			CVEID:        "Daily Summary - " + time.Now().UTC().Format("2006-01-02"),
			Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
			ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
			FixedVersion: "7 fixable vulnerability record(s)",
			PkgName:      "CVE-2025-1234, CVE-2025-5678, CVE-2026-0001",
		})
	case notificationTestTypeAutoHeal:
		return s.autoHealNotificationContentInternal(environmentName, "test-container")
	case notificationTestTypePruneReport:
		return s.pruneReportNotificationContentInternal(environmentName, &system.PruneAllResult{
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
		})
	case notificationTestTypeBatchImageUpdate:
		return s.batchImageUpdateNotificationContentInternal(environmentName, map[string]*imageupdate.Response{
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
		})
	default: // simple and image-update
		imageRef := "nginx:latest"
		if testType == notificationTestTypeSimple {
			imageRef = "test/image:latest"
		}
		return s.imageUpdateNotificationContentInternal(environmentName, imageRef, &imageupdate.Response{
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentDigest:  "sha256:abc123def456789012345678901234567890",
			LatestDigest:   "sha256:xyz789ghi012345678901234567890123456",
			CheckTime:      time.Now(),
			ResponseTimeMs: 100,
		})
	}
}

// TestNotification sends a test message to the provider regardless of its enabled
// state (testing before enabling is legitimate) and returns a warning when a real
// notification of the tested kind would not send.
func (s *NotificationService) TestNotification(ctx context.Context, environmentID string, provider models.NotificationProvider, testType string) (string, error) {
	setting, err := s.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return "", fmt.Errorf("please save your %s settings before testing", provider)
	}
	testType = strings.TrimSpace(testType)
	if testType == "" {
		testType = notificationTestTypeSimple
	}
	if _, ok := supportedNotificationTestTypes[testType]; !ok {
		return "", fmt.Errorf("unsupported notification test type: %s", testType)
	}
	warning := s.testNotificationWarningInternal(setting, testType)

	target, err := s.resolveNotificationTargetInternal(ctx, environmentID)
	if err != nil {
		return "", err
	}

	if provider == models.NotificationProviderEmail && testType == notificationTestTypeSimple {
		return warning, s.sendTestEmail(ctx, target.EnvironmentName, setting.Config)
	}

	content := s.testNotificationContentInternal(target.EnvironmentName, testType)
	handled, sendErr := notifications.Deliver(ctx, provider, setting.Config, content)
	if !handled {
		return "", unknownNotificationProviderErrorInternal(provider)
	}
	return warning, sendErr
}

const logoURLPath = "/api/app-images/logo-email"

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
