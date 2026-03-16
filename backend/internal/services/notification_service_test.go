package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/types/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/notification"
	"github.com/getarcaneapp/arcane/types/system"
)

func setupNotificationTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.NotificationSettings{}, &models.SettingVariable{}, &models.NotificationLog{}, &models.Environment{}, &models.AppriseSettings{}))

	// Initialize crypto for tests (requires 32+ byte key)
	testCfg := &config.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: testCfg.EncryptionKey,
		Environment:   string(testCfg.Environment),
		AgentMode:     testCfg.AgentMode,
	})

	return &database.DB{DB: db}
}

func setupNotificationTestServiceInternal(t *testing.T) (*database.DB, *EnvironmentService, *NotificationService) {
	t.Helper()

	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil)

	cfg := &config.Config{
		AppUrl: "http://localhost:3552",
	}

	return db, envSvc, NewNotificationService(db, cfg, envSvc)
}

func newNotificationTestUpdateInfoInternal() *imageupdate.Response {
	return &imageupdate.Response{
		HasUpdate:     true,
		UpdateType:    "digest",
		CurrentDigest: "sha256:current",
		LatestDigest:  "sha256:latest",
		CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
	}
}

func captureNotificationServiceLogsInternal(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previousLogger := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	return &buf
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	// Create legacy Discord config with webhookUrl
	legacyConfig := map[string]any{
		"webhookUrl": "https://discord.com/api/webhooks/123456789/abcdef123456",
		"username":   "Arcane Bot",
		"avatarUrl":  "https://example.com/avatar.png",
		"events": map[string]bool{
			"image_update":     true,
			"container_update": false,
		},
	}

	configBytes, err := json.Marshal(legacyConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify migration results
	var migratedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&migratedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(migratedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	// Verify webhookId and token were extracted
	require.Equal(t, "123456789", discordConfig.WebhookID)
	require.NotEmpty(t, discordConfig.Token)

	// Verify token is encrypted and can be decrypted
	decryptedToken, err := crypto.Decrypt(discordConfig.Token)
	require.NoError(t, err)
	require.Equal(t, "abcdef123456", decryptedToken)

	// Verify other fields were preserved
	require.Equal(t, "Arcane Bot", discordConfig.Username)
	require.Equal(t, "https://example.com/avatar.png", discordConfig.AvatarURL)
	require.True(t, discordConfig.Events[models.NotificationEventImageUpdate])
	require.False(t, discordConfig.Events[models.NotificationEventContainerUpdate])
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_SkipsIfAlreadyMigrated(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	// Create already-migrated config with webhookId and token
	encryptedToken, err := crypto.Encrypt("already-migrated-token")
	require.NoError(t, err)

	migratedConfig := models.DiscordConfig{
		WebhookID: "999999999",
		Token:     encryptedToken,
		Username:  "Already Migrated",
	}

	configBytes, err := json.Marshal(migratedConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration - should skip
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify config was NOT changed
	var unchangedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(unchangedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	require.Equal(t, "999999999", discordConfig.WebhookID)
	require.Equal(t, encryptedToken, discordConfig.Token)
	require.Equal(t, "Already Migrated", discordConfig.Username)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_NoDiscordConfig(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	// No Discord config exists - migration should not error
	err := svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify no settings were created
	var count int64
	require.NoError(t, db.Model(&models.NotificationSettings{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_InvalidWebhookUrl(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	testCases := []struct {
		name       string
		webhookUrl string
	}{
		{
			name:       "missing webhooks path",
			webhookUrl: "https://discord.com/api/other/123456789/abcdef",
		},
		{
			name:       "incomplete webhook path",
			webhookUrl: "https://discord.com/api/webhooks/123456789",
		},
		{
			name:       "empty webhook url",
			webhookUrl: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up before each sub-test
			db.Exec("DELETE FROM notification_settings")

			legacyConfig := map[string]any{
				"webhookUrl": tc.webhookUrl,
			}

			configBytes, err := json.Marshal(legacyConfig)
			require.NoError(t, err)

			var configJSON models.JSON
			require.NoError(t, json.Unmarshal(configBytes, &configJSON))

			setting := models.NotificationSettings{
				Provider: models.NotificationProviderDiscord,
				Enabled:  true,
				Config:   configJSON,
			}
			require.NoError(t, db.Create(&setting).Error)

			// Migration should not error but should skip invalid URLs
			err = svc.MigrateDiscordWebhookUrlToFields(ctx)
			require.NoError(t, err)

			// Verify config was not changed
			var unchangedSetting models.NotificationSettings
			require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)

			var resultConfig map[string]any
			configBytes, err = json.Marshal(unchangedSetting.Config)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(configBytes, &resultConfig))

			// Should still have webhookUrl (not migrated)
			if tc.webhookUrl != "" {
				require.Equal(t, tc.webhookUrl, resultConfig["webhookUrl"])
			}
		})
	}
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	// Create Discord setting with empty config
	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  false,
		Config:   models.JSON{},
	}
	require.NoError(t, db.Create(&setting).Error)

	// Migration should not error
	err := svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify config remains empty
	var unchangedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&unchangedSetting).Error)
	require.Empty(t, unchangedSetting.Config)
}

func TestNotificationService_MigrateDiscordWebhookUrlToFields_PreservesAllFields(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	// Create legacy config with all optional fields
	legacyConfig := map[string]any{
		"webhookUrl": "https://discord.com/api/webhooks/111222333/token444555",
		"username":   "Custom Bot Name",
		"avatarUrl":  "https://cdn.example.com/bot-avatar.jpg",
		"events": map[string]bool{
			"image_update":     false,
			"container_update": true,
		},
	}

	configBytes, err := json.Marshal(legacyConfig)
	require.NoError(t, err)

	var configJSON models.JSON
	require.NoError(t, json.Unmarshal(configBytes, &configJSON))

	setting := models.NotificationSettings{
		Provider: models.NotificationProviderDiscord,
		Enabled:  true,
		Config:   configJSON,
	}
	require.NoError(t, db.Create(&setting).Error)

	// Run migration
	err = svc.MigrateDiscordWebhookUrlToFields(ctx)
	require.NoError(t, err)

	// Verify all fields were preserved
	var migratedSetting models.NotificationSettings
	require.NoError(t, db.Where("provider = ?", models.NotificationProviderDiscord).First(&migratedSetting).Error)

	var discordConfig models.DiscordConfig
	configBytes, err = json.Marshal(migratedSetting.Config)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &discordConfig))

	require.Equal(t, "111222333", discordConfig.WebhookID)
	require.NotEmpty(t, discordConfig.Token)

	decryptedToken, err := crypto.Decrypt(discordConfig.Token)
	require.NoError(t, err)
	require.Equal(t, "token444555", decryptedToken)

	require.Equal(t, "Custom Bot Name", discordConfig.Username)
	require.Equal(t, "https://cdn.example.com/bot-avatar.jpg", discordConfig.AvatarURL)
	require.False(t, discordConfig.Events[models.NotificationEventImageUpdate])
	require.True(t, discordConfig.Events[models.NotificationEventContainerUpdate])
}

func TestNotificationService_ResolveNotificationTargetInternal_UsesEnvironmentRecordAndFallback(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	target, err := svc.resolveNotificationTargetInternal(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "0", target.EnvironmentID)
	require.Equal(t, "Local Docker", target.EnvironmentName)

	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&models.Environment{
		BaseModel: models.BaseModel{ID: "env-remote", CreatedAt: now, UpdatedAt: &now},
		Name:      "Remote Alpha",
		ApiUrl:    "http://remote.example",
		Enabled:   true,
		Status:    string(models.EnvironmentStatusOnline),
	}).Error)

	target, err = svc.resolveNotificationTargetInternal(ctx, "env-remote")
	require.NoError(t, err)
	require.Equal(t, "env-remote", target.EnvironmentID)
	require.Equal(t, "Remote Alpha", target.EnvironmentName)
}

