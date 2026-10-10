// Package sync applies deployment GitOps syncs: it clones the source, stages
// and promotes project files, and redeploys the linked project or stack.
package sync

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/gitops"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// reservedRootEnvFiles are the project-root env files the override merge owns; git never writes them raw.
var reservedRootEnvFiles = []string{projects.EffectiveEnvFileName, projects.GitSourceEnvFileName, projects.OverrideEnvFileName, projects.GlobalEnvFileName}

type (
	// Service runs deployment syncs. Shared sync limits, error events and job
	// registration stay with the parent and arrive as callbacks.
	Service struct {
		db             *database.DB
		repoService    *gitrepo.GitRepositoryService
		projectService *projectpkg.ProjectService
		swarmService   *swarm.SwarmService
		eventService   *event.EventService
		limits         func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64)
		logError       func(context.Context, *projectpkg.GitOpsSync, user.Actor, string)
		unregisterJob  func(context.Context, string)
	}

	// syncRevision is the recovery data a run records so a retry replays the same source revision.
	syncRevision struct {
		Commit string `json:"commit"`
	}

	// preparedSyncSource is the cloned and validated source a sync applies.
	preparedSyncSource struct {
		repoPath         string
		commitHash       string
		composeContent   string
		envContent       *string
		overrideContent  *string
		overrideFileName string
		// replay marks a retry of a pinned revision, which redeploys even when the files are unchanged.
		replay bool
	}

	// stagedDirectorySync holds the fully prepared directory-sync result before it
	// is promoted into the live project path.
	stagedDirectorySync struct {
		stagePath string
		// stageLogical is stagePath relative to the projects directory, the root every staging mutation is confined to.
		stageLogical    string
		projectsDir     string
		composeFileName string
		project         *projectpkg.Project
		// syncFiles are the repo files written raw; syncedFiles are their paths.
		syncFiles   []projects.SyncFile
		syncedFiles []string
		// oldSyncedFiles are the paths the previous sync tracked.
		oldSyncedFiles []string
		// gitEnvContent is the repo's project-root .env, routed through the env merge.
		gitEnvContent   *string
		serviceCount    int
		contentsChanged bool
		// backupScope names every live path promotion may create, rewrite or delete.
		backupScope projects.ProjectUpdateBackupScope
	}
)

func New(
	db *database.DB,
	repoService *gitrepo.GitRepositoryService,
	projectService *projectpkg.ProjectService,
	swarmService *swarm.SwarmService,
	eventService *event.EventService,
	limits func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64),
	logError func(context.Context, *projectpkg.GitOpsSync, user.Actor, string),
	unregisterJob func(context.Context, string),
) *Service {
	return &Service{
		db:             db,
		repoService:    repoService,
		projectService: projectService,
		swarmService:   swarmService,
		eventService:   eventService,
		limits:         limits,
		logError:       logError,
		unregisterJob:  unregisterJob,
	}
}

// Run clones the sync source and applies it to its project or Swarm stack. A retry replays the
// revision its run pinned, and redeploys it unless the record already confirms that revision.
func (s *Service) Run(ctx context.Context, sync *projectpkg.GitOpsSync, actor user.Actor, result *gitops.SyncResult) (*gitops.SyncResult, error) {
	pinned := ""
	if run, ok := jobcontext.Run(ctx); ok {
		index := slices.IndexFunc(run.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
			return target.ID == sync.ID && len(target.RecoveryData) > 0
		})
		var revision syncRevision
		if index >= 0 {
			if err := json.Unmarshal(run.Outcome.Targets[index].RecoveryData, &revision); err != nil {
				return result, err
			}
		}
		pinned = revision.Commit
	}
	if pinned != "" && kit.FromPtr(sync.LastSyncCommit) == pinned && kit.FromPtr(sync.LastSyncStatus) == "success" {
		result.Success = true
		result.Message = "Revision " + pinned + " is already synced"
		return result, nil
	}

	source, err := s.prepareSyncSource(ctx, sync, result, actor, pinned)
	if source != nil {
		defer s.repoService.Discard(ctx, source.repoPath)
	}
	if err != nil {
		return result, err
	}

	var syncFiles []projects.SyncFile
	if sync.SyncDirectory {
		if syncFiles, err = s.walkAndParseSyncDirectory(ctx, sync, source.repoPath); err != nil {
			return result, s.failSync(ctx, result, sync, actor, "Failed to walk directory", err)
		}
	}
	var project *projectpkg.Project
	var syncedFiles []string
	changed := false
	switch {
	case sync.TargetType == "swarm_stack":
		if s.swarmService == nil {
			return result, s.failSync(ctx, result, sync, actor, "Swarm service is unavailable", errors.New("swarm service is unavailable"))
		}
		swarmFiles := make([]swarmtypes.SyncFile, len(syncFiles))
		for i, file := range syncFiles {
			swarmFiles[i] = swarmtypes.SyncFile{RelativePath: file.RelativePath, Content: file.Content}
			syncedFiles = append(syncedFiles, file.RelativePath)
		}
		// Swarm tasks pull with only the auth embedded in the service spec, so registry auth is always resolved.
		request := swarmtypes.StackDeployRequest{
			Name:             sync.ProjectName,
			ComposeContent:   source.composeContent,
			OverrideContent:  kit.FromPtr(source.overrideContent),
			EnvContent:       kit.FromPtr(source.envContent),
			Files:            swarmFiles,
			Prune:            true,
			WithRegistryAuth: true,
			WorkingDir:       filepath.Dir(filepath.Join(source.repoPath, sync.ComposePath)),
		}
		if _, deployErr := s.swarmService.DeployStack(ctx, sync.EnvironmentID, request); deployErr != nil {
			return result, s.failSync(ctx, result, sync, actor, "Failed to deploy swarm stack", deployErr)
		}
		result.Message = fmt.Sprintf("Successfully deployed swarm stack %s from %s", sync.ProjectName, sync.ComposePath)
	case sync.SyncDirectory:
		if project, syncedFiles, _, changed, err = s.syncProjectDirectory(ctx, sync, syncFiles, actor); err != nil {
			message := kit.Ternary(errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken), "GitOps project binding broken", "Failed to sync project directory")
			return result, s.failSync(ctx, result, sync, actor, message, err)
		}
		result.Message = fmt.Sprintf("Successfully synced directory with %d files to project %s", len(syncedFiles), project.Name)
	default:
		if project, changed, err = s.syncComposeFile(ctx, sync, actor, result, source); err != nil {
			return result, err
		}
		result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)
	}
	if syncedFiles == nil {
		syncedFiles = []string{filepath.Base(sync.ComposePath)}
		if source.overrideFileName != "" {
			syncedFiles = append(syncedFiles, source.overrideFileName)
		}
	}

	if project != nil && (changed || source.replay) {
		if err = s.deployOnce(ctx, sync, project, actor, source.replay); err != nil {
			// The files reached disk, so the row keeps their revision and list.
			errMsg := err.Error()
			result.Message = "Sync wrote files but redeploy failed"
			result.Error = &errMsg
			s.updateSyncStatus(ctx, sync.ID, "failed", errMsg, source.commitHash, syncedFiles)
			s.logError(ctx, sync, actor, errMsg)
			return result, err
		}
	}

	s.updateSyncStatus(ctx, sync.ID, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	targetKind, targetName := "swarm stack", sync.ProjectName
	if project != nil {
		targetKind, targetName = "project", project.Name
	}
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         kit.Ternary(project == nil, "Git sync completed for stack", "Git sync completed"),
		Description:   fmt.Sprintf("Successfully synced '%s' to %s '%s'", sync.Name, targetKind, targetName),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", sync.ID, "target", targetName)
	return result, nil
}

