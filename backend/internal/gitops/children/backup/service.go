// Package backup commits saved project files to a Git branch and reports the
// remote backup state, history and revisions.
package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
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

	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

// Service runs backup syncs. Branch locks, sync limits and error events stay
// with the parent and arrive as callbacks.
type Service struct {
	db             *database.DB
	repoService    *gitrepo.GitRepositoryService
	projectService *projectpkg.ProjectService
	eventService   *event.EventService
	limits         func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64)
	logError       func(context.Context, *projectpkg.GitOpsSync, types.Actor, string)
	branchLock     func(repositoryID, branch string) *sync.Mutex
}

func New(
	db *database.DB,
	repoService *gitrepo.GitRepositoryService,
	projectService *projectpkg.ProjectService,
	eventService *event.EventService,
	limits func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64),
	logError func(context.Context, *projectpkg.GitOpsSync, types.Actor, string),
	branchLock func(repositoryID, branch string) *sync.Mutex,
) *Service {
	return &Service{
		db:             db,
		repoService:    repoService,
		projectService: projectService,
		eventService:   eventService,
		limits:         limits,
		logError:       logError,
		branchLock:     branchLock,
	}
}

// ApplyCreate validates a backup create request and fills the backup fields of
// the new sync inside the caller's transaction.
func (s *Service) ApplyCreate(ctx context.Context, tx *gorm.DB, req types.GitOpsSync, submitted map[string]bool, syncRecord *projectpkg.GitOpsSync) error {
	backupConfig, err := s.prepareBackupCreate(ctx, tx, req)
	if err != nil {
		return err
	}
	syncRecord.ProjectID = &backupConfig.project.ID
	syncRecord.ProjectName = backupConfig.project.Name
	syncRecord.TargetType = "project"
	syncRecord.BackupDirectory = backupConfig.directory
	syncRecord.ComposePath = path.Join(backupConfig.directory, backupConfig.composeFile)
	syncRecord.BackupPaths = database.StringSlice(backupConfig.paths)
	syncRecord.BackupOnSave = kit.Ternary(submitted["backupOnSave"], req.BackupOnSave, true)
	syncRecord.AutoSync = kit.Ternary(submitted["autoSync"], req.AutoSync, true)
	syncRecord.SyncInterval = kit.Ternary(submitted["syncInterval"], req.SyncInterval, DefaultBackupIntervalMinutes)
	return nil
}

// backupCreateConfigInternal is the validated backup configuration for a new sync.
type backupCreateConfigInternal struct {
	project     *projectpkg.Project
	directory   string
	paths       []string
	composeFile string
}

// prepareBackupCreate validates a backup create request against the project and other backups on the branch.
func (s *Service) prepareBackupCreate(ctx context.Context, tx *gorm.DB, req types.GitOpsSync) (*backupCreateConfigInternal, error) {
	if strings.TrimSpace(kit.FromPtr(req.ProjectID)) == "" {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "projectId", Err: errors.New("a project is required for a backup sync")})
	}
	project, err := projectpkg.LockProjectForSync(tx, kit.FromPtr(req.ProjectID))
	if err != nil {
		return nil, err
	}
	if project.GitOpsManagedBy != nil && strings.TrimSpace(*project.GitOpsManagedBy) != "" {
		return nil, common.Classify(common.ErrConflict, errors.New("project is deployed from Git; disconnect that sync before backing it up"))
	}
	var existing int64
	if countBackupsErr := tx.Model(&projectpkg.GitOpsSync{}).
		Where("mode = ? AND project_id = ?", types.GitOpsSyncModeBackup, project.ID).
		Count(&existing).Error; countBackupsErr != nil {
		return nil, fmt.Errorf("failed to check existing backups: %w", countBackupsErr)
	}
	if existing > 0 {
		return nil, common.Classify(common.ErrConflict, errors.New("project already has a Git backup; disconnect it first"))
	}

	directory, err := kit.NormalizeRelativePath(req.BackupDirectory)
	if err == nil && slices.Contains(strings.Split(directory, "/"), ".git") {
		err = errors.New("backup directory must not contain a .git segment")
	}
	if err != nil {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "backupDirectory", Err: err})
	}
	if ensureBackupDestinationFreeErr := ensureBackupDestinationFree(tx, "", req.RepositoryID, req.Branch, directory); ensureBackupDestinationFreeErr != nil {
		return nil, ensureBackupDestinationFreeErr
	}
	if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, false); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}
	composeFile, _, err := s.backupComposeFiles(ctx, project)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "projectId", Err: err})
	}
	paths, err := normalizeBackupPaths(req.BackupPaths)
	if err != nil {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "backupPaths", Err: err})
	}
	return &backupCreateConfigInternal{
		project:     project,
		directory:   directory,
		paths:       paths,
		composeFile: composeFile,
	}, nil
}

