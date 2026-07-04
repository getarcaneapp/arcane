package projects

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.getarcane.app/sys/atomic"
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

func (s ProjectUpdateBackupScope) IsEmpty() bool {
	return !s.TopLevelFiles && len(s.Paths) == 0 && len(s.RenamedDirs) == 0
}

// ProjectUpdateBackup records what BackupProjectUpdateScope copied so
// RestoreProjectUpdateBackup can put the project directory back without
// touching anything outside the update's scope.
type ProjectUpdateBackup struct {
	BackupDir     string
	TopLevelFiles bool
	FileEntries   []string    // regular files copied into BackupDir
	DirEntries    []string    // directories copied recursively
	AbsentEntries []string    // did not exist at backup time -> removed on restore
	RenamedDirs   [][2]string // undone via inverse rename on restore
	Skipped       []string    // unreadable, skipped; preserved on restore
}

// BackupProjectUpdateScope copies the parts of projectDir named by scope into
// backupDir. Unreadable files are skipped (recorded in Skipped) so an
// unrelated foreign-owned file cannot block a save, matching the tolerant
// semantics of the old whole-directory backup.
func BackupProjectUpdateScope(projectDir, backupDir string, scope ProjectUpdateBackupScope) (*ProjectUpdateBackup, error) {
	projRoot, err := os.OpenRoot(projectDir)
	if err != nil {
		return nil, fmt.Errorf("open project directory: %w", err)
	}
	defer func() { _ = projRoot.Close() }()

	backupRoot, err := os.OpenRoot(backupDir)
	if err != nil {
		return nil, fmt.Errorf("open backup directory: %w", err)
	}
	defer func() { _ = backupRoot.Close() }()

	backup := &ProjectUpdateBackup{
		BackupDir:     backupDir,
		TopLevelFiles: scope.TopLevelFiles,
		RenamedDirs:   scope.RenamedDirs,
	}

	if scope.TopLevelFiles {
		if err := backupTopLevelFilesInternal(projRoot, backupRoot, backup); err != nil {
			return nil, err
		}
	}

	for _, rel := range normalizeScopePathsInternal(scope.Paths) {
		if err := backupScopePathInternal(projRoot, backupRoot, projectDir, backupDir, rel, backup); err != nil {
			return nil, err
		}
	}

	dedupeCoveredBackupEntriesInternal(backup)
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

func backupTopLevelFilesInternal(projRoot, backupRoot *os.Root, backup *ProjectUpdateBackup) error {
	entries, err := os.ReadDir(projRoot.Name())
	if err != nil {
		return fmt.Errorf("read project directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if err := copyBackupFileInternal(projRoot, backupRoot, name, backup); err != nil {
			return err
		}
	}
	return nil
}

func backupScopePathInternal(projRoot, backupRoot *os.Root, projectDir, backupDir, rel string, backup *ProjectUpdateBackup) error {
	info, err := projRoot.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		// Record the shallowest nonexistent ancestor so MkdirAll debris from a
		// failed create is removed on restore too.
		absent, absentErr := shallowestAbsentAncestorInternal(projRoot, rel)
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
	case info.IsDir():
		destDir := filepath.Join(backupDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}
		skipped, err := CopyDirectoryContentsTolerant(filepath.Join(projectDir, filepath.FromSlash(rel)), destDir)
		if err != nil {
			return fmt.Errorf("backup project directory %s: %w", rel, err)
		}
		for _, sub := range skipped {
			backup.Skipped = append(backup.Skipped, path.Join(rel, filepath.ToSlash(sub)))
		}
		backup.DirEntries = append(backup.DirEntries, rel)
	case info.Mode().IsRegular():
		if err := copyBackupFileInternal(projRoot, backupRoot, rel, backup); err != nil {
			return err
		}
	default:
		// Symlinks and other special files: the apply operations reject them
		// before mutating anything, so there is nothing to back up.
	}
	return nil
}

func shallowestAbsentAncestorInternal(projRoot *os.Root, rel string) (string, error) {
	current := ""
	for segment := range strings.SplitSeq(rel, "/") {
		current = path.Join(current, segment)
		if _, err := projRoot.Lstat(current); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return current, nil
			}
			return "", fmt.Errorf("inspect project path %s: %w", current, err)
		}
	}
	return rel, nil
}

