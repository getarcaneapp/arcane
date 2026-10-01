package scheduler

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/fswatch"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type FilesystemWatcherJob struct {
	projectService   *project.ProjectService
	templateService  *template.TemplateService
	settingsService  *settings.SettingsService
	projectScanDepth int
	lifecycleCtx     context.Context
	projectsWatcher  *fswatch.Watcher
	templatesWatcher *fswatch.Watcher
	mu               sync.Mutex
	stopped          bool
	retired          []*fswatch.Watcher
}

func NewFilesystemWatcherJob(
	ctx context.Context,
	projectService *project.ProjectService,
	templateService *template.TemplateService,
	settingsService *settings.SettingsService,
	projectScanDepth int,
) (*FilesystemWatcherJob, error) {
	return &FilesystemWatcherJob{
		projectService:   projectService,
		templateService:  templateService,
		settingsService:  settingsService,
		projectScanDepth: projectScanDepth,
		lifecycleCtx:     ctx,
	}, nil
}

func (j *FilesystemWatcherJob) Start(ctx context.Context) error {
	if err := j.RestartProjectsWatcher(ctx); err != nil {
		return err
	}
	if err := j.RestartTemplatesWatcher(ctx); err != nil {
		j.mu.Lock()
		watcher := j.projectsWatcher
		j.projectsWatcher = nil
		j.mu.Unlock()
		if watcher != nil {
			return errors.Combine(err, watcher.Stop())
		}
		return err
	}
	return nil
}

func (j *FilesystemWatcherJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	j.stopped = true
	projectsWatcher, templatesWatcher := j.projectsWatcher, j.templatesWatcher
	j.projectsWatcher = nil
	j.templatesWatcher = nil
	retired := j.retired
	j.retired = nil
	j.mu.Unlock()
	var err error
	if projectsWatcher != nil {
		err = projectsWatcher.Stop()
	}
	if templatesWatcher != nil {
		err = errors.Combine(err, templatesWatcher.Stop())
	}
	for _, watcher := range retired {
		err = errors.Combine(err, watcher.Stop())
	}
	return err
}

func (j *FilesystemWatcherJob) handleFilesystemChangeInternal(ctx context.Context) {
	slog.InfoContext(ctx, "Filesystem change detected, syncing projects")

	if err := j.projectService.SyncProjectsFromFileSystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to sync projects after filesystem change",
			"error", err)
	} else {
		slog.InfoContext(ctx, "Project sync completed after filesystem change")
	}
}

func (j *FilesystemWatcherJob) handleProjectFilePathsChangedInternal(ctx context.Context, paths []string) {
	if len(paths) == 0 || j.projectService == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped || ctx.Err() != nil {
		return
	}
	j.handleFilesystemChangeInternal(ctx)
	j.projectService.HandleProjectFilesChanged(ctx, paths)
}

func (j *FilesystemWatcherJob) handleTemplatesChangeInternal(ctx context.Context) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped || ctx.Err() != nil || j.templateService == nil {
		return
	}
	if err := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to sync templates after filesystem change", "error", err)
	}
}

func (j *FilesystemWatcherJob) RestartProjectsWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting projects filesystem watcher")
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped {
		return errors.New("filesystem watcher stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.projectsWatcher != nil {
		previous := j.projectsWatcher
		j.projectsWatcher = nil
		j.retired = append(j.retired, previous)
		// Callbacks need the domain mutex, so stop the watch loop separately from joining them.
		if err := previous.StopWatching(); err != nil {
			return err
		}
	}
	watcher, err := j.startProjectsWatcherInternal(ctx)
	if err != nil {
		if watcher != nil {
			_ = watcher.StopWatching()
		}
		return err
	}
	j.projectsWatcher = watcher
	return nil
}

