package image

import (
	"context"

	"github.com/containerd/platforms"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/formatter"
	flagsHelper "github.com/docker/cli/cli/flags"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type historyOptions struct {
	image    string
	platform string

	human   bool
	quiet   bool
	noTrunc bool
	format  string
}

// NewHistoryCommand creates a new `docker history` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewHistoryCommand(dockerCLI command.Cli) *cobra.Command {
	return newHistoryCommand(dockerCLI)
}

// newHistoryCommand creates a new `docker history` command
func newHistoryCommand(dockerCLI command.Cli) *cobra.Command {
	var opts historyOptions

	cmd := &cobra.Command{
		Use:   "history [OPTIONS] IMAGE",
		Short: "Show the history of an image",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.image = args[0]
			return runHistory(cmd.Context(), dockerCLI, opts)
		},
		ValidArgsFunction: completion.ImageNames(dockerCLI, 1),
		Annotations: map[string]string{
			"aliases": "docker image history, docker history",
		},
	}

	flags := cmd.Flags()

	flags.BoolVarP(&opts.human, "human", "H", true, "Print sizes and dates in human readable format")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Only show image IDs")
	flags.BoolVar(&opts.noTrunc, "no-trunc", false, "Don't truncate output")
	flags.StringVar(&opts.format, "format", "", flagsHelper.FormatHelp)
	flags.StringVar(&opts.platform, "platform", "", `Show history for the given platform. Formatted as "os[/arch[/variant]]" (e.g., "linux/amd64")`)
	_ = flags.SetAnnotation("platform", "version", []string{"1.48"})

	_ = cmd.RegisterFlagCompletionFunc("platform", completion.Platforms)
	return cmd
}

func runHistory(ctx context.Context, dockerCli command.Cli, opts historyOptions) error {
	var options []client.ImageHistoryOption
	if opts.platform != "" {
		p, err := platforms.Parse(opts.platform)
		if err != nil {
			return errors.Wrap(err, "invalid platform")
		}
		options = append(options, client.ImageHistoryWithPlatform(p))
	}

	history, err := dockerCli.Client().ImageHistory(ctx, opts.image, options...)
	if err != nil {
		return err
	}

	format := opts.format
	if len(format) == 0 {
		format = formatter.TableFormatKey
	}

	historyCtx := formatter.Context{
		Output: dockerCli.Out(),
		Format: newHistoryFormat(format, opts.quiet, opts.human),
		Trunc:  !opts.noTrunc,
	}
	return historyWrite(historyCtx, opts.human, history)
}
