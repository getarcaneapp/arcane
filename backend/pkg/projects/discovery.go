package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func DiscoverProjectDirectories(root string, followSymlinks bool) ([]DiscoveredProjectDir, error) {
	root = filepath.Clean(root)

	isDir, err := IsProjectDirectoryPath(root, followSymlinks)
	if err != nil {
		return nil, err
	}
	if !isDir {
		return nil, fmt.Errorf("project root is not a directory: %s", root)
	}

	discovered := make([]DiscoveredProjectDir, 0)
	ancestors := make(map[string]struct{})

	if err := walkProjectDirectoriesInternal(root, true, followSymlinks, ancestors, &discovered); err != nil {
		return nil, err
	}

	slices.SortStableFunc(discovered, func(a, b DiscoveredProjectDir) int {
		return strings.Compare(filepath.Clean(a.Path), filepath.Clean(b.Path))
	})

	return discovered, nil
}

func walkProjectDirectoriesInternal(path string, isRoot bool, followSymlinks bool, ancestors map[string]struct{}, discovered *[]DiscoveredProjectDir) error {
	identity, err := ResolveDirectoryIdentityInternal(path)
	if err != nil {
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

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		if !IsProjectDirectoryEntry(entry, childPath, followSymlinks) {
			continue
		}

		if err := walkProjectDirectoriesInternal(childPath, false, followSymlinks, ancestors, discovered); err != nil {
			return err
		}
	}

	return nil
}
