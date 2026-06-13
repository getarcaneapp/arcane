package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/moby/moby/client"
	"gorm.io/gorm"
)

const (
	projectRenameJournalKeyPrefixInternal = "project_rename_journal:"

	projectRenameJournalPhaseStartedInternal                = "started"
	projectRenameJournalPhaseTargetsCopiedInternal          = "targets_copied"
	projectRenameJournalPhaseOldVolumesRemovedInternal      = "old_volumes_removed"
	projectRenameJournalPhaseProjectStateCommittedInternal  = "project_state_committed"
	projectRenameJournalPhaseProjectStateRolledBackInternal = "project_state_rolled_back"
)

type projectRenameJournalInternal struct {
	ProjectID  string                               `json:"projectId"`
	OldName    string                               `json:"oldName"`
	NewName    string                               `json:"newName"`
	OldPath    string                               `json:"oldPath"`
	NewPath    string                               `json:"newPath"`
	OldDirName *string                              `json:"oldDirName,omitempty"`
	NewDirName string                               `json:"newDirName"`
	Phase      string                               `json:"phase"`
	Volumes    []projectRenameJournalVolumeInternal `json:"volumes,omitempty"`
	UpdatedAt  time.Time                            `json:"updatedAt"`
}

type projectRenameJournalVolumeInternal struct {
	Key     string            `json:"key"`
	OldName string            `json:"oldName"`
	NewName string            `json:"newName"`
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func projectRenameJournalKeyInternal(projectID string) string {
	return projectRenameJournalKeyPrefixInternal + strings.TrimSpace(projectID)
}

func (s *ProjectService) prepareProjectRenameJournalInternal(proj *models.Project, name *string, projectsDirectory string, migration projectVolumeRenameMigrationInternal) *projectRenameJournalInternal {
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

	if source, ok := migration.(projectVolumeRenameJournalSourceInternal); ok {
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

func projectRenameJournalTargetsCopiedInternal(phase string) bool {
	switch phase {
	case projectRenameJournalPhaseTargetsCopiedInternal,
		projectRenameJournalPhaseOldVolumesRemovedInternal,
		projectRenameJournalPhaseProjectStateCommittedInternal:
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
	return recoverErr
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
		if err := s.completeProjectRenameJournalInternal(ctx, journal); err != nil {
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

func (s *ProjectService) completeProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	if err := ensureProjectRenameTargetsReadyForCleanupInternal(ctx, dockerClient, journal.Volumes); err != nil {
		var missingWithSource *projectRenameTargetMissingWithSourceInternalError
		if errors.As(err, &missingWithSource) {
			slog.WarnContext(ctx, "rolling back committed project rename because target volume is missing and source volume remains", "projectID", journal.ProjectID, "sourceVolume", missingWithSource.SourceVolume, "targetVolume", missingWithSource.TargetVolume)
			return s.rollbackProjectRenameJournalInternal(ctx, journal)
		}
		var externallyRemoved *projectRenameVolumesExternallyRemovedInternalError
		if errors.As(err, &externallyRemoved) {
			slog.WarnContext(ctx, "project rename recovery found source and target volumes externally removed", "projectID", journal.ProjectID, "volumeCount", len(externallyRemoved.Volumes), "error", externallyRemoved)
		} else {
			return err
		}
	}

	for _, vol := range journal.Volumes {
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, vol.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return fmt.Errorf("remove committed source volume %s: %w", vol.OldName, err)
		}
	}
	dockerutil.InvalidateVolumeUsageCache()
	return nil
}

func (s *ProjectService) rollbackProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	directoryRollback, err := rollbackProjectRenameDirectoryInternal(journal)
	if err != nil {
		return err
	}

	volumeErr := s.rollbackProjectRenameJournalVolumesInternal(ctx, journal)
	if volumeErr != nil {
		if projectRenameOnlyPreservedTargetErrorsInternal(volumeErr) {
			slog.WarnContext(ctx, "clearing project rename journal after preserving target volume data", "projectID", journal.ProjectID, "pathsMissing", directoryRollback.PathsMissing, "error", volumeErr)
		} else {
			slog.WarnContext(ctx, "project rename volume rollback failed; restoring project database state and clearing journal", "projectID", journal.ProjectID, "pathsMissing", directoryRollback.PathsMissing, "error", volumeErr)
		}
	}

	if err := s.db.WithContext(ctx).Model(&models.Project{}).
		Where("id = ?", journal.ProjectID).
		Updates(map[string]any{
			"name":     journal.OldName,
			"path":     journal.OldPath,
			"dir_name": journal.OldDirName,
		}).Error; err != nil {
		return errors.Join(volumeErr, fmt.Errorf("restore project database state: %w", err))
	}

	dockerutil.InvalidateVolumeUsageCache()
	return nil
}

func (s *ProjectService) rollbackProjectRenameJournalVolumesInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	if !projectRenameJournalTargetsCopiedInternal(journal.Phase) {
		return nil
	}

	dockerClient, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	var rollbackErr error
	for _, vol := range slices.Backward(journal.Volumes) {
		if err := rollbackProjectRenameJournalVolumeInternal(ctx, dockerClient, vol); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func rollbackProjectRenameJournalVolumeInternal(ctx context.Context, dockerClient *client.Client, vol projectRenameJournalVolumeInternal) error {
	oldExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.OldName)
	if err != nil {
		return err
	}
	newExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.NewName)
	if err != nil {
		return err
	}

	switch {
	case oldExists && newExists:
		return removeProjectRenameJournalTargetVolumeInternal(ctx, dockerClient, vol.NewName, oldExists, newExists)
	case !oldExists && newExists:
		return newProjectRenameTargetPreservedDuringRollbackErrorInternal(vol, errors.New("source volume is missing"))
	case !oldExists && !newExists:
		slog.WarnContext(ctx, "project rename source and target volumes are missing during rollback", "sourceVolume", vol.OldName, "targetVolume", vol.NewName)
	}
	return nil
}

func removeProjectRenameJournalTargetVolumeInternal(ctx context.Context, dockerClient *client.Client, newName string, oldExists bool, newExists bool) error {
	if !oldExists || !newExists {
		return nil
	}

	if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, newName); err != nil {
		return err
	}
	if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, newName, client.VolumeRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove rollback target volume %s: %w", newName, err)
	}
	return nil
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
		slog.Warn("project rename directory rollback found both paths; keeping old path and clearing journal", "oldPath", oldPath, "newPath", newPath)
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

func projectRenameVolumeExistsInternal(ctx context.Context, dockerClient *client.Client, name string) (bool, error) {
	_, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err == nil {
		return true, nil
	}
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect volume %s: %w", name, err)
}

type projectRenameTargetMissingWithSourceInternalError struct {
	SourceVolume string
	TargetVolume string
}

func (e *projectRenameTargetMissingWithSourceInternalError) Error() string {
	return fmt.Sprintf("committed project rename target volume %s is missing while source volume %s still exists", e.TargetVolume, e.SourceVolume)
}

type projectRenameTargetPreservedDuringRollbackInternalError struct {
	SourceVolume string
	TargetVolume string
	Err          error
}

func newProjectRenameTargetPreservedDuringRollbackErrorInternal(vol projectRenameJournalVolumeInternal, err error) error {
	return &projectRenameTargetPreservedDuringRollbackInternalError{
		SourceVolume: vol.OldName,
		TargetVolume: vol.NewName,
		Err:          err,
	}
}

func (e *projectRenameTargetPreservedDuringRollbackInternalError) Error() string {
	return fmt.Sprintf("preserved project rename target volume %s because source volume %s is unavailable during rollback: %v", e.TargetVolume, e.SourceVolume, e.Err)
}

func (e *projectRenameTargetPreservedDuringRollbackInternalError) Unwrap() error {
	return e.Err
}

func projectRenameOnlyPreservedTargetErrorsInternal(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !projectRenameOnlyPreservedTargetErrorsInternal(child) {
				return false
			}
		}
		return true
	}

	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
	return errors.As(err, &preserved)
}

