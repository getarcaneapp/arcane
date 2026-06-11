package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	projectRenameJournalPhaseStartedInternal        = "started"
	projectRenameJournalPhaseTargetsCopiedInternal  = "targets_copied"
	projectRenameJournalPhaseOldVolumesRemoved      = "old_volumes_removed"
	projectRenameJournalPhaseProjectStateCommitted  = "project_state_committed"
	projectRenameJournalPhaseProjectStateRolledBack = "project_state_rolled_back"
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

func (s *ProjectService) prepareProjectRenameJournalInternal(proj *models.Project, name *string, projectsDirectory string, migration projectVolumeRenameMigrationInternal) (*projectRenameJournalInternal, error) {
	if s == nil || s.kvService == nil || proj == nil || name == nil {
		return nil, nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || proj.Name == newName {
		return nil, nil
	}

	newDirName := strings.TrimSpace(projects.SanitizeProjectName(newName))
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return nil, nil
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

	return journal, nil
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
	if err := s.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseProjectStateRolledBack); err != nil {
		return err
	}
	return s.clearProjectRenameJournalInternal(ctx, journal.ProjectID)
}

func (s *ProjectService) completeProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	dockerClient, _, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	for _, vol := range journal.Volumes {
		if _, err := dockerClient.VolumeInspect(ctx, vol.NewName, client.VolumeInspectOptions{}); err != nil {
			return fmt.Errorf("inspect committed target volume %s: %w", vol.NewName, err)
		}
		if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, vol.OldName, client.VolumeRemoveOptions{Force: false}); err != nil {
			return fmt.Errorf("remove committed source volume %s: %w", vol.OldName, err)
		}
	}
	dockerutil.InvalidateVolumeUsageCache()
	return nil
}

func (s *ProjectService) rollbackProjectRenameJournalInternal(ctx context.Context, journal *projectRenameJournalInternal) error {
	if err := rollbackProjectRenameDirectoryInternal(journal); err != nil {
		return err
	}

	dockerClient, helperImage, err := s.projectRenameRecoveryDockerInternal(ctx, len(journal.Volumes) > 0)
	if err != nil {
		return err
	}

	for i := len(journal.Volumes) - 1; i >= 0; i-- {
		vol := journal.Volumes[i]
		oldExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.OldName)
		if err != nil {
			return err
		}
		newExists, err := projectRenameVolumeExistsInternal(ctx, dockerClient, vol.NewName)
		if err != nil {
			return err
		}

		if !oldExists && newExists {
			if err := createProjectRenameRecoverySourceVolumeInternal(ctx, dockerClient, vol); err != nil {
				return err
			}
			if err := copyProjectVolumeDataInternal(ctx, dockerClient, helperImage, vol.NewName, vol.OldName); err != nil {
				return fmt.Errorf("restore volume data from %s to %s: %w", vol.NewName, vol.OldName, err)
			}
			oldExists = true
		}

		if oldExists && newExists {
			if err := removeProjectVolumeHelperContainersInternal(ctx, dockerClient, vol.NewName); err != nil {
				return err
			}
			if err := removeProjectVolumeWithRetryInternal(ctx, dockerClient, vol.NewName, client.VolumeRemoveOptions{Force: true}); err != nil {
				return fmt.Errorf("remove rollback target volume %s: %w", vol.NewName, err)
			}
		}
	}

	if err := s.db.WithContext(ctx).Model(&models.Project{}).
		Where("id = ?", journal.ProjectID).
		Updates(map[string]any{
			"name":     journal.OldName,
			"path":     journal.OldPath,
			"dir_name": journal.OldDirName,
		}).Error; err != nil {
		return fmt.Errorf("restore project database state: %w", err)
	}

	dockerutil.InvalidateVolumeUsageCache()
	return nil
}

func (s *ProjectService) projectRenameRecoveryDockerInternal(ctx context.Context, required bool) (*client.Client, string, error) {
	if !required {
		return nil, "", nil
	}
	if s.dockerService == nil {
		return nil, "", errors.New("docker service unavailable")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to Docker: %w", err)
	}

	helperImage, err := getVolumeHelperImageInternal(ctx, s.dockerService, s.imageService, dockerClient)
	if err != nil {
		return nil, "", err
	}

	return dockerClient, helperImage, nil
}

func rollbackProjectRenameDirectoryInternal(journal *projectRenameJournalInternal) error {
	oldPath := filepath.Clean(journal.OldPath)
	newPath := filepath.Clean(journal.NewPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}

	oldExists := pathExistsInternal(oldPath)
	newExists := pathExistsInternal(newPath)
	switch {
	case oldExists && newExists:
		return fmt.Errorf("cannot rollback project directory rename because both paths exist: %s and %s", oldPath, newPath)
	case !oldExists && newExists:
		if err := os.Rename(newPath, oldPath); err != nil {
			return fmt.Errorf("rollback project directory rename: %w", err)
		}
	case !oldExists && !newExists:
		return fmt.Errorf("cannot rollback project directory rename because both paths are missing: %s and %s", oldPath, newPath)
	}
	return nil
}

func projectRenameVolumeExistsInternal(ctx context.Context, dockerClient *client.Client, name string) (bool, error) {
	if _, err := dockerClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); err == nil {
		return true, nil
	} else if cerrdefs.IsNotFound(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("inspect volume %s: %w", name, err)
	}
}

func createProjectRenameRecoverySourceVolumeInternal(ctx context.Context, dockerClient *client.Client, vol projectRenameJournalVolumeInternal) error {
	if _, err := dockerClient.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:       vol.OldName,
		Driver:     vol.Driver,
		DriverOpts: vol.Options,
		Labels:     vol.Labels,
	}); err != nil {
		return fmt.Errorf("recreate source volume %s: %w", vol.OldName, err)
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
