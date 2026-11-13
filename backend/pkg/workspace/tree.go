package workspace

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
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	kitfs "kit.getarcane.app/fs"

	"getarcane.dev/types/v3"
)

type (
	Options struct {
		ProtectedFiles   map[string]bool
		ExpectedRevision string
		SkipDirectories  string
		MaxDepth         int
		MaxEntries       int
		MaxFileSizeBytes int64
	}

	treeWalker struct {
		ctx              context.Context
		revisionHash     hash.Hash
		protected        map[string]bool
		skipDirs         map[string]bool
		rootAbs          string
		files            []types.WorkspaceFileEntry
		maxDepth         int
		maxEntries       int
		maxFileSizeBytes int64
		entryCount       int
		truncated        bool
	}
)

var (
	ErrRevisionConflict = errors.New("workspace changed; refresh it and try again")
	ErrProtectedPath    = errors.New("protected configuration cannot be modified through the workspace")
	ErrSymlinkPath      = errors.New("symlink workspace paths are not supported")
)

func Read(ctx context.Context, rootPath string, opts Options) ([]types.WorkspaceFileEntry, string, bool, error) {
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolve workspace root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	walker := &treeWalker{
		ctx:              ctx,
		rootAbs:          rootAbs,
		maxDepth:         opts.MaxDepth,
		maxEntries:       opts.MaxEntries,
		maxFileSizeBytes: kit.Ternary(opts.MaxFileSizeBytes <= 0, MaxFileSizeBytes(DefaultMaxFileSizeMB), opts.MaxFileSizeBytes),
		protected:        ProtectedPaths(ctx, rootAbs, opts.ProtectedFiles),
		skipDirs:         SkipDirectorySet(opts.SkipDirectories),
		files:            []types.WorkspaceFileEntry{},
		revisionHash:     sha256.New(),
	}

	if walkErr := walker.walk(); walkErr != nil {
		return nil, "", false, walkErr
	}

	slices.SortFunc(walker.files, CompareEntries)

	return walker.files, hex.EncodeToString(walker.revisionHash.Sum(nil)), walker.truncated, nil
}

func ListFolder(ctx context.Context, rootPath, relativePath string, opts Options) (*types.Workspace, error) {
	skipDirs := SkipDirectorySet(opts.SkipDirectories)
	if relativePath != "" && slices.ContainsFunc(strings.Split(relativePath, "/"), func(segment string) bool { return skipDirs[segment] }) {
		return nil, fmt.Errorf("workspace folder %s: %w", relativePath, fs.ErrNotExist)
	}
	protected := ProtectedPaths(ctx, rootPath, opts.ProtectedFiles)
	files := make([]types.WorkspaceFileEntry, 0, min(opts.MaxEntries, 256))
	truncated := false
	// Skipped and protected names are filtered out below, so the limit leaves room for them.
	result, err := acfs.Root{Path: rootPath}.ListEach(ctx, "/"+relativePath, acfstypes.ListOptions{MaxEntries: opts.MaxEntries + len(skipDirs) + len(protected)}, func(entry acfstypes.Entry) error {
		switch {
		case entry.IsDirectory && skipDirs[entry.Name], relativePath == "" && protected[entry.Name]:
			return nil
		case len(files) >= opts.MaxEntries:
			truncated = true
			return fs.SkipAll
		}
		files = append(files, EntryFromACFS(entry, opts.MaxFileSizeBytes))
		return nil
	})
	switch {
	case errors.Is(err, fs.ErrPermission) && relativePath != "":
		slog.DebugContext(ctx, "Skipping unreadable workspace folder", "relativePath", relativePath, "error", err)
		return &types.Workspace{}, nil
	case err != nil && !errors.Is(err, fs.SkipAll):
		return nil, err
	}
	slices.SortFunc(files, CompareEntries)
	return &types.Workspace{Files: files, FileTreeTruncated: truncated || result.Truncated}, nil
}

