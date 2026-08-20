package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	acfsutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/acfs"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
)

func (s *ProjectService) GetProjectWorkspace(ctx context.Context, projectID string) (*workspacetypes.Workspace, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureProjectPathUnderRoot(ctx, proj, false); err != nil {
		return nil, err
	}
	composeFileName := projects.DefaultComposeFileName
	if composeFile, resolveErr := s.ResolveProjectComposeFile(ctx, proj); resolveErr == nil {
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
	if ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj); ownedErr == nil && len(ownedPaths) > 0 {
		for i := range files {
			if _, ok := ownedPaths[files[i].RelativePath]; ok {
				files[i].Editable = false
				files[i].ReadOnlyReason = workspacetypes.FileReadOnlyGitOpsManaged
			}
		}
	}
	return &workspacetypes.Workspace{Files: files, FileTreeRevision: revision, FileTreeTruncated: truncated}, nil
}

func (s *ProjectService) GetProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (*workspacetypes.FileContent, error) {
	proj, rel, fullPath, entry, err := s.resolveProjectWorkspacePathInternal(ctx, projectID, relativePath)
	if err != nil {
		return nil, err
	}
	_ = proj
	response := &workspacetypes.FileContent{
		Path:         fullPath,
		RelativePath: rel,
		Name:         entry.Name,
		Size:         entry.Size,
	}
	if entry.IsDirectory {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	if entry.IsSymlink {
		response.ReadOnlyReason = workspacetypes.FileReadOnlySymlink
		return response, nil
	}
	if !acfsutils.IsRegular(entry) {
		response.ReadOnlyReason = workspacetypes.FileReadOnlySpecial
		return response, nil
	}
	maxBytes := workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB)
	if entry.Size > maxBytes {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyTooLarge
		return response, nil
	}
	reader, _, err := acfs.OpenRead(ctx, proj.Path, "/"+rel, maxBytes)
	if err != nil {
		return nil, classifyProjectWorkspaceACFSErrorInternal(err, "read project workspace file")
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.WrapIf(err, "read project workspace file")
	}
	response.MimeType = http.DetectContentType(content)
	if !workspacepkg.IsTextContent(content) {
		response.ReadOnlyReason = workspacetypes.FileReadOnlyBinary
		return response, nil
	}
	response.Content = string(content)
	if ownedPaths, ownedErr := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj); ownedErr == nil {
		if _, ok := ownedPaths[rel]; ok {
			// Sync-owned files stay viewable but read-only: git is their
			// source of truth (#3634).
			response.ReadOnlyReason = workspacetypes.FileReadOnlyGitOpsManaged
			return response, nil
		}
	}
	response.Editable = true
	return response, nil
}

func (s *ProjectService) DownloadProjectWorkspaceFile(ctx context.Context, projectID, relativePath string) (io.ReadCloser, int64, string, error) {
	proj, rel, _, entry, err := s.resolveProjectWorkspacePathInternal(ctx, projectID, relativePath)
	if err != nil {
		return nil, 0, "", err
	}
	if entry.IsDirectory {
		return nil, 0, "", common.Classify(common.ErrProjectWorkspaceBadRequest, errors.New("workspace path is a directory"))
	}
	file, size, err := acfs.OpenRead(ctx, proj.Path, "/"+rel, 0)
	if err != nil {
		return nil, 0, "", classifyProjectWorkspaceACFSErrorInternal(err, "open project workspace file")
	}
	return file, size, filepath.Base(rel), nil
}

func (s *ProjectService) UpdateProjectWorkspace(ctx context.Context, projectID string, manifest projecttypes.WorkspaceUpdateManifest, uploads map[int][]byte, user common.User) (*workspacetypes.Workspace, error) {
	if err := workspacepkg.ValidateUpdateManifest(manifest.FileTreeRevision, len(manifest.FileChanges), 500); err != nil {
		return nil, common.Classify(common.ErrProjectWorkspaceBadRequest, err)
	}
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// Only the paths the GitOps sync owns are locked; the rest of the
	// directory is operator-owned overlay (e.g. secret env files a public
	// repo cannot carry) and stays editable (#3634).
	ownedPaths, err := s.gitOpsOwnedWorkspacePathsInternal(ctx, proj)
	if err != nil {
		return nil, err
	}
	if err := validateWorkspaceChangesAgainstGitOpsInternal(manifest.FileChanges, ownedPaths); err != nil {
		return nil, err
	}
	if err := s.EnsureProjectPathUnderRoot(ctx, proj, true); err != nil {
		return nil, err
	}

	projectsDirectory, err := s.GetProjectsDirectory(ctx)
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
		if restoreErr := projects.RestoreProjectUpdateBackup(ctx, proj.Path, backup); restoreErr != nil {
			return nil, errors.Combine(wrapProjectWorkspaceErrorInternal(err), errors.WrapIf(restoreErr, "rollback project workspace"))
		}
		return nil, wrapProjectWorkspaceErrorInternal(err)
	}

	s.refreshProjectImageRefsInternal(ctx, proj)
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		return nil, errors.WrapIf(err, "refresh project after workspace update")
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, database.JSON{
		"action":          "update_project_workspace",
		"fileChangeCount": len(manifest.FileChanges),
	}, "could not log project workspace update")
	return s.GetProjectWorkspace(ctx, projectID)
}

