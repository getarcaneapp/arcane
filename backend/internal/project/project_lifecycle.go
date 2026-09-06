package project

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"emperror.dev/errors"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	"github.com/getarcaneapp/arcane/types/v2"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"

	"go.getarcane.app/acfs"
	buildtypes "go.getarcane.app/builds/types"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
	"gorm.io/gorm"
)

var (
	composeStopProjectServicesInternal = projects.ComposeStop
	composeUpProjectServicesInternal   = projects.ComposeUp
)

func (s *ProjectService) reconcilePulledImageRefsInternal(ctx context.Context, imageRefs []string) {
	if s.imageService == nil {
		return
	}

	for _, imageRef := range imageRefs {
		if err := s.imageService.ReconcilePulledImageUpdate(ctx, imageRef); err != nil {
			slog.WarnContext(ctx, "failed to reconcile pulled image update state", "image", imageRef, "error", err)
		}
	}
}

func (s *ProjectService) pullAndReconcileImageInternal(
	ctx context.Context,
	imageRef string,
	progressWriter io.Writer,
	user common.User,
	credentials []containerregistry.Credential,
) error {
	if s == nil || s.imageService == nil {
		return errors.New("image service not available")
	}

	settings := s.settingsService.GetSettingsConfig()

	pullCtx, pullCancel := context.WithTimeout(ctx, timeouts.GetDuration(settings.DockerImagePullTimeout.AsInt(), timeouts.DefaultDockerImagePull))
	defer pullCancel()

	if err := s.imageService.PullImage(pullCtx, imageRef, progressWriter, user, credentials); err != nil {
		if errors.Is(pullCtx.Err(), context.DeadlineExceeded) {
			return errors.Errorf("image pull timed out for %s (increase DOCKER_IMAGE_PULL_TIMEOUT or setting)", imageRef)
		}
		return errors.WrapIff(err, "failed to pull image %s", imageRef)
	}

	s.reconcilePulledImageRefsInternal(ctx, []string{imageRef})
	return nil
}

func (s *ProjectService) UpdateProjectServices(ctx context.Context, projectID string, servicesToUpdate []string, user common.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	previousStatus := projectFromDb.Status

	// 1. Load project
	compProj, _, err := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, servicesToUpdate...)
	if err != nil {
		return errors.WrapIf(err, "failed to load compose project")
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	// 2. Set status to deploying/restarting
	if err := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusDeploying); err != nil {
		return err
	}

	credentials, err := s.ResolveRegistryCredentials(ctx)
	if err != nil {
		if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to restore project status after credential lookup failure", "projectID", projectID, "error", statusErr)
		}
		return errors.WrapIf(err, "resolve registry credentials")
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	if err := s.composeCoordinator.UpdateServices(ctx, projecttypes.ComposeServiceUpdate{
		Project: compProj, Services: servicesToUpdate,
		Images: s.composeImageOperationsInternal(&user, credentials), Progress: progressWriter,
		AuthConfigs: s.composeRegistryAuthConfigsInternal(ctx), WaitTimeout: s.deployWaitTimeoutInternal(),
		RestoreBeforeMutation: func(ctx context.Context) {
			if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
				slog.ErrorContext(ctx, "failed to restore project status before service update", "projectID", projectID, "error", statusErr)
			}
		},
		Recover: func(ctx context.Context) { s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID) },
	}); err != nil {
		return err
	}

	// 6. Finalize status
	if err := s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning); err != nil {
		return err
	}

	metadata := database.JSON{
		"action":      "update_services",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
		"services":    append([]string(nil), servicesToUpdate...),
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, projectFromDb.Name, user, metadata, "could not log project service update action")

	return nil
}

func ensureProjectMutableInternal(proj *Project) error {
	if proj != nil && proj.IsArchived {
		return common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	return nil
}

func (s *ProjectService) ArchiveProject(ctx context.Context, projectID string, user common.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if proj.IsArchived {
		return nil
	}

	// Gate on live Docker state, not the persisted status row, which can go
	// stale when containers are stopped outside an Arcane project action.
	// A project without a compose file cannot have managed containers running
	// and is a prime archive candidate, so it is allowed through.
	services, servicesErr := s.GetProjectServices(ctx, projectID)
	switch {
	case servicesErr != nil && !errors.Is(servicesErr, common.ErrProjectComposeFileNotFound):
		return errors.WrapIf(servicesErr, "cannot verify project is stopped before archiving")
	case servicesErr == nil:
		if _, running := getServiceCounts(services); running > 0 {
			return common.Classify(common.ErrProjectMustBeStopped, errors.New("project must be stopped before archiving"))
		}
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": true,
		"archived_at": now,
	}).Error; err != nil {
		return errors.WrapIf(err, "failed to archive project")
	}

	metadata := database.JSON{"action": "archived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project archive action")

	return nil
}

