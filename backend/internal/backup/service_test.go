package backup

import (
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
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
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "recovery-key-store-test-key-32bytes", Environment: "test"})
	store := NewRecoveryKeyStore(&database.DB{DB: gormDB})

	configured, err := store.Configured(t.Context())
	require.NoError(t, err)
	require.False(t, configured)

	_, err = store.Get(t.Context())
	require.ErrorIs(t, err, ErrRecoveryKeyNotConfigured)

	key, err := GenerateRecoveryKey()
	require.NoError(t, err)
	require.NoError(t, store.Set(t.Context(), key))

	configured, err = store.Configured(t.Context())
	require.NoError(t, err)
	require.True(t, configured)

	stored, err := store.Get(t.Context())
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