func (s *ProjectService) resolveProjectWorkspacePathInternal(ctx context.Context, projectID, relativePath string) (*Project, string, string, acfstypes.Entry, error) {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return nil, "", "", acfstypes.Entry{}, err
	}
	rel, err := utils.NormalizeRelativePath(relativePath)
	if err != nil {
		return nil, "", "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceForbidden, errors.WrapIf(err, "invalid project workspace path"))
	}
	composeFileName := projects.DefaultComposeFileName
	if composeFile, resolveErr := s.ResolveProjectComposeFile(ctx, proj); resolveErr == nil {
		composeFileName = filepath.Base(composeFile)
	}
	rootName, _, _ := strings.Cut(rel, "/")
	protected := projects.ProtectedProjectFilePaths(composeFileName)
	if protected[rel] || protected[rootName] {
		return nil, "", "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceForbidden, errors.New("project configuration is not part of the workspace"))
	}
	fullPath := filepath.Join(proj.Path, filepath.FromSlash(rel))
	entry, err := acfs.Stat(ctx, proj.Path, "/"+rel, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", acfstypes.Entry{}, common.Classify(common.ErrProjectWorkspaceNotFound, errors.New("project workspace file not found"))
		}
		return nil, "", "", acfstypes.Entry{}, classifyProjectWorkspaceACFSErrorInternal(err, "inspect project workspace file")
	}
	return proj, rel, fullPath, entry, nil
}

func classifyProjectWorkspaceACFSErrorInternal(err error, operation string) error {
	switch {
	case errors.Is(err, acfs.ErrInvalidPath),
		errors.Is(err, acfs.ErrOutsideRoot),
		errors.Is(err, acfs.ErrSymlinkLoop),
		errors.Is(err, acfs.ErrSymlink):
		return common.Classify(common.ErrProjectWorkspaceForbidden, errors.WrapIf(err, operation))
	case errors.Is(err, acfs.ErrAlreadyExists),
		errors.Is(err, acfs.ErrNotEmpty):
		return common.Classify(common.ErrProjectWorkspaceConflict, errors.WrapIf(err, operation))
	case os.IsNotExist(err):
		return common.Classify(common.ErrProjectWorkspaceNotFound, errors.New("project workspace file not found"))
	default:
		return errors.WrapIf(err, operation)
	}
}

func (s *ProjectService) projectWorkspaceApplyOptionsInternal(ctx context.Context, proj *Project, expectedRevision string) projects.ProjectWorkspaceApplyOptions {
	composeFileName := projects.DefaultComposeFileName
	if composeFile, err := s.ResolveProjectComposeFile(ctx, proj); err == nil {
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
		scope.Paths = append(scope.Paths, workspaceChangeTargetPathsInternal(change)...)
	}
	backup, err := backupProjectDirectoryInternal(ctx, projectsDirectory, projectPath, scope)
	if err != nil {
		return nil, nil, err
	}
	backupLogical, err := acfs.LogicalPath(projectsDirectory, backup.BackupDir)
	if err != nil {
		return nil, nil, errors.WrapIf(err, "failed to resolve project backup directory")
	}
	return backup, func() { _ = acfs.RemoveAll(ctx, projectsDirectory, backupLogical) }, nil
}

func isGitOpsManagedProjectInternal(proj *Project) bool {
	return proj != nil && proj.GitOpsManagedBy != nil && strings.TrimSpace(*proj.GitOpsManagedBy) != ""
}

