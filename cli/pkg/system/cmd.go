package system

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

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

		result, err := c.PostJSON[system.PruneAllResult](cmd.Context(), types.SystemPrune(c.EnvID()), req)
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
	Use:          "info",
	Aliases:      []string{"docker-info"},
	Short:        "Get Docker daemon information",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// This endpoint returns dockerinfo.Info directly, not wrapped in an
		// ApiResponse envelope.
		result, err := c.DoJSON[dockerinfo.Info](cmd.Context(), http.MethodGet, types.SystemDockerInfo(c.EnvID()), nil)
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

var systemContainersCmd = &cobra.Command{
	Use:   "containers",
	Short: "Bulk container operations",
}

var startStoppedOnlyFlag bool

var containersStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start all containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.SystemContainersStartAll(c.EnvID())
		successMessage := "Started all containers"
		if startStoppedOnlyFlag {
			path = types.SystemStartStopped(c.EnvID())
			successMessage = "Started all stopped containers"
		}

		return cmdutil.RunPostAction[container.ActionResult](cmd, c, cmdutil.PostActionSpec{
			Path:           path,
			FailureMessage: "failed to start containers",
			SuccessMessage: successMessage,
			JSON:           jsonOutput,
		})
	},
}

var containersStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop all containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		return cmdutil.RunPostAction[container.ActionResult](cmd, c, cmdutil.PostActionSpec{
			Path:           types.SystemContainersStopAll(c.EnvID()),
			FailureMessage: "failed to stop all containers",
			SuccessMessage: "Stopped all containers",
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
		result, err := c.DoJSON[convertResult](cmd.Context(), http.MethodPost, types.SystemConvert(c.EnvID()), req)
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
		if upgradeAllStatusFlag && !upgradeAllFlag {
			return errors.New("--status requires --all")
		}
		if upgradeCheckFlag && upgradeAllFlag {
			return errors.New("--check cannot be combined with --all")
		}
		if upgradeAllFlag {
			return runUpgradeAllInternal(cmd)
		}
		if upgradeCheckFlag {
			return runUpgradeCheckInternal(cmd)
		}

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
			Path:           types.SystemUpgrade(c.EnvID()),
			FailureMessage: "failed to upgrade system",
			SuccessMessage: "System upgrade initiated",
			JSON:           jsonOutput,
		})
	},
}

var upgradeCheckFlag bool

func runUpgradeCheckInternal(cmd *cobra.Command) error {
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
	result, err := c.DoJSON[upgradeCheckResult](cmd.Context(), http.MethodGet, types.SystemUpgradeCheck(c.EnvID()), nil)
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
}

// environmentUpdateJob mirrors the backend's fleet-wide update job payload
// (backend/internal/system.EnvironmentUpdateJob), which is not exported in the
// shared types module.
type environmentUpdateJob struct {
	ID                    string `json:"id"`
	Status                string `json:"status"`
	Username              string `json:"username"`
	ManagerVersionAtStart string `json:"managerVersionAtStart"`
	ManagerTargetVersion  string `json:"managerTargetVersion"`
	Results               []struct {
		EnvironmentID   string `json:"environmentId"`
		EnvironmentName string `json:"environmentName"`
		Status          string `json:"status"`
		FromVersion     string `json:"fromVersion,omitempty"`
		ToVersion       string `json:"toVersion,omitempty"`
		Error           string `json:"error,omitempty"`
	} `json:"results,omitempty"`
	Error       *string    `json:"error,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

func printUpdateAllJobInternal(job environmentUpdateJob) {
	output.KeyValue("Status", job.Status)
	if job.Username != "" {
		output.KeyValue("Triggered By", job.Username)
	}
	if job.ManagerVersionAtStart != "" {
		output.KeyValue("Manager Version", job.ManagerVersionAtStart)
	}
	if job.ManagerTargetVersion != "" {
		output.KeyValue("Target Version", job.ManagerTargetVersion)
	}
	if job.Error != nil && *job.Error != "" {
		output.KeyValue("Error", *job.Error)
	}
	if job.CompletedAt != nil {
		output.KeyValue("Completed At", job.CompletedAt.Format(time.RFC3339))
	}
	if len(job.Results) == 0 {
		return
	}

	headers := []string{"ENVIRONMENT", "STATUS", "FROM", "TO", "ERROR"}
	rows := make([][]string, len(job.Results))
	for i, res := range job.Results {
		name := res.EnvironmentName
		if name == "" {
			name = res.EnvironmentID
		}
		resErr := res.Error
		if resErr == "" {
			resErr = "-"
		}
		rows[i] = []string{name, res.Status, res.FromVersion, res.ToVersion, resErr}
	}
	fmt.Println()
	output.Table(headers, rows)
}

var (
	upgradeAllFlag       bool
	upgradeAllStatusFlag bool
)

func runUpgradeAllInternal(cmd *cobra.Command) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}

	if upgradeAllStatusFlag {
		result, err := c.GetJSON[environmentUpdateJob](cmd.Context(), types.SystemUpgradeAllStatus(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get update-all status")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Update-All Status")
		printUpdateAllJobInternal(result.Data)
		return nil
	}

	if !forceFlag {
		confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to update all environments?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled")
			return nil
		}
	}

	result, err := c.PostJSON[environmentUpdateJob](cmd.Context(), types.SystemUpgradeAll(c.EnvID()), nil)
	if err != nil {
		return errors.WrapIf(err, "failed to trigger update-all")
	}

	if jsonOutput {
		return cmdutil.PrintJSON(result.Data)
	}

	output.Success("Update-all started")
	printUpdateAllJobInternal(result.Data)
	return nil
}

func init() {
	SystemCmd.AddCommand(pruneCmd)
	SystemCmd.AddCommand(dockerInfoCmd)
	SystemCmd.AddCommand(systemContainersCmd)
	SystemCmd.AddCommand(convertCmd)
	SystemCmd.AddCommand(upgradeCmd)
	systemContainersCmd.AddCommand(containersStartCmd)
	systemContainersCmd.AddCommand(containersStopCmd)

	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	dockerInfoCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartCmd.Flags().BoolVar(&startStoppedOnlyFlag, "stopped", false, "Only start containers that are currently stopped")
	containersStopCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	convertCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	upgradeCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Skip confirmation")
	upgradeCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	upgradeCmd.Flags().BoolVar(&upgradeAllFlag, "all", false, "Update all environments with available updates")
	upgradeCmd.Flags().BoolVar(&upgradeAllStatusFlag, "status", false, "Show the status of the latest update-all job (requires --all)")
	upgradeCmd.Flags().BoolVar(&upgradeCheckFlag, "check", false, "Only check whether an upgrade is available")
}
