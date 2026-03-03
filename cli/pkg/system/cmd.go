package system

import (
	"encoding/json"
	"fmt"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/dockerinfo"
	"github.com/getarcaneapp/arcane/types/system"
	"github.com/spf13/cobra"
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

var startStoppedCmd = &cobra.Command{
	Use:          "start-stopped",
	Short:        "Start all stopped containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemStartStopped(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to start stopped containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to start stopped containers: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Started all stopped containers")
		return nil
	},
}

var convertCmd = &cobra.Command{
	Use:          "convert <docker-run-command>",
	Short:        "Convert docker run command to compose",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		req := map[string]string{"dockerRunCommand": args[0]}
		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemConvert(c.EnvID()), req)
		if err != nil {
			return fmt.Errorf("failed to convert command: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to convert command: %w", err)
		}

		var result struct {
			Success       bool   `json:"success"`
			DockerCompose string `json:"dockerCompose"`
			EnvVars       string `json:"envVars"`
			ServiceName   string `json:"serviceName"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			out := map[string]string{
				"dockerCompose": result.DockerCompose,
				"envVars":       result.EnvVars,
				"serviceName":   result.ServiceName,
			}
			resultBytes, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Conversion Result")
		if result.ServiceName != "" {
			fmt.Printf("Service: %s\n\n", result.ServiceName)
		}
		if result.DockerCompose != "" {
			fmt.Println("Docker Compose:")
			fmt.Println(result.DockerCompose)
		}
		if result.EnvVars != "" {
			fmt.Println("Environment Variables:")
			fmt.Println(result.EnvVars)
		}
		return nil
	},
}

var forceFlag bool

var upgradeCmd = &cobra.Command{
	Use:          "upgrade",
	Short:        "Trigger system upgrade",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to upgrade the system?")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.SystemUpgrade(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to upgrade system: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to upgrade system: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("System upgrade initiated")
		return nil
	},
}

var upgradeCheckCmd = &cobra.Command{
	Use:          "upgrade-check",
	Short:        "Check for available upgrades",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.SystemUpgradeCheck(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to check for upgrades: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to check for upgrades: %w", err)
		}

		var result struct {
			CanUpgrade bool   `json:"canUpgrade"`
			Error      bool   `json:"error"`
			Message    string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Upgrade Check")
		output.KeyValue("Can Upgrade", fmt.Sprintf("%t", result.CanUpgrade))
		output.KeyValue("Message", result.Message)
		if result.Error {
			output.KeyValue("Error", "true")
		}
		return nil
	},
}

func init() {
	SystemCmd.AddCommand(pruneCmd)
	SystemCmd.AddCommand(dockerInfoCmd)
	SystemCmd.AddCommand(containersStartAllCmd)
	SystemCmd.AddCommand(containersStopAllCmd)
	SystemCmd.AddCommand(startStoppedCmd)
	SystemCmd.AddCommand(convertCmd)
	SystemCmd.AddCommand(upgradeCmd)
	SystemCmd.AddCommand(upgradeCheckCmd)

	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	dockerInfoCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStopAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	startStoppedCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	convertCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	upgradeCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip confirmation")
	upgradeCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	upgradeCheckCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
