package container

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/spf13/cobra"
)

type killOptions struct {
	signal string

	containers []string
}

// NewKillCommand creates a new cobra.Command for `docker kill`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewKillCommand(dockerCLI command.Cli) *cobra.Command {
	return newKillCommand(dockerCLI)
}

func newKillCommand(dockerCLI command.Cli) *cobra.Command {
	var opts killOptions

	cmd := &cobra.Command{
		Use:   "kill [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Kill one or more running containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.containers = args
			return runKill(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container kill, docker kill",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.signal, "signal", "s", "", "Signal to send to the container")

	_ = cmd.RegisterFlagCompletionFunc("signal", completeSignals)

	return cmd
}

func runKill(ctx context.Context, dockerCLI command.Cli, opts *killOptions) error {
	apiClient := dockerCLI.Client()
	errChan := parallelOperation(ctx, opts.containers, func(ctx context.Context, container string) error {
		return apiClient.ContainerKill(ctx, container, opts.signal)
	})

	var errs []error
	for _, name := range opts.containers {
		if err := <-errChan; err != nil {
			errs = append(errs, err)
			continue
		}
		_, _ = fmt.Fprintln(dockerCLI.Out(), name)
	}
	return errors.Join(errs...)
}
