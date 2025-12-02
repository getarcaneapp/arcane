//go:build !windows
// +build !windows

package osutil

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GetLongPathName is a no-op on non-Windows platforms.
func GetLongPathName(path string) (string, error) {
	return path, nil
}

var windowsPathRegex = regexp.MustCompile(`^[A-Za-z]:[\\/].*$`)

func SanitizePath(path string) string {
	// If we're running in WSL, we need to convert Windows paths to Unix paths.
	// This is because the git binary can be invoked through `git.exe` and
	// therefore returns Windows paths.
	if os.Getenv("WSL_DISTRO_NAME") != "" && windowsPathRegex.MatchString(path) {
		unixPath := strings.ReplaceAll(path, "\\", "/")
		drive := strings.ToLower(string(unixPath[0]))
		rest := filepath.Clean(unixPath[3:])
		return filepath.Join("/mnt", drive, rest)
	}
	return filepath.Clean(path)
}
