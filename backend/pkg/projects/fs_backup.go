package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/fs"
	"kit.getarcane.app/fs/atomic"
)

type (
	ProjectUpdateBackupScope struct {
		Paths         []string
		RenamedDirs   [][2]string
		TopLevelFiles bool
	}

	projectUpdateEnvSymlinkBackup struct {
		relativePath string
		linkTarget   string
		resolvedPath string
	}

	ProjectUpdateBackup struct {
		envSymlink    *projectUpdateEnvSymlinkBackup
		BackupDir     string
		FileEntries   []string
		DirEntries    []string
		AbsentEntries []string    // did not exist at backup time -> removed on restore
		RenamedDirs   [][2]string // undone via inverse rename on restore
		Skipped       []string    // unreadable, skipped; preserved on restore
		TopLevelFiles bool
	}
)

func (s ProjectUpdateBackupScope) IsEmpty() bool {
	return !s.TopLevelFiles && len(s.Paths) == 0 && len(s.RenamedDirs) == 0
}

func BackupProjectUpdateScope(ctx context.Context, projectDir, backupDir string, scope ProjectUpdateBackupScope) (*ProjectUpdateBackup, error) {
	projRoot := acfs.Root{Path: projectDir}
	if _, err := projRoot.Stat(ctx, "/", acfstypes.StatOptions{}); err != nil {
		return nil, fmt.Errorf("open project directory: %w", err)
	}

	backupRoot := acfs.Root{Path: backupDir}
	if _, err := backupRoot.Stat(ctx, "/", acfstypes.StatOptions{}); err != nil {
		return nil, fmt.Errorf("open backup directory: %w", err)
	}

	backup := &ProjectUpdateBackup{
		BackupDir:     backupDir,
		TopLevelFiles: scope.TopLevelFiles,
		RenamedDirs:   scope.RenamedDirs,
	}

	if scope.TopLevelFiles {
		if backupTopLevelFilesErr := backupTopLevelFiles(ctx, projRoot, backupRoot, backup); backupTopLevelFilesErr != nil {
			return nil, backupTopLevelFilesErr
		}
	}

	for _, rel := range normalizeScopePaths(scope.Paths) {
		if backupScopePathErr := backupScopePath(ctx, projRoot, backupRoot, rel, backup); backupScopePathErr != nil {
			return nil, backupScopePathErr
		}
	}

	dedupeCoveredBackupEntries(backup)
	slices.Sort(backup.Skipped)
	return backup, nil
}

func normalizeScopePaths(paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := path.Clean(filepath.ToSlash(p))
		if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			continue
		}
		normalized = append(normalized, cleaned)
	}
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func backupTopLevelFiles(ctx context.Context, projRoot, backupRoot acfs.Root, backup *ProjectUpdateBackup) error {
	entries, err := projRoot.List(ctx, "/")
	if err != nil {
		return fmt.Errorf("read project directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name
		switch {
		case os.FileMode(entry.UnixMode).IsRegular():
			if copyBackupFileErr := copyBackupFile(ctx, projRoot, backupRoot, name, backup); copyBackupFileErr != nil {
				return copyBackupFileErr
			}
		case name == EffectiveEnvFileName && entry.IsSymlink:
			if copyBackupEnvSymlinkErr := copyBackupEnvSymlink(ctx, projRoot, backupRoot, name, backup); copyBackupEnvSymlinkErr != nil {
				return copyBackupEnvSymlinkErr
			}
		}
	}
	return nil
}

func backupScopePath(ctx context.Context, projRoot, backupRoot acfs.Root, rel string, backup *ProjectUpdateBackup) error {
	info, err := projRoot.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if errors.Is(err, os.ErrNotExist) {
		// Record the shallowest nonexistent ancestor so MkdirAll debris from a
		// failed create is removed on restore too.
		absent, absentErr := shallowestAbsentAncestor(ctx, projRoot, rel)
		if absentErr != nil {
			return absentErr
		}
		backup.AbsentEntries = append(backup.AbsentEntries, absent)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect project path %s: %w", rel, err)
	}

	switch {
	case info.IsDirectory:
		destDir := filepath.Join(backupRoot.Path, filepath.FromSlash(rel))
		if mkdirAllErr := backupRoot.MkdirAll(ctx, "/"+rel, 0o755); mkdirAllErr != nil {
			return fmt.Errorf("create backup directory: %w", mkdirAllErr)
		}
		copied, copyDirErr := acfs.Root{Path: filepath.Join(projRoot.Path, filepath.FromSlash(rel))}.CopyTo(ctx, acfs.Root{Path: destDir}, acfstypes.CopyOptions{TolerateUnreadable: true})
		if copyDirErr != nil {
			return fmt.Errorf("backup project directory %s: %w", rel, copyDirErr)
		}
		for _, sub := range copied.Skipped {
			backup.Skipped = append(backup.Skipped, path.Join(rel, filepath.ToSlash(sub)))
		}
		backup.DirEntries = append(backup.DirEntries, rel)
	case os.FileMode(info.UnixMode).IsRegular():
		if copyBackupFileErr := copyBackupFile(ctx, projRoot, backupRoot, rel, backup); copyBackupFileErr != nil {
			return copyBackupFileErr
		}
	case rel == EffectiveEnvFileName && info.IsSymlink:
		if copyBackupEnvSymlinkErr := copyBackupEnvSymlink(ctx, projRoot, backupRoot, rel, backup); copyBackupEnvSymlinkErr != nil {
			return copyBackupEnvSymlinkErr
		}
	default:
		// Other symlinks and special files: the apply operations reject them
		// before mutating anything, so there is nothing to back up.
	}
	return nil
}

func shallowestAbsentAncestor(ctx context.Context, projRoot acfs.Root, rel string) (string, error) {
	current := ""
	for segment := range strings.SplitSeq(rel, "/") {
		current = path.Join(current, segment)
		if _, err := projRoot.Stat(ctx, "/"+current, acfstypes.StatOptions{}); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return current, nil
			}
			return "", fmt.Errorf("inspect project path %s: %w", current, err)
		}
	}
	return rel, nil
}

