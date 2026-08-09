//go:build unix

package projects

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inodeOfInternal(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return stat.Ino
}

func TestMirrorDirectoryContentsPreserving_PreservesInodesAndPrunes(t *testing.T) {
	dst := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dst, "site"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "site", "index.html"), []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "removed.txt"), []byte("stale"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dst, ".env"), []byte("KEY=1"), 0o644))

	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "site"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "site", "index.html"), []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".env"), []byte("KEY=1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "added.txt"), []byte("fresh"), 0o644))

	rootIno := inodeOfInternal(t, dst)
	siteIno := inodeOfInternal(t, filepath.Join(dst, "site"))
	indexIno := inodeOfInternal(t, filepath.Join(dst, "site", "index.html"))

	require.NoError(t, MirrorDirectoryContentsPreserving(src, dst, nil))

	assert.Equal(t, rootIno, inodeOfInternal(t, dst))
	assert.Equal(t, siteIno, inodeOfInternal(t, filepath.Join(dst, "site")))
	assert.Equal(t, indexIno, inodeOfInternal(t, filepath.Join(dst, "site", "index.html")))

	content, err := os.ReadFile(filepath.Join(dst, "site", "index.html"))
	require.NoError(t, err)
	assert.Equal(t, "new", string(content))

	assert.NoFileExists(t, filepath.Join(dst, "removed.txt"))
	assert.FileExists(t, filepath.Join(dst, "added.txt"))

	envContent, err := os.ReadFile(filepath.Join(dst, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=1", string(envContent))
}

func TestCopyDirectoryContentsTolerant_SkipsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "compose.yaml"), []byte("services: {}"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "secrets"), 0o755))
	secretPath := filepath.Join(src, "secrets", "token.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("supersecret"), 0o644))
	require.NoError(t, os.Chmod(secretPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secretPath, 0o644) })

	dst := t.TempDir()
	skipped, err := CopyDirectoryContentsTolerant(src, dst)
	require.NoError(t, err)

	// The readable file is copied through.
	content, err := os.ReadFile(filepath.Join(dst, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "services: {}", string(content))

	// The unreadable file is reported and not copied.
	assert.Equal(t, []string{filepath.Join("secrets", "token.txt")}, skipped)
	assert.NoFileExists(t, filepath.Join(dst, "secrets", "token.txt"))
}

func TestMirrorDirectoryContentsPreserving_KeepsSkippedFile(t *testing.T) {
	// Simulates the project-update rollback: the backup omits a file Arcane
	// could not read, so restoring must preserve that file while still reverting
	// Arcane-managed files and pruning stragglers left by the failed update.
	backup := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(backup, ".env"), []byte("KEY=original"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(backup, "secrets"), 0o755))
	// The backup intentionally does NOT contain secrets/token.txt (it was skipped).

	live := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(live, ".env"), []byte("KEY=changed"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(live, "secrets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(live, "secrets", "token.txt"), []byte("supersecret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(live, "straggler.txt"), []byte("partial write"), 0o644))

	require.NoError(t, MirrorDirectoryContentsPreserving(backup, live, []string{filepath.Join("secrets", "token.txt")}))

	// The skipped (preserved) file survives untouched.
	secretContent, err := os.ReadFile(filepath.Join(live, "secrets", "token.txt"))
	require.NoError(t, err)
	assert.Equal(t, "supersecret", string(secretContent))

	// The Arcane-managed file is reverted to the backed-up version.
	envContent, err := os.ReadFile(filepath.Join(live, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "KEY=original", string(envContent))

	// The straggler created during the failed update is pruned.
	assert.NoFileExists(t, filepath.Join(live, "straggler.txt"))
}

func TestCopyDirectoryContentsTolerant_SkipsNonRegularFiles(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "config"), 0o755))
	// A FIFO stands in for the sockets running containers leave behind
	// (e.g. config/.XDG/wayland-0 in #3003); reading either fails with a
	// non-permission error.
	require.NoError(t, syscall.Mkfifo(filepath.Join(src, "config", "app.sock"), 0o644))

	dst := t.TempDir()
	skipped, err := CopyDirectoryContentsTolerant(src, dst)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("config", "app.sock")}, skipped)
	assert.FileExists(t, filepath.Join(dst, "compose.yaml"))
	assert.NoFileExists(t, filepath.Join(dst, "config", "app.sock"))
}

func TestMirrorDirectoryContentsPreserving_ToleratesUnreadableDestSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}

	dst := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dst, "compose.yaml"), []byte("old\n"), 0o644))
	locked := filepath.Join(dst, "volumes")
	require.NoError(t, os.MkdirAll(filepath.Join(locked, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "config", "app.conf"), []byte("keep"), 0o644))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "compose.yaml"), []byte("new\n"), 0o644))

	// The unreadable live-project subdir must be left intact instead of
	// failing the whole mirror (#3509, #3085).
	require.NoError(t, MirrorDirectoryContentsPreserving(src, dst, []string{"volumes"}))

	content, err := os.ReadFile(filepath.Join(dst, "compose.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(content))
	require.NoError(t, os.Chmod(locked, 0o755))
	assert.FileExists(t, filepath.Join(locked, "config", "app.conf"))
}
