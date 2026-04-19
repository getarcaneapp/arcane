package fswatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
)

type Watcher struct {
	watcher        *fsnotify.Watcher
	watchedPath    string
	maxDepth       int
	followSymlinks bool
	onChange       func(ctx context.Context)
	debounce       time.Duration
	stopCh         chan struct{}
	stoppedCh      chan struct{}
	mu             sync.Mutex
	started        bool
	stopped        bool
	stopErr        error
}

type WatcherOptions struct {
	Debounce          time.Duration
	OnChange          func(ctx context.Context)
	MaxDepth          int
	FollowSymlinkDirs bool
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
		watcher:        watcher,
		watchedPath:    filepath.Clean(watchPath),
		maxDepth:       opts.MaxDepth,
		followSymlinks: opts.FollowSymlinkDirs,
		onChange:       opts.OnChange,
		debounce:       opts.Debounce,
		stopCh:         make(chan struct{}),
		stoppedCh:      make(chan struct{}),
	}, nil
}

func (fw *Watcher) Start(ctx context.Context) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.started {
		return nil
	}
	if fw.stopped {
		return fmt.Errorf("watcher already stopped")
	}

	if err := fw.watcher.Add(fw.watchedPath); err != nil {
		return err
	}

	if err := fw.addExistingDirectories(fw.watchedPath); err != nil {
		slog.WarnContext(ctx, "Failed to add some existing directories to watcher",
			"path", fw.watchedPath,
			"error", err)
	}

	go fw.watchLoop(ctx)
	fw.started = true

	slog.InfoContext(ctx, "Filesystem watcher started", "path", fw.watchedPath)
	return nil
}

func (fw *Watcher) Stop() error {
	fw.mu.Lock()
	if fw.stopped {
		err := fw.stopErr
		fw.mu.Unlock()
		return err
	}

	fw.stopped = true
	started := fw.started
	fw.mu.Unlock()

	if started {
		close(fw.stopCh)
		<-fw.stoppedCh // Wait for watchLoop to finish
	}

	err := fw.watcher.Close()

	fw.mu.Lock()
	fw.stopErr = err
	fw.mu.Unlock()

	return err
}

func (fw *Watcher) watchLoop(ctx context.Context) {
	defer close(fw.stoppedCh)

	debounceTimer := time.NewTimer(fw.debounce)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	debouncePending := false
	lastGoroutineLog := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.stopCh:
			return
		case event, ok := <-fw.watcher.Events:
			if fw.processEventInternal(ctx, event, ok, debounceTimer, &debouncePending) {
				return
			}
		case <-debounceTimer.C:
			if fw.fireDebounceInternal(ctx, &debouncePending, &lastGoroutineLog) {
				continue
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.ErrorContext(ctx, "Filesystem watcher error", "error", err)
		}
	}
}

// processEventInternal handles one fs event; returns true if the watch loop should exit.
func (fw *Watcher) processEventInternal(ctx context.Context, event fsnotify.Event, ok bool, debounceTimer *time.Timer, debouncePending *bool) bool {
	if !ok {
		return true
	}

	// handleEventInternal runs for every event — its job is to keep the
	// underlying fsnotify subscriptions in sync (attaching newly-created
	// directories so we can discover compose files that land inside them
	// afterwards). Sync-firing is gated separately by shouldHandleEventInternal.
	fw.handleEventInternal(ctx, event)

	if !fw.shouldHandleEventInternal(event) {
		return false
	}
	if !debounceTimer.Stop() {
		select {
		case <-debounceTimer.C:
		default:
		}
	}
	debounceTimer.Reset(fw.debounce)
	*debouncePending = true
	return false
}

// fireDebounceInternal runs the debounced onChange callback; returns true if nothing was pending (caller should continue).
func (fw *Watcher) fireDebounceInternal(ctx context.Context, debouncePending *bool, lastGoroutineLog *time.Time) bool {
	if !*debouncePending {
		return true
	}
	*debouncePending = false
	if time.Since(*lastGoroutineLog) > 30*time.Second {
		slog.DebugContext(ctx, "Filesystem watcher debounce triggered",
			"path", fw.watchedPath,
			"goroutines", runtime.NumGoroutine())
		*lastGoroutineLog = time.Now()
	}
	if fw.onChange != nil {
		go fw.onChange(ctx)
	}
	return false
}

// handleEventInternal keeps the underlying fsnotify subscriptions in sync with
// the filesystem: when a new directory is created inside a watched path, it is
// attached recursively so that compose files dropped inside it are discovered.
// It does NOT decide whether the event should trigger a sync — that is
// shouldHandleEventInternal's job.
func (fw *Watcher) handleEventInternal(ctx context.Context, event fsnotify.Event) {
	if event.Has(fsnotify.Create) {
		if fw.isWatchableDirectory(event.Name) {
			if fw.shouldWatchDir(event.Name) {
				if err := fw.addExistingDirectories(event.Name); err != nil {
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
}

// shouldHandleEventInternal reports whether an fsnotify event should trigger a
// project sync. Only events on known compose / env files qualify; bare
// directory events do not, because build tools (ESPHome, PlatformIO, npm,
// etc.) churn through subdirectories and would otherwise spin the sync loop
// forever. New project directories are still discovered: once the directory
// create event has been processed by handleEventInternal (which attaches a
// watch), the compose file's own Create event reaches this filter and passes
// via IsProjectFile.
func (fw *Watcher) shouldHandleEventInternal(event fsnotify.Event) bool {
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) {
		return false
	}
	return projects.IsProjectFile(filepath.Base(event.Name))
}

func (fw *Watcher) addExistingDirectories(root string) error {
	return fw.addExistingDirectoriesRecursiveInternal(root, map[string]struct{}{})
}

func (fw *Watcher) addExistingDirectoriesRecursiveInternal(path string, ancestors map[string]struct{}) error {
	identity, err := projects.ResolveDirectoryIdentityInternal(path)
	if err != nil {
		return err
	}
	if _, seen := ancestors[identity]; seen {
		return nil
	}

	ancestors[identity] = struct{}{}
	defer delete(ancestors, identity)

	if path != fw.watchedPath {
		depth := fw.dirDepth(path)
		if depth < 0 {
			return nil
		}
		if fw.maxDepth > 0 && depth > fw.maxDepth {
			return nil
		}

		if err := fw.watcher.Add(path); err != nil {
			slog.Warn("Failed to add directory to watcher",
				"path", path,
				"error", err)
		}

		if fw.maxDepth > 0 && depth == fw.maxDepth {
			return nil
		}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		if !projects.IsProjectDirectoryEntry(entry, childPath, fw.followSymlinks) {
			continue
		}
		if err := fw.addExistingDirectoriesRecursiveInternal(childPath, ancestors); err != nil {
			return err
		}
	}

	return nil
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

func (fw *Watcher) isWatchableDirectory(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}

	if info.IsDir() {
		return true
	}

	if !fw.followSymlinks || info.Mode()&os.ModeSymlink == 0 {
		return false
	}

	resolvedInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return resolvedInfo.IsDir()
}
