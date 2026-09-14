package gitops

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultBackupIntervalMinutes = 5
	defaultBackupSaveDebounce    = 10 * time.Second
	backupPushAttempts           = 3
	backupStatusRunning          = "running"
	backupStatusConflict         = "conflict"
	backupAuthorName             = "Arcane"
	backupAuthorEmail            = "arcane@localhost"
	backupCommitFileLines        = 20
)

// backupRuntimeInternal holds the in-memory coordination for backup syncs:
// per-branch write serialization and the save debounce timers.
type backupRuntimeInternal struct {
	mu       sync.Mutex
	branches map[string]*sync.Mutex
	timers   map[string]*time.Timer
	debounce time.Duration
}

func newBackupRuntimeInternal() *backupRuntimeInternal {
	return &backupRuntimeInternal{
		branches: make(map[string]*sync.Mutex),
		timers:   make(map[string]*time.Timer),
		debounce: defaultBackupSaveDebounce,
	}
}

func (r *backupRuntimeInternal) branchLock(repositoryID, branch string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := repositoryID + "\x00" + branch
	lock, ok := r.branches[key]
	if !ok {
		lock = &sync.Mutex{}
		r.branches[key] = lock
	}
	return lock
}

func (r *backupRuntimeInternal) schedule(syncID string, run func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if timer, ok := r.timers[syncID]; ok {
		timer.Stop()
	}
	r.timers[syncID] = time.AfterFunc(r.debounce, func() {
		r.mu.Lock()
		delete(r.timers, syncID)
		r.mu.Unlock()
		run()
	})
}

func (r *backupRuntimeInternal) cancel(syncID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if timer, ok := r.timers[syncID]; ok {
		timer.Stop()
		delete(r.timers, syncID)
	}
}

func normalizeSyncModeInternal(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "", gitops.SyncModeDeploy:
		return gitops.SyncModeDeploy, nil
	case gitops.SyncModeBackup:
		return gitops.SyncModeBackup, nil
	default:
		return "", common.Classify(common.ErrValidation, errors.WithDetails(errors.Errorf("unsupported sync mode %q", mode), "field", "mode"))
	}
}

// backupCreateConfigInternal is the validated backup configuration for a new sync.
type backupCreateConfigInternal struct {
	project      *projectpkg.Project
	directory    string
	paths        []string
	composeFile  string
	backupOnSave bool
}

// prepareBackupCreateInternal validates a backup-mode create request against
// the project, its compose files, and other backups on the same branch.
func (s *GitOpsSyncService) prepareBackupCreateInternal(ctx context.Context, tx *gorm.DB, req gitops.CreateSyncRequest) (*backupCreateConfigInternal, error) {
	if req.HasDeploymentOptions() {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(errors.New("deployment options cannot be set on a backup sync"), "field", "mode"))
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(errors.New("a project is required for a backup sync"), "field", "projectId"))
	}
	project, err := lockProjectForSyncInternal(tx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if project.GitOpsManagedBy != nil && strings.TrimSpace(*project.GitOpsManagedBy) != "" {
		return nil, common.Classify(common.ErrConflict, errors.New("project is deployed from Git; disconnect that sync before backing it up"))
	}
	var existing int64
	if err := tx.Model(&projectpkg.GitOpsSync{}).
		Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, project.ID).
		Count(&existing).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to check existing backups")
	}
	if existing > 0 {
		return nil, common.Classify(common.ErrConflict, errors.New("project already has a Git backup; disconnect it first"))
	}

	directory, err := normalizeBackupDirectoryInternal(req.BackupDirectory)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(err, "field", "backupDirectory"))
	}
	if err := ensureBackupDestinationFreeInternal(tx, "", req.RepositoryID, req.Branch, directory); err != nil {
		return nil, err
	}
	if err := s.projectService.EnsureProjectPathUnderRoot(ctx, project, false); err != nil {
		return nil, err
	}
	composeFile, composeFiles, err := s.backupComposeFilesInternal(ctx, project)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(err, "field", "projectId"))
	}
	paths, err := normalizeBackupPathsInternal(req.BackupPaths)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(err, "field", "backupPaths"))
	}
	if len(paths) == 0 {
		paths = composeFiles
	}
	if !backupSelectionCoversInternal(paths, composeFile) {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(errors.New("the backup selection must include the compose file "+composeFile), "field", "backupPaths"))
	}
	config := &backupCreateConfigInternal{project: project, directory: directory, paths: paths, composeFile: composeFile, backupOnSave: true}
	if req.BackupOnSave != nil {
		config.backupOnSave = *req.BackupOnSave
	}
	return config, nil
}

