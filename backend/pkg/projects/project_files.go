package projects

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/project"
)

const (
	MaxManagedProjectFileBytes  = 1024 * 1024
	ProjectFileTreeUseScanDepth = -1
)

var (
	ErrProjectFileRevisionConflict        = errors.New("project file tree changed; refresh the project and try again")
	ErrProjectFileOutsideProjectDirectory = errors.New("path is outside project directory")
	ErrProjectFileProtectedPath           = errors.New("protected project file cannot be modified")
	ErrProjectFileSymlinkPath             = errors.New("symlink project paths are not supported")
)

type ProjectFileApplyOptions struct {
	ExpectedRevision string
	MaxDepth         int
	SkipDirectories  string
	ComposeFileName  string
}

func ReadProjectFileTree(projectPath string, maxDepth int, skipDirectories, composeFileName string) ([]project.ProjectFile, string, error) {
	if maxDepth == ProjectFileTreeUseScanDepth {
		maxDepth = config.Load().ProjectScanMaxDepth
	}

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	protected := ProtectedProjectFilePaths(composeFileName)
	skipDirs := projectScanSkipDirectorySetInternal(skipDirectories)
	files := []project.ProjectFile{}
	revisionEntries := []string{}

	err = filepath.WalkDir(projectAbs, func(absPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if absPath == projectAbs {
			return nil
		}

		rel, err := filepath.Rel(projectAbs, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		depth := strings.Count(rel, "/") + 1
		if depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() && skipDirs[entry.Name()] {
			return filepath.SkipDir
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		isProtected := protected[rel]
		revisionEntries = append(revisionEntries, projectFileRevisionEntryInternal(absPath, rel, info, entry.IsDir(), isProtected))

		if isProtected {
			return nil
		}

		files = append(files, project.ProjectFile{
			Path:         absPath,
			RelativePath: rel,
			Name:         entry.Name(),
			IsDirectory:  entry.IsDir(),
			Size:         fileSizeForTreeInternal(info, entry.IsDir()),
			ModTime:      info.ModTime(),
			Protected:    false,
		})

		return nil
	})
	if err != nil {
		return nil, "", err
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDirectory != files[j].IsDirectory {
			return files[i].IsDirectory
		}
		return files[i].RelativePath < files[j].RelativePath
	})
	sort.Strings(revisionEntries)

	sum := sha256.Sum256([]byte(strings.Join(revisionEntries, "\n")))
	return files, hex.EncodeToString(sum[:]), nil
}

func ApplyProjectFileDrafts(projectPath string, drafts []project.ProjectFileDraft, opts ProjectFileApplyOptions) error {
	if len(drafts) == 0 {
		return nil
	}

	changes := make([]project.ProjectFileChange, 0, len(drafts))
	for _, draft := range drafts {
		operation := "create_file"
		var content *string
		if draft.IsDirectory {
			operation = "create_folder"
		} else {
			contentValue := draft.Content
			content = &contentValue
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

	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	if opts.ExpectedRevision != "" {
		_, currentRevision, err := ReadProjectFileTree(projectAbs, opts.MaxDepth, opts.SkipDirectories, opts.ComposeFileName)
		if err != nil {
			return fmt.Errorf("read project file tree revision: %w", err)
		}
		if currentRevision != opts.ExpectedRevision {
			return ErrProjectFileRevisionConflict
		}
	}

	protected := ProtectedProjectFilePaths(opts.ComposeFileName)
	for _, change := range changes {
		if err := applyProjectFileChangeInternal(projectAbs, protected, change); err != nil {
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
	for _, segment := range strings.Split(cleaned, "/") {
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

func applyProjectFileChangeInternal(projectAbs string, protected map[string]bool, change project.ProjectFileChange) error {
	rel, err := NormalizeProjectRelativePath(change.RelativePath)
	if err != nil {
		return fmt.Errorf("invalid project file path: %w", err)
	}

	switch change.Operation {
	case "create_file":
		if change.Content == nil {
			return errors.New("file content is required")
		}
		return createManagedProjectFileInternal(projectAbs, protected, rel, *change.Content)
	case "create_folder":
		return createManagedProjectFolderInternal(projectAbs, protected, rel)
	case "update_file":
		if change.Content == nil {
			return errors.New("file content is required")
		}
		return updateManagedProjectFileInternal(projectAbs, protected, rel, *change.Content)
	case "rename":
		newName, err := ValidateProjectFileName(change.NewName)
		if err != nil {
			return fmt.Errorf("invalid project file name: %w", err)
		}
		return renameManagedProjectPathInternal(projectAbs, protected, rel, newName)
	case "move":
		return moveManagedProjectPathInternal(projectAbs, protected, rel, change.NewParentPath)
	case "delete":
		return deleteManagedProjectPathInternal(projectAbs, protected, rel, change.Recursive)
	default:
		return fmt.Errorf("unsupported project file operation %q", change.Operation)
	}
}

func createManagedProjectFileInternal(projectAbs string, protected map[string]bool, rel, content string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := validateProjectTextContentInternal(content); err != nil {
		return err
	}

	target, err := validatedProjectTargetPathInternal(projectAbs, rel, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("project file already exists: %s", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), pkgutils.DirPerm); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("create project file: %w", err)
	}
	return nil
}

func createManagedProjectFolderInternal(projectAbs string, protected map[string]bool, rel string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}

	target, err := validatedProjectTargetPathInternal(projectAbs, rel, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("project folder already exists: %s", rel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project folder: %w", err)
	}
	if err := os.MkdirAll(target, pkgutils.DirPerm); err != nil {
		return fmt.Errorf("create project folder: %w", err)
	}
	return nil
}

func updateManagedProjectFileInternal(projectAbs string, protected map[string]bool, rel, content string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}
	if err := validateProjectTextContentInternal(content); err != nil {
		return err
	}

	target, err := validatedProjectTargetPathInternal(projectAbs, rel, true)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project file not found: %s", rel)
		}
		return fmt.Errorf("inspect project file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a folder: %s", rel)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink files are not supported: %w", ErrProjectFileSymlinkPath)
	}

	if err := os.WriteFile(target, []byte(content), pkgutils.FilePerm); err != nil {
		return fmt.Errorf("update project file: %w", err)
	}
	return nil
}

func renameManagedProjectPathInternal(projectAbs string, protected map[string]bool, rel, newName string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}

	source, err := validatedProjectTargetPathInternal(projectAbs, rel, true)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project path not found: %s", rel)
		}
		return fmt.Errorf("inspect project path: %w", err)
	}

	targetRel := path.Join(path.Dir(rel), newName)
	if path.Dir(rel) == "." {
		targetRel = newName
	}
	if err := ensureWritableProjectRelPathInternal(protected, targetRel); err != nil {
		return err
	}
	target, err := validatedProjectTargetPathInternal(projectAbs, targetRel, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("project path already exists: %s", targetRel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project path: %w", err)
	}

	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("rename project path: %w", err)
	}
	return nil
}