// deployOnce redeploys the synced project, recording the deploy on the run. The deploy runs lifecycle hooks, so a
// replay skips a deploy the run confirmed and refuses one that started without a recorded result.
func (s *Service) deployOnce(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor user.Actor, replay bool) error {
	target := scheduler.TargetOutcome{ResourceType: "gitops_deploy", ID: sync.ID + ":deploy", Status: scheduler.Running}
	previous, _ := jobcontext.Run(ctx)
	if index := slices.IndexFunc(previous.Outcome.Targets, func(recorded scheduler.TargetOutcome) bool { return recorded.ID == target.ID }); replay && index >= 0 {
		switch previous.Outcome.Targets[index].Status {
		case scheduler.Succeeded:
			return nil
		case scheduler.Running:
			return errors.New("the interrupted deploy may already have run its lifecycle hooks; redeploy the project to finish it")
		default:
		}
	}
	if err := jobcontext.Progress(ctx, target); err != nil {
		return err
	}
	deployErr := s.redeployIfRunningAfterSync(ctx, sync, project, actor)
	// Interrupted mid-deploy, its effects are unknown, so the deploy stays running for the replay to refuse.
	if ctx.Err() != nil {
		return cmp.Or(deployErr, ctx.Err())
	}
	target.Status = kit.Ternary(deployErr == nil, scheduler.Succeeded, scheduler.Failed)
	return errors.Join(deployErr, jobcontext.Progress(ctx, target))
}

