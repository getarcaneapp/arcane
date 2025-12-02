package container

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
)

type restartOptions struct {
	signal         string
	timeout        int
	timeoutChanged bool

	containers []string
}

// NewRestartCommand creates a new cobra.Command for `docker restart`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewRestartCommand(dockerCLI command.Cli) *cobra.Command {
	return newRestartCommand(dockerCLI)
}

func newRestartCommand(dockerCLI command.Cli) *cobra.Command {
	var opts restartOptions

	cmd := &cobra.Command{
		Use:   "restart [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Restart one or more containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("time") && cmd.Flags().Changed("timeout") {
				return errors.New("conflicting options: cannot specify both --timeout and --time")
			}
			opts.containers = args
			opts.timeoutChanged = cmd.Flags().Changed("timeout") || cmd.Flags().Changed("time")
			return runRestart(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container restart, docker restart",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, true),
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.signal, "signal", "s", "", "Signal to send to the container")
	flags.IntVarP(&opts.timeout, "timeout", "t", 0, "Seconds to wait before killing the container")

	// The --time option is deprecated, but kept for backward compatibility.
	flags.IntVar(&opts.timeout, "time", 0, "Seconds to wait before killing the container (deprecated: use --timeout)")
	_ = flags.MarkDeprecated("time", "use --timeout instead")

	_ = cmd.RegisterFlagCompletionFunc("signal", completeSignals)

	return cmd
}

func runRestart(ctx context.Context, dockerCLI command.Cli, opts *restartOptions) error {
	var timeout *int
	if opts.timeoutChanged {
		timeout = &opts.timeout
	}

	apiClient := dockerCLI.Client()
	var errs []error
	// TODO(thaJeztah): consider using parallelOperation for restart, similar to "stop" and "remove"
	for _, name := range opts.containers {
		err := apiClient.ContainerRestart(ctx, name, container.StopOptions{
			Signal:  opts.signal,
			Timeout: timeout,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_, _ = fmt.Fprintln(dockerCLI.Out(), name)
	}
	return errors.Join(errs...)
}