// gitOpsOwnedWorkspacePathsInternal returns the workspace-relative paths owned
// by the project's GitOps sync — the files git writes on every sync. Only
// these are locked for workspace editing; anything else in the directory is
// operator-owned overlay (secret env files, bind-mounted configs) that the
// sync engine deliberately never prunes (#3634). Returns nil for projects
// without a live sync, including a stale gitops_managed_by marker.
func (s *ProjectService) gitOpsOwnedWorkspacePathsInternal(ctx context.Context, proj *Project) (map[string]struct{}, error) {
	if !isGitOpsManagedProjectInternal(proj) {
		return nil, nil
	}
	sync, err := loadGitOpsSyncForProjectInternal(ctx, s.db, proj.ID)
	if err != nil {
		return nil, errors.WrapIf(err, "load gitops sync for workspace")
	}
	if sync == nil {
		return nil, nil
	}

	owned := make(map[string]struct{})
	add := func(p string) {
		if rel, err := utils.NormalizeRelativePath(p); err == nil {
			owned[rel] = struct{}{}
		}
	}
	if sync.SyncedFiles != nil && *sync.SyncedFiles != "" {
		var files []string
		// Fail closed: an incomplete ownership set would expose synced files
		// as editable, and the next sync would silently overwrite the edits.
		if err := json.Unmarshal([]byte(*sync.SyncedFiles), &files); err != nil {
			return nil, errors.WrapIf(err, "parse gitops synced files for workspace")
		}
		for _, f := range files {
			add(f)
		}
	}
	if sync.ComposePath != "" {
		add(filepath.Base(sync.ComposePath))
	}
	return owned, nil
}

// workspaceChangeTargetPathsInternal lists every normalized project-relative
// path a workspace file change can create, overwrite or delete. Shared by the
// backup scope and the GitOps ownership check so the two cannot diverge.
func workspaceChangeTargetPathsInternal(change projecttypes.WorkspaceFileChange) []string {
	rel, err := utils.NormalizeRelativePath(change.RelativePath)
	if err != nil {
		return nil
	}
	paths := []string{rel}
	switch change.Operation {
	case projecttypes.FileOpRename:
		if newName, nameErr := utils.ValidateFileName(change.NewName); nameErr == nil {
			paths = append(paths, filepath.ToSlash(filepath.Join(filepath.Dir(rel), newName)))
		}
	case projecttypes.FileOpMove:
		paths = append(paths, filepath.ToSlash(filepath.Join(change.NewParentPath, filepath.Base(rel))))
	}
	return paths
}

// validateWorkspaceChangesAgainstGitOpsInternal rejects file changes that
// touch a sync-owned path, directly or by deleting/renaming a folder that
// still holds one.
func validateWorkspaceChangesAgainstGitOpsInternal(changes []projecttypes.WorkspaceFileChange, owned map[string]struct{}) error {
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
		for _, target := range workspaceChangeTargetPathsInternal(change) {
			if touchesOwned(target) {
				return common.Classify(common.ErrProjectWorkspaceForbidden,
					errors.Errorf("%q is managed by git sync and can only be changed in the git repository", target))
			}
		}
	}
	return nil
}

func wrapProjectWorkspaceErrorInternal(err error) error {
	switch {
	case errors.Is(err, projects.ErrProjectWorkspaceRevisionConflict):
		return common.Classify(common.ErrProjectWorkspaceConflict, errors.WithStackIf(err))
	case errors.Is(err, acfs.ErrAlreadyExists), errors.Is(err, acfs.ErrNotEmpty):
		return common.Classify(common.ErrProjectWorkspaceConflict, errors.WrapIf(err, "conflicting project workspace path"))
	case errors.Is(err, projects.ErrProjectWorkspaceOutsideProjectDirectory),
		errors.Is(err, projects.ErrProjectWorkspaceProtectedPath),
		errors.Is(err, projects.ErrProjectWorkspaceSymlinkPath),
		errors.Is(err, acfs.ErrOutsideRoot),
		errors.Is(err, acfs.ErrInvalidPath),
		errors.Is(err, acfs.ErrSymlinkLoop),
		errors.Is(err, acfs.ErrSymlink):
		return common.Classify(common.ErrProjectWorkspaceForbidden, errors.WrapIf(err, "forbidden project workspace path"))
	default:
		return common.Classify(common.ErrProjectWorkspaceBadRequest, errors.WrapIf(err, "invalid project workspace request"))
	}
}
