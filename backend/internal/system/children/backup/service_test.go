package backup

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	backupengine "github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow/flowtest"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis/testing"
)

func TestHistoryDestinationDecoration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&s3.S3Destination{}, &settings.SettingVariable{}))
	destination := s3.S3Destination{Name: "Offsite", Bucket: "backups"}
	require.NoError(t, db.Create(&destination).Error)
	store := &database.DB{DB: db}
	settingsService, err := settings.NewSettingsService(t.Context(), store, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	service := NewService(Dependencies{
		S3Destinations: s3.NewS3DestinationService(store, func(context.Context, string) error { return nil }),
		Settings:       settingsService,
		Store: Store{
			ListHistory: func(context.Context, pagination.QueryParams) ([]types.SystemVolumeBackupHistoryEntry, pagination.Response, error) {
				return []types.SystemVolumeBackupHistoryEntry{
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
		EncryptionKey:     "encryption-secret",
		AdminStaticAPIKey: "admin-key",
		FilePerm:          0o640,
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(t.Context(), &database.DB{DB: db}, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	environment := NewService(Dependencies{
		Config:   cfg,
		Settings: settingsService,
	}).recoveryEnvironment(t.Context())
	require.Equal(t, "encryption-secret", environment["ARCANE_ENCRYPTION_KEY"])
	require.Equal(t, "admin-key", environment["ARCANE_ADMIN_STATIC_API_KEY"])
	require.Equal(t, "0640", environment["ARCANE_FILE_PERM"])
}

func TestRecoveryEnvironmentIncludesEnvSourcedSettings(t *testing.T) {
	settingsEnv := settings.Env{
		"oidcClientSecret":  "oidc-secret",
		"dockerHost":        "tcp://docker:2375",
		"projectsDirectory": "/srv/projects",
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	settingsService, err := settings.NewSettingsService(t.Context(), &database.DB{DB: db}, &config.Config{}, settingsEnv)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	environment := NewService(Dependencies{
		Config:      &config.Config{},
		Settings:    settingsService,
		SettingsEnv: settingsEnv,
	}).recoveryEnvironment(t.Context())
	require.Equal(t, "oidc-secret", environment["ARCANE_OIDC_CLIENT_SECRET"])
	require.Equal(t, "tcp://docker:2375", environment["DOCKER_HOST"])
	require.Equal(t, "/srv/projects", environment["ARCANE_PROJECTS_DIRECTORY"])
	require.NotContains(t, environment, "PROJECTS_DIRECTORY")
}

func TestSetRecoveryKeyKeepsCurrentKeyWhenRekeyFails(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&backupengine.SystemBackupRecoveryConfig{}, &s3.S3Destination{}))
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "system-backup-rekey-test-key-32bytes", Environment: "test"})
	db := &database.DB{DB: gormDB}
	recoveryKeys := backupengine.NewRecoveryKeyStore(db)
	oldKey := "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	require.NoError(t, recoveryKeys.Set(t.Context(), oldKey))
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	engine := backupengine.NewEngine(gate, nil)
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	dockerClient, err := client.New(client.WithHost("tcp://127.0.0.1:1"), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	dockerService := (&docker.DockerClientService{}).WithClient(dockerClient)
	service := NewService(Dependencies{
		Store: Store{
			CountRuns:                 func(context.Context) (int64, error) { return 1, nil },
			CountVolumeRepositoryRuns: func(context.Context) (int64, error) { return 0, nil },
			CountRunningVolumeBackups: func(context.Context) (int64, error) { return 0, nil },
		},
		DB:             db,
		Docker:         dockerService,
		Volumes:        volume.NewVolumeService(db, dockerService, nil, nil, nil, nil, nil, nil, nil, nil),
		Engine:         engine,
		S3Destinations: s3.NewS3DestinationService(db, func(context.Context, string) error { return nil }),
		RecoveryKeys:   recoveryKeys,
	})

	_, err = service.SetRecoveryKey(t.Context(), "ASDFGH-QWERTY-765432-LKJHGF-RQPONM-XWVUTS-5432ZY-NBVCXZ")
	require.Error(t, err)
	storedKey, err := recoveryKeys.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, oldKey, storedKey)
}