func (s *ProjectService) UnarchiveProject(ctx context.Context, projectID string, user common.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if !proj.IsArchived {
		return nil
	}

	if err := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": false,
		"archived_at": gorm.Expr("NULL"),
	}).Error; err != nil {
		return errors.WrapIf(err, "failed to unarchive project")
	}

	metadata := database.JSON{"action": "unarchived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project unarchive action")

	return nil
}

// deployWaitTimeoutInternal resolves how long compose up waits for depends_on
// health/completion conditions, from the deployWaitTimeout setting.
func (s *ProjectService) deployWaitTimeoutInternal() time.Duration {
	return timeouts.GetDuration(s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(), timeouts.DefaultDeployWait)
}

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user common.User, options *projecttypes.DeployOptions) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return errors.WrapIf(err, "failed to get project")
	}
	if err := ensureProjectMutableInternal(projectFromDb); err != nil {
		return err
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusDeploying); err != nil {
		return errors.WrapIf(err, "failed to update project status to deploying")
	}
	var closeSuppression func()
	defer func() {
		if closeSuppression != nil {
			closeSuppression()
		}
	}()

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	projectModel, err := s.composeCoordinator.Deploy(ctx, projecttypes.ComposeDeployment{
		ProjectID: projectID, ProjectPath: projectFromDb.Path, Options: options,
		DefaultPullPolicy: s.settingsService.GetStringSetting(ctx, "defaultDeployPullPolicy", "missing"),
		GitOpsManaged:     projectFromDb.GitOpsManagedBy != nil && *projectFromDb.GitOpsManagedBy != "",
		WaitTimeout:       s.deployWaitTimeoutInternal(), AuthConfigs: s.composeRegistryAuthConfigsInternal(ctx), Progress: progressWriter,
		PreDeploy: func(ctx context.Context) error {
			if s.lifecycleService == nil {
				return nil
			}
			return s.lifecycleService.RunPreDeploy(ctx, projectFromDb, user)
		},
		Load: func(ctx context.Context) (*composetypes.Project, error) {
			model, _, err := s.loadComposeProjectForProjectInternal(ctx, projectFromDb)
			if err == nil {
				closeSuppression = s.eventService.BeginComposeSuppressionWindow(model.Name)
			}
			return model, err
		},
		ResolveImages: func(ctx context.Context) (projecttypes.ComposeImageOperations, error) {
			credentials, err := s.ResolveRegistryCredentials(ctx)
			operations := s.composeImageOperationsInternal(&user, credentials)
			// Deployment pulls are recorded as system actions; builds retain the requesting actor.
			operations.Pull = s.composeImageOperationsInternal(nil, credentials).Pull
			return operations, err
		},
		Recover: func(ctx context.Context) { s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID) },
	})
	if err != nil {
		return err
	}

	metadata := database.JSON{"action": "deploy", "projectID": projectID, "projectName": projectModel.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectDeploy, projectID, projectModel.Name, user, metadata, "could not log project deployment action")

	err = s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning)
	if err != nil {
		slog.Error("failed to update project status and counts after deploy", "projectID", projectID, "error", err)
	}
	return err
}

func (s *ProjectService) DownProject(ctx context.Context, projectID string, user common.User) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	proj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusStopped); err != nil {
		return errors.WrapIf(err, "failed to update project status to stopping")
	}

	defer s.eventService.BeginComposeSuppressionWindow(proj.Name)()

	if err := projects.ComposeDown(ctx, proj, false); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return errors.WrapIf(err, "failed to bring down project")
	}

	metadata := database.JSON{
		"action":      "down",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectStop, projectID, projectFromDb.Name, user, metadata, "could not log project down action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusStopped)
}

