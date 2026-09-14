package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"go.getarcane.app/acfs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultBackupIntervalMinutes = 5
	defaultBackupSaveDebounce    = 10 * time.Second
	backupPushAttempts           = 3
	backupStatusRunning          = "running"
	backupStatusConflict         = "conflict"
	backupCommitFileLines        = 20
)

// backupRuntimeInternal holds the in-memory coordination for backup syncs
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

// prepareBackupCreateInternal validates a backup create request against the project and other backups on the branch.
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

	directory, err := utils.NormalizeRelativePath(req.BackupDirectory)
	if err == nil && slices.Contains(strings.Split(directory, "/"), ".git") {
		err = errors.New("backup directory must not contain a .git segment")
	}
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

// lockProjectForSyncInternal row-locks the project so the one-Git-relationship check and the insert are atomic.
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

// ensureBackupDestinationFreeInternal rejects a destination that overlaps another backup on the same branch.
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
		if other.BackupDirectory == directory || strings.HasPrefix(other.BackupDirectory, directory+"/") || strings.HasPrefix(directory, other.BackupDirectory+"/") {
			return common.Classify(common.ErrConflict, errors.Errorf("backup directory %q overlaps with an existing backup at %q on this branch", directory, other.BackupDirectory))
		}
	}
	return nil
}

// applyModeUpdatesInternal validates mode-specific update fields and appends the column updates.
func (s *GitOpsSyncService) applyModeUpdatesInternal(ctx context.Context, current *projectpkg.GitOpsSync, req gitops.UpdateSyncRequest, updates map[string]any) error {
	if !current.IsBackup() {
		if req.HasBackupOptions() {
			return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("backup options cannot be set on a deployment sync"), "field", "mode"))
		}
		return nil
	}
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

// performBackupInternal snapshots the project and pushes a commit when it differs; adopt overwrites the remote.
func (s *GitOpsSyncService) performBackupInternal(ctx context.Context, sync *projectpkg.GitOpsSync, actor common.User, result *gitops.SyncResult, adopt bool) (*gitops.SyncResult, error) {
	if sync.Repository == nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureRepository, "Repository not found", errors.New("repository not found"), false)
	}
	if sync.ProjectID == nil || strings.TrimSpace(*sync.ProjectID) == "" {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project not linked", errors.New("backup sync has no project"), false)
	}
	project, found, err := s.lookupProjectByIDInternal(ctx, *sync.ProjectID)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Failed to load project", err, false)
	}
	if !found {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project not found", errors.Errorf("project %s no longer exists", *sync.ProjectID), false)
	}
	if err := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureProjectMissing, "Project directory is unavailable", err, false)
	}
	authConfig, err := s.repoService.GetAuthConfig(ctx, sync.Repository)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureAuth, "Failed to get authentication config", err, false)
	}
	identity, err := s.repoService.GetCommitIdentity(ctx, sync.Repository)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureAuth, "Failed to load commit identity", err, false)
	}

	startedAt := time.Now()
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Update("last_sync_status", backupStatusRunning).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to mark git backup running", "error", err, "syncId", sync.ID)
	}

	lock := s.backups.branchLock(sync.RepositoryID, sync.Branch)
	lock.Lock()
	defer lock.Unlock()

	snapshot, err := s.buildBackupSnapshotInternal(ctx, sync, project)
	if err != nil {
		return result, s.failBackupInternal(ctx, sync, result, actor, backupFailureReasonInternal(err), "Failed to snapshot project files", err, false)
	}
	baseline := parseBackupSnapshotInternal(sync.LastBackupSnapshot)

	for attempt := 1; attempt <= backupPushAttempts; attempt++ {
		retry, err := s.commitBackupInternal(ctx, sync, project, actor, result, authConfig, identity, snapshot, baseline, adopt, startedAt)
		if retry {
			slog.InfoContext(ctx, "git backup push rejected; retrying with a fresh checkout", "syncId", sync.ID, "attempt", attempt, "error", err)
			continue
		}
		return result, err
	}
	return result, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailurePushRejected, "Push rejected by remote", errors.Errorf("another writer kept updating branch %s; giving up after %d attempts", sync.Branch, backupPushAttempts), false)
}