func normalizeOptionalProjectParentPathInternal(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	return NormalizeProjectRelativePath(input)
}

func moveManagedProjectPathInternal(projectAbs string, protected map[string]bool, rel, newParentPath string) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}

	parentRel, err := normalizeOptionalProjectParentPathInternal(newParentPath)
	if err != nil {
		return fmt.Errorf("invalid project parent path: %w", err)
	}
	if parentRel != "" {
		if err := ensureWritableProjectRelPathInternal(protected, parentRel); err != nil {
			return err
		}
	}

	source, err := validatedProjectTargetPathInternal(projectAbs, rel, false)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project path not found: %s", rel)
		}
		return fmt.Errorf("inspect project path: %w", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %w", ErrProjectFileSymlinkPath)
	}
	if sourceInfo.IsDir() && parentRel != "" && projectFilePathMatchesInternal(parentRel, rel) {
		return errors.New("folder cannot be moved into itself or a descendant")
	}

	if parentRel != "" {
		parent, err := validatedProjectTargetPathInternal(projectAbs, parentRel, false)
		if err != nil {
			return err
		}
		parentInfo, err := os.Lstat(parent)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("destination folder not found: %s", parentRel)
			}
			return fmt.Errorf("inspect destination folder: %w", err)
		}
		if parentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink destination folders are not supported: %w", ErrProjectFileSymlinkPath)
		}
		if !parentInfo.IsDir() {
			return fmt.Errorf("destination path is not a folder: %s", parentRel)
		}
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
	target, err := validatedProjectTargetPathInternal(projectAbs, targetRel, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("project path already exists: %s", targetRel)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project path: %w", err)
	}

	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("move project path: %w", err)
	}
	return nil
}