// lockProjectForSyncInternal loads a project inside tx with a row lock so the
// one-Git-relationship-per-project checks and the sync insert are atomic.
func lockProjectForSyncInternal(tx *gorm.DB, projectID string) (*projectpkg.Project, error) {
	var project projectpkg.Project
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrProjectNotFound
		}
		return nil, errors.WrapIff(err, "failed to get project %s", projectID)
	}
	return &project, nil
}

// ensureBackupDestinationFreeInternal rejects a destination that equals or
// nests inside another backup's destination on the same repository branch.
func ensureBackupDestinationFreeInternal(tx *gorm.DB, excludeSyncID, repositoryID, branch, directory string) error {
	var others []projectpkg.GitOpsSync
	q := tx.
		Select("id", "backup_directory").
		Where("mode = ? AND repository_id = ? AND branch = ?", gitops.SyncModeBackup, repositoryID, branch)
	if excludeSyncID != "" {
		q = q.Where("id <> ?", excludeSyncID)
	}
	if err := q.Find(&others).Error; err != nil {
		return errors.WrapIf(err, "failed to check backup destinations")
	}
	for _, other := range others {
		if backupDirectoriesOverlapInternal(other.BackupDirectory, directory) {
			return common.Classify(common.ErrConflict, errors.Errorf("backup directory %q overlaps with an existing backup at %q on this branch", directory, other.BackupDirectory))
		}
	}
	return nil
}

// applyModeUpdatesInternal validates the mode-specific fields of an update and
// appends the resulting column updates.
func (s *GitOpsSyncService) applyModeUpdatesInternal(ctx context.Context, current *projectpkg.GitOpsSync, req gitops.UpdateSyncRequest, updates map[string]any) error {
	if current.IsBackup() {
		return s.applyBackupUpdateInternal(ctx, current, req, updates)
	}
	if req.HasBackupOptions() {
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("backup options cannot be set on a deployment sync"), "field", "mode"))
	}
	return nil
}

// applyBackupUpdateInternal validates backup-mode update fields and appends the
// resulting column updates.
func (s *GitOpsSyncService) applyBackupUpdateInternal(ctx context.Context, current *projectpkg.GitOpsSync, req gitops.UpdateSyncRequest, updates map[string]any) error {
	if req.HasDeploymentOptions() {
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("deployment options cannot be set on a backup sync"), "field", "mode"))
	}
	repositoryID := current.RepositoryID
	if req.RepositoryID != nil {
		repositoryID = *req.RepositoryID
	}
	branch := current.Branch
	if req.Branch != nil {
		branch = *req.Branch
	}
	if repositoryID != current.RepositoryID || branch != current.Branch {
		if err := ensureBackupDestinationFreeInternal(s.db.WithContext(ctx), current.ID, repositoryID, branch, current.BackupDirectory); err != nil {
			return err
		}
		updates["last_backup_snapshot"] = nil
		updates["backup_conflict"] = false
		updates["backup_failure_reason"] = nil
	}
	if len(req.BackupPaths) > 0 {
		paths, err := normalizeBackupPathsInternal(req.BackupPaths)
		if err != nil {
			return common.Classify(common.ErrValidation, errors.WithDetails(err, "field", "backupPaths"))
		}
		composeFile := strings.TrimPrefix(strings.TrimPrefix(current.ComposePath, current.BackupDirectory), "/")
		if current.ProjectID != nil {
			if project, found, lookupErr := s.lookupProjectByIDInternal(ctx, *current.ProjectID); lookupErr == nil && found {
				if resolved, _, resolveErr := s.backupComposeFilesInternal(ctx, project); resolveErr == nil {
					composeFile = resolved
				}
			}
		}
		if !backupSelectionCoversInternal(paths, composeFile) {
			return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("the backup selection must include the compose file "+composeFile), "field", "backupPaths"))
		}
		updates["backup_paths"] = database.StringSlice(paths)
	}
	if req.BackupOnSave != nil {
		updates["backup_on_save"] = *req.BackupOnSave
	}
	return nil
}

