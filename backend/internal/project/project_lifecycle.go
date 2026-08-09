package project

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emperror.dev/errors"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	"github.com/getarcaneapp/arcane/types/v2"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/client"
	buildtypes "go.getarcane.app/builds/types"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"
	"gorm.io/gorm"
)

type ProjectBuildOptions struct {
	Services []string
	Provider string
	Push     *bool
	Load     *bool
}

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

func (s *ProjectService) composePullSelectedServicesInternal(
	ctx context.Context,
	compProj *composetypes.Project,
	servicesToUpdate []string,
	user models.User,
	credentials []containerregistry.Credential,
) error {
	if compProj == nil {
		return nil
	}

	imageRefsToPull := projects.SelectedImageRefs(compProj, servicesToUpdate)
	if len(imageRefsToPull) == 0 {
		return nil
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	for _, imageRef := range imageRefsToPull {
		if err := s.pullAndReconcileImageInternal(ctx, imageRef, progressWriter, user, credentials); err != nil {
			return err
		}
	}

	return nil
}

func (s *ProjectService) pullAndReconcileImageInternal(
	ctx context.Context,
	imageRef string,
	progressWriter io.Writer,
	user models.User,
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

func (s *ProjectService) UpdateProjectServices(ctx context.Context, projectID string, servicesToUpdate []string, user models.User) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	previousStatus := projectFromDb.Status

	// 1. Load project
	compProj, _, err := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if err != nil {
		return errors.WrapIf(err, "failed to load compose project")
	}

	// 2. Set status to deploying/restarting
	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusDeploying); err != nil {
		return err
	}

	credentials, err := s.ResolveRegistryCredentials(ctx)
	if err != nil {
		if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to restore project status after credential lookup failure", "projectID", projectID, "error", statusErr)
		}
		return errors.WrapIf(err, "resolve registry credentials")
	}

	// 3. Pull images for specific services
	if err := s.composePullSelectedServicesInternal(ctx, compProj, servicesToUpdate, user, credentials); err != nil {
		if statusErr := s.updateProjectStatusInternal(ctx, projectID, previousStatus); statusErr != nil {
			slog.ErrorContext(ctx, "UpdateProjectServices: failed to restore project status after compose pull failure", "projectID", projectID, "error", statusErr)
		}
		return errors.WrapIf(err, "pull updated service images")
	}

	// 4. Stop specific services
	if err := composeStopProjectServicesInternal(ctx, compProj, servicesToUpdate); err != nil {
		slog.WarnContext(ctx, "compose stop failed, continuing", "error", err)
	}

	// 5. Up specific services
	if err := composeUpProjectServicesInternal(ctx, compProj, servicesToUpdate, false, true, false, s.composeRegistryAuthConfigsInternal(ctx), s.deployWaitTimeoutInternal()); err != nil {
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
		return errors.WrapIf(err, "failed to up services")
	}

	// 6. Finalize status
	if err := s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning); err != nil {
		return err
	}

	metadata := models.JSON{
		"action":      "update_services",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
		"services":    append([]string(nil), servicesToUpdate...),
	}
	s.logProjectEventInternal(ctx, models.EventTypeProjectUpdate, projectID, projectFromDb.Name, user, metadata, "could not log project service update action")

	return nil
}

func ensureProjectMutableInternal(proj *models.Project) error {
	if proj != nil && proj.IsArchived {
		return common.Classify(common.ErrProjectArchived, errors.New("project is archived and must be unarchived before this action"))
	}
	return nil
}

func isProjectArchiveBlockedInternal(proj *models.Project) bool {
	if proj == nil {
		return false
	}
	if proj.RunningCount > 0 {
		return true
	}
	switch proj.Status {
	case models.ProjectStatusRunning, models.ProjectStatusPartiallyRunning, models.ProjectStatusDeploying, models.ProjectStatusRestarting:
		return true
	case models.ProjectStatusStopped, models.ProjectStatusUnknown, models.ProjectStatusStopping:
		return false
	default:
		return false
	}
}

