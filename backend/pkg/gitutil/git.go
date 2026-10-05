package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	nethttp "net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/gofrs/flock"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	kit "go.getarcane.app/kit/pkg"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	// binarySniffBytes is how much of a file is inspected to classify it as binary.
	binarySniffBytes = 512

	// cloneScratchPrefix names the per-run clone scratch dirs created by Clone.
	cloneScratchPrefix = "gitops-"

	errUnsupportedURL = "repository URL must use http(s)://, ssh://, git://, or git@host:path"

	// SSH host key verification modes

	SSHHostKeyVerificationStrict    = "strict"     // Require host key in known_hosts
	SSHHostKeyVerificationAcceptNew = "accept_new" // Auto-add unknown host keys
	SSHHostKeyVerificationSkip      = "skip"       // Skip host key verification (insecure)
	defaultKnownHostsDataDir        = "/app/data"
	defaultKnownHostsPath           = "/app/data/.ssh/known_hosts"
)

// go-git's file transport execs the git binary, which doesn't exist in the
// distroless image. Unregister it so a repository URL can never reach it.
func init() {
	client.InstallProtocol("file", nil)

	// go-git strips multi_ack/multi_ack_detailed from pack negotiation by
	// default, which Azure DevOps rejects by sending an incomplete pack
	// ("invalid reset option: object not found" on clone). Advertise them and
	// keep only thin-pack disabled, matching Flux/ArgoCD; other hosts are
	// unaffected.
	transport.UnsupportedCapabilities = []capability.Capability{
		capability.ThinPack,
	}
}

// Client handles git operations
type Client struct {
	workDir string
}

var scpLikeURLPattern = regexp.MustCompile(`^[^@/]+@[^:/]+:`)

// normalizeURLInternal coerces a repository URL into a form go-git resolves to a
// network transport, never the file transport (which execs the git binary).
// Schemeless host-style URLs (e.g. github.com/org/repo.git) get https://.
func normalizeURLInternal(raw string) (string, error) {
	url := strings.TrimSpace(raw)
	if url == "" {
		return "", errors.New("repository URL is empty")
	}

	if scheme, _, found := strings.Cut(url, "://"); found {
		switch strings.ToLower(scheme) {
		case "http", "https", "ssh", "git":
			return url, nil
		default:
			return "", errors.New(errUnsupportedURL)
		}
	}

	if scpLikeURLPattern.MatchString(url) {
		return url, nil
	}

	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") || strings.HasPrefix(url, "~") {
		return "", errors.New(errUnsupportedURL)
	}

	return "https://" + url, nil
}

// NewClient creates a new git client
func NewClient(workDir string) *Client {
	return &Client{
		workDir: workDir,
	}
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	AuthType               string
	Username               string
	Token                  string
	SSHKey                 string
	SSHHostKeyVerification string // strict, accept_new, skip
}

// getAuthInternal returns the appropriate transport.AuthMethod.
func (c *Client) getAuthInternal(ctx context.Context, url string, localConfig AuthConfig) (transport.AuthMethod, error) {
	switch localConfig.AuthType {
	case "http":
		if localConfig.Token != "" {
			return &githttp.BasicAuth{
				Username: localConfig.Username,
				Password: localConfig.Token,
			}, nil
		}
		return nil, nil
	case "ssh":
		if localConfig.SSHKey != "" {
			endpoint, err := transport.NewEndpoint(url)
			if err != nil {
				return nil, fmt.Errorf("failed to parse SSH repository URL: %w", err)
			}
			username := cmp.Or(endpoint.User, "git")
			publicKeys, err := ssh.NewPublicKeys(username, []byte(localConfig.SSHKey), "")
			if err != nil {
				return nil, fmt.Errorf("failed to create ssh auth: %w", err)
			}

			// Configure host key verification based on mode
			hostKeyCallback, err := c.getSSHHostKeyCallbackInternal(ctx, localConfig.SSHHostKeyVerification)
			if err != nil {
				return nil, fmt.Errorf("failed to configure SSH host key verification: %w", err)
			}
			publicKeys.HostKeyCallbackHelper = ssh.HostKeyCallbackHelper{
				HostKeyCallback: hostKeyCallback,
			}

			return publicKeys, nil
		}
		return nil, errors.New("ssh key required for ssh authentication")
	case "none":
		return nil, nil
	default:
		return nil, nil
	}
}