// performBackupInternal snapshots the project, reconciles it against the
// branch, and pushes a commit when the snapshot differs. adopt replaces
// whatever the remote holds in the backup directory.
func (s *GitOpsSyncService) performBackupInternal(ctx context.Context, sync *projectpkg.GitOpsSync, actor common.User, result *gitops.SyncResult, adopt bool) (*gitops.SyncResult, error) {
	if sync.Repository == nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureRepository, "Repository not found", errors.New("repository not found"))
	}
	if sync.ProjectID == nil || strings.TrimSpace(*sync.ProjectID) == "" {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project not linked", errors.New("backup sync has no project"))
	}
	project, found, err := s.lookupProjectByIDInternal(ctx, *sync.ProjectID)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Failed to load project", err)
	}
	if !found {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project not found", errors.Errorf("project %s no longer exists", *sync.ProjectID))
	}
	if err := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project directory is unavailable", err)
	}
	authConfig, err := s.repoService.GetAuthConfig(ctx, sync.Repository)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureAuth, "Failed to get authentication config", err)
	}

	startedAt := time.Now()
	s.updateBackupRunningInternal(ctx, sync.ID)

	lock := s.backups.branchLock(sync.RepositoryID, sync.Branch)
	lock.Lock()
	defer lock.Unlock()

	snapshot, err := s.buildBackupSnapshotInternal(ctx, sync, project)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, backupSnapshotFailureReasonInternal(err), "Failed to snapshot project files", err)
	}
	baseline := parseBackupSnapshotInternal(sync.LastBackupSnapshot)

	for attempt := 1; attempt <= backupPushAttempts; attempt++ {
		outcome, retry, err := s.runBackupAttemptInternal(ctx, sync, project, actor, result, authConfig, snapshot, baseline, adopt, startedAt)
		if retry {
			slog.InfoContext(ctx, "git backup push rejected; retrying with a fresh checkout", "syncId", sync.ID, "attempt", attempt, "error", err)
			continue
		}
		return outcome, err
	}
	return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailurePushRejected, "Push rejected by remote", errors.Errorf("another writer kept updating branch %s; giving up after %d attempts", sync.Branch, backupPushAttempts))
}

