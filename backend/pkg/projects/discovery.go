package projects

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/workspace"
)

type (
	DiscoveredProjectDir struct {
		DirName string
		Path    string
	}

	DiscoverOptions struct {
		Root               string
		GlobalEnvDirectory string
		SkipDirectories    string
		MaxDepth           int
		FollowSymlinks     bool
	}
)

const (
	ArcaneTrashPrefix = ".arcane-trash-"
)

var (
	gitOpsScratchEmbeddedNameRe = regexp.MustCompile(`\.gitops-(?:backup|sync-stage)-\d{10,}$`)

	filesystemSnapshotDirNames = map[string]bool{
		"#snapshot":  true,
		"#recycle":   true,
		".snapshot":  true,
		".snapshots": true,
		".zfs":       true,
	}
)

func IsProjectDirectoryEntry(ctx context.Context, entry types.Entry, followSymlinks bool) bool {
	if entry.IsDirectory {
		return true
	}

	if !followSymlinks || !entry.IsSymlink {
		return false
	}

	resolved, err := acfs.Host{}.Stat(ctx, entry.Path, types.StatOptions{FollowFinal: true})
	if err != nil {
		return false
	}

	return resolved.IsDirectory
}

func IsProjectDirectoryPath(ctx context.Context, path string, followSymlinks bool) (bool, error) {
	info, err := acfs.Host{}.Stat(ctx, path, types.StatOptions{})
	if err != nil {
		return false, err
	}

	if info.IsDirectory {
		return true, nil
	}

	if !followSymlinks || !info.IsSymlink {
		return false, nil
	}

	resolved, err := acfs.Host{}.Stat(ctx, path, types.StatOptions{FollowFinal: true})
	if err != nil {
		return false, err
	}

	return resolved.IsDirectory, nil
}

func DiscoverProjectDirectories(ctx context.Context, opts DiscoverOptions) ([]DiscoveredProjectDir, error) {
	root := filepath.Clean(opts.Root)

	isDir, err := IsProjectDirectoryPath(ctx, root, opts.FollowSymlinks)
	if err == nil && !isDir {
		return nil, fmt.Errorf("project root is not a directory: %s", root)
	}

	discovered := make([]DiscoveredProjectDir, 0)
	ancestors := make(map[string]struct{})

	if err == nil {
		err = walkProjectDirectories(
			ctx,
			cmp.Or(opts.GlobalEnvDirectory, root),
			root,
			true,
			0,
			opts.MaxDepth,
			opts.FollowSymlinks,
			workspace.SkipDirectorySet(opts.SkipDirectories),
			ancestors,
			&discovered,
		)
	}
	if err != nil {
		// The process runs as the PUID/PGID runtime user (default 65532) even
		// when the container was started as root, so a root-only projects
		// directory is genuinely unreadable (#3489).
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("projects directory is not readable by the runtime user (uid %d): fix its permissions or set PUID/PGID to an owner that can read it: %w", os.Geteuid(), err)
		}
		return nil, err
	}

	slices.SortStableFunc(discovered, func(a, b DiscoveredProjectDir) int {
		return strings.Compare(filepath.Clean(a.Path), filepath.Clean(b.Path))
	})

	return discovered, nil
}

