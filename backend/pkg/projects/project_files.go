package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/project"
)

const (
	MaxManagedProjectFileBytes  = 1024 * 1024
	ProjectFileTreeUseScanDepth = -1
	// DefaultProjectFileTreeMaxEntries bounds the file-tree walk so a project
	// with a huge data directory cannot stall the details load. Not a user
	// setting; pass 0 to use it.
	DefaultProjectFileTreeMaxEntries = 2000
)

const (
	ErrProjectFileRevisionConflict        = errors.Sentinel("project file tree changed; refresh the project and try again")
	ErrProjectFileOutsideProjectDirectory = errors.Sentinel("path is outside project directory")
	ErrProjectFileProtectedPath           = errors.Sentinel("protected project file cannot be modified")
	ErrProjectFileSymlinkPath             = errors.Sentinel("symlink project paths are not supported")
)

type ProjectFileApplyOptions struct {
	ExpectedRevision string
	MaxDepth         int
	MaxEntries       int
	SkipDirectories  string
	ComposeFileName  string
}

func ReadProjectFileTree(projectPath string, maxDepth int, skipDirectories, composeFileName string, maxEntries int) ([]project.ProjectFile, string, bool, error) {
	if maxDepth == ProjectFileTreeUseScanDepth {
		maxDepth = config.LoadProjectFilesConfig().ProjectFileTreeMaxDepth
	}
	if maxEntries <= 0 {
		maxEntries = DefaultProjectFileTreeMaxEntries
	}

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, "", false, errors.WrapIf(err, "resolve project path")
	}
	projectAbs = filepath.Clean(projectAbs)

	root, err := os.OpenRoot(projectAbs)
	if err != nil {
		return nil, "", false, errors.WrapIf(err, "open project directory")
	}
	defer func() { _ = root.Close() }()

	walker := &projectFileTreeWalkerInternal{
		projectAbs:   projectAbs,
		maxDepth:     maxDepth,
		maxEntries:   maxEntries,
		protected:    ProtectedProjectFilePaths(composeFileName),
		skipDirs:     projectScanSkipDirectorySetInternal(skipDirectories),
		files:        []project.ProjectFile{},
		revisionHash: sha256.New(),
	}

	if err := fs.WalkDir(root.FS(), ".", walker.visit); err != nil {
		return nil, "", false, err
	}

	slices.SortFunc(walker.files, func(a, b project.ProjectFile) int {
		if a.IsDirectory != b.IsDirectory {
			if a.IsDirectory {
				return -1
			}
			return 1
		}
		return strings.Compare(a.RelativePath, b.RelativePath)
	})

	return walker.files, hex.EncodeToString(walker.revisionHash.Sum(nil)), walker.truncated, nil
}

type projectFileTreeWalkerInternal struct {
	projectAbs   string
	maxDepth     int
	maxEntries   int
	protected    map[string]bool
	skipDirs     map[string]bool
	files        []project.ProjectFile
	revisionHash hash.Hash
	entryCount   int
	truncated    bool
}

func (w *projectFileTreeWalkerInternal) visit(rel string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
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
		if entry.IsDir() {
			return fs.SkipDir
		}
		return nil
	}

	if entry.Type()&fs.ModeSymlink != 0 {
		return nil
	}
	if entry.IsDir() && w.skipDirs[entry.Name()] {
		return fs.SkipDir
	}

	info, err := entry.Info()
	if err != nil {
		return errors.WrapIf(err, "inspect project file")
	}

	// fs.WalkDir visits entries in deterministic lexical order, so hashing
	// in visit order yields a stable revision.
	isProtected := w.protected[rel]
	writeProjectFileRevisionEntryInternal(w.revisionHash, rel, info, entry.IsDir(), isProtected)
	w.entryCount++

	if isProtected {
		return nil
	}

	size := info.Size()
	if entry.IsDir() {
		size = 0
	}
	w.files = append(w.files, project.ProjectFile{
		Path:         filepath.Join(w.projectAbs, filepath.FromSlash(rel)),
		RelativePath: rel,
		Name:         entry.Name(),
		IsDirectory:  entry.IsDir(),
		Size:         size,
		ModTime:      info.ModTime(),
		Protected:    false,
	})

	return nil
}

