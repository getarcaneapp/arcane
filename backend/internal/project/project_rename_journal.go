package project

import (
	"context"
	"encoding/json/v2"
	stderrors "errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

type activeProjectRenameSyncStateInternal struct {
	skipDiscoveredPaths map[string]struct{}
	protectSeenPaths    map[string]struct{}
}

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
	ProjectID  string                  `json:"projectId"`
	OldName    string                  `json:"oldName"`
	NewName    string                  `json:"newName"`
	OldPath    string                  `json:"oldPath"`
	NewPath    string                  `json:"newPath"`
	OldDirName *string                 `json:"oldDirName,omitempty"`
	NewDirName string                  `json:"newDirName"`
	Phase      string                  `json:"phase"`
	Volumes    []volumes.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt  time.Time               `json:"updatedAt"`
}

type projectRenameRollbackCleanupInternal struct {
	ProjectID string                  `json:"projectId"`
	OldName   string                  `json:"oldName"`
	OldPath   string                  `json:"oldPath"`
	NewName   string                  `json:"newName"`
	NewPath   string                  `json:"newPath"`
	Volumes   []volumes.JournalVolume `json:"volumes,omitempty"`
	UpdatedAt time.Time               `json:"updatedAt"`
}

func (s *ProjectService) activeProjectRenameSyncStateInternal(ctx context.Context) activeProjectRenameSyncStateInternal {
	state := activeProjectRenameSyncStateInternal{
		skipDiscoveredPaths: make(map[string]struct{}),
		protectSeenPaths:    make(map[string]struct{}),
	}
	if s == nil || s.kvService == nil {
		return state
	}

	entries, err := s.kvService.ListByPrefix(ctx, projectRenameJournalKeyPrefixInternal)
	if err != nil {
		slog.WarnContext(ctx, "failed to list project rename journals during filesystem sync", "error", err)
		return state
	}

	for _, entry := range entries {
		var journal projectRenameJournalInternal
		if err := json.Unmarshal([]byte(entry.Value), &journal); err != nil {
			slog.WarnContext(ctx, "failed to decode project rename journal during filesystem sync", "key", entry.Key, "error", err)
			continue
		}
		if !projectRenameJournalFilesystemSyncPendingInternal(journal.Phase) {
			continue
		}
		if oldPath := strings.TrimSpace(journal.OldPath); oldPath != "" {
			state.protectSeenPaths[filepath.Clean(oldPath)] = struct{}{}
		}
		if newPath := strings.TrimSpace(journal.NewPath); newPath != "" {
			state.skipDiscoveredPaths[filepath.Clean(newPath)] = struct{}{}
		}
	}

	return state
}

func (s activeProjectRenameSyncStateInternal) skipDiscoveredPathInternal(path string) bool {
	_, ok := s.skipDiscoveredPaths[filepath.Clean(path)]
	return ok
}

func (s activeProjectRenameSyncStateInternal) markProtectedPathsSeenInternal(seen map[string]struct{}) {
	for seenPath := range s.protectSeenPaths {
		seen[seenPath] = struct{}{}
	}
}

func (s *ProjectService) startProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) (bool, error) {
	if journal == nil {
		return false, nil
	}
	if err := s.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseStartedInternal); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ProjectService) applyProjectVolumeMigrationForUpdateInternal(ctx context.Context, volumeMigration volumes.Migration, renameJournal *projectRenameJournalInternal, applied *bool) error {
	if volumeMigration == nil {
		return nil
	}
	if err := volumeMigration.Apply(ctx); err != nil {
		return errors.WrapIf(err, "failed to rename project volumes")
	}
	*applied = true
	return s.writeProjectRenameJournalInternal(ctx, renameJournal, projectRenameJournalPhaseTargetsCopiedInternal)
}

