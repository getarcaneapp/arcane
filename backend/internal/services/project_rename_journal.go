package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

const (
	projectRenameJournalKeyPrefixInternal         = "project_rename_journal:"
	projectRenameRollbackCleanupKeyPrefixInternal = "project_rename_rollback_cleanup:"

	projectRenameJournalPhaseStartedInternal                = "started"
	projectRenameJournalPhaseTargetsCopiedInternal          = "targets_copied"
	projectRenameJournalPhaseOldVolumesRemovedInternal      = "old_volumes_removed"
	projectRenameJournalPhaseProjectStateCommittedInternal  = "project_state_committed"
	projectRenameJournalPhaseSourceCleanupPendingInternal   = "source_cleanup_pending"
	projectRenameJournalPhaseProjectStateRolledBackInternal = "project_state_rolled_back"
)

type projectRenameJournalInternal struct {
	ProjectID  string                   `json:"projectId"`
	OldName    string                   `json:"oldName"`
	NewName    string                   `json:"newName"`
	OldPath    string                   `json:"oldPath"`
	NewPath    string                   `json:"newPath"`
	OldDirName *string                  `json:"oldDirName,omitempty"`
	NewDirName string                   `json:"newDirName"`
	Phase      string                   `json:"phase"`
	Volumes    []projects.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt  time.Time                `json:"updatedAt"`
}

type projectRenameRollbackCleanupInternal struct {
	ProjectID string                   `json:"projectId"`
	OldName   string                   `json:"oldName"`
	OldPath   string                   `json:"oldPath"`
	NewName   string                   `json:"newName"`
	NewPath   string                   `json:"newPath"`
	Volumes   []projects.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt time.Time                `json:"updatedAt"`
}

func projectRenameJournalKeyInternal(projectID string) string {
	return projectRenameJournalKeyPrefixInternal + strings.TrimSpace(projectID)
}

func projectRenameRollbackCleanupKeyInternal(projectID string) string {
	return projectRenameRollbackCleanupKeyPrefixInternal + strings.TrimSpace(projectID)
}

func (s *ProjectService) prepareProjectRenameJournalInternal(proj *models.Project, name *string, projectsDirectory string, migration projects.Migration) *projectRenameJournalInternal {
	if s == nil || s.kvService == nil || proj == nil || name == nil {
		return nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || proj.Name == newName {
		return nil
	}

	newDirName := strings.TrimSpace(projects.SanitizeProjectName(newName))
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return nil
	}

	journal := &projectRenameJournalInternal{
		ProjectID:  proj.ID,
		OldName:    proj.Name,
		NewName:    newName,
		OldPath:    filepath.Clean(proj.Path),
		NewPath:    filepath.Clean(filepath.Join(projectsDirectory, newDirName)),
		OldDirName: cloneStringPtrInternal(proj.DirName),
		NewDirName: newDirName,
		Phase:      projectRenameJournalPhaseStartedInternal,
	}

	if source, ok := migration.(projects.JournalSource); ok {
		journal.Volumes = source.JournalVolumes()
	}

	return journal
}

func (s *ProjectService) writeProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal, phase string) error {
	if s == nil || s.kvService == nil || journal == nil {
		return nil
	}
	journal.Phase = phase
	journal.UpdatedAt = time.Now().UTC()

	payload, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("marshal project rename journal: %w", err)
	}

	if err := s.kvService.Set(ctx, projectRenameJournalKeyInternal(journal.ProjectID), string(payload)); err != nil {
		return fmt.Errorf("write project rename journal: %w", err)
	}
	return nil
}

func (s *ProjectService) clearProjectRenameJournalInternal(ctx context.Context, projectID string) error {
	if s == nil || s.kvService == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kvService.Delete(ctx, projectRenameJournalKeyInternal(projectID))
}