func ApplyProjectFileDrafts(projectPath string, drafts []project.ProjectFileDraft, opts ProjectFileApplyOptions) error {
	if len(drafts) == 0 {
		return nil
	}

	changes := make([]project.ProjectFileChange, 0, len(drafts))
	for _, draft := range drafts {
		operation := project.FileOpCreateFile
		var content *string
		if draft.IsDirectory {
			operation = project.FileOpCreateFolder
		} else {
			content = new(draft.Content)
		}
		changes = append(changes, project.ProjectFileChange{
			Operation:    operation,
			RelativePath: draft.RelativePath,
			Content:      content,
		})
	}

	opts.ExpectedRevision = ""
	return ApplyProjectFileChanges(projectPath, changes, opts)
}

func ApplyProjectFileChanges(projectPath string, changes []project.ProjectFileChange, opts ProjectFileApplyOptions) error {
	if len(changes) == 0 {
		return nil
	}

	if opts.ExpectedRevision != "" {
		_, currentRevision, _, err := ReadProjectFileTree(projectPath, opts.MaxDepth, opts.SkipDirectories, opts.ComposeFileName, opts.MaxEntries)
		if err != nil {
			return errors.WrapIf(err, "read project file tree revision")
		}
		if currentRevision != opts.ExpectedRevision {
			return ErrProjectFileRevisionConflict
		}
	}

	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return errors.WrapIf(err, "open project directory")
	}
	defer func() { _ = root.Close() }()

	protected := ProtectedProjectFilePaths(opts.ComposeFileName)
	for _, change := range changes {
		if err := applyProjectFileChangeInternal(root, protected, change); err != nil {
			return err
		}
	}

	return nil
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

func NormalizeProjectRelativePath(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("path contains a null byte")
	}
	if strings.Contains(trimmed, "\\") {
		return "", errors.New("path must use forward slashes")
	}
	if path.IsAbs(trimmed) || filepath.IsAbs(trimmed) {
		return "", errors.New("absolute paths are not allowed")
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path traversal is not allowed")
	}
	for segment := range strings.SplitSeq(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("path contains an invalid segment")
		}
	}

	return cleaned, nil
}

func ValidateProjectFileName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("name is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("name contains a null byte")
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return "", errors.New("name must not contain path separators")
	}
	if filepath.VolumeName(trimmed) != "" {
		return "", errors.New("name must not contain a volume prefix")
	}
	if trimmed == "." || trimmed == ".." || filepath.Base(trimmed) != trimmed {
		return "", errors.New("invalid name")
	}
	return trimmed, nil
}

func applyProjectFileChangeInternal(root *os.Root, protected map[string]bool, change project.ProjectFileChange) error {
	rel, err := NormalizeProjectRelativePath(change.RelativePath)
	if err != nil {
		return errors.WrapIf(err, "invalid project file path")
	}

	switch change.Operation {
	case project.FileOpCreateFile:
		if change.Content == nil {
			return errors.New("file content is required")
		}
		return createManagedProjectFileInternal(root, protected, rel, *change.Content)
	case project.FileOpCreateFolder:
		return createManagedProjectFolderInternal(root, protected, rel)
	case project.FileOpUpdateFile:
		if change.Content == nil {
			return errors.New("file content is required")
		}
		return updateManagedProjectFileInternal(root, protected, rel, *change.Content)
	case project.FileOpRename:
		newName, err := ValidateProjectFileName(change.NewName)
		if err != nil {
			return errors.WrapIf(err, "invalid project file name")
		}
		return renameManagedProjectPathInternal(root, protected, rel, newName)
	case project.FileOpMove:
		return moveManagedProjectPathInternal(root, protected, rel, change.NewParentPath)
	case project.FileOpDelete:
		return deleteManagedProjectPathInternal(root, protected, rel, change.Recursive)
	default:
		return errors.Errorf("unsupported project file operation %q", change.Operation)
	}
}

