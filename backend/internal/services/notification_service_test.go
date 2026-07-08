package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/crypto"
)

func setupNotificationTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.NotificationSettings{}, &models.SettingVariable{}, &models.Environment{}, &models.Event{}))

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
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)

	cfg := &config.Config{
		AppUrl: "http://localhost:3552",
	}

	return db, envSvc, NewNotificationService(db, cfg, envSvc, NewEventService(db, cfg, nil))
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

	_, err := svc.DispatchNotification(ctx, "missing-token", notificationdto.DispatchRequest{
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

	_, err := svc.DispatchNotification(ctx, token, notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKind("bogus_kind"),
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedDispatchKind)
	var unsupportedErr = ErrUnsupportedDispatchKind
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

	dispatchResponse, err := svc.DispatchNotification(ctx, token, notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKindImageUpdate,
		ImageUpdate: &notificationdto.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfoInternal(),
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Notification dispatched successfully", dispatchResponse.Message)
	require.Equal(t, 0, dispatchResponse.Delivered)
	logs := logBuffer.String()
	require.Contains(t, logs, "Manager dispatching notification on behalf of agent")
	require.Contains(t, logs, "environment_id=env-remote")
	require.Contains(t, logs, "environment_name=\"Remote Edge\"")
	require.Contains(t, logs, "kind=image_update")
}

func TestNotificationService_SendImageUpdateNotification_AgentModeDispatchesToManager(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)

	var calls atomic.Int32
	var dispatched notificationdto.DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/notifications/dispatch", r.URL.Path)
		require.Equal(t, "agent-token", r.Header.Get("X-API-Key"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&dispatched))
		calls.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": notificationdto.DispatchResponse{
				Message:   "Notification dispatched successfully",
				Delivered: 2,
			},
		}))
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc, nil)

	delivered, err := svc.SendImageUpdateNotification(ctx, "nginx:latest", newNotificationTestUpdateInfoInternal(), models.NotificationEventImageUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 2, delivered)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, notificationdto.DispatchKindImageUpdate, dispatched.Kind)
	require.NotNil(t, dispatched.ImageUpdate)
	require.Equal(t, "nginx:latest", dispatched.ImageUpdate.ImageRef)
}

func TestNotificationService_SendBatchImageUpdateNotification_AgentModeUsesManagerDeliveredCountInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)

	var dispatched notificationdto.DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/notifications/dispatch", r.URL.Path)
		require.Equal(t, "agent-token", r.Header.Get("X-API-Key"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&dispatched))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": notificationdto.DispatchResponse{
				Message:   "Notification dispatched successfully",
				Delivered: 0,
			},
		}))
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc, nil)

	delivered, err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{
		"nginx:latest": newNotificationTestUpdateInfoInternal(),
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, delivered)
	require.Equal(t, notificationdto.DispatchKindBatchImageUpdate, dispatched.Kind)
	require.NotNil(t, dispatched.BatchImageUpdate)
	require.Contains(t, dispatched.BatchImageUpdate.Updates, "nginx:latest")
}

func TestNotificationService_SendImageUpdateNotification_AgentModeRequiresUpdateInfo(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)

	svc := NewNotificationService(db, &config.Config{
		AppUrl:    "http://localhost:3552",
		AgentMode: true,
	}, envSvc, nil)

	_, err := svc.SendImageUpdateNotification(ctx, "nginx:latest", nil, models.NotificationEventImageUpdate)
	require.Error(t, err)
	require.Contains(t, err.Error(), "updateInfo is required")
}

func TestNotificationService_SendBatchImageUpdateNotification_AgentModeSkipsNoOpDispatchInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	envSvc := NewEnvironmentService(db, nil, nil, nil, nil, nil)

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
	}, envSvc, nil)

	t.Run("empty updates", func(t *testing.T) {
		delivered, err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{})
		require.NoError(t, err)
		require.EqualValues(t, 0, delivered)
		require.EqualValues(t, 0, calls.Load())
	})

	t.Run("no changed updates", func(t *testing.T) {
		delivered, err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{
			"nginx:latest": {
				HasUpdate:     false,
				CurrentDigest: "sha256:current",
				LatestDigest:  "sha256:latest",
			},
			"redis:latest": nil,
		})
		require.NoError(t, err)
		require.EqualValues(t, 0, delivered)
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

func TestNotificationCredentialInternal_KeepsPlaintextLegacyValues(t *testing.T) {
	setupNotificationTestDB(t)

	value := "discord-webhook-token/plaintext"

	require.NoError(t, notifications.DecryptStringCredential(&value))
	require.Equal(t, "discord-webhook-token/plaintext", value)
}

func TestNotificationCredentialInternal_DecryptsEncryptedValues(t *testing.T) {
	setupNotificationTestDB(t)

	encrypted, err := crypto.Encrypt("gotify-application-token")
	require.NoError(t, err)

	require.NoError(t, notifications.DecryptStringCredential(&encrypted))
	require.Equal(t, "gotify-application-token", encrypted)
}

func TestNotificationCredentialInternal_ReturnsErrorForCorruptedCiphertext(t *testing.T) {
	setupNotificationTestDB(t)

	encrypted, err := crypto.Encrypt("gotify-application-token")
	require.NoError(t, err)

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	ciphertext[len(ciphertext)-1] ^= 0xff
	require.Error(t, notifications.DecryptStringCredential(new(base64.StdEncoding.EncodeToString(ciphertext))))
}

func TestNotificationCredentialInternal_LeavesEmptyValuesEmpty(t *testing.T) {
	setupNotificationTestDB(t)

	value := ""

	require.NoError(t, notifications.DecryptStringCredential(&value))
	require.Empty(t, value)
}

func TestNotificationService_CreateOrUpdateSettingsEncryptsCredentialFieldsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderDiscord, true, models.JSON{
		"webhookId": "123456789",
		"token":     "discord-secret-token",
		"username":  "Arcane",
	})
	require.NoError(t, err)

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderDiscord).First(&stored).Error)
	require.Equal(t, "123456789", stored.Config["webhookId"])
	require.Equal(t, "Arcane", stored.Config["username"])
	require.NotEqual(t, "discord-secret-token", stored.Config["token"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "discord-secret-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsPreservesStoredCredentialWhenEmptyInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderGotify, true, models.JSON{
		"host":  "gotify.example",
		"token": "initial-gotify-token",
		"title": "Initial",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, models.NotificationProviderGotify, true, models.JSON{
		"host":  "gotify.example",
		"token": "",
		"title": "Updated",
	})
	require.NoError(t, err)

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderGotify).First(&stored).Error)
	require.Equal(t, "Updated", stored.Config["title"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "initial-gotify-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsClearsEmailPasswordWhenAuthModeNoneInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderEmail, true, models.JSON{
		"smtpHost":     "smtp.example",
		"smtpPassword": "stale-password",
		"authMode":     "auto",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, models.NotificationProviderEmail, true, models.JSON{
		"smtpHost":     "smtp.example",
		"smtpPassword": "",
		"authMode":     string(models.EmailAuthModeNone),
	})
	require.NoError(t, err)

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderEmail).First(&stored).Error)
	require.Empty(t, stored.Config["smtpPassword"])
}

