package system

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "system",
		Aliases: []string{"sys"},
		Short:   "System operations",
	}
	cmd.AddCommand(
		newPruneCmd(),
		newDockerInfoCmd(),
		newSystemContainersCmd(),
		newConvertCmd(),
		newUpgradeCmd(),
	)
	return cmd
}

func newPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "prune",
		Short:        "Prune all unused resources",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, false, "Are you sure you want to prune all unused resources?")
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PruneAllResult]{
				Method:         http.MethodPost,
				Path:           clitypes.SystemPrune(c.EnvID()),
				FailureMessage: "failed to prune",
				Body: types.PruneAllRequest{
					Containers: &types.PruneContainersOptions{Mode: types.PruneContainerModeStopped},
					Images:     &types.PruneImagesOptions{Mode: types.PruneImageModeDangling},
					Networks:   &types.PruneNetworksOptions{Mode: types.PruneNetworkModeUnused},
				},
				Render: func(data types.PruneAllResult) error {
					output.Header("System Prune Results")
					output.KeyValue("Space Reclaimed", output.UnsignedBytes(data.SpaceReclaimed))
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newDockerInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "info",
		Aliases:      []string{"docker-info"},
		Short:        "Get Docker daemon information",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// This endpoint returns dockerinfo.Info directly, not wrapped in an
			// ApiResponse envelope.
			body, err := c.DoRaw(cmd.Context(), http.MethodGet, clitypes.SystemDockerInfo(c.EnvID()), nil)
			if err != nil {
				return fmt.Errorf("failed to get docker info: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(body)
			}
			var result types.DockerInfo
			if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
				return fmt.Errorf("failed to get docker info: failed to decode response: %w", decodeErr)
			}

			output.Header("Docker Info")
			output.KeyValue("API Version", result.APIVersion)
			output.KeyValue("OS", result.Os)
			output.KeyValue("Architecture", result.Arch)
			output.KeyValue("Go Version", result.GoVersion)
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newSystemContainersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "containers",
		Short: "Bulk container operations",
	}
	cmd.AddCommand(
		newContainersStartCmd(),
		newContainersStopCmd(),
	)
	return cmd
}

