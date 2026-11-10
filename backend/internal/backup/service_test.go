package backup

import (
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

func TestMarkSnapshotDirectories(t *testing.T) {
	files := []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}
	longOutput := "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"\r\ndrwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume/" +
		"folder\"\r\n-rw-r--r-- root root 5 1 Jan 2026 00:00 \"/volume/file.txt\"\r\nlrwxrwxrwx root root 4 1 Jan 20" +
		"26 00:00 \"/volume/link\" -> \"file.txt\""

	marked, err := markSnapshotDirectories(files, longOutput)
	require.NoError(t, err)
	require.Equal(t, []string{"/volume/", "/volume/folder/", "/volume/file.txt", "/volume/link"}, marked)
	require.Equal(t, []string{"/volume", "/volume/folder", "/volume/file.txt", "/volume/link"}, files)
}

func TestMarkSnapshotDirectoriesRejectsMismatchedListings(t *testing.T) {
	_, err := markSnapshotDirectories([]string{"/volume", "/volume/file.txt"}, "drwxr-xr-x root root 0 1 Jan 2026 00:00 \"/volume\"")
	require.ErrorContains(t, err, "different lengths")
}

func TestQualifySnapshotListing(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		snapshotPath string
		expected     []string
	}{
		{
			name:         "root listing",
			files:        []string{"folder/", "file.txt"},
			snapshotPath: "",
			expected:     []string{"folder/", "file.txt"},
		},
		{
			name:         "empty listing",
			files:        []string{},
			snapshotPath: "folder",
			expected:     []string{},
		},
		{
			name:         "nested listing",
			files:        []string{"nested/", "file.txt", "./link"},
			snapshotPath: "folder",
			expected:     []string{"folder/nested/", "folder/file.txt", "folder/link"},
		},
		{
			name:         "legacy system project listing",
			files:        []string{"demo/compose.yaml"},
			snapshotPath: "app/data/projects",
			expected:     []string{"app/data/projects/demo/compose.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qualified := qualifySnapshotListing(test.files, test.snapshotPath)
			require.Equal(t, test.expected, qualified)
			if len(test.files) > 0 {
				require.NotSame(t, &test.files[0], &qualified[0])
			}
		})
	}
}

func TestRecoveryKeyStoreRoundTrip(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:recovery-key-store?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&SystemBackupRecoveryConfig{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "recovery-key-store-test-key-32bytes", Environment: "test"})
	store := NewRecoveryKeyStore(&database.DB{DB: gormDB})

	configured, err := store.Configured(t.Context())
	require.NoError(t, err)
	require.False(t, configured)

	_, err = store.Get(t.Context(), RecoveryKeyConfigID)
	require.ErrorIs(t, err, ErrRecoveryKeyNotConfigured)

	key, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NoError(t, store.Set(t.Context(), RecoveryKeyConfigID, key))

	configured, err = store.Configured(t.Context())
	require.NoError(t, err)
	require.True(t, configured)

	stored, err := store.Get(t.Context(), RecoveryKeyConfigID)
	require.NoError(t, err)
	require.Equal(t, key, stored)
}

func TestRecoveryKeyValidation(t *testing.T) {
	require.NoError(t, ValidateRecoveryKey("QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"))
	require.Error(t, ValidateRecoveryKey("too-short"))
	require.Error(t, ValidateRecoveryKey(""))
	require.Error(t, ValidateRecoveryKey("QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN-EXTRA"))

	key, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NoError(t, ValidateRecoveryKey(key))
}

func TestEnvironmentRecoveryKey(t *testing.T) {
	const managerKey = "QWERTY-ABCDEF-234567-GHIJKL-MNOPQR-STUVWX-YZ2345-ZXCVBN"
	first := EnvironmentRecoveryKey(managerKey, "env-1")
	require.NoError(t, ValidateRecoveryKey(first))
	require.Equal(t, first, EnvironmentRecoveryKey(" "+managerKey+" ", "env-1"))
	require.NotEqual(t, managerKey, first)
	require.NotEqual(t, first, EnvironmentRecoveryKey(managerKey, "env-2"))
	rotated, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NotEqual(t, first, EnvironmentRecoveryKey(rotated, "env-1"))
}

