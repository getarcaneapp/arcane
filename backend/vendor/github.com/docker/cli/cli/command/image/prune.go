package image

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	all    bool
	filter opts.FilterOpt
}

// NewPruneCommand returns a new cobra prune command for images
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPruneCommand(dockerCLI command.Cli) *cobra.Command {
	return newPruneCommand(dockerCLI)
}

// newPruneCommand returns a new cobra prune command for images
func newPruneCommand(dockerCLI command.Cli) *cobra.Command {
	options := pruneOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "prune [OPTIONS]",
		Short: "Remove unused images",
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
	flags.BoolVarP(&options.all, "all", "a", false, "Remove all unused images, not just dangling ones")
	flags.Var(&options.filter, "filter", `Provide filter values (e.g. "until=<timestamp>")`)

	return cmd
}

const (
	allImageWarning = `WARNING! This will remove all images without at least one container associated to them.
Are you sure you want to continue?`
	danglingWarning = `WARNING! This will remove all dangling images.
Are you sure you want to continue?`
)

func runPrune(ctx context.Context, dockerCli command.Cli, options pruneOptions) (spaceReclaimed uint64, output string, err error) {
	pruneFilters := options.filter.Value().Clone()
	pruneFilters.Add("dangling", strconv.FormatBool(!options.all))
	pruneFilters = command.PruneFilters(dockerCli, pruneFilters)

	warning := danglingWarning
	if options.all {
		warning = allImageWarning
	}
	if !options.force {
		r, err := prompt.Confirm(ctx, dockerCli.In(), dockerCli.Out(), warning)
		if err != nil {
			return 0, "", err
		}
		if !r {
			return 0, "", cancelledErr{errors.New("image prune has been cancelled")}
		}
	}

	report, err := dockerCli.Client().ImagesPrune(ctx, pruneFilters)
	if err != nil {
		return 0, "", err
	}

	if len(report.ImagesDeleted) > 0 {
		var sb strings.Builder
		sb.WriteString("Deleted Images:\n")
		for _, st := range report.ImagesDeleted {
			if st.Untagged != "" {
				sb.WriteString("untagged: ")
				sb.WriteString(st.Untagged)
				sb.WriteByte('\n')
			} else {
				sb.WriteString("deleted: ")
				sb.WriteString(st.Deleted)
				sb.WriteByte('\n')
			}
		}
		output = sb.String()
		spaceReclaimed = report.SpaceReclaimed
	}

	return spaceReclaimed, output, nil
}

type cancelledErr struct{ error }

func (cancelledErr) Cancelled() {}

// RunPrune calls the Image Prune API
// This returns the amount of space reclaimed and a detailed output string
func RunPrune(ctx context.Context, dockerCli command.Cli, all bool, filter opts.FilterOpt) (uint64, string, error) {
	return runPrune(ctx, dockerCli, pruneOptions{force: true, all: all, filter: filter})
}
