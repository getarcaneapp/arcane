package projects

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"emperror.dev/errors"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
)

// ProjectUpdateBackupScope describes exactly what a project update can mutate,
// so the pre-update backup copies only those paths instead of the whole
// project directory (which may contain huge container data directories).
type ProjectUpdateBackupScope struct {
	// TopLevelFiles backs up every top-level regular file in the project
	// directory. Compose/env persistence only ever writes top-level files.
	TopLevelFiles bool
	// Paths are normalized project-relative paths a file change can create,
	// overwrite or delete.
	Paths []string
	// RenamedDirs holds {src, dest} pairs for a rename/move of an existing
	// directory. These are rolled back with an inverse rename instead of a
	// copy, so a huge directory move never triggers a full copy.
	RenamedDirs [][2]string
}

type projectUpdateEnvSymlinkBackup struct {
	relativePath string
	resolvedPath string
}

func (s ProjectUpdateBackupScope) IsEmpty() bool {
	return !s.TopLevelFiles && len(s.Paths) == 0 && len(s.RenamedDirs) == 0
}

// ProjectUpdateBackup records what BackupProjectUpdateScope copied so
// RestoreProjectUpdateBackup can put the project directory back without
// touching anything outside the update's scope.
type ProjectUpdateBackup struct {
	BackupDir        string
	TopLevelFiles    bool
	FileEntries      []string    // regular file contents copied into BackupDir
	DirEntries       []string    // directories copied recursively
	AbsentEntries    []string    // did not exist at backup time -> removed on restore
	RenamedDirs      [][2]string // undone via inverse rename on restore
	Skipped          []string    // unreadable, skipped; preserved on restore
	envSymlink       *projectUpdateEnvSymlinkBackup
	absentParentDirs []string
}

// BackupProjectUpdateScope copies the parts of projectDir named by scope into
// backupDir. Unreadable files are skipped (recorded in Skipped) so an
// unrelated foreign-owned file cannot block a save, matching the tolerant
// semantics of the old whole-directory backup.
//
// External .env targets are resolved explicitly; other operations remain
// confined to their project or backup directory.
func BackupProjectUpdateScope(ctx context.Context, projectDir, backupDir string, scope ProjectUpdateBackupScope) (*ProjectUpdateBackup, error) {
	for _, directory := range []string{projectDir, backupDir} {
		if _, err := acfs.Stat(ctx, directory, "/", false); err != nil {
			return nil, errors.WrapIff(err, "open backup directory %s", directory)
		}
	}
	backup := &ProjectUpdateBackup{
		BackupDir:     backupDir,
		TopLevelFiles: scope.TopLevelFiles,
		RenamedDirs:   scope.RenamedDirs,
	}

	if scope.TopLevelFiles {
		if err := backupTopLevelFilesInternal(ctx, projectDir, backupDir, backup); err != nil {
			return nil, err
		}
	}

	for _, rel := range normalizeScopePathsInternal(scope.Paths) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if backupPathCoveredInternal(backup, rel) {
			continue
		}
		if err := backupScopePathInternal(ctx, projectDir, backupDir, rel, backup); err != nil {
			return nil, err
		}
	}

	dedupeCoveredBackupEntriesInternal(backup)
	slices.Sort(backup.absentParentDirs)
	backup.absentParentDirs = slices.Compact(backup.absentParentDirs)
	slices.Sort(backup.Skipped)
	return backup, nil
}