// ApplyModeUpdates validates mode-specific update fields and appends the column updates.
func (s *Service) ApplyModeUpdates(ctx context.Context, current *projectpkg.GitOpsSync, req types.GitOpsSync, submitted map[string]bool, updates map[string]any) error {
	if current.Mode != types.GitOpsSyncModeBackup {
		return nil
	}
	repositoryID := kit.Ternary(submitted["repositoryId"], req.RepositoryID, current.RepositoryID)
	branch := kit.Ternary(submitted["branch"], req.Branch, current.Branch)
	if repositoryID != current.RepositoryID || branch != current.Branch {
		if err := ensureBackupDestinationFree(s.db.WithContext(ctx), current.ID, repositoryID, branch, current.BackupDirectory); err != nil {
			return err
		}
		updates["last_backup_snapshot"] = nil
		updates["backup_conflict"] = false
		updates["backup_failure_reason"] = nil
	}
	if submitted["backupPaths"] {
		paths, err := normalizeBackupPaths(req.BackupPaths)
		if err != nil {
			return common.Classify(common.ErrValidation, &types.FieldError{Field: "backupPaths", Err: err})
		}
		updates["backup_paths"] = database.StringSlice(paths)
	}
	if submitted["backupOnSave"] {
		updates["backup_on_save"] = req.BackupOnSave
	}
	return nil
}

// Perform snapshots the project and pushes a commit when it differs; adopt overwrites the remote.
func (s *Service) Perform(ctx context.Context, localSync *projectpkg.GitOpsSync, actor types.Actor, result *types.GitOpsSyncResult, adopt bool) (*types.GitOpsSyncResult, error) {
	if localSync.Repository == nil {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureRepository, "Repository not found", errors.New("repository not found"), false)
	}
	if localSync.ProjectID == nil || strings.TrimSpace(*localSync.ProjectID) == "" {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project not linked", errors.New("backup sync has no project"), false)
	}
	project, found, err := s.projectService.FindProjectByID(ctx, *localSync.ProjectID)
	if err != nil {
		err = fmt.Errorf("failed to get project %s: %w", *localSync.ProjectID, err)
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Failed to load project", err, false)
	}
	if !found {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project not found", fmt.Errorf("project %s no longer exists", *localSync.ProjectID), false)
	}
	if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); ensureProjectPathUnderRootErr != nil {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project directory is unavailable", ensureProjectPathUnderRootErr, false)
	}
	authConfig, err := s.repoService.GetAuthConfig(ctx, localSync.Repository)
	if err != nil {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureAuth, "Failed to get authentication config", err, false)
	}
	identity, err := s.repoService.GetCommitIdentity(ctx, localSync.Repository)
	if err != nil {
		return result, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureAuth, "Failed to load commit identity", err, false)
	}

	startedAt := time.Now()
	markBackupRunningErr := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("id = ?", localSync.ID).Update("last_sync_status", BackupStatusRunning).Error
	if markBackupRunningErr != nil {
		slog.ErrorContext(ctx, "Failed to mark git backup running", "error", markBackupRunningErr, "syncId", localSync.ID)
	}

	lock := s.branchLock(localSync.RepositoryID, localSync.Branch)
	lock.Lock()
	defer lock.Unlock()

	snapshot, err := s.buildBackupSnapshot(ctx, localSync, project)
	if err != nil {
		return result, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to snapshot project files", err, false)
	}
	baseline := localSync.BackupSnapshot()

	for attempt := 1; attempt <= BackupPushAttempts; attempt++ {
		retry, commitBackupErr := s.commitBackup(ctx, localSync, project, actor, result, authConfig, identity, snapshot, baseline, adopt, startedAt)
		if retry {
			slog.InfoContext(ctx, "git backup push rejected; retrying with a fresh checkout", "syncId", localSync.ID, "attempt", attempt, "error", commitBackupErr)
			continue
		}
		return result, commitBackupErr
	}
	return result, s.failBackup(
		ctx,
		localSync,
		result,
		actor,
		types.GitOpsBackupFailurePushRejected,
		"Push rejected by remote",
		fmt.Errorf(
			"another writer kept updating branch %s; giving up after %d attempts",
			localSync.Branch,
			BackupPushAttempts,
		),
		false,
	)
}

