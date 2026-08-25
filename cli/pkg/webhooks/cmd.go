// Package webhooks implements the `arcane webhooks` command group for
// per-environment inbound webhooks that trigger actions.
package webhooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/webhook"
	"github.com/spf13/cobra"
)

var (
	forceFlag      bool
	jsonOutput     bool
	createName     string
	createTarget   string
	createAction   string
	createTargetID string
	updateEnabled  bool
)

// WebhooksCmd is the parent command for webhook operations.
var WebhooksCmd = &cobra.Command{
	Use:     "webhooks",
	Aliases: []string{"webhook", "wh"},
	Short:   "Manage inbound webhooks",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List webhooks",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Webhooks(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to list webhooks")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list webhooks")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.ApiResponse[[]webhook.Summary]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "NAME", "ACTION", "TARGET", "TOKEN", "ENABLED", "LAST TRIGGERED"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			enabled := "No"
			if item.Enabled {
				enabled = "Yes"
			}
			lastTriggered := "-"
			if item.LastTriggeredAt != nil {
				lastTriggered = item.LastTriggeredAt.Format(time.RFC3339)
			}
			rows[i] = []string{
				item.ID,
				item.Name,
				item.ActionType,
				webhookTarget(item),
				item.TokenPrefix,
				enabled,
				lastTriggered,
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), int64(len(result.Data)), "webhooks")
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a webhook",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		req := webhook.CreateInput{
			Name:       createName,
			TargetType: createTarget,
			ActionType: createAction,
			TargetID:   createTargetID,
		}

		resp, err := c.Post(cmd.Context(), types.Webhooks(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create webhook")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[webhook.Created]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to create webhook")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Webhook %s created successfully", result.Data.Name)
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Target Type", result.Data.TargetType)
		output.KeyValue("Action", result.Data.ActionType)
		if result.Data.TargetID != "" {
			output.KeyValue("Target ID", result.Data.TargetID)
		}
		if result.Data.Token != "" {
			output.Warning("Store this token securely — it will not be shown again")
			output.KeyValue("Token", result.Data.Token)
		}
		return nil
	},
}

var updateCmd = &cobra.Command{
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

		req := webhook.UpdateInput{Enabled: updateEnabled}
		resp, err := c.Request(cmd.Context(), http.MethodPatch, types.Webhook(c.EnvID(), args[0]), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update webhook")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to update webhook")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(map[string]bool{"enabled": updateEnabled})
		}

		state := "disabled"
		if updateEnabled {
			state = "enabled"
		}
		output.Success("Webhook %s %s", args[0], state)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <webhook-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a webhook",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete webhook %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Webhook(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete webhook")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete webhook")
		}

		output.Success("Webhook %s deleted successfully", args[0])
		return nil
	},
}

var triggerCmd = &cobra.Command{
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

		resp, err := c.Post(cmd.Context(), types.WebhookTrigger(args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to trigger webhook")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to trigger webhook")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		output.Success("Webhook trigger accepted; the action is running in the background")
		return nil
	},
}

func init() {
	WebhooksCmd.AddCommand(listCmd)
	WebhooksCmd.AddCommand(createCmd)
	WebhooksCmd.AddCommand(updateCmd)
	WebhooksCmd.AddCommand(deleteCmd)
	WebhooksCmd.AddCommand(triggerCmd)

	// List command flags
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringVar(&createName, "name", "", "Webhook name")
	createCmd.Flags().StringVar(&createTarget, "target", "", "Target type: container, project, updater, or gitops")
	createCmd.Flags().StringVar(&createAction, "action", "", "Action to run: update, start, stop, restart, redeploy, up, down, run, or sync")
	createCmd.Flags().StringVar(&createTargetID, "target-id", "", "Container ID, project ID, or GitOps sync ID to target (not needed for updater webhooks)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("target")
	_ = createCmd.MarkFlagRequired("action")

	// Update command flags
	updateCmd.Flags().BoolVar(&updateEnabled, "enabled", true, "Whether the webhook is active")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Delete without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Trigger command flags
	triggerCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func webhookTarget(item webhook.Summary) string {
	name := item.TargetName
	if name == "" {
		name = item.TargetID
	}
	if name == "" {
		return item.TargetType
	}
	return fmt.Sprintf("%s/%s", item.TargetType, name)
}
