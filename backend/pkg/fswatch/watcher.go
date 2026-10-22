// Package fswatch stays on os.* rather than acfs: it watches arbitrary
// user-configured paths and deliberately follows symlinks that may resolve
// outside any confinement root, which acfs cannot do.
package fswatch

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
)

type Watcher struct {
	callbacks      sync.WaitGroup
	watcher        *fsnotify.Watcher
	watchedPath    string
	maxDepth       int
	followSymlinks bool
	onChange       func(ctx context.Context)
	onChangePaths  func(ctx context.Context, paths []string)
	debounce       time.Duration
	stopCh         chan struct{}
	stoppedCh      chan struct{}
	cancel         context.CancelFunc
	watchAliases   map[string]string
	pendingPaths   map[string]struct{}
	readyPaths     map[string]struct{}
	changed        bool
	wake           chan struct{}
	mu             sync.Mutex
	started        bool
	stopped        bool
	stopOnce       sync.Once
	stopErr        error
}

type WatcherOptions struct {
	Debounce          time.Duration
	OnChange          func(ctx context.Context)
	OnChangePaths     func(ctx context.Context, paths []string)
	MaxDepth          int
	FollowSymlinkDirs bool
}

func NewWatcher(watchPath string, opts WatcherOptions) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	opts.Debounce = cmp.Or(opts.Debounce, 2*time.Second)

	if opts.MaxDepth < 0 {
		opts.MaxDepth = 0
	}

	return &Watcher{
		watcher:        watcher,
		watchedPath:    filepath.Clean(watchPath),
		maxDepth:       opts.MaxDepth,
		followSymlinks: opts.FollowSymlinkDirs,
		onChange:       opts.OnChange,
		onChangePaths:  opts.OnChangePaths,
		debounce:       opts.Debounce,
		stopCh:         make(chan struct{}),
		stoppedCh:      make(chan struct{}),
		watchAliases:   make(map[string]string),
		pendingPaths:   make(map[string]struct{}),
		readyPaths:     make(map[string]struct{}),
		wake:           make(chan struct{}, 1),
	}, nil
}

func (fw *Watcher) Start(ctx context.Context) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.started {
		return nil
	}
	if fw.stopped {
		return errors.New("watcher already stopped")
	}

	if err := fw.watcher.Add(fw.watchedPath); err != nil {
		// The underlying fsnotify watcher already holds an inotify/kqueue
		// descriptor. A caller whose Start failed has no reason to call Stop, so
		// release it here and mark the watcher spent. fsnotify's Close is
		// idempotent, so a later Stop remains safe.
		fw.stopped = true
		if closeErr := fw.watcher.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close filesystem watcher after failed start",
				"path", fw.watchedPath,
				"error", closeErr)
		}
		return err
	}

	if err := fw.addExistingDirectories(ctx, fw.watchedPath); err != nil {
		slog.WarnContext(ctx, "Failed to add some existing directories to watcher",
			"path", fw.watchedPath,
			"error", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	fw.cancel = cancel
	go fw.watchLoopInternal(watchCtx)
	fw.callbacks.Go(func() { fw.notifyLoop(watchCtx) })
	fw.started = true

	slog.InfoContext(ctx, "Filesystem watcher started", "path", fw.watchedPath)
	return nil
}

// Stop closes the watcher and waits for accepted callbacks.
func (fw *Watcher) Stop() error {
	fw.stopOnce.Do(func() {
		fw.mu.Lock()
		fw.stopped = true
		started := fw.started
		cancel := fw.cancel
		fw.mu.Unlock()

		if started {
			cancel()
			close(fw.stopCh)
			<-fw.stoppedCh // Wait for watchLoop to finish
		}

		err := fw.watcher.Close()

		fw.mu.Lock()
		fw.stopErr = err
		fw.mu.Unlock()
	})

	fw.callbacks.Wait()

	fw.mu.Lock()
	defer fw.mu.Unlock()

	return fw.stopErr
}

func (fw *Watcher) watchLoopInternal(ctx context.Context) {
	defer close(fw.stoppedCh)

	debounceTimer := time.NewTimer(fw.debounce)
	debounceTimer.Stop()
	defer debounceTimer.Stop()
	healthTicker := time.NewTicker(5 * time.Second)
	defer healthTicker.Stop()
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
				if !fw.reconnect(ctx) {
					return
				}
			}
		case <-debounceTimer.C:
			if fw.fireDebounce(ctx, &debouncePending, &lastGoroutineLog) {
				continue
			}
		case <-healthTicker.C:
			if len(fw.watcher.WatchList()) == 0 && !fw.reconnect(ctx) {
				return
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				if !fw.reconnect(ctx) {
					return
				}
				continue
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

	// handleEvent runs for every event — its job is to keep the
	// underlying fsnotify subscriptions in sync (attaching newly-created
	// directories so we can discover compose files that land inside them
	// afterwards). Sync-firing is gated separately by shouldHandleEvent.
	fw.handleEvent(ctx, event)

	if !fw.shouldHandleEvent(event) {
		return false
	}
	fw.recordPendingPathInternal(event.Name)
	debounceTimer.Reset(fw.debounce)
	*debouncePending = true
	return false
}

// fireDebounce runs the debounced onChange callback; returns true if nothing was pending (caller should continue).
func (fw *Watcher) fireDebounce(ctx context.Context, debouncePending *bool, lastGoroutineLog *time.Time) bool {
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
	fw.queueChanges(fw.drainPendingPaths())
	return false
}

func (fw *Watcher) queueChanges(paths []string) {
	fw.mu.Lock()
	fw.changed = true
	for _, path := range paths {
		fw.readyPaths[path] = struct{}{}
	}
	fw.mu.Unlock()
	select {
	case fw.wake <- struct{}{}:
	default:
	}
}

func (fw *Watcher) notifyLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-fw.wake:
		}
		if ctx.Err() != nil {
			return
		}
		fw.mu.Lock()
		changed := fw.changed
		paths := slices.Collect(maps.Keys(fw.readyPaths))
		fw.changed = false
		clear(fw.readyPaths)
		fw.mu.Unlock()
		if !changed {
			continue
		}
		if fw.onChange != nil {
			fw.onChange(ctx)
		}
		if fw.onChangePaths != nil && len(paths) > 0 {
			fw.onChangePaths(ctx, paths)
		}
	}
}

