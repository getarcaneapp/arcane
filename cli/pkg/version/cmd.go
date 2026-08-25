package version

import (
	"encoding/json"
	"fmt"

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
		resp, err := c.Get(cmd.Context(), clitypes.AppVersionEndpoint)
		if err != nil {
			return errors.WrapIf(err, "failed to get version")
		}
		defer func() { _ = resp.Body.Close() }()

		logger.GetLogger().Debug("Response received", "status", resp.Status)

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get version")
		}

		logger.GetLogger().Debug("Raw response", "body", string(body))

		var result version.Info

		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		logger.GetLogger().Debug("Parsed version data", "result", result)

		if cmdutil.JSONOutputEnabled(cmd) {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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