// getSSHHostKeyCallbackInternal returns the SSH host key callback for the verification mode.
func (c *Client) getSSHHostKeyCallbackInternal(ctx context.Context, mode string) (gossh.HostKeyCallback, error) {
	if mode == SSHHostKeyVerificationSkip {
		// Skip host key verification - intentionally insecure, user explicitly opted in via UI
		return gossh.InsecureIgnoreHostKey(), nil //nolint:gosec // User explicitly chose to skip verification
	}

	knownHostsPaths, pathErr := getKnownHostsPathsInternal(
		c.workDir,
		os.Getenv,
		os.Stat,
		os.UserHomeDir,
	)
	if pathErr != nil {
		return nil, pathErr
	}
	if mode == SSHHostKeyVerificationStrict {
		return knownhosts.New(knownHostsPaths...)
	}

	// Other modes default to accepting and remembering new host keys.
	// Ensure the directory exists
	// os.* supports home and override paths outside acfs roots, plus append/flock.
	knownHostsPath := knownHostsPaths[0]
	dir := filepath.Dir(knownHostsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create known_hosts directory: %w", err)
	}

	// Create the file if it doesn't exist
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		file, openFileErr := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if openFileErr != nil {
			return nil, fmt.Errorf("failed to create known_hosts file: %w", openFileErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close known_hosts file", "path", knownHostsPath, "error", closeErr)
		}
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		// Re-read known_hosts on each call to handle concurrent modifications.
		// Unreadable trust must fail closed so changed keys are never accepted.
		existingCallback, err := knownhosts.New(knownHostsPaths...)
		if err != nil {
			return fmt.Errorf("failed to read known_hosts: %w", err)
		}

		existingCallbackErr := existingCallback(hostname, remote, key)
		if existingCallbackErr == nil {
			return nil // Host key matches
		}
		// Check if it's a "key mismatch" error vs "unknown host"
		if keyErr, ok := errors.AsType[*knownhosts.KeyError](existingCallbackErr); ok && len(keyErr.Want) > 0 {
			// Host is known but key doesn't match - this is a security concern
			return fmt.Errorf("host key mismatch for %s (possible MITM attack): %w", hostname, existingCallbackErr)
		}
		// Otherwise, host is unknown - we'll add it

		// Log persistence failures after closing the file and releasing its lock.
		var saveErr error
		defer func() {
			if saveErr != nil {
				slog.WarnContext(ctx, "Failed to save host key", "hostname", hostname, "error", saveErr)
			}
		}()

		fileLock := flock.New(knownHostsPath)
		if lockErr := fileLock.Lock(); lockErr != nil {
			saveErr = fmt.Errorf("failed to acquire lock on known_hosts file: %w", lockErr)
			return nil
		}
		defer func() {
			if unlockErr := fileLock.Unlock(); unlockErr != nil && saveErr == nil {
				saveErr = fmt.Errorf("failed to release lock on known_hosts file: %w", unlockErr)
			}
		}()

		// acfs has no append API, and override paths can be outside its roots.
		file, openErr := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if openErr != nil {
			saveErr = fmt.Errorf("failed to open known_hosts file: %w", openErr)
			return nil
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil && saveErr == nil {
				saveErr = fmt.Errorf("failed to close known_hosts file: %w", closeErr)
			}
		}()
		line := knownhosts.Line([]string{hostname}, key)
		if _, writeErr := file.WriteString(line + "\n"); writeErr != nil {
			saveErr = fmt.Errorf("failed to write to known_hosts file: %w", writeErr)
		}
		return nil
	}, nil
}

