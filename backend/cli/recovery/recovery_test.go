package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/types/v3"
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
	recoveryKey, err := backup.GenerateRecoveryKey()
	require.NoError(t, err)

	require.NoError(t, finalizeRestoredBackup(t.Context(), map[string]string{
		"ARCANE_DATABASE_URL":   databaseURL,
		"ARCANE_ENCRYPTION_KEY": "recovery-finalize-test-key-32bytes",
		"ARCANE_ENVIRONMENT":    "test",
	}, "selected", "activity-1", types.RecoveryRestoreRequest{
		BackupID: "request-id", RemoteSnapshotID: "snapshot-1", S3DestinationID: "destination-1", Size: 1234,
		RecoveryKey:                recoveryKey,
		ProjectsSetting:            "/srv/projects",
		ProjectsDirectoriesSetting: `[{"path":"/srv/more","enabled":true}]`,
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
	var projectsDirectories settings.SettingVariable
	require.NoError(t, db.First(&projectsDirectories, "key = ?", "projectsDirectories").Error)
	require.JSONEq(t, `[{"path":"/srv/more","enabled":true}]`, projectsDirectories.Value)
	var discoveryPending settings.SettingVariable
	require.NoError(t, db.First(&discoveryPending, "key = ?", backup.ProjectDiscoveryPendingKey).Error)
	require.Equal(t, "true", discoveryPending.Value)
	storedKey, err := backup.NewRecoveryKeyStore(&database.DB{DB: db}).Get(t.Context(), backup.RecoveryKeyConfigID)
	require.NoError(t, err)
	require.Equal(t, recoveryKey, storedKey)
}

func TestFinalizeRestoredBackupInternalFallsBackForLegacyManifest(t *testing.T) {
	databaseURL, dsn := newRestoredDatabaseForTest(t, "older", "newest")

	require.NoError(t, finalizeRestoredBackup(t.Context(), map[string]string{
		"ARCANE_DATABASE_URL": databaseURL,
		"ARCANE_ENVIRONMENT":  "test",
	}, "", "", types.RecoveryRestoreRequest{BackupID: "missing-discovery-id"}))

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
	_, err := runStages(t.Context(), []types.RecoveryRestoreStage{{SourcePath: "/data"}}, nil)
	require.ErrorContains(t, err, "restore stage for /data has no destination")
}

func stagedRestoreRequest(data string) types.RecoveryRestoreRequest {
	stage := func(snapshotID string) types.RecoveryRestoreStage {
		return types.RecoveryRestoreStage{
			Swap:       &types.RecoveryRestoreSwap{Directory: data, ID: snapshotID, Preserve: []string{"projects"}},
			Target:     types.RecoveryRestoreTarget{Path: "/restore/" + backup.SwapStagingPrefix + snapshotID, Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: "arcane-data", Target: "/restore"}}},
			SnapshotID: snapshotID,
			SourcePath: "/data",
			Globs:      []string{"!/projects", "!/projects/**"},
			Repository: types.RecoveryRestoreRepository{Environment: nil, Mounts: nil},
		}
	}
	return types.RecoveryRestoreRequest{Stages: []types.RecoveryRestoreStage{stage("restore")}, RollbackStages: []types.RecoveryRestoreStage{stage("safety")}}
}

func TestRestoreDataInternalSkipsRollbackWhenTheSwapChangesNothing(t *testing.T) {
	data := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(data, "arcane.db"), []byte("live"), 0o600))
	var restored []string
	restarts := 0
	err := restoreData(t.Context(), stagedRestoreRequest(data), func(_ context.Context, stage types.RecoveryRestoreStage) error {
		restored = append(restored, stage.SnapshotID)
		staging := filepath.Join(data, backup.SwapStagingPrefix+stage.SnapshotID)
		require.NoError(t, os.MkdirAll(staging, 0o700))
		// A file in the trash folder's place makes the swap fail before it moves anything.
		require.NoError(t, os.WriteFile(filepath.Join(data, backup.SwapTrashPrefix+stage.SnapshotID), nil, 0o600))
		return os.WriteFile(filepath.Join(staging, "arcane.db"), []byte("restored"), 0o600)
	}, func() { restarts++ })

	require.ErrorIs(t, err, backup.ErrSwapNotApplied)
	require.ErrorContains(t, err, "Arcane's data was not changed and Arcane was restarted")
	require.Equal(t, []string{"restore"}, restored)
	require.Equal(t, 1, restarts)
	entries, err := os.ReadDir(data)
	require.NoError(t, err)
	// The leftover in the trash folder's place may hold live entries, so it is kept.
	require.Len(t, entries, 2)
	content, err := os.ReadFile(filepath.Join(data, "arcane.db"))
	require.NoError(t, err)
	require.Equal(t, "live", string(content))
}

func TestRestoreDataInternalSwapsStagedRestoreAroundProjects(t *testing.T) {
	data := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(data, "projects", "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, "projects", "demo", "compose.yaml"), []byte("services: {}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(data, "arcane.db"), []byte("live"), 0o600))
	restarts := 0
	err := restoreData(t.Context(), stagedRestoreRequest(data), func(_ context.Context, stage types.RecoveryRestoreStage) error {
		staging := filepath.Join(data, backup.SwapStagingPrefix+stage.SnapshotID)
		require.NoError(t, os.MkdirAll(staging, 0o700))
		return os.WriteFile(filepath.Join(staging, "arcane.db"), []byte("restored"), 0o600)
	}, func() { restarts++ })

	require.NoError(t, err)
	require.Zero(t, restarts)
	content, err := os.ReadFile(filepath.Join(data, "arcane.db"))
	require.NoError(t, err)
	require.Equal(t, "restored", string(content))
	content, err = os.ReadFile(filepath.Join(data, "projects", "demo", "compose.yaml"))
	require.NoError(t, err)
	require.Equal(t, "services: {}", string(content))
	entries, err := os.ReadDir(data)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}