// commitBackup runs one checkout-analyze-push attempt; retry reports a rejected push worth repeating.
func (
	s *Service,
) commitBackup(
	ctx context.Context,
	localSync *projectpkg.GitOpsSync,
	project *projectpkg.Project,
	actor types.Actor,
	result *types.GitOpsSyncResult,
	authConfig git.AuthConfig,
	identity git.CommitIdentity,
	snapshot *backupSnapshotInternal,
	baseline map[string]string,
	adopt bool,
	startedAt time.Time,
) (
	bool,
	error,
) {
	checkout, err := s.repoService.CheckoutForWrite(ctx, localSync.Repository.URL, localSync.Branch, authConfig)
	if err != nil {
		return false, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to prepare repository checkout", err, false)
	}
	defer s.repoService.Discard(ctx, checkout.RepoPath)

	analysis, err := analyzeBackup(ctx, checkout.RepoPath, localSync.BackupDirectory, snapshot, baseline, adopt)
	if err != nil {
		return false, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureRepository, "Failed to read remote backup", err, false)
	}
	switch analysis.state {
	case BackupPreviewClean:
		if recordBackupSuccessErr := s.recordBackupSuccess(ctx, localSync, snapshot, checkout.HeadCommit, false, startedAt); recordBackupSuccessErr != nil {
			return false, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureSnapshot, "Failed to record backup", recordBackupSuccessErr, false)
		}
		result.Success = true
		result.Message = "Repository already contains the current files for project " + project.Name
		return false, nil
	case BackupPreviewConflict:
		names := make([]string, 0, BackupCommitFileLines+1)
		for _, change := range analysis.conflicts[:min(len(analysis.conflicts), BackupCommitFileLines)] {
			names = append(names, change.Path)
		}
		if len(analysis.conflicts) > BackupCommitFileLines {
			names = append(names, "...")
		}
		return false, s.failBackup(
			ctx,
			localSync,
			result,
			actor,
			types.GitOpsBackupFailureConflict,
			"Backup files changed in the repository",
			fmt.Errorf(
				"%d backup file(s) changed in the repository since the last backup: %s",
				len(
					analysis.conflicts,
				),
				strings.Join(
					names,
					", ",
				),
			),
			true,
		)
	case BackupPreviewOccupied:
		return false, s.failBackup(
			ctx,
			localSync,
			result,
			actor,
			types.GitOpsBackupFailureDestinationOccupied,
			"Backup directory already contains unrelated files",
			fmt.Errorf(
				"directory %s on branch %s already contains files that are not an Arcane backup",
				localSync.BackupDirectory,
				localSync.Branch,
			),
			true,
		)
	}

	var message strings.Builder
	fmt.Fprintf(&message, "Back up %s from Arcane", project.Name)
	if actor.Username != "" && actor.ID != types.SystemUser.ID {
		fmt.Fprintf(&message, " (%s)", actor.Username)
	}
	message.WriteString("\n")
	for _, change := range analysis.changes[:min(len(analysis.changes), BackupCommitFileLines)] {
		fmt.Fprintf(&message, "\n%s %s", change.Change, change.Path)
	}
	if extra := len(analysis.changes) - BackupCommitFileLines; extra > 0 {
		fmt.Fprintf(&message, "\n... and %d more", extra)
	}
	request := git.CommitRequest{
		Message:     message.String(),
		AuthorName:  identity.Name,
		AuthorEmail: identity.Email,
		SignKey:     identity.SignKey,
	}
	for _, file := range snapshot.files {
		request.Files = append(request.Files, git.CommitFile{Path: path.Join(localSync.BackupDirectory, file.Path), Content: file.Content, Executable: file.Executable})
	}
	for _, removed := range analysis.remove {
		request.Remove = append(request.Remove, path.Join(localSync.BackupDirectory, removed))
	}
	legacyManifest := path.Join(localSync.BackupDirectory, LegacyBackupManifestFileName)
	if exists, existsErr := acfs.Exists(ctx, checkout.RepoPath, "/"+legacyManifest); existsErr == nil && exists {
		request.Remove = append(request.Remove, legacyManifest)
	}

	commit, committed, err := s.repoService.CommitAndPush(ctx, checkout, request, authConfig)
	if err != nil {
		if errors.Is(err, git.ErrPushRejected) {
			return true, err
		}
		return false, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to push backup", err, false)
	}

	if recordCommittedBackupErr := s.recordBackupSuccess(ctx, localSync, snapshot, commit, committed, startedAt); recordCommittedBackupErr != nil {
		return false, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureSnapshot, "Failed to record backup", recordCommittedBackupErr, false)
	}
	result.Success = true
	result.Message = fmt.Sprintf("Backed up %d file(s) for project %s to %s", len(snapshot.files), project.Name, localSync.BackupDirectory)
	if _, eventErr := s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git backup completed",
		Description:   fmt.Sprintf("Backed up project '%s' to '%s' (%s)", project.Name, localSync.BackupDirectory, commit[:min(len(commit), 12)]),
		ResourceType:  new("git_sync"),
		ResourceID:    new(localSync.ID),
		ResourceName:  new(localSync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(localSync.EnvironmentID),
	}); eventErr != nil {
		slog.WarnContext(ctx, "Failed to record git backup audit event", "syncId", localSync.ID, "commit", commit, "error", eventErr)
	}
	slog.InfoContext(ctx, "Git backup completed", "syncId", localSync.ID, "project", project.Name, "commit", commit, "committed", committed)
	return false, nil
}

