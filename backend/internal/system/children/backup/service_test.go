package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"
	"kit.getarcane.app/sys/crypto"

	backupengine "getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler/flow/flowtest"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/volume"
	francistest "getarcane.dev/arcane/v3/pkg/francis/testing"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type recordingTransport struct {
	environment.RemoteTransport

	failing string
	calls   map[string]types.CreateSystemBackupRequest
	paths   []string
}

func TestHistoryDestinationDecoration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&s3.S3Destination{}, &settings.SettingVariable{}))
	destination := s3.S3Destination{Name: "Offsite", Bucket: "backups"}
	require.NoError(t, db.Create(&destination).Error)
	store := &database.DB{DB: db}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     store,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	service := NewService(Dependencies{
		S3Destinations: s3.NewS3DestinationService(store, func(context.Context, string) error { return nil }),
		Settings:       settingsService,
		Config:         &config.Config{},
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
	require.NoError(t, NewService(Dependencies{}).RegisterWorkflows(harness.Engine, nil))
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
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     &database.DB{DB: db},
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	recovered := NewService(Dependencies{
		Config:   cfg,
		Settings: settingsService,
	}).recoveryEnvironment(t.Context())
	require.Equal(t, "encryption-secret", recovered["ARCANE_ENCRYPTION_KEY"])
	require.Equal(t, "admin-key", recovered["ARCANE_ADMIN_STATIC_API_KEY"])
	require.Equal(t, "0640", recovered["ARCANE_FILE_PERM"])
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
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     &database.DB{DB: db},
		Config: &config.Config{},
		Env:    settingsEnv,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	recovered := NewService(Dependencies{
		Config:      &config.Config{},
		Settings:    settingsService,
		SettingsEnv: settingsEnv,
	}).recoveryEnvironment(t.Context())
	require.Equal(t, "oidc-secret", recovered["ARCANE_OIDC_CLIENT_SECRET"])
	require.Equal(t, "tcp://docker:2375", recovered["DOCKER_HOST"])
	require.Equal(t, "/srv/projects", recovered["ARCANE_PROJECTS_DIRECTORY"])
	require.NotContains(t, recovered, "PROJECTS_DIRECTORY")
}

func TestSetRecoveryKeyKeepsCurrentKeyWhenRekeyFails(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&backupengine.SystemBackupRecoveryConfig{}, &s3.S3Destination{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "system-backup-rekey-test-key-32bytes", Environment: "test"})
	db := &database.DB{DB: gormDB}
	recoveryKeys := backupengine.NewRecoveryKeyStore(db)
	oldKey := "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	require.NoError(t, recoveryKeys.Set(t.Context(), backupengine.RecoveryKeyConfigID, oldKey))
	runtime := francistest.New(t)
	gate := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, gate.Register(runtime))
	francistest.Start(t, runtime)
	engine := backupengine.NewEngine(gate, nil, func(context.Context, types.BackupRequester) error {
		return nil
	})
	t.Cleanup(func() { require.NoError(t, engine.Stop(context.WithoutCancel(t.Context()))) })
	dockerClient, err := client.New(client.WithHost("tcp://127.0.0.1:1"), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	dockerService := newSettingsDockerService(t, dockerClient)
	service := NewService(Dependencies{
		Store: Store{
			CountRuns:                 func(context.Context) (int64, error) { return 1, nil },
			CountVolumeRepositoryRuns: func(context.Context) (int64, error) { return 0, nil },
			CountRunningVolumeBackups: func(context.Context) (int64, error) { return 0, nil },
		},
		DB:     db,
		Docker: dockerService,
		Volumes: volume.NewVolumeService(volume.VolumeServiceParams{
			Config:        &config.Config{},
			DB:            db,
			DockerService: dockerService,
		}),
		Engine:         engine,
		S3Destinations: s3.NewS3DestinationService(db, func(context.Context, string) error { return nil }),
		RecoveryKeys:   recoveryKeys,
	})

	_, err = service.SetRecoveryKey(t.Context(), "ASDFGH-QWERTY-765432-LKJHGF-RQPONM-XWVUTS-5432ZY-NBVCXZ")
	require.Error(t, err)
	storedKey, err := recoveryKeys.Get(t.Context(), backupengine.RecoveryKeyConfigID)
	require.NoError(t, err)
	require.Equal(t, oldKey, storedKey)
}

