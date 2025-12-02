package image

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	"github.com/docker/cli-docs-tool/annotation"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/image/build"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/cli/cli/trust"
	"github.com/docker/cli/internal/jsonstream"
	"github.com/docker/cli/internal/lazyregexp"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api"
	buildtypes "github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	registrytypes "github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/pkg/progress"
	"github.com/docker/docker/pkg/streamformatter"
	"github.com/moby/go-archive"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type buildOptions struct {
	context        string
	dockerfileName string
	tags           opts.ListOpts
	labels         opts.ListOpts
	buildArgs      opts.ListOpts
	extraHosts     opts.ListOpts
	ulimits        *opts.UlimitOpt
	memory         opts.MemBytes
	memorySwap     opts.MemSwapBytes
	shmSize        opts.MemBytes
	cpuShares      int64
	cpuPeriod      int64
	cpuQuota       int64
	cpuSetCpus     string
	cpuSetMems     string
	cgroupParent   string
	isolation      string
	quiet          bool
	noCache        bool
	rm             bool
	forceRm        bool
	pull           bool
	cacheFrom      []string
	compress       bool
	securityOpt    []string
	networkMode    string
	squash         bool
	target         string
	imageIDFile    string
	platform       string
	untrusted      bool
}

// dockerfileFromStdin returns true when the user specified that the Dockerfile
// should be read from stdin instead of a file
func (o buildOptions) dockerfileFromStdin() bool {
	return o.dockerfileName == "-"
}

func newBuildOptions() buildOptions {
	ulimits := make(map[string]*container.Ulimit)
	return buildOptions{
		tags:       opts.NewListOpts(validateTag),
		buildArgs:  opts.NewListOpts(opts.ValidateEnv),
		ulimits:    opts.NewUlimitOpt(&ulimits),
		labels:     opts.NewListOpts(opts.ValidateLabel),
		extraHosts: opts.NewListOpts(opts.ValidateExtraHost),
	}
}

// NewBuildCommand creates a new `docker build` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewBuildCommand(dockerCLI command.Cli) *cobra.Command {
	return newBuildCommand(dockerCLI)
}