func walkProjectDirectories(ctx context.Context,
	globalEnvDirectory,
	path string,
	isRoot bool,
	currentDepth,
	maxDepth int,
	followSymlinks bool,
	skipDirs map[string]bool,
	ancestors map[string]struct{},
	discovered *[]DiscoveredProjectDir,
) error {
	if !isRoot {
		name := filepath.Base(path)
		if IsIgnoredProjectDirName(name) || (currentDepth > 1 && skipDirs[name]) {
			return nil
		}
	}

	identity, err := ResolveDirectoryIdentity(path)
	if err != nil {
		if !isRoot && errors.Is(err, os.ErrPermission) {
			slog.WarnContext(ctx, "Skipping unreadable project directory during discovery", "path", path, "error", err)
			return nil
		}
		return err
	}
	if _, seen := ancestors[identity]; seen {
		return nil
	}

	ancestors[identity] = struct{}{}
	defer delete(ancestors, identity)

	switch composePath, detectComposeFileErr := DetectComposeFile(ctx, globalEnvDirectory, path); {
	case detectComposeFileErr == nil, errors.Is(detectComposeFileErr, common.ErrProjectEnvUnreadable) && composePath != "":
		if detectComposeFileErr != nil {
			slog.WarnContext(ctx, "Discovered project with an unreadable .env",
				"path", path,
				"envFile", filepath.Join(path, EffectiveEnvFileName),
				"uid", os.Geteuid(),
				"gid", os.Getegid(),
				"error", detectComposeFileErr)
		}
		*discovered = append(*discovered, DiscoveredProjectDir{
			DirName: filepath.Base(path),
			Path:    path,
		})
		if !isRoot {
			return nil
		}
	case errors.Is(detectComposeFileErr, common.ErrComposeFileNotFound):
	default:
		slog.WarnContext(ctx, "Skipping undetectable project directory during discovery", "path", path, "error", detectComposeFileErr)
	}

	if maxDepth > 0 && currentDepth >= maxDepth {
		return nil
	}

	entries, err := acfs.Host{}.List(ctx, path)
	if err != nil {
		if !isRoot && errors.Is(err, os.ErrPermission) {
			slog.WarnContext(ctx, "Skipping unreadable project directory during discovery", "path", path, "error", err)
			return nil
		}
		return err
	}

	for _, entry := range entries {
		childPath := entry.Path
		if !IsProjectDirectoryEntry(ctx, entry, followSymlinks) {
			continue
		}

		if walkProjectDirectoriesErr := walkProjectDirectories(
			ctx,
			globalEnvDirectory,
			childPath,
			false,
			currentDepth+1,
			maxDepth,
			followSymlinks,
			skipDirs,
			ancestors,
			discovered,
		); walkProjectDirectoriesErr != nil {
			return walkProjectDirectoriesErr
		}
	}

	return nil
}

func IsGitOpsScratchDirName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".gitops-sync-stage-") || strings.HasPrefix(name, ".gitops-backup-") {
		return true
	}
	return gitOpsScratchEmbeddedNameRe.MatchString(name)
}

func IsFilesystemSnapshotDirName(name string) bool {
	return filesystemSnapshotDirNames[strings.ToLower(strings.TrimSpace(name))]
}

func PathHasSegment(relPath string, match func(string) bool) bool {
	for segment := range strings.SplitSeq(filepath.ToSlash(relPath), "/") {
		if match(segment) {
			return true
		}
	}
	return false
}

func IsInternalScratchDirName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, ".project-update-preview-") || strings.HasPrefix(name, ".project-update-backup-") {
		return true
	}
	if strings.HasPrefix(name, ArcaneTrashPrefix) {
		return true
	}
	return IsGitOpsScratchDirName(name)
}

func IsIgnoredProjectDirName(name string) bool {
	return IsInternalScratchDirName(name) || IsFilesystemSnapshotDirName(name)
}

func ComposeContentProjectName(composeContent string) string {
	var doc struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(composeContent), &doc); err != nil {
		return ""
	}
	raw := strings.TrimSpace(doc.Name)
	if raw == "" || strings.Contains(raw, "${") {
		return ""
	}
	return loader.NormalizeProjectName(raw)
}

func NormalizeProjectName(name string) string {
	if name == "" {
		return ""
	}
	normalized := loader.NormalizeProjectName(name)
	return kit.Ternary(normalized == "", name, normalized)
}

// ResolveDirectoryIdentity stays on os.*: it resolves symlinks that
// may point anywhere on the host to establish directory identity, which the
// root-confined acfs API cannot do.
func ResolveDirectoryIdentity(path string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		resolvedPath = path
	}

	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", err
	}

	return filepath.Clean(absPath), nil
}

func ComposeEnvRequiresDirectorySync(composePath, overrideFileName string, envContent *string) bool {
	if envContent == nil {
		return false
	}
	envMap, err := ParseProjectEnvContent(*envContent, nil)
	if err != nil || len(envMap) == 0 {
		return false
	}

	allowedComposeFiles := map[string]struct{}{filepath.Base(composePath): {}}
	if overrideFileName != "" {
		allowedComposeFiles[overrideFileName] = struct{}{}
	}
	if entriesNeedDirectorySync(ComposeFileEntriesFromEnv(envMap), allowedComposeFiles) {
		return true
	}

	// Single-file sync writes the synced env content to .env, so a
	// self-reference is satisfiable; anything else never reaches the project.
	return entriesNeedDirectorySync(ComposeEnvFileEntriesFromEnv(envMap), map[string]struct{}{EffectiveEnvFileName: {}})
}

func entriesNeedDirectorySync(entries []string, allowed map[string]struct{}) bool {
	for _, entry := range entries {
		// Normalize so relative prefixes like "./compose.yaml" match allowed.
		cleaned := filepath.Clean(entry)
		if cleaned != filepath.Base(cleaned) {
			return true // nested path can't be single-file synced
		}
		if _, ok := allowed[cleaned]; !ok {
			return true
		}
	}
	return false
}
