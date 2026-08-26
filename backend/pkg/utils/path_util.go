package utils

import (
	"hash"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"emperror.dev/errors"
)

// SQLitePathFromDSN returns the filesystem portion of a SQLite file DSN.
// For "file:data/arcane.db?..." the path is carried in Opaque; for
// "file:/abs/path.db" it is in Path. In-memory DSNs yield ":memory:" or "".
func SQLitePathFromDSN(dsn string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if parsed.Opaque != "" {
		return parsed.Opaque, nil
	}
	return parsed.Path, nil
}

// NormalizeRelativePath validates and normalizes a slash-delimited path rooted
// at a managed file tree. The returned path never begins with a slash.
func NormalizeRelativePath(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("path contains a null byte")
	}
	if strings.Contains(trimmed, "\\") {
		return "", errors.New("path must use forward slashes")
	}
	if path.IsAbs(trimmed) || filepath.IsAbs(trimmed) {
		return "", errors.New("absolute paths are not allowed")
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path traversal is not allowed")
	}
	for segment := range strings.SplitSeq(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("path contains an invalid segment")
		}
	}

	return cleaned, nil
}

// ValidateFileName validates a single file-tree path segment.
func ValidateFileName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("name is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("name contains a null byte")
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", errors.New("name must not contain path separators")
	}
	if filepath.VolumeName(trimmed) != "" {
		return "", errors.New("name must not contain a volume prefix")
	}
	if trimmed == "." || trimmed == ".." || filepath.Base(trimmed) != trimmed {
		return "", errors.New("invalid name")
	}
	return trimmed, nil
}

func FilePathMatches(relativePath, rootPath string) bool {
	return relativePath == rootPath || strings.HasPrefix(relativePath, rootPath+"/")
}

func WriteFileTreeRevisionEntry(h hash.Hash, relativePath, kind string, size, modTimeUnixNano int64, mode string, protected bool) {
	protectedFlag := ""
	if protected {
		protectedFlag = "protected"
	}
	entry := strings.Join([]string{
		relativePath,
		kind,
		strconv.FormatInt(size, 10),
		strconv.FormatInt(modTimeUnixNano, 10),
		mode,
		protectedFlag,
	}, "\x00")
	_, _ = io.WriteString(h, entry)
	_, _ = h.Write([]byte{'\n'})
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
