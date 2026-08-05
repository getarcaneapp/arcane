package services

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
)

func (s *ProjectService) GetProjectWorkspace(ctx context.Context, projectID string) (*workspacetypes.Workspace, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureProjectPathUnderRoot(ctx, proj, false); err != nil {
		return nil, err
	}
	composeFileName := projects.DefaultComposeFileName
	if composeFile, resolveErr := s.resolveProjectComposeFileInternal(ctx, proj); resolveErr == nil {
		composeFileName = filepath.Base(composeFile)
	}
	files, revision, truncated, err := projects.ReadProjectWorkspace(
		proj.Path,
		s.config.ProjectWorkspaceMaxDepth,
		s.config.ProjectScanSkipDirs,
		composeFileName,
		s.config.ProjectWorkspaceMaxEntries,
		workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
	)
	if err != nil {
		return nil, errors.WrapIf(err, "read project workspace")
	}
	return &workspacetypes.Workspace{Files: files, FileTreeRevision: revision, FileTreeTruncated: truncated}, nil
}

func (s *ProjectService) GetProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (*workspacetypes.FileContent, error) {
	proj, rel, fullPath, info, err := s.resolveProjectWorkspacePathInternal(ctx, projectID, relativePath)
	if err != nil {
		return nil, err
	}
	_ = proj
	response := &workspacetypes.FileContent{
		Path:         fullPath,
		RelativePath: rel,
		Name:         filepath.Base(fullPath),
		Size:         info.Size(),
	}
	if info.IsDir() {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		response.ReadOnlyReason = workspacetypes.FileReadOnlySymlink
		return response, nil
	}
	if !info.Mode().IsRegular() {
		response.ReadOnlyReason = workspacetypes.FileReadOnlySpecial
		return response, nil
	}
	maxBytes := workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB)
	if info.Size() > maxBytes {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyTooLarge
		return response, nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, errors.WrapIf(err, "read project workspace file")
	}
	response.MimeType = http.DetectContentType(content)
	if !workspacepkg.IsTextContent(content) {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyBinary
		return response, nil
	}
	response.Editable = true
	response.Content = string(content)
	return response, nil
}

func (s *ProjectService) DownloadProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (io.ReadCloser, int64, string, error) {
	proj, rel, _, info, err := s.resolveProjectWorkspacePathInternal(ctx, projectID, relativePath)
	if err != nil {
		return nil, 0, "", err
	}
	if info.IsDir() {
		return nil, 0, "", common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	root, err := os.OpenRoot(proj.Path)
	if err != nil {
		return nil, 0, "", errors.WrapIf(err, "open project workspace")
	}
	file, err := root.Open(filepath.ToSlash(rel))
	if err != nil {
		_ = root.Close()
		return nil, 0, "", errors.WrapIf(err, "open project workspace file")
	}
	return &projectWorkspaceDownloadInternal{ReadCloser: file, root: root}, info.Size(), filepath.Base(rel), nil
}

type projectWorkspaceDownloadInternal struct {
	io.ReadCloser

	root *os.Root
}

func (d *projectWorkspaceDownloadInternal) Close() error {
	fileErr := d.ReadCloser.Close()
	rootErr := d.root.Close()
	return errors.Combine(fileErr, rootErr)
}

func (s *ProjectService) UpdateProjectWorkspace(ctx context.Context, projectID string, manifest projecttypes.WorkspaceUpdateManifest, uploads map[int][]byte, user models.User) (*workspacetypes.Workspace, error) {
	if err := workspacepkg.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, err)
	}
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if isGitOpsManagedProjectInternal(proj) {
		return nil, common.Classify(common.ErrProjectWorkspaceForbidden, errors.New("GitOps-managed project workspaces cannot be edited"))
	}
	if err := s.ensureProjectPathUnderRoot(ctx, proj, true); err != nil {
		return nil, err
	}

	projectsDirectory, err := s.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return nil, err
	}
	backup, cleanup, err := s.prepareProjectWorkspaceBackupInternal(ctx, projectsDirectory, proj.Path, manifest.FileChanges)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	opts := s.projectWorkspaceApplyOptionsInternal(ctx, proj, manifest.FileTreeRevision)
	if err := projects.ApplyProjectWorkspaceChanges(proj.Path, manifest.FileChanges, uploads, opts); err != nil {
		if restoreErr := projects.RestoreProjectUpdateBackup(proj.Path, backup); restoreErr != nil {
			return nil, errors.Combine(wrapProjectWorkspaceErrorInternal(err), errors.WrapIf(restoreErr, "rollback project workspace"))
		}
		return nil, wrapProjectWorkspaceErrorInternal(err)
	}

	s.refreshProjectImageRefsInternal(ctx, proj)
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		return nil, errors.WrapIf(err, "refresh project after workspace update")
	}
	s.logProjectEventInternal(ctx, models.EventTypeProjectUpdate, proj.ID, proj.Name, user, models.JSON{
		"action":          "update_project_workspace",
		"fileChangeCount": len(manifest.FileChanges),
	}, "could not log project workspace update")
	return s.GetProjectWorkspace(ctx, projectID)
}