func (w *treeWalker) walk() error {
	for queue := []string{"."}; len(queue) > 0; queue = queue[1:] {
		entries, err := acfs.Root{Path: w.rootAbs}.List(w.ctx, kitfs.LogicalPath(queue[0]))
		if err != nil {
			if queue[0] != "." && (errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist)) {
				slog.DebugContext(w.ctx, "Skipping unreadable workspace subdirectory", "relativePath", queue[0], "error", err)
				continue
			}
			return err
		}
		for _, entry := range entries {
			rel := path.Join(queue[0], entry.Name)
			descend := w.visit(rel, entry)
			if w.truncated {
				return nil
			}
			if descend {
				queue = append(queue, rel)
			}
		}
	}
	return nil
}

func (w *treeWalker) visit(rel string, entry acfstypes.Entry) bool {
	// Entries are visited in a fixed level-by-level, name-sorted order, so
	// cutting off after maxEntries still yields a stable revision as long as
	// the concurrency compare walk uses the same cap.
	if w.maxEntries > 0 && w.entryCount >= w.maxEntries {
		w.truncated = true
		return false
	}

	depth := strings.Count(rel, "/") + 1
	if depth > w.maxDepth {
		return false
	}

	if entry.IsDirectory && w.skipDirs[entry.Name] {
		return false
	}

	isProtected := w.protected[rel]
	if isProtected {
		return entry.IsDirectory
	}

	// Protected Compose configuration is intentionally absent from both the
	// workspace response and its revision so configuration saves cannot make
	// a workspace draft stale.
	kind := "file"
	switch {
	case entry.IsDirectory:
		kind = "dir"
	case entry.IsSymlink:
		kind = "symlink"
	case !os.FileMode(entry.UnixMode).IsRegular():
		kind = "special"
	}
	kit.WriteRecord(w.revisionHash, rel, kind)
	w.entryCount++

	size := entry.Size
	if entry.IsDirectory {
		size = 0
	}
	workspaceEntry := types.WorkspaceFileEntry{
		Path:         filepath.Join(w.rootAbs, filepath.FromSlash(rel)),
		RelativePath: rel,
		Name:         entry.Name,
		IsDirectory:  entry.IsDirectory,
		Size:         size,
		ModTime:      time.Unix(0, entry.ModTimeUnixNano),
		Mode:         os.FileMode(entry.UnixMode).String(),
		IsSymlink:    entry.IsSymlink,
	}
	switch kind {
	case "dir":
		workspaceEntry.Editable = true
	case "symlink":
		workspaceEntry.ReadOnlyReason = types.FileReadOnlySymlink
		if target, readlinkErr := (acfs.Host{}).Readlink(w.ctx, workspaceEntry.Path); readlinkErr == nil {
			workspaceEntry.LinkTarget = target
		}
	case "special":
		workspaceEntry.ReadOnlyReason = types.FileReadOnlySpecial
	case "file":
		workspaceEntry.Editable, workspaceEntry.ReadOnlyReason = classifyFile(w.ctx, w.rootAbs, rel, size, w.maxFileSizeBytes)
	}
	w.files = append(w.files, workspaceEntry)

	return entry.IsDirectory
}

func classifyFile(ctx context.Context, rootPath, rel string, size, maxFileSizeBytes int64) (bool, string) {
	if size > maxFileSizeBytes {
		return false, types.FileReadOnlyTooLarge
	}
	content, err := acfs.Root{Path: rootPath}.ReadFile(ctx, "/"+rel)
	if err != nil || !bytes.Equal(bytes.ToValidUTF8(content, []byte{}), content) || bytes.IndexByte(content, 0) >= 0 {
		return false, types.FileReadOnlyBinary
	}
	return true, ""
}

