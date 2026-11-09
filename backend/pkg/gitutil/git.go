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
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/gofrs/flock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/arcane/v3/pkg/logging"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/arcane/v3/pkg/workspace"
	"getarcane.dev/types/v3"
)

type (
	Client struct {
		workDir string
	}

	AuthConfig struct {
		AuthType               string
		Username               string
		Token                  string
		SSHKey                 string
		SSHHostKeyVerification string // strict, accept_new, skip
	}

	BranchInfo struct {
		Name      string
		IsDefault bool
	}

	SyncFileInfo struct {
		RelativePath string
		Content      []byte
		Size         int64
		IsBinary     bool
		Executable   bool
	}

	DirectoryWalkResult struct {
		Files           []SyncFileInfo
		TotalFiles      int
		TotalSize       int64
		SkippedBinaries int
	}
)

const (
	binarySniffBytes   = 512
	CloneScratchPrefix = "gitops-"
	errUnsupportedURL  = "repository URL must use http(s)://, ssh://, git://, or git@host:path"

	SSHHostKeyVerificationStrict    = "strict"
	SSHHostKeyVerificationAcceptNew = "accept_new"
	SSHHostKeyVerificationSkip      = "skip" // Skip host key verification (insecure)
	defaultKnownHostsDataDir        = "/app/data"
	defaultKnownHostsPath           = "/app/data/.ssh/known_hosts"
)

var scpLikeURLPattern = regexp.MustCompile(`^[^@/]+@[^:/]+:`)

// go-git's file transport execs the git binary, which the distroless image lacks, so unregister it.
func init() {
	client.InstallProtocol("file", nil)

	// Azure DevOps sends an incomplete pack without multi_ack, so only thin-pack stays disabled (as in Flux/ArgoCD).
	transport.UnsupportedCapabilities = []capability.Capability{
		capability.ThinPack,
	}
}

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

func NewClient(workDir string) *Client {
	return &Client{
		workDir: workDir,
	}
}

func (c *Client) getAuth(ctx context.Context, url string, localConfig AuthConfig) (transport.AuthMethod, error) {
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

			hostKeyCallback, err := c.getSSHHostKeyCallback(ctx, localConfig.SSHHostKeyVerification)
			if err != nil {
				return nil, fmt.Errorf("failed to configure SSH host key verification: %w", err)
			}
			publicKeys.HostKeyCallbackHelper = ssh.HostKeyCallbackHelper{
				HostKeyCallback: hostKeyCallback,
			}
			// go-git skips algorithm negotiation for custom callbacks, so a server offering a
			// different key type than known_hosts records would fail as a mismatch.
			if localConfig.SSHHostKeyVerification != SSHHostKeyVerificationSkip {
				knownHostsPaths, pathErr := getKnownHostsPaths(c.workDir, os.Getenv, os.Stat, os.UserHomeDir)
				if pathErr != nil {
					return nil, fmt.Errorf("failed to configure SSH host key verification: %w", pathErr)
				}
				knownHosts, dbErr := ssh.NewKnownHostsDb(knownHostsPaths...)
				if dbErr != nil {
					return nil, fmt.Errorf("failed to read known_hosts: %w", dbErr)
				}
				hostWithPort := net.JoinHostPort(endpoint.Host, strconv.Itoa(cmp.Or(endpoint.Port, ssh.DefaultPort)))
				publicKeys.HostKeyAlgorithms = knownHosts.HostKeyAlgorithms(hostWithPort)
			}

			return publicKeys, nil
		}
		return nil, errors.New("ssh key required for ssh authentication")
	default:
		return nil, nil
	}
}

