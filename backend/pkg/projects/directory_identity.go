package projects

import (
	"os"
	"path/filepath"
)

// ResolveDirectoryIdentityInternal stays on os.*: it resolves symlinks that
// may point anywhere on the host to establish directory identity, which the
// root-confined acfs API cannot do.
func ResolveDirectoryIdentityInternal(path string) (string, error) {
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
