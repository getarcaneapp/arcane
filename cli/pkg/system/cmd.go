package system

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/dockerinfo"
	"go.getarcane.app/types/system"
)

var jsonOutput bool

// SystemCmd is the parent command for system operations
var SystemCmd = &cobra.Command{
	Use:     "system",
	Aliases: []string{"sys"},
	Short:   "System operations",
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Prune all unused resources",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemPrune(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to prune: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[system.PruneAllResult]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("System Prune Results")
		output.KeyValue("Space Reclaimed", result.Data.SpaceReclaimed)
		return nil
	},
}

var dockerInfoCmd = &cobra.Command{
	Use:          "docker-info",
	Short:        "Get Docker daemon information",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.SystemDockerInfo(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get docker info: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[dockerinfo.Info]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Docker Info")
		output.KeyValue("API Version", result.Data.APIVersion)
		output.KeyValue("OS", result.Data.Os)
		output.KeyValue("Architecture", result.Data.Arch)
		output.KeyValue("Go Version", result.Data.GoVersion)
		return nil
	},
}

var containersStartAllCmd = &cobra.Command{
	Use:          "containers-start-all",
	Short:        "Start all containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemContainersStartAll(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to start all containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Started all containers")
		return nil
	},
}

var containersStopAllCmd = &cobra.Command{
	Use:          "containers-stop-all",
	Short:        "Stop all containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemContainersStopAll(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to stop all containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Stopped all containers")
		return nil
	},
}

func init() {
	SystemCmd.AddCommand(pruneCmd)
	SystemCmd.AddCommand(dockerInfoCmd)
	SystemCmd.AddCommand(containersStartAllCmd)
	SystemCmd.AddCommand(containersStopAllCmd)

	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	dockerInfoCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStopAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
