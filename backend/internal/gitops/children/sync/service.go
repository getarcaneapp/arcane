// Package sync applies deployment GitOps syncs: it clones the source, stages
// and promotes project files, and redeploys the linked project or stack.
package sync

import (
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

// Service runs deployment syncs. Shared sync limits, error events and job
// registration stay with the parent and arrive as callbacks.
type Service struct {
	db             *database.DB
	repoService    *gitrepo.GitRepositoryService
	projectService *projectpkg.ProjectService
	swarmService   *swarm.SwarmService
	eventService   *event.EventService
	limits         func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64)
	logError       func(context.Context, *projectpkg.GitOpsSync, user.Actor, string)
	unregisterJob  func(context.Context, string)
}

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

// Run clones the sync source and applies it to its project or Swarm stack.
func (s *Service) Run(ctx context.Context, sync *projectpkg.GitOpsSync, actor user.Actor, result *gitops.SyncResult) (*gitops.SyncResult, error) {
	source, err := s.prepareSyncSource(ctx, sync, result, actor)
	if source != nil && source.repoPath != "" {
		defer s.repoService.Discard(ctx, source.repoPath)
	}
	if err != nil {
		return result, err
	}
	if source == nil {
		return result, errors.New("prepared sync source is missing")
	}

	if sync.TargetType == "swarm_stack" {
		return s.performSwarmStackSyncInternal(ctx, sync, sync.ID, actor, result, source)
	}

	if sync.SyncDirectory {
		return s.performDirectorySync(ctx, sync, sync.ID, actor, result, source)
	}

	return s.performSingleFileSyncInternal(ctx, sync, sync.ID, actor, result, source)
}

// DirectoryProject resolves the linked project for a directory sync,
// relinking or recovering it when the stored reference is stale.
func (s *Service) DirectoryProject(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	return s.getDirectorySyncProjectInternal(ctx, sync)
}

// ReconcileRevision confirms an interrupted run when the record already holds
// the source revision that run pinned.
func ReconcileRevision(ctx context.Context, previous scheduler.Run, record *projectpkg.GitOpsSync, outcome scheduler.Outcome) (scheduler.Outcome, error) {
	target, commit, err := recordedRevision(previous, record.ID)
	if err != nil {
		return outcome, err
	}
	if commit == "" || kit.FromPtr(record.LastSyncCommit) != commit || kit.FromPtr(record.LastSyncStatus) != "success" {
		return outcome, nil
	}
	target.Status = scheduler.Succeeded
	if progressErr := jobcontext.Progress(ctx, target); progressErr != nil {
		return outcome, progressErr
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Targets: []scheduler.TargetOutcome{target}}, nil
}

// recordedRevision returns the target and source revision an earlier run recorded for syncID.
func recordedRevision(run scheduler.Run, syncID string) (scheduler.TargetOutcome, string, error) {
	idx := slices.IndexFunc(run.Outcome.Targets, func(target scheduler.TargetOutcome) bool {
		return target.ID == syncID && len(target.RecoveryData) > 0
	})
	if idx < 0 {
		return scheduler.TargetOutcome{}, "", nil
	}
	var revision syncRevision
	if err := json.Unmarshal(run.Outcome.Targets[idx].RecoveryData, &revision); err != nil {
		return scheduler.TargetOutcome{}, "", err
	}
	return run.Outcome.Targets[idx], revision.Commit, nil
}

// syncRevision is the recovery data a sync run records so a retry replays the same source revision.
type syncRevision struct {
	Commit string `json:"commit"`
}

// preparedSyncSource captures the repository data needed by the sync execution
// paths after the source repository has been cloned and validated.
type preparedSyncSource struct {
	repoPath         string
	commitHash       string
	composeContent   string
	envContent       *string
	overrideContent  *string
	overrideFileName string
}

