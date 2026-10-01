package systembackup

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/moby/moby/client"
)

// snapshotWithStagedDatabaseInternal archives a consistent database without blocking actor leases during the upload.
func (s *SystemBackupService) snapshotWithStagedDatabaseInternal(ctx context.Context, dockerClient *client.Client, repository backup.Repository, recoveryKey, backupID string) (backup.Snapshot, error) {
	layout, err := s.backupSourceLayoutInternal(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	stage, err := os.MkdirTemp(layout.dataDirectory, ".arcane-snapshot-")
	if err != nil {
		return backup.Snapshot{}, fmt.Errorf("create system backup staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	sqlDB, err := s.db.SQLDB()
	if err != nil {
		return backup.Snapshot{}, err
	}
	databasePath := filepath.Join(layout.dataDirectory, layout.databaseName)
	stagedDatabase := filepath.Join(stage, layout.databaseName)
	snapshotDatabase := filepath.Join(snapshotDataPath, layout.databaseName)
	if err := s.writeManifestInternal(ctx, backupID, layout); err != nil {
		return backup.Snapshot{}, err
	}
	defer func() { _ = os.Remove(layout.manifestPathInternal()) }()
	layout.excludes = []string{
		".arcane-snapshot-*", systemRecoveryRequestName,
		layout.databaseName + "-wal", layout.databaseName + "-shm", layout.databaseName + "-journal",
	}
	files, err := snapshotSourceFilesInternal(ctx, layout)
	if err != nil {
		return backup.Snapshot{}, err
	}
	if err := stageSystemDatabaseInternal(ctx, sqlDB, databasePath, stagedDatabase); err != nil {
		return backup.Snapshot{}, err
	}
	mounts, inContainer, err := currentMountsInternal(ctx, dockerClient)
	if err != nil {
		return backup.Snapshot{}, err
	}
	stagedMounts, err := sourceMountsInternal(mounts, inContainer, stagedDatabase, snapshotDatabase)
	if err != nil {
		return backup.Snapshot{}, err
	}
	layout.mounts = append(layout.mounts, stagedMounts...)
	input := backup.CreateSnapshotInput{Mounts: layout.mounts, Sources: layout.sources}
	for _, excluded := range layout.excludes {
		input.Globs = append(input.Globs, "!"+snapshotDataPath+"/"+excluded)
	}
	snapshot, err := s.engine.CreateSnapshot(ctx, dockerClient, repository, recoveryKey, "arcane-system-recovery", input)
	if err != nil {
		return backup.Snapshot{}, err
	}
	err = validateSnapshotSourcesInternal(ctx, layout, files)
	if err == nil {
		var confirmed backup.Snapshot
		confirmed, err = s.engine.ConfirmRunSnapshot(ctx, dockerClient, repository, recoveryKey, backupID, snapshot.ID)
		if err == nil {
			return confirmed, nil
		}
	}
	// Tagging changes the ID; only remove the original, unconfirmed snapshot.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	cleanupErr := s.engine.ForgetSnapshots(cleanupCtx, dockerClient, repository, recoveryKey, []string{snapshot.ID})
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("remove unconfirmed system snapshot %s: %w", snapshot.ID, cleanupErr)
	}
	return backup.Snapshot{}, errors.Combine(err, cleanupErr)
}

// snapshotSourceFilesInternal records inputs before taking the database snapshot.
func snapshotSourceFilesInternal(ctx context.Context, layout backupSourceLayoutInternal) (map[string]os.FileInfo, error) {
	roots := []string{layout.dataDirectory}
	if layout.projectsPath == snapshotProjectsPath {
		roots = append(roots, layout.projectsDirectory)
	}
	files := make(map[string]os.FileInfo)
	visit := func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(layout.dataDirectory, filePath)
		if err != nil {
			return err
		}
		if relative == layout.databaseName {
			return nil
		}
		for _, excluded := range layout.excludes {
			matched, err := filepath.Match(excluded, relative)
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[filePath] = info
		return nil
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, visit)
		if err != nil {
			return nil, fmt.Errorf("inspect system backup sources: %w", err)
		}
	}
	return files, nil
}

func validateSnapshotSourcesInternal(ctx context.Context, layout backupSourceLayoutInternal, before map[string]os.FileInfo) error {
	after, err := snapshotSourceFilesInternal(ctx, layout)
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return errors.New("system backup source files changed during capture; retry when file updates finish")
	}
	for filePath, original := range before {
		current, exists := after[filePath]
		if !exists || !os.SameFile(original, current) || original.Size() != current.Size() || original.Mode() != current.Mode() || !original.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("system backup source %q changed during capture; retry when file updates finish", filePath)
		}
	}
	return nil
}

func stageSystemDatabaseInternal(ctx context.Context, db *sql.DB, databasePath, stagedDatabase string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", stagedDatabase); err != nil {
		return fmt.Errorf("stage Arcane database: %w", err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(stagedDatabase, int(stat.Uid), int(stat.Gid)); err != nil {
			return fmt.Errorf("preserve staged database ownership: %w", err)
		}
	}
	if err := os.Chmod(stagedDatabase, info.Mode()); err != nil {
		return fmt.Errorf("preserve staged database permissions: %w", err)
	}
	return nil
}
