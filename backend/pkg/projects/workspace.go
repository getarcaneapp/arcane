package projects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	kitfs "kit.getarcane.app/fs"

	workspacepkg "getarcane.dev/arcane/v3/pkg/workspace"
	"getarcane.dev/types/v3"
)

type (
	ProjectWorkspaceListOptions struct {
		SkipDirectories  string
		ComposeFileName  string
		MaxEntries       int
		MaxFileSizeBytes int64
	}

	ProjectWorkspaceApplyOptions struct {
		ComposeFileName  string
		MaxFileSizeBytes int64
	}
)

var (
	ErrProjectWorkspaceFileChanged             = errors.New("project workspace file changed since it was loaded; refresh it and try again")
	ErrProjectWorkspaceOutsideProjectDirectory = errors.New("path is outside project directory")
	ErrProjectWorkspaceProtectedPath           = errors.New("protected project configuration cannot be modified through the workspace")
	ErrProjectWorkspaceSymlinkPath             = errors.New("symlink workspace paths are not supported")
)

func ListProjectWorkspaceFolder(ctx context.Context, projectPath, relativePath string, opts ProjectWorkspaceListOptions) (*types.Workspace, error) {
	skipDirs := projectScanSkipDirectorySet(opts.SkipDirectories)
	if relativePath != "" && slices.ContainsFunc(strings.Split(relativePath, "/"), func(segment string) bool { return skipDirs[segment] }) {
		return nil, fmt.Errorf("project workspace folder %s: %w", relativePath, fs.ErrNotExist)
	}
	protected := ProtectedProjectPaths(projectPath, opts.ComposeFileName)
	files := make([]types.WorkspaceFileEntry, 0, min(opts.MaxEntries, 256))
	truncated := false
	err := acfs.Root{Path: projectPath}.ListEach(ctx, "/"+relativePath, func(entry acfstypes.Entry) error {
		switch {
		case entry.IsDirectory && skipDirs[entry.Name], relativePath == "" && protected[entry.Name]:
			return nil
		case len(files) >= opts.MaxEntries:
			truncated = true
			return fs.SkipAll
		}
		files = append(files, workspacepkg.EntryFromACFS(entry, opts.MaxFileSizeBytes))
		return nil
	})
	switch {
	case errors.Is(err, fs.ErrPermission) && relativePath != "":
		slog.DebugContext(ctx, "Skipping unreadable project workspace folder", "relativePath", relativePath, "error", err)
		return &types.Workspace{}, nil
	case err != nil && !errors.Is(err, fs.SkipAll):
		return nil, err
	}
	slices.SortFunc(files, workspacepkg.CompareEntries)
	return &types.Workspace{Files: files, FileTreeTruncated: truncated}, nil
}

func ApplyProjectWorkspaceChanges(ctx context.Context, projectPath string, changes []types.WorkspaceFileChange, uploads map[int][]byte, opts ProjectWorkspaceApplyOptions) error {
	if opts.MaxFileSizeBytes <= 0 {
		opts.MaxFileSizeBytes = workspacepkg.MaxFileSizeBytes(workspacepkg.DefaultMaxFileSizeMB)
	}
	uploadReferences := make([]workspacepkg.UploadReference, 0, len(changes))
	for _, change := range changes {
		uploadReferences = append(uploadReferences, workspacepkg.UploadReference{Operation: change.Operation, UploadIndex: change.UploadIndex, BaselineIndex: change.BaselineIndex})
	}
	if err := workspacepkg.ValidateUploadIndices(uploadReferences, len(uploads), types.FileOpCreateFile, types.FileOpUpdateFile); err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return fmt.Errorf("open project directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, change := range changes {
		// Recomputed per change: an earlier change may have removed a directory
		// that shadowed a protected name.
		protected := ProtectedProjectPaths(projectPath, opts.ComposeFileName)
		if applyWorkspaceFileChangeErr := applyWorkspaceFileChange(root, protected, change, uploads, opts.MaxFileSizeBytes); applyWorkspaceFileChangeErr != nil {
			return applyWorkspaceFileChangeErr
		}
	}

	return nil
}

func ProtectedProjectPaths(projectPath, composeFileName string) map[string]bool {
	protected := ProtectedProjectFilePaths(composeFileName)
	for name := range protected {
		if info, err := os.Lstat(filepath.Join(projectPath, name)); err == nil && info.IsDir() {
			delete(protected, name)
		}
	}
	return protected
}

func ProtectedProjectFilePaths(composeFileName string) map[string]bool {
	protected := map[string]bool{
		EffectiveEnvFileName: true,
		GitSourceEnvFileName: true,
		OverrideEnvFileName:  true,
	}
	for _, candidate := range ComposeFileCandidates {
		protected[candidate] = true
	}
	for _, candidate := range ComposeOverrideFileCandidates {
		protected[candidate] = true
	}
	if trimmed := strings.TrimSpace(composeFileName); trimmed != "" {
		protected[path.Base(filepath.ToSlash(trimmed))] = true
	}
	return protected
}