// stagedDirectorySync holds the fully prepared directory-sync result before it
// is promoted into the live project path.
type stagedDirectorySync struct {
	stagePath string
	// stageLogical is stagePath expressed relative to the projects directory,
	// which is the confinement root every staging mutation runs against.
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

// prepareSyncSource clones the source repository, validates that the configured
// compose file exists, and reads the compose/env inputs for the sync flow.
func (s *Service) prepareSyncSource(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor user.Actor) (*preparedSyncSource, error) {
	repository := sync.Repository
	if repository == nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Repository not found", "repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to get authentication config", err.Error())
	}

	// A retry replays the revision its interrupted predecessor recorded, which needs full history.
	pinnedCommit := ""
	if previous, ok := jobcontext.Run(ctx); ok {
		if _, pinnedCommit, err = recordedRevision(previous, sync.ID); err != nil {
			return nil, err
		}
	}
	repoPath, err := s.repoService.Clone(ctx, repository.URL, sync.Branch, authConfig, kit.Ternary(pinnedCommit == "", 1, 0))
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to clone repository", err.Error())
	}
	source := &preparedSyncSource{repoPath: repoPath}

	source.commitHash, err = s.repoService.GetCurrentCommit(ctx, repoPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get commit hash", "error", err)
	}
	if pinnedCommit != "" && pinnedCommit != source.commitHash {
		repo, openErr := git.PlainOpen(repoPath)
		if openErr != nil {
			return source, openErr
		}
		tree, treeErr := repo.Worktree()
		if treeErr != nil {
			return source, treeErr
		}
		if checkoutErr := tree.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(pinnedCommit)}); checkoutErr != nil {
			return source, checkoutErr
		}
		source.commitHash = pinnedCommit
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
		errMsg := "compose file not found: " + sync.ComposePath
		return source, s.failSync(ctx, sync.ID, result, sync, actor, "Compose file not found at "+sync.ComposePath, errMsg)
	}
	source.composeContent, err = s.repoService.ReadFile(ctx, repoPath, sync.ComposePath)
	if err != nil {
		return source, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to read compose file", err.Error())
	}

	envPath := filepath.Join(filepath.Dir(sync.ComposePath), ".env")
	if s.repoService.FileExists(ctx, repoPath, envPath) {
		content, readFileErr := s.repoService.ReadFile(ctx, repoPath, envPath)
		if readFileErr != nil {
			slog.WarnContext(ctx, "Failed to read .env file", "path", envPath, "error", readFileErr)
		} else {
			source.envContent = &content
		}
	}

	// Detect a Docker Compose override file (compose.override.yaml, etc.) sitting
	// beside the compose file in the repo, mirroring `docker compose` behavior.
	// Only auto-load an override when the compose file was referenced by a
	// standard filename; an explicit custom path is the `-f` case, where docker
	// compose does not auto-load overrides.
	if slices.Contains(projects.ComposeFileCandidates(), filepath.Base(sync.ComposePath)) {
		composeDir := filepath.Dir(sync.ComposePath)
		overrideName, overrideContent, overrideFound, overrideErr := projects.ResolveComposeOverride(
			func(name string) bool {
				return s.repoService.FileExists(ctx, repoPath, filepath.Join(composeDir, name))
			},
			func(name string) (string, error) {
				return s.repoService.ReadFile(ctx, repoPath, filepath.Join(composeDir, name))
			},
		)
		// Fail the sync on a read error instead of degrading: WriteComposeOverrideFile
		// with nil content removes the project's existing override, so a transient
		// read failure here would delete the override and redeploy without it.
		if overrideErr != nil {
			return source, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to read compose override file", overrideErr.Error())
		}
		if overrideFound {
			source.overrideContent = &overrideContent
			source.overrideFileName = overrideName
		}
	}

	return source, nil
}

// performDirectorySync runs the directory-sync path and only triggers a
// redeploy when an already running project's synced contents changed.
func (
	s *Service,
) performDirectorySync(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id string,
	actor user.Actor,
	result *gitops.SyncResult,
	source *preparedSyncSource,
) (
	*gitops.SyncResult,
	error,
) {
	slog.InfoContext(ctx, "Using directory sync mode", "syncId", id, "composePath", sync.ComposePath)

	syncFiles, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
	if err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
	}

	project, syncedFiles, _, contentsChanged, err := s.syncProjectDirectoryInternal(ctx, sync, syncFiles, actor)
	if err != nil {
		if errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken) {
			errMsg := err.Error()
			result.Message = "GitOps project binding broken"
			result.Error = new(errMsg)
			return result, err
		}
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to sync project directory", err.Error())
	}

	if contentsChanged {
		if redeployErr := s.redeployIfRunningAfterSync(ctx, sync, project, actor, "directory"); redeployErr != nil {
			if errors.Is(redeployErr, common.ErrRedeployAfterSyncFailed) {
				s.markSyncRedeployFailedInternal(ctx, sync, id, source.commitHash, syncedFiles, redeployErr, actor, result)
			}
			return result, redeployErr
		}
	}

	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced directory with %d files to project %s", len(syncedFiles), project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// singleFileSyncedFilesInternal builds the tracked-file list for a single-file
// sync: the compose file's base name plus the override file name when one was
// resolved. Shared by the compose-only and swarm-stack single-file paths so the
// two never disagree on what a single-file sync tracked.
func singleFileSyncedFilesInternal(sync *projectpkg.GitOpsSync, source *preparedSyncSource) []string {
	syncedFiles := []string{filepath.Base(sync.ComposePath)}
	if source.overrideFileName != "" {
		syncedFiles = append(syncedFiles, source.overrideFileName)
	}
	return syncedFiles
}