func TestNotificationService_ResolveNotificationTargetForAccessTokenInternal_UsesStoredEnvironmentName(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	token := "remote-token"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&models.Environment{
		BaseModel:   models.BaseModel{ID: "env-remote", CreatedAt: now, UpdatedAt: &now},
		Name:        "Remote Edge",
		ApiUrl:      "http://remote.example",
		Enabled:     true,
		Status:      string(models.EnvironmentStatusOnline),
		AccessToken: &token,
	}).Error)

	target, err := svc.resolveNotificationTargetForAccessTokenInternal(ctx, token)
	require.NoError(t, err)
	require.Equal(t, "env-remote", target.EnvironmentID)
	require.Equal(t, "Remote Edge", target.EnvironmentName)
}

func TestNotificationService_DispatchNotification_InvalidAccessTokenReturnsUnauthorizedSentinel(t *testing.T) {
	ctx := context.Background()
	_, _, svc := setupNotificationTestServiceInternal(t)

	err := svc.DispatchNotification(ctx, "missing-token", notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKindImageUpdate,
		ImageUpdate: &notificationdto.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfoInternal(),
		},
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnauthorizedNotificationDispatch)
}

func TestNotificationService_DispatchNotification_UnsupportedKindReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)

	token := "remote-token"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&models.Environment{
		BaseModel:   models.BaseModel{ID: "env-remote", CreatedAt: now, UpdatedAt: &now},
		Name:        "Remote Edge",
		ApiUrl:      "http://remote.example",
		Enabled:     true,
		Status:      string(models.EnvironmentStatusOnline),
		AccessToken: &token,
	}).Error)

	err := svc.DispatchNotification(ctx, token, notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKind("bogus_kind"),
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedDispatchKind)
	var unsupportedErr error = ErrUnsupportedDispatchKind
	require.True(t, errors.Is(err, unsupportedErr))
	require.Contains(t, err.Error(), "bogus_kind")
}