func newSettingsDockerService(t *testing.T, dockerClient *client.Client) *docker.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
}

func (t *recordingTransport) ProxyJSONRequestForEnvironment(_ context.Context, env environment.Environment, method, path string, body []byte, _ any) error {
	var request types.CreateSystemBackupRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return err
	}
	t.calls[env.ID] = request
	t.paths = append(t.paths, method+" "+path)
	if env.ID == t.failing {
		return errors.New("agent is not connected")
	}
	return nil
}

func TestBackUpRemoteEnvironments(t *testing.T) {
	const managerKey = "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&backupengine.SystemBackupRecoveryConfig{}, &environment.Environment{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "system-backup-fanout-test-key-32byte", Environment: "test"})
	db := &database.DB{DB: gormDB}
	recoveryKeys := backupengine.NewRecoveryKeyStore(db)
	require.NoError(t, recoveryKeys.Set(t.Context(), backupengine.RecoveryKeyConfigID, managerKey))
	for _, id := range []string{"env-new", "env-offline", "env-done"} {
		require.NoError(t, gormDB.Create(&environment.Environment{
			BaseModel: database.BaseModel{ID: id},
			Name:      id,
			ApiUrl:    "http://" + id,
			Enabled:   true,
		}).Error)
	}
	require.NoError(t, gormDB.Create(&environment.Environment{
		BaseModel: database.BaseModel{ID: "env-disabled"},
		Name:      "env-disabled",
		ApiUrl:    "http://env-disabled",
	}).Error)
	run := &types.SystemBackupRun{
		ID:              "system-run",
		Destination:     types.SystemBackupDestinationLocalS3,
		S3DestinationID: "offsite",
	}
	previous := types.SchedulerRun{Outcome: types.SchedulerOutcome{Targets: []types.TargetOutcome{
		{ResourceType: "environment", ID: "environment:env-done", Status: types.RunStatusSucceeded},
		{ResourceType: "environment", ID: "environment:env-offline", Status: types.RunStatusFailed},
	}}}

	transport := &recordingTransport{failing: "env-offline", calls: map[string]types.CreateSystemBackupRequest{}}
	service := NewService(Dependencies{
		Config:       &config.Config{},
		RecoveryKeys: recoveryKeys,
	})
	service.environments = environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		DB:              db,
		RemoteTransport: transport,
	})
	recorded := map[string]types.TargetOutcome{}
	ctx := jobcontext.WithExecution(t.Context(), previous, func(target types.TargetOutcome) error {
		recorded[target.ID] = target
		return nil
	})
	service.backUpRemoteEnvironments(ctx, run, "")

	require.Equal(t, []string{"POST /api/backups", "POST /api/backups"}, transport.paths)
	require.Equal(t, map[string]types.CreateSystemBackupRequest{
		"env-new": {
			ID:              "system-run",
			Destination:     types.SystemBackupDestinationLocalS3,
			S3DestinationID: "offsite",
			RecoveryKey:     backupengine.EnvironmentRecoveryKey(managerKey, "env-new"),
		},
		"env-offline": {
			ID:              "system-run",
			Destination:     types.SystemBackupDestinationLocalS3,
			S3DestinationID: "offsite",
			RecoveryKey:     backupengine.EnvironmentRecoveryKey(managerKey, "env-offline"),
		},
	}, transport.calls)
	require.Equal(t, map[string]types.TargetOutcome{
		"environment:env-new":     {ResourceType: "environment", ID: "environment:env-new", Status: types.RunStatusSucceeded},
		"environment:env-offline": {ResourceType: "environment", ID: "environment:env-offline", Status: types.RunStatusFailed, Message: "agent is not connected"},
	}, recorded)

	agentTransport := &recordingTransport{calls: map[string]types.CreateSystemBackupRequest{}}
	agentService := NewService(Dependencies{
		Config:       &config.Config{AgentEnabled: true},
		RecoveryKeys: recoveryKeys,
	})
	agentService.environments = environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		DB:              db,
		RemoteTransport: agentTransport,
	})
	agentService.backUpRemoteEnvironments(t.Context(), run, "")
	require.Empty(t, agentTransport.calls)
}
