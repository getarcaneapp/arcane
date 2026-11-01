package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"kit.getarcane.app/tripwire"

	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

var errWatcherStopped = errors.New("filesystem watcher stopped")

type FilesystemWatcherJob struct {
	projectService   *project.ProjectService
	templateService  *template.TemplateService
	settingsService  *settings.SettingsService
	lifecycleCtx     context.Context
	projectsWatcher  *tripwire.Watcher[string]
	templatesWatcher *tripwire.Watcher[struct{}]
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
	j.projectsWatcher, j.templatesWatcher = nil, nil
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

func (j *FilesystemWatcherJob) RestartProjectsWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting projects filesystem watcher")
	if err := ctx.Err(); err != nil {
		return err
	}
	j.restartMu.Lock()
	defer j.restartMu.Unlock()

	localSettings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, localSettings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		slog.WarnContext(ctx,
			"Projects filesystem watcher is monitoring directories recursively; very deep trees may require increasing fs.inotify.max_user_watches",
			"path", projectsDirectory,
			"sysctl", "fs.inotify.max_user_watches")
	}

	watcher, err := tripwire.New(projectsDirectory, tripwire.Config[string]{
		Key: func(e tripwire.Event) (string, bool) {
			return e.Logical, projects.IsProjectFile(filepath.Base(e.Path)) && !projects.PathContainsIgnoredDirectory(e.Rel)
		},
		OnBatch: func(ctx context.Context, batch tripwire.Batch[string]) {
			j.mu.Lock()
			defer j.mu.Unlock()
			if j.stopped || ctx.Err() != nil {
				return
			}
			slog.InfoContext(ctx, "Filesystem change detected, syncing projects")
			if syncErr := j.projectService.SyncProjectsFromFileSystem(ctx); syncErr != nil {
				slog.ErrorContext(ctx, "Failed to sync projects after filesystem change", "error", syncErr)
			} else {
				slog.InfoContext(ctx, "Project sync completed after filesystem change")
			}
			j.projectService.HandleProjectFilesChanged(ctx, batch.Keys)
		},
		SkipDir:        projects.IsIgnoredProjectDirName,
		Debounce:       500 * time.Millisecond,
		MaxDepth:       localSettings.ProjectScanMaxDepth.AsInt(),
		FollowSymlinks: localSettings.FollowProjectSymlinks.IsTrue(),
	})
	if err != nil {
		return err
	}
	if startErr := watcher.Start(j.lifecycleCtx); startErr != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return startErr
	}
	slog.InfoContext(ctx, "Projects filesystem watcher started", "path", projectsDirectory)

	return j.replaceWatcher(ctx, &j.projectsWatcher, watcher, func(syncCtx context.Context) {
		if syncErr := j.projectService.SyncProjectsFromFileSystem(syncCtx); syncErr != nil {
			slog.ErrorContext(syncCtx, "Initial project sync after watcher start failed", "error", syncErr)
		}
	})
}

func (j *FilesystemWatcherJob) RestartTemplatesWatcher(ctx context.Context) error {
	slog.InfoContext(ctx, "Restarting templates filesystem watcher")
	if err := ctx.Err(); err != nil {
		return err
	}
	j.restartMu.Lock()
	defer j.restartMu.Unlock()

	localSettings, err := j.settingsService.GetSettings(ctx)
	if err != nil {
		return err
	}
	projectsDirectory, err := projects.GetProjectsDirectory(ctx, localSettings.ProjectsDirectory.Value)
	if err != nil {
		return err
	}
	templatesDir, err := projects.GetTemplatesDirectory(ctx, localSettings.TemplatesDirectory.Value)
	if err != nil {
		return err
	}
	initialSync := func(syncCtx context.Context) {
		if syncErr := j.templateService.SyncLocalTemplatesFromFilesystem(syncCtx); syncErr != nil {
			slog.ErrorContext(syncCtx, "Initial template sync failed", "error", syncErr)
		}
	}

	// Watching overlapping trees would auto-import local templates as projects.
	if projects.IsSafeSubdirectory(projectsDirectory, templatesDir) || projects.IsSafeSubdirectory(templatesDir, projectsDirectory) {
		slog.ErrorContext(ctx,
			"Templates and projects directories overlap; templates watcher disabled",
			"projectsDirectory", projectsDirectory,
			"templatesDirectory", templatesDir)
		return j.replaceWatcher(ctx, &j.templatesWatcher, nil, initialSync)
	}

	watcher, err := tripwire.New(templatesDir, tripwire.Config[struct{}]{
		Key: func(e tripwire.Event) (struct{}, bool) {
			return struct{}{}, projects.IsProjectFile(filepath.Base(e.Path)) && !projects.PathContainsIgnoredDirectory(e.Rel)
		},
		OnBatch: func(ctx context.Context, _ tripwire.Batch[struct{}]) {
			j.mu.Lock()
			defer j.mu.Unlock()
			if j.stopped || ctx.Err() != nil {
				return
			}
			if syncErr := j.templateService.SyncLocalTemplatesFromFilesystem(ctx); syncErr != nil {
				slog.ErrorContext(ctx, "Failed to sync templates after filesystem change", "error", syncErr)
			}
		},
		SkipDir:  projects.IsIgnoredProjectDirName,
		Debounce: 3 * time.Second,
		MaxDepth: 1,
	})
	if err != nil {
		return err
	}
	if startErr := watcher.Start(j.lifecycleCtx); startErr != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
		return startErr
	}
	slog.InfoContext(ctx, "Templates filesystem watcher started", "path", templatesDir)

	return j.replaceWatcher(ctx, &j.templatesWatcher, watcher, initialSync)
}

func (j *FilesystemWatcherJob) replaceWatcher[K comparable](ctx context.Context, slot **tripwire.Watcher[K], watcher *tripwire.Watcher[K], initialSync func(context.Context)) error {
	j.mu.Lock()
	if j.stopped {
		j.mu.Unlock()
		if watcher == nil {
			return errWatcherStopped
		}
		return errors.Join(errWatcherStopped, watcher.Stop())
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
