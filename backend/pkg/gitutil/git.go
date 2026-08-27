package git

import (
	"context"
	stderrors "errors"
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

	"emperror.dev/errors"
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
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// binarySniffBytes is how much of a file is inspected to classify it as binary.
const binarySniffBytes = 512

// cloneScratchPrefix names the per-run clone scratch dirs created by Clone.
const cloneScratchPrefix = "gitops-"

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

const errUnsupportedURL = "repository URL must use http(s)://, ssh://, git://, or git@host:path"

// normalizeURL coerces a repository URL into a form go-git resolves to a
// network transport, never the file transport (which execs the git binary).
// Schemeless host-style URLs (e.g. github.com/org/repo.git) get https://.
func normalizeURL(raw string) (string, error) {
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

// SSH host key verification modes
const (
	SSHHostKeyVerificationStrict    = "strict"     // Require host key in known_hosts
	SSHHostKeyVerificationAcceptNew = "accept_new" // Auto-add unknown host keys
	SSHHostKeyVerificationSkip      = "skip"       // Skip host key verification (insecure)
	defaultKnownHostsDataDir        = "/app/data"
	defaultKnownHostsPath           = "/app/data/.ssh/known_hosts"
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
			return &githttp.BasicAuth{
				Username: config.Username,
				Password: config.Token,
			}, nil
		}
		return nil, nil
	case "ssh":
		if config.SSHKey != "" {
			publicKeys, err := ssh.NewPublicKeys("git", []byte(config.SSHKey), "")
			if err != nil {
				return nil, errors.WrapIf(err, "failed to create ssh auth")
			}

			// Configure host key verification based on mode
			hostKeyCallback, err := c.getSSHHostKeyCallback(config.SSHHostKeyVerification)
			if err != nil {
				return nil, errors.WrapIf(err, "failed to configure SSH host key verification")
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
	// os.* rather than acfs: known_hosts lives under the user home (not an arcane
	// confinement root), and acfs has no append/flock API for the writes below.
	dir := filepath.Dir(knownHostsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.WrapIf(err, "failed to create known_hosts directory")
	}

	// Create the file if it doesn't exist
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		file, err := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to create known_hosts file")
		}
		if err := file.Close(); err != nil {
			slog.Warn("Failed to close known_hosts file", "path", knownHostsPath, "error", err)
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
			if keyErr, ok := stderrors.AsType[*knownhosts.KeyError](err); ok && len(keyErr.Want) > 0 {
				// Host is known but key doesn't match - this is a security concern
				return errors.WrapIff(err, "host key mismatch for %s (possible MITM attack)", hostname)
			}
			// Otherwise, host is unknown - we'll add it
		}

		// Add the new host key to known_hosts
		if err := addHostKey(knownHostsPath, hostname, key); err != nil {
			// Log the error but don't fail - still allow the connection
			// The host key just won't be remembered for next time
			slog.Warn("Failed to save host key", "hostname", hostname, "error", err)
		}

		return nil
	}, nil
}

// getKnownHostsPath returns the path to the known_hosts file
func getKnownHostsPath() string {
	return getKnownHostsPathInternal(os.Getenv, os.Stat, os.UserHomeDir)
}

func getKnownHostsPathInternal(getenv func(string) string, stat func(string) (os.FileInfo, error), userHomeDir func() (string, error)) string {
	// Check environment variable first
	if path := getenv("SSH_KNOWN_HOSTS"); path != "" {
		return path
	}

	// Prefer Arcane's writable persistent data directory when it is available,
	// which is the case for published container images and PUID/PGID setups.
	if info, err := stat(defaultKnownHostsDataDir); err == nil && info.IsDir() {
		return defaultKnownHostsPath
	}

	// Fall back to the user's home directory for local development and CI.
	homeDir, err := userHomeDir()
	if err == nil && homeDir != "" {
		return filepath.Join(homeDir, ".ssh", "known_hosts")
	}

	// Last resort for environments without a resolvable home directory.
	return filepath.Join(os.TempDir(), ".ssh", "known_hosts")
}

// addHostKey adds a host key to the known_hosts file
func addHostKey(knownHostsPath, hostname string, key gossh.PublicKey) (err error) {
	// Format the known_hosts line
	line := knownhosts.Line([]string{hostname}, key)

	// Acquire exclusive lock to prevent concurrent writes
	fileLock := flock.New(knownHostsPath)
	if err := fileLock.Lock(); err != nil {
		return errors.WrapIf(err, "failed to acquire lock on known_hosts file")
	}
	defer func() {
		if unlockErr := fileLock.Unlock(); unlockErr != nil && err == nil {
			err = errors.WrapIf(unlockErr, "failed to release lock on known_hosts file")
		}
	}()

	// Append to the file
	// os.* rather than acfs: acfs has no append API, and known_hosts lives under
	// the user home rather than an arcane confinement root.
	file, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.WrapIf(err, "failed to open known_hosts file")
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = errors.WrapIf(cerr, "failed to close known_hosts file")
		}
	}()

	if _, err := file.WriteString(line + "\n"); err != nil {
		return errors.WrapIf(err, "failed to write to known_hosts file")
	}

	return nil
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
		return "", errors.WrapIf(err, "failed to create work dir")
	}
	tmpDir, err := os.MkdirTemp(workDir, cloneScratchPrefix+"*")
	if err != nil {
		return "", errors.WrapIf(err, "failed to create temp dir")
	}

	authMethod, err := c.getAuth(auth)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	url, err = normalizeURL(url)
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
		return "", errors.WrapIf(err, "failed to clone repository")
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
		return "", errors.WrapIf(err, "failed to open repository")
	}

	ref, err := repo.Head()
	if err != nil {
		return "", errors.WrapIf(err, "failed to get HEAD")
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
	refs, err := c.listRemoteReferences(ctx, url, auth)
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

	_, err := c.listRemoteReferences(ctx, url, auth)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) listRemoteReferences(ctx context.Context, url string, auth AuthConfig) ([]*plumbing.Reference, error) {
	authMethod, err := c.getAuth(auth)
	if err != nil {
		return nil, err
	}

	url, err = normalizeURL(url)
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
		return nil, errors.WrapIf(err, "failed to list remote references")
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
		return errors.WrapIf(err, "invalid path")
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
		return nil, errors.WrapIf(err, "path not found")
	}
	if !entry.IsDirectory {
		return nil, errors.New("path is not a directory")
	}

	entries, err := acfs.List(ctx, repoPath, logicalPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to read directory")
	}

	var nodes []gitops.FileTreeNode
	for _, child := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Skip .git directory
		if child.Name == ".git" {
			continue
		}

		nodeType := gitops.FileTreeNodeTypeFile
		if child.IsDirectory {
			nodeType = gitops.FileTreeNodeTypeDirectory
		}

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
		return 0, errors.WrapIff(err, "failed to read git work dir %s", root)
	}

	removed := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
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
		return "", errors.WrapIf(err, "failed to read file")
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

