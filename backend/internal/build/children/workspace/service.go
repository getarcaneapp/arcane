package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/workspace"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"

	acfsutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/acfs"
)

const defaultBuildsDirectory = "/builds"

// Service provides file operations for the manual build workspace.
type Service struct {
	buildsDirectory func() string
}

// NewService builds the workspace service; buildsDirectory reports the configured workspace root.
func NewService(buildsDirectory func() string) *Service {
	return &Service{buildsDirectory: buildsDirectory}
}

func (s *Service) ListDirectory(ctx context.Context, dirPath string) ([]workspace.FileEntry, error) {
	slog.DebugContext(ctx, "build workspace: list directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, err
	}

	cleaned, err := kit.SanitizeBrowsePath(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	entries, err := acfs.List(ctx, root, cleaned)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	results := make([]workspace.FileEntry, 0, len(entries))
	for _, entry := range entries {
		results = append(results, acfsutils.FileEntry(entry))
	}

	return results, nil
}

func (s *Service) GetFileContent(ctx context.Context, filePath string, maxBytes int64) ([]byte, string, error) {
	slog.DebugContext(ctx, "build workspace: get file content", "path", filePath, "maxBytes", maxBytes)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, "", err
	}

	cleaned, err := kit.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid path: %w", err)
	}

	if maxBytes <= 0 {
		maxBytes = 1048576
	}

	file, _, err := acfs.OpenRead(ctx, root, cleaned, maxBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file: %w", err)
	}

	mimeType := http.DetectContentType(content)
	return content, mimeType, nil
}

func (s *Service) DownloadFile(ctx context.Context, filePath string) (io.ReadCloser, int64, error) {
	slog.DebugContext(ctx, "build workspace: download file", "path", filePath)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, 0, err
	}

	cleaned, err := kit.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid path: %w", err)
	}

	file, size, err := acfs.OpenRead(ctx, root, cleaned, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file: %w", err)
	}

	return file, size, nil
}

func (s *Service) UploadFile(ctx context.Context, destPath string, content io.Reader, filename string, size int64) error {
	slog.DebugContext(ctx, "build workspace: upload file", "destPath", destPath, "filename", filename)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	safeFilename, err := sanitizeUploadFilenameInternal(filename)
	if err != nil {
		return err
	}

	cleaned, err := kit.SanitizeBrowsePath(destPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	if mkdirAllErr := acfs.MkdirAll(ctx, root, cleaned, 0o755); mkdirAllErr != nil {
		return fmt.Errorf("failed to create directory: %w", mkdirAllErr)
	}

	targetFile := path.Join(cleaned, safeFilename)
	if _, writeFromErr := acfs.WriteFrom(ctx, root, targetFile, content, size, 0o644); writeFromErr != nil {
		return fmt.Errorf("failed to write file: %w", writeFromErr)
	}

	return nil
}

func (s *Service) CreateDirectory(ctx context.Context, dirPath string) error {
	slog.DebugContext(ctx, "build workspace: create directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := kit.SanitizeBrowsePath(dirPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	if cleaned == "/" {
		return nil
	}

	if mkdirAllErr := acfs.MkdirAll(ctx, root, cleaned, 0o755); mkdirAllErr != nil {
		return fmt.Errorf("failed to create directory: %w", mkdirAllErr)
	}

	return nil
}

func (s *Service) DeleteFile(ctx context.Context, filePath string) error {
	slog.DebugContext(ctx, "build workspace: delete path", "path", filePath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := kit.SanitizeBrowsePath(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	if cleaned == "/" {
		return errors.New("cannot delete root directory")
	}

	if removeAllErr := acfs.RemoveAll(ctx, root, cleaned); removeAllErr != nil {
		return fmt.Errorf("failed to delete path: %w", removeAllErr)
	}

	return nil
}

func (s *Service) resolveRoot() (string, error) {
	if s.buildsDirectory == nil {
		return "", errors.New("settings service not available")
	}

	root := cmp.Or(strings.TrimSpace(s.buildsDirectory()), defaultBuildsDirectory)

	if !filepath.IsAbs(root) {
		return "", errors.New("builds directory must be an absolute path")
	}

	cleaned := filepath.Clean(root)
	if err := os.MkdirAll(cleaned, 0o755); err != nil {
		return "", fmt.Errorf("failed to ensure builds directory: %w", err)
	}

	return cleaned, nil
}

func sanitizeUploadFilenameInternal(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "", errors.New("invalid filename")
	}

	// Reject any path separators (handle both Unix and Windows-style separators).
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("invalid filename: must not contain path separators")
	}

	// On Windows, disallow drive/volume prefixes (e.g. C: or \\server\share).
	if vol := filepath.VolumeName(name); vol != "" {
		return "", errors.New("invalid filename: must not include volume prefix")
	}

	base := filepath.Base(name)
	if base != name {
		return "", errors.New("invalid filename: must not contain path separators")
	}
	if base == "." || base == ".." {
		return "", errors.New("invalid filename")
	}

	return base, nil
}
