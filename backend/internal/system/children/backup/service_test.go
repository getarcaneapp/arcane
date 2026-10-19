package backup

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow/flowtest"
)

func TestHistoryDestinationDecoration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&s3.S3Destination{}, &settings.SettingVariable{}))
	destination := s3.S3Destination{Name: "Offsite", Bucket: "backups"}
	require.NoError(t, db.Create(&destination).Error)
	store := &database.DB{DB: db}
	settingsService, err := settings.NewSettingsService(t.Context(), store, &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	service := NewService(Dependencies{
		S3Destinations: s3.NewS3DestinationService(store, func(context.Context, string) error { return nil }),
		Settings:       settingsService,
		Store: Store{
			ListHistory: func(context.Context, pagination.QueryParams) ([]backup.HistoryEntry, pagination.Response, error) {
				return []backup.HistoryEntry{
					{S3DestinationID: destination.ID},
					{S3DestinationID: "missing"},
				}, pagination.Response{}, nil
			},
		},
	})
	history, _, err := service.ListBackupHistory(t.Context(), pagination.QueryParams{})
	require.NoError(t, err)
	require.Equal(t, "Offsite", history[0].S3DestinationName)
	require.Empty(t, history[1].S3DestinationName)
	require.NoError(t, db.Migrator().DropTable(&s3.S3Destination{}))
	history, _, err = service.ListBackupHistory(t.Context(), pagination.QueryParams{})
	require.NoError(t, err)
	require.Empty(t, history[0].S3DestinationName)
}

func TestBackupWorkflowDefinitions(t *testing.T) {
	harness := flowtest.New(t, nil)
	require.NoError(t, NewService(Dependencies{}).RegisterWorkflows(harness.Engine))
	harness.Start(t)
	flowtest.AssertDefinitions(t, harness)
}

func TestRecoveryEnvironmentIncludesRuntimeSecrets(t *testing.T) {
	cfg := &config.Config{
		JWTSecret:         "jwt-secret",
		EncryptionKey:     "encryption-secret",
		AdminStaticAPIKey: "admin-key",
		OidcClientSecret:  "oidc-secret",
		FilePerm:          0o640,
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(t.Context(), &database.DB{DB: db}, &config.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	environment := NewService(Dependencies{
		Config:   cfg,
		Settings: settingsService,
	}).recoveryEnvironment(t.Context())
	require.Equal(t, "jwt-secret", environment["JWT_SECRET"])
	require.Equal(t, "encryption-secret", environment["ENCRYPTION_KEY"])
	require.Equal(t, "admin-key", environment["ADMIN_STATIC_API_KEY"])
	require.Equal(t, "oidc-secret", environment["OIDC_CLIENT_SECRET"])
	require.Equal(t, "0640", environment["FILE_PERM"])
}