// prepareSyncSource clones the source at the pinned revision, or the branch head, records that revision on
// the run, and reads the compose, env and override inputs.
func (s *Service) prepareSyncSource(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor user.Actor, pinned string) (*preparedSyncSource, error) {
	repository := sync.Repository
	if repository == nil {
		return nil, s.failSync(ctx, result, sync, actor, "Repository not found", errors.New("repository not found"))
	}
	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, s.failSync(ctx, result, sync, actor, "Failed to get authentication config", err)
	}
	// Replaying a pinned revision needs full history.
	repoPath, err := s.repoService.Clone(ctx, repository.URL, sync.Branch, authConfig, kit.Ternary(pinned == "", 1, 0))
	if err != nil {
		// Anything but a rejected or missing repository is worth another attempt.
		if !errors.Is(err, transport.ErrAuthenticationRequired) && !errors.Is(err, transport.ErrAuthorizationFailed) && !errors.Is(err, transport.ErrRepositoryNotFound) {
			err = common.Classify(common.ErrUnavailable, err)
		}
		return nil, s.failSync(ctx, result, sync, actor, "Failed to clone repository", err)
	}
	source := &preparedSyncSource{repoPath: repoPath, replay: pinned != ""}

	source.commitHash, err = s.repoService.GetCurrentCommit(ctx, repoPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get commit hash", "error", err)
	}
	if pinned != "" && pinned != source.commitHash {
		repo, openErr := git.PlainOpen(repoPath)
		if openErr != nil {
			return source, openErr
		}
		tree, treeErr := repo.Worktree()
		if treeErr != nil {
			return source, treeErr
		}
		if checkoutErr := tree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(pinned)}); checkoutErr != nil {
			return source, checkoutErr
		}
		source.commitHash = pinned
	}
	if source.commitHash == "" {
		return source, errors.New("GitOps source revision is unavailable")
	}
	revision, err := json.Marshal(syncRevision{Commit: source.commitHash})
	if err != nil {
		return source, err
	}
	progress := scheduler.TargetOutcome{ResourceType: "gitops_sync", ID: sync.ID, Status: scheduler.Running, RecoveryData: revision}
	if progressErr := jobcontext.Progress(ctx, progress); progressErr != nil {
		return source, progressErr
	}

	if !s.repoService.FileExists(ctx, repoPath, sync.ComposePath) {
		return source, s.failSync(ctx, result, sync, actor, "Compose file not found at "+sync.ComposePath, errors.New("compose file not found: "+sync.ComposePath))
	}
	source.composeContent, err = s.repoService.ReadFile(ctx, repoPath, sync.ComposePath)
	if err != nil {
		return source, s.failSync(ctx, result, sync, actor, "Failed to read compose file", err)
	}

	composeDir := filepath.Dir(sync.ComposePath)
	envPath := filepath.Join(composeDir, ".env")
	if s.repoService.FileExists(ctx, repoPath, envPath) {
		if content, readErr := s.repoService.ReadFile(ctx, repoPath, envPath); readErr != nil {
			slog.WarnContext(ctx, "Failed to read .env file", "path", envPath, "error", readErr)
		} else {
			source.envContent = &content
		}
	}

	// Like docker compose, only a standard compose filename auto-loads a sibling override; a custom path is the -f case.
	if slices.Contains(projects.ComposeFileCandidates(), filepath.Base(sync.ComposePath)) {
		overrideName, overrideContent, overrideFound, overrideErr := projects.ResolveComposeOverride(
			func(name string) bool {
				return s.repoService.FileExists(ctx, repoPath, filepath.Join(composeDir, name))
			},
			func(name string) (string, error) {
				return s.repoService.ReadFile(ctx, repoPath, filepath.Join(composeDir, name))
			},
		)
		// Degrading would delete the project's existing override on a transient read failure.
		if overrideErr != nil {
			return source, s.failSync(ctx, result, sync, actor, "Failed to read compose override file", overrideErr)
		}
		if overrideFound {
			source.overrideContent = &overrideContent
			source.overrideFileName = overrideName
		}
	}
	return source, nil
}

// syncComposeFile applies a single-file sync, creating the project on the first sync. It reports whether the
// project files changed.
func (s *Service) syncComposeFile(ctx context.Context, sync *projectpkg.GitOpsSync, actor user.Actor, result *gitops.SyncResult, source *preparedSyncSource) (*projectpkg.Project, bool, error) {
	slog.InfoContext(ctx, "Using single file sync mode", "syncId", sync.ID, "composePath", sync.ComposePath)
	// Files referenced through COMPOSE_FILE or COMPOSE_ENV_FILES never reach a single-file project.
	if projects.ComposeEnvRequiresDirectorySync(sync.ComposePath, source.overrideFileName, source.envContent) {
		return nil, false, s.failSync(ctx, result, sync, actor, "COMPOSE_FILE or COMPOSE_ENV_FILES references additional files",
			errors.New("the synced .env references additional files via COMPOSE_FILE or COMPOSE_ENV_FILES; enable \"Sync entire directory\" for this sync"))
	}

	if projectID := kit.FromPtr(sync.ProjectID); projectID != "" {
		project, found, err := s.projectService.FindProjectByID(ctx, projectID)
		if err != nil {
			return nil, false, s.failSync(ctx, result, sync, actor, "Failed to load existing project", fmt.Errorf("failed to get project %s: %w", projectID, err))
		}
		if !found {
			slog.WarnContext(ctx, "Existing project not found; GitOps project binding is broken", "projectId", projectID, "syncId", sync.ID)
			bindingErr := common.Classify(common.ErrGitOpsSyncProjectBindingBroken, fmt.Errorf("GitOps sync project binding broken: sync %s references missing project %s", sync.ID, projectID))
			return nil, false, s.failSync(ctx, result, sync, actor, "GitOps project binding broken", bindingErr)
		}
		_, changed, err := s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, source.composeContent, source.envContent, source.overrideContent, source.overrideFileName, actor)
		if err != nil {
			return nil, false, s.failSync(ctx, result, sync, actor, "Failed to update project files", err)
		}
		slog.InfoContext(ctx, "Updated project files", "projectName", project.Name, "projectId", project.ID, "changed", changed)
		return project, changed, nil
	}

	// The non-suffixing create: a name collision means the binding is broken, never a "-N" duplicate.
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, source.composeContent, source.envContent, projecttypes.CreateProjectWorkspaceManifest{}, nil, nil, nil, actor, false)
	if errors.Is(err, projects.ErrProjectDirExists) {
		err = common.Classify(common.ErrGitOpsSyncProjectBindingBroken, fmt.Errorf("GitOps sync project binding broken: sync %s cannot create project %q: a directory with that name "+
			"already exists; refusing to create a duplicate", sync.ID, projects.SanitizeProjectName(sync.ProjectName)))
		return nil, false, s.failSync(ctx, result, sync, actor, "GitOps project binding broken", err)
	}
	if err != nil {
		return nil, false, s.failSync(ctx, result, sync, actor, "Failed to create project", err)
	}
	if err = s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Update("project_id", project.ID).Error; err != nil {
		return nil, false, s.failSync(ctx, result, sync, actor, "Failed to update sync with project ID", err)
	}
	if err = s.db.WithContext(ctx).Model(&projectpkg.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", sync.ID).Error; err != nil {
		return nil, false, s.failSync(ctx, result, sync, actor, "Failed to mark project as GitOps-managed", err)
	}
	if _, _, err = s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, source.composeContent, source.envContent, source.overrideContent, source.overrideFileName, actor); err != nil {
		return nil, false, s.failSync(ctx, result, sync, actor, "Failed to sync project env files", err)
	}
	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)
	return project, true, nil
}

