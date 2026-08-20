package system

import (
	"fmt"
	"net/http"
	"strconv"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/dockerinfo"
	"github.com/getarcaneapp/arcane/types/v2/system"
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

		req := system.PruneAllRequest{
			Containers: &system.PruneContainersOptions{Mode: system.PruneContainerModeStopped},
			Images:     &system.PruneImagesOptions{Mode: system.PruneImageModeDangling},
			Networks:   &system.PruneNetworksOptions{Mode: system.PruneNetworkModeUnused},
		}

		result, err := c.PostJSON[system.PruneAllResult](cmd.Context(), types.Endpoints.SystemPrune(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to prune")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("System Prune Results")
		output.KeyValue("Space Reclaimed", output.UnsignedBytes(result.Data.SpaceReclaimed))
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

		// This endpoint returns dockerinfo.Info directly, not wrapped in an
		// ApiResponse envelope.
		result, err := c.DoJSON[dockerinfo.Info](cmd.Context(), http.MethodGet, types.Endpoints.SystemDockerInfo(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to get docker info")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		output.Header("Docker Info")
		output.KeyValue("API Version", result.APIVersion)
		output.KeyValue("OS", result.Os)
		output.KeyValue("Architecture", result.Arch)
		output.KeyValue("Go Version", result.GoVersion)
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
			return errors.WrapIf(err, "failed to start all containers")
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to start all containers")
		}

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
			return errors.WrapIf(err, "failed to stop all containers")
		}
		defer func() { _ = resp.Body.Close() }()

		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to stop all containers")
		}

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

		return cmdutil.RunPostAction[container.ActionResult](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.SystemStartStopped(c.EnvID()),
			FailureMessage: "failed to start stopped containers",
			SuccessMessage: "Started all stopped containers",
			JSON:           jsonOutput,
		})
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

		// This endpoint returns its payload directly, not wrapped in an
		// ApiResponse envelope.
		type convertResult struct {
			Success       bool   `json:"success"`
			DockerCompose string `json:"dockerCompose"`
			EnvVars       string `json:"envVars"`
			ServiceName   string `json:"serviceName"`
		}
		req := map[string]string{"dockerRunCommand": args[0]}
		result, err := c.DoJSON[convertResult](cmd.Context(), http.MethodPost, types.Endpoints.SystemConvert(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to convert command")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(map[string]string{
				"dockerCompose": result.DockerCompose,
				"envVars":       result.EnvVars,
				"serviceName":   result.ServiceName,
			})
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

		// The upgrade payload type is backend-internal, so the envelope stays
		// untyped to keep --json output complete.
		return cmdutil.RunPostAction[any](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.SystemUpgrade(c.EnvID()),
			FailureMessage: "failed to upgrade system",
			SuccessMessage: "System upgrade initiated",
			JSON:           jsonOutput,
		})
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

		// This endpoint returns its payload directly, not wrapped in an
		// ApiResponse envelope.
		type upgradeCheckResult struct {
			CanUpgrade bool   `json:"canUpgrade"`
			Error      bool   `json:"error"`
			Message    string `json:"message"`
		}
		result, err := c.DoJSON[upgradeCheckResult](cmd.Context(), http.MethodGet, types.Endpoints.SystemUpgradeCheck(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to check for upgrades")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		output.Header("Upgrade Check")
		output.KeyValue("Can Upgrade", strconv.FormatBool(result.CanUpgrade))
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
