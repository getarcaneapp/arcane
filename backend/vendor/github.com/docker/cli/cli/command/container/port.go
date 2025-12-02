package container

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/go-connections/nat"
	"github.com/fvbommel/sortorder"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type portOptions struct {
	container string

	port string
}

// NewPortCommand creates a new cobra.Command for `docker port`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPortCommand(dockerCli command.Cli) *cobra.Command {
	return newPortCommand(dockerCli)
}

func newPortCommand(dockerCli command.Cli) *cobra.Command {
	var opts portOptions

	cmd := &cobra.Command{
		Use:   "port CONTAINER [PRIVATE_PORT[/PROTO]]",
		Short: "List port mappings or a specific mapping for the container",
		Args:  cli.RequiresRangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.container = args[0]
			if len(args) > 1 {
				opts.port = args[1]
			}
			return runPort(cmd.Context(), dockerCli, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container port, docker port",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCli, false),
	}
	return cmd
}

// runPort shows the port mapping for a given container. Optionally, it
// allows showing the mapping for a specific (container)port and proto.
//
// TODO(thaJeztah): currently this defaults to show the TCP port if no
// proto is specified. We should consider changing this to "any" protocol
// for the given private port.
func runPort(ctx context.Context, dockerCli command.Cli, opts *portOptions) error {
	c, err := dockerCli.Client().ContainerInspect(ctx, opts.container)
	if err != nil {
		return err
	}

	var out []string
	if opts.port != "" {
		port, proto, _ := strings.Cut(opts.port, "/")
		if proto == "" {
			proto = "tcp"
		}
		if _, err = strconv.ParseUint(port, 10, 16); err != nil {
			return errors.Wrapf(err, "Error: invalid port (%s)", port)
		}
		frontends, exists := c.NetworkSettings.Ports[nat.Port(port+"/"+proto)]
		if !exists || len(frontends) == 0 {
			return errors.Errorf("Error: No public port '%s' published for %s", opts.port, opts.container)
		}
		for _, frontend := range frontends {
			out = append(out, net.JoinHostPort(frontend.HostIP, frontend.HostPort))
		}
	} else {
		for from, frontends := range c.NetworkSettings.Ports {
			for _, frontend := range frontends {
				out = append(out, fmt.Sprintf("%s -> %s", from, net.JoinHostPort(frontend.HostIP, frontend.HostPort)))
			}
		}
	}

	if len(out) > 0 {
		sort.Slice(out, func(i, j int) bool {
			return sortorder.NaturalLess(out[i], out[j])
		})
		_, _ = fmt.Fprintln(dockerCli.Out(), strings.Join(out, "\n"))
	}

	return nil
}
