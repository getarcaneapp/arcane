package services

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/notifications"
	"github.com/getarcaneapp/arcane/backend/resources"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

const logoURLPath = "/api/app-images/logo-email"

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

func (s *NotificationService) CreateOrUpdateSettings(ctx context.Context, id uint, name string, provider models.NotificationProvider, enabled bool, config models.JSON) (*models.NotificationSettings, error) {
	var setting models.NotificationSettings

	// Clear config if provider is disabled
	if !enabled {
		config = models.JSON{}
	} else {
		// Build Shoutrrr URL from config fields
		urlStr, err := notifications.BuildShoutrrrURL(string(provider), config)
		if err != nil {
			return nil, fmt.Errorf("failed to build notification URL: %w", err)
		}
		config["url"] = urlStr
	}

	if id > 0 {
		if err := s.db.WithContext(ctx).First(&setting, id).Error; err != nil {
			return nil, fmt.Errorf("failed to find notification settings: %w", err)
		}
		setting.Name = name
		setting.Provider = provider
		setting.Enabled = enabled
		setting.Config = config
		if err := s.db.WithContext(ctx).Save(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to update notification settings: %w", err)
		}
	} else {
		setting = models.NotificationSettings{
			Name:     name,
			Provider: provider,
			Enabled:  enabled,
			Config:   config,
		}
		if err := s.db.WithContext(ctx).Create(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to create notification settings: %w", err)
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
		var message string
		var title string
		var isHTML bool

		if setting.Provider == "email" {
			htmlBody, _, err := s.renderEmailTemplate(imageRef, updateInfo)
			if err != nil {
				sendErr = err
			} else {
				message = htmlBody
				title = fmt.Sprintf("Container Update Available: %s", imageRef)
				isHTML = true
			}
		} else {
			message = fmt.Sprintf("Image Update Available for %s. New digest: %s. Type: %s.", imageRef, truncateDigest(updateInfo.LatestDigest), updateInfo.UpdateType)
			title = "Image Update Available"
		}

		if sendErr == nil {
			urlStr, err := s.getURLFromConfig(setting.Config)
			if err != nil {
				sendErr = err
			} else {
				sendErr = s.sendShoutrrrNotification(ctx, urlStr, message, title, isHTML)
			}
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

func (s *NotificationService) getURLFromConfig(config models.JSON) (string, error) {
	if config == nil {
		return "", fmt.Errorf("config is empty")
	}
	urlVal, ok := config["url"]
	if !ok {
		return "", fmt.Errorf("url not found in config")
	}
	urlStr, ok := urlVal.(string)
	if !ok {
		return "", fmt.Errorf("url is not a string")
	}
	return urlStr, nil
}

func (s *NotificationService) sendShoutrrrNotification(ctx context.Context, urlStr string, message string, title string, isHTML bool) error {
	// NOTE: Shoutrrr SMTP v0.8.0 uses the *service* config (parsed from the URL) when building headers,
	// but uses the per-send config (URL + params) when deciding whether to write a multipart body.
	// If UseHTML is only supplied via params, the body becomes multipart while the top-level
	// Content-Type header stays text/plain, causing clients to show raw HTML.
	//
	// Workaround: force `usehtml=Yes` into the SMTP URL whenever we're sending HTML.
	urlStr, err := ensureSMTPUseHTML(urlStr, isHTML)
	if err != nil {
		return err
	}

	sender, err := shoutrrr.CreateSender(urlStr)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr sender: %w", err)
	}

	params := &types.Params{}
	if title != "" {
		params.SetTitle(title)
	}
	// Intentionally do NOT set `usehtml` via params here.
	// See comment above; we want UseHTML to come from the URL so headers are correct.

	errs := sender.Send(message, params)
	if len(errs) > 0 {
		var errMsgs []string
		for _, e := range errs {
			if e != nil {
				errMsgs = append(errMsgs, e.Error())
			}
		}
		if len(errMsgs) > 0 {
			return fmt.Errorf("shoutrrr send failed: %s", strings.Join(errMsgs, "; "))
		}
	}
	return nil
}

func ensureSMTPUseHTML(urlStr string, useHTML bool) (string, error) {
	if !useHTML {
		return urlStr, nil
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid notification url: %w", err)
	}

	if strings.ToLower(u.Scheme) != "smtp" {
		return urlStr, nil
	}

	q := u.Query()
	// Shoutrrr expects Yes/No for bools in URL query strings.
	q.Set("usehtml", "Yes")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// isEventEnabled checks if a specific event type is enabled in the config
func (s *NotificationService) isEventEnabled(config models.JSON, eventType models.NotificationEventType) bool {
	if config == nil {
		return true // Default to enabled if no config
	}

	eventsRaw, ok := config["events"]
	if !ok {
		return true // If no events config, default to enabled
	}

	var events map[string]interface{}
	switch v := eventsRaw.(type) {
	case map[string]interface{}:
		events = v
	case models.JSON:
		events = v
	default:
		return true // If we can't parse, default to enabled
	}

	enabledRaw, ok := events[string(eventType)]
	if !ok {
		return true // If event type not specified, default to enabled
	}

	enabled, ok := enabledRaw.(bool)
	if !ok {
		return true
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
		var message string
		var title string
		var isHTML bool

		if setting.Provider == "email" {
			htmlBody, _, err := s.renderContainerUpdateEmailTemplate(containerName, imageRef, oldDigest, newDigest)
			if err != nil {
				sendErr = err
			} else {
				message = htmlBody
				title = fmt.Sprintf("Container Updated: %s", containerName)
				isHTML = true
			}
		} else {
			message = fmt.Sprintf("Container %s (%s) updated to %s.", containerName, imageRef, truncateDigest(newDigest))
			title = "Container Updated"
		}

		if sendErr == nil {
			urlStr, err := s.getURLFromConfig(setting.Config)
			if err != nil {
				sendErr = err
			} else {
				sendErr = s.sendShoutrrrNotification(ctx, urlStr, message, title, isHTML)
			}
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

func (s *NotificationService) renderEmailTemplate(imageRef string, updateInfo *imageupdate.Response) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]interface{}{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   "Local Docker",
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

func (s *NotificationService) renderContainerUpdateEmailTemplate(containerName, imageRef, oldDigest, newDigest string) (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]interface{}{
		"LogoURL":       logoURL,
		"AppURL":        appURL,
		"Environment":   "Local Docker",
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

	var message string
	var title string
	var isHTML bool

	if provider == "email" {
		switch testType {
		case "image-update":
			htmlBody, _, err := s.renderEmailTemplate("nginx:latest", testUpdate)
			if err != nil {
				return err
			}
			message = htmlBody
			title = "Container Update Available: nginx:latest"
			isHTML = true
		case "batch-image-update":
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
			htmlBody, _, err := s.renderBatchEmailTemplate(testUpdates)
			if err != nil {
				return err
			}
			message = htmlBody
			title = "3 Image Updates Available"
			isHTML = true
		default:
			htmlBody, _, err := s.renderTestEmailTemplate()
			if err != nil {
				return err
			}
			message = htmlBody
			title = "Test Email from Arcane"
			isHTML = true
		}
	} else {
		message = "Test notification from Arcane"
		title = "Test Notification"
	}

	// Use Shoutrrr for everything
	urlStr, err := s.getURLFromConfig(setting.Config)
	if err != nil {
		return err
	}
	return s.sendShoutrrrNotification(ctx, urlStr, message, title, isHTML)
}

func (s *NotificationService) renderTestEmailTemplate() (string, string, error) {
	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]interface{}{
		"LogoURL": logoURL,
		"AppURL":  appURL,
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
		if err := s.sendBatchNotificationToProvider(ctx, setting, updatesWithChanges); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

func (s *NotificationService) sendBatchNotificationToProvider(ctx context.Context, setting models.NotificationSettings, updatesWithChanges map[string]*imageupdate.Response) error {
	if !setting.Enabled {
		return nil
	}

	if !s.isEventEnabled(setting.Config, models.NotificationEventImageUpdate) {
		return nil
	}

	var sendErr error
	var message string
	var title string
	var isHTML bool

	if setting.Provider == "email" {
		htmlBody, _, err := s.renderBatchEmailTemplate(updatesWithChanges)
		if err != nil {
			sendErr = err
		} else {
			message = htmlBody
			updateCount := len(updatesWithChanges)
			title = fmt.Sprintf("%d Image Update%s Available", updateCount, func() string {
				if updateCount > 1 {
					return "s"
				}
				return ""
			}())
			isHTML = true
		}
	} else {
		message = fmt.Sprintf("%d Image Updates Available:\n", len(updatesWithChanges))
		for ref, update := range updatesWithChanges {
			message += fmt.Sprintf("- %s: %s -> %s\n", ref, truncateDigest(update.CurrentDigest), truncateDigest(update.LatestDigest))
		}
		title = "Batch Image Updates"
	}

	if sendErr == nil {
		urlStr, err := s.getURLFromConfig(setting.Config)
		if err != nil {
			sendErr = err
		} else {
			sendErr = s.sendShoutrrrNotification(ctx, urlStr, message, title, isHTML)
		}
	}

	status := "success"
	var errMsg *string
	if sendErr != nil {
		status = "failed"
		msg := sendErr.Error()
		errMsg = &msg
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

	if sendErr != nil {
		return fmt.Errorf("%s: %s", setting.Provider, *errMsg)
	}
	return nil
}

func (s *NotificationService) renderBatchEmailTemplate(updates map[string]*imageupdate.Response) (string, string, error) {
	// Build list of image names
	imageList := make([]string, 0, len(updates))
	for imageRef := range updates {
		imageList = append(imageList, imageRef)
	}

	appURL := s.config.GetAppURL()
	logoURL := appURL + logoURLPath
	data := map[string]interface{}{
		"LogoURL":     logoURL,
		"AppURL":      appURL,
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

func truncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}
