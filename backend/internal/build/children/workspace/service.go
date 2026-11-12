package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/workspace"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		settingsService *settings.SettingsService
	}
)

const (
	defaultBuildsDirectory = "/builds"
)

func NewService(settingsService *settings.SettingsService) *Service {
	return &Service{settingsService: settingsService}
}

func (s *Service) Read(ctx context.Context) (*types.Workspace, error) {
	root, err := s.resolveRoot(ctx)
	if err != nil {
		return nil, err
	}
	files, revision, truncated, err := workspace.Read(ctx, root, s.options(ctx, ""))
	if err != nil {
		return nil, classifyWorkspaceError(fmt.Errorf("read build workspace: %w", err))
	}
	return &types.Workspace{
		Files:             files,
		FileTreeRevision:  revision,
		FileTreeTruncated: truncated,
	}, nil
}

func (s *Service) File(ctx context.Context, relativePath string) (*types.WorkspaceFileContent, error) {
	root, rel, entry, err := s.resolveFile(ctx, relativePath)
	if err != nil {
		return nil, err
	}
	file, err := workspace.ReadFile(ctx, root, rel, entry, workspace.MaxFileSizeBytes(s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")))
	if err != nil {
		return nil, classifyWorkspaceError(err)
	}
	return &file, nil
}

func (s *Service) Download(ctx context.Context, relativePath string) (io.ReadCloser, int64, string, error) {
	root, rel, _, err := s.resolveFile(ctx, relativePath)
	if err != nil {
		return nil, 0, "", err
	}
	file, size, err := acfs.Root{Path: root}.OpenRead(ctx, "/"+rel, 0)
	if err != nil {
		return nil, 0, "", classifyWorkspaceError(fmt.Errorf("open build workspace file: %w", err))
	}
	return file, size, path.Base(rel), nil
}

func (s *Service) Update(ctx context.Context, manifest types.WorkspaceUpdateManifest, uploads map[int][]byte) (*types.Workspace, error) {
	slog.DebugContext(ctx, "build workspace: update", "fileChangeCount", len(manifest.FileChanges))
	if err := workspace.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrBadRequest, err)
	}
	root, err := s.resolveRoot(ctx)
	if err != nil {
		return nil, err
	}
	if applyErr := workspace.Apply(ctx, root, manifest.FileChanges, uploads, s.options(ctx, strings.TrimSpace(manifest.FileTreeRevision))); applyErr != nil {
		return nil, classifyApplyError(applyErr)
	}
	return s.Read(ctx)
}

func (s *Service) Upload(ctx context.Context, parentPath, filename string, content io.Reader, size int64) (*types.Workspace, error) {
	slog.DebugContext(ctx, "build workspace: upload", "parentPath", parentPath, "filename", filename)
	parent, err := workspace.NormalizeParentPath(parentPath)
	if err != nil {
		return nil, common.Classify(common.ErrForbidden, fmt.Errorf("invalid build workspace path: %w", err))
	}
	name, err := kit.ValidateFileName(filename)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid build workspace file name: %w", err))
	}
	root, err := s.resolveRoot(ctx)
	if err != nil {
		return nil, err
	}
	if writeErr := workspace.WriteFile(ctx, root, path.Join(parent, name), content, size); writeErr != nil {
		return nil, classifyApplyError(writeErr)
	}
	return s.Read(ctx)
}

func (s *Service) options(ctx context.Context, expectedRevision string) workspace.Options {
	return workspace.Options{
		ExpectedRevision: expectedRevision,
		SkipDirectories:  s.settingsService.GetStringSetting(ctx, "projectScanSkipDirs"),
		MaxDepth:         s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxDepth"),
		MaxEntries:       s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxEntries"),
		MaxFileSizeBytes: workspace.MaxFileSizeBytes(s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")),
	}
}

func (s *Service) resolveFile(ctx context.Context, relativePath string) (string, string, acfstypes.Entry, error) {
	root, err := s.resolveRoot(ctx)
	if err != nil {
		return "", "", acfstypes.Entry{}, err
	}
	rel, err := kit.NormalizeRelativePath(relativePath)
	if err != nil {
		return "", "", acfstypes.Entry{}, common.Classify(common.ErrForbidden, fmt.Errorf("invalid build workspace path: %w", err))
	}
	entry, err := acfs.Root{Path: root}.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		return "", "", acfstypes.Entry{}, classifyWorkspaceError(fmt.Errorf("inspect build workspace file: %w", err))
	}
	if entry.IsDirectory {
		return "", "", acfstypes.Entry{}, common.Classify(common.ErrBadRequest, errors.New("workspace path is a directory"))
	}
	return root, rel, entry, nil
}

func (s *Service) resolveRoot(ctx context.Context) (string, error) {
	root := cmp.Or(strings.TrimSpace(s.settingsService.GetSettingsConfig().BuildsDirectory.Value), defaultBuildsDirectory)

	if err := s.settingsService.ValidateBuildsDirectory(root); err != nil {
		return "", common.Classify(common.ErrBadRequest, err)
	}

	cleaned := filepath.Clean(root)
	if err := (acfs.Host{}).MkdirAll(ctx, cleaned, 0o755); err != nil {
		return "", fmt.Errorf("failed to ensure builds directory: %w", err)
	}

	return cleaned, nil
}

func classifyWorkspaceError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return common.Classify(common.ErrNotFound, err)
	case errors.Is(err, fs.ErrExist), errors.Is(err, acfs.ErrAlreadyExists):
		return common.Classify(common.ErrConflict, err)
	case errors.Is(err, acfs.ErrInvalidPath), errors.Is(err, acfs.ErrOutsideRoot),
		errors.Is(err, acfs.ErrSymlink), errors.Is(err, acfs.ErrSymlinkLoop):
		return common.Classify(common.ErrForbidden, err)
	case errors.Is(err, acfs.ErrIsDirectory), errors.Is(err, acfs.ErrNotDirectory), errors.Is(err, acfs.ErrNotFile),
		errors.Is(err, syscall.ENOTDIR), errors.Is(err, acfs.ErrRootRemoval):
		return common.Classify(common.ErrBadRequest, err)
	default:
		return err
	}
}

func classifyApplyError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrRevisionConflict), errors.Is(err, acfs.ErrAlreadyExists), errors.Is(err, acfs.ErrNotEmpty):
		return common.Classify(common.ErrConflict, err)
	case errors.Is(err, workspace.ErrSymlinkPath), errors.Is(err, acfs.ErrOutsideRoot), errors.Is(err, acfs.ErrInvalidPath),
		errors.Is(err, acfs.ErrSymlink), errors.Is(err, acfs.ErrSymlinkLoop):
		return common.Classify(common.ErrForbidden, err)
	default:
		return common.Classify(common.ErrBadRequest, err)
	}
}