// redeployIfRunningAfterSync redeploys a running project, or any project when RedeployAfterSync is on. A project
// left stopped still gets its images pulled when PullImageAfterSync is on, so a re-pointed tag stays available.
func (s *Service) redeployIfRunningAfterSync(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor user.Actor) error {
	details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{})
	running := err == nil && (details.Status == string(projectpkg.ProjectStatusRunning) || details.Status == string(projectpkg.ProjectStatusPartiallyRunning))
	switch {
	case running || sync.RedeployAfterSync:
		slog.InfoContext(ctx, "Redeploying project after Git sync", "projectName", project.Name, "projectId", project.ID, "wasRunning", running)
		if redeployErr := s.projectService.RedeployProject(ctx, project.ID, actor, nil); redeployErr != nil {
			slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "error", redeployErr, "projectId", project.ID)
			return common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("redeploy failed: %w", redeployErr))
		}
	case sync.PullImageAfterSync:
		credentials, credentialsErr := s.projectService.ResolveRegistryCredentials(ctx)
		if credentialsErr != nil {
			slog.WarnContext(ctx, "failed to resolve registry credentials for post-sync pull", "error", credentialsErr, "projectId", project.ID)
		}
		slog.InfoContext(ctx, "Pulling project images after Git sync (project not running)", "projectName", project.Name, "projectId", project.ID)
		if pullErr := s.projectService.PullProjectImages(ctx, project.ID, io.Discard, actor, credentials); pullErr != nil {
			slog.ErrorContext(ctx, "failed to pull project images after Git sync", "error", pullErr, "projectId", project.ID)
			return common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("post-sync image pull failed: %w", pullErr))
		}
	}
	return nil
}

// updateSyncStatus stores a run's result on the row. An empty commit keeps the recorded one, and nil
// syncedFiles keeps the tracked list.
func (s *Service) updateSyncStatus(ctx context.Context, id, status, errorMsg, commitHash string, syncedFiles []string) {
	updates := map[string]any{
		"last_sync_at":     time.Now(),
		"last_sync_status": status,
		"last_sync_error":  kit.Ternary[any](errorMsg != "", errorMsg, nil),
	}
	if commitHash != "" {
		updates["last_sync_commit"] = commitHash
	}
	if syncedFiles != nil {
		updates["synced_files"] = projectpkg.EncodeSyncedFiles(syncedFiles)
	}
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status", "error", err, "syncId", id)
	}
}

// failSync records a failed sync on the result, the row and an event, and returns err. A broken project
// binding also turns auto-sync off so the sync stops retrying.
func (s *Service) failSync(ctx context.Context, result *gitops.SyncResult, sync *projectpkg.GitOpsSync, actor user.Actor, message string, err error) error {
	errMsg := err.Error()
	result.Message = message
	result.Error = &errMsg
	s.updateSyncStatus(ctx, sync.ID, "failed", errMsg, "", nil)
	s.logError(ctx, sync, actor, errMsg)
	if errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken) {
		if disableErr := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Update("auto_sync", false).Error; disableErr != nil {
			slog.ErrorContext(ctx, "Failed to disable GitOps auto-sync after broken project binding", "syncId", sync.ID, "error", disableErr)
		}
		sync.AutoSync = false
		s.unregisterJob(ctx, sync.ID)
	}
	return err
}

