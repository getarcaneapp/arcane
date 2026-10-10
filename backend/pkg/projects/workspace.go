package projects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"go.getarcane.app/acfs"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

const (
	ProjectWorkspaceUseScanDepth = -1
)

var (
	ErrProjectWorkspaceRevisionConflict        = errors.New("project workspace changed; refresh it and try again")
	ErrProjectWorkspaceOutsideProjectDirectory = errors.New("path is outside project directory")
	ErrProjectWorkspaceProtectedPath           = errors.New("protected project configuration cannot be modified through the workspace")
	ErrProjectWorkspaceSymlinkPath             = errors.New("symlink workspace paths are not supported")
)

type ProjectWorkspaceApplyOptions struct {
	ExpectedRevision string
	MaxDepth         int
	MaxEntries       int
	SkipDirectories  string
	ComposeFileName  string
	MaxFileSizeBytes int64
}

func ReadProjectWorkspace(
	ctx context.Context,
	projectPath string,
	maxDepth int,
	skipDirectories, composeFileName string,
	maxEntries int,
	maxFileSizeBytes int64,
) ([]workspacetypes.FileEntry, string, bool, error) {
	if maxDepth == ProjectWorkspaceUseScanDepth {
		maxDepth = config.LoadProjectWorkspaceConfig().ProjectWorkspaceMaxDepth
	}
	if maxEntries <= 0 {
		maxEntries = config.LoadProjectWorkspaceConfig().ProjectWorkspaceMaxEntries
	}
	if maxFileSizeBytes <= 0 {
		maxFileSizeBytes = workspacepkg.MaxFileSizeBytes(config.LoadProjectWorkspaceConfig().ProjectWorkspaceMaxFileSizeMB)
	}

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	// The workspace tree walk and apply engine stay on os.Root rather than
	// acfs: the revision hash depends on fs.WalkDir's lexical visit order, and
	// acfs walks in a different order — switching would change every revision
	// and mass-invalidate open editor drafts with false 409s.
	root, err := os.OpenRoot(projectAbs)
	if err != nil {
		return nil, "", false, fmt.Errorf("open project directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	walker := &projectWorkspaceTreeWalkerInternal{
		ctx:              ctx,
		projectAbs:       projectAbs,
		maxDepth:         maxDepth,
		maxEntries:       maxEntries,
		maxFileSizeBytes: maxFileSizeBytes,
		protected:        ProtectedProjectPaths(projectAbs, composeFileName),
		skipDirs:         projectScanSkipDirectorySetInternal(skipDirectories),
		files:            []workspacetypes.FileEntry{},
		revisionHash:     sha256.New(),
	}

	if walkDirErr := fs.WalkDir(root.FS(), ".", walker.visit); walkDirErr != nil {
		return nil, "", false, walkDirErr
	}

	slices.SortFunc(walker.files, func(a, b workspacetypes.FileEntry) int {
		if a.IsDirectory != b.IsDirectory {
			return kit.Ternary(a.IsDirectory, -1, 1)
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})

	return walker.files, hex.EncodeToString(walker.revisionHash.Sum(nil)), walker.truncated, nil
}

type projectWorkspaceTreeWalkerInternal struct {
	ctx              context.Context
	projectAbs       string
	maxDepth         int
	maxEntries       int
	maxFileSizeBytes int64
	protected        map[string]bool
	skipDirs         map[string]bool
	files            []workspacetypes.FileEntry
	revisionHash     hash.Hash
	entryCount       int
	truncated        bool
}

func (w *projectWorkspaceTreeWalkerInternal) visit(rel string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		// fs.WalkDir reports a directory it could not read by invoking the
		// callback a second time with the ReadDir error. Skipping keeps the
		// rest of the tree intact instead of discarding every sibling; the
		// directory itself was already recorded by the first call. A failure
		// on the project workspace root, or one that is not a permission/disappearance
		// error, stays fatal so real I/O problems remain observable.
		if rel != "." && entry != nil && entry.IsDir() && (os.IsPermission(walkErr) || os.IsNotExist(walkErr)) {
			slog.DebugContext(w.ctx, "Skipping unreadable project workspace subdirectory", "relativePath", rel, "error", walkErr)
			return fs.SkipDir
		}
		return walkErr
	}
	if rel == "." {
		return nil
	}

	// The walk visits entries in deterministic lexical order, so cutting
	// off after maxEntries still yields a stable revision as long as the
	// concurrency compare walk uses the same cap.
	if w.entryCount >= w.maxEntries {
		w.truncated = true
		return fs.SkipAll
	}

	depth := strings.Count(rel, "/") + 1
	if depth > w.maxDepth {
		return kit.Ternary(entry.IsDir(), fs.SkipDir, nil)
	}

	if entry.IsDir() && w.skipDirs[entry.Name()] {
		return fs.SkipDir
	}

	info, err := entry.Info()
	if err != nil {
		// An entry that vanished mid-walk or that we are not allowed to stat is
		// skipped rather than failing the whole tree; anything else surfaces.
		// fs.SkipDir on a non-directory would break out of the parent's sibling
		// loop, so split on IsDir.
		if !os.IsPermission(err) && !os.IsNotExist(err) {
			return err
		}
		slog.DebugContext(w.ctx, "Skipping unreadable project workspace entry", "relativePath", rel, "error", err)
		return kit.Ternary(entry.IsDir(), fs.SkipDir, nil)
	}

	isProtected := w.protected[rel]
	if isProtected {
		return nil
	}

	// Protected Compose configuration is intentionally absent from both the
	// workspace response and its revision so configuration saves cannot make
	// a workspace draft stale.
	kind := "file"
	switch {
	case entry.IsDir():
		kind = "dir"
	case info.Mode()&os.ModeSymlink != 0:
		kind = "symlink"
	case !info.Mode().IsRegular():
		kind = "special"
	}
	// The revision is structural (path + kind only): running containers write
	// into their own project directory continuously (logs, certs, app state),
	// so keying on mtime/size would invalidate every workspace draft for a
	// live project and make saves 409 forever (#3199). Structural changes —
	// files appearing, disappearing, or changing kind — still conflict.
	kit.WriteRecord(w.revisionHash, rel, kind)
	w.entryCount++

	size := info.Size()
	if entry.IsDir() {
		size = 0
	}
	workspaceEntry := workspacetypes.FileEntry{
		Path:         filepath.Join(w.projectAbs, filepath.FromSlash(rel)),
		RelativePath: rel,
		Name:         entry.Name(),
		IsDirectory:  entry.IsDir(),
		Size:         size,
		ModTime:      info.ModTime(),
		Mode:         info.Mode().String(),
		IsSymlink:    info.Mode()&os.ModeSymlink != 0,
	}
	switch kind {
	case "dir":
		workspaceEntry.Editable = true
	case "symlink":
		workspaceEntry.ReadOnlyReason = workspacetypes.FileReadOnlySymlink
		if target, readlinkErr := os.Readlink(workspaceEntry.Path); readlinkErr == nil {
			workspaceEntry.LinkTarget = target
		}
	case "special":
		workspaceEntry.ReadOnlyReason = workspacetypes.FileReadOnlySpecial
	case "file":
		workspaceEntry.Editable, workspaceEntry.ReadOnlyReason = classifyProjectWorkspaceFileInternal(workspaceEntry.Path, size, w.maxFileSizeBytes)
	}
	w.files = append(w.files, workspaceEntry)

	return nil
}

func classifyProjectWorkspaceFileInternal(filePath string, size, maxFileSizeBytes int64) (bool, string) {
	if size > maxFileSizeBytes {
		return false, workspacetypes.FileReadOnlyTooLarge
	}
	content, err := os.ReadFile(filePath)
	if err != nil || !bytes.Equal(bytes.ToValidUTF8(content, []byte{}), content) || bytes.IndexByte(content, 0) >= 0 {
		return false, workspacetypes.FileReadOnlyBinary
	}
	return true, ""
}

// ApplyProjectWorkspaceChanges applies changes in order without internal
// rollback: on any error, earlier changes remain on disk. Callers own
// atomicity — ProjectService.UpdateProjectWorkspace wraps every save in
// BackupProjectUpdateScope / RestoreProjectUpdateBackup, and project creation
// removes a new directory or restores the scoped backup of a reused one.
func ApplyProjectWorkspaceChanges(ctx context.Context, projectPath string, changes []project.WorkspaceFileChange, uploads map[int][]byte, opts ProjectWorkspaceApplyOptions) error {
	if opts.MaxFileSizeBytes <= 0 {
		opts.MaxFileSizeBytes = workspacepkg.MaxFileSizeBytes(workspacepkg.DefaultMaxFileSizeMB)
	}
	uploadReferences := make([]workspacepkg.UploadReference, 0, len(changes))
	for _, change := range changes {
		uploadReferences = append(uploadReferences, workspacepkg.UploadReference{Operation: change.Operation, UploadIndex: change.UploadIndex, BaselineIndex: change.BaselineIndex})
	}
	if err := workspacepkg.ValidateUploadIndices(uploadReferences, len(uploads), project.FileOpCreateFile, project.FileOpUpdateFile); err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	if opts.ExpectedRevision != "" {
		_, currentRevision, _, err := ReadProjectWorkspace(ctx, projectPath, opts.MaxDepth, opts.SkipDirectories, opts.ComposeFileName, opts.MaxEntries, opts.MaxFileSizeBytes)
		if err != nil {
			return fmt.Errorf("read project workspace revision: %w", err)
		}
		if currentRevision != opts.ExpectedRevision {
			return ErrProjectWorkspaceRevisionConflict
		}
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
		if applyWorkspaceFileChangeErr := applyWorkspaceFileChangeInternal(root, protected, change, uploads, opts.MaxFileSizeBytes); applyWorkspaceFileChangeErr != nil {
			return applyWorkspaceFileChangeErr
		}
	}

	return nil
}

// ProtectedProjectPaths is ProtectedProjectFilePaths minus names that are
// directories in projectPath. Neither Arcane nor compose treats such a
// directory as project configuration, so it belongs to the workspace.
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
	for _, candidate := range ComposeFileCandidates() {
		protected[candidate] = true
	}
	for _, candidate := range ComposeOverrideFileCandidates() {
		protected[candidate] = true
	}
	if trimmed := strings.TrimSpace(composeFileName); trimmed != "" {
		protected[path.Base(filepath.ToSlash(trimmed))] = true
	}
	return protected
}