func normalizeScopePathsInternal(paths []string) []string {
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

func backupTopLevelFilesInternal(ctx context.Context, projectDir, backupDir string, backup *ProjectUpdateBackup) error {
	entries, err := acfs.List(ctx, projectDir, "/")
	if err != nil {
		return errors.WrapIf(err, "read project directory")
	}
	for _, entry := range entries {
		name := entry.Name
		switch {
		case os.FileMode(entry.UnixMode).IsRegular():
			if err := copyBackupFileInternal(ctx, projectDir, backupDir, name, backup); err != nil {
				return err
			}
		case name == EffectiveEnvFileName && entry.IsSymlink:
			if err := copyBackupEnvSymlinkInternal(ctx, projectDir, backupDir, name, backup); err != nil {
				return err
			}
		}
	}
	return nil
}

func backupScopePathInternal(ctx context.Context, projectDir, backupDir, rel string, backup *ProjectUpdateBackup) error {
	info, err := acfs.Stat(ctx, projectDir, "/"+rel, false)
	if errors.Is(err, os.ErrNotExist) {
		parents, absentErr := absentParentDirectoriesInternal(ctx, projectDir, rel)
		if absentErr != nil {
			return absentErr
		}
		backup.AbsentEntries = append(backup.AbsentEntries, rel)
		backup.absentParentDirs = append(backup.absentParentDirs, parents...)
		return nil
	}
	if err != nil {
		return errors.WrapIff(err, "inspect project path %s", rel)
	}

	switch {
	case info.IsDirectory:
		destDir := filepath.Join(backupDir, filepath.FromSlash(rel))
		if err := acfs.MkdirAll(ctx, backupDir, "/"+rel, 0o755); err != nil {
			return errors.WrapIf(err, "create backup directory")
		}
		copied, err := acfs.CopyDir(ctx, filepath.Join(projectDir, filepath.FromSlash(rel)), destDir, acfstypes.CopyOptions{TolerateUnreadable: true})
		if err != nil {
			return errors.WrapIff(err, "backup project directory %s", rel)
		}
		for _, sub := range copied.Skipped {
			backup.Skipped = append(backup.Skipped, path.Join(rel, filepath.ToSlash(sub)))
		}
		if err := recordBackupSymlinksInternal(ctx, projectDir, rel, backup); err != nil {
			return errors.WrapIff(err, "inspect project directory symlinks %s", rel)
		}
		backup.DirEntries = append(backup.DirEntries, rel)
	case os.FileMode(info.UnixMode).IsRegular():
		if err := copyBackupFileInternal(ctx, projectDir, backupDir, rel, backup); err != nil {
			return err
		}
	case rel == EffectiveEnvFileName && info.IsSymlink:
		return copyBackupEnvSymlinkInternal(ctx, projectDir, backupDir, rel, backup)
	default:
		return errors.Errorf("cannot back up non-regular project path %s", rel)
	}
	return nil
}

func absentParentDirectoriesInternal(ctx context.Context, projectDir, rel string) ([]string, error) {
	var parents []string
	for current := path.Dir(rel); current != "."; current = path.Dir(current) {
		if _, err := acfs.Stat(ctx, projectDir, "/"+current, false); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.WrapIff(err, "inspect project path %s", current)
		}
		parents = append(parents, current)
	}
	return parents, nil
}

func recordBackupSymlinksInternal(ctx context.Context, projectDir, rel string, backup *ProjectUpdateBackup) error {
	err := acfs.ListEach(ctx, projectDir, "/"+rel, func(entry acfstypes.Entry) error {
		current := strings.TrimPrefix(entry.Path, "/")
		if entry.IsSymlink {
			backup.Skipped = append(backup.Skipped, current)
			return nil
		}
		if entry.IsDirectory {
			return recordBackupSymlinksInternal(ctx, projectDir, current, backup)
		}
		return nil
	})
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		backup.Skipped = append(backup.Skipped, rel)
		return nil
	}
	return err
}

func backupPathCoveredInternal(backup *ProjectUpdateBackup, rel string) bool {
	for _, entries := range [][]string{backup.FileEntries, backup.DirEntries, backup.AbsentEntries, backup.Skipped} {
		for _, entry := range entries {
			if rel == entry || strings.HasPrefix(rel, entry+"/") {
				return true
			}
		}
	}
	return false
}

