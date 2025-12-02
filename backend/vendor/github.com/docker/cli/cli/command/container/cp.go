package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
	"github.com/moby/go-archive"
	"github.com/morikuni/aec"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type copyOptions struct {
	source      string
	destination string
	followLink  bool
	copyUIDGID  bool
	quiet       bool
}

type copyDirection int

const (
	fromContainer copyDirection = 1 << iota
	toContainer
	acrossContainers = fromContainer | toContainer
)

type cpConfig struct {
	followLink bool
	copyUIDGID bool
	quiet      bool
	sourcePath string
	destPath   string
	container  string
}

// copyProgressPrinter wraps io.ReadCloser to print progress information when
// copying files to/from a container.
type copyProgressPrinter struct {
	io.ReadCloser
	total *int64
}

const (
	copyToContainerHeader       = "Copying to container - "
	copyFromContainerHeader     = "Copying from container - "
	copyProgressUpdateThreshold = 75 * time.Millisecond
)

func (pt *copyProgressPrinter) Read(p []byte) (int, error) {
	n, err := pt.ReadCloser.Read(p)
	atomic.AddInt64(pt.total, int64(n))
	return n, err
}

func copyProgress(ctx context.Context, dst io.Writer, header string, total *int64) (func(), <-chan struct{}) {
	done := make(chan struct{})
	if !streams.NewOut(dst).IsTerminal() {
		close(done)
		return func() {}, done
	}

	fmt.Fprint(dst, aec.Save)
	fmt.Fprint(dst, "Preparing to copy...")

	restore := func() {
		fmt.Fprint(dst, aec.Restore)
		fmt.Fprint(dst, aec.EraseLine(aec.EraseModes.All))
	}

	go func() {
		defer close(done)
		fmt.Fprint(dst, aec.Hide)
		defer fmt.Fprint(dst, aec.Show)

		fmt.Fprint(dst, aec.Restore)
		fmt.Fprint(dst, aec.EraseLine(aec.EraseModes.All))
		fmt.Fprint(dst, header)

		var last int64
		fmt.Fprint(dst, progressHumanSize(last))

		buf := bytes.NewBuffer(nil)
		ticker := time.NewTicker(copyProgressUpdateThreshold)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n := atomic.LoadInt64(total)
				if n == last {
					// Don't write to the terminal, if we don't need to.
					continue
				}

				// Write to the buffer first to avoid flickering and context switching
				fmt.Fprint(buf, aec.Column(uint(len(header)+1)))
				fmt.Fprint(buf, aec.EraseLine(aec.EraseModes.Tail))
				fmt.Fprint(buf, progressHumanSize(n))

				buf.WriteTo(dst)
				buf.Reset()
				last += n
			}
		}
	}()
	return restore, done
}

// NewCopyCommand creates a new `docker cp` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewCopyCommand(dockerCLI command.Cli) *cobra.Command {
	return newCopyCommand(dockerCLI)
}

func newCopyCommand(dockerCLI command.Cli) *cobra.Command {
	var opts copyOptions

	cmd := &cobra.Command{
		Use: `cp [OPTIONS] CONTAINER:SRC_PATH DEST_PATH|-
	docker cp [OPTIONS] SRC_PATH|- CONTAINER:DEST_PATH`,
		Short: "Copy files/folders between a container and the local filesystem",
		Long: `Copy files/folders between a container and the local filesystem

Use '-' as the source to read a tar archive from stdin
and extract it to a directory destination in a container.
Use '-' as the destination to stream a tar archive of a
container source to stdout.`,
		Args: cli.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "" {
				return errors.New("source can not be empty")
			}
			if args[1] == "" {
				return errors.New("destination can not be empty")
			}
			opts.source = args[0]
			opts.destination = args[1]
			if !cmd.Flag("quiet").Changed {
				// User did not specify "quiet" flag; suppress output if no terminal is attached
				opts.quiet = !dockerCLI.Out().IsTerminal()
			}
			return runCopy(cmd.Context(), dockerCLI, opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container cp, docker cp",
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&opts.followLink, "follow-link", "L", false, "Always follow symbol link in SRC_PATH")
	flags.BoolVarP(&opts.copyUIDGID, "archive", "a", false, "Archive mode (copy all uid/gid information)")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress progress output during copy. Progress output is automatically suppressed if no terminal is attached")
	return cmd
}

func progressHumanSize(n int64) string {
	return units.HumanSizeWithPrecision(float64(n), 3)
}

func runCopy(ctx context.Context, dockerCli command.Cli, opts copyOptions) error {
	srcContainer, srcPath := splitCpArg(opts.source)
	destContainer, destPath := splitCpArg(opts.destination)

	copyConfig := cpConfig{
		followLink: opts.followLink,
		copyUIDGID: opts.copyUIDGID,
		quiet:      opts.quiet,
		sourcePath: srcPath,
		destPath:   destPath,
	}

	var direction copyDirection
	if srcContainer != "" {
		direction |= fromContainer
		copyConfig.container = srcContainer
	}
	if destContainer != "" {
		direction |= toContainer
		copyConfig.container = destContainer
	}

	switch direction {
	case fromContainer:
		return copyFromContainer(ctx, dockerCli, copyConfig)
	case toContainer:
		return copyToContainer(ctx, dockerCli, copyConfig)
	case acrossContainers:
		return errors.New("copying between containers is not supported")
	default:
		return errors.New("must specify at least one container source")
	}
}

func resolveLocalPath(localPath string) (absPath string, _ error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return "", err
	}
	return archive.PreserveTrailingDotOrSeparator(absPath, localPath), nil
}