// syncProjectDirectory stages and validates the synced tree, then creates or updates the project. It reports
// the synced paths, whether it created the project, and whether the contents changed.
func (s *Service) syncProjectDirectory(ctx context.Context, sync *projectpkg.GitOpsSync, syncFiles []projects.SyncFile, actor user.Actor) (*projectpkg.Project, []string, bool, bool, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, nil, false, false, fmt.Errorf("failed to get projects directory: %w", err)
	}
	stageLogical, err := acfs.MkdirTemp(ctx, projectsDir, "/", ".gitops-sync-stage-*")
	if err != nil {
		return nil, nil, false, false, fmt.Errorf("failed to create staging directory: %w", err)
	}
	stage := &stagedDirectorySync{
		stagePath:       filepath.Join(projectsDir, filepath.FromSlash(strings.TrimPrefix(stageLogical, "/"))),
		stageLogical:    stageLogical,
		projectsDir:     projectsDir,
		composeFileName: filepath.Base(sync.ComposePath),
		// Older syncs may still track .env, which cleanup would delete from the stage before the env merge reads it.
		oldSyncedFiles:  slices.DeleteFunc(sync.SyncedFileList(), func(path string) bool { return slices.Contains(reservedRootEnvFiles, path) }),
		contentsChanged: true,
	}
	// The stage must go even after ctx was cancelled: acfs refuses work on a cancelled context.
	defer func() {
		if stage.stagePath != "" {
			_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDir, stageLogical)
		}
	}()

	// Reserved root env files are Arcane's bookkeeping and only come from the env merge, which takes a committed .env as its git source.
	for _, file := range syncFiles {
		switch {
		case !slices.Contains(reservedRootEnvFiles, file.RelativePath):
			stage.syncFiles = append(stage.syncFiles, file)
			stage.syncedFiles = append(stage.syncedFiles, file.RelativePath)
		case file.RelativePath == projects.EffectiveEnvFileName:
			stage.gitEnvContent = new(string(file.Content))
		default:
			slog.WarnContext(ctx, "dropping reserved Arcane env file from git payload; will be re-derived from override merge", "path", file.RelativePath)
		}
	}

	if stage.project, err = s.DirectoryProject(ctx, sync); err != nil {
		return nil, nil, false, false, err
	}
	if stage.project != nil {
		staleFiles, staleErr := projects.StaleComposeFiles(ctx, stage.project.Path, stage.composeFileName, stage.syncedFiles)
		if staleErr != nil {
			return nil, nil, false, false, fmt.Errorf("failed to detect stale compose files: %w", staleErr)
		}
		stage.backupScope.Paths = slices.Concat(stage.syncedFiles, stage.oldSyncedFiles, staleFiles,
			[]string{projects.EffectiveEnvFileName, projects.GitSourceEnvFileName, projects.OverrideEnvFileName})
		if linkErr := linkProjectIntoStage(stage.project.Path, stage.stagePath, stage.backupScope.Paths); linkErr != nil {
			return nil, nil, false, false, fmt.Errorf("failed to stage current project files: %w", linkErr)
		}
		if _, seedErr := seedStageEnvFromDir(ctx, stage.project.Path, projectsDir, stage.stagePath); seedErr != nil {
			return nil, nil, false, false, seedErr
		}
		if stage.contentsChanged, err = projects.DirectorySyncContentsChanged(ctx, stage.project.Path, stage.syncFiles, stage.oldSyncedFiles, stage.composeFileName); err != nil {
			return nil, nil, false, false, fmt.Errorf("failed to compare staged directory changes: %w", err)
		}
	} else if seedErr := s.seedStageEnvFromCandidateDir(ctx, sync, projectsDir, stage.stagePath); seedErr != nil {
		return nil, nil, false, false, seedErr
	}

	preEnvContent, postEnvContent, err := s.applyDirectorySync(ctx, stage.stagePath, stage)
	if err != nil {
		return nil, nil, false, false, err
	}
	if stage.project != nil && projects.EnvContentChanged(preEnvContent, postEnvContent) {
		stage.contentsChanged = true
	}
	if stage.serviceCount, err = s.projectService.ValidateComposeDirectory(ctx, sync.ProjectName, stage.stagePath, stage.composeFileName); err != nil {
		return nil, nil, false, false, fmt.Errorf("invalid compose file: %w", err)
	}

	if stage.project == nil {
		project, createErr := s.createDirectorySyncProject(ctx, sync, stage, actor)
		if createErr != nil {
			return nil, nil, false, false, createErr
		}
		return project, stage.syncedFiles, true, true, nil
	}
	project, err := s.updateDirectorySyncProject(ctx, sync, stage)
	if err != nil {
		return nil, nil, false, false, err
	}
	return project, stage.syncedFiles, false, stage.contentsChanged, nil
}

// seedStageEnvFromCandidateDir copies the env files of a pre-existing directory at the conventional project
// path into a first-sync stage, so a server-side .env can supply ${VAR} values the repo leaves out.
func (s *Service) seedStageEnvFromCandidateDir(ctx context.Context, sync *projectpkg.GitOpsSync, projectsDir, stagePath string) error {
	candidatePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	info, err := os.Stat(candidatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect pre-existing project directory %s: %w", candidatePath, err)
	}
	if !info.IsDir() {
		return nil
	}
	state, err := seedStageEnvFromDir(ctx, candidatePath, projectsDir, stagePath)
	if err != nil || state.HasEffective || (!state.HasGitSource && !state.HasOverride) {
		return err
	}
	// Only .env.git and/or project.env exist, so derive .env from them.
	merged, err := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
	if err != nil {
		return fmt.Errorf("build effective env from pre-existing project: %w", err)
	}
	if writeErr := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.EffectiveEnvFileName, merged); writeErr != nil {
		return fmt.Errorf("seed stage .env: %w", writeErr)
	}
	return nil
}

// DirectoryProject resolves the linked project for a directory sync, relinking or recovering it when the
// stored reference is stale. A sync whose project is gone and cannot be recovered has a broken binding.
func (s *Service) DirectoryProject(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	projectID := kit.FromPtr(sync.ProjectID)
	if projectID != "" {
		project, found, err := s.projectService.FindProjectByID(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("failed to get project %s: %w", projectID, err)
		}
		if found {
			if rootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); rootErr != nil {
				return nil, rootErr
			}
			return project, s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project)
		}
		slog.WarnContext(ctx, "Existing project not found, attempting recovery", "projectId", projectID, "syncId", sync.ID)
	}

	project, err := s.recoverProject(ctx, sync)
	if projectID != "" && project == nil {
		cause := cmp.Or(err, errors.New("no unique recovery candidate was found"))
		return nil, common.Classify(common.ErrGitOpsSyncProjectBindingBroken, fmt.Errorf("GitOps sync project binding broken: sync %s references missing project %s: %w", sync.ID, projectID, cause))
	}
	return project, err
}

