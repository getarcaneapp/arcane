package settings

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for settings operations
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"setting"},
		Short:   "Manage settings",
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	cmd.AddCommand(newListCmd(), newUpdateCmd(), newPublicCmd())
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
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			data, err := os.ReadFile(opts.file)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.file, err)
			}

			var req types.SettingsUpdate
			if unmarshalErr := json.Unmarshal(data, &req); unmarshalErr != nil {
				return fmt.Errorf("failed to parse settings file: %w", unmarshalErr)
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.SettingDto]{
				Method:         http.MethodPatch,
				Path:           clitypes.Settings(c.EnvID()),
				Body:           req,
				FailureMessage: "failed to update settings",
				Render: func([]types.SettingDto) error {
					output.Success("Settings updated successfully")
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "Settings JSON file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
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

	result, err := c.DoJSON[[]types.PublicSetting](cmd.Context(), http.MethodGet, cfg.endpoint(c.EnvID()), nil)
	if err != nil {
		return fmt.Errorf("%s: %w", cfg.failureMessage, err)
	}

	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintJSON(result)
	}

	headers := []string{"KEY", "TYPE", "VALUE"}
	rows := make([][]string, len(result))
	for i, s := range result {
		rows[i] = []string{s.Key, s.Type, s.Value}
	}

	return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: cfg.totalLabel})
}
