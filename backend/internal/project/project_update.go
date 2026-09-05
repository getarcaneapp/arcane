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
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"gorm.io/gorm"
)

func (s *ProjectService) UpdateProject(ctx context.Context, projectID string, name *string, composeContent, envContent, overrideContent *string, user common.User) (*Project, error) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, err
	}

	name = resolveAuthoritativeProjectNameInternal(ctx, &proj, name, composeContent)
	renameRequested := isProjectRenameRequestedInternal(&proj, name)
	if err := s.recoverProjectRenameJournalForProjectInternal(ctx, projectID); err != nil {
		if renameRequested {
			return nil, err
		}
		slog.WarnContext(ctx, "project rename journal recovery failed before non-rename update; continuing", "projectID", projectID, "error", err)
	} else {
		proj, projectsDirectory, err = s.getProjectForUpdate(ctx, projectID)
		if err != nil {
			return nil, err
		}
		name = resolveAuthoritativeProjectNameInternal(ctx, &proj, name, composeContent)
	}

	if err := ensureProjectMutableInternal(&proj); err != nil {
		return nil, err
	}
	if err := s.ensureProjectStoppedForRenameInternal(ctx, &proj, name); err != nil {
		return nil, err
	}

	volumeMigration, err := s.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, &proj, name, projectsDirectory, composeContent, envContent, overrideContent)
	if err != nil {
		return nil, err
	}

	renameJournal := s.prepareProjectRenameJournalInternal(&proj, name, projectsDirectory, volumeMigration)

	backup, cleanupBackup, err := s.prepareProjectUpdateBackupInternal(ctx, projectsDirectory, proj.Path, composeContent, envContent, overrideContent)
	if err != nil {
		return nil, err
	}
	defer cleanupBackup()

	journalActive, err := s.startProjectRenameJournalInternal(ctx, renameJournal)
	if err != nil {
		return nil, err
	}

	projectStateCommitted := false
	if err := withProjectRenameRollbackInternal(ctx, &proj, &projectStateCommitted, func() error {
		return s.applyProjectUpdateWithRenameJournalInternal(ctx, &proj, name, projectsDirectory, composeContent, envContent, overrideContent, volumeMigration, renameJournal, &journalActive, &projectStateCommitted)
	}); err != nil {
		err = s.handleProjectUpdateFailureInternal(ctx, projectID, projectsDirectory, &proj, backup, &journalActive, projectStateCommitted, err)
		return nil, err
	}

	s.refreshProjectAfterContentUpdateInternal(ctx, &proj, composeContent, overrideContent)
	s.logProjectUpdateEventInternal(ctx, &proj, composeContent, envContent, overrideContent, user)

	slog.InfoContext(ctx, "project updated", "projectID", proj.ID, "name", proj.Name)
	return &proj, nil
}

// resolveAuthoritativeProjectNameInternal enforces that a top-level `name:` in
// the compose file is authoritative over the submitted project name. For
// name-only renames, it checks the compose file on disk so the lock can't be
// bypassed via the API.
func resolveAuthoritativeProjectNameInternal(ctx context.Context, proj *Project, name *string, composeContent *string) *string {
	if composeContent != nil {
		if yamlName := projects.ComposeContentProjectName(*composeContent); yamlName != "" {
			return &yamlName
		}
		return name
	}
	if name != nil {
		if onDiskCompose, _, readErr := projects.ReadProjectFiles(ctx, proj.Path, ""); readErr == nil {
			if yamlName := projects.ComposeContentProjectName(onDiskCompose); yamlName != "" {
				return &yamlName
			}
		}
	}
	return name
}

func (s *ProjectService) prepareProjectUpdateBackupInternal(ctx context.Context, projectsDirectory, projectPath string, composeContent, envContent, overrideContent *string) (*projects.ProjectUpdateBackup, func(), error) {
	if composeContent == nil && envContent == nil && overrideContent == nil {
		return nil, func() {}, nil
	}

	scope := projects.ProjectUpdateBackupScope{TopLevelFiles: true}
	if scope.IsEmpty() {
		return nil, func() {}, nil
	}

	backup, err := backupProjectDirectoryInternal(ctx, projectsDirectory, projectPath, scope)
	if err != nil {
		return nil, nil, err
	}

	backupLogical, err := acfs.LogicalPath(projectsDirectory, backup.BackupDir)
	if err != nil {
		return nil, nil, errors.WrapIf(err, "failed to resolve project backup directory")
	}
	// The cleanup must run even when the update was cancelled, or the backup
	// directory leaks: acfs refuses operations on an already-cancelled context.
	cleanupCtx := context.WithoutCancel(ctx)
	return backup, func() { _ = acfs.RemoveAll(cleanupCtx, projectsDirectory, backupLogical) }, nil
}