// CreateProject creates a project's directory, files, and DB row. When
// allowNameSuffix is true a directory-name collision is resolved by appending
// "-N" (the interactive default). When false a collision returns
// projects.ErrProjectDirExists (wrapped) so GitOps creates fail loudly instead of
// minting runaway "-N" duplicate projects on a broken binding.
func (s *ProjectService) CreateProject(ctx context.Context, name, composeContent string, envContent *string, manifest projecttypes.CreateProjectWorkspaceManifest, uploads map[int][]byte, uiTags []string, uiTagColors map[string]projecttypes.TagColor, user common.User, allowNameSuffixOptions ...bool) (*Project, error) {
	normalizedUITags, err := projects.NormalizeProjectTags(uiTags)
	if err != nil {
		return nil, errors.WrapIf(err, "invalid project tags")
	}
	normalizedTagColors, err := normalizeProjectTagColorsInternal(uiTagColors)
	if err != nil {
		return nil, errors.WrapIf(err, "invalid project tag colors")
	}
	allowNameSuffix := true
	if len(allowNameSuffixOptions) > 0 {
		allowNameSuffix = allowNameSuffixOptions[0]
	}
	// A top-level `name:` in the compose file is authoritative over the
	// submitted project name.
	if yamlName := projects.ComposeContentProjectName(composeContent); yamlName != "" {
		name = yamlName
	}
	sanitized := projects.SanitizeProjectName(name)

	projectsDirectory, err := projects.GetProjectsDirectory(ctx, s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"))
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get projects directory")
	}

	basePath := filepath.Join(projectsDirectory, sanitized)
	var projectPath, folderName string
	if allowNameSuffix {
		projectPath, folderName, err = projects.CreateUniqueDir(ctx, projectsDirectory, basePath, name, utils.DirPerm)
	} else {
		projectPath, folderName, err = projects.CreateExactDir(ctx, projectsDirectory, basePath, name, utils.DirPerm)
	}
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create project directory")
	}
	projectLogical, err := acfs.LogicalPath(projectsDirectory, projectPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to resolve created project directory")
	}

	proj := &Project{
		Name:         name,
		DirName:      &folderName,
		Path:         projectPath,
		Status:       ProjectStatusStopped,
		ServiceCount: 0,
		RunningCount: 0,
	}

	if err := projects.ApplyProjectWorkspaceChanges(projectPath, manifest.FileChanges, uploads, projects.ProjectWorkspaceApplyOptions{
		MaxDepth:         s.config.ProjectWorkspaceMaxDepth,
		MaxEntries:       s.config.ProjectWorkspaceMaxEntries,
		MaxFileSizeBytes: workspace.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
		SkipDirectories:  s.config.ProjectScanSkipDirs,
		ComposeFileName:  projects.DefaultComposeFileName,
	}); err != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, wrapProjectWorkspaceErrorInternal(err)
	}

	// GitOps-originated creates (allowNameSuffix=false) tolerate not-yet-supplied
	// ${VAR} references the same way single-file git sync updates do; interactive
	// creates (allowNameSuffix=true) stay strict.
	if err := projects.ValidateComposeContentForUpdate(ctx, projectsDirectory, projectPath, name, composeContent, envContent, nil, "", !allowNameSuffix); err != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, errors.WrapIf(err, "invalid compose file")
	}

	if err := projects.WriteProjectFiles(ctx, projectsDirectory, projectPath, composeContent, envContent); err != nil {
		// Best-effort cleanup to restore pre-transaction behavior.
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, errors.WrapIf(err, "failed to save project files")
	}
	composeMeta, err := projects.ParseArcaneComposeMetadata(
		ctx,
		filepath.Join(projectPath, projects.DefaultComposeFileName),
		projectsDirectory,
		s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false),
	)
	if err != nil {
		slog.WarnContext(ctx, "failed to read Compose project tags during creation", "projectName", name, "error", err)
		composeMeta = projects.ArcaneComposeMetadata{}
	}
	normalizedUITags = excludeComposeOwnedUITagsInternal(normalizedUITags, composeMeta.ProjectTags)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(proj).Error; err != nil {
			return err
		}
		return createUIProjectTagsInternal(tx, proj.ID, normalizedUITags, normalizedTagColors)
	}); err != nil {
		_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDirectory, projectLogical)
		return nil, errors.WrapIf(err, "failed to create project")
	}
	s.refreshComposeProjectNameInternal(ctx, proj)
	s.refreshProjectImageRefsInternal(ctx, proj)
	if err := s.reconcileComposeProjectTagsInternal(ctx, proj.ID, composeMeta.ProjectTags); err != nil {
		cleanupCtx := context.WithoutCancel(ctx)
		databaseCleanupErr := s.db.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
			return deleteProjectWithTagsInternal(tx, proj.ID)
		})
		fileCleanupErr := acfs.RemoveAll(cleanupCtx, projectsDirectory, projectLogical)
		return nil, stderrors.Join(
			errors.WrapIf(err, "reconcile Compose project tags"),
			errors.WrapIf(databaseCleanupErr, "rollback project database state after tag reconciliation failure"),
			errors.WrapIf(fileCleanupErr, "rollback project files after tag reconciliation failure"),
		)
	}

	metadata := database.JSON{"action": "create", "projectID": proj.ID, "projectName": proj.Name, "path": projectPath}
	s.logProjectEventInternal(ctx, event.EventTypeProjectCreate, proj.ID, proj.Name, user, metadata, "could not log project creation")

	return proj, nil
}

