package container

import (
	"context"
	"fmt"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/container"
	"github.com/spf13/cobra"
)

type commitOptions struct {
	container string
	reference string

	pause   bool
	comment string
	author  string
	changes opts.ListOpts
}

// NewCommitCommand creates a new cobra.Command for `docker commit`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewCommitCommand(dockerCLI command.Cli) *cobra.Command {
	return newCommitCommand(dockerCLI)
}

func newCommitCommand(dockerCLI command.Cli) *cobra.Command {
	var options commitOptions

	cmd := &cobra.Command{
		Use:   "commit [OPTIONS] CONTAINER [REPOSITORY[:TAG]]",
		Short: "Create a new image from a container's changes",
		Args:  cli.RequiresRangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.container = args[0]
			if len(args) > 1 {
				options.reference = args[1]
			}
			return runCommit(cmd.Context(), dockerCLI, &options)
		},
		Annotations: map[string]string{
			"aliases": "docker container commit, docker commit",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, false),
	}

	flags := cmd.Flags()
	flags.SetInterspersed(false)

	flags.BoolVarP(&options.pause, "pause", "p", true, "Pause container during commit")
	flags.StringVarP(&options.comment, "message", "m", "", "Commit message")
	flags.StringVarP(&options.author, "author", "a", "", `Author (e.g., "John Hannibal Smith <hannibal@a-team.com>")`)

	options.changes = opts.NewListOpts(nil)
	flags.VarP(&options.changes, "change", "c", "Apply Dockerfile instruction to the created image")

	return cmd
}

func runCommit(ctx context.Context, dockerCli command.Cli, options *commitOptions) error {
	response, err := dockerCli.Client().ContainerCommit(ctx, options.container, container.CommitOptions{
		Reference: options.reference,
		Comment:   options.comment,
		Author:    options.author,
		Changes:   options.changes.GetSlice(),
		Pause:     options.pause,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(dockerCli.Out(), response.ID)
	return nil
}
