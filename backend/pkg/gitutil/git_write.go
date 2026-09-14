package git

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"go.getarcane.app/acfs"
)

// ErrPushRejected reports that the remote branch advanced between checkout and push.
var ErrPushRejected = errors.Sentinel("push rejected: remote branch was updated by another writer")

// WriteCheckout is a scratch clone prepared for committing to one branch.
type WriteCheckout struct {
	RepoPath     string
	Branch       string
	HeadCommit   string
	BranchExists bool
	url          string
	repo         *git.Repository
}

// CommitFile is one file to write into the checkout, repo-relative.
type CommitFile struct {
	Path       string
	Content    []byte
	Executable bool
}

// CommitRequest describes the tree mutation to commit and push.
type CommitRequest struct {
	Files       []CommitFile
	Remove      []string
	Message     string
	AuthorName  string
	AuthorEmail string
}

// HistoryEntry is one commit touching a directory.
type HistoryEntry struct {
	Hash    string
	Author  string
	Email   string
	Message string
	Date    time.Time
	Files   []string
}

// FileDiff is the unified diff of one file within a commit.
type FileDiff struct {
	Path  string
	Patch string
}

// CheckoutForWrite clones url at branch into a scratch directory. A branch that
// does not exist yet is created from the remote default branch, and an empty
// repository is initialized locally so the first push creates the branch.
func (c *Client) CheckoutForWrite(ctx context.Context, url, branch string, auth AuthConfig) (*WriteCheckout, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, errors.New("branch is required")
	}
	normalized, err := normalizeURL(url)
	if err != nil {
		return nil, err
	}

	repoPath, err := c.Clone(ctx, normalized, branch, auth)
	if err == nil {
		repo, openErr := git.PlainOpen(repoPath)
		if openErr != nil {
			_ = c.Cleanup(repoPath)
			return nil, errors.WrapIf(openErr, "failed to open checkout")
		}
		head, headErr := repo.Head()
		if headErr != nil {
			_ = c.Cleanup(repoPath)
			return nil, errors.WrapIf(headErr, "failed to resolve checkout head")
		}
		return &WriteCheckout{RepoPath: repoPath, Branch: branch, HeadCommit: head.Hash().String(), BranchExists: true, url: normalized, repo: repo}, nil
	}

	switch {
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		return c.initEmptyCheckoutInternal(normalized, branch)
	case errors.Is(err, git.NoMatchingRefSpecError{}):
		return c.checkoutNewBranchInternal(ctx, normalized, branch, auth)
	default:
		return nil, err
	}
}

func (c *Client) initEmptyCheckoutInternal(url, branch string) (*WriteCheckout, error) {
	workDir := c.workDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, errors.WrapIf(err, "failed to create work dir")
	}
	repoPath, err := os.MkdirTemp(workDir, cloneScratchPrefix+"*")
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create temp dir")
	}
	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		_ = os.RemoveAll(repoPath)
		return nil, errors.WrapIf(err, "failed to initialize checkout")
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}}); err != nil {
		_ = os.RemoveAll(repoPath)
		return nil, errors.WrapIf(err, "failed to configure remote")
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))); err != nil {
		_ = os.RemoveAll(repoPath)
		return nil, errors.WrapIf(err, "failed to select branch")
	}
	return &WriteCheckout{RepoPath: repoPath, Branch: branch, url: url, repo: repo}, nil
}

func (c *Client) checkoutNewBranchInternal(ctx context.Context, url, branch string, auth AuthConfig) (*WriteCheckout, error) {
	repoPath, err := c.Clone(ctx, url, "", auth)
	if err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		_ = c.Cleanup(repoPath)
		return nil, errors.WrapIf(err, "failed to open checkout")
	}
	worktree, err := repo.Worktree()
	if err != nil {
		_ = c.Cleanup(repoPath)
		return nil, errors.WrapIf(err, "failed to open worktree")
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch), Create: true}); err != nil {
		_ = c.Cleanup(repoPath)
		return nil, errors.WrapIff(err, "failed to create branch %s", branch)
	}
	return &WriteCheckout{RepoPath: repoPath, Branch: branch, url: url, repo: repo}, nil
}

