package gitops

import (
	"context"
	"log/slog"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	git "github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
)

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

// cloneBackupBranchInternal clones the backup branch for read-only inspection.
// A missing branch or empty repository yields an empty path.
func (s *GitOpsSyncService) cloneBackupBranchInternal(ctx context.Context, syncRecord *projectpkg.GitOpsSync) (string, func(), error) {
	authConfig, err := s.repoService.GetAuthConfig(ctx, syncRecord.Repository)
	if err != nil {
		return "", func() {}, err
	}
	checkout, err := s.repoService.CheckoutForWrite(ctx, syncRecord.Repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return "", func() {}, errors.WrapIf(err, "failed to clone repository")
	}
	cleanup := func() {
		if cleanupErr := s.repoService.Cleanup(checkout.RepoPath); cleanupErr != nil {
			slog.WarnContext(ctx, "Failed to cleanup repository", "path", checkout.RepoPath, "error", cleanupErr)
		}
	}
	if !checkout.BranchExists {
		return "", cleanup, nil
	}
	return checkout.RepoPath, cleanup, nil
}

func historyEntryToAPIInternal(entry git.HistoryEntry) gitops.BackupHistoryEntry {
	return gitops.BackupHistoryEntry{
		Commit:  entry.Hash,
		Author:  entry.Author,
		Message: entry.Message,
		Date:    entry.Date,
		Files:   entry.Files,
	}
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
		response.Entries = append(response.Entries, historyEntryToAPIInternal(entry))
	}
	return response, nil
}

// GetBackupRevision returns one revision with per-file diffs inside the backup directory.
func (s *GitOpsSyncService) GetBackupRevision(ctx context.Context, environmentID, id, commit string) (*gitops.BackupRevision, error) {
	commit = strings.TrimSpace(commit)
	if len(commit) < 7 || len(commit) > 64 {
		return nil, common.Classify(common.ErrBadRequest, errors.New("invalid commit hash"))
	}
	for _, r := range commit {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return nil, common.Classify(common.ErrBadRequest, errors.New("invalid commit hash"))
		}
	}

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
	if err != nil {
		return nil, common.Classify(common.ErrNotFound, err)
	}
	revision := &gitops.BackupRevision{Entry: historyEntryToAPIInternal(entry), Diffs: []gitops.BackupFileDiff{}}
	for _, diff := range diffs {
		revision.Diffs = append(revision.Diffs, gitops.BackupFileDiff{Path: diff.Path, Patch: diff.Patch})
	}
	return revision, nil
}