func (s *ProjectService) applyProjectUpdateWithRenameJournalInternal(ctx context.Context, proj *Project, name *string, projectsDirectory string, composeContent, envContent, overrideContent *string, volumeMigration volumetypes.Migration, renameJournal *projecttypes.RenameJournal, journalActive *bool, projectStateCommitted *bool) (err error) {
	volumeMigrationApplied := false
	defer func() {
		stateCommitted := projectStateCommitted != nil && *projectStateCommitted
		if err != nil && volumeMigrationApplied && !stateCommitted {
			if rollbackErr := volumeMigration.Rollback(ctx); rollbackErr != nil {
				err = stderrors.Join(err, errors.WrapIf(rollbackErr, "failed to rollback project volume rename"))
			}
		}
	}()

	if err = s.applyProjectRenameIfNeeded(ctx, proj, name, projectsDirectory); err != nil {
		return err
	}
	if err = s.persistUpdatedProjectFiles(ctx, proj, projectsDirectory, composeContent, envContent, overrideContent); err != nil {
		return err
	}
	if err = projects.ApplyRenameVolumeMigration(ctx, s.renameRecoveryOperationsInternal(), volumeMigration, renameJournal, &volumeMigrationApplied); err != nil {
		return err
	}
	if err = s.saveProjectUpdateInternal(ctx, proj); err != nil {
		return err
	}
	if projectStateCommitted != nil {
		*projectStateCommitted = true
	}
	projects.FinalizeRenameAfterCommit(ctx, s.renameRecoveryOperationsInternal(), proj.ID, volumeMigration, renameJournal, journalActive)
	return nil
}

func (s *ProjectService) saveProjectUpdateInternal(ctx context.Context, proj *Project) error {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return errors.WrapIf(tx.Error, "failed to start project update transaction")
	}

	txCommitted := false
	defer func() {
		if !txCommitted {
			_ = tx.Rollback().Error
		}
	}()

	if err := tx.Save(proj).Error; err != nil {
		return errors.WrapIf(err, "failed to update project")
	}
	if err := tx.Commit().Error; err != nil {
		return errors.WrapIf(err, "failed to commit project update")
	}
	txCommitted = true
	return nil
}

func (s *ProjectService) handleProjectUpdateFailureInternal(ctx context.Context, projectID, projectsDirectory string, proj *Project, backup *projects.ProjectUpdateBackup, journalActive *bool, projectStateCommitted bool, err error) error {
	if projectStateCommitted {
		return err
	}

	if backup != nil {
		if restoreErr := restoreProjectDirectoryBackupInternal(ctx, projectsDirectory, proj.Path, backup); restoreErr != nil {
			err = stderrors.Join(err, errors.WrapIf(restoreErr, "failed to restore project files after update failure"))
		}
	}
	if *journalActive {
		if recoverErr := s.recoverProjectRenameJournalForProjectInternal(ctx, projectID); recoverErr != nil {
			err = stderrors.Join(err, errors.WrapIf(recoverErr, "project rename recovery failed"))
		} else {
			*journalActive = false
		}
	}
	return err
}

func (s *ProjectService) logProjectUpdateEventInternal(ctx context.Context, proj *Project, composeContent, envContent, overrideContent *string, user common.User) {
	metadata := database.JSON{
		"action":      "update",
		"projectID":   proj.ID,
		"projectName": proj.Name,
	}
	if composeContent != nil {
		metadata["composeUpdated"] = true
	}
	if envContent != nil {
		metadata["envUpdated"] = true
	}
	if overrideContent != nil {
		metadata["overrideUpdated"] = true
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log project update action")
}

func (s *ProjectService) refreshProjectAfterContentUpdateInternal(ctx context.Context, proj *Project, composeContent, overrideContent *string) {
	if composeContent == nil && overrideContent == nil {
		return
	}

	s.refreshComposeProjectNameInternal(ctx, proj)
	s.refreshProjectImageRefsInternal(ctx, proj)
	if err := s.reconcileComposeTagsForProjectInternal(ctx, proj); err != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags after project update", "projectID", proj.ID, "error", err)
	}
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		slog.WarnContext(ctx, "failed to update service counts after compose edit", "projectID", proj.ID, "error", err)
	}
}

