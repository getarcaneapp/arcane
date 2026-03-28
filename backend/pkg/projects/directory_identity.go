package projects

import (
	"os"
	"path/filepath"
)

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
