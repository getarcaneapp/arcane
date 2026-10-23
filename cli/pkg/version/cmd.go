package version

import (
	"cmp"
	"fmt"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command displays CLI and server versions.
func Command() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:          "version",
		Short:        "Show CLI and Arcane server versions",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmdutil.JSONOutputEnabled(cmd) {
				output.Header("Arcane CLI")
				output.KeyValue("Version", config.Version)
				output.KeyValue("Revision", config.Revision)
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			var result types.VersionInfo
			if err == nil {
				logger.GetLogger().Debug("Fetching server version", "endpoint", clitypes.AppVersionEndpoint)
				result, err = c.DoJSON[types.VersionInfo](cmd.Context(), http.MethodGet, clitypes.AppVersionEndpoint, nil)
			}
			if err != nil {
				if cmd.Context().Err() != nil {
					return cmd.Context().Err()
				}
				if cmdutil.JSONOutputEnabled(cmd) {
					return fmt.Errorf("failed to get server version: %w", err)
				}
				output.Header("Arcane Server")
				output.KeyValue("Status", "Unavailable")
				output.Warning("Could not get server version: %s", err)
				return nil
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result)
			}

			output.Header("Arcane Server")

			output.KeyValue("Version", cmp.Or(result.DisplayVersion, result.CurrentVersion))
			if result.Revision != "" {
				output.KeyValue("Revision", result.Revision)
			}
			if result.UpdateAvailable {
				output.Warning("Update available! New version: %s", result.NewestVersion)
				if result.ReleaseURL != "" {
					output.Info("Download at: %s", result.ReleaseURL)
				}
			}

			return nil
		},
	}
}
