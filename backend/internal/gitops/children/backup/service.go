package backup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	projectpkg "getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/pkg/gitutil"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		db             *database.DB
		repoService    *gitrepo.GitRepositoryService
		projectService *projectpkg.ProjectService
		eventService   *event.EventService
		limits         func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64)
		logError       func(context.Context, *projectpkg.GitOpsSync, types.Actor, string)
		branchLock     func(repositoryID, branch string) *sync.Mutex
	}

	Params struct {
		DB             *database.DB
		RepoService    *gitrepo.GitRepositoryService
		ProjectService *projectpkg.ProjectService
		EventService   *event.EventService
		Limits         func(context.Context, *projectpkg.GitOpsSync) (int, int64, int64)
		LogError       func(context.Context, *projectpkg.GitOpsSync, types.Actor, string)
		BranchLock     func(repositoryID, branch string) *sync.Mutex
	}

	backupCreateConfig struct {
		project     *projectpkg.Project
		directory   string
		composeFile string
		paths       []string
	}

	backupSnapshot struct {
		hashes map[string]string
		files  []git.CommitFile
	}

	PerformOptions struct {
		Baseline map[string]string
		Adopt    bool
	}

	PullPlan struct {
		Baseline map[string]string
		Commit   string
		Files    []projects.SyncFile
		Removed  []string
	}

	backupAnalysis struct {
		state     string
		changes   []types.GitOpsBackupFileChange
		conflicts []types.GitOpsBackupFileChange
		remove    []string
	}
)

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
	DefaultBackupHistoryLimit    = 20
)

func New(p Params) *Service {
	return &Service{
		db:             p.DB,
		repoService:    p.RepoService,
		projectService: p.ProjectService,
		eventService:   p.EventService,
		limits:         p.Limits,
		logError:       p.LogError,
		branchLock:     p.BranchLock,
	}
}

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

func (s *Service) prepareBackupCreate(ctx context.Context, tx *gorm.DB, req types.GitOpsSync) (*backupCreateConfig, error) {
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
		Where("mode IN ? AND project_id = ?", projectpkg.GitOpsPushModes, project.ID).
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
	if err == nil && len(directory) >= 2 && directory[1] == ':' && unicode.IsLetter(rune(directory[0])) {
		err = errors.New("backup directory must not start with a drive letter")
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
	return &backupCreateConfig{
		project:     project,
		directory:   directory,
		paths:       paths,
		composeFile: composeFile,
	}, nil
}

func (s *Service) ApplyModeUpdates(ctx context.Context, current *projectpkg.GitOpsSync, req types.GitOpsSync, submitted map[string]bool, updates map[string]any) error {
	if !slices.Contains(projectpkg.GitOpsPushModes, current.Mode) {
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

func (s *Service) loadBackupProject(ctx context.Context, localSync *projectpkg.GitOpsSync, actor types.Actor, result *types.GitOpsSyncResult) (*projectpkg.Project, error) {
	if localSync.Repository == nil {
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureRepository, "Repository not found", errors.New("repository not found"), false)
	}
	if strings.TrimSpace(kit.FromPtr(localSync.ProjectID)) == "" {
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project not linked", errors.New("backup sync has no project"), false)
	}
	project, found, err := s.projectService.FindProjectByID(ctx, *localSync.ProjectID)
	if err != nil {
		err = fmt.Errorf("failed to get project %s: %w", *localSync.ProjectID, err)
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Failed to load project", err, false)
	}
	if !found {
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project not found", fmt.Errorf("project %s no longer exists", *localSync.ProjectID), false)
	}
	if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); ensureProjectPathUnderRootErr != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureProjectMissing, "Project directory is unavailable", ensureProjectPathUnderRootErr, false)
	}
	return project, nil
}