func applyWorkspaceFileChange(root *os.Root, protected map[string]bool, change types.WorkspaceFileChange, uploads map[int][]byte, maxFileSizeBytes int64) error {
	rel, err := kit.NormalizeRelativePath(change.RelativePath)
	if err != nil {
		return fmt.Errorf("invalid project workspace path: %w", err)
	}

	switch change.Operation {
	case types.FileOpCreateFile:
		return createProjectWorkspaceFile(root, protected, rel, uploads[*change.UploadIndex], maxFileSizeBytes)
	case types.FileOpCreateFolder:
		return createProjectWorkspaceFolder(root, protected, rel)
	case types.FileOpUpdateFile:
		var baseline []byte
		if change.BaselineIndex != nil {
			baseline = uploads[*change.BaselineIndex]
		}
		return updateProjectWorkspaceFile(root, protected, rel, uploads[*change.UploadIndex], baseline, change.BaselineIndex != nil, maxFileSizeBytes)
	case types.FileOpRename:
		newName, validateFileNameErr := kit.ValidateFileName(change.NewName)
		if validateFileNameErr != nil {
			return fmt.Errorf("invalid project workspace file name: %w", validateFileNameErr)
		}
		return renameProjectWorkspacePath(root, protected, rel, newName)
	case types.FileOpMove:
		return moveProjectWorkspacePath(root, protected, rel, change.NewParentPath)
	case types.FileOpDelete:
		return deleteProjectWorkspacePath(root, protected, rel, change.Recursive)
	default:
		return fmt.Errorf("unsupported project workspace operation %q", change.Operation)
	}
}

func createProjectWorkspaceFile(root *os.Root, protected map[string]bool, rel string, content []byte, maxFileSizeBytes int64) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := workspacepkg.ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, path.Dir(rel)); err != nil {
		return err
	}

	if err := root.MkdirAll(path.Dir(rel), kitfs.DirPerm); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// O_EXCL makes the exists-check-and-create atomic; os.Root confines the
	// path to the project directory in the kernel.
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, kitfs.FilePerm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("project workspace file already exists: %s: %w", rel, acfs.ErrAlreadyExists)
		}
		return fmt.Errorf("create project workspace file: %w", err)
	}
	_, writeErr := f.Write(content)
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return fmt.Errorf("create project workspace file: %w", writeErr)
	}
	return nil
}

func createProjectWorkspaceFolder(root *os.Root, protected map[string]bool, rel string) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, rel); err != nil {
		return err
	}

	if _, err := root.Lstat(rel); err == nil {
		return fmt.Errorf("project workspace folder already exists: %s: %w", rel, acfs.ErrAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project workspace folder: %w", err)
	}
	if err := root.MkdirAll(rel, kitfs.DirPerm); err != nil {
		return fmt.Errorf("create project workspace folder: %w", err)
	}
	return nil
}

func updateProjectWorkspaceFile(root *os.Root, protected map[string]bool, rel string, content, baseline []byte, baselineSet bool, maxFileSizeBytes int64) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := workspacepkg.ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project workspace file not found: %s", rel)
		}
		return fmt.Errorf("inspect project workspace file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a folder: %s", rel)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink files are not supported: %w", ErrProjectWorkspaceSymlinkPath)
	}

	if baselineSet {
		current, readProjectWorkspaceFileLimitedErr := readProjectWorkspaceFileLimited(root, rel, int64(len(baseline))+1)
		if readProjectWorkspaceFileLimitedErr != nil {
			return fmt.Errorf("read project workspace file: %w", readProjectWorkspaceFileLimitedErr)
		}
		if !bytes.Equal(current, baseline) {
			return fmt.Errorf("%s: %w", rel, ErrProjectWorkspaceFileChanged)
		}
	}

	if writeFileErr := root.WriteFile(rel, content, kitfs.FilePerm); writeFileErr != nil {
		return fmt.Errorf("update project workspace file: %w", writeFileErr)
	}
	return nil
}

func readProjectWorkspaceFileLimited(root *os.Root, rel string, maxBytes int64) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxBytes))
}

func renameProjectWorkspacePath(root *os.Root, protected map[string]bool, rel, newName string) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect project workspace path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %w", ErrProjectWorkspaceSymlinkPath)
	}

	targetRel := path.Join(path.Dir(rel), newName)
	if ensureWritableProjectRelPathErr := ensureWritableProjectRelPath(protected, targetRel); ensureWritableProjectRelPathErr != nil {
		return ensureWritableProjectRelPathErr
	}
	if ensureProjectPathHasNoSymlinkErr := ensureProjectPathHasNoSymlink(root, path.Dir(targetRel)); ensureProjectPathHasNoSymlinkErr != nil {
		return ensureProjectPathHasNoSymlinkErr
	}
	if _, lstatErr := root.Lstat(targetRel); lstatErr == nil {
		return fmt.Errorf("project workspace path already exists: %s", targetRel)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return fmt.Errorf("inspect project workspace path: %w", lstatErr)
	}

	if renameErr := root.Rename(rel, targetRel); renameErr != nil {
		return fmt.Errorf("rename project workspace path: %w", renameErr)
	}
	return nil
}

