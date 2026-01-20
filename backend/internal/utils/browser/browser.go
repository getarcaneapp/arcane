package browser

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	containertypes "github.com/getarcaneapp/arcane/types/container"
)

// MaxFilesPerDirectory is the maximum number of files to return from a directory listing
// to prevent memory issues and slow responses for directories with many files.
const MaxFilesPerDirectory = 1000

// MaxFileSize is the maximum file size to read (1MB).
const MaxFileSize = 1024 * 1024

// MaxExecOutputSize is the maximum size of output to read from exec commands (1MB).
const MaxExecOutputSize = 1024 * 1024

// ParseLsOutput parses the output of ls -la command.
func ParseLsOutput(output, basePath string, limit int) []containertypes.FileEntry {
	var files []containertypes.FileEntry
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		// Check limit before processing more lines
		if limit > 0 && len(files) >= limit {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total") {
			continue
		}

		entry := parseLsLine(line, basePath)
		if entry != nil && entry.Name != "." && entry.Name != ".." {
			files = append(files, *entry)
		}
	}

	return files
}

// parseLsLine parses a single line from ls -la output.
func parseLsLine(line, basePath string) *containertypes.FileEntry {
	// Format: -rw-r--r-- 1 root root 1234 2024-01-15T10:30:00 filename
	// Or with symlink: lrwxrwxrwx 1 root root 12 2024-01-15T10:30:00 link -> target

	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil
	}

	mode := fields[0]
	// Size is at index 4 (after permissions, links, user, group)
	sizeStr := fields[4]
	// Date is at index 5
	modTime := fields[5]

	// Name starts at index 6, but may contain spaces
	nameIdx := 6
	if len(fields) <= nameIdx {
		return nil
	}

	name := strings.Join(fields[nameIdx:], " ")
	linkTarget := ""

	// Check for symlink arrow
	if arrowIdx := strings.Index(name, " -> "); arrowIdx != -1 {
		linkTarget = name[arrowIdx+4:]
		name = name[:arrowIdx]
	}

	// Determine file type from mode
	var fileType containertypes.FileEntryType
	switch mode[0] {
	case 'd':
		fileType = containertypes.FileEntryTypeDirectory
	case 'l':
		fileType = containertypes.FileEntryTypeSymlink
	default:
		fileType = containertypes.FileEntryTypeFile
	}

	// Parse size
	size, _ := parseInt64(sizeStr)

	// Build full path
	fullPath := basePath
	if !strings.HasSuffix(basePath, "/") {
		fullPath += "/"
	}
	fullPath += name

	return &containertypes.FileEntry{
		Name:       name,
		Path:       fullPath,
		Type:       fileType,
		Size:       size,
		Mode:       mode,
		ModTime:    modTime,
		LinkTarget: linkTarget,
	}
}

// parseInt64 parses a string to int64, returning 0 on error.
func parseInt64(s string) (int64, error) {
	var result int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid number: %s", s)
		}
		result = result*10 + int64(c-'0')
	}
	return result, nil
}

// ParseTarDirectory reads a TAR stream and extracts only the immediate children of the directory.
func ParseTarDirectory(tarStream io.Reader, basePath string, limit int) ([]containertypes.FileEntry, error) {
	tr := tar.NewReader(tarStream)
	filesMap := make(map[string]containertypes.FileEntry)

	// Normalize base path
	basePath = path.Clean(basePath)
	if basePath == "." {
		basePath = ""
	}

	for {
		// Check limit before reading more entries
		if limit > 0 && len(filesMap) >= limit {
			break
		}

		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// The TAR from Docker includes the directory name as the root
		// e.g., for /etc/, entries are like "etc/passwd", "etc/hosts"
		entryPath := header.Name

		// Remove trailing slash for directories
		entryPath = strings.TrimSuffix(entryPath, "/")

		// Skip the root directory itself
		if entryPath == "" || entryPath == "." {
			continue
		}

		// Count path depth - we only want immediate children (depth 1)
		parts := strings.Split(entryPath, "/")

		// Skip if this is a nested file (depth > 1)
		// The first part is the directory name itself
		if len(parts) > 2 {
			continue
		}

		// Get the actual file/directory name
		var name string
		if len(parts) == 1 {
			name = parts[0]
		} else {
			name = parts[1]
		}

		// Skip . and ..
		if name == "." || name == ".." || name == "" {
			continue
		}

		// Skip if we've already seen this entry (can happen with directory entries)
		if _, exists := filesMap[name]; exists {
			continue
		}

		// Determine file type
		var fileType containertypes.FileEntryType
		switch header.Typeflag {
		case tar.TypeDir:
			fileType = containertypes.FileEntryTypeDirectory
		case tar.TypeSymlink:
			fileType = containertypes.FileEntryTypeSymlink
		default:
			fileType = containertypes.FileEntryTypeFile
		}

		// Build the full path
		fullPath := basePath
		if fullPath != "/" && fullPath != "" {
			fullPath += "/"
		} else if fullPath == "" {
			fullPath = "/"
		}
		fullPath += name

		// Convert file mode to string
		mode := header.FileInfo().Mode().String()

		filesMap[name] = containertypes.FileEntry{
			Name:       name,
			Path:       fullPath,
			Type:       fileType,
			Size:       header.Size,
			Mode:       mode,
			ModTime:    header.ModTime.Format("2006-01-02T15:04:05"),
			LinkTarget: header.Linkname,
		}
	}

	// Convert map to slice
	files := make([]containertypes.FileEntry, 0, len(filesMap))
	for _, entry := range filesMap {
		files = append(files, entry)
	}

	return files, nil
}

// ReadFileContentFromTar reads file content from a TAR stream.
// Returns the content, file size, whether it's binary, whether it was truncated, and any error.
func ReadFileContentFromTar(tarStream io.Reader, fileSize int64) (content string, isBinary bool, truncated bool, err error) {
	truncated = fileSize > MaxFileSize

	// Read the TAR to get file content
	tr := tar.NewReader(tarStream)
	header, err := tr.Next()
	if err != nil {
		return "", false, false, fmt.Errorf("failed to read file from archive: %w", err)
	}

	// Handle symlinks - return the link target as content
	if header.Typeflag == tar.TypeSymlink {
		return header.Linkname, false, false, nil
	}

	// Determine how much to read
	readSize := fileSize
	if truncated {
		readSize = MaxFileSize
	}

	// Read file content
	contentBytes := make([]byte, readSize)
	n, err := io.ReadFull(tr, contentBytes)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", false, false, fmt.Errorf("failed to read file content: %w", err)
	}
	contentBytes = contentBytes[:n]

	// Check if content is binary (contains null bytes or non-printable characters)
	isBinary = IsBinaryContent(contentBytes)

	return string(contentBytes), isBinary, truncated, nil
}

// IsBinaryContent checks if the content contains binary data.
func IsBinaryContent(content []byte) bool {
	for _, b := range content {
		if b == 0 || (b < 32 && b != '\n' && b != '\r' && b != '\t') {
			return true
		}
	}
	return false
}
