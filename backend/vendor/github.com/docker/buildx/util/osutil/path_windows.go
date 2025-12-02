package osutil

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// GetLongPathName converts Windows short pathnames to full pathnames.
// For example C:\Users\ADMIN~1 --> C:\Users\Administrator.
func GetLongPathName(path string) (string, error) {
	// See https://groups.google.com/forum/#!topic/golang-dev/1tufzkruoTg
	p, err := windows.UTF16FromString(path)
	if err != nil {
		return "", err
	}
	b := p // GetLongPathName says we can reuse buffer
	n, err := windows.GetLongPathName(&p[0], &b[0], uint32(len(b)))
	if err != nil {
		return "", err
	}
	if n > uint32(len(b)) {
		b = make([]uint16, n)
		_, err = windows.GetLongPathName(&p[0], &b[0], uint32(len(b)))
		if err != nil {
			return "", err
		}
	}
	return windows.UTF16ToString(b), nil
}

func SanitizePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}