func copyFromContainer(ctx context.Context, dockerCLI command.Cli, copyConfig cpConfig) (err error) {
	dstPath := copyConfig.destPath
	srcPath := copyConfig.sourcePath

	if dstPath != "-" {
		// Get an absolute destination path.
		dstPath, err = resolveLocalPath(dstPath)
		if err != nil {
			return err
		}
	}

	if err := command.ValidateOutputPath(dstPath); err != nil {
		return err
	}

	apiClient := dockerCLI.Client()
	// if client requests to follow symbol link, then must decide target file to be copied
	var rebaseName string
	if copyConfig.followLink {
		srcStat, err := apiClient.ContainerStatPath(ctx, copyConfig.container, srcPath)

		// If the destination is a symbolic link, we should follow it.
		if err == nil && srcStat.Mode&os.ModeSymlink != 0 {
			linkTarget := srcStat.LinkTarget
			if !isAbs(linkTarget) {
				// Join with the parent directory.
				srcParent, _ := archive.SplitPathDirEntry(srcPath)
				linkTarget = filepath.Join(srcParent, linkTarget)
			}

			linkTarget, rebaseName = archive.GetRebaseName(srcPath, linkTarget)
			srcPath = linkTarget
		}
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	content, stat, err := apiClient.CopyFromContainer(ctx, copyConfig.container, srcPath)
	if err != nil {
		return err
	}
	defer content.Close()

	if dstPath == "-" {
		_, err = io.Copy(dockerCLI.Out(), content)
		return err
	}

	srcInfo := archive.CopyInfo{
		Path:       srcPath,
		Exists:     true,
		IsDir:      stat.Mode.IsDir(),
		RebaseName: rebaseName,
	}

	var copiedSize int64
	if !copyConfig.quiet {
		content = &copyProgressPrinter{
			ReadCloser: content,
			total:      &copiedSize,
		}
	}

	preArchive := content
	if len(srcInfo.RebaseName) != 0 {
		_, srcBase := archive.SplitPathDirEntry(srcInfo.Path)
		preArchive = archive.RebaseArchiveEntries(content, srcBase, srcInfo.RebaseName)
	}

	if copyConfig.quiet {
		return archive.CopyTo(preArchive, srcInfo, dstPath)
	}

	restore, done := copyProgress(ctx, dockerCLI.Err(), copyFromContainerHeader, &copiedSize)
	res := archive.CopyTo(preArchive, srcInfo, dstPath)
	cancel()
	<-done
	restore()
	_, _ = fmt.Fprintln(dockerCLI.Err(), "Successfully copied", progressHumanSize(copiedSize), "to", dstPath)

	return res
}

// In order to get the copy behavior right, we need to know information
// about both the source and destination. The API is a simple tar
// archive/extract API but we can use the stat info header about the
// destination to be more informed about exactly what the destination is.
func copyToContainer(ctx context.Context, dockerCLI command.Cli, copyConfig cpConfig) (err error) {
	srcPath := copyConfig.sourcePath
	dstPath := copyConfig.destPath

	if srcPath != "-" {
		// Get an absolute source path.
		srcPath, err = resolveLocalPath(srcPath)
		if err != nil {
			return err
		}
	}

	apiClient := dockerCLI.Client()
	// Prepare destination copy info by stat-ing the container path.
	dstInfo := archive.CopyInfo{Path: dstPath}
	dstStat, err := apiClient.ContainerStatPath(ctx, copyConfig.container, dstPath)

	// If the destination is a symbolic link, we should evaluate it.
	if err == nil && dstStat.Mode&os.ModeSymlink != 0 {
		linkTarget := dstStat.LinkTarget
		if !isAbs(linkTarget) {
			// Join with the parent directory.
			dstParent, _ := archive.SplitPathDirEntry(dstPath)
			linkTarget = filepath.Join(dstParent, linkTarget)
		}

		dstInfo.Path = linkTarget
		dstStat, err = apiClient.ContainerStatPath(ctx, copyConfig.container, linkTarget)
		// FIXME(thaJeztah): unhandled error (should this return?)
	}

	// Validate the destination path
	if err := command.ValidateOutputPathFileMode(dstStat.Mode); err != nil {
		return errors.Wrapf(err, `destination "%s:%s" must be a directory or a regular file`, copyConfig.container, dstPath)
	}

	// Ignore any error and assume that the parent directory of the destination
	// path exists, in which case the copy may still succeed. If there is any
	// type of conflict (e.g., non-directory overwriting an existing directory
	// or vice versa) the extraction will fail. If the destination simply did
	// not exist, but the parent directory does, the extraction will still
	// succeed.
	if err == nil {
		dstInfo.Exists, dstInfo.IsDir = true, dstStat.Mode.IsDir()
	}

	var (
		content         io.ReadCloser
		resolvedDstPath string
		copiedSize      int64
	)

	if srcPath == "-" {
		content = os.Stdin
		resolvedDstPath = dstInfo.Path
		if !dstInfo.IsDir {
			return errors.Errorf("destination \"%s:%s\" must be a directory", copyConfig.container, dstPath)
		}
	} else {
		// Prepare source copy info.
		srcInfo, err := archive.CopyInfoSourcePath(srcPath, copyConfig.followLink)
		if err != nil {
			return err
		}

		srcArchive, err := archive.TarResource(srcInfo)
		if err != nil {
			return err
		}
		defer srcArchive.Close()

		// With the stat info about the local source as well as the
		// destination, we have enough information to know whether we need to
		// alter the archive that we upload so that when the server extracts
		// it to the specified directory in the container we get the desired
		// copy behavior.

		// See comments in the implementation of `archive.PrepareArchiveCopy`
		// for exactly what goes into deciding how and whether the source
		// archive needs to be altered for the correct copy behavior when it is
		// extracted. This function also infers from the source and destination
		// info which directory to extract to, which may be the parent of the
		// destination that the user specified.
		dstDir, preparedArchive, err := archive.PrepareArchiveCopy(srcArchive, srcInfo, dstInfo)
		if err != nil {
			return err
		}
		defer preparedArchive.Close()

		resolvedDstPath = dstDir
		content = preparedArchive
		if !copyConfig.quiet {
			content = &copyProgressPrinter{
				ReadCloser: content,
				total:      &copiedSize,
			}
		}
	}

	options := container.CopyToContainerOptions{
		CopyUIDGID: copyConfig.copyUIDGID,
	}

	if copyConfig.quiet {
		return apiClient.CopyToContainer(ctx, copyConfig.container, resolvedDstPath, content, options)
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	restore, done := copyProgress(ctx, dockerCLI.Err(), copyToContainerHeader, &copiedSize)
	res := apiClient.CopyToContainer(ctx, copyConfig.container, resolvedDstPath, content, options)
	cancel()
	<-done
	restore()
	fmt.Fprintln(dockerCLI.Err(), "Successfully copied", progressHumanSize(copiedSize), "to", copyConfig.container+":"+dstInfo.Path)

	return res
}

// We use `:` as a delimiter between CONTAINER and PATH, but `:` could also be
// in a valid LOCALPATH, like `file:name.txt`. We can resolve this ambiguity by
// requiring a LOCALPATH with a `:` to be made explicit with a relative or
// absolute path:
//
//	`/path/to/file:name.txt` or `./file:name.txt`
//
// This is apparently how `scp` handles this as well:
//
//	http://www.cyberciti.biz/faq/rsync-scp-file-name-with-colon-punctuation-in-it/
//
// We can't simply check for a filepath separator because container names may
// have a separator, e.g., "host0/cname1" if container is in a Docker cluster,
// so we have to check for a `/` or `.` prefix. Also, in the case of a Windows
// client, a `:` could be part of an absolute Windows path, in which case it
// is immediately proceeded by a backslash.
func splitCpArg(arg string) (ctr, path string) {
	if isAbs(arg) {
		// Explicit local absolute path, e.g., `C:\foo` or `/foo`.
		return "", arg
	}

	ctr, path, ok := strings.Cut(arg, ":")
	if !ok || strings.HasPrefix(ctr, ".") {
		// Either there's no `:` in the arg
		// OR it's an explicit local relative path like `./file:name.txt`.
		return "", arg
	}

	return ctr, path
}

// IsAbs is a platform-agnostic wrapper for filepath.IsAbs.
//
// On Windows, golang filepath.IsAbs does not consider a path \windows\system32
// as absolute as it doesn't start with a drive-letter/colon combination. However,
// in docker we need to verify things such as WORKDIR /windows/system32 in
// a Dockerfile (which gets translated to \windows\system32 when being processed
// by the daemon). This SHOULD be treated as absolute from a docker processing
// perspective.
func isAbs(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, string(os.PathSeparator))
}
