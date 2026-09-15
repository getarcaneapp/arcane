package git

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var installWriteTestTransportOnceInternal sync.Once

// installWriteTestTransportInternal serves bare repositories on disk over the
// "http" scheme so the write paths run without a network or the git binary.
func installWriteTestTransportInternal(t *testing.T) {
	t.Helper()
	installWriteTestTransportOnceInternal.Do(func() {
		client.InstallProtocol("http", server.NewClient(server.NewFilesystemLoader(osfs.New("/"))))
	})
}

func newBareRepoURLInternal(t *testing.T) string {
	t.Helper()
	installWriteTestTransportInternal(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	_, err := gogit.PlainInit(bare, true)
	require.NoError(t, err)
	return "http://localhost" + bare
}

func noAuthInternal() AuthConfig {
	return AuthConfig{AuthType: "none"}
}

// pushCommitInternal commits files onto branch through a separate checkout,
// standing in for another writer against the same remote.
func pushCommitInternal(t *testing.T, url, branch, message string, files map[string]string) string {
	t.Helper()
	writer := NewClient(t.TempDir())
	checkout, err := writer.CheckoutForWrite(t.Context(), url, branch, noAuthInternal())
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Cleanup(checkout.RepoPath) })

	request := CommitRequest{Message: message, AuthorName: "Other", AuthorEmail: "other@localhost"}
	for path, content := range files {
		request.Files = append(request.Files, CommitFile{Path: path, Content: []byte(content)})
	}
	head, _, err := writer.CommitAndPush(t.Context(), checkout, request, noAuthInternal())
	require.NoError(t, err)
	return head
}

func TestWriteCommitAndPush_CreatesBranchInEmptyRepository(t *testing.T) {
	ctx := t.Context()
	url := newBareRepoURLInternal(t)
	gitClient := NewClient(t.TempDir())

	checkout, err := gitClient.CheckoutForWrite(ctx, url, "main", noAuthInternal())
	require.NoError(t, err)
	require.False(t, checkout.BranchExists)

	request := CommitRequest{
		Files:       []CommitFile{{Path: "backups/app/compose.yaml", Content: []byte("services: {}\n")}},
		Message:     "first backup",
		AuthorName:  "Arcane",
		AuthorEmail: "arcane@localhost",
	}
	head, committed, err := gitClient.CommitAndPush(ctx, checkout, request, noAuthInternal())
	require.NoError(t, err)
	require.True(t, committed)
	require.NotEmpty(t, head)

	remoteHead, exists, err := gitClient.RemoteBranchHead(ctx, url, "main", noAuthInternal())
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, head, remoteHead)

	second, committed, err := gitClient.CommitAndPush(ctx, checkout, request, noAuthInternal())
	require.NoError(t, err)
	assert.False(t, committed)
	assert.Equal(t, head, second)
}

func TestCheckoutForWrite_CreatesMissingBranchFromDefault(t *testing.T) {
	ctx := t.Context()
	url := newBareRepoURLInternal(t)
	gitClient := NewClient(t.TempDir())

	pushCommitInternal(t, url, "master", "seed", map[string]string{"unrelated/keep.txt": "keep\n"})

	checkout, err := gitClient.CheckoutForWrite(ctx, url, "feature", noAuthInternal())
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitClient.Cleanup(checkout.RepoPath) })
	require.FileExists(t, filepath.Join(checkout.RepoPath, "unrelated", "keep.txt"))

	head, committed, err := gitClient.CommitAndPush(ctx, checkout, CommitRequest{
		Files:       []CommitFile{{Path: "backups/app/compose.yaml", Content: []byte("services: {}\n")}},
		Message:     "backup on new branch",
		AuthorName:  "Arcane",
		AuthorEmail: "arcane@localhost",
	}, noAuthInternal())
	require.NoError(t, err)
	require.True(t, committed)

	remoteHead, exists, err := gitClient.RemoteBranchHead(ctx, url, "feature", noAuthInternal())
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, head, remoteHead)

	verify := NewClient(t.TempDir())
	repoPath, err := verify.Clone(ctx, url, "feature", noAuthInternal())
	require.NoError(t, err)
	t.Cleanup(func() { _ = verify.Cleanup(repoPath) })
	assert.FileExists(t, filepath.Join(repoPath, "unrelated", "keep.txt"))
	assert.FileExists(t, filepath.Join(repoPath, "backups", "app", "compose.yaml"))
}

