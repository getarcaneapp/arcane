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
	ctx := t.Context()

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

func TestWatcher_StartTriggersOnAtomicSaveTempFileInsideProjectDir(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	changeCh := make(chan struct{}, 1)
	ctx := t.Context()

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

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".compose.yaml.tmp"), []byte("services: {}\n"), 0o644))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected temp file change inside project directory to trigger watcher callback")
	}
}

func TestWatcher_StartTriggersOnChmodInsideProjectDir(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	targetFile := filepath.Join(projectDir, "notes.tmp")
	require.NoError(t, os.WriteFile(targetFile, []byte("demo\n"), 0o644))

	changeCh := make(chan struct{}, 1)
	ctx := t.Context()

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

	require.NoError(t, os.Chmod(targetFile, 0o600))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected chmod inside project directory to trigger watcher callback")
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
	ctx := t.Context()

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

func TestWatcher_StartTriggersOnNestedProjectFileBeyondDepthOne(t *testing.T) {
	root := t.TempDir()
	nestedDir := filepath.Join(root, "main-project", "sub-project1")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))

	changeCh := make(chan struct{}, 1)
	ctx := t.Context()

	watcher, err := NewWatcher(root, WatcherOptions{
		Debounce: 25 * time.Millisecond,
		MaxDepth: 0,
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

	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected nested compose file change to trigger watcher callback")
	}
}

func TestWatcher_StartTriggersOnNestedDirectoryCreateAndRemove(t *testing.T) {
	root := t.TempDir()

	changeCh := make(chan struct{}, 4)
	ctx := t.Context()

	watcher, err := NewWatcher(root, WatcherOptions{
		Debounce: 25 * time.Millisecond,
		MaxDepth: 0,
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

	nestedDir := filepath.Join(root, "main-project", "sub-project1")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected nested directory create to trigger watcher callback")
	}

	require.NoError(t, os.RemoveAll(filepath.Join(root, "main-project")))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected nested directory removal to trigger watcher callback")
	}
}

func TestWatcher_Stop_IsIdempotentAfterStart(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()

	watcher, err := NewWatcher(root, WatcherOptions{})
	require.NoError(t, err)
	require.NoError(t, watcher.Start(ctx))

	stopWatcherWithinTimeoutInternal(t, watcher)
	stopWatcherWithinTimeoutInternal(t, watcher)
}

func TestWatcher_Stop_IsSafeBeforeStart(t *testing.T) {
	root := t.TempDir()

	watcher, err := NewWatcher(root, WatcherOptions{})
	require.NoError(t, err)

	stopWatcherWithinTimeoutInternal(t, watcher)
	stopWatcherWithinTimeoutInternal(t, watcher)
}

func stopWatcherWithinTimeoutInternal(t *testing.T, watcher *Watcher) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- watcher.Stop()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher to stop")
	}
}
