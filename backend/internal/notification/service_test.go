package notification

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

func setupNotificationTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&NotificationSettings{}, &settings.SettingVariable{}, &environment.Environment{}, &event.Event{}))

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

func setupNotificationTestService(t *testing.T) (*database.DB, *NotificationService) {
	t.Helper()

	db := setupNotificationTestDB(t)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, nil, nil)

	cfg := &config.Config{
		AppUrl: "http://localhost:3552",
	}

	return db, NewNotificationService(db, cfg, envSvc, event.NewEventService(db, cfg, nil), nil)
}

func newNotificationTestUpdateInfo() *imageupdate.Response {
	return &imageupdate.Response{
		HasUpdate:     true,
		UpdateType:    "digest",
		CurrentDigest: "sha256:current",
		LatestDigest:  "sha256:latest",
		CheckTime:     time.Date(2026, time.January, 9, 15, 4, 5, 0, time.UTC),
	}
}

func captureNotificationServiceLogs(t *testing.T) *bytes.Buffer {
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

func TestNotificationService_ResolveNotificationTarget_UsesEnvironmentRecordAndFallback(t *testing.T) {
	ctx := t.Context()
	db, svc := setupNotificationTestService(t)

	target, err := svc.ResolveNotificationTarget(ctx, "")
	require.NoError(t, err)
	require.Equal(t, "0", target.EnvironmentID)
	require.Equal(t, "Local Docker", target.EnvironmentName)

	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&environment.Environment{
		ID: "env-remote", CreatedAt: now, UpdatedAt: &now,
		Name:    "Remote Alpha",
		ApiUrl:  "http://remote.example",
		Enabled: true,
		Status:  string(environment.EnvironmentStatusOnline),
	}).Error)

	target, err = svc.ResolveNotificationTarget(ctx, "env-remote")
	require.NoError(t, err)
	require.Equal(t, "env-remote", target.EnvironmentID)
	require.Equal(t, "Remote Alpha", target.EnvironmentName)
}

func TestNotificationService_DispatchNotification_InvalidAccessTokenReturnsUnauthorizedSentinel(t *testing.T) {
	ctx := t.Context()
	_, svc := setupNotificationTestService(t)

	_, err := svc.DispatchNotification(ctx, "missing-token", notification.DispatchRequest{
		Kind: notification.DispatchKindImageUpdate,
		ImageUpdate: &notification.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfo(),
		},
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnauthorizedNotificationDispatch)
}

func TestNotificationService_DispatchNotification_UnsupportedKindReturnsSentinel(t *testing.T) {
	ctx := t.Context()
	db, svc := setupNotificationTestService(t)

	token := "remote-token"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&environment.Environment{
		ID: "env-remote", CreatedAt: now, UpdatedAt: &now,
		Name:        "Remote Edge",
		ApiUrl:      "http://remote.example",
		Enabled:     true,
		Status:      string(environment.EnvironmentStatusOnline),
		AccessToken: &token,
	}).Error)

	_, err := svc.DispatchNotification(ctx, token, notification.DispatchRequest{
		Kind: "bogus_kind",
	})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedDispatchKind)
	unsupportedErr := ErrUnsupportedDispatchKind
	require.ErrorIs(t, err, unsupportedErr)
	require.Contains(t, err.Error(), "bogus_kind")
}

