package workspace

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/projects"
	workspacepkg "getarcane.dev/arcane/v3/pkg/workspace"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		settingsService *settings.SettingsService
	}
)

func New(settingsService *settings.SettingsService) *Service {
	return &Service{settingsService: settingsService}
}

func (s *Service) Read(ctx context.Context, projectPath, composeFileName, folderPath string, owned map[string]struct{}) (*types.Workspace, error) {
	rel, err := resolveFolder(ctx, projectPath, composeFileName, folderPath)
	if err != nil {
		return nil, err
	}
	folder, err := workspacepkg.ListFolder(ctx, projectPath, rel, workspacepkg.Options{
		ProtectedFiles:   projects.ProtectedProjectFilePaths(composeFileName),
		SkipDirectories:  s.settingsService.GetStringSetting(ctx, "projectScanSkipDirs"),
		MaxEntries:       s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxEntries"),
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")),
	})
	if err != nil {
		return nil, classifyACFSError(err, "list project workspace folder")
	}
	for i, file := range folder.Files {
		if _, ok := owned[file.RelativePath]; ok {
			folder.Files[i] = types.WorkspaceFileEntry{ //exhaustruct:enforce
				ModTime:        file.ModTime,
				Name:           file.Name,
				Path:           file.Path,
				RelativePath:   file.RelativePath,
				Mode:           file.Mode,
				LinkTarget:     file.LinkTarget,
				Size:           file.Size,
				IsDirectory:    file.IsDirectory,
				IsSymlink:      file.IsSymlink,
				Editable:       false,
				ReadOnlyReason: types.FileReadOnlyGitOpsManaged,
			}
		}
	}
	return folder, nil
}

func (s *Service) File(ctx context.Context, projectPath, composeFileName, relativePath string, owned map[string]struct{}) (*types.WorkspaceFileContent, error) {
	rel, entry, err := resolvePath(ctx, projectPath, composeFileName, relativePath)
	if err != nil {
		return nil, err
	}
	if entry.IsDirectory {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	file, err := workspacepkg.ReadFile(ctx, projectPath, rel, entry, workspacepkg.MaxFileSizeBytes(s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")))
	if err != nil {
		return nil, classifyACFSError(err, "read project workspace file")
	}
	if _, ok := owned[rel]; ok && file.Editable {
		// Sync-owned files stay viewable but read-only: git is their source of truth (#3634).
		return new(workspacepkg.ReadOnlyFile(file, types.FileReadOnlyGitOpsManaged, file.MimeType)), nil
	}
	return &file, nil
}

func (s *Service) Download(ctx context.Context, projectPath, composeFileName, relativePath string) (io.ReadCloser, int64, string, error) {
	rel, entry, err := resolvePath(ctx, projectPath, composeFileName, relativePath)
	if err != nil {
		return nil, 0, "", err
	}
	if entry.IsDirectory {
		return nil, 0, "", common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	file, size, err := acfs.Root{Path: projectPath}.OpenRead(ctx, "/"+rel, 0)
	if err != nil {
		return nil, 0, "", classifyACFSError(err, "open project workspace file")
	}
	return file, size, filepath.Base(rel), nil
}

func (s *Service) Apply(
	ctx context.Context,
	projectsDirectory, projectPath, composeFileName string,
	manifest types.ProjectWorkspaceUpdateManifest,
	uploads map[int][]byte,
) error {
	var paths []string
	for _, change := range manifest.FileChanges {
		paths = append(paths, ChangeTargetPaths(change)...)
	}
	backup, cleanup, err := projects.BackupProjectDirectory(ctx, projectsDirectory, projectPath, ".project-update-backup-*", projects.ProjectUpdateBackupScope{Paths: paths})
	if err != nil {
		return err
	}
	defer cleanup()

	if applyErr := workspacepkg.Apply(ctx, projectPath, manifest.FileChanges, uploads, workspacepkg.Options{
		ProtectedFiles:   projects.ProtectedProjectFilePaths(composeFileName),
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")),
	}); applyErr != nil {
		if restoreErr := projects.RestoreProjectUpdateBackup(ctx, projectPath, backup); restoreErr != nil {
			return errors.Join(WrapProjectWorkspaceError(applyErr), fmt.Errorf("rollback project workspace: %w", restoreErr))
		}
		return WrapProjectWorkspaceError(applyErr)
	}
	return nil
}

func OwnedPaths(syncedFiles *string, composePath string) (map[string]struct{}, error) {
	owned := make(map[string]struct{})
	add := func(p string) {
		if rel, normalizeRelativePathErr := kit.NormalizeRelativePath(p); normalizeRelativePathErr == nil {
			owned[rel] = struct{}{}
		}
	}
	if kit.FromPtr(syncedFiles) != "" {
		var files []string
		if unmarshalErr := json.Unmarshal([]byte(*syncedFiles), &files); unmarshalErr != nil {
			return nil, fmt.Errorf("parse gitops synced files for workspace: %w", unmarshalErr)
		}
		for _, f := range files {
			add(f)
		}
	}
	if composePath != "" {
		add(filepath.Base(composePath))
	}
	return owned, nil
}

func resolveFolder(ctx context.Context, projectPath, composeFileName, folderPath string) (string, error) {
	if strings.TrimSpace(folderPath) == "" {
		return "", nil
	}
	rel, entry, err := resolvePath(ctx, projectPath, composeFileName, folderPath)
	if err != nil {
		return "", err
	}
	// Stat follows intermediate symlinks, so a different path means one was followed.
	switch {
	case entry.IsSymlink || entry.Path != "/"+rel:
		return "", common.Classify(common.ErrProjectWorkspaceForbidden, workspacepkg.ErrSymlinkPath)
	case !entry.IsDirectory:
		return "", common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is not a folder"))
	}
	return rel, nil
}

func resolvePath(ctx context.Context, projectPath, composeFileName, relativePath string) (string, acfstypes.Entry, error) {
	rel, err := kit.NormalizeRelativePath(relativePath)
	if err != nil {
		return "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceForbidden, fmt.Errorf("invalid project workspace path: %w", err))
	}
	rootName, _, _ := strings.Cut(rel, "/")
	protected := workspacepkg.ProtectedPaths(ctx, projectPath, projects.ProtectedProjectFilePaths(composeFileName))
	if protected[rel] || protected[rootName] {
		return "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceForbidden, errors.New("project configuration is not part of the workspace"))
	}
	entry, err := acfs.Root{Path: projectPath}.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceNotFound, errors.New("project workspace file not found"))
		}
		return "", acfstypes.Entry{}, classifyACFSError(err, "inspect project workspace file")
	}
	return rel, entry, nil
}

func WrapProjectWorkspaceError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, workspacepkg.ErrRevisionConflict):
		return common.Classify(common.ErrProjectWorkspaceConflict, err)
	case errors.Is(err, acfs.ErrAlreadyExists), errors.Is(err, acfs.ErrNotEmpty):
		return common.Classify(common.ErrProjectWorkspaceConflict, fmt.Errorf("conflicting project workspace path: %w", err))
	case errors.Is(err, workspacepkg.ErrProtectedPath),
		errors.Is(err, workspacepkg.ErrSymlinkPath),
		errors.Is(err, acfs.ErrOutsideRoot),
		errors.Is(err, acfs.ErrInvalidPath),
		errors.Is(err, acfs.ErrSymlinkLoop),
		errors.Is(err, acfs.ErrSymlink):
		return common.Classify(common.ErrProjectWorkspaceForbidden, fmt.Errorf("forbidden project workspace path: %w", err))
	default:
		return common.Classify(common.ErrProjectWorkspaceBadRequest, fmt.Errorf("invalid project workspace request: %w", err))
	}
}