// performSingleFileSyncInternal preserves the legacy compose-only Git sync behavior.
func (
	s *Service,
) performSingleFileSyncInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id string,
	actor user.Actor,
	result *gitops.SyncResult,
	source *preparedSyncSource,
) (
	*gitops.SyncResult,
	error,
) {
	slog.InfoContext(ctx, "Using single file sync mode", "syncId", id, "composePath", sync.ComposePath)

	// Single-file sync copies only the one compose file (plus .env and a sibling
	// override). If the synced .env references additional files via COMPOSE_FILE
	// or COMPOSE_ENV_FILES, those files never reach the project, so direct the
	// user to directory sync instead of silently deploying an incomplete
	// selection.
	if projects.ComposeEnvRequiresDirectorySync(sync.ComposePath, source.overrideFileName, source.envContent) {
		return result, s.failSync(ctx, id, result, sync, actor,
			"COMPOSE_FILE or COMPOSE_ENV_FILES references additional files",
			"the synced .env references additional files via COMPOSE_FILE or COMPOSE_ENV_FILES; enable \"Sync entire directory\" for this sync")
	}

	syncedFiles := singleFileSyncedFilesInternal(sync, source)

	project, err := s.getOrCreateProjectInternal(ctx, sync, id, source.composeContent, source.envContent, source.overrideContent, source.overrideFileName, result, actor)
	if err != nil {
		if errors.Is(err, common.ErrRedeployAfterSyncFailed) {
			s.markSyncRedeployFailedInternal(ctx, sync, id, source.commitHash, syncedFiles, err, actor, result)
		}
		return result, err
	}

	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// buildSwarmStackDeployRequestInternal assembles the deploy request for a Git Sync that targets
// a Swarm stack. WithRegistryAuth is always set so the Git Sync path resolves stored registry
// credentials the same way a direct stack deploy does. Resolution happens per image at deploy
// time and yields nothing unless a configured container registry matches that image's host, so
// stacks built only from public images are unaffected.
func buildSwarmStackDeployRequestInternal(sync *projectpkg.GitOpsSync, source *preparedSyncSource, overrideContent, envContent string, swarmFiles []swarmtypes.SyncFile) swarmtypes.StackDeployRequest {
	return swarmtypes.StackDeployRequest{
		Name:             sync.ProjectName,
		ComposeContent:   source.composeContent,
		OverrideContent:  overrideContent,
		EnvContent:       envContent,
		Files:            swarmFiles,
		Prune:            true,
		WithRegistryAuth: true,
		WorkingDir:       filepath.Dir(filepath.Join(source.repoPath, sync.ComposePath)),
	}
}

// performSwarmStackSyncInternal executes a single file sync targeted at a Swarm Stack
func (
	s *Service,
) performSwarmStackSyncInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id string,
	actor user.Actor,
	result *gitops.SyncResult,
	source *preparedSyncSource,
) (
	*gitops.SyncResult,
	error,
) {
	slog.InfoContext(ctx, "Deploying Swarm Stack from GitOps sync", "syncId", id, "stackName", sync.ProjectName)

	if s.swarmService == nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Swarm service is unavailable", "swarm service is unavailable")
	}

	envContent := ""
	if source.envContent != nil {
		envContent = *source.envContent
	}

	overrideContent := ""
	if source.overrideContent != nil {
		overrideContent = *source.overrideContent
	}

	var syncFiles []projects.SyncFile
	if sync.SyncDirectory {
		files, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
		if err != nil {
			return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
		}
		syncFiles = files
	}

	swarmFiles := make([]swarmtypes.SyncFile, len(syncFiles))
	syncedFiles := make([]string, 0, len(syncFiles))
	for i, f := range syncFiles {
		swarmFiles[i] = swarmtypes.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
		}
		syncedFiles = append(syncedFiles, f.RelativePath)
	}

	req := buildSwarmStackDeployRequestInternal(sync, source, overrideContent, envContent, swarmFiles)

	if _, err := s.swarmService.DeployStack(ctx, sync.EnvironmentID, req); err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to deploy swarm stack", err.Error())
	}

	if len(syncedFiles) == 0 {
		syncedFiles = singleFileSyncedFilesInternal(sync, source)
	}
	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully deployed swarm stack %s from %s", sync.ProjectName, sync.ComposePath)

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git sync completed for stack",
		Description:   fmt.Sprintf("Successfully synced '%s' to swarm stack '%s'", sync.Name, sync.ProjectName),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})

	slog.InfoContext(ctx, "GitOps swarm stack sync completed", "syncId", id, "stack", sync.ProjectName)
	return result, nil
}

// redeployIfRunningAfterSync redeploys a project when it is already running,
// or when the sync has RedeployAfterSync enabled (in which case a stopped
// project is redeployed too, mirroring Portainer's "Re-pull image" stack
// option). Returns a common.ErrRedeployAfterSyncFailed when the redeploy
// itself fails; callers surface that on the sync row's LastSyncError.
//
// When neither condition holds — project stopped and RedeployAfterSync off —
// RedeployProject is never called (Arcane does not start a stopped project on
// its behalf by default), which also means the image pull that RedeployProject
// performs as a side effect never happens. Left alone, that can leave a
// stopped container referencing an image tag that was since re-pointed or
// pruned upstream, with no local copy to fall back on at next start. If the
// sync opted into PullImageAfterSync, pull the image here instead so it stays
// available even while the project is stopped.
func (s *Service) redeployIfRunningAfterSync(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor user.Actor, syncMode string) error {
	details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{})
	running := err == nil && (details.Status == string(projectpkg.ProjectStatusRunning) || details.Status == string(projectpkg.ProjectStatusPartiallyRunning))

	if !running && !sync.RedeployAfterSync {
		return s.pullImageAfterSyncIfConfiguredInternal(ctx, sync, project, actor)
	}

	slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "syncMode", syncMode, "projectName", project.Name, "projectId", project.ID, "wasRunning", running)
	if redeployProjectErr := s.projectService.RedeployProject(ctx, project.ID, actor, nil); redeployProjectErr != nil {
		slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "syncMode", syncMode, "error", redeployProjectErr, "projectId", project.ID)
		return common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("redeploy failed: %w", redeployProjectErr))
	}
	return nil
}

// pullImageAfterSyncIfConfiguredInternal pulls each service's image for a
// project that Git sync changed but did not redeploy (because it wasn't
// running), when the sync has PullImageAfterSync enabled. Best-effort: a pull
// failure is logged but never fails the sync itself, mirroring how
// RedeployProject treats its own pre-deploy pull.
func (s *Service) pullImageAfterSyncIfConfiguredInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor user.Actor) error {
	if !sync.PullImageAfterSync {
		return nil
	}

	credentials, cerr := s.projectService.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		slog.WarnContext(ctx, "failed to resolve registry credentials for post-sync pull", "error", cerr, "projectId", project.ID)
	}
	slog.InfoContext(ctx, "Pulling project images after Git sync (project not running)", "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.PullProjectImages(ctx, project.ID, io.Discard, actor, credentials); err != nil {
		slog.ErrorContext(ctx, "failed to pull project images after Git sync", "error", err, "projectId", project.ID)
		return common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("post-sync image pull failed: %w", err))
	}
	return nil
}

// logSyncSuccess records the Git sync completion event once the filesystem and
// sync-status updates have already succeeded.
func (s *Service) logSyncSuccess(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor user.Actor) {
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git sync completed",
		Description:   fmt.Sprintf("Successfully synced '%s' to project '%s'", sync.Name, project.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})
}

