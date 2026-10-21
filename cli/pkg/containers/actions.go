package containers

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newKillCmd() *cobra.Command {
	var opts struct {
		signal string
	}
	command := &cobra.Command{
		Use:          "kill <container-id|name>",
		Short:        "Kill a container (send a signal to its main process)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			path := clitypes.ContainerKill(c.EnvID(), resolved.ID)
			if signal := strings.TrimSpace(opts.signal); signal != "" {
				path += "?signal=" + url.QueryEscape(signal)
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           path,
				FailureMessage: "failed to kill container",
				Render: func(types.MessageResponse) error {
					output.Success("Container %s killed successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.signal, "signal", "", "Signal to send (for example SIGTERM, SIGKILL); defaults to SIGKILL")
	return command
}

func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "pause <container-id|name>",
		Short:        "Pause a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerPause(c.EnvID(), resolved.ID),
				FailureMessage: "failed to pause container",
				Render: func(types.MessageResponse) error {
					output.Success("Container %s paused successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
}

func newUnpauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unpause <container-id|name>",
		Short:        "Unpause a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerUnpause(c.EnvID(), resolved.ID),
				FailureMessage: "failed to unpause container",
				Render: func(types.MessageResponse) error {
					output.Success("Container %s unpaused successfully", containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
}

func newCommitCmd() *cobra.Command {
	var opts struct {
		repository string
		tag        string
		comment    string
		author     string
		changes    []string
		noPause    bool
	}
	command := &cobra.Command{
		Use:          "commit <container-id|name>",
		Short:        "Create an image from a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			body := types.ContainerCommitRequest{
				Repository: opts.repository,
				Tag:        opts.tag,
				Comment:    opts.comment,
				Author:     opts.author,
				Changes:    opts.changes,
				NoPause:    opts.noPause,
			}

			c.SetTimeout(30 * time.Minute)

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerCommitResult]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerCommit(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to commit container",
				Render: func(data types.ContainerCommitResult) error {
					output.Success("Container %s committed successfully", containerDisplayName(resolved))
					output.KeyValue("Image ID", data.ID)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.repository, "repository", "", "Target image repository")
	command.Flags().StringVar(&opts.tag, "tag", "", "Target image tag")
	command.Flags().StringVar(&opts.comment, "comment", "", "Commit comment")
	command.Flags().StringVar(&opts.author, "author", "", "Commit author")
	command.Flags().StringArrayVar(&opts.changes, "change", nil, "Dockerfile instruction to apply during commit (repeatable)")
	command.Flags().BoolVar(&opts.noPause, "no-pause", false, "Do not pause the container during commit")
	return command
}

func newEditCmd() *cobra.Command {
	var opts struct {
		file string
	}
	command := &cobra.Command{
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

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			if opts.file == "" {
				return cmdutil.Run(cmd, c, cmdutil.Spec[jsontext.Value]{
					Method:         http.MethodGet,
					Path:           clitypes.ContainerEditConfig(c.EnvID(), resolved.ID),
					FailureMessage: "failed to get container edit config",
					Render:         func(data jsontext.Value) error { return cmdutil.PrintRawJSON(data) },
				})
			}

			data, err := os.ReadFile(opts.file)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.file, err)
			}
			var body types.ContainerEdit
			if unmarshalErr := json.Unmarshal(data, &body); unmarshalErr != nil {
				return fmt.Errorf("failed to parse edit payload: %w", unmarshalErr)
			}

			c.SetTimeout(30 * time.Minute)

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerDetails]{
				Method:         http.MethodPost,
				Path:           clitypes.ContainerEdit(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to edit container",
				Render: func(data types.ContainerDetails) error {
					output.Success("Container %s edited successfully", containerDisplayName(resolved))
					output.KeyValue("New ID", data.ID)
					return nil
				},
			})
		},
	}
	command.Flags().StringVarP(&opts.file, "file", "f", "", "JSON edit payload to apply (omit to print the current editable configuration)")
	return command
}

func newAutoUpdateCmd() *cobra.Command {
	var opts struct {
		enabled bool
	}
	command := &cobra.Command{
		Use:          "auto-update <container-id|name>",
		Short:        "Enable or disable auto-update for a container",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resolved, _, err := ContainerRef.Resolve(cmd.Context(), c, args[0], false)
			if err != nil {
				return err
			}

			body := types.ContainerSetAutoUpdateRequest{Enabled: opts.enabled}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.MessageResponse]{
				Method:         http.MethodPut,
				Path:           clitypes.ContainerAutoUpdate(c.EnvID(), resolved.ID),
				Body:           body,
				FailureMessage: "failed to set container auto-update",
				Render: func(data types.MessageResponse) error {
					output.Success("Auto-update %s for container %s", kit.Ternary(opts.enabled, "enabled", "disabled"), containerDisplayName(resolved))
					return nil
				},
			})
		},
	}
	command.Flags().BoolVar(&opts.enabled, "enabled", false, "Whether auto-update is enabled for this container")
	_ = command.MarkFlagRequired("enabled")
	return command
}
