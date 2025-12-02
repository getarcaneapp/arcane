package container

import (
	"context"
	"io"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	flagsHelper "github.com/docker/cli/cli/flags"
	"github.com/docker/cli/opts"
	"github.com/docker/cli/templates"
	"github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type psOptions struct {
	quiet       bool
	size        bool
	sizeChanged bool
	all         bool
	noTrunc     bool
	nLatest     bool
	last        int
	format      string
	filter      opts.FilterOpt
}

// NewPsCommand creates a new cobra.Command for `docker ps`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewPsCommand(dockerCLI command.Cli) *cobra.Command {
	return newPsCommand(dockerCLI)
}

func newPsCommand(dockerCLI command.Cli) *cobra.Command {
	options := psOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "ps [OPTIONS]",
		Short: "List containers",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.sizeChanged = cmd.Flags().Changed("size")
			return runPs(cmd.Context(), dockerCLI, &options)
		},
		Annotations: map[string]string{
			"category-top": "3",
			"aliases":      "docker container ls, docker container list, docker container ps, docker ps",
		},
		ValidArgsFunction: cobra.NoFileCompletions,
	}

	flags := cmd.Flags()

	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Only display container IDs")
	flags.BoolVarP(&options.size, "size", "s", false, "Display total file sizes")
	flags.BoolVarP(&options.all, "all", "a", false, "Show all containers (default shows just running)")
	flags.BoolVar(&options.noTrunc, "no-trunc", false, "Don't truncate output")
	flags.BoolVarP(&options.nLatest, "latest", "l", false, "Show the latest created container (includes all states)")
	flags.IntVarP(&options.last, "last", "n", -1, "Show n last created containers (includes all states)")
	flags.StringVar(&options.format, "format", "", flagsHelper.FormatHelp)
	flags.VarP(&options.filter, "filter", "f", "Filter output based on conditions provided")

	return cmd
}

func newListCommand(dockerCLI command.Cli) *cobra.Command {
	cmd := *newPsCommand(dockerCLI)
	cmd.Aliases = []string{"ps", "list"}
	cmd.Use = "ls [OPTIONS]"
	return &cmd
}

func buildContainerListOptions(options *psOptions) (*container.ListOptions, error) {
	listOptions := &container.ListOptions{
		All:     options.all,
		Limit:   options.last,
		Size:    options.size,
		Filters: options.filter.Value(),
	}

	if options.nLatest && options.last == -1 {
		listOptions.Limit = 1
	}

	// always validate template when `--format` is used, for consistency
	if len(options.format) > 0 {
		tmpl, err := templates.Parse(options.format)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse template")
		}

		optionsProcessor := formatter.NewContainerContext()

		// This shouldn't error out but swallowing the error makes it harder
		// to track down if preProcessor issues come up.
		if err := tmpl.Execute(io.Discard, optionsProcessor); err != nil {
			return nil, errors.Wrap(err, "failed to execute template")
		}

		// if `size` was not explicitly set to false (with `--size=false`)
		// and `--quiet` is not set, request size if the template requires it
		if !options.quiet && !listOptions.Size && !options.sizeChanged {
			// The --size option isn't set, but .Size may be used in the template.
			// Parse and execute the given template to detect if the .Size field is
			// used. If it is, then automatically enable the --size option. See #24696
			//
			// Only requesting container size information when needed is an optimization,
			// because calculating the size is a costly operation.

			if _, ok := optionsProcessor.FieldsUsed["Size"]; ok {
				listOptions.Size = true
			}
		}
	}

	return listOptions, nil
}

func runPs(ctx context.Context, dockerCLI command.Cli, options *psOptions) error {
	if len(options.format) == 0 {
		// load custom psFormat from CLI config (if any)
		options.format = dockerCLI.ConfigFile().PsFormat
	} else if options.quiet {
		_, _ = dockerCLI.Err().Write([]byte("WARNING: Ignoring custom format, because both --format and --quiet are set.\n"))
	}

	listOptions, err := buildContainerListOptions(options)
	if err != nil {
		return err
	}

	containers, err := dockerCLI.Client().ContainerList(ctx, *listOptions)
	if err != nil {
		return err
	}

	containerCtx := formatter.Context{
		Output: dockerCLI.Out(),
		Format: formatter.NewContainerFormat(options.format, options.quiet, listOptions.Size),
		Trunc:  !options.noTrunc,
	}
	return formatter.ContainerWrite(containerCtx, containers)
}