func TestNotificationService_DispatchNotification_LogsManagerDispatchForAgent(t *testing.T) {
	ctx := context.Background()
	db, _, svc := setupNotificationTestServiceInternal(t)
	logBuffer := captureNotificationServiceLogsInternal(t)

	token := "remote-token"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&models.Environment{
		BaseModel:   models.BaseModel{ID: "env-remote", CreatedAt: now, UpdatedAt: &now},
		Name:        "Remote Edge",
		ApiUrl:      "http://remote.example",
		Enabled:     true,
		Status:      string(models.EnvironmentStatusOnline),
		AccessToken: &token,
	}).Error)

	err := svc.DispatchNotification(ctx, token, notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKindImageUpdate,
		ImageUpdate: &notificationdto.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfoInternal(),
		},
	})

	require.NoError(t, err)
	logs := logBuffer.String()
	require.Contains(t, logs, "Manager dispatching notification on behalf of agent")
	require.Contains(t, logs, "environment_id=env-remote")
	require.Contains(t, logs, "environment_name=\"Remote Edge\"")
	require.Contains(t, logs, "kind=image_update")
}

func TestNotificationService_SendImageUpdateNotification_AgentModeDispatchesToManager(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil)

	var calls atomic.Int32
	var dispatched notificationdto.DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/notifications/dispatch", r.URL.Path)
		require.Equal(t, "agent-token", r.Header.Get("X-API-Key"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&dispatched))
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc)

	err := svc.SendImageUpdateNotification(ctx, "nginx:latest", newNotificationTestUpdateInfoInternal(), models.NotificationEventImageUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, notificationdto.DispatchKindImageUpdate, dispatched.Kind)
	require.NotNil(t, dispatched.ImageUpdate)
	require.Equal(t, "nginx:latest", dispatched.ImageUpdate.ImageRef)
}