func (c *Client) getSSHHostKeyCallback(ctx context.Context, mode string) (gossh.HostKeyCallback, error) {
	if mode == SSHHostKeyVerificationSkip {
		return gossh.InsecureIgnoreHostKey(), nil //nolint:gosec // User explicitly chose to skip verification
	}

	knownHostsPaths, pathErr := getKnownHostsPaths(
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

	knownHostsPath, err := filepath.Abs(knownHostsPaths[0])
	if err != nil {
		return nil, fmt.Errorf("failed to resolve known_hosts path: %w", err)
	}
	if mkdirErr := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(knownHostsPath), 0o700); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create known_hosts directory: %w", mkdirErr)
	}

	if _, statErr := (acfs.Host{}).Stat(ctx, knownHostsPath, acfstypes.StatOptions{FollowFinal: true}); errors.Is(statErr, fs.ErrNotExist) {
		file, openFileErr := acfs.Host{}.OpenFile(ctx, knownHostsPath, acfs.OpenOptions{
			Mode:   0o600,
			Write:  true,
			Create: true,
		})
		if openFileErr != nil {
			return nil, fmt.Errorf("failed to create known_hosts file: %w", openFileErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			slog.WarnContext(ctx, "Failed to close known_hosts file", "path", knownHostsPath, "error", closeErr)
		}
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		// Re-read known_hosts each call for concurrent edits; unreadable trust fails closed.
		existingCallback, err := knownhosts.New(knownHostsPaths...)
		if err != nil {
			return fmt.Errorf("failed to read known_hosts: %w", err)
		}

		existingCallbackErr := existingCallback(hostname, remote, key)
		if existingCallbackErr == nil {
			return nil
		}
		if keyErr, ok := errors.AsType[*knownhosts.KeyError](existingCallbackErr); ok && len(keyErr.Want) > 0 {
			return fmt.Errorf("host key mismatch for %s (possible MITM attack): %w", hostname, existingCallbackErr)
		}

		// Unknown host: save it, logging persistence failures after the file closes and the lock releases.
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

		file, openErr := acfs.Host{}.OpenFile(ctx, knownHostsPath, acfs.OpenOptions{
			Mode:   0o600,
			Write:  true,
			Create: true,
			Append: true,
		})
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

func getKnownHostsPaths(
	workDir string,
	getenv func(string) string,
	stat func(string) (os.FileInfo, error),
	userHomeDir func() (string, error),
) ([]string, error) {
	if localPath := getenv("SSH_KNOWN_HOSTS"); localPath != "" {
		return []string{localPath}, nil
	}

	// Prefer Arcane's writable data directory, present in published images and PUID/PGID setups.
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

func RepositoryAttribute(rawURL string) attribute.KeyValue {
	endpoint, err := transport.NewEndpoint(rawURL)
	if err != nil {
		return attribute.String("arcane.git.repository", "")
	}
	endpoint.User, endpoint.Password = "", ""
	if i := strings.IndexAny(endpoint.Path, "?#"); i >= 0 {
		endpoint.Path = endpoint.Path[:i]
	}
	return attribute.String("arcane.git.repository", endpoint.String())
}

func (c *Client) Clone(ctx context.Context, url, branch string, auth AuthConfig, depth int) (repoPath string, err error) {
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "gitrepo.clone", trace.WithAttributes(
		attribute.String("arcane.git.branch", branch),
		attribute.Int("arcane.git.depth", depth),
	))
	defer func() { logging.EndSpan(span, err) }()

	ctx, cancel := timeouts.WithDefault(ctx, timeouts.DefaultGitOperation)
	defer cancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}

	url, err = normalizeURL(url)
	if err != nil {
		return "", err
	}
	span.SetAttributes(RepositoryAttribute(url))
	authMethod, err := c.getAuth(ctx, url, auth)
	if err != nil {
		return "", err
	}

	workDir, err := filepath.Abs(cmp.Or(c.workDir, os.TempDir()))
	if err != nil {
		return "", fmt.Errorf("failed to resolve work dir: %w", err)
	}
	if err = (acfs.Host{}).MkdirAll(ctx, workDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create work dir: %w", err)
	}
	tmpDir, err := acfs.Host{}.MkdirTemp(ctx, workDir, CloneScratchPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	cloneOptions := &git.CloneOptions{URL: url, Auth: authMethod, Depth: depth}
	if branch != "" {
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOptions.SingleBranch = true
	}

	repo, err := git.PlainCloneContext(ctx, tmpDir, false, cloneOptions)
	if err != nil && depth > 0 && strings.Contains(err.Error(), "shallow") {
		// Servers without shallow support, go-git's own included, get a full clone.
		cloneOptions.Depth = 0
		if err = (acfs.Host{}).RemoveAll(ctx, tmpDir); err == nil {
			repo, err = git.PlainCloneContext(ctx, tmpDir, false, cloneOptions)
		}
	}
	if err != nil {
		_ = acfs.Host{}.RemoveAll(context.WithoutCancel(ctx), tmpDir)
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}
	if head, headErr := repo.Head(); headErr == nil {
		span.SetAttributes(attribute.String("arcane.git.commit", head.Hash().String()))
	}

	return tmpDir, nil
}

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

func (c *Client) ListBranches(ctx context.Context, url string, auth AuthConfig) ([]BranchInfo, error) {
	refs, err := c.listRemoteReferences(ctx, url, auth)
	if err != nil {
		return nil, err
	}

	// HEAD is a symbolic reference to the default branch.
	var defaultBranch string
	if i := slices.IndexFunc(refs, func(ref *plumbing.Reference) bool { return ref.Name().String() == "HEAD" }); i >= 0 && refs[i].Target().IsBranch() {
		defaultBranch = refs[i].Target().Short()
	}

	var branches []BranchInfo
	seen := make(map[string]bool)
	for _, ref := range refs {
		branchName := ref.Name().Short()
		if !ref.Name().IsBranch() || seen[branchName] {
			continue
		}
		seen[branchName] = true
		branches = append(branches, BranchInfo{Name: branchName, IsDefault: branchName == defaultBranch})
	}

	slices.SortFunc(branches, func(a, b BranchInfo) int {
		if a.IsDefault != b.IsDefault {
			return kit.Ternary(a.IsDefault, -1, 1)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return branches, nil
}

func (c *Client) ProbeRemote(ctx context.Context, url string, auth AuthConfig) error {
	_, err := c.listRemoteReferences(ctx, url, auth)
	return err
}

func (c *Client) listRemoteReferences(ctx context.Context, url string, auth AuthConfig) (refs []*plumbing.Reference, err error) {
	ctx, span := otel.Tracer(logging.InstrumentationName).Start(ctx, "gitrepo.list_remote")
	defer func() { logging.EndSpan(span, err) }()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	url, err = normalizeURL(url)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(RepositoryAttribute(url))
	authMethod, err := c.getAuth(ctx, url, auth)
	if err != nil {
		return nil, err
	}

	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	rem := git.NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err = rem.ListContext(listCtx, &git.ListOptions{Auth: authMethod})
	if err != nil {
		return nil, fmt.Errorf("failed to list remote references: %w", err)
	}
	span.SetAttributes(attribute.Int("arcane.result.count", len(refs)))
	return refs, nil
}

func ValidatePath(repoPath, requestedPath string) error {
	cleanRepoPath := filepath.Clean(repoPath)
	cleanRequestedPath := filepath.Clean(filepath.Join(repoPath, requestedPath))

	rel, err := filepath.Rel(cleanRepoPath, cleanRequestedPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return errors.New("path traversal attempt detected")
	}

	return nil
}

func (c *Client) BrowseTree(ctx context.Context, repoPath, targetPath string) ([]types.FileTreeNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	logicalPath := path.Join("/", filepath.ToSlash(targetPath))
	entry, err := acfs.Root{Path: repoPath}.Stat(ctx, logicalPath, acfstypes.StatOptions{FollowFinal: true})
	if err != nil {
		return nil, fmt.Errorf("path not found: %w", err)
	}
	if !entry.IsDirectory {
		return nil, errors.New("path is not a directory")
	}

	entries, err := acfs.Root{Path: repoPath}.List(ctx, logicalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var nodes []types.FileTreeNode
	for _, child := range entries {
		if errErr := ctx.Err(); errErr != nil {
			return nil, errErr
		}
		if child.Name == ".git" {
			continue
		}

		nodeType := kit.Ternary(child.IsDirectory, types.FileTreeNodeTypeDirectory, types.FileTreeNodeTypeFile)

		nodes = append(nodes, types.FileTreeNode{
			Name: child.Name,
			Path: filepath.Join(targetPath, child.Name),
			Type: nodeType,
			Size: child.Size,
		})
	}

	return nodes, nil
}

func (c *Client) Discard(ctx context.Context, repoPath string) {
	if err := (acfs.Host{}).RemoveAll(context.WithoutCancel(ctx), repoPath); err != nil {
		slog.WarnContext(ctx, "Failed to cleanup repository", "path", repoPath, "error", err)
	}
}

func (c *Client) PurgeScratchDirs(ctx context.Context, maxAge time.Duration) (int, error) {
	root, err := filepath.Abs(cmp.Or(c.workDir, os.TempDir()))
	if err != nil {
		return 0, fmt.Errorf("failed to resolve git work dir: %w", err)
	}

	entries, err := acfs.Host{}.List(ctx, root)
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
		if !entry.IsDirectory || !strings.HasPrefix(entry.Name, CloneScratchPrefix) {
			continue
		}
		if maxAge > 0 && time.Since(entry.ModTime) < maxAge {
			continue
		}
		if rmErr := (acfs.Host{}).RemoveAll(ctx, entry.Path); rmErr != nil {
			slog.WarnContext(ctx, "Failed to remove git clone scratch dir", "path", entry.Path, "error", rmErr)
			continue
		}
		removed++
	}
	return removed, nil
}

func (c *Client) TestConnection(ctx context.Context, url, branch string, auth AuthConfig) error {
	tmpDir, err := c.Clone(ctx, url, branch, auth, 1)
	if err != nil {
		return err
	}
	c.Discard(ctx, tmpDir)
	return nil
}

func (c *Client) FileExists(ctx context.Context, repoPath, filePath string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	exists, err := acfs.Root{Path: repoPath}.Exists(ctx, path.Join("/", filepath.ToSlash(filePath)))
	return err == nil && exists
}

func (c *Client) ReadFile(ctx context.Context, repoPath, filePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := acfs.Root{Path: repoPath}.ReadFile(ctx, path.Join("/", filepath.ToSlash(filePath)))
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

func (c *Client) WalkDirectory(ctx context.Context, repoPath, composePath string,
	maxFiles int, maxTotalSize, maxBinarySize int64,
) (*DirectoryWalkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := ValidatePath(repoPath, composePath); err != nil {
		return nil, fmt.Errorf("invalid compose path: %w", err)
	}

	syncDir := filepath.Dir(filepath.Join(repoPath, composePath))

	result := &DirectoryWalkResult{
		Files: make([]SyncFileInfo, 0),
	}
	_, err := acfs.Root{Path: syncDir}.Walk(
		ctx,
		"/",
		acfstypes.WalkOptions{},
		func(entry acfstypes.Entry) error {
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
				reader, _, openErr := acfs.Root{Path: syncDir}.OpenRead(ctx, entry.Path, binarySniffBytes)
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
				if slices.Contains(buf[:n], 0) {
					result.SkippedBinaries++
					return nil
				}
			}

			content, readErr := acfs.Root{Path: syncDir}.ReadFile(ctx, entry.Path)
			if readErr != nil {
				return fmt.Errorf("failed to read file %s: %w", relativePath, readErr)
			}

			fileSize := int64(len(content))
			isBinary := !workspace.IsTextContent(content)
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
		},
	)
	switch {
	case err != nil:
		return nil, err
	case len(result.Files) == 0:
		return nil, errors.New("no files found in sync directory (directory may be empty or all files were skipped)")
	}

	return result, nil
}