func (s *Service) Perform(ctx context.Context, localSync *projectpkg.GitOpsSync, actor types.Actor, result *types.GitOpsSyncResult, opts PerformOptions) (*types.GitOpsSyncResult, error) {
	project, err := s.loadBackupProject(ctx, localSync, actor, result)
	if err != nil {
		return result, err
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

	baseline := kit.Ternary(opts.Baseline != nil, opts.Baseline, localSync.BackupSnapshot())
	snapshot, err := s.buildBackupSnapshot(ctx, localSync, project, slices.Collect(maps.Keys(baseline)))
	if err != nil {
		return result, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to snapshot project files", err, false)
	}

	for attempt := 1; attempt <= BackupPushAttempts; attempt++ {
		retry, commitBackupErr := s.commitBackup(ctx, localSync, project, actor, result, authConfig, identity, snapshot, baseline, opts.Adopt, startedAt)
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

func (s *Service) Plan(ctx context.Context, localSync *projectpkg.GitOpsSync, actor types.Actor, result *types.GitOpsSyncResult) (*PullPlan, error) {
	project, err := s.loadBackupProject(ctx, localSync, actor, result)
	if err != nil {
		return nil, err
	}
	authConfig, err := s.repoService.GetAuthConfig(ctx, localSync.Repository)
	if err != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, types.GitOpsBackupFailureAuth, "Failed to get authentication config", err, false)
	}
	checkout, err := s.repoService.CheckoutForWrite(ctx, localSync.Repository.URL, localSync.Branch, authConfig)
	if err != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to prepare repository checkout", err, false)
	}
	defer s.repoService.Discard(ctx, checkout.RepoPath)

	selection, err := s.backupSelection(ctx, localSync, project)
	if err != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to snapshot project files", err, false)
	}
	remote, err := s.readRemoteBackup(ctx, localSync, checkout.RepoPath, selection)
	if err != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to read remote backup", err, false)
	}
	plan := &PullPlan{Baseline: make(map[string]string, len(remote)), Commit: checkout.HeadCommit}
	for file, content := range remote {
		plan.Baseline[file] = kit.SHA256Hex(content.Content)
	}
	baseline := localSync.BackupSnapshot()
	snapshot, err := s.buildBackupSnapshot(ctx, localSync, project, slices.Concat(slices.Collect(maps.Keys(plan.Baseline)), slices.Collect(maps.Keys(baseline))))
	if err != nil {
		return nil, s.failBackup(ctx, localSync, result, actor, backupFailureReason(err), "Failed to snapshot project files", err, false)
	}

	var conflicts []types.GitOpsBackupFileChange
	files := slices.Concat(slices.Collect(maps.Keys(snapshot.hashes)), slices.Collect(maps.Keys(plan.Baseline)), slices.Collect(maps.Keys(baseline)))
	slices.Sort(files)
	for _, file := range slices.Compact(files) {
		local, remoteHash, base := snapshot.hashes[file], plan.Baseline[file], baseline[file]
		switch {
		case local == remoteHash || remoteHash == base:
		case local == base && remoteHash == "":
			plan.Removed = append(plan.Removed, file)
		case local == base:
			plan.Files = append(plan.Files, remote[file])
		default:
			conflicts = append(conflicts, types.GitOpsBackupFileChange{Path: file, Change: BackupChangeModified})
		}
	}
	if len(conflicts) > 0 {
		return nil, s.failBackup(
			ctx,
			localSync,
			result,
			actor,
			types.GitOpsBackupFailureConflict,
			"Files changed in both Arcane and the repository",
			fmt.Errorf("%d file(s) changed in both Arcane and the repository since the last sync: %s", len(conflicts), changedPathList(conflicts)),
			true,
		)
	}
	return plan, nil
}