func (s *GitOpsSyncService) runBackupAttemptInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor common.User, result *gitops.SyncResult, authConfig git.AuthConfig, snapshot *backupSnapshotInternal, baseline map[string]string, adopt bool, startedAt time.Time) (*gitops.SyncResult, bool, error) {
	checkout, err := s.repoService.CheckoutForWrite(ctx, sync.Repository.URL, sync.Branch, authConfig)
	if err != nil {
		return result, false, s.failBackupInternal(ctx, sync, result, actor, backupCheckoutFailureReasonInternal(err), "Failed to prepare repository checkout", err)
	}
	defer func() {
		if cleanupErr := s.repoService.Cleanup(checkout.RepoPath); cleanupErr != nil {
			slog.WarnContext(ctx, "Failed to cleanup repository", "path", checkout.RepoPath, "error", cleanupErr)
		}
	}()

	remote, err := readRemoteBackupStateInternal(ctx, checkout.RepoPath, sync.BackupDirectory)
	if err != nil {
		return result, false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureRepository, "Failed to read remote backup", err)
	}
	analysis := analyzeBackupInternal(snapshot, remote, baseline, adopt)

	switch analysis.state {
	case backupPreviewClean:
		s.recordBackupSuccessInternal(ctx, sync, snapshot, checkout.HeadCommit, false, startedAt)
		result.Success = true
		result.Message = "Repository already contains the current files for project " + project.Name
		return result, false, nil
	case backupPreviewConflict:
		return result, false, s.failBackupNeedsAttentionInternal(ctx, sync, result, actor, gitops.BackupFailureConflict, "Backup files changed in the repository", errors.Errorf("%d backup file(s) changed in the repository since the last backup: %s", len(analysis.conflicts), summarizeBackupChangesInternal(analysis.conflicts)))
	case backupPreviewOccupied:
		return result, false, s.failBackupNeedsAttentionInternal(ctx, sync, result, actor, gitops.BackupFailureDestinationOccupied, "Backup directory already contains unrelated files", errors.Errorf("directory %s on branch %s already contains files that are not an Arcane backup", sync.BackupDirectory, sync.Branch))
	}

	now := time.Now()
	manifest, err := buildBackupManifestInternal(sync, project.Name, snapshot, now)
	if err != nil {
		return result, false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureSnapshot, "Failed to build backup manifest", err)
	}
	request := git.CommitRequest{
		Message:     backupCommitMessageInternal(project.Name, actor, analysis.changes),
		AuthorName:  backupAuthorName,
		AuthorEmail: backupAuthorEmail,
	}
	for _, file := range snapshot.files {
		request.Files = append(request.Files, git.CommitFile{Path: path.Join(sync.BackupDirectory, file.Path), Content: file.Content, Executable: file.Executable})
	}
	request.Files = append(request.Files, git.CommitFile{Path: path.Join(sync.BackupDirectory, gitops.BackupManifestFileName), Content: manifest})
	for _, removed := range analysis.remove {
		request.Remove = append(request.Remove, path.Join(sync.BackupDirectory, removed))
	}

	commit, committed, err := s.repoService.CommitAndPush(ctx, checkout, request, authConfig)
	if err != nil {
		if errors.Is(err, git.ErrPushRejected) {
			return result, true, err
		}
		return result, false, s.failBackupInternal(ctx, sync, result, actor, backupCheckoutFailureReasonInternal(err), "Failed to push backup", err)
	}

	s.recordBackupSuccessInternal(ctx, sync, snapshot, commit, committed, startedAt)
	result.Success = true
	result.Message = fmt.Sprintf("Backed up %d file(s) for project %s to %s", len(snapshot.files), project.Name, sync.BackupDirectory)
	if _, eventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git backup completed",
		Description:   fmt.Sprintf("Backed up project '%s' to '%s' (%s)", project.Name, sync.BackupDirectory, shortCommitInternal(commit)),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	}); eventErr != nil {
		slog.WarnContext(ctx, "Failed to record git backup audit event", "syncId", sync.ID, "commit", commit, "error", eventErr)
	}
	slog.InfoContext(ctx, "Git backup completed", "syncId", sync.ID, "project", project.Name, "commit", commit, "committed", committed)
	return result, false, nil
}

func shortCommitInternal(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func summarizeBackupChangesInternal(changes []gitops.BackupFileChange) string {
	names := make([]string, 0, min(len(changes), backupCommitFileLines))
	for i, change := range changes {
		if i >= backupCommitFileLines {
			names = append(names, "...")
			break
		}
		names = append(names, change.Path)
	}
	return strings.Join(names, ", ")
}

func backupCommitMessageInternal(projectName string, actor common.User, changes []gitops.BackupFileChange) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Back up %s from Arcane", projectName)
	if actor.Username != "" && actor.ID != common.SystemUser.ID {
		fmt.Fprintf(&builder, " (%s)", actor.Username)
	}
	builder.WriteString("\n")
	for i, change := range changes {
		if i >= backupCommitFileLines {
			fmt.Fprintf(&builder, "\n... and %d more", len(changes)-backupCommitFileLines)
			break
		}
		fmt.Fprintf(&builder, "\n%s %s", change.Change, change.Path)
	}
	return builder.String()
}

func backupSnapshotFailureReasonInternal(err error) string {
	switch {
	case errors.Is(err, errBackupUnreadableInternal):
		return gitops.BackupFailureUnreadableFiles
	case errors.Is(err, errBackupLimitsInternal):
		return gitops.BackupFailureLimits
	default:
		return gitops.BackupFailureSnapshot
	}
}

func backupCheckoutFailureReasonInternal(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "authentication") || strings.Contains(message, "authorization") || strings.Contains(message, "ssh") || strings.Contains(message, "credential") {
		return gitops.BackupFailureAuth
	}
	return gitops.BackupFailureRepository
}

func (s *GitOpsSyncService) updateBackupRunningInternal(ctx context.Context, id string) {
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Update("last_sync_status", backupStatusRunning).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to mark git backup running", "error", err, "syncId", id)
	}
}