func (s *ProjectService) DestroyProject(ctx context.Context, projectID string, removeFiles bool, removeVolumes bool, user common.User) error {
	slog.DebugContext(ctx, "DestroyProject service called",
		"projectID", projectID,
		"removeFiles", removeFiles,
		"removeVolumes", removeVolumes,
		"userID", user.ID,
		"username", user.Username)

	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "Found project to destroy",
		"projectName", proj.Name,
		"projectPath", proj.Path)

	if err := s.DownProject(ctx, projectID, common.SystemUser); err != nil {
		slog.WarnContext(ctx, "failed to bring down project", "error", err)
	}

	if removeVolumes {
		if compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj); lerr == nil {
			defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()
			if derr := projects.ComposeDown(ctx, compProj, true); derr != nil {
				slog.WarnContext(ctx, "failed to remove volumes", "error", derr)
			}
		} else {
			slog.WarnContext(ctx, "failed to load compose project for volume removal", "error", lerr)
		}
	}

	if removeFiles {
		slog.DebugContext(ctx, "Removing project files", "path", proj.Path)
		// An imported project can live anywhere, so the removal is rooted at the
		// parent directory and names the project directory itself.
		if err := acfs.RemoveAll(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path)); err != nil {
			slog.ErrorContext(ctx, "Failed to remove project files", "path", proj.Path, "error", err)
			return errors.WrapIf(err, "failed to remove project files")
		}
		slog.InfoContext(ctx, "Project files removed successfully", "path", proj.Path)
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteProjectWithTagsInternal(tx, projectID)
	}); err != nil {
		return errors.WrapIf(err, "failed to delete project from database")
	}

	if !removeFiles {
		if projectsDir, dirErr := s.GetProjectsDirectory(ctx); dirErr != nil {
			slog.WarnContext(ctx, "Failed to resolve projects directory for quarantine", "error", dirErr)
		} else if projects.IsSafeSubdirectory(projectsDir, proj.Path) && filepath.Clean(projectsDir) != filepath.Clean(proj.Path) {
			trashName := fmt.Sprintf("%s%s-%d", projects.ArcaneTrashPrefix, filepath.Base(proj.Path), time.Now().Unix())
			trashPath := filepath.Join(filepath.Dir(proj.Path), trashName)
			if err := acfs.Rename(ctx, filepath.Dir(proj.Path), "/"+filepath.Base(proj.Path), "/"+trashName); err != nil {
				slog.WarnContext(ctx, "Failed to quarantine project files", "path", proj.Path, "trashPath", trashPath, "error", err)
			} else {
				slog.InfoContext(ctx, "Project files quarantined successfully", "path", proj.Path, "trashPath", trashPath)
			}
		}
	}
	s.invalidateProjectCachesInternal(projectID)

	metadata := database.JSON{"action": "destroy", "projectID": projectID, "projectName": proj.Name, "removeFiles": removeFiles, "removeVolumes": removeVolumes}
	s.logProjectEventInternal(ctx, event.EventTypeProjectDelete, projectID, proj.Name, user, metadata, "could not log project destroy action")

	return nil
}

func (s *ProjectService) RedeployProject(ctx context.Context, projectID string, user common.User, options *projecttypes.DeployOptions) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	disabled := s.projectRedeployDisabledInternal(ctx, *proj)
	if disabled {
		return errors.New("arcane cannot redeploy itself; use the system upgrade flow (Settings -> Updates) instead")
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	if progressWriter == nil {
		progressWriter = io.Discard
	}

	credentials, cerr := s.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		slog.WarnContext(ctx, "failed to resolve registry credentials for redeploy pull", "error", cerr)
	}
	if err := s.PullProjectImages(ctx, projectID, progressWriter, user, credentials); err != nil {
		slog.WarnContext(ctx, "failed to pull project images", "error", err)
	}

	metadata := database.JSON{"action": "redeploy", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, event.EventTypeProjectDeploy, projectID, proj.Name, user, metadata, "could not log project redeploy action")

	return s.DeployProject(ctx, projectID, user, options)
}