func (s *Service) updateSyncStatus(ctx context.Context, id, status, errorMsg, commitHash string) {
	now := time.Now()
	updates := map[string]any{
		"last_sync_at":     now,
		"last_sync_status": status,
	}

	updates["last_sync_error"] = kit.Ternary[any](errorMsg != "", errorMsg, nil)

	if commitHash != "" {
		updates["last_sync_commit"] = commitHash
	}

	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status", "error", err, "syncId", id)
	}
}

func (s *Service) failSync(ctx context.Context, id string, result *gitops.SyncResult, sync *projectpkg.GitOpsSync, actor user.Actor, message, errMsg string) error {
	result.Message = message
	result.Error = new(errMsg)
	s.updateSyncStatus(ctx, id, "failed", errMsg, "")
	s.logError(ctx, sync, actor, errMsg)
	return fmt.Errorf("%s", errMsg)
}

// markSyncRedeployFailedInternal records a sync where the file sync wrote
// cleanly but the auto-redeploy that follows failed (typically a pre-deploy
// lifecycle hook returning non-zero). The synced-files list and commit hash
// are preserved on the row so operators can see what reached disk; the
// error message surfaces the redeploy failure on LastSyncError.
func (
	s *Service,
) markSyncRedeployFailedInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id, commitHash string,
	syncedFiles []string,
	redeployErr error,
	actor user.Actor,
	result *gitops.SyncResult,
) {
	errMsg := redeployErr.Error()
	result.Success = false
	result.Message = "Sync wrote files but redeploy failed"
	result.Error = new(errMsg)
	s.updateSyncStatusWithFiles(ctx, id, "failed", errMsg, commitHash, syncedFiles)
	s.logError(ctx, sync, actor, errMsg)
}

func (s *Service) disableAutoSyncForBrokenBindingInternal(ctx context.Context, sync *projectpkg.GitOpsSync) {
	if sync == nil || sync.ID == "" {
		return
	}
	if err := s.db.WithContext(ctx).
		Model(&projectpkg.GitOpsSync{}).
		Where("id = ?", sync.ID).
		Update("auto_sync", false).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to disable GitOps auto-sync after broken project binding", "syncId", sync.ID, "error", err)
	}
	sync.AutoSync = false
	s.unregisterJob(ctx, sync.ID)
}

func (
	s *Service,
) failSyncAndDisableAutoSyncInternal(
	ctx context.Context,
	id string,
	result *gitops.SyncResult,
	sync *projectpkg.GitOpsSync,
	actor user.Actor,
	message string,
	failure error,
) error {
	errMsg := failure.Error()
	_ = s.failSync(ctx, id, result, sync, actor, message, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
	return failure
}

func (s *Service) recordBrokenProjectBindingInternal(ctx context.Context, sync *projectpkg.GitOpsSync, actor user.Actor, err error) {
	if !errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken) || sync == nil {
		return
	}
	errMsg := err.Error()
	s.updateSyncStatus(ctx, sync.ID, "failed", errMsg, "")
	s.logError(ctx, sync, actor, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
}

func (
	s *Service,
) createProjectForSyncInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id, composeContent string,
	envContent, overrideContent *string,
	overrideFileName string,
	result *gitops.SyncResult,
	actor user.Actor,
) (
	*projectpkg.Project,
	error,
) {
	// Use the non-suffixing create: a GitOps sync must never mint a "-N" duplicate.
	// A name collision means a project directory already exists for this name, so the
	// binding is broken — fail loudly and disable auto-sync instead of duplicating.
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, envContent, projecttypes.CreateProjectWorkspaceManifest{}, nil, nil, nil, actor, false)
	if err != nil {
		if errors.Is(err, projects.ErrProjectDirExists) {
			bindingErr := common.Classify(
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

			return nil, s.failSyncAndDisableAutoSyncInternal(ctx, id, result, sync, actor, "GitOps project binding broken", bindingErr)
		}
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to create project", err.Error())
	}

	// Update sync with project ID
	if linkSyncProjectErr := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(map[string]any{
		"project_id": project.ID,
	}).Error; linkSyncProjectErr != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to update sync with project ID", linkSyncProjectErr.Error())
	}

	// Mark project as GitOps-managed
	if markProjectManagedErr := s.db.WithContext(ctx).Model(&projectpkg.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", id).Error; markProjectManagedErr != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to mark project as GitOps-managed", markProjectManagedErr.Error())
	}

	if _, _, applyGitSyncProjectFilesErr := s.projectService.ApplyGitSyncProjectFiles(
		ctx, project.ID, composeContent, envContent, overrideContent, overrideFileName, actor,
	); applyGitSyncProjectFilesErr != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to sync project env files", applyGitSyncProjectFilesErr.Error())
	}

	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)

	// A freshly created project has no containers yet, so it's always
	// "stopped" from RedeployProject's point of view — mirror
	// updateProjectForSyncInternal's post-sync behavior here instead of
	// silently leaving the new project undeployed with no image pulled.
	if sync.RedeployAfterSync {
		slog.InfoContext(ctx, "Redeploying newly created project from Git sync", "projectName", project.Name, "projectId", project.ID)
		if redeployProjectErr := s.projectService.RedeployProject(ctx, project.ID, actor, nil); redeployProjectErr != nil {
			slog.ErrorContext(ctx, "Failed to redeploy newly created project after Git sync", "error", redeployProjectErr, "projectId", project.ID)
			return nil, common.Classify(common.ErrRedeployAfterSyncFailed, fmt.Errorf("redeploy failed: %w", redeployProjectErr))
		}
	} else if pullImageAfterSyncIfConfiguredErr := s.pullImageAfterSyncIfConfiguredInternal(ctx, sync, project, actor); pullImageAfterSyncIfConfiguredErr != nil {
		return nil, pullImageAfterSyncIfConfiguredErr
	}

	return project, nil
}

