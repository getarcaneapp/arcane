package podman

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "podman",
		Aliases: []string{"pm"},
		Short:   "Manage Podman pods, quadlets, kube YAML, and secrets",
	}
	cmd.AddCommand(
		newStatusCmd(),
		newAutoUpdateCmd(),
		newPodsCmd(),
		newQuadletsCmd(),
		newKubeCmd(),
		newSecretsCmd(),
	)
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show Podman detection and capabilities",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanStatus]{
				Method:         http.MethodGet,
				Path:           internaltypes.PodmanStatus(c.EnvID()),
				FailureMessage: "failed to get Podman status",
				Render: func(data types.PodmanStatus) error {
					if !data.Detected {
						output.Info("Podman was not detected on this environment")
						return nil
					}
					output.Header("Podman Status")
					output.KeyValue("Version", data.Version)
					output.KeyValue("API version", data.APIVersion)
					output.KeyValue("Rootless", kit.Ternary(data.Rootless, "Yes", "No"))
					output.KeyValue("Quadlets", kit.Ternary(data.QuadletsSupported, "Supported", "Unsupported"))
					output.KeyValue("Quadlet apps", kit.Ternary(data.QuadletAppsSupported, "Supported", "Unsupported"))
					output.KeyValue("Auto-update mode", data.AutoUpdateMode)
					output.KeyValue("systemd", kit.Ternary(data.SystemdAvailable, string(data.SystemdScope), "Unavailable"))
					if data.SystemdReason != "" {
						output.KeyValue("systemd reason", data.SystemdReason)
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newAutoUpdateCmd() *cobra.Command {
	var opts struct {
		dryRun bool
		force  bool
	}
	cmd := &cobra.Command{
		Use:          "autoupdate",
		Aliases:      []string{"auto-update"},
		Short:        "Run podman auto-update",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.force || opts.dryRun, "Are you sure you want to auto-update Podman containers?")
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PodmanAutoUpdateResult]{
				Method:         http.MethodPost,
				Path:           internaltypes.PodmanAutoUpdate(c.EnvID()),
				Body:           types.PodmanAutoUpdateOptions{DryRun: opts.dryRun},
				FailureMessage: "failed to run Podman auto-update",
				Render: func(data types.PodmanAutoUpdateResult) error {
					output.Header("Podman Auto-Update")
					output.KeyValue("Mode", data.Mode)
					if data.Unit != nil {
						output.KeyValue("Unit", data.Unit.Name)
						output.KeyValue("State", data.Unit.ActiveState+"/"+data.Unit.SubState)
					}
					for _, message := range data.Errors {
						output.Warning("%s", message)
					}
					if len(data.Reports) > 0 {
						rows := make([][]string, len(data.Reports))
						for i, report := range data.Reports {
							rows[i] = []string{
								report.ContainerName,
								report.ImageName,
								report.Policy,
								report.SystemdUnit,
								report.Updated,
							}
						}
						if printErr := cmdutil.PrintTable(cmd, []string{"CONTAINER", "IMAGE", "POLICY", "UNIT", "UPDATED"}, rows); printErr != nil {
							return printErr
						}
					}
					if len(data.Errors) > 0 {
						return fmt.Errorf("auto-update reported %d error(s)", len(data.Errors))
					}
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Report pending updates without applying them")
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Skip the confirmation prompt")
	cmdutil.SetScope(cmdutil.ScopeAll, cmd)
	return cmd
}

func searchPodman[S any](ctx context.Context, c *client.Client, path string, matches func(S) bool) ([]S, error) {
	result, err := c.GetJSON[[]S](ctx, fmt.Sprintf("%s?limit=%d", path, cmdutil.ShowAllLimit))
	if statusErr, ok := errors.AsType[*client.StatusError](err); ok && statusErr.StatusCode == http.StatusBadRequest {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(result.Data, func(item S) bool {
		return !matches(item)
	}), nil
}