func createManagedProjectFileInternal(root *os.Root, protected map[string]bool, rel, content string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := validateProjectTextContentInternal(content); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(rel)); err != nil {
		return err
	}

	if err := root.MkdirAll(path.Dir(rel), pkgutils.DirPerm); err != nil {
		return mapProjectRootErrorInternal("create parent directory", err)
	}

	// O_EXCL makes the exists-check-and-create atomic; os.Root confines the
	// path to the project directory in the kernel.
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, pkgutils.FilePerm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.Errorf("project file already exists: %s", rel)
		}
		return mapProjectRootErrorInternal("create project file", err)
	}
	_, writeErr := f.WriteString(content)
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return errors.WrapIf(writeErr, "create project file")
	}
	return nil
}

func createManagedProjectFolderInternal(root *os.Root, protected map[string]bool, rel string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	if _, err := root.Lstat(rel); err == nil {
		return errors.Errorf("project folder already exists: %s", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return mapProjectRootErrorInternal("inspect project folder", err)
	}
	if err := root.MkdirAll(rel, pkgutils.DirPerm); err != nil {
		return mapProjectRootErrorInternal("create project folder", err)
	}
	return nil
}

func updateManagedProjectFileInternal(root *os.Root, protected map[string]bool, rel, content string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := validateProjectTextContentInternal(content); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Errorf("project file not found: %s", rel)
		}
		return mapProjectRootErrorInternal("inspect project file", err)
	}
	if info.IsDir() {
		return errors.Errorf("path is a folder: %s", rel)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink files are not supported")
	}

	if err := root.WriteFile(rel, []byte(content), pkgutils.FilePerm); err != nil {
		return mapProjectRootErrorInternal("update project file", err)
	}
	return nil
}

func renameManagedProjectPathInternal(root *os.Root, protected map[string]bool, rel, newName string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Errorf("project path not found: %s", rel)
		}
		return mapProjectRootErrorInternal("inspect project path", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink paths are not supported")
	}

	targetRel := path.Join(path.Dir(rel), newName)
	if err := ensureWritableProjectRelPathInternal(protected, targetRel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(targetRel)); err != nil {
		return err
	}
	if _, err := root.Lstat(targetRel); err == nil {
		return errors.Errorf("project path already exists: %s", targetRel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return mapProjectRootErrorInternal("inspect project path", err)
	}

	if err := root.Rename(rel, targetRel); err != nil {
		return mapProjectRootErrorInternal("rename project path", err)
	}
	return nil
}

func normalizeOptionalProjectParentPathInternal(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	return NormalizeProjectRelativePath(input)
}

func moveManagedProjectPathInternal(root *os.Root, protected map[string]bool, rel, newParentPath string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	parentRel, err := normalizeOptionalProjectParentPathInternal(newParentPath)
	if err != nil {
		return errors.WrapIf(err, "invalid project parent path")
	}
	if parentRel != "" {
		if err := ensureWritableProjectRelPathInternal(protected, parentRel); err != nil {
			return err
		}
	}

	sourceInfo, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Errorf("project path not found: %s", rel)
		}
		return mapProjectRootErrorInternal("inspect project path", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink paths are not supported")
	}
	if sourceInfo.IsDir() && parentRel != "" && projectFilePathMatchesInternal(parentRel, rel) {
		return errors.New("folder cannot be moved into itself or a descendant")
	}

	if err := validateProjectMoveParentInternal(root, parentRel); err != nil {
		return err
	}

	targetRel := path.Base(rel)
	if parentRel != "" {
		targetRel = path.Join(parentRel, path.Base(rel))
	}
	if targetRel == rel {
		return errors.New("project path is already in the destination folder")
	}
	if err := ensureWritableProjectRelPathInternal(protected, targetRel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, path.Dir(targetRel)); err != nil {
		return err
	}
	if _, err := root.Lstat(targetRel); err == nil {
		return errors.Errorf("project path already exists: %s", targetRel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return mapProjectRootErrorInternal("inspect project path", err)
	}

	if err := root.Rename(rel, targetRel); err != nil {
		return mapProjectRootErrorInternal("move project path", err)
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
			return errors.Errorf("destination folder not found: %s", parentRel)
		}
		return mapProjectRootErrorInternal("inspect destination folder", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink destination folders are not supported")
	}
	if !parentInfo.IsDir() {
		return errors.Errorf("destination path is not a folder: %s", parentRel)
	}
	return nil
}

