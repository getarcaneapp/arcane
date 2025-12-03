package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// TestConnection tests if the git repository is accessible
func TestConnection(ctx context.Context, repoURL, branch, username, token string) error {
	// Use git ls-remote equivalent to test connection without cloning
	remote := gogit.NewRemote(nil, &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})

	listOpts := &gogit.ListOptions{}
	if username != "" || token != "" {
		listOpts.Auth = &http.BasicAuth{
			Username: username,
			Password: token,
		}
	}

	refs, err := remote.ListContext(ctx, listOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to repository: %w", err)
	}

	// Check if the branch exists
	if branch != "" {
		branchRef := fmt.Sprintf("refs/heads/%s", branch)
		found := false
		for _, ref := range refs {
			if ref.Name().String() == branchRef {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("branch '%s' not found in repository", branch)
		}
	}

	return nil
}

// SyncRepository clones or pulls a git repository
func SyncRepository(ctx context.Context, repoURL, branch, username, token, composePath string) error {
	repoDir := getRepositoryDir(repoURL)

	auth := buildAuth(username, token)

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		if err := cloneRepository(ctx, repoURL, branch, repoDir, auth); err != nil {
			return err
		}
	} else {
		if err := pullRepository(ctx, repoDir, branch, auth); err != nil {
			return err
		}
	}

	composeFile := filepath.Join(repoDir, composePath)
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found at path: %s", composePath)
	}

	return nil
}

// GetRepositoryPath returns the path where the repository is cloned
func GetRepositoryPath(repoURL string) string {
	return getRepositoryDir(repoURL)
}

func buildAuth(username, token string) *http.BasicAuth {
	if username == "" && token == "" {
		return nil
	}
	// For GitHub PATs, username can be anything non-empty
	if username == "" {
		username = "git"
	}
	return &http.BasicAuth{
		Username: username,
		Password: token,
	}
}

func cloneRepository(ctx context.Context, repoURL, branch, targetDir string, auth *http.BasicAuth) error {
	cloneOpts := &gogit.CloneOptions{
		URL:   repoURL,
		Depth: 1,
	}

	if auth != nil {
		cloneOpts.Auth = auth
	}

	if branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOpts.SingleBranch = true
	}

	_, err := gogit.PlainCloneContext(ctx, targetDir, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

func pullRepository(ctx context.Context, repoDir, branch string, auth *http.BasicAuth) error {
	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	pullOpts := &gogit.PullOptions{
		RemoteName: "origin",
		Force:      true,
	}

	if auth != nil {
		pullOpts.Auth = auth
	}

	if branch != "" {
		pullOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		pullOpts.SingleBranch = true
	}

	err = worktree.PullContext(ctx, pullOpts)
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return fmt.Errorf("failed to pull repository: %w", err)
	}

	return nil
}

func getRepositoryDir(repoURL string) string {
	// Create a sanitized directory name from repository URL
	// Remove protocol and special characters
	dirName := repoURL
	dirName = strings.TrimPrefix(dirName, "https://")
	dirName = strings.TrimPrefix(dirName, "http://")
	dirName = strings.TrimPrefix(dirName, "git@")
	dirName = strings.ReplaceAll(dirName, ":", "_")
	dirName = strings.ReplaceAll(dirName, "/", "_")
	dirName = strings.TrimSuffix(dirName, ".git")

	// Use a base directory for all git repositories
	// Priority: GITOPS_REPOS_DIR env var, then DATA_DIR/gitops-repos, then fallback
	baseDir := os.Getenv("GITOPS_REPOS_DIR")
	if baseDir == "" {
		dataDir := os.Getenv("DATA_DIR")
		if dataDir != "" {
			baseDir = filepath.Join(dataDir, "gitops-repos")
		} else {
			baseDir = "data/gitops-repos"
		}
	}

	return filepath.Join(baseDir, dirName)
}
