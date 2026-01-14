package fs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
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

func NewWatcher(watchPath string, opts WatcherOptions) (*Watcher, error) {
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

	return &Watcher{
		watcher:     watcher,
		watchedPath: filepath.Clean(watchPath),
		maxDepth:    opts.MaxDepth,
		onChange:    opts.OnChange,
		debounce:    opts.Debounce,
		stopCh:      make(chan struct{}),
		stoppedCh:   make(chan struct{}),
	}, nil
}

func (fw *Watcher) Start(ctx context.Context) error {
	if err := fw.watcher.Add(fw.watchedPath); err != nil {
		return err
	}

	if err := fw.addExistingDirectories(fw.watchedPath); err != nil {
		slog.WarnContext(ctx, "Failed to add some existing directories to watcher",
			"path", fw.watchedPath,
			"error", err)
	}

	go fw.watchLoop(ctx)

	slog.InfoContext(ctx, "Filesystem watcher started", "path", fw.watchedPath)
	return nil
}

func (fw *Watcher) Stop() error {
	close(fw.stopCh)
	<-fw.stoppedCh // Wait for watchLoop to finish
	return fw.watcher.Close()
}

func (fw *Watcher) watchLoop(ctx context.Context) {
	defer close(fw.stoppedCh)

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

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
				fw.handleEvent(ctx, event, debounceTimer)
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.ErrorContext(ctx, "Filesystem watcher error", "error", err)
		}
	}
}

func (fw *Watcher) handleEvent(ctx context.Context, event fsnotify.Event, debounceTimer *time.Timer) {
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if fw.shouldWatchDir(event.Name) {
				if err := fw.watcher.Add(event.Name); err != nil {
					slog.WarnContext(ctx, "Failed to add new directory to watcher",
						"path", event.Name,
						"error", err)
				}
			}
		}
	}

	slog.DebugContext(ctx, "Filesystem change detected",
		"path", event.Name,
		"operation", event.Op.String())

	// Reset debounce timer
	if !debounceTimer.Stop() {
		select {
		case <-debounceTimer.C:
		default:
		}
	}
	debounceTimer.Reset(fw.debounce)

	// Start debounce handler if not already running
	select {
	case <-debounceTimer.C:
		// Timer expired, trigger sync
		if fw.onChange != nil {
			go fw.onChange(ctx)
		}
	default:
		// Timer still running, will trigger later
		go fw.handleDebounce(ctx, debounceTimer)
	}
}

func (fw *Watcher) handleDebounce(ctx context.Context, timer *time.Timer) {
	select {
	case <-ctx.Done():
		return
	case <-fw.stopCh:
		return
	case <-timer.C:
		if fw.onChange != nil {
			fw.onChange(ctx)
		}
	}
}

func (fw *Watcher) shouldHandleEvent(event fsnotify.Event) bool {
	name := filepath.Base(event.Name)

	// Watch for new directories, compose files, .env being manipulated.
	if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() || IsProjectFile(name) {
			return true
		}
	}

	return false
}

func (fw *Watcher) addExistingDirectories(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			slog.Warn("Error walking directory",
				"path", path,
				"error", err)
			return err
		}

		if info.IsDir() && path != root {
			depth := fw.dirDepth(path)
			if depth < 0 {
				return filepath.SkipDir
			}
			if fw.maxDepth > 0 && depth > fw.maxDepth {
				return filepath.SkipDir
			}

			if err := fw.watcher.Add(path); err != nil {
				slog.Warn("Failed to add directory to watcher",
					"path", path,
					"error", err)
			}

			if fw.maxDepth > 0 && depth == fw.maxDepth {
				return filepath.SkipDir
			}
		}
		return nil
	})
}

// IsProjectFile checks if a filename is a common Docker Compose or environment file
func IsProjectFile(filename string) bool {
	composeFiles := []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
		".env",
	}

	for _, cf := range composeFiles {
		if filename == cf {
			return true
		}
	}
	return false
}

func (fw *Watcher) dirDepth(path string) int {
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

func (fw *Watcher) shouldWatchDir(path string) bool {
	if fw.maxDepth <= 0 {
		return true
	}
	depth := fw.dirDepth(path)
	return depth > 0 && depth <= fw.maxDepth
}
