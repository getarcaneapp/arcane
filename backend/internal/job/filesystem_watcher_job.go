package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/fs"
)

type FilesystemWatcherJob struct {
	projectService   *services.ProjectService
	templateService  *services.TemplateService
	settingsService  *services.SettingsService
	projectsWatcher  *fs.Watcher
	templatesWatcher *fs.Watcher
}

func NewFilesystemWatcherJob(
	projectService *services.ProjectService,
	templateService *services.TemplateService,
	settingsService *services.SettingsService,
) *FilesystemWatcherJob {
	return &FilesystemWatcherJob{
		projectService:  projectService,
		templateService: templateService,
		settingsService: settingsService,
	}
}

func RegisterFilesystemWatcherJob(ctx context.Context, scheduler *Scheduler, projectService *services.ProjectService, templateService *services.TemplateService, settingsService *services.SettingsService) (*FilesystemWatcherJob, error) {
	job := NewFilesystemWatcherJob(projectService, templateService, settingsService)

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
	projectsDirectory, err := fs.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}

	sw, err := fs.NewWatcher(projectsDirectory, fs.WatcherOptions{
		Debounce: 3 * time.Second, // Wait 3 seconds after last change before syncing
		OnChange: j.handleFilesystemChange,
		MaxDepth: 1,
	})
	if err != nil {
		return err
	}

	j.projectsWatcher = sw

	templatesDir, err := fs.GetTemplatesDirectory(ctx)
	if err != nil {
		return err
	}

	if j.templateService != nil {
		tw, err := fs.NewWatcher(templatesDir, fs.WatcherOptions{
			Debounce: 3 * time.Second,
			OnChange: j.handleTemplatesChange,
			MaxDepth: 1,
		})
		if err != nil {
			return err
		}
		j.templatesWatcher = tw
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
	var firstErr error
	if j.projectsWatcher != nil {
		if err := j.projectsWatcher.Stop(); err != nil {
			firstErr = err
		}
	}
	if j.templatesWatcher != nil {
		if err := j.templatesWatcher.Stop(); err != nil && firstErr == nil {
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

	// Stop the existing projects watcher if it exists
	if j.projectsWatcher != nil {
		if err := j.projectsWatcher.Stop(); err != nil {
			slog.WarnContext(ctx, "Failed to stop projects watcher during restart", "error", err)
		}
	}

	// Get fresh settings to get the new projects directory
	settings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := fs.GetProjectsDirectory(ctx, settings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}

	// Create a new watcher with the updated path
	sw, err := fs.NewWatcher(projectsDirectory, fs.WatcherOptions{
		Debounce: 3 * time.Second,
		OnChange: j.handleFilesystemChange,
		MaxDepth: 1,
	})
	if err != nil {
		return err
	}

	j.projectsWatcher = sw

	// Start the new watcher
	if err := j.projectsWatcher.Start(ctx); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Projects filesystem watcher restarted", "path", projectsDirectory)

	// Perform a sync to ensure we have the latest state from the new directory
	if err := j.projectService.SyncProjectsFromFileSystem(ctx); err != nil {
		slog.ErrorContext(ctx, "Initial project sync after watcher restart failed", "error", err)
	}

	return nil
}
