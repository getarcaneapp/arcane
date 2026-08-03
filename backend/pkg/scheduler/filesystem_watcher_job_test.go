package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/fswatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestFilesystemWatcherJob_ProjectWatcherOptions_UsesConfiguredMaxDepth(t *testing.T) {
	job := &FilesystemWatcherJob{
		projectScanDepth: 1,
	}

	opts := job.projectWatcherOptionsInternal(true)

	assert.Equal(t, 1, opts.MaxDepth)
	assert.True(t, opts.FollowSymlinkDirs)
}

func TestFilesystemWatcherJob_ConcurrentProjectRestartsAreSerializedInternal(t *testing.T) {
	_, settingsService, _ := setupAnalyticsStateServicesInternal(t)
	require.NoError(t, settingsService.SetStringSetting(t.Context(), "projectsDirectory", t.TempDir()))
	require.NoError(t, settingsService.SetStringSetting(t.Context(), "templatesDirectory", t.TempDir()))

	lifecycle := fxtest.NewLifecycle(t)
	actorRuntime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	job, err := NewFilesystemWatcherJob(t.Context(), actorRuntime, nil, nil, settingsService, 2)
	require.NoError(t, err)

	restartErrors := make(chan error, 2)
	var callers sync.WaitGroup
	callers.Add(2)
	for range 2 {
		go func() {
			defer callers.Done()
			restartErrors <- job.RestartProjectsWatcher(t.Context())
		}()
	}
	callers.Wait()
	close(restartErrors)
	for restartErr := range restartErrors {
		require.NoError(t, restartErr)
	}
	var earlyWatcher *fswatch.Watcher
	require.NoError(t, job.projectsWatcher.Do(t.Context(), "capture early watcher", func(_ context.Context, watcher *fswatch.Watcher) error {
		earlyWatcher = watcher
		return nil
	}))
	require.NotNil(t, earlyWatcher)
	require.NoError(t, job.Start(t.Context()))
	var startedWatcher *fswatch.Watcher
	require.NoError(t, job.projectsWatcher.Do(t.Context(), "capture started watcher", func(_ context.Context, watcher *fswatch.Watcher) error {
		startedWatcher = watcher
		return nil
	}))
	require.NotSame(t, earlyWatcher, startedWatcher)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, job.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
