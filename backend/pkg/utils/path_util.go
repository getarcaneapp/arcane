package utils

import (
	"errors"
	"path"
	"strings"
)

// SanitizeBrowsePath normalizes a path within a rooted file browser.
func SanitizeBrowsePath(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "/" {
		return "/", nil
	}

	cleaned := path.Clean(trimmed)
	if !path.IsAbs(cleaned) {
		cleaned = "/" + cleaned
	}
	if strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") || cleaned == "/.." {
		return "", errors.New("invalid path: path traversal not allowed")
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "", errors.New("invalid path: must be absolute")
	}

	return cleaned, nil
}