func applyWorkspaceFileChangeInternal(root *os.Root, protected map[string]bool, change project.WorkspaceFileChange, uploads map[int][]byte, maxFileSizeBytes int64) error {
	rel, err := kit.NormalizeRelativePath(change.RelativePath)
	if err != nil {
		return fmt.Errorf("invalid project workspace path: %w", err)
	}

	switch change.Operation {
	case project.FileOpCreateFile:
		return createProjectWorkspaceFileInternal(root, protected, rel, uploads[*change.UploadIndex], maxFileSizeBytes)
	case project.FileOpCreateFolder:
		return createProjectWorkspaceFolderInternal(root, protected, rel)
	case project.FileOpUpdateFile:
		var baseline []byte
		if change.BaselineIndex != nil {
			baseline = uploads[*change.BaselineIndex]
		}
		return updateProjectWorkspaceFileInternal(root, protected, rel, uploads[*change.UploadIndex], baseline, change.BaselineIndex != nil, maxFileSizeBytes)
	case project.FileOpRename:
		newName, validateFileNameErr := kit.ValidateFileName(change.NewName)
		if validateFileNameErr != nil {
			return fmt.Errorf("invalid project workspace file name: %w", validateFileNameErr)
		}
		return renameProjectWorkspacePathInternal(root, protected, rel, newName)
	case project.FileOpMove:
		return moveProjectWorkspacePathInternal(root, protected, rel, change.NewParentPath)
	case project.FileOpDelete:
		return deleteProjectWorkspacePathInternal(root, protected, rel, change.Recursive)
	default:
		return fmt.Errorf("unsupported project workspace operation %q", change.Operation)
	}
}