func (fw *Watcher) recordPendingPathInternal(path string) {
	if path == "" {
		return
	}
	fw.mu.Lock()
	fw.pendingPaths[filepath.Clean(path)] = struct{}{}
	fw.mu.Unlock()
}

func (fw *Watcher) drainPendingPaths() []string {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	paths := slices.Collect(maps.Keys(fw.pendingPaths))
	clear(fw.pendingPaths)
	return paths
}

// handleEvent keeps the underlying fsnotify subscriptions in sync with
// the filesystem: when a new directory is created inside a watched path, it is
// attached recursively so that compose files dropped inside it are discovered.
// It does NOT decide whether the event should trigger a sync — that is
// shouldHandleEvent's job.
func (fw *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		removed := filepath.Clean(event.Name)
		if _, watched := fw.watchAliases[removed]; watched {
			maps.DeleteFunc(fw.watchAliases, func(watchPath, _ string) bool {
				return watchPath == removed || strings.HasPrefix(watchPath, removed+string(os.PathSeparator))
			})
		}
	}
	if event.Has(fsnotify.Create) && !projects.IsIgnoredProjectDirName(filepath.Base(event.Name)) {
		logicalName := fw.logicalPathForWatchEventInternal(event.Name)
		if fw.isWatchableDirectory(event.Name) {
			if fw.shouldWatchDir(logicalName) {
				if err := fw.addExistingDirectoriesRecursiveInternal(ctx, event.Name, logicalName, map[string]struct{}{}); err != nil {
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

func (fw *Watcher) shouldHandleEvent(event fsnotify.Event) bool {
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Chmod) {
		return false
	}
	if !projects.IsProjectFile(filepath.Base(event.Name)) {
		return false
	}
	// Arcane's own scratch directories (update backups, GitOps staging) churn
	// compose files that must not re-sync the live projects.
	rel, err := filepath.Rel(fw.watchedPath, fw.logicalPathForWatchEventInternal(event.Name))
	return err != nil || !projects.PathContainsIgnoredDirectory(rel)
}

func (fw *Watcher) addExistingDirectories(ctx context.Context, root string) error {
	return fw.addExistingDirectoriesRecursiveInternal(ctx, root, root, map[string]struct{}{})
}

func (fw *Watcher) addExistingDirectoriesRecursiveInternal(ctx context.Context, path, logicalPath string, ancestors map[string]struct{}) error {
	identity, err := projects.ResolveDirectoryIdentityInternal(path)
	if err != nil {
		if path != fw.watchedPath && errors.Is(err, os.ErrPermission) {
			slog.WarnContext(ctx, "Skipping unreadable directory for watcher", "path", path, "error", err)
			return nil
		}
		return err
	}
	if _, seen := ancestors[identity]; seen {
		return nil
	}

	ancestors[identity] = struct{}{}
	defer delete(ancestors, identity)

	if path != fw.watchedPath {
		depth := fw.dirDepth(logicalPath)
		if depth < 0 {
			return nil
		}
		if fw.maxDepth > 0 && depth > fw.maxDepth {
			return nil
		}

		fw.addWatchPathInternal(ctx, path, logicalPath)

		if fw.maxDepth > 0 && depth == fw.maxDepth {
			return nil
		}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if path != fw.watchedPath && errors.Is(err, os.ErrPermission) {
			slog.WarnContext(ctx, "Skipping unreadable directory for watcher", "path", path, "error", err)
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if projects.IsIgnoredProjectDirName(entry.Name()) {
			continue
		}
		childPath := filepath.Join(path, entry.Name())
		if !projects.IsProjectDirectoryEntry(entry, childPath, fw.followSymlinks) {
			continue
		}
		childLogicalPath := filepath.Join(logicalPath, entry.Name())
		if addExistingDirectoriesRecursiveErr := fw.addExistingDirectoriesRecursiveInternal(ctx, childPath, childLogicalPath, ancestors); addExistingDirectoriesRecursiveErr != nil {
			return addExistingDirectoriesRecursiveErr
		}
	}

	return nil
}

func (fw *Watcher) logicalPathForWatchEventInternal(path string) string {
	cleanPath := filepath.Clean(path)
	bestWatchPath := ""
	bestLogicalPath := ""

	for watchPath, logicalPath := range fw.watchAliases {
		cleanWatchPath := filepath.Clean(watchPath)
		if cleanPath != cleanWatchPath && !strings.HasPrefix(cleanPath, cleanWatchPath+string(os.PathSeparator)) {
			continue
		}
		if len(cleanWatchPath) <= len(bestWatchPath) {
			continue
		}
		bestWatchPath = cleanWatchPath
		bestLogicalPath = logicalPath
	}

	if bestWatchPath == "" {
		return cleanPath
	}

	rel, err := filepath.Rel(bestWatchPath, cleanPath)
	if err != nil || rel == "." {
		return filepath.Clean(bestLogicalPath)
	}

	return filepath.Join(bestLogicalPath, rel)
}

func (fw *Watcher) addWatchPathInternal(ctx context.Context, path, logicalPath string) {
	watchPaths := []string{path}
	if fw.followSymlinks {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if resolvedPath, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil && resolvedPath != path {
				watchPaths = append(watchPaths, resolvedPath)
			}
		}
	}

	for _, watchPath := range watchPaths {
		fw.watchAliases[filepath.Clean(watchPath)] = filepath.Clean(logicalPath)
		if err := fw.watcher.Add(watchPath); err != nil {
			slog.WarnContext(ctx, "Failed to add directory to watcher",
				"path", watchPath,
				"error", err)
		}
	}
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

// reconnect replaces a lost filesystem subscription without abandoning its owner.
func (fw *Watcher) reconnect(ctx context.Context) bool {
	if err := fw.watcher.Close(); err != nil {
		slog.DebugContext(ctx, "Closing lost filesystem subscription", "error", err)
	}
	delay := 5 * time.Second
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-fw.stopCh:
			timer.Stop()
			return false
		case <-timer.C:
		}
		watcher, err := fsnotify.NewWatcher()
		if err == nil {
			err = watcher.Add(fw.watchedPath)
			if err == nil {
				fw.watcher = watcher
				fw.watchAliases = make(map[string]string)
				if addExistingDirectoriesErr := fw.addExistingDirectories(ctx, fw.watchedPath); addExistingDirectoriesErr != nil {
					slog.WarnContext(ctx, "Some filesystem subscriptions could not be restored", "error", addExistingDirectoriesErr)
				}
				// Changes while disconnected need a full reconciliation.
				fw.queueChanges([]string{fw.watchedPath})
				return true
			}
			if closeErr := watcher.Close(); closeErr != nil {
				slog.DebugContext(ctx, "Closing failed filesystem subscription", "error", closeErr)
			}
		}
		slog.WarnContext(ctx, "Filesystem subscription unavailable; retrying", "path", fw.watchedPath, "error", err)
		delay = min(delay*2, 5*time.Minute)
	}
}
