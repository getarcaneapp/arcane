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

type waitOptions struct {
	containers []string
}

// NewWaitCommand creates a new cobra.Command for `docker wait`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewWaitCommand(dockerCLI command.Cli) *cobra.Command {
	return newWaitCommand(dockerCLI)
}

func newWaitCommand(dockerCLI command.Cli) *cobra.Command {
	var opts waitOptions

	cmd := &cobra.Command{
		Use:   "wait CONTAINER [CONTAINER...]",
		Short: "Block until one or more containers stop, then print their exit codes",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.containers = args
			return runWait(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container wait, docker wait",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
	}

	return cmd
}

func runWait(ctx context.Context, dockerCLI command.Cli, opts *waitOptions) error {
	apiClient := dockerCLI.Client()

	var errs []error
	for _, ctr := range opts.containers {
		resultC, errC := apiClient.ContainerWait(ctx, ctr, "")

		select {
		case result := <-resultC:
			_, _ = fmt.Fprintf(dockerCLI.Out(), "%d\n", result.StatusCode)
		case err := <-errC:
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
