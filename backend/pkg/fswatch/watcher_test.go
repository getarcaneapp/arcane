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

func TestWatcher_StartIgnoresNonProjectFileWritesInsideProjectDir(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "demo")
	buildDir := filepath.Join(projectDir, ".esphome", "build", "node-1", ".piolibdeps", "node-1", "ESPMicroSpeechFeatures")
	require.NoError(t, os.MkdirAll(buildDir, 0o755))

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

	// Let the initial attach settle so the build dir is subscribed.
	time.Sleep(100 * time.Millisecond)

	// Simulate ESPHome / PlatformIO churning through build artifacts inside a
	// watched project tree. None of these writes touch a compose or env file,
	// so the sync callback must never fire.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(buildDir, "artifact.bin"), []byte{0x01, 0x02, 0x03}, 0o644))
	}

	select {
	case <-changeCh:
		t.Fatal("did not expect non-project file writes to trigger watcher callback")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestWatcher_StartIgnoresChmodOnNonProjectFiles(t *testing.T) {
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

	// Chmod on a non-project file is exactly the noise we're filtering.
	select {
	case <-changeCh:
		t.Fatal("did not expect chmod on a non-project file to trigger watcher callback")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestWatcher_StartFiresOnComposeFileDroppedIntoNewlyCreatedDirectory(t *testing.T) {
	root := t.TempDir()

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

	// A freshly-created subdirectory by itself must NOT trigger a sync.
	projectDir := filepath.Join(root, "fresh-project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	select {
	case <-changeCh:
		t.Fatal("did not expect plain directory create to trigger watcher callback")
	case <-time.After(500 * time.Millisecond):
	}

	// But dropping a compose file inside it must trigger — proving the new
	// directory was attached to the underlying fsnotify watcher even though
	// the create itself was not sync-worthy.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	select {
	case <-changeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected compose file inside newly-created directory to trigger watcher callback")
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

func TestWatcher_StartIgnoresBareNestedDirectoryCreateAndRemove(t *testing.T) {
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

	// Creating nested directories by themselves must not trigger a sync.
	// This is what prevents the ESPHome / PlatformIO churn from spinning the
	// sync loop — they mkdir thousands of build subdirectories during a
	// compile but never drop compose files in them.
	nestedDir := filepath.Join(root, "main-project", "sub-project1")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))

	select {
	case <-changeCh:
		t.Fatal("did not expect plain nested directory create to trigger watcher callback")
	case <-time.After(500 * time.Millisecond):
	}

	require.NoError(t, os.RemoveAll(filepath.Join(root, "main-project")))

	select {
	case <-changeCh:
		t.Fatal("did not expect plain nested directory removal to trigger watcher callback")
	case <-time.After(500 * time.Millisecond):
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
