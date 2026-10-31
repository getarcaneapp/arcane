package settings

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for settings operations
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"setting"},
		Short:   "Manage settings",
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	cmd.AddCommand(newListCmd(), newUpdateCmd(), newSetCmd(), newPublicCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List environment settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsList(cmd, settingsListConfig{
				endpoint:       clitypes.Settings,
				failureMessage: "failed to get settings",
				totalLabel:     "settings",
			})
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		file string
	}
	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update environment settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(opts.file)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.file, err)
			}

			var req types.SettingsUpdate
			if unmarshalErr := json.Unmarshal(data, &req); unmarshalErr != nil {
				return fmt.Errorf("failed to parse settings file: %w", unmarshalErr)
			}

			return runSettingsUpdate(cmd, req)
		},
	}
	cmdutil.SetScope(cmdutil.ScopeAll, cmd)
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Settings JSON file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Set one environment setting",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsUpdate(cmd, types.SettingsUpdate{args[0]: args[1]})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeAll, cmd)
	return cmd
}

func runSettingsUpdate(cmd *cobra.Command, update types.SettingsUpdate) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	resp, err := c.Request(cmd.Context(), http.MethodPatch, clitypes.Settings(c.EnvID()), update)
	if err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if statusErr, ok := errors.AsType[*cmdutil.HTTPStatusError](cmdutil.EnsureSuccessStatus(resp)); ok {
		var problem struct {
			Detail string `json:"detail"`
		}
		if statusErr.StatusCode == http.StatusBadRequest && json.Unmarshal([]byte(statusErr.Body), &problem) == nil && problem.Detail != "" {
			return errors.New(problem.Detail)
		}
		return fmt.Errorf("failed to update settings: %w", statusErr)
	}

	var snapshot types.SettingsSnapshot
	if decodeErr := cmdutil.DecodeJSON(resp, &snapshot); decodeErr != nil {
		return fmt.Errorf("failed to update settings: %w", decodeErr)
	}
	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintJSON(snapshot)
	}
	output.Success("Settings updated successfully")
	return nil
}

func newPublicCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "public",
		Short:        "List public settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsList(cmd, settingsListConfig{
				endpoint:       clitypes.SettingsPublic,
				failureMessage: "failed to get public settings",
				totalLabel:     "public settings",
			})
		},
	}
}

type settingsListConfig struct {
	endpoint       func(string) string
	failureMessage string
	totalLabel     string
}

func runSettingsList(cmd *cobra.Command, cfg settingsListConfig) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	result, err := c.DoJSON[types.SettingsSnapshot](cmd.Context(), http.MethodGet, cfg.endpoint(c.EnvID()), nil)
	if err != nil {
		return fmt.Errorf("%s: %w", cfg.failureMessage, err)
	}

	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintJSON(result)
	}

	headers := []string{"KEY", "TYPE", "VALUE", "SOURCE", "LOCKED"}
	rows := make([][]string, len(result.Settings))
	for i, s := range result.Settings {
		rows[i] = []string{s.Key, s.Type, s.Value, s.Source, kit.Ternary(s.Locked, cmp.Or(s.LockReason, "yes"), "no")}
	}

	return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: cfg.totalLabel})
}
