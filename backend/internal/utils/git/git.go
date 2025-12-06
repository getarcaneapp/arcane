package git

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"go.getarcane.app/types/gitops"
)

// Client handles git operations
type Client struct {
	workDir string
}

// NewClient creates a new git client
func NewClient(workDir string) *Client {
	return &Client{
		workDir: workDir,
	}
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	AuthType string
	Username string
	Token    string
	SSHKey   string
}

// getAuth returns the appropriate transport.AuthMethod
func (c *Client) getAuth(config AuthConfig) (transport.AuthMethod, error) {
	switch config.AuthType {
	case "http":
		if config.Token != "" {
			return &http.BasicAuth{
				Username: config.Username,
				Password: config.Token,
			}, nil
		}
		return nil, nil
	case "ssh":
		if config.SSHKey != "" {
			publicKeys, err := ssh.NewPublicKeys("git", []byte(config.SSHKey), "")
			if err != nil {
				return nil, fmt.Errorf("failed to create ssh auth: %w", err)
			}
			return publicKeys, nil
		}
		return nil, fmt.Errorf("ssh key required for ssh authentication")
	case "none":
		return nil, nil
	default:
		return nil, nil
	}
}

// Clone clones a repository to a temporary directory
func (c *Client) Clone(url, branch string, auth AuthConfig) (string, error) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp(c.workDir, "gitops-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	authMethod, err := c.getAuth(auth)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	cloneOptions := &git.CloneOptions{
		URL:      url,
		Progress: nil,
	}

	if authMethod != nil {
		cloneOptions.Auth = authMethod
	}

	if branch != "" {
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOptions.SingleBranch = true
	}

	_, err = git.PlainClone(tmpDir, false, cloneOptions)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return tmpDir, nil
}

// Pull pulls the latest changes from a repository
func (c *Client) Pull(repoPath string, auth AuthConfig) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	authMethod, err := c.getAuth(auth)
	if err != nil {
		return err
	}

	pullOptions := &git.PullOptions{
		RemoteName: "origin",
		Progress:   nil,
	}

	if authMethod != nil {
		pullOptions.Auth = authMethod
	}

	err = w.Pull(pullOptions)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull repository: %w", err)
	}

	return nil
}

// ValidatePath ensures the path is safe and doesn't escape the repo
func ValidatePath(repoPath, requestedPath string) error {
	// Clean the paths
	cleanRepoPath := filepath.Clean(repoPath)
	cleanRequestedPath := filepath.Clean(filepath.Join(repoPath, requestedPath))

	// Check if the requested path is within the repo
	if !strings.HasPrefix(cleanRequestedPath, cleanRepoPath) {
		return fmt.Errorf("path traversal attempt detected")
	}

	return nil
}

// BrowseTree returns the file tree at the specified path
func (c *Client) BrowseTree(repoPath, targetPath string) ([]gitops.FileTreeNode, error) {
	// Validate path to prevent traversal
	if err := ValidatePath(repoPath, targetPath); err != nil {
		return nil, err
	}

	fullPath := filepath.Join(repoPath, targetPath)

	// Check if path exists
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("path not found: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory")
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var nodes []gitops.FileTreeNode
	for _, entry := range entries {
		// Skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		nodeType := "file"
		if entry.IsDir() {
			nodeType = "directory"
		}

		relativePath := filepath.Join(targetPath, entry.Name())
		node := gitops.FileTreeNode{
			Name: entry.Name(),
			Path: relativePath,
			Type: nodeType,
			Size: info.Size(),
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetFileContent reads a file from the repository
func (c *Client) GetFileContent(repoPath, filePath string) (string, error) {
	// Validate path to prevent traversal
	if err := ValidatePath(repoPath, filePath); err != nil {
		return "", err
	}

	fullPath := filepath.Join(repoPath, filePath)

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// CopyFile copies a file from source to destination
func (c *Client) CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Create destination directory if it doesn't exist
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// Cleanup removes a temporary repository directory
func (c *Client) Cleanup(repoPath string) error {
	return os.RemoveAll(repoPath)
}

// GetLastCommitInfo retrieves information about the last commit
func (c *Client) GetLastCommitInfo(repoPath string) (*CommitInfo, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &CommitInfo{
		Hash:    commit.Hash.String(),
		Author:  commit.Author.Name,
		Message: commit.Message,
		Date:    commit.Author.When,
	}, nil
}

// CommitInfo holds information about a git commit
type CommitInfo struct {
	Hash    string
	Author  string
	Message string
	Date    time.Time
}

// TestConnection tests if the repository can be accessed with the given credentials
func (c *Client) TestConnection(url, branch string, auth AuthConfig) error {
	tmpDir, err := c.Clone(url, branch, auth)
	if err != nil {
		return err
	}
	defer c.Cleanup(tmpDir)
	return nil
}

// ListBranches lists all branches in the repository
func (c *Client) ListBranches(repoPath string) ([]string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	refs, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var branches []string
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		branches = append(branches, ref.Name().Short())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate branches: %w", err)
	}

	return branches, nil
}

// FileExists checks if a file exists in the repository
func (c *Client) FileExists(repoPath, filePath string) bool {
	if err := ValidatePath(repoPath, filePath); err != nil {
		return false
	}

	fullPath := filepath.Join(repoPath, filePath)
	_, err := os.Stat(fullPath)
	return err == nil
}

// GetLatestCommitForFile gets the latest commit that modified a specific file
func (c *Client) GetLatestCommitForFile(repoPath, filePath string) (*CommitInfo, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commits, err := repo.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	var latestCommit *object.Commit
	err = commits.ForEach(func(commit *object.Commit) error {
		// Check if this commit modified the file
		_, err := commit.File(filePath)
		if err == nil {
			latestCommit = commit
			return fmt.Errorf("found") // Stop iteration
		}
		return nil
	})

	if latestCommit == nil {
		return nil, fmt.Errorf("no commits found for file")
	}

	return &CommitInfo{
		Hash:    latestCommit.Hash.String(),
		Author:  latestCommit.Author.Name,
		Message: latestCommit.Message,
		Date:    latestCommit.Author.When,
	}, nil
}