func (s *ProjectService) ArchiveProject(ctx context.Context, projectID string, user models.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if proj.IsArchived {
		return nil
	}
	if isProjectArchiveBlockedInternal(proj) {
		return common.Classify(common.ErrProjectMustBeStopped, errors.New("project must be stopped before archiving"))
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": true,
		"archived_at": now,
	}).Error; err != nil {
		return errors.WrapIf(err, "failed to archive project")
	}

	metadata := models.JSON{"action": "archived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, models.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project archive action")

	return nil
}

func (s *ProjectService) UnarchiveProject(ctx context.Context, projectID string, user models.User) error {
	proj, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return err
	}
	if !proj.IsArchived {
		return nil
	}

	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{
		"is_archived": false,
		"archived_at": gorm.Expr("NULL"),
	}).Error; err != nil {
		return errors.WrapIf(err, "failed to unarchive project")
	}

	metadata := models.JSON{"action": "unarchived", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, models.EventTypeProjectUpdate, projectID, proj.Name, user, metadata, "could not log project unarchive action")

	return nil
}

// resolveRemoveOrphansInternal decides whether compose up should remove orphan containers.
// GitOps-managed projects always remove orphans so the running stack matches the
// tracked compose file. Non-GitOps callers may opt in per request via DeployOptions.
// deployWaitTimeoutInternal resolves how long compose up waits for depends_on
// health/completion conditions, from the deployWaitTimeout setting.
func (s *ProjectService) deployWaitTimeoutInternal() time.Duration {
	return timeouts.GetDuration(s.settingsService.GetSettingsConfig().DeployWaitTimeout.AsInt(), timeouts.DefaultDeployWait)
}

func resolveRemoveOrphansInternal(gitOpsManaged bool, options *project.DeployOptions) bool {
	return gitOpsManaged || (options != nil && options.RemoveOrphans)
}