func (
	s *Service,
) getOrCreateProjectInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id, composeContent string,
	envContent, overrideContent *string,
	overrideFileName string,
	result *gitops.SyncResult,
	actor user.Actor,
) (
	*projectpkg.Project,
	error,
) {
	var project *projectpkg.Project

	if sync.ProjectID != nil && *sync.ProjectID != "" {
		var found bool
		var lookupErr error
		project, found, lookupErr = s.projectService.FindProjectByID(ctx, *sync.ProjectID)
		if lookupErr != nil {
			lookupErr = fmt.Errorf("failed to get project %s: %w", *sync.ProjectID, lookupErr)
			return nil, s.failSync(ctx, id, result, sync, actor, "Failed to load existing project", lookupErr.Error())
		}
		if !found {
			err := common.Classify(common.ErrGitOpsSyncProjectBindingBroken, fmt.Errorf("GitOps sync project binding broken: sync %s references missing project %s", sync.ID, *sync.ProjectID))

			slog.WarnContext(ctx, "Existing project not found; GitOps project binding is broken", "projectId", *sync.ProjectID, "syncId", sync.ID)
			return nil, s.failSyncAndDisableAutoSyncInternal(ctx, id, result, sync, actor, "GitOps project binding broken", err)
		}
	}

	if project == nil {
		return s.createProjectForSyncInternal(ctx, sync, id, composeContent, envContent, overrideContent, overrideFileName, result, actor)
	}

	if err := s.updateProjectForSyncInternal(ctx, sync, id, project, composeContent, envContent, overrideContent, overrideFileName, result, actor); err != nil {
		return nil, err
	}
	return project, nil
}

func (
	s *Service,
) updateProjectForSyncInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	id string,
	project *projectpkg.Project,
	composeContent string,
	envContent, overrideContent *string,
	overrideFileName string,
	result *gitops.SyncResult,
	actor user.Actor,
) error {
	_, changed, err := s.projectService.ApplyGitSyncProjectFiles(
		ctx, project.ID, composeContent, envContent, overrideContent, overrideFileName, actor,
	)
	if err != nil {
		return s.failSync(ctx, id, result, sync, actor, "Failed to update project files", err.Error())
	}
	slog.InfoContext(ctx, "Updated project files", "projectName", project.Name, "projectId", project.ID, "changed", changed)
	if !changed {
		return nil
	}
	return s.redeployIfRunningAfterSync(ctx, sync, project, actor, "single-file")
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

// syncProjectDirectoryInternal runs the new directory-sync path end to end:
// stage files, validate the staged tree, then create or update the project.
func (
	s *Service,
) syncProjectDirectoryInternal(
	ctx context.Context,
	sync *projectpkg.GitOpsSync,
	syncFiles []projects.SyncFile,
	actor user.Actor,
) (
	*projectpkg.Project,
	[]string,
	bool,
	bool,
	error,
) {
	stage, err := s.stageDirectorySyncInternal(ctx, sync, syncFiles)
	if err != nil {
		s.recordBrokenProjectBindingInternal(ctx, sync, actor, err)
		return nil, nil, false, false, err
	}
	// The stage must go even after ctx was cancelled: acfs refuses work on a
	// cancelled context.
	defer func() {
		if stage.stagePath != "" {
			_ = acfs.RemoveAll(context.WithoutCancel(ctx), stage.projectsDir, stage.stageLogical)
		}
	}()

	if stage.project == nil {
		project, createDirectorySyncProjectErr := s.createDirectorySyncProjectInternal(ctx, sync, stage, actor)
		if createDirectorySyncProjectErr != nil {
			// A name-collision on create surfaces as a broken-binding error; disable
			// auto-sync so it cannot keep retrying (no-op for other error kinds).
			s.recordBrokenProjectBindingInternal(ctx, sync, actor, createDirectorySyncProjectErr)
			return nil, nil, false, false, createDirectorySyncProjectErr
		}
		return project, stage.syncedFiles, true, true, nil
	}

	project, err := s.updateDirectorySyncProjectInternal(ctx, sync, stage)
	if err != nil {
		return nil, nil, false, false, err
	}
	return project, stage.syncedFiles, false, stage.contentsChanged, nil
}

// stageDirectorySyncInternal builds a temporary project tree that reflects the exact
// repo layout after sync, including cleanup of files removed from the repo. For an
// existing project the tree is sparse: only paths the sync touches are materialized
// and the rest is linked to the live project, so validation never copies it.
func (s *Service) stageDirectorySyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, syncFiles []projects.SyncFile) (stage *stagedDirectorySync, err error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	// The project-root env files are reserved for the three-file override
	// merge applied below by the Project domain rather than a raw
	// overwrite — a raw .env write would silently wipe edits made in Arcane
	// on every sync.
	filteredSyncFiles, gitEnvContent := partitionReservedRootEnvFiles(ctx, syncFiles)

	stageLogical, err := acfs.MkdirTemp(ctx, projectsDir, "/", ".gitops-sync-stage-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer func() {
		if err != nil {
			_ = acfs.RemoveAll(context.WithoutCancel(ctx), projectsDir, stageLogical)
		}
	}()

	stage = &stagedDirectorySync{
		stagePath:       filepath.Join(projectsDir, filepath.FromSlash(strings.TrimPrefix(stageLogical, "/"))),
		stageLogical:    stageLogical,
		projectsDir:     projectsDir,
		composeFileName: filepath.Base(sync.ComposePath),
		syncFiles:       filteredSyncFiles,
		syncedFiles:     make([]string, len(filteredSyncFiles)),
		gitEnvContent:   gitEnvContent,
		contentsChanged: true,
	}
	for i, file := range filteredSyncFiles {
		stage.syncedFiles[i] = file.RelativePath
	}

	// Syncs created before this fix may still have .env recorded as a tracked
	// file. Drop reserved root env files here too, or CleanupRemovedFiles would
	// treat .env as removed-by-git and delete the stage .env before
	// ApplyGitSyncEnvToDirectory gets a chance to read it for the merge.
	stage.oldSyncedFiles = filterReservedRootEnvFiles(sync.SyncedFileList())

	stage.project, err = s.getDirectorySyncProjectInternal(ctx, sync)
	if err != nil {
		return nil, err
	}

	if stage.project != nil {
		staleFiles, staleErr := projects.StaleComposeFiles(ctx, stage.project.Path, stage.composeFileName, stage.syncedFiles)
		if staleErr != nil {
			return nil, fmt.Errorf("failed to detect stale compose files: %w", staleErr)
		}
		stage.backupScope.Paths = slices.Concat(stage.syncedFiles, stage.oldSyncedFiles, staleFiles,
			[]string{projects.EffectiveEnvFileName, projects.GitSourceEnvFileName, projects.OverrideEnvFileName})
		if linkProjectIntoStageErr := linkProjectIntoStage(stage.project.Path, stage.stagePath, stage.backupScope.Paths); linkProjectIntoStageErr != nil {
			return nil, fmt.Errorf("failed to stage current project files: %w", linkProjectIntoStageErr)
		}
		if _, seedStageEnvFromDirErr := seedStageEnvFromDir(ctx, stage.project.Path, projectsDir, stage.stagePath); seedStageEnvFromDirErr != nil {
			return nil, seedStageEnvFromDirErr
		}
		stage.contentsChanged, err = projects.DirectorySyncContentsChanged(ctx, stage.project.Path, filteredSyncFiles, stage.oldSyncedFiles, stage.composeFileName)
		if err != nil {
			return nil, fmt.Errorf("failed to compare staged directory changes: %w", err)
		}
	} else if seedStageEnvFromCandidateDirErr := s.seedStageEnvFromCandidateDirInternal(ctx, sync, projectsDir, stage.stagePath); seedStageEnvFromCandidateDirErr != nil {
		return nil, seedStageEnvFromCandidateDirErr
	}

	preEnvContent, postEnvContent, err := s.applyDirectorySyncInternal(ctx, stage.stagePath, stage)
	if err != nil {
		return nil, err
	}
	if stage.project != nil && projects.EnvContentChanged(preEnvContent, postEnvContent) {
		stage.contentsChanged = true
	}

	stage.serviceCount, err = s.projectService.ValidateComposeDirectory(ctx, sync.ProjectName, stage.stagePath, stage.composeFileName)
	if err != nil {
		return nil, fmt.Errorf("invalid compose file: %w", err)
	}

	return stage, nil
}