// CommitAndPush writes the requested files, removes the listed paths, commits
// when the tree changed, pushes without force, and verifies the remote branch
// points at the pushed commit. It returns the resulting head and whether a new
// commit was created.
func (c *Client) CommitAndPush(ctx context.Context, checkout *WriteCheckout, req CommitRequest, auth AuthConfig) (string, bool, error) {
	if checkout == nil || checkout.repo == nil {
		return "", false, errors.New("checkout is not prepared for writing")
	}
	worktree, err := checkout.repo.Worktree()
	if err != nil {
		return "", false, errors.WrapIf(err, "failed to open worktree")
	}
	if err := stageCommitFilesInternal(ctx, checkout, worktree, req); err != nil {
		return "", false, err
	}

	signature := &object.Signature{Name: req.AuthorName, Email: req.AuthorEmail, When: time.Now()}
	hash, err := worktree.Commit(req.Message, &git.CommitOptions{Author: signature, Committer: signature})
	if err != nil {
		if errors.Is(err, git.ErrEmptyCommit) {
			return checkout.HeadCommit, false, nil
		}
		return "", false, errors.WrapIf(err, "failed to commit")
	}
	if err := c.pushBranchInternal(ctx, checkout, hash.String(), auth); err != nil {
		return "", false, err
	}
	checkout.HeadCommit = hash.String()
	checkout.BranchExists = true
	return hash.String(), true, nil
}

// stageCommitFilesInternal writes through acfs so a symlink committed in the
// repository can never redirect a write outside the checkout.
func stageCommitFilesInternal(ctx context.Context, checkout *WriteCheckout, worktree *git.Worktree, req CommitRequest) error {
	for _, file := range req.Files {
		if err := ValidatePath(checkout.RepoPath, file.Path); err != nil {
			return err
		}
		logical := path.Join("/", filepath.ToSlash(file.Path))
		if err := acfs.MkdirAll(ctx, checkout.RepoPath, path.Dir(logical), 0o755); err != nil {
			return errors.WrapIff(err, "failed to create directory for %s", file.Path)
		}
		mode := os.FileMode(0o644)
		if file.Executable {
			mode = 0o755
		}
		if _, err := acfs.WriteFrom(ctx, checkout.RepoPath, logical, bytes.NewReader(file.Content), int64(len(file.Content)), mode); err != nil {
			return errors.WrapIff(err, "failed to write %s", file.Path)
		}
		if _, err := worktree.Add(file.Path); err != nil {
			return errors.WrapIff(err, "failed to stage %s", file.Path)
		}
	}
	for _, removed := range req.Remove {
		if err := ValidatePath(checkout.RepoPath, removed); err != nil {
			return err
		}
		if _, err := acfs.Stat(ctx, checkout.RepoPath, path.Join("/", filepath.ToSlash(removed)), false); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return errors.WrapIff(err, "failed to inspect %s", removed)
		}
		if _, err := worktree.Remove(removed); err != nil {
			return errors.WrapIff(err, "failed to remove %s", removed)
		}
	}
	return nil
}

// pushBranchInternal pushes the checkout branch without force and verifies the
// remote now points at commit.
func (c *Client) pushBranchInternal(ctx context.Context, checkout *WriteCheckout, commit string, auth AuthConfig) error {
	authMethod, err := c.getAuthInternal(checkout.url, auth)
	if err != nil {
		return err
	}
	refName := plumbing.NewBranchReferenceName(checkout.Branch)
	pushOptions := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(refName.String() + ":" + refName.String())},
	}
	if authMethod != nil {
		pushOptions.Auth = authMethod
	}
	if err := checkout.repo.PushContext(ctx, pushOptions); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		if isPushRejectedInternal(err) {
			return errors.WrapIf(ErrPushRejected, err.Error())
		}
		return errors.WrapIf(err, "failed to push")
	}

	remoteHead, exists, err := c.RemoteBranchHead(ctx, checkout.url, checkout.Branch, auth)
	if err != nil {
		return errors.WrapIf(err, "failed to verify pushed commit")
	}
	if !exists || remoteHead != commit {
		return errors.WrapIf(ErrPushRejected, "remote branch does not point at the pushed commit")
	}
	return nil
}

func isPushRejectedInternal(err error) bool {
	if errors.Is(err, git.ErrNonFastForwardUpdate) || errors.Is(err, git.ErrForceNeeded) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "non-fast-forward") || strings.Contains(message, "command error") || strings.Contains(message, "failed to update ref")
}

// RemoteBranchHead resolves the remote head of branch without cloning.
func (c *Client) RemoteBranchHead(ctx context.Context, url, branch string, auth AuthConfig) (string, bool, error) {
	refs, err := c.listRemoteReferences(ctx, url, auth)
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return "", false, nil
		}
		return "", false, err
	}
	refName := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == refName {
			return ref.Hash().String(), true, nil
		}
	}
	return "", false, nil
}