func (s *ProjectService) finalizeProjectRenameAfterCommitInternal(ctx context.Context, projectID string, volumeMigration volumes.Migration, renameJournal *projectRenameJournalInternal, journalActive *bool) {
	if renameJournal != nil {
		if err := s.writeProjectRenameJournalInternal(ctx, renameJournal, projectRenameJournalPhaseProjectStateCommittedInternal); err != nil {
			slog.WarnContext(ctx, "failed to mark project rename journal committed", "projectID", projectID, "error", err)
		}
	}

	if committer, ok := volumeMigration.(volumes.Committer); ok {
		if err := committer.Commit(ctx); err != nil {
			slog.WarnContext(ctx, "failed to clean up project source volumes after committed rename", "projectID", projectID, "error", err)
			var cleanupErr *volumes.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				if writeErr := s.writeProjectRenameJournalInternal(ctx, renameJournal, projectRenameJournalPhaseSourceCleanupPendingInternal); writeErr != nil {
					slog.WarnContext(ctx, "failed to mark project rename source cleanup pending", "projectID", projectID, "error", writeErr)
				}
			}
			return
		} else if err := s.writeProjectRenameJournalInternal(ctx, renameJournal, projectRenameJournalPhaseOldVolumesRemovedInternal); err != nil {
			slog.WarnContext(ctx, "failed to mark old project rename volumes removed", "projectID", projectID, "error", err)
		}
	}

	s.completeProjectRenameJournalForUpdateInternal(ctx, renameJournal, projectID, journalActive)
}

func (s *ProjectService) completeProjectRenameJournalForUpdateInternal(ctx context.Context, renameJournal *projectRenameJournalInternal, projectID string, journalActive *bool) {
	if renameJournal == nil {
		return
	}
	if clearErr := s.clearProjectRenameJournalInternal(ctx, projectID); clearErr != nil {
		slog.WarnContext(ctx, "failed to clear project rename journal", "projectID", projectID, "error", clearErr)
		return
	}
	*journalActive = false
}

func withProjectRenameRollback(ctx context.Context, proj *models.Project, projectStateCommitted *bool, run func() error) error {
	originalPath := proj.Path
	originalDirName := proj.DirName

	if err := run(); err != nil {
		if projectStateCommitted != nil && *projectStateCommitted {
			return err
		}
		if proj.Path != originalPath {
			if renameErr := os.Rename(proj.Path, originalPath); renameErr != nil {
				slog.WarnContext(ctx, "failed to rollback project directory rename", "from", proj.Path, "to", originalPath, "error", renameErr)
				return err
			}
			proj.Path = originalPath
			proj.DirName = originalDirName
		}
		return err
	}

	return nil
}

func projectRenameJournalKeyInternal(projectID string) string {
	return projectRenameJournalKeyPrefixInternal + strings.TrimSpace(projectID)
}

func projectRenameRollbackCleanupKeyInternal(projectID string) string {
	return projectRenameRollbackCleanupKeyPrefixInternal + strings.TrimSpace(projectID)
}