func copyBackupFileInternal(projRoot, backupRoot *os.Root, rel string, backup *ProjectUpdateBackup) error {
	info, err := projRoot.Lstat(rel)
	if err != nil {
		return fmt.Errorf("inspect project file %s: %w", rel, err)
	}
	content, err := projRoot.ReadFile(rel)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			backup.Skipped = append(backup.Skipped, rel)
			return nil
		}
		return fmt.Errorf("read project file %s: %w", rel, err)
	}
	if dir := path.Dir(rel); dir != "." {
		if err := backupRoot.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}
	}
	if err := atomic.WriteFile(filepath.Join(backupRoot.Name(), filepath.FromSlash(rel)), content, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write backup file %s: %w", rel, err)
	}
	backup.FileEntries = append(backup.FileEntries, rel)
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
func RestoreProjectUpdateBackup(projectDir string, backup *ProjectUpdateBackup) error {
	projRoot, err := os.OpenRoot(projectDir)
	if err != nil {
		return fmt.Errorf("open project directory: %w", err)
	}
	defer func() { _ = projRoot.Close() }()

	// 1. Undo directory renames, then 2. remove debris the failed update
	// created at paths that did not exist.
	if err := undoRenamedDirsAndDebrisInternal(projRoot, backup); err != nil {
		return err
	}

	// 3. Mirror backed-up directories back in place.
	for _, rel := range backup.DirEntries {
		if err := projRoot.MkdirAll(rel, 0o755); err != nil {
			return fmt.Errorf("recreate project directory %s: %w", rel, err)
		}
		preserve := skippedUnderInternal(backup.Skipped, rel)
		src := filepath.Join(backup.BackupDir, filepath.FromSlash(rel))
		dest := filepath.Join(projectDir, filepath.FromSlash(rel))
		if err := MirrorDirectoryContentsPreserving(src, dest, preserve); err != nil {
			return fmt.Errorf("restore project directory %s: %w", rel, err)
		}
	}

	backupRoot, err := os.OpenRoot(backup.BackupDir)
	if err != nil {
		return fmt.Errorf("open backup directory: %w", err)
	}
	defer func() { _ = backupRoot.Close() }()

	// 4. Restore individual files in place.
	for _, rel := range backup.FileEntries {
		if err := restoreBackupFileInternal(backupRoot, projRoot, rel); err != nil {
			return err
		}
	}

	// 5. Depth-1 mini-mirror of top-level regular files: prune live top-level
	// regular files absent from the backup (and not skipped as unreadable),
	// then copy every top-level backup file back. Top-level directories and
	// symlinks are never touched.
	if backup.TopLevelFiles {
		if err := restoreTopLevelFilesInternal(backupRoot, projRoot, backup.Skipped); err != nil {
			return err
		}
	}

	return nil
}

// undoRenamedDirsAndDebrisInternal reverses directory renames with an inverse
// rename (no copy involved), then removes debris the failed update created at
// paths that were absent at backup time.
func undoRenamedDirsAndDebrisInternal(projRoot *os.Root, backup *ProjectUpdateBackup) error {
	handledAbsent := make(map[string]bool)
	for _, pair := range backup.RenamedDirs {
		src, dest := pair[0], pair[1]
		if _, err := projRoot.Lstat(dest); err != nil {
			continue
		}
		if _, err := projRoot.Lstat(src); err == nil {
			// The failed batch recreated something at src (e.g. rename a -> b
			// then create_folder a). Clear it only when the backup covers src —
			// debris at an absent path or a directory we hold a full copy of —
			// so the renamed directory always moves back; an out-of-scope
			// occupant is left alone.
			if !slices.Contains(backup.DirEntries, src) && !slices.Contains(backup.AbsentEntries, src) {
				continue
			}
			if err := projRoot.RemoveAll(src); err != nil {
				return fmt.Errorf("remove recreated path %s: %w", src, err)
			}
			handledAbsent[src] = true
		}
		if err := projRoot.Rename(dest, src); err != nil {
			return fmt.Errorf("undo rename of %s: %w", src, err)
		}
	}

	for _, rel := range backup.AbsentEntries {
		if handledAbsent[rel] {
			// Already cleared above, and the inverse rename has since restored
			// the original directory at this path — do not delete it again.
			continue
		}
		if err := projRoot.RemoveAll(rel); err != nil {
			return fmt.Errorf("remove created path %s: %w", rel, err)
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

func restoreBackupFileInternal(backupRoot, projRoot *os.Root, rel string) error {
	info, err := backupRoot.Lstat(rel)
	if err != nil {
		return fmt.Errorf("inspect backup file %s: %w", rel, err)
	}
	content, err := backupRoot.ReadFile(rel)
	if err != nil {
		return fmt.Errorf("read backup file %s: %w", rel, err)
	}
	if live, err := projRoot.Lstat(rel); err == nil && live.IsDir() {
		if err := projRoot.RemoveAll(rel); err != nil {
			return fmt.Errorf("remove conflicting directory %s: %w", rel, err)
		}
	}
	if dir := path.Dir(rel); dir != "." {
		if err := projRoot.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("recreate parent directory of %s: %w", rel, err)
		}
	}
	// Atomic write: a crash mid-restore leaves either the failed-update
	// content or the fully restored file, never a torn write.
	if err := atomic.WriteFile(filepath.Join(projRoot.Name(), filepath.FromSlash(rel)), content, info.Mode().Perm()); err != nil {
		return fmt.Errorf("restore project file %s: %w", rel, err)
	}
	return nil
}

func restoreTopLevelFilesInternal(backupRoot, projRoot *os.Root, skipped []string) error {
	backupEntries, err := os.ReadDir(backupRoot.Name())
	if err != nil {
		return fmt.Errorf("read backup directory: %w", err)
	}
	inBackup := make(map[string]bool, len(backupEntries))
	for _, entry := range backupEntries {
		if entry.Type().IsRegular() {
			inBackup[entry.Name()] = true
		}
	}
	skippedTopLevel := make(map[string]bool)
	for _, s := range skipped {
		if !strings.Contains(s, "/") {
			skippedTopLevel[s] = true
		}
	}

	liveEntries, err := os.ReadDir(projRoot.Name())
	if err != nil {
		return fmt.Errorf("read project directory: %w", err)
	}
	for _, entry := range liveEntries {
		name := entry.Name()
		if !entry.Type().IsRegular() || inBackup[name] || skippedTopLevel[name] {
			continue
		}
		if err := projRoot.Remove(name); err != nil {
			return fmt.Errorf("prune created file %s: %w", name, err)
		}
	}

	// backupEntries is sorted by os.ReadDir, so restoration order is
	// deterministic and mirrors the backup order.
	for _, entry := range backupEntries {
		if !entry.Type().IsRegular() {
			continue
		}
		if err := restoreBackupFileInternal(backupRoot, projRoot, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}