func (s *Service) readRemoteBackup(ctx context.Context, localSync *projectpkg.GitOpsSync, repoPath string, selection []string) (map[string]projects.SyncFile, error) {
	root := path.Join("/", localSync.BackupDirectory)
	prefix := strings.TrimSuffix(root, "/") + "/"
	maxFiles, maxTotalSize, _ := s.limits(ctx, localSync)
	files := make(map[string]projects.SyncFile)
	var totalSize int64
	_, err := acfs.Root{Path: repoPath}.Walk(ctx, root, acfstypes.WalkOptions{}, func(entry acfstypes.Entry) error {
		relative := strings.TrimPrefix(entry.Path, prefix)
		switch {
		case entry.IsDirectory && skipBackupDir(entry.Name):
			return fs.SkipDir
		case entry.IsDirectory || entry.IsSymlink || !inBackupScope(selection, localSync.SyncDirectory, relative):
			return nil
		case maxFiles > 0 && len(files) >= maxFiles:
			return fmt.Errorf("file count limit exceeded (max %d files): %w", maxFiles, git.ErrSelectionLimits)
		}
		content, err := acfs.Root{Path: repoPath}.ReadFile(ctx, entry.Path)
		if err != nil {
			return fmt.Errorf("failed to read remote backup file %s: %w", relative, err)
		}
		totalSize += int64(len(content))
		if maxTotalSize > 0 && totalSize > maxTotalSize {
			return fmt.Errorf("total size limit exceeded (max %d bytes): %w", maxTotalSize, git.ErrSelectionLimits)
		}
		files[relative] = projects.SyncFile{RelativePath: relative, Content: content, Executable: os.FileMode(entry.UnixMode)&0o111 != 0}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return files, nil
}

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
	snapshot *backupSnapshot,
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
				changedPathList(
					analysis.conflicts,
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
	if exists, existsErr := (acfs.Root{Path: checkout.RepoPath}).Exists(ctx, "/"+legacyManifest); existsErr == nil && exists {
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
	snapshot *backupSnapshot,
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
		"backup_conflict":       false,
		"backup_failure_reason": nil,
	}
	if localSync.Mode == types.GitOpsSyncModeBackup {
		updates["synced_files"] = projectpkg.EncodeSyncedFiles(paths)
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
	snapshot, err := s.buildBackupSnapshot(previewCtx, syncRecord, project, slices.Collect(maps.Keys(syncRecord.BackupSnapshot())))
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
	for _, candidate := range projects.ComposeOverrideFileCandidates {
		candidatePath := path.Join(composeDir, candidate)
		if exists, existsErr := (acfs.Root{Path: project.Path}).Exists(ctx, "/"+candidatePath); existsErr == nil && exists {
			composeFiles = append(composeFiles, candidatePath)
		}
	}
	return primary, composeFiles, nil
}

func (s *Service) backupSelection(ctx context.Context, localSync *projectpkg.GitOpsSync, project *projectpkg.Project) ([]string, error) {
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
	return paths, nil
}

func (s *Service) buildBackupSnapshot(ctx context.Context, localSync *projectpkg.GitOpsSync, project *projectpkg.Project, tracked []string) (*backupSnapshot, error) {
	paths, err := s.backupSelection(ctx, localSync, project)
	if err != nil {
		return nil, err
	}
	for _, file := range tracked {
		if !localSync.SyncDirectory || backupSelectionCovers(paths, file) || !inBackupScope(paths, true, file) {
			continue
		}
		if exists, existsErr := (acfs.Root{Path: project.Path}).Exists(ctx, "/"+file); existsErr == nil && exists {
			paths = append(paths, file)
		}
	}
	maxFiles, maxTotalSize, _ := s.limits(ctx, localSync)
	options := git.CollectOptions{
		MaxFiles:     maxFiles,
		MaxTotalSize: maxTotalSize,
		SkipDir:      projects.IsInternalScratchDirName,
		SkipFile:     skipBackupFile,
	}

	for range BackupSnapshotMaxRetry {
		files, collectFilesErr := git.CollectFiles(ctx, project.Path, paths, options)
		if collectFilesErr != nil {
			return nil, collectFilesErr
		}
		snapshot := &backupSnapshot{files: files, hashes: make(map[string]string, len(files))}
		for _, file := range files {
			snapshot.hashes[file.Path] = kit.SHA256Hex(file.Content)
		}

		stable := true
		for _, file := range files {
			content, readFileErr := acfs.Root{Path: project.Path}.ReadFile(ctx, "/"+file.Path)
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

func analyzeBackup(ctx context.Context, repoPath, directory string, snapshot *backupSnapshot, baseline map[string]string, adopt bool) (backupAnalysis, error) {
	analysis := backupAnalysis{conflicts: []types.GitOpsBackupFileChange{}}
	entries, err := acfs.Root{Path: repoPath}.List(ctx, "/"+directory)
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
				content, readErr := acfs.Root{Path: repoPath}.ReadFile(ctx, "/"+path.Join(directory, file))
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
	return changes
}

func changedPathList(changes []types.GitOpsBackupFileChange) string {
	names := make([]string, 0, BackupCommitFileLines+1)
	for _, change := range changes[:min(len(changes), BackupCommitFileLines)] {
		names = append(names, change.Path)
	}
	if len(changes) > BackupCommitFileLines {
		names = append(names, "...")
	}
	return strings.Join(names, ", ")
}

func skipBackupFile(name string) bool {
	return name == projects.GitSourceEnvFileName || name == projects.GlobalEnvFileName || name == projects.EffectiveEnvFileName || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")
}

func skipBackupDir(name string) bool {
	return name == ".git" || projects.IsInternalScratchDirName(name)
}

func inBackupScope(selection []string, wholeDirectory bool, file string) bool {
	segments := strings.Split(file, "/")
	switch {
	case slices.Contains(selection, file):
		return true
	case slices.ContainsFunc(segments[:len(segments)-1], skipBackupDir) || skipBackupFile(segments[len(segments)-1]):
		return false
	}
	return wholeDirectory || backupSelectionCovers(selection, file)
}

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

func ensureBackupDestinationFree(tx *gorm.DB, excludeSyncID, repositoryID, branch, directory string) error {
	var others []projectpkg.GitOpsSync
	q := tx.Select("id", "backup_directory").
		Where("mode IN ? AND repository_id = ? AND branch = ?", projectpkg.GitOpsPushModes, repositoryID, branch)
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