func TestNotificationService_SendImageUpdateNotification_AgentModeRequiresUpdateInfo(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil)

	svc := NewNotificationService(db, &config.Config{
		AppUrl:    "http://localhost:3552",
		AgentMode: true,
	}, envSvc)

	err := svc.SendImageUpdateNotification(ctx, "nginx:latest", nil, models.NotificationEventImageUpdate)
	require.Error(t, err)
	require.Contains(t, err.Error(), "updateInfo is required")
}

func TestNotificationService_SendBatchImageUpdateNotification_AgentModeSkipsNoOpDispatchInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc)

	t.Run("empty updates", func(t *testing.T) {
		err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{})
		require.NoError(t, err)
		require.EqualValues(t, 0, calls.Load())
	})

	t.Run("no changed updates", func(t *testing.T) {
		err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{
			"nginx:latest": {
				HasUpdate:     false,
				CurrentDigest: "sha256:current",
				LatestDigest:  "sha256:latest",
			},
			"redis:latest": nil,
		})
		require.NoError(t, err)
		require.EqualValues(t, 0, calls.Load())
	})
}

func TestNotificationService_RenderEmailTemplate_IncludesEnvironment(t *testing.T) {
	_, _, svc := setupNotificationTestServiceInternal(t)

	htmlBody, textBody, err := svc.renderEmailTemplate("Homelab Prod", "nginx:latest", newNotificationTestUpdateInfoInternal())
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Homelab Prod")
	require.Contains(t, textBody, "Homelab Prod")

	subject := notifications.BuildEmailSubject("Homelab Prod", "Container Update Available: nginx:latest")
	require.Equal(t, "[Homelab Prod] Container Update Available: nginx:latest", subject)
}

func TestNotificationService_RenderContainerUpdateEmailTemplate_IncludesEnvironment(t *testing.T) {
	_, _, svc := setupNotificationTestServiceInternal(t)

	htmlBody, textBody, err := svc.renderContainerUpdateEmailTemplate("Lab Remote", "nginx", "nginx:latest", "sha256:old", "sha256:new")
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Lab Remote")
	require.Contains(t, textBody, "Lab Remote")

	subject := notifications.BuildEmailSubject("Lab Remote", "Container Updated: nginx")
	require.Equal(t, "[Lab Remote] Container Updated: nginx", subject)
}

func TestNotificationService_RenderBatchEmailTemplate_IncludesEnvironment(t *testing.T) {
	_, _, svc := setupNotificationTestServiceInternal(t)

	updates := map[string]*imageupdate.Response{
		"nginx:latest": newNotificationTestUpdateInfoInternal(),
		"redis:latest": {
			HasUpdate:     true,
			UpdateType:    "minor",
			CurrentDigest: "sha256:redis-current",
			LatestDigest:  "sha256:redis-latest",
			CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
		},
	}

	htmlBody, textBody, err := svc.renderBatchEmailTemplate("Edge Cluster A", updates)
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Edge Cluster A")
	require.Contains(t, textBody, "Edge Cluster A")

	subject := notifications.BuildEmailSubject("Edge Cluster A", "2 Container Image Updates Available")
	require.Equal(t, "[Edge Cluster A] 2 Container Image Updates Available", subject)
}

func TestNotificationService_RenderVulnerabilitySummaryEmailTemplate_IncludesEnvironment(t *testing.T) {
	_, _, svc := setupNotificationTestServiceInternal(t)

	htmlBody, textBody, err := svc.renderVulnerabilitySummaryEmailTemplate("Remote Alpha", VulnerabilityNotificationPayload{
		CVEID:        "Daily Summary - 2026-01-09",
		ImageName:    "5 image(s) scanned, 2 with fixable vulnerabilities",
		FixedVersion: "7 fixable vulnerability record(s)",
		Severity:     "Critical:1 High:3 Medium:2 Low:1 Unknown:0",
		PkgName:      "CVE-2025-1234",
	})
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Remote Alpha")
	require.Contains(t, textBody, "Remote Alpha")
}

