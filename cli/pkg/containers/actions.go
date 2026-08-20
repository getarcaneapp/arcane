package containers

import (
	"encoding/json/v2"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/spf13/cobra"
)

var (
	killSignal string

	commitRepository string
	commitTag        string
	commitComment    string
	commitAuthor     string
	commitChanges    []string
	commitNoPause    bool

	editFile string

	autoUpdateEnabled bool
)

var containersKillCmd = &cobra.Command{
	Use:          "kill <container-id|name>",
	Short:        "Kill a container (send a signal to its main process)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		path := types.ContainerKill(c.EnvID(), resolved.ID)
		if signal := strings.TrimSpace(killSignal); signal != "" {
			path += "?signal=" + url.QueryEscape(signal)
		}
		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           path,
			FailureMessage: "failed to kill container",
			SuccessMessage: fmt.Sprintf("Container %s killed successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
	},
}

var containersPauseCmd = &cobra.Command{
	Use:          "pause <container-id|name>",
	Short:        "Pause a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}
		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.ContainerPause(c.EnvID(), resolved.ID),
			FailureMessage: "failed to pause container",
			SuccessMessage: fmt.Sprintf("Container %s paused successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
	},
}

var containersUnpauseCmd = &cobra.Command{
	Use:          "unpause <container-id|name>",
	Short:        "Unpause a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}
		return cmdutil.RunPostAction[base.MessageResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.ContainerUnpause(c.EnvID(), resolved.ID),
			FailureMessage: "failed to unpause container",
			SuccessMessage: fmt.Sprintf("Container %s unpaused successfully", containerDisplayName(resolved)),
			JSON:           jsonOutput,
		})
	},
}

var containersCommitCmd = &cobra.Command{
	Use:          "commit <container-id|name>",
	Short:        "Create an image from a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		body := container.CommitRequest{
			Repository: commitRepository,
			Tag:        commitTag,
			Comment:    commitComment,
			Author:     commitAuthor,
			Changes:    commitChanges,
			NoPause:    commitNoPause,
		}

		c.SetTimeout(30 * time.Minute)

		result, err := c.PostJSON[container.CommitResult](cmd.Context(), types.ContainerCommit(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to commit container")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Container %s committed successfully", containerDisplayName(resolved))
		output.KeyValue("Image ID", result.Data.ID)
		return nil
	},
}

var containersEditCmd = &cobra.Command{
	Use:          "edit <container-id|name>",
	Short:        "Edit a container's configuration",
	Long:         "Without --file, prints the container's editable configuration as JSON (pipe it to a file, edit it, then re-apply). With --file, applies the edit payload and recreates the container.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		if editFile == "" {
			result, err := c.GetJSON[container.EditConfig](cmd.Context(), types.ContainerEditConfig(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get container edit config")
			}

			return cmdutil.PrintJSON(result.Data)
		}

		data, err := os.ReadFile(editFile)
		if err != nil {
			return errors.WrapIff(err, "failed to read file %s", editFile)
		}
		var body container.Edit
		if err := json.Unmarshal(data, &body); err != nil {
			return errors.WrapIf(err, "failed to parse edit payload")
		}

		c.SetTimeout(30 * time.Minute)

		result, err := c.PostJSON[container.Details](cmd.Context(), types.ContainerEdit(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to edit container")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Container %s edited successfully", containerDisplayName(resolved))
		output.KeyValue("New ID", result.Data.ID)
		return nil
	},
}

var containersAutoUpdateCmd = &cobra.Command{
	Use:          "autoupdate <container-id|name>",
	Short:        "Enable or disable auto-update for a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resolved, _, err := containerRef.Resolve(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		body := struct {
			Enabled bool `json:"enabled"`
		}{Enabled: autoUpdateEnabled}

		result, err := c.PutJSON[base.MessageResponse](cmd.Context(), types.ContainerAutoUpdate(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to set container auto-update")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		state := "disabled"
		if autoUpdateEnabled {
			state = "enabled"
		}
		output.Success("Auto-update %s for container %s", state, containerDisplayName(resolved))
		return nil
	},
}

func init() {
	ContainersCmd.AddCommand(containersKillCmd)
	ContainersCmd.AddCommand(containersPauseCmd)
	ContainersCmd.AddCommand(containersUnpauseCmd)
	ContainersCmd.AddCommand(containersCommitCmd)
	ContainersCmd.AddCommand(containersEditCmd)
	ContainersCmd.AddCommand(containersAutoUpdateCmd)

	containersKillCmd.Flags().StringVar(&killSignal, "signal", "", "Signal to send (for example SIGTERM, SIGKILL); defaults to SIGKILL")

	containersCommitCmd.Flags().StringVar(&commitRepository, "repository", "", "Target image repository")
	containersCommitCmd.Flags().StringVar(&commitTag, "tag", "", "Target image tag")
	containersCommitCmd.Flags().StringVar(&commitComment, "comment", "", "Commit comment")
	containersCommitCmd.Flags().StringVar(&commitAuthor, "author", "", "Commit author")
	containersCommitCmd.Flags().StringArrayVar(&commitChanges, "change", nil, "Dockerfile instruction to apply during commit (repeatable)")
	containersCommitCmd.Flags().BoolVar(&commitNoPause, "no-pause", false, "Do not pause the container during commit")

	containersEditCmd.Flags().StringVarP(&editFile, "file", "f", "", "JSON edit payload to apply (omit to print the current editable configuration)")

	containersAutoUpdateCmd.Flags().BoolVar(&autoUpdateEnabled, "enabled", false, "Whether auto-update is enabled for this container")
	_ = containersAutoUpdateCmd.MarkFlagRequired("enabled")

	containersKillCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersPauseCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersUnpauseCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersCommitCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersEditCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersAutoUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
