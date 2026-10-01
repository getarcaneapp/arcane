package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	job, err := NewFilesystemWatcherJob(t.Context(), nil, nil, settingsService, 2)
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
	job.mu.Lock()
	earlyWatcher := job.projectsWatcher
	job.mu.Unlock()
	require.NotNil(t, earlyWatcher)
	require.NoError(t, job.Start(t.Context()))
	job.mu.Lock()
	startedWatcher := job.projectsWatcher
	job.mu.Unlock()
	require.NotSame(t, earlyWatcher, startedWatcher)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, job.Stop(stopCtx))
}