func (s *ProjectService) writeProjectRenameRollbackCleanupInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	if s == nil || s.kvService == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" || len(journal.Volumes) == 0 {
		return nil
	}

	cleanup := projectRenameRollbackCleanupInternal{
		ProjectID: journal.ProjectID,
		OldName:   journal.OldName,
		OldPath:   filepath.Clean(journal.OldPath),
		NewName:   journal.NewName,
		NewPath:   filepath.Clean(journal.NewPath),
		Volumes:   journal.Volumes,
		UpdatedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(cleanup)
	if err != nil {
		return fmt.Errorf("marshal project rename rollback cleanup: %w", err)
	}
	if err := s.kvService.Set(ctx, projectRenameRollbackCleanupKeyInternal(journal.ProjectID), string(payload)); err != nil {
		return fmt.Errorf("write project rename rollback cleanup: %w", err)
	}
	return nil
}

func (s *ProjectService) clearProjectRenameRollbackCleanupInternal(ctx context.Context, projectID string) error {
	if s == nil || s.kvService == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kvService.Delete(ctx, projectRenameRollbackCleanupKeyInternal(projectID))
}

func projectRenameJournalTargetsCopiedInternal(phase string) bool {
	switch phase {
	case projectRenameJournalPhaseTargetsCopiedInternal,
		projectRenameJournalPhaseOldVolumesRemovedInternal,
		projectRenameJournalPhaseProjectStateCommittedInternal,
		projectRenameJournalPhaseSourceCleanupPendingInternal:
		return true
	default:
		return false
	}
}

func (s *ProjectService) RecoverProjectRenameJournals(ctx context.Context) error {
	if s == nil || s.kvService == nil {
		return nil
	}

	entries, err := s.kvService.ListByPrefix(ctx, projectRenameJournalKeyPrefixInternal)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var journal projectRenameJournalInternal
		if err := json.Unmarshal([]byte(entry.Value), &journal); err != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename journal %s: %w", entry.Key, err))
			continue
		}
		if err := s.recoverProjectRenameJournalInternal(ctx, &journal); err != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename journal %s: %w", entry.Key, err))
			continue
		}
	}
	return errors.Join(recoverErr, s.recoverProjectRenameRollbackCleanupsInternal(ctx))
}

func (s *ProjectService) recoverProjectRenameJournalForProjectInternal(ctx context.Context, projectID string) error {
	if s == nil || s.kvService == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}

	raw, ok, err := s.kvService.Get(ctx, projectRenameJournalKeyInternal(projectID))
	if err != nil || !ok {
		return err
	}

	var journal projectRenameJournalInternal
	if err := json.Unmarshal([]byte(raw), &journal); err != nil {
		return fmt.Errorf("decode project rename journal: %w", err)
	}
	return s.recoverProjectRenameJournalInternal(ctx, &journal)
}

func (s *ProjectService) recoverProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	if s == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" {
		return nil
	}

	var proj models.Project
	dbErr := s.db.WithContext(ctx).First(&proj, "id = ?", journal.ProjectID).Error
	if dbErr != nil && !errors.Is(dbErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load project for rename recovery: %w", dbErr)
	}

	projectCommitted := dbErr == nil && (proj.Name == journal.NewName || filepath.Clean(proj.Path) == filepath.Clean(journal.NewPath))
	if projectCommitted {
		if journal.Phase == projectRenameJournalPhaseSourceCleanupPendingInternal {
			if err := s.cleanupProjectRenameJournalSourcesInternal(ctx, journal); err != nil {
				return err
			}
			return s.clearProjectRenameJournalInternal(ctx, journal.ProjectID)
		}
		if err := s.cleanupProjectRenameJournalSourcesInternal(ctx, journal); err != nil {
			var cleanupErr *projects.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				if writeErr := s.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseSourceCleanupPendingInternal); writeErr != nil {
					return errors.Join(err, writeErr)
				}
			}
			return err
		}
		return s.clearProjectRenameJournalInternal(ctx, journal.ProjectID)
	}

	if err := s.rollbackProjectRenameJournalInternal(ctx, journal); err != nil {
		return err
	}
	if err := s.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseProjectStateRolledBackInternal); err != nil {
		return err
	}
	return s.clearProjectRenameJournalInternal(ctx, journal.ProjectID)
}

