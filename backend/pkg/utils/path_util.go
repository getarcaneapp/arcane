package utils

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"emperror.dev/errors"
)

// IsWithinRoot reports whether target is root or lies beneath it. Both paths are
// cleaned first; neither is resolved, so this only proves the *spelling* of the
// path stays inside root. Use ResolveWithinRoot when a symlink could redirect it.
func IsWithinRoot(root, target string) bool {
	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(target)
	if targetClean == rootClean {
		return true
	}
	return strings.HasPrefix(targetClean, rootClean+string(os.PathSeparator))
}

// ResolveWithinRoot resolves symlinks in both root and target and returns the
// resolved target, verifying it stays inside root. A target that does not exist
// yet is allowed: its parent is resolved instead, so create/write paths work.
//
// This is the check that makes containment real. A lexical test alone accepts a
// symlink that sits inside root but points at /etc, which is then read, written
// or removed through as if it were an in-root file.
func ResolveWithinRoot(root, target string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", errors.WrapIff(err, "failed to resolve root directory %q", root)
	}

	cleanTarget := filepath.Clean(target)
	resolved, err := filepath.EvalSymlinks(cleanTarget)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", errors.WrapIff(err, "failed to resolve path %q", target)
		}
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(cleanTarget))
		if parentErr != nil {
			return "", errors.WrapIff(parentErr, "failed to resolve parent directory of %q", target)
		}
		resolved = filepath.Join(resolvedParent, filepath.Base(cleanTarget))
	}

	if !IsWithinRoot(resolvedRoot, resolved) {
		return "", errors.Errorf("path %q resolves outside %q", target, root)
	}
	return resolved, nil
}

// SanitizeBrowsePath normalizes a path within a rooted file browser.
func SanitizeBrowsePath(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "/" {
		return "/", nil
	}

	cleaned := path.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid path: path traversal not allowed")
	}
	if !path.IsAbs(cleaned) {
		cleaned = path.Clean("/" + cleaned)
	}
	if strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") || cleaned == "/.." {
		return "", errors.New("invalid path: path traversal not allowed")
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "", errors.New("invalid path: must be absolute")
	}

	return cleaned, nil
}