func (s *ProjectService) DeployProject(ctx context.Context, projectID string, user models.User, options *project.DeployOptions) error {
	projectFromDb, err := s.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil {
		return errors.WrapIf(err, "failed to get project")
	}
	if err := ensureProjectMutableInternal(projectFromDb); err != nil {
		return err
	}

	resolvedPullPolicy := ""
	forceRecreate := false
	recreateVolumes := false
	if options != nil {
		resolvedPullPolicy = projects.NormalizeDeployPullPolicy(options.PullPolicy)
		forceRecreate = options.ForceRecreate
		recreateVolumes = options.RecreateVolumes
	}
	if resolvedPullPolicy == "" {
		resolvedPullPolicy = projects.NormalizeDeployPullPolicy(s.settingsService.GetStringSetting(ctx, "defaultDeployPullPolicy", "missing"))
	}
	if resolvedPullPolicy == "" {
		resolvedPullPolicy = "missing"
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusDeploying); err != nil {
		return errors.WrapIf(err, "failed to update project status to deploying")
	}

	// Run any configured pre-deploy lifecycle hook before loading the compose
	// project so hooks can produce files that compose then consumes (e.g.
	// `sops -d secrets.enc.env > .env.runtime` for an `env_file: .env.runtime`
	// service). A failed hook aborts the deploy.
	if s.lifecycleService != nil {
		if lerr := s.lifecycleService.RunPreDeploy(ctx, projectFromDb, user); lerr != nil {
			s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
			return errors.WrapIf(lerr, "pre-deploy lifecycle hook failed")
		}
	}

	projectModel, _, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if derr != nil {
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
		return errors.WrapIff(derr, "failed to load compose project in %s", projectFromDb.Path)
	}

	credentials, cerr := s.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
		return errors.WrapIf(cerr, "resolve registry credentials")
	}

	progressWriter, _ := ctx.Value(dockerutil.ProgressWriterKey{}).(io.Writer)
	if perr := s.prepareProjectImagesForDeploy(ctx, projectID, projectModel, progressWriter, credentials, &user, resolvedPullPolicy); perr != nil {
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)
		return errors.WrapIf(perr, "failed to prepare project images for deploy")
	}

	gitOpsManaged := projectFromDb.GitOpsManagedBy != nil && *projectFromDb.GitOpsManagedBy != ""
	removeOrphans := resolveRemoveOrphansInternal(gitOpsManaged, options)

	slog.Info("starting compose up with health check support", "projectID", projectID, "projectName", projectModel.Name, "services", len(projectModel.Services), "removeOrphans", removeOrphans)
	// Health/progress streaming (if any) is handled inside projects.ComposeUp via ctx.
	if err := projects.ComposeUp(ctx, projectModel, nil, removeOrphans, forceRecreate, recreateVolumes, s.composeRegistryAuthConfigsInternal(ctx), s.deployWaitTimeoutInternal()); err != nil {
		slog.Error("compose up failed", "projectName", projectModel.Name, "projectID", projectID, "error", err)
		if containers, psErr := s.GetProjectServices(ctx, projectID); psErr == nil {
			slog.Info("containers after failed deploy", "projectID", projectID, "containers", containers)
		}
		s.restoreProjectStatusAfterFailedDeployInternal(ctx, projectID)

		// Provide more helpful error messages
		errMsg := err.Error()
		if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "context deadline exceeded") {
			return errors.WrapIf(err, "deployment timed out waiting for services - long-running 'service_healthy'/'service_completed_successfully' dependencies may need a higher Deploy Wait Timeout setting, and 'service_healthy' requires a healthcheck")
		}
		return errors.WrapIf(err, "failed to deploy project")
	}
	slog.Info("compose up completed successfully", "projectID", projectID, "projectName", projectModel.Name)

	metadata := models.JSON{"action": "deploy", "projectID": projectID, "projectName": projectModel.Name}
	s.logProjectEventInternal(ctx, models.EventTypeProjectDeploy, projectID, projectModel.Name, user, metadata, "could not log project deployment action")

	err = s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning)
	if err != nil {
		slog.Error("failed to update project status and counts after deploy", "projectID", projectID, "error", err)
	}
	return err
}

func (s *ProjectService) DownProject(ctx context.Context, projectID string, user models.User) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	proj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusStopped); err != nil {
		return errors.WrapIf(err, "failed to update project status to stopping")
	}

	if err := projects.ComposeDown(ctx, proj, false); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return errors.WrapIf(err, "failed to bring down project")
	}

	metadata := models.JSON{
		"action":      "down",
		"projectID":   projectID,
		"projectName": projectFromDb.Name,
	}
	s.logProjectEventInternal(ctx, models.EventTypeProjectStop, projectID, projectFromDb.Name, user, metadata, "could not log project down action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusStopped)
}