type syncWalkLimits struct {
	maxFiles      int
	maxTotalSize  int64
	maxBinarySize int64
}

// WalkDirectory walks the directory containing the compose file and returns all files.
// It enforces limits on file count, total size, and skips large binary files.
// The composePath is the path to the compose file within the repo - the directory
// containing this file will be walked.
func (c *Client) WalkDirectory(ctx context.Context, repoPath, composePath string,
	maxFiles int, maxTotalSize, maxBinarySize int64) (*DirectoryWalkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate compose path
	if err := ValidatePath(repoPath, composePath); err != nil {
		return nil, errors.WrapIf(err, "invalid compose path")
	}

	// Get the directory containing the compose file
	syncDir := filepath.Dir(filepath.Join(repoPath, composePath))

	result := &DirectoryWalkResult{
		Files: make([]SyncFileInfo, 0),
	}
	limits := syncWalkLimits{
		maxFiles:      maxFiles,
		maxTotalSize:  maxTotalSize,
		maxBinarySize: maxBinarySize,
	}

	err := acfs.Walk(ctx, syncDir, "/", func(entry acfstypes.Entry) error {
		return c.walkSyncEntry(ctx, syncDir, entry, result, limits)
	})

	if err != nil {
		return nil, err
	}

	// Validate we found at least one file
	if len(result.Files) == 0 {
		return nil, errors.New("no files found in sync directory (directory may be empty or all files were skipped)")
	}

	return result, nil
}

func (c *Client) walkSyncEntry(ctx context.Context, syncDir string, entry acfstypes.Entry, result *DirectoryWalkResult, limits syncWalkLimits) error {
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

	return c.appendSyncFile(ctx, syncDir, entry, result, limits)
}

func (c *Client) appendSyncFile(ctx context.Context, syncDir string, entry acfstypes.Entry, result *DirectoryWalkResult, limits syncWalkLimits) error {
	relativePath := strings.TrimPrefix(entry.Path, "/")
	if limits.maxFiles > 0 && result.TotalFiles >= limits.maxFiles {
		return errors.Errorf("file count limit exceeded (max %d files)", limits.maxFiles)
	}

	if limits.maxBinarySize > 0 && entry.Size > limits.maxBinarySize {
		isBinary, err := c.isBinarySyncFile(ctx, syncDir, entry.Path)
		if err != nil {
			return errors.WrapIff(err, "failed to inspect file %s", relativePath)
		}
		if isBinary {
			result.SkippedBinaries++
			return nil
		}
	}

	content, err := acfs.ReadFile(ctx, syncDir, entry.Path)
	if err != nil {
		return errors.WrapIff(err, "failed to read file %s", relativePath)
	}

	fileSize := int64(len(content))
	isBinary := isBinaryContent(content)

	if isBinary && limits.maxBinarySize > 0 && fileSize > limits.maxBinarySize {
		result.SkippedBinaries++
		return nil
	}

	if limits.maxTotalSize > 0 && result.TotalSize+fileSize > limits.maxTotalSize {
		return errors.Errorf("total size limit exceeded (max %d bytes)", limits.maxTotalSize)
	}

	executable := os.FileMode(entry.UnixMode)&0o111 != 0
	result.Files = append(result.Files, SyncFileInfo{
		RelativePath: relativePath,
		Content:      content,
		Size:         fileSize,
		IsBinary:     isBinary,
		Executable:   executable,
	})
	result.TotalFiles++
	result.TotalSize += fileSize

	return nil
}

func (c *Client) isBinarySyncFile(ctx context.Context, syncDir, logicalPath string) (bool, error) {
	reader, _, err := acfs.OpenRead(ctx, syncDir, logicalPath, binarySniffBytes)
	if err != nil {
		return false, err
	}
	defer func() { _ = reader.Close() }()

	buf := make([]byte, binarySniffBytes)
	n, err := reader.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	return isBinaryContent(buf[:n]), nil
}

// isBinaryContent detects if content is binary using HTTP content type detection.
// Returns true for binary content, false for text content.
func isBinaryContent(content []byte) bool {
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