// applyDirectorySyncInternal applies the sync to targetPath in the order
// validation saw it: drop files git no longer tracks and stale compose files,
// write the repo files, then run the project-root env merge. It returns the
// effective .env content before and after the merge.
func (s *Service) applyDirectorySyncInternal(ctx context.Context, targetPath string, stage *stagedDirectorySync) (string, string, error) {
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

// seedStageEnvFromCandidateDirInternal copies env files from a pre-existing project
// directory at the conventional path (projectsDir/<sanitized-sync-name>/) into the
// staging directory before initial-sync validation. This lets users pre-seed
// ${VAR} substitutions via a server-side .env when the compose file in git
// expects values that the git repo intentionally does not provide. Only env
// files are touched — other files would conflict with what WriteSyncedDirectory
// is about to lay down from git.
func (s *Service) seedStageEnvFromCandidateDirInternal(ctx context.Context, sync *projectpkg.GitOpsSync, projectsDir, stagePath string) error {
	candidatePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	info, err := os.Stat(candidatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect pre-existing project directory %s: %w", candidatePath, err)
	}
	if !info.IsDir() {
		return nil
	}

	state, err := seedStageEnvFromDir(ctx, candidatePath, projectsDir, stagePath)
	if err != nil {
		return err
	}
	if !state.HasEffective && !state.HasGitSource && !state.HasOverride {
		return nil
	}

	if !state.HasEffective {
		// Only .env.git and/or project.env exist, so derive .env from them.
		merged, mergeErr := projects.BuildEffectiveEnvContent(state.GitContent, state.OverrideContent)
		if mergeErr != nil {
			return fmt.Errorf("build effective env from pre-existing project: %w", mergeErr)
		}
		if writeProjectFileErr := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.EffectiveEnvFileName, merged); writeProjectFileErr != nil {
			return fmt.Errorf("seed stage .env: %w", writeProjectFileErr)
		}
	}

	slog.DebugContext(ctx, "Seeded GitOps stage with pre-existing project env files",
		"candidatePath", candidatePath,
		"hasEffective", state.HasEffective,
		"hasGit", state.HasGitSource,
		"hasOverride", state.HasOverride,
	)
	return nil
}

func (s *Service) lookupProjectByPathInternal(ctx context.Context, projectPath string) (*projectpkg.Project, bool, error) {
	var project projectpkg.Project
	if err := s.db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get project by path %s: %w", projectPath, err)
	}

	return &project, true, nil
}

