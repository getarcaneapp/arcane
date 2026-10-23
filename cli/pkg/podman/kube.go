package podman

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	internaltypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func newKubeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kube",
		Short: "Play, tear down, and generate Kubernetes YAML",
	}
	cmd.AddCommand(
		newKubePlayCmd(),
		newKubeDownCmd(),
		newKubeGenerateCmd(),
	)
	return cmd
}

func newKubePlayCmd() *cobra.Command {
	var opts struct {
		replace  bool
		noStart  bool
		networks []string
	}
	cmd := &cobra.Command{
		Use:          "play <file>",
		Short:        "Create pods from Kubernetes YAML",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read kube YAML: %w", err)
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.KubePlayReport]{
				Method: http.MethodPost,
				Path:   internaltypes.PodmanKube(c.EnvID(), "play"),
				Body: types.KubePlayRequest{
					YAML:     string(content),
					Start:    kit.Ternary(opts.noStart, new(false), nil),
					Replace:  opts.replace,
					Networks: opts.networks,
				},
				FailureMessage: "failed to play kube YAML",
				Render: func(data types.KubePlayReport) error {
					failed := slices.ContainsFunc(data.Pods, func(pod types.KubePlayPod) bool {
						return len(pod.ContainerErrors) > 0
					})
					if !failed {
						output.Success("Kube YAML played successfully")
					}
					for _, volume := range data.Volumes {
						output.KeyValue("Volume", volume.Name)
					}
					if len(data.Pods) == 0 {
						return nil
					}
					rows := make([][]string, len(data.Pods))
					for i, pod := range data.Pods {
						rows[i] = []string{
							pod.ID[:min(len(pod.ID), 12)],
							strconv.Itoa(len(pod.Containers)),
							strings.Join(pod.ContainerErrors, "; "),
						}
					}
					if printErr := cmdutil.PrintTable(cmd, []string{"POD", "CONTAINERS", "ERRORS"}, rows); printErr != nil {
						return printErr
					}
					if failed {
						return errors.New("some containers failed to start")
					}
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVar(&opts.replace, "replace", false, "Replace existing pods and containers")
	cmd.Flags().BoolVar(&opts.noStart, "no-start", false, "Create the pods without starting them")
	cmd.Flags().StringArrayVar(&opts.networks, "network", nil, "Network to attach the pods to (repeatable)")
	cmdutil.SetScope(cmdutil.ScopeDestination, cmd)
	return cmd
}

func newKubeDownCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeDestination},
		Use:          "down <file>",
		Short:        "Remove pods created from Kubernetes YAML",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("failed to read kube YAML: %w", err)
			}

			confirmed, confirmationErr := cmdutil.ConfirmOrCancel(cmd, opts.force, fmt.Sprintf("Are you sure you want to tear down the pods in %s?", args[0]))
			if confirmationErr != nil || !confirmed {
				return confirmationErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.KubeDownReport]{
				Method: http.MethodPost,
				Path:   internaltypes.PodmanKube(c.EnvID(), "down"),
				Body: types.KubeDownRequest{
					YAML:  string(content),
					Force: opts.force,
				},
				FailureMessage: "failed to tear down kube YAML",
				Render: func(data types.KubeDownReport) error {
					output.Success("Kube YAML torn down successfully")
					output.KeyValue("Stopped pods", len(data.StopReport))
					output.KeyValue("Removed pods", len(data.RmReport))
					return nil
				},
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Also remove volumes created by the YAML and skip the confirmation prompt")
	return cmd
}

func newKubeGenerateCmd() *cobra.Command {
	var opts struct {
		kind    types.KubeGenerateType
		service bool
	}
	cmd := &cobra.Command{
		Use:          "generate <name>...",
		Short:        "Generate Kubernetes YAML from pods, containers, or volumes",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.KubeGenerateResponse]{
				Method: http.MethodPost,
				Path:   internaltypes.PodmanKube(c.EnvID(), "generate"),
				Body: types.KubeGenerateRequest{
					Names:   args,
					Type:    opts.kind,
					Service: opts.service,
				},
				FailureMessage: "failed to generate kube YAML",
				Render: func(data types.KubeGenerateResponse) error {
					_, writeErr := fmt.Fprint(output.Stdout(), data.YAML)
					return writeErr
				},
			})
		},
	}
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.kind, "type", "", types.KubeGenerateTypePod, "Kubernetes kind to generate",
		types.KubeGenerateTypePod,
		types.KubeGenerateTypeService,
		types.KubeGenerateTypeDeployment,
		types.KubeGenerateTypeDaemonSet,
		types.KubeGenerateTypeJob,
	)
	cmd.Flags().BoolVar(&opts.service, "service", false, "Also generate a Kubernetes service")
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}