// recoverProject finds the one project still managed by the sync, or else adopts the one projects-directory
// entry that matches the sync's name and holds its compose file.
func (s *Service) recoverProject(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	var managed []projectpkg.Project
	if err := s.db.WithContext(ctx).Where("gitops_managed_by = ?", sync.ID).Find(&managed).Error; err != nil {
		return nil, fmt.Errorf("failed to list GitOps-managed projects for sync %s: %w", sync.ID, err)
	}
	var recovered []projectpkg.Project
	for i := range managed {
		if err := s.projectService.EnsureProjectPathUnderRoot(ctx, &managed[i], true); err != nil {
			return nil, err
		}
		if _, err := s.projectService.ResolveProjectComposeFile(ctx, &managed[i]); err == nil {
			recovered = append(recovered, managed[i])
		} else if !errors.Is(err, common.ErrProjectComposeFileNotFound) {
			return nil, err
		}
	}
	switch len(recovered) {
	case 0:
	case 1:
		return &recovered[0], s.projectService.EnsureGitOpsProjectLinked(ctx, sync, &recovered[0])
	default:
		return nil, fmt.Errorf("multiple GitOps-managed projects match sync %s; refusing automatic relink", sync.ID)
	}

	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := acfs.List(ctx, projectsDir, "/")
	if err != nil {
		return nil, fmt.Errorf("failed to list projects directory %s: %w", projectsDir, err)
	}
	composeFileName := strings.TrimSpace(filepath.Base(sync.ComposePath))
	if composeFileName == "" || composeFileName == "." {
		return nil, nil
	}

	prefix := projects.SanitizeProjectName(sync.ProjectName)
	var matches []string
	for _, entry := range entries {
		// acfs reports a symlink as a non-directory, so adoption never follows one; Arcane's own scratch dirs never qualify.
		if !entry.IsDirectory || projects.IsInternalScratchDirName(entry.Name) || (prefix != "" && entry.Name != prefix && !strings.HasPrefix(entry.Name, prefix+"-")) {
			continue
		}
		candidatePath := filepath.Join(projectsDir, entry.Name)
		composeEntry, statErr := acfs.Stat(ctx, projectsDir, path.Join(entry.Path, composeFileName), true)
		switch {
		case statErr == nil && !composeEntry.IsDirectory:
			matches = append(matches, candidatePath)
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return nil, fmt.Errorf("failed to inspect recovery candidate %s: %w", filepath.Join(candidatePath, composeFileName), statErr)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("multiple candidate project directories match sync %s; refusing automatic relink", sync.ID)
	}

	var project projectpkg.Project
	err = s.db.WithContext(ctx).Where("path = ?", matches[0]).First(&project).Error
	if err == nil {
		if ensureErr := s.projectService.EnsureProjectPathUnderRoot(ctx, &project, true); ensureErr != nil {
			return nil, ensureErr
		}
		return &project, s.projectService.EnsureGitOpsProjectLinked(ctx, sync, &project)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("failed to get project by path %s: %w", matches[0], err)
	}

	project = projectpkg.Project{
		Name:            sync.ProjectName,
		DirName:         new(filepath.Base(matches[0])),
		Path:            matches[0],
		Status:          projectpkg.ProjectStatusUnknown,
		StatusReason:    new("Project recovered from existing GitOps-managed directory"),
		GitOpsManagedBy: &sync.ID,
	}
	if project.ServiceCount, err = s.projectService.CountServicesFromCompose(ctx, project); err != nil {
		slog.WarnContext(ctx, "Failed to count services while recovering GitOps project", "syncId", sync.ID, "path", matches[0], "error", err)
	}
	if createErr := s.projectService.CreateGitOpsManagedProject(ctx, sync, &project, user.Actor{}, false); createErr != nil {
		return nil, createErr
	}
	return &project, nil
}

// walkAndParseSyncDirectory walks the repository directory and returns all files with their contents.
// Returns the list of SyncFile entries and an error if any; it fails if the compose file is missing.
func (s *Service) walkAndParseSyncDirectory(ctx context.Context, sync *projectpkg.GitOpsSync, repoPath string) ([]projects.SyncFile, error) {
	slog.InfoContext(ctx, "Starting directory walk", "syncId", sync.ID, "composePath", sync.ComposePath)

	// Walk the directory to get all files
	maxFiles, maxTotalSize, maxBinarySize := s.limits(ctx, sync)

	walkResult, err := s.repoService.WalkDirectory(ctx, repoPath, sync.ComposePath, maxFiles, maxTotalSize, maxBinarySize)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	slog.InfoContext(ctx, "Directory walk complete",
		"syncId", sync.ID,
		"totalFiles", walkResult.TotalFiles,
		"totalSize", walkResult.TotalSize,
		"skippedBinaries", walkResult.SkippedBinaries)

	// WalkDirectory roots the walk at filepath.Dir(sync.ComposePath), so the
	// compose file is always emitted at the top level as filepath.Base(sync.ComposePath).
	composeFileName := filepath.Base(sync.ComposePath)
	composeFound := false

	// Convert walked files to SyncFile format
	syncFiles := make([]projects.SyncFile, len(walkResult.Files))
	for i, f := range walkResult.Files {
		syncFiles[i] = projects.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
			Executable:   f.Executable,
		}
		if f.RelativePath == composeFileName {
			composeFound = true
		}
	}

	if !composeFound {
		return nil, fmt.Errorf("compose file %s not found in walked directory", composeFileName)
	}

	return syncFiles, nil
}

