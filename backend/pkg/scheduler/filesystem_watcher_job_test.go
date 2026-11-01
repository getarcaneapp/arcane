package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/fswatch"
)

func TestFilesystemWatcherJob_ProjectWatcherOptions_UsesConfiguredMaxDepth(t *testing.T) {
	job := &FilesystemWatcherJob{}

	opts := job.projectWatcherOptions(true, 1)

	assert.Equal(t, 1, opts.MaxDepth)
	assert.True(t, opts.FollowSymlinkDirs)
}

func TestFilesystemWatcherJob_ConcurrentProjectRestartsAreSerialized(t *testing.T) {
	job := newFilesystemWatcherTestJob(t)

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

	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, job.Stop(stopCtx))
}

func TestFilesystemWatcherJob_RequestRestartRearmsSupervisor(t *testing.T) {
	job := newFilesystemWatcherTestJob(t)

	superviseCtx, cancelSupervise := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- job.Supervise(superviseCtx) }()

	currentWatcher := func() *fswatch.Watcher {
		job.mu.Lock()
		defer job.mu.Unlock()
		return job.projectsWatcher
	}
	require.Eventually(t, func() bool { return currentWatcher() != nil }, 5*time.Second, 10*time.Millisecond)
	firstWatcher := currentWatcher()

	job.RequestRestart()
	require.Eventually(t, func() bool { return currentWatcher() != firstWatcher }, 5*time.Second, 10*time.Millisecond)

	cancelSupervise()
	require.NoError(t, <-done)
	stopCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, job.Stop(stopCtx))
}

func newFilesystemWatcherTestJob(t *testing.T) *FilesystemWatcherJob {
	t.Helper()

	db, settingsService, kvService := setupAnalyticsStateServices(t)
	require.NoError(t, db.AutoMigrate(&project.Project{}, &project.ProjectTag{}, &template.TemplateRegistry{}, &template.ComposeTemplate{}))
	require.NoError(t, settingsService.SetStringSetting(t.Context(), "projectsDirectory", t.TempDir()))
	require.NoError(t, settingsService.SetStringSetting(t.Context(), "templatesDirectory", t.TempDir()))

	job, err := NewFilesystemWatcherJob(
		t.Context(),
		project.NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, &config.Config{}, kvService, nil),
		template.NewTemplateService(t.Context(), db, nil, settingsService),
		settingsService,
	)
	require.NoError(t, err)
	return job
}