// getKnownHostsPathsInternal returns trusted known_hosts files; new keys are saved to the first.
func getKnownHostsPathsInternal(
	workDir string,
	getenv func(string) string,
	stat func(string) (os.FileInfo, error),
	userHomeDir func() (string, error),
) ([]string, error) {
	// Check environment variable first
	if localPath := getenv("SSH_KNOWN_HOSTS"); localPath != "" {
		return []string{localPath}, nil
	}

	// Prefer Arcane's writable persistent data directory when it is available,
	// which is the case for published container images and PUID/PGID setups.
	if info, err := stat(defaultKnownHostsDataDir); err == nil && info.IsDir() {
		return []string{defaultKnownHostsPath}, nil
	}

	var homePath string
	homeExists := false
	homeDir, err := userHomeDir()
	if err == nil && homeDir != "" {
		homePath = filepath.Join(homeDir, ".ssh", "known_hosts")
		_, statErr := stat(homePath)
		switch {
		case statErr == nil:
			homeExists = true
		case errors.Is(statErr, fs.ErrNotExist):
		case workDir != "" && errors.Is(statErr, fs.ErrPermission):
			// Inaccessible home trust falls back to native storage.
		default:
			return nil, fmt.Errorf("failed to check existing known_hosts file %s: %w", homePath, statErr)
		}
	}

	if workDir == "" {
		if homePath != "" {
			return []string{homePath}, nil
		}
		// Last resort for environments without a resolvable home directory.
		return []string{filepath.Join(os.TempDir(), ".ssh", "known_hosts")}, nil
	}

	// Native storage keeps saved keys while honoring every readable trust file.
	workPath := filepath.Join(workDir, ".ssh", "known_hosts")
	_, workErr := stat(workPath)
	if workErr != nil && !errors.Is(workErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to check git known_hosts file %s: %w", workPath, workErr)
	}
	switch {
	case workErr == nil && homeExists:
		return []string{workPath, homePath}, nil
	case homeExists:
		return []string{homePath}, nil
	default:
		return []string{workPath}, nil
	}
}