// commitBackupInternal runs one checkout-analyze-push attempt; retry reports a rejected push worth repeating.
func (s *GitOpsSyncService) commitBackupInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor common.User, result *gitops.SyncResult, authConfig git.AuthConfig, identity git.CommitIdentity, snapshot *backupSnapshotInternal, baseline map[string]string, adopt bool, startedAt time.Time) (bool, error) {
	checkout, err := s.repoService.CheckoutForWrite(ctx, sync.Repository.URL, sync.Branch, authConfig)
	if err != nil {
		return false, s.failBackupInternal(ctx, sync, result, actor, backupFailureReasonInternal(err), "Failed to prepare repository checkout", err, false)
	}
	defer s.repoService.Discard(ctx, checkout.RepoPath)

	analysis, err := analyzeBackupInternal(ctx, checkout.RepoPath, sync.BackupDirectory, snapshot, baseline, adopt)
	if err != nil {
		return false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureRepository, "Failed to read remote backup", err, false)
	}
	switch analysis.state {
	case backupPreviewClean:
		if err := s.recordBackupSuccessInternal(ctx, sync, snapshot, checkout.HeadCommit, false, startedAt); err != nil {
			return false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureSnapshot, "Failed to record backup", err, false)
		}
		result.Success = true
		result.Message = "Repository already contains the current files for project " + project.Name
		return false, nil
	case backupPreviewConflict:
		names := make([]string, 0, backupCommitFileLines+1)
		for _, change := range analysis.conflicts[:min(len(analysis.conflicts), backupCommitFileLines)] {
			names = append(names, change.Path)
		}
		if len(analysis.conflicts) > backupCommitFileLines {
			names = append(names, "...")
		}
		return false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureConflict, "Backup files changed in the repository", errors.Errorf("%d backup file(s) changed in the repository since the last backup: %s", len(analysis.conflicts), strings.Join(names, ", ")), true)
	case backupPreviewOccupied:
		return false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureDestinationOccupied, "Backup directory already contains unrelated files", errors.Errorf("directory %s on branch %s already contains files that are not an Arcane backup", sync.BackupDirectory, sync.Branch), true)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "Back up %s from Arcane", project.Name)
	if actor.Username != "" && actor.ID != common.SystemUser.ID {
		fmt.Fprintf(&message, " (%s)", actor.Username)
	}
	message.WriteString("\n")
	for _, change := range analysis.changes[:min(len(analysis.changes), backupCommitFileLines)] {
		fmt.Fprintf(&message, "\n%s %s", change.Change, change.Path)
	}
	if extra := len(analysis.changes) - backupCommitFileLines; extra > 0 {
		fmt.Fprintf(&message, "\n... and %d more", extra)
	}
	request := git.CommitRequest{
		Message:     message.String(),
		AuthorName:  identity.Name,
		AuthorEmail: identity.Email,
		SignKey:     identity.SignKey,
	}
	for _, file := range snapshot.files {
		request.Files = append(request.Files, git.CommitFile{Path: path.Join(sync.BackupDirectory, file.Path), Content: file.Content, Executable: file.Executable})
	}
	for _, removed := range analysis.remove {
		request.Remove = append(request.Remove, path.Join(sync.BackupDirectory, removed))
	}
	legacyManifest := path.Join(sync.BackupDirectory, legacyBackupManifestFileNameInternal)
	if exists, existsErr := acfs.Exists(ctx, checkout.RepoPath, "/"+legacyManifest); existsErr == nil && exists {
		request.Remove = append(request.Remove, legacyManifest)
	}

	commit, committed, err := s.repoService.CommitAndPush(ctx, checkout, request, authConfig)
	if err != nil {
		if errors.Is(err, git.ErrPushRejected) {
			return true, err
		}
		return false, s.failBackupInternal(ctx, sync, result, actor, backupFailureReasonInternal(err), "Failed to push backup", err, false)
	}

	if err := s.recordBackupSuccessInternal(ctx, sync, snapshot, commit, committed, startedAt); err != nil {
		return false, s.failBackupInternal(ctx, sync, result, actor, gitops.BackupFailureSnapshot, "Failed to record backup", err, false)
	}
	result.Success = true
	result.Message = fmt.Sprintf("Backed up %d file(s) for project %s to %s", len(snapshot.files), project.Name, sync.BackupDirectory)
	if _, eventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git backup completed",
		Description:   fmt.Sprintf("Backed up project '%s' to '%s' (%s)", project.Name, sync.BackupDirectory, commit[:min(len(commit), 12)]),
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
	return false, nil
}