func deleteManagedProjectPathInternal(root *os.Root, protected map[string]bool, rel string, recursive bool) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := ensureProjectPathHasNoSymlinkInternal(root, rel); err != nil {
		return err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Errorf("project path not found: %s", rel)
		}
		return mapProjectRootErrorInternal("inspect project path", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink paths are not supported")
	}
	if info.IsDir() && !recursive {
		empty, err := isDirectoryEmptyInternal(root, rel)
		if err != nil {
			return err
		}
		if !empty {
			return errors.New("folder is not empty")
		}
	}

	if info.IsDir() {
		if err := root.RemoveAll(rel); err != nil {
			return mapProjectRootErrorInternal("delete project folder", err)
		}
		return nil
	}
	if err := root.Remove(rel); err != nil {
		return mapProjectRootErrorInternal("delete project file", err)
	}
	return nil
}

func mapProjectRootErrorInternal(action string, err error) error {
	return errors.WrapIff(err, "%s", action)
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
			return mapProjectRootErrorInternal("inspect project path", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.WrapIf(ErrProjectFileSymlinkPath, "symlink paths are not supported")
		}
	}
	return nil
}

func ensureWritableProjectRelPathInternal(protected map[string]bool, rel string) error {
	if rel == "." || rel == "" {
		return errors.New("project root cannot be modified")
	}
	rootName, _, _ := strings.Cut(rel, "/")
	if protected[rel] || protected[rootName] {
		return errors.WrapIff(ErrProjectFileProtectedPath, "%s", rel)
	}
	return nil
}

func projectFilePathMatchesInternal(relativePath string, rootPath string) bool {
	return relativePath == rootPath || strings.HasPrefix(relativePath, rootPath+"/")
}

func validateProjectTextContentInternal(content string) error {
	if len(content) > MaxManagedProjectFileBytes {
		return errors.Errorf("file exceeds %d byte limit", MaxManagedProjectFileBytes)
	}
	if !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 {
		return errors.New("binary files are not supported")
	}
	return nil
}

func isDirectoryEmptyInternal(root *os.Root, rel string) (bool, error) {
	f, err := root.Open(rel)
	if err != nil {
		return false, errors.WrapIf(err, "open folder")
	}
	defer func() { _ = f.Close() }()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, errors.WrapIf(err, "read folder")
	}
	return false, nil
}

func writeProjectFileRevisionEntryInternal(h hash.Hash, rel string, info fs.FileInfo, isDir, protected bool) {
	kind := "file"
	if isDir {
		kind = "dir"
	}
	protectedFlag := ""
	if protected {
		protectedFlag = "protected"
	}

	entry := strings.Join([]string{
		rel,
		kind,
		strconv.FormatInt(info.Size(), 10),
		strconv.FormatInt(info.ModTime().UnixNano(), 10),
		info.Mode().String(),
		protectedFlag,
	}, "\x00")
	_, _ = io.WriteString(h, entry)
	_, _ = h.Write([]byte{'\n'})
}