func createProjectWorkspaceFileInternal(root *os.Root, protected map[string]bool, rel string, content []byte, maxFileSizeBytes int64) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := workspacepkg.ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(rel)); err != nil {
		return err
	}

	if err := root.MkdirAll(path.Dir(rel), utils.DirPerm); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// O_EXCL makes the exists-check-and-create atomic; os.Root confines the
	// path to the project directory in the kernel.
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, utils.FilePerm)
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

func createProjectWorkspaceFolderInternal(root *os.Root, protected map[string]bool, rel string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	if _, err := root.Lstat(rel); err == nil {
		return fmt.Errorf("project workspace folder already exists: %s: %w", rel, acfs.ErrAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project workspace folder: %w", err)
	}
	if err := root.MkdirAll(rel, utils.DirPerm); err != nil {
		return fmt.Errorf("create project workspace folder: %w", err)
	}
	return nil
}

func updateProjectWorkspaceFileInternal(root *os.Root, protected map[string]bool, rel string, content, baseline []byte, baselineSet bool, maxFileSizeBytes int64) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := workspacepkg.ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
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

	// The workspace revision is structural only, so an external edit to this
	// same file would otherwise be silently overwritten. The client uploads
	// the content its draft was based on; a mismatch with the current on-disk
	// content is a real per-file conflict. Checked here, in apply order, so an
	// earlier rename/move in the same manifest has already landed and the
	// comparison targets the right path. The read is capped just past the
	// baseline length: a longer on-disk file differs by definition.
	if baselineSet {
		current, readProjectWorkspaceFileLimitedErr := readProjectWorkspaceFileLimitedInternal(root, rel, int64(len(baseline))+1)
		if readProjectWorkspaceFileLimitedErr != nil {
			return fmt.Errorf("read project workspace file: %w", readProjectWorkspaceFileLimitedErr)
		}
		if !bytes.Equal(current, baseline) {
			return fmt.Errorf("file content changed since it was loaded: %w", ErrProjectWorkspaceRevisionConflict)
		}
	}

	if writeFileErr := root.WriteFile(rel, content, utils.FilePerm); writeFileErr != nil {
		return fmt.Errorf("update project workspace file: %w", writeFileErr)
	}
	return nil
}