func (s *ProjectService) ApplyGitSyncProjectFiles(ctx context.Context, projectID string, composeContent string, gitEnvContent *string, gitOverrideContent *string, gitOverrideFileName string, user common.User) (*Project, error) {
	proj, projectsDirectory, err := s.getProjectForUpdate(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := ensureProjectMutableInternal(&proj); err != nil {
		return nil, err
	}

	envUpdate, err := s.prepareGitSyncEnvUpdateInternal(proj.Path, gitEnvContent)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to resolve git env state")
	}

	if err := projects.ValidateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, composeContent, envUpdate.effectiveContent, gitOverrideContent, gitOverrideFileName, true); err != nil {
		return nil, errors.WrapIf(err, "invalid compose file")
	}

	backup, cleanupBackup, err := s.prepareProjectUpdateBackupInternal(ctx, projectsDirectory, proj.Path, &composeContent, gitEnvContent, gitOverrideContent)
	if err != nil {
		return nil, err
	}
	defer cleanupBackup()

	journalActive := false
	projectStateCommitted := false
	if err := s.applyGitSyncProjectFilesInternal(ctx, &proj, projectsDirectory, composeContent, envUpdate, gitOverrideContent, gitOverrideFileName, &projectStateCommitted); err != nil {
		// A failure after the env persist would otherwise leave the project with
		// new env values and an old or partially updated compose file set.
		err = s.handleProjectUpdateFailureInternal(ctx, projectID, projectsDirectory, &proj, backup, &journalActive, projectStateCommitted, err)
		return nil, err
	}

	s.refreshComposeProjectNameInternal(ctx, &proj)
	s.refreshProjectImageRefsInternal(ctx, &proj)
	if err := s.reconcileComposeTagsForProjectInternal(ctx, &proj); err != nil {
		slog.WarnContext(ctx, "failed to reconcile Compose project tags after git sync", "projectID", proj.ID, "error", err)
	}

	// Recalculate service counts and status after compose file sync
	if err := s.updateProjectStatusandCountsInternal(ctx, proj.ID, proj.Status); err != nil {
		slog.WarnContext(ctx, "failed to update service counts after git sync", "projectID", proj.ID, "error", err)
	}

	metadata := database.JSON{
		"action":          "git_sync_update",
		"projectID":       proj.ID,
		"projectName":     proj.Name,
		"composeUpdated":  true,
		"envUpdated":      gitEnvContent != nil,
		"overrideUpdated": gitOverrideContent != nil,
	}
	if gitEnvContent == nil {
		metadata["envSourceRemoved"] = true
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, proj.ID, proj.Name, user, metadata, "could not log git sync project update action")

	return &proj, nil
}

// applyGitSyncProjectFilesInternal persists the synced env, compose, and
// override files, then the project row. The env is persisted first so
// WriteComposeFile targets the COMPOSE_FILE base the updated .env selects, not
// the one the old .env selected. When it fails before the project row is saved,
// the caller restores the pre-update backup.
func (s *ProjectService) applyGitSyncProjectFilesInternal(ctx context.Context, proj *Project, projectsDirectory, composeContent string, envUpdate gitSyncEnvUpdateInternal, gitOverrideContent *string, gitOverrideFileName string, projectStateCommitted *bool) error {
	if err := persistGitSyncEnvFilesInternal(ctx, proj.Path, projectsDirectory, envUpdate); err != nil {
		return errors.WrapIf(err, "failed to sync git env files")
	}
	if err := projects.WriteComposeFile(ctx, projectsDirectory, proj.Path, composeContent); err != nil {
		return errors.WrapIf(err, "failed to save compose file")
	}
	if err := projects.WriteComposeOverrideFile(ctx, projectsDirectory, proj.Path, gitOverrideContent, gitOverrideFileName); err != nil {
		return errors.WrapIf(err, "failed to sync git override file")
	}
	if err := s.db.WithContext(ctx).Save(proj).Error; err != nil {
		return errors.WrapIf(err, "failed to update project")
	}
	if projectStateCommitted != nil {
		*projectStateCommitted = true
	}
	return nil
}

func (s *ProjectService) getProjectForUpdate(ctx context.Context, projectID string) (Project, string, error) {
	var proj Project
	if err := s.db.WithContext(ctx).First(&proj, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Project{}, "", errors.New("project not found")
		}
		return Project{}, "", errors.WrapIf(err, "failed to get project")
	}

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return Project{}, "", errors.WrapIf(err, "failed to get projects directory")
	}

	if err := s.EnsureProjectPathUnderRoot(ctx, &proj, false); err != nil {
		return Project{}, "", err
	}

	return proj, projectsDirectory, nil
}

