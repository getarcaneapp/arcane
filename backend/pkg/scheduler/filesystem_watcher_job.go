package scheduler

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/fswatch"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type FilesystemWatcherJob struct {
	projectService   *services.ProjectService
	templateService  *services.TemplateService
	settingsService  *services.SettingsService
	projectScanDepth int
	projectsWatcher  *fswatch.Watcher
	templatesWatcher *fswatch.Watcher
	mu               sync.Mutex
}

func NewFilesystemWatcherJob(
	projectService *services.ProjectService,
	templateService *services.TemplateService,
	settingsService *services.SettingsService,
	projectScanDepth int,
) *FilesystemWatcherJob {
	return &FilesystemWatcherJob{
		projectService:   projectService,
		templateService:  templateService,
		settingsService:  settingsService,
		projectScanDepth: projectScanDepth,
	}
}

func RegisterFilesystemWatcherJob(ctx context.Context, projectService *services.ProjectService, templateService *services.TemplateService, settingsService *services.SettingsService, projectScanDepth int) (*FilesystemWatcherJob, error) {
	job := NewFilesystemWatcherJob(projectService, templateService, settingsService, projectScanDepth)

	go func() {
		if err := job.Start(ctx); err != nil {
			slog.ErrorContext(ctx, "Filesystem watcher failed", "error", err)
		}
	}()

	slog.InfoContext(ctx, "Filesystem watcher job registered")
	return job, nil
}

func (j *FilesystemWatcherJob) Start(ctx context.Context) error {
	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}
	followProjectSymlinks := settings.FollowProjectSymlinks.IsTrue()
	j.logRecursiveProjectsWatchLimitWarningInternal(ctx, projectsDirectory)

	sw, err := fswatch.NewWatcher(projectsDirectory, j.projectWatcherOptionsInternal(followProjectSymlinks))
	if err != nil {
		return err
	}

	j.projectsWatcher = sw

	templatesDir, err := projects.GetTemplatesDirectory(ctx, settings.TemplatesDirectory.Value)
	if err != nil {
		return err
	}

	if j.templateService != nil {
		if directoriesOverlapInternal(projectsDirectory, templatesDir) {
			slog.ErrorContext(ctx,
				"Templates and projects directories overlap; templates watcher disabled to prevent compose files being treated as projects",
				"projectsDirectory", projectsDirectory,
				"templatesDirectory", templatesDir)
		} else {
			tw, err := fswatch.NewWatcher(templatesDir, fswatch.WatcherOptions{
				Debounce: 3 * time.Second,
				OnChange: j.handleTemplatesChange,
				MaxDepth: 1,
			})
			if err != nil {
				return err
			}
			j.templatesWatcher = tw
		}
	}

	if err := j.projectsWatcher.Start(ctx); err != nil {
		return err
	}
	if j.templatesWatcher != nil {
		if err := j.templatesWatcher.Start(ctx); err != nil {
			if stopErr := j.projectsWatcher.Stop(); stopErr != nil {
				slog.ErrorContext(ctx, "Failed to stop projects watcher after templates watcher start error", "error", stopErr)
			}
			return err
		}
	}

	slog.InfoContext(ctx, "Filesystem watcher started for projects directory",
		"path", projectsDirectory)
	if j.templatesWatcher != nil {
		slog.InfoContext(ctx, "Filesystem watcher started for templates directory",
			"path", templatesDir)
	}

	// Initial sync to surface pre-existing resources
	if err := j.projectService.SyncProjectsFromFileSystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial project sync failed", "error", err)
	}
	if j.templateService != nil {
		if err := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
			slog.ErrorContext(ctx, "Initial template sync failed", "error", err)
		}
	}

	<-ctx.Done()

	return j.Stop()
}

