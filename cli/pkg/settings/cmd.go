package settings

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/spf13/cobra"

	"kit.getarcane.app"
	"kit.getarcane.app/acfs"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

type (
	settingsListConfig struct {
		endpoint       func(*cobra.Command, string) (string, error)
		failureMessage string
		totalLabel     string
	}
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"setting"},
		Short:   "Manage settings",
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	scoped := []*cobra.Command{newListCmd(), newUpdateCmd(), newSetCmd()}
	for _, child := range scoped {
		child.Flags().String("scope", "environment", "Settings scope: environment, manager (app-wide), or authentication (global admin only)")
	}
	cmd.AddCommand(append(scoped, newPublicCmd())...)
	return cmd
}

func settingsEndpoint(cmd *cobra.Command, envID string) (string, error) {
	scope, err := cmd.Flags().GetString("scope")
	if err != nil {
		return "", err
	}
	switch scope {
	case "environment":
		return clitypes.Settings(envID), nil
	case "manager":
		return clitypes.ManagerSettings(), nil
	case "authentication":
		return clitypes.AuthenticationSettings(), nil
	default:
		return "", fmt.Errorf("invalid scope %q: must be environment, manager, or authentication", scope)
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsList(cmd, settingsListConfig{
				endpoint:       settingsEndpoint,
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
		Short:        "Update settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, err := filepath.Abs(opts.file)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", opts.file, err)
			}
			data, err := acfs.Host{Special: true}.ReadFile(cmd.Context(), filePath)
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
		Short:        "Set one setting",
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

	endpoint, err := settingsEndpoint(cmd, c.EnvID())
	if err != nil {
		return err
	}
	resp, err := c.Request(cmd.Context(), http.MethodPatch, endpoint, update)
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
				endpoint: func(_ *cobra.Command, envID string) (string, error) {
					return clitypes.SettingsPublic(envID), nil
				},
				failureMessage: "failed to get public settings",
				totalLabel:     "public settings",
			})
		},
	}
}

func runSettingsList(cmd *cobra.Command, cfg settingsListConfig) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	endpoint, err := cfg.endpoint(cmd, c.EnvID())
	if err != nil {
		return err
	}
	result, err := c.DoJSON[types.SettingsSnapshot](cmd.Context(), http.MethodGet, endpoint, nil)
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