func (s *ProjectService) prepareProjectRenameVolumeMigrationForUpdateInternal(ctx context.Context, proj *Project, name *string, projectsDirectory string, composeContent, envContent, overrideContent *string) (volumetypes.Migration, error) {
	if !isProjectRenameRequestedInternal(proj, name) {
		return nil, nil
	}

	if composeContent == nil && envContent == nil && overrideContent == nil {
		return s.prepareProjectRenameVolumeMigrationInternal(ctx, proj, name)
	}

	previewLogical, err := acfs.MkdirTemp(ctx, projectsDirectory, "/", ".project-update-preview-*")
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create project update preview")
	}
	previewPath := filepath.Join(projectsDirectory, filepath.FromSlash(strings.TrimPrefix(previewLogical, "/")))
	defer func() {
		// The cleanup must run even when the update was cancelled, or the
		// preview directory leaks: acfs refuses operations on an
		// already-cancelled context.
		cleanupCtx := context.WithoutCancel(ctx)
		if removeErr := acfs.RemoveAll(cleanupCtx, projectsDirectory, previewLogical); removeErr != nil {
			slog.WarnContext(cleanupCtx, "failed to remove project update preview", "path", previewPath, "error", removeErr)
		}
	}()

	if _, err := acfs.CopyDir(ctx, proj.Path, previewPath, acfstypes.CopyOptions{}); err != nil {
		return nil, errors.WrapIf(err, "failed to prepare project update preview")
	}

	previewProject := *proj
	previewProject.Path = previewPath
	if err := s.persistUpdatedProjectFiles(ctx, &previewProject, projectsDirectory, composeContent, envContent, overrideContent); err != nil {
		return nil, errors.WrapIf(err, "failed to prepare project update preview")
	}

	return s.prepareProjectRenameVolumeMigrationInternal(ctx, &previewProject, name)
}

func (s *ProjectService) prepareProjectRenameVolumeMigrationInternal(ctx context.Context, proj *Project, name *string) (volumetypes.Migration, error) {
	oldComposeName, newComposeName, ok := projectRenameVolumeMigrationComposeNamesInternal(s, proj, name)
	if !ok {
		return nil, nil
	}

	composeProject, _, err := s.loadComposeProjectForProjectInternal(ctx, proj)
	if err != nil {
		if errors.Is(err, common.ErrProjectComposeFileNotFound) {
			return nil, nil
		}
		return nil, errors.WrapIf(err, "failed to load compose project for volume rename")
	}

	dockerClient, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to connect to Docker for volume rename")
	}

	registry := ""
	if s.settingsService != nil {
		registry = s.settingsService.GetSettingsConfig().ToolsImageRegistry.Value
	}

	return projects.PlanVolumeMigration(ctx, dockerClient, composeProject, oldComposeName, newComposeName, volumehelper.ToolsImage(registry))
}

func projectRenameVolumeMigrationComposeNamesInternal(s *ProjectService, proj *Project, name *string) (string, string, bool) {
	if s == nil || s.dockerService == nil || proj == nil || name == nil {
		return "", "", false
	}

	newProjectName := strings.TrimSpace(*name)
	if newProjectName == "" || proj.Name == newProjectName || proj.Status != ProjectStatusStopped {
		return "", "", false
	}

	oldComposeName := projects.NormalizeProjectName(proj.Name)
	newComposeName := projects.NormalizeProjectName(newProjectName)
	if oldComposeName == "" || newComposeName == "" || oldComposeName == newComposeName {
		return "", "", false
	}

	return oldComposeName, newComposeName, true
}

func isProjectRenameRequestedInternal(proj *Project, name *string) bool {
	if proj == nil || name == nil {
		return false
	}
	newName := strings.TrimSpace(*name)
	return newName != "" && proj.Name != newName
}