func (s *Service) recordBackupSuccess(
	ctx context.Context,
	localSync *projectpkg.GitOpsSync,
	snapshot *backupSnapshotInternal,
	commit string,
	committed bool,
	startedAt time.Time,
) error {
	now := time.Now()
	paths := make([]string, 0, len(snapshot.files))
	for _, file := range snapshot.files {
		paths = append(paths, file.Path)
	}
	hashes, err := json.Marshal(snapshot.hashes, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("failed to encode backup snapshot: %w", err)
	}
	updates := map[string]any{
		"last_sync_at":          now,
		"last_sync_status":      "success",
		"last_sync_error":       nil,
		"last_backup_snapshot":  string(hashes),
		"synced_files":          projectpkg.EncodeSyncedFiles(paths),
		"backup_conflict":       false,
		"backup_failure_reason": nil,
	}
	if commit != "" {
		updates["last_sync_commit"] = commit
	}
	if committed || localSync.LastBackupAt == nil {
		updates["last_backup_at"] = now
	}
	if recordBackupSuccessErr := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", localSync.ID).Updates(updates).Error; recordBackupSuccessErr != nil {
		slog.ErrorContext(ctx, "Failed to record git backup success", "error", recordBackupSuccessErr, "syncId", localSync.ID)
	}
	if clearBackupPendingErr := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("id = ? AND (backup_pending_since IS NULL OR backup_pending_since <= ?)", localSync.ID, startedAt).
		Updates(map[string]any{"backup_pending": false, "backup_pending_since": nil}).Error; clearBackupPendingErr != nil {
		slog.ErrorContext(ctx, "Failed to clear git backup pending flag", "error", clearBackupPendingErr, "syncId", localSync.ID)
	}
	return nil
}

// failBackup records the failure on the sync; needsAttention marks it as a conflict the user must resolve.
func (
	s *Service,
) failBackup(
	ctx context.Context,
	localSync *projectpkg.GitOpsSync,
	result *types.GitOpsSyncResult,
	actor types.Actor,
	reason, message string,
	failure error,
	needsAttention bool,
) error {
	errMsg := failure.Error()
	result.Message = message
	result.Error = new(errMsg)
	status := kit.Ternary(needsAttention, BackupStatusConflict, "failed")
	updates := map[string]any{
		"last_sync_at":          time.Now(),
		"last_sync_status":      status,
		"last_sync_error":       errMsg,
		"backup_conflict":       needsAttention,
		"backup_failure_reason": reason,
	}
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", localSync.ID).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to record git backup failure", "error", err, "syncId", localSync.ID)
	}
	s.logError(ctx, localSync, actor, errMsg)
	if needsAttention {
		return common.Classify(common.ErrConflict, fmt.Errorf("%s: %w", message, failure))
	}
	return fmt.Errorf("%s: %w", message, failure)
}