func copyBackupFileInternal(ctx context.Context, projectDir, backupDir, rel string, backup *ProjectUpdateBackup) error {
	info, err := acfs.Stat(ctx, projectDir, "/"+rel, false)
	if err != nil {
		return errors.WrapIff(err, "inspect project file %s", rel)
	}
	content, err := acfs.ReadFile(ctx, projectDir, "/"+rel)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return errors.WrapIff(err, "read project file %s", rel)
	}
	if dir := path.Dir(rel); dir != "." {
		if err := acfs.MkdirAll(ctx, backupDir, "/"+dir, 0o755); err != nil {
			return errors.WrapIf(err, "create backup directory")
		}
	}
	if err := acfs.Write(ctx, backupDir, "/"+rel, content, acfs.WriteOptions{Mode: os.FileMode(info.UnixMode).Perm()}); err != nil {
		return errors.WrapIff(err, "write backup file %s", rel)
	}
	backup.FileEntries = append(backup.FileEntries, rel)
	return nil
}

func copyBackupEnvSymlinkInternal(ctx context.Context, projectDir, backupDir, rel string, backup *ProjectUpdateBackup) error {
	envPath := filepath.Join(projectDir, filepath.FromSlash(rel))
	resolvedPath, perm, isSymlink, err := resolveEnvFileWriteTargetInternal(ctx, envPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return errors.WrapIff(err, "resolve project env symlink %s", rel)
	}
	if !isSymlink {
		return errors.Errorf("project env file %s is no longer a symlink", rel)
	}

	content, err := acfs.ReadFile(ctx, filepath.Dir(resolvedPath), "/"+filepath.Base(resolvedPath))
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return errors.WrapIff(err, "read project env symlink target %s", rel)
	}
	if err := acfs.Write(ctx, backupDir, "/"+rel, content, acfs.WriteOptions{Mode: perm}); err != nil {
		return errors.WrapIff(err, "write backup file %s", rel)
	}

	backup.FileEntries = append(backup.FileEntries, rel)
	backup.envSymlink = &projectUpdateEnvSymlinkBackup{
		relativePath: rel,
		resolvedPath: filepath.Clean(resolvedPath),
	}
	return nil
}