func Apply(ctx context.Context, rootPath string, changes []types.WorkspaceFileChange, uploads map[int][]byte, opts Options) error {
	uploadReferences := make([]UploadReference, 0, len(changes))
	for _, change := range changes {
		uploadReferences = append(uploadReferences, UploadReference{
			Operation:     change.Operation,
			UploadIndex:   change.UploadIndex,
			BaselineIndex: change.BaselineIndex,
		})
	}
	if err := ValidateUploadIndices(uploadReferences, len(uploads), types.FileOpCreateFile, types.FileOpUpdateFile); err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	if opts.ExpectedRevision != "" {
		_, currentRevision, _, err := Read(ctx, rootPath, opts)
		if err != nil {
			return fmt.Errorf("read workspace revision: %w", err)
		}
		if currentRevision != opts.ExpectedRevision {
			return ErrRevisionConflict
		}
	}

	maxFileSizeBytes := kit.Ternary(opts.MaxFileSizeBytes <= 0, MaxFileSizeBytes(DefaultMaxFileSizeMB), opts.MaxFileSizeBytes)
	for _, change := range changes {
		// Recomputed per change: an earlier change may have removed a directory
		// that shadowed a protected name.
		protected := ProtectedPaths(ctx, rootPath, opts.ProtectedFiles)
		if applyChangeErr := applyChange(ctx, acfs.Root{Path: rootPath}, protected, change, uploads, maxFileSizeBytes); applyChangeErr != nil {
			return applyChangeErr
		}
	}

	return nil
}

func ProtectedPaths(ctx context.Context, rootPath string, files map[string]bool) map[string]bool {
	protected := maps.Clone(files)
	for name := range protected {
		if entry, err := (acfs.Root{Path: rootPath}).Stat(ctx, "/"+name, acfstypes.StatOptions{}); err == nil && entry.IsDirectory {
			delete(protected, name)
		}
	}
	return protected
}

func SkipDirectorySet(skipDirectories string) map[string]bool {
	dirs := map[string]bool{}
	for dir := range strings.SplitSeq(skipDirectories, ",") {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			dirs[dir] = true
		}
	}

	// Never allow .git contents to be exposed through the project workspace.
	dirs[".git"] = true

	return dirs
}

func applyChange(ctx context.Context, root acfs.Root, protected map[string]bool, change types.WorkspaceFileChange, uploads map[int][]byte, maxFileSizeBytes int64) error {
	rel, err := kit.NormalizeRelativePath(change.RelativePath)
	if err != nil {
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	switch change.Operation {
	case types.FileOpCreateFile:
		return createFile(ctx, root, protected, rel, uploads[*change.UploadIndex], maxFileSizeBytes)
	case types.FileOpCreateFolder:
		return createFolder(ctx, root, protected, rel)
	case types.FileOpUpdateFile:
		var baseline []byte
		if change.BaselineIndex != nil {
			baseline = uploads[*change.BaselineIndex]
		}
		return updateFile(ctx, root, protected, rel, uploads[*change.UploadIndex], baseline, change.BaselineIndex != nil, maxFileSizeBytes)
	case types.FileOpRename:
		newName, validateFileNameErr := kit.ValidateFileName(change.NewName)
		if validateFileNameErr != nil {
			return fmt.Errorf("invalid workspace file name: %w", validateFileNameErr)
		}
		return renamePath(ctx, root, protected, rel, newName)
	case types.FileOpMove:
		return movePath(ctx, root, protected, rel, change.NewParentPath)
	case types.FileOpDelete:
		return deletePath(ctx, root, protected, rel, change.Recursive)
	default:
		return fmt.Errorf("unsupported workspace operation %q", change.Operation)
	}
}

func createFile(ctx context.Context, root acfs.Root, protected map[string]bool, rel string, content []byte, maxFileSizeBytes int64) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, path.Dir(rel)); err != nil {
		return err
	}

	if err := root.MkdirAll(ctx, kitfs.LogicalPath(path.Dir(rel)), kitfs.DirPerm); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	f, err := root.OpenFile(ctx, "/"+rel, acfs.OpenOptions{
		Mode:      kitfs.FilePerm,
		Write:     true,
		Create:    true,
		Exclusive: true,
	})
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("workspace file already exists: %s: %w", rel, acfs.ErrAlreadyExists)
		}
		return fmt.Errorf("create workspace file: %w", err)
	}
	_, writeErr := f.Write(content)
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return fmt.Errorf("create workspace file: %w", writeErr)
	}
	return nil
}

