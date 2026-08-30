package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	stderrors "errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"emperror.dev/emperror"
	"emperror.dev/errors"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
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
	if err := withProjectRenameRollback(ctx, &proj, &projectStateCommitted, func() error {
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

func (s *ProjectService) applyProjectUpdateWithRenameJournalInternal(ctx context.Context, proj *Project, name *string, projectsDirectory string, composeContent, envContent, overrideContent *string, volumeMigration volumes.Migration, renameJournal *projectRenameJournalInternal, journalActive *bool, projectStateCommitted *bool) (err error) {
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
	if err = s.applyProjectVolumeMigrationForUpdateInternal(ctx, volumeMigration, renameJournal, &volumeMigrationApplied); err != nil {
		return err
	}
	if err = s.saveProjectUpdateInternal(ctx, proj); err != nil {
		return err
	}
	if projectStateCommitted != nil {
		*projectStateCommitted = true
	}
	s.finalizeProjectRenameAfterCommitInternal(ctx, proj.ID, volumeMigration, renameJournal, journalActive)
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

	if err := validateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, composeContent, envUpdate.effectiveContent, gitOverrideContent, gitOverrideFileName, true); err != nil {
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

func (s *ProjectService) prepareProjectRenameVolumeMigrationForUpdateInternal(ctx context.Context, proj *Project, name *string, projectsDirectory string, composeContent, envContent, overrideContent *string) (volumes.Migration, error) {
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

func (s *ProjectService) prepareProjectRenameVolumeMigrationInternal(ctx context.Context, proj *Project, name *string) (volumes.Migration, error) {
	oldComposeName, newComposeName, ok := projectRenameVolumeMigrationComposeNamesInternal(s, proj, name)
	if !ok {
		return nil, nil
	}

	composeProject, _, err := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
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

	return volumes.PlanMigration(ctx, dockerClient, composeProject, oldComposeName, newComposeName)
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
		if err := validateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, *composeContent, effectiveEnvContent, valOverride, valOverrideName, false); err != nil {
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
	if err := validateComposeContentForUpdate(ctx, projectsDirectory, proj.Path, proj.Name, baseContent, effectiveEnvContent, valOverride, valOverrideName, false); err != nil {
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

func validateComposeContentForUpdate(ctx context.Context, projectsDirectory, projectPath, projectName, composeContent string, effectiveEnvContent *string, overrideContent *string, overrideFileName string, lenient bool) (err error) {
	defer func() {
		if panicErr := emperror.Recover(recover()); panicErr != nil {
			err = errors.WrapIf(panicErr, "compose file contains invalid syntax")
		}
	}()

	fullEnvMap, envErr := projects.BuildValidationEnvironment(ctx, projectsDirectory, projectPath, effectiveEnvContent)
	if envErr != nil {
		return envErr
	}

	// COMPOSE_FILE in the (edited) env selects the exact file set, so validate
	// against it — a save that introduces a broken selection fails fast.
	envOpts, envOptsErr := projects.ParseComposeEnvOptions(projectPath, fullEnvMap)
	if envOptsErr != nil {
		return envOptsErr
	}

	validationProjectName := projects.NormalizeProjectName(projectName)
	var configFiles []composetypes.ConfigFile
	switch {
	case len(envOpts.ConfigFiles) > 0:
		// The compose tab edits the base (first) file. Later entries are read
		// from disk; existence was already checked while parsing COMPOSE_FILE. An
		// Arcane-managed override lives in the project root and only applies when
		// that exact path is listed in the selection (docker skips auto-overrides
		// for an explicit file set); a same-named file in a subdirectory is not
		// the managed override.
		overrideName := strings.TrimSpace(overrideFileName)
		if overrideName == "" {
			overrideName = projects.DefaultComposeOverrideFileName
		}
		overridePath := ""
		if absProjectPath, absErr := filepath.Abs(filepath.Clean(projectPath)); absErr == nil {
			overridePath = filepath.Join(absProjectPath, overrideName)
		}
		for i, f := range envOpts.ConfigFiles {
			switch {
			case i == 0:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f, Content: []byte(composeContent)})
			case overrideContent != nil && overridePath != "" && f == overridePath:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f, Content: []byte(*overrideContent)})
			default:
				configFiles = append(configFiles, composetypes.ConfigFile{Filename: f})
			}
		}
	default:
		configFiles = []composetypes.ConfigFile{
			{Filename: filepath.Join(projectPath, "compose.yaml"), Content: []byte(composeContent)},
		}
		// When an override is supplied, validate the *merged* config as `docker
		// compose` would deploy it. Overrides can add services and are layered on
		// top (listed after the base so the override wins).
		if overrideContent != nil {
			overrideName := strings.TrimSpace(overrideFileName)
			if overrideName == "" {
				overrideName = projects.DefaultComposeOverrideFileName
			}
			configFiles = append(configFiles, composetypes.ConfigFile{
				Filename: filepath.Join(projectPath, overrideName),
				Content:  []byte(*overrideContent),
			})
		}
	}

	cfg := composetypes.ConfigDetails{
		Version:     api.ComposeVersion,
		WorkingDir:  projectPath,
		ConfigFiles: configFiles,
		Environment: composetypes.Mapping(fullEnvMap),
	}

	missingIncludeLoader := projects.NewMissingIncludeStubLoader(projectPath)
	defer missingIncludeLoader.Cleanup()

	err = projects.WithTransientValidationEnvFile(ctx, projectPath, effectiveEnvContent, func() error {
		_, loadErr := loader.LoadWithContext(ctx, cfg, func(opts *loader.Options) {
			opts.ResourceLoaders = append([]loader.ResourceLoader{missingIncludeLoader}, opts.ResourceLoaders...)
			if validationProjectName != "" {
				opts.SetProjectName(validationProjectName, false)
			}
			if lenient {
				projects.ApplyLenientLoaderOptions(ctx, opts, cfg.ConfigFiles[0].Filename)
			}
		})
		return loadErr
	})

	return err
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
