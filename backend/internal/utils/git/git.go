package git

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/gitops"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
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
	workDir := c.workDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	// Ensure the work directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create work dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(workDir, "gitops-*")
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

// GetCurrentCommit returns the HEAD commit hash of a cloned repository
func (c *Client) GetCurrentCommit(repoPath string) (string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	return ref.Hash().String(), nil
}

// BranchInfo holds information about a git branch
type BranchInfo struct {
	Name      string
	IsDefault bool
}

// ListBranches lists all branches in a remote repository
func (c *Client) ListBranches(url string, auth AuthConfig) ([]BranchInfo, error) {
	authMethod, err := c.getAuth(auth)
	if err != nil {
		return nil, err
	}

	// Create a remote without cloning
	rem := git.NewRemote(nil, &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	listOptions := &git.ListOptions{}
	if authMethod != nil {
		listOptions.Auth = authMethod
	}

	refs, err := rem.List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote references: %w", err)
	}

	var branches []BranchInfo
	var defaultBranch string

	// Find the default branch (HEAD points to it)
	for _, ref := range refs {
		if ref.Name().String() == "HEAD" {
			// HEAD is a symbolic reference that points to the default branch
			if ref.Target().IsBranch() {
				defaultBranch = ref.Target().Short()
			}
			break
		}
	}

	// Collect all branches
	seen := make(map[string]bool)
	for _, ref := range refs {
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			if seen[branchName] {
				continue
			}
			seen[branchName] = true

			branches = append(branches, BranchInfo{
				Name:      branchName,
				IsDefault: branchName == defaultBranch,
			})
		}
	}

	// Sort branches with default first
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].IsDefault {
			return true
		}
		if branches[j].IsDefault {
			return false
		}
		return branches[i].Name < branches[j].Name
	})

	return branches, nil
}

// ValidatePath ensures the path is safe and doesn't escape the repo
func ValidatePath(repoPath, requestedPath string) error {
	// Clean the paths
	cleanRepoPath := filepath.Clean(repoPath)
	cleanRequestedPath := filepath.Clean(filepath.Join(repoPath, requestedPath))

	// Check if the requested path is within the repo using relative path validation
	rel, err := filepath.Rel(cleanRepoPath, cleanRequestedPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+".."+string(filepath.Separator)) {
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

		nodeType := gitops.FileTreeNodeTypeFile
		if entry.IsDir() {
			nodeType = gitops.FileTreeNodeTypeDirectory
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

// Cleanup removes a temporary repository directory
func (c *Client) Cleanup(repoPath string) error {
	return os.RemoveAll(repoPath)
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
	defer func() {
		_ = c.Cleanup(tmpDir)
	}()
	return nil
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

// ReadFile reads a file from the repository
func (c *Client) ReadFile(repoPath, filePath string) (string, error) {
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