func backupProjectDirectoryInternal(ctx context.Context, projectsDirectory, projectPath string, scope projects.ProjectUpdateBackupScope) (*projects.ProjectUpdateBackup, error) {
	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to resolve project path")
	}
	projectAbs = filepath.Clean(projectAbs)

	rootAbs, err := filepath.Abs(projectsDirectory)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to resolve projects directory")
	}
	rootAbs = filepath.Clean(rootAbs)
	if !projects.IsSafeSubdirectory(rootAbs, projectAbs) || projectAbs == rootAbs {
		return nil, errors.New("project path is outside projects directory")
	}

	backupLogical, err := acfs.MkdirTemp(ctx, projectsDirectory, "/", ".project-update-backup-*")
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create project backup directory")
	}
	backupPath := filepath.Join(projectsDirectory, filepath.FromSlash(strings.TrimPrefix(backupLogical, "/")))
	// Tolerate files Arcane cannot read (e.g. foreign-owned secrets): skip them
	// in the backup so an unrelated unreadable file can't block the whole save.
	// The skipped paths are recorded so the rollback restore can preserve them.
	backup, err := projects.BackupProjectUpdateScope(ctx, projectAbs, backupPath, scope)
	if err != nil {
		// The unwind must run even when the backup failed because ctx was
		// cancelled, or the fresh backup directory leaks.
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, backupLogical)
		return nil, errors.WrapIf(err, "failed to backup project files")
	}
	if len(backup.Skipped) > 0 {
		slog.WarnContext(ctx, "skipped unreadable files while backing up project; they will be left untouched on rollback", "projectPath", projectAbs, "skipped", backup.Skipped)
	}
	return backup, nil
}

func restoreProjectDirectoryBackupInternal(ctx context.Context, projectsDirectory, projectPath string, backup *projects.ProjectUpdateBackup) error {
	projectAbs, err := filepath.Abs(projectPath)
	if err != nil {
		return errors.WrapIf(err, "failed to resolve project path")
	}
	projectAbs = filepath.Clean(projectAbs)

	rootAbs, err := filepath.Abs(projectsDirectory)
	if err != nil {
		return errors.WrapIf(err, "failed to resolve projects directory")
	}
	rootAbs = filepath.Clean(rootAbs)
	if !projects.IsSafeSubdirectory(rootAbs, projectAbs) || projectAbs == rootAbs {
		return errors.New("project path is outside projects directory")
	}

	projectLogical, err := acfs.LogicalPath(rootAbs, projectAbs)
	if err != nil {
		return errors.WrapIf(err, "failed to resolve project directory")
	}

	slog.DebugContext(ctx, "restoring project directory backup", "path", projectAbs, "backup", backup.BackupDir)
	if err := acfs.MkdirAll(ctx, rootAbs, projectLogical, utils.DirPerm); err != nil {
		return errors.WrapIf(err, "failed to recreate project directory")
	}
	// Restore only the paths the update could have mutated, in place: files
	// that were skipped during backup (unreadable, e.g. foreign-owned secrets)
	// are preserved, and out-of-scope files are never touched.
	if err := projects.RestoreProjectUpdateBackup(ctx, projectAbs, backup); err != nil {
		return errors.WrapIf(err, "failed to restore project backup")
	}
	return nil
}

func (s *ProjectService) persistUpdatedProjectFiles(ctx context.Context, proj *Project, projectsDirectory string, composeContent, envContent, overrideContent *string) error {
	switch {
	case composeContent != nil:
		effectiveEnvContent, err := s.resolveEffectiveEnvContentForUpdateInternal(proj.Path, envContent)
		if err != nil {
			return errors.WrapIf(err, "invalid compose file")
		}
		valOverride, valOverrideName := projects.ResolveEffectiveOverrideForValidation(proj.Path, overrideContent)
		if err := projects.ValidateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, *composeContent, effectiveEnvContent, valOverride, valOverrideName, false); err != nil {
			return errors.WrapIf(err, "invalid compose file")
		}
		// The env is persisted first so WriteComposeFile targets the COMPOSE_FILE
		// base the updated .env selects, not the one the old .env selected. A
		// non-nil composeContent is an explicit submission and is always written;
		// clients omit it when the compose editor is unchanged.
		if envContent != nil {
			if err := persistEffectiveEnvContentInternal(ctx, proj.Path, projectsDirectory, *envContent); err != nil {
				return errors.WrapIf(err, "failed to save project files")
			}
		} else if err := s.ensureEffectiveEnvFileInternal(ctx, proj.Path, projectsDirectory); err != nil {
			return errors.WrapIf(err, "failed to save project files")
		}
		if err := projects.WriteComposeFile(ctx, projectsDirectory, proj.Path, *composeContent); err != nil {
			return errors.WrapIf(err, "failed to save project files")
		}
		if err := projects.ApplyOverrideFileChange(ctx, projectsDirectory, proj.Path, overrideContent); err != nil {
			return errors.WrapIf(err, "failed to save project files")
		}
	case overrideContent != nil:
		if err := s.persistOverrideOnlyUpdateInternal(ctx, proj, projectsDirectory, envContent, overrideContent); err != nil {
			return err
		}
	case envContent != nil:
		if err := persistEffectiveEnvContentInternal(ctx, proj.Path, projectsDirectory, *envContent); err != nil {
			return err
		}
	}

	return nil
}

