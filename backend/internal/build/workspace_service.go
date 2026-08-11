package build

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"emperror.dev/errors"

	"log/slog"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	acfsutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/acfs"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"go.getarcane.app/acfs"
)

const defaultBuildsDirectory = "/builds"

// BuildWorkspaceService provides file operations for the manual build workspace.
type BuildWorkspaceService struct {
	settings *settings.SettingsService
}

func NewBuildWorkspaceService(settings *settings.SettingsService) *BuildWorkspaceService {
	return &BuildWorkspaceService{settings: settings}
}

func (s *BuildWorkspaceService) ListDirectory(ctx context.Context, dirPath string) ([]workspacetypes.FileEntry, error) {
	slog.DebugContext(ctx, "build workspace: list directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, err
	}

	cleaned, err := utils.SanitizeBrowsePath(dirPath)
	if err != nil {
		return nil, errors.WrapIf(err, "invalid path")
	}

	entries, err := acfs.List(ctx, root, cleaned)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list directory")
	}

	results := make([]workspacetypes.FileEntry, 0, len(entries))
	for _, entry := range entries {
		results = append(results, acfsutils.FileEntry(entry))
	}

	return results, nil
}

func (s *BuildWorkspaceService) GetFileContent(ctx context.Context, filePath string, maxBytes int64) ([]byte, string, error) {
	slog.DebugContext(ctx, "build workspace: get file content", "path", filePath, "max_bytes", maxBytes)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, "", err
	}

	cleaned, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, "", errors.WrapIf(err, "invalid path")
	}

	if maxBytes <= 0 {
		maxBytes = 1048576
	}

	file, _, err := acfs.OpenRead(ctx, root, cleaned, maxBytes)
	if err != nil {
		return nil, "", errors.WrapIf(err, "failed to open file")
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, "", errors.WrapIf(err, "failed to read file")
	}

	mimeType := http.DetectContentType(content)
	return content, mimeType, nil
}

func (s *BuildWorkspaceService) DownloadFile(ctx context.Context, filePath string) (io.ReadCloser, int64, error) {
	slog.DebugContext(ctx, "build workspace: download file", "path", filePath)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, 0, err
	}

	cleaned, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return nil, 0, errors.WrapIf(err, "invalid path")
	}

	file, size, err := acfs.OpenRead(ctx, root, cleaned, 0)
	if err != nil {
		return nil, 0, errors.WrapIf(err, "failed to open file")
	}

	return file, size, nil
}

func (s *BuildWorkspaceService) UploadFile(ctx context.Context, destPath string, content io.Reader, filename string, size int64) error {
	slog.DebugContext(ctx, "build workspace: upload file", "dest_path", destPath, "filename", filename)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	safeFilename, err := sanitizeUploadFilenameInternal(filename)
	if err != nil {
		return err
	}

	cleaned, err := utils.SanitizeBrowsePath(destPath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}

	if err := acfs.MkdirAll(ctx, root, cleaned, 0o755); err != nil {
		return errors.WrapIf(err, "failed to create directory")
	}

	targetFile := path.Join(cleaned, safeFilename)
	if _, err := acfs.WriteFrom(ctx, root, targetFile, content, size, 0o644); err != nil {
		return errors.WrapIf(err, "failed to write file")
	}

	return nil
}

func (s *BuildWorkspaceService) CreateDirectory(ctx context.Context, dirPath string) error {
	slog.DebugContext(ctx, "build workspace: create directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := utils.SanitizeBrowsePath(dirPath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}

	if cleaned == "/" {
		return nil
	}

	if err := acfs.MkdirAll(ctx, root, cleaned, 0o755); err != nil {
		return errors.WrapIf(err, "failed to create directory")
	}

	return nil
}

func (s *BuildWorkspaceService) DeleteFile(ctx context.Context, filePath string) error {
	slog.DebugContext(ctx, "build workspace: delete path", "path", filePath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := utils.SanitizeBrowsePath(filePath)
	if err != nil {
		return errors.WrapIf(err, "invalid path")
	}

	if cleaned == "/" {
		return errors.New("cannot delete root directory")
	}

	if err := acfs.RemoveAll(ctx, root, cleaned); err != nil {
		return errors.WrapIf(err, "failed to delete path")
	}

	return nil
}

func (s *BuildWorkspaceService) resolveRoot() (string, error) {
	if s.settings == nil {
		return "", errors.New("settings service not available")
	}

	root := strings.TrimSpace(s.settings.GetSettingsConfig().BuildsDirectory.Value)
	if root == "" {
		root = defaultBuildsDirectory
	}

	if !filepath.IsAbs(root) {
		return "", errors.New("builds directory must be an absolute path")
	}

	cleaned := filepath.Clean(root)
	if err := os.MkdirAll(cleaned, 0o755); err != nil {
		return "", errors.WrapIf(err, "failed to ensure builds directory")
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