func TestNotificationService_DispatchNotification_LogsManagerDispatchForAgent(t *testing.T) {
	ctx := t.Context()
	db, svc := setupNotificationTestService(t)
	logBuffer := captureNotificationServiceLogs(t)

	token := "remote-token"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&environment.Environment{
		ID: "env-remote", CreatedAt: now, UpdatedAt: &now,
		Name:        "Remote Edge",
		ApiUrl:      "http://remote.example",
		Enabled:     true,
		Status:      string(environment.EnvironmentStatusOnline),
		AccessToken: &token,
	}).Error)

	dispatchResponse, err := svc.DispatchNotification(ctx, token, notification.DispatchRequest{
		Kind: notification.DispatchKindImageUpdate,
		ImageUpdate: &notification.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfo(),
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Notification dispatched successfully", dispatchResponse.Message)
	require.Equal(t, 0, dispatchResponse.Delivered)
	logs := logBuffer.String()
	require.Contains(t, logs, "Manager dispatching notification on behalf of agent")
	require.Contains(t, logs, "environmentId=env-remote")
	require.Contains(t, logs, "environmentName=\"Remote Edge\"")
	require.Contains(t, logs, "kind=image_update")
}

func TestNotificationService_SendImageUpdateNotification_AgentModeDispatchesToManager(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, nil, nil)

	var calls atomic.Int32
	var dispatched notification.DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/notifications/dispatch", r.URL.Path) {
			return
		}
		if !assert.Equal(t, "agent-token", r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&dispatched)) {
			return
		}
		calls.Add(1)
		if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": notification.DispatchResponse{
				Message:   "Notification dispatched successfully",
				Delivered: 2,
			},
		})) {
			return
		}
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc, nil, nil)

	delivered, err := svc.SendImageUpdateNotification(ctx, "nginx:latest", newNotificationTestUpdateInfo(), notifications.NotificationEventImageUpdate)
	require.NoError(t, err)
	require.Equal(t, 2, delivered)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, notification.DispatchKindImageUpdate, dispatched.Kind)
	require.NotNil(t, dispatched.ImageUpdate)
	require.Equal(t, "nginx:latest", dispatched.ImageUpdate.ImageRef)
}

func TestNotificationService_SendBatchImageUpdateNotification_AgentModeUsesManagerDeliveredCount(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, nil, nil)

	var dispatched notification.DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodPost, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/notifications/dispatch", r.URL.Path) {
			return
		}
		if !assert.Equal(t, "agent-token", r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&dispatched)) {
			return
		}
		if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": notification.DispatchResponse{
				Message:   "Notification dispatched successfully",
				Delivered: 0,
			},
		})) {
			return
		}
	}))
	defer server.Close()

	svc := NewNotificationService(db, &config.Config{
		AppUrl:        "http://localhost:3552",
		AgentMode:     true,
		AgentToken:    "agent-token",
		ManagerApiUrl: server.URL,
	}, envSvc, nil, nil)

	delivered, err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{
		"nginx:latest": newNotificationTestUpdateInfo(),
	})
	require.NoError(t, err)
	require.Equal(t, 0, delivered)
	require.Equal(t, notification.DispatchKindBatchImageUpdate, dispatched.Kind)
	require.NotNil(t, dispatched.BatchImageUpdate)
	require.Contains(t, dispatched.BatchImageUpdate.Updates, "nginx:latest")
}

func TestNotificationService_SendImageUpdateNotification_AgentModeRequiresUpdateInfo(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, nil, nil)

	svc := NewNotificationService(db, &config.Config{
		AppUrl:    "http://localhost:3552",
		AgentMode: true,
	}, envSvc, nil, nil)

	_, err := svc.SendImageUpdateNotification(ctx, "nginx:latest", nil, notifications.NotificationEventImageUpdate)
	require.Error(t, err)
	require.Contains(t, err.Error(), "updateInfo is required")
}

func TestNotificationService_SendBatchImageUpdateNotification_AgentModeSkipsNoOpDispatch(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, nil, nil)

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
	}, envSvc, nil, nil)

	t.Run("empty updates", func(t *testing.T) {
		delivered, err := svc.SendBatchImageUpdateNotification(ctx, map[string]*imageupdate.Response{})
		require.NoError(t, err)
		require.Equal(t, 0, delivered)
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
		require.Equal(t, 0, delivered)
		require.EqualValues(t, 0, calls.Load())
	})
}

func TestBuildImageUpdateNotificationMessage_IncludesEnvironment(t *testing.T) {
	updateInfo := newNotificationTestUpdateInfo()

	message := notifications.BuildImageUpdateNotificationMessage(notifications.MessageFormatMarkdown, "Remote Alpha", "nginx:latest", updateInfo)
	require.Contains(t, message, "**Environment:** Remote Alpha")
	require.Equal(t, 1, strings.Count(message, "Environment"))

	plainMessage := notifications.BuildImageUpdateNotificationMessage(notifications.MessageFormatPlain, "Remote Alpha", "nginx:latest", updateInfo)
	require.Contains(t, plainMessage, "Environment: Remote Alpha")
}