// persistOverrideOnlyUpdateInternal handles a save that changes the override (and
// optionally the env) without touching the base compose file. It validates the
// on-disk base merged with the requested override so a base that is only valid
// *with* its override still validates, and a delete that would break the base
// fails before touching disk.
func (s *ProjectService) persistOverrideOnlyUpdateInternal(ctx context.Context, proj *Project, projectsDirectory string, envContent, overrideContent *string) error {
	baseContent, _, err := projects.ReadProjectFiles(ctx, proj.Path, "")
	if err != nil {
		return errors.WrapIf(err, "failed to read project files")
	}
	effectiveEnvContent, err := s.resolveEffectiveEnvContentForUpdateInternal(proj.Path, envContent)
	if err != nil {
		return errors.WrapIf(err, "invalid compose file")
	}
	valOverride, valOverrideName := projects.ResolveEffectiveOverrideForValidation(proj.Path, overrideContent)
	if err := projects.ValidateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, baseContent, effectiveEnvContent, valOverride, valOverrideName, false); err != nil {
		return errors.WrapIf(err, "invalid compose file")
	}
	if envContent != nil {
		if err := persistEffectiveEnvContentInternal(ctx, proj.Path, projectsDirectory, *envContent); err != nil {
			return errors.WrapIf(err, "failed to save project files")
		}
	}
	if err := projects.ApplyOverrideFileChange(ctx, projectsDirectory, proj.Path, overrideContent); err != nil {
		return errors.WrapIf(err, "failed to save project files")
	}
	return nil
}

func (s *ProjectService) ensureProjectStoppedForRenameInternal(ctx context.Context, proj *Project, name *string) error {
	if !isProjectRenameRequestedInternal(proj, name) {
		return nil
	}
	if proj.Status != ProjectStatusStopped && proj.Status != ProjectStatusUnknown {
		return errors.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
	}

	services, err := s.GetProjectServices(ctx, proj.ID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve project status before rename", "projectID", proj.ID, "error", err)
		return errors.WrapIff(err, "project must be stopped before renaming (current status: %s): failed to verify live status", proj.Status)
	}

	status := calculateProjectStatus(services)
	if status != ProjectStatusStopped {
		return errors.Errorf("project must be stopped before renaming (current status: %s)", status)
	}

	serviceCount, runningCount := getServiceCounts(services)
	proj.Status = ProjectStatusStopped
	proj.StatusReason = nil
	proj.ServiceCount = serviceCount
	proj.RunningCount = runningCount
	return nil
}

func (s *ProjectService) applyProjectRenameIfNeeded(ctx context.Context, proj *Project, name *string, projectsDirectory string) error {
	if name == nil {
		return nil
	}

	newName := strings.TrimSpace(*name)
	if newName == "" || proj.Name == newName {
		return nil
	}

	if proj.Status != ProjectStatusStopped {
		return errors.Errorf("project must be stopped before renaming (current status: %s)", proj.Status)
	}

	newDirName := projects.SanitizeProjectName(newName)
	if newDirName == "" || strings.Trim(newDirName, "_") == "" {
		return errors.New("invalid project name: results in empty directory name")
	}

	currentPath := filepath.Clean(proj.Path)
	targetPath := filepath.Clean(filepath.Join(projectsDirectory, newDirName))
	if currentPath != targetPath {
		targetLogical, err := acfs.LogicalPath(projectsDirectory, targetPath)
		if err != nil {
			return errors.WrapIf(err, "failed to resolve project directory rename target")
		}
		exists, err := acfs.Exists(ctx, projectsDirectory, targetLogical)
		if err != nil {
			return errors.WrapIf(err, "failed to check project directory rename target")
		}
		if exists {
			return errors.Errorf("project directory already exists: %s", targetPath)
		}

		// An imported project can live outside the projects directory, in which
		// case the move crosses roots and cannot be a confined rename.
		currentLogical, currentErr := acfs.LogicalPath(projectsDirectory, currentPath)
		if currentErr != nil {
			// The cross-root move cannot go through acfs, so the cancellation
			// check acfs.Rename performs happens here instead.
			if err := ctx.Err(); err != nil {
				return err
			}
			err = os.Rename(currentPath, targetPath)
		} else {
			err = acfs.Rename(ctx, projectsDirectory, currentLogical, targetLogical)
		}
		if err != nil {
			return errors.WrapIf(err, "failed to rename project directory")
		}

		proj.Path = targetPath
	}

	proj.DirName = &newDirName
	proj.Name = newName
	return nil
}

