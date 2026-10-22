// Package webhooks implements the `arcane webhooks` command group for
// per-environment inbound webhooks that trigger actions.
package webhooks

import (
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for webhook operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "webhooks",
		Aliases: []string{"webhook", "wh"},
		Short:   "Manage inbound webhooks",
	}
	cmdutil.SetScope(cmdutil.ScopeResource, command)
	command.AddCommand(newListCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newTriggerCmd())
	return command
}

func newListCmd() *cobra.Command {
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List webhooks",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.Webhook]{
				Method:         http.MethodGet,
				Path:           clitypes.Webhooks(c.EnvID()),
				FailureMessage: "failed to list webhooks",
				Render: func(data []types.Webhook) error {
					headers := []string{"ID", "NAME", "ACTION", "TARGET", "TOKEN", "ENABLED", "LAST TRIGGERED"}
					rows := make([][]string, len(data))
					for i, item := range data {
						lastTriggered := "-"
						if item.LastTriggeredAt != nil {
							lastTriggered = item.LastTriggeredAt.Format(time.RFC3339)
						}
						target := item.TargetType
						if name := cmp.Or(item.TargetName, item.TargetID); name != "" {
							target += "/" + name
						}
						rows[i] = []string{
							item.ID,
							item.Name,
							item.ActionType,
							target,
							item.TokenPrefix,
							kit.Ternary(item.Enabled, "Yes", "No"),
							lastTriggered,
						}
					}

					return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: "webhooks", Total: int64(len(data)), Paginated: true})
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, command)
	return command
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name     string
		target   string
		action   string
		targetID string
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create a webhook",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.WebhookCreated]{
				Method:         http.MethodPost,
				Path:           clitypes.Webhooks(c.EnvID()),
				FailureMessage: "failed to create webhook",
				Body: types.Webhook{
					Name:       opts.name,
					TargetType: opts.target,
					ActionType: opts.action,
					TargetID:   opts.targetID,
				},
				Render: func(data types.WebhookCreated) error {
					output.Success("Webhook %s created successfully", data.Name)
					output.KeyValue("ID", data.ID)
					output.KeyValue("Target Type", data.TargetType)
					output.KeyValue("Action", data.ActionType)
					if data.TargetID != "" {
						output.KeyValue("Target ID", data.TargetID)
					}
					if data.Token != "" {
						output.Warning("Store this token securely — it will not be shown again")
						output.KeyValue("Token", data.Token)
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeDestination, command)
	command.Flags().StringVar(&opts.name, "name", "", "Webhook name")
	cmdutil.EnumVarP(command, command.Flags(), &opts.target, "target", "", "", "Target type", "container", "project", "updater", "gitops")
	cmdutil.EnumVarP(command, command.Flags(), &opts.action, "action", "", "", "Action to run", "update", "start", "stop", "restart", "redeploy", "up", "down", "run", "sync")
	command.Flags().StringVar(&opts.targetID, "target-id", "", "Container ID, project ID, or GitOps sync ID to target (not needed for updater webhooks)")
	_ = command.MarkFlagRequired("name")
	_ = command.MarkFlagRequired("target")
	_ = command.MarkFlagRequired("action")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		enabled bool
	}
	command := &cobra.Command{
		Use:          "update <webhook-id>",
		Short:        "Update a webhook",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("enabled") {
				return errors.New("no fields to update: specify --enabled")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[jsontext.Value]{
				Method:         http.MethodPatch,
				Path:           clitypes.Webhook(c.EnvID(), args[0]),
				Body:           map[string]any{"enabled": opts.enabled},
				FailureMessage: "failed to update webhook",
				Render: func(jsontext.Value) error {
					output.Success("Webhook %s %s", args[0], kit.Ternary(opts.enabled, "enabled", "disabled"))
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(webhookRef, command)
	command.Flags().BoolVar(&opts.enabled, "enabled", true, "Whether the webhook is active")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <webhook-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a webhook",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete webhook %s?", args[0]),
				Path:           clitypes.Webhook(c.EnvID(), args[0]),
				FailureMessage: "failed to delete webhook",
				SuccessMessage: fmt.Sprintf("Webhook %s deleted successfully", args[0]),
			})
		},
	}
	cmdutil.SetResource(webhookRef, command)
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Delete without confirmation")
	return command
}

func newTriggerCmd() *cobra.Command {
	command := &cobra.Command{
		Use:          "trigger <token>",
		Short:        "Trigger a webhook by token",
		Long:         "Trigger a webhook by its token. The token is the sole authentication mechanism; no login is required. The server accepts the trigger and runs the action in the background.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.UnauthClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[jsontext.Value]{
				Method:         http.MethodPost,
				Path:           clitypes.WebhookTrigger(args[0]),
				FailureMessage: "failed to trigger webhook",
				Render: func(jsontext.Value) error {
					output.Success("Webhook trigger accepted; the action is running in the background")
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeGlobal, command)
	return command
}

var webhookRef = cmdutil.ResourceRef[types.Webhook, types.Webhook]{
	EnvironmentScoped: true,
	Singular:          "webhook", Plural: "webhooks",
	IDHint: "the identifier", ListCmd: "arcane-cli webhooks list",
	GetPath: nil, ListPath: clitypes.Webhooks,
	Matches: func(item types.Webhook, lower, original string) bool {
		return strings.EqualFold(item.ID, original)
	},
	Label:        func(item types.Webhook) string { return item.Name + " (" + item.ID + ")" },
	Promote:      func(item types.Webhook) *types.Webhook { return &item },
	IdentifierOf: func(item types.Webhook) string { return item.ID },
}