func (s *Service) findRecoverableManagedProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	var managedProjects []projectpkg.Project
	if err := s.db.WithContext(ctx).
		Where("gitops_managed_by = ?", sync.ID).
		Find(&managedProjects).Error; err != nil {
		return nil, fmt.Errorf("failed to list GitOps-managed projects for sync %s: %w", sync.ID, err)
	}

	matches := make([]projectpkg.Project, 0, len(managedProjects))
	for i := range managedProjects {
		project := managedProjects[i]
		if err := s.projectService.EnsureProjectPathUnderRoot(ctx, &project, true); err != nil {
			return nil, err
		}
		if _, err := s.projectService.ResolveProjectComposeFile(ctx, &project); err != nil {
			if errors.Is(err, common.ErrProjectComposeFileNotFound) {
				continue
			}
			return nil, err
		}
		matches = append(matches, project)
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("multiple GitOps-managed projects match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *Service) findUniqueProjectDirectoryCandidateInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (string, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return "", err
	}

	entries, err := acfs.List(ctx, projectsDir, "/")
	if err != nil {
		return "", fmt.Errorf("failed to list projects directory %s: %w", projectsDir, err)
	}

	composeFileName := strings.TrimSpace(filepath.Base(sync.ComposePath))
	if composeFileName == "" || composeFileName == "." {
		return "", nil
	}

	prefix := projects.SanitizeProjectName(sync.ProjectName)
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		// Adoption never follows symlinks, and acfs reports a symlink as a
		// non-directory, so a plain directory is the only candidate kind.
		if !entry.IsDirectory {
			continue
		}
		// Never adopt one of Arcane's own scratch dirs as a recovery candidate.
		if projects.IsInternalScratchDirName(entry.Name) {
			continue
		}
		if prefix != "" && entry.Name != prefix && !strings.HasPrefix(entry.Name, prefix+"-") {
			continue
		}

		candidatePath := filepath.Join(projectsDir, entry.Name)
		composePath := filepath.Join(candidatePath, composeFileName)
		if composeEntry, statErr := acfs.Stat(ctx, projectsDir, path.Join(entry.Path, composeFileName), true); statErr == nil {
			if !composeEntry.IsDirectory {
				matches = append(matches, candidatePath)
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return "", fmt.Errorf("failed to inspect recovery candidate %s: %w", composePath, statErr)
		}
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple candidate project directories match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *Service) createRecoveredProjectFromDirectoryInternal(ctx context.Context, sync *projectpkg.GitOpsSync, projectPath string) (*projectpkg.Project, error) {
	project := &projectpkg.Project{
		Name:            sync.ProjectName,
		DirName:         new(filepath.Base(projectPath)),
		Path:            projectPath,
		Status:          projectpkg.ProjectStatusUnknown,
		StatusReason:    new("Project recovered from existing GitOps-managed directory"),
		ServiceCount:    0,
		RunningCount:    0,
		GitOpsManagedBy: &sync.ID,
	}

	if serviceCount, err := s.projectService.CountServicesFromCompose(ctx, *project); err == nil {
		project.ServiceCount = serviceCount
	} else {
		slog.WarnContext(ctx, "Failed to count services while recovering GitOps project", "syncId", sync.ID, "path", projectPath, "error", err)
	}

	if err := s.projectService.CreateGitOpsManagedProject(ctx, sync, project, user.Actor{}, false); err != nil {
		return nil, err
	}

	return project, nil
}

func (s *Service) recoverProjectFromDirectoryCandidateInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	projectPath, err := s.findUniqueProjectDirectoryCandidateInternal(ctx, sync)
	if err != nil || projectPath == "" {
		return nil, err
	}

	project, found, err := s.lookupProjectByPathInternal(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	if found {
		if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); ensureProjectPathUnderRootErr != nil {
			return nil, ensureProjectPathUnderRootErr
		}
		if ensureGitOpsProjectLinkedErr := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); ensureGitOpsProjectLinkedErr != nil {
			return nil, ensureGitOpsProjectLinkedErr
		}
		return project, nil
	}

	return s.createRecoveredProjectFromDirectoryInternal(ctx, sync, projectPath)
}

// getDirectorySyncProjectInternal resolves the linked project for a sync when one
// exists, while tolerating deleted/stale project references.
func (s *Service) getDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	if sync == nil {
		return nil, nil
	}

	establishedBinding := hasEstablishedProjectBinding(sync)
	if establishedBinding {
		project, found, err := s.projectService.FindProjectByID(ctx, *sync.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to get project %s: %w", *sync.ProjectID, err)
		}
		if found {
			if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); ensureProjectPathUnderRootErr != nil {
				return nil, ensureProjectPathUnderRootErr
			}
			if ensureGitOpsProjectLinkedErr := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); ensureGitOpsProjectLinkedErr != nil {
				return nil, ensureGitOpsProjectLinkedErr
			}
			return project, nil
		}

		slog.WarnContext(ctx, "Existing project not found, attempting recovery", "projectId", *sync.ProjectID, "syncId", sync.ID)
	}

	project, findRecoverableManagedProjectErr := s.findRecoverableManagedProjectInternal(ctx, sync)
	if findRecoverableManagedProjectErr != nil {
		if establishedBinding {
			return nil, common.Classify(
				common.ErrGitOpsSyncProjectBindingBroken,
				fmt.Errorf(
					"GitOps sync project binding broken: sync %s references missing project %s: %w",
					sync.ID,
					*sync.ProjectID,
					findRecoverableManagedProjectErr,
				),
			)
		}
		return nil, findRecoverableManagedProjectErr
	}
	if project != nil {
		if err := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); err != nil {
			return nil, err
		}
		return project, nil
	}

	project, findRecoverableManagedProjectErr = s.recoverProjectFromDirectoryCandidateInternal(ctx, sync)
	if findRecoverableManagedProjectErr != nil {
		if establishedBinding {
			return nil, common.Classify(
				common.ErrGitOpsSyncProjectBindingBroken,
				fmt.Errorf(
					"GitOps sync project binding broken: sync %s references missing project %s: %w",
					sync.ID,
					*sync.ProjectID,
					findRecoverableManagedProjectErr,
				),
			)
		}
		return nil, findRecoverableManagedProjectErr
	}
	if project != nil {
		return project, nil
	}

	if establishedBinding {
		return nil, common.Classify(
			common.ErrGitOpsSyncProjectBindingBroken,
			fmt.Errorf(
				"GitOps sync project binding broken: sync %s references missing project %s: no unique recovery candidate was found",
				sync.ID,
				*sync.ProjectID,
			),
		)
	}

	return nil, nil
}

