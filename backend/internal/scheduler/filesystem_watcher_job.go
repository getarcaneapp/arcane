package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"kit.getarcane.app/acfs"
	"kit.getarcane.app/acfs/types"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/tripwire"

	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/pkg/projects"
)

type (
	FilesystemWatcherJob struct {
		projectService   *project.ProjectService
		templateService  *template.TemplateService
		settingsService  *settings.SettingsService
		lifecycleCtx     context.Context
		restart          chan struct{}
		projectsWatcher  []*tripwire.Watcher[string]
		templatesWatcher []*tripwire.Watcher[struct{}]
		restartMu        sync.Mutex
		mu               sync.Mutex
		stopped          bool
	}
)

var errWatcherStopped = errors.New("filesystem watcher stopped")

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
	attempt := 0
	for ctx.Err() == nil {
		if startErr := j.Start(ctx); startErr != nil {
			slog.WarnContext(ctx, "Filesystem watcher startup failed; retrying", "error", startErr)
			if (concurrency.Backoff{Base: 5 * time.Second, Max: 5 * time.Minute}).Sleep(ctx, attempt) != nil {
				return nil
			}
			attempt++
			continue
		}
		attempt = 0
		select {
		case <-ctx.Done():
			return nil
		case <-j.restart:
		}
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
	return errors.Join(stopWatchers(projectsWatcher), stopWatchers(templatesWatcher))
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
	roots, err := j.projectService.EnabledProjectsRoots(ctx)
	if err != nil {
		return err
	}

	watchers := make([]*tripwire.Watcher[string], 0, len(roots))
	for i, root := range roots {
		if _, statErr := (acfs.Host{}).Stat(ctx, root, types.StatOptions{FollowFinal: true}); i > 0 && statErr != nil {
			slog.WarnContext(ctx, "Skipping projects filesystem watcher for unavailable projects directory", "path", root, "error", statErr)
			continue
		}
		if runtime.GOOS == "linux" {
			slog.WarnContext(ctx,
				"Projects filesystem watcher is monitoring directories recursively; very deep trees may require increasing fs.inotify.max_user_watches",
				"path", root,
				"sysctl", "fs.inotify.max_user_watches")
		}

		watcher, newErr := tripwire.New(root, tripwire.Config[string]{
			Key: func(e tripwire.Event) (string, bool) {
				return e.Logical, projects.IsProjectFile(filepath.Base(e.Path)) && !projects.PathHasSegment(e.Rel, projects.IsIgnoredProjectDirName)
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
		if newErr != nil {
			return errors.Join(newErr, stopWatchers(watchers))
		}
		if startErr := watcher.Start(j.lifecycleCtx); startErr != nil { //nolint:contextcheck // watcher lifetime belongs to the application, not this replacement task.
			return errors.Join(startErr, stopWatchers(watchers))
		}
		slog.InfoContext(ctx, "Projects filesystem watcher started", "path", root)
		watchers = append(watchers, watcher)
	}

	return j.replaceWatcher(ctx, &j.projectsWatcher, watchers, func(syncCtx context.Context) {
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
	roots, err := j.projectService.EnabledProjectsRoots(ctx)
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
	for _, root := range roots {
		if projects.IsSafeSubdirectory(root, templatesDir) || projects.IsSafeSubdirectory(templatesDir, root) {
			slog.ErrorContext(ctx,
				"Templates and projects directories overlap; templates watcher disabled",
				"projectsDirectory", root,
				"templatesDirectory", templatesDir)
			return j.replaceWatcher(ctx, &j.templatesWatcher, nil, initialSync)
		}
	}

	watcher, err := tripwire.New(templatesDir, tripwire.Config[struct{}]{
		Key: func(e tripwire.Event) (struct{}, bool) {
			return struct{}{}, projects.IsProjectFile(filepath.Base(e.Path)) && !projects.PathHasSegment(e.Rel, projects.IsIgnoredProjectDirName)
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

	return j.replaceWatcher(ctx, &j.templatesWatcher, []*tripwire.Watcher[struct{}]{watcher}, initialSync)
}

func (j *FilesystemWatcherJob) replaceWatcher[K comparable](ctx context.Context, slot *[]*tripwire.Watcher[K], watchers []*tripwire.Watcher[K], initialSync func(context.Context)) error {
	j.mu.Lock()
	if j.stopped {
		j.mu.Unlock()
		if len(watchers) == 0 {
			return errWatcherStopped
		}
		return errors.Join(errWatcherStopped, stopWatchers(watchers))
	}
	previous := *slot
	*slot = watchers
	j.mu.Unlock()

	err := stopWatchers(previous)

	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.stopped && ctx.Err() == nil {
		initialSync(ctx)
	}
	return err
}

func stopWatchers[K comparable](watchers []*tripwire.Watcher[K]) error {
	var err error
	for _, watcher := range watchers {
		err = errors.Join(err, watcher.Stop())
	}
	return err
}
