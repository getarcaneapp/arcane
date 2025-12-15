package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/imageupdate"
)

type AppriseService struct {
	db     *database.DB
	config *config.Config
}

func NewAppriseService(db *database.DB, cfg *config.Config) *AppriseService {
	return &AppriseService{
		db:     db,
		config: cfg,
	}
}

type AppriseNotificationPayload struct {
	Body   string   `json:"body"`
	Title  string   `json:"title,omitempty"`
	Type   string   `json:"type,omitempty"`
	Tag    []string `json:"tag,omitempty"`
	Format string   `json:"format,omitempty"`
}

type NtfyNotificationPayload struct {
	Topic    string   `json:"topic,omitempty"`
	Message  string   `json:"message"`
	Title    string   `json:"title,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

type ntfyURLInfo struct {
	isNtfy   bool
	httpURL  string
	topic    string
}

func parseNtfyURL(apiURL string) ntfyURLInfo {
	apiURL = strings.TrimSpace(apiURL)

	if strings.HasPrefix(apiURL, "ntfys://") {
		rest := strings.TrimPrefix(apiURL, "ntfys://")
		parts := strings.SplitN(rest, "/", 2)
		host := parts[0]
		topic := ""
		if len(parts) > 1 {
			topic = strings.Trim(parts[1], "/")
		}
		httpURL := "https://" + host
		if topic != "" {
			httpURL = httpURL + "/" + topic
		}
		return ntfyURLInfo{isNtfy: true, httpURL: httpURL, topic: topic}
	}

	if strings.HasPrefix(apiURL, "ntfy://") {
		rest := strings.TrimPrefix(apiURL, "ntfy://")
		parts := strings.SplitN(rest, "/", 2)
		host := parts[0]
		topic := ""
		if len(parts) > 1 {
			topic = strings.Trim(parts[1], "/")
		}
		httpURL := "http://" + host
		if topic != "" {
			httpURL = httpURL + "/" + topic
		}
		return ntfyURLInfo{isNtfy: true, httpURL: httpURL, topic: topic}
	}

	parsed, err := url.Parse(apiURL)
	if err != nil {
		return ntfyURLInfo{isNtfy: false, httpURL: apiURL, topic: ""}
	}

	host := strings.ToLower(parsed.Host)
	if strings.Contains(host, "ntfy") {
		topic := strings.Trim(parsed.Path, "/")
		return ntfyURLInfo{isNtfy: true, httpURL: apiURL, topic: topic}
	}

	return ntfyURLInfo{isNtfy: false, httpURL: apiURL, topic: ""}
}

func (s *AppriseService) GetSettings(ctx context.Context) (*models.AppriseSettings, error) {
	var settings models.AppriseSettings
	if err := s.db.WithContext(ctx).First(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *AppriseService) CreateOrUpdateSettings(ctx context.Context, apiURL string, enabled bool, imageUpdateTag, containerUpdateTag string) (*models.AppriseSettings, error) {
	var settings models.AppriseSettings

	err := s.db.WithContext(ctx).First(&settings).Error
	if err != nil {
		settings = models.AppriseSettings{
			APIURL:             apiURL,
			Enabled:            enabled,
			ImageUpdateTag:     imageUpdateTag,
			ContainerUpdateTag: containerUpdateTag,
		}
		if err := s.db.WithContext(ctx).Create(&settings).Error; err != nil {
			return nil, fmt.Errorf("failed to create apprise settings: %w", err)
		}
	} else {
		settings.APIURL = apiURL
		settings.Enabled = enabled
		settings.ImageUpdateTag = imageUpdateTag
		settings.ContainerUpdateTag = containerUpdateTag
		if err := s.db.WithContext(ctx).Save(&settings).Error; err != nil {
			return nil, fmt.Errorf("failed to update apprise settings: %w", err)
		}
	}

	return &settings, nil
}

func (s *AppriseService) SendNotification(ctx context.Context, title, body, format string, notificationType models.NotificationEventType) error {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get apprise settings: %w", err)
	}

	if !settings.Enabled {
		return nil
	}

	if settings.APIURL == "" {
		return fmt.Errorf("apprise API URL not configured")
	}

	var tags []string
	switch notificationType {
	case models.NotificationEventImageUpdate:
		if settings.ImageUpdateTag != "" {
			tags = []string{settings.ImageUpdateTag}
		}
	case models.NotificationEventContainerUpdate:
		if settings.ContainerUpdateTag != "" {
			tags = []string{settings.ContainerUpdateTag}
		}
	}

	ntfyInfo := parseNtfyURL(settings.APIURL)

	var jsonData []byte
	var targetURL string

	if ntfyInfo.isNtfy {
		ntfyPayload := NtfyNotificationPayload{
			Message:  body,
			Title:    title,
			Tags:     tags,
			Priority: 3,
		}

		if ntfyInfo.topic != "" {
			targetURL = ntfyInfo.httpURL
		} else {
			ntfyPayload.Topic = "arcane"
			parsed, _ := url.Parse(ntfyInfo.httpURL)
			parsed.Path = ""
			targetURL = parsed.String()
		}

		jsonData, err = json.Marshal(ntfyPayload)
		if err != nil {
			return fmt.Errorf("failed to marshal ntfy payload: %w", err)
		}

		slog.InfoContext(ctx, "Sending ntfy notification",
			slog.String("url", targetURL),
			slog.String("title", title),
			slog.String("topic", ntfyInfo.topic),
			slog.Any("tags", tags))
	} else {
		payload := AppriseNotificationPayload{
			Title:  title,
			Body:   body,
			Type:   "info",
			Tag:    tags,
			Format: format,
		}

		jsonData, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal notification payload: %w", err)
		}

		targetURL = settings.APIURL

		slog.InfoContext(ctx, "Sending Apprise notification",
			slog.String("url", settings.APIURL),
			slog.String("title", title),
			slog.Any("tags", tags),
			slog.String("type", string(notificationType)))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Notification API returned error",
			slog.Int("status", resp.StatusCode),
			slog.String("response", bodyString),
			slog.String("url", targetURL))
		return fmt.Errorf("notification API returned status %d: %s", resp.StatusCode, bodyString)
	}

	slog.InfoContext(ctx, "Notification sent successfully",
		slog.Int("status", resp.StatusCode),
		slog.String("response", bodyString))

	return nil
}

func (s *AppriseService) SendImageUpdateNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response) error {
	title := fmt.Sprintf("Container Image Update Available: %s", imageRef)
	body := fmt.Sprintf(
		"Image: %s\nUpdate Type: %s\nCurrent Digest: %s\nLatest Digest: %s",
		imageRef,
		updateInfo.UpdateType,
		truncateDigest(updateInfo.CurrentDigest),
		truncateDigest(updateInfo.LatestDigest),
	)
	return s.SendNotification(ctx, title, body, "text", models.NotificationEventImageUpdate)
}

func (s *AppriseService) SendContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string) error {
	title := fmt.Sprintf("Container Updated: %s", containerName)
	body := fmt.Sprintf(
		"Container: %s\nImage: %s\nPrevious Version: %s\nCurrent Version: %s\nStatus: Updated Successfully",
		containerName,
		imageRef,
		truncateDigest(oldDigest),
		truncateDigest(newDigest),
	)
	return s.SendNotification(ctx, title, body, "text", models.NotificationEventContainerUpdate)
}

func (s *AppriseService) SendBatchImageUpdateNotification(ctx context.Context, updates map[string]*imageupdate.Response) error {
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

	title := fmt.Sprintf("%d Container Image Update(s) Available", len(updatesWithChanges))
	body := "The following images have updates available:\n\n"

	for imageRef, update := range updatesWithChanges {
		body += fmt.Sprintf("• %s\n  Type: %s\n  Current: %s\n  Latest: %s\n\n",
			imageRef,
			update.UpdateType,
			truncateDigest(update.CurrentDigest),
			truncateDigest(update.LatestDigest),
		)
	}

	return s.SendNotification(ctx, title, body, "text", models.NotificationEventImageUpdate)
}

func (s *AppriseService) TestNotification(ctx context.Context) error {
	title := "Test Notification from Arcane"
	body := "If you're reading this, your Apprise integration is working correctly!"
	return s.SendNotification(ctx, title, body, "text", models.NotificationEventImageUpdate)
}
