package git

import (
	"errors"
	"fmt"
	"net"
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
	"github.com/gofrs/flock"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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

// SSH host key verification modes
const (
	SSHHostKeyVerificationStrict    = "strict"     // Require host key in known_hosts
	SSHHostKeyVerificationAcceptNew = "accept_new" // Auto-add unknown host keys
	SSHHostKeyVerificationSkip      = "skip"       // Skip host key verification (insecure)
)

// AuthConfig holds authentication configuration
type AuthConfig struct {
	AuthType               string
	Username               string
	Token                  string
	SSHKey                 string
	SSHHostKeyVerification string // strict, accept_new, skip
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

			// Configure host key verification based on mode
			hostKeyCallback, err := c.getSSHHostKeyCallback(config.SSHHostKeyVerification)
			if err != nil {
				return nil, fmt.Errorf("failed to configure SSH host key verification: %w", err)
			}
			publicKeys.HostKeyCallbackHelper = ssh.HostKeyCallbackHelper{
				HostKeyCallback: hostKeyCallback,
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

// getSSHHostKeyCallback returns the appropriate SSH host key callback based on verification mode
func (c *Client) getSSHHostKeyCallback(mode string) (gossh.HostKeyCallback, error) {
	switch mode {
	case SSHHostKeyVerificationStrict:
		// Use known_hosts verification respecting SSH_KNOWN_HOSTS env var
		return knownhosts.New(getKnownHostsPath())
	case SSHHostKeyVerificationSkip:
		// Skip host key verification - intentionally insecure, user explicitly opted in via UI
		return gossh.InsecureIgnoreHostKey(), nil //nolint:gosec // User explicitly chose to skip verification
	case SSHHostKeyVerificationAcceptNew, "":
		// Default: accept and remember new host keys
		return c.createAcceptNewHostKeyCallback()
	default:
		// Fall back to accept_new for unknown modes
		return c.createAcceptNewHostKeyCallback()
	}
}

// createAcceptNewHostKeyCallback creates a callback that accepts new host keys and saves them
func (c *Client) createAcceptNewHostKeyCallback() (gossh.HostKeyCallback, error) {
	knownHostsPath := getKnownHostsPath()

	// Ensure the directory exists
	dir := filepath.Dir(knownHostsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create known_hosts directory: %w", err)
	}

	// Create the file if it doesn't exist
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		file, err := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to create known_hosts file: %w", err)
		}
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close known_hosts file %s: %v\n", knownHostsPath, err)
		}
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		// Re-read known_hosts on each call to handle concurrent modifications
		existingCallback, err := knownhosts.New(knownHostsPath)
		if err != nil {
			existingCallback = nil
		}

		// Check if the host is already known
		if existingCallback != nil {
			err := existingCallback(hostname, remote, key)
			if err == nil {
				return nil // Host key matches
			}
			// Check if it's a "key mismatch" error vs "unknown host"
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
				// Host is known but key doesn't match - this is a security concern
				return fmt.Errorf("host key mismatch for %s (possible MITM attack): %w", hostname, err)
			}
			// Otherwise, host is unknown - we'll add it
		}

		// Add the new host key to known_hosts
		if err := addHostKey(knownHostsPath, hostname, key); err != nil {
			// Log the error but don't fail - still allow the connection
			// The host key just won't be remembered for next time
			fmt.Fprintf(os.Stderr, "Warning: failed to save host key for %s: %v\n", hostname, err)
		}

		return nil
	}, nil
}

// getKnownHostsPath returns the path to the known_hosts file
func getKnownHostsPath() string {
	// Check environment variable first
	if path := os.Getenv("SSH_KNOWN_HOSTS"); path != "" {
		return path
	}

	// Fall back to default location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Use a fallback in the working directory
		return filepath.Join(os.TempDir(), ".ssh", "known_hosts")
	}
	return filepath.Join(homeDir, ".ssh", "known_hosts")
}

// addHostKey adds a host key to the known_hosts file
func addHostKey(knownHostsPath, hostname string, key gossh.PublicKey) (err error) {
	// Format the known_hosts line
	line := knownhosts.Line([]string{hostname}, key)

	// Acquire exclusive lock to prevent concurrent writes
	fileLock := flock.New(knownHostsPath)
	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock on known_hosts file: %w", err)
	}
	defer fileLock.Unlock() //nolint:errcheck

	// Append to the file
	file, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open known_hosts file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close known_hosts file: %w", cerr)
		}
	}()

	if _, err := file.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("failed to write to known_hosts file: %w", err)
	}

	return nil
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
