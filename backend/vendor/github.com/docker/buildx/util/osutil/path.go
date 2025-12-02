package osutil

import (
	"os"
	"path/filepath"
)

// GetWd retrieves the current working directory.
//
// On Windows, this function will return the long path name
// version of the path.
func GetWd() string {
	wd, _ := os.Getwd()
	if lp, err := GetLongPathName(wd); err == nil {
		return lp
	}
	return wd
}

func IsLocalDir(c string) bool {
	st, err := os.Stat(c)
	return err == nil && st.IsDir()
}

func ToAbs(path string) string {
	if !filepath.IsAbs(path) {
		path, _ = filepath.Abs(filepath.Join(GetWd(), path))
	}
	return SanitizePath(path)
}