func TestBuildContainerUpdateNotificationMessage_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildContainerUpdateNotificationMessage(notifications.MessageFormatMarkdown, "Local Lab", "nginx", "nginx:latest", "sha256:old", "sha256:new")

	require.Contains(t, message, "**Environment:** Local Lab")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestBuildBatchImageUpdateNotificationMessage_EnvironmentAppearsOnce(t *testing.T) {
	updates := map[string]*imageupdate.Response{
		"nginx:latest": newNotificationTestUpdateInfo(),
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

func TestBuildVulnerabilitySummaryNotificationMessage_IncludesEnvironment(t *testing.T) {
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

func TestBuildPruneReportNotificationMessage_IncludesEnvironment(t *testing.T) {
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

func TestBuildAutoHealNotificationMessage_IncludesEnvironment(t *testing.T) {
	message := notifications.BuildAutoHealNotificationMessage(notifications.MessageFormatMarkdown, "Cluster One", "nginx")

	require.Contains(t, message, "**Environment:** Cluster One")
	require.Equal(t, 1, strings.Count(message, "Environment"))
}

func TestNotificationCredential_KeepsPlaintextLegacyValues(t *testing.T) {
	setupNotificationTestDB(t)

	value := "discord-webhook-token/plaintext"

	require.NoError(t, notifications.DecryptStringCredential(&value))
	require.Equal(t, "discord-webhook-token/plaintext", value)
}

func TestNotificationCredential_DecryptsEncryptedValues(t *testing.T) {
	setupNotificationTestDB(t)

	encrypted, err := crypto.Encrypt("gotify-application-token")
	require.NoError(t, err)

	require.NoError(t, notifications.DecryptStringCredential(&encrypted))
	require.Equal(t, "gotify-application-token", encrypted)
}

func TestNotificationCredential_ReturnsErrorForCorruptedCiphertext(t *testing.T) {
	setupNotificationTestDB(t)

	encrypted, err := crypto.Encrypt("gotify-application-token")
	require.NoError(t, err)

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	ciphertext[len(ciphertext)-1] ^= 0xff
	require.Error(t, notifications.DecryptStringCredential(new(base64.StdEncoding.EncodeToString(ciphertext))))
}

func TestNotificationCredential_LeavesEmptyValuesEmpty(t *testing.T) {
	setupNotificationTestDB(t)

	value := ""

	require.NoError(t, notifications.DecryptStringCredential(&value))
	require.Empty(t, value)
}

func TestNotificationService_CreateOrUpdateSettingsEncryptsCredentialFields(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderDiscord, true, database.JSON{
		"webhookId": "123456789",
		"token":     "discord-secret-token",
		"username":  "Arcane",
	})
	require.NoError(t, err)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderDiscord).First(&stored).Error)
	require.Equal(t, "123456789", stored.Config["webhookId"])
	require.Equal(t, "Arcane", stored.Config["username"])
	require.NotEqual(t, "discord-secret-token", stored.Config["token"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "discord-secret-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsPreservesStoredCredentialWhenEmpty(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"token": "initial-gotify-token",
		"title": "Initial",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"token": "",
		"title": "Updated",
	})
	require.NoError(t, err)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderGotify).First(&stored).Error)
	require.Equal(t, "Updated", stored.Config["title"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "initial-gotify-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsRejectsTargetChangeWithStoredCredential(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	created, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"port":  443,
		"token": "initial-gotify-token",
	})
	require.NoError(t, err)
	originalToken := created.Config["token"]

	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "attacker.example",
		"port":  443,
		"token": "",
	})
	require.ErrorIs(t, err, common.ErrValidation)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderGotify).First(&stored).Error)
	require.Equal(t, "gotify.example", stored.Config["host"])
	require.Equal(t, originalToken, stored.Config["token"])

	updated, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"port":  8443,
		"token": "",
	})
	require.NoError(t, err)
	require.Equal(t, 8443, updated.Config["port"])
	require.Equal(t, originalToken, updated.Config["token"])
}