// DirectoryHistory lists the newest commits that touched directory, newest first.
func (c *Client) DirectoryHistory(ctx context.Context, repoPath, directory string, limit int) ([]HistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to open repository")
	}
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, errors.WrapIf(err, "failed to resolve head")
	}
	prefix := directoryPrefixInternal(directory)
	iter, err := repo.Log(&git.LogOptions{From: head.Hash(), PathFilter: func(p string) bool { return strings.HasPrefix(p, prefix) }})
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read history")
	}
	defer iter.Close()

	entries := make([]HistoryEntry, 0, max(limit, 0))
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, err := iter.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, errors.WrapIf(err, "failed to iterate history")
		}
		files, err := commitDirectoryFilesInternal(commit, prefix)
		if err != nil {
			return nil, err
		}
		entries = append(entries, historyEntryInternal(commit, files))
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries, nil
}

// CommitDiff returns the commit and the per-file unified diffs inside directory.
func (c *Client) CommitDiff(ctx context.Context, repoPath, commitHash, directory string) (HistoryEntry, []FileDiff, error) {
	if err := ctx.Err(); err != nil {
		return HistoryEntry{}, nil, err
	}
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return HistoryEntry{}, nil, errors.WrapIf(err, "failed to open repository")
	}
	commit, err := repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return HistoryEntry{}, nil, errors.WrapIf(err, "commit not found")
	}
	prefix := directoryPrefixInternal(directory)
	patch, err := commitPatchInternal(commit)
	if err != nil {
		return HistoryEntry{}, nil, err
	}

	var files []string
	var diffs []FileDiff
	for _, filePatch := range patch.FilePatches() {
		name, relative, ok := patchPathInDirectoryInternal(filePatch, prefix)
		if !ok {
			continue
		}
		files = append(files, relative)
		single, err := commitFilePatchInternal(commit, name)
		if err != nil {
			return HistoryEntry{}, nil, err
		}
		diffs = append(diffs, FileDiff{Path: relative, Patch: single})
	}
	return historyEntryInternal(commit, files), diffs, nil
}

func directoryPrefixInternal(directory string) string {
	cleaned := strings.Trim(path.Clean(filepath.ToSlash(directory)), "/")
	if cleaned == "" || cleaned == "." {
		return ""
	}
	return cleaned + "/"
}

func historyEntryInternal(commit *object.Commit, files []string) HistoryEntry {
	sort.Strings(files)
	if files == nil {
		files = []string{}
	}
	return HistoryEntry{
		Hash:    commit.Hash.String(),
		Author:  commit.Author.Name,
		Email:   commit.Author.Email,
		Message: strings.TrimSpace(commit.Message),
		Date:    commit.Author.When,
		Files:   files,
	}
}

func commitPatchInternal(commit *object.Commit) (*object.Patch, error) {
	var parentTree *object.Tree
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to load parent commit")
		}
		parentTree, err = parent.Tree()
		if err != nil {
			return nil, errors.WrapIf(err, "failed to load parent tree")
		}
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, errors.WrapIf(err, "failed to load commit tree")
	}
	patch, err := parentTree.Patch(tree)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to diff commit")
	}
	return patch, nil
}

func commitDirectoryFilesInternal(commit *object.Commit, prefix string) ([]string, error) {
	patch, err := commitPatchInternal(commit)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, filePatch := range patch.FilePatches() {
		if _, relative, ok := patchPathInDirectoryInternal(filePatch, prefix); ok {
			files = append(files, relative)
		}
	}
	return files, nil
}

// patchPathInDirectoryInternal picks the side of a file patch that lives under
// prefix, so renames into or out of the directory are still reported.
func patchPathInDirectoryInternal(filePatch diff.FilePatch, prefix string) (string, string, bool) {
	from, to := filePatch.Files()
	for _, file := range []diff.File{to, from} {
		if file == nil {
			continue
		}
		if relative, ok := strings.CutPrefix(file.Path(), prefix); ok {
			return file.Path(), relative, true
		}
	}
	return "", "", false
}

func commitFilePatchInternal(commit *object.Commit, name string) (string, error) {
	var parentTree *object.Tree
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return "", errors.WrapIf(err, "failed to load parent commit")
		}
		parentTree, err = parent.Tree()
		if err != nil {
			return "", errors.WrapIf(err, "failed to load parent tree")
		}
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", errors.WrapIf(err, "failed to load commit tree")
	}
	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return "", errors.WrapIf(err, "failed to diff commit")
	}
	for _, change := range changes {
		if change.From.Name != name && change.To.Name != name {
			continue
		}
		patch, err := change.Patch()
		if err != nil {
			return "", errors.WrapIff(err, "failed to build patch for %s", name)
		}
		return patch.String(), nil
	}
	return "", nil
}