// dedupeCoveredBackupEntriesInternal drops entries that live under a directory
// already covered by DirEntries or AbsentEntries (e.g. a file change inside a
// folder that is also being deleted recursively).
func dedupeCoveredBackupEntriesInternal(backup *ProjectUpdateBackup) {
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

// RestoreProjectUpdateBackup rolls the scoped parts of projectDir back to the
// state captured in backup. Files are restored in place (preserving inodes so
// container bind mounts stay valid) and out-of-scope files are never touched.
func RestoreProjectUpdateBackup(ctx context.Context, projectDir string, backup *ProjectUpdateBackup) error {
	for _, directory := range []string{projectDir, backup.BackupDir} {
		if _, err := acfs.Stat(ctx, directory, "/", false); err != nil {
			return errors.WrapIff(err, "open restore directory %s", directory)
		}
	}
	// 1. Undo directory renames, then 2. remove debris the failed update
	// created at paths that did not exist.
	if err := undoRenamedDirsAndDebrisInternal(ctx, projectDir, backup); err != nil {
		return err
	}

	// 3. Mirror backed-up directories back in place.
	for _, rel := range backup.DirEntries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if live, err := acfs.Stat(ctx, projectDir, "/"+rel, false); err == nil && !live.IsDirectory {
			if live.IsSymlink {
				return errors.Errorf("refusing to restore project directory %s: destination is a symlink", rel)
			}
			if err := acfs.Remove(ctx, projectDir, "/"+rel); err != nil {
				return errors.WrapIff(err, "remove conflicting file %s", rel)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.WrapIff(err, "inspect project directory %s", rel)
		}
		if err := acfs.MkdirAll(ctx, projectDir, "/"+rel, 0o755); err != nil {
			return errors.WrapIff(err, "recreate project directory %s", rel)
		}
		preserve := skippedUnderInternal(backup.Skipped, rel)
		src := filepath.Join(backup.BackupDir, filepath.FromSlash(rel))
		dest := filepath.Join(projectDir, filepath.FromSlash(rel))
		if err := acfs.MirrorDir(ctx, src, dest, acfstypes.MirrorOptions{Preserve: preserve}); err != nil {
			return errors.WrapIff(err, "restore project directory %s", rel)
		}
	}

	// 4. Restore individual files in place.
	for _, rel := range backup.FileEntries {
		if err := restoreBackupFileInternal(ctx, projectDir, backup, rel); err != nil {
			return err
		}
	}

	// 5. Depth-1 mini-mirror of top-level regular files: prune live top-level
	// regular files absent from the backup (and not skipped as unreadable),
	// then copy every top-level backup file back. Top-level directories and
	// symlinks are never touched.
	if backup.TopLevelFiles {
		if err := restoreTopLevelFilesInternal(ctx, projectDir, backup); err != nil {
			return err
		}
	}

	return nil
}

// undoRenamedDirsAndDebrisInternal reverses directory renames with an inverse
// rename (no copy involved), then removes debris the failed update created at
// paths that were absent at backup time.
func undoRenamedDirsAndDebrisInternal(ctx context.Context, projectDir string, backup *ProjectUpdateBackup) error {
	handledAbsent := make(map[string]bool)
	for _, pair := range backup.RenamedDirs {
		src, dest := pair[0], pair[1]
		if _, err := acfs.Stat(ctx, projectDir, "/"+dest, false); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return errors.WrapIff(err, "inspect renamed path %s", dest)
			}
			continue
		}
		if _, err := acfs.Stat(ctx, projectDir, "/"+src, false); err == nil {
			// The failed batch recreated something at src (e.g. rename a -> b
			// then create_folder a). Clear it only when the backup covers src —
			// debris at an absent path or a directory we hold a full copy of —
			// so the renamed directory always moves back; an out-of-scope
			// occupant is left alone.
			if !slices.Contains(backup.DirEntries, src) && !slices.Contains(backup.AbsentEntries, src) {
				continue
			}
			if err := acfs.RemoveAll(ctx, projectDir, "/"+src); err != nil {
				return errors.WrapIff(err, "remove recreated path %s", src)
			}
			handledAbsent[src] = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.WrapIff(err, "inspect rename source %s", src)
		}
		if err := acfs.Rename(ctx, projectDir, "/"+dest, "/"+src); err != nil {
			return errors.WrapIff(err, "undo rename of %s", src)
		}
	}

	for _, rel := range backup.AbsentEntries {
		if handledAbsent[rel] {
			// Already cleared above, and the inverse rename has since restored
			// the original directory at this path — do not delete it again.
			continue
		}
		if err := acfs.RemoveAll(ctx, projectDir, "/"+rel); err != nil {
			return errors.WrapIff(err, "remove created path %s", rel)
		}
	}
	return cleanupCreatedParentsInternal(ctx, projectDir, backup.absentParentDirs)
}

func cleanupCreatedParentsInternal(ctx context.Context, projectDir string, parents []string) error {
	// Only remove empty ancestors; applications may have created siblings.
	for _, rel := range slices.Backward(parents) {
		info, err := acfs.Stat(ctx, projectDir, "/"+rel, false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return errors.WrapIff(err, "inspect created parent directory %s", rel)
		}
		if !info.IsDirectory {
			continue
		}
		if err := acfs.Remove(ctx, projectDir, "/"+rel); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, acfs.ErrNotEmpty) {
			return errors.WrapIff(err, "remove empty created parent directory %s", rel)
		}
	}
	return nil
}

func skippedUnderInternal(skipped []string, rel string) []string {
	prefix := rel + "/"
	var under []string
	for _, s := range skipped {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			under = append(under, filepath.FromSlash(rest))
		}
	}
	return under
}

