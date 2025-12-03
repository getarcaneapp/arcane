package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/resources"
	"go.getarcane.app/types/imageupdate"
)

type NotificationService struct {
	db             *database.DB
	config         *config.Config
	appriseService *AppriseService
}

func NewNotificationService(db *database.DB, cfg *config.Config) *NotificationService {
	return &NotificationService{
		db:             db,
		config:         cfg,
		appriseService: NewAppriseService(db, cfg),
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
	// Send to Apprise if enabled (don't block on error)
	if appriseErr := s.appriseService.SendImageUpdateNotification(ctx, imageRef, updateInfo); appriseErr != nil {
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
			sendErr = s.sendDiscordNotification(ctx, imageRef, updateInfo, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailNotification(ctx, imageRef, updateInfo, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = &msg
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

	var configMap map[string]interface{}
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		return true // Default to enabled if we can't parse
	}

	events, ok := configMap["events"].(map[string]interface{})
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
	// Send to Apprise if enabled (don't block on error)
	if appriseErr := s.appriseService.SendContainerUpdateNotification(ctx, containerName, imageRef, oldDigest, newDigest); appriseErr != nil {
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
			sendErr = s.sendDiscordContainerUpdateNotification(ctx, containerName, imageRef, oldDigest, newDigest, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendEmailContainerUpdateNotification(ctx, containerName, imageRef, oldDigest, newDigest, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = &msg
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

func (s *NotificationService) sendDiscordNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Discord config: %w", err)
	}

	if discordConfig.WebhookURL == "" {
		return fmt.Errorf("discord webhook URL not configured")
	}

	// Decrypt webhook URL if encrypted
	webhookURL := discordConfig.WebhookURL
	if decrypted, err := utils.Decrypt(webhookURL); err == nil {
		webhookURL = decrypted
	}

	// Validate webhook URL to prevent SSRF
	if err := validateWebhookURL(webhookURL); err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	username := discordConfig.Username
	if username == "" {
		username = "Arcane"
	}

	color := 3447003 // Blue
	if updateInfo.HasUpdate {
		color = 15844367 // Gold for updates
	}

	fields := []map[string]interface{}{
		{
			"name":   "Image",
			"value":  imageRef,
			"inline": false,
		},
		{
			"name":   "Update Available",
			"value":  fmt.Sprintf("%t", updateInfo.HasUpdate),
			"inline": true,
		},
		{
			"name":   "Update Type",
			"value":  updateInfo.UpdateType,
			"inline": true,
		},
	}

	if updateInfo.CurrentDigest != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Current Digest",
			"value":  truncateDigest(updateInfo.CurrentDigest),
			"inline": true,
		})
	}
	if updateInfo.LatestDigest != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Latest Digest",
			"value":  truncateDigest(updateInfo.LatestDigest),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"username": username,
		"embeds": []map[string]interface{}{
			{
				"title":       "🔔 Container Image Update Available",
				"description": fmt.Sprintf("A new update has been detected for **%s**", imageRef),
				"color":       color,
				"fields":      fields,
				"timestamp":   time.Now().Format(time.RFC3339),
			},
		},
	}

	if discordConfig.AvatarURL != "" {
		payload["avatar_url"] = discordConfig.AvatarURL
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create Discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *NotificationService) sendEmailNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response, config models.JSON) error {
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
		if decrypted, err := utils.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		}
	}

	htmlBody, textBody, err := s.renderEmailTemplate(imageRef, updateInfo)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := fmt.Sprintf("Container Update Available: %s", notifications.SanitizeForEmail(imageRef))
	message := notifications.BuildMultipartMessage(emailConfig.FromAddress, emailConfig.ToAddresses, subject, htmlBody, textBody)

	client, err := notifications.ConnectSMTP(ctx, emailConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	if err := client.SendMessage(emailConfig.FromAddress, emailConfig.ToAddresses, message); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderEmailTemplate(imageRef string, updateInfo *imageupdate.Response) (string, string, error) {
	data := map[string]interface{}{
		"LogoURL":       "https://raw.githubusercontent.com/getarcaneapp/arcane/main/backend/resources/images/logo-full.svg",
		"AppURL":        s.config.AppUrl,
		"ImageRef":      imageRef,
		"HasUpdate":     updateInfo.HasUpdate,
		"UpdateType":    updateInfo.UpdateType,
		"CurrentDigest": truncateDigest(updateInfo.CurrentDigest),
		"LatestDigest":  truncateDigest(updateInfo.LatestDigest),
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

func (s *NotificationService) sendDiscordContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal Discord config: %w", err)
	}

	if discordConfig.WebhookURL == "" {
		return fmt.Errorf("discord webhook URL not configured")
	}

	webhookURL := discordConfig.WebhookURL
	if decrypted, err := utils.Decrypt(webhookURL); err == nil {
		webhookURL = decrypted
	}

	if err := validateWebhookURL(webhookURL); err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	username := discordConfig.Username
	if username == "" {
		username = "Arcane"
	}

	fields := []map[string]interface{}{
		{
			"name":   "Container",
			"value":  containerName,
			"inline": false,
		},
		{
			"name":   "Image",
			"value":  imageRef,
			"inline": false,
		},
		{
			"name":   "Status",
			"value":  "✅ Updated Successfully",
			"inline": false,
		},
	}

	if oldDigest != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Previous Version",
			"value":  truncateDigest(oldDigest),
			"inline": true,
		})
	}
	if newDigest != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Current Version",
			"value":  truncateDigest(newDigest),
			"inline": true,
		})
	}

	embed := map[string]interface{}{
		"title":       "Container Successfully Updated",
		"description": "Your container has been updated with the latest image version.",
		"color":       5025616, // Green color for success
		"fields":      fields,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	payload := map[string]interface{}{
		"username": username,
		"embeds":   []map[string]interface{}{embed},
	}

	if discordConfig.AvatarURL != "" {
		payload["avatar_url"] = discordConfig.AvatarURL
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (s *NotificationService) sendEmailContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string, config models.JSON) error {
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
		if decrypted, err := utils.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		}
	}

	htmlBody, textBody, err := s.renderContainerUpdateEmailTemplate(containerName, imageRef, oldDigest, newDigest)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	subject := fmt.Sprintf("Container Updated: %s", notifications.SanitizeForEmail(containerName))
	message := notifications.BuildMultipartMessage(emailConfig.FromAddress, emailConfig.ToAddresses, subject, htmlBody, textBody)

	client, err := notifications.ConnectSMTP(ctx, emailConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	if err := client.SendMessage(emailConfig.FromAddress, emailConfig.ToAddresses, message); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderContainerUpdateEmailTemplate(containerName, imageRef, oldDigest, newDigest string) (string, string, error) {
	data := map[string]interface{}{
		"LogoURL":       "https://raw.githubusercontent.com/getarcaneapp/arcane/main/backend/resources/images/logo-full.svg",
		"AppURL":        s.config.AppUrl,
		"ContainerName": containerName,
		"ImageRef":      imageRef,
		"OldDigest":     truncateDigest(oldDigest),
		"NewDigest":     truncateDigest(newDigest),
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

func (s *NotificationService) TestNotification(ctx context.Context, provider models.NotificationProvider, testType string) error {
	setting, err := s.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return fmt.Errorf("please save your %s settings before testing", provider)
	}

	testUpdate := &imageupdate.Response{
		HasUpdate:      true,
		UpdateType:     "digest",
		CurrentDigest:  "sha256:abc123def456789012345678901234567890",
		LatestDigest:   "sha256:xyz789ghi012345678901234567890123456",
		CheckTime:      time.Now(),
		ResponseTimeMs: 100,
	}

	switch provider {
	case models.NotificationProviderDiscord:
		return s.sendDiscordNotification(ctx, "test/image:latest", testUpdate, setting.Config)
	case models.NotificationProviderEmail:
		if testType == "image-update" {
			return s.sendEmailNotification(ctx, "nginx:latest", testUpdate, setting.Config)
		}
		return s.sendTestEmail(ctx, setting.Config)
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
}

func (s *NotificationService) sendTestEmail(ctx context.Context, config models.JSON) error {
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
		if decrypted, err := utils.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		}
	}

	htmlBody, textBody, err := s.renderTestEmailTemplate()
	if err != nil {
		return fmt.Errorf("failed to render test email template: %w", err)
	}

	subject := "Test Email from Arcane"
	message := notifications.BuildMultipartMessage(emailConfig.FromAddress, emailConfig.ToAddresses, subject, htmlBody, textBody)

	client, err := notifications.ConnectSMTP(ctx, emailConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	if err := client.SendMessage(emailConfig.FromAddress, emailConfig.ToAddresses, message); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderTestEmailTemplate() (string, string, error) {
	data := map[string]interface{}{
		"LogoURL": "https://raw.githubusercontent.com/getarcaneapp/arcane/main/backend/resources/images/logo-full.svg",
		"AppURL":  s.config.AppUrl,
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
		slog.WarnContext(ctx, "Failed to log notification",
			slog.String("provider", string(provider)),
			slog.String("error", err.Error()))
	}
}

func (s *NotificationService) SendBatchImageUpdateNotification(ctx context.Context, updates map[string]*imageupdate.Response) error {
	if len(updates) == 0 {
		return nil
	}

	updatesWithChanges := make(map[string]*imageupdate.Response)
	for imageRef, update := range updates {
		if update != nil && update.HasUpdate {
			updatesWithChanges[imageRef] = update
		}
	}

	if len(updatesWithChanges) == 0 {
		return nil
	}

	// Send to Apprise if enabled
	if appriseErr := s.appriseService.SendBatchImageUpdateNotification(ctx, updatesWithChanges); appriseErr != nil {
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
			sendErr = s.sendBatchDiscordNotification(ctx, updatesWithChanges, setting.Config)
		case models.NotificationProviderEmail:
			sendErr = s.sendBatchEmailNotification(ctx, updatesWithChanges, setting.Config)
		default:
			slog.WarnContext(ctx, "Unknown notification provider", "provider", setting.Provider)
			continue
		}

		status := "success"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			msg := sendErr.Error()
			errMsg = &msg
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

func (s *NotificationService) sendBatchDiscordNotification(ctx context.Context, updates map[string]*imageupdate.Response, config models.JSON) error {
	var discordConfig models.DiscordConfig
	configBytes, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal discord config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &discordConfig); err != nil {
		return fmt.Errorf("failed to unmarshal discord config: %w", err)
	}

	if err := validateWebhookURL(discordConfig.WebhookURL); err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	fields := make([]map[string]interface{}, 0, len(updates))
	for imageRef, update := range updates {
		fields = append(fields, map[string]interface{}{
			"name": imageRef,
			"value": fmt.Sprintf(
				"**Type:** %s\n**Current:** `%s`\n**Latest:** `%s`",
				update.UpdateType,
				truncateDigest(update.CurrentDigest),
				truncateDigest(update.LatestDigest),
			),
			"inline": false,
		})
	}

	title := "Container Image Updates Available"
	description := fmt.Sprintf("%d container image(s) have updates available.", len(updates))
	if len(updates) == 1 {
		description = "1 container image has an update available."
	}

	embed := map[string]interface{}{
		"title":       title,
		"description": description,
		"color":       3447003,
		"fields":      fields,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}

	if discordConfig.Username != "" {
		payload["username"] = discordConfig.Username
	}
	if discordConfig.AvatarURL != "" {
		payload["avatar_url"] = discordConfig.AvatarURL
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discordConfig.WebhookURL, bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord webhook failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (s *NotificationService) sendBatchEmailNotification(ctx context.Context, updates map[string]*imageupdate.Response, config models.JSON) error {
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
		if decrypted, err := utils.Decrypt(emailConfig.SMTPPassword); err == nil {
			emailConfig.SMTPPassword = decrypted
		}
	}

	htmlBody, textBody, err := s.renderBatchEmailTemplate(updates)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	updateCount := len(updates)
	subject := fmt.Sprintf("%d Image Update%s Available", updateCount, func() string {
		if updateCount > 1 {
			return "s"
		}
		return ""
	}())
	message := notifications.BuildMultipartMessage(emailConfig.FromAddress, emailConfig.ToAddresses, subject, htmlBody, textBody)

	client, err := notifications.ConnectSMTP(ctx, emailConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	if err := client.SendMessage(emailConfig.FromAddress, emailConfig.ToAddresses, message); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *NotificationService) renderBatchEmailTemplate(updates map[string]*imageupdate.Response) (string, string, error) {
	data := map[string]interface{}{
		"LogoURL":     "https://raw.githubusercontent.com/getarcaneapp/arcane/main/backend/resources/images/logo-full.svg",
		"AppURL":      s.config.AppUrl,
		"UpdateCount": len(updates),
		"CheckTime":   time.Now().Format(time.RFC1123),
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

func truncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}

// validateWebhookURL validates that the webhook URL is a valid Discord webhook URL
// This prevents SSRF attacks by ensuring the URL points to Discord's API
func validateWebhookURL(webhookURL string) error {
	parsedURL, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("failed to parse webhook URL: %w", err)
	}

	// Ensure it's HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("webhook URL must use HTTPS")
	}

	// Validate it's a Discord webhook URL
	validHosts := []string{
		"discord.com",
		"discordapp.com",
	}

	isValid := false
	for _, validHost := range validHosts {
		if parsedURL.Host == validHost || strings.HasSuffix(parsedURL.Host, "."+validHost) {
			isValid = true
			break
		}
	}

	if !isValid {
		return fmt.Errorf("webhook URL must be a Discord webhook URL")
	}

	// Validate it's a webhook path
	if !strings.HasPrefix(parsedURL.Path, "/api/webhooks/") {
		return fmt.Errorf("invalid Discord webhook path")
	}

	return nil
}