func ValidateWorkspaceChangesAgainstGitOps(changes []types.WorkspaceFileChange, owned map[string]struct{}) error {
	if len(owned) == 0 {
		return nil
	}
	touchesOwned := func(target string) bool {
		if _, ok := owned[target]; ok {
			return true
		}
		prefix := target + "/"
		for ownedPath := range owned {
			if strings.HasPrefix(ownedPath, prefix) {
				return true
			}
		}
		return false
	}
	for _, change := range changes {
		for _, target := range ChangeTargetPaths(change) {
			if touchesOwned(target) {
				return common.Classify(common.ErrProjectWorkspaceForbidden, fmt.Errorf("%q is managed by git sync and can only be changed in the git repository", target))
			}
		}
	}
	return nil
}

// Backup scope and GitOps ownership share it so they cannot diverge.
func ChangeTargetPaths(change types.WorkspaceFileChange) []string {
	rel, err := kit.NormalizeRelativePath(change.RelativePath)
	if err != nil {
		return nil
	}
	paths := []string{rel}
	switch change.Operation {
	case types.FileOpRename:
		if newName, nameErr := kit.ValidateFileName(change.NewName); nameErr == nil {
			paths = append(paths, filepath.ToSlash(filepath.Join(filepath.Dir(rel), newName)))
		}
	case types.FileOpMove:
		if parent, parentErr := workspacepkg.NormalizeParentPath(change.NewParentPath); parentErr == nil {
			paths = append(paths, path.Join(parent, path.Base(rel)))
		}
	}
	return paths
}

func classifyACFSError(err error, operation string) error {
	switch {
	case errors.Is(err, acfs.ErrInvalidPath), errors.Is(err, acfs.ErrOutsideRoot), errors.Is(err, acfs.ErrSymlinkLoop), errors.Is(err, acfs.ErrSymlink):
		return common.Classify(common.ErrProjectWorkspaceForbidden, fmt.Errorf("%s: %w", operation, err))
	case errors.Is(err, acfs.ErrAlreadyExists), errors.Is(err, acfs.ErrNotEmpty):
		return common.Classify(common.ErrProjectWorkspaceConflict, fmt.Errorf("%s: %w", operation, err))
	case errors.Is(err, os.ErrNotExist):
		return common.Classify(common.ErrProjectWorkspaceNotFound, errors.New("project workspace file not found"))
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