func (s *ProjectService) prepareProjectRenameJournalInternal(proj *models.Project, name *string, projectsDirectory string, migration volumes.Migration) *projectRenameJournalInternal {
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

	if source, ok := migration.(volumes.JournalSource); ok {
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
		return errors.WrapIf(err, "marshal project rename journal")
	}

	if err := s.kvService.Set(ctx, projectRenameJournalKeyInternal(journal.ProjectID), string(payload)); err != nil {
		return errors.WrapIf(err, "write project rename journal")
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
		return errors.WrapIf(err, "marshal project rename rollback cleanup")
	}
	if err := s.kvService.Set(ctx, projectRenameRollbackCleanupKeyInternal(journal.ProjectID), string(payload)); err != nil {
		return errors.WrapIf(err, "write project rename rollback cleanup")
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

func projectRenameJournalFilesystemSyncPendingInternal(phase string) bool {
	switch phase {
	case projectRenameJournalPhaseStartedInternal,
		projectRenameJournalPhaseTargetsCopiedInternal:
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
			recoverErr = stderrors.Join(recoverErr, errors.WrapIff(err, "decode project rename journal %s", entry.Key))
			continue
		}
		if err := s.recoverProjectRenameJournalInternal(ctx, &journal); err != nil {
			recoverErr = stderrors.Join(recoverErr, errors.WrapIff(err, "recover project rename journal %s", entry.Key))
			continue
		}
	}
	return stderrors.Join(recoverErr, s.recoverProjectRenameRollbackCleanupsInternal(ctx))
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
		return errors.WrapIf(err, "decode project rename journal")
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
		return errors.WrapIf(dbErr, "load project for rename recovery")
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
			var cleanupErr *volumes.SourceCleanupError
			if errors.As(err, &cleanupErr) {
				if writeErr := s.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseSourceCleanupPendingInternal); writeErr != nil {
					return stderrors.Join(err, writeErr)
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

	if err := volumes.EnsureTargetsReadyForCleanup(ctx, dockerClient, journal.Volumes); err != nil {
		var missingWithSource *volumes.TargetMissingWithSourceError
		if errors.As(err, &missingWithSource) {
			slog.WarnContext(ctx, "rolling back project rename because target volume is missing and source volume remains", "projectID", journal.ProjectID, "sourceVolume", missingWithSource.SourceVolume, "targetVolume", missingWithSource.TargetVolume)
			return s.rollbackProjectRenameJournalInternal(ctx, journal)
		}
		var externallyRemoved *volumes.VolumesExternallyRemovedError
		if errors.As(err, &externallyRemoved) {
			slog.WarnContext(ctx, "project rename cleanup found source and target volumes externally removed", "projectID", journal.ProjectID, "volumeCount", len(externallyRemoved.Volumes), "error", externallyRemoved)
		} else {
			return err
		}
	}

	return volumes.RemoveSourceVolumes(ctx, dockerClient, journal.Volumes)
}

func (s *ProjectService) rollbackProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	pathsMissing, directoryErr := projects.RollbackRenamedProjectDirectory(journal.OldPath, journal.NewPath)

	volumeErr := s.rollbackProjectRenameJournalVolumesInternal(ctx, journal)

	if err := s.db.WithContext(ctx).Model(&models.Project{}).
		Where("id = ?", journal.ProjectID).
		Updates(map[string]any{
			"name":     journal.OldName,
			"path":     journal.OldPath,
			"dir_name": journal.OldDirName,
		}).Error; err != nil {
		return stderrors.Join(directoryErr, volumeErr, errors.WrapIf(err, "restore project database state"))
	}

	if directoryErr != nil {
		slog.WarnContext(ctx, "keeping project rename journal after restoring database state because directory rollback failed", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", directoryErr)
	}

	if volumeErr != nil {
		if volumes.OnlyPreservedTargetErrors(volumeErr) {
			slog.WarnContext(ctx, "clearing project rename journal after preserving target volume data", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", volumeErr)
		} else {
			if cleanupErr := s.writeProjectRenameRollbackCleanupInternal(ctx, journal); cleanupErr != nil {
				return stderrors.Join(directoryErr, volumeErr, cleanupErr)
			}
			slog.WarnContext(ctx, "queued project rename target volume cleanup after restoring database state despite volume rollback failure", "projectID", journal.ProjectID, "pathsMissing", pathsMissing, "error", volumeErr)
		}
	}

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
			recoverErr = stderrors.Join(recoverErr, errors.WrapIff(err, "decode project rename rollback cleanup %s", entry.Key))
			continue
		}
		if err := s.recoverProjectRenameRollbackCleanupInternal(ctx, &cleanup); err != nil {
			recoverErr = stderrors.Join(recoverErr, errors.WrapIff(err, "recover project rename rollback cleanup %s", entry.Key))
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
		return errors.WrapIf(dbErr, "load project for rename rollback cleanup")
	}

	if proj.Name != cleanup.OldName || filepath.Clean(proj.Path) != filepath.Clean(cleanup.OldPath) {
		slog.WarnContext(ctx, "clearing project rename rollback cleanup because project state changed", "projectID", cleanup.ProjectID, "projectName", proj.Name, "projectPath", proj.Path)
		return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
	}

	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(cleanup.Volumes) > 0)
	if err != nil {
		return err
	}

	if err := volumes.CleanupRollbackTargetVolumes(ctx, dockerClient, cleanup.Volumes); err != nil {
		if volumes.OnlyPreservedTargetErrors(err) {
			slog.WarnContext(ctx, "clearing project rename rollback cleanup after preserving target volume data", "projectID", cleanup.ProjectID, "error", err)
			return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
		}
		return err
	}

	dockerutil.InvalidateVolumeUsageCache(dockerClient)
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

	return volumes.RollbackVolumes(ctx, dockerClient, journal.Volumes)
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
		return nil, errors.WrapIf(err, "failed to connect to Docker")
	}

	return dockerClient, nil
}

func cloneStringPtrInternal(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