func backupFailureReasonInternal(err error) string {
	switch {
	case errors.Is(err, git.ErrSelectionUnreadable):
		return gitops.BackupFailureUnreadableFiles
	case errors.Is(err, git.ErrSelectionLimits):
		return gitops.BackupFailureLimits
	case errors.Is(err, git.ErrSelectionInvalid):
		return gitops.BackupFailureSnapshot
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "authentication") || strings.Contains(message, "authorization") || strings.Contains(message, "ssh") || strings.Contains(message, "credential") {
		return gitops.BackupFailureAuth
	}
	return gitops.BackupFailureRepository
}

func (s *GitOpsSyncService) recordBackupSuccessInternal(ctx context.Context, sync *projectpkg.GitOpsSync, snapshot *backupSnapshotInternal, commit string, committed bool, startedAt time.Time) error {
	now := time.Now()
	paths := make([]string, 0, len(snapshot.files))
	for _, file := range snapshot.files {
		paths = append(paths, file.Path)
	}
	hashes, err := json.Marshal(snapshot.hashes, json.Deterministic(true))
	if err != nil {
		return errors.WrapIf(err, "failed to encode backup snapshot")
	}
	updates := map[string]any{
		"last_sync_at":          now,
		"last_sync_status":      "success",
		"last_sync_error":       nil,
		"last_backup_snapshot":  string(hashes),
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
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("id = ? AND (backup_pending_since IS NULL OR backup_pending_since <= ?)", sync.ID, startedAt).
		Updates(map[string]any{"backup_pending": false, "backup_pending_since": nil}).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to clear git backup pending flag", "error", err, "syncId", sync.ID)
	}
	return nil
}

// failBackupInternal records the failure on the sync; needsAttention marks it as a conflict the user must resolve.
func (s *GitOpsSyncService) failBackupInternal(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor common.User, reason, message string, failure error, needsAttention bool) error {
	errMsg := failure.Error()
	result.Message = message
	result.Error = new(errMsg)
	status := "failed"
	if needsAttention {
		status = backupStatusConflict
	}
	updates := map[string]any{
		"last_sync_at":          time.Now(),
		"last_sync_status":      status,
		"last_sync_error":       errMsg,
		"backup_conflict":       needsAttention,
		"backup_failure_reason": reason,
	}
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", sync.ID).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to record git backup failure", "error", err, "syncId", sync.ID)
	}
	s.logSyncError(ctx, sync, actor, errMsg)
	if needsAttention {
		return common.Classify(common.ErrConflict, errors.WithMessage(failure, message))
	}
	return errors.WithMessage(failure, message)
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
	defer s.repoService.Discard(previewCtx, checkout.RepoPath)
	analysis, err := analyzeBackupInternal(previewCtx, checkout.RepoPath, syncRecord.BackupDirectory, snapshot, parseBackupSnapshotInternal(syncRecord.LastBackupSnapshot), false)
	if err != nil {
		return nil, err
	}
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

