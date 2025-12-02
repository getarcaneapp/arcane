package container

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/image"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/streams"
	"github.com/docker/cli/cli/trust"
	"github.com/docker/cli/internal/jsonstream"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/container"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Pull constants
const (
	PullImageAlways  = "always"
	PullImageMissing = "missing" // Default (matches previous behavior)
	PullImageNever   = "never"
)

type createOptions struct {
	name         string
	platform     string
	untrusted    bool
	pull         string // always, missing, never
	quiet        bool
	useAPISocket bool
}

// NewCreateCommand creates a new cobra.Command for `docker create`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewCreateCommand(dockerCLI command.Cli) *cobra.Command {
	return newCreateCommand(dockerCLI)
}

func newCreateCommand(dockerCLI command.Cli) *cobra.Command {
	var options createOptions
	var copts *containerOptions

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] IMAGE [COMMAND] [ARG...]",
		Short: "Create a new container",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			copts.Image = args[0]
			if len(args) > 1 {
				copts.Args = args[1:]
			}
			return runCreate(cmd.Context(), dockerCLI, cmd.Flags(), &options, copts)
		},
		Annotations: map[string]string{
			"aliases": "docker container create, docker create",
		},
		ValidArgsFunction: completion.ImageNames(dockerCLI, -1),
	}

	flags := cmd.Flags()
	flags.SetInterspersed(false)

	flags.StringVar(&options.name, "name", "", "Assign a name to the container")
	flags.StringVar(&options.pull, "pull", PullImageMissing, `Pull image before creating ("`+PullImageAlways+`", "|`+PullImageMissing+`", "`+PullImageNever+`")`)
	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the pull output")
	flags.BoolVarP(&options.useAPISocket, "use-api-socket", "", false, "Bind mount Docker API socket and required auth")
	flags.SetAnnotation("use-api-socket", "experimentalCLI", nil) // Marks flag as experimental for now.

	// Add an explicit help that doesn't have a `-h` to prevent the conflict
	// with hostname
	flags.Bool("help", false, "Print usage")

	// TODO(thaJeztah): consider adding platform as "image create option" on containerOptions
	addPlatformFlag(flags, &options.platform)
	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)

	flags.BoolVar(&options.untrusted, "disable-content-trust", !trust.Enabled(), "Skip image verification")
	copts = addFlags(flags)

	addCompletions(cmd, dockerCLI)

	flags.VisitAll(func(flag *pflag.Flag) {
		// Set a default completion function if none was set. We don't look
		// up if it does already have one set, because Cobra does this for
		// us, and returns an error (which we ignore for this reason).
		_ = cmd.RegisterFlagCompletionFunc(flag.Name, cobra.NoFileCompletions)
	})

	return cmd
}

func runCreate(ctx context.Context, dockerCli command.Cli, flags *pflag.FlagSet, options *createOptions, copts *containerOptions) error {
	if err := validatePullOpt(options.pull); err != nil {
		return cli.StatusError{
			Status:     withHelp(err, "create").Error(),
			StatusCode: 125,
		}
	}
	proxyConfig := dockerCli.ConfigFile().ParseProxyConfig(dockerCli.Client().DaemonHost(), opts.ConvertKVStringsToMapWithNil(copts.env.GetSlice()))
	newEnv := []string{}
	for k, v := range proxyConfig {
		if v == nil {
			newEnv = append(newEnv, k)
		} else {
			newEnv = append(newEnv, k+"="+*v)
		}
	}
	copts.env = *opts.NewListOptsRef(&newEnv, nil)
	containerCfg, err := parse(flags, copts, dockerCli.ServerInfo().OSType)
	if err != nil {
		return cli.StatusError{
			Status:     withHelp(err, "create").Error(),
			StatusCode: 125,
		}
	}
	id, err := createContainer(ctx, dockerCli, containerCfg, options)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(dockerCli.Out(), id)
	return nil
}

// FIXME(thaJeztah): this is the only code-path that uses APIClient.ImageCreate. Rewrite this to use the regular "pull" code (or vice-versa).
func pullImage(ctx context.Context, dockerCli command.Cli, img string, options *createOptions) error {
	encodedAuth, err := command.RetrieveAuthTokenFromImage(dockerCli.ConfigFile(), img)
	if err != nil {
		return err
	}

	responseBody, err := dockerCli.Client().ImageCreate(ctx, img, imagetypes.CreateOptions{
		RegistryAuth: encodedAuth,
		Platform:     options.platform,
	})
	if err != nil {
		return err
	}
	defer responseBody.Close()

	out := dockerCli.Err()
	if options.quiet {
		out = streams.NewOut(io.Discard)
	}
	return jsonstream.Display(ctx, responseBody, out)
}

type cidFile struct {
	path    string
	file    *os.File
	written bool
}

