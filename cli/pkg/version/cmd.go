package version

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/logger"
	"go.getarcane.app/cli/internal/output"
	clitypes "go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/version"
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

		logger.GetLogger().Debug("Sending request to ", clitypes.Endpoints.VersionEndpoint)
		resp, err := c.Get(cmd.Context(), clitypes.Endpoints.VersionEndpoint)
		if err != nil {
			return fmt.Errorf("failed to get version: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		logger.GetLogger().Debugf("Response status: %s", resp.Status)

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		logger.GetLogger().Debugf("Raw response: %s", string(body))

		var result version.Info

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		logger.GetLogger().Debugf("Parsed version data: %+v", result)

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