func createFolder(ctx context.Context, root acfs.Root, protected map[string]bool, rel string) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, rel); err != nil {
		return err
	}

	if _, err := root.Stat(ctx, "/"+rel, acfstypes.StatOptions{}); err == nil {
		return fmt.Errorf("workspace folder already exists: %s: %w", rel, acfs.ErrAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect workspace folder: %w", err)
	}
	if err := root.MkdirAll(ctx, "/"+rel, kitfs.DirPerm); err != nil {
		return fmt.Errorf("create workspace folder: %w", err)
	}
	return nil
}

func updateFile(ctx context.Context, root acfs.Root, protected map[string]bool, rel string, content, baseline []byte, baselineSet bool, maxFileSizeBytes int64) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ValidateContentSize(content, maxFileSizeBytes); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, rel); err != nil {
		return err
	}

	entry, err := root.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace file not found: %s", rel)
		}
		return fmt.Errorf("inspect workspace file: %w", err)
	}
	if entry.IsDirectory {
		return fmt.Errorf("path is a folder: %s", rel)
	}
	if entry.IsSymlink {
		return fmt.Errorf("symlink files are not supported: %w", ErrSymlinkPath)
	}

	if baselineSet {
		reader, _, openErr := root.OpenRead(ctx, "/"+rel, int64(len(baseline))+1)
		if openErr != nil {
			return fmt.Errorf("read workspace file: %w", openErr)
		}
		current, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return fmt.Errorf("read workspace file: %w", readErr)
		}
		if !bytes.Equal(current, baseline) {
			return fmt.Errorf("file content changed since it was loaded: %w", ErrRevisionConflict)
		}
	}

	file, err := root.OpenFile(ctx, "/"+rel, acfs.OpenOptions{
		Write:    true,
		Truncate: true,
	})
	if err != nil {
		return fmt.Errorf("update workspace file: %w", err)
	}
	if _, err = file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("update workspace file: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("update workspace file: %w", err)
	}
	return nil
}

func renamePath(ctx context.Context, root acfs.Root, protected map[string]bool, rel, newName string) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, rel); err != nil {
		return err
	}

	entry, err := root.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect workspace path: %w", err)
	}
	if entry.IsSymlink {
		return fmt.Errorf("symlink paths are not supported: %w", ErrSymlinkPath)
	}

	targetRel := path.Join(path.Dir(rel), newName)
	if writableErr := ensureWritableRelPath(protected, targetRel); writableErr != nil {
		return writableErr
	}
	if symlinkErr := ensureNoSymlink(ctx, root, path.Dir(targetRel)); symlinkErr != nil {
		return symlinkErr
	}
	if _, lstatErr := root.Stat(ctx, "/"+targetRel, acfstypes.StatOptions{}); lstatErr == nil {
		return fmt.Errorf("workspace path already exists: %s", targetRel)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return fmt.Errorf("inspect workspace path: %w", lstatErr)
	}

	if renameErr := root.Rename(ctx, "/"+rel, "/"+targetRel); renameErr != nil {
		return fmt.Errorf("rename workspace path: %w", renameErr)
	}
	return nil
}

func NormalizeParentPath(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	return kit.NormalizeRelativePath(input)
}