type projectRenameVolumesExternallyRemovedInternalError struct {
	Volumes []projectRenameJournalVolumeInternal
}

func (e *projectRenameVolumesExternallyRemovedInternalError) Error() string {
	if e == nil || len(e.Volumes) == 0 {
		return "committed project rename source and target volumes are both missing"
	}
	if len(e.Volumes) == 1 {
		vol := e.Volumes[0]
		return fmt.Sprintf("committed project rename target volume %s is missing and source volume %s is also missing", vol.NewName, vol.OldName)
	}
	return fmt.Sprintf("committed project rename source and target volumes are both missing for %d volume pairs", len(e.Volumes))
}

func ensureProjectRenameTargetsReadyForCleanupInternal(ctx context.Context, dockerClient *client.Client, volumes []projectRenameJournalVolumeInternal) error {
	if len(volumes) == 0 {
		return nil
	}
	if dockerClient == nil {
		return errors.New("docker service unavailable")
	}

	var missingWithSource *projectRenameTargetMissingWithSourceInternalError
	var externallyRemoved []projectRenameJournalVolumeInternal
	for _, vol := range volumes {
		targetExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.NewName)
		if err != nil {
			return err
		}
		if targetExists {
			continue
		}

		sourceExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.OldName)
		if err != nil {
			return err
		}
		if sourceExists {
			if missingWithSource == nil {
				missingWithSource = &projectRenameTargetMissingWithSourceInternalError{
					SourceVolume: vol.OldName,
					TargetVolume: vol.NewName,
				}
			}
			continue
		}
		externallyRemoved = append(externallyRemoved, vol)
	}
	if missingWithSource != nil {
		return missingWithSource
	}
	if len(externallyRemoved) > 0 {
		return &projectRenameVolumesExternallyRemovedInternalError{Volumes: externallyRemoved}
	}
	return nil
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
