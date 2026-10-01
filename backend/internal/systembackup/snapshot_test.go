package systembackup

import (
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStageSystemDatabaseExcludesLiveFilesInternal(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, ".arcane-snapshot-stage")
	require.NoError(t, os.Mkdir(stage, 0o700))
	for _, name := range []string{systemRecoveryRequestName, "settings.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("data"), 0o600))
	}
	require.NoError(t, os.Symlink("settings.txt", filepath.Join(root, "link")))
	db, err := sql.Open("sqlite", filepath.Join(root, "arcane.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), "PRAGMA journal_mode=WAL; CREATE TABLE evidence (id TEXT); INSERT INTO evidence VALUES ('committed')")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(root, "arcane.db"), 0o640))
	projects := t.TempDir()
	composePath := filepath.Join(projects, "compose.yaml")
	require.NoError(t, os.WriteFile(composePath, []byte("services: {}"), 0o600))
	layout := backupSourceLayoutInternal{
		dataDirectory:     root,
		databaseName:      "arcane.db",
		projectsDirectory: projects,
		projectsPath:      snapshotProjectsPath,
		excludes:          []string{".arcane-snapshot-*", systemRecoveryRequestName, "arcane.db-wal", "arcane.db-shm", "arcane.db-journal"},
	}
	files, err := snapshotSourceFilesInternal(t.Context(), layout)
	require.NoError(t, err)
	require.NoError(t, stageSystemDatabaseInternal(t.Context(), db, filepath.Join(root, "arcane.db"), filepath.Join(stage, "arcane.db")))
	require.NoError(t, validateSnapshotSourcesInternal(t.Context(), layout, files))
	require.NoError(t, os.WriteFile(composePath, []byte("services: {app: {image: nginx}}"), 0o600))
	require.ErrorContains(t, validateSnapshotSourcesInternal(t.Context(), layout, files), "changed during capture")
	entries, err := os.ReadDir(stage)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "arcane.db", entries[0].Name())
	info, err := entries[0].Info()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	original, err := os.Stat(filepath.Join(root, "arcane.db"))
	require.NoError(t, err)
	require.Equal(t, original.Sys().(*syscall.Stat_t).Uid, info.Sys().(*syscall.Stat_t).Uid)
	require.Equal(t, original.Sys().(*syscall.Stat_t).Gid, info.Sys().(*syscall.Stat_t).Gid)
	snapshot, err := sql.Open("sqlite", filepath.Join(stage, "arcane.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = snapshot.Close() })
	var id string
	require.NoError(t, snapshot.QueryRowContext(t.Context(), "SELECT id FROM evidence").Scan(&id))
	require.Equal(t, "committed", id)
}