// SubscribeProjectFileChanges marks backups pending on save and runs them after a debounce when auto backup is on.
func (s *GitOpsSyncService) SubscribeProjectFileChanges(ctx context.Context) {
	if s.projectService == nil {
		return
	}
	runCtx := s.jobs.Context(ctx)
	s.projectService.FilesChanged().Subscribe(func(projectID string) {
		var syncs []projectpkg.GitOpsSync
		if err := s.db.WithContext(runCtx).
			Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, projectID).
			Find(&syncs).Error; err != nil {
			slog.ErrorContext(runCtx, "Failed to load git backups for changed project", "projectId", projectID, "error", err)
			return
		}
		now := time.Now()
		for _, syncRecord := range syncs {
			if err := s.db.WithContext(runCtx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
				Updates(map[string]any{"backup_pending": true, "backup_pending_since": now}).Error; err != nil {
				slog.ErrorContext(runCtx, "Failed to mark git backup pending", "syncId", syncRecord.ID, "error", err)
				continue
			}
			if !syncRecord.AutoSync || !syncRecord.BackupOnSave {
				continue
			}
			syncID, environmentID := syncRecord.ID, syncRecord.EnvironmentID
			s.backups.schedule(syncID, func() {
				if s.jobs.Enabled() {
					if _, err := s.jobs.Scheduler().Submit(runCtx, schedulertypes.Request{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: "save"}); err != nil {
						slog.ErrorContext(runCtx, "git backup admission after save failed", "syncId", syncID, "error", err)
					}
					return
				}
				go func() {
					if _, err := s.PerformSync(runCtx, environmentID, syncID, common.SystemUser); err != nil {
						slog.ErrorContext(runCtx, "git backup after save failed", "syncId", syncID, "error", err)
					}
				}()
			})
		}
	})
}

// ReconcileInterruptedBackupsOnStartup turns backups left running by a restart into pending failures.
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

const (
	backupSnapshotMaxRetry               = 3
	legacyBackupManifestFileNameInternal = ".arcane-backup.json"
	backupPreviewClean                   = "clean"
	backupPreviewChanges                 = "changes"
	backupPreviewConflict                = "conflict"
	backupPreviewOccupied                = "destination_occupied"
	backupChangeAdded                    = "added"
	backupChangeModified                 = "modified"
	backupChangeRemoved                  = "removed"
)

// backupSnapshotInternal is the set of project files a backup run commits.
type backupSnapshotInternal struct {
	files  []git.CommitFile
	hashes map[string]string
}

// backupAnalysisInternal is the decision for one backup run.
type backupAnalysisInternal struct {
	state     string
	changes   []gitops.BackupFileChange
	conflicts []gitops.BackupFileChange
	remove    []string
}