// createDirectorySyncProjectInternal promotes a validated staged tree into a new
// managed project directory and links it back to the Git sync record.
func (s *Service) createDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync, actor user.Actor) (*projectpkg.Project, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	// Non-suffixing create: a GitOps sync must never mint a "-N" duplicate. Any
	// adoptable existing directory was already resolved by getDirectorySyncProjectInternal,
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

// updateDirectorySyncProjectInternal applies a validated stage to the existing
// project path in place so running containers keep their bind-mount inodes; a
// backup scoped to the touched paths allows rollback if promotion fails.
func (s *Service) updateDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync) (*projectpkg.Project, error) {
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

	if _, _, err := s.applyDirectorySyncInternal(ctx, projectPath, stage); err != nil {
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

// updateSyncStatusWithFiles updates sync status including the list of synced files
func (s *Service) updateSyncStatusWithFiles(ctx context.Context, id, status, errorMsg, commitHash string, syncedFiles []string) {
	now := time.Now()
	updates := map[string]any{
		"last_sync_at":     now,
		"last_sync_status": status,
		"synced_files":     projectpkg.EncodeSyncedFiles(syncedFiles),
	}

	updates["last_sync_error"] = kit.Ternary[any](errorMsg != "", errorMsg, nil)

	if commitHash != "" {
		updates["last_sync_commit"] = commitHash
	}

	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status with files", "error", err, "syncId", id)
	}
}

// seedStageEnvFromDir copies the readable project-root env files from
// sourceDir into the stage verbatim and returns the env state it read. A
// permission-locked file is skipped rather than aborting the whole staging
// attempt, matching how the env merge leaves such files untouched.
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

// partitionReservedRootEnvFiles splits syncFiles into the set safe to
// write directly (everything except the reserved root env files) and the
// git-sourced root .env content, if the repo has one. A committed
// .env.git/project.env/.env.global at the project root is dropped rather than
// raw-written: those are Arcane-owned bookkeeping files that must only ever be
// produced by the override merge, never by an untrusted git payload.
func partitionReservedRootEnvFiles(ctx context.Context, syncFiles []projects.SyncFile) ([]projects.SyncFile, *string) {
	filtered := make([]projects.SyncFile, 0, len(syncFiles))
	var gitEnvContent *string
	for _, file := range syncFiles {
		if !isReservedRootEnvFile(file.RelativePath) {
			filtered = append(filtered, file)
			continue
		}
		switch file.RelativePath {
		case projects.EffectiveEnvFileName:
			content := string(file.Content)
			gitEnvContent = &content
		case projects.GlobalEnvFileName:
			slog.WarnContext(ctx, "dropping .env.global from git payload; project-root env files are not raw-written from git and .env.global is not project-scoped",
				"path", file.RelativePath,
			)
		default:
			slog.WarnContext(ctx, "dropping reserved Arcane env file from git payload; will be re-derived from override merge",
				"path", file.RelativePath,
			)
		}
	}
	return filtered, gitEnvContent
}

// isReservedRootEnvFile reports whether relPath is one of the
// project-root env bookkeeping files the override-merge system owns (.env,
// .env.git, project.env, .env.global). These are excluded from the raw
// directory-sync write and routed through ProjectService.ApplyGitSyncEnvToDirectory
// instead. Exact-matching RelativePath intentionally leaves nested env files
// (e.g. svc/.env) in the raw write set — only the project root is
// merge-managed.
func isReservedRootEnvFile(relPath string) bool {
	switch relPath {
	case projects.EffectiveEnvFileName, projects.GitSourceEnvFileName, projects.OverrideEnvFileName, projects.GlobalEnvFileName:
		return true
	default:
		return false
	}
}

// filterReservedRootEnvFiles drops reserved root env file paths from a
// tracked synced-file list. Syncs created before the override-merge migration
// may still have .env recorded from an earlier sync; leaving it in would make
// CleanupRemovedFiles treat .env as removed-by-git and delete the live-copied
// stage .env before ApplyGitSyncEnvToDirectory can read it for the merge.
func filterReservedRootEnvFiles(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if !isReservedRootEnvFile(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// linkProjectIntoStage builds the sparse stage view of an existing
// project: paths in scope are left for the apply step, directories on the way
// to them are recreated, and everything else is exposed through a symlink to
// its live location so validation sees the whole tree without copying it.
// The links are validation-only and never promoted. acfs refuses to create
// symlinks by design, so this is the one place the stage is built with os.
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

func hasEstablishedProjectBinding(sync *projectpkg.GitOpsSync) bool {
	return sync != nil && sync.ProjectID != nil && strings.TrimSpace(*sync.ProjectID) != ""
}
