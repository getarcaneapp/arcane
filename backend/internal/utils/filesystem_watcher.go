package utils

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type FilesystemWatcher struct {
	watcher     *fsnotify.Watcher
	watchedPath string
	maxDepth    int
	onChange    func(ctx context.Context)
	debounce    time.Duration
	stopCh      chan struct{}
	stoppedCh   chan struct{}
}

type WatcherOptions struct {
	Debounce time.Duration
	OnChange func(ctx context.Context)
	MaxDepth int
}

var watchableFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
	".env",
	".env.global",
}

func NewFilesystemWatcher(watchPath string, opts WatcherOptions) (*FilesystemWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if opts.Debounce == 0 {
		opts.Debounce = 2 * time.Second
	}

	if opts.MaxDepth < 0 {
		opts.MaxDepth = 0
	}

	return &FilesystemWatcher{
		watcher:     watcher,
		watchedPath: filepath.Clean(watchPath),
		maxDepth:    opts.MaxDepth,
		onChange:    opts.OnChange,
		debounce:    opts.Debounce,
		stopCh:      make(chan struct{}),
		stoppedCh:   make(chan struct{}),
	}, nil
}

func (fw *FilesystemWatcher) Start(ctx context.Context) error {
	if err := fw.watcher.Add(fw.watchedPath); err != nil {
		return err
	}

	if err := fw.addExistingDirectories(fw.watchedPath); err != nil {
		slog.WarnContext(ctx, "Failed to add some existing directories to watcher", "path", fw.watchedPath, "error", err)
	}

	go fw.watchLoop(ctx)

	slog.InfoContext(ctx, "Filesystem watcher started", "path", fw.watchedPath)
	return nil
}

func (fw *FilesystemWatcher) Stop() error {
	close(fw.stopCh)
	<-fw.stoppedCh // Wait for watchLoop to finish
	return fw.watcher.Close()
}

func (fw *FilesystemWatcher) watchLoop(ctx context.Context) {
	defer close(fw.stoppedCh)

	var timer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.stopCh:
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			if fw.shouldHandleEvent(event) {
				// Handle directory creation
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if fw.shouldWatchDir(event.Name) {
							if err := fw.watcher.Add(event.Name); err != nil {
								slog.ErrorContext(ctx, "Failed to add new directory to watcher", "path", event.Name, "error", err)
							}
						}
					}
				}

				slog.DebugContext(ctx, "Filesystem change detected", "path", event.Name, "operation", event.Op.String())

				// Reset/Start debounce timer
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(fw.debounce)
				timerCh = timer.C
			}
		case <-timerCh:
			timerCh = nil
			if fw.onChange != nil {
				fw.onChange(ctx)
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.ErrorContext(ctx, "Filesystem watcher error", "error", err)
		}
	}
}

func (fw *FilesystemWatcher) shouldHandleEvent(event fsnotify.Event) bool {
	name := filepath.Base(event.Name)

	// Watch for new directories, compose files, .env being manipulated.
	if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() || isWatchableFile(name) {
			return true
		}
	}

	return false
}

func (fw *FilesystemWatcher) addExistingDirectories(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Warn("Error walking directory", "path", path, "error", err)
			return err
		}

		if d.IsDir() && path != root {
			depth := fw.dirDepth(path)
			if depth < 0 {
				return filepath.SkipDir
			}
			if fw.maxDepth > 0 && depth > fw.maxDepth {
				return filepath.SkipDir
			}

			if err := fw.watcher.Add(path); err != nil {
				slog.Error("Failed to add directory to watcher", "path", path, "error", err)
			}

			if fw.maxDepth > 0 && depth == fw.maxDepth {
				return filepath.SkipDir
			}
		}
		return nil
	})
}

func isWatchableFile(filename string) bool {
	for _, cf := range watchableFiles {
		if filename == cf {
			return true
		}
	}
	return false
}

func (fw *FilesystemWatcher) dirDepth(path string) int {
	cleanRoot := fw.watchedPath
	cleanPath := filepath.Clean(path)
	if cleanPath == cleanRoot {
		return 0
	}

	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return -1
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return -1
	}

	rel = filepath.ToSlash(rel)
	return strings.Count(rel, "/") + 1
}

func (fw *FilesystemWatcher) shouldWatchDir(path string) bool {
	if fw.maxDepth <= 0 {
		return true
	}
	depth := fw.dirDepth(path)
	return depth > 0 && depth <= fw.maxDepth
}
