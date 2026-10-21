package recovery

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
)

func newRestoredDatabaseForTest(t *testing.T, runIDs ...string) (string, string) {
	t.Helper()
	databaseURL := "file:" + filepath.Join(t.TempDir(), "arcane.db")
	dsn, err := database.ParseSQLiteConnectionString(databaseURL)
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&system.SystemBackupRun{}, &activity.Activity{}, &settings.SettingVariable{}, &backup.SystemBackupRecoveryConfig{}))
	require.NoError(t, db.Create(&settings.SettingVariable{Key: "projectsDirectory", Value: "/app/data/projects"}).Error)
	for i, id := range runIDs {
		require.NoError(t, db.Create(&system.SystemBackupRun{
			ID: id, CreatedAt: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC),
			Status: system.SystemBackupStatusRunning,
		}).Error)
	}
	resourceType := "system_backup"
	require.NoError(t, db.Create(&activity.Activity{
		ID: "activity-1", EnvironmentID: "0", Type: types.ActivityTypeResourceAction,
		Status: types.ActivityStatusRunning, ResourceType: &resourceType,
		StartedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	return databaseURL, dsn
}

func TestFinalizeRestoredBackup(t *testing.T) {
	databaseURL, dsn := newRestoredDatabaseForTest(t, "older", "selected")
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "recovery-finalize-test-key-32bytes", Environment: "test"})
	recoveryKey, err := backup.GenerateRecoveryKey()
	require.NoError(t, err)

	require.NoError(t, finalizeRestoredBackup(t.Context(), databaseURL, "selected", "activity-1", types.RecoveryRestoreRequest{
		BackupID: "request-id", RemoteSnapshotID: "snapshot-1", S3DestinationID: "destination-1", Size: 1234,
		RecoveryKey:     recoveryKey,
		ProjectsSetting: "/srv/projects",
		SafetyBackup: &types.RecoverySafetyBackup{
			ID: "safety", LocalSnapshotID: "safety-snapshot", Size: 4321,
			CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		},
	}))

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	var selected, older, safety system.SystemBackupRun
	require.NoError(t, db.First(&selected, "id = ?", "selected").Error)
	require.Equal(t, system.SystemBackupStatusSucceeded, selected.Status)
	require.EqualValues(t, 1234, selected.Size)
	require.Equal(t, "snapshot-1", selected.RemoteSnapshotID)
	require.Equal(t, "destination-1", selected.S3DestinationID)
	require.NoError(t, db.First(&older, "id = ?", "older").Error)
	require.Equal(t, system.SystemBackupStatusRunning, older.Status)
	require.NoError(t, db.First(&safety, "id = ?", "safety").Error)
	require.Equal(t, system.SystemBackupStatusSucceeded, safety.Status)
	require.Equal(t, system.SystemBackupTriggerSafety, safety.Trigger)
	require.Equal(t, "safety-snapshot", safety.LocalSnapshotID)
	require.EqualValues(t, 4321, safety.Size)
	var entry activity.Activity
	require.NoError(t, db.First(&entry, "id = ?", "activity-1").Error)
	require.Equal(t, types.ActivityStatusSuccess, entry.Status)
	var projectsSetting settings.SettingVariable
	require.NoError(t, db.First(&projectsSetting, "key = ?", "projectsDirectory").Error)
	require.Equal(t, "/srv/projects", projectsSetting.Value)
	storedKey, err := backup.NewRecoveryKeyStore(&database.DB{DB: db}).Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, recoveryKey, storedKey)
}

func TestFinalizeRestoredBackupInternalFallsBackForLegacyManifest(t *testing.T) {
	databaseURL, dsn := newRestoredDatabaseForTest(t, "older", "newest")

	require.NoError(t, finalizeRestoredBackup(t.Context(), databaseURL, "", "", types.RecoveryRestoreRequest{BackupID: "missing-discovery-id"}))

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	var newest system.SystemBackupRun
	require.NoError(t, db.First(&newest, "id = ?", "newest").Error)
	require.Equal(t, system.SystemBackupStatusSucceeded, newest.Status)
	var entry activity.Activity
	require.NoError(t, db.First(&entry, "id = ?", "activity-1").Error)
	require.Equal(t, types.ActivityStatusSuccess, entry.Status)
}

func TestRunStagesInternalRejectsStagesWithoutDestination(t *testing.T) {
	err := runStages(t.Context(), nil, types.RecoveryRestoreRequest{}, []types.RecoveryRestoreStage{{SourcePath: "/data"}})
	require.ErrorContains(t, err, "restore stage for /data has no destination")
}
