package gitutil

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/buildx/util/osutil"
	"github.com/pkg/errors"
)

// Git represents an active git object
type Git struct {
	ctx     context.Context
	wd      string
	gitpath string
}

// Option provides a variadic option for configuring the git client.
type Option func(b *Git)

// WithContext sets context.
func WithContext(ctx context.Context) Option {
	return func(b *Git) {
		b.ctx = ctx
	}
}

// WithWorkingDir sets working directory.
func WithWorkingDir(wd string) Option {
	return func(b *Git) {
		b.wd = wd
	}
}

// New initializes a new git client
func New(opts ...Option) (*Git, error) {
	var err error
	c := &Git{
		ctx: context.Background(),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.gitpath, err = gitPath(c.wd)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Git) IsInsideWorkTree() bool {
	out, err := c.Run("rev-parse", "--is-inside-work-tree")
	return out == "true" && err == nil
}

func (c *Git) IsDirty() bool {
	out, err := c.Run("status", "--porcelain", "--ignored")
	return strings.TrimSpace(out) != "" || err != nil
}

func (c *Git) RootDir() (string, error) {
	root, err := c.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return osutil.SanitizePath(root), nil
}

func (c *Git) GitDir() (string, error) {
	dir, err := c.RootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".git"), nil
}

func (c *Git) RemoteURL() (string, error) {
	// Try default remote based on remote tracking branch
	if remote, err := c.currentRemote(); err == nil && remote != "" {
		if ru, err := c.clean(c.run("remote", "get-url", remote)); err == nil && ru != "" {
			return stripCredentials(ru), nil
		}
	}
	// Next try to get the remote URL from the origin remote first
	if ru, err := c.clean(c.run("remote", "get-url", "origin")); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	// If that fails, try to get the remote URL from the upstream remote
	if ru, err := c.clean(c.run("remote", "get-url", "upstream")); err == nil && ru != "" {
		return stripCredentials(ru), nil
	}
	return "", errors.New("no remote URL found for either origin or upstream")
}

func (c *Git) FullCommit() (string, error) {
	return c.clean(c.run("show", "--format=%H", "HEAD", "--quiet", "--"))
}

func (c *Git) ShortCommit() (string, error) {
	return c.clean(c.run("show", "--format=%h", "HEAD", "--quiet", "--"))
}

func (c *Git) Tag() (string, error) {
	var tag string
	var err error
	for _, fn := range []func() (string, error){
		func() (string, error) {
			return c.clean(c.run("tag", "--points-at", "HEAD", "--sort", "-version:creatordate"))
		},
		func() (string, error) {
			return c.clean(c.run("describe", "--tags", "--abbrev=0"))
		},
	} {
		tag, err = fn()
		if tag != "" || err != nil {
			return tag, err
		}
	}
	return tag, err
}

func (c *Git) Run(args ...string) (string, error) {
	return c.clean(c.run(args...))
}

func (c *Git) run(args ...string) (string, error) {
	var extraArgs = []string{
		"-c", "log.showSignature=false",
	}

	args = append(extraArgs, args...)
	cmd := exec.CommandContext(c.ctx, c.gitpath, args...)
	if c.wd != "" {
		cmd.Dir = c.wd
	}

	// Override the locale to ensure consistent output
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.New(stderr.String())
	}
	return stdout.String(), nil
}

func (c *Git) clean(out string, err error) (string, error) {
	out = strings.ReplaceAll(strings.Split(out, "\n")[0], "'", "")
	if err != nil {
		err = errors.New(strings.TrimSuffix(err.Error(), "\n"))
	}
	return out, err
}

func (c *Git) currentRemote() (string, error) {
	symref, err := c.Run("symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", err
	}
	if symref == "" {
		return "", nil
	}
	// git for-each-ref --format='%(upstream:remotename)'
	remote, err := c.Run("for-each-ref", "--format=%(upstream:remotename)", symref)
	if err != nil {
		return "", err
	}
	return remote, nil
}

func IsUnknownRevision(err error) bool {
	if err == nil {
		return false
	}
	// https://github.com/git/git/blob/a6a323b31e2bcbac2518bddec71ea7ad558870eb/setup.c#L204
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unknown revision or path not in the working tree") || strings.Contains(errMsg, "bad revision")
}

// stripCredentials takes a URL and strips username and password from it.
// e.g. "https://user:password@host.tld/path.git" will be changed to
// "https://host.tld/path.git".
// TODO: remove this function once fix from BuildKit is vendored here
func stripCredentials(s string) string {
	ru, err := url.Parse(s)
	if err != nil {
		return s // string is not a URL, just return it
	}
	ru.User = nil
	return ru.String()
}