// CreateProject creates a project's directory, files, and DB row. When
// allowNameSuffix is true a directory-name collision is resolved by appending
// "-N" (the interactive default). When false a collision returns
// projects.ErrProjectDirExists (wrapped) so GitOps creates fail loudly instead of
// minting runaway "-N" duplicate projects on a broken binding.
func (s *ProjectService) CreateProject(ctx context.Context, name, composeContent string, envContent *string, manifest project.CreateProjectWorkspaceManifest, uploads map[int][]byte, user models.User, allowNameSuffixOptions ...bool) (*models.Project, error) {
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
		projectPath, folderName, err = projects.CreateUniqueDir(projectsDirectory, basePath, name, utils.DirPerm)
	} else {
		projectPath, folderName, err = projects.CreateExactDir(projectsDirectory, basePath, name, utils.DirPerm)
	}
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create project directory")
	}

	proj := &models.Project{
		Name:         name,
		DirName:      &folderName,
		Path:         projectPath,
		Status:       models.ProjectStatusStopped,
		ServiceCount: 0,
		RunningCount: 0,
	}

	if err := projects.ApplyProjectWorkspaceChanges(projectPath, manifest.FileChanges, uploads, projects.ProjectWorkspaceApplyOptions{
		MaxDepth:         s.config.ProjectWorkspaceMaxDepth,
		MaxEntries:       s.config.ProjectWorkspaceMaxEntries,
		MaxFileSizeBytes: workspacepkg.MaxFileSizeBytes(s.config.ProjectWorkspaceMaxFileSizeMB),
		SkipDirectories:  s.config.ProjectScanSkipDirs,
		ComposeFileName:  projects.DefaultComposeFileName,
	}); err != nil {
		_ = os.RemoveAll(projectPath)
		return nil, wrapProjectWorkspaceErrorInternal(err)
	}

	// GitOps-originated creates (allowNameSuffix=false) tolerate not-yet-supplied
	// ${VAR} references the same way single-file git sync updates do; interactive
	// creates (allowNameSuffix=true) stay strict.
	if err := validateComposeContentForUpdate(ctx, projectsDirectory, projectPath, name, composeContent, envContent, nil, "", !allowNameSuffix); err != nil {
		_ = os.RemoveAll(projectPath)
		return nil, errors.WrapIf(err, "invalid compose file")
	}

	if err := projects.WriteProjectFiles(projectsDirectory, projectPath, composeContent, envContent); err != nil {
		// Best-effort cleanup to restore pre-transaction behavior.
		_ = os.RemoveAll(projectPath)
		return nil, errors.WrapIf(err, "failed to save project files")
	}

	if err := s.db.WithContext(ctx).Create(proj).Error; err != nil {
		_ = os.RemoveAll(projectPath)
		return nil, errors.WrapIf(err, "failed to create project")
	}
	s.refreshComposeProjectNameInternal(ctx, proj)
	s.refreshProjectImageRefsInternal(ctx, proj)

	metadata := models.JSON{"action": "create", "projectID": proj.ID, "projectName": proj.Name, "path": projectPath}
	s.logProjectEventInternal(ctx, models.EventTypeProjectCreate, proj.ID, proj.Name, user, metadata, "could not log project creation")

	return proj, nil
}

func (s *ProjectService) DestroyProject(ctx context.Context, projectID string, removeFiles bool, removeVolumes bool, user models.User) error {
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

	if err := s.DownProject(ctx, projectID, models.SystemUser); err != nil {
		slog.WarnContext(ctx, "failed to bring down project", "error", err)
	}

	if removeVolumes {
		if compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil); lerr == nil {
			if derr := projects.ComposeDown(ctx, compProj, true); derr != nil {
				slog.WarnContext(ctx, "failed to remove volumes", "error", derr)
			}
		} else {
			slog.WarnContext(ctx, "failed to load compose project for volume removal", "error", lerr)
		}
	}

	if removeFiles {
		slog.DebugContext(ctx, "Removing project files", "path", proj.Path)
		if err := os.RemoveAll(proj.Path); err != nil {
			slog.ErrorContext(ctx, "Failed to remove project files", "path", proj.Path, "error", err)
			return errors.WrapIf(err, "failed to remove project files")
		}
		slog.InfoContext(ctx, "Project files removed successfully", "path", proj.Path)
	}

	if err := s.db.WithContext(ctx).Delete(proj).Error; err != nil {
		return errors.WrapIf(err, "failed to delete project from database")
	}

	if !removeFiles {
		if projectsDir, dirErr := s.GetProjectsDirectory(ctx); dirErr != nil {
			slog.WarnContext(ctx, "Failed to resolve projects directory for quarantine", "error", dirErr)
		} else if projects.IsSafeSubdirectory(projectsDir, proj.Path) && filepath.Clean(projectsDir) != filepath.Clean(proj.Path) {
			trashPath := filepath.Join(filepath.Dir(proj.Path), fmt.Sprintf("%s%s-%d", projects.ArcaneTrashPrefix, filepath.Base(proj.Path), time.Now().Unix()))
			if err := os.Rename(proj.Path, trashPath); err != nil {
				slog.WarnContext(ctx, "Failed to quarantine project files", "path", proj.Path, "trashPath", trashPath, "error", err)
			} else {
				slog.InfoContext(ctx, "Project files quarantined successfully", "path", proj.Path, "trashPath", trashPath)
			}
		}
	}
	s.parsedCompose.invalidate(projectID)

	metadata := models.JSON{"action": "destroy", "projectID": projectID, "projectName": proj.Name, "removeFiles": removeFiles, "removeVolumes": removeVolumes}
	s.logProjectEventInternal(ctx, models.EventTypeProjectDelete, projectID, proj.Name, user, metadata, "could not log project destroy action")

	return nil
}

