package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/fswatch"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

type FilesystemWatcherJob struct {
	projectService   *project.ProjectService
	templateService  *template.TemplateService
	settingsService  *settings.SettingsService
	lifecycleCtx     context.Context
	projectsWatcher  *fswatch.Watcher
	templatesWatcher *fswatch.Watcher
	restart          chan struct{}
	restartMu        sync.Mutex
	mu               sync.Mutex
	stopped          bool
}

func NewFilesystemWatcherJob(
	ctx context.Context,
	projectService *project.ProjectService,
	templateService *template.TemplateService,
	settingsService *settings.SettingsService,
) (*FilesystemWatcherJob, error) {
	return &FilesystemWatcherJob{
		projectService:  projectService,
		templateService: templateService,
		settingsService: settingsService,
		lifecycleCtx:    ctx,
		restart:         make(chan struct{}, 1),
	}, nil
}

func (j *FilesystemWatcherJob) Start(ctx context.Context) error {
	if err := j.RestartProjectsWatcher(ctx); err != nil {
		return err
	}
	return j.RestartTemplatesWatcher(ctx)
}

func (j *FilesystemWatcherJob) Supervise(ctx context.Context) error {
	delay := 5 * time.Second
	for ctx.Err() == nil {
		startErr := j.Start(ctx)
		if startErr == nil {
			delay = 5 * time.Second
			select {
			case <-ctx.Done():
				return nil
			case <-j.restart:
				continue
			}
		}
		slog.WarnContext(ctx, "Filesystem watcher startup failed; retrying", "error", startErr)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(delay*2, 5*time.Minute)
	}
	return nil
}

func (j *FilesystemWatcherJob) RequestRestart() {
	select {
	case j.restart <- struct{}{}:
	default:
	}
}

func (j *FilesystemWatcherJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	j.stopped = true
	projectsWatcher, templatesWatcher := j.projectsWatcher, j.templatesWatcher
	j.projectsWatcher = nil
	j.templatesWatcher = nil
	j.mu.Unlock()
	var err error
	if projectsWatcher != nil {
		err = projectsWatcher.Stop()
	}
	if templatesWatcher != nil {
		err = errors.Join(err, templatesWatcher.Stop())
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
	if err := ctx.Err(); err != nil {
		return err
	}
	j.restartMu.Lock()
	defer j.restartMu.Unlock()
	watcher, err := j.startProjectsWatcher(ctx)
	if err != nil {
		return err
	}
	return j.replaceWatcher(ctx, &j.projectsWatcher, watcher, func(syncCtx context.Context) {
		if j.projectService == nil {
			return
		}
		if syncErr := j.projectService.SyncProjectsFromFileSystem(syncCtx); syncErr != nil {
			slog.ErrorContext(syncCtx, "Initial project sync after watcher start failed", "error", syncErr)
		}
	})
}

func (j *FilesystemWatcherJob) replaceWatcher(ctx context.Context, slot **fswatch.Watcher, watcher *fswatch.Watcher, initialSync func(context.Context)) error {
	j.mu.Lock()
	if j.stopped {
		j.mu.Unlock()
		if watcher == nil {
			return errors.New("filesystem watcher stopped")
		}
		return errors.Join(errors.New("filesystem watcher stopped"), watcher.Stop())
	}
	previous := *slot
	*slot = watcher
	j.mu.Unlock()

	var err error
	if previous != nil {
		err = previous.Stop()
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.stopped && ctx.Err() == nil {
		initialSync(ctx)
	}
	return err
}

func (j *FilesystemWatcherJob) startProjectsWatcher(ctx context.Context) (*fswatch.Watcher, error) {
	localSettings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, localSettings.ProjectsDirectory.Value)
	if err != nil {
		return nil, err
	}
	j.logRecursiveProjectsWatchLimitWarningInternal(ctx, projectsDirectory)

	watcher, err := fswatch.NewWatcher(projectsDirectory, j.projectWatcherOptions(localSettings.FollowProjectSymlinks.IsTrue(), localSettings.ProjectScanMaxDepth.AsInt()))
	if err != nil {
		return nil, err
	}
	if startErr := watcher.Start(j.lifecycleCtx); startErr != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return nil, startErr
	}

	slog.InfoContext(ctx, "Projects filesystem watcher started", "path", projectsDirectory)
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

func (j *FilesystemWatcherJob) projectWatcherOptions(followProjectSymlinks bool, maxDepth int) fswatch.WatcherOptions {
	return fswatch.WatcherOptions{
		Debounce:          500 * time.Millisecond,
		OnChangePaths:     j.handleProjectFilePathsChangedInternal,
		MaxDepth:          maxDepth,
		FollowSymlinkDirs: followProjectSymlinks,
	}
}

func (j *FilesystemWatcherJob) RestartTemplatesWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting templates filesystem watcher")
	if err := ctx.Err(); err != nil {
		return err
	}
	j.restartMu.Lock()
	defer j.restartMu.Unlock()
	watcher, err := j.startTemplatesWatcher(ctx)
	if err != nil {
		return err
	}
	return j.replaceWatcher(ctx, &j.templatesWatcher, watcher, func(syncCtx context.Context) {
		if j.templateService == nil {
			return
		}
		if syncErr := j.templateService.SyncLocalTemplatesFromFilesystem(syncCtx); syncErr != nil {
			slog.ErrorContext(syncCtx, "Initial template sync failed", "error", syncErr)
		}
	})
}

func (j *FilesystemWatcherJob) startTemplatesWatcher(ctx context.Context) (*fswatch.Watcher, error) {
	if j.templateService == nil {
		return nil, nil
	}

	localSettings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, localSettings.ProjectsDirectory.Value)
	if err != nil {
		return nil, err
	}
	templatesDir, err := projects.GetTemplatesDirectory(ctx, localSettings.TemplatesDirectory.Value)
	if err != nil {
		return nil, err
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
	if startErr := watcher.Start(j.lifecycleCtx); startErr != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return nil, startErr
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