func readProjectWorkspaceFileLimitedInternal(root *os.Root, rel string, maxBytes int64) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxBytes))
}

func renameProjectWorkspacePathInternal(root *os.Root, protected map[string]bool, rel, newName string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
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
	if ensureWritableProjectRelPathErr := ensureWritableProjectRelPathInternal(protected, targetRel); ensureWritableProjectRelPathErr != nil {
		return ensureWritableProjectRelPathErr
	}
	if ensureProjectPathHasNoSymlinkErr := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(targetRel)); ensureProjectPathHasNoSymlinkErr != nil {
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

// NormalizeProjectParentPath normalizes a move destination folder; a blank
// one is the project root.
func NormalizeProjectParentPath(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	return kit.NormalizeRelativePath(input)
}

func moveProjectWorkspacePathInternal(root *os.Root, protected map[string]bool, rel, newParentPath string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	parentRel, err := NormalizeProjectParentPath(newParentPath)
	if err != nil {
		return fmt.Errorf("invalid project workspace parent path: %w", err)
	}
	if parentRel != "" {
		if ensureWritableProjectRelPathErr := ensureWritableProjectRelPathInternal(protected, parentRel); ensureWritableProjectRelPathErr != nil {
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

	if validateProjectMoveParentErr := validateProjectMoveParentInternal(root, parentRel); validateProjectMoveParentErr != nil {
		return validateProjectMoveParentErr
	}

	targetRel := path.Base(rel)
	if parentRel != "" {
		targetRel = path.Join(parentRel, path.Base(rel))
	}
	if targetRel == rel {
		return errors.New("project workspace path is already in the destination folder")
	}
	if ensureWritableProjectRelPathErr2 := ensureWritableProjectRelPathInternal(protected, targetRel); ensureWritableProjectRelPathErr2 != nil {
		return ensureWritableProjectRelPathErr2
	}
	if ensureProjectPathHasNoSymlinkErr := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(targetRel)); ensureProjectPathHasNoSymlinkErr != nil {
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

func validateProjectMoveParentInternal(root *os.Root, parentRel string) error {
	if parentRel == "" {
		return nil
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, parentRel); err != nil {
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

func deleteProjectWorkspacePathInternal(root *os.Root, protected map[string]bool, rel string, recursive bool) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
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
		empty, isDirectoryEmptyErr := isDirectoryEmptyInternal(root, rel)
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

func ensureProjectPathHasNoSymlinkInternal(root *os.Root, rel string) error {
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

func ensureWritableProjectRelPathInternal(protected map[string]bool, rel string) error {
	if rel == "." || rel == "" {
		return errors.New("project workspace root cannot be modified")
	}
	rootName, _, _ := strings.Cut(rel, "/")
	if protected[rel] || protected[rootName] {
		return fmt.Errorf("%s: %w", rel, ErrProjectWorkspaceProtectedPath)
	}
	return nil
}

func isDirectoryEmptyInternal(root *os.Root, rel string) (bool, error) {
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
