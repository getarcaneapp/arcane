package image

import (
	"context"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/spf13/cobra"
)

type tagOptions struct {
	image string
	name  string
}

// NewTagCommand creates a new `docker tag` command
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewTagCommand(dockerCLI command.Cli) *cobra.Command {
	return newTagCommand(dockerCLI)
}

// newTagCommand creates a new `docker tag` command
func newTagCommand(dockerCli command.Cli) *cobra.Command {
	var opts tagOptions

	cmd := &cobra.Command{
		Use:   "tag SOURCE_IMAGE[:TAG] TARGET_IMAGE[:TAG]",
		Short: "Create a tag TARGET_IMAGE that refers to SOURCE_IMAGE",
		Args:  cli.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.image = args[0]
			opts.name = args[1]
			return runTag(cmd.Context(), dockerCli, opts)
		},
		Annotations: map[string]string{
			"aliases": "docker image tag, docker tag",
		},
		ValidArgsFunction: completion.ImageNames(dockerCli, 2),
	}

	flags := cmd.Flags()
	flags.SetInterspersed(false)

	return cmd
}

func runTag(ctx context.Context, dockerCli command.Cli, opts tagOptions) error {
	return dockerCli.Client().ImageTag(ctx, opts.image, opts.name)
}