func (s *GitOpsSyncService) recordBackupSuccessInternal(ctx context.Context, sync *projectpkg.GitOpsSync, snapshot *backupSnapshotInternal, commit string, committed bool, startedAt time.Time) {
	now := time.Now()
	paths := make([]string, 0, len(snapshot.files))
	for _, file := range snapshot.files {
		paths = append(paths, file.Path)
	}
	updates := map[string]any{
		"last_sync_at":          now,
		"last_sync_status":      "success",
		"last_sync_error":       nil,
		"last_backup_snapshot":  marshalBackupSnapshotInternal(snapshot.hashes),
		"synced_files":          marshalSyncedFiles(paths),
		"backup_conflict":       false,
		"backup_failure_reason": nil,
	}
	if commit != "" {
		updates["last_sync_commit"] = commit
	}
	if committed || sync.LastBackupAt == nil {
		updates["last_backup_at"] = now
	}
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to record git backup success", "error", err, "syncId", sync.ID)
	}
	s.clearBackupPendingInternal(ctx, sync.ID, startedAt)
}

// clearBackupPendingInternal clears the pending flag unless a save arrived
// after the run started, in which case the next run picks it up.
func (s *GitOpsSyncService) clearBackupPendingInternal(ctx context.Context, id string, startedAt time.Time) {
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("id = ? AND (backup_pending_since IS NULL OR backup_pending_since <= ?)", id, startedAt).
		Updates(map[string]any{"backup_pending": false, "backup_pending_since": nil}).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to clear git backup pending flag", "error", err, "syncId", id)
	}
}

func (s *GitOpsSyncService) failBackupInternal(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor common.User, reason, message string, failure error) error {
	errMsg := failure.Error()
	result.Message = message
	result.Error = new(errMsg)
	s.updateBackupFailureInternal(ctx, sync.ID, "failed", false, reason, errMsg)
	s.logSyncError(ctx, sync, actor, errMsg)
	return errors.WithMessage(failure, message)
}

func (s *GitOpsSyncService) failBackupNeedsAttentionInternal(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor common.User, reason, message string, failure error) error {
	errMsg := failure.Error()
	result.Message = message
	result.Error = new(errMsg)
	s.updateBackupFailureInternal(ctx, sync.ID, backupStatusConflict, true, reason, errMsg)
	s.logSyncError(ctx, sync, actor, errMsg)
	return common.Classify(common.ErrConflict, errors.WithMessage(failure, message))
}

func (s *GitOpsSyncService) updateBackupFailureInternal(ctx context.Context, id, status string, conflict bool, reason, errMsg string) {
	updates := map[string]any{
		"last_sync_at":          time.Now(),
		"last_sync_status":      status,
		"last_sync_error":       errMsg,
		"backup_conflict":       conflict,
		"backup_failure_reason": reason,
	}
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to record git backup failure", "error", err, "syncId", id)
	}
}

// PreviewBackup reports what the next backup run would commit or why it needs attention.
func (s *GitOpsSyncService) PreviewBackup(ctx context.Context, environmentID, id string) (*gitops.BackupPreview, error) {
	previewCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSyncInternal(previewCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	if syncRecord.ProjectID == nil {
		return nil, common.Classify(common.ErrNotFound, errors.New("backup sync has no project"))
	}
	project, found, err := s.lookupProjectByIDInternal(previewCtx, *syncRecord.ProjectID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, common.ErrProjectNotFound
	}
	if err := s.projectService.EnsureProjectPathUnderRoot(previewCtx, project, false); err != nil {
		return nil, err
	}
	snapshot, err := s.buildBackupSnapshotInternal(previewCtx, syncRecord, project)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, err)
	}
	authConfig, err := s.repoService.GetAuthConfig(previewCtx, syncRecord.Repository)
	if err != nil {
		return nil, err
	}
	checkout, err := s.repoService.CheckoutForWrite(previewCtx, syncRecord.Repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to clone repository")
	}
	defer func() {
		if cleanupErr := s.repoService.Cleanup(checkout.RepoPath); cleanupErr != nil {
			slog.WarnContext(previewCtx, "Failed to cleanup repository", "path", checkout.RepoPath, "error", cleanupErr)
		}
	}()
	remote, err := readRemoteBackupStateInternal(previewCtx, checkout.RepoPath, syncRecord.BackupDirectory)
	if err != nil {
		return nil, err
	}
	analysis := analyzeBackupInternal(snapshot, remote, parseBackupSnapshotInternal(syncRecord.LastBackupSnapshot), false)
	files := make([]string, 0, len(snapshot.files))
	for _, file := range snapshot.files {
		files = append(files, file.Path)
	}
	return &gitops.BackupPreview{
		State:        analysis.state,
		RemoteCommit: checkout.HeadCommit,
		Changes:      analysis.changes,
		Conflicts:    analysis.conflicts,
		Files:        files,
	}, nil
}

