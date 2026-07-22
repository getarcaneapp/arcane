package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	notificationdto "github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/labstack/echo/v5"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func setupNotificationHandlerTestService(t *testing.T) (*database.DB, *services.NotificationService) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.NotificationSettings{}, &models.SettingVariable{}, &models.Environment{}))

	databaseDB := &database.DB{DB: db}
	envSvc := services.NewEnvironmentService(databaseDB, nil, nil, nil, nil, nil)

	return databaseDB, services.NewNotificationService(databaseDB, &config.Config{}, envSvc, nil)
}

func TestIsSupportedNotificationTestType(t *testing.T) {
	expected := []string{
		"simple",
		"image-update",
		"batch-image-update",
		"vulnerability-found",
		"prune-report",
		"auto-heal",
	}

	for _, tt := range expected {
		assert.True(t, isSupportedNotificationTestType(tt), "expected %q to be supported", tt)
	}

	assert.False(t, isSupportedNotificationTestType("bogus"))
	assert.False(t, isSupportedNotificationTestType(""))
}

func TestNormalizeNotificationTestType(t *testing.T) {
	assert.Equal(t, "simple", normalizeNotificationTestType(""))
	assert.Equal(t, "simple", normalizeNotificationTestType("  "))
	assert.Equal(t, "auto-heal", normalizeNotificationTestType("auto-heal"))
	assert.Equal(t, "auto-heal", normalizeNotificationTestType("  auto-heal  "))
}

func TestNotificationHandlerGetAllNotificationSettingsRedactsCredentialsInternal(t *testing.T) {
	ctx := context.Background()
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})

	db, svc := setupNotificationHandlerTestService(t)
	h := &NotificationHandler{
		notificationService: svc,
		config:              &config.Config{},
	}

	_, err := svc.CreateOrUpdateSettings(ctx, models.NotificationProviderDiscord, true, models.JSON{
		"webhookId": "123456789",
		"token":     "discord-secret-token",
		"username":  "Arcane",
	})
	require.NoError(t, err)

	output, err := h.GetAllNotificationSettings(ctx, &GetAllNotificationSettingsInput{EnvironmentID: "0"})
	require.NoError(t, err)
	require.Len(t, output.Body, 1)
	require.Equal(t, "123456789", output.Body[0].Config["webhookId"])
	require.Equal(t, "Arcane", output.Body[0].Config["username"])
	require.Empty(t, output.Body[0].Config["token"])

	var stored models.NotificationSettings
	require.NoError(t, db.WithContext(ctx).Where("provider = ?", models.NotificationProviderDiscord).First(&stored).Error)
	require.NotEqual(t, "discord-secret-token", stored.Config["token"])
	decrypted, err := crypto.Decrypt(stored.Config["token"].(string))
	require.NoError(t, err)
	require.Equal(t, "discord-secret-token", decrypted)
}

func TestDispatchNotification_RejectsAgentModeInternal(t *testing.T) {
	h := &NotificationHandler{config: &config.Config{AgentMode: true}}

	resp, err := h.DispatchNotification(context.Background(), &DispatchNotificationInput{})
	require.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifications are managed on the Arcane manager")
}

func TestDispatchNotification_ReturnsBadRequestForUnsupportedDispatchKind(t *testing.T) {
	ctx := context.Background()
	db, svc := setupNotificationHandlerTestService(t)
	h := &NotificationHandler{
		notificationService: svc,
		config:              &config.Config{},
	}

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

	resp, err := h.DispatchNotification(ctx, &DispatchNotificationInput{
		APIKey: token,
		Body: notificationdto.DispatchRequest{
			Kind: notificationdto.DispatchKind("bogus_kind"),
		},
	})
	require.Nil(t, resp)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), "unsupported dispatch kind")
}

func TestDispatchNotificationRoute_AuthenticatesEnvironmentAccessToken(t *testing.T) {
	ctx := context.Background()
	db, svc := setupNotificationHandlerTestService(t)
	cfg := &config.Config{}
	envSvc := services.NewEnvironmentService(db, nil, nil, nil, nil, nil)

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

	router := echo.New()
	humaConfig := huma.DefaultConfig("test", "1.0.0")
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"BearerAuth": {Type: "http", Scheme: "bearer"},
		"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
	}
	humaConfig.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		name := huma.DefaultSchemaNamer(t, hint)
		if t.PkgPath() == "" {
			return name
		}
		return strings.NewReplacer("/", "_", ".", "_").Replace(t.PkgPath()) + "_" + name
	})
	api := humaecho.NewWithGroup(router, router.Group("/api"), humaConfig)
	api.UseMiddleware(humamw.NewAuthBridge(api, &services.AuthService{}, nil, nil, envSvc, cfg))
	RegisterNotifications(api, svc, cfg)

	payload, err := json.Marshal(notificationdto.DispatchRequest{
		Kind: notificationdto.DispatchKindImageUpdate,
		ImageUpdate: &notificationdto.DispatchImageUpdate{
			ImageRef: "nginx:latest",
			UpdateInfo: imageupdate.Response{
				HasUpdate:     true,
				UpdateType:    "digest",
				CurrentDigest: "sha256:current",
				LatestDigest:  "sha256:latest",
			},
		},
	})
	require.NoError(t, err)

	t.Run("accepts stored environment token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/notifications/dispatch", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", token)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var response base.ApiResponse[notificationdto.DispatchResponse]
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
		require.True(t, response.Success)
		require.Equal(t, "Notification dispatched successfully", response.Data.Message)
		require.Zero(t, response.Data.Delivered)
	})

	for _, testCase := range []struct {
		name  string
		token string
	}{
		{name: "rejects missing token"},
		{name: "rejects unknown token", token: "unknown-token"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/notifications/dispatch", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			if testCase.token != "" {
				req.Header.Set("X-API-Key", testCase.token)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}
