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

type pauseOptions struct {
	containers []string
}

// NewPauseCommand creates a new cobra.Command for `docker pause`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPauseCommand(dockerCLI command.Cli) *cobra.Command {
	return newPauseCommand(dockerCLI)
}

func newPauseCommand(dockerCLI command.Cli) *cobra.Command {
	var opts pauseOptions

	return &cobra.Command{
		Use:   "pause CONTAINER [CONTAINER...]",
		Short: "Pause all processes within one or more containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.containers = args
			return runPause(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container pause, docker pause",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false, func(ctr container.Summary) bool {
			return ctr.State != container.StatePaused
		}),
	}
}

func runPause(ctx context.Context, dockerCLI command.Cli, opts *pauseOptions) error {
	apiClient := dockerCLI.Client()
	errChan := parallelOperation(ctx, opts.containers, apiClient.ContainerPause)

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