// applyDirectorySync drops untracked and stale compose files, writes the repo files, then runs the env merge, the
// order validation saw. It returns the effective .env content before and after the merge.
func (s *Service) applyDirectorySync(ctx context.Context, targetPath string, stage *stagedDirectorySync) (string, string, error) {
	if len(stage.oldSyncedFiles) > 0 {
		if err := projects.CleanupRemovedFiles(ctx, stage.projectsDir, targetPath, stage.oldSyncedFiles, stage.syncedFiles); err != nil {
			return "", "", fmt.Errorf("failed to clean removed synced files: %w", err)
		}
	}

	if err := projects.RemoveStaleComposeFiles(ctx, targetPath, stage.composeFileName, stage.syncedFiles); err != nil {
		return "", "", fmt.Errorf("failed to remove stale compose files: %w", err)
	}

	// Write the repo files (excluding reserved root env files, handled below)
	// after cleanup so validation sees the final on-disk tree exactly as it
	// will exist in the managed project.
	if _, err := projects.WriteSyncedDirectory(ctx, stage.projectsDir, targetPath, stage.syncFiles); err != nil {
		return "", "", fmt.Errorf("failed to write staged sync files: %w", err)
	}

	// Route the project-root .env through the same three-file override merge
	// single-file git sync uses: git is source-of-truth, edits made in Arcane
	// become an override that wins, and new git-introduced keys still flow in.
	return s.projectService.ApplyGitSyncEnvToDirectory(ctx, targetPath, stage.projectsDir, stage.gitEnvContent)
}

// createDirectorySyncProject promotes a validated staged tree into a new
// managed project directory and links it back to the Git sync record.
func (s *Service) createDirectorySyncProject(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync, actor user.Actor) (*projectpkg.Project, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	// Non-suffixing create: a GitOps sync must never mint a "-N" duplicate. Any
	// adoptable existing directory was already resolved by getDirectorySyncProject,
	// so a collision here means the name is taken by an unrelated/unrecoverable dir;
	// treat it as a broken binding rather than creating a duplicate.
	basePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	projectPath, folderName, err := projects.CreateExactDir(ctx, projectsDir, basePath, sync.ProjectName, utils.DirPerm)
	if err != nil {
		if errors.Is(err, projects.ErrProjectDirExists) {
			return nil, common.Classify(
				common.ErrGitOpsSyncProjectBindingBroken,
				fmt.Errorf(
					"GitOps sync project binding broken: sync %s cannot create project %q: a directory with that name "+
						"already exists; refusing to create a duplicate",
					sync.ID,
					projects.SanitizeProjectName(
						sync.ProjectName,
					),
				),
			)
		}
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	projectLogical, err := acfs.LogicalPath(projectsDir, projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve created project directory: %w", err)
	}
	if removeErr := acfs.Remove(ctx, projectsDir, projectLogical); removeErr != nil {
		return nil, fmt.Errorf("failed to prepare project directory: %w", removeErr)
	}

	if renameErr := acfs.Rename(ctx, projectsDir, stage.stageLogical, projectLogical); renameErr != nil {
		return nil, fmt.Errorf("failed to promote staged project directory: %w", renameErr)
	}
	stage.stagePath = ""

	if chmodErr := os.Chmod(projectPath, utils.DirPerm); chmodErr != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, projectLogical)
		return nil, fmt.Errorf("failed to set project directory permissions: %w", chmodErr)
	}

	project := &projectpkg.Project{
		Name:         sync.ProjectName,
		DirName:      new(folderName),
		Path:         projectPath,
		Status:       projectpkg.ProjectStatusStopped,
		ServiceCount: stage.serviceCount,
		RunningCount: 0,
	}

	if createGitOpsManagedProjectErr := s.projectService.CreateGitOpsManagedProject(ctx, sync, project, actor); createGitOpsManagedProjectErr != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, projectLogical)
		return nil, createGitOpsManagedProjectErr
	}

	return project, nil
}

