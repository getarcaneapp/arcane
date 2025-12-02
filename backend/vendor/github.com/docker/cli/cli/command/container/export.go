package container

import (
	"context"
	"io"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/moby/sys/atomicwriter"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	container string
	output    string
}

// NewExportCommand creates a new `docker export` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewExportCommand(dockerCLI command.Cli) *cobra.Command {
	return newExportCommand(dockerCLI)
}

func newExportCommand(dockerCLI command.Cli) *cobra.Command {
	var opts exportOptions

	cmd := &cobra.Command{
		Use:   "export [OPTIONS] CONTAINER",
		Short: "Export a container's filesystem as a tar archive",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.container = args[0]
			return runExport(cmd.Context(), dockerCLI, opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container export, docker export",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, true),
	}

	flags := cmd.Flags()

	flags.StringVarP(&opts.output, "output", "o", "", "Write to a file, instead of STDOUT")

	return cmd
}

func runExport(ctx context.Context, dockerCLI command.Cli, opts exportOptions) error {
	var output io.Writer
	if opts.output == "" {
		if dockerCLI.Out().IsTerminal() {
			return errors.New("cowardly refusing to save to a terminal. Use the -o flag or redirect")
		}
		output = dockerCLI.Out()
	} else {
		writer, err := atomicwriter.New(opts.output, 0o600)
		if err != nil {
			return errors.Wrap(err, "failed to export container")
		}
		defer writer.Close()
		output = writer
	}

	responseBody, err := dockerCLI.Client().ContainerExport(ctx, opts.container)
	if err != nil {
		return err
	}
	defer responseBody.Close()

	_, err = io.Copy(output, responseBody)
	return err
}