func copyBackupFile(ctx context.Context, projRoot, backupRoot acfs.Root, rel string, backup *ProjectUpdateBackup) error {
	info, err := projRoot.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		return fmt.Errorf("inspect project file %s: %w", rel, err)
	}
	content, err := projRoot.ReadFile(ctx, "/"+rel)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return fmt.Errorf("read project file %s: %w", rel, err)
	}
	if dir := path.Dir(rel); dir != "." {
		if mkdirAllErr := backupRoot.MkdirAll(ctx, "/"+dir, 0o755); mkdirAllErr != nil {
			return fmt.Errorf("create backup directory: %w", mkdirAllErr)
		}
	}
	if writeFileErr := atomic.WriteFile(filepath.Join(backupRoot.Path, filepath.FromSlash(rel)), content, os.FileMode(info.UnixMode).Perm()); writeFileErr != nil {
		return fmt.Errorf("write backup file %s: %w", rel, writeFileErr)
	}
	backup.FileEntries = append(backup.FileEntries, rel)
	return nil
}

func copyBackupEnvSymlink(ctx context.Context, projRoot, backupRoot acfs.Root, rel string, backup *ProjectUpdateBackup) error {
	linkTarget, err := projRoot.Readlink(ctx, "/"+rel)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return fmt.Errorf("read project env symlink %s: %w", rel, err)
	}

	resolvedPath, perm, isSymlink, err := resolveEnvFileWriteTarget(ctx, filepath.Join(projRoot.Path, filepath.FromSlash(rel)))
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			// Dangling link: env writes leave it untouched, so there is nothing to restore.
			return nil
		}
		return fmt.Errorf("resolve project env symlink %s: %w", rel, err)
	}
	if !isSymlink {
		return fmt.Errorf("project env file %s is no longer a symlink", rel)
	}

	content, err := acfs.Host{}.ReadFile(ctx, resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return fmt.Errorf("read project env symlink target %s: %w", rel, err)
	}
	if writeFileErr := atomic.WriteFile(filepath.Join(backupRoot.Path, filepath.FromSlash(rel)), content, perm); writeFileErr != nil {
		return fmt.Errorf("write backup file %s: %w", rel, writeFileErr)
	}

	backup.FileEntries = append(backup.FileEntries, rel)
	backup.envSymlink = &projectUpdateEnvSymlinkBackup{
		relativePath: rel,
		linkTarget:   linkTarget,
		resolvedPath: filepath.Clean(resolvedPath),
	}
	return nil
}

