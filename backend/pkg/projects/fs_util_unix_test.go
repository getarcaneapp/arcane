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

func TestMirrorDirectoryContents_PreservesInodesAndPrunes(t *testing.T) {
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

	require.NoError(t, MirrorDirectoryContents(src, dst))

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
