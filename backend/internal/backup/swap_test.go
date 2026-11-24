package backup

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func writeSwapFiles(t *testing.T, data string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Join(data, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(data, name), []byte(content), 0o644))
	}
}

func swapInode(t *testing.T, filePath string) uint64 {
	t.Helper()
	info, err := os.Lstat(filePath)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestSwapReplacesArcaneDataAndLeavesPreservedPaths(t *testing.T) {
	data := t.TempDir()
	staging := SwapStagingPrefix + "snap"
	writeSwapFiles(t, data, map[string]string{
		"arcane.db":                        "live database",
		"arcane.db-wal":                    "stale wal",
		"arcane.db-shm":                    "stale shm",
		"settings.txt":                     "live",
		"gone.txt":                         "not in the backup",
		"projects/demo/compose.yaml":       "services: {}",
		"stacks/apps/web/compose.yaml":     "services: {}",
		"stacks/notes.txt":                 "not in the backup",
		"dir-to-file/inner.txt":            "live",
		"file-to-dir":                      "live",
		staging + "/arcane.db":             "restored database",
		staging + "/settings.txt":          "restored",
		staging + "/stacks/readme.txt":     "restored",
		staging + "/dir-to-file":           "restored",
		staging + "/file-to-dir/inner.txt": "restored",
		staging + "/projects/junk.txt":     "never placed",
	})
	require.NoError(t, os.Chmod(filepath.Join(data, staging, "settings.txt"), 0o600))
	require.NoError(t, os.Symlink("settings.txt", filepath.Join(data, "link")))
	require.NoError(t, os.Symlink("projects", filepath.Join(data, "projects-link")))
	require.NoError(t, os.Symlink("arcane.db", filepath.Join(data, staging, "link")))
	require.NoError(t, os.Mkdir(filepath.Join(data, staging, "new-dir"), 0o700))
	require.NoError(t, os.Chmod(filepath.Join(data, staging, "new-dir"), 0o750))
	preservedInodes := map[string]uint64{}
	for _, name := range []string{"projects", "projects/demo/compose.yaml", "projects-link", "stacks", "stacks/apps"} {
		preservedInodes[name] = swapInode(t, filepath.Join(data, name))
	}

	plan, err := PlanSwap(t.Context(), types.RecoveryRestoreSwap{Directory: data, ID: "snap", Preserve: []string{"projects", "projects-link", "projects-link/demo", "stacks/apps"}})
	require.NoError(t, err)
	// Removals run before any staged entry moves in, so stale SQLite sidecars are gone before the database is replaced.
	require.Subset(t, plan.removals, []string{"arcane.db-shm", "arcane.db-wal", "gone.txt", "stacks/notes.txt"})
	require.NotContains(t, plan.removals, "arcane.db")
	require.NoError(t, plan.Apply(t.Context()))

	read := func(name string) string {
		content, readErr := os.ReadFile(filepath.Join(data, name))
		require.NoError(t, readErr)
		return string(content)
	}
	require.Equal(t, "restored database", read("arcane.db"))
	require.Equal(t, "restored", read("settings.txt"))
	require.Equal(t, "restored", read("stacks/readme.txt"))
	require.Equal(t, "restored", read("dir-to-file"))
	require.Equal(t, "restored", read("file-to-dir/inner.txt"))
	require.Equal(t, "services: {}", read("projects/demo/compose.yaml"))
	require.Equal(t, "services: {}", read("stacks/apps/web/compose.yaml"))
	for name, inode := range preservedInodes {
		require.Equal(t, inode, swapInode(t, filepath.Join(data, name)), name)
	}
	for _, removed := range []string{"arcane.db-wal", "arcane.db-shm", "gone.txt", "stacks/notes.txt", "projects/junk.txt", staging, SwapTrashPrefix + "snap"} {
		_, statErr := os.Lstat(filepath.Join(data, removed))
		require.ErrorIs(t, statErr, os.ErrNotExist, removed)
	}
	target, err := os.Readlink(filepath.Join(data, "link"))
	require.NoError(t, err)
	require.Equal(t, "arcane.db", target)
	info, err := os.Stat(filepath.Join(data, "settings.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(data, "new-dir"))
	require.NoError(t, err)
	require.Equal(t, os.ModeDir|0o750, info.Mode()&(os.ModeDir|os.ModePerm))
}

func TestSwapCreatesMissingProtectedParent(t *testing.T) {
	data := t.TempDir()
	staging := SwapStagingPrefix + "snap"
	writeSwapFiles(t, data, map[string]string{staging + "/stacks/readme.txt": "restored", staging + "/stacks/apps/old.txt": "never placed"})
	require.NoError(t, os.Chmod(filepath.Join(data, staging, "stacks"), 0o710))

	plan, err := PlanSwap(t.Context(), types.RecoveryRestoreSwap{Directory: data, ID: "snap", Preserve: []string{"stacks/apps"}})
	require.NoError(t, err)
	require.NoError(t, plan.Apply(t.Context()))

	info, err := os.Stat(filepath.Join(data, "stacks"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o710), info.Mode().Perm())
	_, err = os.Stat(filepath.Join(data, "stacks", "readme.txt"))
	require.NoError(t, err)
	_, err = os.Lstat(filepath.Join(data, "stacks", "apps"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSwapUndoesFailedApply(t *testing.T) {
	data := t.TempDir()
	staging := SwapStagingPrefix + "snap"
	writeSwapFiles(t, data, map[string]string{
		"arcane.db":                    "live database",
		"gone.txt":                     "not in the backup",
		staging + "/arcane.db":         "restored database",
		staging + "/stacks/readme.txt": "restored",
		staging + "/zzz.txt":           "vanishes before the swap",
	})
	liveDatabase := swapInode(t, filepath.Join(data, "arcane.db"))

	plan, err := PlanSwap(t.Context(), types.RecoveryRestoreSwap{Directory: data, ID: "snap", Preserve: []string{"stacks/apps"}})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(data, staging, "zzz.txt")))
	err = plan.Apply(t.Context())
	require.ErrorContains(t, err, "move restored zzz.txt into place")
	require.ErrorIs(t, err, ErrSwapNotApplied)
	require.NotContains(t, err.Error(), "undo the partial swap")

	require.Equal(t, liveDatabase, swapInode(t, filepath.Join(data, "arcane.db")))
	content, err := os.ReadFile(filepath.Join(data, "gone.txt"))
	require.NoError(t, err)
	require.Equal(t, "not in the backup", string(content))
	content, err = os.ReadFile(filepath.Join(data, staging, "stacks", "readme.txt"))
	require.NoError(t, err)
	require.Equal(t, "restored", string(content))
	for _, removed := range []string{"stacks", "zzz.txt", SwapTrashPrefix + "snap"} {
		_, statErr := os.Lstat(filepath.Join(data, removed))
		require.ErrorIs(t, statErr, os.ErrNotExist, removed)
	}
}

func TestPlanSwapRejectsUnsafePlansBeforeAnyChange(t *testing.T) {
	tests := []struct {
		prepare  func(t *testing.T, data string)
		name     string
		id       string
		preserve []string
	}{
		{name: "escaping path", id: "snap", preserve: []string{"../outside"}, prepare: func(*testing.T, string) {}},
		{
			name:     "parent behind a symbolic link",
			id:       "snap",
			preserve: []string{"linked/projects"},
			prepare: func(t *testing.T, data string) {
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(data, "linked")))
			},
		},
		{
			name:     "backup has a file where a parent folder is needed",
			id:       "snap",
			preserve: []string{"stacks/apps"},
			prepare: func(t *testing.T, data string) {
				writeSwapFiles(t, data, map[string]string{"stacks/apps/compose.yaml": "services: {}", SwapStagingPrefix + "snap/stacks": "file"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := t.TempDir()
			writeSwapFiles(t, data, map[string]string{"arcane.db": "live database", SwapStagingPrefix + "snap/arcane.db": "restored database"})
			test.prepare(t, data)

			_, err := PlanSwap(t.Context(), types.RecoveryRestoreSwap{Directory: data, ID: test.id, Preserve: test.preserve})
			require.Error(t, err)
			content, err := os.ReadFile(filepath.Join(data, "arcane.db"))
			require.NoError(t, err)
			require.Equal(t, "live database", string(content))
			_, err = os.Stat(filepath.Join(data, SwapStagingPrefix+"snap/arcane.db"))
			require.NoError(t, err)
		})
	}
}

func TestRemoveSwapLeftovers(t *testing.T) {
	data := t.TempDir()
	writeSwapFiles(t, data, map[string]string{
		SwapStagingPrefix + "old/arcane.db": "partial",
		SwapTrashPrefix + "old/1":           "moved aside",
		".arcane-snapshot-abc/arcane.db":    "backup staging",
		"settings.txt":                      "live",
	})

	require.NoError(t, RemoveSwapLeftovers(t.Context(), data, SwapStagingPrefix))

	entries, err := os.ReadDir(data)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.ElementsMatch(t, []string{".arcane-snapshot-abc", SwapTrashPrefix + "old", "settings.txt"}, names)
}