// Preview reports what the next backup run would commit or why it needs attention.
func (s *Service) Preview(previewCtx context.Context, syncRecord *projectpkg.GitOpsSync) (*types.GitOpsBackupPreview, error) {
	if syncRecord.ProjectID == nil {
		return nil, common.Classify(common.ErrNotFound, errors.New("backup sync has no project"))
	}
	project, found, err := s.projectService.FindProjectByID(previewCtx, *syncRecord.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project %s: %w", *syncRecord.ProjectID, err)
	}
	if !found {
		return nil, common.ErrProjectNotFound
	}
	if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(previewCtx, project, false); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}
	snapshot, err := s.buildBackupSnapshot(previewCtx, syncRecord, project)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, err)
	}
	authConfig, err := s.repoService.GetAuthConfig(previewCtx, syncRecord.Repository)
	if err != nil {
		return nil, err
	}
	checkout, err := s.repoService.CheckoutForWrite(previewCtx, syncRecord.Repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer s.repoService.Discard(previewCtx, checkout.RepoPath)
	analysis, err := analyzeBackup(previewCtx, checkout.RepoPath, syncRecord.BackupDirectory, snapshot, syncRecord.BackupSnapshot(), false)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(snapshot.files))
	for _, file := range snapshot.files {
		files = append(files, file.Path)
	}
	return &types.GitOpsBackupPreview{
		State:        analysis.state,
		RemoteCommit: checkout.HeadCommit,
		Changes:      analysis.changes,
		Conflicts:    analysis.conflicts,
		Files:        files,
	}, nil
}

// backupSnapshotInternal is the set of project files a backup run commits.
type backupSnapshotInternal struct {
	files  []git.CommitFile
	hashes map[string]string
}

// backupAnalysisInternal is the decision for one backup run.
type backupAnalysisInternal struct {
	state     string
	changes   []types.GitOpsBackupFileChange
	conflicts []types.GitOpsBackupFileChange
	remove    []string
}

// backupComposeFiles resolves the primary compose file and sibling overrides as project-relative paths.
func (s *Service) backupComposeFiles(ctx context.Context, project *projectpkg.Project) (string, []string, error) {
	composePath, err := s.projectService.ResolveProjectComposeFile(ctx, project)
	if err != nil {
		return "", nil, err
	}
	relative, err := filepath.Rel(project.Path, composePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("compose file %s is outside the project directory", composePath)
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

// buildBackupSnapshot reads the selected project files, retrying when they change mid-read.
func (s *Service) buildBackupSnapshot(ctx context.Context, localSync *projectpkg.GitOpsSync, project *projectpkg.Project) (*backupSnapshotInternal, error) {
	_, composeFiles, err := s.backupComposeFiles(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", err.Error(), git.ErrSelectionInvalid)
	}
	paths := slices.Clone([]string(localSync.BackupPaths))
	for _, composeFile := range composeFiles {
		if !backupSelectionCovers(paths, composeFile) {
			paths = append(paths, composeFile)
		}
	}
	maxFiles, maxTotalSize, _ := s.limits(ctx, localSync)
	options := git.CollectOptions{
		MaxFiles:     maxFiles,
		MaxTotalSize: maxTotalSize,
		SkipDir:      projects.IsInternalScratchDirName,
		SkipFile: func(name string) bool {
			return name == projects.GitSourceEnvFileName || name == projects.GlobalEnvFileName || name == projects.EffectiveEnvFileName || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")
		},
	}

	for range BackupSnapshotMaxRetry {
		files, collectFilesErr := git.CollectFiles(ctx, project.Path, paths, options)
		if collectFilesErr != nil {
			return nil, collectFilesErr
		}
		snapshot := &backupSnapshotInternal{files: files, hashes: make(map[string]string, len(files))}
		for _, file := range files {
			snapshot.hashes[file.Path] = kit.SHA256Hex(file.Content)
		}

		stable := true
		for _, file := range files {
			content, readFileErr := acfs.ReadFile(ctx, project.Path, "/"+file.Path)
			if readFileErr != nil {
				return nil, fmt.Errorf("cannot re-read %s: %v: %w", file.Path, readFileErr.Error(), git.ErrSelectionUnreadable)
			}
			if kit.SHA256Hex(content) != snapshot.hashes[file.Path] {
				stable = false
				break
			}
		}
		if stable {
			return snapshot, nil
		}
	}
	return nil, fmt.Errorf("project files changed while the backup snapshot was being taken: %w", git.ErrSelectionInvalid)
}

// analyzeBackup compares the snapshot with the checkout; baseline is the last push and the only proof the directory is ours.
func analyzeBackup(ctx context.Context, repoPath, directory string, snapshot *backupSnapshotInternal, baseline map[string]string, adopt bool) (backupAnalysisInternal, error) {
	analysis := backupAnalysisInternal{conflicts: []types.GitOpsBackupFileChange{}}
	entries, err := acfs.List(ctx, repoPath, "/"+directory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return analysis, fmt.Errorf("failed to inspect remote backup directory: %w", err)
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
					return analysis, fmt.Errorf("failed to read remote backup file %s: %w", file, readErr)
				}
				remote[file] = kit.SHA256Hex(content)
			}
		}
	}

	analysis.changes = diffBackupHashes(snapshot.hashes, remote)
	for file := range remote {
		if _, ok := snapshot.hashes[file]; !ok {
			analysis.remove = append(analysis.remove, file)
		}
	}
	sort.Strings(analysis.remove)

	switch {
	case len(analysis.changes) == 0:
		analysis.state = BackupPreviewClean
	case adopt:
		analysis.state = BackupPreviewChanges
	case baseline == nil && occupied:
		analysis.state = BackupPreviewOccupied
	case baseline == nil:
		analysis.state = BackupPreviewChanges
	default:
		analysis.conflicts = diffBackupHashes(remote, baseline)
		analysis.state = BackupPreviewChanges
		if len(analysis.conflicts) > 0 {
			analysis.state = BackupPreviewConflict
		}
	}
	return analysis, nil
}