func restoreBackupFileInternal(ctx context.Context, projectDir string, backup *ProjectUpdateBackup, rel string) error {
	info, err := acfs.Stat(ctx, backup.BackupDir, "/"+rel, false)
	if err != nil {
		return errors.WrapIff(err, "inspect backup file %s", rel)
	}
	content, err := acfs.ReadFile(ctx, backup.BackupDir, "/"+rel)
	if err != nil {
		return errors.WrapIff(err, "read backup file %s", rel)
	}
	if backup.envSymlink != nil && backup.envSymlink.relativePath == rel {
		return restoreBackupEnvSymlinkInternal(ctx, projectDir, backup.envSymlink, content, os.FileMode(info.UnixMode).Perm())
	}
	if live, err := acfs.Stat(ctx, projectDir, "/"+rel, false); err == nil && live.IsDirectory {
		if err := acfs.RemoveAll(ctx, projectDir, "/"+rel); err != nil {
			return errors.WrapIff(err, "remove conflicting directory %s", rel)
		}
	}
	if dir := path.Dir(rel); dir != "." {
		if err := acfs.MkdirAll(ctx, projectDir, "/"+dir, 0o755); err != nil {
			return errors.WrapIff(err, "recreate parent directory of %s", rel)
		}
	}
	if err := acfs.Write(ctx, projectDir, "/"+rel, content, acfs.WriteOptions{Mode: os.FileMode(info.UnixMode).Perm(), InPlace: true}); err != nil {
		return errors.WrapIff(err, "restore project file %s", rel)
	}
	return nil
}

func restoreBackupEnvSymlinkInternal(ctx context.Context, projectDir string, envBackup *projectUpdateEnvSymlinkBackup, content []byte, perm os.FileMode) error {
	info, err := acfs.Stat(ctx, projectDir, "/"+envBackup.relativePath, false)
	if err != nil {
		return errors.WrapIf(err, "inspect project env symlink during restore")
	}
	if !info.IsSymlink {
		return errors.Errorf("refusing to restore project env file %s: destination is no longer a symlink", envBackup.relativePath)
	}

	envPath := filepath.Join(projectDir, filepath.FromSlash(envBackup.relativePath))
	resolvedPath, _, isSymlink, err := resolveEnvFileWriteTargetInternal(ctx, envPath)
	if err != nil {
		return errors.WrapIf(err, "resolve project env symlink during restore")
	}
	if !isSymlink || filepath.Clean(resolvedPath) != envBackup.resolvedPath {
		return errors.Errorf("refusing to restore project env file %s: resolved symlink target changed", envBackup.relativePath)
	}

	if err := acfs.Write(ctx, filepath.Dir(resolvedPath), "/"+filepath.Base(resolvedPath), content, acfs.WriteOptions{Mode: perm, InPlace: true}); err != nil {
		return errors.WrapIff(err, "restore project env symlink target %s", envBackup.relativePath)
	}
	return nil
}

func restoreTopLevelFilesInternal(ctx context.Context, projectDir string, backup *ProjectUpdateBackup) error {
	backupEntries, err := acfs.List(ctx, backup.BackupDir, "/")
	if err != nil {
		return errors.WrapIf(err, "read backup directory")
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

	liveEntries, err := acfs.List(ctx, projectDir, "/")
	if err != nil {
		return errors.WrapIf(err, "read project directory")
	}
	for _, entry := range liveEntries {
		name := entry.Name
		if !os.FileMode(entry.UnixMode).IsRegular() || inBackup[name] || skippedTopLevel[name] {
			continue
		}
		if err := acfs.Remove(ctx, projectDir, "/"+name); err != nil {
			return errors.WrapIff(err, "prune created file %s", name)
		}
	}

	// ACFS returns entries in name order, so restoration order is
	// deterministic and mirrors the backup order.
	for _, entry := range backupEntries {
		if !os.FileMode(entry.UnixMode).IsRegular() {
			continue
		}
		if err := restoreBackupFileInternal(ctx, projectDir, backup, entry.Name); err != nil {
			return err
		}
	}
	return nil
}