func TestNotificationService_CreateOrUpdateSettingsClearsEmailPasswordWhenAuthModeNone(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderEmail, true, database.JSON{
		"smtpHost":     "smtp.example",
		"smtpPassword": "stale-password",
		"authMode":     "auto",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderEmail, true, database.JSON{
		"smtpHost":     "smtp.example",
		"smtpPassword": "",
		"authMode":     string(notifications.EmailAuthModeNone),
	})
	require.NoError(t, err)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderEmail).First(&stored).Error)
	require.Empty(t, stored.Config["smtpPassword"])
}

func TestNotificationService_CreateOrUpdateSettingsPreservesCredentialAcrossDisable(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"token": "initial-gotify-token",
		"title": "Initial",
	})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, false, database.JSON{})
	require.NoError(t, err)

	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderGotify, true, database.JSON{
		"host":  "gotify.example",
		"token": "",
		"title": "Re-enabled",
	})
	require.NoError(t, err)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderGotify).First(&stored).Error)
	require.Equal(t, "Re-enabled", stored.Config["title"])

	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "initial-gotify-token", decrypted)
}

func TestNotificationService_CreateOrUpdateSettingsKeepsConfigWhenDisabled(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, nil, nil)

	_, err := svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderNtfy, true, database.JSON{
		"host":  "ntfy.example",
		"topic": "arcane",
		"events": map[string]any{
			"image_update": true,
			"prune_report": false,
		},
	})
	require.NoError(t, err)

	// Disabling submits the same config back; it must survive the toggle.
	_, err = svc.CreateOrUpdateSettings(ctx, notifications.NotificationProviderNtfy, false, database.JSON{
		"host":  "ntfy.example",
		"topic": "arcane",
		"events": map[string]any{
			"image_update": true,
			"prune_report": false,
		},
	})
	require.NoError(t, err)

	var stored NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", notifications.NotificationProviderNtfy).First(&stored).Error)
	require.False(t, stored.Enabled)
	require.Equal(t, "ntfy.example", stored.Config["host"])
	require.Equal(t, "arcane", stored.Config["topic"])
	events, ok := stored.Config["events"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, events["prune_report"])
}

func TestNotificationService_NotifyEnabledProviders_SkipsFiltersAndAggregates(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{}, nil, event.NewEventService(db, nil, nil), nil)

	var webhookCalls atomic.Int32
	var cancelOnWebhook atomic.Pointer[context.CancelFunc]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		if cancelWebhook := cancelOnWebhook.Load(); cancelWebhook != nil {
			(*cancelWebhook)()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rows := []NotificationSettings{
		{Provider: notifications.NotificationProviderDiscord, Enabled: false, Config: database.JSON{}},
		{Provider: notifications.NotificationProviderSlack, Enabled: true, Config: database.JSON{
			"events": map[string]any{string(notifications.NotificationEventPruneReport): false},
		}},
		{Provider: notifications.NotificationProviderGeneric, Enabled: true, Config: database.JSON{"webhookUrl": server.URL}},
		// An unconfigured Telegram provider fails before any network call.
		{Provider: notifications.NotificationProviderTelegram, Enabled: true, Config: database.JSON{}},
	}
	for i := range rows {
		require.NoError(t, db.WithContext(ctx).Create(&rows[i]).Error)
	}

	content := notifications.Content{Text: notifications.TextByFormat(func(notifications.MessageFormat) string { return "loop-test" })}
	target := NotificationTarget{EnvironmentID: "0", EnvironmentName: "Local Docker"}
	delivered, err := svc.notifyEnabledProviders(ctx, target, notifications.NotificationEventPruneReport, "loop-test", database.JSON{"eventType": "prune_report"}, content)

	// Disabled and event-disabled rows are never dispatched.
	require.EqualValues(t, 1, webhookCalls.Load())
	require.Equal(t, 1, delivered)
	require.Error(t, err)
	require.Contains(t, err.Error(), "notification errors: telegram: telegram bot token not configured")

	// Each dispatched attempt lands in the event log.
	var events []event.Event
	require.NoError(t, db.WithContext(ctx).Where("type = ?", event.EventTypeNotificationSend).Order("created_at").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, event.EventSeveritySuccess, events[0].Severity)
	require.Equal(t, "Notification sent via generic", events[0].Title)
	require.Equal(t, "loop-test", events[0].Description)
	require.Equal(t, event.EventSeverityError, events[1].Severity)
	require.Equal(t, "Notification failed via telegram", events[1].Title)
	require.Contains(t, events[1].Description, "telegram bot token not configured")

	// Completed attempts persist even when cancellation ends the caller's wait.
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	releaseLogging := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseLogging) })
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("block_notification_events", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			<-releaseLogging
		}
	}))
	canceledCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelOnWebhook.Store(&cancel)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = svc.notifyEnabledProviders(canceledCtx, target, notifications.NotificationEventPruneReport, "canceled-loop-test", nil, content)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("notification call waited for event persistence after cancellation")
	}
	releaseOnce.Do(func() { close(releaseLogging) })
	require.Eventually(t, func() bool {
		var count int64
		return db.WithContext(ctx).Model(&event.Event{}).Where("description LIKE ?", "canceled-loop-test%").Count(&count).Error == nil && count == 2
	}, time.Second, 10*time.Millisecond)
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
		_, ok := notificationTestEventTypes[tt]
		require.True(t, ok, "expected %q to be in notificationTestEventTypes", tt)
	}

	require.Len(t, notificationTestEventTypes, len(expected),
		"notificationTestEventTypes has unexpected entries")
}

