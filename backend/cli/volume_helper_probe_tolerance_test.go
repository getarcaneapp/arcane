package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocatedBytesForPathInternal_SkipsUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission-mode walk errors are not reliable in this environment")
	}

	dir := t.TempDir()
	lockedDir := filepath.Join(dir, "locked")
	require.NoError(t, os.MkdirAll(lockedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(lockedDir, "data.txt"), []byte("hidden"), 0o600))
	require.NoError(t, os.Chmod(lockedDir, 0))
	t.Cleanup(func() {
		_ = os.Chmod(lockedDir, 0o755)
	})

	_, err := allocatedBytesForPathInternal(context.Background(), dir)

	require.NoError(t, err)
}

func TestAllocatedBytesForPathInternal_ReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := allocatedBytesForPathInternal(ctx, t.TempDir())

	require.ErrorIs(t, err, context.Canceled)
}

func TestInternalVolumeHelperSkippableWalkError(t *testing.T) {
	require.True(t, internalVolumeHelperSkippableWalkError(&os.PathError{Op: "lstat", Path: "missing", Err: syscall.ENOENT}))
	require.True(t, internalVolumeHelperSkippableWalkError(&os.PathError{Op: "open", Path: "locked", Err: syscall.EACCES}))
	require.False(t, internalVolumeHelperSkippableWalkError(errors.New("unexpected walk failure")))
}
