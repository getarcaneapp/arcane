package projects

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/cache"

	"getarcane.dev/types/v3"
)

type (
	composeCache[T any] struct {
		entries *cache.Cache[string, composeCacheEntry[T]]
		clone   func(T) T
	}

	composeCacheEntry[T any] struct {
		value       T
		stamps      map[string]composeCacheStamp
		fingerprint string
	}

	composeCacheStamp struct {
		modTime time.Time
		size    int64
	}
)

// clone, when supplied, isolates stored values from mutations by callers.
func NewComposeCache[T any](capacity int, clone func(T) T) types.ComposeCache[T] {
	return &composeCache[T]{
		entries: &cache.Cache[string, composeCacheEntry[T]]{
			Capacity: capacity,
		},
		clone: clone,
	}
}

func CloneComposeProject(project *composetypes.Project) *composetypes.Project {
	if project == nil {
		return nil
	}
	// With no names this compose-go operation deep-copies without disabling services.
	return project.WithServicesDisabled()
}

func (c *composeCache[T]) Get(ctx context.Context, projectID, fingerprint string) (T, bool) {
	entry, found := c.entries.Peek(projectID)
	if found && entry.fingerprint == fingerprint && validComposeCacheEntry(ctx, entry.stamps) {
		if c.clone != nil {
			return c.clone(entry.value), true
		}
		return entry.value, true
	}
	var zero T
	return zero, false
}

func (c *composeCache[T]) Set(ctx context.Context, projectID, fingerprint, projectPath, projectsDirectory, composePath string, composeFiles, envFiles []string, loadedAt time.Time, value T) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}
	stamps := make(map[string]composeCacheStamp)
	for _, path := range append([]string{composePath}, composeFiles...) {
		if path == "" {
			continue
		}
		entry, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
		if err != nil {
			return fmt.Errorf("stat compose file %s: %w", path, err)
		}
		if !time.Unix(0, entry.ModTimeUnixNano).Before(loadedAt) {
			return nil
		}
		stamps[path] = composeCacheStamp{modTime: time.Unix(0, entry.ModTimeUnixNano), size: entry.Size}
	}
	dependencies := append([]string{filepath.Join(projectPath, EffectiveEnvFileName)}, envFiles...)
	if projectsDirectory != "" {
		dependencies = append(dependencies, filepath.Join(projectsDirectory, GlobalEnvFileName))
	}
	// A later standard override must invalidate a model loaded before it existed.
	for _, name := range ComposeOverrideFileCandidates {
		dependencies = append(dependencies, filepath.Join(filepath.Dir(composePath), name))
	}
	for _, path := range dependencies {
		entry, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
		if err == nil {
			if !time.Unix(0, entry.ModTimeUnixNano).Before(loadedAt) {
				return nil
			}
			stamps[path] = composeCacheStamp{modTime: time.Unix(0, entry.ModTimeUnixNano), size: entry.Size}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat compose dependency %s: %w", path, err)
		}
		stamps[path] = composeCacheStamp{}
	}
	if c.clone != nil {
		value = c.clone(value)
	}
	c.entries.Set(cache.Entry[string, composeCacheEntry[T]]{
		Key:   projectID,
		Value: composeCacheEntry[T]{fingerprint: fingerprint, stamps: stamps, value: value},
	})
	return nil
}

func (c *composeCache[T]) Invalidate(projectID string) {
	c.entries.Delete(projectID)
}

func validComposeCacheEntry(ctx context.Context, stamps map[string]composeCacheStamp) bool {
	for path, stamp := range stamps {
		entry, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) || !stamp.modTime.IsZero() {
				return false
			}
			continue
		}
		if stamp.modTime.IsZero() || !time.Unix(0, entry.ModTimeUnixNano).Equal(stamp.modTime) || entry.Size != stamp.size {
			return false
		}
		// A chmod does not bump mtime, so confirm the file is still readable.
		file, _, err := acfs.Host{}.OpenRead(ctx, path, 0)
		if err != nil {
			return false
		}
		if closeErr := file.Close(); closeErr != nil {
			return false
		}
	}
	return true
}

func LoadCachedComposeProject(ctx context.Context,
	projectCache types.ComposeCache[*composetypes.Project],
	projectID,
	projectPath,
	composePath,
	projectName,
	projectsDirectory string,
	autoInject bool,
	pathMapper *PathMapper) (*composetypes.Project,
	error,
) {
	fingerprint := fmt.Sprintf("%q|%q|%q|%t|%#v", composePath, projectName, projectsDirectory, autoInject, pathMapper)
	if projectCache != nil {
		if cached, ok := projectCache.Get(ctx, projectID, fingerprint); ok {
			return cached, nil
		}
	}
	dependencies := types.ComposeDependencies{}
	loadedAt := time.Now()
	model, err := LoadComposeProject(ctx, LoadOptions{
		ComposeFile:       composePath,
		ProjectName:       projectName,
		ProjectsDirectory: projectsDirectory,
		AutoInjectEnv:     autoInject,
		PathMapper:        pathMapper,
		Dependencies:      &dependencies,
	})
	if err != nil {
		return nil, err
	}
	if projectCache == nil {
		return model, nil
	}
	composeFiles, envFiles, ok := ComposeCacheDependencies(ctx, model, dependencies, composePath, projectsDirectory, autoInject)
	if !ok {
		// Metadata discovery failure disables caching; the executable model is valid.
		return model, nil
	}
	if setErr := projectCache.Set(ctx, projectID, fingerprint, projectPath, projectsDirectory, composePath, composeFiles, envFiles, loadedAt, model); setErr != nil {
		return nil, setErr
	}
	return model, nil
}

func ComposeCacheDependencies(
	ctx context.Context,
	model *composetypes.Project,
	dependencies types.ComposeDependencies,
	composePath, projectsDirectory string,
	autoInject bool,
) ([]string, []string, bool) {
	envFiles := dependencies.EnvFiles
	if envOpts, parseErr := ParseComposeEnvOptions(ctx, model.WorkingDir, EnvMap(model.Environment)); parseErr == nil {
		envFiles = append(envFiles, envOpts.EnvFiles...)
	}
	// Metadata discovery already follows recursive includes and their interpolation env files.
	meta, err := ParseArcaneComposeMetadata(ctx, composePath, projectsDirectory, autoInject)
	if err != nil {
		return nil, nil, false
	}
	composeFiles := append(append([]string{}, model.ComposeFiles...), meta.ComposeFiles...)
	return composeFiles, append(envFiles, meta.EnvFiles...), true
}