// ResolveBackupConflict applies the chosen strategy to a backup that needs attention.
func (s *GitOpsSyncService) ResolveBackupConflict(ctx context.Context, environmentID, id string, req gitops.ResolveBackupConflictRequest, actor common.User) (*gitops.SyncResult, error) {
	if req.Strategy != gitops.BackupConflictUseArcane {
		return nil, common.Classify(common.ErrValidation, errors.WithDetails(errors.Errorf("unsupported strategy %q", req.Strategy), "field", "strategy"))
	}
	syncRecord, err := s.getBackupSyncInternal(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.performSyncAdmittedInternal(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor, true)
}

// SubscribeProjectFileChanges marks backups pending when project files are
// saved and, when automatic backup is on, runs them after a short debounce.
func (s *GitOpsSyncService) SubscribeProjectFileChanges(ctx context.Context) {
	if s.projectService == nil {
		return
	}
	runCtx := s.jobs.Context(ctx)
	s.projectService.FilesChanged().Subscribe(func(projectID string) {
		s.onProjectFilesChangedInternal(runCtx, projectID)
	})
}

func (s *GitOpsSyncService) onProjectFilesChangedInternal(ctx context.Context, projectID string) {
	var syncs []projectpkg.GitOpsSync
	if err := s.db.WithContext(ctx).
		Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, projectID).
		Find(&syncs).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load git backups for changed project", "projectId", projectID, "error", err)
		return
	}
	now := time.Now()
	for i := range syncs {
		syncRecord := syncs[i]
		if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
			Updates(map[string]any{"backup_pending": true, "backup_pending_since": now}).Error; err != nil {
			slog.ErrorContext(ctx, "Failed to mark git backup pending", "syncId", syncRecord.ID, "error", err)
			continue
		}
		if !syncRecord.AutoSync || !syncRecord.BackupOnSave {
			continue
		}
		syncID, environmentID := syncRecord.ID, syncRecord.EnvironmentID
		s.backups.schedule(syncID, func() { s.triggerBackupInternal(ctx, syncID, environmentID) })
	}
}

func (s *GitOpsSyncService) triggerBackupInternal(ctx context.Context, syncID, environmentID string) {
	if s.jobs.Enabled() {
		if _, err := s.jobs.Scheduler().Submit(ctx, schedulertypes.Request{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: "save"}); err != nil {
			slog.ErrorContext(ctx, "git backup admission after save failed", "syncId", syncID, "error", err)
		}
		return
	}
	go func() {
		if _, err := s.PerformSync(ctx, environmentID, syncID, common.SystemUser); err != nil {
			slog.ErrorContext(ctx, "git backup after save failed", "syncId", syncID, "error", err)
		}
	}()
}

// ReconcileInterruptedBackupsOnStartup turns backups left in the running state
// by a restart into pending failures so they are retried or surfaced.
func (s *GitOpsSyncService) ReconcileInterruptedBackupsOnStartup(ctx context.Context) error {
	err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("mode = ? AND last_sync_status = ?", gitops.SyncModeBackup, backupStatusRunning).
		Updates(map[string]any{
			"last_sync_status":      "failed",
			"last_sync_error":       "backup was interrupted by a restart",
			"backup_failure_reason": gitops.BackupFailureRepository,
			"backup_pending":        true,
			"backup_pending_since":  time.Now(),
		}).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.WrapIf(err, "failed to reconcile interrupted git backups")
	}
	return nil
}