func (cid *cidFile) Close() error {
	if cid.file == nil {
		return nil
	}
	cid.file.Close()

	if cid.written {
		return nil
	}
	if err := os.Remove(cid.path); err != nil {
		return errors.Wrapf(err, "failed to remove the CID file '%s'", cid.path)
	}

	return nil
}

func (cid *cidFile) Write(id string) error {
	if cid.file == nil {
		return nil
	}
	if _, err := cid.file.Write([]byte(id)); err != nil {
		return errors.Wrap(err, "failed to write the container ID to the file")
	}
	cid.written = true
	return nil
}

func newCIDFile(cidPath string) (*cidFile, error) {
	if cidPath == "" {
		return &cidFile{}, nil
	}
	if _, err := os.Stat(cidPath); err == nil {
		return nil, errors.Errorf("container ID file found, make sure the other container isn't running or delete %s", cidPath)
	}

	f, err := os.Create(cidPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the container ID file")
	}

	return &cidFile{path: cidPath, file: f}, nil
}

//nolint:gocyclo
func createContainer(ctx context.Context, dockerCli command.Cli, containerCfg *containerConfig, options *createOptions) (containerID string, err error) {
	config := containerCfg.Config
	hostConfig := containerCfg.HostConfig
	networkingConfig := containerCfg.NetworkingConfig

	var (
		trustedRef reference.Canonical
		namedRef   reference.Named
	)

	containerIDFile, err := newCIDFile(hostConfig.ContainerIDFile)
	if err != nil {
		return "", err
	}
	defer containerIDFile.Close()

	ref, err := reference.ParseAnyReference(config.Image)
	if err != nil {
		return "", err
	}
	if named, ok := ref.(reference.Named); ok {
		namedRef = reference.TagNameOnly(named)

		if taggedRef, ok := namedRef.(reference.NamedTagged); ok && !options.untrusted {
			var err error
			trustedRef, err = image.TrustedReference(ctx, dockerCli, taggedRef)
			if err != nil {
				return "", err
			}
			config.Image = reference.FamiliarString(trustedRef)
		}
	}

	const dockerConfigPathInContainer = "/run/secrets/docker/config.json"
	var apiSocketCreds map[string]types.AuthConfig

	if options.useAPISocket {
		// We'll create two new mounts to handle this flag:
		// 1. Mount the actual docker socket.
		// 2. A synthezised ~/.docker/config.json with resolved tokens.

		if dockerCli.ServerInfo().OSType == "windows" {
			return "", errors.New("flag --use-api-socket can't be used with a Windows Docker Engine")
		}

		// hard-code engine socket path until https://github.com/moby/moby/pull/43459 gives us a discovery mechanism
		containerCfg.HostConfig.Mounts = append(containerCfg.HostConfig.Mounts, mount.Mount{
			Type:        mount.TypeBind,
			Source:      "/var/run/docker.sock",
			Target:      "/var/run/docker.sock",
			BindOptions: &mount.BindOptions{},
		})

		/*

		   Ideally, we'd like to copy the config into a tmpfs but unfortunately,
		   the mounts won't be in place until we start the container. This can
		   leave around the config if the container doesn't get deleted.

		   We are using the most compose-secret-compatible approach,
		   which is implemented at
		   https://github.com/docker/compose/blob/main/pkg/compose/convergence.go#L737

		   // Prepare a tmpfs mount for our credentials so they go away after the
		   // container exits. We'll copy into this mount after the container is
		   // created.
		   containerCfg.HostConfig.Mounts = append(containerCfg.HostConfig.Mounts, mount.Mount{
		       Type:   mount.TypeTmpfs,
		       Target: "/docker/",
		       TmpfsOptions: &mount.TmpfsOptions{
		           SizeBytes: 1 << 20, // only need a small partition
		           Mode:      0o600,
		       },
		   })
		*/

		var envvarPresent bool
		for _, envvar := range containerCfg.Config.Env {
			if strings.HasPrefix(envvar, "DOCKER_CONFIG=") {
				envvarPresent = true
			}
		}

		// If the DOCKER_CONFIG env var is already present, we assume the client knows
		// what they're doing and don't inject the creds.
		if !envvarPresent {
			// Resolve this here for later, ensuring we error our before we create the container.
			creds, err := readCredentials(dockerCli)
			if err != nil {
				return "", fmt.Errorf("resolving credentials failed: %w", err)
			}
			if len(creds) > 0 {
				// Set our special little location for the config file.
				containerCfg.Config.Env = append(containerCfg.Config.Env, "DOCKER_CONFIG="+path.Dir(dockerConfigPathInContainer))

				apiSocketCreds = creds // inject these after container creation.
			}
		}
	}

	var platform *ocispec.Platform
	// Engine API version 1.41 first introduced the option to specify platform on
	// create. It will produce an error if you try to set a platform on older API
	// versions, so check the API version here to maintain backwards
	// compatibility for CLI users.
	if options.platform != "" && versions.GreaterThanOrEqualTo(dockerCli.Client().ClientVersion(), "1.41") {
		p, err := platforms.Parse(options.platform)
		if err != nil {
			return "", errors.Wrap(invalidParameter(err), "error parsing specified platform")
		}
		platform = &p
	}

	pullAndTagImage := func() error {
		if err := pullImage(ctx, dockerCli, config.Image, options); err != nil {
			return err
		}
		if taggedRef, ok := namedRef.(reference.NamedTagged); ok && trustedRef != nil {
			return trust.TagTrusted(ctx, dockerCli.Client(), dockerCli.Err(), trustedRef, taggedRef)
		}
		return nil
	}

	if options.pull == PullImageAlways {
		if err := pullAndTagImage(); err != nil {
			return "", err
		}
	}

	hostConfig.ConsoleSize[0], hostConfig.ConsoleSize[1] = dockerCli.Out().GetTtySize()

	response, err := dockerCli.Client().ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, options.name)
	if err != nil {
		// Pull image if it does not exist locally and we have the PullImageMissing option. Default behavior.
		if errdefs.IsNotFound(err) && namedRef != nil && options.pull == PullImageMissing {
			if !options.quiet {
				// we don't want to write to stdout anything apart from container.ID
				_, _ = fmt.Fprintf(dockerCli.Err(), "Unable to find image '%s' locally\n", reference.FamiliarString(namedRef))
			}

			if err := pullAndTagImage(); err != nil {
				return "", err
			}

			var retryErr error
			response, retryErr = dockerCli.Client().ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, options.name)
			if retryErr != nil {
				return "", retryErr
			}
		} else {
			return "", err
		}
	}

	if warn := localhostDNSWarning(*hostConfig); warn != "" {
		response.Warnings = append(response.Warnings, warn)
	}

	containerID = response.ID
	for _, w := range response.Warnings {
		_, _ = fmt.Fprintln(dockerCli.Err(), "WARNING:", w)
	}
	err = containerIDFile.Write(containerID)

	if options.useAPISocket && len(apiSocketCreds) > 0 {
		// Create a new config file with just the auth.
		newConfig := &configfile.ConfigFile{
			AuthConfigs: apiSocketCreds,
		}

		if err := copyDockerConfigIntoContainer(ctx, dockerCli.Client(), containerID, dockerConfigPathInContainer, newConfig); err != nil {
			return "", fmt.Errorf("injecting docker config.json into container failed: %w", err)
		}
	}

	return containerID, err
}