func (s *ProjectService) projectRedeployDisabledInternal(ctx context.Context, proj Project) bool {
	containers, err := s.listGlobalComposeContainersInternal(ctx)
	if err != nil {
		slog.WarnContext(ctx, "could not list compose containers to check self-redeploy guard; skipping guard", "error", err)
		return false
	}

	containersByProject := groupComposeContainersByProjectInternal(containers)

	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()
	for _, containerSummary := range lookupProjectContainers(proj, containersByProject) {
		if labels.ShouldDisableArcaneServerRedeploy(containerSummary.Labels, containerSummary.ID, currentContainerID, currentContainerErr) {
			return true
		}
	}

	return false
}

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, user common.User, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj)
	if lerr != nil {
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	for _, img := range projects.PullableImageRefs(compProj) {
		if err := s.pullAndReconcileImageInternal(ctx, img, progressWriter, user, credentials); err != nil {
			return err
		}
	}

	return nil
}

func (s *ProjectService) BuildProjectServices(ctx context.Context, projectID string, options projecttypes.BuildOptions, progressWriter io.Writer, user *common.User) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	projectModel, _, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb)
	if derr != nil {
		return errors.WrapIff(derr, "failed to load compose project in %s", projectFromDb.Path)
	}

	defer s.eventService.BeginComposeSuppressionWindow(projectModel.Name)()

	return s.composeCoordinator.BuildServices(ctx, projectID, projectModel, options, progressWriter, s.composeImageOperationsInternal(user, nil))
}

// EnsureProjectImagesPresent checks all compose service images for the project and
// pulls based on service pull policy:
// - always/refresh: always pull
// - missing/if_not_present/default: pull only if local image is missing
// - never: never pull (fails early if image is missing locally)
func (s *ProjectService) EnsureProjectImagesPresent(ctx context.Context, projectID string, progressWriter io.Writer, user common.User, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj)
	if lerr != nil {
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	return s.composeCoordinator.EnsureImagesPresent(ctx, compProj, progressWriter, s.composeImageOperationsInternal(&user, credentials))
}

func (s *ProjectService) restoreProjectStatusAfterFailedDeployInternal(ctx context.Context, projectID string) {
	services, err := s.GetProjectServices(ctx, projectID)
	if err == nil {
		serviceCount, runningCount := getServiceCounts(services)
		status := calculateProjectStatus(services)
		updateErr := s.db.WithContext(ctx).Model(&Project{}).Where("id = ?", projectID).Updates(map[string]any{
			"status":        status,
			"service_count": serviceCount,
			"running_count": runningCount,
			"updated_at":    time.Now(),
		}).Error
		if updateErr == nil {
			return
		}
		slog.WarnContext(ctx, "failed to restore project status after deploy failure", "projectID", projectID, "error", updateErr)
	} else {
		slog.WarnContext(ctx, "failed to inspect project services after deploy failure", "projectID", projectID, "error", err)
	}

	if updateErr := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusStopped); updateErr != nil {
		slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectID", projectID, "error", updateErr)
	}
}

func (s *ProjectService) RestartProject(ctx context.Context, projectID string, services []string, user common.User) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRestarting); err != nil {
		return errors.WrapIf(err, "failed to update project status to restarting")
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	defer s.eventService.BeginComposeSuppressionWindow(compProj.Name)()

	if err := projects.ComposeRestart(ctx, compProj, services); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, ProjectStatusRunning)
		return errors.WrapIf(err, "failed to restart project")
	}

	metadata := database.JSON{
		"action":      "restart",
		"projectID":   projectID,
		"projectName": proj.Name,
	}
	if len(services) > 0 {
		metadata["services"] = append([]string(nil), services...)
	}
	s.logProjectEventInternal(ctx, event.EventTypeProjectStart, projectID, proj.Name, user, metadata, "could not log project restart action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, ProjectStatusRunning)
}

func (s *ProjectService) composeImageOperationsInternal(user *common.User, credentials []containerregistry.Credential) projecttypes.ComposeImageOperations {
	operations := projecttypes.ComposeImageOperations{
		Pull: func(ctx context.Context, image string, progress io.Writer) error {
			actor := common.SystemUser
			if user != nil {
				actor = *user
			}
			return s.pullAndReconcileImageInternal(ctx, image, progress, actor, credentials)
		},
	}
	if s.imageService != nil {
		operations.Exists = s.imageService.ImageExistsLocally
		operations.LastTagged = s.imageService.ImageLastTagTime
	}
	if s.buildService != nil {
		operations.BuildProvider = s.buildService.BuildSettings().BuildProvider
		operations.Build = func(ctx context.Context, request buildtypes.BuildRequest, progress io.Writer, service string) error {
			_, err := s.buildService.BuildImage(ctx, types.LocalDockerEnvironmentID, request, progress, service, user)
			return err
		}
	}
	return operations
}