// cloneBackupBranch clones the backup branch for read-only inspection
func (s *Service) cloneBackupBranch(ctx context.Context, syncRecord *projectpkg.GitOpsSync) (string, func(), error) {
	authConfig, err := s.repoService.GetAuthConfig(ctx, syncRecord.Repository)
	if err != nil {
		return "", func() {}, err
	}
	checkout, err := s.repoService.CheckoutForWrite(ctx, syncRecord.Repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return "", func() {}, fmt.Errorf("failed to clone repository: %w", err)
	}
	cleanup := func() { s.repoService.Discard(ctx, checkout.RepoPath) }
	if !checkout.BranchExists {
		return "", cleanup, nil
	}
	return checkout.RepoPath, cleanup, nil
}

// History lists revisions that touched the backup directory.
func (s *Service) History(historyCtx context.Context, syncRecord *projectpkg.GitOpsSync, limit int) (*types.GitOpsBackupHistoryResponse, error) {
	repoPath, cleanup, err := s.cloneBackupBranch(historyCtx, syncRecord)
	defer cleanup()
	if err != nil {
		return nil, err
	}
	response := &types.GitOpsBackupHistoryResponse{Entries: []types.GitOpsBackupHistoryEntry{}}
	if repoPath == "" {
		return response, nil
	}
	if limit <= 0 {
		limit = DefaultBackupHistoryLimit
	}
	entries, err := s.repoService.DirectoryHistory(historyCtx, repoPath, syncRecord.BackupDirectory, limit)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		response.Entries = append(response.Entries, types.GitOpsBackupHistoryEntry{Commit: entry.Hash, Author: entry.Author, Message: entry.Message, Date: entry.Date, Files: entry.Files})
	}
	return response, nil
}

// Revision returns one revision with per-file diffs inside the backup directory.
func (s *Service) Revision(revisionCtx context.Context, syncRecord *projectpkg.GitOpsSync, commit string) (*types.GitOpsBackupRevision, error) {
	commit = strings.TrimSpace(commit)
	repoPath, cleanup, err := s.cloneBackupBranch(revisionCtx, syncRecord)
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
	revision := &types.GitOpsBackupRevision{
		Entry: types.GitOpsBackupHistoryEntry{Commit: entry.Hash, Author: entry.Author, Message: entry.Message, Date: entry.Date, Files: entry.Files},
		Diffs: []types.GitOpsBackupFileDiff{},
	}
	for _, diff := range diffs {
		revision.Diffs = append(revision.Diffs, types.GitOpsBackupFileDiff{Path: diff.Path, Patch: diff.Patch})
	}
	return revision, nil
}