func deleteManagedProjectPathInternal(projectAbs string, protected map[string]bool, rel string, recursive bool) error {
	if err := ensureWritableProjectRelPathInternal(protected, rel); err != nil {
		return err
	}

	target, err := validatedProjectTargetPathInternal(projectAbs, rel, true)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project path not found: %s", rel)
		}
		return fmt.Errorf("inspect project path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %w", ErrProjectFileSymlinkPath)
	}
	if info.IsDir() && !recursive {
		empty, err := isDirectoryEmptyInternal(target)
		if err != nil {
			return err
		}
		if !empty {
			return errors.New("folder is not empty")
		}
	}

	if info.IsDir() {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("delete project folder: %w", err)
		}
		return nil
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("delete project file: %w", err)
	}
	return nil
}

func validatedProjectTargetPathInternal(projectAbs, rel string, mustExist bool) (string, error) {
	target := filepath.Clean(filepath.Join(projectAbs, filepath.FromSlash(rel)))
	if !IsSafeSubdirectory(projectAbs, target) || target == projectAbs {
		return "", ErrProjectFileOutsideProjectDirectory
	}
	if err := validateExistingProjectAncestorsInternal(projectAbs, target, mustExist); err != nil {
		return "", err
	}
	return target, nil
}

func validateExistingProjectAncestorsInternal(projectAbs, target string, mustExist bool) error {
	info, err := os.Lstat(target)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %w", ErrProjectFileSymlinkPath)
		}
	} else if mustExist || !errors.Is(err, os.ErrNotExist) {
		return err
	}

	current := filepath.Dir(target)
	for current != projectAbs && IsSafeSubdirectory(projectAbs, current) {
		info, err := os.Lstat(current)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink parent directories are not supported: %w", ErrProjectFileSymlinkPath)
			}
			if !info.IsDir() {
				return fmt.Errorf("parent path is not a directory: %s", current)
			}
		case errors.Is(err, os.ErrNotExist):
			// A later create operation may create this parent. Keep walking to
			// validate the nearest existing ancestor.
		default:
			return err
		}
		current = filepath.Dir(current)
	}
	return nil
}

func ensureWritableProjectRelPathInternal(protected map[string]bool, rel string) error {
	if rel == "." || rel == "" {
		return errors.New("project root cannot be modified")
	}
	rootName := strings.Split(rel, "/")[0]
	if protected[rel] || protected[rootName] {
		return fmt.Errorf("%w: %s", ErrProjectFileProtectedPath, rel)
	}
	return nil
}

func projectFilePathMatchesInternal(relativePath string, rootPath string) bool {
	return relativePath == rootPath || strings.HasPrefix(relativePath, rootPath+"/")
}

func validateProjectTextContentInternal(content string) error {
	if len(content) > MaxManagedProjectFileBytes {
		return fmt.Errorf("file exceeds %d byte limit", MaxManagedProjectFileBytes)
	}
	if !utf8.ValidString(content) || IsBinaryProjectFileContent([]byte(content)) {
		return errors.New("binary files are not supported")
	}
	return nil
}

func isDirectoryEmptyInternal(dir string) (bool, error) {
	f, err := os.Open(dir)
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

func fileSizeForTreeInternal(info os.FileInfo, isDir bool) int64 {
	if isDir {
		return 0
	}
	return info.Size()
}

func projectFileRevisionEntryInternal(absPath, rel string, info os.FileInfo, isDir, protected bool) string {
	var contentHash string
	if !isDir && info.Size() <= MaxManagedProjectFileBytes {
		if content, err := os.ReadFile(absPath); err == nil {
			sum := sha256.Sum256(content)
			contentHash = hex.EncodeToString(sum[:])
		}
	}

	var b bytes.Buffer
	b.WriteString(rel)
	b.WriteByte('\x00')
	if isDir {
		b.WriteString("dir")
	} else {
		b.WriteString("file")
	}
	b.WriteByte('\x00')
	b.WriteString(fmt.Sprintf("%d", info.Size()))
	b.WriteByte('\x00')
	b.WriteString(fmt.Sprintf("%d", info.ModTime().UnixNano()))
	b.WriteByte('\x00')
	b.WriteString(info.Mode().String())
	b.WriteByte('\x00')
	if protected {
		b.WriteString("protected")
	}
	b.WriteByte('\x00')
	b.WriteString(contentHash)
	return b.String()
}