// Clone clones a repository to a temporary directory
func (c *Client) Clone(ctx context.Context, url, branch string, auth AuthConfig) (string, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Create a temporary directory
	workDir := c.workDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	// Ensure the work directory exists
	// os.* rather than acfs: this creates the clone staging root itself (under the
	// system temp dir by default), which has to exist before acfs could open it.
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create work dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(workDir, cloneScratchPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	url, err = normalizeURLInternal(url)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	authMethod, err := c.getAuthInternal(ctx, url, auth)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
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

	_, err = git.PlainCloneContext(ctx, tmpDir, false, cloneOptions)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return tmpDir, nil
}

// GetCurrentCommit returns the HEAD commit hash of a cloned repository
func (c *Client) GetCurrentCommit(ctx context.Context, repoPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
func (c *Client) ListBranches(ctx context.Context, url string, auth AuthConfig) ([]BranchInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs, err := c.listRemoteReferencesInternal(ctx, url, auth)
	if err != nil {
		return nil, err
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

// ProbeRemote verifies that a remote repository is reachable without cloning it.
func (c *Client) ProbeRemote(ctx context.Context, url string, auth AuthConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := c.listRemoteReferencesInternal(ctx, url, auth)
	return err
}

func (c *Client) listRemoteReferencesInternal(ctx context.Context, url string, auth AuthConfig) ([]*plumbing.Reference, error) {
	url, err := normalizeURLInternal(url)
	if err != nil {
		return nil, err
	}

	authMethod, err := c.getAuthInternal(ctx, url, auth)
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

	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	refs, err := rem.ListContext(listCtx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote references: %w", err)
	}

	return refs, nil
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
		return errors.New("path traversal attempt detected")
	}

	return nil
}

// BrowseTree returns the file tree at the specified path. The clone directory
// is the confinement root: paths that escape it, and symbolic links pointing
// outside it, are rejected rather than followed.
func (c *Client) BrowseTree(ctx context.Context, repoPath, targetPath string) ([]gitops.FileTreeNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	logicalPath := path.Join("/", filepath.ToSlash(targetPath))
	entry, err := acfs.Stat(ctx, repoPath, logicalPath, true)
	if err != nil {
		return nil, fmt.Errorf("path not found: %w", err)
	}
	if !entry.IsDirectory {
		return nil, errors.New("path is not a directory")
	}

	entries, err := acfs.List(ctx, repoPath, logicalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var nodes []gitops.FileTreeNode
	for _, child := range entries {
		if errErr := ctx.Err(); errErr != nil {
			return nil, errErr
		}
		// Skip .git directory
		if child.Name == ".git" {
			continue
		}

		nodeType := kit.Ternary(child.IsDirectory, gitops.FileTreeNodeTypeDirectory, gitops.FileTreeNodeTypeFile)

		nodes = append(nodes, gitops.FileTreeNode{
			Name: child.Name,
			Path: filepath.Join(targetPath, child.Name),
			Type: nodeType,
			Size: child.Size,
		})
	}

	return nodes, nil
}

// Cleanup removes a temporary repository directory
// Stays on os.RemoveAll: acfs refuses to remove its own root, and here the whole
// clone directory itself is what gets deleted.
func (c *Client) Cleanup(repoPath string) error {
	return os.RemoveAll(repoPath)
}

// Discard removes a scratch checkout, logging instead of failing when removal breaks.
func (c *Client) Discard(ctx context.Context, repoPath string) {
	if err := c.Cleanup(repoPath); err != nil {
		slog.WarnContext(ctx, "Failed to cleanup repository", "path", repoPath, "error", err)
	}
}

// PurgeScratchDirs removes clone scratch dirs ("gitops-*") under the work dir
// whose mtime is older than maxAge. maxAge <= 0 removes all (boot sweep).
func (c *Client) PurgeScratchDirs(ctx context.Context, maxAge time.Duration) (int, error) {
	root := c.workDir
	if root == "" {
		root = os.TempDir()
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read git work dir %s: %w", root, err)
	}

	removed := 0
	for _, entry := range entries {
		if errErr := ctx.Err(); errErr != nil {
			return removed, errErr
		}
		entryPath := filepath.Join(root, entry.Name())
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), cloneScratchPrefix) {
			continue
		}
		if maxAge > 0 {
			info, infoErr := entry.Info()
			if infoErr != nil {
				slog.WarnContext(ctx, "Failed to stat git clone scratch dir", "path", entryPath, "error", infoErr)
				continue
			}
			if time.Since(info.ModTime()) < maxAge {
				continue
			}
		}
		if rmErr := os.RemoveAll(entryPath); rmErr != nil {
			slog.WarnContext(ctx, "Failed to remove git clone scratch dir", "path", entryPath, "error", rmErr)
			continue
		}
		removed++
	}
	return removed, nil
}

// CommitInfo holds information about a git commit
type CommitInfo struct {
	Hash    string
	Author  string
	Message string
	Date    time.Time
}

// TestConnection tests if the repository can be accessed with the given credentials
func (c *Client) TestConnection(ctx context.Context, url, branch string, auth AuthConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	tmpDir, err := c.Clone(ctx, url, branch, auth)
	if err != nil {
		return err
	}
	defer func() {
		_ = c.Cleanup(tmpDir)
	}()
	return nil
}

// FileExists checks if a file exists in the repository
func (c *Client) FileExists(ctx context.Context, repoPath, filePath string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	exists, err := acfs.Exists(ctx, repoPath, path.Join("/", filepath.ToSlash(filePath)))
	return err == nil && exists
}

// ReadFile reads a file from the repository
func (c *Client) ReadFile(ctx context.Context, repoPath, filePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := acfs.ReadFile(ctx, repoPath, path.Join("/", filepath.ToSlash(filePath)))
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

// SyncFileInfo holds information about a file to be synced
type SyncFileInfo struct {
	RelativePath string // Path relative to the sync directory
	Content      []byte
	Size         int64
	IsBinary     bool
	// Executable mirrors git's +x bit so callers can preserve it on the
	// destination. Required for lifecycle hooks: a script committed as
	// 100755 must arrive in the project workspace runnable, otherwise the
	// lifecycle runner fails to exec it.
	Executable bool
}

// DirectoryWalkResult holds the result of walking a directory for sync
type DirectoryWalkResult struct {
	Files           []SyncFileInfo
	TotalFiles      int
	TotalSize       int64
	SkippedBinaries int
}

// WalkDirectory walks the directory containing the compose file and returns all files.
// It enforces limits on file count, total size, and skips large binary files.
// The composePath is the path to the compose file within the repo - the directory
// containing this file will be walked.
func (c *Client) WalkDirectory(ctx context.Context, repoPath, composePath string,
	maxFiles int, maxTotalSize, maxBinarySize int64,
) (*DirectoryWalkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate compose path
	if err := ValidatePath(repoPath, composePath); err != nil {
		return nil, fmt.Errorf("invalid compose path: %w", err)
	}

	// Get the directory containing the compose file
	syncDir := filepath.Dir(filepath.Join(repoPath, composePath))

	result := &DirectoryWalkResult{
		Files: make([]SyncFileInfo, 0),
	}
	err := acfs.Walk(ctx, syncDir, "/", func(entry acfstypes.Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsSymlink {
			return nil
		}
		if entry.IsDirectory {
			if entry.Name == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		relativePath := strings.TrimPrefix(entry.Path, "/")
		if maxFiles > 0 && result.TotalFiles >= maxFiles {
			return fmt.Errorf("file count limit exceeded (max %d files)", maxFiles)
		}

		if maxBinarySize > 0 && entry.Size > maxBinarySize {
			reader, _, openErr := acfs.OpenRead(ctx, syncDir, entry.Path, binarySniffBytes)
			if openErr != nil {
				return fmt.Errorf("failed to inspect file %s: %w", relativePath, openErr)
			}
			buf := make([]byte, binarySniffBytes)
			n, readErr := reader.Read(buf)
			closeErr := reader.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("failed to inspect file %s: %w", relativePath, errors.Join(readErr, closeErr))
			}
			if closeErr != nil {
				return fmt.Errorf("failed to close file %s after inspection: %w", relativePath, closeErr)
			}
			if IsBinaryContent(buf[:n]) {
				result.SkippedBinaries++
				return nil
			}
		}

		content, readErr := acfs.ReadFile(ctx, syncDir, entry.Path)
		if readErr != nil {
			return fmt.Errorf("failed to read file %s: %w", relativePath, readErr)
		}

		fileSize := int64(len(content))
		isBinary := IsBinaryContent(content)
		if isBinary && maxBinarySize > 0 && fileSize > maxBinarySize {
			result.SkippedBinaries++
			return nil
		}
		if maxTotalSize > 0 && result.TotalSize+fileSize > maxTotalSize {
			return fmt.Errorf("total size limit exceeded (max %d bytes)", maxTotalSize)
		}

		result.Files = append(result.Files, SyncFileInfo{
			RelativePath: relativePath,
			Content:      content,
			Size:         fileSize,
			IsBinary:     isBinary,
			Executable:   os.FileMode(entry.UnixMode)&0o111 != 0,
		})
		result.TotalFiles++
		result.TotalSize += fileSize
		return nil
	})
	switch {
	case err != nil:
		return nil, err
	case len(result.Files) == 0:
		return nil, errors.New("no files found in sync directory (directory may be empty or all files were skipped)")
	}

	return result, nil
}

// IsBinaryContent reports whether content looks binary rather than text.
func IsBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check first 512 bytes (or less if file is smaller)
	checkSize := min(len(content), 512)

	// Use net/http's content type detection
	contentType := nethttp.DetectContentType(content[:checkSize])

	// Text types are not binary
	if strings.HasPrefix(contentType, "text/") {
		return false
	}

	// Common text-based application types
	textAppTypes := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
		"application/x-sh",
	}
	for _, t := range textAppTypes {
		if strings.HasPrefix(contentType, t) {
			return false
		}
	}

	// For application/octet-stream, do additional null-byte check
	// Text files rarely have null bytes, so their presence indicates binary
	if contentType == "application/octet-stream" {
		return slices.Contains(content[:checkSize], 0)
	}

	// Everything else is considered binary
	return strings.HasPrefix(contentType, "application/") ||
		strings.HasPrefix(contentType, "image/") ||
		strings.HasPrefix(contentType, "video/") ||
		strings.HasPrefix(contentType, "audio/")
}