// updateDirectorySyncProject applies a validated stage in place, so running containers keep their bind-mount
// inodes; a backup scoped to the touched paths rolls a failed promotion back.
func (s *Service) updateDirectorySyncProject(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync) (*projectpkg.Project, error) {
	project := stage.project
	projectPath := filepath.Clean(project.Path)
	existed := true

	// The project directory is the confinement root for the writes below and may
	// itself be a symlink, so it is probed and bootstrapped through os.
	if info, err := os.Stat(projectPath); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("project path is not a directory: %s", projectPath)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		existed = false
		if mkdirAllErr := os.MkdirAll(projectPath, utils.DirPerm); mkdirAllErr != nil {
			return nil, fmt.Errorf("failed to recreate project directory: %w", mkdirAllErr)
		}
		if chmodErr := os.Chmod(projectPath, utils.DirPerm); chmodErr != nil {
			return nil, fmt.Errorf("failed to set project directory permissions: %w", chmodErr)
		}
	} else {
		return nil, fmt.Errorf("failed to inspect current project directory: %w", err)
	}

	// Rollback must run even after ctx was cancelled: acfs refuses work on a
	// cancelled context.
	cleanupCtx := context.WithoutCancel(ctx)
	var backup *projects.ProjectUpdateBackup
	keepBackup := false
	if existed {
		var removeBackup func()
		var err error
		backup, removeBackup, err = projects.BackupProjectDirectory(ctx, stage.projectsDir, projectPath, ".gitops-backup-*", stage.backupScope)
		if err != nil {
			return nil, fmt.Errorf("failed to back up current project directory: %w", err)
		}
		defer func() {
			if !keepBackup {
				removeBackup()
			}
		}()
	}

	restore := func(cause error) error {
		var restoreErr error
		if existed {
			restoreErr = projects.RestoreProjectDirectoryBackup(cleanupCtx, stage.projectsDir, projectPath, backup)
		} else {
			restoreErr = acfs.RemoveAll(cleanupCtx, filepath.Dir(projectPath), "/"+filepath.Base(projectPath))
		}
		if restoreErr == nil {
			return cause
		}
		if backup != nil {
			// Kept so the previous configuration can be recovered by hand.
			keepBackup = true
			slog.ErrorContext(ctx, "Failed to restore project directory after sync promotion failure; backup kept", "projectPath", projectPath, "backupPath", backup.BackupDir, "error", restoreErr)
		}
		return errors.Join(cause, fmt.Errorf("rollback project directory: %w", restoreErr))
	}

	if _, _, err := s.applyDirectorySync(ctx, projectPath, stage); err != nil {
		return nil, restore(fmt.Errorf("failed to promote staged project directory: %w", err))
	}

	if err := s.db.WithContext(ctx).Model(&projectpkg.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
		"service_count":     stage.serviceCount,
		"gitops_managed_by": sync.ID,
		"updated_at":        time.Now(),
	}).Error; err != nil {
		return nil, restore(fmt.Errorf("failed to update project metadata after directory sync: %w", err))
	}

	return project, nil
}

// seedStageEnvFromDir copies sourceDir's readable project-root env files into the stage and returns the state
// it read; like the env merge, it skips a permission-locked file.
func seedStageEnvFromDir(ctx context.Context, sourceDir, projectsDir, stagePath string) (projects.ProjectEnvState, error) {
	state, err := projects.ReadProjectEnvState(sourceDir)
	if err != nil {
		return state, fmt.Errorf("read env files from %s: %w", sourceDir, err)
	}
	if state.HasGitSource {
		if writeProjectFileErr := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.GitSourceEnvFileName, state.GitContent); writeProjectFileErr != nil {
			return state, fmt.Errorf("seed stage .env.git: %w", writeProjectFileErr)
		}
	}
	if state.HasOverride {
		if writeOverrideEnvErr := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.OverrideEnvFileName, state.OverrideContent); writeOverrideEnvErr != nil {
			return state, fmt.Errorf("seed stage project.env: %w", writeOverrideEnvErr)
		}
	}
	if state.HasEffective {
		if writeEffectiveEnvErr := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.EffectiveEnvFileName, state.DirectContent); writeEffectiveEnvErr != nil {
			return state, fmt.Errorf("seed stage .env: %w", writeEffectiveEnvErr)
		}
	}
	return state, nil
}

// linkProjectIntoStage symlinks every live path outside scopePaths into the stage, so validation sees the whole
// project without copying it. The links are never promoted; acfs refuses symlinks, so this uses os.
func linkProjectIntoStage(livePath, stagePath string, scopePaths []string) error {
	scope := make(map[string]struct{}, len(scopePaths))
	ancestors := make(map[string]struct{})
	for _, scopePath := range scopePaths {
		cleaned := path.Clean(filepath.ToSlash(scopePath))
		scope[cleaned] = struct{}{}
		for dir := path.Dir(cleaned); dir != "." && dir != "/"; dir = path.Dir(dir) {
			ancestors[dir] = struct{}{}
		}
	}

	var link func(rel string) error
	link = func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(livePath, filepath.FromSlash(rel)))
		if err != nil {
			return kit.Ternary(rel == "" && errors.Is(err, fs.ErrNotExist), nil, err)
		}
		for _, entry := range entries {
			entryRel := path.Join(rel, entry.Name())
			if _, touched := scope[entryRel]; touched {
				continue
			}
			stageEntry := filepath.Join(stagePath, filepath.FromSlash(entryRel))
			if _, isAncestor := ancestors[entryRel]; isAncestor && entry.IsDir() {
				if mkdirErr := os.Mkdir(stageEntry, utils.DirPerm); mkdirErr != nil {
					return mkdirErr
				}
				if linkErr := link(entryRel); linkErr != nil {
					return linkErr
				}
				continue
			}
			if symlinkErr := os.Symlink(filepath.Join(livePath, filepath.FromSlash(entryRel)), stageEntry); symlinkErr != nil {
				return symlinkErr
			}
		}
		return nil
	}
	return link("")
}
