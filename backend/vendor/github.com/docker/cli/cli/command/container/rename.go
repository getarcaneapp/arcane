package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type renameOptions struct {
	oldName string
	newName string
}

// NewRenameCommand creates a new cobra.Command for `docker rename`
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewRenameCommand(dockerCLI command.Cli) *cobra.Command {
	return newRenameCommand(dockerCLI)
}

func newRenameCommand(dockerCLI command.Cli) *cobra.Command {
	var opts renameOptions

	cmd := &cobra.Command{
		Use:   "rename CONTAINER NEW_NAME",
		Short: "Rename a container",
		Args:  cli.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.oldName = args[0]
			opts.newName = args[1]
			return runRename(cmd.Context(), dockerCLI, &opts)
		},
		Annotations: map[string]string{
			"aliases": "docker container rename, docker rename",
		},
		ValidArgsFunction: completion.ContainerNames(dockerCLI, true),
	}
	return cmd
}

func runRename(ctx context.Context, dockerCli command.Cli, opts *renameOptions) error {
	oldName := strings.TrimSpace(opts.oldName)
	newName := strings.TrimSpace(opts.newName)

	if oldName == "" || newName == "" {
		return errors.New("Error: Neither old nor new names may be empty")
	}

	if err := dockerCli.Client().ContainerRename(ctx, oldName, newName); err != nil {
		fmt.Fprintln(dockerCli.Err(), err)
		return errors.Errorf("Error: failed to rename container named %s", oldName)
	}
	return nil
}