func dedupeCoveredBackupEntries(backup *ProjectUpdateBackup) {
	covered := append(append([]string{}, backup.DirEntries...), backup.AbsentEntries...)
	hasCoveredAncestor := func(rel string) bool {
		for _, dir := range covered {
			if rel != dir && strings.HasPrefix(rel, dir+"/") {
				return true
			}
		}
		return false
	}
	backup.FileEntries = slices.DeleteFunc(backup.FileEntries, hasCoveredAncestor)
	backup.DirEntries = slices.DeleteFunc(backup.DirEntries, hasCoveredAncestor)
	backup.AbsentEntries = slices.DeleteFunc(backup.AbsentEntries, hasCoveredAncestor)
}

func RestoreProjectUpdateBackup(ctx context.Context, projectDir string, backup *ProjectUpdateBackup) error {
	projRoot := acfs.Root{Path: projectDir}
	if _, err := projRoot.Stat(ctx, "/", acfstypes.StatOptions{}); err != nil {
		return fmt.Errorf("open project directory: %w", err)
	}

	if undoRenamedDirsAndDebrisErr := undoRenamedDirsAndDebris(ctx, projRoot, backup); undoRenamedDirsAndDebrisErr != nil {
		return undoRenamedDirsAndDebrisErr
	}

	for _, rel := range backup.DirEntries {
		if live, lstatErr := projRoot.Stat(ctx, "/"+rel, acfstypes.StatOptions{}); lstatErr == nil && !live.IsDirectory {
			if removeErr := projRoot.Remove(ctx, "/"+rel); removeErr != nil {
				return fmt.Errorf("remove conflicting file %s: %w", rel, removeErr)
			}
		}
		if mkdirAllErr := projRoot.MkdirAll(ctx, "/"+rel, 0o755); mkdirAllErr != nil {
			return fmt.Errorf("recreate project directory %s: %w", rel, mkdirAllErr)
		}
		preserve := skippedUnder(backup.Skipped, rel)
		src := filepath.Join(backup.BackupDir, filepath.FromSlash(rel))
		dest := filepath.Join(projectDir, filepath.FromSlash(rel))
		if mirrorDirErr := (acfs.Root{Path: src}).MirrorTo(ctx, acfs.Root{Path: dest}, acfstypes.MirrorOptions{Preserve: preserve}); mirrorDirErr != nil {
			return fmt.Errorf("restore project directory %s: %w", rel, mirrorDirErr)
		}
	}

	backupRoot := acfs.Root{Path: backup.BackupDir}
	if _, err := backupRoot.Stat(ctx, "/", acfstypes.StatOptions{}); err != nil {
		return fmt.Errorf("open backup directory: %w", err)
	}

	for _, rel := range backup.FileEntries {
		if restoreBackupFileErr := restoreBackupFile(ctx, backupRoot, projRoot, backup, rel); restoreBackupFileErr != nil {
			return restoreBackupFileErr
		}
	}

	if backup.TopLevelFiles {
		if restoreTopLevelFilesErr := restoreTopLevelFiles(ctx, backupRoot, projRoot, backup); restoreTopLevelFilesErr != nil {
			return restoreTopLevelFilesErr
		}
	}

	return nil
}

func undoRenamedDirsAndDebris(ctx context.Context, projRoot acfs.Root, backup *ProjectUpdateBackup) error {
	handledAbsent := make(map[string]bool)
	for _, pair := range backup.RenamedDirs {
		src, dest := pair[0], pair[1]
		if _, err := projRoot.Stat(ctx, "/"+dest, acfstypes.StatOptions{}); err != nil {
			continue
		}
		if _, err := projRoot.Stat(ctx, "/"+src, acfstypes.StatOptions{}); err == nil {
			// The failed batch recreated something at src (e.g. rename a -> b
			// then create_folder a). Clear it only when the backup covers src —
			// debris at an absent path or a directory we hold a full copy of —
			// so the renamed directory always moves back; an out-of-scope
			// occupant is left alone.
			if !slices.Contains(backup.DirEntries, src) && !slices.Contains(backup.AbsentEntries, src) {
				continue
			}
			if removeAllErr := projRoot.RemoveAll(ctx, "/"+src); removeAllErr != nil {
				return fmt.Errorf("remove recreated path %s: %w", src, removeAllErr)
			}
			handledAbsent[src] = true
		}
		if err := projRoot.Rename(ctx, "/"+dest, "/"+src); err != nil {
			return fmt.Errorf("undo rename of %s: %w", src, err)
		}
	}

	for _, rel := range backup.AbsentEntries {
		if handledAbsent[rel] {
			// Already cleared above, and the inverse rename has since restored
			// the original directory at this path — do not delete it again.
			continue
		}
		if err := projRoot.RemoveAll(ctx, "/"+rel); err != nil {
			return fmt.Errorf("remove created path %s: %w", rel, err)
		}
	}
	return nil
}