func NormalizeProjectParentPath(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	return kit.NormalizeRelativePath(input)
}

func moveProjectWorkspacePath(root *os.Root, protected map[string]bool, rel, newParentPath string) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, rel); err != nil {
		return err
	}

	parentRel, err := NormalizeProjectParentPath(newParentPath)
	if err != nil {
		return fmt.Errorf("invalid project workspace parent path: %w", err)
	}
	if parentRel != "" {
		if ensureWritableProjectRelPathErr := ensureWritableProjectRelPath(protected, parentRel); ensureWritableProjectRelPathErr != nil {
			return ensureWritableProjectRelPathErr
		}
	}

	sourceInfo, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect project workspace path: %w", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %w", ErrProjectWorkspaceSymlinkPath)
	}
	if sourceInfo.IsDir() && parentRel != "" && kit.FilePathMatches(parentRel, rel) {
		return errors.New("folder cannot be moved into itself or a descendant")
	}

	if validateProjectMoveParentErr := validateProjectMoveParent(root, parentRel); validateProjectMoveParentErr != nil {
		return validateProjectMoveParentErr
	}

	targetRel := path.Base(rel)
	if parentRel != "" {
		targetRel = path.Join(parentRel, path.Base(rel))
	}
	if targetRel == rel {
		return errors.New("project workspace path is already in the destination folder")
	}
	if ensureWritableProjectRelPathErr2 := ensureWritableProjectRelPath(protected, targetRel); ensureWritableProjectRelPathErr2 != nil {
		return ensureWritableProjectRelPathErr2
	}
	if ensureProjectPathHasNoSymlinkErr := ensureProjectPathHasNoSymlink(root, path.Dir(targetRel)); ensureProjectPathHasNoSymlinkErr != nil {
		return ensureProjectPathHasNoSymlinkErr
	}
	if _, lstatErr := root.Lstat(targetRel); lstatErr == nil {
		return fmt.Errorf("project workspace path already exists: %s", targetRel)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return fmt.Errorf("inspect project workspace path: %w", lstatErr)
	}

	if renameErr := root.Rename(rel, targetRel); renameErr != nil {
		return fmt.Errorf("move project workspace path: %w", renameErr)
	}
	return nil
}

func validateProjectMoveParent(root *os.Root, parentRel string) error {
	if parentRel == "" {
		return nil
	}
	if err := ensureProjectPathHasNoSymlink(root, parentRel); err != nil {
		return err
	}

	parentInfo, err := root.Lstat(parentRel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("destination folder not found: %s", parentRel)
		}
		return fmt.Errorf("inspect destination folder: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink destination folders are not supported: %w", ErrProjectWorkspaceSymlinkPath)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("destination path is not a folder: %s", parentRel)
	}
	return nil
}

func deleteProjectWorkspacePath(root *os.Root, protected map[string]bool, rel string, recursive bool) error {
	if err := ensureWritableProjectRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlink(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect project workspace path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %w", ErrProjectWorkspaceSymlinkPath)
	}
	if info.IsDir() && !recursive {
		empty, isDirectoryEmptyErr := isDirectoryEmpty(root, rel)
		if isDirectoryEmptyErr != nil {
			return isDirectoryEmptyErr
		}
		if !empty {
			return errors.New("folder is not empty")
		}
	}

	if info.IsDir() {
		if removeAllErr := root.RemoveAll(rel); removeAllErr != nil {
			return fmt.Errorf("delete project workspace folder: %w", removeAllErr)
		}
		return nil
	}
	if removeErr := root.Remove(rel); removeErr != nil {
		return fmt.Errorf("delete project workspace file: %w", removeErr)
	}
	return nil
}

func ensureProjectPathHasNoSymlink(root *os.Root, rel string) error {
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "" {
		return nil
	}

	current := ""
	for segment := range strings.SplitSeq(cleaned, "/") {
		if current == "" {
			current = segment
		} else {
			current = path.Join(current, segment)
		}

		info, err := root.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("inspect project workspace path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %w", ErrProjectWorkspaceSymlinkPath)
		}
	}
	return nil
}

func ensureWritableProjectRelPath(protected map[string]bool, rel string) error {
	if rel == "." || rel == "" {
		return errors.New("project workspace root cannot be modified")
	}
	rootName, _, _ := strings.Cut(rel, "/")
	if protected[rel] || protected[rootName] {
		return fmt.Errorf("%s: %w", rel, ErrProjectWorkspaceProtectedPath)
	}
	return nil
}

func isDirectoryEmpty(root *os.Root, rel string) (bool, error) {
	f, err := root.Open(rel)
	if err != nil {
		return false, fmt.Errorf("open folder: %w", err)
	}
	defer func() { _ = f.Close() }()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read folder: %w", err)
	}
	return false, nil
}