func TestNotificationService_CreateOrUpdateSettingsPreservesCredentialAcrossDisableInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderGotify, true, models.JSON{
		"host":  "gotify.example",
		"token": "initial-gotify-token",
		"title": "Initial",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, models.NotificationProviderGotify, false, models.JSON{})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, models.NotificationProviderGotify, true, models.JSON{
		"host":  "gotify.example",
		"token": "",
		"title": "Re-enabled",
	})
	require.NoError(t, err)

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderGotify).First(&stored).Error)
	require.Equal(t, "Re-enabled", stored.Config["title"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "initial-gotify-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsKeepsConfigWhenDisabledInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderNtfy, true, models.JSON{
		"host":  "ntfy.example",
		"topic": "arcane",
		"events": map[string]any{
			"image_update": true,
			"prune_report": false,
		},
	})
	require.NoError(t, err)

	// Disabling submits the same config back; it must survive the toggle.
	_, err = svc.CreateOrUpdateSettings(ctx, models.NotificationProviderNtfy, false, models.JSON{
		"host":  "ntfy.example",
		"topic": "arcane",
		"events": map[string]any{
			"image_update": true,
			"prune_report": false,
		},
	})
	require.NoError(t, err)

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderNtfy).First(&stored).Error)
	require.False(t, stored.Enabled)
	require.Equal(t, "ntfy.example", stored.Config["host"])
	require.Equal(t, "arcane", stored.Config["topic"])
	events, ok := stored.Config["events"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, events["prune_report"])
}

func TestNotificationService_NotifyEnabledProvidersInternal_SkipsFiltersAndAggregatesInternal(t *testing.T) {
	ctx := context.Background()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, NewEventService(db, nil, nil))

	rows := []models.NotificationSettings{
		{Provider: models.NotificationProviderDiscord, Enabled: false, Config: models.JSON{}},
		{Provider: models.NotificationProviderSlack, Enabled: true, Config: models.JSON{
			"events": map[string]any{string(models.NotificationEventPruneReport): false},
		}},
		{Provider: models.NotificationProviderGotify, Enabled: true, Config: models.JSON{}},
		{Provider: models.NotificationProviderNtfy, Enabled: true, Config: models.JSON{}},
	}
	for i := range rows {
		require.NoError(t, db.WithContext(ctx).Create(&rows[i]).Error)
	}

	var dispatched []models.NotificationProvider
	target := NotificationTarget{EnvironmentID: "0", EnvironmentName: "Local Docker"}
	delivered, err := svc.notifyEnabledProvidersInternal(ctx, target, models.NotificationEventPruneReport, "loop-test", models.JSON{"eventType": "prune_report"},
		func(_ context.Context, provider models.NotificationProvider, _ models.JSON) (bool, error) {
			dispatched = append(dispatched, provider)
			if provider == models.NotificationProviderNtfy {
				return true, errors.New("boom")
			}
			return true, nil
		})

	// Disabled and event-disabled rows are never dispatched.
	require.Equal(t, []models.NotificationProvider{models.NotificationProviderGotify, models.NotificationProviderNtfy}, dispatched)
	require.Equal(t, 1, delivered)
	require.Error(t, err)
	require.Contains(t, err.Error(), "notification errors: ntfy: boom")

	// Each dispatched attempt lands in the event log.
	var events []models.Event
	require.NoError(t, db.WithContext(ctx).Where("type = ?", models.EventTypeNotificationSend).Order("created_at").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, models.EventSeveritySuccess, events[0].Severity)
	require.Equal(t, "Notification sent via gotify", events[0].Title)
	require.Equal(t, "loop-test", events[0].Description)
	require.Equal(t, models.EventSeverityError, events[1].Severity)
	require.Equal(t, "Notification failed via ntfy", events[1].Title)
	require.Contains(t, events[1].Description, "boom")
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
