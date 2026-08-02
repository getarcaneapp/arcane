package scheduler

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/fswatch"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type FilesystemWatcherJob struct {
	projectService   *services.ProjectService
	templateService  *services.TemplateService
	settingsService  *services.SettingsService
	projectScanDepth int
	lifecycleCtx     context.Context
	projectsWatcher  *actors.Resource[*fswatch.Watcher]
	templatesWatcher *actors.Resource[*fswatch.Watcher]
}

func NewFilesystemWatcherJob(
	ctx context.Context,
	actorRuntime *actors.Runtime,
	projectService *services.ProjectService,
	templateService *services.TemplateService,
	settingsService *services.SettingsService,
	projectScanDepth int,
) (*FilesystemWatcherJob, error) {
	projectsWatcher, err := actors.NewResource(ctx, actorRuntime, "filesystem-watcher", "projects", 3, (*fswatch.Watcher).Stop)
	if err != nil {
		return nil, err
	}
	templatesWatcher, err := actors.NewResource(ctx, actorRuntime, "filesystem-watcher", "templates", 3, (*fswatch.Watcher).Stop)
	if err != nil {
		return nil, errors.Combine(err, projectsWatcher.Stop(ctx))
	}
	return &FilesystemWatcherJob{
		projectService:   projectService,
		templateService:  templateService,
		settingsService:  settingsService,
		projectScanDepth: projectScanDepth,
		lifecycleCtx:     ctx,
		projectsWatcher:  projectsWatcher,
		templatesWatcher: templatesWatcher,
	}, nil
}

func (j *FilesystemWatcherJob) Start(ctx context.Context) error {
	if err := j.projectsWatcher.Restart(ctx, "start projects filesystem watcher", j.startProjectsWatcherInternal); err != nil {
		return err
	}
	if err := j.templatesWatcher.Restart(ctx, "start templates filesystem watcher", j.startTemplatesWatcherInternal); err != nil {
		return errors.Combine(err, j.projectsWatcher.Clear(ctx, "clear projects watcher after template startup failure"))
	}
	return nil
}

func (j *FilesystemWatcherJob) Stop(ctx context.Context) error {
	return errors.Combine(j.projectsWatcher.Stop(ctx), j.templatesWatcher.Stop(ctx))
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
	err := j.projectsWatcher.Do(ctx, "sync changed project files", func(workCtx context.Context, _ *fswatch.Watcher) error {
		j.handleFilesystemChangeInternal(workCtx)
		j.projectService.HandleProjectFilesChanged(workCtx, paths)
		return nil
	})
	if err != nil && ctx.Err() == nil && !errors.Is(err, actors.ErrResourceStopped) {
		slog.ErrorContext(ctx, "Failed to dispatch changed project files", "error", err)
	}
}

func (j *FilesystemWatcherJob) handleTemplatesChangeInternal(ctx context.Context) {
	err := j.templatesWatcher.Do(ctx, "sync changed templates", func(workCtx context.Context, _ *fswatch.Watcher) error {
		slog.InfoContext(workCtx, "Template directory change detected, syncing templates")
		if j.templateService == nil {
			return nil
		}
		if syncErr := j.templateService.SyncLocalTemplatesFromFilesystem(workCtx); syncErr != nil {
			slog.ErrorContext(workCtx, "Failed to sync templates after filesystem change", "error", syncErr)
		} else {
			slog.InfoContext(workCtx, "Template sync completed after filesystem change")
		}
		return nil
	})
	if err != nil && ctx.Err() == nil && !errors.Is(err, actors.ErrResourceStopped) {
		slog.ErrorContext(ctx, "Failed to dispatch template filesystem change", "error", err)
	}
}

func (j *FilesystemWatcherJob) RestartProjectsWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting projects filesystem watcher")
	return j.projectsWatcher.Restart(ctx, "restart projects filesystem watcher", j.startProjectsWatcherInternal)
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
	return j.templatesWatcher.Restart(ctx, "restart templates filesystem watcher", j.startTemplatesWatcherInternal)
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
