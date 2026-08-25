package containers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
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
		return runContainerPostAction[base.MessageResponse](cmd, args[0], containerPostActionConfig[base.MessageResponse]{
			endpoint: func(envID, containerID string) string {
				path := types.ContainerKill(envID, containerID)
				if signal := strings.TrimSpace(killSignal); signal != "" {
					path += "?signal=" + url.QueryEscape(signal)
				}
				return path
			},
			failureMessage: "failed to kill container",
			successVerb:    "killed",
		})
	},
}

var containersPauseCmd = &cobra.Command{
	Use:          "pause <container-id|name>",
	Short:        "Pause a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runContainerPostAction[base.MessageResponse](cmd, args[0], containerPostActionConfig[base.MessageResponse]{
			endpoint:       types.ContainerPause,
			failureMessage: "failed to pause container",
			successVerb:    "paused",
		})
	},
}

var containersUnpauseCmd = &cobra.Command{
	Use:          "unpause <container-id|name>",
	Short:        "Unpause a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runContainerPostAction[base.MessageResponse](cmd, args[0], containerPostActionConfig[base.MessageResponse]{
			endpoint:       types.ContainerUnpause,
			failureMessage: "failed to unpause container",
			successVerb:    "unpaused",
		})
	},
}

var containersCommitCmd = &cobra.Command{
	Use:          "commit <container-id|name>",
	Short:        "Create an image from a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
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

		resp, err := c.Post(cmd.Context(), types.ContainerCommit(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to commit container")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to commit container")
		}

		var result base.ApiResponse[container.CommitResult]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		if editFile == "" {
			resp, err := c.Get(cmd.Context(), types.ContainerEditConfig(c.EnvID(), resolved.ID))
			if err != nil {
				return errors.WrapIf(err, "failed to get container edit config")
			}
			defer func() { _ = resp.Body.Close() }()
			if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
				return errors.WrapIf(err, "failed to get container edit config")
			}

			var result base.ApiResponse[container.EditConfig]
			if err := cmdutil.DecodeJSON(resp, &result); err != nil {
				return err
			}

			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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

		resp, err := c.Post(cmd.Context(), types.ContainerEdit(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to edit container")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to edit container")
		}

		var result base.ApiResponse[container.Details]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolved, _, err := resolveContainer(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		body := struct {
			Enabled bool `json:"enabled"`
		}{Enabled: autoUpdateEnabled}

		resp, err := c.Put(cmd.Context(), types.ContainerAutoUpdate(c.EnvID(), resolved.ID), body)
		if err != nil {
			return errors.WrapIf(err, "failed to set container auto-update")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to set container auto-update")
		}

		var result base.ApiResponse[base.MessageResponse]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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
