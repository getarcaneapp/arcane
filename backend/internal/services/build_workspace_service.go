package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"log/slog"

	volumetypes "github.com/getarcaneapp/arcane/types/volume"
)

const defaultBuildsDirectory = "/builds"

// BuildWorkspaceService provides file operations for the manual build workspace.
type BuildWorkspaceService struct {
	settings *SettingsService
}

func NewBuildWorkspaceService(settings *SettingsService) *BuildWorkspaceService {
	return &BuildWorkspaceService{settings: settings}
}

func (s *BuildWorkspaceService) ListDirectory(ctx context.Context, dirPath string) ([]volumetypes.FileEntry, error) {
	slog.DebugContext(ctx, "build workspace: list directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, err
	}

	cleaned, err := sanitizeBuildPath(dirPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	targetPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	results := make([]volumetypes.FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		entryPath := path.Join(cleaned, entry.Name())
		if entryPath == "" {
			entryPath = "/"
		}

		isSymlink := entry.Type()&os.ModeSymlink != 0
		mode := info.Mode().String()

		fileEntry := volumetypes.FileEntry{
			Name:        entry.Name(),
			Path:        entryPath,
			IsDirectory: info.IsDir(),
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			Mode:        mode,
			IsSymlink:   isSymlink,
		}

		if isSymlink {
			target, err := os.Readlink(filepath.Join(targetPath, entry.Name()))
			if err == nil && target != "" {
				fileEntry.LinkTarget = resolveLinkTarget(root, target)
			}
		}

		results = append(results, fileEntry)
	}

	return results, nil
}

func (s *BuildWorkspaceService) GetFileContent(ctx context.Context, filePath string, maxBytes int64) ([]byte, string, error) {
	slog.DebugContext(ctx, "build workspace: get file content", "path", filePath, "max_bytes", maxBytes)
	root, err := s.resolveRoot()
	if err != nil {
		return nil, "", err
	}

	cleaned, err := sanitizeBuildPath(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("invalid path: %w", err)
	}

	fullPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return nil, "", err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("path is a directory")
	}

	if maxBytes <= 0 {
		maxBytes = 1048576
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file: %w", err)
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

	cleaned, err := sanitizeBuildPath(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid path: %w", err)
	}

	fullPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return nil, 0, err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return nil, 0, fmt.Errorf("path is a directory")
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file: %w", err)
	}

	return file, info.Size(), nil
}

func (s *BuildWorkspaceService) UploadFile(ctx context.Context, destPath string, content io.Reader, filename string) error {
	slog.DebugContext(ctx, "build workspace: upload file", "dest_path", destPath, "filename", filename)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	safeFilename, err := sanitizeUploadFilename(filename)
	if err != nil {
		return err
	}

	cleaned, err := sanitizeBuildPath(destPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	dirPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	targetFile := filepath.Join(dirPath, safeFilename)
	file, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, content); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (s *BuildWorkspaceService) CreateDirectory(ctx context.Context, dirPath string) error {
	slog.DebugContext(ctx, "build workspace: create directory", "path", dirPath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := sanitizeBuildPath(dirPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	fullPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return err
	}

	if cleaned == "/" {
		return nil
	}

	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

func (s *BuildWorkspaceService) DeleteFile(ctx context.Context, filePath string) error {
	slog.DebugContext(ctx, "build workspace: delete path", "path", filePath)
	root, err := s.resolveRoot()
	if err != nil {
		return err
	}

	cleaned, err := sanitizeBuildPath(filePath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	if cleaned == "/" {
		return fmt.Errorf("cannot delete root directory")
	}

	fullPath, err := joinBuildRoot(root, cleaned)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(fullPath); err != nil {
		return fmt.Errorf("failed to delete path: %w", err)
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
		return "", fmt.Errorf("builds directory must be an absolute path")
	}

	cleaned := filepath.Clean(root)
	if err := os.MkdirAll(cleaned, 0o755); err != nil {
		return "", fmt.Errorf("failed to ensure builds directory: %w", err)
	}

	return cleaned, nil
}

func sanitizeBuildPath(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "/" {
		return "/", nil
	}

	cleaned := path.Clean(trimmed)
	if !path.IsAbs(cleaned) {
		cleaned = "/" + cleaned
	}
	if strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") || cleaned == "/.." {
		return "", fmt.Errorf("invalid path: path traversal not allowed")
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("invalid path: must be absolute")
	}

	return cleaned, nil
}

func sanitizeUploadFilename(filename string) (string, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "", fmt.Errorf("invalid filename")
	}

	// Reject any path separators (handle both Unix and Windows-style separators).
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("invalid filename: must not contain path separators")
	}

	// On Windows, disallow drive/volume prefixes (e.g. C: or \\server\share).
	if vol := filepath.VolumeName(name); vol != "" {
		return "", fmt.Errorf("invalid filename: must not include volume prefix")
	}

	base := filepath.Base(name)
	if base != name {
		return "", fmt.Errorf("invalid filename: must not contain path separators")
	}
	if base == "." || base == ".." {
		return "", fmt.Errorf("invalid filename")
	}

	return base, nil
}

func joinBuildRoot(root, cleaned string) (string, error) {
	rel := strings.TrimPrefix(cleaned, "/")
	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	if !isWithinRoot(root, fullPath) {
		return "", fmt.Errorf("invalid path: outside builds directory")
	}
	return fullPath, nil
}

func isWithinRoot(root, target string) bool {
	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(target)
	if targetClean == rootClean {
		return true
	}
	return strings.HasPrefix(targetClean, rootClean+string(os.PathSeparator))
}

func resolveLinkTarget(root, target string) string {
	if target == "" {
		return ""
	}
	if filepath.IsAbs(target) {
		targetClean := filepath.Clean(target)
		if isWithinRoot(root, targetClean) {
			rel, err := filepath.Rel(root, targetClean)
			if err != nil {
				return "(external)"
			}
			rel = filepath.ToSlash(rel)
			if rel == "." || rel == "" {
				return "/"
			}
			return "/" + rel
		}
		return "(external)"
	}

	return filepath.ToSlash(target)
}
