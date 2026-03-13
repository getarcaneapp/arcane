package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	notificationdto "github.com/getarcaneapp/arcane/types/notification"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupNotificationHandlerTestService(t *testing.T) (*database.DB, *services.NotificationService) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.NotificationSettings{}, &models.SettingVariable{}, &models.NotificationLog{}, &models.Environment{}, &models.AppriseSettings{}))

	databaseDB := &database.DB{DB: db}
	envSvc := services.NewEnvironmentService(databaseDB, nil, nil, nil, nil)

	return databaseDB, services.NewNotificationService(databaseDB, &config.Config{}, envSvc)
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