// newBuildCommand creates a new `docker build` command
func newBuildCommand(dockerCli command.Cli) *cobra.Command {
	options := newBuildOptions()

	cmd := &cobra.Command{
		Use:   "build [OPTIONS] PATH | URL | -",
		Short: "Build an image from a Dockerfile",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.context = args[0]
			return runBuild(cmd.Context(), dockerCli, options)
		},
		Annotations: map[string]string{
			"category-top": "4",
			"aliases":      "docker image build, docker build, docker builder build",
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		},
	}

	flags := cmd.Flags()

	flags.VarP(&options.tags, "tag", "t", `Name and optionally a tag in the "name:tag" format`)
	flags.SetAnnotation("tag", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#tag"})
	flags.Var(&options.buildArgs, "build-arg", "Set build-time variables")
	flags.SetAnnotation("build-arg", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#build-arg"})
	flags.Var(options.ulimits, "ulimit", "Ulimit options")
	flags.StringVarP(&options.dockerfileName, "file", "f", "", `Name of the Dockerfile (Default is "PATH/Dockerfile")`)
	flags.SetAnnotation("file", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#file"})
	flags.VarP(&options.memory, "memory", "m", "Memory limit")
	flags.Var(&options.memorySwap, "memory-swap", `Swap limit equal to memory plus swap: -1 to enable unlimited swap`)
	flags.Var(&options.shmSize, "shm-size", `Size of "/dev/shm"`)
	flags.Int64VarP(&options.cpuShares, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.Int64Var(&options.cpuPeriod, "cpu-period", 0, "Limit the CPU CFS (Completely Fair Scheduler) period")
	flags.Int64Var(&options.cpuQuota, "cpu-quota", 0, "Limit the CPU CFS (Completely Fair Scheduler) quota")
	flags.StringVar(&options.cpuSetCpus, "cpuset-cpus", "", "CPUs in which to allow execution (0-3, 0,1)")
	flags.StringVar(&options.cpuSetMems, "cpuset-mems", "", "MEMs in which to allow execution (0-3, 0,1)")
	flags.StringVar(&options.cgroupParent, "cgroup-parent", "", `Set the parent cgroup for the "RUN" instructions during build`)
	flags.SetAnnotation("cgroup-parent", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#cgroup-parent"})
	flags.StringVar(&options.isolation, "isolation", "", "Container isolation technology")
	flags.Var(&options.labels, "label", "Set metadata for an image")
	flags.BoolVar(&options.noCache, "no-cache", false, "Do not use cache when building the image")
	flags.BoolVar(&options.rm, "rm", true, "Remove intermediate containers after a successful build")
	flags.BoolVar(&options.forceRm, "force-rm", false, "Always remove intermediate containers")
	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")
	flags.BoolVar(&options.pull, "pull", false, "Always attempt to pull a newer version of the image")
	flags.StringSliceVar(&options.cacheFrom, "cache-from", []string{}, "Images to consider as cache sources")
	flags.BoolVar(&options.compress, "compress", false, "Compress the build context using gzip")
	flags.StringSliceVar(&options.securityOpt, "security-opt", []string{}, "Security options")
	flags.StringVar(&options.networkMode, "network", "default", "Set the networking mode for the RUN instructions during build")
	flags.SetAnnotation("network", "version", []string{"1.25"})
	flags.SetAnnotation("network", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#network"})
	flags.Var(&options.extraHosts, "add-host", `Add a custom host-to-IP mapping ("host:ip")`)
	flags.SetAnnotation("add-host", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#add-host"})
	flags.StringVar(&options.target, "target", "", "Set the target build stage to build.")
	flags.SetAnnotation("target", annotation.ExternalURL, []string{"https://docs.docker.com/reference/cli/docker/buildx/build/#target"})
	flags.StringVar(&options.imageIDFile, "iidfile", "", "Write the image ID to the file")

	flags.BoolVar(&options.untrusted, "disable-content-trust", !trust.Enabled(), "Skip image verification")

	flags.StringVar(&options.platform, "platform", os.Getenv("DOCKER_DEFAULT_PLATFORM"), "Set platform if server is multi-platform capable")
	flags.SetAnnotation("platform", "version", []string{"1.38"})

	flags.BoolVar(&options.squash, "squash", false, "Squash newly built layers into a single new layer")
	flags.SetAnnotation("squash", "experimental", nil)
	flags.SetAnnotation("squash", "version", []string{"1.25"})

	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)

	return cmd
}

// lastProgressOutput is the same as progress.Output except
// that it only output with the last update. It is used in
// non terminal scenarios to suppress verbose messages
type lastProgressOutput struct {
	output progress.Output
}

// WriteProgress formats progress information from a ProgressReader.
func (out *lastProgressOutput) WriteProgress(prog progress.Progress) error {
	if !prog.LastUpdate {
		return nil
	}

	return out.output.WriteProgress(prog)
}

//nolint:gocyclo
func runBuild(ctx context.Context, dockerCli command.Cli, options buildOptions) error {
	var (
		buildCtx      io.ReadCloser
		dockerfileCtx io.ReadCloser
		contextDir    string
		relDockerfile string
		progBuff      io.Writer
		buildBuff     io.Writer
		remote        string
	)

	contextType, err := build.DetectContextType(options.context)
	if err != nil {
		return err
	}

	if options.dockerfileFromStdin() {
		if contextType == build.ContextTypeStdin {
			return errors.New("invalid argument: can't use stdin for both build context and dockerfile")
		}
		dockerfileCtx = dockerCli.In()
	}

	progBuff = dockerCli.Out()
	buildBuff = dockerCli.Out()
	if options.quiet {
		progBuff = bytes.NewBuffer(nil)
		buildBuff = bytes.NewBuffer(nil)
	}
	if options.imageIDFile != "" {
		// Avoid leaving a stale file if we eventually fail
		if err := os.Remove(options.imageIDFile); err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "Removing image ID file")
		}
	}

	switch contextType {
	case build.ContextTypeStdin:
		// buildCtx is tar archive. if stdin was dockerfile then it is wrapped
		buildCtx, relDockerfile, err = build.GetContextFromReader(dockerCli.In(), options.dockerfileName)
		if err != nil {
			return fmt.Errorf("unable to prepare context from STDIN: %w", err)
		}
	case build.ContextTypeLocal:
		contextDir, relDockerfile, err = build.GetContextFromLocalDir(options.context, options.dockerfileName)
		if err != nil {
			return errors.Errorf("unable to prepare context: %s", err)
		}
		if strings.HasPrefix(relDockerfile, ".."+string(filepath.Separator)) {
			// Dockerfile is outside of build-context; read the Dockerfile and pass it as dockerfileCtx
			dockerfileCtx, err = os.Open(options.dockerfileName)
			if err != nil {
				return errors.Errorf("unable to open Dockerfile: %v", err)
			}
			defer dockerfileCtx.Close()
		}
	case build.ContextTypeGit:
		var tempDir string
		tempDir, relDockerfile, err = build.GetContextFromGitURL(options.context, options.dockerfileName)
		if err != nil {
			return errors.Errorf("unable to prepare context: %s", err)
		}
		defer func() {
			_ = os.RemoveAll(tempDir)
		}()
		contextDir = tempDir
	case build.ContextTypeRemote:
		buildCtx, relDockerfile, err = build.GetContextFromURL(progBuff, options.context, options.dockerfileName)
		if err != nil && options.quiet {
			_, _ = fmt.Fprintln(dockerCli.Err(), progBuff)
		}
	default:
		return errors.Errorf("unable to prepare context: path %q not found", options.context)
	}

	// read from a directory into tar archive
	if buildCtx == nil {
		excludes, err := build.ReadDockerignore(contextDir)
		if err != nil {
			return err
		}

		if err := build.ValidateContextDirectory(contextDir, excludes); err != nil {
			return errors.Wrap(err, "checking context")
		}

		// And canonicalize dockerfile name to a platform-independent one
		relDockerfile = filepath.ToSlash(relDockerfile)

		excludes = build.TrimBuildFilesFromExcludes(excludes, relDockerfile, options.dockerfileFromStdin())
		buildCtx, err = archive.TarWithOptions(contextDir, &archive.TarOptions{
			ExcludePatterns: excludes,
			ChownOpts:       &archive.ChownOpts{UID: 0, GID: 0},
		})
		if err != nil {
			return err
		}
	}

	// replace Dockerfile if it was added from stdin or a file outside the build-context, and there is archive context
	if dockerfileCtx != nil && buildCtx != nil {
		buildCtx, relDockerfile, err = build.AddDockerfileToBuildContext(dockerfileCtx, buildCtx)
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var resolvedTags []*resolvedTag
	if !options.untrusted {
		translator := func(ctx context.Context, ref reference.NamedTagged) (reference.Canonical, error) {
			return TrustedReference(ctx, dockerCli, ref)
		}
		// if there is a tar wrapper, the dockerfile needs to be replaced inside it
		if buildCtx != nil {
			// Wrap the tar archive to replace the Dockerfile entry with the rewritten
			// Dockerfile which uses trusted pulls.
			buildCtx = replaceDockerfileForContentTrust(ctx, buildCtx, relDockerfile, translator, &resolvedTags)
		} else if dockerfileCtx != nil {
			// if there was not archive context still do the possible replacements in Dockerfile
			newDockerfile, _, err := rewriteDockerfileFromForContentTrust(ctx, dockerfileCtx, translator)
			if err != nil {
				return err
			}
			dockerfileCtx = io.NopCloser(bytes.NewBuffer(newDockerfile))
		}
	}

	if options.compress {
		buildCtx, err = build.Compress(buildCtx)
		if err != nil {
			return err
		}
	}

	// Setup an upload progress bar
	progressOutput := streamformatter.NewProgressOutput(progBuff)
	if !dockerCli.Out().IsTerminal() {
		progressOutput = &lastProgressOutput{output: progressOutput}
	}

	// if up to this point nothing has set the context then we must have another
	// way for sending it(streaming) and set the context to the Dockerfile
	if dockerfileCtx != nil && buildCtx == nil {
		buildCtx = dockerfileCtx
	}

	var body io.Reader
	if buildCtx != nil {
		body = progress.NewProgressReader(buildCtx, progressOutput, 0, "", "Sending build context to Docker daemon")
	}

	configFile := dockerCli.ConfigFile()
	creds, _ := configFile.GetAllCredentials()
	authConfigs := make(map[string]registrytypes.AuthConfig, len(creds))
	for k, authConfig := range creds {
		authConfigs[k] = registrytypes.AuthConfig{
			Username:      authConfig.Username,
			Password:      authConfig.Password,
			ServerAddress: authConfig.ServerAddress,

			// TODO(thaJeztah): Are these expected to be included?
			Auth:          authConfig.Auth,
			IdentityToken: authConfig.IdentityToken,
			RegistryToken: authConfig.RegistryToken,
		}
	}
	buildOpts := imageBuildOptions(dockerCli, options)
	buildOpts.Dockerfile = relDockerfile
	buildOpts.AuthConfigs = authConfigs
	buildOpts.RemoteContext = remote

	response, err := dockerCli.Client().ImageBuild(ctx, body, buildOpts)
	if err != nil {
		if options.quiet {
			_, _ = fmt.Fprintf(dockerCli.Err(), "%s", progBuff)
		}
		return err
	}
	defer response.Body.Close()

	imageID := ""
	aux := func(msg jsonstream.JSONMessage) {
		var result buildtypes.Result
		if err := json.Unmarshal(*msg.Aux, &result); err != nil {
			_, _ = fmt.Fprintf(dockerCli.Err(), "Failed to parse aux message: %s", err)
		} else {
			imageID = result.ID
		}
	}

	err = jsonstream.Display(ctx, response.Body, streams.NewOut(buildBuff), jsonstream.WithAuxCallback(aux))
	if err != nil {
		var jerr *jsonstream.JSONError
		if errors.As(err, &jerr) {
			// If no error code is set, default to 1
			if jerr.Code == 0 {
				jerr.Code = 1
			}
			if options.quiet {
				_, _ = fmt.Fprintf(dockerCli.Err(), "%s%s", progBuff, buildBuff)
			}
			return cli.StatusError{Status: jerr.Message, StatusCode: jerr.Code}
		}
		return err
	}

	// Everything worked so if -q was provided the output from the daemon
	// should be just the image ID and we'll print that to stdout.
	if options.quiet {
		imageID = fmt.Sprintf("%s", buildBuff)
		_, _ = fmt.Fprint(dockerCli.Out(), imageID)
	}

	if options.imageIDFile != "" {
		if imageID == "" {
			return errors.Errorf("Server did not provide an image ID. Cannot write %s", options.imageIDFile)
		}
		if err := os.WriteFile(options.imageIDFile, []byte(imageID), 0o666); err != nil {
			return err
		}
	}
	if !options.untrusted {
		// Since the build was successful, now we must tag any of the resolved
		// images from the above Dockerfile rewrite.
		for _, resolved := range resolvedTags {
			if err := trust.TagTrusted(ctx, dockerCli.Client(), dockerCli.Err(), resolved.digestRef, resolved.tagRef); err != nil {
				return err
			}
		}
	}

	return nil
}

type translatorFunc func(context.Context, reference.NamedTagged) (reference.Canonical, error)

// validateTag checks if the given image name can be resolved.
func validateTag(rawRepo string) (string, error) {
	_, err := reference.ParseNormalizedNamed(rawRepo)
	if err != nil {
		return "", err
	}

	return rawRepo, nil
}

var dockerfileFromLinePattern = lazyregexp.New(`(?i)^[\s]*FROM[ \f\r\t\v]+(?P<image>[^ \f\r\t\v\n#]+)`)

// resolvedTag records the repository, tag, and resolved digest reference
// from a Dockerfile rewrite.
type resolvedTag struct {
	digestRef reference.Canonical
	tagRef    reference.NamedTagged
}

// rewriteDockerfileFromForContentTrust rewrites the given Dockerfile by resolving images in
// "FROM <image>" instructions to a digest reference. `translator` is a
// function that takes a repository name and tag reference and returns a
// trusted digest reference.
// This should be called *only* when content trust is enabled
func rewriteDockerfileFromForContentTrust(ctx context.Context, dockerfile io.Reader, translator translatorFunc) (newDockerfile []byte, resolvedTags []*resolvedTag, err error) {
	scanner := bufio.NewScanner(dockerfile)
	buf := bytes.NewBuffer(nil)

	// Scan the lines of the Dockerfile, looking for a "FROM" line.
	for scanner.Scan() {
		line := scanner.Text()

		matches := dockerfileFromLinePattern.FindStringSubmatch(line)
		if matches != nil && matches[1] != api.NoBaseImageSpecifier {
			// Replace the line with a resolved "FROM repo@digest"
			var ref reference.Named
			ref, err = reference.ParseNormalizedNamed(matches[1])
			if err != nil {
				return nil, nil, err
			}
			ref = reference.TagNameOnly(ref)
			if ref, ok := ref.(reference.NamedTagged); ok {
				trustedRef, err := translator(ctx, ref)
				if err != nil {
					return nil, nil, err
				}

				line = dockerfileFromLinePattern.ReplaceAllLiteralString(line, "FROM "+reference.FamiliarString(trustedRef))
				resolvedTags = append(resolvedTags, &resolvedTag{
					digestRef: trustedRef,
					tagRef:    ref,
				})
			}
		}

		_, err := fmt.Fprintln(buf, line)
		if err != nil {
			return nil, nil, err
		}
	}

	return buf.Bytes(), resolvedTags, scanner.Err()
}

// replaceDockerfileForContentTrust wraps the given input tar archive stream and
// uses the translator to replace the Dockerfile which uses a trusted reference.
// Returns a new tar archive stream with the replaced Dockerfile.
func replaceDockerfileForContentTrust(ctx context.Context, inputTarStream io.ReadCloser, dockerfileName string, translator translatorFunc, resolvedTags *[]*resolvedTag) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		tarReader := tar.NewReader(inputTarStream)
		tarWriter := tar.NewWriter(pipeWriter)

		defer inputTarStream.Close()

		for {
			hdr, err := tarReader.Next()
			if err == io.EOF {
				// Signals end of archive.
				_ = tarWriter.Close()
				_ = pipeWriter.Close()
				return
			}
			if err != nil {
				_ = pipeWriter.CloseWithError(err)
				return
			}

			content := io.Reader(tarReader)
			if hdr.Name == dockerfileName {
				// This entry is the Dockerfile. Since the tar archive was
				// generated from a directory on the local filesystem, the
				// Dockerfile will only appear once in the archive.
				var newDockerfile []byte
				newDockerfile, *resolvedTags, err = rewriteDockerfileFromForContentTrust(ctx, content, translator)
				if err != nil {
					_ = pipeWriter.CloseWithError(err)
					return
				}
				hdr.Size = int64(len(newDockerfile))
				content = bytes.NewBuffer(newDockerfile)
			}

			if err := tarWriter.WriteHeader(hdr); err != nil {
				_ = pipeWriter.CloseWithError(err)
				return
			}

			if _, err := io.Copy(tarWriter, content); err != nil {
				_ = pipeWriter.CloseWithError(err)
				return
			}
		}
	}()

	return pipeReader
}

func imageBuildOptions(dockerCli command.Cli, options buildOptions) buildtypes.ImageBuildOptions {
	configFile := dockerCli.ConfigFile()
	return buildtypes.ImageBuildOptions{
		Version:        buildtypes.BuilderV1,
		Memory:         options.memory.Value(),
		MemorySwap:     options.memorySwap.Value(),
		Tags:           options.tags.GetSlice(),
		SuppressOutput: options.quiet,
		NoCache:        options.noCache,
		Remove:         options.rm,
		ForceRemove:    options.forceRm,
		PullParent:     options.pull,
		Isolation:      container.Isolation(options.isolation),
		CPUSetCPUs:     options.cpuSetCpus,
		CPUSetMems:     options.cpuSetMems,
		CPUShares:      options.cpuShares,
		CPUQuota:       options.cpuQuota,
		CPUPeriod:      options.cpuPeriod,
		CgroupParent:   options.cgroupParent,
		ShmSize:        options.shmSize.Value(),
		Ulimits:        options.ulimits.GetList(),
		BuildArgs:      configFile.ParseProxyConfig(dockerCli.Client().DaemonHost(), opts.ConvertKVStringsToMapWithNil(options.buildArgs.GetSlice())),
		Labels:         opts.ConvertKVStringsToMap(options.labels.GetSlice()),
		CacheFrom:      options.cacheFrom,
		SecurityOpt:    options.securityOpt,
		NetworkMode:    options.networkMode,
		Squash:         options.squash,
		ExtraHosts:     options.extraHosts.GetSlice(),
		Target:         options.target,
		Platform:       options.platform,
	}
}
