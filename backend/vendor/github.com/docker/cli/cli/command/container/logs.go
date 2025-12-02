package container

import (
	"context"
	"io"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/spf13/cobra"
)

type logsOptions struct {
	follow     bool
	since      string
	until      string
	timestamps bool
	details    bool
	tail       string

	container string
}

// NewLogsCommand creates a new cobra.Command for `docker logs`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewLogsCommand(dockerCLI command.Cli) *cobra.Command {
	return newLogsCommand(dockerCLI)
}

func newLogsCommand(dockerCLI command.Cli) *cobra.Command {
	var opts logsOptions

	cmd := &cobra.Command{
		Use:   "logs [OPTIONS] CONTAINER",
		Short: "Fetch the logs of a container",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.container = args[0]
			return runLogs(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container logs, docker logs",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, true),
	}

	flags := cmd.Flags()
	flags.BoolVarP(&opts.follow, "follow", "f", false, "Follow log output")
	flags.StringVar(&opts.since, "since", "", `Show logs since timestamp (e.g. "2013-01-02T13:23:37Z") or relative (e.g. "42m" for 42 minutes)`)
	flags.StringVar(&opts.until, "until", "", `Show logs before a timestamp (e.g. "2013-01-02T13:23:37Z") or relative (e.g. "42m" for 42 minutes)`)
	flags.SetAnnotation("until", "version", []string{"1.35"})
	flags.BoolVarP(&opts.timestamps, "timestamps", "t", false, "Show timestamps")
	flags.BoolVar(&opts.details, "details", false, "Show extra details provided to logs")
	flags.StringVarP(&opts.tail, "tail", "n", "all", "Number of lines to show from the end of the logs")
	return cmd
}

func runLogs(ctx context.Context, dockerCli command.Cli, opts *logsOptions) error {
	c, err := dockerCli.Client().ContainerInspect(ctx, opts.container)
	if err != nil {
		return err
	}

	responseBody, err := dockerCli.Client().ContainerLogs(ctx, c.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Since:      opts.since,
		Until:      opts.until,
		Timestamps: opts.timestamps,
		Follow:     opts.follow,
		Tail:       opts.tail,
		Details:    opts.details,
	})
	if err != nil {
		return err
	}
	defer responseBody.Close()

	if c.Config.Tty {
		_, err = io.Copy(dockerCli.Out(), responseBody)
	} else {
		_, err = stdcopy.StdCopy(dockerCli.Out(), dockerCli.Err(), responseBody)
	}
	return err
}
