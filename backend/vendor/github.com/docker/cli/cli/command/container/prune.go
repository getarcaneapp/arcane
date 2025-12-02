package container

import (
	"context"
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/internal/prompt"
	"github.com/docker/cli/opts"
	"github.com/docker/go-units"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type pruneOptions struct {
	force  bool
	filter opts.FilterOpt
}

// NewPruneCommand returns a new cobra prune command for containers
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPruneCommand(dockerCLI command.Cli) *cobra.Command {
	return newPruneCommand(dockerCLI)
}

func newPruneCommand(dockerCLI command.Cli) *cobra.Command {
	options := pruneOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "prune [OPTIONS]",
		Short: "Remove all stopped containers",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spaceReclaimed, output, err := runPrune(cmd.Context(), dockerCLI, options)
			if err != nil {
				return err
			}
			if output != "" {
				fmt.Fprintln(dockerCLI.Out(), output)
			}
			fmt.Fprintln(dockerCLI.Out(), "Total reclaimed space:", units.HumanSize(float64(spaceReclaimed)))
			return nil
		},
		Annotations:       map[string]string{"version": "1.25"},
		ValidArgsFunction: cobra.NoFileCompletions,
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")
	flags.Var(&options.filter, "filter", `Provide filter values (e.g. "until=<timestamp>")`)

	return cmd
}

const warning = `WARNING! This will remove all stopped containers.
Are you sure you want to continue?`

func runPrune(ctx context.Context, dockerCli command.Cli, options pruneOptions) (spaceReclaimed uint64, output string, err error) {
	pruneFilters := command.PruneFilters(dockerCli, options.filter.Value())

	if !options.force {
		r, err := prompt.Confirm(ctx, dockerCli.In(), dockerCli.Out(), warning)
		if err != nil {
			return 0, "", err
		}
		if !r {
			return 0, "", cancelledErr{errors.New("container prune has been cancelled")}
		}
	}

	report, err := dockerCli.Client().ContainersPrune(ctx, pruneFilters)
	if err != nil {
		return 0, "", err
	}

	if len(report.ContainersDeleted) > 0 {
		output = "Deleted Containers:\n"
		for _, id := range report.ContainersDeleted {
			output += id + "\n"
		}
		spaceReclaimed = report.SpaceReclaimed
	}

	return spaceReclaimed, output, nil
}

type cancelledErr struct{ error }

func (cancelledErr) Cancelled() {}

// RunPrune calls the Container Prune API
// This returns the amount of space reclaimed and a detailed output string
func RunPrune(ctx context.Context, dockerCli command.Cli, _ bool, filter opts.FilterOpt) (uint64, string, error) {
	return runPrune(ctx, dockerCli, pruneOptions{force: true, filter: filter})
}