func hashBackupContentInternal(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func normalizeBackupPathsInternal(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, 0, len(raw))
	for _, entry := range raw {
		cleaned, err := utils.NormalizeRelativePath(entry)
		if err != nil {
			return nil, errors.WrapIff(err, "invalid backup path %q", entry)
		}
		base := path.Base(cleaned)
		if base == projects.GitSourceEnvFileName || base == projects.GlobalEnvFileName || slices.Contains(strings.Split(cleaned, "/"), ".git") {
			return nil, errors.Errorf("backup path %q is reserved", entry)
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	sort.Strings(normalized)
	return normalized, nil
}

// backupSelectionCoversInternal reports whether the selection includes file directly or via a parent directory.
func backupSelectionCoversInternal(paths []string, file string) bool {
	for _, selected := range paths {
		if selected == file || strings.HasPrefix(file, selected+"/") {
			return true
		}
	}
	return false
}

// backupComposeFilesInternal resolves the primary compose file and sibling overrides as project-relative paths.
func (s *GitOpsSyncService) backupComposeFilesInternal(ctx context.Context, project *projectpkg.Project) (string, []string, error) {
	composePath, err := s.projectService.ResolveProjectComposeFile(ctx, project)
	if err != nil {
		return "", nil, err
	}
	relative, err := filepath.Rel(project.Path, composePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, errors.Errorf("compose file %s is outside the project directory", composePath)
	}
	primary := filepath.ToSlash(relative)
	composeFiles := []string{primary}
	composeDir := path.Dir(primary)
	for _, candidate := range projects.ComposeOverrideFileCandidates() {
		candidatePath := path.Join(composeDir, candidate)
		if exists, existsErr := acfs.Exists(ctx, project.Path, "/"+candidatePath); existsErr == nil && exists {
			composeFiles = append(composeFiles, candidatePath)
		}
	}
	return primary, composeFiles, nil
}

// buildBackupSnapshotInternal reads the selected project files, retrying when they change mid-read.
func (s *GitOpsSyncService) buildBackupSnapshotInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project) (*backupSnapshotInternal, error) {
	paths := []string(sync.BackupPaths)
	if len(paths) == 0 {
		return nil, errors.WrapIf(git.ErrSelectionInvalid, "no files are selected for backup")
	}
	primary, _, err := s.backupComposeFilesInternal(ctx, project)
	if err != nil {
		return nil, errors.WrapIf(git.ErrSelectionInvalid, err.Error())
	}
	maxFiles, maxTotalSize, _ := s.getEffectiveSyncLimits(ctx, sync)
	options := git.CollectOptions{
		MaxFiles:     maxFiles,
		MaxTotalSize: maxTotalSize,
		SkipDir:      projects.IsInternalScratchDirName,
		SkipFile: func(name string) bool {
			return name == projects.GitSourceEnvFileName || name == projects.GlobalEnvFileName || name == projects.EffectiveEnvFileName || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")
		},
	}

	for range backupSnapshotMaxRetry {
		files, err := git.CollectFiles(ctx, project.Path, paths, options)
		if err != nil {
			return nil, err
		}
		snapshot := &backupSnapshotInternal{files: files, hashes: make(map[string]string, len(files))}
		for _, file := range files {
			snapshot.hashes[file.Path] = hashBackupContentInternal(file.Content)
		}
		if _, ok := snapshot.hashes[primary]; !ok {
			return nil, errors.WrapIff(git.ErrSelectionInvalid, "compose file %s is not included in the backup selection", primary)
		}

		stable := true
		for _, file := range files {
			content, err := acfs.ReadFile(ctx, project.Path, "/"+file.Path)
			if err != nil {
				return nil, errors.WrapIff(git.ErrSelectionUnreadable, "cannot re-read %s: %v", file.Path, err)
			}
			if hashBackupContentInternal(content) != snapshot.hashes[file.Path] {
				stable = false
				break
			}
		}
		if stable {
			return snapshot, nil
		}
	}
	return nil, errors.WrapIf(git.ErrSelectionInvalid, "project files changed while the backup snapshot was being taken")
}

// analyzeBackupInternal compares the snapshot with the checkout; baseline is the last push and the only proof the directory is ours.
func analyzeBackupInternal(ctx context.Context, repoPath, directory string, snapshot *backupSnapshotInternal, baseline map[string]string, adopt bool) (backupAnalysisInternal, error) {
	analysis := backupAnalysisInternal{conflicts: []gitops.BackupFileChange{}}
	entries, err := acfs.List(ctx, repoPath, "/"+directory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return analysis, errors.WrapIf(err, "failed to inspect remote backup directory")
	}
	occupied := err == nil && len(entries) > 0
	remote := make(map[string]string)
	if occupied {
		for _, owned := range []map[string]string{baseline, snapshot.hashes} {
			for file := range owned {
				if _, done := remote[file]; done {
					continue
				}
				content, readErr := acfs.ReadFile(ctx, repoPath, "/"+path.Join(directory, file))
				if readErr != nil {
					if errors.Is(readErr, fs.ErrNotExist) {
						continue
					}
					return analysis, errors.WrapIff(readErr, "failed to read remote backup file %s", file)
				}
				remote[file] = hashBackupContentInternal(content)
			}
		}
	}

	analysis.changes = diffBackupHashesInternal(snapshot.hashes, remote)
	for file := range remote {
		if _, ok := snapshot.hashes[file]; !ok {
			analysis.remove = append(analysis.remove, file)
		}
	}
	sort.Strings(analysis.remove)

	switch {
	case len(analysis.changes) == 0:
		analysis.state = backupPreviewClean
	case adopt:
		analysis.state = backupPreviewChanges
	case baseline == nil && occupied:
		analysis.state = backupPreviewOccupied
	case baseline == nil:
		analysis.state = backupPreviewChanges
	default:
		analysis.conflicts = diffBackupHashesInternal(remote, baseline)
		analysis.state = backupPreviewChanges
		if len(analysis.conflicts) > 0 {
			analysis.state = backupPreviewConflict
		}
	}
	return analysis, nil
}

func diffBackupHashesInternal(local, remote map[string]string) []gitops.BackupFileChange {
	var changes []gitops.BackupFileChange
	for file, hash := range local {
		remoteHash, ok := remote[file]
		switch {
		case !ok:
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeAdded})
		case remoteHash != hash:
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeModified})
		}
	}
	for file := range remote {
		if _, ok := local[file]; !ok {
			changes = append(changes, gitops.BackupFileChange{Path: file, Change: backupChangeRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []gitops.BackupFileChange{}
	}
	return changes
}

func parseBackupSnapshotInternal(raw *string) map[string]string {
	if raw == nil || *raw == "" {
		return nil
	}
	var hashes map[string]string
	if err := json.Unmarshal([]byte(*raw), &hashes); err != nil {
		return nil
	}
	return hashes
}

const defaultBackupHistoryLimit = 20

func (s *GitOpsSyncService) getBackupSyncInternal(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	if !syncRecord.IsBackup() {
		return nil, common.Classify(common.ErrBadRequest, errors.New("sync does not back up to Git"))
	}
	if syncRecord.Repository == nil {
		return nil, common.Classify(common.ErrNotFound, errors.New("repository not found"))
	}
	return syncRecord, nil
}

// cloneBackupBranchInternal clones the backup branch for read-only inspection
func (s *GitOpsSyncService) cloneBackupBranchInternal(ctx context.Context, syncRecord *projectpkg.GitOpsSync) (string, func(), error) {
	authConfig, err := s.repoService.GetAuthConfig(ctx, syncRecord.Repository)
	if err != nil {
		return "", func() {}, err
	}
	checkout, err := s.repoService.CheckoutForWrite(ctx, syncRecord.Repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return "", func() {}, errors.WrapIf(err, "failed to clone repository")
	}
	cleanup := func() { s.repoService.Discard(ctx, checkout.RepoPath) }
	if !checkout.BranchExists {
		return "", cleanup, nil
	}
	return checkout.RepoPath, cleanup, nil
}

// GetBackupHistory lists revisions that touched the backup directory.
func (s *GitOpsSyncService) GetBackupHistory(ctx context.Context, environmentID, id string, limit int) (*gitops.BackupHistoryResponse, error) {
	historyCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSyncInternal(historyCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	repoPath, cleanup, err := s.cloneBackupBranchInternal(historyCtx, syncRecord)
	defer cleanup()
	if err != nil {
		return nil, err
	}
	response := &gitops.BackupHistoryResponse{Entries: []gitops.BackupHistoryEntry{}}
	if repoPath == "" {
		return response, nil
	}
	if limit <= 0 {
		limit = defaultBackupHistoryLimit
	}
	entries, err := s.repoService.DirectoryHistory(historyCtx, repoPath, syncRecord.BackupDirectory, limit)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		response.Entries = append(response.Entries, gitops.BackupHistoryEntry{Commit: entry.Hash, Author: entry.Author, Message: entry.Message, Date: entry.Date, Files: entry.Files})
	}
	return response, nil
}

// GetBackupRevision returns one revision with per-file diffs inside the backup directory.
func (s *GitOpsSyncService) GetBackupRevision(ctx context.Context, environmentID, id, commit string) (*gitops.BackupRevision, error) {
	commit = strings.TrimSpace(commit)
	revisionCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSyncInternal(revisionCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	repoPath, cleanup, err := s.cloneBackupBranchInternal(revisionCtx, syncRecord)
	defer cleanup()
	if err != nil {
		return nil, err
	}
	if repoPath == "" {
		return nil, common.Classify(common.ErrNotFound, errors.New("revision not found"))
	}
	entry, diffs, err := s.repoService.CommitDiff(revisionCtx, repoPath, commit, syncRecord.BackupDirectory)
	if errors.Is(err, git.ErrInvalidCommit) {
		return nil, common.Classify(common.ErrBadRequest, err)
	}
	if err != nil {
		return nil, common.Classify(common.ErrNotFound, err)
	}
	revision := &gitops.BackupRevision{
		Entry: gitops.BackupHistoryEntry{Commit: entry.Hash, Author: entry.Author, Message: entry.Message, Date: entry.Date, Files: entry.Files},
		Diffs: []gitops.BackupFileDiff{},
	}
	for _, diff := range diffs {
		revision.Diffs = append(revision.Diffs, gitops.BackupFileDiff{Path: diff.Path, Patch: diff.Patch})
	}
	return revision, nil
}
