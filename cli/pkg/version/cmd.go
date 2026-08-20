package version

import (
	"net/http"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/logger"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"github.com/spf13/cobra"
)

// VersionCmd gets the server version
var VersionCmd = &cobra.Command{
	Use:          "version",
	Short:        "Get the Arcane server version",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.GetLogger().Debug("Fetching server version")

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// /api/version returns the trimmed version.Check payload, which carries
		// neither displayVersion nor revision. /api/app-version returns the full
		// version.Info, including the update-check fields printed below.
		logger.GetLogger().Debug("Sending request", "endpoint", clitypes.AppVersionEndpoint)
		result, err := c.DoJSON[version.Info](cmd.Context(), http.MethodGet, clitypes.AppVersionEndpoint, nil)
		if err != nil {
			return errors.WrapIf(err, "failed to get version")
		}

		logger.GetLogger().Debug("Parsed version data", "result", result)

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result)
		}

		output.Header("Arcane Environment Details: \n")

		output.KeyValue("Version", result.DisplayVersion)
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
