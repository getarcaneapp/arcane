package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/formatter/tabwriter"
	"github.com/spf13/cobra"
)

type topOptions struct {
	container string

	args []string
}

// NewTopCommand creates a new cobra.Command for `docker top`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewTopCommand(dockerCLI command.Cli) *cobra.Command {
	return newTopCommand(dockerCLI)
}

func newTopCommand(dockerCLI command.Cli) *cobra.Command {
	var opts topOptions

	cmd := &cobra.Command{
		Use:   "top CONTAINER [ps OPTIONS]",
		Short: "Display the running processes of a container",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.container = args[0]
			opts.args = args[1:]
			return runTop(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container top, docker top",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
	}

	flags := cmd.Flags()
	flags.SetInterspersed(false)

	return cmd
}

func runTop(ctx context.Context, dockerCli command.Cli, opts *topOptions) error {
	procList, err := dockerCli.Client().ContainerTop(ctx, opts.container, opts.args)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(dockerCli.Out(), 20, 1, 3, ' ', 0)
	fmt.Fprintln(w, strings.Join(procList.Titles, "\t"))

	for _, proc := range procList.Processes {
		fmt.Fprintln(w, strings.Join(proc, "\t"))
	}
	w.Flush()
	return nil
}