func TestNotificationService_DispatchNotificationForEnvironment_ResolvesTunnelSessionEnvironment(t *testing.T) {
	ctx := t.Context()
	db, svc := setupNotificationTestService(t)

	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&environment.Environment{
		ID: "env-edge", CreatedAt: now, UpdatedAt: &now,
		Name:    "edge-env",
		ApiUrl:  "http://edge:3553",
		Enabled: true,
	}).Error)

	// No access token involved: the environment comes from the tunnel session (#3002).
	resp, err := svc.DispatchNotificationForEnvironment(ctx, "env-edge", notification.DispatchRequest{
		Kind: notification.DispatchKindImageUpdate,
		ImageUpdate: &notification.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfo(),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Notification dispatched successfully", resp.Message)

	_, err = svc.DispatchNotificationForEnvironment(ctx, "env-edge", notification.DispatchRequest{Kind: "bogus"})
	require.ErrorIs(t, err, ErrUnsupportedDispatchKind)
}

func TestNotificationService_AgentDispatchWithoutHTTPConfigFallsBackToTunnel(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	svc := NewNotificationService(db, &config.Config{AgentMode: true}, nil, event.NewEventService(db, &config.Config{AgentMode: true}, nil), nil)

	// No MANAGER_API_URL/AGENT_TOKEN and no active tunnel: the error must point
	// at both options instead of only the HTTP env vars (#3002).
	_, err := svc.dispatchNotificationToManager(ctx, notification.DispatchRequest{
		Kind: notification.DispatchKindImageUpdate,
		ImageUpdate: &notification.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfo(),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "connected edge tunnel")
}

func TestNotificationService_AgentDispatchHTTPFailureFallsBackToTunnel(t *testing.T) {
	ctx := t.Context()
	db := setupNotificationTestDB(t)
	cfg := &config.Config{AgentMode: true, ManagerApiUrl: "http://127.0.0.1:0", AgentToken: "token"}
	svc := NewNotificationService(db, cfg, nil, event.NewEventService(db, cfg, nil), nil)

	// HTTP dispatch fails and no tunnel is connected either: the fallback must
	// not mask the HTTP transport error.
	_, err := svc.dispatchNotificationToManager(ctx, notification.DispatchRequest{
		Kind: notification.DispatchKindImageUpdate,
		ImageUpdate: &notification.DispatchImageUpdate{
			ImageRef:   "nginx:latest",
			UpdateInfo: *newNotificationTestUpdateInfo(),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to dispatch notification to manager")
}
