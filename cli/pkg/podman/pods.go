package podman

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func newPodsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pods",
		Aliases: []string{"pod"},
		Short:   "Manage Podman pods",
	}
	cmd.AddCommand(
		newPodListCmd(),
		newPodInspectCmd(),
		newPodCreateCmd(),
		newPodDeleteCmd(),
	)
	for _, action := range []struct{ name, verb string }{
		{name: "start", verb: "Start"},
		{name: "stop", verb: "Stop"},
		{name: "restart", verb: "Restart"},
	} {
		var timeout int
		actionCmd := &cobra.Command{
			Use:          action.name + " <pod>",
			Short:        action.verb + " a pod",
			Args:         cobra.ExactArgs(1),
			SilenceUsage: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := cmdutil.ClientFromCommand(cmd)
				if err != nil {
					return err
				}

				return cmdutil.Run(cmd, c, cmdutil.Spec[base.MessageResponse]{
					Method:         http.MethodPost,
					Path:           types.PodmanPodAction(c.EnvID(), args[0], action.name),
					Body:           podman.PodActionRequest{Timeout: kit.Ternary(cmd.Flags().Changed("timeout"), new(timeout), nil)},
					FailureMessage: "failed to " + action.name + " pod",
					Render: func(base.MessageResponse) error {
						output.Success("Pod %s: %s completed", args[0], action.name)
						return nil
					},
				})
			},
		}
		if action.name == "stop" {
			actionCmd.Flags().IntVarP(&timeout, "timeout", "t", 10, "Seconds to wait before killing the pod")
		}
		cmd.AddCommand(actionCmd)
	}
	return cmd
}

func newPodListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List pods",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[podman.Pod]{
				Resource: "pods",
				Endpoint: types.PodmanPods(c.EnvID()),
				Params: cmdutil.ListParams{
					Resource:        "pods",
					Limit:           opts.limit,
					FallbackDefault: 20,
					Start:           opts.start,
					All:             opts.all,
				},
				Headers: []string{"ID", "NAME", "STATUS", "CONTAINERS", "CREATED"},
				Row: func(pod podman.Pod) []string {
					return []string{
						pod.ID[:min(len(pod.ID), 12)],
						pod.Name,
						output.TintStatus(pod.Status),
						strconv.Itoa(len(pod.Containers)),
						pod.Created.Format("2006-01-02 15:04"),
					}
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of pods to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newPodInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "inspect <pod>",
		Aliases:      []string{"get"},
		Short:        "Show pod details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[podman.PodInspect]{
				Method:         http.MethodGet,
				Path:           types.PodmanPod(c.EnvID(), args[0]),
				FailureMessage: "failed to inspect pod",
				Render: func(data podman.PodInspect) error {
					output.Header("Pod Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("State", output.TintStatus(data.State))
					output.KeyValue("Created", data.Created.Format("2006-01-02 15:04"))
					output.KeyValue("Hostname", cmp.Or(data.Hostname, "-"))
					output.KeyValue("Infra container", cmp.Or(data.InfraContainerID[:min(len(data.InfraContainerID), 12)], "-"))
					output.KeyValue("Shared namespaces", cmp.Or(strings.Join(data.SharedNamespaces, ", "), "-"))
					output.KeyValue("Containers", data.NumContainers)
					if len(data.Containers) == 0 {
						return nil
					}
					rows := make([][]string, len(data.Containers))
					for i, container := range data.Containers {
						rows[i] = []string{container.ID[:min(len(container.ID), 12)], container.Name, output.TintStatus(container.State)}
					}
					return cmdutil.PrintTable(cmd, []string{"ID", "NAME", "STATE"}, rows)
				},
			})
		},
	}
}

func newPodDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <pod>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a pod",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete pod %s?", args[0]),
				Path:           cmdutil.AppendQuery(types.PodmanPod(c.EnvID(), args[0]), url.Values{"force": []string{strconv.FormatBool(opts.force)}}),
				FailureMessage: "failed to delete pod",
				SuccessMessage: fmt.Sprintf("Pod %s deleted successfully", args[0]),
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Stop and remove running containers and skip the confirmation prompt")
	return cmd
}
