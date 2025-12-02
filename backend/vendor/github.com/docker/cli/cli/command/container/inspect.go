// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package container

import (
	"context"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/inspect"
	flagsHelper "github.com/docker/cli/cli/flags"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	format string
	size   bool
	refs   []string
}

// newInspectCommand creates a new cobra.Command for `docker container inspect`
func newInspectCommand(dockerCli command.Cli) *cobra.Command {
	var opts inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Display detailed information on one or more containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.refs = args
			return runInspect(cmd.Context(), dockerCli, opts)
		},
		ValidArgsFunction: completion.ContainerNames(dockerCli, true),
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.format, "format", "f", "", flagsHelper.InspectFormatHelp)
	flags.BoolVarP(&opts.size, "size", "s", false, "Display total file sizes")

	return cmd
}

func runInspect(ctx context.Context, dockerCLI command.Cli, opts inspectOptions) error {
	apiClient := dockerCLI.Client()
	return inspect.Inspect(dockerCLI.Out(), opts.refs, opts.format, func(ref string) (any, []byte, error) {
		return apiClient.ContainerInspectWithRaw(ctx, ref, opts.size)
	})
}