func (j *FilesystemWatcherJob) Stop() error {
	j.mu.Lock()
	projectsWatcher := j.projectsWatcher
	templatesWatcher := j.templatesWatcher
	j.projectsWatcher = nil
	j.templatesWatcher = nil
	j.mu.Unlock()

	var firstErr error
	if projectsWatcher != nil {
		if err := projectsWatcher.Stop(); err != nil {
			firstErr = err
		}
	}
	if templatesWatcher != nil {
		if err := templatesWatcher.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (j *FilesystemWatcherJob) handleFilesystemChange(ctx context.Context) {
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
	j.handleFilesystemChange(ctx)
	j.projectService.HandleProjectFilesChanged(ctx, paths)
}

func (j *FilesystemWatcherJob) handleTemplatesChange(ctx context.Context) {
	slog.InfoContext(ctx, "Template directory change detected, syncing templates")
	if j.templateService == nil {
		return
	}
	if err := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to sync templates after filesystem change", "error", err)
	} else {
		slog.InfoContext(ctx, "Template sync completed after filesystem change")
	}
}

func (j *FilesystemWatcherJob) RestartProjectsWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting projects filesystem watcher")

	j.mu.Lock()
	oldProjectsWatcher := j.projectsWatcher
	j.projectsWatcher = nil
	j.mu.Unlock()

	if oldProjectsWatcher != nil {
		if err := oldProjectsWatcher.Stop(); err != nil {
			slog.WarnContext(ctx, "Failed to stop projects watcher during restart", "error", err)
		}
	}

	// Get fresh settings to get the new projects directory
	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}
	followProjectSymlinks := settings.FollowProjectSymlinks.IsTrue()
	j.logRecursiveProjectsWatchLimitWarningInternal(ctx, projectsDirectory)

	// Create a new watcher with the updated path
	sw, err := fswatch.NewWatcher(projectsDirectory, j.projectWatcherOptionsInternal(followProjectSymlinks))
	if err != nil {
		return err
	}

	// Start the new watcher
	if err := sw.Start(ctx); err != nil {
		return err
	}

	j.mu.Lock()
	j.projectsWatcher = sw
	j.mu.Unlock()

	slog.InfoContext(ctx, "Projects filesystem watcher restarted", "path", projectsDirectory)

	// Perform a sync to ensure we have the latest state from the new directory
	if err := j.projectService.SyncProjectsFromFileSystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial project sync after watcher restart failed", "error", err)
	}

	return nil
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
	if j.templateService == nil {
		return nil
	}
	slog.InfoContext(ctx, "Restarting templates filesystem watcher")

	j.mu.Lock()
	oldTemplatesWatcher := j.templatesWatcher
	j.templatesWatcher = nil
	j.mu.Unlock()

	if oldTemplatesWatcher != nil {
		if err := oldTemplatesWatcher.Stop(); err != nil {
			slog.WarnContext(ctx, "Failed to stop templates watcher during restart", "error", err)
		}
	}

	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}
	templatesDir, err := projects.GetTemplatesDirectory(ctx, settings.TemplatesDirectory.Value)
	if err != nil {
		return err
	}

	if directoriesOverlapInternal(projectsDirectory, templatesDir) {
		slog.ErrorContext(ctx,
			"Templates and projects directories overlap; templates watcher not restarted",
			"projectsDirectory", projectsDirectory,
			"templatesDirectory", templatesDir)
		return nil
	}

	tw, err := fswatch.NewWatcher(templatesDir, fswatch.WatcherOptions{
		Debounce: 3 * time.Second,
		OnChange: j.handleTemplatesChange,
		MaxDepth: 1,
	})
	if err != nil {
		return err
	}
	if err := tw.Start(ctx); err != nil {
		return err
	}

	j.mu.Lock()
	j.templatesWatcher = tw
	j.mu.Unlock()

	slog.InfoContext(ctx, "Templates filesystem watcher restarted", "path", templatesDir)

	if err := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial template sync after watcher restart failed", "error", err)
	}

	return nil
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