func TestWriteCommitAndPush_RejectsWhenRemoteAdvanced(t *testing.T) {
	ctx := t.Context()
	url := newBareRepoURLInternal(t)
	gitClient := NewClient(t.TempDir())

	pushCommitInternal(t, url, "main", "seed", map[string]string{"backups/app/compose.yaml": "services: {}\n"})

	checkout, err := gitClient.CheckoutForWrite(ctx, url, "main", noAuthInternal())
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitClient.Cleanup(checkout.RepoPath) })

	pushCommitInternal(t, url, "main", "other writer", map[string]string{"other/notes.txt": "notes\n"})

	_, _, err = gitClient.CommitAndPush(ctx, checkout, CommitRequest{
		Files:       []CommitFile{{Path: "backups/app/compose.yaml", Content: []byte("services: {app: {}}\n")}},
		Message:     "stale backup",
		AuthorName:  "Arcane",
		AuthorEmail: "arcane@localhost",
	}, noAuthInternal())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPushRejected)
}

func TestDirectoryHistoryAndCommitDiff(t *testing.T) {
	ctx := t.Context()
	repoPath := t.TempDir()
	repo, err := gogit.PlainInit(repoPath, false)
	require.NoError(t, err)
	worktree, err := repo.Worktree()
	require.NoError(t, err)

	commit := func(message string, files map[string]string) string {
		for name, content := range files {
			target := filepath.Join(repoPath, filepath.FromSlash(name))
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
			require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
			_, err := worktree.Add(name)
			require.NoError(t, err)
		}
		signature := &object.Signature{Name: "Arcane", Email: "arcane@localhost", When: time.Now()}
		hash, err := worktree.Commit(message, &gogit.CommitOptions{Author: signature, Committer: signature})
		require.NoError(t, err)
		return hash.String()
	}

	commit("backup one", map[string]string{"backups/app/compose.yaml": "services: {}\n"})
	commit("unrelated", map[string]string{"docs/readme.md": "docs\n"})
	changed := commit("backup two", map[string]string{"backups/app/compose.yaml": "services: {app: {}}\n"})

	gitClient := NewClient(t.TempDir())
	entries, err := gitClient.DirectoryHistory(ctx, repoPath, "backups/app", 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, changed, entries[0].Hash)
	assert.Equal(t, []string{"compose.yaml"}, entries[0].Files)
	assert.Equal(t, "backup two", entries[0].Message)
	assert.Equal(t, "backup one", entries[1].Message)

	entry, diffs, err := gitClient.CommitDiff(ctx, repoPath, changed, "backups/app")
	require.NoError(t, err)
	assert.Equal(t, changed, entry.Hash)
	require.Len(t, diffs, 1)
	assert.Equal(t, "compose.yaml", diffs[0].Path)
	assert.Contains(t, diffs[0].Patch, "services: {app: {}}")
}

func TestDirectoryHistory_LimitsAndMissingHead(t *testing.T) {
	ctx := t.Context()
	repoPath := t.TempDir()
	_, err := gogit.PlainInit(repoPath, false)
	require.NoError(t, err)

	gitClient := NewClient(t.TempDir())
	entries, err := gitClient.DirectoryHistory(ctx, repoPath, "backups/app", 10)
	require.NoError(t, err)
	assert.Empty(t, entries)

	_, _, err = gitClient.CommitDiff(ctx, repoPath, "0000000000000000000000000000000000000000", "backups/app")
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}