func (s *ProjectService) resolveProjectWorkspacePathInternal(ctx context.Context, projectID, relativePath string) (*models.Project, string, string, os.FileInfo, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, "", "", nil, err
	}
	rel, err := utils.NormalizeRelativePath(relativePath)
	if err != nil {
		return nil, "", "", nil, common.Classify(common.ErrProjectWorkspaceForbidden, errors.WrapIf(err, "invalid project workspace path"))
	}
	composeFileName := projects.DefaultComposeFileName
	if composeFile, resolveErr := s.resolveProjectComposeFileInternal(ctx, proj); resolveErr == nil {
		composeFileName = filepath.Base(composeFile)
	}
	rootName, _, _ := strings.Cut(rel, "/")
	protected := projects.ProtectedProjectFilePaths(composeFileName)
	if protected[rel] || protected[rootName] {
		return nil, "", "", nil, common.Classify(common.ErrProjectWorkspaceForbidden, errors.New("project configuration is not part of the workspace"))
	}
	fullPath := filepath.Join(proj.Path, filepath.FromSlash(rel))
	info, err := os.Lstat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", nil, common.Classify(common.ErrProjectWorkspaceNotFound, errors.New("project workspace file not found"))
		}
		return nil, "", "", nil, errors.WrapIf(err, "inspect project workspace file")
	}
	return proj, rel, fullPath, info, nil
}

func (s *ProjectService) projectWorkspaceApplyOptionsInternal(ctx context.Context, proj *models.Project, expectedRevision string) projects.ProjectWorkspaceApplyOptions {
	composeFileName := projects.DefaultComposeFileName
	if composeFile, err := s.resolveProjectComposeFileInternal(ctx, proj); err == nil {
		composeFileName = filepath.Base(composeFile)
	}
	return projects.ProjectWorkspaceApplyOptions{
		ExpectedRevision: strings.TrimSpace(expectedRevision),
		MaxDepth:         s.config.ProjectWorkspaceMaxDepth,
		MaxEntries:       s.config.ProjectWorkspaceMaxEntries,
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
		SkipDirectories:  s.config.ProjectScanSkipDirs,
		ComposeFileName:  composeFileName,
	}
}

func (s *ProjectService) prepareProjectWorkspaceBackupInternal(ctx context.Context, projectsDirectory, projectPath string, changes []projecttypes.WorkspaceFileChange) (*projects.ProjectUpdateBackup, func(), error) {
	scope := projects.ProjectUpdateBackupScope{}
	for _, change := range changes {
		rel, err := utils.NormalizeRelativePath(change.RelativePath)
		if err != nil {
			continue
		}
		scope.Paths = append(scope.Paths, rel)
		switch change.Operation {
		case projecttypes.FileOpRename:
			if newName, nameErr := utils.ValidateFileName(change.NewName); nameErr == nil {
				scope.Paths = append(scope.Paths, filepath.ToSlash(filepath.Join(filepath.Dir(rel), newName)))
			}
		case projecttypes.FileOpMove:
			scope.Paths = append(scope.Paths, filepath.ToSlash(filepath.Join(change.NewParentPath, filepath.Base(rel))))
		}
	}
	backup, err := s.backupProjectDirectoryInternal(ctx, projectsDirectory, projectPath, scope)
	if err != nil {
		return nil, nil, err
	}
	return backup, func() { _ = os.RemoveAll(backup.BackupDir) }, nil
}

func wrapProjectWorkspaceErrorInternal(err error) error {
	switch {
	case errors.Is(err, projects.ErrProjectWorkspaceRevisionConflict):
		return common.Classify(common.ErrProjectWorkspaceConflict, errors.WithStackIf(err))
	case errors.Is(err, projects.ErrProjectWorkspaceOutsideProjectDirectory),
		errors.Is(err, projects.ErrProjectWorkspaceProtectedPath),
		errors.Is(err, projects.ErrProjectWorkspaceSymlinkPath):
		return common.Classify(common.ErrProjectWorkspaceForbidden, errors.WrapIf(err, "forbidden project workspace path"))
	default:
		return common.Classify(common.ErrProjectWorkspaceBadRequest, errors.WrapIf(err, "invalid project workspace request"))
	}
}