func diffBackupHashes(local, remote map[string]string) []types.GitOpsBackupFileChange {
	var changes []types.GitOpsBackupFileChange
	for file, hash := range local {
		remoteHash, ok := remote[file]
		switch {
		case !ok:
			changes = append(changes, types.GitOpsBackupFileChange{Path: file, Change: BackupChangeAdded})
		case remoteHash != hash:
			changes = append(changes, types.GitOpsBackupFileChange{Path: file, Change: BackupChangeModified})
		}
	}
	for file := range remote {
		if _, ok := local[file]; !ok {
			changes = append(changes, types.GitOpsBackupFileChange{Path: file, Change: BackupChangeRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []types.GitOpsBackupFileChange{}
	}
	return changes
}

const (
	DefaultBackupIntervalMinutes = 5
	DefaultBackupSaveDebounce    = 10 * time.Second
	BackupPushAttempts           = 3
	BackupStatusRunning          = "running"
	BackupStatusConflict         = "conflict"
	BackupCommitFileLines        = 20

	BackupSnapshotMaxRetry       = 3
	LegacyBackupManifestFileName = ".arcane-backup.json"
	BackupPreviewClean           = "clean"
	BackupPreviewChanges         = "changes"
	BackupPreviewConflict        = "conflict"
	BackupPreviewOccupied        = "destination_occupied"
	BackupChangeAdded            = "added"
	BackupChangeModified         = "modified"
	BackupChangeRemoved          = "removed"

	DefaultBackupHistoryLimit = 20
)

// backupSelectionCovers reports whether the selection includes file directly or via a parent directory.
func backupSelectionCovers(paths []string, file string) bool {
	for _, selected := range paths {
		if selected == file || strings.HasPrefix(file, selected+"/") {
			return true
		}
	}
	return false
}

func normalizeBackupPaths(raw []string) ([]string, error) {
	normalized := make([]string, 0, len(raw))
	for _, entry := range raw {
		cleaned, err := kit.NormalizeRelativePath(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid backup path %q: %w", entry, err)
		}
		localBase := path.Base(cleaned)
		if localBase == projects.GitSourceEnvFileName || localBase == projects.GlobalEnvFileName || slices.Contains(strings.Split(cleaned, "/"), ".git") {
			return nil, fmt.Errorf("backup path %q is reserved", entry)
		}
		normalized = append(normalized, cleaned)
	}
	normalized = kit.Unique(normalized)
	sort.Strings(normalized)
	return normalized, nil
}

func backupFailureReason(err error) string {
	switch {
	case errors.Is(err, git.ErrSelectionUnreadable):
		return types.GitOpsBackupFailureUnreadableFiles
	case errors.Is(err, git.ErrSelectionLimits):
		return types.GitOpsBackupFailureLimits
	case errors.Is(err, git.ErrSelectionInvalid):
		return types.GitOpsBackupFailureSnapshot
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"authentication", "authorization", "ssh", "credential"} {
		if strings.Contains(message, marker) {
			return types.GitOpsBackupFailureAuth
		}
	}
	return types.GitOpsBackupFailureRepository
}

// ensureBackupDestinationFree rejects a destination that overlaps another backup on the same branch.
func ensureBackupDestinationFree(tx *gorm.DB, excludeSyncID, repositoryID, branch, directory string) error {
	var others []projectpkg.GitOpsSync
	q := tx.Select("id", "backup_directory").
		Where("mode = ? AND repository_id = ? AND branch = ?", types.GitOpsSyncModeBackup, repositoryID, branch)
	if excludeSyncID != "" {
		q = q.Where("id <> ?", excludeSyncID)
	}
	if err := q.Find(&others).Error; err != nil {
		return fmt.Errorf("failed to check backup destinations: %w", err)
	}
	for _, other := range others {
		if other.BackupDirectory == directory || strings.HasPrefix(other.BackupDirectory, directory+"/") || strings.HasPrefix(directory, other.BackupDirectory+"/") {
			return common.Classify(common.ErrConflict, fmt.Errorf("backup directory %q overlaps with an existing backup at %q on this branch", directory, other.BackupDirectory))
		}
	}
	return nil
}