func (s *ProjectService) cleanupProjectRenameJournalSourcesInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	if err := projects.EnsureTargetsReadyForCleanup(ctx, dockerClient, journal.Volumes); err != nil {
		var missingWithSource *projects.TargetMissingWithSourceError
		if errors.As(err, &missingWithSource) {
			slog.WarnContext(ctx, "rolling back project rename because target volume is missing and source volume remains", "projectID", journal.ProjectID, "sourceVolume", missingWithSource.SourceVolume, "targetVolume", missingWithSource.TargetVolume)
			return s.rollbackProjectRenameJournalInternal(ctx, journal)
		}
		var externallyRemoved *projects.VolumesExternallyRemovedError
		if errors.As(err, &externallyRemoved) {
			slog.WarnContext(ctx, "project rename cleanup found source and target volumes externally removed", "projectID", journal.ProjectID, "volumeCount", len(externallyRemoved.Volumes), "error", externallyRemoved)
		} else {
			return err
		}
	}

	return projects.RemoveSourceVolumes(ctx, dockerClient, journal.Volumes)
}

func (s *ProjectService) rollbackProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	directoryRollback, directoryErr := rollbackProjectRenameDirectoryInternal(journal)

	volumeErr := s.rollbackProjectRenameJournalVolumesInternal(ctx, journal)

	if err := s.db.WithContext(ctx).Model(&models.Project{}).
		Where("id = ?", journal.ProjectID).
		Updates(map[string]any{
			"name":     journal.OldName,
			"path":     journal.OldPath,
			"dir_name": journal.OldDirName,
		}).Error; err != nil {
		return errors.Join(directoryErr, volumeErr, fmt.Errorf("restore project database state: %w", err))
	}

	if directoryErr != nil {
		slog.WarnContext(ctx, "keeping project rename journal after restoring database state because directory rollback failed", "projectID", journal.ProjectID, "pathsMissing", directoryRollback.PathsMissing, "error", directoryErr)
	}

	if volumeErr != nil {
		if projects.OnlyPreservedTargetErrors(volumeErr) {
			slog.WarnContext(ctx, "clearing project rename journal after preserving target volume data", "projectID", journal.ProjectID, "pathsMissing", directoryRollback.PathsMissing, "error", volumeErr)
		} else {
			if cleanupErr := s.writeProjectRenameRollbackCleanupInternal(ctx, journal); cleanupErr != nil {
				return errors.Join(directoryErr, volumeErr, cleanupErr)
			}
			slog.WarnContext(ctx, "queued project rename target volume cleanup after restoring database state despite volume rollback failure", "projectID", journal.ProjectID, "pathsMissing", directoryRollback.PathsMissing, "error", volumeErr)
		}
	}

	dockerutil.InvalidateVolumeUsageCache()
	return directoryErr
}

func (s *ProjectService) recoverProjectRenameRollbackCleanupsInternal(ctx context.Context) error {
	if s == nil || s.kvService == nil {
		return nil
	}

	entries, err := s.kvService.ListByPrefix(ctx, projectRenameRollbackCleanupKeyPrefixInternal)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var cleanup projectRenameRollbackCleanupInternal
		if err := json.Unmarshal([]byte(entry.Value), &cleanup); err != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("decode project rename rollback cleanup %s: %w", entry.Key, err))
			continue
		}
		if err := s.recoverProjectRenameRollbackCleanupInternal(ctx, &cleanup); err != nil {
			recoverErr = errors.Join(recoverErr, fmt.Errorf("recover project rename rollback cleanup %s: %w", entry.Key, err))
			continue
		}
	}
	return recoverErr
}