func newContainersStartCmd() *cobra.Command {
	var opts struct {
		startStoppedOnlyFlag bool
	}
	cmd := &cobra.Command{
		Use:          "start",
		Short:        "Start all containers",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerActionResult]{
				Method:         http.MethodPost,
				Path:           clitypes.SystemContainersAction(c.EnvID(), kit.Ternary(opts.startStoppedOnlyFlag, "startstopped", "startall")),
				FailureMessage: "failed to start containers",
				Render: func(types.ContainerActionResult) error {
					output.Success("%s", kit.Ternary(opts.startStoppedOnlyFlag, "Started all stopped containers", "Started all containers"))
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVar(&opts.startStoppedOnlyFlag, "stopped", false, "Only start containers that are currently stopped")
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newContainersStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "stop",
		Short:        "Stop all containers",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ContainerActionResult]{
				Method:         http.MethodPost,
				Path:           clitypes.SystemContainersAction(c.EnvID(), "stopall"),
				FailureMessage: "failed to stop all containers",
				Render:         func(types.ContainerActionResult) error { output.Success("%s", "Stopped all containers"); return nil },
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newConvertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "convert <docker-run-command>",
		Short:        "Convert docker run command to compose",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Post(cmd.Context(), clitypes.SystemConvert(c.EnvID()), types.ConvertDockerRunRequest{DockerRunCommand: args[0]})
			if err != nil {
				return fmt.Errorf("failed to convert command: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, readErr := cmdutil.ReadJSONBody(resp)
			if readErr != nil {
				return fmt.Errorf("failed to convert command: %w", readErr)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(body)
			}
			var result types.ConvertDockerRunResponse
			if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
				return fmt.Errorf("failed to convert command: failed to decode response: %w", decodeErr)
			}

			output.Header("Conversion Result")
			if result.ServiceName != "" {
				_, _ = fmt.Fprintf(output.Stdout(), "Service: %s\n\n", result.ServiceName)
			}
			if result.DockerCompose != "" {
				_, _ = fmt.Fprintln(output.Stdout(), "Docker Compose:")
				_, _ = fmt.Fprintln(output.Stdout(), result.DockerCompose)
			}
			if result.EnvVars != "" {
				_, _ = fmt.Fprintln(output.Stdout(), "Environment Variables:")
				_, _ = fmt.Fprintln(output.Stdout(), result.EnvVars)
			}
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newUpgradeCmd() *cobra.Command {
	var opts struct{ force, all, status, check bool }
	cmd := &cobra.Command{
		Use:          "upgrade",
		Short:        "Trigger system upgrade",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if opts.status && !opts.all {
				return errors.New("--status requires --all")
			}
			return cmdutil.ExclusiveFlags(cmd, "check", "all")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := runtime.From(cmd.Context())
			switch {
			case opts.check:
				return cmdutil.RunEnvironments(cmd, args, runUpgradeCheck, false)
			case app.PreviewEnabled() || opts.all:
				return runUpgradeAll(cmd, opts.force, opts.status)
			default:
				confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.force, "Are you sure you want to upgrade the system?")
				if confirmationErr != nil || !confirmed {
					return confirmationErr
				}
				c, err := cmdutil.ClientFromCommand(cmd)
				if err != nil {
					return err
				}
				return cmdutil.Run(cmd, c, cmdutil.Spec[any]{
					Method:         http.MethodPost,
					Path:           clitypes.SystemUpgrade(c.EnvID()),
					FailureMessage: "failed to upgrade system",
					Render:         func(any) error { output.Success("System upgrade initiated"); return nil },
				})
			}
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip confirmation")
	cmd.Flags().BoolVar(&opts.all, "all", false, "Update all environments with available updates")
	cmd.Flags().BoolVar(&opts.status, "status", false, "Show the status of the latest upgrade job (requires --all)")
	cmd.Flags().BoolVar(&opts.check, "check", false, "Only check whether an upgrade is available")
	cmdutil.SetScope(cmdutil.ScopeGlobal, cmd)
	return cmd
}

func runUpgradeCheck(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}
	// Upgrade checks return a direct payload rather than an API envelope.
	body, err := c.DoRaw(cmd.Context(), http.MethodGet, clitypes.SystemUpgradeCheck(c.EnvID()), nil)
	if err != nil {
		return fmt.Errorf("failed to check for upgrades: %w", err)
	}
	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintRawJSON(body)
	}
	var result types.UpgradeCheckResult
	if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
		return fmt.Errorf("failed to check for upgrades: failed to decode response: %w", decodeErr)
	}
	output.Header("Upgrade Check")
	output.KeyValue("Can Upgrade", strconv.FormatBool(result.CanUpgrade))
	output.KeyValue("Message", result.Message)
	if result.Error {
		output.KeyValue("Error", "true")
	}
	return nil
}

func runUpgradeAll(cmd *cobra.Command, force, status bool) error {
	app, _ := runtime.From(cmd.Context())
	if app.PreviewEnabled() {
		environments, discoveryErr := app.Environments(cmd.Context())
		if discoveryErr != nil {
			return discoveryErr
		}
		if len(environments) == 0 {
			return errors.New("no enabled environments available")
		}
		for _, env := range environments {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Upgrade environment: %s (%s)\n", cmp.Or(env.Name, env.ID), env.ID)
		}
		app.SelectEnvironment("0")
		defer app.SelectEnvironment("")
	}
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}
	if status {
		return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpgradeJob]{
			Method:         http.MethodGet,
			Path:           clitypes.SystemUpgradeJobCurrent(c.EnvID()),
			FailureMessage: "failed to get upgrade job status",
			Render: func(data types.UpgradeJob) error {
				output.Header("Upgrade Job Status")
				return renderUpgradeResults(data)
			},
		})
	}
	confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, force, "Are you sure you want to update all environments?")
	if confirmationErr != nil || !confirmed {
		return confirmationErr
	}
	return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpgradeJob]{
		Method:         http.MethodPost,
		Path:           clitypes.SystemUpgradeJobs(c.EnvID()),
		FailureMessage: "failed to start upgrade job",
		Render: func(data types.UpgradeJob) error {
			output.Success("Upgrade job started")
			return renderUpgradeResults(data)
		},
	})
}

func renderUpgradeResults(job types.UpgradeJob) error {
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
		return nil
	}
	rows := make([][]string, len(job.Results))
	for i, res := range job.Results {
		rows[i] = []string{cmp.Or(res.EnvironmentName, res.EnvironmentID), res.Status, res.FromVersion, res.ToVersion, cmp.Or(res.Error, "-")}
	}
	if _, spacingErr := fmt.Fprintln(output.Stdout()); spacingErr != nil {
		return fmt.Errorf("failed to print update status: %w", spacingErr)
	}
	return output.Table([]string{"ENVIRONMENT", "STATUS", "FROM", "TO", "ERROR"}, rows)
}
