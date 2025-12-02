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

type unpauseOptions struct {
	containers []string
}

// NewUnpauseCommand creates a new cobra.Command for `docker unpause`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewUnpauseCommand(dockerCli command.Cli) *cobra.Command {
	return newUnpauseCommand(dockerCli)
}

func newUnpauseCommand(dockerCli command.Cli) *cobra.Command {
	var opts unpauseOptions

	cmd := &cobra.Command{
		Use:   "unpause CONTAINER [CONTAINER...]",
		Short: "Unpause all processes within one or more containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.containers = args
			return runUnpause(cmd.Context(), dockerCli, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container unpause, docker unpause",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCli, false, func(ctr container.Summary) bool {
			return ctr.State == container.StatePaused
		}),
	}
	return cmd
}

func runUnpause(ctx context.Context, dockerCLI command.Cli, opts *unpauseOptions) error {
	apiClient := dockerCLI.Client()
	errChan := parallelOperation(ctx, opts.containers, apiClient.ContainerUnpause)
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
