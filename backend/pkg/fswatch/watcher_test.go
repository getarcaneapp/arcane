package fswatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWatcher_StartWatchesExistingSymlinkDirectoriesWhenEnabled(t *testing.T) {
	root := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := filepath.Join(targetRoot, "real-project")
	require.NoError(t, os.MkdirAll(targetPath, 0o755))

	linkPath := filepath.Join(root, "linked-project")
	require.NoError(t, os.Symlink(targetPath, linkPath))

	changeCh := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher, err := NewWatcher(root, WatcherOptions{
		Debounce:          25 * time.Millisecond,
		MaxDepth:          1,
		FollowSymlinkDirs: true,
		OnChange: func(context.Context) {
			select {
			case changeCh <- struct{}{}:
			default:
			}
		},
	})
	require.NoError(t, err)
	require.NoError(t, watcher.Start(ctx))
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	require.NoError(t, os.WriteFile(filepath.Join(targetPath, "compose.yaml"), []byte("services: {}\n"), 0o644))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected symlinked directory change to trigger watcher callback")
	}
}

func TestWatcher_StartSkipsExistingSymlinkDirectoriesWhenDisabled(t *testing.T) {
	root := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := filepath.Join(targetRoot, "real-project")
	require.NoError(t, os.MkdirAll(targetPath, 0o755))

	linkPath := filepath.Join(root, "linked-project")
	require.NoError(t, os.Symlink(targetPath, linkPath))

	changeCh := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher, err := NewWatcher(root, WatcherOptions{
		Debounce: 25 * time.Millisecond,
		MaxDepth: 1,
		OnChange: func(context.Context) {
			select {
			case changeCh <- struct{}{}:
			default:
			}
		},
	})
	require.NoError(t, err)
	require.NoError(t, watcher.Start(ctx))
	defer func() {
		require.NoError(t, watcher.Stop())
	}()

	require.NoError(t, os.WriteFile(filepath.Join(targetPath, "compose.yaml"), []byte("services: {}\n"), 0o644))

	select {
	case <-changeCh:
		t.Fatal("did not expect symlinked directory change to trigger watcher callback when disabled")
	case <-time.After(300 * time.Millisecond):
	}
}