func TestSnapshotCommand(t *testing.T) {
	single, err := snapshotCommand("volume", RootSnapshotInput(mount.Mount{Type: mount.TypeVolume, Source: "data", Target: "/volume"}))
	require.NoError(t, err)
	require.Equal(t, []string{"backup", "--init", "--json", "--host", "arcane", "--label", "volume", "--as-path", "/", "--", "/volume"}, single)

	multi, err := snapshotCommand("arcane-system-recovery", CreateSnapshotInput{Sources: []string{"/data", "/projects"}, Globs: []string{"!/data/arcane.db-wal"}})
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			"backup",
			"--init",
			"--json",
			"--host",
			"arcane",
			"--label",
			"arcane-system-recovery",
			"--glob",
			"!/data/arcane.db-wal",
			"--",
			"/data",
			"/projects",
		},
		multi,
	)

	_, err = snapshotCommand("x", CreateSnapshotInput{Sources: []string{"/data", "/projects"}, AsPath: "/"})
	require.ErrorContains(t, err, "single source")
	_, err = snapshotCommand("x", CreateSnapshotInput{})
	require.ErrorContains(t, err, "at least one snapshot source")
}

func TestExpiredUnmanagedRunIDs(t *testing.T) {
	db, err := database.Initialize(t.Context(), "file:backup-unmanaged-expiry?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	now := time.Now()
	day := 24 * time.Hour
	insert := func(id, trigger, status string, createdAt time.Time, orphanedAt *time.Time) {
		require.NoError(t, db.Exec(
			"INSERT INTO volume_backups (id, volume_name, size, created_at, trigger, status, orphaned_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			id, "app-data", 0, createdAt, trigger, status, orphanedAt,
		).Error)
	}
	insert("safety-old", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusSucceeded, now.Add(-8*day), nil)
	insert("safety-new", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusSucceeded, now.Add(-day), nil)
	insert("safety-running", types.VolumeBackupTriggerSafety, types.VolumeBackupStatusRunning, now.Add(-30*day), nil)
	insert("orphan-new", types.VolumeBackupTriggerScheduled, types.VolumeBackupStatusSucceeded, now.Add(-30*day), nil)
	insert("orphan-stale", types.VolumeBackupTriggerScheduled, types.VolumeBackupStatusFailed, now.Add(-20*day), new(now.Add(-8*day)))
	insert("managed-again", types.VolumeBackupTriggerManual, types.VolumeBackupStatusSucceeded, now.Add(-10*day), new(now.Add(-8*day)))
	sweep := types.UnmanagedBackupSweep{Now: now, Retention: 7 * day}
	orphanedAt := func(id string) *time.Time {
		var row struct{ OrphanedAt *time.Time }
		require.NoError(t, db.Table("volume_backups").Select("orphaned_at").Where("id = ?", id).Scan(&row).Error)
		return row.OrphanedAt
	}

	expired, err := ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", []string{"orphan-new", "orphan-stale"}, sweep)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"orphan-stale", "safety-old"}, expired)
	require.NotNil(t, orphanedAt("orphan-new"), "a newly orphaned backup starts its grace period instead of expiring by age")
	require.Nil(t, orphanedAt("managed-again"), "a backup managed again ends its grace period")

	recheck := types.UnmanagedBackupSweep{Now: now, Within: []string{"orphan-stale", "safety-new"}, Retention: 7 * day}
	expired, err = ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", []string{"orphan-new", "orphan-stale"}, recheck)
	require.NoError(t, err)
	require.Equal(t, []string{"orphan-stale"}, expired, "a recheck only reports backups from the batch it locked")

	expired, err = ExpiredUnmanagedRunIDs(t.Context(), db, "volume_backups", nil, sweep)
	require.NoError(t, err)
	require.Equal(t, []string{"safety-old"}, expired)
	require.Nil(t, orphanedAt("orphan-stale"))
}