func skippedUnder(skipped []string, rel string) []string {
	prefix := rel + "/"
	var under []string
	for _, s := range skipped {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			under = append(under, filepath.FromSlash(rest))
		}
	}
	return under
}

func restoreBackupFile(ctx context.Context, backupRoot, projRoot acfs.Root, backup *ProjectUpdateBackup, rel string) error {
	info, err := backupRoot.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if err != nil {
		return fmt.Errorf("inspect backup file %s: %w", rel, err)
	}
	content, err := backupRoot.ReadFile(ctx, "/"+rel)
	if err != nil {
		return fmt.Errorf("read backup file %s: %w", rel, err)
	}
	if backup.envSymlink != nil && backup.envSymlink.relativePath == rel {
		return restoreBackupEnvSymlink(ctx, projRoot, backup.envSymlink, content, os.FileMode(info.UnixMode).Perm())
	}
	live, liveErr := projRoot.Stat(ctx, "/"+rel, acfstypes.StatOptions{})
	if liveErr == nil && os.FileMode(live.UnixMode).IsRegular() {
		// In place: the inode survives so container bind mounts of the file stay valid.
		if writeErr := projRoot.Write(ctx, "/"+rel, content, acfs.WriteOptions{
			Mode:    os.FileMode(info.UnixMode).Perm(),
			InPlace: true,
		}); writeErr != nil {
			return fmt.Errorf("restore project file %s: %w", rel, writeErr)
		}
		return nil
	}
	if liveErr == nil && live.IsDirectory {
		if removeAllErr := projRoot.RemoveAll(ctx, "/"+rel); removeAllErr != nil {
			return fmt.Errorf("remove conflicting directory %s: %w", rel, removeAllErr)
		}
	}
	if dir := path.Dir(rel); dir != "." {
		if mkdirAllErr := projRoot.MkdirAll(ctx, "/"+dir, 0o755); mkdirAllErr != nil {
			return fmt.Errorf("recreate parent directory of %s: %w", rel, mkdirAllErr)
		}
	}
	// Atomic create: a crash mid-restore leaves either nothing or the fully
	// restored file, never a torn write.
	if writeFileErr := atomic.WriteFile(filepath.Join(projRoot.Path, filepath.FromSlash(rel)), content, os.FileMode(info.UnixMode).Perm()); writeFileErr != nil {
		return fmt.Errorf("restore project file %s: %w", rel, writeFileErr)
	}
	return nil
}

func restoreBackupEnvSymlink(ctx context.Context, projRoot acfs.Root, envBackup *projectUpdateEnvSymlinkBackup, content []byte, perm os.FileMode) error {
	info, err := projRoot.Stat(ctx, "/"+envBackup.relativePath, acfstypes.StatOptions{})
	if err != nil {
		return fmt.Errorf("inspect project env symlink during restore: %w", err)
	}
	if !info.IsSymlink {
		return fmt.Errorf("refusing to restore project env file %s: destination is no longer a symlink", envBackup.relativePath)
	}

	linkTarget, err := projRoot.Readlink(ctx, "/"+envBackup.relativePath)
	if err != nil {
		return fmt.Errorf("read project env symlink during restore: %w", err)
	}
	if linkTarget != envBackup.linkTarget {
		return fmt.Errorf("refusing to restore project env file %s: symlink target changed", envBackup.relativePath)
	}

	resolvedPath, _, isSymlink, err := resolveEnvFileWriteTarget(ctx, filepath.Join(projRoot.Path, filepath.FromSlash(envBackup.relativePath)))
	if err != nil {
		return fmt.Errorf("resolve project env symlink during restore: %w", err)
	}
	if !isSymlink || filepath.Clean(resolvedPath) != envBackup.resolvedPath {
		return fmt.Errorf("refusing to restore project env file %s: resolved symlink target changed", envBackup.relativePath)
	}

	if writeFileErr := atomic.WriteFile(resolvedPath, content, perm); writeFileErr != nil {
		return fmt.Errorf("restore project env symlink target %s: %w", envBackup.relativePath, writeFileErr)
	}
	return nil
}

