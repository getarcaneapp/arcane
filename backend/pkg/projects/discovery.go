package projects

import "os"

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
