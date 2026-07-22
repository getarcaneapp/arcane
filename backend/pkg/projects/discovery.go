package projects

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"emperror.dev/errors"
)

type DiscoveredProjectDir struct {
	DirName string
	Path    string
}

// IsProjectDirectoryEntry reports whether a directory entry should be treated as a project directory.
// Regular directories are always accepted. Symlinked directories are accepted only when enabled.
func IsProjectDirectoryEntry(entry os.DirEntry, path string, followSymlinks bool) bool {
	if entry == nil {
		return false
	}

	if entry.IsDir() {
		return true
	}

	if !followSymlinks || entry.Type()&os.ModeSymlink == 0 {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

// IsProjectDirectoryPath reports whether an existing path should be treated as a project directory.
// Regular directories are always accepted. Symlinked directories are accepted only when enabled.
func IsProjectDirectoryPath(path string, followSymlinks bool) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}

	if info.IsDir() {
		return true, nil
	}

	if !followSymlinks || info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}

	resolvedInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return resolvedInfo.IsDir(), nil
}

func DiscoverProjectDirectories(root string, followSymlinks bool, maxDepth int) ([]DiscoveredProjectDir, error) {
	root = filepath.Clean(root)

	isDir, err := IsProjectDirectoryPath(root, followSymlinks)
	if err != nil {
		return nil, err
	}
	if !isDir {
		return nil, errors.Errorf("project root is not a directory: %s", root)
	}

	discovered := make([]DiscoveredProjectDir, 0)
	ancestors := make(map[string]struct{})

	if err := walkProjectDirectoriesInternal(root, true, 0, maxDepth, followSymlinks, ancestors, &discovered); err != nil {
		return nil, err
	}

	slices.SortStableFunc(discovered, func(a, b DiscoveredProjectDir) int {
		return strings.Compare(filepath.Clean(a.Path), filepath.Clean(b.Path))
	})

	return discovered, nil
}

func walkProjectDirectoriesInternal(path string, isRoot bool, currentDepth int, maxDepth int, followSymlinks bool, ancestors map[string]struct{}, discovered *[]DiscoveredProjectDir) error {
	if !isRoot {
		name := filepath.Base(path)
		if IsInternalScratchDirName(name) || IsFilesystemSnapshotDirName(name) {
			return nil
		}
	}

	identity, err := ResolveDirectoryIdentityInternal(path)
	if err != nil {
		if !isRoot && errors.Is(err, os.ErrPermission) {
			slog.Warn("Skipping unreadable project directory during discovery", "path", path, "error", err)
			return nil
		}
		return err
	}
	if _, seen := ancestors[identity]; seen {
		return nil
	}

	ancestors[identity] = struct{}{}
	defer delete(ancestors, identity)

	// If this directory contains a compose file, treat it as a single project
	// and stop descending. Nested compose files are assumed to belong to the
	// parent project (e.g. via compose `include:` directives) and should not
	// be discovered as separate top-level projects.
	//
	// The projects root directory itself is exempt — we always descend into it
	// so siblings under the root are all discovered, even if the root happens
	// to contain its own compose file.
	if _, err := DetectComposeFile(path); err == nil {
		*discovered = append(*discovered, DiscoveredProjectDir{
			DirName: filepath.Base(path),
			Path:    path,
		})
		if !isRoot {
			return nil
		}
	}

	if maxDepth > 0 && currentDepth >= maxDepth {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if !isRoot && errors.Is(err, os.ErrPermission) {
			slog.Warn("Skipping unreadable project directory during discovery", "path", path, "error", err)
			return nil
		}
		return err
	}

	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		if !IsProjectDirectoryEntry(entry, childPath, followSymlinks) {
			continue
		}

		if err := walkProjectDirectoriesInternal(childPath, false, currentDepth+1, maxDepth, followSymlinks, ancestors, discovered); err != nil {
			return err
		}
	}

	return nil
}

// gitOpsScratchEmbeddedNameRe matches the legacy name-embedded gitops scratch
// directory form, e.g. "Makerra.gitops-backup-1780656786384743013". That form always
// used a time.Now().UnixNano() suffix (≥18 digits), so a long digit run is required:
// it keeps a real user project ending in "<base>.gitops-backup-notes" (non-numeric) or
// "<base>.gitops-backup-2024" (short, year-like) from being mistaken for Arcane scratch.
// The hidden os.MkdirTemp forms (".gitops-backup-*", ".gitops-sync-stage-*") are matched
// by prefix, not by this regex.
var gitOpsScratchEmbeddedNameRe = regexp.MustCompile(`\.gitops-(?:backup|sync-stage)-\d{10,}$`)

// IsGitOpsScratchDirName reports whether name is an Arcane GitOps scratch directory:
// the hidden staging/backup temp dirs (".gitops-sync-stage-*", ".gitops-backup-*")
// or the legacy name-embedded backup form ("<name>.gitops-backup-<digits>"). These are
// Arcane-internal working directories and must never be imported as user projects.
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

// filesystemSnapshotDirNames are directory names used by filesystems and NAS
// appliances for read-only snapshots or trash. They contain point-in-time
// copies of real project directories and must never be discovered as projects:
// #snapshot/#recycle (Synology BTRFS), .snapshot (NetApp), .snapshots
// (snapper/btrbk), .zfs (ZFS snapdir).
var filesystemSnapshotDirNames = map[string]bool{
	"#snapshot":  true,
	"#recycle":   true,
	".snapshot":  true,
	".snapshots": true,
	".zfs":       true,
}

// IsFilesystemSnapshotDirName reports whether name is a well-known filesystem
// snapshot/trash directory (see filesystemSnapshotDirNames).
func IsFilesystemSnapshotDirName(name string) bool {
	return filesystemSnapshotDirNames[strings.ToLower(strings.TrimSpace(name))]
}

// PathContainsSnapshotDirectory reports whether any segment of relPath is a
// filesystem snapshot/trash directory name, i.e. the path points into a
// point-in-time copy rather than a live project directory.
func PathContainsSnapshotDirectory(relPath string) bool {
	for segment := range strings.SplitSeq(filepath.ToSlash(relPath), "/") {
		if IsFilesystemSnapshotDirName(segment) {
			return true
		}
	}
	return false
}

// ArcaneTrashPrefix is the prefix Arcane uses when quarantining (soft-deleting) a
// project's files, e.g. ".arcane-trash-<name>-<unix>". Trash directories are
// Arcane-managed and must never be discovered or imported as user projects.
const ArcaneTrashPrefix = ".arcane-trash-"

// IsInternalScratchDirName reports whether name is any Arcane-managed scratch
// directory that must never be discovered or imported as a user project: the
// project-update preview/backup temp dirs, the quarantine/trash dirs (see
// ArcaneTrashPrefix), or the GitOps sync-stage/backup dirs (see
// IsGitOpsScratchDirName). This is the single source of truth for the discovery
// walker and the DB cleanup pass.
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