func TestNotificationService_RenderPruneReportEmailTemplate_IncludesEnvironment(t *testing.T) {
	_, _, svc := setupNotificationTestServiceInternal(t)

	htmlBody, textBody, err := svc.renderPruneReportEmailTemplate("Cluster West", &system.PruneAllResult{
		SpaceReclaimed:           3825205248,
		ContainerSpaceReclaimed:  503316480,
		ImageSpaceReclaimed:      2449473536,
		VolumeSpaceReclaimed:     641728512,
		BuildCacheSpaceReclaimed: 230162432,
	})
	require.NoError(t, err)
	require.Contains(t, htmlBody, "Cluster West")
	require.Contains(t, textBody, "Cluster West")
}

func TestBuildImageUpdateNotificationMessageInternal_IncludesEnvironment(t *testing.T) {
	updateInfo := newNotificationTestUpdateInfoInternal()

	message := notifications.BuildImageUpdateNotificationMessage(notifications.MessageFormatMarkdown, "Remote Alpha", "nginx:latest", updateInfo)
	require.Contains(t, message, "**Environment:** Remote Alpha")
	require.Equal(t, 1, strings.Count(message, "Environment"))

	plainMessage := notifications.BuildImageUpdateNotificationMessage(notifications.MessageFormatPlain, "Remote Alpha", "nginx:latest", updateInfo)
	require.Contains(t, plainMessage, "Environment: Remote Alpha")
}

func TestBuildContainerUpdateNotificationMessageInternal_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildContainerUpdateNotificationMessage(notifications.MessageFormatMarkdown, "Local Lab", "nginx", "nginx:latest", "sha256:old", "sha256:new")

	require.Contains(t, message, "**Environment:** Local Lab")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestBuildBatchImageUpdateNotificationMessageInternal_EnvironmentAppearsOnce(t *testing.T) {
	updates := map[string]*imageupdate.Response{
		"nginx:latest": newNotificationTestUpdateInfoInternal(),
		"redis:latest": {
			HasUpdate:     true,
			UpdateType:    "minor",
			CurrentDigest: "sha256:redis-current",
			LatestDigest:  "sha256:redis-latest",
			CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
		},
	}

	message := notifications.BuildBatchImageUpdateNotificationMessage(notifications.MessageFormatMarkdown, "Cluster One", updates)
	require.Contains(t, message, "**Environment:** Cluster One")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestBuildVulnerabilitySummaryNotificationMessageInternal_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildVulnerabilitySummaryNotificationMessage(
		notifications.MessageFormatMarkdown,
		"Remote Alpha",
		"Daily Summary - 2026-01-09",
		"5 image(s) scanned",
		"7 fixable vulnerability record(s)",
		"Critical:1 High:3",
		"CVE-2025-1234",
	)

	require.Contains(t, message, "**Environment:** Remote Alpha")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestBuildPruneReportNotificationMessageInternal_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildPruneReportNotificationMessage(notifications.MessageFormatMarkdown, "Cluster One", &system.PruneAllResult{
		SpaceReclaimed:           3825205248,
		ContainerSpaceReclaimed:  503316480,
		ImageSpaceReclaimed:      2449473536,
		VolumeSpaceReclaimed:     641728512,
		BuildCacheSpaceReclaimed: 230162432,
	})

	require.Contains(t, message, "**Environment:** Cluster One")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestBuildAutoHealNotificationMessageInternal_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatMarkdown, "Cluster One", "nginx")

	require.Contains(t, message, "**Environment:** Cluster One")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestSupportedNotificationTestTypes_IncludesAutoHeal(t *testing.T) {
	expected := []string{
		notificationTestTypeSimple,
		notificationTestTypeImageUpdate,
		notificationTestTypeBatchImageUpdate,
		notificationTestTypeVulnerability,
		notificationTestTypePruneReport,
		notificationTestTypeAutoHeal,
	}

	for _, tt := range expected {
		_, ok := supportedNotificationTestTypes[tt]
		require.True(t, ok, "expected %q to be in supportedNotificationTestTypes", tt)
	}

	require.Equal(t, len(expected), len(supportedNotificationTestTypes),
		"supportedNotificationTestTypes has unexpected entries")
}
