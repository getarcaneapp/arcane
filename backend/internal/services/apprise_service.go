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
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	"gorm.io/gorm"
)

// AppriseService handles sending notifications through Apprise API
//
// Deprecated: Built-in providers (e.g., SMTP via Shoutrrr) are preferred.
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

func (s *AppriseService) GetSettings(ctx context.Context) (*models.AppriseSettings, error) {
	var settings models.AppriseSettings
	if err := s.db.WithContext(ctx).First(&settings).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &settings, nil
}

func (s *AppriseService) CreateOrUpdateSettings(ctx context.Context, apiURL string, enabled bool, imageUpdateTag, containerUpdateTag string) (*models.AppriseSettings, error) {
	var settings models.AppriseSettings

	err := s.db.WithContext(ctx).First(&settings).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to check apprise settings: %w", err)
		}
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

	if settings == nil || !settings.Enabled {
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

	case models.NotificationEventPruneReport:
		// Handle tags for prune report if needed, or leave empty

	case models.NotificationEventVulnerabilityFound:
		// No dedicated tag in AppriseSettings; notification is sent without a tag
	}

	payload := AppriseNotificationPayload{
		Title:  title,
		Body:   body,
		Type:   "info",
		Tag:    tags,
		Format: format,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification payload: %w", err)
	}

	slog.InfoContext(ctx, "Sending Apprise notification", "url", settings.APIURL, "title", title, "tags", tags, "type", string(notificationType))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req) //nolint:gosec // intentional request to user-configured Apprise API endpoint
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body for debugging
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Apprise API returned error", "status", resp.StatusCode, "response", bodyString, "url", settings.APIURL)
		return fmt.Errorf("apprise API returned status %d: %s", resp.StatusCode, bodyString)
	}

	slog.InfoContext(ctx, "Apprise notification sent successfully", "status", resp.StatusCode, "response", bodyString)

	return nil
}

func (s *AppriseService) SendImageUpdateNotification(ctx context.Context, imageRef string, updateInfo *imageupdate.Response) error {
	title := fmt.Sprintf("Container Image Update Available: %s", imageRef)
	body := fmt.Sprintf(
		"Image: %s\nUpdate Type: %s\nCurrent Digest: %s\nLatest Digest: %s",
		imageRef,
		updateInfo.UpdateType,
		updateInfo.CurrentDigest,
		updateInfo.LatestDigest,
	)
	return s.SendNotification(ctx, title, body, "text", models.NotificationEventImageUpdate)
}

func (s *AppriseService) SendContainerUpdateNotification(ctx context.Context, containerName, imageRef, oldDigest, newDigest string) error {
	title := fmt.Sprintf("Container Updated: %s", containerName)
	body := fmt.Sprintf(
		"Container: %s\nImage: %s\nPrevious Version: %s\nCurrent Version: %s\nStatus: Updated Successfully",
		containerName,
		imageRef,
		oldDigest,
		newDigest,
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
	var body strings.Builder
	body.WriteString("The following images have updates available:\n\n")

	for imageRef, update := range updatesWithChanges {
		fmt.Fprintf(&body, "• %s\n  Type: %s\n  Current: %s\n  Latest: %s\n\n",
			imageRef,
			update.UpdateType,
			update.CurrentDigest,
			update.LatestDigest,
		)
	}

	return s.SendNotification(ctx, title, body.String(), "text", models.NotificationEventImageUpdate)
}

func (s *AppriseService) TestNotification(ctx context.Context, testType string) error {
	switch testType {
	case "vulnerability-found":
		title := "Vulnerability Summary Notification"
		body := fmt.Sprintf(
			"Summary Date: %s\nCritical: 1\nHigh: 3\nMedium: 2\nLow: 1\nUnknown: 0\nFixable vulnerabilities: 7\nExamples: CVE-2025-1234, CVE-2025-5678, CVE-2026-0001",
			time.Now().UTC().Format("2006-01-02"),
		)
		return s.SendNotification(ctx, title, body, "text", models.NotificationEventVulnerabilityFound)
	case "prune-report":
		title := "System Prune Report"
		body := "Containers pruned: 2\nImages deleted: 1\nVolumes deleted: 1\nNetworks deleted: 1\nSpace reclaimed: 3.56 GB"
		return s.SendNotification(ctx, title, body, "text", models.NotificationEventPruneReport)
	case "image-update":
		testUpdate := &imageupdate.Response{
			HasUpdate:      true,
			UpdateType:     "digest",
			CurrentDigest:  "sha256:abc123def456789012345678901234567890",
			LatestDigest:   "sha256:xyz789ghi012345678901234567890123456",
			CheckTime:      time.Now(),
			ResponseTimeMs: 100,
		}
		return s.SendImageUpdateNotification(ctx, "nginx:latest", testUpdate)
	case "batch-image-update":
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
		return s.SendBatchImageUpdateNotification(ctx, testUpdates)
	case "simple", "":
		title := "Test Notification from Arcane"
		body := "If you're reading this, your Apprise integration is working correctly!"
		return s.SendNotification(ctx, title, body, "text", models.NotificationEventImageUpdate)
	default:
		return fmt.Errorf("unsupported apprise test type: %s", testType)
	}
}