// check the DNS settings passed via --dns against localhost regexp to warn if
// they are trying to set a DNS to a localhost address.
//
// TODO(thaJeztah): move this to the daemon, which can make a better call if it will work or not (depending on networking mode).
func localhostDNSWarning(hostConfig container.HostConfig) string {
	for _, dnsIP := range hostConfig.DNS {
		if addr, err := netip.ParseAddr(dnsIP); err == nil && addr.IsLoopback() {
			return fmt.Sprintf("Localhost DNS (%s) may fail in containers.", addr)
		}
	}
	return ""
}

func validatePullOpt(val string) error {
	switch val {
	case PullImageAlways, PullImageMissing, PullImageNever, "":
		// valid option, but nothing to do yet
		return nil
	default:
		return fmt.Errorf(
			"invalid pull option: '%s': must be one of %q, %q or %q",
			val,
			PullImageAlways,
			PullImageMissing,
			PullImageNever,
		)
	}
}

// copyDockerConfigIntoContainer takes the client configuration and copies it
// into the container.
//
// The path should be an absolute path in the container, commonly
// /root/.docker/config.json.
func copyDockerConfigIntoContainer(ctx context.Context, dockerAPI client.APIClient, containerID string, configPath string, config *configfile.ConfigFile) error {
	var configBuf bytes.Buffer
	if err := config.SaveToWriter(&configBuf); err != nil {
		return fmt.Errorf("saving creds: %w", err)
	}

	// We don't need to get super fancy with the tar creation.
	var tarBuf bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuf)
	tarWriter.WriteHeader(&tar.Header{
		Name: configPath,
		Size: int64(configBuf.Len()),
		Mode: 0o600,
	})

	if _, err := io.Copy(tarWriter, &configBuf); err != nil {
		return fmt.Errorf("writing config to tar file for config copy: %w", err)
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("closing tar for config copy failed: %w", err)
	}

	if err := dockerAPI.CopyToContainer(ctx, containerID, "/",
		&tarBuf, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("copying config.json into container failed: %w", err)
	}

	return nil
}