func restoreTopLevelFiles(ctx context.Context, backupRoot, projRoot acfs.Root, backup *ProjectUpdateBackup) error {
	backupEntries, err := backupRoot.List(ctx, "/")
	if err != nil {
		return fmt.Errorf("read backup directory: %w", err)
	}
	inBackup := make(map[string]bool, len(backupEntries))
	for _, entry := range backupEntries {
		if os.FileMode(entry.UnixMode).IsRegular() {
			inBackup[entry.Name] = true
		}
	}
	skippedTopLevel := make(map[string]bool)
	for _, s := range backup.Skipped {
		if !strings.Contains(s, "/") {
			skippedTopLevel[s] = true
		}
	}

	liveEntries, err := projRoot.List(ctx, "/")
	if err != nil {
		return fmt.Errorf("read project directory: %w", err)
	}
	for _, entry := range liveEntries {
		name := entry.Name
		if !os.FileMode(entry.UnixMode).IsRegular() || inBackup[name] || skippedTopLevel[name] {
			continue
		}
		if removeErr := projRoot.Remove(ctx, "/"+name); removeErr != nil {
			return fmt.Errorf("prune created file %s: %w", name, removeErr)
		}
	}

	for _, entry := range backupEntries {
		if !os.FileMode(entry.UnixMode).IsRegular() {
			continue
		}
		if restoreBackupFileErr := restoreBackupFile(ctx, backupRoot, projRoot, backup, entry.Name); restoreBackupFileErr != nil {
			return restoreBackupFileErr
		}
	}
	return nil
}

func BackupProjectDirectory(ctx context.Context, projectsDirectory, projectPath, pattern string, scope ProjectUpdateBackupScope) (*ProjectUpdateBackup, func(), error) {
	projectAbs, rootAbs, err := resolveProjectUnderRoot(projectsDirectory, projectPath)
	if err != nil {
		return nil, nil, err
	}

	backupLogical, err := acfs.Root{Path: rootAbs}.MkdirTemp(ctx, "/", pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create project backup directory: %w", err)
	}
	backupPath := filepath.Join(rootAbs, filepath.FromSlash(strings.TrimPrefix(backupLogical, "/")))
	cleanupCtx := context.WithoutCancel(ctx)
	cleanup := func() { _ = acfs.Root{Path: rootAbs}.RemoveAll(cleanupCtx, backupLogical) }

	// Tolerate files Arcane cannot read (e.g. foreign-owned secrets): skip them
	// in the backup so an unrelated unreadable file can't block the whole save.
	// The skipped paths are recorded so the rollback restore can preserve them.
	backup, err := BackupProjectUpdateScope(ctx, projectAbs, backupPath, scope)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to backup project files: %w", err)
	}
	if len(backup.Skipped) > 0 {
		slog.WarnContext(ctx, "skipped unreadable files while backing up project; they will be left untouched on rollback", "projectPath", projectAbs, "skipped", backup.Skipped)
	}
	return backup, cleanup, nil
}

func RestoreProjectDirectoryBackup(ctx context.Context, projectsDirectory, projectPath string, backup *ProjectUpdateBackup) error {
	projectAbs, rootAbs, err := resolveProjectUnderRoot(projectsDirectory, projectPath)
	if err != nil {
		return err
	}

	projectLogical, err := acfs.Root{Path: rootAbs}.LogicalPath(projectAbs)
	if err != nil {
		return fmt.Errorf("failed to resolve project directory: %w", err)
	}

	slog.DebugContext(ctx, "restoring project directory backup", "path", projectAbs, "backup", backup.BackupDir)
	if mkdirAllErr := (acfs.Root{Path: rootAbs}).MkdirAll(ctx, projectLogical, fs.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("failed to recreate project directory: %w", mkdirAllErr)
	}
	if restoreProjectUpdateBackupErr := RestoreProjectUpdateBackup(ctx, projectAbs, backup); restoreProjectUpdateBackupErr != nil {
		return fmt.Errorf("failed to restore project backup: %w", restoreProjectUpdateBackupErr)
	}
	return nil
}

func resolveProjectUnderRoot(projectsDirectory, projectPath string) (projectAbs, rootAbs string, err error) {
	projectAbs, err = filepath.Abs(projectPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve project path: %w", err)
	}
	projectAbs = filepath.Clean(projectAbs)

	rootAbs, err = filepath.Abs(projectsDirectory)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve projects directory: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	if !IsSafeSubdirectory(rootAbs, projectAbs) || projectAbs == rootAbs {
		return "", "", errors.New("project path is outside projects directory")
	}
	return projectAbs, rootAbs, nil
}