type activeProjectRenameSyncStateInternal struct {
	skipDiscoveredPaths map[string]struct{}
	protectSeenPaths    map[string]struct{}
}

func (s *ProjectService) activeProjectRenameSyncStateInternal(ctx context.Context) activeProjectRenameSyncStateInternal {
	state := activeProjectRenameSyncStateInternal{
		skipDiscoveredPaths: make(map[string]struct{}),
		protectSeenPaths:    make(map[string]struct{}),
	}
	if s == nil || s.kvService == nil {
		return state
	}

	entries, err := s.kvService.ListByPrefix(ctx, projecttypes.RenameJournalKeyPrefix)
	if err != nil {
		slog.WarnContext(ctx, "failed to list project rename journals during filesystem sync", "error", err)
		return state
	}

	for _, entry := range entries {
		var journal projecttypes.RenameJournal
		if err := json.Unmarshal([]byte(entry.Value), &journal); err != nil {
			slog.WarnContext(ctx, "failed to decode project rename journal during filesystem sync", "key", entry.Key, "error", err)
			continue
		}
		if !projects.RenameJournalFilesystemSyncPending(journal.Phase) {
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

func (s *ProjectService) startProjectRenameJournalInternal(ctx context.Context, journal *projecttypes.RenameJournal) (bool, error) {
	if journal == nil {
		return false, nil
	}
	if err := s.writeProjectRenameJournalInternal(ctx, journal, projecttypes.RenameJournalPhaseStarted); err != nil {
		return false, err
	}
	return true, nil
}

func withProjectRenameRollbackInternal(ctx context.Context, proj *Project, projectStateCommitted *bool, run func() error) error {
	originalPath := proj.Path
	originalDirName := proj.DirName

	if err := run(); err != nil {
		if projectStateCommitted != nil && *projectStateCommitted {
			return err
		}
		if proj.Path != originalPath {
			// The rollback has to run even when the caller's context is already
			// cancelled, or a cancelled update leaves the directory renamed
			// with the database still pointing at the original path.
			rollbackCtx := context.WithoutCancel(ctx)

			// Both paths share a parent whenever the rename stayed inside the
			// projects directory; an imported project can sit elsewhere, in
			// which case the move crosses roots and cannot be confined.
			var renameErr error
			if parent := filepath.Dir(originalPath); parent == filepath.Dir(proj.Path) {
				renameErr = acfs.Rename(rollbackCtx, parent, "/"+filepath.Base(proj.Path), "/"+filepath.Base(originalPath))
			} else {
				renameErr = os.Rename(proj.Path, originalPath)
			}
			if renameErr != nil {
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

func (s *ProjectService) prepareProjectRenameJournalInternal(proj *Project, name *string, projectsDirectory string, migration volumetypes.Migration) *projecttypes.RenameJournal {
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

	journal := &projecttypes.RenameJournal{
		ProjectID:  proj.ID,
		OldName:    proj.Name,
		NewName:    newName,
		OldPath:    filepath.Clean(proj.Path),
		NewPath:    filepath.Clean(filepath.Join(projectsDirectory, newDirName)),
		OldDirName: cloneStringPtrInternal(proj.DirName),
		NewDirName: newDirName,
		Phase:      projecttypes.RenameJournalPhaseStarted,
	}

	if source, ok := migration.(volumetypes.JournalSource); ok {
		journal.Volumes = source.JournalVolumes()
	}

	return journal
}

func (s *ProjectService) writeProjectRenameJournalInternal(ctx context.Context, journal *projecttypes.RenameJournal, phase string) error {
	if s == nil || s.kvService == nil || journal == nil {
		return nil
	}
	journal.Phase = phase
	journal.UpdatedAt = time.Now().UTC()

	payload, err := json.Marshal(journal)
	if err != nil {
		return errors.WrapIf(err, "marshal project rename journal")
	}

	if err := s.kvService.Set(ctx, projecttypes.RenameJournalKeyPrefix+journal.ProjectID, string(payload)); err != nil {
		return errors.WrapIf(err, "write project rename journal")
	}
	return nil
}

func (s *ProjectService) clearProjectRenameJournalInternal(ctx context.Context, projectID string) error {
	if s == nil || s.kvService == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kvService.Delete(ctx, projecttypes.RenameJournalKeyPrefix+projectID)
}

func (s *ProjectService) writeProjectRenameRollbackCleanupInternal(ctx context.Context, journal *projecttypes.RenameJournal) error {
	if s == nil || s.kvService == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" || len(journal.Volumes) == 0 {
		return nil
	}

	cleanup := projecttypes.RenameRollbackCleanup{
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
	if err := s.kvService.Set(ctx, projecttypes.RenameRollbackCleanupKeyPrefix+journal.ProjectID, string(payload)); err != nil {
		return errors.WrapIf(err, "write project rename rollback cleanup")
	}
	return nil
}

func (s *ProjectService) clearProjectRenameRollbackCleanupInternal(ctx context.Context, projectID string) error {
	if s == nil || s.kvService == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	return s.kvService.Delete(ctx, projecttypes.RenameRollbackCleanupKeyPrefix+projectID)
}

func (s *ProjectService) RecoverProjectRenameJournals(ctx context.Context) error {
	if s == nil || s.kvService == nil {
		return nil
	}

	entries, err := s.kvService.ListByPrefix(ctx, projecttypes.RenameJournalKeyPrefix)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var journal projecttypes.RenameJournal
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

	raw, ok, err := s.kvService.Get(ctx, projecttypes.RenameJournalKeyPrefix+projectID)
	if err != nil || !ok {
		return err
	}

	var journal projecttypes.RenameJournal
	if err := json.Unmarshal([]byte(raw), &journal); err != nil {
		return errors.WrapIf(err, "decode project rename journal")
	}
	return s.recoverProjectRenameJournalInternal(ctx, &journal)
}

func (s *ProjectService) recoverProjectRenameJournalInternal(ctx context.Context, journal *projecttypes.RenameJournal) error {
	if s == nil || journal == nil || strings.TrimSpace(journal.ProjectID) == "" {
		return nil
	}

	var proj Project
	dbErr := s.db.WithContext(ctx).First(&proj, "id = ?", journal.ProjectID).Error
	if dbErr != nil && !errors.Is(dbErr, gorm.ErrRecordNotFound) {
		return errors.WrapIf(dbErr, "load project for rename recovery")
	}

	projectCommitted := dbErr == nil && (proj.Name == journal.NewName || filepath.Clean(proj.Path) == filepath.Clean(journal.NewPath))
	return projects.RecoverRenameJournal(ctx, journal, projectCommitted, s.renameRecoveryOperationsInternal())
}

func (s *ProjectService) recoverProjectRenameRollbackCleanupsInternal(ctx context.Context) error {
	if s == nil || s.kvService == nil {
		return nil
	}

	entries, err := s.kvService.ListByPrefix(ctx, projecttypes.RenameRollbackCleanupKeyPrefix)
	if err != nil {
		return err
	}

	var recoverErr error
	for _, entry := range entries {
		var cleanup projecttypes.RenameRollbackCleanup
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

func (s *ProjectService) recoverProjectRenameRollbackCleanupInternal(ctx context.Context, cleanup *projecttypes.RenameRollbackCleanup) error {
	if s == nil || cleanup == nil || strings.TrimSpace(cleanup.ProjectID) == "" {
		return nil
	}
	if len(cleanup.Volumes) == 0 {
		return s.clearProjectRenameRollbackCleanupInternal(ctx, cleanup.ProjectID)
	}

	var proj Project
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

	return projects.CleanupRenameRollbackTargets(ctx, cleanup, s.renameRecoveryOperationsInternal())
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

func (s *ProjectService) renameRecoveryOperationsInternal() projecttypes.RenameRecoveryOperations {
	return projecttypes.RenameRecoveryOperations{
		Docker:               s.projectRenameRecoveryDockerInternal,
		WriteJournal:         s.writeProjectRenameJournalInternal,
		ClearJournal:         s.clearProjectRenameJournalInternal,
		ClearRollbackCleanup: s.clearProjectRenameRollbackCleanupInternal,
		WriteRollbackCleanup: s.writeProjectRenameRollbackCleanupInternal,
		RestoreState: func(ctx context.Context, journal *projecttypes.RenameJournal) error {
			return s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", journal.ProjectID).Updates(map[string]any{
				"name": journal.OldName, "path": journal.OldPath, "dir_name": journal.OldDirName,
			}).Error
		},
	}
}