func movePath(ctx context.Context, root acfs.Root, protected map[string]bool, rel, newParentPath string) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, rel); err != nil {
		return err
	}

	parentRel, err := NormalizeParentPath(newParentPath)
	if err != nil {
		return fmt.Errorf("invalid workspace parent path: %w", err)
	}
	if parentRel != "" {
		if writableErr := ensureWritableRelPath(protected, parentRel); writableErr != nil {
			return writableErr
		}
	}

	source, err := root.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect workspace path: %w", err)
	}
	if source.IsSymlink {
		return fmt.Errorf("symlink paths are not supported: %w", ErrSymlinkPath)
	}
	if source.IsDirectory && parentRel != "" && kit.FilePathMatches(parentRel, rel) {
		return errors.New("folder cannot be moved into itself or a descendant")
	}

	if parentErr := validateMoveParent(ctx, root, parentRel); parentErr != nil {
		return parentErr
	}

	targetRel := path.Base(rel)
	if parentRel != "" {
		targetRel = path.Join(parentRel, path.Base(rel))
	}
	if targetRel == rel {
		return errors.New("workspace path is already in the destination folder")
	}
	if writableTargetErr := ensureWritableRelPath(protected, targetRel); writableTargetErr != nil {
		return writableTargetErr
	}
	if symlinkErr := ensureNoSymlink(ctx, root, path.Dir(targetRel)); symlinkErr != nil {
		return symlinkErr
	}
	if _, lstatErr := root.Stat(ctx, "/"+targetRel, acfstypes.StatOptions{}); lstatErr == nil {
		return fmt.Errorf("workspace path already exists: %s", targetRel)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return fmt.Errorf("inspect workspace path: %w", lstatErr)
	}

	if renameErr := root.Rename(ctx, "/"+rel, "/"+targetRel); renameErr != nil {
		return fmt.Errorf("move workspace path: %w", renameErr)
	}
	return nil
}

func validateMoveParent(ctx context.Context, root acfs.Root, parentRel string) error {
	if parentRel == "" {
		return nil
	}
	if err := ensureNoSymlink(ctx, root, parentRel); err != nil {
		return err
	}

	parent, err := root.Stat(ctx, "/"+parentRel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("destination folder not found: %s", parentRel)
		}
		return fmt.Errorf("inspect destination folder: %w", err)
	}
	if parent.IsSymlink {
		return fmt.Errorf("symlink destination folders are not supported: %w", ErrSymlinkPath)
	}
	if !parent.IsDirectory {
		return fmt.Errorf("destination path is not a folder: %s", parentRel)
	}
	return nil
}

func deletePath(ctx context.Context, root acfs.Root, protected map[string]bool, rel string, recursive bool) error {
	if err := ensureWritableRelPath(protected, rel); err != nil {
		return err
	}
	if err := ensureNoSymlink(ctx, root, rel); err != nil {
		return err
	}

	entry, err := root.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace path not found: %s", rel)
		}
		return fmt.Errorf("inspect workspace path: %w", err)
	}
	if entry.IsSymlink {
		return fmt.Errorf("symlink paths are not supported: %w", ErrSymlinkPath)
	}
	if entry.IsDirectory && !recursive {
		children, listErr := root.List(ctx, "/"+rel)
		if listErr != nil {
			return fmt.Errorf("read folder: %w", listErr)
		}
		if len(children) > 0 {
			return errors.New("folder is not empty")
		}
	}

	if entry.IsDirectory {
		if removeAllErr := root.RemoveAll(ctx, "/"+rel); removeAllErr != nil {
			return fmt.Errorf("delete workspace folder: %w", removeAllErr)
		}
		return nil
	}
	if removeErr := root.Remove(ctx, "/"+rel); removeErr != nil {
		return fmt.Errorf("delete workspace file: %w", removeErr)
	}
	return nil
}

func ensureNoSymlink(ctx context.Context, root acfs.Root, rel string) error {
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

		entry, err := root.Stat(ctx, "/"+current, acfstypes.StatOptions{})
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("inspect workspace path: %w", err)
		}
		if entry.IsSymlink {
			return fmt.Errorf("symlink paths are not supported: %w", ErrSymlinkPath)
		}
	}
	return nil
}

func ensureWritableRelPath(protected map[string]bool, rel string) error {
	if rel == "." || rel == "" {
		return errors.New("workspace root cannot be modified")
	}
	rootName, _, _ := strings.Cut(rel, "/")
	if protected[rel] || protected[rootName] {
		return fmt.Errorf("%s: %w", rel, ErrProtectedPath)
	}
	return nil
}
