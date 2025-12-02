package container

import (
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

// NewContainerCommand returns a cobra command for `container` subcommands
//
// Deprecated: Do not import commands directly. They will be removed in a future release.
func NewContainerCommand(dockerCLI command.Cli) *cobra.Command {
	return newContainerCommand(dockerCLI)
}

func newContainerCommand(dockerCLI command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container",
		Short: "Manage containers",
		Args:  cli.NoArgs,
		RunE:  command.ShowHelp(dockerCLI.Err()),
	}
	cmd.AddCommand(
		newAttachCommand(dockerCLI),
		newCommitCommand(dockerCLI),
		newCopyCommand(dockerCLI),
		newCreateCommand(dockerCLI),
		newDiffCommand(dockerCLI),
		newExecCommand(dockerCLI),
		newExportCommand(dockerCLI),
		newKillCommand(dockerCLI),
		newLogsCommand(dockerCLI),
		newPauseCommand(dockerCLI),
		newPortCommand(dockerCLI),
		newRenameCommand(dockerCLI),
		newRestartCommand(dockerCLI),
		newRemoveCommand(dockerCLI),
		newRunCommand(dockerCLI),
		newStartCommand(dockerCLI),
		newStatsCommand(dockerCLI),
		newStopCommand(dockerCLI),
		newTopCommand(dockerCLI),
		newUnpauseCommand(dockerCLI),
		newUpdateCommand(dockerCLI),
		newWaitCommand(dockerCLI),
		newListCommand(dockerCLI),
		newInspectCommand(dockerCLI),
		newPruneCommand(dockerCLI),
	)
	return cmd
}