func (j *FilesystemWatcherJob) startProjectsWatcherInternal(ctx context.Context) (*fswatch.Watcher, error) {
	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return nil, err
	}
	j.logRecursiveProjectsWatchLimitWarningInternal(ctx, projectsDirectory)

	watcher, err := fswatch.NewWatcher(projectsDirectory, j.projectWatcherOptionsInternal(settings.FollowProjectSymlinks.IsTrue()))
	if err != nil {
		return nil, err
	}
	if err := watcher.Start(j.lifecycleCtx); err != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return watcher, err
	}

	slog.InfoContext(ctx, "Projects filesystem watcher started", "path", projectsDirectory)
	if j.projectService != nil {
		if err := j.projectService.SyncProjectsFromFileSystem(ctx); err != nil {
			slog.ErrorContext(ctx, "Initial project sync after watcher start failed", "error", err)
		}
	}
	return watcher, nil
}

func (j *FilesystemWatcherJob) logRecursiveProjectsWatchLimitWarningInternal(ctx context.Context, projectsDirectory string) {
	if runtime.GOOS != "linux" {
		return
	}

	slog.WarnContext(ctx,
		"Projects filesystem watcher is monitoring directories recursively; very deep trees may require increasing fs.inotify.max_user_watches",
		"path", projectsDirectory,
		"sysctl", "fs.inotify.max_user_watches")
}

func (j *FilesystemWatcherJob) projectWatcherOptionsInternal(followProjectSymlinks bool) fswatch.WatcherOptions {
	return fswatch.WatcherOptions{
		Debounce:          500 * time.Millisecond,
		OnChangePaths:     j.handleProjectFilePathsChangedInternal,
		MaxDepth:          j.projectScanDepth,
		FollowSymlinkDirs: followProjectSymlinks,
	}
}

func (j *FilesystemWatcherJob) RestartTemplatesWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting templates filesystem watcher")
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped {
		return errors.New("filesystem watcher stopped")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.templatesWatcher != nil {
		previous := j.templatesWatcher
		j.templatesWatcher = nil
		j.retired = append(j.retired, previous)
		// Callbacks need the domain mutex, so stop the watch loop separately from joining them.
		if err := previous.StopWatching(); err != nil {
			return err
		}
	}
	watcher, err := j.startTemplatesWatcherInternal(ctx)
	if err != nil {
		if watcher != nil {
			_ = watcher.StopWatching()
		}
		return err
	}
	j.templatesWatcher = watcher
	return nil
}

func (j *FilesystemWatcherJob) startTemplatesWatcherInternal(ctx context.Context) (*fswatch.Watcher, error) {
	if j.templateService == nil {
		return nil, nil
	}

	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return nil, err
	}
	templatesDir, err := projects.GetTemplatesDirectory(ctx, settings.TemplatesDirectory.Value)
	if err != nil {
		return nil, err
	}

	if err := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial template sync failed", "error", err)
	}

	if directoriesOverlapInternal(projectsDirectory, templatesDir) {
		slog.ErrorContext(ctx,
			"Templates and projects directories overlap; templates watcher disabled",
			"projectsDirectory", projectsDirectory,
			"templatesDirectory", templatesDir)
		return nil, nil
	}

	watcher, err := fswatch.NewWatcher(templatesDir, fswatch.WatcherOptions{
		Debounce: 3 * time.Second,
		OnChange: j.handleTemplatesChangeInternal,
		MaxDepth: 1,
	})
	if err != nil {
		return nil, err
	}
	if err := watcher.Start(j.lifecycleCtx); err != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return watcher, err
	}

	slog.InfoContext(ctx, "Templates filesystem watcher started", "path", templatesDir)

	return watcher, nil
}

// directoriesOverlapInternal returns true when a or b is the same as or contained in the
// other. Used to refuse running both watchers against the same tree, which would
// cause local templates to be auto-imported as projects.
func directoriesOverlapInternal(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return projects.IsSafeSubdirectory(a, b) || projects.IsSafeSubdirectory(b, a)
}