func (s *ProjectService) RedeployProject(ctx context.Context, projectID string, user models.User, options *project.DeployOptions) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	disabled := projectRedeployDisabledInternal(ctx, *proj)
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

	metadata := models.JSON{"action": "redeploy", "projectID": projectID, "projectName": proj.Name}
	s.logProjectEventInternal(ctx, models.EventTypeProjectDeploy, projectID, proj.Name, user, metadata, "could not log project redeploy action")

	return s.DeployProject(ctx, projectID, user, options)
}

func projectRedeployDisabledInternal(ctx context.Context, proj models.Project) bool {
	containers, err := projects.ListGlobalComposeContainers(ctx)
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

func (s *ProjectService) PullProjectImages(ctx context.Context, projectID string, progressWriter io.Writer, user models.User, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if lerr != nil {
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	images := map[string]struct{}{}
	for _, svc := range compProj.Services {
		if svc.Build == nil {
			if img := strings.TrimSpace(svc.Image); img != "" {
				images[img] = struct{}{}
			}
		}
		// pre_start hook images and type:image volume sources are pulled from a
		// registry even for build services — they cannot be built.
		for _, img := range api.GetDependentImages(svc, compProj.Name) {
			images[img] = struct{}{}
		}
		for _, vol := range svc.Volumes {
			if vol.Type == composetypes.VolumeTypeImage && strings.TrimSpace(vol.Source) != "" {
				images[strings.TrimSpace(vol.Source)] = struct{}{}
			}
		}
	}

	for img := range images {
		if err := s.pullAndReconcileImageInternal(ctx, img, progressWriter, user, credentials); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProjectService) BuildProjectServices(ctx context.Context, projectID string, options ProjectBuildOptions, progressWriter io.Writer, user *models.User) error {
	projectFromDb, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	projectModel, _, derr := s.loadComposeProjectForProjectInternal(ctx, projectFromDb, nil)
	if derr != nil {
		return errors.WrapIff(derr, "failed to load compose project in %s", projectFromDb.Path)
	}

	return s.buildProjectServicesInternal(ctx, projectID, projectModel, options, progressWriter, user)
}

// EnsureProjectImagesPresent checks all compose service images for the project and
// pulls based on service pull policy:
// - always/refresh: always pull
// - missing/if_not_present/default: pull only if local image is missing
// - never: never pull (fails early if image is missing locally)
func (s *ProjectService) EnsureProjectImagesPresent(ctx context.Context, projectID string, progressWriter io.Writer, user models.User, credentials []containerregistry.Credential) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	compProj, _, lerr := s.loadComposeProjectForProjectInternal(ctx, proj, nil)
	if lerr != nil {
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	pullPlan := projects.BuildImagePullPlan(compProj)

	return s.ensureImagesPresent(ctx, pullPlan, progressWriter, credentials, user)
}

func (s *ProjectService) ensureImagesPresent(ctx context.Context, pullPlan map[string]projects.ImagePullMode, progressWriter io.Writer, credentials []containerregistry.Credential, user models.User) error {
	for img, mode := range pullPlan {
		exists, ierr := s.imageService.ImageExistsLocally(ctx, img)
		if ierr != nil && mode != projects.ImagePullModeAlways {
			slog.WarnContext(ctx, "failed to check local image existence", "image", img, "error", ierr)
			// Non-fatal: attempt to pull to be safe
		}

		if mode == projects.ImagePullModeNever {
			if ierr != nil {
				slog.WarnContext(ctx, "pull_policy is 'never' but image presence check failed; continuing without pull", "image", img, "error", ierr)
				continue
			}
			if !exists {
				return errors.Errorf("image %s is not available locally and pull_policy is 'never'", img)
			}
			slog.DebugContext(ctx, "pull_policy is 'never'; using local image without pull", "image", img)
			continue
		}

		if mode == projects.ImagePullModeIfMissing && exists {
			slog.DebugContext(ctx, "image already present locally; skipping pull", "image", img)
			continue
		}

		if err := s.pullAndReconcileImageInternal(ctx, img, progressWriter, user, credentials); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProjectService) prepareProjectImagesForDeploy(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	progressWriter io.Writer,
	credentials []containerregistry.Credential,
	user *models.User,
	pullPolicyOverride string,
) error {
	if project == nil {
		return nil
	}

	for name, svc := range project.Services {
		svc, imageName, updated := projects.PrepareDeployServiceConfig(projectID, project.Name, name, svc)
		if updated {
			project.Services[name] = svc
		}

		if imageName == "" {
			continue
		}

		decision := projects.DecideDeployImageAction(svc, pullPolicyOverride)
		if updated {
			decision = projects.DeployImageDecision{Build: true}
		}
		if err := s.ensureDeployServiceImageReady(ctx, projectID, project, name, svc, imageName, decision, progressWriter, credentials, user); err != nil {
			return err
		}

		// pre_start hook images and type:image volume sources cannot be built:
		// they follow the service's pull decision minus the build/fallback paths.
		dependentDecision := projects.DeployImageDecision{PullIfMissing: true}
		switch {
		case decision.RequireLocalOnly:
			dependentDecision = projects.DeployImageDecision{RequireLocalOnly: true}
		case decision.PullAlways:
			dependentDecision = projects.DeployImageDecision{PullAlways: true}
		}
		dependentImages := api.GetDependentImages(svc, project.Name)
		for _, vol := range svc.Volumes {
			if vol.Type == composetypes.VolumeTypeImage && strings.TrimSpace(vol.Source) != "" {
				dependentImages = append(dependentImages, strings.TrimSpace(vol.Source))
			}
		}
		for _, img := range dependentImages {
			if err := s.ensureDeployServiceImageReady(ctx, projectID, project, name, svc, img, dependentDecision, progressWriter, credentials, user); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *ProjectService) ensureDeployServiceImageReady(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	imageName string,
	decision projects.DeployImageDecision,
	progressWriter io.Writer,
	credentials []containerregistry.Credential,
	user *models.User,
) error {
	if decision.Build {
		return s.buildServiceImageForDeploy(ctx, projectID, project, serviceName, svc, progressWriter, user)
	}

	exists, err := s.imageService.ImageExistsLocally(ctx, imageName)
	if err != nil {
		slog.WarnContext(ctx, "failed to check local image existence", "image", imageName, "error", err)
	}

	if decision.RequireLocalOnly {
		if !exists {
			return errors.Errorf("image %s is not available locally and pull_policy is set to never", imageName)
		}
		return nil
	}

	if !projects.ShouldPullDeployImage(decision, exists) {
		return nil
	}

	err = s.pullAndReconcileImageInternal(ctx, imageName, progressWriter, models.SystemUser, credentials)
	if err == nil {
		return nil
	}
	if svc.Build != nil && decision.FallbackBuildOnPullFail {
		slog.WarnContext(ctx, "image pull failed, falling back to build", "service", serviceName, "image", imageName, "error", err)
		return s.buildServiceImageForDeploy(ctx, projectID, project, serviceName, svc, progressWriter, user)
	}
	return errors.WrapIff(err, "failed to pull image %s", imageName)
}

func (s *ProjectService) buildServiceImageForDeploy(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	progressWriter io.Writer,
	user *models.User,
) error {
	if s.buildService == nil {
		return errors.Errorf("build service not available for service %s", serviceName)
	}

	buildReq, updatedSvc, updated, err := s.prepareServiceBuildRequest(ctx, projectID, project, serviceName, svc, ProjectBuildOptions{})
	if err != nil {
		return err
	}
	if updated {
		project.Services[serviceName] = updatedSvc
	}

	if _, err := s.buildService.BuildImage(ctx, types.LocalDockerEnvironmentID, buildReq, progressWriter, serviceName, user); err != nil {
		return err
	}

	return nil
}

func (s *ProjectService) resolveEffectiveBuildProvider(override string) string {
	provider := strings.ToLower(strings.TrimSpace(override))
	if provider != "" {
		return provider
	}

	if s.buildService != nil {
		provider = strings.ToLower(strings.TrimSpace(s.buildService.BuildSettings().BuildProvider))
	}

	if provider == "" {
		provider = "local"
	}

	return provider
}

func (s *ProjectService) prepareServiceBuildRequest(
	ctx context.Context,
	projectID string,
	project *composetypes.Project,
	serviceName string,
	svc composetypes.ServiceConfig,
	options ProjectBuildOptions,
) (buildtypes.BuildRequest, composetypes.ServiceConfig, bool, error) {
	_ = ctx
	imageName, updatedSvc, updated := projects.EnsureServiceImage(projectID, project.Name, serviceName, svc)
	effectiveProvider := s.resolveEffectiveBuildProvider(options.Provider)

	if updated && effectiveProvider == "depot" {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s must define an image when using depot build provider", serviceName)
	}
	if updated && options.Push != nil && *options.Push {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s must define an image when push is enabled", serviceName)
	}

	// The build context (and any absolute Dockerfile path) is read locally by
	// Arcane — both the docker provider (`archive.TarWithOptions`) and the
	// buildkit provider (`SolveOpt.LocalDirs`) stream the directory contents
	// to the daemon from the Arcane process's own filesystem. It must
	// therefore stay as a container path; translating it to the host path
	// (which is what bind mount sources need) makes `os.Stat` fail because
	// the host path doesn't exist inside the Arcane container. See #2314.
	// For that reason the build context dir is deliberately left untranslated.
	contextDir, err := projects.ResolveBuildContext(project.WorkingDir, updatedSvc, serviceName)
	if err != nil {
		return buildtypes.BuildRequest{}, updatedSvc, updated, err
	}

	dockerfileInline := updatedSvc.Build.DockerfileInline
	if strings.TrimSpace(updatedSvc.Build.Dockerfile) != "" && strings.TrimSpace(dockerfileInline) != "" {
		return buildtypes.BuildRequest{}, updatedSvc, updated, errors.Errorf("service %s cannot define both dockerfile and dockerfile_inline", serviceName)
	}

	dockerfilePath := ""
	if strings.TrimSpace(dockerfileInline) == "" {
		dockerfilePath = projects.ResolveDockerfilePath(updatedSvc)
	}

	buildReq := buildtypes.BuildRequest{
		ContextDir:       contextDir,
		Dockerfile:       dockerfilePath,
		DockerfileInline: dockerfileInline,
		Tags:             projects.MergeBuildTags(imageName, updatedSvc.Build.Tags),
		Target:           strings.TrimSpace(updatedSvc.Build.Target),
		BuildArgs:        projects.BuildArgsFromCompose(updatedSvc.Build.Args),
		Labels:           projects.LabelsFromCompose(updatedSvc.Build.Labels),
		CacheFrom:        append([]string(nil), updatedSvc.Build.CacheFrom...),
		CacheTo:          append([]string(nil), updatedSvc.Build.CacheTo...),
		NoCache:          updatedSvc.Build.NoCache,
		Pull:             updatedSvc.Build.Pull,
		Network:          strings.TrimSpace(updatedSvc.Build.Network),
		Isolation:        strings.TrimSpace(updatedSvc.Build.Isolation),
		ShmSize:          int64(updatedSvc.Build.ShmSize),
		Ulimits:          projects.UlimitsFromCompose(updatedSvc.Build.Ulimits),
		Entitlements: append(
			[]string(nil),
			updatedSvc.Build.Entitlements...,
		),
		Privileged: updatedSvc.Build.Privileged,
		ExtraHosts: updatedSvc.Build.ExtraHosts.AsList(":"),
		Platforms:  projects.BuildPlatformsFromCompose(updatedSvc),
		Provider:   effectiveProvider,
	}
	if options.Push != nil {
		buildReq.Push = *options.Push
	}
	if options.Load != nil {
		buildReq.Load = *options.Load
	}

	return buildReq, updatedSvc, updated, nil
}

func (s *ProjectService) restoreProjectStatusAfterFailedDeployInternal(ctx context.Context, projectID string) {
	services, err := s.GetProjectServices(ctx, projectID)
	if err == nil {
		serviceCount, runningCount := getServiceCounts(services)
		status := calculateProjectStatus(services)
		updateErr := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", projectID).Updates(map[string]any{
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

	if updateErr := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusStopped); updateErr != nil {
		slog.WarnContext(ctx, "failed to set stopped status after deploy failure", "projectID", projectID, "error", updateErr)
	}
}

func (s *ProjectService) buildProjectServicesInternal(ctx context.Context, projectID string, project *composetypes.Project, options ProjectBuildOptions, progressWriter io.Writer, user *models.User) error {
	if s.buildService == nil {
		return nil
	}
	if project == nil {
		return nil
	}

	selected := projects.NormalizeBuildSelections(options.Services)

	buildCount := 0
	for name, svc := range project.Services {
		if svc.Build == nil {
			continue
		}
		if !projects.ServiceSelected(selected, name) {
			continue
		}

		buildReq, updatedSvc, updated, err := s.prepareServiceBuildRequest(ctx, projectID, project, name, svc, options)
		if err != nil {
			return err
		}
		if updated {
			project.Services[name] = updatedSvc
		}

		buildCount++
		if _, err := s.buildService.BuildImage(ctx, types.LocalDockerEnvironmentID, buildReq, progressWriter, name, user); err != nil {
			return err
		}
	}

	if buildCount == 0 && len(selected) > 0 {
		return errors.Errorf("no build-enabled services matched: %s", strings.Join(options.Services, ", "))
	}

	return nil
}

func (s *ProjectService) RestartProject(ctx context.Context, projectID string, services []string, user models.User) error {
	proj, err := s.getMutableProjectInternal(ctx, projectID)
	if err != nil {
		return err
	}

	if err := s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRestarting); err != nil {
		return errors.WrapIf(err, "failed to update project status to restarting")
	}

	// Get configured projects directory from settings
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory := getProjectsDirectoryOrDefaultInternal(ctx, cfg)

	var dockerClient *client.Client
	if s.dockerService != nil {
		dockerClient, _ = s.dockerService.GetClient(ctx)
	}
	pathMapper := projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"),
		"/app/data/projects",
		dockerClient,
	)

	compProj, _, lerr := projects.LoadComposeProjectFromDir(ctx, proj.Path, projects.NormalizeProjectName(proj.Name), projectsDirectory, utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false), pathMapper)
	if lerr != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return errors.WrapIf(lerr, "failed to load compose project")
	}

	if err := projects.ComposeRestart(ctx, compProj, services); err != nil {
		_ = s.updateProjectStatusInternal(ctx, projectID, models.ProjectStatusRunning)
		return errors.WrapIf(err, "failed to restart project")
	}

	metadata := models.JSON{
		"action":      "restart",
		"projectID":   projectID,
		"projectName": proj.Name,
	}
	if len(services) > 0 {
		metadata["services"] = append([]string(nil), services...)
	}
	s.logProjectEventInternal(ctx, models.EventTypeProjectStart, projectID, proj.Name, user, metadata, "could not log project restart action")

	return s.updateProjectStatusandCountsInternal(ctx, projectID, models.ProjectStatusRunning)
}
