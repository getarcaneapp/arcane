package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TestConnection tests if the git repository is accessible
func TestConnection(ctx context.Context, repoURL, branch, username, token string) error {
	// Use git ls-remote to test connection without cloning
	authenticatedURL := buildAuthenticatedURL(repoURL, username, token)

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", authenticatedURL, branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to connect to repository: %w (output: %s)", err, string(output))
	}

	// Check if the branch exists
	if branch != "" && !strings.Contains(string(output), branch) {
		return fmt.Errorf("branch '%s' not found in repository", branch)
	}

	return nil
}

// SyncRepository clones or pulls a git repository
func SyncRepository(ctx context.Context, repoURL, branch, username, token, composePath string) error {
	authenticatedURL := buildAuthenticatedURL(repoURL, username, token)

	// Generate a unique directory name based on repository URL
	repoDir := getRepositoryDir(repoURL)

	// Check if repository already exists
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// Clone the repository
		if err := cloneRepository(ctx, authenticatedURL, branch, repoDir); err != nil {
			return err
		}
	} else {
		// Pull latest changes
		if err := pullRepository(ctx, repoDir, branch); err != nil {
			return err
		}
	}

	// Verify compose file exists
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

func cloneRepository(ctx context.Context, authenticatedURL, branch, targetDir string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, "--depth", "1", authenticatedURL, targetDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w (output: %s)", err, string(output))
	}

	return nil
}

func pullRepository(ctx context.Context, repoDir, branch string) error {
	// Fetch latest changes
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to fetch repository: %w (output: %s)", err, string(output))
	}

	// Reset to origin branch
	var resetTarget string
	if branch != "" {
		resetTarget = fmt.Sprintf("origin/%s", branch)
		// #nosec G204 -- branch is validated against refs/heads/* by git itself during fetch
		cmd = exec.CommandContext(ctx, "git", "-C", repoDir, "reset", "--hard", resetTarget)
	} else {
		// #nosec G204 -- static string, no user input
		cmd = exec.CommandContext(ctx, "git", "-C", repoDir, "reset", "--hard", "origin/HEAD")
	}
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to reset repository: %w (output: %s)", err, string(output))
	}

	return nil
}

func buildAuthenticatedURL(repoURL, username, token string) string {
	// If no credentials provided, return original URL
	if username == "" && token == "" {
		return repoURL
	}

	// Parse the URL and inject credentials
	// Support for https://github.com/user/repo.git format
	if strings.HasPrefix(repoURL, "https://") {
		url := strings.TrimPrefix(repoURL, "https://")
		if token != "" {
			// GitHub personal access tokens can be used as username with empty password
			// or as password with username
			if username != "" {
				return fmt.Sprintf("https://%s:%s@%s", username, token, url)
			}
			return fmt.Sprintf("https://%s@%s", token, url)
		}
	}

	return repoURL
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
