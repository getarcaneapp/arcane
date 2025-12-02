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

type stopOptions struct {
	signal         string
	timeout        int
	timeoutChanged bool

	containers []string
}

// NewStopCommand creates a new cobra.Command for `docker stop`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewStopCommand(dockerCLI command.Cli) *cobra.Command {
	return newStopCommand(dockerCLI)
}

func newStopCommand(dockerCLI command.Cli) *cobra.Command {
	var opts stopOptions

	cmd := &cobra.Command{
		Use:   "stop [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Stop one or more running containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("time") && cmd.Flags().Changed("timeout") {
				return errors.New("conflicting options: cannot specify both --timeout and --time")
			}
			opts.containers = args
			opts.timeoutChanged = cmd.Flags().Changed("timeout") || cmd.Flags().Changed("time")
			return runStop(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container stop, docker stop",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
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

func runStop(ctx context.Context, dockerCLI command.Cli, opts *stopOptions) error {
	var timeout *int
	if opts.timeoutChanged {
		timeout = &opts.timeout
	}

	apiClient := dockerCLI.Client()
	errChan := parallelOperation(ctx, opts.containers, func(ctx context.Context, id string) error {
		return apiClient.ContainerStop(ctx, id, container.StopOptions{
			Signal:  opts.signal,
			Timeout: timeout,
		})
	})
	var errs []error
	for _, ctr := range opts.containers {
		if err := <-errChan; err != nil {
			errs = append(errs, err)
			continue
		}
		_, _ = fmt.Fprintln(dockerCLI.Out(), ctr)
	}
	return errors.Join(errs...)
}