func (s *ProjectService) recoverProjectRenameRollbackCleanupInternal(ctx context.Context, cleanup *projectRenameRollbackCleanupInternal) error {
	if s == nil || cleanup == nil || strings.TrimSpace(cleanup.ProjectID) == "" {
		return nil
	}
	if len(cleanup.Volumes) == 0 {
		return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
	}

	var proj models.Project
	dbErr := s.db.WithContext(ctx).First(&proj, "id = ?", cleanup.ProjectID).Error
	if dbErr != nil {
		if errors.Is(dbErr, gorm.ErrRecordNotFound) {
			slog.WarnContext(ctx, "clearing project rename rollback cleanup because project no longer exists", "projectID", cleanup.ProjectID)
			return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
		}
		return fmt.Errorf("load project for rename rollback cleanup: %w", dbErr)
	}

	if proj.Name != cleanup.OldName || filepath.Clean(proj.Path) != filepath.Clean(cleanup.OldPath) {
		slog.WarnContext(ctx, "clearing project rename rollback cleanup because project state changed", "projectID", cleanup.ProjectID, "projectName", proj.Name, "projectPath", proj.Path)
		return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
	}

	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(cleanup.Volumes) > 0)
	if err != nil {
		return err
	}

	if err := projects.CleanupRollbackTargetVolumes(ctx, dockerClient, cleanup.Volumes); err != nil {
		if projects.OnlyPreservedTargetErrors(err) {
			slog.WarnContext(ctx, "clearing project rename rollback cleanup after preserving target volume data", "projectID", cleanup.ProjectID, "error", err)
			return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
		}
		return err
	}

	dockerutil.InvalidateVolumeUsageCache()
	return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
}

func (s *ProjectService) rollbackProjectRenameJournalVolumesInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	if !projectRenameJournalTargetsCopiedInternal(journal.Phase) {
		return nil
	}

	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	return projects.RollbackVolumes(ctx, dockerClient, journal.Volumes)
}

func (s *ProjectService) projectRenameRecoveryDockerInternal(ctx context.Context, dockerRequired bool) (*client.Client, error) {
	if !dockerRequired {
		return nil, nil
	}
	if s.dockerService == nil {
		return nil, errors.New("docker service unavailable")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	return dockerClient, nil
}

type projectRenameDirectoryRollbackInternal struct {
	PathsMissing bool
}

func rollbackProjectRenameDirectoryInternal(journal *projectRenameJournalInternal) (projectRenameDirectoryRollbackInternal, error) {
	var result projectRenameDirectoryRollbackInternal
	oldPath := filepath.Clean(journal.OldPath)
	newPath := filepath.Clean(journal.NewPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return result, nil
	}

	oldExists := pathExistsInternal(oldPath)
	newExists := pathExistsInternal(newPath)
	switch {
	case oldExists && newExists:
		conflictPath, err := relocateProjectRenameConflictDirectoryInternal(newPath)
		if err != nil {
			slog.Warn("project rename directory rollback found both paths and failed to relocate target path; keeping old path and clearing journal", "oldPath", oldPath, "newPath", newPath, "error", err)
		} else {
			slog.Warn("project rename directory rollback found both paths; moved target path aside and kept old path", "oldPath", oldPath, "newPath", newPath, "conflictPath", conflictPath)
		}
	case !oldExists && newExists:
		if err := os.Rename(newPath, oldPath); err != nil {
			return result, fmt.Errorf("rollback project directory rename: %w", err)
		}
	case !oldExists && !newExists:
		result.PathsMissing = true
		slog.Warn("project rename directory paths are missing during rollback", "oldPath", oldPath, "newPath", newPath)
	}
	return result, nil
}

func relocateProjectRenameConflictDirectoryInternal(path string) (string, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	now := time.Now().UTC().UnixNano()
	for attempt := range 10 {
		conflictPath := filepath.Join(parent, fmt.Sprintf(".%s.rename-conflict-%d-%d", base, now, attempt))
		if _, err := os.Stat(conflictPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check conflict path: %w", err)
		}
		if err := os.Rename(path, conflictPath); err != nil {
			return "", fmt.Errorf("relocate project rename target path: %w", err)
		}
		return conflictPath, nil
	}
	return "", fmt.Errorf("relocate project rename target path: no available conflict path for %s", path)
}

func pathExistsInternal(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cloneStringPtrInternal(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
